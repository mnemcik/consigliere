package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ActiveSession is one live session derived from a badge file.
type ActiveSession struct {
	Project   string
	Area      string
	Dirty     bool
	MTime     time.Time
	SessionID string
}

// ActiveProjects lists sessions whose badge file is still "live" per the
// liveness rule: a dirty session counts while its file's mtime is within
// dirtyWindow; a clean session while within activeWindow. Sessions without a
// project are skipped. Results are sorted by project then session id. Ports
// active-projects.sh (stat → os.Stat, jq → ReadContext). `now` is injected so
// callers control the clock (tests stay deterministic).
func ActiveProjects(root string, now time.Time, activeWindow, dirtyWindow time.Duration) ([]ActiveSession, error) {
	matches, err := filepath.Glob(filepath.Join(ContextDir(root), "*.json"))
	if err != nil {
		return nil, err
	}
	var out []ActiveSession
	for _, f := range matches {
		fi, serr := os.Stat(f)
		if serr != nil {
			continue
		}
		sid := strings.TrimSuffix(filepath.Base(f), ".json")
		c, rerr := ReadContext(root, sid)
		if rerr != nil || c == nil || c.Project == "" {
			continue
		}
		window := activeWindow
		if c.Dirty {
			window = dirtyWindow
		}
		if now.Sub(fi.ModTime()) > window {
			continue // stale
		}
		out = append(out, ActiveSession{
			Project: c.Project, Area: c.Area, Dirty: c.Dirty,
			MTime: fi.ModTime(), SessionID: sid,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Project != out[j].Project {
			return out[i].Project < out[j].Project
		}
		return out[i].SessionID < out[j].SessionID
	})
	return out, nil
}
