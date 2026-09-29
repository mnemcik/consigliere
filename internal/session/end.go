package session

import "fmt"

// EndSession releases a session's claim when the agent reports that the
// session ended (for Claude Code, the SessionEnd hook: exit, /clear, /resume,
// logout). Only a clean, unpaused claim is released:
//
//   - a dirty badge is kept, because the session ended without a wrap and its
//     worktree may hold unlanded work; cg active keeps listing it within the
//     dirty window;
//   - the badge carrying the pause marker is kept while its project's
//     resume.md exists, because a pause keeps its claim until another session
//     resumes the project.
//
// It reports whether the claim was released. A session without a badge file
// is not an error.
func EndSession(root, wsRoot, sessionID string) (bool, error) {
	if !ValidSessionID(sessionID) {
		return false, fmt.Errorf("invalid session id %q", sessionID)
	}
	released := false
	err := withBadgeLock(root, sessionID, func() error {
		c, err := ReadContext(root, sessionID)
		if err != nil || c == nil || c.Dirty {
			return err
		}
		if c.Paused {
			paused, err := IsPaused(wsRoot, c.Project)
			if err != nil || paused {
				return err
			}
		}
		if err := removeBadge(root, sessionID); err != nil {
			return err
		}
		released = true
		return nil
	})
	return released, err
}
