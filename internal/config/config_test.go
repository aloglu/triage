package config

import (
	"path/filepath"
	"testing"
)

func TestManagerSaveAndLoad(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	manager, err := NewManager()
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	cfg := AppConfig{
		StorageMode: ModeGitHub,
		Repo:        "aloglu/triage-inbox",
		DataFile:    filepath.Join(t.TempDir(), "items.json"),
	}

	if err := manager.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, ok, err := manager.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok {
		t.Fatal("Load() reported missing config after save")
	}

	if got.StorageMode != cfg.StorageMode {
		t.Fatalf("StorageMode = %q, want %q", got.StorageMode, cfg.StorageMode)
	}
	if got.Repo != cfg.Repo {
		t.Fatalf("Repo = %q, want %q", got.Repo, cfg.Repo)
	}
	if got.DataFile != cfg.DataFile {
		t.Fatalf("DataFile = %q, want %q", got.DataFile, cfg.DataFile)
	}
}
