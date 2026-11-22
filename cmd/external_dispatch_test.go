package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// dispatchWorkspace installs extensions declaring commands (name → commands)
// into a fresh workspace with config home cfgHome, makes it the current
// directory, and returns it.
func dispatchWorkspace(t *testing.T, cfgHome string, exts ...[]string) string {
	t.Helper()
	ws := newWorkspace(t)
	for _, e := range exts {
		installRepo(t, cfgHome, ws, commandsManifest(e[0], "1.0.0", e[1:]...))
	}
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Chdir(ws)
	return ws
}

// pathProgram puts an executable cg-<ns> in a fresh directory at the front of
// PATH (git stays reachable), so a test can tell whether dispatch reached the
// PATH fallback.
func pathProgram(t *testing.T, ns string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cg-"+ns)
	if runtime.GOOS == "windows" {
		p += ".exe"
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho path\n"), 0o755); err != nil { //nolint:gosec // an executable test fixture
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return p
}

// mustDispatch runs externalDispatch and fails unless it resolves.
func mustDispatch(t *testing.T, args ...string) *externalRun {
	t.Helper()
	run, err := externalDispatch(args)
	if err != nil {
		t.Fatalf("externalDispatch(%q): %v", args, err)
	}
	if run == nil {
		t.Fatalf("externalDispatch(%q) resolved nothing", args)
	}
	return run
}

// dispatchErr runs externalDispatch and fails unless it returns a usage error
// containing every part.
func dispatchErr(t *testing.T, args []string, parts ...string) {
	t.Helper()
	run, err := externalDispatch(args)
	if err == nil {
		t.Fatalf("externalDispatch(%q) = %+v, want an error", args, run)
	}
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitUsage {
		t.Errorf("externalDispatch(%q) error %v is not a usage error", args, err)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("externalDispatch(%q) error %q lacks %q", args, err, p)
		}
	}
}

func binOf(t *testing.T, ws, name string) string {
	t.Helper()
	cfg, err := workspace.Detect(ws)
	if err != nil {
		t.Fatal(err)
	}
	ref := findRef(cfg.Extensions, name)
	if ref == nil {
		t.Fatalf("%s not recorded", name)
	}
	return filepath.Join(extension.SourceRoot(ref), "bin", "cg-"+name)
}

// S15: the longest declared command the typed words start with wins, and the
// binary gets the arguments after the namespace word.
func TestDispatchLongestMatch(t *testing.T) {
	requireGit(t)
	ws := dispatchWorkspace(t, t.TempDir(), []string{"alpha", "shop", "shop add"}, []string{"beta", "pr merge"})
	for _, tc := range []struct {
		args    []string
		command string
		rest    []string
	}{
		{[]string{"shop", "add", "milk"}, "shop add", []string{"add", "milk"}},
		{[]string{"shop", "list"}, "shop", []string{"list"}},
		{[]string{"shop"}, "shop", []string{}},
		{[]string{"--no-auto-update", "shop", "add"}, "shop add", []string{"add"}},
		// Q7: typed words end at the first flag.
		{[]string{"shop", "--x", "add"}, "shop", []string{"--x", "add"}},
		{[]string{"pr", "merge", "--now", "7"}, "pr merge", []string{"merge", "--now", "7"}},
	} {
		run := mustDispatch(t, tc.args...)
		if run.command != tc.command || !slices.Equal(run.args, tc.rest) {
			t.Errorf("%q → command %q args %q, want %q %q", tc.args, run.command, run.args, tc.command, tc.rest)
		}
		want := "alpha"
		if tc.command == "pr merge" {
			want = "beta"
		}
		if run.path != binOf(t, ws, want) {
			t.Errorf("%q → %s, want %s", tc.args, run.path, binOf(t, ws, want))
		}
		if run.root != ws {
			t.Errorf("%q served by %q, want %q", tc.args, run.root, ws)
		}
	}
}

