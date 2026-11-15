package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
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
		"workspace, or without a terminal, it prints this help instead.\n\n" +
		"All arguments go to the agent, flags included; use `cg help start` for this text.",
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		root := workspaceRootForDispatch()
		if root == "" {
			cwd, _ := os.Getwd()
			return cgerr.New(cgerr.ExitUsage, "cg start: not inside a Consigliere workspace: %s", cwd)
		}
		return startSession(root, args)
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
	rootCmd.RunE = runRoot
}

// runRoot is bare `cg`: start the session when inside a workspace and attached
// to a terminal, otherwise print help as before. The terminal check keeps
// scripts and pipes (`cg | cat`, `cg </dev/null`) from launching an agent.
func runRoot(cmd *cobra.Command, _ []string) error {
	root := workspaceRootForDispatch()
	if root == "" || !isInteractive() {
		return cmd.Help()
	}
	return startSession(root, nil)
}

// isInteractive reports whether stdin and stdout are both a terminal.
// A variable so tests can stand in for a terminal.
var isInteractive = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) //nolint:gosec // fd fits in int
}

// runAgent replaces (Unix) or runs (Windows) the agent process; see
// start_exec_*.go. A variable so tests can capture the call instead.
var runAgent = execAgent

// startSession resolves the agent command for the workspace at root and runs it
// with extra appended.
func startSession(root string, extra []string) error {
	cfg, err := workspace.Detect(root)
	if err != nil {
		return err
	}
	agent := cfg.SessionSettings().Agent
	// Only the program must be named; later elements are passed verbatim (an
	// empty string is a valid argument).
	if strings.TrimSpace(agent[0]) == "" {
		return cgerr.New(cgerr.ExitUsage, "cg start: session.agent in %s has an empty program name: %q", filepath.Join(root, ".cg.json"), agent)
	}
	path, err := exec.LookPath(agent[0])
	if err != nil {
		return cgerr.New(cgerr.ExitUsage,
			"cg start: %q not found on PATH (the agent comes from session.agent in .cg.json, default %q)",
			agent[0], workspace.DefaultAgentCommand)
	}
	argv := append(append([]string{agent[0]}, agent[1:]...), extra...)
	return runAgent(path, argv)
}

// argsForStart handles root persistent flags given before the `start` verb
// (`cg --no-auto-update start …`). startCmd disables flag parsing so that every
// argument after `start` reaches the agent unchanged, which also leaves such
// leading flags unparsed and forwarded to the agent. argsForStart parses them
// into the root flag set and returns the arguments without them. Any other
// argument list, or leading flags that don't parse, is returned unchanged.
func argsForStart(args []string) []string {
	i := 0
	for i < len(args) && args[i] != "" && args[i][0] == '-' {
		i++
	}
	if i == 0 || i == len(args) || args[i] != startCmd.Name() {
		return args
	}
	if err := rootCmd.PersistentFlags().Parse(args[:i]); err != nil {
		return args
	}
	return args[i:]
}
