package gitbash

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFind(t *testing.T) {
	gitRoot := filepath.Join("C:", "Program Files", "Git")
	gitBash := filepath.Join(gitRoot, "bin", "bash.exe")
	userBash := filepath.Join("C:", "Users", "me", "AppData", "Local", "Programs", "Git", "bin", "bash.exe")
	custom := filepath.Join("D:", "tools", "bash.exe")
	wslBash := filepath.Join("C:", "Windows", "System32", "bash.exe")

	tests := []struct {
		name   string
		goos   string
		env    map[string]string
		path   map[string]string // name -> what LookPath returns
		files  []string
		want   string
		wantOK bool
	}{
		{
			name: "off Windows, bash from PATH", goos: "linux",
			path: map[string]string{"bash": "/usr/bin/bash"},
			want: "/usr/bin/bash", wantOK: true,
		},
		{name: "off Windows, no bash", goos: "darwin"},
		{
			name: "env var wins", goos: "windows",
			env:   map[string]string{EnvVar: custom},
			path:  map[string]string{"git": filepath.Join(gitRoot, "cmd", "git.exe")},
			files: []string{custom, gitBash},
			want:  custom, wantOK: true,
		},
		{
			name: "env var naming a missing file is ignored", goos: "windows",
			env:   map[string]string{EnvVar: custom},
			path:  map[string]string{"git": filepath.Join(gitRoot, "cmd", "git.exe")},
			files: []string{gitBash},
			want:  gitBash, wantOK: true,
		},
		{
			name: "next to git in cmd", goos: "windows",
			path:  map[string]string{"git": filepath.Join(gitRoot, "cmd", "git.exe")},
			files: []string{gitBash},
			want:  gitBash, wantOK: true,
		},
		{
			name: "next to git in mingw64/bin", goos: "windows",
			path:  map[string]string{"git": filepath.Join(gitRoot, "mingw64", "bin", "git.exe")},
			files: []string{gitBash},
			want:  gitBash, wantOK: true,
		},
		{
			name: "default install location without git on PATH", goos: "windows",
			env:   map[string]string{"ProgramFiles": filepath.Join("C:", "Program Files")},
			files: []string{gitBash},
			want:  gitBash, wantOK: true,
		},
		{
			name: "per-user install", goos: "windows",
			env:   map[string]string{"LOCALAPPDATA": filepath.Join("C:", "Users", "me", "AppData", "Local")},
			files: []string{userBash},
			want:  userBash, wantOK: true,
		},
		{
			name: "WSL bash on PATH is not Git Bash", goos: "windows",
			path:  map[string]string{"bash": wslBash},
			files: []string{wslBash},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string { return tt.env[k] }
			lookPath := func(name string) (string, error) {
				if p, ok := tt.path[name]; ok {
					return p, nil
				}
				return "", errors.New("not found")
			}
			exists := func(p string) bool {
				for _, f := range tt.files {
					if f == p {
						return true
					}
				}
				return false
			}
			got, ok := find(tt.goos, getenv, lookPath, exists)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("find() = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// TestFindOnWindowsRunner checks the real lookup on a machine with Git for
// Windows, which CI's Windows runner has.
func TestFindOnWindowsRunner(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Git Bash lookup only applies on Windows")
	}
	p, ok := Find()
	if !ok {
		t.Fatal("Find() found no Git Bash on a runner with Git for Windows")
	}
	if !strings.EqualFold(filepath.Base(p), "bash.exe") || strings.Contains(strings.ToLower(p), `\system32\`) {
		t.Errorf("Find() = %q, want Git for Windows' bash.exe", p)
	}
}
