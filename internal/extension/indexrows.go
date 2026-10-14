package extension

import (
	"fmt"
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
// indexRel is empty or the index doesn't exist. It returns the row to record,
// or nil when no block was written.
func applyIndexRows(root, indexRel string, m *Manifest, plans []copyPlan) (*LedgerIndexRow, error) {
	if indexRel == "" {
		return nil, nil
	}
	indexPath := filepath.Join(root, filepath.FromSlash(indexRel))
	raw, err := os.ReadFile(indexPath) //nolint:gosec // workspace-relative index path from .cg.json, cleaned by the caller
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	content := string(raw)
	crlf := manifest.AllCRLF(content)
	lf := manifest.LF(content)

	marker := indexMarker(m.Name)
	start, end := indexMarkers(marker)
	others, _ := removeBlock(lf, start, end, indexLine)
	linked := linkTargets(others)

	var rows []string
	for _, p := range plans {
		if p.template {
			continue
		}
		link := indexLink(indexRel, p.dest)
		if linked[link] {
			continue
		}
		rows = append(rows, indexRow(link, noteTitle(p.data, p.dest), noteIndexText(m, p.dest)))
	}

	var updated string
	var row *LedgerIndexRow
	if len(rows) == 0 {
		updated = others
	} else {
		body := fmt.Sprintf("## From the %s extension\n\n%s", m.Name, strings.Join(rows, "\n"))
		updated = upsertBlock(lf, start, end, body, indexLine)
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
// already gone is not an error.
func reverseIndexRows(root string, l *Ledger) error {
	for _, r := range l.IndexRows {
		rel, err := CleanSubdir(r.File)
		if err != nil || rel == "" {
			continue // not a path cg would have written
		}
		p := filepath.Join(root, filepath.FromSlash(rel))
		raw, err := os.ReadFile(p) //nolint:gosec // cleaned workspace-relative path from the ledger
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		content := string(raw)
		start, end := indexMarkers(r.Marker)
		next, ok := removeBlock(manifest.LF(content), start, end, indexLine)
		if !ok {
			continue
		}
		if manifest.AllCRLF(content) {
			next = manifest.CRLF(next)
		}
		if err := writeFileAtomic(p, []byte(next), 0o644); err != nil {
			return err
		}
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

func indexRow(link, title, text string) string {
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

// linkRe matches the target of a markdown inline link: ](target) or
// ](<target>), up to a title, fragment or the closing parenthesis.
var linkRe = regexp.MustCompile(`\]\(\s*(?:<([^>]*)>|([^)\s]*))`)

// linkTargets returns the link targets in content, cleaned for comparison with
// indexLink: no fragment, no leading "./".
func linkTargets(content string) map[string]bool {
	out := map[string]bool{}
	for _, m := range linkRe.FindAllStringSubmatch(content, -1) {
		t := m[1] + m[2]
		if i := strings.IndexByte(t, '#'); i >= 0 {
			t = t[:i]
		}
		if t == "" {
			continue
		}
		out[path.Clean(strings.ReplaceAll(t, "%20", " "))] = true
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