// S8 through dispatch: each command of a shared namespace runs its own
// extension, whichever is listed first.
func TestDispatchSharedNamespace(t *testing.T) {
	requireGit(t)
	ws := dispatchWorkspace(t, t.TempDir(), []string{"alpha", "pr merge"}, []string{"beta", "pr watch"})
	if run := mustDispatch(t, "pr", "watch"); run.path != binOf(t, ws, "beta") {
		t.Errorf("pr watch ran %s", run.path)
	}
	if run := mustDispatch(t, "pr", "merge"); run.path != binOf(t, ws, "alpha") {
		t.Errorf("pr merge ran %s", run.path)
	}
}

// S16, S33: a typed command matching none of the deciding root's entries is a
// usage error listing them, and the PATH fallback is not used.
func TestDispatchUndeclaredCommandInDeclaredNamespace(t *testing.T) {
	requireGit(t)
	dispatchWorkspace(t, t.TempDir(), []string{"alpha", "pr merge", "pr list"})
	pathProgram(t, "pr")
	for _, args := range [][]string{{"pr"}, {"pr", "watch"}, {"pr", "--help"}} {
		dispatchErr(t, args, `"pr"`, "pr merge", "pr list")
	}
}

// ledgerAdd appends entries to an installed extension's ledger, as a
// hand-edited or pulled .cg/ext/<name>.json could.
func ledgerAdd(t *testing.T, ws, name string, entries ...extension.CommandContribution) {
	t.Helper()
	led := ledgerOf(t, ws, name)
	led.Commands = append(led.Commands, entries...)
	if err := led.Save(ws); err != nil {
		t.Fatal(err)
	}
}

// S17, S33: an entry that isn't verified stops dispatch, naming the extension
// and the fix, without falling back to PATH; a shorter verified entry never
// stands in for it.
func TestDispatchStopsOnUnverifiedEntry(t *testing.T) {
	requireGit(t)
	cases := []struct {
		name  string
		setup func(t *testing.T, ws string)
		args  []string
	}{
		{"command not in the manifest", func(t *testing.T, ws string) {
			ledgerAdd(t, ws, "alpha", extension.CommandContribution{Command: "shop add", Binary: "cg-alpha"})
		}, []string{"shop", "add"}},
		{"binary differs", func(t *testing.T, ws string) {
			led := ledgerOf(t, ws, "alpha")
			led.Commands[0].Binary = "cg-other"
			if err := led.Save(ws); err != nil {
				t.Fatal(err)
			}
		}, []string{"shop"}},
		{"manifest doesn't validate", func(t *testing.T, ws string) {
			p := filepath.Join(filepath.Dir(filepath.Dir(binOf(t, ws, "alpha"))), extension.ManifestFile)
			if err := os.WriteFile(p, []byte(`{"manifest": 9}`), 0o644); err != nil {
				t.Fatal(err)
			}
		}, []string{"shop", "x"}},
		{"tree missing", func(t *testing.T, ws string) {
			if err := os.RemoveAll(filepath.Dir(filepath.Dir(binOf(t, ws, "alpha")))); err != nil {
				t.Fatal(err)
			}
		}, []string{"shop"}},
		{"binary missing", func(t *testing.T, ws string) {
			if err := os.Remove(binOf(t, ws, "alpha")); err != nil {
				t.Fatal(err)
			}
		}, []string{"shop"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws := dispatchWorkspace(t, t.TempDir(), []string{"alpha", "shop"})
			tc.setup(t, ws)
			pathProgram(t, "shop")
			dispatchErr(t, tc.args, `"alpha"`, "cg init", "cg extension update alpha")
		})
	}
}

