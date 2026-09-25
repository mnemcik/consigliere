package share

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Scan rule identifiers, as reported in findings and named in acknowledgements.
const (
	RuleToken         = "token"
	RuleURLCredential = "url-credential" //nolint:gosec // G101: a rule identifier, not a credential
	RulePrivateKey    = "private-key"
	RuleJWT           = "jwt"
	RuleAssignment    = "credential-assignment"
	RuleLocalPath     = "local-path"
	RuleVaultRef      = "vault-reference"
	RuleDenylist      = "denylist"
	RulePlaceholder   = "placeholder"
)

// Finding is one match of a scan rule in a rendered file.
type Finding struct {
	File  string // published path
	Line  int    // 1-based line in the rendered file
	Rule  string
	Match string // what matched; masked for secret-class rules, truncated otherwise
	Hash  string // identifies the line for an acknowledgement
}

// Ack acknowledges a finding the owner has judged safe to publish. It names a
// rule and the hash of a trimmed line, so it covers every identical line and
// editing the line brings the finding back. A private-key finding cannot be
// acknowledged: only its header line is matched, so acknowledging it would
// let the key body through.
type Ack struct {
	Rule string
	Hash string
}

// ScanOptions configures Scan.
type ScanOptions struct {
	// Denylist holds owner-defined terms (customer names, codenames) that must
	// never leave, matched case-insensitively.
	Denylist     []string
	Acknowledged []Ack
}

// How a match is shown in a report. Reports reach terminals and AI
// transcripts, so a secret's value is never printed whole.
const (
	showTruncated  = iota // not a secret: shown, cut to a readable length
	showPrefix            // secret with a public prefix (ghp_, eyJ): prefix + length
	showLengthOnly        // secret with no public part (a password): length only
)

type patternRule struct {
	name  string
	re    *regexp.Regexp
	group int // submatch that is the finding; 0 means the whole match
	show  int
	// accept, when set, must approve the matched value for it to count.
	accept func(value string) bool
}

const q = `"'` + "`" // quote characters, for building classes

