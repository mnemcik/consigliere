package extension

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/mnemcik/consigliere/internal/githooks"
)

// claudeMDName is the workspace CLAUDE.md an extension's sections are inserted into.
const claudeMDName = "CLAUDE.md"

// settingsRel is the Claude Code settings file hook contributions register in.
const settingsRel = ".claude/settings.json"

// gitignoreName is the workspace .gitignore that gitignore blocks go into.
const gitignoreName = ".gitignore"

// hooksDirRel is where hook wrappers are installed.
const hooksDirRel = ".claude/hooks"

// Apply applies all of m's contributions from cloneDir into the workspace at
// root and returns a ledger of exactly what was written, for clean removal.
//
// It is fully transactional: every workspace file it may modify (CLAUDE.md,
// settings.json, .gitignore, each copied note/template, each hook wrapper) is snapshotted
// before any change, and on failure the snapshot is restored — so a failed
// apply leaves the workspace byte-for-byte as it was, even when contributions
// overwrite files a prior install of the same extension already placed.
// Command and launcher contributions touch no workspace files (command
// dispatch and `cg start` read them from the clone); they
// are recorded so the ledger is a complete account of the manifest.
func Apply(root, cloneDir string, m *Manifest) (*Ledger, error) {
	return ApplyWith(root, cloneDir, m, ApplyOptions{Force: true})
}

// ApplyOptions carries what Apply needs to respect files the user owns.
type ApplyOptions struct {
	// Old is the ledger of the install being replaced, nil for a first install.
	Old *Ledger
	// LegacyBases stands in for Old.Files when Old predates them (see
	// LegacyBases). LegacyCertain says they come from exactly the commit that
	// was installed (the store's tree at a recorded commit), rather than from
	// a legacy clone that may have moved since.
	LegacyBases   map[string]string
	LegacyCertain bool
	// Force overwrites an existing, unrecorded file that differs from the
	// extension's version instead of refusing.
	Force bool
	// NotesIndex is the workspace-relative notes index the notes' rows go in
	// (see indexrows.go). Empty writes no rows.
	NotesIndex string

	// SkillTargets are the agent tools skills are installed for (.cg.json
	// skills.targets); nil means the default.
	SkillTargets []string
	// SkillMode says how edited managed skill files are treated.
	SkillMode SkillMode
	// Context bounds the git calls that tell committed skill files apart.
	Context context.Context
	// ExtensionName and Repo name the extension in skill warnings.
	ExtensionName string
	Repo          string
	// SkillReport, when set, receives what the run tells the user about
	// skills.
	SkillReport *SkillReport
}

func (o *ApplyOptions) base(dest string) fileBase {
	if o.Old == nil {
		return fileBase{}
	}
	// A destination the old version shipped as a skill file is cg's too: its
	// base is in Files (S41).
	_, skill := oldSkillFiles(o.Old)[dest]
	b := fileBase{tracked: containsStr(o.Old.Notes, dest) || containsStr(o.Old.Templates, dest) || skill}
	if o.Old.HasContentIDs() {
		b.id = o.Old.Files[dest]
	} else {
		b.id, b.uncertain = o.LegacyBases[dest], !o.LegacyCertain
	}
	return b
}

// copyPlan is the decision for one copied file, made before anything is
// written so a refusal leaves the workspace untouched.
type copyPlan struct {
	dest     string
	data     []byte
	base     fileBase
	next     string
	decision copyDecision
	template bool
}

func planCopies(root, cloneDir string, m *Manifest, opts *ApplyOptions) ([]copyPlan, error) {
	var plans []copyPlan
	var refused []string
	add := func(c CopyContribution, template bool) error {
		data, err := os.ReadFile(filepath.Join(cloneDir, filepath.FromSlash(c.Src))) //nolint:gosec // manifest-declared path inside the source
		if err != nil {
			return err
		}
		disk, err := fileID(filepath.Join(root, filepath.FromSlash(c.Dest)))
		if err != nil {
			return err
		}
		p := copyPlan{dest: c.Dest, data: data, base: opts.base(c.Dest), next: ContentID(data), template: template}
		p.decision = decideCopy(p.base, disk, p.next, opts.Force)
		if p.decision == copyRefuse {
			refused = append(refused, c.Dest)
		}
		plans = append(plans, p)
		return nil
	}
	for _, n := range m.Contributes.Notes {
		if err := add(n, false); err != nil {
			return nil, err
		}
	}
	for _, t := range m.Contributes.Templates {
		if err := add(t, true); err != nil {
			return nil, err
		}
	}
	if len(refused) > 0 {
		return nil, &ConflictError{Paths: refused}
	}
	return plans, nil
}

