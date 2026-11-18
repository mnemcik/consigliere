package cmd

import (
	"os"
	"strings"

	"github.com/mnemcik/consigliere/internal/autoupdate"
	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags
var Version = "dev"

// Command names, shared between cobra Use fields and the bootstrap skip logic.
const (
	cmdUpdate  = "update"
	cmdVersion = "version"
	cmdHelp    = "help"
	cmdCheck   = "check"
)

// noAutoUpdate is bound to the persistent --no-auto-update flag.
var noAutoUpdate bool

var rootCmd = &cobra.Command{
	Use:     "cg",
	Short:   "Consigliere — personal workspace management",
	Long:    "Consigliere (cg) is a personal workspace management framework.\nIt provides structure, templates, and conventions for organizing projects, ideas, notes, areas, and insights.",
	Version: Version,
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
	case cmdVersion, cmdHelp, cmdUpdate:
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
	// External-subcommand dispatch (git's `git foo` → `git-foo` pattern): if the
	// first arg isn't a built-in cg command but an installed extension or $PATH
	// provides cg-<verb>, exec it. Built-ins always take precedence.
	if path, rest, ok := externalDispatch(os.Args[1:]); ok {
		return runExternal(path, rest)
	}
	args, err := argsForStart(os.Args[1:])
	if err != nil {
		rootCmd.PrintErrln("Error:", err)
		return err
	}
	rootCmd.SetArgs(args)
	err = rootCmd.Execute()
	if hint := unknownCommandHint(err, workspaceRootForDispatch()); hint != "" {
		rootCmd.PrintErrln(hint)
	}
	return err
}

// extensionCommandHint follows cobra's "unknown command" error outside a
// workspace: the verb may be an extension's command, which resolves only inside
// the workspace that installed it or in a session started there with `cg`.
const extensionCommandHint = "If it is an extension's command: those work inside the workspace that installed them, " +
	"and in a session started there with `cg` (which puts them on PATH)."

// unknownCommandHint returns extensionCommandHint when err is cobra's unknown
// command error and cg runs outside any workspace (root ""), "" otherwise.
func unknownCommandHint(err error, root string) string {
	if err == nil || root != "" || !strings.HasPrefix(err.Error(), "unknown command ") {
		return ""
	}
	return extensionCommandHint
}
