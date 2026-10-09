package uninstall

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aloglu/triage/internal/config"
	"github.com/aloglu/triage/internal/store"
)

type testInstall struct {
	executable string
	paths      config.Paths
	modCache   string
}

func setupTestInstallation(t *testing.T) testInstall {
	t.Helper()
	dir := t.TempDir()
	install := testInstall{
		executable: filepath.Join(dir, "bin", "triage"),
		paths:      config.Paths{ConfigDir: filepath.Join(dir, "config", "triage"), CacheDir: filepath.Join(dir, "cache", "triage")},
	}
	t.Setenv("TRIAGE_CONFIG_DIR", install.paths.ConfigDir)
	t.Setenv("TRIAGE_CACHE_DIR", install.paths.CacheDir)
	previous := executablePath
	executablePath = func() (string, error) { return install.executable, nil }
	previousCache := goModCache
	install.modCache = filepath.Join(dir, "gomod")
	goModCache = func() string { return install.modCache }
	t.Cleanup(func() { executablePath, goModCache = previous, previousCache })
	for _, src := range []string{"github.com/aloglu/triage@v0.2.0/go.mod", "cache/download/github.com/aloglu/triage/@v/list", "github.com/other/lib@v1.0.0/go.mod"} {
		path := filepath.Join(install.modCache, filepath.FromSlash(src))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o444); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(filepath.Dir(path), 0o555)
	}
	t.Cleanup(func() { makeWritable(install.modCache) })

	for _, path := range []string{install.executable, install.paths.ConfigFile(), filepath.Join(install.paths.CacheDir, "repos", "a__b.json")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return install
}

func TestDryRunDoesNotRemoveAnything(t *testing.T) {
	install := setupTestInstallation(t)
	var out bytes.Buffer
	if err := Run([]string{"--dry-run"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatal(err)
	}
	assertExists(t, install.executable)
	assertExists(t, install.paths.ConfigFile())
	for _, want := range []string{"nothing will be removed", install.executable, install.paths.ConfigDir, install.paths.CacheDir} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestRequiresAffirmativeConfirmation(t *testing.T) {
	install := setupTestInstallation(t)
	var out bytes.Buffer
	if err := Run(nil, strings.NewReader("no\n"), &out, &out); err != nil {
		t.Fatal(err)
	}
	assertExists(t, install.executable)
	if !strings.Contains(out.String(), "Uninstall cancelled") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestKeepDataRemovesOnlyExecutable(t *testing.T) {
	install := setupTestInstallation(t)
	var out bytes.Buffer
	if err := Run([]string{"--keep-data", "--yes"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, install.executable)
	assertExists(t, install.paths.ConfigFile())
	assertExists(t, install.paths.CacheDir)
	assertMissing(t, filepath.Join(install.modCache, "github.com/aloglu/triage@v0.2.0"))
}

func TestRefusesSourceOutsideModuleCache(t *testing.T) {
	install := setupTestInstallation(t)
	if err := validateSourceTarget(filepath.Join(install.modCache, "github.com/other/lib@v1.0.0")); err == nil {
		t.Error("another module's source must not be removable")
	}
	if err := validateSourceTarget(t.TempDir()); err == nil {
		t.Error("a path outside the module cache must not be removable")
	}
}

func TestRemovesEverythingAndWarnsAboutUnsentChanges(t *testing.T) {
	install := setupTestInstallation(t)
	if _, err := store.New(install.paths).Enqueue(store.Op{Kind: store.OpComment, Repo: "a/b", Number: 1, Comment: "hi"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(nil, strings.NewReader("y\n"), &out, &out); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, install.executable)
	assertMissing(t, install.paths.ConfigDir)
	assertMissing(t, install.paths.CacheDir)
	assertMissing(t, filepath.Join(install.modCache, "github.com/aloglu/triage@v0.2.0"))
	assertMissing(t, filepath.Join(install.modCache, "cache/download/github.com/aloglu/triage"))
	assertExists(t, filepath.Join(install.modCache, "github.com/other/lib@v1.0.0/go.mod"))
	if !strings.Contains(out.String(), "1 change(s) haven't been sent") {
		t.Fatalf("expected unsent warning:\n%s", out.String())
	}
}

func TestRefusesToRemoveForeignDirectory(t *testing.T) {
	setupTestInstallation(t)
	foreign := t.TempDir()
	t.Setenv("TRIAGE_CACHE_DIR", foreign)
	var out bytes.Buffer
	if err := Run([]string{"--yes"}, strings.NewReader(""), &out, &out); err == nil {
		t.Fatal("expected refusal")
	}
	assertExists(t, foreign)
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be removed, stat err = %v", path, err)
	}
}