// ApplyWith is Apply that keeps files the user changed (see ownership.go):
// such a file is left in place, and recorded as pending when the extension
// changed it too. It refuses, before writing anything, when an unrecorded file
// that differs from the extension's version is in the way, unless opts.Force.
//
//nolint:gocritic // by value on purpose: ApplyWith fills in its own copy
func ApplyWith(root, cloneDir string, m *Manifest, opts ApplyOptions) (*Ledger, error) {
	plans, err := planCopies(root, cloneDir, m, &opts)
	if err != nil {
		return nil, err
	}
	if opts.ExtensionName == "" {
		opts.ExtensionName = m.Name
	}
	skills, err := planSkills(root, cloneDir, m, &opts, opts.SkillReport)
	if err != nil {
		return nil, err
	}
	snap := newSnapshot()
	if err := captureSkills(root, skills, snap); err != nil {
		return nil, err
	}
	capture := func(rel string) error {
		return snap.capture(filepath.Join(root, filepath.FromSlash(rel)))
	}
	if err := capture(claudeMDName); err != nil {
		return nil, err
	}
	if err := capture(settingsRel); err != nil {
		return nil, err
	}
	if err := capture(gitignoreName); err != nil {
		return nil, err
	}
	if opts.NotesIndex != "" && !indexIsFile(root, opts.NotesIndex) {
		opts.NotesIndex = "" // no index to list the notes in
	}
	if opts.NotesIndex != "" {
		if err := capture(opts.NotesIndex); err != nil {
			return nil, err
		}
	}
	for _, n := range m.Contributes.Notes {
		if err := capture(n.Dest); err != nil {
			return nil, err
		}
	}
	for _, t := range m.Contributes.Templates {
		if err := capture(t.Dest); err != nil {
			return nil, err
		}
	}
	for _, h := range m.Contributes.Hooks {
		if err := capture(hookDestRel(m.Name, h.Wrapper)); err != nil {
			return nil, err
		}
	}
	for _, g := range m.Contributes.GitHooks {
		if err := capture(gitHookDestRel(m.Name, g)); err != nil {
			return nil, err
		}
	}

	l := &Ledger{Name: m.Name, Version: m.Version}
	if err := applyAll(root, cloneDir, m, l, plans, opts.NotesIndex); err != nil {
		snap.restore() // workspace returns to its pre-apply state
		return nil, err
	}
	if len(skills.paths) > 0 || len(skills.skills) > 0 || len(skills.gone) > 0 {
		if l.Files == nil {
			l.Files = map[string]string{}
		}
		if err := applySkills(root, skills, l, snap, opts.SkillReport); err != nil {
			snap.restore()
			return nil, err
		}
	}
	return l, nil
}

// applyAll performs the contribution steps, recording each into l. Callers wrap
// it with a snapshot so a mid-step failure can be rolled back.
func applyAll(root, cloneDir string, m *Manifest, l *Ledger, plans []copyPlan, notesIndex string) error {
	for _, s := range m.Contributes.ClaudeMDSections {
		if e := applySection(root, cloneDir, m.Name, s); e != nil {
			return e
		}
		l.ClaudeMDSections = append(l.ClaudeMDSections, s.ID)
	}
	for _, g := range m.Contributes.Gitignore {
		if e := applyGitignore(root, cloneDir, m.Name, g); e != nil {
			return e
		}
		l.Gitignore = append(l.Gitignore, g.ID)
	}
	for _, p := range plans {
		if l.Files == nil {
			l.Files = map[string]string{}
		}
		switch p.decision {
		case copyWrite:
			if e := writeFileAtomicMode(filepath.Join(root, filepath.FromSlash(p.dest)), p.data, 0o644); e != nil {
				return e
			}
			l.Files[p.dest] = p.next
		case copyAdopt:
			l.Files[p.dest] = p.next
		case copyKeep:
			l.Files[p.dest] = p.base.id
		case copyPending:
			if p.base.id != "" {
				l.Files[p.dest] = p.base.id
			}
			l.Pending = append(l.Pending, PendingFile{Dest: p.dest, Base: p.base.id, Upstream: p.next, UncertainBase: p.base.uncertain})
		case copyRefuse:
			return &ConflictError{Paths: []string{p.dest}} // planCopies already refused
		}
		if p.template {
			l.Templates = append(l.Templates, p.dest)
		} else {
			l.Notes = append(l.Notes, p.dest)
		}
	}
	row, err := applyIndexRows(root, notesIndex, m, plans)
	if err != nil {
		return fmt.Errorf("notes index: %w", err)
	}
	if row != nil {
		l.IndexRows = append(l.IndexRows, *row)
	}
	if e := applyHooks(root, cloneDir, m, l); e != nil {
		return e
	}
	for _, g := range m.Contributes.GitHooks {
		dest := gitHookDestRel(m.Name, g)
		if e := copyExecutable(filepath.Join(cloneDir, filepath.FromSlash(g.Script)), filepath.Join(root, filepath.FromSlash(dest))); e != nil {
			return fmt.Errorf("git hook %q: %w", g.Script, e)
		}
		l.GitHooks = append(l.GitHooks, dest)
	}
	l.Commands = append(l.Commands, m.Contributes.Commands...)
	l.Launcher = append(l.Launcher, m.Contributes.Launcher...)
	return nil
}

