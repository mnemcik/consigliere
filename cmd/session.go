package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

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
	root, wsRoot, _ := sessionRoots(cmd.Context(), cwd)
	if root == "" {
		return nil
	}
	if _, err := session.EndSession(root, wsRoot, in.SessionID); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "cg session end: %v\n", err)
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
	root, _, _ := sessionRoots(cmd.Context(), cwd)
	if root == "" {
		return nil
	}

	// Ignore writes to the badge file itself — framework bookkeeping isn't work.
	filePath := in.ToolInput.FilePath
	if filePath == "" {
		filePath = in.ToolInput.NotebookPath
	}
	if session.IsContextPath(root, filePath) {
		return nil
	}

	if err := session.MarkDirty(root, in.SessionID); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "cg session mark-dirty: %v\n", err)
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
	// A fresh clone has the contributed git-hook scripts (versioned) but not
	// the dispatcher in .git/hooks (unversioned); this runs at every session
	// start, so reconcile here and say so only when something is wrong.
	if msgs := ensureGitHooks(cmd.Context(), cwd); len(msgs) > 0 {
		emitSystemMessage(cmd.OutOrStdout(), "cg: "+strings.Join(msgs, "; "))
	}
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
		emitSystemMessage(cmd.OutOrStdout(), res.SystemMessage)
	}
	return nil
}

var (
	setContextSessionID string
	setContextArea      string
	setContextProject   string
)

var sessionSetContextCmd = &cobra.Command{
	Use:   "set-context",
	Short: "Record the session's area and project for the status-line badge",
	Long: `Writes the area and project a session is working on to its badge state
file (.claude/session-context/<session-id>.json under the main worktree root),
creating it when absent. The status line renders the badge from this file and
cg active lists sessions from it. Run it once the session-start gate's area and
project are confirmed, and again whenever the session switches either one.
Existing fields such as the dirty flag are preserved. It refuses to run
outside a Consigliere workspace. The session ID must match [A-Za-z0-9_-]+.
The file format is described in docs/cg-subcommands.md, "Session badge file".

When the project is paused (projects/<project>/resume.md exists), this session
is resuming it, so other sessions' claims on the project are released.

` + sessionIDHelp,
	Example: `  cg session set-context --session-id 1b2c... --area platform --project api-gateway`,
	Args:    cobra.NoArgs,
	RunE:    runSessionSetContext,
}

func init() {
	f := sessionSetContextCmd.Flags()
	f.StringVar(&setContextSessionID, "session-id", "", "session ID (shown in the session-start gate; see below for defaults)")
	f.StringVar(&setContextArea, "area", "", "area slug the session works in")
	f.StringVar(&setContextProject, "project", "", "project slug the session works on")
	for _, name := range []string{"area", "project"} {
		_ = sessionSetContextCmd.MarkFlagRequired(name)
	}
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
	root, wsRoot, _ := sessionRoots(cmd.Context(), cwd)
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
	root, wsRoot, _ := sessionRoots(cmd.Context(), cwd)
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
	// Required-flag checks only test presence, so --area "" would slip through.
	setContextArea = strings.TrimSpace(setContextArea)
	setContextProject = strings.TrimSpace(setContextProject)
	if setContextArea == "" || setContextProject == "" {
		return fmt.Errorf("--area and --project must be non-empty")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// Refuse outside a workspace: inside any other git repo the badge root
	// would still resolve, leaving a stray .claude/ the status line never reads.
	root, wsRoot, _ := sessionRoots(cmd.Context(), cwd)
	if wsRoot == "" || root == "" {
		return fmt.Errorf("not inside a Consigliere workspace: %s", cwd)
	}

	if err := session.WriteContext(root, id, session.Claim{Area: setContextArea, Project: setContextProject}); err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Session badge set: [%s/%s] (%s)\n",
		setContextArea, setContextProject, session.ContextFile(root, id))
	retired, err := session.HandOver(root, wsRoot, id, setContextProject)
	if err != nil {
		return err
	}
	for _, sid := range retired {
		_, _ = fmt.Fprintf(out, "Resuming paused project: released session %s\n", sid)
	}
	return nil
}

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

	// Badge files live at the main worktree root; fall back to the walk-up root.
	root, err := gitx.CommonRoot(cmd.Context(), cwd)
	if err != nil {
		root, _, _ = workspace.FindRoot(cwd)
	}
	cfg, _ := workspace.Detect(root)
	s := cfg.SessionSettings()

	text, emit := session.Gate(cmd.Context(), root, session.GateInput{
		Prompt:    in.Prompt,
		SessionID: in.SessionID,
		CWD:       cwd,
	}, s.GateTemplate, s.PruneDays)
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

	root, _, cfg := sessionRoots(cmd.Context(), cwd)
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
// cwd belongs to. badgeRoot is the main worktree root (where start-gate puts
// badge files), so a session in a linked worktree still finds its badge; it
// falls back to the walk-up workspace root outside a git repo. wsRoot is the
// walk-up workspace root ("" when cwd is not in a workspace). cfg is the
// workspace config, read from wsRoot and else from badgeRoot, so a workspace
// nested below the git top level keeps its settings.
func sessionRoots(ctx context.Context, cwd string) (badgeRoot, wsRoot string, cfg *workspace.Config) {
	wsRoot, cfg, _ = workspace.FindRoot(cwd)
	badgeRoot, err := gitx.CommonRoot(ctx, cwd)
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

// emitSystemMessage writes a Claude Code {"systemMessage": "..."} object.
func emitSystemMessage(w io.Writer, msg string) {
	out, err := json.Marshal(struct {
		SystemMessage string `json:"systemMessage"`
	}{msg})
	if err != nil {
		return
	}
	_, _ = fmt.Fprintln(w, string(out))
}
