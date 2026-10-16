package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/gitx"
)

// stubVerifyFetch replaces the post-push verification fetch with one that
// fails with err on every call, and counts the calls.
func stubVerifyFetch(t *testing.T, err error) *int {
	t.Helper()
	calls := 0
	savedFetch, savedDelay := verifyFetch, verifyRetryDelay
	verifyFetch = func(context.Context, string, string, ...string) error {
		calls++
		return err
	}
	verifyRetryDelay = time.Millisecond
	t.Cleanup(func() { verifyFetch, verifyRetryDelay = savedFetch, savedDelay })
	return &calls
}

// A remote that drops after a successful push must not turn the land into a
// failure: the push landed, and only the read-back is missing.
func TestLandUnreachableVerificationReportsPushSucceeded(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "verify1", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	mustGit(t, ctx, wt, "commit", "--allow-empty", "-m", "feature work")
	want := headSHA(t, ctx, wt)
	calls := stubVerifyFetch(t, fmt.Errorf("git fetch origin main --quiet: exit status 128: ssh: connect to host example.com port 22: Operation timed out\n%w: check the network", gitx.ErrRemoteUnreachable))

	res, err := Land(ctx, landOpts(wt), &log)
	if err != nil {
		t.Fatalf("Land failed after a successful push: %v\nlog: %s", err, log.String())
	}
	if res.SHA != want || !res.Unverified {
		t.Errorf("result = %+v, want SHA %s and Unverified", res, want)
	}
	if *calls != verifyRetries+1 {
		t.Errorf("verification fetch ran %d times, want %d (1 + %d retries)", *calls, verifyRetries+1, verifyRetries)
	}
	out := log.String()
	for _, s := range []string{"push succeeded; could not verify", "git fetch origin main && git merge-base --is-ancestor HEAD origin/main"} {
		if !strings.Contains(out, s) {
			t.Errorf("log does not say %q:\n%s", s, out)
		}
	}
	// The push really landed: the bare origin has the commit.
	originMain, err := gitx.Run(ctx, root, "ls-remote", "origin", "refs/heads/main")
	if err != nil || !strings.HasPrefix(originMain, want) {
		t.Errorf("origin main = %q (%v), want %s", originMain, err, want)
	}
}

// A verification fetch that clears on a retry is a normal, verified land.
func TestLandVerificationRetrySucceeds(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "verify2", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	mustGit(t, ctx, wt, "commit", "--allow-empty", "-m", "feature work")
	calls := 0
	savedFetch, savedDelay := verifyFetch, verifyRetryDelay
	verifyFetch = func(ctx context.Context, dir, remote string, refs ...string) error {
		calls++
		if calls == 1 {
			return fmt.Errorf("fetch: %w", gitx.ErrRemoteUnreachable)
		}
		return gitx.Fetch(ctx, dir, remote, refs...)
	}
	verifyRetryDelay = time.Millisecond
	t.Cleanup(func() { verifyFetch, verifyRetryDelay = savedFetch, savedDelay })

	res, err := Land(ctx, landOpts(wt), &log)
	if err != nil {
		t.Fatalf("Land: %v\nlog: %s", err, log.String())
	}
	if res.Unverified || calls != 2 {
		t.Errorf("Unverified = %v after %d fetches, want a verified land after 2", res.Unverified, calls)
	}
}

// Any other verification failure is not retried and says the push succeeded,
// with the assertion exit code rather than 1.
func TestLandOtherVerificationFailureNamesPush(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "verify3", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	mustGit(t, ctx, wt, "commit", "--allow-empty", "-m", "feature work")
	calls := stubVerifyFetch(t, errors.New("git fetch origin main --quiet: exit status 128: fatal: couldn't find remote ref main"))

	_, err = Land(ctx, landOpts(wt), &log)
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.Code != cgerr.ExitAssertFail {
		t.Fatalf("err = %v, want ExitAssertFail", err)
	}
	if !strings.Contains(err.Error(), "push succeeded; could not verify") {
		t.Errorf("error does not say the push succeeded: %v", err)
	}
	if *calls != 1 {
		t.Errorf("unclassified failure retried: %d fetches", *calls)
	}
}

// Cancelling the land while the verification fetch waits to retry still says
// the push succeeded, with the assertion exit code, not a bare git error.
func TestLandCancelledDuringVerificationNamesPush(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "verify4", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	mustGit(t, ctx, wt, "commit", "--allow-empty", "-m", "feature work")
	landCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	savedFetch, savedDelay := verifyFetch, verifyRetryDelay
	verifyFetch = func(context.Context, string, string, ...string) error {
		cancel()
		return fmt.Errorf("fetch: %w", gitx.ErrRemoteUnreachable)
	}
	verifyRetryDelay = time.Hour // the cancellation, not the delay, ends the wait
	t.Cleanup(func() { verifyFetch, verifyRetryDelay = savedFetch, savedDelay })

	_, err = Land(landCtx, landOpts(wt), &log)
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.Code != cgerr.ExitAssertFail {
		t.Fatalf("err = %v, want ExitAssertFail", err)
	}
	if !strings.Contains(err.Error(), "push succeeded; could not verify") {
		t.Errorf("error does not say the push succeeded: %v", err)
	}
}

func TestRemoteFailureExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int // 0 = left uncoded
	}{
		{"unreachable", fmt.Errorf("git fetch: %w", gitx.ErrRemoteUnreachable), cgerr.ExitRemote},
		{"auth", fmt.Errorf("git fetch: %w", gitx.ErrRemoteAuth), cgerr.ExitRemote},
		{"unrelated", errors.New("git fetch: fatal: couldn't find remote ref main"), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := remoteFailure(tt.err)
			var coded *cgerr.CodedError
			isCoded := errors.As(got, &coded)
			switch {
			case tt.want == 0 && isCoded:
				t.Errorf("unclassified error got code %d", coded.Code)
			case tt.want != 0 && (!isCoded || coded.Code != tt.want):
				t.Errorf("remoteFailure() = %v, want code %d", got, tt.want)
			case !errors.Is(got, tt.err):
				t.Error("original error no longer wrapped")
			}
		})
	}
	if remoteFailure(nil) != nil {
		t.Error("remoteFailure(nil) != nil")
	}
}

// cg worktree create against an origin it cannot reach exits ExitRemote, not 1.
func TestCreateUnreachableOriginExitsRemote(t *testing.T) {
	// A proxy from the environment would answer instead of the dead port.
	for _, k := range []string{"http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY", "all_proxy", "ALL_PROXY"} {
		t.Setenv(k, "")
	}
	t.Setenv("no_proxy", "*")
	t.Setenv("NO_PROXY", "*")
	ctx, root := setupWorkspace(t)
	// A port nothing listens on: the connection is refused at once, offline too.
	mustGit(t, ctx, root, "remote", "set-url", "origin", "http://127.0.0.1:1/repo.git")
	var log bytes.Buffer
	_, err := Create(ctx, "unreach", defaultOpts(root), &log)
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.Code != cgerr.ExitRemote {
		t.Fatalf("err = %v, want ExitRemote", err)
	}
	if !errors.Is(err, gitx.ErrRemoteUnreachable) {
		t.Errorf("not classified unreachable: %v", err)
	}
}
