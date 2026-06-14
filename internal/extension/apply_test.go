package extension

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeClone builds a fake extension clone dir with the given manifest plus stub
// files for every referenced contribution path.
func makeClone(t *testing.T, m *Manifest) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range m.Contributes.ClaudeMDSections {
		write(s.Path, "BODY:"+s.ID)
	}
	for _, n := range m.Contributes.Notes {
		write(n.Src, "note-body")
	}
	for _, tp := range m.Contributes.Templates {
		write(tp.Src, "template-body")
	}
	for _, h := range m.Contributes.Hooks {
		write(h.Wrapper, "#!/usr/bin/env bash\nexit 0\n")
	}
	return dir
}

func fullManifest() *Manifest {
	return &Manifest{
		Manifest: 1, Name: "demo", Version: "1.0.0", Description: "d",
		Contributes: Contributions{
			ClaudeMDSections: []SectionContribution{{ID: "demo-rules", Path: "fragments/rules.md"}},
			Notes:            []CopyContribution{{Src: "notes/guide.md", Dest: "notes/demo-guide.md"}},
			Templates:        []CopyContribution{{Src: "tpl/t.md", Dest: "templates/demo.md"}},
			Hooks:            []HookContribution{{Event: "SessionStart", Wrapper: "hooks/gate.sh", Command: "cg-demo gate"}},
			Subcommands:      []SubcommandContribution{{Namespace: "demo", Binary: "cg-demo"}},
		},
	}
}

func TestApplyAndReverseAllContributions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# CLAUDE.md\n\nuser stuff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := fullManifest()
	clone := makeClone(t, m)

	ledger, err := Apply(root, clone, m)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// CLAUDE.md section present with the fragment body.
	claude, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if !strings.Contains(string(claude), "ext:demo:section:start=demo-rules") || !strings.Contains(string(claude), "BODY:demo-rules") {
		t.Errorf("section not applied:\n%s", claude)
	}
	// Note + template copied.
	assertFile(t, filepath.Join(root, "notes", "demo-guide.md"), "note-body")
	assertFile(t, filepath.Join(root, "templates", "demo.md"), "template-body")
	// Hook wrapper installed (name-prefixed) + executable + registered.
	hookPath := filepath.Join(root, ".claude", "hooks", "demo-gate.sh")
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("hook wrapper not installed: %v", err)
	}
	if info.Mode()&0o100 == 0 {
		t.Errorf("hook wrapper should be executable, mode=%v", info.Mode())
	}
	settings, _ := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if !strings.Contains(string(settings), ".claude/hooks/demo-gate.sh") {
		t.Errorf("hook not registered in settings.json:\n%s", settings)
	}
	// Ledger records everything.
	if len(ledger.ClaudeMDSections) != 1 || len(ledger.Notes) != 1 || len(ledger.Templates) != 1 ||
		len(ledger.Hooks) != 1 || len(ledger.Subcommands) != 1 {
		t.Errorf("ledger incomplete: %+v", ledger)
	}

	// Reverse undoes all of it.
	if err := Reverse(root, ledger); err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	claude, _ = os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if strings.Contains(string(claude), "ext:demo:section") {
		t.Errorf("section not reversed:\n%s", claude)
	}
	if string(claude) != "# CLAUDE.md\n\nuser stuff\n" {
		t.Errorf("CLAUDE.md not restored: %q", claude)
	}
	for _, p := range []string{
		filepath.Join(root, "notes", "demo-guide.md"),
		filepath.Join(root, "templates", "demo.md"),
		hookPath,
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s should be removed, stat err=%v", p, err)
		}
	}
	settings, _ = os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if strings.Contains(string(settings), "gate.sh") {
		t.Errorf("hook not unregistered:\n%s", settings)
	}
}

func TestApplyRollsBackOnError(t *testing.T) {
	root := t.TempDir()
	const claudeBase = "# CLAUDE.md\n\nuser content\n"
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(claudeBase), 0o644); err != nil {
		t.Fatal(err)
	}
	m := fullManifest()
	clone := makeClone(t, m)
	// Break the hook by deleting its source so applyHook fails after sections,
	// notes, and templates already applied.
	if err := os.Remove(filepath.Join(clone, "hooks", "gate.sh")); err != nil {
		t.Fatal(err)
	}

	_, err := Apply(root, clone, m)
	if err == nil {
		t.Fatal("expected Apply to fail on the missing hook source")
	}
	// The section applied before the failure must be reversed (CLAUDE.md restored).
	claude, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if string(claude) != claudeBase {
		t.Errorf("CLAUDE.md not restored on rollback: %q", claude)
	}
	// Copied files applied before the failure must be removed.
	for _, p := range []string{
		filepath.Join(root, "notes", "demo-guide.md"),
		filepath.Join(root, "templates", "demo.md"),
	} {
		if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
			t.Errorf("%s should not exist after rollback, stat err=%v", p, statErr)
		}
	}
}

