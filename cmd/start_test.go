package cmd

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// stubStart replaces the terminal check and the agent exec for one test and
// records what would have run.
type stubStart struct {
	called bool
	path   string
	argv   []string
}

func newStubStart(t *testing.T, interactive bool) *stubStart {
	t.Helper()
	s := &stubStart{}
	oldTTY, oldRun := isInteractive, runAgent
	isInteractive = func() bool { return interactive }
	runAgent = func(path string, argv []string) error {
		s.called, s.path, s.argv = true, path, argv
		return nil
	}
	t.Cleanup(func() { isInteractive, runAgent = oldTTY, oldRun })
	return s
}

// workspaceWithAgent makes a workspace whose session.agent is agent and makes
// it the working directory.
func workspaceWithAgent(t *testing.T, agent []string) {
	t.Helper()
	ws := newWorkspace(t)
	if agent != nil {
		cfg, err := workspace.Detect(ws)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Session = &workspace.SessionConfig{Agent: agent}
		if err := cfg.Save(ws); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(ws)
}

// testAgent is an executable that exists on every platform: the test binary.
func testAgent(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestSessionSettingsDefaultAgent(t *testing.T) {
	var c *workspace.Config
	if got := c.SessionSettings().Agent; !reflect.DeepEqual(got, []string{"claude"}) {
		t.Errorf("default agent = %q, want [claude]", got)
	}
	c = &workspace.Config{Session: &workspace.SessionConfig{Agent: []string{"codex", "--x"}}}
	if got := c.SessionSettings().Agent; !reflect.DeepEqual(got, []string{"codex", "--x"}) {
		t.Errorf("configured agent = %q", got)
	}
}

func TestStartRunsConfiguredAgentWithArgs(t *testing.T) {
	exe := testAgent(t)
	workspaceWithAgent(t, []string{exe, "--flag"})
	s := newStubStart(t, true)

	if err := startCmd.RunE(startCmd, []string{"--resume", "abc"}); err != nil {
		t.Fatal(err)
	}
	if !s.called || s.path != exe {
		t.Fatalf("agent not run with %q: %+v", exe, s)
	}
	if want := []string{exe, "--flag", "--resume", "abc"}; !reflect.DeepEqual(s.argv, want) {
		t.Errorf("argv = %q, want %q", s.argv, want)
	}
}

func TestBareCgInWorkspaceWithTerminalStartsAgent(t *testing.T) {
	exe := testAgent(t)
	workspaceWithAgent(t, []string{exe})
	s := newStubStart(t, true)

	if err := runRoot(rootCmd, nil); err != nil {
		t.Fatal(err)
	}
	if !s.called || !reflect.DeepEqual(s.argv, []string{exe}) {
		t.Errorf("bare cg should start the agent: %+v", s)
	}
}

func TestBareCgWithoutTerminalPrintsHelp(t *testing.T) {
	workspaceWithAgent(t, []string{testAgent(t)})
	s := newStubStart(t, false)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	t.Cleanup(func() { rootCmd.SetOut(nil) })

	if err := runRoot(rootCmd, nil); err != nil {
		t.Fatal(err)
	}
	if s.called {
		t.Error("agent started without a terminal")
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("want help, got %q", out.String())
	}
}

func TestBareCgOutsideWorkspacePrintsHelp(t *testing.T) {
	t.Chdir(t.TempDir())
	s := newStubStart(t, true)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	t.Cleanup(func() { rootCmd.SetOut(nil) })

	if err := runRoot(rootCmd, nil); err != nil {
		t.Fatal(err)
	}
	if s.called || !strings.Contains(out.String(), "Usage:") {
		t.Errorf("outside a workspace bare cg must print help: called=%v out=%q", s.called, out.String())
	}
}

func TestStartOutsideWorkspaceFails(t *testing.T) {
	t.Chdir(t.TempDir())
	newStubStart(t, true)
	err := startCmd.RunE(startCmd, nil)
	var ce *cgerr.CodedError
	if !errors.As(err, &ce) || ce.Code != cgerr.ExitUsage || !strings.Contains(err.Error(), "not inside a Consigliere workspace") {
		t.Errorf("want ExitUsage 'not inside', got %v", err)
	}
}

func TestStartAgentNotFound(t *testing.T) {
	workspaceWithAgent(t, []string{"cg-test-no-such-agent-xyz"})
	s := newStubStart(t, true)
	err := startCmd.RunE(startCmd, nil)
	var ce *cgerr.CodedError
	if !errors.As(err, &ce) || ce.Code != cgerr.ExitUsage || !strings.Contains(err.Error(), "session.agent") {
		t.Errorf("want ExitUsage naming session.agent, got %v", err)
	}
	if s.called {
		t.Error("agent must not run when it is not found")
	}
}

func TestStartRejectsEmptyProgramName(t *testing.T) {
	workspaceWithAgent(t, []string{" ", "x"})
	s := newStubStart(t, true)
	if err := startCmd.RunE(startCmd, nil); err == nil || !strings.Contains(err.Error(), "empty program name") {
		t.Errorf("want empty-program error, got %v", err)
	}
	if s.called {
		t.Error("agent must not run with an invalid session.agent")
	}
}

func TestStartKeepsEmptyArguments(t *testing.T) {
	exe := testAgent(t)
	workspaceWithAgent(t, []string{exe, "wrapper.js", ""})
	s := newStubStart(t, true)
	if err := startCmd.RunE(startCmd, []string{""}); err != nil {
		t.Fatal(err)
	}
	if want := []string{exe, "wrapper.js", "", ""}; !reflect.DeepEqual(s.argv, want) {
		t.Errorf("argv = %q, want %q", s.argv, want)
	}
}

func TestLeadingRootFlagsBeforeStart(t *testing.T) {
	exe := testAgent(t)
	workspaceWithAgent(t, []string{exe})
	s := newStubStart(t, true)
	t.Cleanup(func() {
		noAutoUpdate = false
		rootCmd.SetArgs(nil)
	})

	args, err := argsForStart([]string{"--no-auto-update", "start", "foo", "--no-auto-update"})
	if err != nil {
		t.Fatal(err)
	}
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !noAutoUpdate {
		t.Error("--no-auto-update before start was not applied")
	}
	// Flags after `start` belong to the agent, even ones named like cg's.
	if want := []string{exe, "foo", "--no-auto-update"}; !reflect.DeepEqual(s.argv, want) {
		t.Errorf("argv = %q, want %q", s.argv, want)
	}
}

func TestArgsForStartLeavesOtherCommandsAlone(t *testing.T) {
	for _, args := range [][]string{
		{"--no-auto-update", "status"},
		{"start", "--x"},
		{"--no-auto-update"},
		{},
	} {
		if got, err := argsForStart(args); err != nil || !reflect.DeepEqual(got, args) {
			t.Errorf("argsForStart(%q) = %q, %v; want unchanged", args, got, err)
		}
	}
}

func TestArgsForStartRejectsBadLeadingFlags(t *testing.T) {
	t.Cleanup(func() { noAutoUpdate = false })
	for _, args := range [][]string{
		{"--no-auto-update=invalid", "start", "prompt"},
		{"--bogus", "start"},
	} {
		got, err := argsForStart(args)
		var ce *cgerr.CodedError
		if !errors.As(err, &ce) || ce.Code != cgerr.ExitUsage {
			t.Errorf("argsForStart(%q) = %q, %v; want an ExitUsage error, nothing forwarded", args, got, err)
		}
	}
}

// executeRoot runs args through cobra (as main does after argsForStart) and
// returns what cg printed and the error Execute returned.
func executeRoot(t *testing.T, args []string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
		rootCmd.SilenceErrors, rootCmd.SilenceUsage = false, false
		startCmd.SilenceErrors, startCmd.SilenceUsage = false, false
		noAutoUpdate = false
		for _, name := range []string{"version", cmdHelp} {
			if f := rootCmd.Flags().Lookup(name); f != nil {
				_ = f.Value.Set("false")
				f.Changed = false
			}
		}
	})
	err = rootCmd.Execute()
	return out.String(), errOut.String(), err
}

