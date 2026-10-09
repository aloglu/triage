package engine

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aloglu/triage/internal/config"
	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/ghtest"
	"github.com/aloglu/triage/internal/issue"
	"github.com/aloglu/triage/internal/store"
)

const repo = "aloglu/bookshelf"

func setup(t *testing.T) (*Engine, *ghtest.Server) {
	t.Helper()
	server := ghtest.New(t)
	server.AddRepo(repo)
	dir := t.TempDir()
	st := store.New(config.Paths{ConfigDir: filepath.Join(dir, "config"), CacheDir: filepath.Join(dir, "cache")})
	return New(server.Client(), st, issue.DefaultConvention()), server
}

func snapshot(t *testing.T, e *Engine) Snapshot {
	t.Helper()
	snap, err := e.Snapshot([]string{repo})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func find(t *testing.T, snap Snapshot, number int) issue.Issue {
	t.Helper()
	for _, i := range snap.Issues {
		if i.Number == number {
			return i
		}
	}
	t.Fatalf("issue #%d not in snapshot", number)
	return issue.Issue{}
}

func flush(t *testing.T, e *Engine) FlushResult {
	t.Helper()
	result, err := e.Flush(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestRefreshIsIncrementalAndUsesETag(t *testing.T) {
	e, server := setup(t)
	ctx := context.Background()
	for i := 0; i < 150; i++ {
		server.AddIssue(repo, "issue", "")
	}

	result, err := e.Refresh(ctx, repo, false)
	if err != nil || result.Changed != 150 {
		t.Fatalf("first refresh = %+v, %v", result, err)
	}
	if got := len(snapshot(t, e).Issues); got != 150 {
		t.Fatalf("cached %d issues", got)
	}

	// The first follow-up moves to a since= URL; after that GitHub answers
	// 304 Not Modified and nothing else is requested.
	for range 2 {
		before := len(server.Requests)
		result, err = e.Refresh(ctx, repo, false)
		if err != nil || result.Changed != 0 {
			t.Fatalf("unchanged refresh = %+v, %v", result, err)
		}
		if got := server.Requests[before:]; len(got) != 1 || !strings.Contains(got[0], "/issues?") {
			t.Fatalf("unchanged refresh should make one listing request, made %v", got)
		}
	}

	server.EditIssue(repo, 7, func(i *gh.Issue) { i.Title = "renamed" })
	result, err = e.Refresh(ctx, repo, false)
	if err != nil || result.Changed != 1 {
		t.Fatalf("incremental refresh = %+v, %v", result, err)
	}
	if got := find(t, snapshot(t, e), 7).Title; got != "renamed" {
		t.Fatalf("title = %q", got)
	}
}

func TestCreateWithStatusAndType(t *testing.T) {
	e, server := setup(t)
	ops, err := e.Create(Draft{Repo: repo, Title: " Fix top bar ", Body: "on mobile", Type: issue.TypeBug, Status: issue.StatusInProgress})
	if err != nil || len(ops) != 1 {
		t.Fatalf("Create = %v, %v", ops, err)
	}

	snap := snapshot(t, e)
	if len(snap.Issues) != 1 || snap.Issues[0].Number != 0 || snap.Issues[0].Title != "Fix top bar" {
		t.Fatalf("optimistic issue = %+v", snap.Issues)
	}
	if total, _ := snap.PendingCount(); total != 1 {
		t.Fatalf("pending = %d", total)
	}

	result := flush(t, e)
	if result.Sent != 1 || result.Created[ops[0].LocalID] != 1 {
		t.Fatalf("flush = %+v", result)
	}
	if got := server.LabelNames(repo, 1); !reflect.DeepEqual(got, []string{"bug", "in progress"}) {
		t.Fatalf("labels = %v", got)
	}
	label, ok := server.RepoLabel(repo, "in progress")
	if !ok || label.Color != "fbca04" || label.Description == "" {
		t.Fatalf("status label should be created with a color and description: %+v", label)
	}
	snap = snapshot(t, e)
	if len(snap.Issues) != 1 || snap.Issues[0].Number != 1 || len(snap.Pending) != 0 {
		t.Fatalf("after flush = %+v pending %v", snap.Issues, snap.Pending)
	}
}

func TestCreateDoneQueuesClose(t *testing.T) {
	e, server := setup(t)
	if _, err := e.Create(Draft{Repo: repo, Title: "Old", Status: issue.StatusWontDo}); err != nil {
		t.Fatal(err)
	}
	if got := e.conv.StatusOf(snapshot(t, e).Issues[0]); got != issue.StatusWontDo {
		t.Fatalf("optimistic status = %v", got)
	}
	if result := flush(t, e); result.Sent != 2 {
		t.Fatalf("flush = %+v", result)
	}
	if got := server.Issue(repo, 1); got.State != "closed" || got.StateReason != "not_planned" {
		t.Fatalf("issue = %+v", got)
	}
}

func TestOfflineChangesQueueInOrder(t *testing.T) {
	e, server := setup(t)
	server.AddIssue(repo, "Existing", "", "idea")
	if _, err := e.Refresh(context.Background(), repo, false); err != nil {
		t.Fatal(err)
	}
	server.SetOffline(true)

	current := find(t, snapshot(t, e), 1)
	if _, err := e.SetStatus(current, issue.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	ops, _ := e.Create(Draft{Repo: repo, Title: "Made offline"})
	created := snapshot(t, e).Issues[0]
	if created.LocalID != ops[0].LocalID {
		t.Fatalf("expected local issue first, got %+v", created)
	}
	if _, err := e.Comment(created, "first comment"); err != nil {
		t.Fatal(err)
	}

	result := flush(t, e)
	if !result.Offline || result.Sent != 0 {
		t.Fatalf("offline flush = %+v", result)
	}
	if got := e.conv.StatusOf(find(t, snapshot(t, e), 1)); got != issue.StatusInProgress {
		t.Fatalf("optimistic status = %v", got)
	}

	server.SetOffline(false)
	result = flush(t, e)
	if result.Sent != 3 || result.Offline {
		t.Fatalf("online flush = %+v", result)
	}
	if got := server.LabelNames(repo, 1); !reflect.DeepEqual(got, []string{"in progress"}) {
		t.Fatalf("labels = %v", got)
	}
	if comments := server.Comments(repo, 2); len(comments) != 1 || comments[0].Body != "first comment" {
		t.Fatalf("comment on new issue = %+v", comments)
	}
}

func TestEditConflict(t *testing.T) {
	e, server := setup(t)
	server.AddIssue(repo, "Title", "original body")
	if _, err := e.Refresh(context.Background(), repo, false); err != nil {
		t.Fatal(err)
	}
	current := find(t, snapshot(t, e), 1)

	server.EditIssue(repo, 1, func(i *gh.Issue) { i.Body = "their body" })
	// A label change elsewhere must not count as a conflict.
	if _, err := e.Edit(current, "My title", "my body"); err != nil {
		t.Fatal(err)
	}

	result := flush(t, e)
	if result.Held != 1 || result.Sent != 0 {
		t.Fatalf("flush = %+v", result)
	}
	snap := snapshot(t, e)
	ops := snap.Pending[current.Key()]
	if len(ops) != 1 || ops[0].Conflict == nil || *ops[0].Conflict.RemoteBody != "their body" {
		t.Fatalf("conflict = %+v", ops)
	}
	if got := server.Issue(repo, 1); got.Title != "Title" {
		t.Fatalf("conflicting edit must not be sent, title = %q", got.Title)
	}

	if err := e.KeepMine(ops[0]); err != nil {
		t.Fatal(err)
	}
	if result := flush(t, e); result.Sent != 1 {
		t.Fatalf("flush after keep mine = %+v", result)
	}
	if got := server.Issue(repo, 1); got.Title != "My title" || got.Body != "my body" {
		t.Fatalf("issue = %+v", got)
	}
}

func TestEditWithoutConflictAndFolding(t *testing.T) {
	e, server := setup(t)
	server.AddIssue(repo, "Title", "body")
	if _, err := e.Refresh(context.Background(), repo, false); err != nil {
		t.Fatal(err)
	}
	server.SetOffline(true)
	current := find(t, snapshot(t, e), 1)
	if _, err := e.Edit(current, "Second", "body"); err != nil {
		t.Fatal(err)
	}
	current = find(t, snapshot(t, e), 1)
	if _, err := e.Edit(current, "Third", "body"); err != nil {
		t.Fatal(err)
	}
	if total, _ := snapshot(t, e).PendingCount(); total != 1 {
		t.Fatalf("edits should fold into one op, got %d", total)
	}
	server.SetOffline(false)
	if result := flush(t, e); result.Sent != 1 || result.Held != 0 {
		t.Fatalf("flush = %+v", result)
	}
	if got := server.Issue(repo, 1).Title; got != "Third" {
		t.Fatalf("title = %q", got)
	}
}

func TestPermissionErrorHoldsOnlyThatIssue(t *testing.T) {
	e, server := setup(t)
	server.AddIssue(repo, "One", "")
	server.AddRepo("aloglu/other")
	server.AddIssue("aloglu/other", "Two", "")
	ctx := context.Background()
	repos := []string{repo, "aloglu/other"}
	if _, err := e.RefreshAll(ctx, repos, false); err != nil {
		t.Fatal(err)
	}
	server.SetReadOnly(repo, true)

	snap, _ := e.Snapshot(repos)
	for _, i := range snap.Issues {
		if _, err := e.SetStatus(i, issue.StatusBlocked); err != nil {
			t.Fatal(err)
		}
		if _, err := e.Comment(i, "note"); err != nil {
			t.Fatal(err)
		}
	}
	result := flush(t, e)
	if result.Sent != 2 || result.Held != 1 {
		t.Fatalf("flush = %+v", result)
	}
	snap, _ = e.Snapshot(repos)
	if _, held := snap.PendingCount(); held != 1 {
		t.Fatalf("held = %d", held)
	}
	for _, op := range snap.Pending[repo+"#1"] {
		if op.Kind == store.OpLabels && !strings.Contains(op.Error, "permission") {
			t.Fatalf("error = %q", op.Error)
		}
	}
}

func TestRestoreUndoesSentAndUnsentChanges(t *testing.T) {
	e, server := setup(t)
	server.AddIssue(repo, "Title", "", "idea")
	if _, err := e.Refresh(context.Background(), repo, false); err != nil {
		t.Fatal(err)
	}
	before := find(t, snapshot(t, e), 1)

	ops, _ := e.SetStatus(before, issue.StatusDone)
	if _, err := e.Restore(ids(ops), before, find(t, snapshot(t, e), 1)); err != nil {
		t.Fatal(err)
	}
	if total, _ := snapshot(t, e).PendingCount(); total != 0 {
		t.Fatalf("unsent change should simply be discarded, pending %d", total)
	}

	ops, _ = e.SetStatus(before, issue.StatusDone)
	flush(t, e)
	if server.Issue(repo, 1).State != "closed" {
		t.Fatal("expected issue closed")
	}
	if _, err := e.Restore(ids(ops), before, find(t, snapshot(t, e), 1)); err != nil {
		t.Fatal(err)
	}
	flush(t, e)
	got := server.Issue(repo, 1)
	if got.State != "open" || !reflect.DeepEqual(server.LabelNames(repo, 1), []string{"idea"}) {
		t.Fatalf("restored issue = %+v labels %v", got, server.LabelNames(repo, 1))
	}
}

func TestComments(t *testing.T) {
	e, server := setup(t)
	server.AddIssue(repo, "Title", "")
	ctx := context.Background()
	if _, err := e.Refresh(ctx, repo, false); err != nil {
		t.Fatal(err)
	}
	current := find(t, snapshot(t, e), 1)
	if _, err := e.Comment(current, "hello"); err != nil {
		t.Fatal(err)
	}
	flush(t, e)
	if _, err := e.Refresh(ctx, repo, false); err != nil {
		t.Fatal(err)
	}
	current = find(t, snapshot(t, e), 1)
	comments, err := e.Comments(ctx, current)
	if err != nil || len(comments) != 1 || comments[0].Body != "hello" || comments[0].Author != "octo" {
		t.Fatalf("comments = %+v, %v", comments, err)
	}
	before := len(server.Requests)
	if _, err := e.Comments(ctx, current); err != nil || len(server.Requests) != before {
		t.Fatalf("second read should come from cache (%d new requests)", len(server.Requests)-before)
	}
}

func ids(ops []store.Op) []string {
	var out []string
	for _, op := range ops {
		out = append(out, op.ID)
	}
	return out
}

func TestRefreshCatchesIssuesThatAppearLate(t *testing.T) {
	e, server := setup(t)
	ctx := context.Background()
	late := server.AddIssue(repo, "late", "")
	server.AddIssue(repo, "newer", "")
	// Simulate GitHub's listing lagging: hide the older issue from the
	// first refresh only.
	server.HideFromListing(repo, late.Number, true)
	if _, err := e.Refresh(ctx, repo, false); err != nil {
		t.Fatal(err)
	}
	server.HideFromListing(repo, late.Number, false)
	if _, err := e.Refresh(ctx, repo, false); err != nil {
		t.Fatal(err)
	}
	if got := len(snapshot(t, e).Issues); got != 2 {
		t.Fatalf("an issue that showed up late in the listing was missed: have %d issues", got)
	}
}

func TestFullRefreshDropsDeletedIssues(t *testing.T) {
	e, server := setup(t)
	ctx := context.Background()
	// Created a minute apart: no time-based guess can tell them apart.
	server.AddIssue(repo, "doomed", "")
	server.AddIssue(repo, "survivor", "")
	if _, err := e.Refresh(ctx, repo, false); err != nil {
		t.Fatal(err)
	}
	server.DeleteIssue(repo, 1)

	// An incremental refresh can't see deletions.
	if _, err := e.Refresh(ctx, repo, false); err != nil {
		t.Fatal(err)
	}
	if got := len(snapshot(t, e).Issues); got != 2 {
		t.Fatalf("incremental refresh changed the issue count to %d", got)
	}

	result, err := e.Refresh(ctx, repo, true)
	if err != nil || result.Removed != 1 {
		t.Fatalf("full refresh = %+v, %v", result, err)
	}
	snap := snapshot(t, e)
	if len(snap.Issues) != 1 || snap.Issues[0].Title != "survivor" {
		t.Fatalf("issues after full refresh = %+v", snap.Issues)
	}
}

func TestFullRefreshRechecksMissingIssuesWhenListingUnchanged(t *testing.T) {
	e, server := setup(t)
	ctx := context.Background()
	server.AddIssue(repo, "doomed", "")
	server.AddIssue(repo, "survivor", "")
	if _, err := e.Refresh(ctx, repo, true); err != nil {
		t.Fatal(err)
	}
	server.DeleteIssue(repo, 1)
	if _, err := e.Refresh(ctx, repo, true); err != nil {
		t.Fatal(err)
	}
	// Simulate a cache that still holds the deleted issue while the stored
	// listing is current (e.g. the lookup failed last time).
	cache, _ := e.store.LoadRepo(repo)
	cache.Issues = append(cache.Issues, issue.Issue{Repo: repo, Number: 1, Title: "doomed", State: issue.StateOpen})
	if err := e.store.SaveRepo(cache); err != nil {
		t.Fatal(err)
	}
	result, err := e.Refresh(ctx, repo, true)
	if err != nil || result.Removed != 1 {
		t.Fatalf("refresh with an unchanged listing = %+v, %v", result, err)
	}
}

func TestFullRefreshKeepsIssuesTheListingHasNotCaughtUpWith(t *testing.T) {
	e, server := setup(t)
	ctx := context.Background()
	server.AddIssue(repo, "old", "")
	server.Advance(time.Hour)
	fresh := server.AddIssue(repo, "just created", "")
	if _, err := e.Refresh(ctx, repo, true); err != nil {
		t.Fatal(err)
	}
	server.HideFromListing(repo, fresh.Number, true)
	server.EditIssue(repo, 1, func(*gh.Issue) {}) // change something so the listing isn't a 304
	result, err := e.Refresh(ctx, repo, true)
	if err != nil || result.Removed != 0 || len(snapshot(t, e).Issues) != 2 {
		t.Fatalf("a recently changed issue missing from the listing was dropped: %+v, %v", result, err)
	}
}

func TestChangesToDeletedIssueAreDropped(t *testing.T) {
	e, server := setup(t)
	server.AddIssue(repo, "Title", "")
	if _, err := e.Refresh(context.Background(), repo, false); err != nil {
		t.Fatal(err)
	}
	current := find(t, snapshot(t, e), 1)
	server.DeleteIssue(repo, 1)
	if _, err := e.SetStatus(current, issue.StatusBlocked); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Comment(current, "hi"); err != nil {
		t.Fatal(err)
	}
	result := flush(t, e)
	if result.Dropped != 2 || result.Held != 0 {
		t.Fatalf("flush = %+v", result)
	}
	snap := snapshot(t, e)
	if total, _ := snap.PendingCount(); total != 0 || len(snap.Issues) != 0 {
		t.Fatalf("after flush: %d pending, issues %+v", total, snap.Issues)
	}
}
