package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Session states reported by ActiveProjects.
const (
	StateLive   = "live"   // a claimed badge within its liveness window
	StatePaused = "paused" // the project has a resume.md; nobody is on it now
)

// ActiveSession is one entry in the active-work listing: a live session
// derived from a badge file, or a paused project derived from its resume.md
// (SessionID is then the pausing session's, when its badge still exists).
type ActiveSession struct {
	Project   string
	Area      string
	Dirty     bool
	MTime     time.Time
	SessionID string
	State     string
}

// ActiveOptions configures ActiveProjects.
type ActiveOptions struct {
	// WorkspaceRoot is where projects/<slug>/resume.md is looked up. Empty
	// disables paused detection.
	WorkspaceRoot string
	// Now is injected so callers control the clock (tests stay deterministic).
	Now time.Time
	// ActiveWindow and DirtyWindow bound how long an unreleased badge counts
	// as live after its last write: a dirty badge within DirtyWindow, a clean
	// one within ActiveWindow. They only matter for sessions that ended
	// without releasing their claim (crash, closed terminal, no wrap); an
	// end-mode wrap releases the claim and deletes the badge.
	ActiveWindow, DirtyWindow time.Duration
	// ExcludeSessionID drops the caller's own session from the listing.
	ExcludeSessionID string
}

// ActiveProjects lists live sessions and paused projects. While a project has
// a resume.md, the badge carrying the pause marker (the session that paused
// it) is reported as paused, whatever its age. Other badges for the project,
// such as a session resuming it, are treated as live. A paused
// project with no badge left, and no live session resuming it, is listed from
// its resume.md. Badges without a project are skipped. Results are sorted by
// project, then state, then session ID.
func ActiveProjects(root string, opts ActiveOptions) ([]ActiveSession, error) {
	matches, err := filepath.Glob(filepath.Join(ContextDir(root), "*.json"))
	if err != nil {
		return nil, err
	}
	var out []ActiveSession
	covered := map[string]bool{} // projects already reported by a badge
	for _, f := range matches {
		sid := strings.TrimSuffix(filepath.Base(f), ".json")
		if sid == opts.ExcludeSessionID {
			continue
		}
		fi, serr := os.Stat(f)
		if serr != nil {
			continue
		}
		c, rerr := ReadContext(root, sid)
		if rerr != nil || c == nil || c.Project == "" {
			continue
		}
		s := ActiveSession{
			Project: c.Project, Area: c.Area, Dirty: c.Dirty,
			MTime: fi.ModTime(), SessionID: sid, State: StateLive,
		}
		paused, perr := IsPaused(opts.WorkspaceRoot, c.Project)
		if perr != nil {
			return nil, perr
		}
		if paused && c.Paused {
			s.State = StatePaused
			covered[c.Project] = true
			out = append(out, s)
			continue
		}
		window := opts.ActiveWindow
		if c.Dirty {
			window = opts.DirtyWindow
		}
		if opts.Now.Sub(fi.ModTime()) > window {
			continue // stale
		}
		covered[c.Project] = true
		out = append(out, s)
	}
	out = append(out, pausedWithoutBadge(opts.WorkspaceRoot, covered)...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Project != out[j].Project {
			return out[i].Project < out[j].Project
		}
		if out[i].State != out[j].State {
			return out[i].State < out[j].State
		}
		return out[i].SessionID < out[j].SessionID
	})
	return out, nil
}

// pausedWithoutBadge lists paused projects (projects/*/resume.md) that no
// badge already reported, as paused or as a live resuming session.
func pausedWithoutBadge(wsRoot string, seen map[string]bool) []ActiveSession {
	if wsRoot == "" {
		return nil
	}
	files, err := filepath.Glob(filepath.Join(wsRoot, "projects", "*", "resume.md"))
	if err != nil {
		return nil
	}
	var out []ActiveSession
	for _, f := range files {
		project := filepath.Base(filepath.Dir(f))
		if seen[project] {
			continue
		}
		fi, serr := os.Stat(f)
		if serr != nil {
			continue
		}
		out = append(out, ActiveSession{Project: project, MTime: fi.ModTime(), State: StatePaused})
	}
	return out
}
