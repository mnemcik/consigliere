package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/migrate"
	"github.com/mnemcik/consigliere/internal/workspace"
	"github.com/mnemcik/consigliere/templates"
)

var migrateApply bool

func init() {
	migrateCmd.Flags().BoolVar(&migrateApply, "apply", false, "write the changes (default: dry run)")
	rootCmd.AddCommand(migrateCmd)
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Convert item metadata from ## Meta blocks to YAML frontmatter",
	Long: `Convert every item under areas/, ideas/, notes/, insights/ and projects/
from a ## Meta bullet block to YAML frontmatter, and replace the item
templates under templates/ that are still unedited copies cg shipped.

A dry run by default: it lists what would change and writes nothing. Pass
--apply to write. --apply needs a clean working tree, so the result is one
reviewable git diff.

Every field bullet becomes a frontmatter key and the ## Meta heading goes.
Prose and comments inside the Meta section stay in the body; only the
vocabulary comments cg's templates shipped are dropped. A Status followed by
prose splits into status and status_note. Area review narrative moves to a
## Review History section. Where frontmatter already holds a field, it wins,
and the differing ## Meta value is listed as a conflict.

Framework and extension notes are skipped: their ## Meta stays in the body so
cg sync and cg extension update keep delivering them. Templates the workspace
edited are reported, not touched. Running it again changes nothing.

It acts on the worktree you run it in, not the main checkout.`,
	Args: cobra.NoArgs,
	RunE: runMigrate,
}

func runMigrate(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// The current worktree, deliberately not the git common root: a
	// workspace-wide rewrite run from a session worktree must land there.
	root := gitx.ShowToplevel(cmd.Context(), cwd)
	if root == "" {
		root, _, _ = workspace.FindRoot(cwd)
	}
	if root == "" {
		return fmt.Errorf("not inside a Consigliere workspace")
	}
	if cfg, _ := workspace.Detect(root); cfg == nil {
		return fmt.Errorf("%s is not a Consigliere workspace (no .cg.json)", root)
	}
	if migrateApply && !gitx.IsClean(cmd.Context(), root) {
		return fmt.Errorf("the working tree has uncommitted changes; commit or stash them first so the migration is one reviewable diff")
	}

	plan, err := migrate.Build(root, templates.FS)
	if err != nil {
		return err
	}
	report(cmd, plan)
	if !migrateApply {
		if len(plan.Changes) > 0 || hasReplace(plan) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "\nDry run: nothing written. Re-run with --apply to write.")
		}
		return nil
	}
	if err := plan.Apply(); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "\nWritten. Review with git diff, then commit.")
	return nil
}

func hasReplace(p *migrate.Plan) bool {
	for _, t := range p.Templates {
		if t.Action == migrate.TemplateReplace {
			return true
		}
	}
	return false
}

func report(cmd *cobra.Command, p *migrate.Plan) {
	out := cmd.OutOrStdout()
	var conflicts, notes int
	for _, c := range p.Changes {
		conflicts += len(c.Conflicts)
		notes += len(c.Notes)
	}
	_, _ = fmt.Fprintf(out, "Items to convert: %d   conflicts: %d   notes: %d   skipped (framework/extension notes): %d\n",
		len(p.Changes), conflicts, notes, len(p.Skipped))
	for _, c := range p.Changes {
		for _, x := range c.Conflicts {
			_, _ = fmt.Fprintf(out, "  conflict  %s: %s\n", c.Path, x)
		}
		for _, x := range c.Notes {
			_, _ = fmt.Fprintf(out, "  note      %s: %s\n", c.Path, x)
		}
	}
	for _, t := range p.Templates {
		switch t.Action {
		case migrate.TemplateReplace:
			_, _ = fmt.Fprintf(out, "  template  %s: unedited copy, replaced with the frontmatter version\n", t.Path)
		case migrate.TemplateCustomised:
			_, _ = fmt.Fprintf(out, "  template  %s: edited by this workspace, left alone; port your edits to the frontmatter form by hand\n", t.Path)
		}
	}
	for _, e := range p.Errors {
		_, _ = fmt.Fprintf(out, "  error     %s (left unchanged)\n", e)
	}
	if len(p.Changes) == 0 && len(p.Templates) == 0 && len(p.Errors) == 0 {
		_, _ = fmt.Fprintln(out, "Nothing to migrate.")
	}
}
