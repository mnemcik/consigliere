package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mnemcik/consigliere/internal/autoupdate"
	"github.com/spf13/cobra"
)

func init() {
	updateCmd.AddCommand(updateCheckCmd)
	updateCmd.AddCommand(updateUpgradeCmd)
	rootCmd.AddCommand(updateCmd)
}

var updateCmd = &cobra.Command{
	Use:   cmdUpdate,
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

var updateUpgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Download and install the latest cg release in place",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()
		var b strings.Builder

		if !autoupdate.IsReleaseVersion(Version) {
			fmt.Fprintf(&b, "cg is a development build (%s); build from source to update.\n", Version)
			_, err := fmt.Fprint(out, b.String())
			return err
		}

		// Refuse to self-replace externally-managed installs (DEC-011).
		if m := autoupdate.DetectManagement(); !m.SelfManaged {
			switch m.Kind {
			case autoupdate.KindHomebrew:
				fmt.Fprintln(&b, "cg was installed via Homebrew — upgrade it with your package manager:")
				fmt.Fprintln(&b, "    brew upgrade --cask cg")
			default:
				fmt.Fprintln(&b, "cg was not installed via install.sh, so it won't self-replace.")
				fmt.Fprintln(&b, "Re-run the installer to upgrade:")
				fmt.Fprintln(&b, "    curl -fsSL https://raw.githubusercontent.com/mnemcik/consigliere/main/install.sh | bash")
			}
			_, err := fmt.Fprint(out, b.String())
			return err
		}

		ctx := context.Background()
		repo := autoupdate.Repo()
		res, err := autoupdate.Check(ctx, Version, repo)
		if err != nil {
			return fmt.Errorf("could not check for updates (offline?): %w", err)
		}
		if !res.Available {
			fmt.Fprintf(&b, "✅ Already on the latest version (%s).\n", res.Current)
			_, werr := fmt.Fprint(out, b.String())
			return werr
		}

		if _, err := fmt.Fprintf(out, "Upgrading cg %s → %s …\n", res.Current, res.Latest); err != nil {
			return err
		}
		m := autoupdate.DetectManagement()
		if err := autoupdate.InstallRelease(ctx, repo, res.Latest, m.BinaryPath); err != nil {
			return fmt.Errorf("upgrade failed: %w", err)
		}

		var tail strings.Builder
		if err := autoupdate.RefreshInstalledState(res.Latest, time.Now().UTC().Format(time.RFC3339)); err != nil {
			// Non-fatal: the binary is already replaced; only the bookkeeping
			// file failed to refresh.
			fmt.Fprintf(&tail, "⚠️  upgraded, but could not refresh installed.json: %v\n", err)
		}
		fmt.Fprintf(&tail, "✅ Upgraded to %s.\n", res.Latest)
		_, werr := fmt.Fprint(out, tail.String())
		return werr
	},
}
