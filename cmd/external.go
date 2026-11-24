package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/session"
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
// whatever the spelling. Both must be the same file (os.SameFile) and resolve
// (symlinks followed) to the same path compared ignoring case and Unicode
// normalisation (NFC vs NFD names on macOS). The file check catches a
// different directory that happens to share a name on a case-sensitive
// filesystem; the path check catches filesystems whose file IDs aren't unique
// (some network, FUSE and overlay mounts). A path that can't be resolved, or
// two spellings the string check can't equate (a bind mount, a Windows
// junction), match nothing: the callers then fail closed.
func samePath(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		return false
	}
	if !strings.EqualFold(norm.NFC.String(filepath.Clean(ra)), norm.NFC.String(filepath.Clean(rb))) {
		return false
	}
	ia, err := os.Stat(ra) //nolint:gosec // only compared, never opened
	if err != nil {
		return false
	}
	ib, err := os.Stat(rb) //nolint:gosec // only compared, never opened
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
	// sessionWorktree is the session's worktree, told to a binary the
	// session root serves for a directory outside it; "" otherwise.
	sessionWorktree string
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
	if verb := args[verbIdx]; verb == cmdHelp || verb == "completion" ||
		verb == cobra.ShellCompRequestCmd || verb == cobra.ShellCompNoDescRequestCmd {
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
			if servedFromOutside(run.root) {
				run.sessionWorktree = sessionWorktreeFor(run.root)
			}
			return run, nil
		}
	}
	if cwdErr != nil {
		return nil, cwdErr
	}
	if !plainName(typed[0]) {
		return nil, nil
	}
	// A declared namespace never reaches PATH (S33), also when typed in a
	// spelling the PATH lookup would still match.
	if ns := declaredSpelling(roots, typed[0]); ns != "" {
		return nil, cgerr.New(cgerr.ExitUsage,
			"unknown command \"cg %s\": the namespace %q is an extension's (did you mean `cg %s`?)", typed[0], ns, ns)
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
// When the current directory's .cg.json can't be read and it belongs to
// another workspace, the session root is still consulted (it would come first
// anyway), and the error is returned as cwdErr: it stops dispatch only if the
// session root doesn't decide the namespace. When it is one of the session
// repo's worktrees' (which would replace the session root), it stops dispatch
// outright. (A broken .cg.json of the session root itself makes CG_WORKSPACE
// ignored, so no root is left and dispatch stops too. A main checkout made with
// --separate-git-dir isn't recognised as a worktree: git lists it under its git
// directory's path.)
func consultedRoots() (roots []string, cwdErr error) {
	_, sroot, _ := sessionRoot()
	if sdir, broken := brokenSessionConfig(); broken != nil {
		return brokenSessionRoots(sdir, broken)
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwdErr = cgerr.New(cgerr.ExitUsage, "can't determine the current directory (%v), so cg can't tell which workspace's extension commands apply", err)
		return sessionOnly(sroot), cwdErr
	}
	root, _, err := workspace.FindRoot(cwd)
	dangling := ""
	if err == nil {
		// FindRoot walks past a .cg.json it can't follow (a dangling
		// symlink, read as absent); for dispatch it might declare the
		// command, so it counts as unreadable.
		if dangling = danglingConfig(cwd, root); dangling != "" {
			err = fmt.Errorf("%s is a dangling symlink", dangling)
		}
	}
	if err != nil {
		broken := dangling
		if broken == "" {
			broken = unreadableConfig(cwd)
		}
		name := broken
		if name == "" {
			name = "a " + workspace.ConfigFile + " in or above " + cwd
		}
		cwdErr = cgerr.New(cgerr.ExitUsage, "can't read %s (%v), so cg can't tell which extension commands it declares",
			name, err)
		if dir := filepath.Dir(broken); broken == "" || (sroot != "" && (samePath(dir, sroot) || sessionWorktree(sroot, dir))) {
			return nil, cwdErr
		}
		return sessionOnly(sroot), cwdErr
	}
	switch {
	case sroot == "" && root == "":
		return nil, nil
	case sroot == "":
		return []string{root}, nil
	case root == "":
		return []string{sroot}, nil
	case samePath(root, sroot):
		return []string{sroot}, nil
	case sessionWorktree(sroot, root):
		return []string{root}, nil
	default:
		return []string{sroot, root}, nil
	}
}

// brokenSessionRoots handles a CG_WORKSPACE whose .cg.json exists but can't be
// read or decoded (S25): dispatch stops with broken, unless the current
// directory's workspace is one of the session repo's worktrees (S21), whose
// own .cg.json alone counts.
func brokenSessionRoots(sdir string, broken error) ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, broken
	}
	root, _, err := workspace.FindRoot(cwd)
	if err != nil || root == "" || samePath(root, sdir) || !sessionWorktree(sdir, root) {
		return nil, broken
	}
	if dangling := danglingConfig(cwd, root); dangling != "" {
		return nil, fmt.Errorf("%w; %s is a dangling symlink too", broken, dangling)
	}
	return []string{root}, nil
}

