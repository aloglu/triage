package githubsync

import (
	"strings"
	"testing"
	"time"

	"github.com/aloglu/triage/internal/model"
)

func TestSerializeAndParseBody(t *testing.T) {
	item := model.Item{
		Project: "triage",
		Stage:   model.StageActive,
		Body:    "Line one.\n\nLine two.",
	}

	raw := SerializeBody(item)
	project, stage, body, err := ParseBody(raw)
	if err != nil {
		t.Fatalf("ParseBody() error = %v", err)
	}
	if project != item.Project {
		t.Fatalf("project = %q, want %q", project, item.Project)
	}
	if stage != item.Stage {
		t.Fatalf("stage = %q, want %q", stage, item.Stage)
	}
	if body != item.Body {
		t.Fatalf("body = %q, want %q", body, item.Body)
	}
}

func TestParseBodyRejectsMissingFrontmatter(t *testing.T) {
	_, _, _, err := ParseBody("no frontmatter")
	if err == nil {
		t.Fatal("ParseBody() error = nil, want error")
	}
}

func TestMergeLabelsPreservesUnmanaged(t *testing.T) {
	oldItem := model.Item{Project: "triage", Stage: model.StageIdea}
	newItem := model.Item{Project: "triage", Stage: model.StageActive}

	got := mergeLabels([]string{"triage", "idea", "keep-me"}, oldItem, newItem)
	want := []string{"active", "keep-me", "triage"}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("labels = %v, want %v", got, want)
	}
}

func TestMergeLabelsIncludesTrashed(t *testing.T) {
	oldItem := model.Item{Project: "triage", Stage: model.StageActive}
	newItem := model.Item{Project: "triage", Stage: model.StageActive, Trashed: true}

	got := mergeLabels([]string{"triage", "active"}, oldItem, newItem)
	want := []string{"active", "trashed", "triage"}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("labels = %v, want %v", got, want)
	}
}

func TestIssueToItem(t *testing.T) {
	now := time.Now().UTC()
	item, err := issueToItem("aloglu/triage-inbox", issueResponse{
		Number:    12,
		Title:     "Test issue",
		Body:      "---\nproject: triage\nstage: planned\n---\n\nBody text\n",
		State:     "open",
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("issueToItem() error = %v", err)
	}
	if item.Project != "triage" || item.Stage != model.StagePlanned || item.Body != "Body text" {
		t.Fatalf("unexpected item = %+v", item)
	}
	if !item.RemoteUpdatedAt.Equal(now) {
		t.Fatalf("remote updated at = %v, want %v", item.RemoteUpdatedAt, now)
	}
}

func TestIssueToItemMarksTrashedFromLabel(t *testing.T) {
	now := time.Now().UTC()
	item, err := issueToItem("aloglu/triage-inbox", issueResponse{
		Number:    12,
		Title:     "Test issue",
		Body:      "---\nproject: triage\nstage: planned\n---\n\nBody text\n",
		State:     "closed",
		CreatedAt: now,
		UpdatedAt: now,
		Labels: []label{
			{Name: "triage"},
			{Name: "planned"},
			{Name: "trashed"},
		},
	})
	if err != nil {
		t.Fatalf("issueToItem() error = %v", err)
	}
	if !item.Trashed {
		t.Fatalf("expected item to be marked trashed")
	}
}
