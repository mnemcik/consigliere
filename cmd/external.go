package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// Variables cg sets in the environment of a binary it runs for an extension
// command. They describe that one run, so dispatch never reads them.
const (
	// envCommand is the declared command that matched, e.g. "live run".
	envCommand = "CG_COMMAND"
)

// envWorkspace names the workspace a session started with `cg` serves: `cg
// start` sets it to the absolute root it resolved, and dispatch reads it
// (never rewrites it) to prefer that workspace from other directories.
const envWorkspace = "CG_WORKSPACE"

// More per-run variables cg sets for a binary it runs.
const (
	// envExtensionWorkspace is the workspace root whose declared command
	// it is.
	envExtensionWorkspace = "CG_EXTENSION_WORKSPACE"
	// envSessionWorktree is the session's worktree, for a writer run from
	// outside the session's workspace.
	envSessionWorktree = "CG_SESSION_WORKTREE"
)

// perRunVars are the variables that describe one dispatched run. A PATH
// program never gets them, and a session cg starts doesn't inherit them.
var perRunVars = []string{envCommand, envExtensionWorkspace, envSessionWorktree}

// samePath reports whether a and b are the same existing file or directory,
// whatever the spelling: symlinks are followed, and on a case-insensitive
// filesystem the case of the path doesn't matter (os.SameFile compares the
// files themselves). A path that can't be stat'ed matches nothing.
func samePath(a, b string) bool {
	ia, err := os.Stat(a) //nolint:gosec // only compared, never opened
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ia, ib)
}

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
	roots, cwdErr := consultedRoots()
	for _, root := range roots {
		run, decided, err := dispatchIn(root, typed)
		if err != nil {
			return nil, err
		}
		if decided {
			run.args = rest
			return run, nil
		}
	}
	if cwdErr != nil {
		return nil, cwdErr
	}
	if !plainName(typed[0]) {
		return nil, nil
	}
	if p, err := exec.LookPath("cg-" + typed[0]); err == nil {
		return &externalRun{path: p, args: rest}, nil
	}
	return nil, nil
}

