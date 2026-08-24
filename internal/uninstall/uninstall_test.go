package uninstall

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aloglu/triage/internal/config"
)

func TestRunDryRunDoesNotRemoveAnything(t *testing.T) {
	paths := setupTestInstallation(t, false)
	var out bytes.Buffer

	if err := Run([]string{"--dry-run"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertExists(t, paths.executable)
	assertExists(t, paths.configFile)
	if !strings.Contains(out.String(), "nothing will be removed") {
		t.Fatalf("output = %q, want dry-run notice", out.String())
	}
	if !strings.Contains(out.String(), paths.executable) || !strings.Contains(out.String(), filepath.Dir(paths.configFile)) {
		t.Fatalf("output = %q, want planned paths", out.String())
	}
}

func TestRunRequiresAffirmativeConfirmation(t *testing.T) {
	paths := setupTestInstallation(t, false)
	var out bytes.Buffer

	if err := Run(nil, strings.NewReader("no\n"), &out, &out); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertExists(t, paths.executable)
	assertExists(t, paths.configFile)
	if !strings.Contains(out.String(), "Uninstall cancelled") {
		t.Fatalf("output = %q, want cancellation", out.String())
	}
}

func TestRunKeepDataRemovesOnlyExecutable(t *testing.T) {
	paths := setupTestInstallation(t, false)
	var out bytes.Buffer

	if err := Run([]string{"--keep-data", "--yes"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertMissing(t, paths.executable)
	assertExists(t, paths.configFile)
	assertExists(t, paths.dataFile)
	assertExists(t, paths.draftFile)
}

func TestRunRemovesDefaultAndCustomPaths(t *testing.T) {
	paths := setupTestInstallation(t, true)
	var out bytes.Buffer

	if err := Run([]string{"--yes"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertMissing(t, paths.executable)
	assertMissing(t, paths.configFile)
	assertMissing(t, paths.dataFile)
	assertMissing(t, filepath.Dir(paths.draftFile))
	if !strings.Contains(out.String(), "GitHub issues and repository labels will not be changed") {
		t.Fatalf("output = %q, want GitHub preservation notice", out.String())
	}
}

func TestExecuteRejectsHomeDirectoryBeforeRemovingAnything(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	executable := filepath.Join(t.TempDir(), "triage")
	writeFile(t, executable, "binary")

	plan := Plan{targets: []target{
		{kind: targetBinary, path: executable},
		{kind: targetDraftsDir, path: home, recursive: true},
	}}
	if err := execute(plan, &bytes.Buffer{}); err == nil {
		t.Fatal("execute() error = nil, want unsafe path error")
	}
	assertExists(t, executable)
}

func TestPrintPathsShowsConfiguredLocations(t *testing.T) {
	paths := setupTestInstallation(t, true)
	var out bytes.Buffer

	if err := PrintPaths(&out); err != nil {
		t.Fatalf("PrintPaths() error = %v", err)
	}
	for _, want := range []string{paths.executable, paths.configFile, paths.dataFile, filepath.Dir(filepath.Dir(paths.draftFile))} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output = %q, want %q", out.String(), want)
		}
	}
}

type testPaths struct {
	executable string
	configFile string
	dataFile   string
	draftFile  string
}

func setupTestInstallation(t *testing.T, customPaths bool) testPaths {
	t.Helper()
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("HOME", t.TempDir())

	executable := filepath.Join(t.TempDir(), "bin", "triage")
	writeFile(t, executable, "binary")
	previousExecutablePath := executablePath
	executablePath = func() (string, error) { return executable, nil }
	t.Cleanup(func() { executablePath = previousExecutablePath })

	manager, err := config.NewManager()
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	dataFile, err := config.DefaultDataFile()
	if err != nil {
		t.Fatalf("DefaultDataFile() error = %v", err)
	}
	draftsFolder, err := config.DefaultDraftsFolder()
	if err != nil {
		t.Fatalf("DefaultDraftsFolder() error = %v", err)
	}
	if customPaths {
		dataFile = filepath.Join(t.TempDir(), "data", "items.json")
		draftsFolder = filepath.Join(t.TempDir(), "drafts")
	}
	draftFile := filepath.Join(draftsFolder, "processed", "draft.md")
	writeFile(t, dataFile, "[]\n")
	writeFile(t, draftFile, "draft\n")
	if err := manager.Save(config.AppConfig{
		StorageMode:  config.ModeLocal,
		DataFile:     dataFile,
		DraftsFolder: draftsFolder,
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	return testPaths{
		executable: executable,
		configFile: manager.Path(),
		dataFile:   dataFile,
		draftFile:  draftFile,
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %q to exist: %v", path, err)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %q to be absent, stat error = %v", path, err)
	}
}
