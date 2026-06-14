package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/workspace"
)

func TestResolveExternalSubcommandFromExtension(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	ws := newWorkspace(t)

	// Record an installed extension declaring the `secret` namespace → cg-demo.
	cfg, _ := workspace.Detect(ws)
	cfg.UpsertExtension(&workspace.ExtensionRef{Name: "demo", Version: "1.0.0", Source: "direct", Repo: "x"})
	if err := cfg.Save(ws); err != nil {
		t.Fatal(err)
	}
	led := &extension.Ledger{Name: "demo", Version: "1.0.0",
		Subcommands: []extension.SubcommandContribution{{Namespace: "secret", Binary: "cg-demo"}}}
	if err := led.Save(ws); err != nil {
		t.Fatal(err)
	}
	// Drop the binary into the (XDG-controlled) clone bin dir.
	bin := filepath.Join(cloneDirFor(cfgHome, "demo"), "bin", "cg-demo")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := resolveExternalSubcommand(ws, "secret")
	if !ok || got != bin {
		t.Errorf("resolveExternalSubcommand(secret) = %q,%v; want %q,true", got, ok, bin)
	}
	if _, ok := resolveExternalSubcommand(ws, "nope"); ok {
		t.Error("unknown namespace should not resolve")
	}
}

func TestResolveExternalSubcommandPATHFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH executable-bit semantics differ on Windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "cg-greet")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	got, ok := resolveExternalSubcommand("", "greet")
	if !ok || got != bin {
		t.Errorf("PATH fallback = %q,%v; want %q,true", got, ok, bin)
	}
}

func TestExternalDispatchBuiltinsWin(t *testing.T) {
	cases := [][]string{
		{"version"},
		{"--help"},
		{"extension", "list"},
		{"help"},
		{}, // no args
	}
	for _, args := range cases {
		if _, _, ok := externalDispatch(args); ok {
			t.Errorf("externalDispatch(%v) should not dispatch externally", args)
		}
	}
}

func TestExternalDispatchSkipsLeadingFlags(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH executable-bit semantics differ on Windows")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cg-probe"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Chdir(t.TempDir()) // not a workspace; PATH fallback applies

	path, rest, ok := externalDispatch([]string{"--no-auto-update", "probe", "arg1", "--flag"})
	if !ok {
		t.Fatal("verb after a leading persistent flag should still dispatch")
	}
	if filepath.Base(path) != "cg-probe" {
		t.Errorf("resolved %q, want cg-probe", path)
	}
	if len(rest) != 2 || rest[0] != "arg1" || rest[1] != "--flag" {
		t.Errorf("rest = %v, want [arg1 --flag]", rest)
	}
}

func TestRunExternalPropagatesExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is POSIX")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "cg-fail")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := runExternal(script, nil)
	if err == nil {
		t.Fatal("expected an error for a non-zero exit")
	}
	var coded interface{ ExitCode() int }
	if !errors.As(err, &coded) || coded.ExitCode() != 7 {
		t.Errorf("expected exit code 7, got %v", err)
	}
}
