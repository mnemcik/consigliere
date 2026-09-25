package share

import (
	"crypto/sha256"
	"encoding/base64"
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
// Order matters: when two rules match the same value, the first one reports
// it, so specific formats (a GitHub token, a JWT) come before generic
// carriers (Bearer, URL userinfo).
var patternRules = []patternRule{
	{RuleToken, regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,}|glpat-[A-Za-z0-9_-]{20,}` +
		`|xox[abprs]-[A-Za-z0-9-]{10,}|xapp-[A-Za-z0-9-]{10,}|(?:AKIA|ASIA)[0-9A-Z]{16}|sk-(?:ant-)?[A-Za-z0-9_-]{20,}` +
		`|sk_(?:live|test)_[A-Za-z0-9]{16,}|AIza[0-9A-Za-z_-]{35}|npm_[A-Za-z0-9]{36})\b`), 0, showPrefix, nil},
	{RuleJWT, regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`), 0, showPrefix, nil},
	{RuleToken, regexp.MustCompile(`https://hooks\.slack\.com/services/[A-Za-z0-9/]+`), 0, showLengthOnly, nil},
	// A WWW-Authenticate challenge (Bearer authorization_uri="...") starts
	// with an auth-parameter name, and a real token mixes letters and digits,
	// which prose ("Bearer authentication flows") and placeholders do not.
	{RuleToken, regexp.MustCompile(`(?i)\bbearer\s+([A-Za-z0-9._~+/=-]{16,})`), 1, showLengthOnly,
		func(v string) bool { return !authParamRe.MatchString(v) && looksPassword(v) }},
	{RuleToken, regexp.MustCompile(`(?i)\bbasic\s+([A-Za-z0-9+/]{12,}={0,2})`), 1, showLengthOnly, isBasicCredential},
	// userinfo runs to the last @ before the path, as net/url parses it, so a
	// password may itself contain @; the user part may be empty (redis://:pw@).
	{RuleURLCredential, regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9+.-]*://[^\s/?#:@<>` + q + `]*:([^\s/?#<>` + q + `]+)@`), 1, showLengthOnly,
		looksPassword},
	// curl -u user:pass. -u means other things elsewhere (date -u is UTC), so
	// the rule needs a curl on the line and an identifier-shaped user.
	{RuleURLCredential, regexp.MustCompile(`\bcurl\b.*?\s(?:-u|--user)\s+[A-Za-z0-9._@-]+:(\S+)`), 1, showLengthOnly, looksPassword},
	{RuleAssignment, regexp.MustCompile(`--password[=\s]+(\S+)`), 1, showLengthOnly, looksPassword},
	{RulePrivateKey, regexp.MustCompile(`-{5}BEGIN [A-Z ]*PRIVATE KEY(?: BLOCK)?-{5}`), 0, showTruncated, nil},
	// Only paths naming a user account are flagged: ~/.config and the like are
	// conventional locations that identify neither the owner nor the machine.
	// A POSIX form must start a path (line start, whitespace, a quote, a paren,
	// = or a PATH-style :) or follow file://, and continue past the account
	// name, so a web route such as "GET /Users/me" is not taken for a home.
	{RuleLocalPath, regexp.MustCompile(`(?:^|[\s(=:` + q + `]|file://)(/(?:Users|home)/[A-Za-z0-9._-]+)/`), 1, showTruncated, nil},
	{RuleLocalPath, regexp.MustCompile(`(?i)\b([a-z]:[\\/]users[\\/][a-z0-9._-]+)`), 1, showTruncated, nil},
	// A real reference names a vault and an item (op://vault/item); a bare
	// "op://..." in prose is a mention of the syntax, not a reference.
	{RuleVaultRef, regexp.MustCompile(`op://[^/\s)>` + q + `]+/[^\s)>` + q + `]+`), 0, showTruncated, nil},
}

var authParamRe = regexp.MustCompile(`^[a-z_]+=`)

