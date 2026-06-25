package session

import (
	"bufio"
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// StatuslineInput is the statusLine hook payload the renderer consumes.
type StatuslineInput struct {
	CWD       string
	SessionID string
	Raw       []byte // the original stdin JSON, forwarded to the upstream statusline
}

// Statusline renders the status line: the upstream output (if an upstream is
// configured) followed by a colored "[area/project]" badge for the active
// session, with a trailing red ● when the session is dirty. It returns the base
// output unchanged when there is no workspace or no badge context. Ports
// statusline.sh (the workspace-specific caveman badge is intentionally dropped —
// a workspace can re-add it via its own wrapper). badgeFormat uses {area} and
// {project} tokens (default "[{area}/{project}]").
func Statusline(ctx context.Context, root string, in StatuslineInput, upstream, badgeFormat string) string {
	base := runUpstream(ctx, upstream, in.Raw)

	if root == "" {
		return base
	}
	c, err := ReadContext(root, in.SessionID)
	if err != nil || c == nil || (c.Area == "" && c.Project == "") {
		return base
	}

	badge := renderBadge(root, c, badgeFormat)
	if base == "" {
		return badge
	}
	return base + "\n" + badge
}

// upstreamTimeout bounds how long the configured upstream may run before the
// status line falls back to the badge alone. Generous enough for a git-driven
// status line, short enough that a hung command never blocks the CLI. A var (not
// a const) so tests can shorten it.
var upstreamTimeout = 2 * time.Second

// runUpstream executes the configured upstream as a shell command, forwarding
// the hook's stdin JSON, and returns its stdout. upstream is a command string
// (run via "bash -c"), so it can reproduce whatever the user's prior statusLine
// was — "bash ~/.claude/statusline.sh", "node line.js", or a bare executable
// path (which still runs, e.g. "bash -c /abs/path.sh"). Any error — including the
// timeout below — yields "" so a broken or slow upstream never breaks the status
// line.
func runUpstream(ctx context.Context, upstream string, input []byte) string {
	if upstream == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	// #nosec G204 -- upstream is trusted workspace config (.cg.json), the same
	// trust level as the .claude/settings.json statusLine command and hook
	// wrappers Claude Code already executes. The shell ("bash -c") is required so
	// the value can be a full command (e.g. "bash ~/.claude/statusline.sh"), not
	// only a bare path. A malicious workspace could run code here exactly as it
	// could via settings.json, so the real trust boundary is opening an untrusted
	// workspace, not this call.
	cmd := exec.CommandContext(ctx, "bash", "-c", upstream)
	cmd.Stdin = strings.NewReader(string(input))
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

func renderBadge(root string, c *Context, badgeFormat string) string {
	if badgeFormat == "" {
		badgeFormat = "[{area}/{project}]"
	}
	label := strings.NewReplacer("{area}", c.Area, "{project}", c.Project).Replace(badgeFormat)
	// Collapse an empty side of "area/project" when only one is set.
	label = strings.ReplaceAll(label, "[/", "[")
	label = strings.ReplaceAll(label, "/]", "]")

	code := resolveColor(root, c)
	badge := fmt.Sprintf("\033[1;%dm%s\033[0m", code, label)
	if c.Dirty {
		badge += " \033[1;31m●\033[0m"
	}
	return badge
}

// colorCodes maps the named colors used in project/area Meta blocks to ANSI SGR
// foreground codes.
var colorCodes = map[string]int{
	"red": 31, "green": 32, "yellow": 33, "blue": 34,
	"magenta": 35, "purple": 35, "cyan": 36, "white": 97,
	"bright-red": 91, "bred": 91, "bright-green": 92, "bgreen": 92,
	"bright-yellow": 93, "byellow": 93, "bright-blue": 94, "bblue": 94,
	"bright-magenta": 95, "bmagenta": 95, "bright-purple": 95,
	"bright-cyan": 96, "bcyan": 96,
}

// resolveColor picks the badge color: project README **Color:**, then area
// **Color:**, then a deterministic hash of the slug.
func resolveColor(root string, c *Context) int {
	if c.Project != "" {
		if code, ok := colorCodes[readColorField(filepath.Join(root, "projects", c.Project, "README.md"))]; ok {
			return code
		}
	}
	if c.Area != "" {
		if code, ok := colorCodes[readColorField(filepath.Join(root, "areas", c.Area+".md"))]; ok {
			return code
		}
	}
	slug := c.Project
	if slug == "" {
		slug = c.Area
	}
	return colorFromSlug(slug)
}

var colorFieldRe = regexp.MustCompile(`^(?:- )?\*\*Color:\*\*\s*(.+?)\s*$`)

// readColorField reads "**Color:** <name>" from the first 40 lines of a
// markdown file, returning "" when absent or a {placeholder}.
func readColorField(file string) string {
	f, err := os.Open(file) //nolint:gosec // file path derived from workspace layout
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for line := 0; line < 40 && sc.Scan(); line++ {
		m := colorFieldRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		val := strings.Trim(m[1], "`")
		if strings.HasPrefix(val, "{") && strings.HasSuffix(val, "}") {
			return "" // template placeholder counts as unset
		}
		return val
	}
	return ""
}

// brightPalette is the deterministic fallback color set (ANSI bright + normal).
var brightPalette = []int{91, 92, 93, 94, 95, 96, 31, 32, 33, 34, 35, 36}

func colorFromSlug(slug string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(slug))
	//nolint:gosec // len(brightPalette) is a fixed small constant; no overflow
	return brightPalette[h.Sum32()%uint32(len(brightPalette))]
}
