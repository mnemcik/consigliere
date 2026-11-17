package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// installSubcommandExt records the extension live-runs in the workspace at ws with
// ledgerSubs in its ledger and writes its tree: a manifest declaring
// manifestSubs and the files in binaries under bin/. It returns the tree's bin
// directory.
func installSubcommandExt(t *testing.T, ws string, ledgerSubs, manifestSubs []extension.SubcommandContribution, binaries ...string) string {
	t.Helper()
	const name = "live-runs"
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
	led := &extension.Ledger{Name: name, Version: "0.1.0", Subcommands: ledgerSubs}
	if err := led.Save(ws); err != nil {
		t.Fatal(err)
	}
	tree := extension.SourceRoot(ref)
	bin := filepath.Join(tree, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	m := extension.Manifest{Manifest: extension.SchemaVersion, Name: name, Version: "0.1.0", Description: "d"}
	m.Contributes.Subcommands = manifestSubs
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, extension.ManifestFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, b := range binaries {
		if err := os.WriteFile(filepath.Join(bin, b), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return bin
}

// stubPathAtStart replaces the agent exec with one that records PATH as the
// agent would see it. It sets PATH to a known value for the test with
// t.Setenv, which also restores it: exec sets PATH for real, so any test that
// runs `cg start` with a subcommand-contributing extension must go through
// this (or t.Setenv("PATH", …)) to keep PATH from leaking into later tests.
func stubPathAtStart(t *testing.T, path string) *string {
	t.Helper()
	t.Setenv("PATH", path)
	newStubStart(t, true)
	var seen string
	runAgent = func(string, []string) error {
		seen = os.Getenv("PATH")
		return nil
	}
	return &seen
}

var liveSub = []extension.SubcommandContribution{{Namespace: "live", Binary: "cg-live"}}

func TestStartAppendsExtensionBinDirsToPath(t *testing.T) {
	ws, _ := launcherWorkspace(t)
	bin := installSubcommandExt(t, ws, liveSub, liveSub, "cg-live", "cg-live-tee")
	orig := t.TempDir()
	seen := stubPathAtStart(t, orig)

	if err := startCmd.RunE(startCmd, nil); err != nil {
		t.Fatal(err)
	}
	if want := orig + string(filepath.ListSeparator) + bin; *seen != want {
		t.Errorf("agent's PATH = %q, want %q (appended, after the user's own)", *seen, want)
	}
}

// The ledger is committed in the workspace, so it can't put a directory on
// PATH that the extension's own manifest doesn't back.
func TestStartLeavesPathAloneForUntrustedSubcommands(t *testing.T) {
	other := []extension.SubcommandContribution{{Namespace: "live", Binary: "cg-other"}}
	for _, tc := range []struct {
		name             string
		ledger, manifest []extension.SubcommandContribution
		binaries         []string
	}{
		{"manifest declares none", liveSub, nil, []string{"cg-live"}},
		{"different binary", liveSub, other, []string{"cg-live", "cg-other"}},
		{"binary missing", liveSub, liveSub, nil},
		// Declared, present, but not reachable through the cg-<namespace>
		// fallback, so its directory must not be exposed.
		{"binary not named cg-<namespace>", []extension.SubcommandContribution{{Namespace: "live", Binary: "live-runner"}},
			[]extension.SubcommandContribution{{Namespace: "live", Binary: "live-runner"}}, []string{"live-runner"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, _ := launcherWorkspace(t)
			installSubcommandExt(t, ws, tc.ledger, tc.manifest, tc.binaries...)
			orig := t.TempDir()
			seen := stubPathAtStart(t, orig)

			if err := startCmd.RunE(startCmd, nil); err != nil {
				t.Fatal(err)
			}
			if *seen != orig {
				t.Errorf("agent's PATH = %q, want it unchanged (%q)", *seen, orig)
			}
		})
	}
}

func TestStartWithoutSubcommandsLeavesPathAlone(t *testing.T) {
	ws, _ := launcherWorkspace(t)
	installLauncher(t, ws, "tmux", "cg-tmux", "cg-tmux", nil, true)
	orig := t.TempDir()
	seen := stubPathAtStart(t, orig)

	if err := startCmd.RunE(startCmd, nil); err != nil {
		t.Fatal(err)
	}
	if *seen != orig {
		t.Errorf("agent's PATH = %q, want it unchanged (%q)", *seen, orig)
	}
}

func TestAppendPathDirs(t *testing.T) {
	sep := string(filepath.ListSeparator)
	a, b, c := filepath.Join("x", "a"), filepath.Join("x", "b"), filepath.Join("x", "c")
	cases := []struct {
		name, cur string
		dirs      []string
		want      string
	}{
		{"empty PATH", "", []string{a}, a},
		{"appended", a, []string{b}, a + sep + b},
		{"already there (cg start inside a cg session)", a + sep + b, []string{b}, a + sep + b},
		{"unclean duplicate", a + sep + b + string(filepath.Separator), []string{b}, a + sep + b + string(filepath.Separator)},
		{"repeated in dirs", a, []string{b, c, b}, a + sep + b + sep + c},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases, struct {
			name, cur string
			dirs      []string
			want      string
		}{"case differs (Windows)", `C:\X\Bin`, []string{`c:\x\bin`}, `C:\X\Bin`})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := appendPathDirs(tc.cur, tc.dirs); got != tc.want {
				t.Errorf("appendPathDirs(%q, %q) = %q, want %q", tc.cur, tc.dirs, got, tc.want)
			}
		})
	}
}

