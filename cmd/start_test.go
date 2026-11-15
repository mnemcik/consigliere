package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/extension"
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

func TestStartRejectsEmptyAgentElement(t *testing.T) {
	workspaceWithAgent(t, []string{testAgent(t), " "})
	s := newStubStart(t, true)
	if err := startCmd.RunE(startCmd, nil); err == nil || !strings.Contains(err.Error(), "empty element") {
		t.Errorf("want empty-element error, got %v", err)
	}
	if s.called {
		t.Error("agent must not run with an invalid session.agent")
	}
}

// installLauncher records extension name in the workspace at ws with a launcher
// ledger entry and, when withBinary, drops its binary into the extension tree.
// It returns the binary's path.
func installLauncher(t *testing.T, ws, name string, args []string, withBinary bool) string {
	t.Helper()
	ref := &workspace.ExtensionRef{Name: name, Version: "0.1.0", Source: "direct",
		Repo: "https://example.com/" + name, Commit: "0123456789abcdef0123456789abcdef01234567"}
	cfg, err := workspace.Detect(ws)
	if err != nil {
		t.Fatal(err)
	}
	cfg.UpsertExtension(ref)
	if err := cfg.Save(ws); err != nil {
		t.Fatal(err)
	}
	led := &extension.Ledger{Name: name, Version: "0.1.0",
		Launcher: []extension.LauncherContribution{{Binary: "cg-" + name, Args: args}}}
	if err := led.Save(ws); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(extension.SourceRoot(ref), "bin", "cg-"+name)
	if withBinary {
		if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return bin
}

func TestStartRunsLauncherWithAgent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	exe := testAgent(t)
	workspaceWithAgent(t, []string{exe, "--flag"})
	ws, _ := os.Getwd()
	bin := installLauncher(t, ws, "tmux", []string{"launch"}, true)
	s := newStubStart(t, true)

	if err := startCmd.RunE(startCmd, []string{"--resume", "abc"}); err != nil {
		t.Fatal(err)
	}
	if s.path != bin {
		t.Fatalf("ran %q, want the launcher %q", s.path, bin)
	}
	want := []string{bin, "launch", "--", exe, "--flag", "--resume", "abc"}
	if !reflect.DeepEqual(s.argv, want) {
		t.Errorf("argv = %q, want %q", s.argv, want)
	}
}

func TestStartWithMissingLauncherBinaryStartsAgent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	exe := testAgent(t)
	workspaceWithAgent(t, []string{exe})
	ws, _ := os.Getwd()
	installLauncher(t, ws, "tmux", nil, false)
	s := newStubStart(t, true)

	if err := startCmd.RunE(startCmd, nil); err != nil {
		t.Fatal(err)
	}
	if s.path != exe || !reflect.DeepEqual(s.argv, []string{exe}) {
		t.Errorf("want the agent directly, got %+v", s)
	}
}

func TestStartRefusesTwoLaunchers(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	workspaceWithAgent(t, []string{testAgent(t)})
	ws, _ := os.Getwd()
	installLauncher(t, ws, "tmux", nil, true)
	installLauncher(t, ws, "zellij", nil, true)
	s := newStubStart(t, true)

	err := startCmd.RunE(startCmd, nil)
	var ce *cgerr.CodedError
	if !errors.As(err, &ce) || ce.Code != cgerr.ExitUsage || !strings.Contains(err.Error(), "tmux, zellij") {
		t.Errorf("want ExitUsage naming both extensions, got %v", err)
	}
	if s.called {
		t.Error("nothing may start when two launchers are installed")
	}
}
