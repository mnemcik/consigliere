// Package gitbash finds Git Bash, the bash that Claude Code runs cg's hooks and
// status line with on Windows. Without Git for Windows, Claude Code runs them
// in PowerShell instead, where cg's bash scripts don't run
// (https://code.claude.com/docs/en/setup#install-on-native-windows).
package gitbash

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// EnvVar is the variable Claude Code reads for the path of Git Bash when it
// can't find it itself.
const EnvVar = "CLAUDE_CODE_GIT_BASH_PATH"

// Find returns the path of Git Bash and whether one was found. Off Windows it
// returns bash from PATH. On Windows it never takes bash from PATH, which can be
// WSL's bash.exe: that reads Windows paths differently and doesn't see the
// Windows environment.
func Find() (string, bool) {
	return find(runtime.GOOS, os.Getenv, exec.LookPath, fileExists)
}

func find(goos string, getenv func(string) string, lookPath func(string) (string, error), exists func(string) bool) (string, bool) {
	if goos != "windows" {
		p, err := lookPath("bash")
		return p, err == nil
	}
	if p := getenv(EnvVar); p != "" && exists(p) {
		return p, true
	}
	for _, p := range candidates(getenv, lookPath) {
		if exists(p) {
			return p, true
		}
	}
	return "", false
}

// candidates lists where Git for Windows keeps bash.exe, in the order to try:
// next to the git on PATH first, then the default install locations.
func candidates(getenv func(string) string, lookPath func(string) (string, error)) []string {
	var roots []string
	// git.exe is in Git\cmd, Git\bin or Git\mingw64\bin; bash.exe is in Git\bin.
	if git, err := lookPath("git"); err == nil {
		dir := filepath.Dir(git)
		roots = append(roots, filepath.Dir(dir), filepath.Dir(filepath.Dir(dir)))
	}
	for _, v := range []string{"ProgramFiles", "ProgramW6432"} {
		if d := getenv(v); d != "" {
			roots = append(roots, filepath.Join(d, "Git"))
		}
	}
	if d := getenv("LOCALAPPDATA"); d != "" {
		roots = append(roots, filepath.Join(d, "Programs", "Git"))
	}
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		out = append(out, filepath.Join(r, "bin", "bash.exe"))
	}
	return out
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
