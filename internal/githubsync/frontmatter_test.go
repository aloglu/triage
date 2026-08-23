package githubsync

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aloglu/triage/internal/config"
	"github.com/aloglu/triage/internal/model"
)

func TestSerializeAndParseBody(t *testing.T) {
	item := model.Item{
		Project: "triage",
		Type:    model.TypeFeature,
		Stage:   model.StageActive,
		Body:    "Line one.\n\nLine two.",
	}

	raw := SerializeBody(item)
	project, itemType, stage, trashed, body, err := ParseBody(raw)
	if err != nil {
		t.Fatalf("ParseBody() error = %v", err)
	}
	if project != item.Project {
		t.Fatalf("project = %q, want %q", project, item.Project)
	}
	if itemType != item.Type {
		t.Fatalf("type = %q, want %q", itemType, item.Type)
	}
	if stage != item.Stage {
		t.Fatalf("stage = %q, want %q", stage, item.Stage)
	}
	if trashed {
		t.Fatal("trashed = true, want false")
	}
	if body != item.Body {
		t.Fatalf("body = %q, want %q", body, item.Body)
	}
	if !strings.Contains(raw, "```yaml") {
		t.Fatalf("serialized body = %q, want yaml code fence", raw)
	}
}

func TestSerializeAndParseBodyPreservesTrashedState(t *testing.T) {
	item := model.Item{Project: "triage", Type: model.TypeChore, Stage: model.StageDone, Trashed: true}

	raw := SerializeBody(item)
	_, _, _, trashed, _, err := ParseBody(raw)
	if err != nil {
		t.Fatalf("ParseBody() error = %v", err)
	}
	if !trashed {
		t.Fatal("trashed = false, want true")
	}
	if !strings.Contains(raw, "trashed: true") {
		t.Fatalf("serialized body = %q, want trashed metadata", raw)
	}
}

func TestParseBodyRejectsMissingFrontmatter(t *testing.T) {
	_, _, _, _, _, err := ParseBody("no frontmatter")
	if err == nil {
		t.Fatal("ParseBody() error = nil, want error")
	}
	if !errors.Is(err, ErrUnmanagedIssue) {
		t.Fatalf("ParseBody() error = %v, want ErrUnmanagedIssue", err)
	}
}

func TestParseBodyAcceptsReorderedFrontmatter(t *testing.T) {
	raw := "---\nstage: planned\nproject: triage\ntype: bug\n---\n\nBody text\n"

	project, itemType, stage, _, body, err := ParseBody(raw)
	if err != nil {
		t.Fatalf("ParseBody() error = %v", err)
	}
	if project != "triage" {
		t.Fatalf("project = %q, want %q", project, "triage")
	}
	if itemType != model.TypeBug {
		t.Fatalf("type = %q, want %q", itemType, model.TypeBug)
	}
	if stage != model.StagePlanned {
		t.Fatalf("stage = %q, want %q", stage, model.StagePlanned)
	}
	if body != "Body text" {
		t.Fatalf("body = %q, want %q", body, "Body text")
	}
}

func TestParseBodyAcceptsFencedFrontmatter(t *testing.T) {
	raw := "```yaml\nproject: triage\ntype: bug\nstage: planned\n```\n\nBody text\n"

	project, itemType, stage, _, body, err := ParseBody(raw)
	if err != nil {
		t.Fatalf("ParseBody() error = %v", err)
	}
	if project != "triage" || itemType != model.TypeBug || stage != model.StagePlanned || body != "Body text" {
		t.Fatalf("unexpected parse result: %q %q %q %q", project, itemType, stage, body)
	}
}

func TestParseBodyAcceptsCapitalizedKeysAndValues(t *testing.T) {
	raw := "---\nProject: triage\nType: Bug\nStage: Active\n---\n\nBody text\n"

	project, itemType, stage, _, body, err := ParseBody(raw)
	if err != nil {
		t.Fatalf("ParseBody() error = %v", err)
	}
	if project != "triage" {
		t.Fatalf("project = %q, want %q", project, "triage")
	}
	if itemType != model.TypeBug {
		t.Fatalf("type = %q, want %q", itemType, model.TypeBug)
	}
	if stage != model.StageActive {
		t.Fatalf("stage = %q, want %q", stage, model.StageActive)
	}
	if body != "Body text" {
		t.Fatalf("body = %q, want %q", body, "Body text")
	}
}

func TestParseBodyAcceptsPlanningAlias(t *testing.T) {
	raw := "---\nproject: triage\ntype: feature\nstage: planning\n---\n"

	_, _, stage, _, _, err := ParseBody(raw)
	if err != nil {
		t.Fatalf("ParseBody() error = %v", err)
	}
	if stage != model.StagePlanned {
		t.Fatalf("stage = %q, want %q", stage, model.StagePlanned)
	}
}

