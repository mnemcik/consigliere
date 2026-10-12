package extension

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
// Subcommand contributions touch no workspace files (the external-subcommand
// resolver reads them from the clone); they are recorded so the ledger is a
// complete account of the manifest.
func Apply(root, cloneDir string, m *Manifest) (*Ledger, error) {
	return ApplyWith(root, cloneDir, m, ApplyOptions{Force: true})
}

// ApplyOptions carries what Apply needs to respect files the user owns.
type ApplyOptions struct {
	// Old is the ledger of the install being replaced, nil for a first install.
	Old *Ledger
	// LegacyBases stands in for Old.Files when Old predates them (see
	// LegacyBases).
	LegacyBases map[string]string
	// Force overwrites an existing, unrecorded file that differs from the
	// extension's version instead of refusing.
	Force bool
}

func (o *ApplyOptions) base(dest string) string {
	if o.Old != nil && o.Old.Files != nil {
		return o.Old.Files[dest]
	}
	return o.LegacyBases[dest]
}

// copyPlan is the decision for one copied file, made before anything is
// written so a refusal leaves the workspace untouched.
type copyPlan struct {
	dest     string
	data     []byte
	base     string
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
		disk, err := fileBlob(filepath.Join(root, filepath.FromSlash(c.Dest)))
		if err != nil {
			return err
		}
		p := copyPlan{dest: c.Dest, data: data, base: opts.base(c.Dest), next: BlobID(data), template: template}
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
func ApplyWith(root, cloneDir string, m *Manifest, opts ApplyOptions) (*Ledger, error) {
	plans, err := planCopies(root, cloneDir, m, &opts)
	if err != nil {
		return nil, err
	}
	snap := newSnapshot()
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
	if err := applyAll(root, cloneDir, m, l, plans); err != nil {
		snap.restore() // workspace returns to its pre-apply state
		return nil, err
	}
	return l, nil
}

// applyAll performs the contribution steps, recording each into l. Callers wrap
// it with a snapshot so a mid-step failure can be rolled back.
func applyAll(root, cloneDir string, m *Manifest, l *Ledger, plans []copyPlan) error {
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
			l.Files[p.dest] = p.base
		case copyPending:
			l.Files[p.dest] = p.base
			l.Pending = append(l.Pending, PendingFile{Dest: p.dest, Base: p.base, Upstream: p.next})
		case copyRefuse:
			return &ConflictError{Paths: []string{p.dest}} // planCopies already refused
		}
		if p.template {
			l.Templates = append(l.Templates, p.dest)
		} else {
			l.Notes = append(l.Notes, p.dest)
		}
	}
	for _, h := range m.Contributes.Hooks {
		wrapperRel, e := applyHook(root, cloneDir, m.Name, h)
		if e != nil {
			return e
		}
		l.Hooks = append(l.Hooks, LedgerHook{Event: h.Event, Wrapper: wrapperRel})
	}
	for _, g := range m.Contributes.GitHooks {
		dest := gitHookDestRel(m.Name, g)
		if e := copyExecutable(filepath.Join(cloneDir, filepath.FromSlash(g.Script)), filepath.Join(root, filepath.FromSlash(dest))); e != nil {
			return fmt.Errorf("git hook %q: %w", g.Script, e)
		}
		l.GitHooks = append(l.GitHooks, dest)
	}
	l.Subcommands = append(l.Subcommands, m.Contributes.Subcommands...)
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
	for _, rel := range append(append([]string(nil), l.Notes...), l.Templates...) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		disk, err := fileBlob(path)
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
// hook wrapper path. Subcommands are not file-backed, so none are returned.
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
	for _, p := range old.Notes {
		if !containsStr(next.Notes, p) {
			orphan.Notes = append(orphan.Notes, p)
		}
	}
	for _, p := range old.Templates {
		if !containsStr(next.Templates, p) {
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
	if len(orphan.ClaudeMDSections) == 0 && len(orphan.Notes) == 0 &&
		len(orphan.Templates) == 0 && len(orphan.Hooks) == 0 && len(orphan.GitHooks) == 0 &&
		len(orphan.Gitignore) == 0 {
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

// applyHook copies the wrapper into .claude/hooks/ (executable) and registers it
// for its event in settings.json. It returns the workspace-relative wrapper path.
// The installed filename is prefixed with the extension name so wrappers from
// different extensions can't collide at the same path.
func applyHook(root, cloneDir, name string, h HookContribution) (string, error) {
	destRel := hookDestRel(name, h.Wrapper)
	destAbs := filepath.Join(root, filepath.FromSlash(destRel))
	if err := copyExecutable(filepath.Join(cloneDir, filepath.FromSlash(h.Wrapper)), destAbs); err != nil {
		return "", fmt.Errorf("hook %q: %w", h.Wrapper, err)
	}
	if err := registerHook(root, h.Event, destRel); err != nil {
		// Don't leave an unregistered wrapper the ledger won't know about.
		_ = os.Remove(destAbs)
		return "", err
	}
	return destRel, nil
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
		if os.IsNotExist(err) {
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

// restore returns every captured file to its recorded state. It is best-effort:
// it is only ever called on an already-failing path, so a restore error can't
// improve the outcome and would only mask the original failure.
func (s *snapshot) restore() {
	for _, f := range s.files {
		if f.existed {
			_ = os.MkdirAll(filepath.Dir(f.path), 0o755)
			_ = writeFileAtomicMode(f.path, f.data, f.mode)
		} else {
			_ = os.Remove(f.path)
		}
	}
}