// sessionOnly is the session root as a root list, or none.
func sessionOnly(sroot string) []string {
	if sroot == "" {
		return nil
	}
	return []string{sroot}
}

// sessionRoot reads CG_WORKSPACE. It returns the variable's value, the session
// root when the value is usable, and otherwise why it is ignored (S25): a
// relative path, a directory that doesn't exist (e.g. a worktree removed since
// the session started), one without .cg.json, or one whose .cg.json isn't a
// Consigliere workspace's. A .cg.json that exists but can't be read or decoded
// gives no root and no reason: brokenSessionConfig reports it, and dispatch
// stops. Unset, all three are "".
func sessionRoot() (value, root, ignored string) {
	value, root, ignored, _ = readSession()
	return value, root, ignored
}

// brokenSessionConfig returns the session root and the usage error to stop
// with when CG_WORKSPACE names a directory whose .cg.json exists but can't be
// read or decoded; nil otherwise.
func brokenSessionConfig() (dir string, broken error) {
	value, _, _, broken := readSession()
	if broken == nil {
		return "", nil
	}
	return filepath.Clean(value), broken
}

// readSession classifies CG_WORKSPACE (S25): usable (root set), ignored
// (ignored says why), or broken (a .cg.json entry that exists but can't be read
// or doesn't decode to an object: a directory, a dangling symlink, an empty
// file, invalid JSON, null, a non-object, or a field of the wrong type). It
// decodes .cg.json as workspace.Detect does.
func readSession() (value, root, ignored string, broken error) {
	value, set := os.LookupEnv(envWorkspace)
	switch {
	case !set:
		return "", "", "", nil
	case value == "":
		return "", "", "it is empty", nil
	case !filepath.IsAbs(value):
		return value, "", "it is not an absolute path", nil
	}
	dir := filepath.Clean(value)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return value, "", "it names a directory that doesn't exist", nil
		}
		return value, "", "", sessionConfigError(dir, err)
	}
	p := filepath.Join(dir, workspace.ConfigFile)
	if _, err := os.Lstat(p); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return value, "", "it names a directory with no " + workspace.ConfigFile, nil
		}
		return value, "", "", sessionConfigError(dir, err)
	}
	data, err := os.ReadFile(p) //nolint:gosec // the workspace the session named
	if err != nil {
		return value, "", "", sessionConfigError(dir, err)
	}
	trimmed := bytes.TrimSpace(data)
	switch {
	case len(trimmed) == 0:
		return value, "", "", sessionConfigError(dir, errors.New("it is empty"))
	case bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '{':
		return value, "", "", sessionConfigError(dir, errors.New("it isn't a JSON object"))
	}
	var cfg workspace.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return value, "", "", sessionConfigError(dir, err)
	}
	if cfg.Type != workspace.TypeConsigliere {
		return value, "", "its " + workspace.ConfigFile + " isn't a Consigliere workspace's", nil
	}
	return value, dir, "", nil
}

// sessionConfigError is the usage error for a session .cg.json that can't be
// used.
func sessionConfigError(dir string, err error) error {
	return cgerr.New(cgerr.ExitUsage, "can't read %s (%v), the session's workspace (CG_WORKSPACE), so cg can't tell which extension commands it declares",
		filepath.Join(dir, workspace.ConfigFile), err)
}

// servedFromOutside reports whether root, which serves a command, is the
// session root serving it for a current directory outside the session root
// and its worktrees (S22, S24). A worktree of the session's repo never gets
// here: it is consulted alone.
func servedFromOutside(root string) bool {
	_, sroot, _ := sessionRoot()
	if sroot == "" || !samePath(root, sroot) {
		return false
	}
	cwd, err := os.Getwd()
	if err != nil {
		return true
	}
	cwdRoot, _, err := workspace.FindRoot(cwd)
	return err != nil || cwdRoot == "" || !samePath(cwdRoot, sroot)
}

