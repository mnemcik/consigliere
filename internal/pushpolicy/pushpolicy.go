// Package pushpolicy implements the external-repo push-policy lookup and the
// PreToolUse gate promoted from lookup-push-policy.sh and
// external-repo-push-policy.sh. The lookup reads a repo's declared policy from
// the workspace's area files; the gate parses a `git push` command and emits a
// Claude Code permissionDecision. Splitting the pure lookup from the hook body
// is DEC-002 of the consigliere-cg-subcommands project.
package pushpolicy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mnemcik/consigliere/internal/gitx"
)

// Policy values declared in area "External Repos" tables.
const (
	Direct  = "direct"
	PR      = "pr"
	Unknown = "unknown"
)

// Lookup returns the push policy declared for owner/repo across the area files
// in areasDir: "direct", "pr", or "unknown" (when the repo isn't declared, or
// areas disagree — fail closed). Ports lookup-push-policy.sh.
func Lookup(areasDir, slug string) string {
	if slug == "" {
		return Unknown
	}
	files, err := filepath.Glob(filepath.Join(areasDir, "*.md"))
	if err != nil {
		return Unknown
	}
	found := map[string]bool{}
	for _, f := range files {
		data, rerr := os.ReadFile(f) //nolint:gosec // area files under the workspace
		if rerr != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, slug) {
				continue
			}
			for _, cell := range strings.Split(line, "|") {
				switch strings.TrimSpace(cell) {
				case Direct:
					found[Direct] = true
				case PR:
					found[PR] = true
				}
			}
		}
	}
	if len(found) == 1 {
		for k := range found {
			return k
		}
	}
	return Unknown // not declared, or conflicting declarations
}

var (
	gitPushRe = regexp.MustCompile(`(^|[\s;&|()])git\s+push(\s|$)`)
	cdRe      = regexp.MustCompile(`cd\s+([^\s&|;()]+)`)
	segSplit  = regexp.MustCompile(`\s*(\|\||&&|;|\|)\s*`)
	broadRe   = regexp.MustCompile(`(^|\s)(--?all|--?mirror)(\s|=|$)`)
	// flags that consume the following token as their value
	valueFlags = map[string]bool{
		"--repo": true, "--receive-pack": true, "--exec": true,
		"--push-option": true, "-o": true, "--signed": true,
		"--force-with-lease": true, "--force-if-includes": true,
	}
)

// GateInput is the PreToolUse hook payload the gate consumes.
type GateInput struct {
	ToolName     string
	Command      string
	ToolInputCWD string // tool_input.cwd
	SessionCWD   string // top-level hook cwd (used as a fallback target dir)
}

// Gate inspects a Bash `git push` command and returns a Claude Code
// permissionDecision JSON when the target repo has a declared policy, or
// emit=false to stay out of the way (non-push commands, undeclared repos).
// areasDir is the workspace's areas directory (where policies live); the push
// target repo is resolved from the command's cwd. Ports
// external-repo-push-policy.sh (the workspace-specific personal-workspace skip
// is dropped — an undeclared repo already returns Unknown → no-op).
func Gate(ctx context.Context, areasDir string, in GateInput) (string, bool) {
	if in.ToolName != "Bash" || !gitPushRe.MatchString(in.Command) {
		return "", false
	}
	targetCwd := resolveTargetCwd(in)
	remote, err := gitx.RemoteURL(ctx, targetCwd, "origin")
	if err != nil || remote == "" {
		return "", false
	}
	slug := slugFromRemote(remote)

	switch Lookup(areasDir, slug) {
	case Direct:
		return permission("allow", "area declares direct-push policy for "+slug), true
	case PR:
		return prGate(ctx, targetCwd, in.Command, slug), true
	default:
		return "", false
	}
}

func resolveTargetCwd(in GateInput) string {
	if m := cdRe.FindStringSubmatch(in.Command); m != nil {
		p := m[1]
		if strings.HasPrefix(p, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				p = filepath.Join(home, p[2:])
			}
		}
		return p
	}
	if in.ToolInputCWD != "" {
		return in.ToolInputCWD
	}
	if in.SessionCWD != "" {
		return in.SessionCWD
	}
	return "."
}

// slugFromRemote reduces an origin URL to "owner/repo" (sans ".git").
func slugFromRemote(remote string) string {
	s := strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	// Take the last two path/colon-separated components.
	s = strings.TrimRight(s, "/")
	sepIdx := strings.LastIndexAny(s, ":/")
	if sepIdx < 0 {
		return s
	}
	repo := s[sepIdx+1:]
	rest := s[:sepIdx]
	ownerIdx := strings.LastIndexAny(rest, ":/")
	owner := rest
	if ownerIdx >= 0 {
		owner = rest[ownerIdx+1:]
	}
	return owner + "/" + repo
}

// prGate builds the decision for a PR-only repo: deny broad pushes and direct
// pushes to the default branch; allow feature-branch pushes.
func prGate(ctx context.Context, targetCwd, command, slug string) string {
	defaultBranch := gitx.DefaultBranch(ctx, targetCwd)
	args := pushArgs(command)

	if broadRe.MatchString(" " + args + " ") {
		return permission("deny", "area declares PR-only policy for "+slug+
			" — broad pushes (--all/--mirror) would land changes on "+defaultBranch+", which requires a PR")
	}

	target := destRef(args)
	if target == "" {
		// No explicit refspec → push uses current HEAD.
		if b, err := gitx.SymbolicRefShort(ctx, targetCwd); err == nil {
			target = b
		}
	}
	target = strings.TrimPrefix(target, "refs/heads/")

	if target == defaultBranch {
		return permission("deny", "area declares PR-only policy for "+slug+
			" — direct push to "+defaultBranch+" is blocked; push a session branch and open a PR instead")
	}
	return permission("allow", "area declares PR-only policy for "+slug+
		"; pushing feature branch "+target+" is allowed (PR enforcement happens at merge time on the host)")
}

// pushArgs returns the arguments following "git push" in the command segment
// that contains it (tolerating leading `cd ... &&` and trailing chained cmds).
func pushArgs(command string) string {
	for _, seg := range segSplit.Split(command, -1) {
		loc := gitPushRe.FindStringIndex(seg)
		if loc == nil {
			continue
		}
		rest := seg[loc[1]:]
		return strings.TrimSpace(rest)
	}
	return ""
}

// destRef walks the push arguments and returns the destination ref of the first
// refspec (the part after ":" if present), skipping flags and the remote name.
func destRef(args string) string {
	var target string
	skipNext := false
	remoteSeen := false
	for _, tok := range strings.Fields(args) {
		if skipNext {
			skipNext = false
			continue
		}
		switch {
		case tok == "--":
			continue
		case valueFlags[tok]:
			skipNext = true
			continue
		case strings.HasPrefix(tok, "-"):
			continue
		}
		if !remoteSeen {
			remoteSeen = true // first non-flag token is the remote name
			continue
		}
		if i := strings.LastIndex(tok, ":"); i >= 0 {
			target = tok[i+1:]
		} else {
			target = tok
		}
	}
	return target
}

func permission(decision, reason string) string {
	b, _ := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       decision,
			"permissionDecisionReason": reason,
		},
	})
	return string(b)
}
