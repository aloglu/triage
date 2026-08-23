package fileutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAtomicWriteFileDoesNotChangeExistingDirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-style permission bits are not stable on windows")
	}

	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	path := filepath.Join(dir, "items.json")
	if err := AtomicWriteFile(path, []byte("[]\n"), 0o700, 0o600); err != nil {
		t.Fatalf("AtomicWriteFile() error = %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o750 {
		t.Fatalf("existing directory mode = %#o, want %#o", got, 0o750)
	}
}

func TestAtomicWriteFileUsesPrivatePermissionsForNewDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-style permission bits are not stable on windows")
	}

	dir := filepath.Join(t.TempDir(), "new", "private")
	path := filepath.Join(dir, "items.json")
	if err := AtomicWriteFile(path, []byte("[]\n"), 0o700, 0o600); err != nil {
		t.Fatalf("AtomicWriteFile() error = %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("new directory mode = %#o, want %#o", got, 0o700)
	}
}

func TestReplaceFileWindowsPreservesReplacementSemantics(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.json")
	tempPath := filepath.Join(dir, ".items.tmp")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("write destination: %v", err)
	}
	if err := os.WriteFile(tempPath, []byte("new"), 0o600); err != nil {
		t.Fatalf("write temporary file: %v", err)
	}

	if err := replaceFileWindows(tempPath, path); err != nil {
		t.Fatalf("replaceFileWindows() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("replacement contents = %q, want %q", got, "new")
	}
	if _, err := os.Stat(tempPath + ".previous"); !os.IsNotExist(err) {
		t.Fatalf("backup still exists after successful replacement: %v", err)
	}
}