// sessionWorktreeFor returns the session's worktree for a binary the session
// root serves from outside it: the worktree recorded in the session's state
// file (the session ID from CG_SESSION_ID or CLAUDE_CODE_SESSION_ID) while it
// is still one of the session repo's linked worktrees (listed, not the main
// checkout); else the session root itself when it is a
// linked worktree; else "". The state file is the one under the session repo's
// main checkout. git runs in the session root with its repository variables
// removed, and any failure means no worktree.
func sessionWorktreeFor(sroot string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// S38: the recorded worktree. Only the common dir is needed (no
	// --path-format, which needs git 2.31).
	if id := resolveSessionID(""); session.ValidSessionID(id) {
		if out, err := gitx.RunCleanRaw(ctx, sroot, "rev-parse", "--git-common-dir"); err == nil {
			if lines := gitLines(out); len(lines) == 1 {
				common := gitx.OSPath(lines[0])
				if !filepath.IsAbs(common) {
					common = filepath.Join(sroot, common) // relative to where git ran
				}
				// The state file lives under the main checkout: the common dir's parent.
				if c, rerr := session.ReadContext(canonicalPath(filepath.Dir(common)), id); rerr == nil && c != nil && c.Worktree != "" &&
					sessionRepoWorktree(ctx, sroot, c.Worktree) {
					return c.Worktree
				}
			}
		}
	}
	// S38b: the session root itself when it is a linked worktree, decided
	// exactly as the spec states.
	out, err := gitx.RunCleanRaw(ctx, sroot, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-dir", "--git-common-dir")
	if err != nil {
		return ""
	}
	lines := gitLines(out)
	if len(lines) != 3 {
		return ""
	}
	if linkedWorktreeRoot(sroot, gitx.OSPath(lines[0]), gitx.OSPath(lines[1]), gitx.OSPath(lines[2])) {
		return sroot
	}
	return ""
}

// linkedWorktreeRoot reports whether session is a linked worktree's root: its
// top level is session, and its git dir differs from the common dir, both
// compared after resolving symlinks. Anything that can't be resolved means no.
func linkedWorktreeRoot(sroot, top, gitDir, common string) bool {
	if !samePath(top, sroot) {
		return false
	}
	g, err := filepath.EvalSymlinks(gitDir)
	if err != nil {
		return false
	}
	c, err := filepath.EvalSymlinks(common)
	if err != nil {
		return false
	}
	return !samePath(g, c)
}

// gitLines splits git's output into lines, dropping only the final line
// ending (and CRs of CRLF endings): paths keep leading or trailing spaces.
func gitLines(out []byte) []string {
	s := strings.TrimSuffix(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
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
func sessionWorktree(sroot, dir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// No --path-format or -z: they need git 2.31 and 2.36.
	// Raw output, split by line: a path may begin or end with a space.
	out, err := gitx.RunCleanRaw(ctx, sroot, "rev-parse", "--show-toplevel", "--git-common-dir")
	if err != nil {
		return false
	}
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n"), "\n")
	if len(lines) != 2 || !samePath(gitx.OSPath(lines[0]), sroot) {
		return false
	}
	common := gitx.OSPath(lines[1])
	if !filepath.IsAbs(common) {
		common = filepath.Join(sroot, common) // relative to where git ran
	}
	return listedWorktreeOf(ctx, sroot, common, dir)
}

// sessionRepoRoot is the session repo root (spec Terms): the session root
// when CG_WORKSPACE isn't ignored (usable, or naming a .cg.json that can't be
// read), else the current directory's workspace root, else "". It decides the
// badge root and the worktree record; dispatch uses the session root alone.
func sessionRepoRoot(cwd string) string {
	value, _, ignored, _ := readSession()
	if value != "" && ignored == "" {
		return filepath.Clean(value)
	}
	root, _, err := workspace.FindRoot(cwd)
	if err != nil {
		return ""
	}
	return root
}

// sessionCommandsRoot is the session commands root (spec Terms, S38c): the
// session repo root, except when the current directory's workspace is another
// Consigliere workspace than the session's (not the session root and not one
// of its linked worktrees, decided as dispatch does, S21–S23): then that
// workspace. Only the session commands use it; the worktree record and
// dispatch keep the session repo root.
func sessionCommandsRoot(cwd string) string {
	srr := sessionRepoRoot(cwd)
	value, _, ignored, _ := readSession()
	if value == "" || ignored != "" {
		return srr // the current directory's workspace already
	}
	sroot := filepath.Clean(value)
	root, _, err := workspace.FindRoot(cwd)
	if err != nil || root == "" || samePath(root, sroot) || sessionWorktree(sroot, root) {
		return srr
	}
	return root
}

// sessionRepoWorktree reports whether dir is one of the linked worktrees of
// the repo at repoRoot: listed by `git worktree list --porcelain` run in
// repoRoot with git's repository variables removed, not the main checkout (the
// list's first entry), compared after resolving symlinks, and its .git still
// pointing back into the repo (gitDirIn). Any failure means no.
func sessionRepoWorktree(ctx context.Context, repoRoot, dir string) bool {
	_, ok := sessionRepoWorktreePath(ctx, repoRoot, dir)
	return ok
}

// sessionRepoWorktreePath is sessionRepoWorktree that also returns the path
// as git lists it (its spelling, which may differ from dir's in case or
// symlinks).
func sessionRepoWorktreePath(ctx context.Context, repoRoot, dir string) (string, bool) {
	out, err := gitx.RunCleanRaw(ctx, repoRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", false
	}
	lines := gitLines(out)
	if len(lines) != 1 {
		return "", false
	}
	common := gitx.OSPath(lines[0])
	if !filepath.IsAbs(common) {
		common = filepath.Join(repoRoot, common) // relative to where git ran
	}
	list, err := gitx.RunCleanRaw(ctx, repoRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return "", false
	}
	first := true
	for _, line := range strings.Split(string(list), "\n") {
		p, ok := strings.CutPrefix(strings.TrimSuffix(line, "\r"), "worktree ")
		if !ok {
			continue
		}
		p = gitx.OSPath(p)
		if first { // the main checkout
			first = false
			if samePath(p, dir) {
				return "", false
			}
			continue
		}
		if samePath(p, dir) {
			return p, gitDirIn(dir, common)
		}
	}
	return "", false
}

// listedWorktreeOf reports whether dir is listed by `git worktree list` run in
// the session root, and its .git still belongs to the repository whose common
// git directory is common (gitDirIn).
func listedWorktreeOf(ctx context.Context, sroot, common, dir string) bool {
	list, err := gitx.RunCleanRaw(ctx, sroot, "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	// One attribute per line; the path is taken without trimming (a leading
	// or trailing space is part of it), only a CRLF line ending dropped. A
	// path containing a newline can't be read this way, so it never matches:
	// that worktree counts as another workspace.
	for _, line := range strings.Split(string(list), "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSuffix(line, "\r"), "worktree "); ok && samePath(gitx.OSPath(p), dir) {
			return gitDirIn(dir, common)
		}
	}
	return false
}

// gitDirIn reports whether dir's .git belongs to the repository whose common
// git directory is common: .git is that directory (the main checkout), or a
// file whose "gitdir:" names an entry of its worktrees/ directory whose own
// gitdir file names dir's .git back (a linked worktree). (A main checkout made
// with --separate-git-dir never gets here: git lists it under the git
// directory's path, not the checkout's.)
func gitDirIn(dir, common string) bool {
	dotGit := filepath.Join(dir, ".git")
	info, err := os.Stat(dotGit) //nolint:gosec // a fixed name in a listed worktree
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

// danglingConfig names the first .cg.json from dir upwards, up to and
// including stop (the root FindRoot found; "" walks to the filesystem root),
// that is an entry whose target doesn't exist (a dangling symlink), or "".
func danglingConfig(dir, stop string) string {
	for d := filepath.Clean(dir); ; {
		p := filepath.Join(d, workspace.ConfigFile)
		if _, lerr := os.Lstat(p); lerr == nil {
			if _, serr := os.Stat(p); errors.Is(serr, fs.ErrNotExist) {
				return p
			}
		}
		if stop != "" && d == filepath.Clean(stop) {
			return ""
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// unreadableConfig names the first .cg.json from dir upwards that
// workspace.Detect can't read, the one FindRoot stopped at, or "" when none
// fails any more (a transient error).
func unreadableConfig(dir string) string {
	for d := filepath.Clean(dir); ; {
		cfg, err := workspace.Detect(d)
		if err != nil {
			return filepath.Join(d, workspace.ConfigFile)
		}
		if cfg != nil || workspace.HasStructuralMarkers(d) {
			return "" // where FindRoot would stop now: nothing fails any more
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
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
	// badPath says the extension's .cg.json entry has an invalid path: only
	// editing it helps.
	badPath bool
	// mismatch says the tree is intact but the ledger disagrees with its
	// manifest: cg extension update rewrites the ledger (removing the tree,
	// which other workspaces share, would only break them).
	mismatch bool
	// legacy says the entry records no commit: its source is the legacy
	// clone, and cg init reinstalls it from the default branch.
	legacy bool
	// tree is the extension's whole pinned tree in the store (its source
	// root, not the subdirectory of a co-located extension): what cg init
	// checks before restoring.
	tree string
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
			dir, why string
			m        *extension.Manifest
			checked  bool
			badPath  bool
		)
		tree := extension.SourceRoot(e)
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
			entry := commandEntry{ext: c, name: e.Name, unverified: why, badPath: badPath, tree: tree, legacy: !extension.ValidCommit(e.Commit)}
			if why == "" {
				entry.bin = filepath.Join(dir, "bin", c.Binary)
				switch {
				case !slices.Contains(m.Contributes.Commands, c):
					entry.unverified = "its manifest doesn't declare that command with that binary"
					entry.mismatch = true
				case !verifiedCommand(dir, m, c):
					entry.unverified = "its binary " + entry.bin + " is missing"
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
		// A damaged tree (manifest unreadable, binary missing): cg init
		// restores only a tree that is missing, and re-initialises an
		// existing workspace only with --force, so it has to go first; other
		// workspaces sharing it were damaged too and re-export it the same
		// way. update moves to the latest version instead.
		fix := fmt.Sprintf("remove %s if it is still there and run `cg init --force` in %s to re-export the recorded version, or run `cg extension update %s` there to move to its latest",
			best.tree, root, best.name)
		switch {
		case best.badPath:
			fix = fmt.Sprintf("correct its entry in %s", filepath.Join(root, workspace.ConfigFile))
		case best.legacy && !best.mismatch:
			fix = fmt.Sprintf("remove %s if it is still there and run `cg init --force` in %s to reinstall it from its default branch, or run `cg extension update %s` there",
				best.tree, root, best.name)
		case best.mismatch:
			fix = fmt.Sprintf("run `cg extension update %s` in %s, which moves it to its latest version and rewrites its record (its tree is intact and shared with other workspaces, so leave it)", best.name, root)
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
	if r.sessionWorktree != "" {
		env = append(env, envSessionWorktree+"="+r.sessionWorktree)
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
	state := "CG_WORKSPACE is not set"
	switch value, _, ignored := sessionRoot(); {
	case ignored != "":
		state = fmt.Sprintf("CG_WORKSPACE=%q is ignored: %s", value, ignored)
	case value != "":
		state = fmt.Sprintf("CG_WORKSPACE is %q", value)
	}
	return "If it is an extension's command: it runs inside the workspace that installed it, " +
		"and from any directory in a session started with `cg` in that workspace, which carries the workspace along (" + state + ")."
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

// fallbackNamespace folds a typed namespace to the name the PATH lookup would
// match: lower-cased where the filesystem is case-insensitive (macOS,
// Windows), and on Windows without an extension.
func fallbackNamespace(ns string) string {
	return lookupName(ns, foldsCase, isWindowsOS)
}

// lookupName folds ns as the PATH lookup would: case where it folds, and on
// Windows trailing dots and spaces (the file system drops them, so
// `cg shop.exe.` runs cg-shop.exe) and any extension (LookPath tries a name
// that already has one as is, so cg-shop.bat is reachable as `cg shop.bat`
// whatever PATHEXT says).
func lookupName(ns string, fold, windows bool) string {
	if fold {
		ns = strings.ToLower(ns)
	}
	if windows {
		ns = strings.TrimRight(ns, ". ")
		ns = strings.TrimRight(strings.TrimSuffix(ns, filepath.Ext(ns)), ". ")
	}
	return ns
}

// foldsCase says the PATH lookup matches names case-insensitively, as the
// default filesystems on macOS and Windows do.
const foldsCase = isWindowsOS || runtime.GOOS == "darwin"

// declaredSpelling returns the namespace a consulted root declares that the
// typed namespace spells differently but the PATH lookup would still match
// (S33), or "".
func declaredSpelling(roots []string, typed string) string {
	folded := fallbackNamespace(typed)
	if folded == typed {
		return ""
	}
	for _, root := range roots {
		if entries, err := namespaceEntries(root, folded); err == nil && len(entries) > 0 {
			return folded
		}
	}
	return ""
}