// assignmentRe finds the "name:" or "name=" of any assignment; the value is
// read separately (assignmentValue), so a value never swallows a later
// assignment on the same line (sv=...&sig=...). Whether name is a credential
// is decided from its segments (credentialKind), not by substring, so
// assignee, monkey, authority and compass are ordinary names.
var assignmentRe = regexp.MustCompile(`(?:^|[^A-Za-z0-9_-])([A-Za-z][A-Za-z0-9_-]*)["']?\s*[:=]\s*["']?`)

// resourcePathRe matches a lowercase slash-separated resource path
// (projects/idella-prod/secrets/db-2024): an identifier, not a credential.
var resourcePathRe = regexp.MustCompile(`^[a-z0-9._-]+(?:/[a-z0-9._-]+)+/?$`)

var uuidRe = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var hexRe = regexp.MustCompile(`(?i)^[0-9a-f]{16,}$`)

// camelRe splits camelCase names into segments (clientSecret → client, Secret).
var camelRe = regexp.MustCompile(`[A-Z]?[a-z0-9]+|[A-Z]+(?:[^a-z]|$)`)

// placeholderRe matches a template placeholder such as {Project Title}: a
// capitalised multi-word phrase in single braces. Single-word forms ({setId})
// are API path parameters, not placeholders, and are left alone.
var placeholderRe = regexp.MustCompile(`\{[A-Z][a-z]+(?: [A-Za-z]+)+\}`)

// Scan checks rendered files for content that must not leave the workspace
// and returns the unacknowledged findings, sorted by file, line, rule and
// match. A file's published path is scanned too, as line 0.
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
		lines := append([]string{file}, strings.Split(body, "\n")...)
		for i, line := range lines {
			hash := lineHash(line)
			add := func(rule, match string) {
				if rule != RulePrivateKey && acked[Ack{Rule: rule, Hash: hash}] {
					return
				}
				findings = append(findings, Finding{File: file, Line: i, Rule: rule, Match: match, Hash: hash})
			}
			scanLine(line, deny, add)
		}
	}
	sort.SliceStable(findings, func(a, b int) bool {
		fa, fb := findings[a], findings[b]
		switch {
		case fa.File != fb.File:
			return fa.File < fb.File
		case fa.Line != fb.Line:
			return fa.Line < fb.Line
		case fa.Rule != fb.Rule:
			return fa.Rule < fb.Rule
		}
		return fa.Match < fb.Match
	})
	return findings
}

