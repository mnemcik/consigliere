package session

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ResumeFile returns the path of a project's pause cursor (written by the
// wrap skill's pause mode) under the workspace root.
func ResumeFile(wsRoot, project string) string {
	return filepath.Join(wsRoot, "projects", project, "resume.md")
}

// IsPaused reports whether a project was paused mid-work and not yet resumed:
// its resume.md exists. A resuming session deletes the file once work resumes.
func IsPaused(wsRoot, project string) bool {
	if wsRoot == "" || project == "" {
		return false
	}
	_, err := os.Stat(ResumeFile(wsRoot, project))
	return err == nil
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

// HandOver retires other sessions' claims on project when the project is
// paused (its resume.md exists): the caller is resuming the paused work, so
// the pausing session's badge no longer describes a live session. It returns
// the retired session IDs, sorted. It does nothing when the project is not
// paused, so two sessions that claim the same fresh project both stay listed.
func HandOver(root, wsRoot, callerID, project string) ([]string, error) {
	if !IsPaused(wsRoot, project) {
		return nil, nil
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
