package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mnemcik/consigliere/internal/workspace"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(matchCmd)
}

var matchCmd = &cobra.Command{
	Use:   "match [prompt]",
	Short: "Match a prompt to an existing project",
	Long:  "Match a user prompt against the project index to find the best matching project. Returns structured output for programmatic use.",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runMatch,
}

type project struct {
	Name   string
	Status string
	Areas  string
	Folder string
}

func runMatch(cmd *cobra.Command, args []string) error {
	prompt := strings.Join(args, " ")
	promptLower := strings.ToLower(prompt)

	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	// Guard: verify workspace
	cfg, err := workspace.Detect(dir)
	if err != nil {
		fmt.Println("NO_MATCH: Error reading .cg.json")
		return nil
	}
	if cfg == nil {
		fmt.Println("NO_MATCH: Not a Consigliere workspace. Run `cg init` to set one up.")
		return nil
	}

	// Read project index
	indexPath := "projects/TODO.md"
	if p, ok := cfg.Indexes["projects"]; ok {
		indexPath = p
	}

	projects, err := parseProjectIndex(filepath.Join(dir, indexPath))
	if err != nil || len(projects) == 0 {
		fmt.Println("NO_MATCH: Project index not found or empty.")
		return nil
	}

	// Score each project
	type scored struct {
		project project
		score   int
	}
	var candidates []scored

	promptWords := tokenize(promptLower)

	for _, p := range projects {
		score := 0
		nameLower := strings.ToLower(p.Name)
		folderLower := strings.ToLower(p.Folder)
		areasLower := strings.ToLower(p.Areas)

		// Exact name contained in prompt (highest signal)
		if strings.Contains(promptLower, nameLower) {
			score += 100
		}

		// Folder slug match
		if strings.Contains(promptLower, folderLower) {
			score += 80
		}

		// Area slug match
		areaSlugs := extractAreaSlugs(areasLower)
		for _, slug := range areaSlugs {
			if strings.Contains(promptLower, slug) {
				score += 40
			}
		}

		// Keyword overlap (words from project name found in prompt)
		nameWords := tokenize(nameLower)
		matchedWords := 0
		for _, w := range nameWords {
			if len(w) < 3 {
				continue // skip short words (a, the, for, etc.)
			}
			for _, pw := range promptWords {
				if w == pw {
					matchedWords++
					break
				}
			}
		}
		if len(nameWords) > 0 && matchedWords > 0 {
			// Score based on proportion of name words matched
			score += matchedWords * 20
		}

		if score > 0 {
			candidates = append(candidates, scored{project: p, score: score})
		}
	}

	if len(candidates) == 0 {
		truncated := prompt
		if len(truncated) > 50 {
			truncated = truncated[:50] + "..."
		}
		fmt.Printf("NO_MATCH: No project matches the prompt \"%s\"\n", truncated)
		return nil
	}

	// Sort by score descending (simple bubble sort, small list)
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// Clear winner: top score is significantly higher than second
	if len(candidates) == 1 || candidates[0].score > candidates[1].score*2 {
		p := candidates[0].project
		fmt.Printf("MATCH: %s\n", p.Name)
		fmt.Printf("SLUG: %s\n", p.Folder)
		fmt.Printf("PATH: projects/%s/\n", p.Folder)
		fmt.Printf("STATUS: %s\n", p.Status)
		return nil
	}

	// Ambiguous: show top candidates
	limit := 3
	if len(candidates) < limit {
		limit = len(candidates)
	}
	fmt.Printf("AMBIGUOUS: %d candidates\n", limit)
	for i := 0; i < limit; i++ {
		p := candidates[i].project
		fmt.Printf("CANDIDATE: %s | SLUG: %s | PATH: projects/%s/ | STATUS: %s\n",
			p.Name, p.Folder, p.Folder, p.Status)
	}

	return nil
}

// parseProjectIndex reads a markdown table from the project index file
func parseProjectIndex(path string) ([]project, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var projects []project
	scanner := bufio.NewScanner(f)

	// Table row pattern: | # | Name | Status | Areas | Folder |
	// We look for rows with at least 5 pipe-separated fields after the header
	headerFound := false
	separatorFound := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "|") {
			continue
		}

		cells := splitTableRow(line)
		if len(cells) < 5 {
			continue
		}

		// Detect header row
		if !headerFound {
			headerFound = true
			continue
		}

		// Detect separator row (|---|---|...)
		if !separatorFound {
			if strings.Contains(cells[0], "---") {
				separatorFound = true
				continue
			}
		}

		if !separatorFound {
			continue
		}

		// Parse data row
		// Expected: | # | Project | Status | Areas | Folder |
		name := strings.TrimSpace(cells[1])
		status := strings.TrimSpace(cells[2])
		areas := strings.TrimSpace(cells[3])
		folder := strings.TrimSpace(cells[4])

		// Extract folder slug from markdown link if present: [name](path/)
		folder = extractFolderSlug(folder)

		projects = append(projects, project{
			Name:   name,
			Status: status,
			Areas:  areas,
			Folder: folder,
		})
	}

	return projects, scanner.Err()
}

func splitTableRow(line string) []string {
	// Remove leading/trailing pipes and split
	line = strings.Trim(line, "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

var folderLinkRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

func extractFolderSlug(s string) string {
	// Match [name](path/) or [name](path)
	matches := folderLinkRe.FindStringSubmatch(s)
	if len(matches) >= 3 {
		slug := strings.TrimSuffix(matches[2], "/")
		// Remove any path prefix
		if i := strings.LastIndex(slug, "/"); i >= 0 {
			slug = slug[i+1:]
		}
		return slug
	}
	return strings.TrimSuffix(strings.TrimSpace(s), "/")
}

func extractAreaSlugs(areas string) []string {
	// Areas are formatted as: `slug1`, `slug2`
	var slugs []string
	for _, part := range strings.Split(areas, ",") {
		slug := strings.TrimSpace(part)
		slug = strings.Trim(slug, "`")
		if slug != "" {
			slugs = append(slugs, slug)
		}
	}
	return slugs
}

func tokenize(s string) []string {
	// Split on non-alphanumeric characters
	re := regexp.MustCompile(`[a-z0-9]+`)
	return re.FindAllString(s, -1)
}
