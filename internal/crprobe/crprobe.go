// Package crprobe is a throwaway file for a CodeRabbit behaviour test (PR not to be merged).
package crprobe

import (
	"os"
	"strings"
)

// FirstWord returns the first word of s.
func FirstWord(s string) string {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// ReadConfig returns the contents of the file at path.
func ReadConfig(path string) []byte {
	b, _ := os.ReadFile(path)
	return b
}
