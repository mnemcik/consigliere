package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/session"
)

var (
	activeSlugs     bool
	activeJSON      bool
	activeSessionID string
)

func init() {
	activeCmd.Flags().BoolVar(&activeSlugs, "slugs", false, "print only distinct active project slugs")
	activeCmd.Flags().BoolVar(&activeJSON, "json", false, "print a JSON array of active sessions")
	activeCmd.Flags().StringVar(&activeSessionID, "session-id", "", "the caller's session ID, excluded from the listing (see below for defaults)")
	rootCmd.AddCommand(activeCmd)
}

var activeCmd = &cobra.Command{
	Use:   "active",
	Short: "List projects with a live session, and paused projects",
	Long: `List sessions that still claim a project, plus paused projects.

A session claims a project with cg session set-context and releases it with
cg session release (the end-mode wrap does this), so a wrapped session is no
longer listed. The caller's own session is always excluded.

A project with projects/<slug>/resume.md is paused: it is listed with state
"paused", whatever the age of its badge, and is not in --slugs, since nobody
is working on it. Resuming it (set-context on that project) releases the
pausing session's claim.

A claim that was never released (crash, closed terminal, no wrap) counts as
live while its badge file was written recently: within dirtyWindow (default
48h) when dirty, within activeWindow (default 4h) when clean. Windows come from
.cg.json (session.activeWindowMin / dirtyWindowMin).

Default output is one tab-separated line per entry: project, area, dirty,
mtime, session, state. Use --slugs for distinct live project slugs or --json
for records.

` + sessionIDHelp,
	Args: cobra.NoArgs,
	RunE: runActive,
}

func runActive(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	if activeSlugs && activeJSON {
		return fmt.Errorf("--slugs and --json are mutually exclusive")
	}

	ctx := cmd.Context()
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, wsRoot, cfg := sessionRoots(ctx, cwd)
	s := cfg.SessionSettings()

	sessions, err := session.ActiveProjects(root, session.ActiveOptions{
		WorkspaceRoot:    wsRoot,
		Now:              time.Now(),
		ActiveWindow:     time.Duration(s.ActiveWindowMin) * time.Minute,
		DirtyWindow:      time.Duration(s.DirtyWindowMin) * time.Minute,
		ExcludeSessionID: resolveSessionID(activeSessionID),
	})
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	switch {
	case activeJSON:
		type rec struct {
			Project   string `json:"project"`
			Area      string `json:"area"`
			Dirty     bool   `json:"dirty"`
			MTime     string `json:"mtime"`
			SessionID string `json:"session_id"`
			State     string `json:"state"`
		}
		recs := make([]rec, 0, len(sessions))
		for _, s := range sessions {
			recs = append(recs, rec{s.Project, s.Area, s.Dirty, s.MTime.Format("2006-01-02 15:04"), s.SessionID, s.State})
		}
		data, merr := json.MarshalIndent(recs, "", "  ")
		if merr != nil {
			return merr
		}
		_, _ = fmt.Fprintln(out, string(data))
	case activeSlugs:
		seen := map[string]bool{}
		for _, s := range sessions {
			if s.State == session.StateLive && !seen[s.Project] {
				seen[s.Project] = true
				_, _ = fmt.Fprintln(out, s.Project)
			}
		}
	default:
		for _, s := range sessions {
			_, _ = fmt.Fprintf(out, "%s\t%s\t%t\t%s\t%s\t%s\n",
				s.Project, s.Area, s.Dirty, s.MTime.Format("2006-01-02 15:04"), s.SessionID, s.State)
		}
	}
	return nil
}
