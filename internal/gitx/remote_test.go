package gitx

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Stderr fixtures as git, ssh and libcurl print them.
const (
	sshAuthStderr = "git@github.com: Permission denied (publickey).\r\n" +
		"fatal: Could not read from remote repository.\n\n" +
		"Please make sure you have the correct access rights\nand the repository exists."
	sshTimeoutStderr = "ssh: connect to host github.com port 22: Operation timed out\n" +
		"fatal: Could not read from remote repository.\n\n" +
		"Please make sure you have the correct access rights\nand the repository exists."
)

func TestClassifyRemote(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   error
	}{
		{"ssh publickey", sshAuthStderr, ErrRemoteAuth},
		{"ssh publickey,password", "Permission denied (publickey,password).", ErrRemoteAuth},
		{"ssh host key", "Host key verification failed.\nfatal: Could not read from remote repository.", ErrRemoteAuth},
		{"ssh agent refused", "sign_and_send_pubkey: signing failed for ED25519 \"key\" from agent: agent refused operation\ngit@host: Permission denied (publickey).", ErrRemoteAuth},
		{"https auth failed", "remote: Invalid username or password.\nfatal: Authentication failed for 'https://example.com/o/r.git/'", ErrRemoteAuth},
		{"https no prompt", "fatal: could not read Username for 'https://example.com': terminal prompts disabled", ErrRemoteAuth},
		{"https 403", "fatal: unable to access 'https://example.com/o/r.git/': The requested URL returned error: 403", ErrRemoteAuth},
		{"ssh timeout", sshTimeoutStderr, ErrRemoteUnreachable},
		{"ssh connection timed out (linux)", "ssh: connect to host example.com port 22: Connection timed out", ErrRemoteUnreachable},
		{"ssh dns", "ssh: Could not resolve hostname example.com: nodename nor servname provided, or not known", ErrRemoteUnreachable},
		{"ssh refused", "ssh: connect to host example.com port 22: Connection refused", ErrRemoteUnreachable},
		{"ssh no route", "ssh: connect to host example.com port 22: No route to host", ErrRemoteUnreachable},
		{"ssh net unreachable", "ssh: connect to host example.com port 22: Network is unreachable", ErrRemoteUnreachable},
		{"ssh closed", "Connection closed by 140.82.121.4 port 22\r\nfatal: Could not read from remote repository.", ErrRemoteUnreachable},
		{"ssh kex reset", "kex_exchange_identification: read: Connection reset by peer", ErrRemoteUnreachable},
		{"curl dns", "fatal: unable to access 'https://example.com/r.git/': Could not resolve host: example.com", ErrRemoteUnreachable},
		{"curl connect", "fatal: unable to access 'https://example.com/r.git/': Failed to connect to example.com port 443 after 75003 ms: Couldn't connect to server", ErrRemoteUnreachable},
		{"curl proxy", "fatal: unable to access 'https://example.com/r.git/': Could not resolve proxy: proxy.example", ErrRemoteUnreachable},
		{"curl windows reset", "fatal: unable to access 'https://example.com/r.git/': Recv failure: Connection was reset", ErrRemoteUnreachable},
		{"unrelated: missing ref", "fatal: couldn't find remote ref main", nil},
		{"unrelated: not a repo", "fatal: '/no/such' does not appear to be a git repository\nfatal: Could not read from remote repository.", nil},
		{"unrelated: local permission", "error: cannot open .git/FETCH_HEAD: Permission denied", nil},
		{"unrelated: non-ff", " ! [rejected]        HEAD -> main (fetch first)\nerror: failed to push some refs", nil},
		{"empty", "", nil},
		{"whitespace", " \n\t", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyRemote(tt.stderr); !errors.Is(got, tt.want) || (tt.want == nil) != (got == nil) {
				t.Errorf("classifyRemote() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunErrorWrapsClass(t *testing.T) {
	execErr := &exec.ExitError{}
	tests := []struct {
		name     string
		stderr   string
		want     error
		wantHint string
	}{
		{"auth", sshAuthStderr, ErrRemoteAuth, "remote authentication failed: check that git has a working credential"},
		{"unreachable", sshTimeoutStderr, ErrRemoteUnreachable, "remote unreachable: check the network connection"},
		{"unrelated", "fatal: couldn't find remote ref main", nil, ""},
		{"empty", "", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runError([]string{"fetch", "origin", "main"}, execErr, tt.stderr)
			msg := err.Error()
			if !strings.HasPrefix(msg, "git fetch origin main: ") {
				t.Errorf("message lost its command prefix: %q", msg)
			}
			if !strings.Contains(msg, strings.TrimSpace(tt.stderr)) {
				t.Errorf("message lost git's stderr: %q", msg)
			}
			var ee *exec.ExitError
			if !errors.As(err, &ee) {
				t.Error("exec error no longer wrapped")
			}
			if tt.want == nil {
				if IsRemote(err) {
					t.Errorf("unclassified failure reported as remote: %q", msg)
				}
				return
			}
			if !errors.Is(err, tt.want) || !IsRemote(err) {
				t.Errorf("errors.Is(err, %v) = false; err = %q", tt.want, msg)
			}
			// The class and hint are the last line, so they read after git's own text.
			if lines := strings.Split(msg, "\n"); !strings.HasPrefix(lines[len(lines)-1], tt.wantHint) {
				t.Errorf("last line = %q, want prefix %q", lines[len(lines)-1], tt.wantHint)
			}
		})
	}
}

// A local command is never classified, even when a hook it ran printed a
// network-looking message about something else.
func TestRunErrorLeavesLocalCommandsUnclassified(t *testing.T) {
	err := runError([]string{"commit", "-m", "x"}, &exec.ExitError{}, "hook: dial tcp 127.0.0.1:5432: connect: connection refused")
	if IsRemote(err) {
		t.Errorf("local command classified: %v", err)
	}
}

// The hints are cg core text: they must not assume which tool holds the
// user's keys or which forge hosts the remote.
func TestRemoteHintsNameNoVendor(t *testing.T) {
	for _, c := range remoteClasses {
		h := strings.ToLower(c.hint)
		for _, v := range []string{"1password", "keychain", "credential manager", "gcm", "pageant", "bitwarden", "github", "gitlab", "bitbucket", "azure"} {
			if strings.Contains(h, v) {
				t.Errorf("hint for %v names %q: %s", c.err, v, c.hint)
			}
		}
	}
}

func TestTouchesRemote(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"fetch", "origin", "main", "--quiet"}, true},
		{[]string{"push", "origin", "HEAD:main"}, true},
		{[]string{"pull", "--ff-only"}, true},
		{[]string{"clone", "--quiet", "--", "u", "d"}, true},
		{[]string{"ls-remote", "--heads", "u"}, true},
		{[]string{"-c", "core.autocrlf=false", "fetch", "origin"}, true},
		{[]string{"-C", "fetch", "status"}, false}, // "fetch" is -C's value
		{[]string{"--git-dir", "push", "log"}, false},
		{[]string{"--git-dir=/x/.git", "push", "origin"}, true},
		{[]string{"--literal-pathspecs", "status"}, false},
		{[]string{"--attr-source", "fetch", "status"}, false}, // "fetch" is --attr-source's value
		{[]string{"--attr-source", "HEAD", "push", "origin"}, true},
		{[]string{"--attr-source=HEAD", "push", "origin"}, true},
		{[]string{"merge", "--ff-only", "origin/main"}, false},
		{[]string{"rev-parse", "HEAD"}, false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := touchesRemote(tt.args); got != tt.want {
			t.Errorf("touchesRemote(%q) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

// A real fetch from a path that is not a repository fails without a remote
// class: a wrong URL is not a network or credential problem.
func TestFetchMissingRepoIsUnclassified(t *testing.T) {
	ctx, dir := initRepo(t)
	mustRun(t, ctx, dir, "remote", "add", "origin", filepath.Join(t.TempDir(), "missing"))
	err := Fetch(context.Background(), dir, "origin", "main")
	if err == nil {
		t.Fatal("fetch from a missing repository succeeded")
	}
	if IsRemote(err) {
		t.Errorf("missing repository classified as a remote failure: %v", err)
	}
}