// plainName reports whether cg-<ns> is a plain program name for the PATH
// fallback. LookPath takes a name with a path separator, or (on Windows) a
// colon, as a path relative to the current directory instead of searching
// PATH.
func plainName(ns string) bool {
	return ns != "" && !strings.ContainsAny(ns, `/\:`)
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

// consultedRoots are the workspace roots dispatch looks at, in order. Without
// a usable CG_WORKSPACE (the session root), only the workspace of the current
// directory (the cwd root). With one: the session root alone when the cwd root
// is the session root; the cwd root alone when it is one of the session repo's
// worktrees; the session root, then the cwd root, when the cwd root is
// another workspace (or the worktree check can't vouch for it); the session
// root alone outside any workspace. A .cg.json on the way up that can't be
// read is an error: it might be a workspace declaring the command.
//
// When the current directory's .cg.json can't be read, the session root is
// still consulted (it comes first anyway), and the error is returned as
// cwdErr: it stops dispatch only if the session root doesn't decide the
// namespace.
func consultedRoots() (roots []string, cwdErr error) {
	_, session, _ := sessionRoot()
	cwd, err := os.Getwd()
	if err != nil {
		cwdErr = cgerr.New(cgerr.ExitUsage, "can't determine the current directory (%v), so cg can't tell which workspace's extension commands apply", err)
		return sessionOnly(session), cwdErr
	}
	root, _, err := workspace.FindRoot(cwd)
	if err != nil {
		cwdErr = cgerr.New(cgerr.ExitUsage, "can't read %s (%v), so cg can't tell which extension commands it declares",
			unreadableConfig(cwd), err)
		return sessionOnly(session), cwdErr
	}
	switch {
	case session == "" && root == "":
		return nil, nil
	case session == "":
		return []string{root}, nil
	case root == "":
		return []string{session}, nil
	case samePath(root, session):
		return []string{session}, nil
	case sessionWorktree(session, root):
		return []string{root}, nil
	default:
		return []string{session, root}, nil
	}
}

// sessionOnly is the session root as a root list, or none.
func sessionOnly(session string) []string {
	if session == "" {
		return nil
	}
	return []string{session}
}

// sessionRoot reads CG_WORKSPACE. It returns the variable's value, the session
// root when the value is usable, and otherwise why it is ignored: a relative
// path, or a directory without a valid .cg.json (e.g. a worktree removed
// since the session started). Unset, all three are "".
func sessionRoot() (value, root, ignored string) {
	value, set := os.LookupEnv(envWorkspace)
	switch {
	case !set:
		return "", "", ""
	case value == "":
		return "", "", "it is empty"
	case !filepath.IsAbs(value):
		return value, "", "it is not an absolute path"
	}
	if cfg, err := workspace.Detect(value); err != nil || cfg == nil {
		return value, "", "it names no valid .cg.json"
	}
	return value, filepath.Clean(value), ""
}

// sessionWorktree reports whether dir is the top level of one of the session
// repo's worktrees, as the session root's own `git worktree list` says. The
// list is read from the session root, never from dir, so a planted .git file
// in dir proves nothing. git runs with its repository variables removed, so
// the caller's environment can't redirect it. Any failure (no git, the session
// root not the top level of its repo, an unreadable list) means no.
//
// A listed path is trusted only while it still is that worktree: dir's own
// .git must be the session repo's git directory (the main checkout), or name
// one of its worktrees/ entries whose gitdir names dir's .git back (a linked
// one). That is read from the files, without running git in dir. Otherwise a
// directory put where a deleted worktree was would count.
func sessionWorktree(session, dir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := gitx.RunClean(ctx, session, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	if err != nil {
		return false
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 2 || !samePath(gitx.OSPath(lines[0]), session) {
		return false
	}
	common := gitx.OSPath(lines[1])
	list, err := gitx.RunCleanRaw(ctx, session, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return false
	}
	// -z: each attribute ends with NUL, and an empty one ends a worktree's
	// block, so paths are taken exactly (a trailing space included).
	for _, attr := range strings.Split(string(list), "\x00") {
		if p, ok := strings.CutPrefix(attr, "worktree "); ok && samePath(gitx.OSPath(p), dir) {
			return gitDirIn(dir, common)
		}
	}
	return false
}

// gitDirIn reports whether dir's .git belongs to the repository whose common
// git directory is common: .git is that directory (the main checkout), or a
// file whose "gitdir:" names an entry of its worktrees/ directory whose own
// gitdir file names dir's .git back.
func gitDirIn(dir, common string) bool {
	dotGit := filepath.Join(dir, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return samePath(dotGit, common)
	}
	data, err := os.ReadFile(dotGit) //nolint:gosec // a fixed name in a listed worktree
	if err != nil {
		return false
	}
	gd, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return false
	}
	gd = filepath.FromSlash(strings.TrimSpace(gd))
	if !filepath.IsAbs(gd) {
		gd = filepath.Join(dir, gd)
	}
	if !samePath(filepath.Dir(gd), filepath.Join(common, "worktrees")) {
		return false
	}
	back, err := os.ReadFile(filepath.Join(gd, "gitdir")) //nolint:gosec // inside the session repo's git directory
	if err != nil {
		return false
	}
	bp := filepath.FromSlash(strings.TrimSpace(string(back)))
	if !filepath.IsAbs(bp) {
		bp = filepath.Join(gd, bp)
	}
	return samePath(bp, dotGit)
}

// unreadableConfig names the first .cg.json from dir upwards that
// workspace.Detect can't read, the one FindRoot stopped at.
func unreadableConfig(dir string) string {
	for d := filepath.Clean(dir); ; {
		if _, err := workspace.Detect(d); err != nil {
			return filepath.Join(d, workspace.ConfigFile)
		}
		parent := filepath.Dir(d)
		if parent == d {
			return workspace.ConfigFile
		}
		d = parent
	}
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
	// badPath says the extension's .cg.json entry has an invalid path.
	badPath bool
}

// namespaceEntries returns the ledger command entries of the workspace at root
// whose namespace is ns, in .cg.json order, each verified against its
// extension's manifest in its pinned tree. A .cg.json or ledger that can't be
// read is an error: a broken ledger might declare the namespace, a longer
// command than any readable entry (S15) or the same one (S18), so dispatch
// must neither pick among the rest nor hand the namespace to a later root or
// to PATH.
func namespaceEntries(root, ns string) ([]commandEntry, error) {
	cfg, err := workspace.Detect(root)
	if err != nil {
		return nil, cgerr.New(cgerr.ExitUsage, "can't read %s (%v), so cg can't tell which extension commands it declares",
			filepath.Join(root, workspace.ConfigFile), err)
	}
	if cfg == nil {
		return nil, nil
	}
	var out []commandEntry
	for i := range cfg.Extensions {
		e := &cfg.Extensions[i]
		led, lerr := extension.LoadLedger(root, e.Name)
		if lerr != nil {
			return nil, unreadableLedgerError(root, e.Name, lerr)
		}
		if led == nil {
			continue
		}
		var (
			dir, why         string
			m                *extension.Manifest
			checked, badPath bool
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
					badPath = errors.Is(verr, errBadPath)
				}
			}
			entry := commandEntry{ext: c, name: e.Name, unverified: why, badPath: badPath}
			if why == "" {
				entry.bin = filepath.Join(dir, "bin", c.Binary)
				if !verifiedCommand(dir, m, c) {
					entry.unverified = "its manifest doesn't declare that command with that binary, or the binary is missing"
				}
			}
			out = append(out, entry)
		}
	}
	return out, nil
}

