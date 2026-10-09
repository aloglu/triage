// Package engine keeps the local cache in step with GitHub: it fetches
// changes, queues the user's edits in the outbox, and sends them.
package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/issue"
	"github.com/aloglu/triage/internal/store"
)

// Engine coordinates the GitHub client, the cache, and the outbox. Its
// methods are safe to call from multiple goroutines.
type Engine struct {
	gh    *gh.Client
	store *store.Store
	conv  issue.Convention

	// mu serializes cache writes and flushes within this process.
	mu sync.Mutex
}

func New(client *gh.Client, st *store.Store, conv issue.Convention) *Engine {
	return &Engine{gh: client, store: st, conv: conv}
}

func (e *Engine) Convention() issue.Convention { return e.conv }

func (e *Engine) Store() *store.Store { return e.store }

// Online reports whether the engine has a GitHub client. Without one it can
// still read the cache and queue changes.
func (e *Engine) Online() bool { return e.gh != nil }

// Me returns the authenticated user's login, from GitHub when possible and
// from the cache otherwise.
func (e *Engine) Me(ctx context.Context) string {
	if e.gh != nil {
		if login, err := e.gh.Viewer(ctx); err == nil {
			if meta := e.store.LoadMeta(); meta.Login != login {
				meta.Login = login
				_ = e.store.SaveMeta(meta)
			}
			return login
		}
	}
	return e.store.LoadMeta().Login
}

// CachedMe returns the last known login without contacting GitHub.
func (e *Engine) CachedMe() string { return e.store.LoadMeta().Login }

// Snapshot is the issues triage should display: the cache with every
// pending change applied on top.
type Snapshot struct {
	Issues []issue.Issue
	// Pending maps issue keys to the changes still waiting to be sent.
	Pending map[string][]store.Op
	// Labels lists every known label per repo.
	Labels map[string][]gh.Label
	// FetchedAt is the time of each repo's last successful refresh.
	FetchedAt map[string]time.Time
}

// PendingCount returns how many ops are queued, and how many of those need
// the user's attention.
func (s Snapshot) PendingCount() (total, held int) {
	for _, ops := range s.Pending {
		for _, op := range ops {
			total++
			if op.Held() {
				held++
			}
		}
	}
	return total, held
}

// LastFetched returns the oldest refresh time across repos, or zero if a
// repo has never been fetched.
func (s Snapshot) LastFetched() time.Time {
	var oldest time.Time
	for _, at := range s.FetchedAt {
		if at.IsZero() {
			return time.Time{}
		}
		if oldest.IsZero() || at.Before(oldest) {
			oldest = at
		}
	}
	return oldest
}

// Snapshot loads the cached issues of repos with pending changes applied.
func (e *Engine) Snapshot(repos []string) (Snapshot, error) {
	snap := Snapshot{Pending: map[string][]store.Op{}, Labels: map[string][]gh.Label{}, FetchedAt: map[string]time.Time{}}
	tracked := map[string]bool{}
	for _, repo := range repos {
		cache, err := e.store.LoadRepo(repo)
		if err != nil {
			return snap, err
		}
		tracked[strings.ToLower(repo)] = true
		snap.Issues = append(snap.Issues, cache.Issues...)
		snap.Labels[repo] = cache.Labels
		snap.FetchedAt[repo] = cache.FetchedAt
	}
	ops, err := e.store.Pending()
	if err != nil {
		return snap, err
	}
	for _, op := range ops {
		if !tracked[strings.ToLower(op.Repo)] {
			continue
		}
		snap.Issues = applyOp(snap.Issues, op, e.CachedMe())
		key := targetKey(snap.Issues, op)
		snap.Pending[key] = append(snap.Pending[key], op)
	}
	return snap, nil
}

// targetKey returns the key of the issue op applies to, following a local
// ID to the real issue once it has been created.
func targetKey(issues []issue.Issue, op store.Op) string {
	if at := findTarget(issues, op); at >= 0 {
		return issues[at].Key()
	}
	return op.Target()
}

func findTarget(issues []issue.Issue, op store.Op) int {
	for i, candidate := range issues {
		if !strings.EqualFold(candidate.Repo, op.Repo) {
			continue
		}
		if op.Number != 0 && candidate.Number == op.Number {
			return i
		}
		if op.Number == 0 && op.LocalID != "" && candidate.LocalID == op.LocalID {
			return i
		}
	}
	return -1
}

