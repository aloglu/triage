package config

import (
	"path/filepath"
	"testing"
	"time"
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
		ProjectRepos: map[string]string{
			"serein":    "aloglu/serein",
			"inkubator": "aloglu/inkubator",
		},
		DataFile:         filepath.Join(t.TempDir(), "items.json"),
		Density:          "compact",
		ProjectLabelSync: ProjectLabelNever,
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
	if len(got.ProjectRepos) != len(cfg.ProjectRepos) {
		t.Fatalf("ProjectRepos length = %d, want %d", len(got.ProjectRepos), len(cfg.ProjectRepos))
	}
	for project, repo := range cfg.ProjectRepos {
		if got.ProjectRepos[project] != repo {
			t.Fatalf("ProjectRepos[%q] = %q, want %q", project, got.ProjectRepos[project], repo)
		}
	}
	if got.DataFile != cfg.DataFile {
		t.Fatalf("DataFile = %q, want %q", got.DataFile, cfg.DataFile)
	}
	if got.Density != cfg.Density {
		t.Fatalf("Density = %q, want %q", got.Density, cfg.Density)
	}
	if got.ProjectLabelSync != cfg.ProjectLabelSync {
		t.Fatalf("ProjectLabelSync = %q, want %q", got.ProjectLabelSync, cfg.ProjectLabelSync)
	}
}

func TestNormalizeDefaultsProjectLabelSyncToAuto(t *testing.T) {
	got := Normalize(AppConfig{})
	if got.ProjectLabelSync != ProjectLabelAuto {
		t.Fatalf("ProjectLabelSync = %q, want %q", got.ProjectLabelSync, ProjectLabelAuto)
	}
}

func TestNormalizeProjectRepos(t *testing.T) {
	got := Normalize(AppConfig{
		ProjectRepos: map[string]string{
			" Serein ": "aloglu/serein",
			"":         "owner/skip",
			"Broken":   "not-a-repo",
		},
	})

	if len(got.ProjectRepos) != 1 {
		t.Fatalf("ProjectRepos length = %d, want 1", len(got.ProjectRepos))
	}
	if got.ProjectRepos["serein"] != "aloglu/serein" {
		t.Fatalf("ProjectRepos[\"serein\"] = %q, want %q", got.ProjectRepos["serein"], "aloglu/serein")
	}
}

func TestNormalizeLastSuccessfulSyncAtUTC(t *testing.T) {
	local := time.Date(2026, 4, 10, 12, 0, 0, 0, time.FixedZone("+03", 3*60*60))
	got := Normalize(AppConfig{LastSuccessfulSyncAt: local})

	if got.LastSuccessfulSyncAt.IsZero() {
		t.Fatal("expected last successful sync time to be preserved")
	}
	if got.LastSuccessfulSyncAt.Location() != time.UTC {
		t.Fatalf("LastSuccessfulSyncAt location = %v, want UTC", got.LastSuccessfulSyncAt.Location())
	}
}