// gitHookDestRel is the workspace-relative install path of a git-hook script:
// .cg/git-hooks/<hook>.d/<extension>-<base>. Callers run githooks.Ensure
// afterwards to install the dispatcher that runs it.
func gitHookDestRel(name string, g GitHookContribution) string {
	return filepath.ToSlash(filepath.Join(githooks.DirRel, g.Hook+".d", name+"-"+filepath.Base(g.Script)))
}

// hookDestRel is the workspace-relative install path of a hook wrapper: the
// extension name prefixes the base filename so wrappers from different
// extensions never collide.
func hookDestRel(name, wrapper string) string {
	return filepath.ToSlash(filepath.Join(hooksDirRel, name+"-"+filepath.Base(wrapper)))
}

// Reverse undoes everything recorded in l from the workspace at root. It is
// tolerant: a contribution already gone (file deleted, section absent) is not an
// error. A copied note or template is deleted only when its content is still
// the version cg wrote (l.Files); one the user changed, or one with no record,
// is left in place and returned in kept.
func Reverse(root string, l *Ledger) (kept []string, err error) {
	claudePath := filepath.Join(root, claudeMDName)
	if len(l.ClaudeMDSections) > 0 {
		if content, err := os.ReadFile(claudePath); err == nil {
			changed := false
			for _, id := range l.ClaudeMDSections {
				if next, ok := RemoveSection(string(content), l.Name, id); ok {
					content = []byte(next)
					changed = true
				}
			}
			if changed {
				if err := writeFileAtomic(claudePath, content, 0o644); err != nil {
					return kept, err
				}
			}
		} else if !os.IsNotExist(err) {
			return kept, err
		}
	}
	if err := reverseGitignore(root, l); err != nil {
		return kept, err
	}
	if err := reverseIndexRows(root, l); err != nil {
		return kept, err
	}
	for _, rel := range append(append([]string(nil), l.Notes...), l.Templates...) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		disk, err := fileID(path)
		if err != nil {
			return kept, err
		}
		switch disk {
		case "":
		case l.Files[rel]:
			if err := removeIfPresent(path); err != nil {
				return kept, err
			}
		default:
			kept = append(kept, rel)
		}
	}
	for _, h := range l.Hooks {
		if err := removeIfPresent(filepath.Join(root, filepath.FromSlash(h.Wrapper))); err != nil {
			return kept, err
		}
		if err := unregisterHook(root, h.Wrapper); err != nil {
			return kept, err
		}
	}
	for _, rel := range l.GitHooks {
		if err := removeIfPresent(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			return kept, err
		}
	}
	return kept, nil
}

