package githubsync

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/aloglu/triage/internal/config"
	"github.com/aloglu/triage/internal/model"
)

func TestUpdateItemConflict(t *testing.T) {
	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	remoteNow := now.Add(5 * time.Minute)

	var calls []string
	client := &Client{
		run: func(ctx context.Context, method, endpoint string, payload any, target any) error {
			calls = append(calls, method+" "+endpoint)
			switch {
			case method == "GET" && strings.Contains(endpoint, "/issues/12"):
				resp := target.(*issueResponse)
				*resp = issueResponse{
					Number:    12,
					Title:     "Remote title",
					Body:      "---\nproject: triage\nstage: blocked\n---\n\nRemote body\n",
					State:     "open",
					CreatedAt: now,
					UpdatedAt: remoteNow,
				}
				return nil
			case method == "PATCH":
				t.Fatalf("unexpected PATCH during conflict detection")
			}
			t.Fatalf("unexpected call: %s %s", method, endpoint)
			return nil
		},
	}

	item := model.Item{
		Title:           "Local title",
		Project:         "triage",
		Stage:           model.StageActive,
		Body:            "Local body",
		CreatedAt:       now,
		UpdatedAt:       now,
		RemoteUpdatedAt: now,
		IssueNumber:     12,
		Repo:            "aloglu/triage-inbox",
	}

	_, _, err := client.UpsertItem("aloglu/triage-inbox", item)
	if err == nil {
		t.Fatal("expected conflict error")
	}

	conflict, ok := err.(*ConflictError)
	if !ok {
		t.Fatalf("error type = %T, want *ConflictError", err)
	}
	if conflict.Remote.Title != "Remote title" || conflict.Remote.Stage != model.StageBlocked {
		t.Fatalf("unexpected remote item: %+v", conflict.Remote)
	}
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "GET ") {
		t.Fatalf("unexpected calls: %v", calls)
	}
}

