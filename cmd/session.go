package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/session"
	"github.com/mnemcik/consigliere/internal/workspace"
	"github.com/mnemcik/consigliere/internal/worktree"
)

func init() {
	sessionCmd.AddCommand(sessionEndCmd)
	sessionCmd.AddCommand(sessionMarkDirtyCmd)
	sessionCmd.AddCommand(sessionPauseCmd)
	sessionCmd.AddCommand(sessionPullLatestCmd)
	sessionCmd.AddCommand(sessionReleaseCmd)
	sessionCmd.AddCommand(sessionSetContextCmd)
	sessionCmd.AddCommand(sessionStartGateCmd)
	sessionCmd.AddCommand(sessionStatuslineCmd)
	rootCmd.AddCommand(sessionCmd)
}

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Claude Code hook bodies and session badge state",
	Long: `Bodies for the Claude Code hooks that the framework ships as thin bash
wrappers. Each reads the hook's stdin JSON and writes the hook's expected
stdout; they are designed to never fail the session, so operational problems
are reported in-band rather than via a non-zero exit.`,
}

// sessionEndInput is the SessionEnd hook payload the claim release consumes.
type sessionEndInput struct {
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
}

var sessionEndCmd = &cobra.Command{
	Use:   "end",
	Short: "SessionEnd hook: release a clean, unpaused session claim",
	Long: `Hook body for Claude Code's SessionEnd event (exit, /clear, /resume, logout).
Releases the session's claim unless the session has unwrapped work (dirty) or
paused its project, so cg active stops listing a session that ended without a
wrap. It prints nothing on success, because Claude Code shows a SessionEnd
hook's output to the user.`,
	Args:   cobra.NoArgs,
	RunE:   runSessionEnd,
	Hidden: true, // invoked by the hook wrapper, not interactively
}

func runSessionEnd(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true

	var in sessionEndInput
	if err := decodeStdin(cmd.InOrStdin(), &in); err != nil || !session.ValidSessionID(in.SessionID) {
		return nil // malformed / no session — never fail the hook
	}
	cwd := in.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	// The claim may be in either file (made in another workspace, or in the
	// session's own before moving there): end it in both. A root without the
	// session's file is skipped before EndSession's git calls and lock wait,
	// which share the hook's short time budget.
	for _, r := range sessionStateRoots(cmd.Context(), cwd) {
		if _, err := os.Lstat(session.ContextFile(r.badge, in.SessionID)); err != nil {
			continue
		}
		if _, err := session.EndSession(r.badge, r.ws, in.SessionID); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "cg session end: %v\n", err)
		}
	}
	return nil
}

// markDirtyInput is the PostToolUse hook payload the dirty-marker consumes.
type markDirtyInput struct {
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
	ToolInput struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	} `json:"tool_input"`
}

var sessionMarkDirtyCmd = &cobra.Command{
	Use:    "mark-dirty",
	Short:  "PostToolUse hook: flag the session as having unwrapped work",
	Args:   cobra.NoArgs,
	RunE:   runSessionMarkDirty,
	Hidden: true, // invoked by the hook wrapper, not interactively
}

func runSessionMarkDirty(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true

	var in markDirtyInput
	if err := decodeStdin(cmd.InOrStdin(), &in); err != nil || in.SessionID == "" {
		return nil // malformed / no session — never fail the hook
	}

	cwd := in.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	roots := sessionStateRoots(cmd.Context(), cwd)

	// Ignore writes to the badge file itself — framework bookkeeping isn't work.
	filePath := in.ToolInput.FilePath
	if filePath == "" {
		filePath = in.ToolInput.NotebookPath
	}
	for _, r := range roots {
		if session.IsContextPath(r.badge, filePath) {
			return nil
		}
	}

	// Mark the session's file wherever it has one: the work may belong to
	// either claim, or to the worktree record in the session repo root's
	// file, and a file left clean is released at end. An older claim marked
	// too only lingers in cg active until pruned. A root without the file is
	// skipped, so no lock file is left there.
	for _, r := range roots {
		if _, err := os.Lstat(session.ContextFile(r.badge, in.SessionID)); err != nil {
			continue
		}
		if err := session.MarkDirty(r.badge, in.SessionID); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "cg session mark-dirty: %v\n", err)
		}
	}
	return nil
}

