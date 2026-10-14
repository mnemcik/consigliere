package extension

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mnemcik/consigliere/internal/manifest"
)

// DefaultNotesIndex is the notes index a workspace uses when .cg.json names
// none.
const DefaultNotesIndex = "notes/INDEX.md"

// An extension's notes are listed in the workspace notes index as one block per
// extension, with its own heading, so removal is exact and no state is shared
// between extensions:
//
//	<!-- ext:<name>:index:start -->
//	## From the <name> extension
//
//	- [<title>](<link>) — <index text>
//	<!-- ext:<name>:index:end -->
//
// The block is cg's: an update rewrites it. A note the index already links to
// outside the block gets no row, so a row the user placed by hand stays the
// only one.

// indexMarker is the ledger's name for an extension's index block.
func indexMarker(name string) string {
	return "ext:" + name + ":index"
}

func indexMarkers(marker string) (start, end string) {
	return "<!-- " + marker + ":start -->", "<!-- " + marker + ":end -->"
}

// applyIndexRows writes the index block for m's notes into the index at
// indexRel, or removes it when no note needs a row. It does nothing when
// indexRel is empty or the index isn't a regular file. It returns the row to
// record, or nil when no block was written.
func applyIndexRows(root, indexRel string, m *Manifest, plans []copyPlan) (*LedgerIndexRow, error) {
	if indexRel == "" {
		return nil, nil
	}
	indexPath := filepath.Join(root, filepath.FromSlash(indexRel))
	content, ok, err := loadIndex(indexPath)
	if err != nil || !ok {
		return nil, err
	}
	// A CRLF checkout is edited as LF and written back as CRLF; any other file
	// is edited as it is, so a stray CRLF line of the user's stays as it was.
	crlf := manifest.AllCRLF(content)
	lf := content
	if crlf {
		lf = manifest.LF(content)
	}

	marker := indexMarker(m.Name)
	start, end := indexMarkers(marker)
	if err := checkBlock(lf, start, end); err != nil {
		return nil, fmt.Errorf("%s: %w", indexRel, err)
	}
	others, _ := removeBlock(lf, start, end, indexLine)
	linked := linkTargets(others, path.Dir(indexRel))

	var rows []string
	for _, p := range plans {
		if p.template {
			continue
		}
		dest := path.Clean(p.dest)
		if linked[dest] {
			continue
		}
		data := p.data
		if p.decision == copyKeep || p.decision == copyPending {
			// The user's file stays, so the row describes it; a note the
			// user deleted gets no row.
			disk, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dest))) //nolint:gosec // manifest dest, validated inside the workspace
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, err
			}
			data = disk
		}
		rows = append(rows, indexRow(indexLink(indexRel, dest), noteTitle(data, dest), noteIndexText(m, p.dest)))
	}

	var updated string
	var row *LedgerIndexRow
	if len(rows) == 0 {
		updated = others
	} else {
		body := fmt.Sprintf("## From the %s extension\n\n%s", m.Name, strings.Join(rows, "\n"))
		updated = upsertBlock(lf, start, end, body, indexLine)
		if strings.TrimSpace(lf) == "" {
			updated = strings.TrimLeft(updated, "\r\n")
		}
		row = &LedgerIndexRow{File: indexRel, Marker: marker}
	}
	if updated == lf {
		return row, nil
	}
	if crlf {
		updated = manifest.CRLF(updated)
	}
	return row, writeFileAtomic(indexPath, []byte(updated), 0o644)
}

