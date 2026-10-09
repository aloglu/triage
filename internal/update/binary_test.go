package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func releaseArchive(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"README.md": "readme", "triage": content} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func serveRelease(t *testing.T, archive []byte, sum string) {
	t.Helper()
	asset := AssetName(runtime.GOOS, runtime.GOARCH)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v9.9.9/checksums.txt":
			fmt.Fprintf(w, "%s  %s\n%s  triage_plan9_mips.tar.gz\n", sum, asset, strings.Repeat("0", 64))
		case "/v9.9.9/" + asset:
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	previous := downloadBase
	downloadBase = server.URL
	t.Cleanup(func() { downloadBase = previous })
}

func TestInstallReleaseReplacesBinary(t *testing.T) {
	archive := releaseArchive(t, "new binary")
	sum := sha256.Sum256(archive)
	serveRelease(t, archive, hex.EncodeToString(sum[:]))

	target := filepath.Join(t.TempDir(), "triage")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := InstallRelease(context.Background(), "v9.9.9", target); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	info, _ := os.Stat(target)
	if string(got) != "new binary" || info.Mode().Perm() != 0o755 {
		t.Fatalf("target = %q, mode %v", got, info.Mode())
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".triage-update-*")); len(leftovers) != 0 {
		t.Fatalf("temp files left behind: %v", leftovers)
	}
}

func TestInstallReleaseRejectsBadChecksum(t *testing.T) {
	serveRelease(t, releaseArchive(t, "tampered"), strings.Repeat("a", 64))
	target := filepath.Join(t.TempDir(), "triage")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := InstallRelease(context.Background(), "v9.9.9", target)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "old binary" {
		t.Fatalf("binary changed after a failed update: %q", got)
	}
}

func TestInstallReleaseMissingVersion(t *testing.T) {
	serveRelease(t, nil, "")
	target := filepath.Join(t.TempDir(), "triage")
	os.WriteFile(target, []byte("old binary"), 0o755)
	if err := InstallRelease(context.Background(), "v0.0.1", target); err == nil {
		t.Fatal("expected an error for a missing release")
	}
}
