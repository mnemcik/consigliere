package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/audit"
)

func init() {
	colorsCmd.AddCommand(colorsCheckCmd)
	rootCmd.AddCommand(colorsCmd)
}

var colorsCmd = &cobra.Command{
	Use:   "colors",
	Short: "Inspect badge color assignments",
}

var colorsCheckCmd = &cobra.Command{
	Use:   cmdCheck,
	Short: "Report duplicate or missing badge colors",
	Long: `Scan projects/*/README.md and areas/*.md for the **Color:** Meta field,
reporting assignments, items without a color, and any color used by more than
one item. Exits non-zero when duplicates exist, so it can gate a pre-commit hook.`,
	Args:          cobra.NoArgs,
	RunE:          runColorsCheck,
	SilenceErrors: true, // the report is the output; the non-zero exit is the signal
}

func runColorsCheck(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true

	root, err := workspaceRoot(cmd)
	if err != nil {
		return err
	}
	rep, err := audit.ColorsCheck(root)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	for _, d := range rep.Duplicates {
		_, _ = fmt.Fprintf(out, "DUPLICATE: color %q is used by %d items:\n", d.Color, len(d.Files))
		for _, f := range d.Files {
			_, _ = fmt.Fprintf(out, "  - %s\n", f)
		}
		_, _ = fmt.Fprintln(out)
	}

	_, _ = fmt.Fprintf(out, "Assigned colors (%d):\n", len(rep.Assigned))
	for _, e := range rep.Assigned {
		_, _ = fmt.Fprintf(out, "  %-20s %s\n", e.Color, e.File)
	}
	if len(rep.Missing) > 0 {
		_, _ = fmt.Fprintf(out, "\nWithout color (%d):\n", len(rep.Missing))
		for _, f := range rep.Missing {
			_, _ = fmt.Fprintf(out, "  %s\n", f)
		}
	}
	_, _ = fmt.Fprintf(out, "\nSummary: %d assigned, %d without color, %d duplicate group(s).\n",
		len(rep.Assigned), len(rep.Missing), len(rep.Duplicates))

	if len(rep.Duplicates) > 0 {
		// Non-zero exit gates pre-commit use; SilenceErrors keeps the report clean.
		return errors.New("duplicate badge colors found")
	}
	return nil
}