// patternRules match values, never bare words: "client_id/secret via API
// management" describes a credential without containing one and must pass.
var patternRules = []patternRule{
	{RuleToken, regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,}|glpat-[A-Za-z0-9_-]{20,}` +
		`|xox[abprs]-[A-Za-z0-9-]{10,}|xapp-[A-Za-z0-9-]{10,}|(?:AKIA|ASIA)[0-9A-Z]{16}|sk-(?:ant-)?[A-Za-z0-9_-]{20,}` +
		`|sk_(?:live|test)_[A-Za-z0-9]{16,}|AIza[0-9A-Za-z_-]{35}|npm_[A-Za-z0-9]{36})`), 0, showPrefix, nil},
	{RuleToken, regexp.MustCompile(`https://hooks\.slack\.com/services/[A-Za-z0-9/]+`), 0, showLengthOnly, nil},
	// A WWW-Authenticate challenge (Bearer authorization_uri="...") starts
	// with an auth-parameter name, which a token never does.
	{RuleToken, regexp.MustCompile(`(?i)\bbearer\s+([A-Za-z0-9._~+/=-]{16,})`), 1, showLengthOnly,
		func(v string) bool { return !authParamRe.MatchString(v) }},
	{RuleURLCredential, regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9+.-]*://[^\s/:@<>` + q + `]+:([^\s/@<>` + q + `]+)@`), 1, showLengthOnly,
		looksPassword},
	{RulePrivateKey, regexp.MustCompile(`-{5}BEGIN [A-Z ]*PRIVATE KEY(?: BLOCK)?-{5}`), 0, showTruncated, nil},
	{RuleJWT, regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`), 0, showPrefix, nil},
	// Only paths naming a user account are flagged: ~/.config and the like are
	// conventional locations that identify neither the owner nor the machine.
	// A POSIX form must start a path and continue past the account name, so a
	// web route such as "GET /Users/me" is not taken for a home directory.
	{RuleLocalPath, regexp.MustCompile(`(?:^|[\s(=` + q + `])(/(?:Users|home)/[A-Za-z0-9._-]+)/`), 1, showTruncated, nil},
	{RuleLocalPath, regexp.MustCompile(`(?i)\b([a-z]:\\users\\[a-z0-9._-]+)`), 1, showTruncated, nil},
	// A real reference names a vault and an item (op://vault/item); a bare
	// "op://..." in prose is a mention of the syntax, not a reference.
	{RuleVaultRef, regexp.MustCompile(`op://[^/\s)>` + q + `]+/[^\s)>` + q + `]+`), 0, showTruncated, nil},
}

var authParamRe = regexp.MustCompile(`^[a-z_]+=`)

// assignmentRe finds a credential-looking key assigned a value. The key may be
// part of a longer name (GITHUB_TOKEN, db_password, AZURE_CLIENT_SECRET), and
// sig covers SAS query strings. Group 1 is the key name, group 2 the value,
// which is then judged on its own, so prose ("secret: see the vault") passes.
var assignmentRe = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])([A-Za-z0-9_-]*?(?:api[_-]?key|access[_-]?key|account[_-]?key` +
	`|client[_-]?secret|secret|token|password|passwd|pwd|auth|sig|key)[A-Za-z0-9_-]*)["']?\s*[:=]\s*["']?([^\s,;&<>` + q + `]{8,})`)

// resourcePathRe matches a lowercase slash-separated resource path
// (projects/idella-prod/secrets/db-2024): an identifier, not a credential.
var resourcePathRe = regexp.MustCompile(`^[a-z0-9._-]+(?:/[a-z0-9._-]+)+/?$`)

// placeholderRe matches a template placeholder such as {Project Title}: a
// capitalised multi-word phrase in single braces. Single-word forms ({setId})
// are API path parameters, not placeholders, and are left alone.
var placeholderRe = regexp.MustCompile(`\{[A-Z][a-z]+(?: [A-Za-z]+)+\}`)

// Scan checks rendered files for content that must not leave the workspace
// and returns the unacknowledged findings, sorted by file and line.
func Scan(files map[string]string, opts ScanOptions) []Finding {
	acked := make(map[Ack]bool, len(opts.Acknowledged))
	for _, a := range opts.Acknowledged {
		acked[a] = true
	}
	var deny []string
	for _, term := range opts.Denylist {
		if t := strings.ToLower(strings.TrimSpace(term)); t != "" {
			deny = append(deny, t)
		}
	}

	var findings []Finding
	for file, body := range files {
		for i, line := range strings.Split(body, "\n") {
			hash := lineHash(line)
			add := func(rule, match string) {
				if rule != RulePrivateKey && acked[Ack{Rule: rule, Hash: hash}] {
					return
				}
				findings = append(findings, Finding{File: file, Line: i + 1, Rule: rule, Match: match, Hash: hash})
			}
			scanLine(line, deny, add)
		}
	}
	sort.Slice(findings, func(a, b int) bool {
		fa, fb := findings[a], findings[b]
		if fa.File != fb.File {
			return fa.File < fb.File
		}
		if fa.Line != fb.Line {
			return fa.Line < fb.Line
		}
		return fa.Rule < fb.Rule
	})
	return findings
}

func scanLine(line string, deny []string, add func(rule, match string)) {
	// Byte ranges already reported as secrets, so an assignment whose value is
	// one of them (token=ghp_...) is not reported a second time.
	var secretSpans [][2]int
	for _, r := range patternRules {
		for _, loc := range r.re.FindAllStringSubmatchIndex(line, -1) {
			lo, hi := loc[2*r.group], loc[2*r.group+1]
			if lo < 0 {
				continue
			}
			value := line[lo:hi]
			if r.accept != nil && !r.accept(value) {
				continue
			}
			if r.show != showTruncated {
				secretSpans = append(secretSpans, [2]int{lo, hi})
			}
			add(r.name, show(value, r.show))
		}
	}
	for _, loc := range assignmentRe.FindAllStringSubmatchIndex(line, -1) {
		key, value := strings.ToLower(line[loc[2]:loc[3]]), line[loc[4]:loc[5]]
		if overlaps(loc[4], loc[5], secretSpans) || resourcePathRe.MatchString(value) {
			continue
		}
		if strings.Contains(key, "pass") || strings.Contains(key, "pwd") {
			if looksPassword(value) {
				add(RuleAssignment, show(value, showLengthOnly))
			}
			continue
		}
		if len(value) >= 12 && looksSecret(value) {
			add(RuleAssignment, show(value, showLengthOnly))
		}
	}
	for _, loc := range placeholderRe.FindAllStringIndex(line, -1) {
		// {{Name}} is a templating language's own syntax, not a leftover.
		if (loc[0] > 0 && line[loc[0]-1] == '{') || (loc[1] < len(line) && line[loc[1]] == '}') {
			continue
		}
		add(RulePlaceholder, line[loc[0]:loc[1]])
	}
	if strings.Contains(line, OwnerPlaceholder) {
		add(RulePlaceholder, OwnerPlaceholder)
	}
	lower := strings.ToLower(line)
	for _, term := range deny {
		if strings.Contains(lower, term) {
			add(RuleDenylist, term)
		}
	}
}

func overlaps(lo, hi int, spans [][2]int) bool {
	for _, s := range spans {
		if lo < s[1] && s[0] < hi {
			return true
		}
	}
	return false
}

// isPlaceholder reports values that stand in for a credential: <your-key>,
// ${VAR}, ***, or an elided "...".
func isPlaceholder(v string) bool {
	return strings.ContainsAny(v, "<>{}*$") || strings.Contains(v, "...") || strings.Contains(v, "…")
}

// classes reports whether v contains letters and digits.
func classes(v string) (letter, digit bool) {
	for _, c := range v {
		switch {
		case c >= '0' && c <= '9':
			digit = true
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			letter = true
		}
	}
	return letter, digit
}

// looksPassword reports whether a password-position value is plausibly real.
// Human-chosen passwords are low-entropy by design (Summer2024!!), so any
// value mixing letters and digits counts, unless it is a placeholder.
func looksPassword(v string) bool {
	letter, digit := classes(v)
	return len(v) >= 8 && letter && digit && !isPlaceholder(v)
}

// looksSecret reports whether a key-position value is plausibly a generated
// credential: not a placeholder, letters and digits, and high entropy.
func looksSecret(v string) bool {
	letter, digit := classes(v)
	return letter && digit && !isPlaceholder(v) && entropy(v) >= 3.5
}

// entropy is the Shannon entropy of s in bits per character.
func entropy(s string) float64 {
	counts := map[rune]int{}
	n := 0
	for _, c := range s {
		counts[c]++
		n++
	}
	var h float64
	for _, k := range counts {
		p := float64(k) / float64(n)
		h -= p * math.Log2(p)
	}
	return h
}

// maxShown caps a non-secret match in a report, so a runaway match can never
// carry the rest of a line (and whatever it holds) into the output.
const maxShown = 60

// show renders a match for a report according to mode. It counts and cuts in
// runes, so a multi-byte value is never split mid-character.
func show(v string, mode int) string {
	r := []rune(v)
	switch mode {
	case showPrefix:
		if len(r) > 8 {
			return string(r[:4]) + "…(" + strconv.Itoa(len(r)) + " chars)"
		}
		return strings.Repeat("*", len(r))
	case showLengthOnly:
		return "***(" + strconv.Itoa(len(r)) + " chars)"
	}
	if len(r) > maxShown {
		return string(r[:maxShown]) + "…"
	}
	return v
}

// lineHash identifies a line for acknowledgements: the first 16 hex digits of
// the SHA-256 of the line with surrounding whitespace trimmed.
func lineHash(line string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(line)))
	return hex.EncodeToString(sum[:])[:16]
}