// reverseIndexRows removes the index blocks recorded in l. A block or index
// already gone is not an error; a block whose markers are broken is, rather
// than a guess at where it ends.
func reverseIndexRows(root string, l *Ledger) error {
	for _, r := range l.IndexRows {
		rel, err := CleanSubdir(r.File)
		if err != nil || rel == "" {
			continue // not a path cg would have written
		}
		p := filepath.Join(root, rel)
		content, ok, err := loadIndex(p)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		crlf := manifest.AllCRLF(content)
		lf := content
		if crlf {
			lf = manifest.LF(content)
		}
		start, end := indexMarkers(r.Marker)
		if err := checkBlock(lf, start, end); err != nil {
			return fmt.Errorf("%s: %w", r.File, err)
		}
		next, found := removeBlock(lf, start, end, indexLine)
		if !found {
			continue
		}
		if crlf {
			next = manifest.CRLF(next)
		}
		if err := writeFileAtomic(p, []byte(next), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// CheckIndexRows reports a recorded index block whose markers are broken.
// Update runs it on the old ledger before applying anything, so a block that
// would fail to reverse stops the update before any file is written.
func CheckIndexRows(root string, l *Ledger) error {
	if l == nil {
		return nil
	}
	for _, r := range l.IndexRows {
		rel, err := CleanSubdir(r.File)
		if err != nil || rel == "" {
			continue
		}
		content, ok, err := loadIndex(filepath.Join(root, rel))
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		lf := content
		if manifest.AllCRLF(content) {
			lf = manifest.LF(content)
		}
		start, end := indexMarkers(r.Marker)
		if err := checkBlock(lf, start, end); err != nil {
			return fmt.Errorf("%s: %w", r.File, err)
		}
	}
	return nil
}

// loadIndex reads the index at p. ok is false when there is no regular file
// there, which is not an error: such a workspace just has no notes index.
func loadIndex(p string) (content string, ok bool, err error) {
	info, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if !info.Mode().IsRegular() {
		return "", false, nil
	}
	raw, err := os.ReadFile(p) //nolint:gosec // cleaned workspace-relative index path
	if err != nil {
		return "", false, err
	}
	return string(raw), true, nil
}

// indexIsFile reports whether the index at indexRel is a regular file, so
// ApplyWith snapshots only an index it may write.
func indexIsFile(root, indexRel string) bool {
	_, ok, _ := loadIndex(filepath.Join(root, filepath.FromSlash(indexRel)))
	return ok
}

// checkBlock refuses a block whose markers don't pair up: a start with no end
// after it, or a marker that appears twice. Editing such a file could append
// a second block or remove the user's own lines between stray markers.
func checkBlock(content, start, end string) error {
	si, ei := indexLine(content, start), indexLine(content, end)
	switch {
	case si < 0 && ei < 0:
		return nil
	case si < 0 || ei < 0 || ei < si:
		return fmt.Errorf("the %s block's markers are broken; fix or delete them, then retry", strings.TrimSuffix(strings.TrimPrefix(start, "<!-- "), ":start -->"))
	}
	if indexLine(content[si+len(start):], start) >= 0 || indexLine(content[ei+len(end):], end) >= 0 {
		return fmt.Errorf("the %s block appears twice; delete one copy, then retry", strings.TrimSuffix(strings.TrimPrefix(start, "<!-- "), ":start -->"))
	}
	return nil
}

// noteIndexText is the manifest's index text for the note copied to dest.
func noteIndexText(m *Manifest, dest string) string {
	for _, n := range m.Contributes.Notes {
		if n.Dest == dest {
			return strings.TrimSpace(n.Index)
		}
	}
	return ""
}

// linkEscaper encodes the characters that would end a link's path: "#" and
// "?" start a fragment or query, and "%" starts an escape.
var linkEscaper = strings.NewReplacer("%", "%25", "#", "%23", "?", "%3F")

func indexRow(link, title, text string) string {
	link = linkEscaper.Replace(link)
	if strings.ContainsAny(link, " ()") {
		link = "<" + link + ">"
	}
	title = strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`).Replace(title)
	row := fmt.Sprintf("- [%s](%s)", title, link)
	if text != "" {
		row += " — " + text
	}
	return row
}

// indexLink is the link from the index at indexRel to the note at dest, both
// workspace-relative with slashes.
func indexLink(indexRel, dest string) string {
	dir := path.Dir(indexRel)
	if dir == "." {
		return dest
	}
	up := ""
	for dir != "." && !strings.HasPrefix(dest, dir+"/") {
		dir = path.Dir(dir)
		up += "../"
	}
	if dir == "." {
		return up + dest
	}
	return up + strings.TrimPrefix(dest, dir+"/")
}

// linkRe matches the target of a markdown inline link, ](target) or
// ](<target>), and of a reference definition, [label]: target (not a
// footnote, [^1]: text), up to a title, fragment or the closing parenthesis.
var linkRe = regexp.MustCompile(`\]\(\s*(?:<([^>]*)>|([^)\s]*))|(?m)^ {0,3}\[[^\]^][^\]]*\]:\s*(?:<([^>]*)>|(\S+))`)

// linkTargets returns the workspace-relative paths that content, an index in
// the workspace directory dir, links to: resolved against dir, with no
// fragment or query.
func linkTargets(content, dir string) map[string]bool {
	out := map[string]bool{}
	for _, m := range linkRe.FindAllStringSubmatch(content, -1) {
		t := m[1] + m[2] + m[3] + m[4]
		if i := strings.IndexAny(t, "#?"); i >= 0 {
			t = t[:i]
		}
		if t == "" || strings.Contains(t, "://") || strings.HasPrefix(t, "/") {
			continue
		}
		if u, err := url.PathUnescape(t); err == nil {
			t = u
		}
		out[path.Join(dir, t)] = true
	}
	return out
}

// noteTitle is the note's frontmatter title, else its first level-1 heading,
// else its file name without the extension.
func noteTitle(data []byte, dest string) string {
	text := manifest.LF(string(data))
	fm, body := manifest.SplitFrontmatter(text)
	if fm != "" {
		var parsed struct {
			Title string `yaml:"title"`
		}
		inner := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(fm, "---\n"), "\n"), "---")
		if yaml.Unmarshal([]byte(inner), &parsed) == nil {
			if t := oneLine(parsed.Title); t != "" {
				return t
			}
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "# ") {
			if t := oneLine(strings.TrimPrefix(line, "# ")); t != "" {
				return t
			}
		}
	}
	return strings.TrimSuffix(path.Base(dest), path.Ext(dest))
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