var sessionPullLatestCmd = &cobra.Command{
	Use:    "pull-latest",
	Short:  "SessionStart hook: fast-forward the main worktree's landing branch",
	Args:   cobra.NoArgs,
	RunE:   runSessionPullLatest,
	Hidden: true,
}

func runSessionPullLatest(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true

	// Drain stdin so the hook driver never gets a SIGPIPE; we need no field.
	_, _ = io.Copy(io.Discard, cmd.InOrStdin())

	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	// Claude Code reads one JSON object from a hook, so every part of the
	// report is collected and emitted once, at the end.
	var notices []string
	var claudeContext string
	defer func() {
		// A fresh clone has the contributed git-hook scripts (versioned) but
		// not the dispatcher in .git/hooks (unversioned); this runs at every
		// session start, so reconcile here and say so only when something is
		// wrong. It runs after the pull, so it approves the scripts the pull
		// brought in, and so does the pending-files check below.
		if msgs := ensureGitHooks(cmd.Context(), cwd); len(msgs) > 0 {
			notices = append(notices, "cg: "+strings.Join(msgs, "; "))
		}
		if user, claude := pendingFilesNotice(cmd.Context(), cwd); user != "" {
			notices = append(notices, user)
			claudeContext = claude
		}
		emitHookOutput(cmd.OutOrStdout(), strings.Join(notices, "\n"), claudeContext)
	}()
	landingBranch := workspace.DefaultLandingBranch
	if root, cerr := gitx.CommonRoot(cmd.Context(), cwd); cerr == nil {
		var configured string
		if cfg, derr := workspace.Detect(root); derr == nil {
			landingBranch = cfg.WorktreeSettings().LandingBranch
			configured = cfg.ConfiguredLandingStrategy()
		}
		// Local landing moves the main worktree's branch itself, so there is
		// nothing to pull, and no origin to fetch from.
		if worktree.EffectiveStrategy(cmd.Context(), root, configured) == workspace.StrategyLocal {
			return nil
		}
	}

	res := session.PullLatest(cmd.Context(), cwd, landingBranch)
	if res.SystemMessage != "" {
		notices = append(notices, res.SystemMessage)
	}
	return nil
}

// pendingFilesNotice reports extension files that both the user and the
// extension changed (see `cg extension pending`). It returns a one-line notice
// for the user, and context for Claude: Claude raises the files with the user
// and recommends how to settle each. Both are empty when nothing is pending.
func pendingFilesNotice(ctx context.Context, dir string) (user, claude string) {
	root, err := gitx.CommonRoot(ctx, dir)
	if err != nil {
		return "", ""
	}
	cfg, err := workspace.Detect(root)
	if err != nil || cfg == nil {
		return "", ""
	}
	rows := pendingRows(extension.PendingAll(root, cfg.Extensions))
	if len(rows) == 0 {
		return "", ""
	}
	const maxListed = 30
	var list []string
	for i, r := range rows {
		if i == maxListed {
			list = append(list, fmt.Sprintf("and %d more (`cg extension pending` lists all)", len(rows)-maxListed))
			break
		}
		list = append(list, r.Extension+" "+r.File)
	}
	files := strings.Join(list, ", ")
	user = fmt.Sprintf("cg: %d extension file(s) differ from the extension's version and need your decision: %s", len(rows), files)
	// The procedure comes first, so a long file list can never push it out of
	// what Claude reads.
	claude = `cg: some contributed extension files were changed both by the user and by the extension, so cg kept the user's version and is waiting for a decision. Raise this with the user now, one file at a time. Nothing changes until they decide.
1. Run ` + "`cg extension diff <extension> <file>`" + `. It shows the extension's change and the user's change, both against what cg installed, and whether a three-way merge is clean. It also says when the installed version is uncertain or unknown, or the user deleted the file.
2. Recommend one option, with a one-line reason:
   - merge (` + "`cg extension resolve <extension> <file> --merge`" + `) when the two changes don't overlap and the merge is clean; check the merged result makes sense (valid JSON or YAML where the file is one). It is refused unless the merge is clean.
   - keep (` + "`--keep`" + `) when the user's change deliberately overrides what the extension changed.
   - take (` + "`--take`" + `) when the user's change was incidental or is now obsolete.
3. Run the option the user chooses. Each makes the extension's version the new baseline, so the file is raised again only when the extension changes it again.

Files: ` + files
	return user, claude
}

