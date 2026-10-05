package worktree

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/gitx"
)

// Local lands are serialised by a lock file in the shared git dir. Without
// it, two lands can each run `merge --ff-only` in the main checkout at once:
// the loser writes its files into the working tree before git's ref update
// fails, leaving the checkout with files its HEAD does not have. A retry
// cannot repair that, so the lands must not overlap.
const landLockName = "cg-land.lock"

// Variables so tests can shorten them.
var (
	landLockWait  = 60 * time.Second // give up waiting for another land
	landLockStale = 10 * time.Minute // a lock older than this is abandoned
	landLockPoll  = 50 * time.Millisecond
)

// acquireLandLock takes the land lock for the repository containing dir and
// returns a function that releases it. It waits while another land holds the
// lock, takes over a lock older than landLockStale (a crashed land), and fails
// with ExitPushFail once landLockWait has passed.
func acquireLandLock(ctx context.Context, dir string, logf func(string, ...any)) (func(), error) {
	common, err := gitx.Run(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(common, landLockName)
	deadline := time.Now().Add(landLockWait)
	waiting := false
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644) //nolint:gosec // fixed name in the git dir
		if err == nil {
			_, _ = fmt.Fprintf(f, "pid %d at %s\n", os.Getpid(), time.Now().Format(time.RFC3339))
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		if info, serr := os.Stat(path); serr == nil && time.Since(info.ModTime()) > landLockStale {
			logf("removing stale land lock %s (older than %s)\n", path, landLockStale)
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, cgerr.New(cgerr.ExitPushFail,
				"another land has held %s for over %s — if no land is running, delete the file and re-run", path, landLockWait)
		}
		if !waiting {
			logf("waiting for another session's land to finish\n")
			waiting = true
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(landLockPoll):
		}
	}
}
