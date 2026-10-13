package gitbash

import (
	"errors"
	"os"
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
			name: "git in cmd doesn't look in the folder above Git", goos: "windows",
			path:  map[string]string{"git": filepath.Join(gitRoot, "cmd", "git.exe")},
			files: []string{filepath.Join("C:", "Program Files", "bin", "bash.exe")},
		},
		{
			name: "32-bit install location", goos: "windows",
			env:   map[string]string{"ProgramFiles(x86)": filepath.Join("C:", "Program Files (x86)")},
			files: []string{filepath.Join("C:", "Program Files (x86)", "Git", "bin", "bash.exe")},
			want:  filepath.Join("C:", "Program Files (x86)", "Git", "bin", "bash.exe"), wantOK: true,
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
			got, ok := find(tt.goos, getenv, nil, lookPath, exists)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("find() = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// EnvVar set in the "env" block of Claude Code's settings files is found the way
// Claude Code's docs say to set it, in their precedence order, and wins over the
// process environment, as it does in Claude Code.
func TestConfiguredFromSettings(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	local := write("local.json", `{"env": {"`+EnvVar+`": "local"}}`)
	shared := write("shared.json", `{"env": {"`+EnvVar+`": "shared"}}`)
	other := write("other.json", `{"env": {"OTHER": "x"}}`)
	broken := write("broken.json", `{not json`)
	mixed := write("mixed.json", `{"env": {"DISABLE_TELEMETRY": 1, "`+EnvVar+`": "mixed"}}`)
	missing := filepath.Join(dir, "missing.json")
	noEnv := func(string) string { return "" }
	fromEnv := func(k string) string {
		if k == EnvVar {
			return "env"
		}
		return ""
	}

	tests := []struct {
		name     string
		getenv   func(string) string
		settings []string
		want     string
	}{
		{"first file that sets it wins", noEnv, []string{local, shared}, "local"},
		{"files without it, unreadable or broken are skipped", noEnv, []string{missing, broken, other, shared}, "shared"},
		{"a non-string env entry doesn't hide it", noEnv, []string{mixed}, "mixed"},
		{"settings win over the environment", fromEnv, []string{local}, "local"},
		{"environment when no settings file sets it", fromEnv, []string{other}, "env"},
		{"nowhere", noEnv, []string{other}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := configured(tt.getenv, tt.settings); got != tt.want {
				t.Errorf("configured() = %q, want %q", got, tt.want)
			}
		})
	}

	// find uses a settings value only when that file exists.
	bash := write("bash.exe", "")
	settings := write("settings.json", `{"env": {"`+EnvVar+`": `+jsonString(bash)+`}}`)
	notFound := func(string) (string, error) { return "", errors.New("not found") }
	if got, ok := find("windows", noEnv, []string{settings}, notFound, fileExists); got != bash || !ok {
		t.Errorf("find() = %q, %v; want %q from settings", got, ok, bash)
	}

	// Like Claude Code, a configured file not named bash or sh is ignored.
	notBash := write("cmd.exe", "")
	settings = write("settings.json", `{"env": {"`+EnvVar+`": `+jsonString(notBash)+`}}`)
	if got, ok := find("windows", noEnv, []string{settings}, notFound, fileExists); ok {
		t.Errorf("find() = %q, accepted a configured file not named bash or sh", got)
	}
}

func jsonString(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

func TestSettingsFiles(t *testing.T) {
	root, home := filepath.Join("w"), filepath.Join("h")
	want := []string{
		filepath.Join(root, ".claude", "settings.local.json"),
		filepath.Join(root, ".claude", "settings.json"),
		filepath.Join(home, ".claude", "settings.json"),
	}
	got := settingsFiles(root, home)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("settingsFiles() = %v, want %v", got, want)
	}
	if got := settingsFiles("", home); len(got) != 1 {
		t.Errorf("settingsFiles without a root = %v, want only the user's", got)
	}
}

// TestFindOnWindowsRunner checks the real lookup on a machine with Git for
// Windows, which CI's Windows runner has.
func TestFindOnWindowsRunner(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Git Bash lookup only applies on Windows")
	}
	p, ok := Find("")
	if !ok {
		t.Fatal("Find() found no Git Bash on a runner with Git for Windows")
	}
	if !strings.EqualFold(filepath.Base(p), "bash.exe") || strings.Contains(strings.ToLower(p), `\system32\`) {
		t.Errorf("Find() = %q, want Git for Windows' bash.exe", p)
	}
}
