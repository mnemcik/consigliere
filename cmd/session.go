package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/session"
	"github.com/mnemcik/consigliere/internal/workspace"
)

func init() {
	sessionCmd.AddCommand(sessionMarkDirtyCmd)
	sessionCmd.AddCommand(sessionPullLatestCmd)
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
	// Badge files live at the main worktree root, matching start-gate and
	// set-context; fall back to the walk-up root outside a git repo.
	root, err := gitx.CommonRoot(cmd.Context(), cwd)
	if err != nil {
		root, _, err = workspace.FindRoot(cwd)
	}
	if err != nil || root == "" {
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
	landingBranch := workspace.DefaultLandingBranch
	if root, cerr := gitx.CommonRoot(cmd.Context(), cwd); cerr == nil {
		if cfg, derr := workspace.Detect(root); derr == nil {
			landingBranch = cfg.WorktreeSettings().LandingBranch
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
Existing fields such as the dirty flag are preserved.`,
	Example: `  cg session set-context --session-id 1b2c... --area platform --project api-gateway`,
	Args:    cobra.NoArgs,
	RunE:    runSessionSetContext,
}

func init() {
	f := sessionSetContextCmd.Flags()
	f.StringVar(&setContextSessionID, "session-id", "", "Claude Code session ID (shown in the session-start gate)")
	f.StringVar(&setContextArea, "area", "", "area slug the session works in")
	f.StringVar(&setContextProject, "project", "", "project slug the session works on")
	for _, name := range []string{"session-id", "area", "project"} {
		_ = sessionSetContextCmd.MarkFlagRequired(name)
	}
}

func runSessionSetContext(cmd *cobra.Command, _ []string) error {
	if !session.ValidSessionID(setContextSessionID) {
		return fmt.Errorf("invalid --session-id %q", setContextSessionID)
	}
	// Required-flag checks only test presence, so --area "" would slip through.
	setContextArea = strings.TrimSpace(setContextArea)
	setContextProject = strings.TrimSpace(setContextProject)
	if setContextArea == "" || setContextProject == "" {
		return fmt.Errorf("--area and --project must be non-empty")
	}
	cmd.SilenceUsage = true

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// Badge files live at the main worktree root, matching start-gate.
	root, err := gitx.CommonRoot(cmd.Context(), cwd)
	if err != nil {
		root, _, err = workspace.FindRoot(cwd)
	}
	if err != nil || root == "" {
		return fmt.Errorf("not inside a Consigliere workspace: %s", cwd)
	}

	if err := session.WriteContext(root, setContextSessionID, setContextArea, setContextProject); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Session badge set: [%s/%s] (%s)\n",
		setContextArea, setContextProject, session.ContextFile(root, setContextSessionID))
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

	// Badge files live at the main worktree root (where start-gate and
	// set-context put them), so a session running in a linked worktree still
	// finds its badge; fall back to the walk-up root outside a git repo.
	root, err := gitx.CommonRoot(cmd.Context(), cwd)
	if err != nil {
		root, _, _ = workspace.FindRoot(cwd)
	}
	cfg, _ := workspace.Detect(root)
	s := cfg.SessionSettings()

	out := session.Statusline(cmd.Context(), root, session.StatuslineInput{
		CWD:       cwd,
		SessionID: in.SessionID,
		Raw:       raw,
	}, s.StatuslineUpstream, s.BadgeFormat)
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
	return nil
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
