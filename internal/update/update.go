// Package update finds out whether a newer triage release exists and
// installs it.
package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/store"
)

const (
	// ReleaseRepo is where triage releases are published.
	ReleaseRepo = "aloglu/triage"
	// Package is what `go install` installs.
	Package = "github.com/aloglu/triage/cmd/triage"
	// checkInterval is how often GitHub is asked about new releases.
	checkInterval = 24 * time.Hour
)

// Release describes an available version.
type Release struct {
	Version string
	Notes   string
	URL     string
}

// IsRelease reports whether version is a tagged release, as opposed to a
// development build, which never gets update notices.
func IsRelease(version string) bool {
	return semver.IsValid(version) && semver.Prerelease(version) == "" && semver.Build(version) == ""
}

// Newer reports whether latest is a newer release than current.
func Newer(current, latest string) bool {
	return IsRelease(current) && IsRelease(latest) && semver.Compare(latest, current) > 0
}

// Cached returns the newest release seen so far if it's newer than current,
// without contacting GitHub.
func Cached(st *store.Store, current string) (Release, bool) {
	meta := st.LoadMeta()
	if !Newer(current, meta.LatestVersion) {
		return Release{}, false
	}
	return Release{Version: meta.LatestVersion, Notes: meta.LatestNotes, URL: meta.LatestURL}, true
}

// Due reports whether it's time to ask GitHub again.
func Due(st *store.Store) bool {
	return time.Since(st.LoadMeta().UpdateCheckedAt) > checkInterval
}

// Check asks GitHub for the latest release, remembers it, and returns it
// when it's newer than current.
func Check(ctx context.Context, client *gh.Client, st *store.Store, current string) (Release, bool, error) {
	if client == nil {
		return Release{}, false, errors.New("not signed in to GitHub")
	}
	latest, err := client.LatestRelease(ctx, ReleaseRepo)
	if err != nil {
		return Release{}, false, err
	}
	meta := st.LoadMeta()
	meta.UpdateCheckedAt = time.Now().UTC()
	meta.LatestVersion = latest.TagName
	meta.LatestNotes = latest.Body
	meta.LatestURL = latest.HTMLURL
	if err := st.SaveMeta(meta); err != nil {
		return Release{}, false, err
	}
	release := Release{Version: latest.TagName, Notes: latest.Body, URL: latest.HTMLURL}
	return release, Newer(current, latest.TagName), nil
}

// ShouldNotify reports whether the command line should mention an update
// now, at most once a day; it records that it did.
func ShouldNotify(st *store.Store, current string) (Release, bool) {
	release, ok := Cached(st, current)
	if !ok {
		return Release{}, false
	}
	meta := st.LoadMeta()
	if time.Since(meta.UpdateNotifiedAt) < checkInterval {
		return Release{}, false
	}
	meta.UpdateNotifiedAt = time.Now().UTC()
	_ = st.SaveMeta(meta)
	return release, true
}

// Install runs `go install` for version and returns where the binary went.
func Install(ctx context.Context, version string) (string, error) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return "", errors.New("the Go toolchain (`go`) isn't on your PATH; install Go, then run `triage update` again")
	}
	cmd := exec.CommandContext(ctx, goBin, "install", Package+"@"+version)
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("go install failed: %s", strings.TrimSpace(string(output)))
	}
	return InstallDir(goBin), nil
}

// InstallDir returns the directory `go install` writes binaries to.
func InstallDir(goBin string) string {
	out, err := exec.Command(goBin, "env", "GOBIN", "GOPATH").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) != "" {
		return strings.TrimSpace(lines[0])
	}
	if len(lines) > 1 {
		gopath := strings.Split(strings.TrimSpace(lines[1]), string(os.PathListSeparator))[0]
		return filepath.Join(gopath, "bin")
	}
	return ""
}

// RunningFrom returns the directory of the running triage binary.
func RunningFrom() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}
