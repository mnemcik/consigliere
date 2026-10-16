package gitx

import (
	"errors"
	"fmt"
	"strings"
)

// Remote failure classes. Run and RunEnv wrap one of these into their error
// when git's stderr shows the remote could not be reached or refused the
// credentials, so a caller can tell a dead network from a rejected key with
// errors.Is instead of reading the message.
var (
	// ErrRemoteAuth: the remote was reached but did not accept the
	// credentials git offered (SSH key, HTTPS token or password).
	ErrRemoteAuth = errors.New("remote authentication failed")
	// ErrRemoteUnreachable: the remote host could not be reached at all —
	// name resolution, a refused or timed-out connection, a reset.
	ErrRemoteUnreachable = errors.New("remote unreachable")
)

// remoteClass is one failure class: its sentinel, the remediation appended to
// the error, and the lower-case stderr fragments that identify it.
type remoteClass struct {
	err    error
	hint   string
	tokens []string
}

// remoteClasses is checked in order; the first class with a matching token
// wins. The tokens lean on the ssh layer and on libcurl, which do not
// translate their messages; the few that git itself prints ("Authentication
// failed for", "could not read Username") are translated under LANG, which is
// why RunEnv runs remote commands with LC_ALL=C. Every token is specific to a
// network or authentication failure: a bare "Permission denied" is also how
// a local file permission error reads, so only ssh's "Permission denied ("
// counts.
//
// The hints name no vendor or tool: which agent or credential store holds the
// key is the user's setup, not cg's to guess.
var remoteClasses = []remoteClass{
	{
		err:  ErrRemoteAuth,
		hint: "check that git has a working credential for this remote (an SSH key loaded in the agent, or an HTTPS token) and that the remote's host key is trusted, then retry",
		tokens: []string{
			"permission denied (",                   // ssh: Permission denied (publickey).
			"host key verification failed",          // ssh
			"sign_and_send_pubkey",                  // ssh: the agent refused to sign
			"authentication failed for",             // git over HTTPS
			"could not read username for",           // git over HTTPS, no credential and no prompt
			"could not read password for",           // git over HTTPS
			"terminal prompts disabled",             // git over HTTPS with GIT_TERMINAL_PROMPT=0
			"invalid username or password",          // server message relayed by git
			"the requested url returned error: 401", // libcurl
			"the requested url returned error: 403", // libcurl
		},
	},
	{
		err:  ErrRemoteUnreachable,
		hint: "check the network connection (and any VPN or proxy in use), then retry",
		tokens: []string{
			"could not resolve host",    // ssh "hostname", libcurl "host:"
			"could not resolve proxy",   // libcurl
			"resolving timed out",       // libcurl
			"name or service not known", // getaddrinfo
			"operation timed out",
			"connection timed out",
			"connection refused",
			"network is unreachable",
			"no route to host",
			"connection closed by",       // ssh: Connection closed by <host> port 22
			"connection reset by",        // ssh, libcurl
			"connection was reset",       // libcurl on Windows
			"failed to connect to",       // libcurl
			"couldn't connect to server", // libcurl
		},
	},
}

// classifyRemote returns ErrRemoteAuth or ErrRemoteUnreachable when stderr
// shows that failure, and nil otherwise — including for empty stderr, which
// carries no evidence either way (a context timeout kills git silently).
func classifyRemote(stderr string) error {
	if c := matchRemote(stderr); c != nil {
		return c.err
	}
	return nil
}

func matchRemote(stderr string) *remoteClass {
	s := strings.ToLower(stderr)
	if strings.TrimSpace(s) == "" {
		return nil
	}
	for i := range remoteClasses {
		for _, tok := range remoteClasses[i].tokens {
			if strings.Contains(s, tok) {
				return &remoteClasses[i]
			}
		}
	}
	return nil
}

// IsRemote reports whether err is a classified remote failure: unreachable,
// or authentication refused.
func IsRemote(err error) bool {
	return errors.Is(err, ErrRemoteUnreachable) || errors.Is(err, ErrRemoteAuth)
}

// runError builds the error for a failed `git <args...>`: the command, the
// exec error and git's stderr, plus — when a command that talks to a remote
// shows a remote failure — a final line naming the class and what to check,
// with the class sentinel wrapped alongside the exec error. Local commands are
// never classified: a hook they run can print "connection refused" about
// something else entirely.
func runError(args []string, err error, stderr string) error {
	what := "git " + strings.Join(args, " ")
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("%s: %w", what, err)
	}
	if !touchesRemote(args) {
		return fmt.Errorf("%s: %w: %s", what, err, stderr)
	}
	if c := matchRemote(stderr); c != nil {
		return fmt.Errorf("%s: %w: %s\n%w: %s", what, err, stderr, c.err, c.hint)
	}
	return fmt.Errorf("%s: %w: %s", what, err, stderr)
}

// touchesRemote reports whether a git command line talks to a remote, so its
// messages need LC_ALL=C and its failures are classified. Leading global
// options (-c k=v, -C dir, --git-dir dir, --attr-source tree, --flag=value,
// --flag) are skipped to find the subcommand.
func touchesRemote(args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-c" || a == "-C" || a == "--git-dir" || a == "--work-tree" || a == "--namespace" ||
			a == "--attr-source":
			i++ // skip the option's value
		case strings.HasPrefix(a, "-"):
		default:
			switch a {
			case "fetch", "pull", "push", "clone", "ls-remote":
				return true
			}
			return false
		}
	}
	return false
}
