package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/mnemcik/consigliere/internal/autoupdate"
	"github.com/spf13/cobra"
)

func init() {
	updateCmd.AddCommand(updateCheckCmd)
	rootCmd.AddCommand(updateCmd)
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for and install cg updates",
	Long: "Manage cg's own binary version.\n\n" +
		"`cg update check` reports whether a newer release is available; " +
		"`cg update upgrade` downloads and installs it in place. cg also checks " +
		"for updates in the background; disable with --no-auto-update or " +
		"CONSIGLIERE_AUTO_UPDATE=0.",
}

var updateCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check whether a newer cg release is available",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		var b strings.Builder

		if !autoupdate.IsReleaseVersion(Version) {
			fmt.Fprintf(&b, "cg is a development build (%s); skipping update check.\n", Version)
			_, err := fmt.Fprint(cmd.OutOrStdout(), b.String())
			return err
		}

		res, err := autoupdate.Check(context.Background(), Version, autoupdate.Repo())
		if err != nil {
			// A failed check is not a command failure — the user may simply be
			// offline. Report it and exit zero.
			fmt.Fprintln(&b, "⚠️  Could not check for updates (offline, or GitHub unreachable).")
			fmt.Fprintf(&b, "    %v\n", err)
			_, werr := fmt.Fprint(cmd.OutOrStdout(), b.String())
			return werr
		}

		fmt.Fprintf(&b, "Current version: %s\n", res.Current)
		fmt.Fprintf(&b, "Latest version:  %s\n", res.Latest)
		if res.Available {
			fmt.Fprintf(&b, "\n🎉 A newer version is available — upgrade with:\n    cg update upgrade\n")
		} else {
			fmt.Fprintln(&b, "\n✅ You are on the latest version.")
		}
		_, werr := fmt.Fprint(cmd.OutOrStdout(), b.String())
		return werr
	},
}
