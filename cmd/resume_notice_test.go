package cmd

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/session"
)

func TestPrintResumeNotice(t *testing.T) {
	var none bytes.Buffer
	printResumeNotice(&none, "p", nil, "session/")
	if none.Len() != 0 {
		t.Errorf("not paused: printed %q", none.String())
	}

	// The session slug (p-fix) differs from the project slug (p): the command
	// to re-enter the worktree must use the session slug.
	var out bytes.Buffer
	printResumeNotice(&out, "p", []session.Resume{{
		Project:  "p",
		File:     "/ws--p-fix/projects/p/resume.md",
		Checkout: session.Checkout{Dir: "/ws--p-fix", Branch: "session/p-fix"},
	}}, "session/")
	got := out.String()
	for _, want := range []string{
		"cg worktree create p-fix",
		"/ws--p-fix/projects/p/resume.md",
		`git -C '/ws--p-fix' rm -f -- '/ws--p-fix/projects/p/resume.md'`,
		`drop pause cursor' -- '/ws--p-fix/projects/p/resume.md'`,
		`move into '/ws--p-fix'`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("notice lacks %q:\n%s", want, got)
		}
	}

	// In the caller's own checkout there is nothing to re-enter; a branch
	// outside the session prefix gets the directory, not a create command.
	out.Reset()
	printResumeNotice(&out, "p", []session.Resume{
		{Project: "p", File: "/ws/projects/p/resume.md", Checkout: session.Checkout{Dir: "/ws", Branch: "main", Own: true}},
		{Project: "p", File: "/other/projects/p/resume.md", Checkout: session.Checkout{Dir: "/other", Branch: "feature"}},
	}, "session/")
	got = out.String()
	if strings.Contains(got, "cg worktree create") {
		t.Errorf("no session worktree: must not print a create command:\n%s", got)
	}
	if !strings.Contains(got, `move into '/other'`) {
		t.Errorf("notice lacks the other checkout:\n%s", got)
	}
	// Every cursor gets its own deletion, in its own checkout.
	for _, want := range []string{`git -C '/ws' rm -f -- '/ws/projects/p/resume.md'`, `git -C '/other' rm -f -- '/other/projects/p/resume.md'`} {
		if !strings.Contains(got, want) {
			t.Errorf("notice lacks %q:\n%s", want, got)
		}
	}
	// A path with a space stays one argument.
	out.Reset()
	printResumeNotice(&out, "p", []session.Resume{{Project: "p", File: "/my ws/projects/p/resume.md",
		Checkout: session.Checkout{Dir: "/my ws", Own: true}}}, "session/")
	if !strings.Contains(out.String(), `rm -f -- '/my ws/projects/p/resume.md'`) {
		t.Errorf("path with a space must be quoted:\n%s", out.String())
	}
}

// The quoted path must reach the program unchanged through a real POSIX
// shell: no expansion of $, backticks or quotes, no word splitting.
func TestShellQuoteRoundTrip(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH")
	}
	t.Setenv("HOME", "/expanded")
	for _, p := range []string{
		"/plain/path",
		"/my ws/x",
		"/$HOME/x",
		"/`echo pwned`/x",
		`/it's "quoted"/x`,
		`C:\Users\me\ws`,
		"/100%/x",
		"",
		"/two\nlines",
	} {
		out, err := exec.CommandContext(t.Context(), sh, "-c", "printf '%s' "+shellQuote(p)).Output() //nolint:gosec // test drives sh with fixed inputs
		if err != nil || string(out) != p {
			t.Errorf("sh got %q (%v) for %q via %s", out, err, p, shellQuote(p))
		}
	}
}