func TestParseDraftAcceptsReorderedCaseInsensitiveMetadata(t *testing.T) {
	raw := "```yaml\nStage: Planning\nProject: Serein\nTitle: Fix navbar resize\nType: Bug\nRepo: aloglu/serein\n```\n\nDraft body\n"

	meta, body, err := ParseDraft(raw)
	if err != nil {
		t.Fatalf("ParseDraft() error = %v", err)
	}
	if meta.Title != "Fix navbar resize" {
		t.Fatalf("title = %q", meta.Title)
	}
	if meta.Project != "Serein" {
		t.Fatalf("project = %q", meta.Project)
	}
	if meta.Repo != "aloglu/serein" {
		t.Fatalf("repo = %q", meta.Repo)
	}
	if meta.Type != model.TypeBug {
		t.Fatalf("type = %q", meta.Type)
	}
	if meta.Stage != model.StagePlanned {
		t.Fatalf("stage = %q", meta.Stage)
	}
	if body != "Draft body" {
		t.Fatalf("body = %q", body)
	}
}

func TestParseDraftDefaultsTypeAndStage(t *testing.T) {
	raw := "```yaml\ntitle: Write docs\nproject: triage\n```\n\nBody text\n"

	meta, body, err := ParseDraft(raw)
	if err != nil {
		t.Fatalf("ParseDraft() error = %v", err)
	}
	if meta.Type != model.TypeFeature {
		t.Fatalf("type = %q, want %q", meta.Type, model.TypeFeature)
	}
	if meta.Stage != model.StageIdea {
		t.Fatalf("stage = %q, want %q", meta.Stage, model.StageIdea)
	}
	if body != "Body text" {
		t.Fatalf("body = %q", body)
	}
}

func TestMergeLabelsPreservesUnmanaged(t *testing.T) {
	oldItem := model.Item{Project: "triage", Type: model.TypeFeature, Stage: model.StageIdea}
	newItem := model.Item{Project: "triage", Type: model.TypeFeature, Stage: model.StageActive}

	got := mergeLabels([]string{"triage", "idea", "keep-me"}, oldItem, newItem, "aloglu/triage-inbox", config.ProjectLabelAlways, config.MetadataLabelsOn)
	want := []string{"triage", "idea", "keep-me"}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("labels = %v, want %v", got, want)
	}
}

func TestMergeLabelsMigratesNamespacedLabelsOnManagedIssue(t *testing.T) {
	oldItem := model.Item{Project: "triage", Type: model.TypeFeature, Stage: model.StageIdea}
	newItem := model.Item{Project: "triage", Type: model.TypeBug, Stage: model.StageActive}

	got := mergeLabels([]string{managedMarkerLabel, managedProjectPrefix + "triage", managedTypePrefix + "feature", managedStagePrefix + "idea", "keep-me"}, oldItem, newItem, "aloglu/triage-inbox", config.ProjectLabelAlways, config.MetadataLabelsOn)
	want := []string{"active", "bug", "keep-me", "triage", managedMarkerLabel}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("labels = %v, want %v", got, want)
	}
}

func TestMergeLabelsIncludesTrashed(t *testing.T) {
	oldItem := model.Item{Project: "triage", Type: model.TypeFeature, Stage: model.StageActive}
	newItem := model.Item{Project: "triage", Type: model.TypeFeature, Stage: model.StageActive, Trashed: true}

	got := mergeLabels([]string{managedMarkerLabel, "triage", "active", "feature"}, oldItem, newItem, "aloglu/triage-inbox", config.ProjectLabelAlways, config.MetadataLabelsOn)
	want := []string{"active", "feature", "trashed", "triage", managedMarkerLabel}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("labels = %v, want %v", got, want)
	}
}

func TestMergeLabelsAutoOmitsProjectLabelForMatchingRepo(t *testing.T) {
	oldItem := model.Item{Project: "serein", Type: model.TypeFeature, Stage: model.StageIdea}
	newItem := model.Item{Project: "serein", Type: model.TypeBug, Stage: model.StageActive}

	got := mergeLabels([]string{managedMarkerLabel, "serein", "idea", "feature"}, oldItem, newItem, "aloglu/serein-web", config.ProjectLabelAuto, config.MetadataLabelsOn)
	want := []string{"active", "bug", managedMarkerLabel}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("labels = %v, want %v", got, want)
	}
}

func TestMergeLabelsAutoKeepsProjectLabelForInboxRepo(t *testing.T) {
	oldItem := model.Item{Project: "triage", Type: model.TypeFeature, Stage: model.StageIdea}
	newItem := model.Item{Project: "triage", Type: model.TypeFeature, Stage: model.StageActive}

	got := mergeLabels([]string{managedMarkerLabel, "triage", "idea", "feature"}, oldItem, newItem, "aloglu/triage-inbox", config.ProjectLabelAuto, config.MetadataLabelsOn)
	want := []string{"active", "feature", "triage", managedMarkerLabel}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("labels = %v, want %v", got, want)
	}
}