// OrphanLedger returns the contributions present in old but absent from next —
// the artifacts a reinstall/update no longer ships, which must be reversed after
// the new manifest is applied. Identity is by section id, copied-file dest, and
// hook wrapper path. Commands are not file-backed, so none are returned.
// Returns nil when old is nil or nothing is orphaned.
func OrphanLedger(old, next *Ledger) *Ledger {
	if old == nil {
		return nil
	}
	orphan := &Ledger{Name: old.Name, Version: old.Version, Files: old.Files}
	for _, id := range old.ClaudeMDSections {
		if !containsStr(next.ClaudeMDSections, id) {
			orphan.ClaudeMDSections = append(orphan.ClaudeMDSections, id)
		}
	}
	// A destination that moved between notes, templates and skills is still
	// shipped (S40). Skill files themselves are deleted by the skill plan.
	nextSkills := oldSkillFiles(next)
	for _, p := range old.Notes {
		if _, skill := nextSkills[p]; !containsStr(next.Notes, p) && !containsStr(next.Templates, p) && !skill {
			orphan.Notes = append(orphan.Notes, p)
		}
	}
	for _, p := range old.Templates {
		if _, skill := nextSkills[p]; !containsStr(next.Notes, p) && !containsStr(next.Templates, p) && !skill {
			orphan.Templates = append(orphan.Templates, p)
		}
	}
	for _, h := range old.Hooks {
		if !hooksContainWrapper(next.Hooks, h.Wrapper) {
			orphan.Hooks = append(orphan.Hooks, h)
		}
	}
	for _, p := range old.GitHooks {
		if !containsStr(next.GitHooks, p) {
			orphan.GitHooks = append(orphan.GitHooks, p)
		}
	}
	for _, id := range old.Gitignore {
		if !containsStr(next.Gitignore, id) {
			orphan.Gitignore = append(orphan.Gitignore, id)
		}
	}
	// A block in an index the new install wrote none to: the extension no
	// longer ships notes, or .cg.json now names a different index.
	for _, r := range old.IndexRows {
		if !indexRowsContain(next.IndexRows, r) {
			orphan.IndexRows = append(orphan.IndexRows, r)
		}
	}
	if len(orphan.ClaudeMDSections) == 0 && len(orphan.Notes) == 0 &&
		len(orphan.Templates) == 0 && len(orphan.Hooks) == 0 && len(orphan.GitHooks) == 0 &&
		len(orphan.Gitignore) == 0 && len(orphan.IndexRows) == 0 {
		return nil
	}
	return orphan
}

func containsStr(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

func indexRowsContain(rs []LedgerIndexRow, want LedgerIndexRow) bool {
	for _, r := range rs {
		if r == want {
			return true
		}
	}
	return false
}

func hooksContainWrapper(hs []LedgerHook, wrapper string) bool {
	for _, h := range hs {
		if h.Wrapper == wrapper {
			return true
		}
	}
	return false
}

func applySection(root, cloneDir, name string, s SectionContribution) error {
	body, err := os.ReadFile(filepath.Join(cloneDir, filepath.FromSlash(s.Path)))
	if err != nil {
		return fmt.Errorf("section %q: %w", s.ID, err)
	}
	claudePath := filepath.Join(root, claudeMDName)
	content, err := os.ReadFile(claudePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		content = nil
	}
	next := UpsertSection(string(content), name, s.ID, strings.Trim(string(body), "\n"))
	return writeFileAtomic(claudePath, []byte(next), 0o644)
}

// applyGitignore inserts or replaces the extension's block in the workspace
// .gitignore, creating the file if there is none.
func applyGitignore(root, cloneDir, name string, g SectionContribution) error {
	body, err := os.ReadFile(filepath.Join(cloneDir, filepath.FromSlash(g.Path)))
	if err != nil {
		return fmt.Errorf("gitignore %q: %w", g.ID, err)
	}
	path := filepath.Join(root, gitignoreName)
	content, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		content = nil
	}
	next := UpsertGitignore(string(content), name, g.ID, strings.Trim(string(body), "\n"))
	return writeFileAtomic(path, []byte(next), 0o644)
}

// reverseGitignore removes the blocks recorded in l from the workspace
// .gitignore. A file left with nothing but whitespace is deleted, so an install
// that created .gitignore leaves no trace.
func reverseGitignore(root string, l *Ledger) error {
	if len(l.Gitignore) == 0 {
		return nil
	}
	path := filepath.Join(root, gitignoreName)
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	text, changed := string(content), false
	for _, id := range l.Gitignore {
		if next, ok := RemoveGitignore(text, l.Name, id); ok {
			text, changed = next, true
		}
	}
	if !changed {
		return nil
	}
	if strings.TrimSpace(text) == "" {
		return removeIfPresent(path)
	}
	return writeFileAtomic(path, []byte(text), 0o644)
}

