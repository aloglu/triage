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
		TrackedRepos: []string{
			"aloglu/triage-inbox",
			"owner/secondary-repo",
		},
		DataFile: filepath.Join(t.TempDir(), "items.json"),
		Density:  "compact",
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
	if len(got.TrackedRepos) != len(cfg.TrackedRepos) {
		t.Fatalf("TrackedRepos length = %d, want %d", len(got.TrackedRepos), len(cfg.TrackedRepos))
	}
	for idx := range cfg.TrackedRepos {
		if got.TrackedRepos[idx] != cfg.TrackedRepos[idx] {
			t.Fatalf("TrackedRepos[%d] = %q, want %q", idx, got.TrackedRepos[idx], cfg.TrackedRepos[idx])
		}
	}
	if got.DataFile != cfg.DataFile {
		t.Fatalf("DataFile = %q, want %q", got.DataFile, cfg.DataFile)
	}
	if got.Density != cfg.Density {
		t.Fatalf("Density = %q, want %q", got.Density, cfg.Density)
	}
}
