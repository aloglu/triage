package githubsync

import (
	"context"
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
