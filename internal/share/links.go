package share

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Pieces of an inline link, shared by the matcher and the safety-net guard.
const (
	// linkText allows one level of nested brackets, so a linked image
	// ([![alt](img)](target)) is matched as the outer link.
	linkText = `((?:[^\[\]]|\[[^\[\]]*\])*)`
	// linkTarget is an <angle-bracket> target or a bare one that may contain
	// one level of balanced parentheses (a(b).md).
	linkTarget = `(<[^<>\n]*>|(?:[^()\s]|\([^()\s]*\))+)`
	linkTitle  = `(\s+(?:"[^"]*"|'[^']*'|\([^)]*\)))?`
)

// linkRe matches an inline markdown link or image: [text](target "title").
// Groups: 1 "!" for images, 2 text, 3 target, 4 optional title.
var linkRe = regexp.MustCompile(`(!?)\[` + linkText + `\]\(\s*` + linkTarget + linkTitle + `\s*\)`)

// refDefRe matches a reference definition: [label]: target "title".
// Groups: 1 prefix up to the target, 2 target, 3 rest of the line.
var refDefRe = regexp.MustCompile(`^( {0,3}\[[^\]]+\]:\s*)(<[^<>\n]*>|\S+)(.*)$`)

// guardRe finds the target after any remaining "](" for the safety net.
var guardRe = regexp.MustCompile(`\]\(\s*` + linkTarget)

// schemeRe requires at least two characters so a Windows drive path (C:\...)
// is not mistaken for a URL scheme.
var schemeRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]+:`)

// PrivateMarker is appended to the text of a de-linked private link.
const PrivateMarker = "*(private)*"

// isExternal reports whether a link target leaves the workspace (a URL or
// other scheme) or stays within the current page (a bare fragment). A file:
// URL is a local path, so it is never external.
func isExternal(target string) bool {
	target = unwrapTarget(target)
	if strings.HasPrefix(target, "#") {
		return true
	}
	scheme := schemeRe.FindString(target)
	return scheme != "" && !strings.EqualFold(scheme, "file:")
}

// unwrapTarget strips the angle brackets of an <angle-bracket> target.
func unwrapTarget(target string) string {
	if strings.HasPrefix(target, "<") && strings.HasSuffix(target, ">") {
		return target[1 : len(target)-1]
	}
	return target
}

// resolveLinks rewrites the relative links in body. src is the file's
// workspace-relative path and pub its published path; targets maps
// workspace-relative paths to published ones. Fenced code blocks and inline
// code spans are left untouched. A private reference definition is dropped,
// leaving its uses as plain text. Any link form the rewriter does not
// understand is an error rather than a pass-through, so an unrecognised shape
// can never carry a private path out.
func resolveLinks(body, src, pub string, targets map[string]string) (string, error) {
	lines := strings.Split(body, "\n")
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		out, err := resolveRefDef(line, src, pub, targets)
		if err == nil && out == line {
			out, err = resolveLine(line, src, pub, targets)
		}
		if err == nil {
			err = guard(out, src, pub, targets)
		}
		if err != nil {
			return "", fmt.Errorf("line %d: %w", i+1, err)
		}
		lines[i] = out
	}
	return strings.Join(lines, "\n"), nil
}

// resolveRefDef rewrites a reference-definition line, drops it (returns "")
// when its target is private, and returns any other line unchanged.
func resolveRefDef(line, src, pub string, targets map[string]string) (string, error) {
	m := refDefRe.FindStringSubmatch(line)
	if len(m) < 4 || isExternal(m[2]) {
		return line, nil
	}
	rewritten, ok, err := resolveTarget(unwrapTarget(m[2]), src, pub, targets)
	if err != nil || !ok {
		return "", err
	}
	return m[1] + rewrap(m[2], rewritten) + m[3], nil
}

// resolveLine rewrites the links on one line. A link that starts inside an
// inline code span is literal text and left alone; a link whose text merely
// contains a code span ([`name` DEC-5](...)) is still a link, and its text is
// resolved too, so a nested image is handled whatever the outer target is.
func resolveLine(line, src, pub string, targets map[string]string) (string, error) {
	spans := codeSpans(line)
	var b strings.Builder
	last := 0
	for _, loc := range linkRe.FindAllStringSubmatchIndex(line, -1) {
		if inSpans(loc[0], spans) {
			continue
		}
		image := line[loc[2]:loc[3]]
		target := line[loc[6]:loc[7]]
		title := ""
		if loc[8] >= 0 {
			title = line[loc[8]:loc[9]]
		}
		text, err := resolveLine(line[loc[4]:loc[5]], src, pub, targets)
		if err != nil {
			return "", err
		}

		b.WriteString(line[last:loc[0]])
		last = loc[1]
		if isExternal(target) {
			b.WriteString(image + "[" + text + "](" + target + title + ")")
			continue
		}
		rewritten, ok, err := resolveTarget(unwrapTarget(target), src, pub, targets)
		if err != nil {
			return "", err
		}
		switch {
		case ok:
			b.WriteString(image + "[" + text + "](" + rewrap(target, rewritten) + title + ")")
		case text == "":
			b.WriteString(PrivateMarker)
		default:
			b.WriteString(text + " " + PrivateMarker)
		}
	}
	b.WriteString(line[last:])
	return b.String(), nil
}

// rewrap keeps the angle brackets of an <angle-bracket> original.
func rewrap(original, rewritten string) string {
	if strings.HasPrefix(original, "<") {
		return "<" + rewritten + ">"
	}
	return rewritten
}

// guard fails when a "](" outside code still points at a workspace path that
// has no published counterpart: a link shape the rewriter did not match.
func guard(line, src, pub string, targets map[string]string) error {
	spans := codeSpans(line)
	for _, loc := range guardRe.FindAllStringSubmatchIndex(line, -1) {
		if inSpans(loc[0], spans) {
			continue
		}
		target := line[loc[2]:loc[3]]
		if isExternal(target) {
			continue
		}
		if _, ok, err := resolveTarget(unwrapTarget(target), src, pub, targets); err != nil || !ok {
			return fmt.Errorf("unsupported link form around %q; simplify the link or put it in a code span", line[loc[0]:loc[1]])
		}
	}
	return nil
}

// codeSpans returns the [start, end) byte ranges of the inline code spans on a
// line: a run of N backticks up to the next run of exactly N. An unmatched run
// is literal text.
func codeSpans(line string) [][2]int {
	var spans [][2]int
	for i := 0; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}
		n := runLen(line, i)
		closed := false
		for j := i + n; j < len(line); {
			if line[j] != '`' {
				j++
				continue
			}
			m := runLen(line, j)
			if m == n {
				spans = append(spans, [2]int{i, j + m})
				i, closed = j+m, true
				break
			}
			j += m
		}
		if !closed {
			i += n
		}
	}
	return spans
}

