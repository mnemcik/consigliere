package extension

import (
	"fmt"
	"strings"
)

// Extension CLAUDE.md fragments live in their own marker namespace,
// ext:<name>:section, distinct from the framework's cg:section and the user's
// user:section (DEC-003). cg sync parses only cg:section, so these blocks are
// invisible to it and an extension can never clobber a framework rule.

func sectionMarkers(name, id string) (start, end string) {
	return fmt.Sprintf("<!-- ext:%s:section:start=%s -->", name, id),
		fmt.Sprintf("<!-- ext:%s:section:end=%s -->", name, id)
}

// UpsertSection inserts the ext:<name>:section=<id> block with the given body,
// or replaces its inner body if the block already exists. A replaced block keeps
// its markers; a new block is appended after a blank line with a trailing
// newline (matching the framework's section padding).
func UpsertSection(content, name, id, body string) string {
	start, end := sectionMarkers(name, id)
	si := strings.Index(content, start)
	if si >= 0 {
		afterStart := si + len(start)
		if rel := strings.Index(content[afterStart:], end); rel >= 0 {
			endIdx := afterStart + rel
			return content[:afterStart] + "\n" + body + "\n" + content[endIdx:]
		}
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + fmt.Sprintf("\n%s\n%s\n%s\n", start, body, end)
}

// RemoveSection deletes the ext:<name>:section=<id> block (markers included),
// returning the new content and whether the block was found. It also trims the
// blank line that UpsertSection inserted before the block, so repeated
// add/remove cycles don't accumulate blank lines.
func RemoveSection(content, name, id string) (string, bool) {
	start, end := sectionMarkers(name, id)
	si := strings.Index(content, start)
	if si < 0 {
		return content, false
	}
	ei := strings.Index(content[si:], end)
	if ei < 0 {
		return content, false
	}
	blockEnd := si + ei + len(end)
	// Absorb a single trailing newline after the end marker.
	if blockEnd < len(content) && content[blockEnd] == '\n' {
		blockEnd++
	}
	// Absorb the blank line UpsertSection put before the block.
	prefix := content[:si]
	prefix = strings.TrimRight(prefix, "\n")
	if prefix != "" {
		prefix += "\n"
	}
	return prefix + content[blockEnd:], true
}
