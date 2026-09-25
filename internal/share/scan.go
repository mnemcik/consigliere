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
	RuleToken       = "token"
	RulePrivateKey  = "private-key"
	RuleJWT         = "jwt"
	RuleAssignment  = "credential-assignment"
	RuleLocalPath   = "local-path"
	RuleVaultRef    = "vault-reference"
	RuleDenylist    = "denylist"
	RulePlaceholder = "placeholder"
)

// Finding is one match of a scan rule in a rendered file.
type Finding struct {
	File  string // published path
	Line  int    // 1-based line in the rendered file
	Rule  string
	Match string // what matched; masked for secret-class rules
	Hash  string // identifies the line for an acknowledgement
}

// Ack acknowledges a finding the owner has judged safe to publish. It names a
// rule and a line hash, so editing the line brings the finding back.
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

type patternRule struct {
	name   string
	re     *regexp.Regexp
	secret bool // mask the match in reports
}

// patternRules match values, never bare words: "client_id/secret via API
// management" describes a credential without containing one and must pass.
var patternRules = []patternRule{
	{RuleToken, regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,}|xox[abprs]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|sk-(?:ant-)?[A-Za-z0-9_-]{20,})`), true},
	{RuleToken, regexp.MustCompile(`https://hooks\.slack\.com/services/[A-Za-z0-9/]+`), true},
	{RulePrivateKey, regexp.MustCompile(`-{5}BEGIN [A-Z ]*PRIVATE KEY-{5}`), false},
	{RuleJWT, regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`), true},
	// Only paths naming a user account are flagged: ~/.config and the like are
	// conventional locations that identify neither the owner nor the machine.
	{RuleLocalPath, regexp.MustCompile(`(?:/Users/[A-Za-z0-9._-]+|/home/[A-Za-z0-9._-]+|[A-Za-z]:\\Users\\[A-Za-z0-9._ -]+)`), false},
	// A real reference names a vault and an item (op://vault/item); a bare
	// "op://..." in prose is a mention of the syntax, not a reference.
	{RuleVaultRef, regexp.MustCompile(`op://[^/\s)"'` + "`" + `>]+/[^\s)"'` + "`" + `>]+`), false},
}

// assignmentRe finds a credential-looking key assigned a value. The value is
// then judged by entropy, so prose ("secret: see the vault") does not trip it.
var assignmentRe = regexp.MustCompile(`(?i)\b(?:api[_-]?key|access[_-]?key|account[_-]?key|client[_-]?secret|secret|token|password|passwd|pwd|auth)\b["']?\s*[:=]\s*["']?([^\s"'` + "`" + `,;]{12,})`)

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
				if !acked[Ack{Rule: rule, Hash: hash}] {
					findings = append(findings, Finding{File: file, Line: i + 1, Rule: rule, Match: match, Hash: hash})
				}
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
	for _, r := range patternRules {
		for _, m := range r.re.FindAllString(line, -1) {
			m = strings.TrimLeft(m, " \t(\"'`")
			if r.secret {
				m = mask(m)
			}
			add(r.name, m)
		}
	}
	for _, m := range assignmentRe.FindAllStringSubmatch(line, -1) {
		if v := m[1]; looksSecret(v) {
			add(RuleAssignment, mask(v))
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

// looksSecret reports whether an assigned value is plausibly a credential:
// not an obvious placeholder, a mix of character classes, and high entropy.
func looksSecret(v string) bool {
	if strings.ContainsAny(v, "<>{}*$") || strings.Contains(v, "...") || strings.Contains(v, "…") {
		return false
	}
	var letter, digit bool
	for _, c := range v {
		switch {
		case c >= '0' && c <= '9':
			digit = true
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			letter = true
		}
	}
	return letter && digit && entropy(v) >= 3.5
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

// mask keeps enough of a secret to find it in the source without printing it
// into terminals and transcripts.
func mask(s string) string {
	if len(s) <= 8 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + "…(" + strconv.Itoa(len(s)) + " chars)"
}

// lineHash identifies a line for acknowledgements: the first 16 hex digits of
// the SHA-256 of the line with surrounding whitespace trimmed.
func lineHash(line string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(line)))
	return hex.EncodeToString(sum[:])[:16]
}
