package cmd

import (
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/gitbash"
)

func TestGitBashWarning(t *testing.T) {
	found := func() (string, bool) { return `C:\Program Files\Git\bin\bash.exe`, true }
	missing := func() (string, bool) { return "", false }

	if got := gitBashWarning("linux", missing); got != "" {
		t.Errorf("warned off Windows: %q", got)
	}
	if got := gitBashWarning("windows", found); got != "" {
		t.Errorf("warned with Git Bash found: %q", got)
	}
	got := gitBashWarning("windows", missing)
	for _, want := range []string{"Git for Windows", gitbash.EnvVar} {
		if !strings.Contains(got, want) {
			t.Errorf("warning %q doesn't mention %q", got, want)
		}
	}
}
