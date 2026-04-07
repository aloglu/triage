package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aloglu/triage/internal/config"
	imodel "github.com/aloglu/triage/internal/model"
	"github.com/aloglu/triage/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestCommandCompletionMatchesSuggestion(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeCommand
	m.commandInput.SetValue("view a")
	m.commandInput.CursorEnd()

	if got := matchedCommandSuggestions(m.commandInput.Value(), m.commandSuggestions()); len(got) != 2 {
		t.Fatalf("matched suggestions = %v, want 2 matches", got)
	}
	if got := m.commandCompletionSuffix(); got != "" {
		t.Fatalf("commandCompletionSuffix(view a) = %q, want empty for ambiguous match", got)
	}

	m.commandInput.SetValue("storage g")
	m.commandInput.CursorEnd()
	if got := m.commandCompletionSuffix(); got != "ithub " {
		t.Fatalf("commandCompletionSuffix(storage g) = %q, want %q", got, "ithub ")
	}
}

func TestCommandTabCompletesSuggestion(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeCommand
	m.commandInput.SetValue("sort created d")
	m.commandInput.CursorEnd()

	updated, _ := m.updateCommand(tea.KeyMsg{Type: tea.KeyTab})
	got := updated.(modelUI)
	if got.commandInput.Value() != "sort created desc" {
		t.Fatalf("tab completion = %q, want %q", got.commandInput.Value(), "sort created desc")
	}
}

func TestCommandStageCompletion(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeCommand
	m.commandInput.SetValue("stage act")
	m.commandInput.CursorEnd()

	if got := m.commandCompletionSuffix(); got != "ive" {
		t.Fatalf("commandCompletionSuffix(stage act) = %q, want %q", got, "ive")
	}
}

func TestCommandDownThenTabAcceptsHighlightedSuggestion(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeCommand
	m.commandInput.SetValue("view a")
	m.commandInput.CursorEnd()

	updated, _ := m.updateCommand(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(modelUI)
	updated, _ = m.updateCommand(tea.KeyMsg{Type: tea.KeyTab})
	got := updated.(modelUI)

	if got.commandInput.Value() != "view archive" {
		t.Fatalf("tab completion after down = %q, want %q", got.commandInput.Value(), "view archive")
	}
}

func TestCommandBackspaceClosesWhenInputBecomesEmpty(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeCommand
	m.commandInput.SetValue("q")
	m.commandInput.CursorEnd()
	m.commandInput.Focus()

	updated, _ := m.updateCommand(tea.KeyMsg{Type: tea.KeyBackspace})
	got := updated.(modelUI)
	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want %v", got.mode, modeNormal)
	}
}

func TestCommandBackspaceClosesWhenAlreadyEmpty(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeCommand
	m.commandInput.SetValue("")
	m.commandInput.CursorEnd()
	m.commandInput.Focus()

	updated, _ := m.updateCommand(tea.KeyMsg{Type: tea.KeyBackspace})
	got := updated.(modelUI)
	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want %v", got.mode, modeNormal)
	}
}

func TestUpdateNormalQuestionMarkOpensShortcutsModal(t *testing.T) {
	m := New().(modelUI)

	updated, _ := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	got := updated.(modelUI)
	if got.mode != modeShortcuts {
		t.Fatalf("mode = %v, want %v", got.mode, modeShortcuts)
	}
}

func TestRenderContentShowsCommandOverlayForAmbiguousMatches(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeCommand
	m.commandInput.SetValue("view a")
	m.commandInput.CursorEnd()

	rendered := m.renderContent()
	if !strings.Contains(rendered, "view all") {
		t.Fatalf("expected command overlay to include view all")
	}
	if !strings.Contains(rendered, "view archive") {
		t.Fatalf("expected command overlay to include view archive")
	}
}

