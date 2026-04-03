package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const ConfigFile = ".cg.json"

type Config struct {
	Type    string            `json:"type"`
	Version string            `json:"version"`
	Indexes map[string]string `json:"indexes"`
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

	if cfg.Type != "consigliere" {
		return nil, nil
	}

	return &cfg, nil
}
