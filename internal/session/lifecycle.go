package session

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mnemcik/consigliere/internal/gitx"
)

// ResumeFile returns the path of a project's pause cursor (written by the
// wrap skill's pause mode) under the workspace root.
func ResumeFile(wsRoot, project string) string {
	return filepath.Join(wsRoot, "projects", project, "resume.md")
}

// resumeLookupTimeout bounds the git calls that list worktrees. Paused state is
// read from the SessionEnd hook, whose budget is short; a slow or broken git
// falls back to the caller's own checkout.
const resumeLookupTimeout = time.Second

// ResumeDirs returns the workspace roots a project's resume.md is looked up
// under: wsRoot first, then the same workspace in every other worktree of its
// repository. The pause-mode wrap writes resume.md in the session worktree and
// commits it to the unlanded session branch, so a session started in the main
// checkout (or in another worktree) only finds it there. A workspace nested
// below the git top level is mapped to the same subdirectory of each worktree.
// Outside git, or when git fails, only wsRoot is returned.
func ResumeDirs(wsRoot string) []string {
	if wsRoot == "" {
		return nil
	}
	dirs := []string{wsRoot}
	seen := map[string]bool{canonical(wsRoot): true}

	ctx, cancel := context.WithTimeout(context.Background(), resumeLookupTimeout)
	defer cancel()
	top := gitx.ShowToplevel(ctx, wsRoot)
	if top == "" {
		return dirs
	}
	rel, err := filepath.Rel(canonical(top), canonical(wsRoot))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return dirs
	}
	paths, err := gitx.WorktreePaths(ctx, wsRoot)
	if err != nil {
		return dirs
	}
	for _, p := range paths {
		d := filepath.Join(p, rel)
		if c := canonical(d); !seen[c] {
			seen[c] = true
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// canonical resolves symlinks so the same directory reached through two paths
// (on macOS /tmp and /private/tmp) compares equal. A path that cannot be
// resolved is only cleaned.
func canonical(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// ResumeFiles returns every existing resume.md for project, looked up in each
// of ResumeDirs(wsRoot), the caller's own checkout first. A missing file is
// skipped; any other error is returned.
func ResumeFiles(wsRoot, project string) ([]string, error) {
	if wsRoot == "" || project == "" {
		return nil, nil
	}
	var found []string
	for _, d := range ResumeDirs(wsRoot) {
		f := ResumeFile(d, project)
		if _, err := os.Stat(f); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return found, err
		}
		found = append(found, f)
	}
	return found, nil
}

// IsPaused reports whether a project is paused: its resume.md exists in any
// worktree of the workspace (the wrap skill's pause mode writes it, and the
// resuming session deletes it once it has read it). A missing file means not
// paused; any other error is returned.
func IsPaused(wsRoot, project string) (bool, error) {
	found, err := ResumeFiles(wsRoot, project)
	return len(found) > 0, err
}

// MarkPaused records that a session paused its project: it sets the pause
// marker ("paused": true and "pausedAt") in the session's badge. The marker is
// what identifies the pausing session, so another session that worked on the
// same project, or one resuming it, is never mistaken for it. It reports
// whether a badge was marked; a session without a badge file is not an error.
func MarkPaused(root, sessionID string, now time.Time) (bool, error) {
	if !ValidSessionID(sessionID) {
		return false, fmt.Errorf("invalid session id %q", sessionID)
	}
	marked := false
	err := withBadgeLock(root, sessionID, func() error {
		path := ContextFile(root, sessionID)
		m, err := readContextMap(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		m[keyPaused] = true
		m[keyPausedAt] = now.UTC().Format(time.RFC3339)
		marked = true
		return writeJSONAtomic(path, m)
	})
	return marked && err == nil, err
}

// Release ends a session's claim by deleting its badge file, so cg active no
// longer lists it and a later claim starts from a clean file. Releasing a
// session that has no badge file is not an error.
func Release(root, sessionID string) error {
	if !ValidSessionID(sessionID) {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	return withBadgeLock(root, sessionID, func() error { return removeBadge(root, sessionID) })
}

// HandOver retires the pausing session's claim on project when the caller
// resumes it: every other badge for the project that carries the pause marker.
// Other badges for the project, such as a session that already resumed it or a
// parallel session that never paused, are kept. It returns the retired session IDs,
// sorted. It does nothing when the project is not paused, so two sessions that
// claim the same fresh project both stay listed.
func HandOver(root, wsRoot, callerID, project string) ([]string, error) {
	paused, err := IsPaused(wsRoot, project)
	if err != nil || !paused {
		return nil, err
	}
	matches, err := filepath.Glob(filepath.Join(ContextDir(root), "*.json"))
	if err != nil {
		return nil, err
	}
	var retired []string
	for _, f := range matches {
		sid := strings.TrimSuffix(filepath.Base(f), ".json")
		if sid == callerID || !ValidSessionID(sid) {
			continue
		}
		// Re-read under that session's lock: it may be claiming the project
		// again (which clears its marker) at the same moment.
		released := false
		err := withBadgeLock(root, sid, func() error {
			c, rerr := ReadContext(root, sid)
			if rerr != nil || c == nil || c.Project != project || !c.Paused {
				return nil
			}
			released = true
			return removeBadge(root, sid)
		})
		if err != nil {
			return retired, err
		}
		if released {
			retired = append(retired, sid)
		}
	}
	sort.Strings(retired)
	return retired, nil
}
