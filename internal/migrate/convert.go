// Package migrate converts workspace items from a `## Meta` bullet block to
// YAML frontmatter.
//
// The conversion is lossless by construction: every field bullet becomes a
// frontmatter key, and everything else inside the Meta section -- prose
// paragraphs, comments the templates did not ship -- stays in the body where
// it was. Only the `## Meta` heading, the bullets and the vocabulary comments
// cg's own templates shipped are removed. Readers already accept both forms
// (internal/meta), so a part-migrated workspace is valid at every step.
package migrate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mnemcik/consigliere/internal/manifest"
)

// Result describes what Convert did to one file.
type Result struct {
	// Changed is false when the file has no `## Meta` block to convert.
	Changed bool
	// Conflicts lists fields whose `## Meta` value differs from the value
	// already in frontmatter. Frontmatter wins (DEC-005); these are for review.
	Conflicts []string
	// Notes lists anything else worth a reviewer's attention: a date collapse
	// that discarded a later date, a duplicate label.
	Notes []string
}

var (
	bulletRe      = regexp.MustCompile(`^- \*\*([^*]+):\*\*\s?(.*)$`)
	inlineCommRe  = regexp.MustCompile(`\s*<!--(.*?)-->\s*`)
	dateRe        = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2})\b`)
	backtickedRe  = regexp.MustCompile("`([^`]+)`")
	linkTextRe    = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	leadTokenRe   = regexp.MustCompile("^`?([A-Za-z][A-Za-z0-9-]*)`?(.*)$")
	placeholderRe = regexp.MustCompile(`^\{.*\}$`)
	nonKeyRe      = regexp.MustCompile(`[^a-z0-9]+`)
)

// Keys the converter treats specially.
const (
	keyAreas        = "areas"
	keyCreated      = "created"
	keyOutputType   = "output_type"
	keyLastReviewed = "last_reviewed"
	keySlug         = "slug"
)

// keyOrder is where a key goes when Convert adds it. Keys already in the
// frontmatter keep their position; anything not listed sorts after these.
var keyOrder = []string{
	"title", "status", "status_note", "priority", keyAreas, "tags", "category",
	keySlug, keyCreated, keyLastReviewed, "color", "authority", "verified",
	"origin", keyOutputType, "source_session", "session_context",
}

// labelKeys maps a `## Meta` label (lower-cased) to its frontmatter key.
// Unlisted labels become their snake_case form.
var labelKeys = map[string]string{
	"last reviewed":   keyLastReviewed,
	"output type":     keyOutputType,
	"source session":  "source_session",
	"session context": "session_context",
	// DEC-010: every creation-date label collapses onto created.
	"started":  keyCreated,
	"observed": keyCreated,
	"date":     keyCreated,
	// Earlier review entries join the latest one in ## Review History.
	"previously reviewed": historyKey,
}

// historyKey marks a label whose value is a review-history entry rather than
// a frontmatter field.
const historyKey = "\x00history"

// vocabKeys hold a controlled vocabulary: a leading token, lower-cased, with
// any trailing prose split into <key>_note (DEC-016).
var vocabKeys = map[string]bool{
	"status": true, "priority": true, "category": true, keyOutputType: true, "authority": true,
}

// listKeys hold slugs or tags.
var listKeys = map[string]bool{keyAreas: true, "tags": true}

// dateKeys hold a date; created takes the earliest when several labels map to
// it (DEC-014).
var dateKeys = map[string]bool{keyCreated: true, "verified": true, keyLastReviewed: true}

// shippedComments are the vocabulary comments cg's templates placed inside the
// Meta block. The new templates carry them as YAML comments, so they are
// dropped. Any other comment is the workspace's own and is kept.
var shippedComments = map[string]bool{
	"<!-- Statuses: defining → in-progress → done | on-hold -->":                                                     true,
	"<!-- Statuses: raw → exploring → ready → parked | rejected -->":                                                 true,
	"<!-- Statuses: draft → promoted | rejected -->":                                                                 true,
	`<!-- When status reaches "ready", create a project from templates/project.md -->`:                               true,
	`<!-- When status reaches "ready", create a project from templates/project/ -->`:                                 true,
	"<!-- Categories: tool-gotchas | workflow | architecture | process | research | reference | troubleshooting -->": true,
	"<!-- Reserved tag: `ai-instructions` marks a note whose content governs how Claude behaves, as opposed to one documenting how a tool behaves. It exists so the behavioural rules can be found and reviewed as a set. Framework notes shipped by cg carry it already. -->": true,
}

// field is one parsed `## Meta` bullet.
type field struct {
	label, value, comment string
}

// Convert rewrites one markdown document from a `## Meta` block to
// frontmatter. A document with no `## Meta` block is returned unchanged.
func Convert(doc string) (string, Result, error) {
	var res Result
	crlf := strings.Contains(doc, "\r\n")
	if crlf {
		doc = strings.ReplaceAll(doc, "\r\n", "\n")
	}

	fmBlock, body := manifest.SplitFrontmatter(doc)
	lines := strings.Split(body, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "## Meta" {
			start = i
			break
		}
	}
	if start < 0 {
		return restore(doc, crlf), res, nil
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") || strings.HasPrefix(lines[i], "# ") {
			end = i
			break
		}
	}

	fields, kept := parseSection(lines[start+1 : end])

	root, err := frontmatterMapping(fmBlock)
	if err != nil {
		return "", res, err
	}
	history := apply(root, fields, &res)
	if _, ok := lookup(root, "title"); !ok {
		if t := firstHeading(lines); t != "" {
			set(root, "title", strNode(t))
		}
	}

	newBody := rebuildBody(lines, start, end, kept)
	if len(history) > 0 {
		newBody = addReviewHistory(newBody, history)
	}

	out, err := encode(root)
	if err != nil {
		return "", res, err
	}
	res.Changed = true
	return restore("---\n"+out+"---\n\n"+newBody, crlf), res, nil
}