// applyOp returns issues as they will look once op reaches GitHub.
func applyOp(issues []issue.Issue, op store.Op, me string) []issue.Issue {
	if op.Kind == store.OpCreate {
		if findTarget(issues, op) >= 0 {
			return issues
		}
		created := issue.Issue{
			Repo:      op.Repo,
			LocalID:   op.LocalID,
			Title:     deref(op.Title),
			Body:      deref(op.Body),
			State:     issue.StateOpen,
			Labels:    op.Labels,
			Assignees: op.Assignees,
			Author:    me,
			CreatedAt: op.CreatedAt,
			UpdatedAt: op.CreatedAt,
		}
		return append([]issue.Issue{created}, issues...)
	}
	at := findTarget(issues, op)
	if at < 0 {
		return issues
	}
	target := issues[at]
	switch op.Kind {
	case store.OpEdit:
		if op.Conflict != nil {
			break
		}
		if op.Title != nil {
			target.Title = *op.Title
		}
		if op.Body != nil {
			target.Body = *op.Body
		}
	case store.OpLabels:
		target.Labels = issue.WithLabels(target.Labels, op.Add, op.Remove)
	case store.OpState:
		target = issue.Change{State: op.State, StateReason: op.StateReason}.Apply(target)
	case store.OpAssign:
		target.Assignees = issue.WithLabels(target.Assignees, op.Add, op.Remove)
	case store.OpComment:
		target.Comments++
	}
	if op.CreatedAt.After(target.UpdatedAt) {
		target.UpdatedAt = op.CreatedAt
	}
	issues[at] = target
	return issues
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// refreshOverlap is how far each incremental refresh reaches back before
// the newest issue already seen.
const refreshOverlap = 10 * time.Minute

// fullCheckInterval is how often a refresh also fetches the complete issue
// list to notice issues that were deleted or moved to another repo, which
// incremental listings can't show.
const fullCheckInterval = 30 * time.Minute

// RefreshResult describes what a refresh found.
type RefreshResult struct {
	Repo string
	// Changed counts new, updated, and removed issues.
	Changed int
	// Removed counts issues that no longer exist on GitHub.
	Removed int
}

// Refresh fetches issues changed since the last refresh of repo. With full
// set, or when the last full check is old, it fetches the complete list and
// also drops issues that were deleted or transferred away.
func (e *Engine) Refresh(ctx context.Context, repo string, full bool) (RefreshResult, error) {
	result := RefreshResult{Repo: repo}
	if e.gh == nil {
		return result, &gh.Error{Kind: gh.ErrAuth, Message: "not logged in to GitHub"}
	}
	e.mu.Lock()
	cache, err := e.store.LoadRepo(repo)
	e.mu.Unlock()
	if err != nil {
		return result, err
	}
	full = full || cache.FullCheckedAt.IsZero() || time.Since(cache.FullCheckedAt) > fullCheckInterval

	var page gh.IssuePage
	if full {
		page, err = e.gh.ListIssues(ctx, repo, time.Time{}, cache.FullETag)
	} else {
		// GitHub's issue listing is eventually consistent: an issue can show
		// up a few seconds after a newer one. Asking for a window that
		// overlaps the last fetch catches such stragglers; the ETag still
		// makes unchanged repos cheap because the URL only changes when the
		// watermark moves.
		since := cache.Watermark
		if !since.IsZero() {
			since = since.Add(-refreshOverlap)
		}
		page, err = e.gh.ListIssues(ctx, repo, since, cache.ETag)
	}
	if err != nil {
		return result, err
	}
	fetched := make([]issue.Issue, 0, len(page.Issues))
	for _, src := range page.Issues {
		fresh := issue.FromGitHub(repo, src)
		if !containsIssue(cache.Issues, fresh) {
			result.Changed++
		}
		fetched = append(fetched, fresh)
	}
	var gone map[int]bool
	var listed []int
	if full {
		listed = cache.FullListed
		if !page.NotModified {
			listed = make([]int, 0, len(fetched))
			for _, i := range fetched {
				listed = append(listed, i.Number)
			}
		}
		var confirmed []issue.Issue
		gone, confirmed = e.checkMissing(ctx, repo, cache.Issues, listed)
		fetched = append(fetched, confirmed...)
	}
	// Label definitions rarely change; refetch them only alongside issue
	// changes, which is when new labels tend to appear.
	var labels []gh.Label
	if result.Changed > 0 || len(gone) > 0 || len(cache.Labels) == 0 {
		labels, err = e.gh.ListLabels(ctx, repo)
		if err != nil {
			return result, err
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	// Reload in case a write landed while we were fetching.
	cache, err = e.store.LoadRepo(repo)
	if err != nil {
		return result, err
	}
	if full {
		result.Removed = e.drop(&cache, gone)
		result.Changed += result.Removed
		cache.FullCheckedAt = time.Now().UTC()
		cache.FullListed = listed
		if !page.NotModified {
			cache.FullETag = page.ETag
		}
	} else if !page.NotModified {
		cache.ETag = page.ETag
	}
	cache.Merge(fetched)
	if labels != nil {
		cache.Labels = labels
	}
	cache.FetchedAt = time.Now().UTC()
	return result, e.store.SaveRepo(cache)
}

// checkMissing asks GitHub about each cached issue missing from a complete
// listing. Issues that were deleted or transferred to another repo are
// returned in gone. Issues that still exist (the listing can lag behind
// recent changes) are returned in confirmed so the cache stays current.
// Issues that can't be checked right now are left alone.
func (e *Engine) checkMissing(ctx context.Context, repo string, cached []issue.Issue, listed []int) (gone map[int]bool, confirmed []issue.Issue) {
	present := make(map[int]bool, len(listed))
	for _, n := range listed {
		present[n] = true
	}
	gone = map[int]bool{}
	for _, i := range cached {
		if present[i.Number] {
			continue
		}
		current, err := e.gh.GetIssue(ctx, repo, i.Number)
		switch {
		case gh.IsNotFound(err):
			gone[i.Number] = true
		case err != nil:
			// Offline or similar: check again on the next full refresh.
		case !belongsTo(current, repo):
			// GitHub follows transferred issues to their new repo.
			gone[i.Number] = true
		default:
			confirmed = append(confirmed, issue.FromGitHub(repo, current))
		}
	}
	return gone, confirmed
}

func belongsTo(i gh.Issue, repo string) bool {
	if i.HTMLURL == "" {
		return true
	}
	return strings.Contains(strings.ToLower(i.HTMLURL), "/"+strings.ToLower(repo)+"/issues/")
}

// drop removes the given issue numbers from the cache.
func (e *Engine) drop(cache *store.RepoCache, numbers map[int]bool) int {
	if len(numbers) == 0 {
		return 0
	}
	kept := cache.Issues[:0]
	removed := 0
	for _, i := range cache.Issues {
		if numbers[i.Number] {
			removed++
			e.store.ForgetComments(cache.Repo, i.Number)
			continue
		}
		kept = append(kept, i)
	}
	cache.Issues = kept
	return removed
}

func containsIssue(issues []issue.Issue, fresh issue.Issue) bool {
	for _, existing := range issues {
		if existing.Number == fresh.Number {
			return existing.UpdatedAt.Equal(fresh.UpdatedAt)
		}
	}
	return false
}

// RefreshAll refreshes repos concurrently and returns the first error.
// full is passed on to Refresh.
func (e *Engine) RefreshAll(ctx context.Context, repos []string, full bool) ([]RefreshResult, error) {
	results := make([]RefreshResult, len(repos))
	errs := make([]error, len(repos))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, repo := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i], errs[i] = e.Refresh(ctx, repo, full)
		}()
	}
	wg.Wait()
	return results, errors.Join(errs...)
}

