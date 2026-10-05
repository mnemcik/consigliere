package worktree

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// setupLocalWorkspace builds a main workspace root with a main branch and no
// remote, returning (ctx, root). Skips when git is unavailable.
func setupLocalWorkspace(t *testing.T) (ctx context.Context, root string) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git not available on PATH")
	}
	if runtime.GOOS == "windows" {
		t.Skip("skipping git-integration test on windows")
	}
	ctx = context.Background()
	base := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		base = resolved
	}
	root = filepath.Join(base, "ws")
	mustGit(t, ctx, "", "init", "--quiet", "--initial-branch=main", root)
	mustGit(t, ctx, root, "config", "user.email", "test@example.com")
	mustGit(t, ctx, root, "config", "user.name", "Test")
	mustGit(t, ctx, root, "config", "commit.gpgsign", "false")
	mustGit(t, ctx, root, "commit", "--allow-empty", "-m", "init")
	return ctx, root
}

func localOpts(root string) Options {
	o := defaultOpts(root)
	o.Local = true
	return o
}

func localLandOpts(wt string) *LandOptions {
	o := landOpts(wt)
	o.Strategy = workspace.StrategyLocal
	return o
}

func wantExit(t *testing.T, err error, code int, log *bytes.Buffer) {
	t.Helper()
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != code {
		t.Fatalf("expected exit %d, got %v\nlog: %s", code, err, log.String())
	}
}

func TestEffectiveStrategy(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	if got := EffectiveStrategy(ctx, root, ""); got != workspace.StrategyLocal {
		t.Errorf("unset, no origin: got %q, want local", got)
	}
	if got := EffectiveStrategy(ctx, root, workspace.StrategyPR); got != workspace.StrategyPR {
		t.Errorf("explicit value must win: got %q", got)
	}
	ctx, root = setupWorkspace(t)
	if got := EffectiveStrategy(ctx, root, ""); got != workspace.DefaultLandingStrategy {
		t.Errorf("unset, with origin: got %q, want %q", got, workspace.DefaultLandingStrategy)
	}
}

// A full create → land → remove cycle with no remote: the main checkout ends
// up on the landed commit with its files, and remove sees the branch as landed.
func TestLocalCycle(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "loc1", localOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v\nlog: %s", err, log.String())
	}
	commitFile(t, ctx, wt, "landed.md", "new\n", "feature work")
	want := headSHA(t, ctx, wt)

	entries, err := List(ctx, localOpts(root))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Slug == "loc1" && e.Ahead != 1 {
			t.Errorf("list: Ahead = %d, want 1", e.Ahead)
		}
	}

	res, err := Land(ctx, localLandOpts(wt), &log)
	if err != nil {
		t.Fatalf("Land: %v\nlog: %s", err, log.String())
	}
	if res.SHA != want || res.Strategy != workspace.StrategyLocal {
		t.Errorf("result = %+v, want SHA %s, strategy local", res, want)
	}
	if got := headSHA(t, ctx, root); got != want {
		t.Errorf("main checkout HEAD = %s, want %s", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, "landed.md")); err != nil {
		t.Errorf("landed file not in the main checkout: %v", err)
	}

	if err := Remove(ctx, "loc1", localOpts(root), &log); err != nil {
		t.Fatalf("Remove after land: %v\nlog: %s", err, log.String())
	}
}

// Unlanded work blocks remove in local mode too.
func TestLocalRemoveRefusesUnlanded(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "loc2", localOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, ctx, wt, "commit", "--allow-empty", "-m", "not landed")
	err = Remove(ctx, "loc2", localOpts(root), &log)
	wantExit(t, err, cgerr.ExitDirty, &log)
}

// Another session landed first: the land rebases onto main and still lands.
func TestLocalLandRebasesOntoAdvancedMain(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "loc3", localOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, ctx, wt, "feature.txt", "feature\n", "feature work")
	commitFile(t, ctx, root, "other.txt", "other\n", "landed by another session")
	other := headSHA(t, ctx, root)

	res, err := Land(ctx, localLandOpts(wt), &log)
	if err != nil {
		t.Fatalf("Land: %v\nlog: %s", err, log.String())
	}
	if !gitx.IsAncestor(ctx, root, other, res.SHA) {
		t.Errorf("landed commit does not contain the other session's work")
	}
	if got := headSHA(t, ctx, root); got != res.SHA {
		t.Errorf("main checkout HEAD = %s, want %s", got, res.SHA)
	}
}

func TestLocalLandConflictExits3(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "loc4", localOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, ctx, wt, "same.txt", "session\n", "session edit")
	commitFile(t, ctx, root, "same.txt", "main\n", "main edit")
	_, err = Land(ctx, localLandOpts(wt), &log)
	wantExit(t, err, cgerr.ExitConflict, &log)
	mustGit(t, ctx, wt, "rebase", "--abort")
}