func restore(s string, crlf bool) string {
	if crlf {
		return strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

// parseSection splits the lines of a Meta section into field bullets and the
// lines to keep in the body. A continuation line joins its bullet's value; a
// shipped template comment is dropped.
func parseSection(sec []string) (fields []field, kept []string) {
	for i := 0; i < len(sec); i++ {
		l := sec[i]
		if m := bulletRe.FindStringSubmatch(l); m != nil {
			f := field{label: strings.TrimSpace(m[1]), value: m[2]}
			for i+1 < len(sec) && isContinuation(sec[i+1]) {
				i++
				f.value += " " + strings.TrimSpace(sec[i])
			}
			if cm := inlineCommRe.FindAllStringSubmatch(f.value, -1); cm != nil {
				var parts []string
				for _, c := range cm {
					parts = append(parts, strings.TrimSpace(c[1]))
				}
				f.comment = strings.Join(parts, " ")
				f.value = inlineCommRe.ReplaceAllString(f.value, " ")
			}
			f.value = strings.TrimSpace(f.value)
			fields = append(fields, f)
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(l), "<!--") {
			block := []string{l}
			for !strings.Contains(sec[i], "-->") && i+1 < len(sec) {
				i++
				block = append(block, sec[i])
			}
			if shippedComments[strings.Join(strings.Fields(strings.Join(block, " ")), " ")] {
				continue
			}
			kept = append(kept, block...)
			continue
		}
		kept = append(kept, l)
	}
	return fields, kept
}

// isContinuation reports whether a line continues the bullet above it: it is
// indented and is not itself a new bullet.
func isContinuation(l string) bool {
	if strings.TrimSpace(l) == "" {
		return false
	}
	return (strings.HasPrefix(l, "  ") || strings.HasPrefix(l, "\t")) &&
		!strings.HasPrefix(strings.TrimSpace(l), "- **")
}

// apply merges the parsed fields into the frontmatter mapping. It returns the
// review narrative, if any, that belongs in ## Review History.
func apply(root *yaml.Node, fields []field, res *Result) []string {
	var created, createdNotes []string
	var createdComment string
	var latest string
	var earlier []string

	// A label repeated in one block is a list in all but name. Typed keys
	// (vocabularies, dates, slugs) cannot hold one, so a repeat is kept
	// verbatim under <key>_extra rather than dropped.
	counts := map[string]int{}
	for _, f := range fields {
		counts[keyFor(f.label)]++
	}
	repeated := map[string][]string{}
	wroteFirst := map[string]bool{}

	for _, f := range fields {
		key := keyFor(f.label)
		v := f.value
		if isEmptyValue(v) {
			continue
		}
		if key == historyKey {
			earlier = append(earlier, v)
			continue
		}
		if counts[key] > 1 && key != keyCreated {
			// An untyped key collects every value into one list. A typed key
			// keeps its first value as the field and collects the rest.
			if !typedKey(key) || wroteFirst[key] {
				repeated[key] = append(repeated[key], v)
				continue
			}
			wroteFirst[key] = true
		}
		switch {
		case key == keyCreated:
			if d := dateRe.FindString(v); d != "" {
				created = append(created, d)
				if createdComment == "" {
					createdComment = f.comment
				}
				// "2026-04-14 (first session)" or a range: the date is the
				// field, the rest is kept beside it.
				if strings.TrimSpace(v) != d {
					createdNotes = append(createdNotes, v)
				}
				continue
			}
			setNew(root, key, strNode(v), f.comment, res)
		case dateKeys[key]:
			d := dateRe.FindString(v)
			if d == "" {
				setNew(root, key, strNode(v), f.comment, res)
				continue
			}
			setNew(root, key, dateNode(d), f.comment, res)
			if key == keyLastReviewed && strings.TrimSpace(v) != d {
				latest = v
			} else if rest := strings.TrimSpace(strings.Replace(v, d, "", 1)); rest != "" && key != keyLastReviewed {
				setNew(root, key+"_note", strNode(trimProse(rest)), "", res)
			}
		case vocabKeys[key]:
			m := leadTokenRe.FindStringSubmatch(unbold(v))
			if m == nil {
				setNew(root, key, strNode(strings.ToLower(v)), f.comment, res)
				continue
			}
			setNew(root, key, strNode(strings.ToLower(m[1])), f.comment, res)
			if rest := trimProse(m[2]); rest != "" {
				setNew(root, key+"_note", strNode(rest), "", res)
			}
		case listKeys[key]:
			setNew(root, key, listNode(splitList(v)), f.comment, res)
			// "`vips-core` (primary), `devops-release`": the annotations are
			// not slugs, so the whole value is kept beside the list.
			if hasProse(v) {
				setNew(root, key+"_note", strNode(v), "", res)
			}
		case key == keySlug:
			setNew(root, key, strNode(strings.ToLower(strings.Trim(v, "` "))), f.comment, res)
		default:
			setNew(root, key, strNode(v), f.comment, res)
		}
	}

	for _, key := range sortedKeys(repeated) {
		name := key
		if typedKey(key) {
			name += "_extra"
		}
		setNew(root, name, listNode(repeated[key]), "", res)
	}

	if len(created) > 0 {
		sort.Strings(created) // ISO dates sort chronologically.
		if created[0] != created[len(created)-1] {
			res.Notes = append(res.Notes, fmt.Sprintf("created: kept the earliest of %s (DEC-014)", strings.Join(created, ", ")))
		}
		setNew(root, keyCreated, dateNode(created[0]), createdComment, res)
		if len(createdNotes) > 0 {
			setNew(root, "created_note", strNode(strings.Join(createdNotes, "; ")), "", res)
		}
	}
	if latest == "" && len(earlier) == 0 {
		return nil
	}
	if latest != "" {
		return append([]string{latest}, earlier...)
	}
	return earlier
}

// hasProse reports whether a list value says more than its items: text left
// once links, backticked items, separators and the items themselves are gone.
func hasProse(v string) bool {
	rest := linkTextRe.ReplaceAllString(v, "")
	rest = backtickedRe.ReplaceAllString(rest, "")
	if !backtickedRe.MatchString(v) && !linkTextRe.MatchString(v) {
		return false // a plain comma list is all items
	}
	return strings.Trim(rest, " ,;") != ""
}

// typedKey reports a key whose value has a type a list would break.
func typedKey(key string) bool {
	return vocabKeys[key] || listKeys[key] || dateKeys[key] || key == keySlug
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// unbold moves a bold lead-in out of the way of the token match:
// "**done (2026-06-13)** — shipped" reads as "done (2026-06-13) — shipped".
func unbold(v string) string {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "**") {
		return v
	}
	end := strings.Index(v[2:], "**")
	if end < 0 {
		return strings.TrimPrefix(v, "**")
	}
	return strings.TrimSpace(strings.TrimSpace(v[2:2+end]) + " " + strings.TrimSpace(v[2+end+2:]))
}

// keyFor maps a Meta label to its frontmatter key.
func keyFor(label string) string {
	l := strings.ToLower(strings.TrimSpace(label))
	if k, ok := labelKeys[l]; ok {
		return k
	}
	return strings.Trim(nonKeyRe.ReplaceAllString(l, "_"), "_")
}

// isEmptyValue reports a value that states nothing: blank, an unfilled
// {placeholder}, or a bare N/A. "N/A (spun off from X)" says something and is
// kept.
func isEmptyValue(v string) bool {
	t := strings.TrimSpace(strings.Trim(strings.TrimSpace(v), "`"))
	switch strings.ToLower(t) {
	case "", "n/a", "none", "-", "—":
		return true
	}
	return placeholderRe.MatchString(t)
}

// trimProse tidies the text after a leading token: separators and one level
// of wrapping parentheses go.
func trimProse(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSpace(strings.TrimLeft(s, "—–-:;, "))
	if wrapped(s) {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

// wrapped reports whether s is one parenthesised group from first byte to
// last: "(a (b) c)" is, "(a) and (b)" is not.
func wrapped(s string) bool {
	if !strings.HasPrefix(s, "(") || !strings.HasSuffix(s, ")") {
		return false
	}
	depth := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i != len(s)-1 {
				return false
			}
		}
	}
	return depth == 0
}

// splitList reads the list shapes in use: backticked values (also inside link
// text), link text, or a plain comma-separated list. Values are lower-cased:
// slugs and tags are controlled vocabularies (DEC-010).
func splitList(v string) []string {
	var raw []string
	if m := backtickedRe.FindAllStringSubmatch(v, -1); m != nil {
		for _, x := range m {
			raw = append(raw, x[1])
		}
	} else if m := linkTextRe.FindAllStringSubmatch(v, -1); m != nil {
		for _, x := range m {
			raw = append(raw, x[1])
		}
	} else {
		raw = strings.Split(v, ",")
	}
	out := []string{}
	seen := map[string]bool{}
	for _, r := range raw {
		r = strings.ToLower(strings.TrimSpace(r))
		if r == "" || placeholderRe.MatchString(r) || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

func firstHeading(lines []string) string {
	for _, l := range lines {
		if strings.HasPrefix(l, "# ") {
			return strings.TrimSpace(l[2:])
		}
	}
	return ""
}

// rebuildBody replaces the Meta section with the lines it kept, leaving one
// blank line on either side of what remains.
func rebuildBody(lines []string, start, end int, kept []string) string {
	kept = trimBlank(kept)
	before := trimTrailingBlank(lines[:start])
	after := lines[end:]
	out := append([]string{}, before...)
	if len(kept) > 0 {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, kept...)
	}
	if len(after) > 0 {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, after...)
	}
	return strings.TrimLeft(strings.Join(out, "\n"), "\n")
}

func trimTrailingBlank(l []string) []string {
	for len(l) > 0 && strings.TrimSpace(l[len(l)-1]) == "" {
		l = l[:len(l)-1]
	}
	return l
}

func trimBlank(l []string) []string {
	for len(l) > 0 && strings.TrimSpace(l[0]) == "" {
		l = l[1:]
	}
	return trimTrailingBlank(l)
}

// addReviewHistory puts the review narrative from `Last reviewed:` under a
// ## Review History section: into an existing one, else before
// ## Related Areas, else at the end.
func addReviewHistory(body string, entries []string) string {
	var entry []string
	for _, e := range entries {
		entry = append(entry, "- "+e)
	}
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "## Review History" {
			at := i + 1
			for at < len(lines) && strings.TrimSpace(lines[at]) == "" {
				at++
			}
			return strings.Join(append(lines[:at:at], append(entry, lines[at:]...)...), "\n")
		}
	}
	section := append(append([]string{"## Review History", ""}, entry...), "")
	for i, l := range lines {
		if strings.TrimSpace(l) == "## Related Areas" {
			return strings.Join(append(lines[:i:i], append(section, lines[i:]...)...), "\n")
		}
	}
	return strings.TrimRight(body, "\n") + "\n\n" + strings.Join(section[:len(section)-1], "\n") + "\n"
}
