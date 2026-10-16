package cmd

import (
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/workspace"
	"github.com/mnemcik/consigliere/internal/worktree"
)

func TestLandStrategy(t *testing.T) {
	tests := []struct {
		flag, resolved, want string
		wantErr              bool
	}{
		{"", workspace.StrategyLocal, workspace.StrategyLocal, false},
		{"", workspace.StrategyDirectToMain, workspace.StrategyDirectToMain, false},
		{workspace.StrategyPR, workspace.StrategyDirectToMain, workspace.StrategyPR, false},
		{workspace.StrategyLocal, workspace.StrategyLocal, workspace.StrategyLocal, false},
		{workspace.StrategyLocal, workspace.StrategyDirectToMain, "", true},
		{workspace.StrategyDirectToMain, workspace.StrategyLocal, "", true},
	}
	for _, tc := range tests {
		got, err := landStrategy(tc.flag, tc.resolved)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("landStrategy(%q, %q) = %q, %v; want %q, err=%v", tc.flag, tc.resolved, got, err, tc.want, tc.wantErr)
		}
	}
}

// A land whose push succeeded but could not be verified must be told apart from
// a verified one on stdout, not only by a warning on stderr: the SHA stays the
// first line, and an "unverified" line follows it.
func TestWriteLandResult(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	tests := []struct {
		name string
		res  worktree.LandResult
		want string
	}{
		{"verified", worktree.LandResult{Strategy: workspace.StrategyDirectToMain, SHA: sha}, sha + "\n"},
		{"unverified", worktree.LandResult{Strategy: workspace.StrategyDirectToMain, SHA: sha, Unverified: true}, sha + "\nunverified\n"},
		{"local", worktree.LandResult{Strategy: workspace.StrategyLocal, SHA: sha}, sha + "\n"},
		{"pr", worktree.LandResult{Strategy: workspace.StrategyPR, PRURL: "https://example.com/pr/1"}, "https://example.com/pr/1\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			writeLandResult(&b, tc.res)
			if b.String() != tc.want {
				t.Errorf("stdout = %q, want %q", b.String(), tc.want)
			}
		})
	}
}