// S18: two ledgers declaring the same command anywhere in the typed namespace
// stop dispatch, whichever command the typed words match.
func TestDispatchStopsOnDuplicateInNamespace(t *testing.T) {
	requireGit(t)
	ws := dispatchWorkspace(t, t.TempDir(), []string{"alpha", "pr merge"}, []string{"beta", "pr watch"})
	ledgerAdd(t, ws, "beta", extension.CommandContribution{Command: "pr merge", Binary: "cg-beta"})
	for _, args := range [][]string{{"pr", "merge"}, {"pr", "watch"}, {"pr"}} {
		dispatchErr(t, args, `"pr merge"`, `"alpha"`, `"beta"`)
	}
	// Another namespace is unaffected.
	ledgerAdd(t, ws, "beta", extension.CommandContribution{Command: "shop", Binary: "cg-beta"})
	updateManifestInTree(t, ws, "beta", commandsManifest("beta", "1.0.0", "pr watch", "shop"))
	if run := mustDispatch(t, "shop"); run.command != "shop" || run.path != binOf(t, ws, "beta") {
		t.Errorf("shop = %+v, want beta's shop", run)
	}
}

// S32: an undeclared namespace runs cg-<namespace> from PATH with the
// arguments after the namespace.
func TestDispatchPathFallback(t *testing.T) {
	requireGit(t)
	dispatchWorkspace(t, t.TempDir(), []string{"alpha", "shop"})
	p := pathProgram(t, "greet")
	run := mustDispatch(t, "greet", "a", "--b")
	if run.path != p || run.command != "" || run.root != "" || !slices.Equal(run.args, []string{"a", "--b"}) {
		t.Errorf("fallback run = %+v, want %s with [a --b]", run, p)
	}
	// Outside any workspace too (S19).
	t.Chdir(t.TempDir())
	if run := mustDispatch(t, "greet"); run.path != p {
		t.Errorf("fallback outside a workspace = %+v", run)
	}
	// Nothing resolves: no run, no error (cobra reports the unknown command).
	if run, err := externalDispatch([]string{"nope"}); run != nil || err != nil {
		t.Errorf("unresolved = %+v, %v", run, err)
	}
}

// U1: built-ins win over a declared command, even one a ledger was edited to
// claim.
func TestDispatchBuiltinsWin(t *testing.T) {
	requireGit(t)
	ws := dispatchWorkspace(t, t.TempDir(), []string{"alpha", "shop"})
	ledgerAdd(t, ws, "alpha", extension.CommandContribution{Command: "version", Binary: "cg-alpha"})
	for _, args := range [][]string{{"version"}, {"extension", "list"}, {"help"}, {"--help"}, {}} {
		if run, err := externalDispatch(args); run != nil || err != nil {
			t.Errorf("externalDispatch(%q) = %+v, %v; built-ins must win", args, run, err)
		}
	}
}

// S30: dispatch never reads the per-run variables.
func TestDispatchIgnoresPerRunVariables(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	other := dispatchWorkspace(t, cfgHome, []string{"beta", "shop"})
	ws := dispatchWorkspace(t, cfgHome, []string{"alpha", "shop"})
	for _, v := range []string{"CG_EXTENSION_WORKSPACE", "CG_SESSION_WORKTREE"} {
		t.Setenv(v, other)
	}
	t.Setenv("CG_COMMAND", "shop add")
	run := mustDispatch(t, "shop", "add")
	if run.root != ws || run.command != "shop" || run.path != binOf(t, ws, "alpha") {
		t.Errorf("run = %+v, want alpha's shop from %s", run, ws)
	}
}

// envOf returns the value of key in env and whether it is set.
func envOf(env []string, key string) (string, bool) {
	for _, kv := range env {
		// Windows keeps a variable's own spelling (Path), and names are
		// case-insensitive there.
		if k, v, ok := strings.Cut(kv, "="); ok && envKeyEqual(k, key) {
			return v, true
		}
	}
	return "", false
}

