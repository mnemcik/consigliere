package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/session"
	"github.com/mnemcik/consigliere/internal/workspace"
)

func init() {
	sessionCmd.AddCommand(sessionMarkDirtyCmd)
	sessionCmd.AddCommand(sessionPullLatestCmd)
	rootCmd.AddCommand(sessionCmd)
}

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Claude Code hook bodies (gate, dirty-flag, pull-latest, statusline)",
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
	root, _, err := workspace.FindRoot(cwd)
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
