package session

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ResumeFile returns the path of a project's pause cursor (written by the
// wrap skill's pause mode) under the workspace root.
func ResumeFile(wsRoot, project string) string {
	return filepath.Join(wsRoot, "projects", project, "resume.md")
}

// PausedSince reports whether a project is paused and, if so, when: its
// resume.md exists (the wrap skill's pause mode writes it). A missing file
// means not paused; any other error is returned. The time lets callers tell the
// pausing session's badge (written no later than resume.md) from a resuming
// session's badge (written after it), since resume.md stays until the resuming
// session has read it.
func PausedSince(wsRoot, project string) (time.Time, bool, error) {
	if wsRoot == "" || project == "" {
		return time.Time{}, false, nil
	}
	fi, err := os.Stat(ResumeFile(wsRoot, project))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	return fi.ModTime(), true, nil
}

// pausedBadge reports whether a badge written at badgeTime belongs to the
// session that paused the project at pausedAt, rather than one resuming it.
func pausedBadge(badgeTime, pausedAt time.Time) bool {
	return !badgeTime.After(pausedAt)
}

// Release ends a session's claim by deleting its badge file, so cg active no
// longer lists it and a later claim starts from a clean file. Releasing a
// session that has no badge file is not an error.
func Release(root, sessionID string) error {
	if !ValidSessionID(sessionID) {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	if err := os.Remove(ContextFile(root, sessionID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// HandOver retires the pausing session's claim on project when the caller
// resumes it: every other badge for the project written no later than its
// resume.md. A badge written after resume.md belongs to a session that already
// resumed the project, so it is kept. It returns the retired session IDs,
// sorted. It does nothing when the project is not paused, so two sessions that
// claim the same fresh project both stay listed.
func HandOver(root, wsRoot, callerID, project string) ([]string, error) {
	pausedAt, paused, err := PausedSince(wsRoot, project)
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
		fi, serr := os.Stat(f)
		if serr != nil || !pausedBadge(fi.ModTime(), pausedAt) {
			continue
		}
		c, rerr := ReadContext(root, sid)
		if rerr != nil || c == nil || c.Project != project {
			continue
		}
		if err := Release(root, sid); err != nil {
			return retired, err
		}
		retired = append(retired, sid)
	}
	sort.Strings(retired)
	return retired, nil
}
