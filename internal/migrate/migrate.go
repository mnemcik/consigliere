package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mnemcik/consigliere/internal/manifest"
)

// itemDirs are the workspace trees that hold items.
var itemDirs = []string{keyAreas, "ideas", "notes", "insights", "projects"}

// indexFiles are curated tables, not items (DEC-011).
var indexFiles = map[string]bool{
	"projects/TODO.md": true, "areas/INDEX.md": true, "ideas/BACKLOG.md": true,
	"notes/INDEX.md": true, "insights/DRAFTS.md": true,
}

// itemTemplates are the templates that changed form in 1.23.0, as paths under
// both the embedded FS and the workspace's templates/ directory.
var itemTemplates = []string{"project/README.md", "idea.md", "area.md", "note.md", "insight.md"}

// shippedTemplates holds the SHA-256 of every `## Meta`-form version of an
// item template cg has ever shipped. A workspace copy matching one is
// untouched and safe to replace; anything else is the workspace's own edit.
var shippedTemplates = map[string]bool{
	"e7f146dfc1a09e5bdddb6cc7b67245474ee60ab954cf1f532a934c53a952d441": true, // project/README.md, v1.0.0
	"810c736979aab9e37bccc7f4acff4b231d44471003cf115b6e81856f646c7be3": true, // idea.md, v1.0.0
	"3fc360abea8fa866b669a8a6fb19ed7d09cb975c994b470f86e314d777e1a643": true, // area.md, v1.0.0
	"e270f0e6c5db76e6ae8c975c821a939cdec9709a93027bad6484ac34c726f654": true, // area.md, free-form tags
	"633962759731cef9a7b5cf318320c862f5310ade1e0a5c4538d335907637c590": true, // note.md, v1.0.0
	"7e6b82e35021480eca23e19f7527ebd11888e9264ea09f6261cde92d95cd1092": true, // note.md, ai-instructions comment
	"22c65a126497a62639dde513a130aa3362e1c3026f4c2bbd1e396ecd971bb9bf": true, // insight.md, v1.0.0
}

// Template actions.
const (
	TemplateReplace    = "replace"    // an untouched shipped copy; replaced
	TemplateCustomised = "customised" // edited by the workspace; left alone
)

// FileChange is one item Convert rewrote.
type FileChange struct {
	Path string // workspace-relative, slash-separated
	Result
	content string
}

// TemplateChange is one item template still in `## Meta` form.
type TemplateChange struct {
	Path    string
	Action  string
	content string
}

// Plan is everything a migration would do, computed without writing.
type Plan struct {
	Root      string
	Changes   []FileChange
	Templates []TemplateChange
	// Skipped lists framework and extension notes, which keep their body
	// `## Meta` by design (DEC-012).
	Skipped []string
	// Errors lists files that could not be converted; they are left as is.
	Errors []string
}

// Build computes a migration plan for the workspace at root. shipped is the
// embedded template tree (templates.FS).
func Build(root string, shipped fs.FS) (*Plan, error) {
	p := &Plan{Root: root}
	managed, err := managedNotes(root)
	if err != nil {
		return nil, err
	}
	for _, dir := range itemDirs {
		walkErr := filepath.WalkDir(filepath.Join(root, dir), func(abs string, d fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") || d.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			rel, err := filepath.Rel(root, abs)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if indexFiles[rel] {
				return nil
			}
			if managed[rel] {
				p.Skipped = append(p.Skipped, rel)
				return nil
			}
			b, err := os.ReadFile(abs) //nolint:gosec // abs is under the workspace root, from WalkDir
			if err != nil {
				return err
			}
			out, res, err := Convert(string(b))
			if err != nil {
				p.Errors = append(p.Errors, fmt.Sprintf("%s: %v", rel, err))
				return nil
			}
			if res.Changed {
				p.Changes = append(p.Changes, FileChange{Path: rel, Result: res, content: out})
			}
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	for _, t := range itemTemplates {
		b, err := os.ReadFile(filepath.Join(root, "templates", filepath.FromSlash(t))) //nolint:gosec // fixed names under the workspace root
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !strings.Contains(string(b), "## Meta") {
			continue // already in frontmatter form
		}
		rel := path.Join("templates", t)
		sum := sha256.Sum256(b)
		if !shippedTemplates[hex.EncodeToString(sum[:])] {
			p.Templates = append(p.Templates, TemplateChange{Path: rel, Action: TemplateCustomised})
			continue
		}
		fresh, err := fs.ReadFile(shipped, t)
		if err != nil {
			return nil, err
		}
		p.Templates = append(p.Templates, TemplateChange{Path: rel, Action: TemplateReplace, content: string(fresh)})
	}
	sort.Slice(p.Changes, func(i, j int) bool { return p.Changes[i].Path < p.Changes[j].Path })
	sort.Strings(p.Skipped)
	return p, nil
}

// Apply writes the plan: every converted item and every replaced template.
func (p *Plan) Apply() error {
	for _, c := range p.Changes {
		if err := writeKeepMode(filepath.Join(p.Root, filepath.FromSlash(c.Path)), c.content); err != nil {
			return err
		}
	}
	for _, t := range p.Templates {
		if t.Action != TemplateReplace {
			continue
		}
		if err := writeKeepMode(filepath.Join(p.Root, filepath.FromSlash(t.Path)), t.content); err != nil {
			return err
		}
	}
	return nil
}

func writeKeepMode(file, content string) error {
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	return os.WriteFile(file, []byte(content), info.Mode().Perm())
}

// managedNotes returns the notes cg or an extension ships: the framework notes
// recorded in .cg/manifest.json and the notes in every extension ledger under
// .cg/ext/. Their `## Meta` stays in the body so `cg sync` and
// `cg extension update` keep delivering them (DEC-012).
func managedNotes(root string) (map[string]bool, error) {
	out := map[string]bool{}
	m, err := manifest.Load(root)
	if err != nil {
		return nil, err
	}
	if m != nil {
		for n := range m.Notes {
			out[n] = true
		}
	}
	ledgers, err := filepath.Glob(filepath.Join(root, ".cg", "ext", "*.json"))
	if err != nil {
		return nil, err
	}
	for _, l := range ledgers {
		b, err := os.ReadFile(l) //nolint:gosec // a ledger under the workspace's .cg/ext
		if err != nil {
			return nil, err
		}
		var led struct {
			Notes []string `json:"notes"`
		}
		if err := json.Unmarshal(b, &led); err != nil {
			return nil, fmt.Errorf("%s: %w", l, err)
		}
		for _, n := range led.Notes {
			out[n] = true
		}
	}
	return out, nil
}

// Content returns the converted text of a planned change.
func (c *FileChange) Content() string { return c.content }