func TestCreateItemAssignsViewerAndEnsuresLabelColors(t *testing.T) {
	now := time.Date(2026, 4, 8, 11, 30, 0, 0, time.UTC)
	refreshed := now.Add(2 * time.Second)

	type labelCall struct {
		method  string
		payload labelPayload
	}

	var labelCalls []labelCall
	assigned := false
	client := &Client{
		run: func(ctx context.Context, method, endpoint string, payload any, target any) error {
			switch {
			case method == "GET" && endpoint == "repos/aloglu/triage-inbox/labels/triage":
				return &Error{Kind: ErrorNotFound, Method: method, Endpoint: endpoint, Repo: "aloglu/triage-inbox", Resource: "repo", Message: "not found"}
			case method == "GET" && endpoint == "repos/aloglu/triage-inbox/labels/feature":
				return &Error{Kind: ErrorNotFound, Method: method, Endpoint: endpoint, Repo: "aloglu/triage-inbox", Resource: "repo", Message: "not found"}
			case method == "GET" && endpoint == "repos/aloglu/triage-inbox/labels/active":
				resp := target.(*label)
				*resp = label{Name: "active", Color: "ffffff"}
				return nil
			case method == "POST" && endpoint == "repos/aloglu/triage-inbox/labels":
				labelCalls = append(labelCalls, labelCall{method: method, payload: payload.(labelPayload)})
				return nil
			case method == "PATCH" && endpoint == "repos/aloglu/triage-inbox/labels/active":
				labelCalls = append(labelCalls, labelCall{method: method, payload: payload.(labelPayload)})
				return nil
			case method == "POST" && endpoint == "repos/aloglu/triage-inbox/issues":
				req := payload.(issuePayload)
				if req.Title != "Test issue" {
					t.Fatalf("title = %q", req.Title)
				}
				if len(req.Labels) != 3 || req.Labels[0] != "triage" || req.Labels[1] != "feature" || req.Labels[2] != "active" {
					t.Fatalf("labels = %v", req.Labels)
				}
				resp := target.(*issueResponse)
				*resp = issueResponse{
					Number:    7,
					Title:     req.Title,
					Body:      req.Body,
					State:     "open",
					CreatedAt: now,
					UpdatedAt: now,
				}
				return nil
			case method == "GET" && endpoint == "user":
				resp := target.(*viewerResponse)
				*resp = viewerResponse{Login: "aloglu"}
				return nil
			case method == "POST" && endpoint == "repos/aloglu/triage-inbox/issues/7/assignees":
				req := payload.(assigneesPayload)
				if len(req.Assignees) != 1 || req.Assignees[0] != "aloglu" {
					t.Fatalf("assignees = %v", req.Assignees)
				}
				assigned = true
				return nil
			case method == "GET" && endpoint == "repos/aloglu/triage-inbox/issues/7":
				resp := target.(*issueResponse)
				*resp = issueResponse{
					Number:    7,
					Title:     "Test issue",
					Body:      "---\nproject: triage\ntype: feature\nstage: active\n---\n\nBody\n",
					State:     "open",
					CreatedAt: now,
					UpdatedAt: refreshed,
				}
				return nil
			default:
				t.Fatalf("unexpected call: %s %s", method, endpoint)
				return nil
			}
		},
	}

	item := model.Item{
		Title:   "Test issue",
		Project: "triage",
		Stage:   model.StageActive,
		Body:    "Body",
	}

	saved, warning, err := client.UpsertItem("aloglu/triage-inbox", item)
	if err != nil {
		t.Fatalf("UpsertItem() error = %v", err)
	}
	if warning != "" {
		t.Fatalf("warning = %q, want empty", warning)
	}
	if !assigned {
		t.Fatal("expected assignee API call")
	}
	if saved.IssueNumber != 7 || saved.Repo != "aloglu/triage-inbox" {
		t.Fatalf("unexpected saved item: %+v", saved)
	}
	if !saved.RemoteUpdatedAt.Equal(refreshed) {
		t.Fatalf("RemoteUpdatedAt = %v, want %v", saved.RemoteUpdatedAt, refreshed)
	}
	if len(labelCalls) != 3 {
		t.Fatalf("label calls = %d, want 3", len(labelCalls))
	}
	if labelCalls[0].method != "POST" || labelCalls[0].payload.Name != "triage" || labelCalls[0].payload.Color != projectLabelColor("triage") {
		t.Fatalf("unexpected project label call: %+v", labelCalls[0])
	}
	if labelCalls[1].method != "POST" || labelCalls[1].payload.Name != "feature" || labelCalls[1].payload.Color != managedLabelColor("feature") {
		t.Fatalf("unexpected type label call: %+v", labelCalls[1])
	}
	if labelCalls[2].method != "PATCH" || labelCalls[2].payload.Name != "active" || labelCalls[2].payload.Color != managedLabelColor("active") {
		t.Fatalf("unexpected stage label call: %+v", labelCalls[2])
	}
}

func TestCreateItemAssignmentFailureReturnsWarning(t *testing.T) {
	now := time.Date(2026, 4, 8, 11, 30, 0, 0, time.UTC)

	client := &Client{
		run: func(ctx context.Context, method, endpoint string, payload any, target any) error {
			switch {
			case method == "GET" && strings.HasPrefix(endpoint, "repos/aloglu/triage-inbox/labels/"):
				return &Error{Kind: ErrorNotFound, Method: method, Endpoint: endpoint, Repo: "aloglu/triage-inbox", Resource: "repo", Message: "not found"}
			case method == "POST" && endpoint == "repos/aloglu/triage-inbox/labels":
				return nil
			case method == "POST" && endpoint == "repos/aloglu/triage-inbox/issues":
				resp := target.(*issueResponse)
				*resp = issueResponse{
					Number:    9,
					Title:     "Warn issue",
					Body:      "---\nproject: triage\nstage: planned\n---\n",
					State:     "open",
					CreatedAt: now,
					UpdatedAt: now,
				}
				return nil
			case method == "GET" && endpoint == "user":
				resp := target.(*viewerResponse)
				*resp = viewerResponse{Login: "aloglu"}
				return nil
			case method == "POST" && endpoint == "repos/aloglu/triage-inbox/issues/9/assignees":
				return errors.New("assignment failed")
			default:
				t.Fatalf("unexpected call: %s %s", method, endpoint)
				return nil
			}
		},
	}

	saved, warning, err := client.UpsertItem("aloglu/triage-inbox", model.Item{
		Title:   "Warn issue",
		Project: "triage",
		Stage:   model.StagePlanned,
	})
	if err != nil {
		t.Fatalf("UpsertItem() error = %v", err)
	}
	if saved.IssueNumber != 9 {
		t.Fatalf("issue number = %d, want 9", saved.IssueNumber)
	}
	if warning == "" {
		t.Fatal("expected assignment warning")
	}
}