// applyHooks copies each distinct wrapper once into .claude/hooks/
// (executable), registers every hook entry in settings.json in one write, and
// records one ledger entry per hook entry. The installed filename is prefixed
// with the extension name so wrappers from different extensions can't collide
// at the same path. A failure is undone by ApplyWith's snapshot, which covers
// settings.json and every wrapper's install path.
func applyHooks(root, cloneDir string, m *Manifest, l *Ledger) error {
	regs := make([]hookReg, 0, len(m.Contributes.Hooks))
	copied := map[string]bool{}
	for _, h := range m.Contributes.Hooks {
		destRel := hookDestRel(m.Name, h.Wrapper)
		if !copied[destRel] {
			destAbs := filepath.Join(root, filepath.FromSlash(destRel))
			if err := copyExecutable(filepath.Join(cloneDir, filepath.FromSlash(h.Wrapper)), destAbs); err != nil {
				return fmt.Errorf("hook %q: %w", h.Wrapper, err)
			}
			copied[destRel] = true
		}
		regs = append(regs, hookReg{Event: h.Event, Matcher: h.Matcher, If: h.If, CommandRel: destRel})
	}
	if err := registerHooks(root, regs); err != nil {
		return err
	}
	for _, r := range regs {
		l.Hooks = append(l.Hooks, LedgerHook{Event: r.Event, Wrapper: r.CommandRel})
	}
	return nil
}

// copyExecutable copies a hook or git-hook script, executable.
func copyExecutable(src, dest string) error {
	data, err := os.ReadFile(src) //nolint:gosec // src is a manifest-declared path inside the clone
	if err != nil {
		return err
	}
	// Atomic, so a hook or the gh shim run by a parallel session never executes
	// a half-written file, and a failed copy leaves the previous file intact.
	return writeFileAtomicMode(dest, data, 0o755)
}

func removeIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// snapshot records the pre-apply state of a set of files so Apply can roll the
// workspace back exactly on failure — restoring overwritten files to their prior
// bytes and removing files that did not exist before. This makes Apply safe to
// run over a prior install of the same extension (overlapping dest paths): a
// failed reapply leaves the prior install intact.
type snapshot struct {
	files []fileSnapshot
	seen  map[string]bool
	// dirs are directories the run created, removed again on restore.
	dirs []string
}

type fileSnapshot struct {
	path    string
	existed bool
	data    []byte
	mode    os.FileMode
}

func newSnapshot() *snapshot {
	return &snapshot{seen: map[string]bool{}}
}

// capture records the current state of path (each path once).
func (s *snapshot) capture(path string) error {
	if s.seen[path] {
		return nil
	}
	s.seen[path] = true
	data, err := os.ReadFile(path)
	if err != nil {
		info, serr := os.Stat(path)
		if os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR) || (serr == nil && info.IsDir()) {
			// No file there (a directory a skill file replaces counts as
			// none: its files are captured on their own).
			s.files = append(s.files, fileSnapshot{path: path, existed: false})
			return nil
		}
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	s.files = append(s.files, fileSnapshot{path: path, existed: true, data: data, mode: info.Mode().Perm()})
	return nil
}

// captureDirs records the directories between root and path's parent that
// don't exist yet, so restore can remove them again.
func (s *snapshot) captureDirs(root, path string) error {
	var missing []string
	for dir := filepath.Dir(path); dir != root && len(dir) > len(root); dir = filepath.Dir(dir) {
		if _, err := os.Lstat(dir); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return err
		}
		missing = append(missing, dir)
	}
	s.dirs = append(s.dirs, missing...) // deepest first within each call
	return nil
}

// restore returns every captured file to its recorded state. It is best-effort:
// it is only ever called on an already-failing path, so a restore error can't
// improve the outcome and would only mask the original failure.
func (s *snapshot) restore() {
	// Last captured first: a file written where a captured directory held
	// files goes before those files are written back.
	for i := len(s.files) - 1; i >= 0; i-- {
		f := s.files[i]
		if f.existed {
			if info, err := os.Lstat(f.path); err == nil && info.IsDir() {
				// The run made the file's path a directory; its files were
				// restored (removed) above. Remove the directories it created
				// under it, deepest first, then the directory itself.
				var under []string
				for _, d := range s.dirs {
					if d == f.path || strings.HasPrefix(d, f.path+string(filepath.Separator)) {
						under = append(under, d)
					}
				}
				sort.Slice(under, func(i, j int) bool { return len(under[i]) > len(under[j]) })
				for _, d := range under {
					_ = os.Remove(d)
				}
				_ = os.Remove(f.path)
			}
			_ = os.MkdirAll(filepath.Dir(f.path), 0o755)
			_ = writeFileAtomicMode(f.path, f.data, f.mode)
		} else {
			_ = os.Remove(f.path)
		}
	}
	// Created directories, deepest first; one still holding a file restored
	// above (or anything else) stays.
	dirs := append([]string(nil), s.dirs...)
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		// Only a directory: a file restored above may have the same path
		// (a managed file the run had replaced with a directory).
		if info, err := os.Lstat(d); err == nil && info.IsDir() {
			_ = os.Remove(d)
		}
	}
}
