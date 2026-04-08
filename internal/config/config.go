package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	ModeLocal  = "local"
	ModeGitHub = "github"
)

type AppConfig struct {
	StorageMode string `json:"storage_mode"`
	Repo        string `json:"repo,omitempty"`
	DataFile    string `json:"data_file"`
	Density     string `json:"density,omitempty"`
}

type Manager struct {
	path string
}

func NewManager() (*Manager, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve config dir: %w", err)
	}

	return &Manager{
		path: filepath.Join(configDir, "triage", "config.json"),
	}, nil
}

func (m *Manager) Path() string {
	return m.path
}

func (m *Manager) Load() (AppConfig, bool, error) {
	var cfg AppConfig

	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, false, nil
		}
		return cfg, false, fmt.Errorf("read config: %w", err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, false, fmt.Errorf("decode config: %w", err)
	}

	if cfg.StorageMode == "" {
		return cfg, false, fmt.Errorf("config missing storage mode")
	}
	if cfg.DataFile == "" {
		return cfg, false, fmt.Errorf("config missing data file")
	}
	if cfg.Density == "" {
		cfg.Density = "comfortable"
	}

	return cfg, true, nil
}

func (m *Manager) Save(cfg AppConfig) error {
	if cfg.StorageMode == "" {
		return fmt.Errorf("storage mode is required")
	}
	if cfg.DataFile == "" {
		return fmt.Errorf("data file is required")
	}
	if cfg.Density == "" {
		cfg.Density = "comfortable"
	}

	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	if err := os.WriteFile(m.path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

func DefaultDataFile() (string, error) {
	dataDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve data dir: %w", err)
	}

	return filepath.Join(dataDir, "triage", "items.json"), nil
}
