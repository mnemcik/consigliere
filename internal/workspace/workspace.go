package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const ConfigFile = ".cg.json"

// TypeConsigliere is the value of the .cg.json "type" field that marks a
// directory as a Consigliere workspace.
const TypeConsigliere = "consigliere"

// Default values for the optional v1.1 config blocks. They are baked into the
// binary so an absent or pre-1.1 .cg.json behaves exactly as before: the
// *Settings() accessors fill any zero field with these.
const (
	DefaultBranchPrefix     = "session/"
	DefaultLandingBranch    = "main"
	DefaultLandingStrategy  = "direct-to-main"
	DefaultActiveWindowMin  = 240  // 4h — clean-session liveness window
	DefaultDirtyWindowMin   = 2880 // 48h — dirty-session liveness window
	DefaultPruneDays        = 7
	DefaultBadgeFormat      = "[{area}/{project}]"
	DefaultPushPolicySource = "areas"
)

// Landing strategies recognised by `cg worktree land`.
const (
	StrategyDirectToMain = "direct-to-main"
	StrategyPR           = "pr"
)

type Config struct {
	Type    string            `json:"type"`
	Version string            `json:"version"`
	Indexes map[string]string `json:"indexes"`

	// v1.1 additive blocks. All optional; nil means "use defaults".
	Worktree   *WorktreeConfig   `json:"worktree,omitempty"`
	Session    *SessionConfig    `json:"session,omitempty"`
	PushPolicy *PushPolicyConfig `json:"pushPolicy,omitempty"`
}

// WorktreeConfig tunes `cg worktree`. Root overrides the prefix used to derive
// the worktree directory (default: the main workspace root); the worktree path
// is "<root>--<slug>" and the branch is "<branchPrefix><slug>".
type WorktreeConfig struct {
	Root            string `json:"root,omitempty"`
	BranchPrefix    string `json:"branchPrefix,omitempty"`
	LandingBranch   string `json:"landingBranch,omitempty"`
	LandingStrategy string `json:"landingStrategy,omitempty"`
}

// SessionConfig tunes the `cg session` hook bodies.
type SessionConfig struct {
	ActiveWindowMin    int    `json:"activeWindowMin,omitempty"`
	DirtyWindowMin     int    `json:"dirtyWindowMin,omitempty"`
	PruneDays          int    `json:"pruneDays,omitempty"`
	GateTemplate       string `json:"gateTemplate,omitempty"`
	StatuslineUpstream string `json:"statuslineUpstream,omitempty"`
	BadgeFormat        string `json:"badgeFormat,omitempty"`
}

// PushPolicyConfig tunes `cg push-policy`.
type PushPolicyConfig struct {
	Source string `json:"source,omitempty"`
}

// WorktreeSettings returns the effective worktree settings with defaults
// applied. Safe to call on a nil *Config (returns all-defaults).
func (c *Config) WorktreeSettings() WorktreeConfig {
	var w WorktreeConfig
	if c != nil && c.Worktree != nil {
		w = *c.Worktree
	}
	if w.BranchPrefix == "" {
		w.BranchPrefix = DefaultBranchPrefix
	}
	if w.LandingBranch == "" {
		w.LandingBranch = DefaultLandingBranch
	}
	if w.LandingStrategy == "" {
		w.LandingStrategy = DefaultLandingStrategy
	}
	return w
}

// SessionSettings returns the effective session settings with defaults applied.
// Safe to call on a nil *Config.
func (c *Config) SessionSettings() SessionConfig {
	var s SessionConfig
	if c != nil && c.Session != nil {
		s = *c.Session
	}
	if s.ActiveWindowMin == 0 {
		s.ActiveWindowMin = DefaultActiveWindowMin
	}
	if s.DirtyWindowMin == 0 {
		s.DirtyWindowMin = DefaultDirtyWindowMin
	}
	if s.PruneDays == 0 {
		s.PruneDays = DefaultPruneDays
	}
	if s.BadgeFormat == "" {
		s.BadgeFormat = DefaultBadgeFormat
	}
	return s
}

// PushPolicySettings returns the effective push-policy settings with defaults
// applied. Safe to call on a nil *Config.
func (c *Config) PushPolicySettings() PushPolicyConfig {
	var p PushPolicyConfig
	if c != nil && c.PushPolicy != nil {
		p = *c.PushPolicy
	}
	if p.Source == "" {
		p.Source = DefaultPushPolicySource
	}
	return p
}

// Detect checks if the given directory is a Consigliere workspace.
// Returns the config if found, nil otherwise.
func Detect(dir string) (*Config, error) {
	path := filepath.Join(dir, ConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Type != TypeConsigliere {
		return nil, nil
	}

	return &cfg, nil
}

// FindRoot walks up from startDir looking for the workspace root: the nearest
// ancestor containing a valid .cg.json. As a fallback (for workspaces that
// predate .cg.json or whose config is being repaired) it also accepts a
// directory carrying the structural markers CLAUDE.md + areas/INDEX.md +
// projects/TODO.md. It returns the root and its config (cfg may be nil when the
// root was matched only by the structural fallback). If no root is found it
// returns ("", nil, nil) — absence is not an error. This ports the workspace
// root detection the bash hooks performed from an arbitrary cwd.
func FindRoot(startDir string) (root string, cfg *Config, err error) {
	dir := filepath.Clean(startDir)
	for {
		if c, derr := Detect(dir); derr != nil {
			return "", nil, derr
		} else if c != nil {
			return dir, c, nil
		}
		if hasStructuralMarkers(dir) {
			return dir, nil, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil, nil // reached filesystem root
		}
		dir = parent
	}
}

func hasStructuralMarkers(dir string) bool {
	markers := []string{
		"CLAUDE.md",
		filepath.Join("areas", "INDEX.md"),
		filepath.Join("projects", "TODO.md"),
	}
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(dir, m)); err != nil {
			return false
		}
	}
	return true
}
