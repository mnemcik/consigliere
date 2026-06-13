package session

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mnemcik/consigliere/internal/gitx"
)

// skipAllPattern matches prompts that suppress the gate entirely: slash
// commands and quick/throwaway requests that aren't tracked project work. It is
// framework-neutral; a workspace can add its own classification in its hook
// wrapper or gate template.
var skipAllPattern = regexp.MustCompile(`(?i)^(/|idea:|just a quick|no project|skip the project)`)

// defaultGate is the built-in, framework-neutral gate body used when no
// session.gateTemplate is configured. Workspaces with their own wording point
// session.gateTemplate at a file they own. Tokens below are substituted.
const defaultGate = `⚠️ SESSION-START — before making changes, confirm:
1. The area(s) this work belongs to.
2. The project tracking it (or create one).
3. You are in the right working tree for your workflow.

Session ID: {{session_id}}
Badge state file for this session: {{badge_file}}
{{worktree_warning}}`

// defaultWorktreeWarning is appended (as the {{worktree_warning}} token) when
// the session runs in the main worktree, where the git index is shared with
// other sessions rooted there.
const defaultWorktreeWarning = `⚠️ You are in the MAIN worktree — its git index is shared with every other
session rooted here. Create a session worktree (cg worktree create <slug>)
before writing files if your workflow uses them.`

// GateInput is the UserPromptSubmit hook payload fields the gate consumes.
type GateInput struct {
	Prompt    string
	SessionID string
	CWD       string
}

// Gate renders the session-start gate text for a prompt, or returns emit=false
// when the prompt should be let through silently (slash commands, quick
// questions). It also best-effort prunes stale per-session badge files. Ports
// session-start-gate.sh; the wording is config-driven (DEC-001): a workspace
// supplies its own via gateTemplatePath, else a framework-neutral default is
// used. root is the main worktree root (where the badge files live).
func Gate(ctx context.Context, root string, in GateInput, gateTemplatePath string, pruneDays int) (text string, emit bool) {
	pruneStaleContexts(root, pruneDays)

	if skipAllPattern.MatchString(strings.TrimSpace(in.Prompt)) {
		return "", false
	}

	body := defaultGate
	if gateTemplatePath != "" {
		path := gateTemplatePath
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if data, err := os.ReadFile(path); err == nil { //nolint:gosec // path derived from workspace config
			body = string(data)
		}
	}

	warning := ""
	if inMainWorktree(ctx, in.CWD, root) {
		warning = defaultWorktreeWarning
	}

	repl := strings.NewReplacer(
		"{{session_id}}", in.SessionID,
		"{{badge_file}}", ContextFile(root, in.SessionID),
		"{{worktree_warning}}", warning,
	)
	body = strings.TrimRight(repl.Replace(body), "\n")

	return "<user-prompt-submit-hook>\n" + body + "\n</user-prompt-submit-hook>", true
}

// inMainWorktree reports whether cwd's working tree is the main worktree (its
// toplevel equals the shared common root), where the index is shared.
func inMainWorktree(ctx context.Context, cwd, root string) bool {
	top := gitx.ShowToplevel(ctx, cwd)
	if top == "" {
		return false
	}
	rt, err := filepath.EvalSymlinks(top)
	if err != nil {
		rt = top
	}
	rr, err := filepath.EvalSymlinks(root)
	if err != nil {
		rr = root
	}
	return rt == rr
}

// pruneStaleContexts removes badge files older than pruneDays (best-effort).
func pruneStaleContexts(root string, pruneDays int) {
	if pruneDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(pruneDays) * 24 * time.Hour)
	matches, err := filepath.Glob(filepath.Join(ContextDir(root), "*.json"))
	if err != nil {
		return
	}
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.ModTime().Before(cutoff) {
			_ = os.Remove(m)
		}
	}
}