func TestOverlayBottomPreservesRightSideOfBaseLine(t *testing.T) {
	base := strings.Join([]string{
		"left border                           right border",
		"left border                           right border",
	}, "\n")
	overlay := strings.Join([]string{
		"menu",
		"box ",
	}, "\n")

	got := overlayBottom(base, overlay)
	lines := strings.Split(got, "\n")
	if !strings.HasSuffix(lines[0], "right border") {
		t.Fatalf("expected first line to preserve right side, got %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], "right border") {
		t.Fatalf("expected second line to preserve right side, got %q", lines[1])
	}
}

func TestRenderCommandOverlayDoesNotPadToFullWidth(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeCommand
	m.commandInput.SetValue("view a")
	m.commandInput.CursorEnd()

	overlay := m.renderCommandOverlay(92)
	for i, line := range strings.Split(overlay, "\n") {
		if got := lipgloss.Width(line); got >= 92 {
			t.Fatalf("overlay line %d width = %d, want less than 92", i, got)
		}
	}
}

func TestRunViewCommandAllShowsOnlyActiveItems(t *testing.T) {
	m := New().(modelUI)
	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{Title: "Active", Project: "project", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
		{Title: "Done", Project: "project", Stage: imodel.StageDone, UpdatedAt: now, CreatedAt: now},
	}
	m.viewMode = viewArchive
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	updated := m.runViewCommand([]string{"all"}).(modelUI)
	if updated.viewMode != viewActive {
		t.Fatalf("viewMode = %v, want %v", updated.viewMode, viewActive)
	}
	if len(updated.filtered) != 1 {
		t.Fatalf("filtered count = %d, want 1", len(updated.filtered))
	}
	if updated.items[updated.filtered[0]].Title != "Active" {
		t.Fatalf("visible item = %q, want %q", updated.items[updated.filtered[0]].Title, "Active")
	}
}

func TestRunViewCommandTrashShowsOnlyTrashedItems(t *testing.T) {
	m := New().(modelUI)
	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{Title: "Active", Project: "project", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
		{Title: "Trashed", Project: "project", Stage: imodel.StageActive, Trashed: true, UpdatedAt: now, CreatedAt: now},
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	updated := m.runViewCommand([]string{"trash"}).(modelUI)
	if updated.viewMode != viewTrash {
		t.Fatalf("viewMode = %v, want %v", updated.viewMode, viewTrash)
	}
	if len(updated.filtered) != 1 || updated.items[updated.filtered[0]].Title != "Trashed" {
		t.Fatalf("trash view did not narrow to trashed item")
	}
}

func TestRunCommandShortcutsEntersShortcutsModal(t *testing.T) {
	m := New().(modelUI)

	model, _ := m.runCommand("shortcuts")
	updated := model.(modelUI)
	if updated.mode != modeShortcuts {
		t.Fatalf("mode = %v, want %v", updated.mode, modeShortcuts)
	}
}

func TestRunSearchCommandSetsQuery(t *testing.T) {
	m := New().(modelUI)
	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{Title: "Alpha task", Project: "project", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
		{Title: "Beta task", Project: "project", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	updated := m.runSearchCommand("alpha").(modelUI)
	if updated.lastSearch != "alpha" {
		t.Fatalf("lastSearch = %q, want %q", updated.lastSearch, "alpha")
	}
	if len(updated.filtered) != 1 || updated.items[updated.filtered[0]].Title != "Alpha task" {
		t.Fatalf("search filtering did not narrow to Alpha task")
	}
}

func TestRunProjectCommandMatchesExistingProject(t *testing.T) {
	m := New().(modelUI)
	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{Title: "One", Project: "Serein", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
		{Title: "Two", Project: "Personal", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	updated := m.runProjectCommand("serein").(modelUI)
	if updated.projectFilter != "Serein" {
		t.Fatalf("projectFilter = %q, want %q", updated.projectFilter, "Serein")
	}
	if len(updated.filtered) != 1 || updated.items[updated.filtered[0]].Project != "Serein" {
		t.Fatalf("project filtering did not narrow to Serein")
	}
}

func TestRunStageCommandFiltersItems(t *testing.T) {
	m := New().(modelUI)
	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{Title: "Idea", Project: "Serein", Stage: imodel.StageIdea, UpdatedAt: now, CreatedAt: now},
		{Title: "Active", Project: "Serein", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
	}
	m.projectFilter = allProjectsLabel
	m.stageFilter = allStagesLabel
	m.rebuildFiltered()

	updated := m.runStageCommand("active").(modelUI)
	if updated.stageFilter != "active" {
		t.Fatalf("stageFilter = %q, want %q", updated.stageFilter, "active")
	}
	if len(updated.filtered) != 1 || updated.items[updated.filtered[0]].Title != "Active" {
		t.Fatalf("stage filtering did not narrow to Active")
	}
}

func TestRunStageCommandAllClearsFilter(t *testing.T) {
	m := New().(modelUI)
	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{Title: "Idea", Project: "Serein", Stage: imodel.StageIdea, UpdatedAt: now, CreatedAt: now},
		{Title: "Active", Project: "Serein", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
	}
	m.projectFilter = allProjectsLabel
	m.stageFilter = "active"
	m.rebuildFiltered()

	updated := m.runStageCommand("all").(modelUI)
	if updated.stageFilter != allStagesLabel {
		t.Fatalf("stageFilter = %q, want %q", updated.stageFilter, allStagesLabel)
	}
	if len(updated.filtered) != 2 {
		t.Fatalf("filtered count = %d, want 2", len(updated.filtered))
	}
}

func TestRunCommandNewEntersEditMode(t *testing.T) {
	m := New().(modelUI)
	model, _ := m.runCommand("new")
	updated := model.(modelUI)

	if updated.mode != modeEdit {
		t.Fatalf("mode = %v, want %v", updated.mode, modeEdit)
	}
	if updated.form.focusIndex != 0 {
		t.Fatalf("focusIndex = %d, want 0", updated.form.focusIndex)
	}
}

func TestRunDeleteRestoreAndPurgeCommandsInLocalMode(t *testing.T) {
	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	store := storage.NewJSONStore(filepath.Join(t.TempDir(), "items.json"))
	m := New().(modelUI)
	m.store = store
	m.config = config.AppConfig{StorageMode: config.ModeLocal, DataFile: store.Path()}
	m.items = []imodel.Item{
		{Title: "Keep", Project: "project", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	deleted := m.runDeleteCommand().(modelUI)
	if !deleted.items[0].Trashed {
		t.Fatalf("expected item to be trashed after delete")
	}

	trashedView := deleted.runViewCommand([]string{"trash"}).(modelUI)
	if len(trashedView.filtered) != 1 {
		t.Fatalf("expected trashed item to appear in trash view")
	}

	restored := trashedView.runRestoreCommand().(modelUI)
	if restored.items[0].Trashed {
		t.Fatalf("expected item to be restored from trash")
	}

	retrashed := restored.runViewCommand([]string{"all"}).(modelUI)
	retrashed = retrashed.runDeleteCommand().(modelUI)
	retrashed = retrashed.runViewCommand([]string{"trash"}).(modelUI)
	purgePrompt := retrashed.runPurgeCommand().(modelUI)
	if purgePrompt.mode != modeConfirm || purgePrompt.confirm == nil {
		t.Fatalf("expected purge to enter confirm mode")
	}

	purged := purgePrompt.confirmActionNow().(modelUI)
	if len(purged.items) != 0 {
		t.Fatalf("expected purge to remove item permanently")
	}
}

func TestRenderFooterShowsIdleHintAndMetadata(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeNormal
	m.statusMessage = ""
	m.statusUntil = time.Time{}

	idle := m.renderFooter()
	if !strings.Contains(idle, ": command") {
		t.Fatalf("expected idle footer hint, got %q", idle)
	}
	if !strings.Contains(idle, "view:") {
		t.Fatalf("expected footer metadata, got %q", idle)
	}
}

func TestRenderHeaderShowsStatusInsteadOfFooter(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeNormal

	m.statusMessage = "synced"
	m.statusUntil = time.Now().Add(time.Second)
	header := m.renderHeader()
	if !strings.Contains(header, "synced") {
		t.Fatalf("expected status header, got %q", header)
	}

	footer := m.renderFooter()
	if strings.Contains(footer, "synced") {
		t.Fatalf("expected footer to omit status, got %q", footer)
	}
}

func TestRenderConfirmModalShowsButtons(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeConfirm
	m.items = []imodel.Item{
		{Title: "Danger", Project: "project", Stage: imodel.StageActive},
	}
	m.confirm = &confirmState{
		action:    confirmPurge,
		itemIndex: 0,
	}

	rendered := m.renderConfirmModal()
	if !strings.Contains(rendered, "(P)urge") {
		t.Fatalf("expected confirm modal to show purge button")
	}
	if !strings.Contains(rendered, "(C)ancel") {
		t.Fatalf("expected confirm modal to show cancel button")
	}
}

func TestRenderFooterHiddenInConfirmMode(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeConfirm

	if got := m.renderFooter(); got != "" {
		t.Fatalf("expected confirm footer to be hidden, got %q", got)
	}
}
