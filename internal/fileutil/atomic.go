package fileutil

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// AtomicWriteFile writes data to path with private permissions and replaces the
// destination only after the temporary file is fully written.
func AtomicWriteFile(path string, data []byte, dirPerm, filePerm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	if err := os.Chmod(dir, dirPerm); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("chmod dir: %w", err)
	}

	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()

	if err := temp.Chmod(filePerm); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := replaceFile(tempPath, path); err != nil {
		return err
	}
	cleanup = false

	if err := os.Chmod(path, filePerm); err != nil {
		return fmt.Errorf("chmod file: %w", err)
	}

	if dirHandle, err := os.Open(dir); err == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}

	return nil
}

func replaceFile(tempPath, path string) error {
	if err := os.Rename(tempPath, path); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return fmt.Errorf("replace file: %w", err)
	}

	_ = os.Remove(path)
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	return nil
}
