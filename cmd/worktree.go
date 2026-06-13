package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
	"github.com/mnemcik/consigliere/internal/worktree"
)

var (
	worktreeCreateForce  bool
	worktreeLandStrategy string
	worktreeRemoveForce  bool
)

func init() {
	worktreeCreateCmd.Flags().BoolVar(&worktreeCreateForce, "force", false,
		"reuse or attach even when the branch has unlanded commits")
	worktreeLandCmd.Flags().StringVar(&worktreeLandStrategy, "strategy", "",
		"landing strategy: direct-to-main or pr (default: from .cg.json, else direct-to-main)")
	worktreeRemoveCmd.Flags().BoolVar(&worktreeRemoveForce, "force", false,
		"remove even when the branch has unlanded commits (discards them)")
	worktreeCmd.AddCommand(worktreeCreateCmd)
	worktreeCmd.AddCommand(worktreeLandCmd)
	worktreeCmd.AddCommand(worktreeRemoveCmd)
	worktreeCmd.AddCommand(worktreeListCmd)
	rootCmd.AddCommand(worktreeCmd)
}

// worktreeRootConfig resolves the caller's cwd, the main workspace root, and the
// effective worktree settings from the workspace's .cg.json.
func worktreeRootConfig(ctx context.Context) (cwd, root string, w workspace.WorktreeConfig, err error) {
	cwd, err = os.Getwd()
	if err != nil {
		return "", "", workspace.WorktreeConfig{}, err
	}
	root, err = gitx.CommonRoot(ctx, cwd)
	if err != nil {
		return "", "", workspace.WorktreeConfig{}, fmt.Errorf("not inside a git repository: %w", err)
	}
	cfg, err := workspace.Detect(root)
	if err != nil {
		return "", "", workspace.WorktreeConfig{}, fmt.Errorf("error reading %s: %w", workspace.ConfigFile, err)
	}
	return cwd, root, cfg.WorktreeSettings(), nil
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
	_, root, w, err := worktreeRootConfig(ctx)
	if err != nil {
		return err
	}

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
	// The land runs from inside the session worktree (cwd); config lives in the
	// main workspace root (the shared common dir's parent).
	cwd, _, w, err := worktreeRootConfig(ctx)
	if err != nil {
		return err
	}

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

var worktreeRemoveCmd = &cobra.Command{
	Use:   "remove <slug>",
	Short: "Remove a session worktree and delete its branch",
	Long: `Remove the worktree for <slug> and delete its local branch.

Refuses (exit 2) when the branch has commits not yet landed on the landing
branch; re-run with --force to remove anyway (discarding the unlanded work).
Will not run from inside the worktree being removed.`,
	Args: cobra.ExactArgs(1),
	RunE: runWorktreeRemove,
}

func runWorktreeRemove(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	ctx := cmd.Context()
	_, root, w, err := worktreeRootConfig(ctx)
	if err != nil {
		return err
	}

	return worktree.Remove(ctx, args[0], worktree.Options{
		Root:          root,
		Prefix:        w.Root,
		BranchPrefix:  w.BranchPrefix,
		LandingBranch: w.LandingBranch,
		Force:         worktreeRemoveForce,
	}, cmd.ErrOrStderr())
}

var worktreeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List worktrees, annotating session worktrees",
	Long: `List every worktree of the repository. Session worktrees (branch with the
configured prefix) are annotated with their slug and how many commits they are
ahead of the local landing ref (no network access; "?" when unknown).`,
	Args: cobra.NoArgs,
	RunE: runWorktreeList,
}

func runWorktreeList(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	ctx := cmd.Context()
	_, root, w, err := worktreeRootConfig(ctx)
	if err != nil {
		return err
	}

	entries, err := worktree.List(ctx, worktree.Options{
		Root:          root,
		Prefix:        w.Root,
		BranchPrefix:  w.BranchPrefix,
		LandingBranch: w.LandingBranch,
	})
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "SLUG\tAHEAD\tBRANCH\tPATH")
	for _, e := range entries {
		slug := e.Slug
		if slug == "" {
			slug = "-"
		}
		branch := e.Branch
		if e.Detached {
			branch = "(detached)"
		}
		ahead := "-"
		if e.IsSession() {
			if e.Ahead < 0 {
				ahead = "?"
			} else {
				ahead = fmt.Sprintf("%d", e.Ahead)
			}
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", slug, ahead, branch, e.Path)
	}
	return tw.Flush()
}
