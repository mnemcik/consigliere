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

// resumeLookupTimeout bounds the git call that lists worktrees. Paused state
// is read from the SessionEnd hook, whose budget is short (1.5 s, shared with
// the badge lock wait); a slow or broken git falls back to the caller's own
// checkout.
const resumeLookupTimeout = 400 * time.Millisecond

// Checkout is the workspace as checked out in one worktree of its repository.
type Checkout struct {
	Dir    string // the workspace root in that worktree
	Branch string // the branch checked out there; "" when detached or unknown
	Own    bool   // the caller's own checkout
}

// Checkouts returns the workspace in every worktree of its repository: the
// caller's own checkout (wsRoot) first, then the same workspace directory in
// each other worktree. The pause-mode wrap writes resume.md in the session
// worktree and commits it to the unlanded session branch, so a session started
// in the main checkout (or in another worktree) only finds it there. A
// workspace nested below the git top level is mapped to the same subdirectory
// of each worktree. Outside git, or when git fails, only wsRoot is returned.
func Checkouts(wsRoot string) []Checkout {
	if wsRoot == "" {
		return nil
	}
	own := Checkout{Dir: wsRoot, Own: true}
	ctx, cancel := context.WithTimeout(context.Background(), resumeLookupTimeout)
	defer cancel()
	top := gitx.ShowToplevel(ctx, wsRoot)
	if top == "" {
		return []Checkout{own}
	}
	rel, err := filepath.Rel(canonical(top), canonical(wsRoot))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return []Checkout{own}
	}
	list, err := gitx.WorktreeList(ctx, wsRoot)
	if err != nil {
		return []Checkout{own}
	}
	out := []Checkout{own}
	seen := map[string]bool{canonical(wsRoot): true}
	for _, w := range list {
		d := filepath.Join(w.Path, rel)
		c := canonical(d)
		if c == canonical(wsRoot) {
			out[0].Branch = w.Branch
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, Checkout{Dir: d, Branch: w.Branch})
	}
	return out
}

// ResumeDirs returns the Dir of each of Checkouts(wsRoot).
func ResumeDirs(wsRoot string) []string {
	var dirs []string
	for _, c := range Checkouts(wsRoot) {
		dirs = append(dirs, c.Dir)
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

// Resume is one pause cursor: a project's resume.md in one checkout.
type Resume struct {
	Project  string
	File     string
	Checkout Checkout
}

// FindResume returns every resume.md for project across Checkouts(wsRoot),
// the caller's own checkout first.
func FindResume(wsRoot, project string) ([]Resume, error) {
	if wsRoot == "" || project == "" {
		return nil, nil
	}
	return findResume(Checkouts(wsRoot), project)
}

// findResume looks project's resume.md up in each checkout. A missing file is
// skipped. Any other error is returned when it comes from the caller's own
// checkout; from another worktree (a sibling directory the caller cannot read,
// an odd mount) it is skipped, so one bad worktree cannot break every lookup.
func findResume(cs []Checkout, project string) ([]Resume, error) {
	var found []Resume
	for _, c := range cs {
		f := ResumeFile(c.Dir, project)
		if _, err := os.Stat(f); err != nil {
			if errors.Is(err, fs.ErrNotExist) || !c.Own {
				continue
			}
			return found, err
		}
		found = append(found, Resume{Project: project, File: f, Checkout: c})
	}
	return found, nil
}

// IsPaused reports whether a project is paused: its resume.md exists in any
// worktree of the workspace (the wrap skill's pause mode writes it, and the
// resuming session deletes it once it has read it). A missing file means not
// paused; an error reading the caller's own checkout is returned.
func IsPaused(wsRoot, project string) (bool, error) {
	if wsRoot == "" || project == "" {
		return false, nil
	}
	return pausedIn(Checkouts(wsRoot), project)
}

func pausedIn(cs []Checkout, project string) (bool, error) {
	found, err := findResume(cs, project)
	return len(found) > 0, err
}

// PausedProjects lists every paused project across Checkouts(wsRoot), one
// entry per project (the caller's own checkout wins, then worktree order),
// sorted by project.
func PausedProjects(wsRoot string) []Resume {
	return pausedProjectsIn(Checkouts(wsRoot))
}

func pausedProjectsIn(cs []Checkout) []Resume {
	var out []Resume
	seen := map[string]bool{}
	for _, c := range cs {
		m, err := filepath.Glob(filepath.Join(c.Dir, "projects", "*", "resume.md"))
		if err != nil {
			continue
		}
		for _, f := range m {
			project := filepath.Base(filepath.Dir(f))
			if seen[project] {
				continue
			}
			seen[project] = true
			out = append(out, Resume{Project: project, File: f, Checkout: c})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Project < out[j].Project })
	return out
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
