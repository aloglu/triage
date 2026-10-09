package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingReturnsDefaults(t *testing.T) {
	cfg, exists, err := Load(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil || exists {
		t.Fatalf("Load = exists %v, err %v", exists, err)
	}
	if cfg.RefreshMinutes != 5 || cfg.Labels.InProgress != "in progress" {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "triage", "config.toml")
	cfg := Default()
	cfg.Repos = []string{"aloglu/triage", "https://github.com/aloglu/bookshelf.git", "aloglu/triage"}
	cfg.DefaultRepo = "aloglu/triage"
	cfg.Labels.InProgress = "wip"
	cfg.Views = []View{{Name: "UI bugs", Query: "type:bug label:ui"}}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	got, exists, err := Load(path)
	if err != nil || !exists {
		t.Fatalf("Load = exists %v, err %v", exists, err)
	}
	if strings.Join(got.Repos, ",") != "aloglu/triage,aloglu/bookshelf" {
		t.Errorf("repos = %v", got.Repos)
	}
	if got.Labels.InProgress != "wip" || got.Labels.Bug != "bug" {
		t.Errorf("labels = %+v", got.Labels)
	}
	if len(got.Views) != 1 || got.Views[0].Query != "type:bug label:ui" {
		t.Errorf("views = %+v", got.Views)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("config permissions = %v, %v", info.Mode().Perm(), err)
	}
}

func TestLoadReportsBadRepo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`repos = ["not a repo", "a/b"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "not a repo") {
		t.Fatalf("expected error naming the bad repo, got %v", err)
	}
	if len(cfg.Repos) != 1 || cfg.Repos[0] != "a/b" {
		t.Fatalf("valid repos should survive: %v", cfg.Repos)
	}
}

func TestAddRemoveRepo(t *testing.T) {
	cfg := Default()
	if !cfg.AddRepo("a/b") || cfg.AddRepo("A/B") {
		t.Fatal("AddRepo should dedupe case-insensitively")
	}
	cfg.DefaultRepo = "a/b"
	if !cfg.RemoveRepo("a/B") || cfg.DefaultRepo != "" || len(cfg.Repos) != 0 {
		t.Fatalf("RemoveRepo left %+v", cfg)
	}
}