// unreadableLedgerError says that extension name's ledger in the workspace at
// root can't be read. `cg extension update` and `remove` read the same ledger,
// so they can't fix it: restoring a damaged file can, or deleting it and
// reinstalling the extension.
func unreadableLedgerError(root, name string, err error) error {
	return cgerr.New(cgerr.ExitUsage, "can't read %s (%v), so cg can't tell which commands extension %q declares; if it is damaged, restore it (for example from git), or delete it and reinstall the extension with `cg extension install --force`",
		extension.LedgerPath(root, name), err, name)
}

// dispatchIn looks for the typed command among the declared commands of the
// workspace at root. decided is false when the workspace declares nothing in
// the namespace, so the next root (or PATH) may answer; otherwise this root
// decides, and either run is set or err says why nothing runs.
func dispatchIn(root string, typed []string) (run *externalRun, decided bool, err error) {
	ns := typed[0]
	entries, err := namespaceEntries(root, ns)
	if err != nil {
		return nil, true, err
	}
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
		if len(words) > len(typed) || len(words) < bestLen || !wordsPrefix(words, typed) {
			continue
		}
		// An equal match is the same command of the same extension (two
		// extensions declaring it stopped above), listed twice in .cg.json:
		// take a verified one, as the install-time check counts either.
		if len(words) == bestLen && (best.unverified == "" || entries[i].unverified != "") {
			continue
		}
		best, bestLen = &entries[i], len(words)
	}
	if best == nil {
		cmds := make([]string, 0, len(entries))
		for _, en := range entries {
			// An extension .cg.json lists twice repeats its entries.
			if c := "  cg " + en.ext.Command; !slices.Contains(cmds, c) {
				cmds = append(cmds, c)
			}
		}
		return nil, true, cgerr.New(cgerr.ExitUsage,
			"unknown command \"cg %s\": the extensions installed in %s declare these commands in %q:\n%s",
			strings.Join(typed, " "), root, ns, strings.Join(cmds, "\n"))
	}
	if best.unverified != "" {
		fix := fmt.Sprintf("run `cg init` to restore the recorded version, or `cg extension update %s`", best.name)
		if best.badPath {
			fix = fmt.Sprintf("correct its entry in %s", filepath.Join(root, workspace.ConfigFile))
		}
		return nil, true, cgerr.New(cgerr.ExitUsage,
			"cg %s: extension %q (installed in %s) can't run it: %s; %s",
			best.ext.Command, best.name, root, best.unverified, fix)
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
// passed through, and the environment the binary is told. For a workspace's
// declared command: CG_COMMAND (the command that matched) and
// CG_EXTENSION_WORKSPACE (the root that served it). A PATH program gets
// neither, nor CG_SESSION_WORKTREE, so a value left over from an outer run
// never reaches it. CG_WORKSPACE passes through unchanged.
func (r *externalRun) cmd() *exec.Cmd {
	c := exec.CommandContext(context.Background(), r.path, r.args...) //nolint:gosec // path resolved from installed extensions / PATH
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	env := withoutEnv(os.Environ(), perRunVars...)
	if r.root != "" {
		env = append(env, envCommand+"="+r.command, envExtensionWorkspace+"="+r.root)
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
	switch value, _, ignored := sessionRoot(); {
	case ignored != "":
		session = fmt.Sprintf("CG_WORKSPACE=%q is ignored: %s", value, ignored)
	case value != "":
		session = fmt.Sprintf("CG_WORKSPACE is %q", value)
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
