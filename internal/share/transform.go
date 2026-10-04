package share

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// frontmatterDelim opens and closes a YAML frontmatter block.
const frontmatterDelim = "---"

// splitFrontmatter separates a leading YAML frontmatter block from the rest of
// body. ok is false when body has no block, including an unterminated one,
// which is not frontmatter and is left in place.
func splitFrontmatter(body string) (fm, rest string, ok bool) {
	lines := strings.Split(body, "\n")
	if strings.TrimRight(lines[0], "\r") != frontmatterDelim {
		return "", body, false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == frontmatterDelim {
			return strings.Join(lines[1:i], "\n"), strings.Join(lines[i+1:], "\n"), true
		}
	}
	return "", body, false
}

// stripFrontmatter drops a leading YAML frontmatter block. Workspace
// frontmatter carries fields that must not leave (priority); the published
// Meta block is rebuilt instead, taking the shareable fields from
// frontmatterMeta.
func stripFrontmatter(body string) string {
	_, rest, ok := splitFrontmatter(body)
	if !ok {
		return body
	}
	lines := strings.Split(rest, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

// keptFrontmatter is the frontmatter counterpart of keptMetaFields. The keys
// are pointers so an absent key is told apart from an empty one.
type keptFrontmatter struct {
	Created    *string `yaml:"created"`
	OutputType *string `yaml:"output_type"`
	Origin     *string `yaml:"origin"`
}

// frontmatterMeta reads the shareable fields from a leading YAML frontmatter
// block, keyed by the Meta label they are published under. A README migrated
// to frontmatter has no ## Meta block left to read them from. A missing,
// unterminated or malformed block yields nothing, so the ## Meta fallback
// still applies.
func frontmatterMeta(body string) map[string]string {
	raw, _, ok := splitFrontmatter(body)
	if !ok {
		return nil
	}
	var fm keptFrontmatter
	if yaml.Unmarshal([]byte(raw), &fm) != nil {
		return nil
	}
	out := map[string]string{}
	for label, v := range map[string]*string{
		"Started":     fm.Created,
		"Output type": fm.OutputType,
		originLabel:   fm.Origin,
	} {
		if v != nil {
			out[label] = strings.TrimSpace(*v)
		}
	}
	return out
}

// applyExclusions removes every line between a share:exclude:start marker and
// its matching end marker, markers included. Markers must sit on their own
// line; nesting, a stray end or an unclosed start is an error, because
// guessing the intended boundary could publish what the owner meant to keep.
func applyExclusions(body string) (string, error) {
	lines := strings.Split(body, "\n")
	kept := make([]string, 0, len(lines))
	openedAt := 0
	for i, line := range lines {
		switch strings.TrimSpace(line) {
		case ExcludeStart:
			if openedAt != 0 {
				return "", fmt.Errorf("line %d: nested %s (previous opened at line %d)", i+1, ExcludeStart, openedAt)
			}
			openedAt = i + 1
			continue
		case ExcludeEnd:
			if openedAt == 0 {
				return "", fmt.Errorf("line %d: %s without a matching start", i+1, ExcludeEnd)
			}
			openedAt = 0
			continue
		}
		if openedAt == 0 {
			kept = append(kept, line)
		}
	}
	if openedAt != 0 {
		return "", fmt.Errorf("line %d: %s is never closed", openedAt, ExcludeStart)
	}
	return strings.Join(kept, "\n"), nil
}

var metaFieldRe = regexp.MustCompile(`^\s*-\s+\*\*([^*]+):\*\*\s*(.*)$`)

// Meta fields copied from the source README when present. Status and Areas
// come from the index; everything else is dropped.
var keptMetaFields = []string{"Started", "Output type", originLabel}

// originLabel is kept only when it points outside the workspace.
const originLabel = "Origin"

// rewriteMeta replaces README's ## Meta section with the shareable subset:
// Status and Areas from the project index, then Started, Output type and
// Origin from the source. fm holds those fields as read from frontmatter; a
// key present there wins over the same field in ## Meta. Origin survives only
// when it is an external reference (a ticket key or URL), not a path into the
// workspace.
func rewriteMeta(body string, p Project, fm map[string]string) string {
	lines := strings.Split(body, "\n")
	start, end := -1, len(lines)
	for i, line := range lines {
		if start < 0 {
			if strings.TrimSpace(line) == "## Meta" {
				start = i
			}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			end = i
			break
		}
	}

	source := map[string]string{}
	if start >= 0 {
		for _, line := range lines[start+1 : end] {
			if m := metaFieldRe.FindStringSubmatch(line); m != nil {
				source[strings.TrimSpace(m[1])] = strings.TrimSpace(m[2])
			}
		}
	}
	for k, v := range fm {
		source[k] = v
	}

	block := []string{"## Meta", ""}
	if p.Status != "" {
		block = append(block, "- **Status:** "+p.Status)
	}
	if len(p.Areas) > 0 {
		quoted := make([]string, len(p.Areas))
		for i, a := range p.Areas {
			quoted[i] = "`" + a + "`"
		}
		block = append(block, "- **Areas:** "+strings.Join(quoted, ", "))
	}
	for _, key := range keptMetaFields {
		v, ok := source[key]
		if !ok || v == "" || (key == originLabel && !isExternalReference(v)) {
			continue
		}
		block = append(block, "- **"+key+":** "+v)
	}
	block = append(block, "")

	if start < 0 {
		return insertAfterTitle(lines, block)
	}
	out := make([]string, 0, len(lines)-(end-start)+len(block))
	out = append(out, lines[:start]...)
	out = append(out, block...)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

// insertAfterTitle places the Meta block after the first H1 (or at the top).
func insertAfterTitle(lines, block []string) string {
	at := 0
	for i, line := range lines {
		if strings.HasPrefix(line, "# ") {
			at = i + 1
			break
		}
	}
	out := make([]string, 0, len(lines)+len(block)+1)
	out = append(out, lines[:at]...)
	if at > 0 {
		out = append(out, "")
	}
	out = append(out, block...)
	out = append(out, lines[at:]...)
	return strings.Join(out, "\n")
}

// isExternalReference reports whether a Meta value points outside the
// workspace: no relative link and no bare markdown path.
func isExternalReference(v string) bool {
	links := linkRe.FindAllStringSubmatch(v, -1)
	for _, m := range links {
		if !isExternal(m[3]) {
			return false
		}
	}
	return len(links) > 0 || !strings.Contains(strings.ToLower(v), ".md")
}
