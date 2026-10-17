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
// session.gateTemplate at a file they own. Tokens below are substituted, and
// the set-context block is added (see withSetContext).
const defaultGate = `⚠️ SESSION-START — before making changes, confirm:
1. The area(s) this work belongs to.
2. The project tracking it (or create one).
3. You are in the right working tree for your workflow.

{{worktree_warning}}`

// setContextBlock tells the agent how to record the session's area and project,
// which is what creates the badge file. cg adds it to every gate rather than
// leaving it to the template: the template is user-owned and never updated, so
// a workspace whose copy predates the instruction would otherwise never write a
// badge (#195).
const setContextBlock = `Session ID: {{session_id}}
Badge state file for this session: {{badge_file}}
Once the area and project are confirmed, record them for the status line:
  cg session set-context --session-id {{session_id}} --area <area-slug> --project <project-slug>`

// setContextPlaceholder places the set-context block in a gate template.
const setContextPlaceholder = "{{set_context}}"

// setContextCommand marks a template that already carries its own set-context
// instruction, such as a copy of the default template from before cg added it.
const setContextCommand = "cg session set-context"

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

	body = withSetContext(body)

	repl := strings.NewReplacer(
		"{{session_id}}", in.SessionID,
		"{{badge_file}}", ContextFile(root, in.SessionID),
		"{{worktree_warning}}", warning,
	)
	body = strings.TrimRight(repl.Replace(body), "\n")

	return "<user-prompt-submit-hook>\n" + body + "\n</user-prompt-submit-hook>", true
}

// withSetContext puts the set-context block in body: at {{set_context}} when
// the template has it, otherwise before {{worktree_warning}}, otherwise at the
// end. A template that already names the command is left as it is, and lines
// of the block the template already has are not repeated (the original default
// template had the session ID and badge file lines but not the command).
func withSetContext(body string) string {
	if strings.Contains(body, setContextPlaceholder) {
		return strings.ReplaceAll(body, setContextPlaceholder, setContextBlock)
	}
	if strings.Contains(body, setContextCommand) {
		return body
	}
	have := map[string]bool{}
	for _, l := range strings.Split(body, "\n") {
		have[strings.TrimSpace(l)] = true
	}
	var add []string
	for _, l := range strings.Split(setContextBlock, "\n") {
		if !have[strings.TrimSpace(l)] {
			add = append(add, l)
		}
	}
	block := strings.Join(add, "\n")
	if strings.Contains(body, "{{worktree_warning}}") {
		return strings.Replace(body, "{{worktree_warning}}", block+"\n{{worktree_warning}}", 1)
	}
	if strings.TrimSpace(body) == "" {
		return block
	}
	return strings.TrimRight(body, "\n") + "\n\n" + block
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

// pruneStaleContexts removes badge files older than pruneDays, and lock files
// whose badge is gone and that nobody has locked for pruneDays (best-effort).
// A lock file is deleted only while pruning holds its lock, so no writer holds
// it at that moment, and a writer that opened it earlier notices the deletion
// after locking and retries on the new file (see lockOpened). A lock file in
// use is skipped. On Windows, deleting a file that is open may fail; the file
// is then left for a later prune.
func pruneStaleContexts(root string, pruneDays int) {
	if pruneDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(pruneDays) * 24 * time.Hour)
	old := func(p string) bool {
		fi, err := os.Stat(p)
		return err == nil && fi.ModTime().Before(cutoff)
	}
	badges, err := filepath.Glob(filepath.Join(ContextDir(root), "*.json"))
	if err != nil {
		return
	}
	for _, b := range badges {
		if old(b) {
			_ = os.Remove(b)
		}
	}
	locks, err := filepath.Glob(filepath.Join(ContextDir(root), "*.lock"))
	if err != nil {
		return
	}
	for _, l := range locks {
		badge := strings.TrimSuffix(l, ".lock") + ".json"
		if !old(l) || fileExists(badge) {
			continue
		}
		f, _, err := tryLockFile(l)
		if err != nil || f == nil {
			continue // in use, or cannot lock: leave it
		}
		removeLocked(f, l, !fileExists(badge)) // a writer may have claimed the session meanwhile
	}
}

// fileExists reports whether p exists.
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
