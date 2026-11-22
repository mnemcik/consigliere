package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// Variables cg sets in the environment of a binary it runs for an extension
// command. They describe that one run, so dispatch never reads them.
const (
	// envCommand is the declared command that matched, e.g. "live run".
	envCommand = "CG_COMMAND"
)

// envWorkspace names the workspace a session started with `cg` serves.
const envWorkspace = "CG_WORKSPACE"

// isWindowsOS selects Windows rules (case-insensitive variable names).
const isWindowsOS = runtime.GOOS == "windows"

// externalRun is an extension command (or a PATH program) dispatch resolved.
type externalRun struct {
	path string   // the binary
	args []string // the arguments after the namespace word
	// command is the declared command that matched; "" for a binary found
	// through the PATH fallback.
	command string
	// root is the workspace whose declared command it is; "" for the PATH
	// fallback.
	root string
}

// externalDispatch decides whether args invoke an extension command (or a
// cg-<namespace> program on PATH) rather than a built-in. It returns nil, nil
// when nothing external resolves, so cobra handles args (a built-in, or its
// unknown-command error). Built-ins always win.
//
// The typed words are the leading arguments after cg's own flags, up to the
// first one starting with "-". Their first word is the namespace. The first
// consulted workspace with any ledger entry in the namespace decides it: the
// longest of its declared commands the typed words start with runs, provided
// its entry is verified against the extension's pinned manifest. No match, an
// unverified entry, or one command declared by two ledgers is an error; none
// of them falls through to another workspace or to PATH. Only a namespace no
// consulted workspace declares reaches the cg-<namespace> PATH fallback.
func externalDispatch(args []string) (*externalRun, error) {
	// Leading persistent flags (e.g. --no-auto-update) are cg's, not the
	// command's. (All root persistent flags are booleans, so none takes a
	// value.)
	verbIdx := -1
	for i, a := range args {
		if a == "" || a[0] != '-' {
			verbIdx = i
			break
		}
	}
	if verbIdx < 0 {
		return nil, nil // no verb (flags only)
	}
	if verb := args[verbIdx]; verb == cmdHelp || verb == "completion" || verb == "__complete" {
		return nil, nil
	}
	// If cobra can resolve the verb onward to a real (non-root) command, it's a
	// built-in and wins.
	if target, _, err := rootCmd.Find(args[verbIdx:]); err == nil && target != rootCmd {
		return nil, nil
	}
	typed := typedWords(args[verbIdx:])
	rest := args[verbIdx+1:]
	for _, root := range consultedRoots() {
		run, decided, err := dispatchIn(root, typed)
		if err != nil {
			return nil, err
		}
		if decided {
			run.args = rest
			return run, nil
		}
	}
	if p, err := exec.LookPath("cg-" + typed[0]); err == nil {
		return &externalRun{path: p, args: rest}, nil
	}
	return nil, nil
}

// typedWords returns the leading arguments up to the first that starts with
// "-" (or is empty). args[0] is the namespace, which never starts with "-".
func typedWords(args []string) []string {
	n := 1
	for n < len(args) && args[n] != "" && args[n][0] != '-' {
		n++
	}
	return args[:n]
}

// consultedRoots are the workspace roots dispatch looks at, in order: the
// workspace of the current directory, if any.
func consultedRoots() []string {
	if root := workspaceRootForDispatch(); root != "" {
		return []string{root}
	}
	return nil
}

// commandEntry is one ledger command entry of a workspace, checked against the
// extension's pinned manifest.
type commandEntry struct {
	ext extension.CommandContribution
	// name is the extension that declares it.
	name string
	// bin is the binary's path in the pinned tree.
	bin string
	// unverified says why the entry can't run; "" when it is verified.
	unverified string
}

// namespaceEntries returns the ledger command entries of the workspace at root
// whose namespace is ns, in .cg.json order, each verified against its
// extension's manifest in its pinned tree.
func namespaceEntries(root, ns string) []commandEntry {
	cfg, err := workspace.Detect(root)
	if err != nil || cfg == nil {
		return nil
	}
	var out []commandEntry
	for i := range cfg.Extensions {
		e := &cfg.Extensions[i]
		led, lerr := extension.LoadLedger(root, e.Name)
		if lerr != nil || led == nil {
			continue
		}
		var (
			dir, why string
			m        *extension.Manifest
			checked  bool
		)
		for _, c := range led.Commands {
			if extension.CommandNamespace(c.Command) != ns {
				continue
			}
			if !checked {
				checked = true
				var verr error
				if dir, m, verr = verifiedManifest(e); verr != nil {
					why = verr.Error()
				}
			}
			entry := commandEntry{ext: c, name: e.Name, unverified: why}
			if why == "" {
				entry.bin = filepath.Join(dir, "bin", c.Binary)
				if !verifiedCommand(dir, m, c) {
					entry.unverified = "its manifest doesn't declare that command with that binary, or the binary is missing"
				}
			}
			out = append(out, entry)
		}
	}
	return out
}

