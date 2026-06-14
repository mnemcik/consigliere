package extension

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// claudeMDName is the workspace CLAUDE.md an extension's sections are inserted into.
const claudeMDName = "CLAUDE.md"

// settingsRel is the Claude Code settings file hook contributions register in.
const settingsRel = ".claude/settings.json"

// hooksDirRel is where hook wrappers are installed.
const hooksDirRel = ".claude/hooks"

// Apply applies all of m's contributions from cloneDir into the workspace at
// root and returns a ledger of exactly what was written, for clean removal.
//
// It is fully transactional: every workspace file it may modify (CLAUDE.md,
// settings.json, each copied note/template, each hook wrapper) is snapshotted
// before any change, and on failure the snapshot is restored — so a failed
// apply leaves the workspace byte-for-byte as it was, even when contributions
// overwrite files a prior install of the same extension already placed.
// Subcommand contributions touch no workspace files (the external-subcommand
// resolver reads them from the clone); they are recorded so the ledger is a
// complete account of the manifest.
func Apply(root, cloneDir string, m *Manifest) (*Ledger, error) {
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

	l := &Ledger{Name: m.Name, Version: m.Version}
	if err := applyAll(root, cloneDir, m, l); err != nil {
		snap.restore() // workspace returns to its pre-apply state
		return nil, err
	}
	return l, nil
}

// applyAll performs the contribution steps, recording each into l. Callers wrap
// it with a snapshot so a mid-step failure can be rolled back.
func applyAll(root, cloneDir string, m *Manifest, l *Ledger) error {
	for _, s := range m.Contributes.ClaudeMDSections {
		if e := applySection(root, cloneDir, m.Name, s); e != nil {
			return e
		}
		l.ClaudeMDSections = append(l.ClaudeMDSections, s.ID)
	}
	for _, n := range m.Contributes.Notes {
		if e := copyInto(cloneDir, root, n.Src, n.Dest); e != nil {
			return e
		}
		l.Notes = append(l.Notes, n.Dest)
	}
	for _, t := range m.Contributes.Templates {
		if e := copyInto(cloneDir, root, t.Src, t.Dest); e != nil {
			return e
		}
		l.Templates = append(l.Templates, t.Dest)
	}
	for _, h := range m.Contributes.Hooks {
		wrapperRel, e := applyHook(root, cloneDir, m.Name, h)
		if e != nil {
			return e
		}
		l.Hooks = append(l.Hooks, LedgerHook{Event: h.Event, Wrapper: wrapperRel})
	}
	l.Subcommands = append(l.Subcommands, m.Contributes.Subcommands...)
	return nil
}

// hookDestRel is the workspace-relative install path of a hook wrapper: the
// extension name prefixes the base filename so wrappers from different
// extensions never collide.
func hookDestRel(name, wrapper string) string {
	return filepath.ToSlash(filepath.Join(hooksDirRel, name+"-"+filepath.Base(wrapper)))
}

// Reverse undoes everything recorded in l from the workspace at root. It is
// tolerant: a contribution already gone (file deleted, section absent) is not an
// error, so Reverse works both for clean removal and rollback of a partial Apply.
func Reverse(root string, l *Ledger) error {
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
				if err := os.WriteFile(claudePath, content, 0o644); err != nil { //nolint:gosec // workspace file
					return err
				}
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	for _, rel := range l.Notes {
		if err := removeIfPresent(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	for _, rel := range l.Templates {
		if err := removeIfPresent(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	for _, h := range l.Hooks {
		if err := removeIfPresent(filepath.Join(root, filepath.FromSlash(h.Wrapper))); err != nil {
			return err
		}
		if err := unregisterHook(root, h.Wrapper); err != nil {
			return err
		}
	}
	return nil
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
	orphan := &Ledger{Name: old.Name, Version: old.Version}
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
	if len(orphan.ClaudeMDSections) == 0 && len(orphan.Notes) == 0 &&
		len(orphan.Templates) == 0 && len(orphan.Hooks) == 0 {
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
	return os.WriteFile(claudePath, []byte(next), 0o644) //nolint:gosec // workspace file
}

// applyHook copies the wrapper into .claude/hooks/ (executable) and registers it
// for its event in settings.json. It returns the workspace-relative wrapper path.
// The installed filename is prefixed with the extension name so wrappers from
// different extensions can't collide at the same path.
func applyHook(root, cloneDir, name string, h HookContribution) (string, error) {
	destRel := hookDestRel(name, h.Wrapper)
	destAbs := filepath.Join(root, filepath.FromSlash(destRel))
	if err := copyFile(filepath.Join(cloneDir, filepath.FromSlash(h.Wrapper)), destAbs, 0o755); err != nil {
		return "", fmt.Errorf("hook %q: %w", h.Wrapper, err)
	}
	if err := registerHook(root, h.Event, destRel); err != nil {
		// Don't leave an unregistered wrapper the ledger won't know about.
		_ = os.Remove(destAbs)
		return "", err
	}
	return destRel, nil
}

// copyInto copies cloneDir/src to root/dest, creating parent dirs.
func copyInto(cloneDir, root, src, dest string) error {
	return copyFile(
		filepath.Join(cloneDir, filepath.FromSlash(src)),
		filepath.Join(root, filepath.FromSlash(dest)),
		0o644,
	)
}

func copyFile(src, dest string, mode os.FileMode) error {
	in, err := os.Open(src) //nolint:gosec // src is a manifest-declared path inside the clone
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil { //nolint:gosec // workspace dir
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode) //nolint:gosec // dest is workspace-relative
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dest) // don't leave a partial file the ledger won't track
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dest)
		return err
	}
	// OpenFile only applies mode on create; ensure the executable bit sticks.
	return os.Chmod(dest, mode)
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
			_ = os.WriteFile(f.path, f.data, f.mode)
		} else {
			_ = os.Remove(f.path)
		}
	}
}
