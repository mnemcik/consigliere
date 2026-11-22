package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
		"The session gets CG_WORKSPACE set to the workspace root (a worktree's root when\n" +
		"started in one), so extension commands keep working after the session moves to\n" +
		"another directory, such as an external repository.\n\n" +
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
		run, err := resolveAgent(root, args)
		if err != nil {
			cmd.SilenceUsage = true
			return err
		}
		return finishAgent(cmd, run.exec())
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
	rootCmd.RunE = runRoot
}

// noAgentHint is what bare `cg` prints after the help when the workspace's
// agent can't be started; `cg start` shows the reason.
const noAgentHint = "cg: no agent started: run `cg start` to see why (the agent comes from session.agent in .cg.json)"

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
	run, err := resolveAgent(root, nil)
	if err == nil {
		err = run.exec()
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

// agentRun is what `cg start` runs: the program path and its argv, and the
// extension whose launcher wraps the agent ("" when none does).
type agentRun struct {
	path     string
	argv     []string
	launcher string
	// root is the absolute workspace root the session serves: CG_WORKSPACE.
	root string
}

// exec runs it. A launcher that fails to start is named in the error; an exit
// code passed through from the agent or launcher (a CodedError without a
// message) is returned as is, so cg prints nothing of its own.
func (r agentRun) exec() error {
	err := runAgent(r.path, r.argv, sessionEnv(r.root))
	if err == nil || r.launcher == "" {
		return err
	}
	if isExitCode(err) {
		return err
	}
	return fmt.Errorf("cg start: launcher of extension %s (%s): %w", r.launcher, r.path, err)
}

// sessionEnv is the environment of the session `cg start` starts for the
// workspace at root: CG_WORKSPACE set to root, so extension commands find the
// workspace from any directory the session works in, and without the per-run
// variables, which describe one dispatched run and mustn't leak into a
// session started from inside an extension's binary. (A workspace found only
// by its folder markers gets it too, as S31 says; dispatch ignores it there,
// S25, since there is no .cg.json.)
func sessionEnv(root string) []string {
	env := withoutEnv(os.Environ(), append([]string{envWorkspace}, perRunVars...)...)
	return append(env, envWorkspace+"="+root)
}

// resolveAgent works out what `cg start` runs for the workspace at root with
// extra appended: the agent from session.agent, wrapped by an installed
// extension's launcher when there is one. An error means nothing can be
// started (bare `cg` then falls back to help).
func resolveAgent(root string, extra []string) (agentRun, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return agentRun{}, err
	}
	cfg, err := workspace.Detect(root)
	if err != nil {
		return agentRun{}, err
	}
	agent := cfg.SessionSettings().Agent
	// Only the program must be named; later elements are passed verbatim (an
	// empty string is a valid argument).
	if strings.TrimSpace(agent[0]) == "" {
		return agentRun{}, cgerr.New(cgerr.ExitUsage, "cg start: session.agent in %s has an empty program name: %q", filepath.Join(root, ".cg.json"), agent)
	}
	path, err := exec.LookPath(agent[0])
	if err != nil {
		return agentRun{}, cgerr.New(cgerr.ExitUsage,
			"cg start: %q not found on PATH (the agent comes from session.agent in .cg.json, default %q)",
			agent[0], workspace.DefaultAgentCommand)
	}
	argv := append(append([]string{agent[0]}, agent[1:]...), extra...)
	l, err := findLauncher(root, cfg)
	if err != nil {
		return agentRun{}, err
	}
	if l == nil {
		return agentRun{path: path, argv: argv, root: root}, nil
	}
	// The launcher gets its own args, "--", then the agent argv as configured
	// (agent[0] as written, not the resolved path, so it can name sessions).
	largv := append(append(append([]string{l.bin}, l.args...), "--"), argv...)
	return agentRun{path: l.bin, argv: largv, launcher: l.ext, root: root}, nil
}

// launcher is an installed extension's launcher contribution, resolved.
type launcher struct {
	ext  string
	bin  string
	args []string
}

// findLauncher returns the launcher contributed by the workspace's installed
// extensions, nil when none contributes one. The ledger under .cg/ext/ is
// committed in the workspace, so it is not trusted on its own: a launcher runs
// only when the extension's own manifest, in its tree at the recorded commit,
// validates and declares the same launcher. A mismatch, a tree that can't be
// read or a missing binary is skipped with a warning, so the session still
// starts. Two or more launchers is an error naming them: which one wraps the
// agent must be a choice, not install order.
func findLauncher(root string, cfg *workspace.Config) (*launcher, error) {
	if cfg == nil {
		// A legacy workspace (structural markers, no .cg.json) has no
		// extensions, so no launcher: the agent starts directly.
		return nil, nil
	}
	var found []launcher
	for i := range cfg.Extensions {
		e := &cfg.Extensions[i]
		led, err := extension.LoadLedger(root, e.Name)
		if err != nil || led == nil || len(led.Launcher) == 0 {
			continue
		}
		warn := func(format string, a ...any) {
			_, _ = fmt.Fprintf(os.Stderr, "cg start: extension %s: "+format+"; starting the agent without it\n", append([]any{e.Name}, a...)...)
		}
		dir, m, err := verifiedManifest(e)
		if err != nil {
			if errors.Is(err, errBadPath) {
				warn("%v", err)
			} else {
				warn("%v; `cg init` restores the recorded version", err)
			}
			continue
		}
		if len(m.Contributes.Launcher) == 0 || !sameLauncher(led.Launcher[0], m.Contributes.Launcher[0]) {
			warn("the launcher in .cg/ext/%s.json doesn't match the extension's manifest", e.Name)
			continue
		}
		lc := m.Contributes.Launcher[0]
		bin := filepath.Join(dir, "bin", lc.Binary)
		if info, serr := os.Stat(bin); serr != nil || info.IsDir() {
			warn("launcher %s is missing; `cg init` restores the recorded version", bin)
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

// sameLauncher compares two launcher entries, treating absent and empty args
// as equal.
func sameLauncher(a, b extension.LauncherContribution) bool {
	if a.Binary != b.Binary || len(a.Args) != len(b.Args) {
		return false
	}
	return len(a.Args) == 0 || reflect.DeepEqual(a.Args, b.Args)
}

// argsForStart handles root flags given before the `start` verb
// (`cg --no-auto-update start …`). startCmd disables flag parsing so that every
// argument after `start` reaches the agent unchanged, which also leaves such
// leading flags unparsed. argsForStart parses them (root persistent and local
// flags) and returns the arguments without them; a leading flag that doesn't
// parse is an error (it belongs to cg, not the agent). A help request
// (`cg --help start`) becomes `help start`, and `cg --version start` prints the
// version, since handing either to cobra as is would run start and pass the
// flag to the agent. Any other argument list is returned unchanged.
func argsForStart(args []string) ([]string, error) {
	i := 0
	for i < len(args) && args[i] != "" && args[i][0] == '-' {
		i++
	}
	if i == 0 || i == len(args) || args[i] != startCmd.Name() {
		return args, nil
	}
	fs := pflag.NewFlagSet("cg", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.AddFlagSet(rootCmd.PersistentFlags())
	version := fs.BoolP("version", "v", false, "")
	if err := fs.Parse(args[:i]); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return []string{cmdHelp, startCmd.Name()}, nil
		}
		return nil, cgerr.New(cgerr.ExitUsage, "%v", err)
	}
	if *version {
		return []string{"--" + cmdVersion}, nil
	}
	return args[i:], nil
}
