//go:build !unix && !windows

package session

import "os"

// tryLockFile has no advisory lock on this platform; callers proceed unlocked.
func tryLockFile(string) (*os.File, bool, error) { return nil, false, nil }

func unlockFile(*os.File) {}
