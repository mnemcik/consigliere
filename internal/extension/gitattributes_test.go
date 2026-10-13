package extension

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/gitx"
)

func TestEnsureGitattributes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitattributes")
	read := func() string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	// A dry run on a workspace without the file reports a change, writes nothing.
	if changed, err := EnsureGitattributes(root, false); err != nil || !changed {
		t.Fatalf("dry run = %v, %v; want a change", changed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("dry run wrote .gitattributes")
	}

	// Created with just the block, then idempotent.
	if changed, err := EnsureGitattributes(root, true); err != nil || !changed {
		t.Fatalf("create = %v, %v", changed, err)
	}
	created := read()
	if !strings.HasPrefix(created, gitattributesStart+"\n") || !strings.Contains(created, "*.sh text eol=lf\n") ||
		!strings.HasSuffix(created, gitattributesEnd+"\n") {
		t.Errorf("created:\n%s", created)
	}
	if changed, err := EnsureGitattributes(root, true); err != nil || changed {
		t.Errorf("second run = %v, %v; want no change", changed, err)
	}

	// The user's own lines stay; an outdated block is replaced in place.
	old := "*.png binary\n\n" + gitattributesStart + "\n*.sh text\n" + gitattributesEnd + "\n*.md diff=markdown\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, err := EnsureGitattributes(root, true); err != nil || !changed {
		t.Fatalf("update = %v, %v", changed, err)
	}
	got := read()
	if !strings.HasPrefix(got, "*.png binary\n\n"+gitattributesStart) || !strings.HasSuffix(got, gitattributesEnd+"\n*.md diff=markdown\n") ||
		strings.Count(got, gitattributesStart) != 1 || !strings.Contains(got, ".cg/git-hooks/** text=auto eol=lf") {
		t.Errorf("updated:\n%s", got)
	}

	// A CRLF file stays CRLF, with no mixed lines.
	if err := os.WriteFile(path, []byte("*.png binary\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureGitattributes(root, true); err != nil {
		t.Fatal(err)
	}
	if got := read(); strings.Count(got, "\n") != strings.Count(got, "\r\n") || !strings.Contains(got, gitattributesStart) {
		t.Errorf("CRLF file:\n%q", got)
	}
}

// consigliere#170: with core.autocrlf on, cg's block makes a checkout write
// shell scripts with LF, while other files still follow the user's config; and
// a checkout from before the block is repaired by NormalizeScriptLineEndings.
func TestScriptsCheckedOutWithLF(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	git := func(dir string, args ...string) {
		t.Helper()
		if _, err := gitx.Run(ctx, dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	write := func(dir, rel, body string, mode os.FileMode) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	read := func(dir, rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, []byte("[core]\n\tautocrlf = true\n[user]\n\tname = t\n\temail = t@example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)

	const script = "#!/bin/sh\necho ok\n"
	origin := t.TempDir()
	git(origin, "init", "--quiet")
	write(origin, ".claude/hooks/gate.sh", script, 0o755)
	write(origin, ".cg/git-hooks/post-commit.d/demo-x", script, 0o755)
	write(origin, "notes/n.md", "a\nb\n", 0o644)
	// Sorts first in git's listing, so trimming the output would lose its space.
	const lead = " lead.sh"
	write(origin, lead, script, 0o755)
	git(origin, "add", "-A")
	git(origin, "commit", "--quiet", "-m", "before the block")

	// A clone made before the block: everything is CRLF.
	before := filepath.Join(t.TempDir(), "before")
	git("", "clone", "--quiet", origin, before)
	if got := read(before, ".claude/hooks/gate.sh"); got != strings.ReplaceAll(script, "\n", "\r\n") {
		t.Skipf("git didn't check out CRLF here (%q); nothing to test", got)
	}

	if _, err := EnsureGitattributes(origin, true); err != nil {
		t.Fatal(err)
	}
	git(origin, "add", "-A")
	git(origin, "commit", "--quiet", "-m", "add cg's block")

	// A clone made after: scripts LF, other files still CRLF.
	after := filepath.Join(t.TempDir(), "after")
	git("", "clone", "--quiet", origin, after)
	for _, rel := range []string{".claude/hooks/gate.sh", ".cg/git-hooks/post-commit.d/demo-x"} {
		if got := read(after, rel); got != script {
			t.Errorf("%s checked out as %q, want LF", rel, got)
		}
	}
	if got := read(after, "notes/n.md"); got != "a\r\nb\r\n" {
		t.Errorf("notes/n.md = %q: the block must leave other files to the user's config", got)
	}

	// The old clone, once it has the block, is repaired: a dry run lists the
	// scripts and writes nothing; an apply converts them and keeps their mode.
	git(before, "pull", "--quiet")
	// One script the user has edited, in their CRLF checkout.
	const edited = ".cg/git-hooks/post-commit.d/demo-x"
	write(before, edited, "#!/bin/sh\r\necho mine\r\n", 0o755)
	// A binary hook whose bytes happen to hold CRLF: not text, never touched.
	const bin = ".cg/git-hooks/post-commit.d/demo-bin"
	binData := "\x7fELF\x00\x01\r\n\x00"
	write(before, bin, binData, 0o755)
	git(before, "add", "--", bin)
	pending, err := NormalizeScriptLineEndings(ctx, before, false)
	if err != nil || len(pending) != 3 {
		t.Fatalf("dry run = %q, %v; want the three scripts", pending, err)
	}
	if got := read(before, ".claude/hooks/gate.sh"); got == script {
		t.Fatal("dry run rewrote a script")
	}
	if _, err := NormalizeScriptLineEndings(ctx, before, true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(before, ".claude", "hooks", "gate.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if got := read(before, ".claude/hooks/gate.sh"); got != script {
		t.Errorf("gate.sh after normalising = %q", got)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 { // Windows has no executable bit
		t.Errorf("gate.sh lost its executable bit: %v", info.Mode())
	}
	if got := read(before, lead); got != script {
		t.Errorf("%q after normalising = %q", lead, got)
	}
	if got := read(before, "notes/n.md"); got != "a\r\nb\r\n" {
		t.Errorf("notes/n.md was rewritten: %q", got)
	}
	if got := read(before, bin); got != binData {
		t.Errorf("binary hook was rewritten: %q", got)
	}
	if got := read(before, edited); got != "#!/bin/sh\necho mine\n" {
		t.Errorf("edited script after normalising = %q", got)
	}
	// Only the user's edit is left, unstaged: the untouched script is not
	// reported as modified, and nothing of the user's is staged for them.
	if status, err := gitx.Run(ctx, before, "status", "--porcelain", "--", ".cg/git-hooks/post-commit.d/demo-x", ".claude/"); err != nil || status != "M "+edited {
		t.Errorf("status after normalising = %q %v, want only the user's unstaged edit", status, err)
	}
	if again, err := NormalizeScriptLineEndings(ctx, before, true); err != nil || len(again) != 0 {
		t.Errorf("second run = %v, %v; want nothing", again, err)
	}
}
