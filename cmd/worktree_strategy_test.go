package cmd

import (
	"testing"

	"github.com/mnemcik/consigliere/internal/workspace"
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
