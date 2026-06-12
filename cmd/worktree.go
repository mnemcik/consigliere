package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
	"github.com/mnemcik/consigliere/internal/worktree"
)

var worktreeCreateForce bool

func init() {
	worktreeCreateCmd.Flags().BoolVar(&worktreeCreateForce, "force", false,
		"reuse or attach even when the branch has unlanded commits")
	worktreeCmd.AddCommand(worktreeCreateCmd)
	rootCmd.AddCommand(worktreeCmd)
}

var worktreeCmd = &cobra.Command{
	Use:   "worktree",
	Short: "Manage per-session git worktrees",
	Long: `Manage the ephemeral per-session worktrees that keep parallel Claude Code
sessions from sharing a git index.

Each session works on a branch "<branchPrefix><slug>" (default prefix
"session/") in a sibling worktree directory "<workspace-root>--<slug>", and
lands its commits onto the landing branch (default "main").`,
}

var worktreeCreateCmd = &cobra.Command{
	Use:   "create <slug>",
	Short: "Create or reuse a session worktree on an ephemeral branch",
	Long: `Create (or idempotently reuse) the worktree for <slug>.

Prints the worktree path to stdout on success so callers can:

    path=$(cg worktree create my-slug) && cd "$path"

Exits 2 when the target branch or worktree has unlanded commits; re-run with
--force to proceed as-is.`,
	Args: cobra.ExactArgs(1),
	RunE: runWorktreeCreate,
}

func runWorktreeCreate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := gitx.CommonRoot(ctx, cwd)
	if err != nil {
		return fmt.Errorf("not inside a git repository: %w", err)
	}

	cfg, err := workspace.Detect(root)
	if err != nil {
		return fmt.Errorf("error reading %s: %w", workspace.ConfigFile, err)
	}
	w := cfg.WorktreeSettings()

	path, err := worktree.Create(ctx, args[0], worktree.Options{
		Root:          root,
		Prefix:        w.Root,
		BranchPrefix:  w.BranchPrefix,
		LandingBranch: w.LandingBranch,
		Force:         worktreeCreateForce,
	}, cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(cmd.OutOrStdout(), path)
	return nil
}
