package gitx

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func wantStates(t *testing.T, ctx context.Context, dir string, want map[string]PathState) {
	t.Helper()
	paths := make([]string, 0, len(want))
	for p := range want {
		paths = append(paths, p)
	}
	got, err := PathStates(ctx, dir, paths)
	if err != nil {
		t.Fatal(err)
	}
	for p, w := range want {
		g, ok := got[p]
		if !ok {
			t.Errorf("%s: no state returned", p)
			continue
		}
		if g != w {
			t.Errorf("%s: got state %d, want %d", p, g, w)
		}
	}
}

// Q10: committed means git status reports nothing for the path.
func TestPathStates(t *testing.T) {
	ctx, dir := initRepo(t)
	writeFile(t, dir, ".gitignore", "ignored.md\n")
	for _, f := range []string{"clean.md", "modified.md", "staged.md", "deleted.md", "renamed.md", "assumed.md", "skipped.md", "sub dir/with space.md"} {
		writeFile(t, dir, f, "v1\n")
	}
	mustRun(t, ctx, dir, "add", "-A")
	mustRun(t, ctx, dir, "commit", "-m", "files")
	writeFile(t, dir, "modified.md", "v2\n")
	writeFile(t, dir, "staged.md", "v2\n")
	mustRun(t, ctx, dir, "add", "staged.md")
	if err := os.Remove(filepath.Join(dir, "deleted.md")); err != nil {
		t.Fatal(err)
	}
	mustRun(t, ctx, dir, "mv", "renamed.md", "renamed-to.md")
	writeFile(t, dir, "untracked.md", "new\n")
	writeFile(t, dir, "ignored.md", "x\n")
	mustRun(t, ctx, dir, "update-index", "--assume-unchanged", "assumed.md")
	mustRun(t, ctx, dir, "update-index", "--skip-worktree", "skipped.md")
	wantStates(t, ctx, dir, map[string]PathState{
		"clean.md":              PathCommitted,
		"sub dir/with space.md": PathCommitted,
		"missing-never.md":      PathCommitted, // not in HEAD, not on disk: nothing to keep
		"modified.md":           PathUncommitted,
		"staged.md":             PathUncommitted,
		"deleted.md":            PathUncommitted,
		"renamed.md":            PathUncommitted,
		"renamed-to.md":         PathUncommitted,
		"untracked.md":          PathUncommitted,
		"ignored.md":            PathIgnored,
		"assumed.md":            PathUncommitted,
		"skipped.md":            PathUncommitted,
	})
}

// A CRLF checkout of a committed LF file is committed: git applies line
// endings itself. The CRLF file comes from git's own checkout, as on a
// Windows clone.
func TestPathStatesCRLFCheckout(t *testing.T) {
	ctx, dir := initRepo(t)
	writeFile(t, dir, ".gitattributes", "*.md text eol=crlf\n")
	writeFile(t, dir, "a.md", "one\ntwo\n")
	mustRun(t, ctx, dir, "add", "-A")
	mustRun(t, ctx, dir, "commit", "-m", "a")
	if err := os.Remove(filepath.Join(dir, "a.md")); err != nil {
		t.Fatal(err)
	}
	mustRun(t, ctx, dir, "checkout", "--", "a.md")
	if b, _ := os.ReadFile(filepath.Join(dir, "a.md")); string(b) != "one\r\ntwo\r\n" {
		t.Fatalf("checkout wrote %q", b)
	}
	wantStates(t, ctx, dir, map[string]PathState{"a.md": PathCommitted})
}

// A subdirectory as dir: paths are relative to it.
func TestPathStatesFromSubdirectory(t *testing.T) {
	ctx, dir := initRepo(t)
	writeFile(t, dir, "ws/a.md", "v1\n")
	writeFile(t, dir, "ws/b.md", "v1\n")
	mustRun(t, ctx, dir, "add", "-A")
	mustRun(t, ctx, dir, "commit", "-m", "a")
	writeFile(t, dir, "ws/b.md", "v2\n")
	wantStates(t, ctx, filepath.Join(dir, "ws"), map[string]PathState{"a.md": PathCommitted, "b.md": PathUncommitted})
}

