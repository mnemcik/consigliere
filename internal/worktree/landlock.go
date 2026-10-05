package worktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/gitx"
)

// Local lands are serialised by an OS file lock on a file in the shared git
// dir. Without it, two lands can each run `merge --ff-only` in the main
// checkout at once: the loser writes its files into the working tree before
// git's ref update fails, leaving the checkout with files its HEAD does not
// have. A retry cannot repair that, so the lands must not overlap.
//
// The lock is flock(2) on Unix and LockFileEx on Windows, so the OS drops it
// when the holding process exits, however it exits. There is no stale lock to
// detect or take over. The file itself stays; only the lock on it matters.
const landLockName = "cg-land.lock"

// Variables so tests can shorten them.
var (
	landLockWait = 60 * time.Second // give up waiting for another land
	landLockPoll = 50 * time.Millisecond
)

// acquireLandLock takes the land lock for the repository containing dir and
// returns a function that releases it. It waits while another land holds the
// lock and fails with ExitPushFail once landLockWait has passed.
func acquireLandLock(ctx context.Context, dir string, logf func(string, ...any)) (func(), error) {
	common, err := gitx.Run(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(common, landLockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644) //nolint:gosec // fixed name in the git dir
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(landLockWait)
	waiting := false
	for {
		got, err := tryLockFile(f)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("locking %s: %w", path, err)
		}
		if got {
			return func() {
				_ = unlockFile(f)
				_ = f.Close()
			}, nil
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, cgerr.New(cgerr.ExitPushFail,
				"another land has held %s for over %s", path, landLockWait)
		}
		if !waiting {
			logf("waiting for another session's land to finish\n")
			waiting = true
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(landLockPoll):
		}
	}
}
