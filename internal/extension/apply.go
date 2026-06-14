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
// It is best-effort transactional: if any step fails, everything already applied
// is reversed before the error is returned, so a failed install leaves the
// workspace unchanged. Subcommand contributions touch no workspace files (the
// external-subcommand resolver reads them from the clone); they are recorded so
// the ledger is a complete account of the manifest.
func Apply(root, cloneDir string, m *Manifest) (ledger *Ledger, err error) {
	l := &Ledger{Name: m.Name, Version: m.Version}
	defer func() {
		if err != nil {
			_ = Reverse(root, l) // roll back partial application
			ledger = nil
		}
	}()

	for _, s := range m.Contributes.ClaudeMDSections {
		if e := applySection(root, cloneDir, m.Name, s); e != nil {
			return nil, e
		}
		l.ClaudeMDSections = append(l.ClaudeMDSections, s.ID)
	}
	for _, n := range m.Contributes.Notes {
		if e := copyInto(cloneDir, root, n.Src, n.Dest); e != nil {
			return nil, e
		}
		l.Notes = append(l.Notes, n.Dest)
	}
	for _, t := range m.Contributes.Templates {
		if e := copyInto(cloneDir, root, t.Src, t.Dest); e != nil {
			return nil, e
		}
		l.Templates = append(l.Templates, t.Dest)
	}
	for _, h := range m.Contributes.Hooks {
		wrapperRel, e := applyHook(root, cloneDir, h)
		if e != nil {
			return nil, e
		}
		l.Hooks = append(l.Hooks, LedgerHook{Event: h.Event, Wrapper: wrapperRel})
	}
	l.Subcommands = append(l.Subcommands, m.Contributes.Subcommands...)
	return l, nil
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
func applyHook(root, cloneDir string, h HookContribution) (string, error) {
	base := filepath.Base(h.Wrapper)
	destRel := filepath.ToSlash(filepath.Join(hooksDirRel, base))
	if err := copyFile(filepath.Join(cloneDir, filepath.FromSlash(h.Wrapper)),
		filepath.Join(root, filepath.FromSlash(destRel)), 0o755); err != nil {
		return "", fmt.Errorf("hook %q: %w", h.Wrapper, err)
	}
	if err := registerHook(root, h.Event, destRel); err != nil {
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
		return err
	}
	if err := out.Close(); err != nil {
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