func TestMergeLabelsCanDisableMetadataWithoutTouchingConventionalLabels(t *testing.T) {
	item := model.Item{Project: "triage", Type: model.TypeBug, Stage: model.StageActive}

	got := mergeLabels([]string{managedMarkerLabel, managedTypePrefix + "bug", "bug", "keep-me"}, item, item, "aloglu/triage-inbox", config.ProjectLabelAlways, config.MetadataLabelsOff)
	want := []string{"bug", "keep-me", managedMarkerLabel}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("labels = %v, want %v", got, want)
	}
}

func TestConventionalProjectLabelStaysWithinGitHubLimit(t *testing.T) {
	project := strings.Repeat("project-", 10)
	got := conventionalProjectLabel(project)
	if len([]rune(got)) > githubLabelNameLimit {
		t.Fatalf("project label length = %d, want at most %d: %q", len([]rune(got)), githubLabelNameLimit, got)
	}
	if got != conventionalProjectLabel(project) {
		t.Fatal("project label is not deterministic")
	}
	if got == conventionalProjectLabel(project+"different") {
		t.Fatal("different long project names produced the same managed label")
	}
}

func TestIssueToItem(t *testing.T) {
	now := time.Now().UTC()
	item, err := issueToItem("aloglu/triage-inbox", issueResponse{
		Number:    12,
		Title:     "Test issue",
		Body:      "```yaml\nproject: triage\ntype: bug\nstage: planned\n```\n\nBody text\n",
		State:     "open",
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("issueToItem() error = %v", err)
	}
	if item.Project != "triage" || item.Type != model.TypeBug || item.Stage != model.StagePlanned || item.Body != "Body text" {
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
		Body:      "```yaml\nproject: triage\ntype: feature\nstage: planned\n```\n\nBody text\n",
		State:     "closed",
		CreatedAt: now,
		UpdatedAt: now,
		Labels: []label{
			{Name: managedMarkerLabel},
			{Name: "triage"},
			{Name: "feature"},
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

func TestIssueToItemIgnoresLabelsWithoutManagedMarker(t *testing.T) {
	now := time.Now().UTC()
	item, err := issueToItem("aloglu/triage-inbox", issueResponse{
		Number:    13,
		Title:     "Unmarked issue",
		Body:      "```yaml\nproject: triage\ntype: feature\nstage: active\n```\n",
		State:     "closed",
		CreatedAt: now,
		UpdatedAt: now,
		Labels:    []label{{Name: "trashed"}, {Name: "bug"}},
	})
	if err != nil {
		t.Fatalf("issueToItem() error = %v", err)
	}
	if item.Trashed {
		t.Fatal("unmarked issue used its trashed label as app metadata")
	}
	if item.Stage != model.StageDone {
		t.Fatalf("stage = %q, want %q", item.Stage, model.StageDone)
	}
}

func TestIssueToItemNormalizesClosedIssueToDoneStage(t *testing.T) {
	now := time.Now().UTC()
	item, err := issueToItem("aloglu/triage-inbox", issueResponse{
		Number:    14,
		Title:     "Closed elsewhere",
		Body:      "```yaml\nproject: triage\ntype: bug\nstage: active\n```\n\nBody text\n",
		State:     "closed",
		CreatedAt: now,
		UpdatedAt: now,
		Labels: []label{
			{Name: managedMarkerLabel},
			{Name: "triage"},
			{Name: "bug"},
			{Name: "active"},
		},
	})
	if err != nil {
		t.Fatalf("issueToItem() error = %v", err)
	}
	if item.Stage != model.StageDone {
		t.Fatalf("stage = %q, want %q", item.Stage, model.StageDone)
	}
	if item.Trashed {
		t.Fatal("expected non-trashed closed issue")
	}
}

func TestIssueToItemKeepsClosedTrashedIssueTrashed(t *testing.T) {
	now := time.Now().UTC()
	item, err := issueToItem("aloglu/triage-inbox", issueResponse{
		Number:    15,
		Title:     "Closed trash",
		Body:      "```yaml\nproject: triage\ntype: chore\nstage: active\n```\n\nBody text\n",
		State:     "closed",
		CreatedAt: now,
		UpdatedAt: now,
		Labels: []label{
			{Name: managedMarkerLabel},
			{Name: "triage"},
			{Name: "chore"},
			{Name: "active"},
			{Name: "trashed"},
		},
	})
	if err != nil {
		t.Fatalf("issueToItem() error = %v", err)
	}
	if !item.Trashed {
		t.Fatal("expected trashed item")
	}
	if item.Stage != model.StageActive {
		t.Fatalf("stage = %q, want %q", item.Stage, model.StageActive)
	}
}

func TestCanonicalIssueBodyMatchesIgnoresTrailingNewlineOnly(t *testing.T) {
	item := model.Item{
		Project: "triage",
		Type:    model.TypeFeature,
		Stage:   model.StageActive,
		Body:    "Body text",
	}

	if !canonicalIssueBodyMatches("```yaml\nproject: triage\ntype: feature\nstage: active\n```\n\nBody text", item) {
		t.Fatal("expected canonical body match")
	}
}