// emitHookOutput writes the hook's single JSON object: systemMessage for the
// user and, when set, additionalContext for Claude. It writes nothing when
// both are empty.
func emitHookOutput(w io.Writer, systemMessage, additionalContext string) {
	if systemMessage == "" && additionalContext == "" {
		return
	}
	type specific struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	}
	out := struct {
		SystemMessage      string    `json:"systemMessage,omitempty"`
		HookSpecificOutput *specific `json:"hookSpecificOutput,omitempty"`
	}{SystemMessage: systemMessage}
	if additionalContext != "" {
		out.HookSpecificOutput = &specific{HookEventName: "SessionStart", AdditionalContext: additionalContext}
	}
	data, err := json.Marshal(out)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintln(w, string(data))
}

var (
	setContextSessionID string
	setContextArea      string
	setContextProject   string
	setContextWorktree  string
)

var sessionSetContextCmd = &cobra.Command{
	Use:   "set-context",
	Short: "Record the session's area and project (status-line badge) or its worktree",
	Long: `Writes the area and project a session is working on to its badge state
file (.claude/session-context/<session-id>.json under the main worktree root
of the session's workspace: CG_WORKSPACE, else the current one),
creating it when absent. The status line renders the badge from this file and
cg active lists sessions from it. Run it once the session-start gate's area and
project are confirmed, and again whenever the session switches either one.
Existing fields such as the dirty flag are preserved. It refuses to run
outside a Consigliere workspace. The session ID must match [A-Za-z0-9_-]+.
The file format is described in docs/cg-subcommands.md, "Session badge file".

When the project is paused (projects/<project>/resume.md exists in any worktree
of the workspace), this session is resuming it, so the pausing session's claim
is released, and the path of each resume.md is printed, with the worktree that
holds the paused work.

--worktree records the session's worktree, so the workspace's extension
commands write there when the session runs them from other directories. cg
worktree create records it already; use this when the workspace's own scripts
create worktrees. The path must be one of the workspace repository's linked
worktrees (in git worktree list, and not the main checkout, so writers are
never sent there). It may be given alone, or with --area and --project.

` + sessionIDHelp,
	Example: `  cg session set-context --session-id 1b2c... --area platform --project api-gateway
  cg session set-context --worktree "$(pwd)"`,
	Args: cobra.NoArgs,
	RunE: runSessionSetContext,
}

func init() {
	f := sessionSetContextCmd.Flags()
	f.StringVar(&setContextSessionID, "session-id", "", "session ID (shown in the session-start gate; see below for defaults)")
	f.StringVar(&setContextArea, "area", "", "area slug the session works in")
	f.StringVar(&setContextProject, "project", "", "project slug the session works on")
	f.StringVar(&setContextWorktree, "worktree", "", "path of the session's worktree (one of the repository's linked worktrees, not the main checkout)")
	sessionSetContextCmd.MarkFlagsRequiredTogether("area", "project")
	sessionReleaseCmd.Flags().StringVar(&releaseSessionID, "session-id", "", "session ID (see below for defaults)")
	sessionPauseCmd.Flags().StringVar(&pauseSessionID, "session-id", "", "session ID (see below for defaults)")
}

var pauseSessionID string

var sessionPauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Mark the session as the one that paused its project (pause-mode wrap)",
	Long: `Sets the pause marker on the session's badge, so cg active, the resume
hand-over and the SessionEnd hook can tell the session that paused a project
from another session that worked on it. Run it when a session pauses, which
the pause-mode wrap does after writing resume.md. The session keeps its claim.
A session without a badge file is left alone. Claiming a project with
cg session set-context clears the marker.

` + sessionIDHelp,
	Example: `  cg session pause --session-id 1b2c...`,
	Args:    cobra.NoArgs,
	RunE:    runSessionPause,
}