// dispatchIn looks for the typed command among the declared commands of the
// workspace at root. decided is false when the workspace declares nothing in
// the namespace, so the next root (or PATH) may answer; otherwise this root
// decides, and either run is set or err says why nothing runs.
func dispatchIn(root string, typed []string) (run *externalRun, decided bool, err error) {
	ns := typed[0]
	entries := namespaceEntries(root, ns)
	if len(entries) == 0 {
		return nil, false, nil
	}
	// One command declared by two ledgers (a hand-edited or pulled .cg.json)
	// can't say which program runs: refuse the whole namespace.
	first := map[string]string{}
	for _, en := range entries {
		if other, dup := first[en.ext.Command]; dup && other != en.name {
			return nil, true, cgerr.New(cgerr.ExitUsage,
				"command %q is declared by both extension %q and extension %q in %s; remove one of them (`cg extension remove <name>`)",
				en.ext.Command, other, en.name, root)
		}
		first[en.ext.Command] = en.name
	}
	// The longest declared command the typed words start with, chosen among
	// every entry before verification: a shorter verified entry never stands
	// in for an unverified longer one.
	var best *commandEntry
	bestLen := 0
	for i := range entries {
		words := strings.Split(entries[i].ext.Command, " ")
		if len(words) > len(typed) || len(words) <= bestLen || !wordsPrefix(words, typed) {
			continue
		}
		best, bestLen = &entries[i], len(words)
	}
	if best == nil {
		cmds := make([]string, 0, len(entries))
		for _, en := range entries {
			cmds = append(cmds, "  cg "+en.ext.Command)
		}
		return nil, true, cgerr.New(cgerr.ExitUsage,
			"unknown command \"cg %s\": the extensions installed in %s declare these commands in %q:\n%s",
			strings.Join(typed, " "), root, ns, strings.Join(cmds, "\n"))
	}
	if best.unverified != "" {
		return nil, true, cgerr.New(cgerr.ExitUsage,
			"cg %s: extension %q (installed in %s) can't run it: %s; run `cg init` to restore the recorded version, or `cg extension update %s`",
			best.ext.Command, best.name, root, best.unverified, best.name)
	}
	return &externalRun{path: best.bin, command: best.ext.Command, root: root}, true, nil
}

// wordsPrefix reports whether words is a prefix of typed.
func wordsPrefix(words, typed []string) bool {
	for i, w := range words {
		if typed[i] != w {
			return false
		}
	}
	return true
}

// cmd builds the command that runs r: the binary with its arguments, stdio
// passed through, and the environment the binary is told: CG_COMMAND for a
// workspace's declared command, removed for a PATH program, so a value left
// over from an outer run never reaches it.
func (r *externalRun) cmd() *exec.Cmd {
	c := exec.CommandContext(context.Background(), r.path, r.args...) //nolint:gosec // path resolved from installed extensions / PATH
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	env := withoutEnv(os.Environ(), envCommand)
	if r.root != "" {
		env = append(env, envCommand+"="+r.command)
	}
	c.Env = env
	return c
}

// withoutEnv returns env without the given keys.
func withoutEnv(env []string, keys ...string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, key := range keys {
			if envKeyEqual(k, key) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// envKeyEqual compares environment variable names, ignoring case on Windows,
// where they are case-insensitive.
func envKeyEqual(a, b string) bool {
	if isWindowsOS {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// run execs r, and propagates its exit code via a CodedError so main.go exits
// with the same status. Its stderr is the command's own, so no extra error line
// is printed on a non-zero exit.
func (r *externalRun) run() error {
	if err := r.cmd().Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return cgerr.New(ee.ExitCode(), "")
		}
		return err
	}
	return nil
}

// unknownCommandHint follows cobra's unknown-command error: the command may be
// an extension's, which resolves only where the workspace that installed it is
// in effect. It returns "" for any other error.
func unknownCommandHint(err error) string {
	if err == nil || !strings.HasPrefix(err.Error(), "unknown command ") {
		return ""
	}
	session := "CG_WORKSPACE is not set"
	if v, ok := os.LookupEnv(envWorkspace); ok {
		session = fmt.Sprintf("CG_WORKSPACE is %q", v)
	}
	return "If it is an extension's command: it runs inside the workspace that installed it, " +
		"and from any directory in a session started with `cg` in that workspace, which carries the workspace along (" + session + ")."
}

// workspaceRootForDispatch resolves the workspace root from cwd, returning "" if
// not inside a workspace (PATH fallback still applies).
func workspaceRootForDispatch() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	root, _, _ := workspace.FindRoot(cwd)
	return root
}
