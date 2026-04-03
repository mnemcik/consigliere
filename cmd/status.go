package cmd

import (
	"fmt"
	"os"
	"path/filepath"

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
		projects, _ := parseProjectIndex(filepath.Join(dir, indexPath))
		if len(projects) > 0 {
			active := 0
			for _, p := range projects {
				if p.Status != "Done" && p.Status != "done" {
					active++
				}
			}
			fmt.Printf("Projects: %d total, %d active\n", len(projects), active)
		} else {
			fmt.Println("Projects: 0")
		}
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
