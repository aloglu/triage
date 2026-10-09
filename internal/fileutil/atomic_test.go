package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFileDoesNotChangeExistingDirectoryPermissions(t *testing.T) {

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
