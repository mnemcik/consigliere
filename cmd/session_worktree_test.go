package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/session"
)

// runCg runs `cg <args…>` in dir and returns its output.
func runCg(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	worktreeCreateForce, worktreeRemoveForce = false, false
	for _, c := range []*cobra.Command{sessionSetContextCmd, worktreeCreateCmd, worktreeRemoveCmd} {
		c.SilenceUsage = false
		c.Flags().VisitAll(func(f *pflag.Flag) {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
	}
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return buf.String(), err
}

// recordedWorktree returns the worktree the session's state file records under
// the workspace's main checkout.
func recordedWorktree(t *testing.T, ws, id string) string {
	t.Helper()
	c, err := session.ReadContext(ws, id)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		return ""
	}
	return c.Worktree
}

// gitWorkspace is a committed workspace with no remote (local landing), as cwd.
func gitWorkspace(t *testing.T) string {
	t.Helper()
	ws := newWorkspace(t)
	gitCommitAll(t, ws)
	resolved, err := filepath.EvalSymlinks(ws)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// S35, S37: worktree create records the worktree in the session's state file;
// remove of that worktree clears it.
func TestWorktreeCreateRecordsAndRemoveClears(t *testing.T) {
	requireGit(t)
	for _, idVar := range []string{"CG_SESSION_ID", "CLAUDE_CODE_SESSION_ID"} {
		t.Run(idVar, func(t *testing.T) {
			ws := gitWorkspace(t)
			if err := session.WriteContext(ws, "s1", session.Claim{Area: "a", Project: "p"}); err != nil {
				t.Fatal(err)
			}
			t.Setenv(idVar, "s1")
			out, err := runCg(t, ws, "worktree", "create", "one")
			if err != nil {
				t.Fatalf("create: %v\n%s", err, out)
			}
			path := strings.TrimSpace(lastLine(out))
			got := recordedWorktree(t, ws, "s1")
			if got == "" || !filepath.IsAbs(got) || !samePath(got, path) {
				t.Fatalf("recorded %q, want the absolute path of %q", got, path)
			}
			if c, _ := session.ReadContext(ws, "s1"); c.Area != "a" || c.Project != "p" {
				t.Errorf("other fields lost: %+v", c)
			}
			// Removing another worktree keeps the record.
			if out, err := runCg(t, ws, "worktree", "create", "two"); err != nil {
				t.Fatalf("create two: %v\n%s", err, out)
			}
			if got := recordedWorktree(t, ws, "s1"); !strings.HasSuffix(filepath.ToSlash(got), "--two") {
				t.Fatalf("after create two, recorded %q", got)
			}
			if out, err := runCg(t, ws, "worktree", "remove", "one"); err != nil {
				t.Fatalf("remove one: %v\n%s", err, out)
			}
			if got := recordedWorktree(t, ws, "s1"); !strings.HasSuffix(filepath.ToSlash(got), "--two") {
				t.Errorf("removing another worktree changed the record to %q", got)
			}
			// Another session recorded the same worktree.
			if err := session.SetWorktree(ws, "other", recordedWorktree(t, ws, "s1")); err != nil {
				t.Fatal(err)
			}
			if out, err := runCg(t, ws, "worktree", "remove", "two"); err != nil {
				t.Fatalf("remove two: %v\n%s", err, out)
			}
			if got := recordedWorktree(t, ws, "s1"); got != "" {
				t.Errorf("record %q left after removing it", got)
			}
			if got := recordedWorktree(t, ws, "other"); got != "" {
				t.Errorf("another session's record %q left after removing it", got)
			}
		})
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

// S35, S39: without a session ID, or with an invalid one, create records
// nothing and still succeeds.
func TestWorktreeCreateWithoutValidSessionID(t *testing.T) {
	requireGit(t)
	for _, id := range []string{"", "../../x"} {
		ws := gitWorkspace(t)
		if id != "" {
			t.Setenv("CG_SESSION_ID", id)
		}
		if out, err := runCg(t, ws, "worktree", "create", "x"); err != nil {
			t.Fatalf("create with id %q: %v\n%s", id, err, out)
		}
		if _, err := os.Stat(session.ContextDir(ws)); !os.IsNotExist(err) {
			t.Errorf("id %q: a session-context directory was created", id)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(ws), "x.json")); !os.IsNotExist(err) {
			t.Errorf("id %q: a file was written outside", id)
		}
	}
}

// S36: set-context --worktree records a listed worktree, alone or with a
// claim, and refuses a path the repo doesn't list.
func TestSetContextWorktree(t *testing.T) {
	requireGit(t)
	ws := gitWorkspace(t)
	wt := addBranchWorktree(t, ws, "own")
	if out, err := runCg(t, ws, "session", "set-context", "--session-id", "s1", "--worktree", wt); err != nil {
		t.Fatalf("set-context --worktree: %v\n%s", err, out)
	}
	if got := recordedWorktree(t, ws, "s1"); !samePath(got, wt) || !filepath.IsAbs(got) {
		t.Errorf("recorded %q, want %s", got, wt)
	}
	// From inside the worktree, with a relative path and a claim.
	if out, err := runCg(t, wt, "session", "set-context", "--session-id", "s2", "--area", "a", "--project", "p", "--worktree", "."); err != nil {
		t.Fatalf("set-context with claim: %v\n%s", err, out)
	}
	c, _ := session.ReadContext(ws, "s2")
	if c == nil || c.Area != "a" || !samePath(c.Worktree, wt) {
		t.Errorf("s2 = %+v", c)
	}
	for _, bad := range []string{t.TempDir(), filepath.Join(ws, "sub-not-a-worktree")} {
		if err := os.MkdirAll(bad, 0o755); err != nil {
			t.Fatal(err)
		}
		out, err := runCg(t, ws, "session", "set-context", "--session-id", "s3", "--worktree", bad)
		if err == nil || !strings.Contains(err.Error(), "worktree") {
			t.Errorf("unlisted %s accepted: %v\n%s", bad, err, out)
		}
		if got := recordedWorktree(t, ws, "s3"); got != "" {
			t.Errorf("unlisted path recorded: %q", got)
		}
	}
	// An empty value (an unset shell variable) is refused, not taken as the
	// current directory.
	if out, err := runCg(t, ws, "session", "set-context", "--session-id", "s5", "--worktree", ""); err == nil {
		t.Errorf("--worktree \"\" accepted:\n%s", out)
	}
	if got := recordedWorktree(t, ws, "s5"); got != "" {
		t.Errorf("empty --worktree recorded %q", got)
	}
	// Neither a claim nor a worktree is an error.
	if _, err := runCg(t, ws, "session", "set-context", "--session-id", "s4"); err == nil {
		t.Error("set-context with nothing to set succeeded")
	}
}

// sessionWorktreeOf dispatches `cg shop` and returns CG_SESSION_WORKTREE as
// the binary would see it ("" when unset).
func sessionWorktreeOf(t *testing.T) string {
	t.Helper()
	run := mustDispatch(t, "shop")
	v, _ := envOf(run.cmd().Env, envSessionWorktree)
	return v
}

// S38: served by the session root for a directory outside it and its
// worktrees, with a valid session ID and a recorded, listed worktree, the
// binary gets CG_SESSION_WORKTREE.
func TestDispatchSessionWorktree(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	ws := sessionWorkspace(t, cfgHome, []string{"alpha", "shop"})
	wt := addBranchWorktree(t, ws, "rec")
	if err := session.SetWorktree(ws, "s1", wt); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envWorkspace, ws)
	t.Setenv("CG_SESSION_ID", "s1")
	external := t.TempDir()
	t.Chdir(external)
	if got := sessionWorktreeOf(t); !samePath(got, wt) {
		t.Errorf("CG_SESSION_WORKTREE = %q, want %s", got, wt)
	}
	// CLAUDE_CODE_SESSION_ID works too.
	unsetEnv(t, "CG_SESSION_ID")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "s1")
	if got := sessionWorktreeOf(t); !samePath(got, wt) {
		t.Errorf("with CLAUDE_CODE_SESSION_ID: %q", got)
	}
	// Inside the session root, it isn't set.
	t.Chdir(ws)
	if got := sessionWorktreeOf(t); got != "" {
		t.Errorf("inside the session root: %q", got)
	}
	// From another workspace, served by the session root: set.
	t.Chdir(otherWorkspace(t, cfgHome))
	if got := sessionWorktreeOf(t); !samePath(got, wt) {
		t.Errorf("from another workspace: %q", got)
	}
	// S30: dispatch doesn't read an inherited value.
	t.Setenv(envSessionWorktree, external)
	if got := sessionWorktreeOf(t); !samePath(got, wt) {
		t.Errorf("with a stale CG_SESSION_WORKTREE: %q", got)
	}
}

// S38b: without a valid record, a session root that is itself a linked
// worktree is the session's worktree; any other session root gets none.
func TestDispatchSessionWorktreeFallback(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	main := sessionWorkspace(t, cfgHome, []string{"alpha", "shop"})
	linked := addBranchWorktree(t, main, "linked")
	external := t.TempDir()

	cases := []struct {
		name    string
		session string
		setup   func(t *testing.T)
		want    string // "" = unset
	}{
		{"main checkout, no record", main, func(t *testing.T) { t.Setenv("CG_SESSION_ID", "s1") }, ""},
		{"linked worktree, no record", linked, func(t *testing.T) { t.Setenv("CG_SESSION_ID", "s1") }, linked},
		{"linked worktree, no session ID", linked, func(t *testing.T) {}, linked},
		{"linked worktree, invalid session ID", linked, func(t *testing.T) { t.Setenv("CG_SESSION_ID", "../../x") }, linked},
		{"linked worktree, stale record", linked, func(t *testing.T) {
			gone := addBranchWorktree(t, main, "gone")
			if err := session.SetWorktree(main, "s2", gone); err != nil {
				t.Fatal(err)
			}
			if _, err := gitx.Run(context.Background(), main, "worktree", "remove", "--force", gone); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CG_SESSION_ID", "s2")
		}, linked},
		{"linked worktree, unreadable state file", linked, func(t *testing.T) {
			if err := os.MkdirAll(session.ContextDir(main), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(session.ContextFile(main, "s3"), []byte("{"), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CG_SESSION_ID", "s3")
		}, linked},
		{"main checkout, stale record", main, func(t *testing.T) {
			gone := addBranchWorktree(t, main, "gone2")
			if err := session.SetWorktree(main, "s4", gone); err != nil {
				t.Fatal(err)
			}
			if _, err := gitx.Run(context.Background(), main, "worktree", "remove", "--force", gone); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CG_SESSION_ID", "s4")
		}, ""},
		{"linked worktree, git missing", linked, func(t *testing.T) { t.Setenv("PATH", t.TempDir()) }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			unsetEnv(t, "CG_SESSION_ID")
			t.Setenv(envWorkspace, tc.session)
			tc.setup(t)
			t.Chdir(external)
			got := sessionWorktreeOf(t)
			if tc.want == "" && got != "" {
				t.Errorf("CG_SESSION_WORKTREE = %q, want unset", got)
			}
			if tc.want != "" && !samePath(got, tc.want) {
				t.Errorf("CG_SESSION_WORKTREE = %q, want %s", got, tc.want)
			}
		})
	}
}

// S38b: a session root that is a subdirectory of its repo isn't a linked
// worktree's root; a symlinked spelling of a linked worktree still is.
func TestDispatchSessionWorktreeFallbackRoots(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	main := sessionWorkspace(t, cfgHome)
	linked := addBranchWorktree(t, main, "l")
	// A workspace in a subdirectory of the linked worktree, declaring shop.
	inner := filepath.Join(linked, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	// install writes to the git top level, so install elsewhere and copy the
	// result into the nested workspace.
	src := otherWorkspace(t, cfgHome, []string{"alpha", "shop"})
	for _, f := range []string{".cg.json", filepath.Join(".cg", "ext", "alpha.json")} {
		data, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(inner, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(inner, f), data, 0o644); err != nil { //nolint:gosec // a test fixture in its own temp dir
			t.Fatal(err)
		}
	}
	t.Setenv(envWorkspace, inner)
	t.Chdir(t.TempDir())
	if got := sessionWorktreeOf(t); got != "" {
		t.Errorf("subdirectory session root: %q, want unset", got)
	}

	if isWindowsOS {
		t.Skip("symlinks need privileges on Windows")
	}
	linked2 := addBranchWorktree(t, main, "l2")
	installRepo(t, cfgHome, linked2, commandsManifest("alpha", "1.0.0", "shop"))
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(linked2, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envWorkspace, link)
	t.Chdir(t.TempDir()) // installRepo left us inside linked2
	if got := sessionWorktreeOf(t); !samePath(got, linked2) {
		t.Errorf("symlinked linked worktree: %q, want %s", got, linked2)
	}
}