func scanLine(line string, deny []string, add func(rule, match string)) {
	// Byte ranges already reported, so one value is reported once however
	// many rules match it (token=ghp_..., Bearer eyJ..., token: op://...).
	var reported [][2]int
	for _, r := range patternRules {
		for _, loc := range r.re.FindAllStringSubmatchIndex(line, -1) {
			lo, hi := loc[2*r.group], loc[2*r.group+1]
			if lo < 0 || overlaps(lo, hi, reported) {
				continue
			}
			value := line[lo:hi]
			if r.accept != nil && !r.accept(value) {
				continue
			}
			reported = append(reported, [2]int{lo, hi})
			add(r.name, show(value, r.show))
		}
	}
	for _, loc := range assignmentRe.FindAllStringSubmatchIndex(line, -1) {
		kind := credentialKind(line[loc[2]:loc[3]])
		if kind == kindNone {
			continue
		}
		lo, hi := assignmentValue(line, loc[1], kind)
		if hi-lo < 8 || overlaps(lo, hi, reported) {
			continue
		}
		if value := line[lo:hi]; assignedSecret(kind, value) {
			reported = append(reported, [2]int{lo, hi})
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

// assignmentValue returns the byte range of the value starting at start. A
// password runs to whitespace or a quote, since passwords contain , ; and &;
// any other value also ends at a list or query separator (sig=...&se=...).
func assignmentValue(line string, start, kind int) (lo, hi int) {
	stop := " \t\r\n" + q
	if kind != kindPassword {
		stop += ",;&<>"
	}
	end := strings.IndexAny(line[start:], stop)
	if end < 0 {
		return start, len(line)
	}
	return start, start + end
}

// How strongly an assignment's name marks its value as a credential.
const (
	kindNone     = iota
	kindKey      // only a trailing "key" segment (ssh_key, Subscription-Key)
	kindSecret   // secret, token, credential(s), apikey, auth, sig
	kindPassword // pass, password, passwd, passphrase, pwd
)

// credentialKind classifies an assignment name by its segments, split on _ -
// and camelCase: SMTP_PASS and PGPASSWORD are passwords, clientSecret is a
// secret, Ocp-Apim-Subscription-Key is a key, while compass, assignee,
// authority, passport_number and primary_key_column are none.
func credentialKind(name string) int {
	var segs []string
	for _, part := range strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' }) {
		for _, s := range camelRe.FindAllString(part, -1) {
			segs = append(segs, strings.ToLower(s))
		}
	}
	kind := kindNone
	for i, s := range segs {
		switch {
		case s == "pass" || s == "pwd" || strings.HasSuffix(s, "password") || strings.HasSuffix(s, "passwd") || strings.HasSuffix(s, "passphrase"):
			return kindPassword
		case s == "secret" || s == "secrets" || s == "token" || s == "credential" || s == "credentials" ||
			s == "apikey" || s == "auth" || s == "sig":
			kind = kindSecret
		case s == "key" && i == len(segs)-1 && kind == kindNone:
			kind = kindKey
		}
	}
	return kind
}

// assignedSecret reports whether value, assigned to a name of the given kind,
// is plausibly a real credential.
func assignedSecret(kind int, value string) bool {
	switch kind {
	case kindPassword:
		return looksPassword(value)
	case kindSecret, kindKey:
		if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") ||
			resourcePathRe.MatchString(value) || (kind == kindKey && uuidRe.MatchString(value)) {
			return false
		}
		return looksSecret(value)
	}
	return false
}

func overlaps(lo, hi int, spans [][2]int) bool {
	for _, s := range spans {
		if lo < s[1] && s[0] < hi {
			return true
		}
	}
	return false
}

// isPlaceholder reports values that stand in for a credential by their shape:
// <your-key>, ${VAR}, {{var}}, $VAR, %VAR%, ***, or an elided "..." — not any
// value that merely contains $ or *, which real passwords do.
func isPlaceholder(v string) bool {
	switch {
	case strings.HasPrefix(v, "<") && strings.HasSuffix(v, ">"),
		strings.Contains(v, "${"), strings.Contains(v, "{{"),
		strings.Contains(v, "***"), strings.Contains(v, "..."), strings.Contains(v, "…"),
		envRefRe.MatchString(v):
		return true
	}
	return false
}

var envRefRe = regexp.MustCompile(`^(?:\$[A-Z_][A-Z0-9_]*|%[A-Z_][A-Z0-9_]*%)$`)

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
// value of 8+ characters mixing letters and digits counts, unless it is a
// placeholder.
func looksPassword(v string) bool {
	letter, digit := classes(v)
	return len(v) >= 8 && letter && digit && !isPlaceholder(v)
}

// looksSecret reports whether a key-position value is plausibly a generated
// credential: 12+ characters mixing letters and digits, not a placeholder,
// and either hex (API keys are often 32 hex digits, whose entropy tops out at
// 4 bits) or high-entropy for its length. The threshold scales with length,
// because a short random string cannot reach a fixed 3.5 bits per character.
func looksSecret(v string) bool {
	letter, digit := classes(v)
	if len(v) < 12 || !letter || !digit || isPlaceholder(v) {
		return false
	}
	if hexRe.MatchString(v) {
		// Random hex tops out at 4 bits per character; a run like aaaa…1 is
		// hex-shaped but not random.
		return entropy(v) >= math.Min(3.0, 0.7*math.Log2(float64(len(v))))
	}
	return entropy(v) >= math.Min(3.5, 0.75*math.Log2(float64(len(v))))
}

// isBasicCredential reports whether a Basic auth value decodes to user:pass.
func isBasicCredential(v string) bool {
	b, err := base64.StdEncoding.DecodeString(v)
	return err == nil && strings.Contains(string(b), ":")
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
