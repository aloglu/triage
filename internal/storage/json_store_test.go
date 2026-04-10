package storage

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/aloglu/triage/internal/model"
)

func TestJSONStoreSaveAndLoadItems(t *testing.T) {
	store := NewJSONStore(filepath.Join(t.TempDir(), "items.json"))

	now := time.Now().UTC().Truncate(time.Second)
	want := []model.Item{
		{
			Title:       "Persist an item",
			Project:     "triage",
			Stage:       model.StageActive,
			Body:        "Body text",
			CreatedAt:   now,
			UpdatedAt:   now,
			IssueNumber: 12,
			Repo:        "aloglu/triage-inbox",
		},
	}

	if err := store.SaveItems(want); err != nil {
		t.Fatalf("SaveItems() error = %v", err)
	}

	got, ok, err := store.LoadItems()
	if err != nil {
		t.Fatalf("LoadItems() error = %v", err)
	}
	if !ok {
		t.Fatal("LoadItems() reported missing file after save")
	}
	if len(got) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(got))
	}

	if got[0].Title != want[0].Title {
		t.Fatalf("Title = %q, want %q", got[0].Title, want[0].Title)
	}
	if got[0].Project != want[0].Project {
		t.Fatalf("Project = %q, want %q", got[0].Project, want[0].Project)
	}
	if got[0].Stage != want[0].Stage {
		t.Fatalf("Stage = %q, want %q", got[0].Stage, want[0].Stage)
	}
	if got[0].Body != want[0].Body {
		t.Fatalf("Body = %q, want %q", got[0].Body, want[0].Body)
	}
	if !got[0].CreatedAt.Equal(want[0].CreatedAt) {
		t.Fatalf("CreatedAt = %v, want %v", got[0].CreatedAt, want[0].CreatedAt)
	}
	if !got[0].UpdatedAt.Equal(want[0].UpdatedAt) {
		t.Fatalf("UpdatedAt = %v, want %v", got[0].UpdatedAt, want[0].UpdatedAt)
	}
	if got[0].IssueNumber != want[0].IssueNumber {
		t.Fatalf("IssueNumber = %d, want %d", got[0].IssueNumber, want[0].IssueNumber)
	}
	if got[0].Repo != want[0].Repo {
		t.Fatalf("Repo = %q, want %q", got[0].Repo, want[0].Repo)
	}
}

func TestJSONStoreSaveItemsUsesPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-style permission bits are not stable on windows")
	}

	store := NewJSONStore(filepath.Join(t.TempDir(), "items.json"))
	if err := store.SaveItems([]model.Item{{Title: "Private"}}); err != nil {
		t.Fatalf("SaveItems() error = %v", err)
	}

	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("items file mode = %#o, want %#o", got, 0o600)
	}

	dirInfo, err := os.Stat(filepath.Dir(store.Path()))
	if err != nil {
		t.Fatalf("Stat(items dir) error = %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("items dir mode = %#o, want %#o", got, 0o700)
	}
}
