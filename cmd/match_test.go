package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseProjectIndex(t *testing.T) {
	dir := t.TempDir()
	content := `# Projects & Todo List

## Active Projects

| # | Project | Status | Areas | Folder |
|---|---------|--------|-------|--------|
| 1 | OAuth Strategy | In Progress | ` + "`identity-auth`" + ` | [oauth-strategy](oauth-strategy/) |
| 2 | Daily Tips | In Progress | ` + "`claude`" + ` | [daily-tips](daily-tips/) |
| 3 | Git Branching | Defining | ` + "`devops`" + ` | [git-branching](git-branching/) |
`

	indexPath := filepath.Join(dir, "TODO.md")
	if err := os.WriteFile(indexPath, []byte(content), 0o644); err != nil {
		t.Fatalf("cannot write index: %v", err)
	}

	projects, err := parseProjectIndex(indexPath)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(projects) != 3 {
		t.Fatalf("expected 3 projects, got %d", len(projects))
	}

	if projects[0].Name != "OAuth Strategy" {
		t.Errorf("expected 'OAuth Strategy', got '%s'", projects[0].Name)
	}

	if projects[0].Folder != "oauth-strategy" {
		t.Errorf("expected folder 'oauth-strategy', got '%s'", projects[0].Folder)
	}

	if projects[2].Status != "Defining" {
		t.Errorf("expected status 'Defining', got '%s'", projects[2].Status)
	}
}

func TestExtractFolderSlug(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"[oauth-strategy](oauth-strategy/)", "oauth-strategy"},
		{"[my-project](my-project/)", "my-project"},
		{"plain-slug", "plain-slug"},
		{"plain-slug/", "plain-slug"},
		{"[name](path/to/slug/)", "slug"},
	}

	for _, tt := range tests {
		result := extractFolderSlug(tt.input)
		if result != tt.expected {
			t.Errorf("extractFolderSlug(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestExtractAreaSlugs(t *testing.T) {
	result := extractAreaSlugs("`identity-auth`, `devops-release`")
	if len(result) != 2 {
		t.Fatalf("expected 2 slugs, got %d", len(result))
	}
	if result[0] != "identity-auth" {
		t.Errorf("expected 'identity-auth', got '%s'", result[0])
	}
	if result[1] != "devops-release" {
		t.Errorf("expected 'devops-release', got '%s'", result[1])
	}
}

func TestTokenize(t *testing.T) {
	result := tokenize("oauth identity provider strategy")
	expected := []string{"oauth", "identity", "provider", "strategy"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(result))
	}
	for i, v := range expected {
		if result[i] != v {
			t.Errorf("token[%d]: expected '%s', got '%s'", i, v, result[i])
		}
	}
}