// Submit queues a change and returns it with its ID.
func (e *Engine) Submit(op store.Op) (store.Op, error) {
	if op.Kind == store.OpCreate && op.LocalID == "" {
		op.LocalID = store.NewID()
	}
	return e.store.Enqueue(op)
}

// FlushResult summarizes a flush.
type FlushResult struct {
	Sent    int
	Held    int
	Offline bool
	// Dropped counts changes discarded because their issue was deleted.
	Dropped int
	// Created maps local IDs to the issue numbers GitHub assigned.
	Created map[string]int
	// Err is the most recent failure, for display.
	Err error
}

// errConflict signals that an edit clashes with a remote change.
type errConflict struct{ conflict store.Conflict }

func (errConflict) Error() string { return "the issue changed on GitHub since you started editing" }

// Flush sends queued changes in order. Changes to an issue wait behind any
// earlier change to the same issue that couldn't be sent. Flushing stops at
// the first network failure; the rest is retried on the next flush.
func (e *Engine) Flush(ctx context.Context) (FlushResult, error) {
	return e.flush(ctx, nil)
}

// FlushOnly sends only the ops with the given IDs (and nothing else).
func (e *Engine) FlushOnly(ctx context.Context, ids ...string) (FlushResult, error) {
	only := map[string]bool{}
	for _, id := range ids {
		only[id] = true
	}
	return e.flush(ctx, only)
}

