package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/extension"
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
		"arguments passed on to it, e.g. `cg start --resume <id>`. An installed\n" +
		"extension can contribute a launcher that wraps the agent (e.g. in tmux).\n\n" +
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
	for _, a := range agent {
		if strings.TrimSpace(a) == "" {
			return cgerr.New(cgerr.ExitUsage, "cg start: session.agent in %s has an empty element: %q", filepath.Join(root, ".cg.json"), agent)
		}
	}
	path, err := exec.LookPath(agent[0])
	if err != nil {
		return cgerr.New(cgerr.ExitUsage,
			"cg start: %q not found on PATH (the agent comes from session.agent in .cg.json, default %q)",
			agent[0], workspace.DefaultAgentCommand)
	}
	argv := append(append([]string{agent[0]}, agent[1:]...), extra...)
	l, err := findLauncher(root, cfg)
	if err != nil {
		return err
	}
	if l == nil {
		return runAgent(path, argv)
	}
	// The launcher gets its own args, "--", then the agent argv as configured
	// (agent[0] as written, not the resolved path, so it can name sessions).
	largv := append(append(append([]string{l.bin}, l.args...), "--"), argv...)
	return runAgent(l.bin, largv)
}

// launcher is an installed extension's launcher contribution, resolved.
type launcher struct {
	ext  string
	bin  string
	args []string
}

// findLauncher returns the launcher contributed by the workspace's installed
// extensions, nil when none contributes one. Two or more is an error naming
// them: which one wraps the agent must be a choice, not install order. A
// launcher whose binary is missing (an extension tree that is gone) is skipped
// with a warning, so the session still starts.
func findLauncher(root string, cfg *workspace.Config) (*launcher, error) {
	var found []launcher
	for i := range cfg.Extensions {
		e := &cfg.Extensions[i]
		led, err := extension.LoadLedger(root, e.Name)
		if err != nil || led == nil || len(led.Launcher) == 0 {
			continue
		}
		sub, err := extension.CleanSubdir(e.Path)
		if err != nil {
			continue
		}
		lc := led.Launcher[0]
		bin := filepath.Join(extension.SourceRoot(e), sub, "bin", lc.Binary)
		if info, serr := os.Stat(bin); serr != nil || info.IsDir() {
			_, _ = fmt.Fprintf(os.Stderr, "cg start: extension %s's launcher %s is missing; starting the agent directly (cg extension update %s restores it)\n", e.Name, bin, e.Name)
			continue
		}
		found = append(found, launcher{ext: e.Name, bin: bin, args: lc.Args})
	}
	switch len(found) {
	case 0:
		return nil, nil
	case 1:
		return &found[0], nil
	}
	names := make([]string, len(found))
	for i, l := range found {
		names[i] = l.ext
	}
	return nil, cgerr.New(cgerr.ExitUsage, "cg start: more than one installed extension contributes a launcher (%s); remove all but one", strings.Join(names, ", "))
}