// S28 (CG_COMMAND), S29 (CG_COMMAND), U6: a workspace-served binary gets the
// matched command; a PATH binary gets none, even when the caller had one; PATH
// is passed through unchanged.
func TestExternalCommandEnvironment(t *testing.T) {
	t.Setenv("CG_COMMAND", "stale")
	t.Setenv("PATH", "/usr/bin")
	served := (&externalRun{path: "/x/cg-live", args: []string{"run"}, command: "live run", root: "/ws"}).cmd()
	if v, ok := envOf(served.Env, "CG_COMMAND"); !ok || v != "live run" {
		t.Errorf("served CG_COMMAND = %q, %v", v, ok)
	}
	fallback := (&externalRun{path: "/x/cg-greet"}).cmd()
	if _, ok := envOf(fallback.Env, "CG_COMMAND"); ok {
		t.Error("a PATH binary must not see CG_COMMAND")
	}
	for _, c := range [][]string{served.Env, fallback.Env} {
		if v, _ := envOf(c, "PATH"); v != "/usr/bin" {
			t.Errorf("PATH = %q, want it unchanged", v)
		}
	}
	if !slices.Equal(served.Args, []string{"/x/cg-live", "run"}) {
		t.Errorf("args = %q", served.Args)
	}
}

// S28 end to end: the binary runs with the arguments after the namespace and
// CG_COMMAND set.
func TestDispatchRunsBinary(t *testing.T) {
	requireGit(t)
	if runtime.GOOS == "windows" {
		t.Skip("extension commands don't run on Windows yet (#239)")
	}
	dispatchWorkspace(t, t.TempDir(), []string{"alpha", "live", "live run"})
	run := mustDispatch(t, "live", "run", "--until-pane", "x")
	c := run.cmd()
	var out bytes.Buffer
	c.Stdout = &out
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "live run|run --until-pane x" {
		t.Errorf("binary saw %q", got)
	}
}

// S34: the unknown-command hint says whether CG_WORKSPACE is set and that a
// session started with cg carries its workspace.
func TestUnknownCommandHint(t *testing.T) {
	unknown := errors.New(`unknown command "frob" for "cg"`)
	t.Setenv("CG_WORKSPACE", "")
	os.Unsetenv("CG_WORKSPACE") //nolint:errcheck // t.Setenv restores it
	h := unknownCommandHint(unknown)
	for _, want := range []string{"CG_WORKSPACE is not set", "session started with `cg`"} {
		if !strings.Contains(h, want) {
			t.Errorf("hint %q lacks %q", h, want)
		}
	}
	if h := unknownCommandHint(errors.New("some other error")); h != "" {
		t.Errorf("hint for another error: %q", h)
	}
	if h := unknownCommandHint(nil); h != "" {
		t.Errorf("hint for no error: %q", h)
	}
}

// A co-located extension keeps bin/ under its subdir, next to its manifest;
// dispatch runs that binary, not a same-named one at the tree's root.
func TestDispatchSubdirExtension(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	ws := newWorkspace(t)
	repo := makeMonorepo(t, map[string]string{"shopping": commandsManifest("shopping", "0.1.0", "shop", "shop add")})
	// A binary of the same name at the repo root belongs to no extension.
	commitFile(t, repo, "bin/cg-shopping", "#!/bin/sh\nexit 1\n")
	if out, err := runExtCfg(t, cfgHome, ws, "install", repo, "--path", "shopping"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	cfg, _ := workspace.Detect(ws)
	want := filepath.Join(extension.SourceRoot(findRef(cfg.Extensions, "shopping")), "shopping", "bin", "cg-shopping")
	if run := mustDispatch(t, "shop", "add"); run.path != want {
		t.Errorf("ran %s, want %s", run.path, want)
	}
}

// The binary's exit code becomes cg's, with no message of cg's own.
func TestExternalRunPropagatesExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is POSIX")
	}
	script := filepath.Join(t.TempDir(), "cg-fail")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil { //nolint:gosec // an executable test fixture
		t.Fatal(err)
	}
	err := (&externalRun{path: script}).run()
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != 7 || coded.Error() != "" {
		t.Errorf("run() = %v, want a silent exit code 7", err)
	}
}