// What the PATH entry is for: from a directory outside the workspace, `cg live`
// resolves through the cg-<namespace> PATH fallback.
func TestExtensionCommandResolvesOutsideWorkspaceAfterStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("extension subcommands don't run on Windows yet (#239)")
	}
	ws, _ := launcherWorkspace(t)
	bin := installSubcommandExt(t, ws, liveSub, liveSub, "cg-live")
	stubPathAtStart(t, t.TempDir())
	if err := startCmd.RunE(startCmd, nil); err != nil {
		t.Fatal(err)
	}

	t.Chdir(t.TempDir()) // e.g. another repository
	if root := workspaceRootForDispatch(); root != "" {
		t.Fatalf("test dir is inside a workspace: %s", root)
	}
	got, ok := resolveExternalSubcommand("", "live")
	if want := filepath.Join(bin, "cg-live"); !ok || got != want {
		t.Errorf("resolveExternalSubcommand(live) = %q, %v; want %q", got, ok, want)
	}
}

func TestUnknownCommandHintOnlyOutsideWorkspace(t *testing.T) {
	unknown := errors.New(`unknown command "live" for "cg"`)
	if got := unknownCommandHint(unknown, ""); got != extensionCommandHint {
		t.Errorf("outside a workspace: got %q", got)
	}
	if got := unknownCommandHint(unknown, "/ws"); got != "" {
		t.Errorf("inside a workspace: want no hint, got %q", got)
	}
	if got := unknownCommandHint(errors.New("other failure"), ""); got != "" {
		t.Errorf("other error: want no hint, got %q", got)
	}
	if got := unknownCommandHint(nil, ""); got != "" {
		t.Errorf("no error: want no hint, got %q", got)
	}
	if !strings.Contains(extensionCommandHint, "`cg`") {
		t.Error("hint should name `cg` as the way to start a session")
	}
}

// An unset PATH must stay unset: setting it to just the extension dirs would
// drop the system's default search path.
func TestStartLeavesUnsetPathUnset(t *testing.T) {
	ws, _ := launcherWorkspace(t)
	installSubcommandExt(t, ws, liveSub, liveSub, "cg-live")
	t.Setenv("PATH", "")
	if err := os.Unsetenv("PATH"); err != nil {
		t.Fatal(err)
	}
	newStubStart(t, true)
	var set bool
	runAgent = func(string, []string) error {
		_, set = os.LookupEnv("PATH")
		return nil
	}
	if err := startCmd.RunE(startCmd, nil); err != nil {
		t.Fatal(err)
	}
	if set {
		t.Error("PATH was unset and must stay unset")
	}
}

// captureStderr runs f with os.Stderr redirected and returns what it wrote.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()
	f()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// An extension with both a launcher and a subcommand whose tree is missing (a
// fresh clone before `cg init`) is reported once, not once per contribution.
func TestMissingTreeWarnsOnce(t *testing.T) {
	ws, exe := launcherWorkspace(t)
	ref := &workspace.ExtensionRef{Name: "both", Version: "0.1.0", Source: "direct",
		Repo: "https://example.com/both", Commit: "0123456789abcdef0123456789abcdef01234567"}
	cfg, err := workspace.Detect(ws)
	if err != nil {
		t.Fatal(err)
	}
	cfg.UpsertExtension(ref)
	if err := cfg.Save(ws); err != nil {
		t.Fatal(err)
	}
	led := &extension.Ledger{Name: "both", Version: "0.1.0", Subcommands: liveSub,
		Launcher: []extension.LauncherContribution{{Binary: "cg-tmux"}}}
	if err := led.Save(ws); err != nil {
		t.Fatal(err)
	}
	stubPathAtStart(t, t.TempDir())

	var runErr error
	stderr := captureStderr(t, func() { runErr = startCmd.RunE(startCmd, nil) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	if n := strings.Count(stderr, "can't read its manifest"); n != 1 {
		t.Errorf("want the missing tree reported once, got %d times:\n%s", n, stderr)
	}
	_ = exe
}
