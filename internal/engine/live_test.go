package engine

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/aloglu/triage/internal/config"
	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/issue"
	"github.com/aloglu/triage/internal/store"
)

// TestLive runs the write path against real GitHub. It only runs when
// TRIAGE_LIVE_REPO names a throwaway repository you can write to:
//
//	TRIAGE_LIVE_REPO=you/sandbox go test ./internal/engine -run Live -v
func TestLive(t *testing.T) {
	repo := os.Getenv("TRIAGE_LIVE_REPO")
	if repo == "" {
		t.Skip("set TRIAGE_LIVE_REPO to a throwaway repo to run")
	}
	client, err := gh.New()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dir := t.TempDir()
	e := New(client, store.New(config.Paths{ConfigDir: filepath.Join(dir, "c"), CacheDir: filepath.Join(dir, "k")}), issue.DefaultConvention())
	me := e.Me(ctx)

	snap := func() Snapshot {
		t.Helper()
		s, err := e.Snapshot([]string{repo})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	flush := func() FlushResult {
		t.Helper()
		r, err := e.Flush(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if r.Err != nil {
			t.Fatalf("flush error: %v", r.Err)
		}
		return r
	}
	remote := func(number int) gh.Issue {
		t.Helper()
		i, err := client.GetIssue(ctx, repo, number)
		if err != nil {
			t.Fatal(err)
		}
		return i
	}
	labels := func(i gh.Issue) []string {
		var out []string
		for _, l := range i.Labels {
			out = append(out, l.Name)
		}
		sort.Strings(out)
		return out
	}

	ops, err := e.Create(Draft{Repo: repo, Title: "Live test", Body: "original", Type: issue.TypeFeature, Status: issue.StatusIdea})
	if err != nil {
		t.Fatal(err)
	}
	number := flush().Created[ops[0].LocalID]
	t.Logf("created #%d", number)
	if got := labels(remote(number)); !reflect.DeepEqual(got, []string{"enhancement", "idea"}) {
		t.Fatalf("labels after create = %v", got)
	}

	find := func() issue.Issue {
		t.Helper()
		for _, i := range snap().Issues {
			if i.Number == number {
				return i
			}
		}
		t.Fatalf("#%d not in snapshot", number)
		return issue.Issue{}
	}

	// Status with a space in the label name, then type change.
	if _, err := e.SetStatus(find(), issue.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	if _, err := e.SetType(find(), issue.TypeBug); err != nil {
		t.Fatal(err)
	}
	flush()
	if got := labels(remote(number)); !reflect.DeepEqual(got, []string{"bug", "in progress"}) {
		t.Fatalf("labels after status+type = %v", got)
	}

	// Remove the spaced label by moving to blocked.
	before := find()
	statusOps, _ := e.SetStatus(before, issue.StatusBlocked)
	flush()
	if got := labels(remote(number)); !reflect.DeepEqual(got, []string{"blocked", "bug"}) {
		t.Fatalf("labels after blocked = %v", got)
	}

	// Undo after it was sent.
	if _, err := e.Restore(ids(statusOps), before, find()); err != nil {
		t.Fatal(err)
	}
	flush()
	if got := labels(remote(number)); !reflect.DeepEqual(got, []string{"bug", "in progress"}) {
		t.Fatalf("labels after undo = %v", got)
	}

	// Won't do, then reopen.
	if _, err := e.SetStatus(find(), issue.StatusWontDo); err != nil {
		t.Fatal(err)
	}
	flush()
	if got := remote(number); got.State != "closed" || got.StateReason != "not_planned" || len(got.Labels) != 1 {
		t.Fatalf("after won't do: %s/%s %v", got.State, got.StateReason, labels(got))
	}
	if _, err := e.SetStatus(find(), issue.StatusTodo); err != nil {
		t.Fatal(err)
	}
	flush()
	if got := remote(number); got.State != "open" {
		t.Fatalf("reopen: state %s", got.State)
	}

	// Assign and unassign.
	if _, err := e.Assign(find(), []string{me}, nil); err != nil {
		t.Fatal(err)
	}
	flush()
	if got := remote(number); len(got.Assignees) != 1 || got.Assignees[0].Login != me {
		t.Fatalf("assignees = %+v", got.Assignees)
	}
	if _, err := e.Assign(find(), nil, []string{me}); err != nil {
		t.Fatal(err)
	}
	flush()
	if got := remote(number); len(got.Assignees) != 0 {
		t.Fatalf("assignees after unassign = %+v", got.Assignees)
	}

	// Comment, then read comments back.
	if _, err := e.Comment(find(), "live comment"); err != nil {
		t.Fatal(err)
	}
	flush()
	if _, err := e.Refresh(ctx, repo, false); err != nil {
		t.Fatal(err)
	}
	comments, err := e.Comments(ctx, find())
	if err != nil || len(comments) != 1 || comments[0].Body != "live comment" {
		t.Fatalf("comments = %+v, %v", comments, err)
	}

	// Conflict: edit based on a stale body while GitHub changed it.
	stale := find()
	theirs := "changed on github"
	if _, err := client.UpdateIssue(ctx, repo, number, gh.IssuePatch{Body: &theirs}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Edit(stale, "Live test edited", "mine"); err != nil {
		t.Fatal(err)
	}
	r, err := e.Flush(ctx)
	if err != nil || r.Held != 1 {
		t.Fatalf("expected a held conflict, got %+v, %v", r, err)
	}
	if got := remote(number); got.Body != theirs || got.Title != "Live test" {
		t.Fatalf("conflicting edit was sent: %q / %q", got.Title, got.Body)
	}
	held := snap().Pending[stale.Key()]
	if len(held) != 1 || held[0].Conflict == nil {
		t.Fatalf("held = %+v", held)
	}
	if err := e.KeepMine(held[0]); err != nil {
		t.Fatal(err)
	}
	flush()
	if got := remote(number); got.Body != "mine" || got.Title != "Live test edited" {
		t.Fatalf("after keep mine: %q / %q", got.Title, got.Body)
	}

	// Incremental refresh: once GitHub's listing settles, an unchanged repo
	// comes back 304 Not Modified.
	var notModified bool
	for attempt := 0; attempt < 10 && !notModified; attempt++ {
		time.Sleep(2 * time.Second)
		cache, _ := e.store.LoadRepo(repo)
		page, err := client.ListIssues(ctx, repo, cache.Watermark.Add(-refreshOverlap), cache.ETag)
		if err != nil {
			t.Fatal(err)
		}
		notModified = page.NotModified
		if _, err := e.Refresh(ctx, repo, false); err != nil {
			t.Fatal(err)
		}
	}
	if !notModified {
		t.Error("an unchanged repo never came back 304")
	}
}