func TestBareCgWithoutAgentFallsBackToHelp(t *testing.T) {
	workspaceWithAgent(t, []string{"cg-test-no-such-agent-xyz"})
	s := newStubStart(t, true)

	stdout, stderr, err := executeRoot(t, []string{"--no-auto-update"})
	if err != nil {
		t.Fatalf("bare cg must exit 0 when the agent is missing, got %v", err)
	}
	if s.called {
		t.Error("no agent may start")
	}
	if !strings.Contains(stdout, "Usage:") {
		t.Errorf("want help on stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, noAgentHint) || strings.Contains(stderr, "Error:") {
		t.Errorf("want only the hint on stderr, got %q", stderr)
	}
}

func TestStartWithoutAgentErrorsWithoutUsage(t *testing.T) {
	workspaceWithAgent(t, []string{"cg-test-no-such-agent-xyz"})
	newStubStart(t, true)

	stdout, stderr, err := executeRoot(t, []string{"start"})
	var ce *cgerr.CodedError
	if !errors.As(err, &ce) || ce.Code != cgerr.ExitUsage {
		t.Fatalf("want ExitUsage, got %v", err)
	}
	if !strings.Contains(stderr, "Error: cg start:") {
		t.Errorf("want the error on stderr, got %q", stderr)
	}
	if strings.Contains(stdout+stderr, "Usage:") {
		t.Errorf("no usage text after the error; stdout %q stderr %q", stdout, stderr)
	}
}

// On Windows execAgent returns the agent's exit code as a CodedError with no
// message; cg must pass the code on without printing anything of its own.
func TestAgentExitCodePrintsNothing(t *testing.T) {
	for _, args := range [][]string{{"start"}, {"--no-auto-update"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			workspaceWithAgent(t, []string{testAgent(t)})
			newStubStart(t, true)
			runAgent = func(string, []string) error { return cgerr.New(3, "") }

			stdout, stderr, err := executeRoot(t, args)
			var ce *cgerr.CodedError
			if !errors.As(err, &ce) || ce.Code != 3 {
				t.Fatalf("want exit code 3 passed on, got %v", err)
			}
			if stdout != "" || stderr != "" {
				t.Errorf("cg must print nothing; stdout %q stderr %q", stdout, stderr)
			}
		})
	}
}