func runSessionPause(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	id := resolveSessionID(pauseSessionID)
	if !session.ValidSessionID(id) {
		return fmt.Errorf("invalid or missing session ID %q (pass --session-id or set CG_SESSION_ID)", id)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, wsRoot, _ := sessionCommandRoots(cmd.Context(), cwd)
	if wsRoot == "" || root == "" {
		return fmt.Errorf("not inside a Consigliere workspace: %s", cwd)
	}
	marked, err := session.MarkPaused(root, id, time.Now())
	if err != nil {
		return err
	}
	if marked {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Session paused: %s\n", id)
	} else {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No badge for session %s; nothing to mark\n", id)
	}
	return nil
}

// sessionIDHelp documents resolveSessionID for every command that takes one.
const sessionIDHelp = `The session ID comes from --session-id, else $CG_SESSION_ID, else
$CLAUDE_CODE_SESSION_ID (set by Claude Code). Other agents set CG_SESSION_ID
or pass the flag.`

// resolveSessionID picks the session ID from the flag, then the agent-neutral
// CG_SESSION_ID, then Claude Code's CLAUDE_CODE_SESSION_ID. It returns "" when
// none is set.
func resolveSessionID(flag string) string {
	for _, v := range []string{flag, os.Getenv("CG_SESSION_ID"), os.Getenv("CLAUDE_CODE_SESSION_ID")} {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

var releaseSessionID string

var sessionReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "End the session's claim on its project (end-mode wrap)",
	Long: `Deletes the session's badge state file, so the status-line badge clears and
cg active stops listing the session. Run it when a session is finished, which
the end-mode wrap does. Do not run it on a pause: a paused session keeps its
claim until another session resumes the project. Releasing a session that has
no badge file succeeds.

` + sessionIDHelp,
	Example: `  cg session release --session-id 1b2c...`,
	Args:    cobra.NoArgs,
	RunE:    runSessionRelease,
}

func runSessionRelease(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	id := resolveSessionID(releaseSessionID)
	if !session.ValidSessionID(id) {
		return fmt.Errorf("invalid or missing session ID %q (pass --session-id or set CG_SESSION_ID)", id)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, wsRoot, _ := sessionCommandRoots(cmd.Context(), cwd)
	if wsRoot == "" || root == "" {
		return fmt.Errorf("not inside a Consigliere workspace: %s", cwd)
	}
	if err := session.Release(root, id); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Session released: %s\n", id)
	return nil
}

func runSessionSetContext(cmd *cobra.Command, _ []string) error {
	// Flag values are validated below; a bad value is not a usage error.
	cmd.SilenceUsage = true
	id := resolveSessionID(setContextSessionID)
	if !session.ValidSessionID(id) {
		return fmt.Errorf("invalid or missing session ID %q (pass --session-id or set CG_SESSION_ID)", id)
	}
	// Flag checks only test presence, so --area "" would slip through.
	setContextArea = strings.TrimSpace(setContextArea)
	setContextProject = strings.TrimSpace(setContextProject)
	claim := cmd.Flags().Changed("area") || cmd.Flags().Changed("project")
	worktreeSet := cmd.Flags().Changed("worktree")
	switch {
	case !claim && !worktreeSet:
		return fmt.Errorf("give --area and --project, or --worktree")
	case claim && (setContextArea == "" || setContextProject == ""):
		return fmt.Errorf("--area and --project must be non-empty")
	case worktreeSet && strings.TrimSpace(setContextWorktree) == "":
		// filepath.Abs("") is the current directory: an unset shell variable
		// must not record wherever the command happens to run.
		return fmt.Errorf("--worktree must be non-empty")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// Refuse outside a workspace: inside any other git repo the badge root
	// would still resolve, leaving a stray .claude/ the status line never reads.
	// The claim goes to the session commands root's state file (S38c); the
	// worktree record stays with the session repo root's, wherever this runs
	// (S36).
	root, wsRoot, cfg := sessionCommandRoots(cmd.Context(), cwd)
	if wsRoot == "" || root == "" {
		return fmt.Errorf("not inside a Consigliere workspace: %s", cwd)
	}
	out := cmd.OutOrStdout()
	// Validate the worktree before writing anything, and record it after the
	// claim, so a refused path or a failed claim changes nothing about it.
	var wtPath, wtBadge string
	if worktreeSet {
		badge, repoRoot, _ := sessionRoots(cmd.Context(), cwd)
		if repoRoot == "" || badge == "" {
			return fmt.Errorf("not inside a Consigliere workspace: %s", cwd)
		}
		p, werr := listedWorktree(cmd.Context(), repoRoot, setContextWorktree)
		if werr != nil {
			return werr
		}
		wtPath, wtBadge = p, badge
	}
	recordWorktree := func() error {
		if !worktreeSet {
			return nil
		}
		if err := session.SetWorktree(wtBadge, id, wtPath); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "Session worktree set: %s (%s)\n", wtPath, session.ContextFile(wtBadge, id))
		return nil
	}
	if !claim {
		return recordWorktree()
	}

	if err := session.WriteContext(root, id, session.Claim{Area: setContextArea, Project: setContextProject}); err != nil {
		return err
	}
	// Right after the claim, before the hand-over steps that may fail.
	if err := recordWorktree(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "Session badge set: [%s/%s] (%s)\n",
		setContextArea, setContextProject, session.ContextFile(root, id))
	retired, err := session.HandOver(root, wsRoot, id, setContextProject)
	if err != nil {
		return err
	}
	for _, sid := range retired {
		_, _ = fmt.Fprintf(out, "Resuming paused project: released session %s\n", sid)
	}
	resumes, err := session.FindResume(wsRoot, setContextProject)
	if err != nil {
		return err
	}
	printResumeNotice(out, setContextProject, resumes, cfg.WorktreeSettings().BranchPrefix)
	return nil
}

// listedWorktree resolves p (relative to the current directory) and, when it is
// one of the linked worktrees of the session repo whose root is repoRoot (S36:
// listed by its `git worktree list`, not the main checkout), returns git's
// listed spelling of it in recordPath form; otherwise an error.
func listedWorktree(ctx context.Context, repoRoot, p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	listed, ok := sessionRepoWorktreePath(ctx, repoRoot, abs)
	if !ok {
		return "", fmt.Errorf("%s is not one of the session repository's linked worktrees (see `git worktree list` in %s; the main checkout doesn't count)", abs, repoRoot)
	}
	return recordPath(listed), nil // the form create and remove record
}

// printResumeNotice tells the agent claiming a paused project where its pickup
// context is and how to pick it up. The pause-mode wrap leaves resume.md in the
// session worktree, on a branch that is not landed, so an agent started in the
// main checkout would not find it at projects/<slug>/resume.md there. The
// worktree's session slug can differ from the project slug, so the command to
// re-enter it is derived from its branch.
func printResumeNotice(out io.Writer, project string, resumes []session.Resume, branchPrefix string) {
	if len(resumes) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "Project %s is paused (/wrap pause). To resume it:\n", project)
	entered := false
	for _, r := range resumes {
		if !r.Checkout.Own {
			if !entered {
				if slug, ok := strings.CutPrefix(r.Checkout.Branch, branchPrefix); ok && slug != "" && branchPrefix != "" {
					_, _ = fmt.Fprintf(out, "  - re-enter its worktree: cg worktree create %s\n", slug)
				}
				// cg worktree create prints the path but does not change directory.
				_, _ = fmt.Fprintf(out, "  - move into %s and do all further work there (Claude Code: EnterWorktree with that path),\n"+
					"    not in this checkout\n", shellQuote(r.Checkout.Dir))
				entered = true
			} else {
				_, _ = fmt.Fprintf(out, "  - another pause cursor for it is in %s\n", shellQuote(r.Checkout.Dir))
			}
		}
		_, _ = fmt.Fprintf(out, "  - read its pickup context: %s\n", shellQuote(r.File))
	}
	_, _ = fmt.Fprintln(out, "  - once the user confirms work has resumed, delete each pickup file listed above in the\n"+
		"    checkout that holds it and commit only that deletion; cg worktree land refuses to land it:")
	for _, r := range resumes {
		// -f: the pause-mode wrap edits resume.md after its WIP commit (the SHA),
		// and plain git rm refuses a file with local modifications. The commit
		// pathspec leaves anything else staged in that checkout (in the main
		// checkout, possibly another session's files) out of the commit.
		dir, file := shellQuote(r.Checkout.Dir), shellQuote(r.File)
		msg := shellQuote("Resume " + project + ": drop pause cursor")
		_, _ = fmt.Fprintf(out, "      git -C %s rm -f -- %s && git -C %s commit -m %s -- %s\n",
			dir, file, dir, msg, file)
	}
}

// shellQuote single-quotes a path for a POSIX shell, so nothing in it is
// expanded or split: spaces, $, backticks and double quotes stay literal. A
// single quote in the path closes the quoting, is escaped, and reopens it.
// Agents run these commands in a POSIX shell (Claude Code uses Git Bash on
// Windows), where backslashes inside single quotes are literal too, so a
// Windows path is unchanged.
func shellQuote(p string) string { return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'" }

// startGateInput is the UserPromptSubmit hook payload the gate consumes.
type startGateInput struct {
	Prompt    string `json:"prompt"`
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
}

var sessionStartGateCmd = &cobra.Command{
	Use:    "start-gate",
	Short:  "UserPromptSubmit hook: emit the session-start gate reminder",
	Args:   cobra.NoArgs,
	RunE:   runSessionStartGate,
	Hidden: true,
}

func runSessionStartGate(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true

	var in startGateInput
	if err := decodeStdin(cmd.InOrStdin(), &in); err != nil {
		return nil
	}
	cwd := in.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	// Prompts the gate lets through need no roots (and no git); stale badge
	// pruning waits for the next prompt that shows the gate.
	if session.SkipsPrompt(in.Prompt) {
		return nil
	}

	// Badge files live at the main worktree root of the session repo root
	// (CG_WORKSPACE, else cwd's workspace), as for every other badge command,
	// so a session working in an external repo is told its own file. Without
	// a session repo root, fall back to cwd's repository.
	root, wsRoot, cfg := sessionCommandRoots(cmd.Context(), cwd)
	if root == "" {
		var err error
		if root, err = gitx.CommonRoot(cmd.Context(), cwd); err != nil {
			root, _, _ = workspace.FindRoot(cwd)
		}
		cfg, _ = workspace.Detect(root)
	}
	s := cfg.SessionSettings()
	// A relative template belongs to the directory whose .cg.json named it:
	// wsRoot when its own .cg.json was read, else root (sessionRoots' fallback).
	tpl := s.GateTemplate
	if tpl != "" && !filepath.IsAbs(tpl) {
		base := root
		if wsRoot != "" {
			if c, derr := workspace.Detect(wsRoot); derr == nil && c != nil {
				base = wsRoot
			}
		}
		tpl = filepath.Join(base, tpl)
	}

	text, emit := session.Gate(cmd.Context(), root, &session.GateInput{
		Prompt:        in.Prompt,
		SessionID:     in.SessionID,
		CWD:           cwd,
		WorkspaceRoot: wsRoot,
	}, tpl, s.PruneDays)
	if emit {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), text)
	}
	return nil
}

// statuslineInput is the statusLine hook payload the renderer consumes.
type statuslineInput struct {
	CWD       string `json:"cwd"`
	SessionID string `json:"session_id"`
}

var sessionStatuslineCmd = &cobra.Command{
	Use:    "statusline",
	Short:  "statusLine hook: render the area/project badge",
	Args:   cobra.NoArgs,
	RunE:   runSessionStatusline,
	Hidden: true,
}

func runSessionStatusline(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true

	raw, _ := io.ReadAll(cmd.InOrStdin())
	var in statuslineInput
	_ = json.Unmarshal(raw, &in)
	cwd := in.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// The badge is the session commands root's claim, else the session repo
	// root's (one made before moving to another workspace), which is
	// resolved only when needed: this renders often.
	root, wsRoot, cfg := sessionCommandRoots(cmd.Context(), cwd)
	if !holdsClaim(root, in.SessionID) {
		if rr := sessionRepoRoot(cwd); rr != "" && rr != wsRoot {
			if r, _, rcfg := rootsOf(cmd.Context(), rr); r != "" && r != root && holdsClaim(r, in.SessionID) {
				root, cfg = r, rcfg
			}
		}
	}
	s := cfg.SessionSettings()

	out := session.Statusline(cmd.Context(), root, session.StatuslineInput{
		CWD:       cwd,
		SessionID: in.SessionID,
		Raw:       raw,
	}, s.StatuslineUpstream, s.BadgeFormat)
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
	return nil
}

// sessionRoots resolves where a session's badge file lives and which workspace
// the session serves. wsRoot is the session repo root: the session's workspace
// (CG_WORKSPACE, when it isn't ignored), else the walk-up workspace root of cwd
// ("" when there is neither), so a session working in an external repo still
// reads and writes its own badge. badgeRoot is the main worktree root of
// wsRoot's repository (where start-gate puts badge files), so a session in a
// linked worktree still finds its badge; it falls back to wsRoot outside a git
// repo. cfg is the workspace config, read from wsRoot and else from
// badgeRoot, so a workspace nested below the git top level keeps its settings.
func sessionRoots(ctx context.Context, cwd string) (badgeRoot, wsRoot string, cfg *workspace.Config) {
	return rootsOf(ctx, sessionRepoRoot(cwd))
}

// sessionCommandRoots is sessionRoots for the session commands S38c lists
// (start gate, set-context's claim, pause, release, cg active): from the
// session commands root, so another Consigliere workspace the agent works in
// keeps its own session state. end, mark-dirty and the status line reach both
// roots' files (sessionStateRoots, runSessionStatusline).
func sessionCommandRoots(ctx context.Context, cwd string) (badgeRoot, wsRoot string, cfg *workspace.Config) {
	return rootsOf(ctx, sessionCommandsRoot(cwd))
}

// stateRoot is a badge root and the workspace root it belongs to.
type stateRoot struct{ badge, ws string }

// sessionStateRoots returns the roots of the session commands root and the
// session repo root, each once and in that order, for the commands S38c
// doesn't switch that must reach the session's file under either root (end,
// mark-dirty). Each root is resolved once.
func sessionStateRoots(ctx context.Context, cwd string) []stateRoot {
	var out []stateRoot
	cr, rr := sessionCommandsRoot(cwd), sessionRepoRoot(cwd)
	for _, ws := range []string{cr, rr} {
		if ws == "" || slices.ContainsFunc(out, func(r stateRoot) bool { return r.ws == ws }) {
			continue
		}
		badge, wsRoot, _ := rootsOf(ctx, ws)
		if badge == "" || slices.ContainsFunc(out, func(r stateRoot) bool { return r.badge == badge }) {
			continue
		}
		out = append(out, stateRoot{badge, wsRoot})
	}
	return out
}

// holdsClaim reports whether the state file for id under the badge root
// root can be read and holds a claim (an area or a project, as the status
// line shows).
func holdsClaim(root, id string) bool {
	if root == "" {
		return false
	}
	c, err := session.ReadContext(root, id)
	return err == nil && c != nil && (c.Area != "" || c.Project != "")
}

// rootsOf is sessionRoots' result for the root wsRoot ("" for none).
func rootsOf(ctx context.Context, wsRoot string) (badgeRoot, _ string, cfg *workspace.Config) {
	if wsRoot == "" {
		return "", "", nil
	}
	cfg, _ = workspace.Detect(wsRoot)
	badgeRoot, err := gitx.CommonRoot(ctx, wsRoot)
	if err != nil {
		badgeRoot = wsRoot
	}
	if cfg == nil && badgeRoot != "" {
		cfg, _ = workspace.Detect(badgeRoot)
	}
	return badgeRoot, wsRoot, cfg
}

// decodeStdin reads all of r and unmarshals the JSON into v.
func decodeStdin(r io.Reader, v any) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
