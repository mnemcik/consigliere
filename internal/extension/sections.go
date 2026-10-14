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
	return upsertBlock(content, start, end, body, strings.Index)
}

// RemoveSection deletes the ext:<name>:section=<id> block (markers included),
// returning the new content and whether the block was found. It also trims the
// blank line that UpsertSection inserted before the block, so repeated
// add/remove cycles don't accumulate blank lines.
func RemoveSection(content, name, id string) (string, bool) {
	start, end := sectionMarkers(name, id)
	return removeBlock(content, start, end, strings.Index)
}

// Extension .gitignore blocks use the same scheme as CLAUDE.md sections, with
// #-comment markers so git reads them as comments. The markers have no closing
// delimiter, so they are matched as whole lines (indexLine): otherwise the id
// "cache" would also match "start=cache-v2".
func gitignoreMarkers(name, id string) (start, end string) {
	return fmt.Sprintf("# ext:%s:gitignore:start=%s", name, id),
		fmt.Sprintf("# ext:%s:gitignore:end=%s", name, id)
}

// UpsertGitignore inserts or replaces the ext:<name>:gitignore=<id> block, as
// UpsertSection does for CLAUDE.md.
func UpsertGitignore(content, name, id, body string) string {
	start, end := gitignoreMarkers(name, id)
	return upsertBlock(content, start, end, body, indexLine)
}

// RemoveGitignore deletes the ext:<name>:gitignore=<id> block, as RemoveSection
// does for CLAUDE.md.
func RemoveGitignore(content, name, id string) (string, bool) {
	start, end := gitignoreMarkers(name, id)
	return removeBlock(content, start, end, indexLine)
}

// indexLine returns the index of the first occurrence of marker that makes up a
// whole line (an optional \r before the newline is allowed), or -1.
func indexLine(content, marker string) int {
	for off := 0; ; {
		i := strings.Index(content[off:], marker)
		if i < 0 {
			return -1
		}
		i += off
		after := content[i+len(marker):]
		if (i == 0 || content[i-1] == '\n') &&
			(after == "" || after[0] == '\n' || strings.HasPrefix(after, "\r\n")) {
			return i
		}
		off = i + 1
	}
}

func upsertBlock(content, start, end, body string, find func(string, string) int) string {
	si := find(content, start)
	if si >= 0 {
		afterStart := si + len(start)
		if rel := find(content[afterStart:], end); rel >= 0 {
			endIdx := afterStart + rel
			return content[:afterStart] + "\n" + body + "\n" + content[endIdx:]
		}
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + fmt.Sprintf("\n%s\n%s\n%s\n", start, body, end)
}

func removeBlock(content, start, end string, find func(string, string) int) (string, bool) {
	si := find(content, start)
	if si < 0 {
		return content, false
	}
	ei := find(content[si:], end)
	if ei < 0 {
		return content, false
	}
	blockEnd := si + ei + len(end)
	// Absorb a single trailing newline after the end marker.
	if strings.HasPrefix(content[blockEnd:], "\r\n") {
		blockEnd += 2
	} else if blockEnd < len(content) && content[blockEnd] == '\n' {
		blockEnd++
	}
	// Absorb the one blank line upsertBlock put before the block, leaving any
	// blank lines of the user's own in place.
	prefix := content[:si]
	if prefix == "\n" {
		prefix = ""
	} else if strings.HasSuffix(prefix, "\n\n") {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix + content[blockEnd:], true
}