func TestBareCgFallsBackWhenTheAgentFailsToStart(t *testing.T) {
	workspaceWithAgent(t, []string{testAgent(t)})
	newStubStart(t, true)
	runAgent = func(string, []string) error { return errors.New("fork/exec: no such file or directory") }

	stdout, stderr, err := executeRoot(t, []string{"--no-auto-update"})
	if err != nil || !strings.Contains(stdout, "Usage:") || !strings.Contains(stderr, noAgentHint) {
		t.Errorf("bare cg: err %v stdout %q stderr %q", err, stdout, stderr)
	}
	// cg start keeps the error.
	_, stderr, err = executeRoot(t, []string{"start"})
	if err == nil || !strings.Contains(stderr, "no such file") || strings.Contains(stderr, "Usage:") {
		t.Errorf("cg start: err %v stderr %q", err, stderr)
	}
}

func TestLeadingHelpBeforeStartIsAHelpRequest(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		args := []string{flag, "start"}
		got, err := argsForStart(args)
		if err != nil || !reflect.DeepEqual(got, []string{"help", "start"}) {
			t.Fatalf("argsForStart(%q) = %q, %v; want [help start]", args, got, err)
		}
		workspaceWithAgent(t, []string{testAgent(t)})
		s := newStubStart(t, true)
		stdout, _, err := executeRoot(t, got)
		if err != nil || s.called || !strings.Contains(stdout, "cg start [agent args") {
			t.Errorf("%s start: want help and no agent; err %v called %v stdout %.60q", flag, err, s.called, stdout)
		}
	}
}

func TestLeadingVersionBeforeStartPrintsTheVersion(t *testing.T) {
	for _, flag := range []string{"--version", "-v"} {
		got, err := argsForStart([]string{"--no-auto-update", flag, "start", "x"})
		if err != nil || !reflect.DeepEqual(got, []string{"--version"}) {
			t.Fatalf("argsForStart(%s start) = %q, %v; want [--version]", flag, got, err)
		}
		workspaceWithAgent(t, []string{testAgent(t)})
		s := newStubStart(t, true)
		stdout, _, err := executeRoot(t, got)
		if err != nil || s.called || !strings.Contains(stdout, "version") {
			t.Errorf("%s start: want the version and no agent; err %v called %v stdout %q", flag, err, s.called, stdout)
		}
	}
}