func (e *Engine) flush(ctx context.Context, only map[string]bool) (FlushResult, error) {
	result := FlushResult{Created: map[string]int{}}
	if e.gh == nil {
		result.Offline = true
		return result, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	ops, err := e.store.Pending()
	if err != nil {
		return result, err
	}
	blocked := map[string]bool{}
	for _, queued := range ops {
		target := queued.Target()
		if queued.Number == 0 {
			if number, ok := result.Created[queued.LocalID]; ok {
				target = fmt.Sprintf("%s#%d", queued.Repo, number)
			}
		}
		skip := blocked[target] || queued.Sending() || queued.Held() || (only != nil && !only[queued.ID])
		if skip {
			blocked[target] = true
			if queued.Held() {
				result.Held++
			}
			continue
		}

		op, ok, err := e.store.ClaimOp(queued.ID)
		if err != nil {
			return result, err
		}
		if !ok {
			blocked[target] = true
			continue
		}
		if op.Kind != store.OpCreate && op.Number == 0 {
			number := result.Created[op.LocalID]
			if number == 0 {
				number = e.lookupLocalID(op.Repo, op.LocalID)
			}
			if number == 0 {
				// The issue hasn't been created yet; wait for it.
				blocked[target] = true
				if err := e.store.Release(op); err != nil {
					return result, err
				}
				continue
			}
			op.Number = number
		}

		err = e.send(ctx, &op, &result)
		var conflict errConflict
		switch {
		case err == nil:
			result.Sent++
			if err := e.store.Complete(op.ID); err != nil {
				return result, err
			}
		case gh.IsOffline(err):
			op.Attempts++
			result.Offline = true
			result.Err = err
			if err := e.store.Release(op); err != nil {
				return result, err
			}
			return result, nil
		case gh.IsGone(err):
			// The issue was deleted on GitHub; the change has nowhere to go.
			result.Dropped++
			if err := e.store.Complete(op.ID); err != nil {
				return result, err
			}
			e.removeIssue(op.Repo, op.Number)
		case errors.As(err, &conflict):
			op.Conflict = &conflict.conflict
			result.Held++
			result.Err = err
			blocked[target] = true
			if err := e.store.Release(op); err != nil {
				return result, err
			}
		case errors.Is(err, context.Canceled):
			_ = e.store.Release(op)
			return result, err
		default:
			op.Attempts++
			op.Error = gh.UserMessage(err)
			result.Held++
			result.Err = err
			blocked[target] = true
			if err := e.store.Release(op); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

func (e *Engine) lookupLocalID(repo, localID string) int {
	cache, err := e.store.LoadRepo(repo)
	if err != nil {
		return 0
	}
	for _, candidate := range cache.Issues {
		if candidate.LocalID == localID {
			return candidate.Number
		}
	}
	return 0
}

func (e *Engine) send(ctx context.Context, op *store.Op, result *FlushResult) error {
	switch op.Kind {
	case store.OpCreate:
		if err := e.ensureLabels(ctx, op.Repo, op.Labels); err != nil {
			return err
		}
		created, err := e.gh.CreateIssue(ctx, op.Repo, gh.NewIssue{Title: deref(op.Title), Body: deref(op.Body), Labels: op.Labels, Assignees: op.Assignees})
		if err != nil {
			return err
		}
		fresh := issue.FromGitHub(op.Repo, created)
		fresh.LocalID = op.LocalID
		result.Created[op.LocalID] = fresh.Number
		e.rewriteLocalID(op.Repo, op.LocalID, fresh.Number)
		return e.putIssue(fresh)

	case store.OpEdit:
		current, err := e.gh.GetIssue(ctx, op.Repo, op.Number)
		if err != nil {
			return err
		}
		var patch gh.IssuePatch
		var conflict store.Conflict
		if op.Title != nil && current.Title != *op.Title {
			if op.BaseTitle != nil && current.Title != *op.BaseTitle {
				conflict.RemoteTitle = &current.Title
			}
			patch.Title = op.Title
		}
		if op.Body != nil && normalizeBody(current.Body) != normalizeBody(*op.Body) {
			if op.BaseBody != nil && normalizeBody(current.Body) != normalizeBody(*op.BaseBody) {
				conflict.RemoteBody = &current.Body
			}
			patch.Body = op.Body
		}
		if conflict.RemoteTitle != nil || conflict.RemoteBody != nil {
			return errConflict{conflict}
		}
		if patch.Title == nil && patch.Body == nil {
			return nil
		}
		updated, err := e.gh.UpdateIssue(ctx, op.Repo, op.Number, patch)
		if err != nil {
			return err
		}
		return e.putIssue(issue.FromGitHub(op.Repo, updated))

	case store.OpLabels:
		if len(op.Add) > 0 {
			if err := e.ensureLabels(ctx, op.Repo, op.Add); err != nil {
				return err
			}
			if err := e.gh.AddLabels(ctx, op.Repo, op.Number, op.Add); err != nil {
				return err
			}
		}
		for _, label := range op.Remove {
			if err := e.gh.RemoveLabel(ctx, op.Repo, op.Number, label); err != nil {
				return err
			}
		}
		return e.patchIssue(op.Repo, op.Number, func(i *issue.Issue) {
			i.Labels = issue.WithLabels(i.Labels, op.Add, op.Remove)
		})

	case store.OpState:
		patch := gh.IssuePatch{State: &op.State}
		if op.StateReason != "" {
			patch.StateReason = &op.StateReason
		}
		updated, err := e.gh.UpdateIssue(ctx, op.Repo, op.Number, patch)
		if err != nil {
			return err
		}
		return e.putIssue(issue.FromGitHub(op.Repo, updated))

	case store.OpAssign:
		if len(op.Add) > 0 {
			if err := e.gh.AddAssignees(ctx, op.Repo, op.Number, op.Add); err != nil {
				return err
			}
		}
		if len(op.Remove) > 0 {
			if err := e.gh.RemoveAssignees(ctx, op.Repo, op.Number, op.Remove); err != nil {
				return err
			}
		}
		return e.patchIssue(op.Repo, op.Number, func(i *issue.Issue) {
			i.Assignees = issue.WithLabels(i.Assignees, op.Add, op.Remove)
		})

	case store.OpComment:
		comment, err := e.gh.CreateComment(ctx, op.Repo, op.Number, op.Comment)
		if err != nil {
			return err
		}
		if cached, ok := e.store.LoadComments(op.Repo, op.Number); ok {
			cached.Comments = append(cached.Comments, commentFromGitHub(comment))
			_ = e.store.SaveComments(op.Repo, op.Number, cached)
		}
		return e.patchIssue(op.Repo, op.Number, func(i *issue.Issue) { i.Comments++ })
	}
	return fmt.Errorf("unknown change kind %q", op.Kind)
}

func normalizeBody(body string) string {
	return strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
}

// rewriteLocalID points queued ops at a newly created issue's number.
func (e *Engine) rewriteLocalID(repo, localID string, number int) {
	ops, err := e.store.Pending()
	if err != nil {
		return
	}
	for _, op := range ops {
		if op.Kind != store.OpCreate && op.Number == 0 && op.LocalID == localID && strings.EqualFold(op.Repo, repo) && !op.Sending() {
			op.Number = number
			_, _ = e.store.Update(op)
		}
	}
}

// ensureLabels creates convention labels that don't exist in repo yet, so
// they get a proper color and description instead of GitHub's default grey.
func (e *Engine) ensureLabels(ctx context.Context, repo string, labels []string) error {
	cache, err := e.store.LoadRepo(repo)
	if err != nil {
		return err
	}
	var created []gh.Label
	for _, name := range labels {
		if !e.conv.IsConventionLabel(name) || hasLabel(cache.Labels, name) {
			continue
		}
		label := gh.Label{Name: name, Color: e.conv.LabelColor(name), Description: e.conv.LabelDescription(name)}
		if err := e.gh.CreateLabel(ctx, repo, label); err != nil {
			// Without permission to create labels, adding the label to the
			// issue may still work; let that request decide.
			if kindIsPermission(err) {
				continue
			}
			return err
		}
		created = append(created, label)
	}
	if len(created) > 0 {
		cache.Labels = append(cache.Labels, created...)
		return e.store.SaveRepo(cache)
	}
	return nil
}

func kindIsPermission(err error) bool {
	var ghErr *gh.Error
	return errors.As(err, &ghErr) && ghErr.Kind == gh.ErrPermission
}

func hasLabel(labels []gh.Label, name string) bool {
	for _, label := range labels {
		if strings.EqualFold(label.Name, name) {
			return true
		}
	}
	return false
}

func (e *Engine) removeIssue(repo string, number int) {
	cache, err := e.store.LoadRepo(repo)
	if err != nil {
		return
	}
	kept := cache.Issues[:0]
	for _, i := range cache.Issues {
		if i.Number != number {
			kept = append(kept, i)
		}
	}
	cache.Issues = kept
	e.store.ForgetComments(repo, number)
	_ = e.store.SaveRepo(cache)
}

func (e *Engine) putIssue(fresh issue.Issue) error {
	cache, err := e.store.LoadRepo(fresh.Repo)
	if err != nil {
		return err
	}
	cache.Put(fresh)
	return e.store.SaveRepo(cache)
}

func (e *Engine) patchIssue(repo string, number int, edit func(*issue.Issue)) error {
	cache, err := e.store.LoadRepo(repo)
	if err != nil {
		return err
	}
	for i := range cache.Issues {
		if cache.Issues[i].Number == number {
			edit(&cache.Issues[i])
			return e.store.SaveRepo(cache)
		}
	}
	return nil
}

// serverUpdatedAt returns when GitHub last changed the issue, ignoring
// pending local changes (which are timestamped with the local clock).
func (e *Engine) serverUpdatedAt(i issue.Issue) time.Time {
	cache, err := e.store.LoadRepo(i.Repo)
	if err == nil {
		for _, cached := range cache.Issues {
			if cached.Number == i.Number {
				return cached.UpdatedAt
			}
		}
	}
	return time.Time{}
}

// Comments returns an issue's comments, from the cache when the issue
// hasn't changed on GitHub since they were fetched.
func (e *Engine) Comments(ctx context.Context, i issue.Issue) ([]store.Comment, error) {
	if i.Number == 0 {
		return nil, nil
	}
	updated := e.serverUpdatedAt(i)
	cached, ok := e.store.LoadComments(i.Repo, i.Number)
	if ok && !updated.IsZero() && !cached.IssueUpdatedAt.Before(updated) {
		return cached.Comments, nil
	}
	if e.gh == nil {
		return cached.Comments, nil
	}
	fetched, err := e.gh.ListComments(ctx, i.Repo, i.Number)
	if err != nil {
		if ok {
			return cached.Comments, err
		}
		return nil, err
	}
	comments := make([]store.Comment, 0, len(fetched))
	for _, comment := range fetched {
		comments = append(comments, commentFromGitHub(comment))
	}
	_ = e.store.SaveComments(i.Repo, i.Number, store.CommentCache{IssueUpdatedAt: updated, Comments: comments})
	return comments, nil
}

// CachedComments returns cached comments without contacting GitHub, and
// whether they are current.
func (e *Engine) CachedComments(i issue.Issue) ([]store.Comment, bool) {
	cached, ok := e.store.LoadComments(i.Repo, i.Number)
	updated := e.serverUpdatedAt(i)
	return cached.Comments, ok && !updated.IsZero() && !cached.IssueUpdatedAt.Before(updated)
}

func commentFromGitHub(c gh.Comment) store.Comment {
	return store.Comment{Author: c.User.Login, Body: c.Body, CreatedAt: c.CreatedAt, URL: c.HTMLURL}
}

// SortIssues orders issues by most recently updated first.
func SortIssues(issues []issue.Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		if !issues[i].UpdatedAt.Equal(issues[j].UpdatedAt) {
			return issues[i].UpdatedAt.After(issues[j].UpdatedAt)
		}
		return issues[i].Key() < issues[j].Key()
	})
}
