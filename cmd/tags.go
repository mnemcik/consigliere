package cmd

import (
	"fmt"
	"os"
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

// workspaceRoot resolves the workspace root from cwd (git common root, falling
// back to the structural walk-up).
func workspaceRoot(cmd *cobra.Command) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if root, cerr := gitx.CommonRoot(cmd.Context(), cwd); cerr == nil {
		return root, nil
	}
	root, _, _ := workspace.FindRoot(cwd)
	if root == "" {
		return "", fmt.Errorf("not inside a Consigliere workspace")
	}
	return root, nil
}
