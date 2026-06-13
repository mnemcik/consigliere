package pushpolicy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeArea(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLookup(t *testing.T) {
	dir := t.TempDir()
	writeArea(t, dir, "a.md", `## External Repos
| Repo | Policy |
|------|--------|
| org/direct-repo | direct |
| org/pr-repo | pr |
`)

	cases := map[string]string{
		"org/direct-repo": Direct,
		"org/pr-repo":     PR,
		"org/missing":     Unknown,
		"":                Unknown,
	}
	for slug, want := range cases {
		if got := Lookup(dir, slug); got != want {
			t.Errorf("Lookup(%q) = %q, want %q", slug, got, want)
		}
	}
}

func TestLookupConflictingFailsClosed(t *testing.T) {
	dir := t.TempDir()
	writeArea(t, dir, "a.md", "| org/repo | direct |\n")
	writeArea(t, dir, "b.md", "| org/repo | pr |\n")
	if got := Lookup(dir, "org/repo"); got != Unknown {
		t.Errorf("conflicting declarations should be Unknown, got %q", got)
	}
}

func TestSlugFromRemote(t *testing.T) {
	cases := map[string]string{
		"git@github.com:org/repo.git":     "org/repo",
		"https://github.com/org/repo.git": "org/repo",
		"https://github.com/org/repo":     "org/repo",
		"ssh://git@host.com/org/repo.git": "org/repo",
	}
	for url, want := range cases {
		if got := slugFromRemote(url); got != want {
			t.Errorf("slugFromRemote(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestPushArgsAndDestRef(t *testing.T) {
	cases := []struct {
		cmd   string
		dest  string
		broad bool
	}{
		{"git push origin feature", "feature", false},
		{"cd /x && git push origin HEAD:main", "main", false},
		{"git push origin HEAD:refs/heads/feat", "feat", false},
		{"git push --force origin my-branch", "my-branch", false},
		{"git push origin --all", "", true},
		{"git push -o ci.skip origin topic", "topic", false},
	}
	for _, tc := range cases {
		args := pushArgs(tc.cmd)
		if got := broadRe.MatchString(" " + args + " "); got != tc.broad {
			t.Errorf("%q: broad = %v, want %v (args=%q)", tc.cmd, got, tc.broad, args)
		}
		if tc.broad {
			continue
		}
		dest := strings.TrimPrefix(destRef(args), "refs/heads/")
		if dest != tc.dest {
			t.Errorf("%q: destRef = %q, want %q (args=%q)", tc.cmd, dest, tc.dest, args)
		}
	}
}

func TestPermissionJSON(t *testing.T) {
	out := permission("deny", "nope")
	for _, want := range []string{`"hookEventName":"PreToolUse"`, `"permissionDecision":"deny"`, `"permissionDecisionReason":"nope"`} {
		if !strings.Contains(out, want) {
			t.Errorf("permission JSON missing %q: %s", want, out)
		}
	}
}