func TestSyncRepoMarksItemsPendingWhenManagedMetadataIsMissing(t *testing.T) {
	now := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	client := &Client{
		run: func(ctx context.Context, method, endpoint string, payload any, target any) error {
			switch {
			case method == "GET" && endpoint == "repos/aloglu/triage-inbox/issues?state=all&per_page=100":
				resp := target.(*[]issueResponse)
				*resp = []issueResponse{{
					Number:    9,
					Title:     "Mobile issue",
					Body:      "```yaml\nproject: triage\ntype: bug\nstage: active\n```\n\nBody\n",
					State:     "open",
					CreatedAt: now,
					UpdatedAt: now,
				}}
				return nil
			case method == "GET" && endpoint == "user":
				resp := target.(*viewerResponse)
				*resp = viewerResponse{Login: "aloglu"}
				return nil
			default:
				t.Fatalf("unexpected call: %s %s", method, endpoint)
				return nil
			}
		},
		labelSync: config.ProjectLabelAuto,
	}

	items, err := client.SyncRepo("aloglu/triage-inbox")
	if err != nil {
		t.Fatalf("SyncRepo() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].PendingSync != model.SyncUpdate {
		t.Fatalf("PendingSync = %q, want %q", items[0].PendingSync, model.SyncUpdate)
	}
}

func TestSyncRepoLeavesItemsCleanWhenManagedMetadataMatches(t *testing.T) {
	now := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	client := &Client{
		run: func(ctx context.Context, method, endpoint string, payload any, target any) error {
			switch {
			case method == "GET" && endpoint == "repos/aloglu/triage-inbox/issues?state=all&per_page=100":
				resp := target.(*[]issueResponse)
				*resp = []issueResponse{{
					Number:    9,
					Title:     "Mobile issue",
					Body:      "```yaml\nproject: triage\ntype: bug\nstage: active\n```\n\nBody\n",
					State:     "open",
					CreatedAt: now,
					UpdatedAt: now,
					Labels: []label{
						{Name: "triage", Color: projectLabelColor("triage")},
						{Name: "bug", Color: managedLabelColor("bug")},
						{Name: "active", Color: managedLabelColor("active")},
					},
					Assignees: []viewerResponse{{Login: "aloglu"}},
				}}
				return nil
			case method == "GET" && endpoint == "user":
				resp := target.(*viewerResponse)
				*resp = viewerResponse{Login: "aloglu"}
				return nil
			default:
				t.Fatalf("unexpected call: %s %s", method, endpoint)
				return nil
			}
		},
		labelSync: config.ProjectLabelAuto,
	}

	items, err := client.SyncRepo("aloglu/triage-inbox")
	if err != nil {
		t.Fatalf("SyncRepo() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].PendingSync != model.SyncNone {
		t.Fatalf("PendingSync = %q, want empty", items[0].PendingSync)
	}
}

func TestSyncRepoMarksItemsPendingWhenBodyFormatIsNonCanonical(t *testing.T) {
	now := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	client := &Client{
		run: func(ctx context.Context, method, endpoint string, payload any, target any) error {
			switch {
			case method == "GET" && endpoint == "repos/aloglu/triage-inbox/issues?state=all&per_page=100":
				resp := target.(*[]issueResponse)
				*resp = []issueResponse{{
					Number:    9,
					Title:     "Mobile issue",
					Body:      "---\nproject: triage\nstage: active\ntype: bug\n---\n\nBody\n",
					State:     "open",
					CreatedAt: now,
					UpdatedAt: now,
					Labels: []label{
						{Name: "triage", Color: projectLabelColor("triage")},
						{Name: "bug", Color: managedLabelColor("bug")},
						{Name: "active", Color: managedLabelColor("active")},
					},
					Assignees: []viewerResponse{{Login: "aloglu"}},
				}}
				return nil
			case method == "GET" && endpoint == "user":
				resp := target.(*viewerResponse)
				*resp = viewerResponse{Login: "aloglu"}
				return nil
			default:
				t.Fatalf("unexpected call: %s %s", method, endpoint)
				return nil
			}
		},
		labelSync: config.ProjectLabelAuto,
	}

	items, err := client.SyncRepo("aloglu/triage-inbox")
	if err != nil {
		t.Fatalf("SyncRepo() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].PendingSync != model.SyncUpdate {
		t.Fatalf("PendingSync = %q, want %q", items[0].PendingSync, model.SyncUpdate)
	}
}

func TestClassifyAPIErrorMissingCLI(t *testing.T) {
	err := classifyAPIError("GET", "repos/aloglu/triage-inbox/issues", "exec: \"gh\": executable file not found", exec.ErrNotFound)

	var githubErr *Error
	if !errors.As(err, &githubErr) {
		t.Fatalf("error type = %T, want *Error", err)
	}
	if githubErr.Kind != ErrorCLIUnavailable {
		t.Fatalf("kind = %s, want %s", githubErr.Kind, ErrorCLIUnavailable)
	}
	if got := UserMessage(err); got != "GitHub CLI (`gh`) is not installed." {
		t.Fatalf("message = %q", got)
	}
}

func TestClassifyAPIErrorAuthRequired(t *testing.T) {
	err := classifyAPIError("GET", "repos/aloglu/triage-inbox/issues", "To get started with GitHub CLI, please run: gh auth login", errors.New("exit status 4"))

	var githubErr *Error
	if !errors.As(err, &githubErr) {
		t.Fatalf("error type = %T, want *Error", err)
	}
	if githubErr.Kind != ErrorAuthRequired {
		t.Fatalf("kind = %s, want %s", githubErr.Kind, ErrorAuthRequired)
	}
	if got := UserMessage(err); got != "GitHub authentication required. Run `gh auth login`." {
		t.Fatalf("message = %q", got)
	}
}

func TestClassifyAPIErrorRepoNotFound(t *testing.T) {
	err := classifyAPIError("GET", "repos/aloglu/triage-inbox/issues", "{\"message\":\"Not Found\",\"status\":404}", errors.New("exit status 1"))

	var githubErr *Error
	if !errors.As(err, &githubErr) {
		t.Fatalf("error type = %T, want *Error", err)
	}
	if githubErr.Kind != ErrorNotFound {
		t.Fatalf("kind = %s, want %s", githubErr.Kind, ErrorNotFound)
	}
	if got := UserMessage(err); got != "GitHub repository not found or inaccessible: aloglu/triage-inbox." {
		t.Fatalf("message = %q", got)
	}
}

func TestClassifyGraphQLErrorPermissionDenied(t *testing.T) {
	err := classifyGraphQLError("mutation { deleteIssue(input: {issueId: \"x\"}) { clientMutationId } }", "Must have admin rights to Repository.", errors.New("exit status 1"))

	var githubErr *Error
	if !errors.As(err, &githubErr) {
		t.Fatalf("error type = %T, want *Error", err)
	}
	if githubErr.Kind != ErrorPermissionDenied {
		t.Fatalf("kind = %s, want %s", githubErr.Kind, ErrorPermissionDenied)
	}
	if got := UserMessage(err); got != "GitHub denied this action. Deleting issues requires admin permission." {
		t.Fatalf("message = %q", got)
	}
}
