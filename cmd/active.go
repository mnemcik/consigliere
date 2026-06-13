package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/session"
	"github.com/mnemcik/consigliere/internal/workspace"
)

var (
	activeSlugs bool
	activeJSON  bool
)

func init() {
	activeCmd.Flags().BoolVar(&activeSlugs, "slugs", false, "print only distinct active project slugs")
	activeCmd.Flags().BoolVar(&activeJSON, "json", false, "print a JSON array of active sessions")
	rootCmd.AddCommand(activeCmd)
}

var activeCmd = &cobra.Command{
	Use:   "active",
	Short: "List projects with a live Claude Code session",
	Long: `List sessions whose per-session badge file is still live: a dirty session
counts while recently touched (dirtyWindow, default 48h), a clean one within
activeWindow (default 4h). Windows come from .cg.json (session.activeWindowMin /
dirtyWindowMin). Use --slugs for distinct project slugs or --json for records.`,
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
	root, err := gitx.CommonRoot(ctx, cwd)
	if err != nil {
		root, _, _ = workspace.FindRoot(cwd)
	}
	cfg, _ := workspace.Detect(root)
	s := cfg.SessionSettings()

	sessions, err := session.ActiveProjects(root, time.Now(),
		time.Duration(s.ActiveWindowMin)*time.Minute,
		time.Duration(s.DirtyWindowMin)*time.Minute)
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
		}
		recs := make([]rec, 0, len(sessions))
		for _, s := range sessions {
			recs = append(recs, rec{s.Project, s.Area, s.Dirty, s.MTime.Format("2006-01-02 15:04"), s.SessionID})
		}
		data, merr := json.MarshalIndent(recs, "", "  ")
		if merr != nil {
			return merr
		}
		_, _ = fmt.Fprintln(out, string(data))
	case activeSlugs:
		seen := map[string]bool{}
		for _, s := range sessions {
			if !seen[s.Project] {
				seen[s.Project] = true
				_, _ = fmt.Fprintln(out, s.Project)
			}
		}
	default:
		for _, s := range sessions {
			_, _ = fmt.Fprintf(out, "%s\t%s\t%t\t%s\t%s\n",
				s.Project, s.Area, s.Dirty, s.MTime.Format("2006-01-02 15:04"), s.SessionID)
		}
	}
	return nil
}
