package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// startCmd starts the workspace's AI session: the command in `.cg.json`
// session.agent (default `claude`), run in the current directory with the
// user's arguments appended. Bare `cg` inside a workspace, from a terminal,
// does the same (see runRoot).
var startCmd = &cobra.Command{
	Use:   "start [agent args…]",
	Short: "Start this workspace's AI session (session.agent, default claude)",
	Long: "Start this workspace's AI session: run the command in .cg.json session.agent\n" +
		"(an argv array, default [\"claude\"]) in the current directory, with any\n" +
		"arguments passed on to it, e.g. `cg start --resume <id>`.\n\n" +
		"Bare `cg` does the same inside a workspace when run from a terminal; outside a\n" +
		"workspace, without a terminal, or when the agent can't be started, it prints\n" +
		"this help instead.\n\n" +
		"session.agent comes from the workspace's own .cg.json, so in a workspace you\n" +
		"cloned, check it before typing `cg`: the command runs before the agent's own\n" +
		"folder-trust prompt.\n\n" +
		"All arguments go to the agent, flags included; use `cg help start` for this text.",
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root := workspaceRootForDispatch()
		if root == "" {
			cmd.SilenceUsage = true
			cwd, _ := os.Getwd()
			return cgerr.New(cgerr.ExitUsage, "cg start: not inside a Consigliere workspace: %s", cwd)
		}
		path, argv, err := resolveAgent(root, args)
		if err != nil {
			cmd.SilenceUsage = true
			return err
		}
		return finishAgent(cmd, runAgent(path, argv))
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
	rootCmd.RunE = runRoot
}

// noAgentHint is what bare `cg` prints after the help when the workspace's
// agent can't be started; `cg start` shows the reason.
const noAgentHint = "cg: no agent started: set session.agent in .cg.json, or run `cg start` to see the error"

// runRoot is bare `cg`: start the session when inside a workspace and attached
// to a terminal, otherwise print help as before. The terminal check keeps
// scripts and pipes (`cg | cat`, `cg </dev/null`) from launching an agent.
// When the agent can't be started (not on PATH, invalid session.agent, or it
// fails to start), it also prints help, plus a one-line hint, and exits 0 as
// bare `cg` always did.
func runRoot(cmd *cobra.Command, _ []string) error {
	root := workspaceRootForDispatch()
	if root == "" || !isInteractive() {
		return cmd.Help()
	}
	path, argv, err := resolveAgent(root, nil)
	if err == nil {
		err = runAgent(path, argv)
		if err == nil || isExitCode(err) {
			return finishAgent(cmd, err)
		}
	}
	// The agent couldn't be resolved or failed to start (e.g. removed after
	// the lookup): help plus the hint, exit 0, as bare cg always did.
	if herr := cmd.Help(); herr != nil {
		return herr
	}
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), noAgentHint)
	return nil
}

// isExitCode reports whether err only carries an exit code passed through
// from the agent (a CodedError without a message), as opposed to a failure to
// start it.
func isExitCode(err error) bool {
	var ce *cgerr.CodedError
	return errors.As(err, &ce) && ce.Err.Error() == ""
}

// finishAgent applies cg's output rules to the result of running the agent:
// never the usage text, and no "Error:" line when the error only carries the
// agent's own exit code (Windows, where cg waits for the agent): the agent has
// already said what went wrong.
func finishAgent(cmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}
	cmd.SilenceUsage = true
	if isExitCode(err) {
		cmd.SilenceErrors = true
	}
	return err
}

// isInteractive reports whether stdin and stdout are both a terminal.
// A variable so tests can stand in for a terminal.
var isInteractive = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) //nolint:gosec // fd fits in int
}

// runAgent replaces (Unix) or runs (Windows) the agent process; see
// start_exec_*.go. A variable so tests can capture the call instead.
var runAgent = execAgent

// resolveAgent works out what `cg start` runs for the workspace at root with
// extra appended: the resolved program path and the full argv. An error means
// the agent can't be started (bare `cg` then falls back to help).
func resolveAgent(root string, extra []string) (path string, argv []string, err error) {
	cfg, err := workspace.Detect(root)
	if err != nil {
		return "", nil, err
	}
	agent := cfg.SessionSettings().Agent
	// Only the program must be named; later elements are passed verbatim (an
	// empty string is a valid argument).
	if strings.TrimSpace(agent[0]) == "" {
		return "", nil, cgerr.New(cgerr.ExitUsage, "cg start: session.agent in %s has an empty program name: %q", filepath.Join(root, ".cg.json"), agent)
	}
	path, err = exec.LookPath(agent[0])
	if err != nil {
		return "", nil, cgerr.New(cgerr.ExitUsage,
			"cg start: %q not found on PATH (the agent comes from session.agent in .cg.json, default %q)",
			agent[0], workspace.DefaultAgentCommand)
	}
	argv = append(append([]string{agent[0]}, agent[1:]...), extra...)
	return path, argv, nil
}

// argsForStart handles root persistent flags given before the `start` verb
// (`cg --no-auto-update start …`). startCmd disables flag parsing so that every
// argument after `start` reaches the agent unchanged, which also leaves such
// leading flags unparsed. argsForStart parses them into the root flag set and
// returns the arguments without them; a leading flag that doesn't parse is an
// error (it belongs to cg, not the agent). Any other argument list is returned
// unchanged.
func argsForStart(args []string) ([]string, error) {
	i := 0
	for i < len(args) && args[i] != "" && args[i][0] == '-' {
		i++
	}
	if i == 0 || i == len(args) || args[i] != startCmd.Name() {
		return args, nil
	}
	if err := rootCmd.PersistentFlags().Parse(args[:i]); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			// `cg --help start`: a help request. Handing the arguments to cobra
			// as they are would run start, whose disabled flag parsing passes
			// --help to the agent, so ask for start's help explicitly.
			return []string{cmdHelp, startCmd.Name()}, nil
		}
		return nil, cgerr.New(cgerr.ExitUsage, "%v", err)
	}
	return args[i:], nil
}
