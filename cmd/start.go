package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
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
		"extension can contribute a launcher that wraps the agent (e.g. in tmux).\n" +
		"Installed extensions' commands are appended to the session's PATH, so\n" +
		"`cg <namespace>` keeps working after the session leaves the workspace.\n\n" +
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

// agentRun is what `cg start` runs: the program path and its argv, the
// extension whose launcher wraps the agent ("" when none does), and the
// extensions' bin directories to append to the session's PATH.
type agentRun struct {
	path     string
	argv     []string
	launcher string
	binDirs  []string
}

// exec runs it. A launcher that fails to start is named in the error; an exit
// code passed through from the agent or launcher (a CodedError without a
// message) is returned as is, so cg prints nothing of its own.
//
// Before it starts, the extensions' bin directories are appended to PATH, so
// every process of the session (the agent, its shell commands, the runs it
// starts) finds `cg <namespace>` through the cg-<namespace> PATH fallback even
// after leaving the workspace, e.g. to run in another repository. Appended, not
// prepended: nothing in an extension's bin/ can shadow a command the user
// already has. A launcher passes PATH on to the agent (tmux takes it from the
// client that creates the session). An unset PATH stays unset: setting it to
// just these directories would drop the system's default search path.
func (r *agentRun) exec() error {
	if cur, set := os.LookupEnv("PATH"); set && len(r.binDirs) > 0 {
		if err := os.Setenv("PATH", appendPathDirs(cur, r.binDirs)); err != nil {
			return fmt.Errorf("cg start: set PATH: %w", err)
		}
	}
	err := runAgent(r.path, r.argv)
	if err == nil || r.launcher == "" {
		return err
	}
	if isExitCode(err) {
		return err
	}
	return fmt.Errorf("cg start: launcher of extension %s (%s): %w", r.launcher, r.path, err)
}

// resolveAgent works out what `cg start` runs for the workspace at root with
// extra appended: the agent from session.agent, wrapped by an installed
// extension's launcher when there is one. An error means nothing can be
// started (bare `cg` then falls back to help).
func resolveAgent(root string, extra []string) (agentRun, error) {
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
	dirs := extensionBinDirs(root, cfg)
	if l == nil {
		return agentRun{path: path, argv: argv, binDirs: dirs}, nil
	}
	// The launcher gets its own args, "--", then the agent argv as configured
	// (agent[0] as written, not the resolved path, so it can name sessions).
	largv := append(append(append([]string{l.bin}, l.args...), "--"), argv...)
	return agentRun{path: l.bin, argv: largv, launcher: l.ext, binDirs: dirs}, nil
}

// trustedManifest loads extension e's own manifest from its tree at the
// recorded commit and validates it. It returns the extension's directory in the
// tree. The ledger under .cg/ext/ is committed in the workspace, so whatever it
// claims is checked against this manifest before cg runs anything from it.
func trustedManifest(e *workspace.ExtensionRef) (dir string, m *extension.Manifest, reason string) {
	sub, err := extension.CleanSubdir(e.Path)
	if err != nil {
		return "", nil, fmt.Sprintf("invalid path %q", e.Path)
	}
	dir = filepath.Join(extension.SourceRoot(e), sub)
	m, err = extension.LoadManifest(dir)
	if err == nil {
		err = m.Validate()
	}
	if err != nil {
		return "", nil, fmt.Sprintf("can't read its manifest (%v); `cg init` restores the recorded version", err)
	}
	return dir, m, ""
}

// extensionBinDirs returns the bin directories of the installed extensions that
// contribute a subcommand, for exec to append to PATH. An extension counts only
// when its own manifest (trustedManifest) declares a subcommand its ledger
// lists, and that binary exists. Anything else is skipped with a warning; the
// session starts either way, without that extension's commands outside the
// workspace.
func extensionBinDirs(root string, cfg *workspace.Config) []string {
	if cfg == nil {
		return nil // a legacy workspace has no extensions
	}
	var dirs []string
	for i := range cfg.Extensions {
		e := &cfg.Extensions[i]
		led, err := extension.LoadLedger(root, e.Name)
		if err != nil || led == nil || len(led.Subcommands) == 0 {
			continue
		}
		warn := func(format string, a ...any) {
			_, _ = fmt.Fprintf(os.Stderr, "cg start: extension %s: "+format+"; its commands won't be on the session's PATH\n", append([]any{e.Name}, a...)...)
		}
		dir, m, reason := trustedManifest(e)
		if reason != "" {
			// findLauncher has already reported the same reason for an
			// extension that also has a launcher.
			if len(led.Launcher) == 0 {
				warn("%s", reason)
			}
			continue
		}
		bin := filepath.Join(dir, "bin")
		ok := false
		for _, sc := range led.Subcommands {
			// Only a binary named cg-<namespace> is reachable through the PATH
			// fallback; adding the directory for another name would expose
			// its files without making the command work.
			if sc.Binary != "cg-"+sc.Namespace || !slices.Contains(m.Contributes.Subcommands, sc) {
				continue
			}
			if info, serr := os.Stat(filepath.Join(bin, sc.Binary)); serr == nil && !info.IsDir() {
				ok = true
				break
			}
		}
		if !ok {
			warn("no subcommand in .cg/ext/%s.json is declared by the extension's manifest as bin/cg-<namespace> with the binary present", e.Name)
			continue
		}
		dirs = append(dirs, bin)
	}
	return dirs
}

// appendPathDirs appends dirs to the PATH value cur, skipping any already in it
// (a `cg start` inside a session that cg started) or repeated.
func appendPathDirs(cur string, dirs []string) string {
	var parts []string
	if cur != "" {
		parts = filepath.SplitList(cur)
	}
	seen := make(map[string]bool, len(parts)+len(dirs))
	for _, p := range parts {
		seen[pathKey(p)] = true
	}
	for _, d := range dirs {
		if k := pathKey(d); !seen[k] {
			seen[k] = true
			parts = append(parts, d)
		}
	}
	return strings.Join(parts, string(filepath.ListSeparator))
}

// pathKey is how appendPathDirs compares PATH entries: cleaned, and without
// case on Windows, where paths are case-insensitive.
func pathKey(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(filepath.Clean(p))
	}
	return filepath.Clean(p)
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
		dir, m, reason := trustedManifest(e)
		if reason != "" {
			warn("%s", reason)
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