func TestReverseToleratesMissingArtifacts(t *testing.T) {
	root := t.TempDir()
	l := &Ledger{
		Name: "demo", Version: "1.0.0",
		ClaudeMDSections: []string{"gone"},
		Notes:            []string{"notes/gone.md"},
		Hooks:            []LedgerHook{{Event: "SessionStart", Wrapper: ".claude/hooks/gone.sh"}},
	}
	if err := Reverse(root, l); err != nil {
		t.Errorf("Reverse should tolerate already-absent artifacts, got %v", err)
	}
}

func TestOrphanLedger(t *testing.T) {
	old := &Ledger{
		Name:             "demo",
		ClaudeMDSections: []string{"a", "b"},
		Notes:            []string{"notes/x.md", "notes/y.md"},
		Templates:        []string{"templates/t.md"},
		Hooks:            []LedgerHook{{Event: "SessionStart", Wrapper: ".claude/hooks/demo-old.sh"}},
	}
	next := &Ledger{
		Name:             "demo",
		ClaudeMDSections: []string{"a"},                                                               // b dropped
		Notes:            []string{"notes/x.md"},                                                      // y dropped
		Templates:        []string{"templates/t.md"},                                                  // kept
		Hooks:            []LedgerHook{{Event: "SessionStart", Wrapper: ".claude/hooks/demo-new.sh"}}, // wrapper changed
	}
	orphan := OrphanLedger(old, next)
	if orphan == nil {
		t.Fatal("expected orphans")
	}
	if len(orphan.ClaudeMDSections) != 1 || orphan.ClaudeMDSections[0] != "b" {
		t.Errorf("section orphans wrong: %+v", orphan.ClaudeMDSections)
	}
	if len(orphan.Notes) != 1 || orphan.Notes[0] != "notes/y.md" {
		t.Errorf("note orphans wrong: %+v", orphan.Notes)
	}
	if len(orphan.Templates) != 0 {
		t.Errorf("kept template should not be orphaned: %+v", orphan.Templates)
	}
	if len(orphan.Hooks) != 1 || orphan.Hooks[0].Wrapper != ".claude/hooks/demo-old.sh" {
		t.Errorf("hook orphans wrong: %+v", orphan.Hooks)
	}

	// Identical ledgers produce no orphans; nil old is nil.
	if OrphanLedger(next, next) != nil {
		t.Error("identical ledgers should have no orphans")
	}
	if OrphanLedger(nil, next) != nil {
		t.Error("nil old should yield nil")
	}
}

func TestApplyDoesNotTouchWorkspaceOnFailure(t *testing.T) {
	// A failed Apply must leave a previously-applied install intact (the
	// caller applies the new manifest first, before reversing orphans).
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# CLAUDE.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	good := fullManifest()
	clone := makeClone(t, good)
	prior, err := Apply(root, clone, good)
	if err != nil {
		t.Fatalf("initial apply: %v", err)
	}

	// A new manifest whose hook source is missing → Apply fails and self-rolls-back.
	broken := fullManifest()
	broken.Version = "2.0.0"
	brokenClone := makeClone(t, broken)
	if err := os.Remove(filepath.Join(brokenClone, "hooks", "gate.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(root, brokenClone, broken); err == nil {
		t.Fatal("expected failure")
	}

	// The prior install's artifacts must still be present (Apply rolled back only
	// its own partial work; the caller hadn't reversed the prior ledger yet).
	mustHave(t, prior, root)
}

func mustHave(t *testing.T, l *Ledger, root string) {
	t.Helper()
	for _, n := range l.Notes {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(n))); err != nil {
			t.Errorf("prior note %s should survive: %v", n, err)
		}
	}
	for _, h := range l.Hooks {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(h.Wrapper))); err != nil {
			t.Errorf("prior hook %s should survive: %v", h.Wrapper, err)
		}
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("expected file %s: %v", path, err)
		return
	}
	if string(got) != want {
		t.Errorf("%s = %q, want %q", path, got, want)
	}
}
