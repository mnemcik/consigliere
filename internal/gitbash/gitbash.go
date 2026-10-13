// Package gitbash finds Git Bash, the bash that Claude Code runs cg's hooks and
// status line with on Windows. Without Git for Windows, Claude Code runs them
// in PowerShell instead, where cg's bash scripts don't run
// (https://code.claude.com/docs/en/setup#install-on-native-windows).
package gitbash

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// EnvVar is the variable Claude Code reads for the path of Git Bash when it
// can't find it itself.
const EnvVar = "CLAUDE_CODE_GIT_BASH_PATH"

// Find returns the path of Git Bash and whether one was found. Off Windows it
// returns bash from PATH. On Windows it never takes bash from PATH, which can be
// WSL's bash.exe: that reads Windows paths differently and doesn't see the
// Windows environment. EnvVar is read from the environment, then from the "env"
// block of Claude Code's settings files, where Claude Code's docs say to set it:
// the workspace's (root, when not "") and the user's.
func Find(root string) (string, bool) {
	home, _ := os.UserHomeDir()
	return find(runtime.GOOS, os.Getenv, settingsFiles(root, home), exec.LookPath, fileExists)
}

// settingsFiles lists Claude Code's settings files in precedence order: the
// workspace's local and shared settings, then the user's.
func settingsFiles(root, home string) []string {
	var files []string
	if root != "" {
		files = append(files,
			filepath.Join(root, ".claude", "settings.local.json"),
			filepath.Join(root, ".claude", "settings.json"))
	}
	if home != "" {
		files = append(files, filepath.Join(home, ".claude", "settings.json"))
	}
	return files
}

func find(goos string, getenv func(string) string, settings []string, lookPath func(string) (string, error), exists func(string) bool) (string, bool) {
	if goos != "windows" {
		p, err := lookPath("bash")
		return p, err == nil
	}
	if p := configured(getenv, settings); p != "" && exists(p) {
		return p, true
	}
	for _, p := range candidates(getenv, lookPath) {
		if exists(p) {
			return p, true
		}
	}
	return "", false
}

// configured returns EnvVar from the environment, or else from the first
// settings file that sets it in its "env" block. Unreadable files are skipped.
func configured(getenv func(string) string, settings []string) string {
	if p := getenv(EnvVar); p != "" {
		return p
	}
	for _, f := range settings {
		data, err := os.ReadFile(f) // #nosec G304 -- Claude Code's own settings files
		if err != nil {
			continue
		}
		var s struct {
			Env map[string]string `json:"env"`
		}
		if json.Unmarshal(data, &s) == nil && s.Env[EnvVar] != "" {
			return s.Env[EnvVar]
		}
	}
	return ""
}

// candidates lists where Git for Windows keeps bash.exe, in the order to try:
// next to the git on PATH first, then the default install locations. A git
// from MSYS2 or Cygwin has a bash next to it too, and that one is accepted.
func candidates(getenv func(string) string, lookPath func(string) (string, error)) []string {
	var roots []string
	// git.exe is in Git\cmd, Git\bin or Git\mingw64\bin (clangarm64 on arm64);
	// bash.exe is in Git\bin.
	if git, err := lookPath("git"); err == nil {
		parent := filepath.Dir(filepath.Dir(git))
		roots = append(roots, parent)
		switch strings.ToLower(filepath.Base(parent)) {
		case "mingw64", "mingw32", "clangarm64":
			roots = append(roots, filepath.Dir(parent))
		}
	}
	for _, v := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
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
