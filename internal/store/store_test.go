package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aloglu/triage/internal/config"
	"github.com/aloglu/triage/internal/issue"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return New(config.Paths{ConfigDir: filepath.Join(dir, "config"), CacheDir: filepath.Join(dir, "cache")})
}

func TestRepoCacheMerge(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cache := RepoCache{Repo: "a/b", Issues: []issue.Issue{
		{Number: 1, Title: "one", UpdatedAt: t0},
		{Number: 2, Title: "two", UpdatedAt: t0, LocalID: "L1"},
	}}
	cache.Merge([]issue.Issue{
		{Number: 2, Title: "two edited", UpdatedAt: t0.Add(time.Hour)},
		{Number: 3, Title: "three", UpdatedAt: t0.Add(2 * time.Hour)},
	})
	if len(cache.Issues) != 3 || cache.Issues[0].Number != 3 {
		t.Fatalf("issues = %+v", cache.Issues)
	}
	if got := cache.Issues[1]; got.Title != "two edited" || got.LocalID != "L1" {
		t.Fatalf("merged issue = %+v", got)
	}
	if !cache.Watermark.Equal(t0.Add(2 * time.Hour)) {
		t.Fatalf("watermark = %v", cache.Watermark)
	}
}

func TestRepoCacheRoundTrip(t *testing.T) {
	s := newTestStore(t)
	empty, err := s.LoadRepo("a/b")
	if err != nil || len(empty.Issues) != 0 {
		t.Fatalf("empty load = %+v, %v", empty, err)
	}
	cache := RepoCache{Repo: "a/b", ETag: `"x"`, Issues: []issue.Issue{{Repo: "a/b", Number: 1, Title: "hi"}}}
	if err := s.SaveRepo(cache); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadRepo("a/b")
	if err != nil || got.ETag != `"x"` || len(got.Issues) != 1 {
		t.Fatalf("load = %+v, %v", got, err)
	}
	if err := s.ForgetRepo("a/b"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LoadRepo("a/b"); len(got.Issues) != 0 {
		t.Fatal("ForgetRepo left data behind")
	}
}

func TestOutboxClaimLifecycle(t *testing.T) {
	s := newTestStore(t)
	title := "new"
	first, err := s.Enqueue(Op{Kind: OpCreate, Repo: "a/b", LocalID: "L1", Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := s.Enqueue(Op{Kind: OpComment, Repo: "a/b", LocalID: "L1", Comment: "hi"})

	ops, err := s.Pending()
	if err != nil || len(ops) != 2 || ops[0].ID != first.ID || ops[1].ID != second.ID {
		t.Fatalf("pending = %+v, %v", ops, err)
	}

	if ok, err := s.Claim(first.ID); !ok || err != nil {
		t.Fatalf("first claim = %v, %v", ok, err)
	}
	if ok, _ := s.Claim(first.ID); ok {
		t.Fatal("an op must not be claimable twice")
	}
	if ok, _ := s.Update(first); ok {
		t.Fatal("Update must not touch a claimed op")
	}
	ops, _ = s.Pending()
	if !ops[0].Sending() || ops[1].Sending() {
		t.Fatalf("claimed flags wrong: %v %v", ops[0].Sending(), ops[1].Sending())
	}

	first.Error = "boom"
	if err := s.Release(first); err != nil {
		t.Fatal(err)
	}
	ops, _ = s.Pending()
	if ops[0].Error != "boom" || ops[0].Sending() {
		t.Fatalf("released op = %+v", ops[0])
	}

	if ok, err := s.Discard(first.ID); !ok || err != nil {
		t.Fatalf("discard = %v, %v", ok, err)
	}
	ops, _ = s.Pending()
	if len(ops) != 1 || ops[0].ID != second.ID {
		t.Fatalf("after discard = %+v", ops)
	}
}

func TestStaleClaimIsReleased(t *testing.T) {
	s := newTestStore(t)
	op, _ := s.Enqueue(Op{Kind: OpComment, Repo: "a/b", Number: 1, Comment: "hi"})
	if ok, _ := s.Claim(op.ID); !ok {
		t.Fatal("claim failed")
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(s.opPath(op.ID)+claimedSuffix, old, old); err != nil {
		t.Fatal(err)
	}
	ops, _ := s.Pending()
	if len(ops) != 1 || ops[0].Sending() {
		t.Fatalf("stale claim not released: %+v", ops)
	}
	if ok, _ := s.Claim(op.ID); !ok {
		t.Fatal("released op should be claimable")
	}
}
