package wizard

import (
	"fmt"
	"regexp"
	"strings"
)

// RenderProfile returns the contents of a PROFILE.md filled from the wizard
// answers. Callers write this to the workspace root, overwriting the default
// template that `cg init` copies.
func RenderProfile(a *Answers) string {
	if a == nil {
		a = &Answers{}
	}
	var b strings.Builder
	b.WriteString("# Profile\n\n")
	b.WriteString("## Role\n\n")
	if a.ProfileRole != "" {
		b.WriteString(a.ProfileRole)
	} else {
		b.WriteString("[Your role and organization]")
	}
	b.WriteString("\n\n")

	b.WriteString("## Responsibilities\n\n")
	if a.ProfileFocus != "" {
		for _, line := range splitLines(a.ProfileFocus) {
			fmt.Fprintf(&b, "- %s\n", line)
		}
	} else {
		b.WriteString("- [Primary responsibility]\n- [Secondary responsibility]\n")
	}
	b.WriteString("\n")

	b.WriteString("## Context for AI Assistants\n\n")
	if a.ProfileName != "" {
		fmt.Fprintf(&b, "- Owner: %s\n", a.ProfileName)
	}
	b.WriteString("- [What kinds of ideas are typically submitted to this workspace]\n")
	b.WriteString("- [Primary domains or lenses for interpreting work]\n")
	b.WriteString("- [Guidance on how to handle ambiguous inputs]\n")
	return b.String()
}

// RenderArea returns the contents of `areas/<slug>.md` for the first area the
// wizard collected. `today` should be "YYYY-MM-DD" — injected by the caller so
// tests are deterministic.
func RenderArea(a *Answers, today string) string {
	if a == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", a.AreaName)
	b.WriteString("## Meta\n\n")
	fmt.Fprintf(&b, "- **Slug:** `%s`\n", a.AreaSlug)
	fmt.Fprintf(&b, "- **Tags:** %s\n", normalizeTags(a.AreaTags))
	fmt.Fprintf(&b, "- **Created:** %s\n", today)
	fmt.Fprintf(&b, "- **Last reviewed:** %s\n\n", today)

	b.WriteString("## Overview\n\n")
	if a.AreaOverview != "" {
		b.WriteString(a.AreaOverview)
	} else {
		b.WriteString("One or two sentences describing what this area covers and why it matters.")
	}
	b.WriteString("\n\n")

	b.WriteString("## Key Systems & Components\n\nWhat systems, services, APIs, or tools belong to this area?\n\n")
	b.WriteString("## Key Contacts\n\n| Role | Who | Notes |\n|------|-----|-------|\n| — | — | — |\n\n")
	b.WriteString("## Architecture & Constraints\n\nKnown architectural decisions, constraints, compliance requirements, or technical debt.\n\n")
	b.WriteString("## Current State\n\nWhat is the current state of this area? What is working, what is not?\n\n")
	b.WriteString("## Related Areas\n\nLinks to other areas that interact with or depend on this one.\n\n")
	b.WriteString("## Associated Items\n\n<!-- Updated automatically when projects/ideas/notes reference this area -->\n\n")
	b.WriteString("### Projects\n\n### Ideas\n\n### Notes\n")
	return b.String()
}

// InsertAreaIndexRow appends a table row for the new area to the flat
// `## Areas` section of an existing `areas/INDEX.md`. If the table has no
// rows yet, the new row is added directly under the header separator;
// otherwise it is appended after the last existing row.
//
// Returns the updated index content. If the expected section header cannot be
// found, the input is returned unchanged — callers should fall back to
// appending manually or surfacing a warning.
func InsertAreaIndexRow(index string, a *Answers) string {
	if a == nil {
		return index
	}
	section := "## Areas"
	// Idempotency: if any row already links to `<slug>.md`, leave the index
	// untouched. Re-running `cg init --wizard --force` must not duplicate rows.
	if strings.Contains(index, fmt.Sprintf("](%s.md)", a.AreaSlug)) {
		return index
	}
	row := fmt.Sprintf("| [%s](%s.md) | `%s` | %s | %s |",
		escapeTableCell(a.AreaName), a.AreaSlug, a.AreaSlug,
		escapeTableCell(normalizeTags(a.AreaTags)),
		escapeTableCell(firstSentence(a.AreaOverview)))

	lines := strings.Split(index, "\n")
	sectionIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == section {
			sectionIdx = i
			break
		}
	}
	if sectionIdx == -1 {
		return index
	}

	// Walk forward from the section header to find the table separator
	// (the `|------|` line). Insert either right after it (empty table) or
	// after the last contiguous table row.
	sepIdx := -1
	for i := sectionIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "|---") {
			sepIdx = i
			break
		}
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			break // next section; malformed
		}
	}
	if sepIdx == -1 {
		return index
	}

	insertAt := sepIdx + 1
	for i := sepIdx + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "|") && !strings.HasPrefix(trimmed, "|---") {
			insertAt = i + 1
			continue
		}
		break
	}

	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:insertAt]...)
	out = append(out, row)
	out = append(out, lines[insertAt:]...)
	return strings.Join(out, "\n")
}

// slugSanitizer matches any run of characters that aren't lowercase
// alphanumerics. Runs of existing dashes collapse with other non-alphanum
// into a single `-`, so `foo--bar` and `foo - bar` both canonicalize to
// `foo-bar` — guaranteeing SanitizeSlug output always passes the
// downstream `validSlug` regex in cmd/init.go.
var slugSanitizer = regexp.MustCompile(`[^a-z0-9]+`)

// SanitizeSlug lowercases, replaces runs of non-alphanumeric characters with
// a single dash, and trims leading/trailing dashes. Used to nudge free-form
// wizard input toward a valid area slug before writing the area file.
func SanitizeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugSanitizer.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, ".\n"); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// escapeTableCell sanitises free-form user input for safe use inside a
// Markdown table cell: replaces newlines with spaces and pipes with escaped
// pipes. Keeps the INDEX.md table well-formed even if the user types "foo |
// bar" or pastes multi-line text as an area overview.
func escapeTableCell(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.ReplaceAll(s, "|", `\|`)
}

// normalizeTags canonicalizes a comma-separated tag string: trims whitespace
// around each entry, lowercases, drops empties and duplicates while preserving
// first-seen order, and rejoins as "tag1, tag2". Empty input yields "".
func normalizeTags(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	seen := make(map[string]struct{})
	var out []string
	for _, part := range strings.Split(s, ",") {
		t := strings.ToLower(strings.TrimSpace(part))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return strings.Join(out, ", ")
}
