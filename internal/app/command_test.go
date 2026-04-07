package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aloglu/triage/internal/config"
	"github.com/aloglu/triage/internal/githubsync"
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

func TestRenderCommandInputLineShowsPathHintForExportImport(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeCommand

	m.commandInput.SetValue("export json")
	m.commandInput.CursorEnd()
	if got := stripANSI(m.renderCommandInputLine()); !strings.Contains(got, "<path>") {
		t.Fatalf("expected export command line to show path hint, got %q", got)
	}

	m.commandInput.SetValue("import json ")
	m.commandInput.CursorEnd()
	if got := stripANSI(m.renderCommandInputLine()); !strings.Contains(got, "<path>") {
		t.Fatalf("expected import command line to show path hint, got %q", got)
	}
}

func TestRenderCommandInputLineShowsRepoHintForStorageGitHub(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeCommand
	m.commandInput.SetValue("storage github")
	m.commandInput.CursorEnd()

	if got := stripANSI(m.renderCommandInputLine()); !strings.Contains(got, "owner/repo") {
		t.Fatalf("expected storage github command line to show repo hint, got %q", got)
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

func TestSearchUpdatesInstantlyAsYouType(t *testing.T) {
	m := New().(modelUI)
	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{Title: "Alpha task", Project: "project", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
		{Title: "Beta task", Project: "project", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	opened, _ := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	searching := opened.(modelUI)
	updated, _ := searching.updateSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	got := updated.(modelUI)

	if got.mode != modeSearch {
		t.Fatalf("mode = %v, want %v", got.mode, modeSearch)
	}
	if got.lastSearch != "a" {
		t.Fatalf("lastSearch = %q, want %q", got.lastSearch, "a")
	}
	if len(got.filtered) != 2 {
		t.Fatalf("expected live search for %q to keep 2 matches, got %d", got.lastSearch, len(got.filtered))
	}

	updated, _ = got.updateSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	got = updated.(modelUI)
	if got.lastSearch != "al" {
		t.Fatalf("lastSearch = %q, want %q", got.lastSearch, "al")
	}
	if len(got.filtered) != 1 || got.items[got.filtered[0]].Title != "Alpha task" {
		t.Fatalf("expected live search to narrow to Alpha task")
	}
}

func TestClearingSearchRestoresPreviousViewAndExitsSearchMode(t *testing.T) {
	m := New().(modelUI)
	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{Title: "Alpha task", Project: "project", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
		{Title: "Beta task", Project: "project", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	opened, _ := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	searching := opened.(modelUI)
	updated, _ := searching.updateSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	got := updated.(modelUI)
	if len(got.filtered) != 0 {
		t.Fatalf("expected temporary search to empty the list")
	}

	updated, _ = got.updateSearch(tea.KeyMsg{Type: tea.KeyBackspace})
	got = updated.(modelUI)
	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want %v", got.mode, modeNormal)
	}
	if got.lastSearch != "" {
		t.Fatalf("lastSearch = %q, want empty after restoring pre-search state", got.lastSearch)
	}
	if len(got.filtered) != 2 {
		t.Fatalf("expected full view to be restored, got %d items", len(got.filtered))
	}
}

func TestEscInSearchRestoresPreviousSearch(t *testing.T) {
	m := New().(modelUI)
	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{Title: "Alpha task", Project: "project", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
		{Title: "Beta task", Project: "project", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
	}
	m.projectFilter = allProjectsLabel
	m.lastSearch = "alpha"
	m.rebuildFiltered()

	opened, _ := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	searching := opened.(modelUI)
	updated, _ := searching.updateSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	searching = updated.(modelUI)
	updated, _ = searching.updateSearch(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(modelUI)
	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want %v", got.mode, modeNormal)
	}
	if got.lastSearch != "alpha" {
		t.Fatalf("lastSearch = %q, want restored search %q", got.lastSearch, "alpha")
	}
	if len(got.filtered) != 1 || got.items[got.filtered[0]].Title != "Alpha task" {
		t.Fatalf("expected previous search view to be restored")
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

func TestRunExportCommandWritesJSONFile(t *testing.T) {
	m := New().(modelUI)
	m.config = config.AppConfig{StorageMode: config.ModeLocal}
	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{
			Title:     "Exported",
			Project:   "project",
			Stage:     imodel.StageActive,
			Body:      "body",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	path := filepath.Join(t.TempDir(), "export.json")
	updated := m.runExportCommand("json " + path).(modelUI)
	if !strings.Contains(updated.statusMessage, "Exported 1 items") {
		t.Fatalf("unexpected export status: %q", updated.statusMessage)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read export file: %v", err)
	}

	var got []imodel.Item
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal export file: %v", err)
	}
	if len(got) != 1 || got[0].Title != "Exported" {
		t.Fatalf("unexpected exported payload: %+v", got)
	}
}

func TestRunExportCommandRequiresPath(t *testing.T) {
	m := New().(modelUI)
	m.config = config.AppConfig{StorageMode: config.ModeLocal}

	updated := m.runExportCommand("json").(modelUI)
	if updated.statusMessage != "Usage: export json <path>" {
		t.Fatalf("unexpected usage status: %q", updated.statusMessage)
	}
}

func TestRunExportCommandRequiresLocalMode(t *testing.T) {
	m := New().(modelUI)
	m.config = config.AppConfig{StorageMode: config.ModeGitHub}

	updated := m.runExportCommand("json /tmp/out.json").(modelUI)
	if updated.statusMessage != "Export is only available in local mode." {
		t.Fatalf("unexpected export mode status: %q", updated.statusMessage)
	}
}

func TestRunImportCommandEntersConfirmModeAndImports(t *testing.T) {
	m := New().(modelUI)
	store := storage.NewJSONStore(filepath.Join(t.TempDir(), "items.json"))
	m.store = store
	m.config = config.AppConfig{StorageMode: config.ModeLocal, DataFile: store.Path()}

	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	importPath := filepath.Join(t.TempDir(), "import.json")
	payload, err := json.Marshal([]imodel.Item{{
		Title:     "Imported",
		Project:   "project",
		Stage:     imodel.StageActive,
		Body:      "body",
		CreatedAt: now,
		UpdatedAt: now,
	}})
	if err != nil {
		t.Fatalf("marshal import payload: %v", err)
	}
	if err := os.WriteFile(importPath, payload, 0o600); err != nil {
		t.Fatalf("write import payload: %v", err)
	}

	prompt := m.runImportCommand("json " + importPath).(modelUI)
	if prompt.mode != modeConfirm || prompt.confirm == nil || prompt.confirm.action != confirmImport {
		t.Fatalf("expected import command to enter confirm mode")
	}

	imported := prompt.confirmActionNow().(modelUI)
	if len(imported.items) != 1 || imported.items[0].Title != "Imported" {
		t.Fatalf("unexpected imported items: %+v", imported.items)
	}
	if !strings.Contains(imported.statusMessage, "Imported 1 items") {
		t.Fatalf("unexpected import status: %q", imported.statusMessage)
	}
}

func TestRunImportCommandRequiresLocalMode(t *testing.T) {
	m := New().(modelUI)
	m.config = config.AppConfig{StorageMode: config.ModeGitHub}

	updated := m.runImportCommand("json /tmp/in.json").(modelUI)
	if updated.statusMessage != "Import is only available in local mode." {
		t.Fatalf("unexpected import mode status: %q", updated.statusMessage)
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

func TestBeginSyncShowsLoadingStatus(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeNormal
	m.config.StorageMode = config.ModeGitHub
	m.config.Repo = "aloglu/triage-inbox"
	m.githubClient = githubsync.NewClient()

	updated, cmd := m.beginSync()
	got := updated.(modelUI)
	if cmd == nil {
		t.Fatalf("expected beginSync to return a command")
	}
	if !got.syncing {
		t.Fatalf("expected beginSync to mark model as syncing")
	}
	if got.statusKind != statusLoading {
		t.Fatalf("expected loading status kind, got %v", got.statusKind)
	}
	if !strings.Contains(got.renderHeader(), "Syncing GitHub issues") {
		t.Fatalf("expected header to show loading status, got %q", got.renderHeader())
	}
}

func TestFinishSyncShowsSuccessStatus(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.config.StorageMode = config.ModeLocal
	m.store = storage.NewJSONStore(filepath.Join(t.TempDir(), "items.json"))
	m.syncing = true

	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	updated := m.finishSync(syncResultMsg{
		repo: "aloglu/triage-inbox",
		items: []imodel.Item{{
			Title:     "Synced",
			Project:   "project",
			Stage:     imodel.StageActive,
			CreatedAt: now,
			UpdatedAt: now,
		}},
	}).(modelUI)

	if updated.syncing {
		t.Fatalf("expected finishSync to clear syncing state")
	}
	if updated.statusKind != statusSuccess {
		t.Fatalf("expected success status kind, got %v", updated.statusKind)
	}
	if !strings.Contains(updated.statusMessage, "Synced 1 issues") {
		t.Fatalf("unexpected sync status: %q", updated.statusMessage)
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