func runLen(s string, i int) int {
	n := 0
	for i+n < len(s) && s[i+n] == '`' {
		n++
	}
	return n
}

func inSpans(pos int, spans [][2]int) bool {
	for _, s := range spans {
		if pos >= s[0] && pos < s[1] {
			return true
		}
	}
	return false
}

// resolveTarget maps a relative link target to its published form. It returns
// ok=false for a target with no published counterpart (a private link) and an
// error for a target that escapes the workspace root.
func resolveTarget(target, src, pub string, targets map[string]string) (published string, ok bool, err error) {
	file, frag := target, ""
	if i := strings.IndexByte(target, '#'); i >= 0 {
		file, frag = target[:i], target[i:]
	}
	if strings.HasPrefix(file, "/") {
		return "", false, nil // absolute filesystem path: never shareable
	}
	resolved := path.Clean(path.Join(path.Dir(src), file))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "", false, fmt.Errorf("link %q escapes the workspace", target)
	}

	published, ok = targets[resolved]
	if !ok {
		// A folder link ("../other-project/") stands for its README.
		published, ok = targets[resolved+"/"+readmeFile]
	}
	if !ok {
		return "", false, nil
	}
	return relativePath(path.Dir(pub), published) + frag, true, nil
}

// relativePath returns the slash path from directory from to file to.
func relativePath(from, to string) string {
	fromParts := splitPath(from)
	toParts := splitPath(to)
	common := 0
	for common < len(fromParts) && common < len(toParts)-1 && fromParts[common] == toParts[common] {
		common++
	}
	parts := make([]string, 0, len(fromParts)-common+len(toParts)-common)
	for range fromParts[common:] {
		parts = append(parts, "..")
	}
	parts = append(parts, toParts[common:]...)
	return strings.Join(parts, "/")
}

func splitPath(p string) []string {
	if p == "." || p == "" {
		return nil
	}
	return strings.Split(p, "/")
}
