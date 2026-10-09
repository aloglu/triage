package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/aloglu/triage/internal/config"
	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/store"
)

func TestNewer(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"v0.2.0", "v0.2.1", true},
		{"v0.2.1", "v0.2.1", false},
		{"v0.3.0", "v0.2.1", false},
		{"v0.2.0", "v0.10.0", true},
		{"dev", "v0.2.1", false},
		{"v0.2.1-0.20261009112233-abcdef123456", "v0.3.0", false}, // pseudo-version
		{"v0.2.0+dirty", "v0.3.0", false},
		{"v0.2.0", "v0.3.0-rc.1", false},
	}
	for _, tt := range tests {
		if got := Newer(tt.current, tt.latest); got != tt.want {
			t.Errorf("Newer(%q, %q) = %v", tt.current, tt.latest, got)
		}
	}
}

func TestCheckCachesAndNotifiesOncePerDay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/aloglu/triage/releases/latest" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"tag_name":"v0.3.0","body":"notes","html_url":"https://example/rel"}`))
	}))
	defer server.Close()
	dir := t.TempDir()
	st := store.New(config.Paths{ConfigDir: filepath.Join(dir, "c"), CacheDir: filepath.Join(dir, "k")})
	client := gh.NewWithHTTP(server.Client(), server.URL, "github.com")

	if !Due(st) {
		t.Fatal("a fresh install should be due for a check")
	}
	release, newer, err := Check(context.Background(), client, st, "v0.2.0")
	if err != nil || !newer || release.Version != "v0.3.0" || release.Notes != "notes" {
		t.Fatalf("Check = %+v, %v, %v", release, newer, err)
	}
	if Due(st) {
		t.Fatal("should not be due right after a check")
	}
	if cached, ok := Cached(st, "v0.2.0"); !ok || cached.Version != "v0.3.0" {
		t.Fatalf("Cached = %+v, %v", cached, ok)
	}
	if _, ok := Cached(st, "v0.3.0"); ok {
		t.Fatal("no update for the latest version")
	}
	if _, ok := ShouldNotify(st, "v0.2.0"); !ok {
		t.Fatal("first notice should be shown")
	}
	if _, ok := ShouldNotify(st, "v0.2.0"); ok {
		t.Fatal("second notice on the same day should be suppressed")
	}
}
