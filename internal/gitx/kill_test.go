package gitx

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary double as a process git spawns: with
// GITX_TEST_HANG set it hangs instead of running the tests.
func TestMain(m *testing.M) {
	if os.Getenv("GITX_TEST_HANG") != "" {
		time.Sleep(20 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// A cancelled git returns promptly even when a process git started still
// holds its stderr: a transport such as ssh, stuck connecting, outlives the
// git that is killed, and Run must not wait for it.
func TestRunReturnsWhenContextEndsDespiteLingeringChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ext:: command quoting of the test binary path is POSIX-specific")
	}
	if !Available() {
		t.Skip("git not available on PATH")
	}
	exe, err := os.Executable()
	if err != nil || strings.ContainsAny(exe, " %") {
		t.Skipf("test binary path unusable in an ext:: url: %q (%v)", exe, err)
	}
	t.Setenv("GITX_TEST_HANG", "1")
	saved := waitDelay
	waitDelay = 100 * time.Millisecond
	t.Cleanup(func() { waitDelay = saved })

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = Run(ctx, t.TempDir(), "-c", "protocol.ext.allow=always", "ls-remote", "ext::"+exe)
	if err == nil {
		t.Fatal("ls-remote against a hanging transport succeeded")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Run returned %s after its context ended; want promptly", d)
	}
}

// A git that exits 0 while a process it started still holds its stderr (a hook
// that backgrounds a task without redirecting it) succeeded: the WaitDelay
// that cuts the wait short must not turn that into a failure.
func TestRunSucceedsWhenHookChildOutlivesGit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hook fixture is a POSIX shell script that backgrounds a job")
	}
	ctx, dir := initRepo(t)
	hook := filepath.Join(dir, ".git", "hooks", "post-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nsleep 2 &\n"), 0o755); err != nil { //nolint:gosec // an executable test hook
		t.Fatal(err)
	}
	saved := waitDelay
	waitDelay = 100 * time.Millisecond
	t.Cleanup(func() { waitDelay = saved })

	if _, err := Run(ctx, dir, "commit", "--allow-empty", "-m", "via Run"); err != nil {
		t.Errorf("Run: git exited 0, but Run failed: %v", err)
	}
	if _, err := RunRaw(ctx, dir, "commit", "--allow-empty", "-m", "via RunRaw"); err != nil {
		t.Errorf("RunRaw: git exited 0, but RunRaw failed: %v", err)
	}
	if got := mustRun(t, ctx, dir, "log", "-1", "--format=%s"); got != "via RunRaw" {
		t.Errorf("last commit = %q, want the RunRaw commit", got)
	}
}
