package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mnemcik/consigliere/internal/workspace"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(statusCmd)
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show workspace overview",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg, err := workspace.Detect(dir)
	if err != nil {
		return fmt.Errorf("error reading %s: %w", workspace.ConfigFile, err)
	}
	if cfg == nil {
		fmt.Println("Not a Consigliere workspace.")
		fmt.Println("Run `cg init` to set one up.")
		return nil
	}

	fmt.Printf("Consigliere workspace (v%s)\n\n", cfg.Version)

	// Count projects
	if indexPath, ok := cfg.Indexes["projects"]; ok {
		fmt.Println(projectsSummary(filepath.Join(dir, indexPath)))
	}

	// Count areas
	areaCount := countDirFiles(filepath.Join(dir, "areas"), ".md", "INDEX.md")
	fmt.Printf("Areas:    %d\n", areaCount)

	// Count ideas
	ideaCount := countDirFiles(filepath.Join(dir, "ideas"), ".md", "BACKLOG.md")
	fmt.Printf("Ideas:    %d\n", ideaCount)

	// Count notes
	noteCount := countDirFiles(filepath.Join(dir, "notes"), ".md", "INDEX.md")
	fmt.Printf("Notes:    %d\n", noteCount)

	return nil
}

func countDirFiles(dir, ext, exclude string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ext && e.Name() != exclude {
			count++
		}
	}
	return count
}

// standingSection is the projects/TODO.md heading that holds standing
// projects, and standingHeading the README heading that marks one.
const (
	standingSection = "Standing Commitments"
	standingHeading = "## Standard to Maintain"
)

// projectsSummary renders the `Projects:` line of `cg status` from the project
// index at indexPath. A standing project (a recurring duty with no finish
// line) is counted apart from the active ones, so it does not inflate them.
func projectsSummary(indexPath string) string {
	projects, _ := parseProjectIndex(indexPath)
	if len(projects) == 0 {
		return "Projects: 0"
	}
	active, standing := 0, 0
	for i := range projects {
		p := &projects[i]
		if strings.EqualFold(p.Status, "done") {
			continue
		}
		if isStandingProject(p, filepath.Dir(indexPath)) {
			standing++
		} else {
			active++
		}
	}
	line := fmt.Sprintf("Projects: %d total, %d active", len(projects), active)
	if standing > 0 {
		line += fmt.Sprintf(", %d standing", standing)
	}
	return line
}

// isStandingProject reports whether p is a standing project: its row sits in
// the `## Standing Commitments` table, or its README carries the
// `## Standard to Maintain` heading. Either signal is enough, so a workspace
// whose index has no standing table still counts its standing projects.
func isStandingProject(p *project, projectsDir string) bool {
	if strings.EqualFold(p.Section, standingSection) {
		return true
	}
	if p.Folder == "" {
		return false
	}
	return hasStandingHeading(filepath.Join(projectsDir, p.Folder, "README.md"))
}

// hasStandingHeading reports whether the markdown file at path has a
// `## Standard to Maintain` heading outside a fenced code block. A missing or
// unreadable file has none. Line endings do not matter: a CRLF file matches.
func hasStandingHeading(path string) bool {
	f, err := os.Open(path) //nolint:gosec // read-only heading check; the folder slug is a single path segment (extractFolderSlug)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	fence := "" // the opening fence run while inside a fenced block
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		raw := strings.TrimRight(scanner.Text(), " \t\r")
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		line := strings.TrimSpace(raw)
		if fence != "" {
			// A fence closes only on a run of the same character at least
			// as long as the one that opened it.
			if indent < 4 && strings.HasPrefix(line, fence) && strings.Trim(line, fence[:1]) == "" {
				fence = ""
			}
			continue
		}
		if indent >= 4 {
			continue // an indented code block, not a heading
		}
		if run := fenceRun(line); run != "" {
			fence = run
			continue
		}
		if strings.EqualFold(line, standingHeading) {
			return true
		}
	}
	return false
}

// fenceRun returns the backtick or tilde run that opens a fenced code block
// on line, or "" when line opens none.
func fenceRun(line string) string {
	for _, c := range []string{"`", "~"} {
		n := len(line) - len(strings.TrimLeft(line, c))
		if n >= 3 {
			return strings.Repeat(c, n)
		}
	}
	return ""
}