// Q7: the typed words end at the first argument starting with "-", even when a
// ledger has such a word (a manifest can't declare one, S2; a hand-edited
// ledger can).
func TestDispatchTypedWordsEndAtFlag(t *testing.T) {
	requireGit(t)
	ws := dispatchWorkspace(t, t.TempDir(), []string{"alpha", "shop"})
	ledgerAdd(t, ws, "alpha", extension.CommandContribution{Command: "shop --all", Binary: "cg-alpha"})
	if run := mustDispatch(t, "shop", "--all"); run.command != "shop" {
		t.Errorf("`cg shop --all` matched %q, want the bare namespace", run.command)
	}
}

// updateManifestInTree rewrites an installed extension's manifest in its
// pinned tree, so a ledger edit can be made to check out.
func updateManifestInTree(t *testing.T, ws, name, manifest string) {
	t.Helper()
	p := filepath.Join(filepath.Dir(filepath.Dir(binOf(t, ws, name))), extension.ManifestFile)
	if err := os.WriteFile(p, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A ledger or .cg.json that can't be read might declare the namespace, so
// dispatch stops instead of handing it to a PATH program.
func TestDispatchUnreadableStateFailsClosed(t *testing.T) {
	requireGit(t)
	t.Run("ledger", func(t *testing.T) {
		ws := dispatchWorkspace(t, t.TempDir(), []string{"alpha", "shop"})
		if err := os.WriteFile(extension.LedgerPath(ws, "alpha"), []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		pathProgram(t, "shop")
		dispatchErr(t, []string{"shop", "add"}, extension.LedgerPath(ws, "alpha"), "restore it")
		// update and remove read the same ledger, so they can't be the fix.
		if _, err := externalDispatch([]string{"shop", "add"}); err != nil &&
			(strings.Contains(err.Error(), "cg extension update") || strings.Contains(err.Error(), "cg extension remove")) {
			t.Errorf("error %q suggests update or remove, which fail on the same ledger", err)
		}
	})
	t.Run(".cg.json", func(t *testing.T) {
		ws := dispatchWorkspace(t, t.TempDir(), []string{"alpha", "shop"})
		if err := os.WriteFile(filepath.Join(ws, workspace.ConfigFile), []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		pathProgram(t, "shop")
		dispatchErr(t, []string{"shop", "add"}, filepath.Join(ws, workspace.ConfigFile))
		// From a subdirectory too: the error names the file, wherever it is.
		sub := filepath.Join(ws, "sub")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(sub)
		dispatchErr(t, []string{"shop", "add"}, filepath.Join(ws, workspace.ConfigFile))
	})
}

// The PATH fallback only looks up a plain name: a namespace that LookPath
// would take as a path (a separator, or a colon on Windows) is never run.
func TestPlainName(t *testing.T) {
	for ns, want := range map[string]bool{
		"greet": true, "gh-dash": true, "Foo_2": true,
		"x/../tool": false, `x\..\tool`: false, "x:tool": false, "": false,
	} {
		if got := plainName(ns); got != want {
			t.Errorf("plainName(%q) = %v, want %v", ns, got, want)
		}
	}
}

func TestDispatchPathFallbackRefusesSeparators(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable-bit fixture is POSIX; TestPlainName covers the Windows forms")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir(filepath.Join(dir, "cg-x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tool"), []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // an executable test fixture
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	if run, err := externalDispatch([]string{"x/../tool"}); run != nil || err != nil {
		t.Errorf("externalDispatch(x/../tool) = %+v, %v; want nothing", run, err)
	}
}

// A broken ledger stops dispatch through its workspace even when a readable
// ledger declares the namespace: it might declare a longer command (S15) or
// the same one (S18).
func TestDispatchUnreadableLedgerStopsDeclaredNamespace(t *testing.T) {
	requireGit(t)
	// beta's verified `pr` next to alpha's `pr merge`, as a pulled .cg.json
	// could leave them (install refuses the pair).
	ws := dispatchWorkspace(t, t.TempDir(), []string{"alpha", "pr merge"}, []string{"beta", "other"})
	ledgerAdd(t, ws, "beta", extension.CommandContribution{Command: "pr", Binary: "cg-beta"})
	updateManifestInTree(t, ws, "beta", commandsManifest("beta", "1.0.0", "other", "pr"))
	if err := os.WriteFile(extension.LedgerPath(ws, "alpha"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"pr", "merge"}, {"pr"}, {"other"}} {
		dispatchErr(t, args, extension.LedgerPath(ws, "alpha"))
	}
}

// With an extension listed twice in .cg.json, dispatch runs a verified entry
// when one of them verifies, as the install-time check counts it.
func TestDispatchDuplicateEntryPrefersVerified(t *testing.T) {
	requireGit(t)
	for _, staleFirst := range []bool{true, false} {
		ws := dispatchWorkspace(t, t.TempDir(), []string{"alpha", "shop"})
		cfg, err := workspace.Detect(ws)
		if err != nil || cfg == nil || len(cfg.Extensions) != 1 {
			t.Fatalf("Detect: %+v, %v", cfg, err)
		}
		stale := cfg.Extensions[0]
		stale.Commit = strings.Repeat("0", 40) // a tree that isn't there
		if staleFirst {
			cfg.Extensions = append([]workspace.ExtensionRef{stale}, cfg.Extensions...)
		} else {
			cfg.Extensions = append(cfg.Extensions, stale)
		}
		if err := cfg.Save(ws); err != nil {
			t.Fatal(err)
		}
		if run := mustDispatch(t, "shop"); run.command != "shop" {
			t.Errorf("staleFirst=%v: shop = %+v", staleFirst, run)
		}
	}
}

// The install-time check refuses when a ledger can't be read: it might
// declare the incoming command.
func TestInstallRefusesWithUnreadableLedger(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	installRepo(t, cfgHome, ws, commandsManifest("alpha", "1.0.0", "shop add"))
	if err := os.WriteFile(extension.LedgerPath(ws, "alpha"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := makeExtRepo(t, commandsManifest("beta", "1.0.0", "shop add"))
	out, err := runExtCfg(t, cfgHome, ws, "install", repo)
	wantConflict(t, err, out, extension.LedgerPath(ws, "alpha"))
}

// Another extension's broken ledger blocks only when something incoming
// declares a command; an incoming extension's own broken ledger always blocks.
func TestUnreadableLedgerInCommandCheck(t *testing.T) {
	requireGit(t)
	ws := newWorkspace(t)
	cfgHome := t.TempDir()
	installRepo(t, cfgHome, ws, commandsManifest("alpha", "1.0.0", "shop add"))
	if err := os.WriteFile(extension.LedgerPath(ws, "alpha"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := workspace.Detect(ws)
	if _, err := checkCommands(ws, cfg.Extensions, map[string][]extension.CommandContribution{"beta": nil}, nil); err != nil {
		t.Errorf("no incoming command: %v", err)
	}
	if _, err := checkCommands(ws, cfg.Extensions, map[string][]extension.CommandContribution{"alpha": nil}, nil); err == nil {
		t.Error("alpha's own broken ledger must block, even with no commands")
	}
	// Replacing alpha itself would fail later, reading the same ledger:
	// refuse up front, naming it.
	_, err := checkCommands(ws, cfg.Extensions, map[string][]extension.CommandContribution{
		"alpha": {{Command: "shop add", Binary: "cg-alpha"}}}, nil)
	if err == nil || !strings.Contains(err.Error(), extension.LedgerPath(ws, "alpha")) {
		t.Errorf("replacing alpha itself: %v", err)
	}
}

// A multi-target update refuses before applying any target when one target's
// ledger can't be read.
func TestUpdateRefusesBeforeApplyingWithUnreadableTargetLedger(t *testing.T) {
	requireGit(t)
	for name, cmds := range map[string][][]string{
		"with commands":    {{"shop"}, {"pr"}},
		"without commands": {nil, nil},
	} {
		t.Run(name, func(t *testing.T) {
			cfgHome, ws := t.TempDir(), newWorkspace(t)
			alpha := installRepo(t, cfgHome, ws, commandsManifest("alpha", "1.0.0", cmds[0]...))
			beta := installRepo(t, cfgHome, ws, commandsManifest("beta", "1.0.0", cmds[1]...))
			updateExtRepo(t, alpha, commandsManifest("alpha", "1.1.0", cmds[0]...), "")
			updateExtRepo(t, beta, commandsManifest("beta", "1.1.0", cmds[1]...), "")
			if err := os.WriteFile(extension.LedgerPath(ws, "beta"), []byte("{"), 0o644); err != nil {
				t.Fatal(err)
			}
			before := snapshotDir(t, ws)
			out, err := runExtCfg(t, cfgHome, ws, "update")
			wantConflict(t, err, out, extension.LedgerPath(ws, "beta"), "restore it")
			assertUnchanged(t, ws, before)
		})
	}
}

// The S17 error names its remedy once.
func TestDispatchUnverifiedErrorNamesFixOnce(t *testing.T) {
	requireGit(t)
	ws := dispatchWorkspace(t, t.TempDir(), []string{"alpha", "shop"})
	if err := os.Remove(filepath.Join(filepath.Dir(filepath.Dir(binOf(t, ws, "alpha"))), extension.ManifestFile)); err != nil {
		t.Fatal(err)
	}
	_, err := externalDispatch([]string{"shop"})
	if err == nil || strings.Count(err.Error(), "cg init") != 1 || !strings.Contains(err.Error(), "cg extension update alpha") {
		t.Errorf("error = %v, want `cg init` named once and `cg extension update alpha`", err)
	}
}

// The usage error lists each declared command once, even when .cg.json lists
// the extension twice.
func TestDispatchUsageListsEachCommandOnce(t *testing.T) {
	requireGit(t)
	ws := dispatchWorkspace(t, t.TempDir(), []string{"alpha", "shop add", "shop list"})
	cfg, _ := workspace.Detect(ws)
	cfg.Extensions = append(cfg.Extensions, cfg.Extensions[0])
	if err := cfg.Save(ws); err != nil {
		t.Fatal(err)
	}
	_, err := externalDispatch([]string{"shop", "zzz"})
	if err == nil || strings.Count(err.Error(), "cg shop add") != 1 {
		t.Errorf("error = %v, want cg shop add listed once", err)
	}
}

// A legacy entry (no recorded commit) reads its name-keyed clone.
func TestDispatchLegacyEntry(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	ws := newWorkspace(t)
	cfg, _ := workspace.Detect(ws)
	cfg.UpsertExtension(&workspace.ExtensionRef{Name: "demo", Version: "1.0.0", Source: workspace.ExtSourceDirect, Repo: "x"})
	if err := cfg.Save(ws); err != nil {
		t.Fatal(err)
	}
	if err := (&extension.Ledger{Name: "demo", Version: "1.0.0",
		Commands: []extension.CommandContribution{{Command: "secret", Binary: "cg-demo"}}}).Save(ws); err != nil {
		t.Fatal(err)
	}
	clone := legacyCloneDir(cfgHome)
	if err := os.MkdirAll(filepath.Join(clone, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := commandsManifest("demo", "1.0.0", "secret")
	if err := os.WriteFile(filepath.Join(clone, extension.ManifestFile), []byte(m), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(clone, "bin", "cg-demo")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // an executable test fixture
		t.Fatal(err)
	}
	t.Chdir(ws)
	if run := mustDispatch(t, "secret", "get"); run.path != bin || run.command != "secret" {
		t.Errorf("legacy run = %+v, want %s", run, bin)
	}
}