// No commit yet, or no repository: nothing is committed.
func TestPathStatesUnbornAndNoRepo(t *testing.T) {
	if !Available() {
		t.Skip("git not available on PATH")
	}
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "a.md", "x\n")
	wantStates(t, ctx, dir, map[string]PathState{"a.md": PathUncommitted})
	mustRun(t, ctx, dir, "init", "--quiet")
	mustRun(t, ctx, dir, "add", "a.md")
	wantStates(t, ctx, dir, map[string]PathState{"a.md": PathUncommitted})
}

func TestBatches(t *testing.T) {
	for _, fold := range []bool{false, true} {
		saved := foldsCase
		foldsCase = fold
		testBatches(t)
		foldsCase = saved
	}
}

func testBatches(t *testing.T) {
	t.Helper()
	long := make([]string, 0, 3000)
	for i := 0; i < 3000; i++ {
		long = append(long, "a/very/long/path/to/a/skill/file/number/0000000000.md")
	}
	bs := batches(long)
	if len(bs) < 2 {
		t.Fatalf("expected several batches, got %d", len(bs))
	}
	n := 0
	for _, b := range bs {
		size := 0
		args := pathspecArgs(nil, b) // the paths as the command line carries them
		for _, a := range args[slices.Index(args, "--")+1:] {
			size += len(a) + 1
		}
		if size > argBudget {
			t.Errorf("batch of %d bytes exceeds the budget", size)
		}
		n += len(b)
	}
	if n != len(long) {
		t.Errorf("batches hold %d paths, want %d", n, len(long))
	}
}

// Where git folds case (core.ignorecase), a change git reports under the
// index's spelling still counts for the managed path's spelling. Folding is
// forced on, so this runs on every platform.
func TestPathStatesFoldsCase(t *testing.T) {
	ctx, dir := initRepo(t)
	saved := foldsCase
	foldsCase = true
	t.Cleanup(func() { foldsCase = saved })
	mustRun(t, ctx, dir, "config", "core.ignorecase", "true")
	writeFile(t, dir, "skill.md", "v1\n")
	writeFile(t, dir, "a[b].md", "x\n")
	writeFile(t, dir, "ab.md", "y\n")
	mustRun(t, ctx, dir, "add", "-A")
	mustRun(t, ctx, dir, "commit", "-m", "a")
	writeFile(t, dir, "skill.md", "v2\n")
	writeFile(t, dir, "ab.md", "y2\n")
	// a[b].md is literal (as a glob it would match ab.md): ab.md's change
	// doesn't count for it. Both names are valid on Windows.
	wantStates(t, ctx, dir, map[string]PathState{"SKILL.md": PathUncommitted, "a[b].md": PathCommitted})
}

// A workspace without its own repository, inside a repository that ignores a
// directory above it: git reports only that directory, which still covers
// the workspace's paths.
func TestPathStatesIgnoredDirectoryAbove(t *testing.T) {
	for name, c := range map[string]struct{ ignore, ws string }{
		"the workspace itself": {"ws/", "ws"},
		"a parent":             {"a/", "a/b"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, dir := initRepo(t)
			writeFile(t, dir, ".gitignore", c.ignore+"\n")
			mustRun(t, ctx, dir, "add", ".gitignore")
			mustRun(t, ctx, dir, "commit", "-m", "ignore")
			ws := filepath.Join(dir, filepath.FromSlash(c.ws))
			writeFile(t, ws, ".claude/skills/s/SKILL.md", "edited\n")
			wantStates(t, ctx, ws, map[string]PathState{".claude/skills/s/SKILL.md": PathIgnored})
		})
	}
}
