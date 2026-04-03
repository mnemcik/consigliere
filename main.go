package main

import (
	"os"

	"github.com/mnemcik/consigliere/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
