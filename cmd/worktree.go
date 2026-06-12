package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
	"github.com/mnemcik/consigliere/internal/worktree"
)

var (
	worktreeCreateForce  bool
	worktreeLandStrategy string
)

func init() {
	worktreeCreateCmd.Flags().BoolVar(&worktreeCreateForce, "force", false,
		"reuse or attach even when the branch has unlanded commits")
	worktreeLandCmd.Flags().StringVar(&worktreeLandStrategy, "strategy", "",
		"landing strategy: direct-to-main or pr (default: from .cg.json, else direct-to-main)")
	worktreeCmd.AddCommand(worktreeCreateCmd)
	worktreeCmd.AddCommand(worktreeLandCmd)
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
	// Past argument validation, errors are operational (git/workspace failures),
	// not usage mistakes — report them without dumping the command's help. Arg
	// validation runs before RunE, so `cobra.ExactArgs` errors still print usage.
	cmd.SilenceUsage = true

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

var worktreeLandCmd = &cobra.Command{
	Use:   "land [<sha>]",
	Short: "Land the session worktree's commits onto the landing branch",
	Long: `Land the current session worktree's HEAD onto the landing branch (default
"main"), run from inside the worktree.

direct-to-main (default): pushes HEAD → origin/<landingBranch>; on a non-ff
rejection it rebases onto the landing branch and retries. A rebase conflict
leaves the rebase in progress and exits 3. Prints the landed SHA on success.

pr: pushes the session branch and opens a pull request via 'gh'; prints the PR
URL.

Optional <sha>: asserts that commit is reachable from HEAD before landing.

Exit codes: 1 usage, 3 rebase conflict, 4 push failed, 5 assertion failed.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorktreeLand,
}

func runWorktreeLand(cmd *cobra.Command, args []string) error {
	// Past arg validation, failures are operational — don't dump usage.
	cmd.SilenceUsage = true

	ctx := cmd.Context()

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// The land runs from inside the session worktree; config lives in the main
	// workspace root (the shared common dir's parent).
	root, err := gitx.CommonRoot(ctx, cwd)
	if err != nil {
		return fmt.Errorf("not inside a git repository: %w", err)
	}
	cfg, err := workspace.Detect(root)
	if err != nil {
		return fmt.Errorf("error reading %s: %w", workspace.ConfigFile, err)
	}
	w := cfg.WorktreeSettings()

	strategy := w.LandingStrategy
	if worktreeLandStrategy != "" {
		strategy = worktreeLandStrategy
	}
	var targetSHA string
	if len(args) == 1 {
		targetSHA = args[0]
	}

	res, err := worktree.Land(ctx, &worktree.LandOptions{
		WorktreeDir:   cwd,
		BranchPrefix:  w.BranchPrefix,
		LandingBranch: w.LandingBranch,
		Strategy:      strategy,
		TargetSHA:     targetSHA,
	}, cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	out := res.SHA
	if res.Strategy == workspace.StrategyPR {
		out = res.PRURL
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
	return nil
}