// An uncommitted edit in the main checkout to a file the land touches blocks
// the fast-forward: the land fails with exit 6 and the edit survives.
func TestLocalLandBlockedByDirtyCheckout(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	commitFile(t, ctx, root, "shared.md", "base\n", "shared")
	var log bytes.Buffer
	wt, err := Create(ctx, "loc5", localOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, ctx, wt, "shared.md", "from the session\n", "session edit")
	before := headSHA(t, ctx, root)
	if err := os.WriteFile(filepath.Join(root, "shared.md"), []byte("uncommitted in main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = Land(ctx, localLandOpts(wt), &log)
	wantExit(t, err, cgerr.ExitLandingBlocked, &log)
	if !strings.Contains(err.Error(), "shared.md") {
		t.Errorf("error does not name the file in the way: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "shared.md")); string(got) != "uncommitted in main\n" {
		t.Errorf("uncommitted edit overwritten: %q", got)
	}
	if got := headSHA(t, ctx, root); got != before {
		t.Errorf("main moved despite the blocked land")
	}
}

// An uncommitted edit the land does not touch does not block it, as with a
// `git pull`; the edit is kept.
func TestLocalLandKeepsUnrelatedDirtyFile(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "loc6", localOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, ctx, wt, "landed.md", "new\n", "feature work")
	if err := os.WriteFile(filepath.Join(root, "wip.md"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Land(ctx, localLandOpts(wt), &log)
	if err != nil {
		t.Fatalf("Land: %v\nlog: %s", err, log.String())
	}
	if got := headSHA(t, ctx, root); got != res.SHA {
		t.Errorf("main checkout not fast-forwarded")
	}
	if got, _ := os.ReadFile(filepath.Join(root, "wip.md")); string(got) != "wip\n" {
		t.Errorf("unrelated edit lost: %q", got)
	}
}

// With main checked out nowhere, the branch ref moves.
func TestLocalLandMovesRefWhenNotCheckedOut(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	mustGit(t, ctx, root, "checkout", "--quiet", "--detach")
	var log bytes.Buffer
	wt, err := Create(ctx, "loc7", localOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, ctx, wt, "commit", "--allow-empty", "-m", "feature work")
	want := headSHA(t, ctx, wt)
	if _, err := Land(ctx, localLandOpts(wt), &log); err != nil {
		t.Fatalf("Land: %v\nlog: %s", err, log.String())
	}
	got, err := gitx.RevParse(ctx, root, "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("main = %s, want %s", got, want)
	}
}

// A worktree mid-rebase of main: the land refuses (exit 6) and main stays put.
func TestLocalLandRefusesDuringRebaseOfMain(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	mustGit(t, ctx, root, "commit", "--allow-empty", "-m", "second")
	var log bytes.Buffer
	wt, err := Create(ctx, "loc8", localOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, ctx, wt, "commit", "--allow-empty", "-m", "feature work")
	before := headSHA(t, ctx, root)
	if _, err := gitx.RunEnv(ctx, root, []string{"GIT_SEQUENCE_EDITOR=true"}, "rebase", "-i", "--exec", "false", "HEAD~1"); err == nil {
		t.Fatal("expected the rebase to stop")
	}
	_, err = Land(ctx, localLandOpts(wt), &log)
	wantExit(t, err, cgerr.ExitLandingBlocked, &log)
	mustGit(t, ctx, root, "rebase", "--abort")
	got, err := gitx.RevParse(ctx, root, "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if got != before {
		t.Errorf("main moved during a rebase")
	}
}

// Two sessions landing one after the other both end up on main.
func TestLocalLandTwoSessions(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	var log bytes.Buffer
	a, err := Create(ctx, "loc9a", localOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Create(ctx, "loc9b", localOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, ctx, a, "a.txt", "a\n", "a")
	commitFile(t, ctx, b, "b.txt", "b\n", "b")
	ra, err := Land(ctx, localLandOpts(a), &log)
	if err != nil {
		t.Fatalf("Land a: %v\nlog: %s", err, log.String())
	}
	rb, err := Land(ctx, localLandOpts(b), &log)
	if err != nil {
		t.Fatalf("Land b: %v\nlog: %s", err, log.String())
	}
	if !gitx.IsAncestor(ctx, root, ra.SHA, rb.SHA) || headSHA(t, ctx, root) != rb.SHA {
		t.Errorf("main does not hold both landings\nlog: %s", log.String())
	}
}

// An explicit remote strategy on a repository with no origin names the fix.
func TestRemoteStrategyWithoutOriginIsUsageError(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	var log bytes.Buffer
	_, err := Create(ctx, "loc10", defaultOpts(root), &log)
	wantExit(t, err, cgerr.ExitUsage, &log)
	if !bytes.Contains([]byte(err.Error()), []byte(`"local"`)) {
		t.Errorf("error does not name the local strategy: %v", err)
	}
}

// Sessions landing at the same moment: the land lock runs them one at a time,
// each rebasing onto the landings before it, so every landing ends up on main
// and the main checkout is left clean.
func TestLocalLandConcurrent(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	saved := landRetryDelay
	landRetryDelay = 10 * time.Millisecond
	t.Cleanup(func() { landRetryDelay = saved })

	const n = 4
	trees := make([]string, n)
	for i := range trees {
		var log bytes.Buffer
		wt, err := Create(ctx, fmt.Sprintf("conc%d", i), localOpts(root), &log)
		if err != nil {
			t.Fatal(err)
		}
		commitFile(t, ctx, wt, fmt.Sprintf("f%d.txt", i), "x\n", fmt.Sprintf("work %d", i))
		trees[i] = wt
	}

	var wg sync.WaitGroup
	results := make([]LandResult, n)
	errs := make([]error, n)
	logs := make([]bytes.Buffer, n)
	for i := range trees {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			o := localLandOpts(trees[i])
			o.MaxRetries = 2 * n
			results[i], errs[i] = Land(ctx, o, &logs[i])
		}(i)
	}
	wg.Wait()

	main := headSHA(t, ctx, root)
	for i := range trees {
		if errs[i] != nil {
			t.Fatalf("land %d: %v\nlog: %s", i, errs[i], logs[i].String())
		}
		if !gitx.IsAncestor(ctx, root, results[i].SHA, main) {
			t.Errorf("landing %d (%s) is not on main", i, results[i].SHA)
		}
	}
	if st, _ := gitx.Run(ctx, root, "status", "--porcelain"); st != "" {
		t.Errorf("main checkout left dirty after concurrent lands:\n%s", st)
	}
}

func landLockPath(t *testing.T, ctx context.Context, root string) string {
	t.Helper()
	common, err := gitx.Run(ctx, root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(common, landLockName)
}

// A land lock held by another process: the land waits, gives up with exit 4
// naming the lock file, and succeeds once that process is killed — the OS
// drops the lock with the process, so a crashed land never leaves one behind.
func TestLandLockHeldByKilledProcess(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	saved := landLockWait
	landLockWait = 300 * time.Millisecond
	t.Cleanup(func() { landLockWait = saved })

	holder := exec.Command(os.Args[0], "-test.run=^TestHelperHoldLandLock$") //nolint:gosec // the test binary itself
	holder.Env = append(os.Environ(), "CG_TEST_HOLD_LAND_LOCK="+root)
	out, err := holder.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Process.Kill(); _ = holder.Wait() })
	if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || !strings.Contains(line, "held") {
		t.Fatalf("helper did not take the lock: %q, %v", line, err)
	}

	var log bytes.Buffer
	wt, err := Create(ctx, "lock1", localOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, ctx, wt, "commit", "--allow-empty", "-m", "work")
	_, err = Land(ctx, localLandOpts(wt), &log)
	wantExit(t, err, cgerr.ExitPushFail, &log)
	if !strings.Contains(err.Error(), landLockName) {
		t.Errorf("error does not name the lock file: %v", err)
	}

	_ = holder.Process.Kill()
	_ = holder.Wait()
	if _, err := Land(ctx, localLandOpts(wt), &log); err != nil {
		t.Fatalf("lock not released by the killed process: %v\nlog: %s", err, log.String())
	}
}

// TestHelperHoldLandLock is not a test: TestLandLockHeldByKilledProcess runs
// it in a child process, where it takes the land lock and holds it until
// killed.
func TestHelperHoldLandLock(t *testing.T) {
	root := os.Getenv("CG_TEST_HOLD_LAND_LOCK")
	if root == "" {
		t.Skip("helper process only")
	}
	if _, err := acquireLandLock(context.Background(), root, func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	fmt.Println("held")
	time.Sleep(time.Minute)
}

// A git index.lock left in the main checkout by a crashed git process: the
// land retries, then fails with exit 4 naming the file, and main stays put.
func TestLocalLandStaleIndexLockNamed(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	saved := landRetryDelay
	landRetryDelay = time.Millisecond
	t.Cleanup(func() { landRetryDelay = saved })
	var log bytes.Buffer
	wt, err := Create(ctx, "lock2", localOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, ctx, wt, "landed.md", "new\n", "feature work")
	before := headSHA(t, ctx, root)
	if err := os.WriteFile(filepath.Join(root, ".git", "index.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Land(ctx, localLandOpts(wt), &log)
	wantExit(t, err, cgerr.ExitPushFail, &log)
	if !strings.Contains(err.Error(), "index.lock") {
		t.Errorf("error does not name index.lock: %v", err)
	}
	if got := headSHA(t, ctx, root); got != before {
		t.Errorf("main moved despite the failed land")
	}
}
