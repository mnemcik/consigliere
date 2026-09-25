package share

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// linkRe matches an inline markdown link or image: [text](target "title").
// Groups: 1 "!" for images, 2 text, 3 target, 4 optional title.
var linkRe = regexp.MustCompile(`(!?)\[([^\]]*)\]\(([^)\s]+)(\s+"[^"]*")?\)`)

var schemeRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

// PrivateMarker is appended to the text of a de-linked private link.
const PrivateMarker = "*(private)*"

// isExternal reports whether a link target leaves the workspace (a URL or
// other scheme) or stays within the current page (a bare fragment).
func isExternal(target string) bool {
	return strings.HasPrefix(target, "#") || schemeRe.MatchString(target)
}

// resolveLinks rewrites the relative links in body. src is the file's
// workspace-relative path and pub its published path; targets maps
// workspace-relative paths to published ones. Fenced code blocks and inline
// code spans are left untouched.
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
		out, err := resolveLine(line, src, pub, targets)
		if err != nil {
			return "", fmt.Errorf("line %d: %w", i+1, err)
		}
		lines[i] = out
	}
	return strings.Join(lines, "\n"), nil
}

// resolveLine rewrites the links on one line. A link that starts inside an
// inline code span is literal text and left alone; a link whose text merely
// contains a code span ([`name` DEC-5](...)) is still a link.
func resolveLine(line, src, pub string, targets map[string]string) (string, error) {
	spans := codeSpans(line)
	var b strings.Builder
	last := 0
	for _, loc := range linkRe.FindAllStringSubmatchIndex(line, -1) {
		if inSpans(loc[0], spans) {
			continue
		}
		image := line[loc[2]:loc[3]]
		text := line[loc[4]:loc[5]]
		target := line[loc[6]:loc[7]]
		title := ""
		if loc[8] >= 0 {
			title = line[loc[8]:loc[9]]
		}
		if isExternal(target) {
			continue
		}
		rewritten, ok, err := resolveTarget(target, src, pub, targets)
		if err != nil {
			return "", err
		}
		b.WriteString(line[last:loc[0]])
		switch {
		case ok:
			b.WriteString(image + "[" + text + "](" + rewritten + title + ")")
		case text == "":
			b.WriteString(PrivateMarker)
		default:
			b.WriteString(text + " " + PrivateMarker)
		}
		last = loc[1]
	}
	b.WriteString(line[last:])
	return b.String(), nil
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
