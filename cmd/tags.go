package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/audit"
	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

func init() {
	rootCmd.AddCommand(tagsCmd)
}

var tagsCmd = &cobra.Command{
	Use:   "tags",
	Short: "Count the free-form area tags in use",
	Long:  "Scan areas/*.md for the `- **Tags:**` Meta line and report each tag with its count and the areas carrying it, most-used first.",
	Args:  cobra.NoArgs,
	RunE:  runTags,
}

func runTags(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true

	root, err := workspaceRoot(cmd)
	if err != nil {
		return err
	}
	counts, err := audit.Tags(root)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(counts) == 0 {
		_, _ = fmt.Fprintln(out, "no tags found in areas/*.md")
		return nil
	}
	_, _ = fmt.Fprintln(out, "Tag counts (descending) — areas carrying each tag:")
	_, _ = fmt.Fprintln(out)
	for _, c := range counts {
		_, _ = fmt.Fprintf(out, "  %3d  %-20s  %s\n", len(c.Areas), c.Tag, strings.Join(c.Areas, ","))
	}
	return nil
}

// workspaceRoot resolves the workspace the command runs in: the current git
// worktree's root when that is a workspace, else the nearest directory walking
// up from cwd (a workspace in a subdirectory of its repo, or no git at all).
//
// It deliberately does not use the git common root. From a session worktree
// that is the main worktree, so a command run there would read and write the
// main checkout instead of the session's own files (consigliere#140). Session
// state that genuinely belongs to the main worktree, the badge and the active
// registry, resolves it separately in internal/session; cg share uses
// landedWorkspaceRoot.
func workspaceRoot(cmd *cobra.Command) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if top := gitx.ShowToplevel(cmd.Context(), cwd); top != "" {
		cfg, derr := workspace.Detect(top)
		if derr != nil {
			// A .cg.json that does not parse: say which, not "not a workspace".
			return "", fmt.Errorf("%s: %w", filepath.Join(top, workspace.ConfigFile), derr)
		}
		if cfg != nil {
			return top, nil
		}
	}
	root, _, ferr := workspace.FindRoot(cwd)
	if ferr != nil {
		return "", ferr
	}
	if root == "" {
		return "", fmt.Errorf("not inside a Consigliere workspace")
	}
	return root, nil
}
