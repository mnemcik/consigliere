package main

import (
	"errors"
	"os"

	"github.com/mnemcik/consigliere/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		// Preserve the exit-code contract of the promoted shell helpers: a
		// command may return an error carrying a specific process exit code.
		var coded interface{ ExitCode() int }
		if errors.As(err, &coded) {
			os.Exit(coded.ExitCode())
		}
		os.Exit(1)
	}
}
