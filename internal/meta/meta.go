// Package meta reads the structured metadata attached to a workspace item.
//
// Two on-disk forms are supported. The original form is a `## Meta` block of
// markdown bullets:
//
//	## Meta
//
//	- **Tags:** tool, framework
//	- **Color:** cyan
//
// The preferred form is a YAML frontmatter block:
//
//	---
//	tags: [tool, framework]
//	color: cyan
//	---
//
// Frontmatter is the direction of travel: it is the de facto standard for
// markdown metadata, it is typed, and it is read with a real parser rather
// than regexes over prose. The `## Meta` form stays supported indefinitely so
// no workspace is forced to migrate.
//
// Resolution is **per field**, not per file. A file may carry frontmatter
// covering only some fields -- a workspace part-way through migration, or one
// whose frontmatter was generated from a subset of the Meta block -- and every
// field the frontmatter omits still resolves from `## Meta`.
//
// Omission is judged by key *presence*, not by emptiness. `tags: []` and
// `color: ""` are declarations that the item has none, and are honoured over
// any `## Meta` block below. The one exception is an unfilled `{placeholder}`,
// which is a template artefact rather than a decision and still falls back.
//
// Reads are bounded. resolveColor runs on every statusline render, and a real
// workspace holds 60 KB area files whose Meta block carries 10 KB of review
// prose on a single line, so Read streams a capped number of lines rather than
// loading the whole file.
package meta

import (
	"bufio"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Fields is the structured metadata cg reads from a workspace item. It covers
// what cg consumes today; status, priority and areas come from the index
// tables rather than from item files, so they are deliberately absent until
// that changes.
type Fields struct {
	Tags  []string
	Color string
}

// frontmatter mirrors Fields, for YAML decoding. The fields are pointers so an
// omitted key is distinguishable from one explicitly set to an empty value:
// `tags: []` means "this item has no tags" and must not be overridden by a
// stale `## Meta` block, whereas an absent `tags:` key means "not stated here,
// look further down".
type frontmatter struct {
	Tags  *flexList `yaml:"tags"`
	Color *string   `yaml:"color"`
}

// flexList accepts either a YAML sequence (`tags: [a, b]`) or a single scalar
// (`tags: a, b`), since hand-written frontmatter uses both.
type flexList []string

func (f *flexList) UnmarshalYAML(n *yaml.Node) error {
	var seq []string
	if err := n.Decode(&seq); err == nil {
		*f = seq
		return nil
	}
	var one string
	if err := n.Decode(&one); err != nil {
		return err
	}
	// A bare `tags: a, b, c` is one string to YAML, but the author plainly
	// meant a list, so split it rather than yielding a single nonsense value.
	for _, p := range strings.Split(one, ",") {
		if p = strings.TrimSpace(strings.Trim(p, "`")); p != "" {
			*f = append(*f, p)
		}
	}
	return nil
}

var (
	tagsFieldRe   = regexp.MustCompile(`^- \*\*Tags:\*\*\s*(.+?)\s*$`)
	colorFieldRe  = regexp.MustCompile(`^(?:- )?\*\*Color:\*\*\s*(.+?)\s*$`)
	backtickedRe  = regexp.MustCompile("`([^`]+)`")
	placeholderRe = regexp.MustCompile(`^\{.*\}$`)
)

const (
	// scanLineLimit bounds how far into a document the `## Meta` fallback
	// looks. The block always sits near the top, and the same `- **Label:**`
	// bullet shape recurs deep in document bodies (`**Fix:**`,
	// `**Bitemporal model:**`), so an unbounded scan would read prose as
	// metadata.
	scanLineLimit = 60

	// maxLineBytes raises bufio's per-line ceiling above its 64 KB default.
	// A single real Meta line runs past 10 KB where `**Last reviewed:**`
	// carries accumulated review history.
	maxLineBytes = 1 << 20
)

// Read returns the structured metadata for the markdown file at path.
// Frontmatter wins field by field; anything it omits falls back to `## Meta`.
// A missing or unreadable file is an error; a file with neither form yields a
// zero Fields.
func Read(path string) (Fields, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from the workspace layout
	if err != nil {
		return Fields{}, err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	var out Fields
	var fmLines []string
	inFrontmatter := false
	// Whether frontmatter settled each field. Emptiness cannot stand in for
	// this: an explicit `tags: []` resolves the field to "none".
	var tagsSet, colorSet bool

	for line := 0; line < scanLineLimit && sc.Scan(); line++ {
		text := strings.TrimRight(sc.Text(), "\r")

		// Only a `---` on the very first line opens frontmatter, so a
		// horizontal rule mid-document is never mistaken for one.
		if line == 0 && text == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if text == "---" {
				inFrontmatter = false
				var parsed frontmatter
				// A malformed block is not fatal: fall through to `## Meta`
				// rather than failing the caller over one bad file.
				if yaml.Unmarshal([]byte(strings.Join(fmLines, "\n")), &parsed) == nil {
					if parsed.Tags != nil {
						out.Tags, tagsSet = clean(*parsed.Tags), true
					}
					// A `{placeholder}` is an unfilled template value, not a
					// declaration, so it stays unresolved and falls back --
					// unlike an explicit empty string, which is a decision.
					if parsed.Color != nil && !placeholderRe.MatchString(strings.TrimSpace(*parsed.Color)) {
						out.Color, colorSet = cleanScalar(*parsed.Color), true
					}
				}
				continue
			}
			fmLines = append(fmLines, text)
			continue
		}

		if m := tagsFieldRe.FindStringSubmatch(text); m != nil && !tagsSet {
			out.Tags, tagsSet = clean(splitScalars(m[1])), true
		}
		if m := colorFieldRe.FindStringSubmatch(text); m != nil && !colorSet {
			out.Color, colorSet = cleanScalar(m[1]), true
		}
		if tagsSet && colorSet {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}

// splitScalars reads the two value shapes in use: backtick-quoted values, and
// a plain comma-separated list.
func splitScalars(raw string) []string {
	if ticked := backtickedRe.FindAllStringSubmatch(raw, -1); len(ticked) > 0 {
		vals := make([]string, 0, len(ticked))
		for _, t := range ticked {
			vals = append(vals, t[1])
		}
		return vals
	}
	return strings.Split(raw, ",")
}

func clean(in []string) []string {
	var out []string
	for _, v := range in {
		if v = cleanScalar(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// cleanScalar trims whitespace and backticks, discards unfilled template
// placeholders like `{color}` so a freshly-copied template reads as unset, and
// lower-cases the result.
//
// Case is normalised because every field this package reads is a controlled
// vocabulary, not prose: colours key into a fixed map, and tags are counted by
// `cg tags`. Authors write them inconsistently -- a single `Defining` among
// ninety-nine `defining` values silently splits any grouping, and `Bright-Cyan`
// simply fails to resolve. Normalising on read makes the vocabulary
// case-insensitive at the one point every consumer shares.
func cleanScalar(v string) string {
	v = strings.TrimSpace(strings.Trim(strings.TrimSpace(v), "`"))
	if v == "" || placeholderRe.MatchString(v) {
		return ""
	}
	return strings.ToLower(v)
}
