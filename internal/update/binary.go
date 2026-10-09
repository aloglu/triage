package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// downloadBase is where release files live; tests point it elsewhere.
var downloadBase = "https://github.com/" + ReleaseRepo + "/releases/download"

// AssetName is the release archive for a platform.
func AssetName(goos, goarch string) string {
	return fmt.Sprintf("triage_%s_%s.tar.gz", goos, goarch)
}

var downloadClient = &http.Client{Timeout: 3 * time.Minute}

// InstallRelease replaces the binary at target with version's release
// build for this platform. The download is checked against the release's
// checksums, and target is replaced in one step, so a failure at any point
// leaves the current binary untouched.
func InstallRelease(ctx context.Context, version, target string) error {
	asset := AssetName(runtime.GOOS, runtime.GOARCH)
	sums, err := fetch(ctx, version, "checksums.txt")
	if err != nil {
		return err
	}
	want, err := checksumFor(sums, asset)
	if err != nil {
		return err
	}
	archive, err := fetch(ctx, version, asset)
	if err != nil {
		return err
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("the downloaded %s doesn't match its checksum; nothing was changed", asset)
	}
	binary, err := extractBinary(archive)
	if err != nil {
		return err
	}
	return replaceFile(target, binary)
}

func fetch(ctx context.Context, version, name string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s/%s", downloadBase, version, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: GitHub answered %s", name, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

// checksumFor finds name's SHA-256 in a checksums.txt file.
func checksumFor(sums []byte, name string) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("no prebuilt triage for %s/%s in this release", runtime.GOOS, runtime.GOARCH)
}

// extractBinary returns the triage executable from a release archive.
func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("the release archive has no triage binary")
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == "triage" {
			return io.ReadAll(io.LimitReader(tr, 200<<20))
		}
	}
}

// replaceFile writes data next to target and renames it over target.
func replaceFile(target string, data []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".triage-update-*")
	if err != nil {
		return fmt.Errorf("can't write to %s (%w); reinstall with the install script or move triage somewhere you own", dir, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("replace %s: %w", target, err)
	}
	return nil
}

// Executable returns the path of the running binary, with symlinks
// resolved.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
