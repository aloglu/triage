package githubsync

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

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

	_, err := client.UpsertItem("aloglu/triage-inbox", item)
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
