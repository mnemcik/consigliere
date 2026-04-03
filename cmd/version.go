package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the Consigliere version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("cg version %s\n", Version)
	},
}
