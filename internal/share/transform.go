package share

import (
	"fmt"
	"regexp"
	"strings"
)

// stripFrontmatter drops a leading YAML frontmatter block. Workspace
// frontmatter is a derived copy of the index metadata and carries fields that
// must not leave (priority); the published Meta block is rebuilt instead.
func stripFrontmatter(body string) string {
	lines := strings.Split(body, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return body
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			rest := lines[i+1:]
			for len(rest) > 0 && strings.TrimSpace(rest[0]) == "" {
				rest = rest[1:]
			}
			return strings.Join(rest, "\n")
		}
	}
	return body // unterminated: not frontmatter, leave it
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
var keptMetaFields = []string{"Started", "Output type", "Origin"}

// rewriteMeta replaces README's ## Meta section with the shareable subset:
// Status and Areas from the project index, then Started, Output type and
// Origin from the source. Origin survives only when it is an external
// reference (a ticket key or URL), not a path into the workspace.
func rewriteMeta(body string, p Project) string {
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
		if !ok || v == "" || (key == "Origin" && !isExternalReference(v)) {
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
