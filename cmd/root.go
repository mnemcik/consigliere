package cmd

import (
	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags
var Version = "dev"

var rootCmd = &cobra.Command{
	Use:   "cg",
	Short: "Consigliere — personal workspace management",
	Long:  "Consigliere (cg) is a personal workspace management framework.\nIt provides structure, templates, and conventions for organizing projects, ideas, notes, areas, and insights.",
}

func Execute() error {
	return rootCmd.Execute()
}
