package cmd

import (
	"bytes"
	"testing"
)

func TestVersionSubcommand(t *testing.T) {
	oldVersion := Version
	oldRootVersion := rootCmd.Version
	Version = "test-1.2.3"
	rootCmd.Version = Version
	defer func() {
		Version = oldVersion
		rootCmd.Version = oldRootVersion
	}()

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"version"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := buf.String()
	want := "cg version test-1.2.3\n"
	if got != want {
		t.Errorf("version subcommand output: got %q, want %q", got, want)
	}
}

func TestVersionFlag(t *testing.T) {
	oldVersion := rootCmd.Version
	rootCmd.Version = "test-4.5.6"
	defer func() { rootCmd.Version = oldVersion }()

	for _, arg := range []string{"--version", "-v"} {
		t.Run(arg, func(t *testing.T) {
			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)
			rootCmd.SetArgs([]string{arg})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("execute: %v", err)
			}
			got := buf.String()
			want := "cg version test-4.5.6\n"
			if got != want {
				t.Errorf("%s output: got %q, want %q", arg, got, want)
			}
		})
	}
}
