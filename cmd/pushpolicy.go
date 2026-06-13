package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/pushpolicy"
	"github.com/mnemcik/consigliere/internal/workspace"
)

func init() {
	pushPolicyCmd.AddCommand(pushPolicyLookupCmd)
	pushPolicyCmd.AddCommand(pushPolicyGateCmd)
	rootCmd.AddCommand(pushPolicyCmd)
}

var pushPolicyCmd = &cobra.Command{
	Use:   "push-policy",
	Short: "Resolve and enforce external-repo push policies",
	Long: `Look up and enforce the push policy declared for an external repository in
the workspace's area "External Repos" tables (direct vs PR-only).`,
}

var pushPolicyLookupCmd = &cobra.Command{
	Use:   "lookup <owner/repo>",
	Short: "Print the declared push policy for a repo (direct|pr|unknown)",
	Args:  cobra.ExactArgs(1),
	RunE:  runPushPolicyLookup,
}

func runPushPolicyLookup(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	root, err := workspaceRoot(cmd)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), pushpolicy.Lookup(filepath.Join(root, "areas"), args[0]))
	return nil
}

// pushGateInput is the PreToolUse hook payload the gate consumes.
type pushGateInput struct {
	ToolName  string `json:"tool_name"`
	CWD       string `json:"cwd"`
	ToolInput struct {
		Command string `json:"command"`
		CWD     string `json:"cwd"`
	} `json:"tool_input"`
}

var pushPolicyGateCmd = &cobra.Command{
	Use:    "gate",
	Short:  "PreToolUse hook: allow/deny a git push by the repo's declared policy",
	Args:   cobra.NoArgs,
	RunE:   runPushPolicyGate,
	Hidden: true,
}

func runPushPolicyGate(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true

	var in pushGateInput
	if err := decodeStdin(cmd.InOrStdin(), &in); err != nil {
		return nil // never fail the hook
	}

	// The policy tables live in the session's workspace (the top-level hook cwd),
	// not the push target's repo.
	root, _, _ := workspace.FindRoot(in.CWD)
	if root == "" {
		return nil
	}

	decision, emit := pushpolicy.Gate(cmd.Context(), filepath.Join(root, "areas"), pushpolicy.GateInput{
		ToolName:     in.ToolName,
		Command:      in.ToolInput.Command,
		ToolInputCWD: in.ToolInput.CWD,
		SessionCWD:   in.CWD,
	})
	if emit {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), decision)
	}
	return nil
}
