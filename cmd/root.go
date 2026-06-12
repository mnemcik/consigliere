package cmd

import (
	"github.com/mnemcik/consigliere/internal/autoupdate"
	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags
var Version = "dev"

// Command names, shared between cobra Use fields and the bootstrap skip logic.
const (
	cmdUpdate  = "update"
	cmdVersion = "version"
)

// noAutoUpdate is bound to the persistent --no-auto-update flag.
var noAutoUpdate bool

var rootCmd = &cobra.Command{
	Use:     "cg",
	Short:   "Consigliere — personal workspace management",
	Long:    "Consigliere (cg) is a personal workspace management framework.\nIt provides structure, templates, and conventions for organizing projects, ideas, notes, areas, and insights.",
	Version: Version,
	// Don't print usage on operational (non-usage) errors — a failed
	// `cg worktree ...` should report its error, not dump the command's help.
	SilenceUsage: true,
	// Runs before every subcommand: surface a one-shot "updated" notice from a
	// prior background install, then fire off the detached freshness check.
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		autoupdate.PrintUpdatedNoticeIfAny(cmd.ErrOrStderr())
		autoupdate.Bootstrap(autoUpdateDisabled(cmd))
		return nil
	},
	// After the command's own output, surface the persistent major-available
	// nudge (skipped for version/help and the update commands themselves).
	PersistentPostRunE: func(cmd *cobra.Command, _ []string) error {
		if !autoUpdateDisabled(cmd) {
			autoupdate.PrintMajorNoticeIfAny(cmd.ErrOrStderr())
		}
		return nil
	},
}

func init() {
	rootCmd.InitDefaultVersionFlag()
	if f := rootCmd.Flags().Lookup("version"); f != nil {
		f.Shorthand = "v"
	}
	rootCmd.PersistentFlags().BoolVar(&noAutoUpdate, "no-auto-update", false,
		"skip the background check for newer cg releases this run")
}

// autoUpdateDisabled decides whether to skip the background update check for
// this invocation. It is suppressed for dev builds, when the user opts out, and
// for commands where a background spawn would be surprising (version/help and
// the update subcommands themselves).
func autoUpdateDisabled(cmd *cobra.Command) bool {
	if noAutoUpdate || !autoupdate.IsReleaseVersion(Version) {
		return true
	}
	switch cmd.Name() {
	case cmdVersion, "help", cmdUpdate:
		return true
	}
	if p := cmd.Parent(); p != nil && p.Name() == cmdUpdate {
		return true
	}
	return false
}

func Execute() error {
	// When invoked as the detached worker, run the update check and exit
	// without dispatching any user command.
	if autoupdate.IsWorkerInvocation() {
		autoupdate.RunWorker(Version)
		return nil
	}
	return rootCmd.Execute()
}
