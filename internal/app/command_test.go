package app

import (
	"encoding/json"
	"fmt"
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

func TestRenderCommandInputLineShowsModeHintForProjectLabel(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeCommand
	m.commandInput.SetValue("project-label")
	m.commandInput.CursorEnd()

	if got := stripANSI(m.renderCommandInputLine()); !strings.Contains(got, "always|auto|never") {
		t.Fatalf("expected project-label command line to show mode hint, got %q", got)
	}
}

func TestRenderCommandInputLineKeepsHintsOnlyForOpenArgumentSlots(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeCommand

	m.commandInput.SetValue("density c")
	m.commandInput.CursorEnd()
	if got := stripANSI(m.renderCommandInputLine()); strings.Contains(got, "comfortable|compact") {
		t.Fatalf("expected density command line to drop option hint once typing begins, got %q", got)
	}

	m.commandInput.SetValue("project-label a")
	m.commandInput.CursorEnd()
	if got := stripANSI(m.renderCommandInputLine()); strings.Contains(got, "always|auto|never") {
		t.Fatalf("expected project-label command line to drop mode hint once typing begins, got %q", got)
	}

	m.commandInput.SetValue("sort updated")
	m.commandInput.CursorEnd()
	if got := stripANSI(m.renderCommandInputLine()); !strings.Contains(got, "asc|desc") {
		t.Fatalf("expected sort command line to show direction hint, got %q", got)
	}

	m.commandInput.SetValue("sort updated a")
	m.commandInput.CursorEnd()
	if got := stripANSI(m.renderCommandInputLine()); strings.Contains(got, "asc|desc") {
		t.Fatalf("expected sort command line to drop direction hint once typing begins, got %q", got)
	}

	m.commandInput.SetValue("storage g")
	m.commandInput.CursorEnd()
	if got := stripANSI(m.renderCommandInputLine()); strings.Contains(got, "owner/repo") {
		t.Fatalf("expected storage command line to avoid repo hint before github is complete, got %q", got)
	}
}

func TestRenderCommandInputLineShowsHintsForProjectRepo(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeCommand

	m.commandInput.SetValue("project-repo")
	m.commandInput.CursorEnd()
	if got := stripANSI(m.renderCommandInputLine()); !strings.Contains(got, "<project> <owner/repo>") {
		t.Fatalf("expected project-repo command line to show project/repo hint, got %q", got)
	}

	m.commandInput.SetValue("project-repo clear")
	m.commandInput.CursorEnd()
	if got := stripANSI(m.renderCommandInputLine()); !strings.Contains(got, "<project>") {
		t.Fatalf("expected project-repo clear command line to show project hint, got %q", got)
	}

	m.commandInput.SetValue("project-repo s")
	m.commandInput.CursorEnd()
	if got := stripANSI(m.renderCommandInputLine()); strings.Contains(got, "<owner/repo>") {
		t.Fatalf("expected project-repo command line to drop repo hint while project input is in progress, got %q", got)
	}

	m.commandInput.SetValue("project-repo serein ")
	m.commandInput.CursorEnd()
	if got := stripANSI(m.renderCommandInputLine()); !strings.Contains(got, "<owner/repo>") {
		t.Fatalf("expected project-repo command line to show repo hint for the next empty slot, got %q", got)
	}

	m.commandInput.SetValue("project-repo serein a")
	m.commandInput.CursorEnd()
	if got := stripANSI(m.renderCommandInputLine()); strings.Contains(got, "<owner/repo>") {
		t.Fatalf("expected project-repo command line to drop repo hint once repo input begins, got %q", got)
	}
}

func TestRenderCommandOverlayKeepsLongProjectRepoSuggestionOnOneLine(t *testing.T) {
	m := New().(modelUI)
	m.commandInput.SetValue("project-repo")
	m.commandInput.CursorEnd()
	m.items = []imodel.Item{{Project: "inkubator"}}

	overlay := stripANSI(m.renderCommandOverlay(92))
	if strings.Contains(overlay, "project-repo clear\ninkubator") {
		t.Fatalf("expected long project-repo suggestion to truncate instead of wrapping, got %q", overlay)
	}
	maxWidth := 0
	for _, line := range strings.Split(overlay, "\n") {
		if w := lipgloss.Width(line); w > maxWidth {
			maxWidth = w
		}
	}
	if maxWidth < 40 {
		t.Fatalf("expected wider command overlay, got max width %d in %q", maxWidth, overlay)
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

func TestCommandBackspaceKeepsPaletteOpenWhenInputBecomesEmpty(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeCommand
	m.commandInput.SetValue("q")
	m.commandInput.CursorEnd()
	m.commandInput.Focus()

	updated, _ := m.updateCommand(tea.KeyMsg{Type: tea.KeyBackspace})
	got := updated.(modelUI)
	if got.mode != modeCommand {
		t.Fatalf("mode = %v, want %v", got.mode, modeCommand)
	}
	if got.commandInput.Value() != "" {
		t.Fatalf("commandInput = %q, want empty", got.commandInput.Value())
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

func TestCommandSecondBackspaceClosesAfterClearingInput(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeCommand
	m.commandInput.SetValue("q")
	m.commandInput.CursorEnd()
	m.commandInput.Focus()

	updated, _ := m.updateCommand(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(modelUI)
	if m.mode != modeCommand {
		t.Fatalf("mode after first backspace = %v, want %v", m.mode, modeCommand)
	}

	updated, _ = m.updateCommand(tea.KeyMsg{Type: tea.KeyBackspace})
	got := updated.(modelUI)
	if got.mode != modeNormal {
		t.Fatalf("mode after second backspace = %v, want %v", got.mode, modeNormal)
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

func TestUpdateNormalQOpensQuitConfirm(t *testing.T) {
	m := New().(modelUI)

	updated, _ := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	got := updated.(modelUI)
	if got.mode != modeConfirm {
		t.Fatalf("mode = %v, want %v", got.mode, modeConfirm)
	}
	if got.confirm == nil || got.confirm.action != confirmQuit {
		t.Fatalf("expected quit confirm state, got %#v", got.confirm)
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

func TestRunCommandReposEntersReposModal(t *testing.T) {
	m := New().(modelUI)

	model, _ := m.runCommand("repos")
	updated := model.(modelUI)
	if updated.mode != modeRepos {
		t.Fatalf("mode = %v, want %v", updated.mode, modeRepos)
	}
}

func TestGithubIssueURLUsesRemoteRepo(t *testing.T) {
	item := imodel.Item{
		Repo:        "owner/new-repo",
		SyncedRepo:  "owner/old-repo",
		IssueNumber: 42,
	}

	url, ok := githubIssueURL(item)
	if !ok {
		t.Fatal("expected github issue URL to be available")
	}
	if url != "https://github.com/owner/old-repo/issues/42" {
		t.Fatalf("url = %q, want %q", url, "https://github.com/owner/old-repo/issues/42")
	}
}

func TestRunCommandOpenWarnsForUnsyncedItem(t *testing.T) {
	m := New().(modelUI)
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	m.items = []imodel.Item{{
		Title:     "Local only",
		Project:   "project",
		Type:      imodel.TypeFeature,
		Stage:     imodel.StageActive,
		UpdatedAt: now,
		CreatedAt: now,
	}}
	m.rebuildFiltered()

	model, cmd := m.runCommand("open")
	if cmd != nil {
		t.Fatal("expected no command for unsynced item")
	}
	updated := model.(modelUI)
	if !strings.Contains(updated.statusMessage, "not on GitHub yet") {
		t.Fatalf("statusMessage = %q, want unsynced warning", updated.statusMessage)
	}
}

func TestRunCommandOpenStartsBrowserForSyncedItem(t *testing.T) {
	m := New().(modelUI)
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	m.items = []imodel.Item{{
		Title:       "Synced",
		Project:     "project",
		Type:        imodel.TypeFeature,
		Stage:       imodel.StageActive,
		UpdatedAt:   now,
		CreatedAt:   now,
		Repo:        "owner/project",
		SyncedRepo:  "owner/project",
		IssueNumber: 7,
	}}
	m.rebuildFiltered()

	var opened string
	prevOpenURLFn := openURLFn
	openURLFn = func(url string) error {
		opened = url
		return nil
	}
	defer func() { openURLFn = prevOpenURLFn }()

	model, cmd := m.runCommand("open")
	if cmd == nil {
		t.Fatal("expected open command")
	}
	updated := model.(modelUI)
	if updated.statusKind != statusLoading {
		t.Fatalf("statusKind = %v, want loading", updated.statusKind)
	}

	msg := cmd().(openURLResultMsg)
	if opened != "https://github.com/owner/project/issues/7" {
		t.Fatalf("opened = %q, want issue URL", opened)
	}
	finished := updated.finishOpenURL(msg).(modelUI)
	if !strings.Contains(finished.statusMessage, "Opened issue on GitHub") {
		t.Fatalf("statusMessage = %q, want open success", finished.statusMessage)
	}
}

func TestWithStatusStripsTrailingPeriods(t *testing.T) {
	m := New().(modelUI)

	success := m.setStatusSuccess("Opened issue on GitHub.").(modelUI)
	if success.statusMessage != "Opened issue on GitHub" {
		t.Fatalf("statusMessage = %q, want trailing period stripped", success.statusMessage)
	}

	loading := m.setStatusLoading("Syncing GitHub issues...").(modelUI)
	if loading.statusMessage != "Syncing GitHub issues" {
		t.Fatalf("statusMessage = %q, want trailing dots stripped", loading.statusMessage)
	}
}

func TestRenderHeaderShowsSpinnerForLoadingStatus(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeNormal
	m = m.setStatusLoading("Syncing GitHub issues").(modelUI)

	header := stripANSI(m.renderHeader())
	if !strings.Contains(header, "⠋ Syncing GitHub issues") {
		t.Fatalf("expected spinner in loading header, got %q", header)
	}
}

func TestStatusSpinnerTickAdvancesFrame(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeNormal
	m = m.setStatusLoading("Syncing GitHub issues").(modelUI)

	updated, cmd := m.Update(statusSpinnerTickMsg(time.Now()))
	if cmd == nil {
		t.Fatal("expected spinner tick to schedule another tick")
	}
	got := updated.(modelUI)
	if got.statusSpinnerFrame != 1 {
		t.Fatalf("statusSpinnerFrame = %d, want 1", got.statusSpinnerFrame)
	}
	header := stripANSI(got.renderHeader())
	if !strings.Contains(header, "⠙ Syncing GitHub issues") {
		t.Fatalf("expected advanced spinner frame in header, got %q", header)
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

func TestRunDensityCommandPersistsChoice(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	manager, err := config.NewManager()
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	dataFile := filepath.Join(t.TempDir(), "items.json")
	m := New().(modelUI)
	m.configManager = manager
	m.store = storage.NewJSONStore(dataFile)
	m.config = config.AppConfig{
		StorageMode: config.ModeLocal,
		DataFile:    dataFile,
		Density:     densityComfortable.String(),
	}
	m.listDensity = densityComfortable

	updated := m.runDensityCommand("compact").(modelUI)
	if updated.listDensity != densityCompact {
		t.Fatalf("listDensity = %v, want %v", updated.listDensity, densityCompact)
	}

	cfg, ok, err := manager.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok {
		t.Fatal("expected density change to persist config")
	}
	if cfg.Density != "compact" {
		t.Fatalf("Density = %q, want %q", cfg.Density, "compact")
	}
}

func TestRunProjectLabelCommandPersistsChoice(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	manager, err := config.NewManager()
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	dataFile := filepath.Join(t.TempDir(), "items.json")
	m := New().(modelUI)
	m.configManager = manager
	m.store = storage.NewJSONStore(dataFile)
	m.config = config.AppConfig{
		StorageMode:      config.ModeGitHub,
		Repo:             "aloglu/triage-inbox",
		DataFile:         dataFile,
		Density:          densityComfortable.String(),
		ProjectLabelSync: config.ProjectLabelAuto,
	}

	updated := m.runProjectLabelCommand("never").(modelUI)
	if updated.config.ProjectLabelSync != config.ProjectLabelNever {
		t.Fatalf("ProjectLabelSync = %q, want %q", updated.config.ProjectLabelSync, config.ProjectLabelNever)
	}

	cfg, ok, err := manager.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok {
		t.Fatal("expected project-label change to persist config")
	}
	if cfg.ProjectLabelSync != config.ProjectLabelNever {
		t.Fatalf("ProjectLabelSync = %q, want %q", cfg.ProjectLabelSync, config.ProjectLabelNever)
	}
}

func TestRunProjectRepoCommandPersistsChoice(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	manager, err := config.NewManager()
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	dataFile := filepath.Join(t.TempDir(), "items.json")
	m := New().(modelUI)
	m.configManager = manager
	m.store = storage.NewJSONStore(dataFile)
	m.config = config.AppConfig{
		StorageMode: config.ModeGitHub,
		Repo:        "aloglu/triage-inbox",
		DataFile:    dataFile,
	}

	updated := m.runProjectRepoCommand("serein owner/serein").(modelUI)
	if updated.config.ProjectRepos["serein"] != "owner/serein" {
		t.Fatalf("ProjectRepos[\"serein\"] = %q, want %q", updated.config.ProjectRepos["serein"], "owner/serein")
	}

	cfg, ok, err := manager.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok {
		t.Fatal("expected project repo change to persist config")
	}
	if cfg.ProjectRepos["serein"] != "owner/serein" {
		t.Fatalf("ProjectRepos[\"serein\"] = %q, want %q", cfg.ProjectRepos["serein"], "owner/serein")
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

func TestEscInCleanEditExitsWithoutConfirm(t *testing.T) {
	m := New().(modelUI)
	entered, _ := m.enterEdit(-1)
	m = entered.(modelUI)

	updated, _ := m.updateEdit(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(modelUI)
	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want %v", got.mode, modeNormal)
	}
	if got.confirm != nil {
		t.Fatalf("expected no confirm, got %#v", got.confirm)
	}
	if got.statusMessage != "Edit cancelled" {
		t.Fatalf("status = %q, want %q", got.statusMessage, "Edit cancelled")
	}
}

func TestEscInDirtyNewEditPromptsForDiscard(t *testing.T) {
	m := New().(modelUI)
	entered, _ := m.enterEdit(-1)
	m = entered.(modelUI)

	updated, _ := m.updateEdit(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(modelUI)
	updated, _ = m.updateEdit(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(modelUI)
	if got.mode != modeConfirm {
		t.Fatalf("mode = %v, want %v", got.mode, modeConfirm)
	}
	if got.confirm == nil || got.confirm.action != confirmDiscardEdit {
		t.Fatalf("expected discard confirm, got %#v", got.confirm)
	}
}

func TestDiscardConfirmCancelReturnsToEdit(t *testing.T) {
	m := New().(modelUI)
	entered, _ := m.enterEdit(-1)
	m = entered.(modelUI)
	m.form.titleInput.SetValue("Draft")
	m.confirm = &confirmState{action: confirmDiscardEdit}
	m.mode = modeConfirm

	updated, _ := m.updateConfirm(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(modelUI)
	if got.mode != modeEdit {
		t.Fatalf("mode = %v, want %v", got.mode, modeEdit)
	}
	if got.confirm != nil {
		t.Fatalf("expected confirm to clear, got %#v", got.confirm)
	}
	if got.form.titleInput.Value() != "Draft" {
		t.Fatalf("title = %q, want %q", got.form.titleInput.Value(), "Draft")
	}
}

func TestDiscardConfirmEnterDiscardsEdit(t *testing.T) {
	m := New().(modelUI)
	entered, _ := m.enterEdit(-1)
	m = entered.(modelUI)
	m.form.titleInput.SetValue("Draft")
	m.confirm = &confirmState{action: confirmDiscardEdit}
	m.mode = modeConfirm

	updated, _ := m.updateConfirm(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(modelUI)
	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want %v", got.mode, modeNormal)
	}
	if got.confirm != nil {
		t.Fatalf("expected confirm to clear, got %#v", got.confirm)
	}
	if got.statusMessage != "Edit cancelled" {
		t.Fatalf("status = %q, want %q", got.statusMessage, "Edit cancelled")
	}
}

func TestEscAfterChangingEditStagePromptsForDiscard(t *testing.T) {
	now := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	m := New().(modelUI)
	m.items = []imodel.Item{{
		Title:     "Existing",
		Project:   "project",
		Type:      imodel.TypeFeature,
		Stage:     imodel.StageIdea,
		CreatedAt: now,
		UpdatedAt: now,
	}}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	entered, _ := m.enterEdit(0)
	m = entered.(modelUI)
	m.form.stageIndex = 1

	updated, _ := m.updateEdit(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(modelUI)
	if got.mode != modeConfirm {
		t.Fatalf("mode = %v, want %v", got.mode, modeConfirm)
	}
	if got.confirm == nil || got.confirm.action != confirmDiscardEdit {
		t.Fatalf("expected discard confirm, got %#v", got.confirm)
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
	if updated.statusMessage != "Export is only available in local mode" {
		t.Fatalf("unexpected export mode status: %q", updated.statusMessage)
	}
}

func TestRunImportCommandEntersConfirmModeAndImports(t *testing.T) {
	m := New().(modelUI)
	store := storage.NewJSONStore(filepath.Join(t.TempDir(), "items.json"))
	m.store = store
	m.configManager = nil
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

	importedModel, _ := prompt.confirmActionNow()
	imported := importedModel.(modelUI)
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
	if updated.statusMessage != "Import is only available in local mode" {
		t.Fatalf("unexpected import mode status: %q", updated.statusMessage)
	}
}

func TestRenderCommandInputLineShowsHintsForDrafts(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeCommand

	m.commandInput.SetValue("drafts")
	m.commandInput.CursorEnd()
	if got := stripANSI(m.renderCommandInputLine()); !strings.Contains(got, "show|reset|folder <path>") {
		t.Fatalf("expected drafts command line to show drafts hint, got %q", got)
	}

	m.commandInput.SetValue("drafts folder ")
	m.commandInput.CursorEnd()
	if got := stripANSI(m.renderCommandInputLine()); !strings.Contains(got, "<path>") {
		t.Fatalf("expected drafts folder command line to show path hint, got %q", got)
	}

	m.commandInput.SetValue("drafts folder /tmp")
	m.commandInput.CursorEnd()
	if got := stripANSI(m.renderCommandInputLine()); strings.Contains(got, "<path>") {
		t.Fatalf("expected drafts folder command line to drop path hint once typing begins, got %q", got)
	}
}

func TestRunDraftsCommandImportsAndProcessesFilesInLocalMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	m := New().(modelUI)
	store := storage.NewJSONStore(filepath.Join(t.TempDir(), "items.json"))
	draftsDir := filepath.Join(t.TempDir(), "drafts")
	m.store = store
	m.config = config.Normalize(config.AppConfig{
		StorageMode:  config.ModeLocal,
		DataFile:     store.Path(),
		DraftsFolder: draftsDir,
	})

	raw := "```yaml\ntitle: Drafted issue\nproject: serein\ntype: bug\nstage: active\n```\n\nBody text\n"
	source := filepath.Join(draftsDir, "draft.md")
	if err := os.MkdirAll(draftsDir, 0o700); err != nil {
		t.Fatalf("mkdir drafts dir: %v", err)
	}
	if err := os.WriteFile(source, []byte(raw), 0o600); err != nil {
		t.Fatalf("write draft file: %v", err)
	}

	updated := m.runDraftsCommand("").(modelUI)
	if len(updated.items) != 1 {
		t.Fatalf("items length = %d, want 1", len(updated.items))
	}
	if updated.items[0].Title != "Drafted issue" {
		t.Fatalf("title = %q", updated.items[0].Title)
	}
	if updated.items[0].PendingSync != imodel.SyncNone {
		t.Fatalf("pending sync = %q, want none", updated.items[0].PendingSync)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("expected source draft to be moved, stat err = %v", err)
	}
	processed := filepath.Join(draftsDir, "processed", "draft.md")
	if _, err := os.Stat(processed); err != nil {
		t.Fatalf("expected processed draft to exist: %v", err)
	}
	if updated.statusMessage != "Imported 1 drafts" {
		t.Fatalf("unexpected drafts status: %q", updated.statusMessage)
	}
}

func TestRunDraftsCommandImportsPendingCreatesInGitHubMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	m := New().(modelUI)
	store := storage.NewJSONStore(filepath.Join(t.TempDir(), "items.json"))
	draftsDir := filepath.Join(t.TempDir(), "drafts")
	m.store = store
	m.config = config.Normalize(config.AppConfig{
		StorageMode:  config.ModeGitHub,
		Repo:         "aloglu/triage-inbox",
		DataFile:     store.Path(),
		DraftsFolder: draftsDir,
	})

	raw := "```yaml\ntitle: Drafted issue\nproject: serein\ntype: feature\nstage: idea\n```\n\nBody text\n"
	if err := os.MkdirAll(draftsDir, 0o700); err != nil {
		t.Fatalf("mkdir drafts dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(draftsDir, "draft.md"), []byte(raw), 0o600); err != nil {
		t.Fatalf("write draft file: %v", err)
	}

	updated := m.runDraftsCommand("").(modelUI)
	if len(updated.items) != 1 {
		t.Fatalf("items length = %d, want 1", len(updated.items))
	}
	if updated.items[0].PendingSync != imodel.SyncCreate {
		t.Fatalf("pending sync = %q, want %q", updated.items[0].PendingSync, imodel.SyncCreate)
	}
	if updated.items[0].Repo != "aloglu/triage-inbox" {
		t.Fatalf("repo = %q, want %q", updated.items[0].Repo, "aloglu/triage-inbox")
	}
}

func TestRunDraftsFolderCommandPersistsCustomPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	manager, err := config.NewManager()
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	store := storage.NewJSONStore(filepath.Join(t.TempDir(), "items.json"))
	m := New().(modelUI)
	m.store = store
	m.configManager = manager
	m.config = config.Normalize(config.AppConfig{
		StorageMode: config.ModeLocal,
		DataFile:    store.Path(),
	})

	want := filepath.Join(t.TempDir(), "my drafts")
	updated := m.runDraftsCommand("folder " + want).(modelUI)
	if updated.config.DraftsFolder != filepath.Clean(want) {
		t.Fatalf("DraftsFolder = %q, want %q", updated.config.DraftsFolder, filepath.Clean(want))
	}

	loaded, ok, err := manager.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok {
		t.Fatal("expected saved config to exist")
	}
	if loaded.DraftsFolder != filepath.Clean(want) {
		t.Fatalf("saved DraftsFolder = %q, want %q", loaded.DraftsFolder, filepath.Clean(want))
	}
}

func TestUpdateConfirmEnterPerformsImport(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	store := storage.NewJSONStore(filepath.Join(t.TempDir(), "items.json"))
	m := New().(modelUI)
	m.store = store
	m.config = config.AppConfig{StorageMode: config.ModeLocal, DataFile: store.Path()}
	m.mode = modeConfirm
	m.confirm = &confirmState{
		action:     confirmImport,
		importPath: "/tmp/in.json",
		importItems: []imodel.Item{{
			Title:     "Imported",
			Project:   "project",
			Type:      imodel.TypeFeature,
			Stage:     imodel.StageActive,
			CreatedAt: now,
			UpdatedAt: now,
		}},
	}

	updated, _ := m.updateConfirm(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(modelUI)
	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want %v", got.mode, modeNormal)
	}
	if len(got.items) != 1 || got.items[0].Title != "Imported" {
		t.Fatalf("unexpected imported items: %+v", got.items)
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

	deletedModel, _ := m.runDeleteCommand()
	deleted := deletedModel.(modelUI)
	if !deleted.items[0].Trashed {
		t.Fatalf("expected item to be trashed after delete")
	}

	trashedView := deleted.runViewCommand([]string{"trash"}).(modelUI)
	if len(trashedView.filtered) != 1 {
		t.Fatalf("expected trashed item to appear in trash view")
	}

	restoredModel, _ := trashedView.runRestoreCommand()
	restored := restoredModel.(modelUI)
	if restored.items[0].Trashed {
		t.Fatalf("expected item to be restored from trash")
	}

	retrashed := restored.runViewCommand([]string{"all"}).(modelUI)
	retrashedModel, _ := retrashed.runDeleteCommand()
	retrashed = retrashedModel.(modelUI)
	retrashed = retrashed.runViewCommand([]string{"trash"}).(modelUI)
	purgePrompt := retrashed.runPurgeCommand().(modelUI)
	if purgePrompt.mode != modeConfirm || purgePrompt.confirm == nil {
		t.Fatalf("expected purge to enter confirm mode")
	}

	purgedModel, _ := purgePrompt.confirmActionNow()
	purged := purgedModel.(modelUI)
	if len(purged.items) != 0 {
		t.Fatalf("expected purge to remove item permanently")
	}
}

func TestUpdateConfirmEnterPerformsPurge(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	store := storage.NewJSONStore(filepath.Join(t.TempDir(), "items.json"))
	m := New().(modelUI)
	m.store = store
	m.config = config.AppConfig{StorageMode: config.ModeLocal, DataFile: store.Path()}
	m.mode = modeConfirm
	m.items = []imodel.Item{{
		Title:     "Trash me",
		Project:   "project",
		Type:      imodel.TypeFeature,
		Stage:     imodel.StageActive,
		Trashed:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}}
	m.confirm = &confirmState{
		action:    confirmPurge,
		itemIndex: 0,
	}

	updated, _ := m.updateConfirm(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(modelUI)
	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want %v", got.mode, modeNormal)
	}
	if len(got.items) != 0 {
		t.Fatalf("expected purge to remove item permanently")
	}
}

func TestRunDeleteCommandInGitHubModeQueuesLocalSync(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	store := storage.NewJSONStore(filepath.Join(t.TempDir(), "items.json"))
	m := New().(modelUI)
	m.store = store
	m.configManager = nil
	m.config = config.AppConfig{StorageMode: config.ModeGitHub, Repo: "aloglu/triage-inbox", DataFile: store.Path()}
	m.githubClient = githubsync.NewClient()
	m.items = []imodel.Item{
		{Title: "Delete me", Project: "project", Type: imodel.TypeFeature, Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now, Repo: "aloglu/triage-inbox", SyncedRepo: "aloglu/triage-inbox", IssueNumber: 7},
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	updated, cmd := m.runDeleteCommand()
	got := updated.(modelUI)
	if cmd != nil {
		t.Fatal("expected delete command to return no background command")
	}
	if !got.items[0].Trashed {
		t.Fatal("expected item to be marked trashed locally")
	}
	if got.items[0].PendingSync != imodel.SyncDelete {
		t.Fatalf("PendingSync = %q, want %q", got.items[0].PendingSync, imodel.SyncDelete)
	}
	if !strings.Contains(got.statusMessage, "Press S to sync") {
		t.Fatalf("statusMessage = %q, want local queue message", got.statusMessage)
	}
}

func TestRunSyncCommandOpensReviewWhenPending(t *testing.T) {
	m := New().(modelUI)
	m.config = config.AppConfig{StorageMode: config.ModeGitHub, Repo: "aloglu/triage-inbox"}
	m.items = []imodel.Item{{
		Title:       "Queued",
		Project:     "project",
		Type:        imodel.TypeFeature,
		Stage:       imodel.StageActive,
		Repo:        "aloglu/triage-inbox",
		PendingSync: imodel.SyncUpdate,
	}}

	updated, cmd := m.runSyncCommand()
	if cmd != nil {
		t.Fatal("expected sync review to open before running a command")
	}
	got := updated.(modelUI)
	if got.mode != modeConfirm || got.confirm == nil || got.confirm.action != confirmSync {
		t.Fatalf("expected sync review confirm, got mode=%v confirm=%#v", got.mode, got.confirm)
	}
}

func TestUpdateConfirmScrollsSyncReview(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 14
	m.mode = modeConfirm
	m.confirm = &confirmState{action: confirmSync}
	for idx := 0; idx < 12; idx++ {
		m.items = append(m.items, imodel.Item{
			Title:       fmt.Sprintf("Queued %02d", idx),
			Project:     "project",
			Type:        imodel.TypeFeature,
			Stage:       imodel.StageActive,
			Repo:        "aloglu/triage-inbox",
			PendingSync: imodel.SyncUpdate,
		})
	}

	rendered := stripANSI(m.renderConfirmModal())
	if !strings.Contains(rendered, "(S)ync") || !strings.Contains(rendered, "(C)ancel") {
		t.Fatalf("expected sync review buttons to remain visible, got %q", rendered)
	}

	updated, _ := m.updateConfirm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	got := updated.(modelUI)
	if got.detailScroll == 0 {
		t.Fatalf("expected sync review to scroll, detailScroll=%d", got.detailScroll)
	}
}

func TestPendingSyncReviewLinesShowBulletedTitlesAndDetails(t *testing.T) {
	m := New().(modelUI)
	m.items = []imodel.Item{{
		Title:       "TTS Issues",
		Project:     "serein",
		Type:        imodel.TypeBug,
		Stage:       imodel.StageActive,
		Repo:        "aloglu/triage-inbox",
		PendingSync: imodel.SyncUpdate,
	}}

	lines := m.pendingSyncReviewLines(64)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "• TTS Issues") {
		t.Fatalf("expected bulleted sync review title, got %q", joined)
	}
	if !strings.Contains(joined, "update  aloglu/triage-inbox") {
		t.Fatalf("expected sync review details, got %q", joined)
	}
}

func TestSaveFormInGitHubModeQueuesLocalSave(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	store := storage.NewJSONStore(filepath.Join(t.TempDir(), "items.json"))
	m := New().(modelUI)
	m.store = store
	m.configManager = nil
	m.config = config.AppConfig{StorageMode: config.ModeGitHub, Repo: "aloglu/triage-inbox", DataFile: store.Path()}
	m.items = []imodel.Item{{
		Title:           "Existing",
		Project:         "project",
		Type:            imodel.TypeFeature,
		Stage:           imodel.StageActive,
		Body:            "before",
		CreatedAt:       now.Add(-time.Hour),
		UpdatedAt:       now.Add(-time.Hour),
		RemoteUpdatedAt: now.Add(-time.Hour),
		IssueNumber:     7,
		Repo:            "aloglu/triage-inbox",
		SyncedRepo:      "aloglu/triage-inbox",
	}}
	m.form.isNew = false
	m.form.editingIndex = 0
	m.form.titleInput.SetValue("Existing")
	m.form.projectInput.SetValue("project")
	m.form.repoInput.SetValue("aloglu/triage-inbox")
	m.form.bodyInput.SetValue("after")
	m.form.typeIndex = 0
	m.form.stageIndex = 2

	updated, cmd := m.saveForm()
	if cmd != nil {
		t.Fatal("expected GitHub-mode save to stay local")
	}
	got := updated.(modelUI)
	if got.items[0].PendingSync != imodel.SyncUpdate {
		t.Fatalf("PendingSync = %q, want %q", got.items[0].PendingSync, imodel.SyncUpdate)
	}
	if got.items[0].Body != "after" {
		t.Fatalf("Body = %q, want %q", got.items[0].Body, "after")
	}
	if !strings.Contains(got.statusMessage, "Saved locally") {
		t.Fatalf("statusMessage = %q, want local save message", got.statusMessage)
	}
}

func TestSelectItemUsesRemoteIdentityBeforeTitle(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	target := imodel.Item{
		Title:           "Duplicate",
		Project:         "inkubator",
		Type:            imodel.TypeBug,
		Stage:           imodel.StagePlanned,
		CreatedAt:       now.Add(time.Minute),
		UpdatedAt:       now.Add(time.Minute),
		RemoteUpdatedAt: now.Add(time.Minute),
		IssueNumber:     11,
		Repo:            "aloglu/serein",
		SyncedRepo:      "aloglu/serein",
	}
	m := New().(modelUI)
	m.items = []imodel.Item{
		{
			Title:           "Duplicate",
			Project:         "serein",
			Type:            imodel.TypeFeature,
			Stage:           imodel.StageActive,
			CreatedAt:       now,
			UpdatedAt:       now,
			RemoteUpdatedAt: now,
			IssueNumber:     7,
			Repo:            "aloglu/triage-inbox",
			SyncedRepo:      "aloglu/triage-inbox",
		},
		target,
	}
	m.rebuildFiltered()

	m.selectItem(target)
	selectedIndex, ok := m.selectedItemIndex()
	if !ok {
		t.Fatal("expected selected item")
	}
	if got := m.items[selectedIndex].IssueNumber; got != 11 {
		t.Fatalf("selected issue = %d, want %d", got, 11)
	}
}

func TestRunUndoCommandRevertsLocalTrash(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	store := storage.NewJSONStore(filepath.Join(t.TempDir(), "items.json"))
	m := New().(modelUI)
	m.store = store
	m.configManager = nil
	m.config = config.AppConfig{StorageMode: config.ModeGitHub, Repo: "aloglu/triage-inbox", DataFile: store.Path()}
	m.githubClient = githubsync.NewClient()
	m.items = []imodel.Item{
		{Title: "Delete me", Project: "project", Type: imodel.TypeFeature, Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now, Repo: "aloglu/triage-inbox", SyncedRepo: "aloglu/triage-inbox", IssueNumber: 7},
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	deleted, _ := m.runDeleteCommand()
	trashed := deleted.(modelUI)
	if trashed.undo == nil {
		t.Fatal("expected delete to capture undo state")
	}

	undone := trashed.runUndoCommand().(modelUI)
	if undone.items[0].Trashed {
		t.Fatal("expected undo to restore trashed item")
	}
	if undone.items[0].PendingSync != imodel.SyncNone {
		t.Fatalf("PendingSync = %q, want cleared", undone.items[0].PendingSync)
	}
	if !strings.Contains(undone.statusMessage, "Undid trash") {
		t.Fatalf("statusMessage = %q, want undo confirmation", undone.statusMessage)
	}
}

func TestRunDeleteCommandForUnsyncedCreateDoesNotQueueRemoteCreate(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	store := storage.NewJSONStore(filepath.Join(t.TempDir(), "items.json"))
	m := New().(modelUI)
	m.store = store
	m.configManager = nil
	m.config = config.AppConfig{StorageMode: config.ModeGitHub, Repo: "aloglu/triage-inbox", DataFile: store.Path()}
	m.items = []imodel.Item{
		{
			Title:       "Draft issue",
			Project:     "project",
			Type:        imodel.TypeFeature,
			Stage:       imodel.StageActive,
			CreatedAt:   now,
			UpdatedAt:   now,
			Repo:        "aloglu/triage-inbox",
			PendingSync: imodel.SyncCreate,
		},
	}
	m.rebuildFiltered()

	deleted, _ := m.runDeleteCommand()
	got := deleted.(modelUI)
	if !got.items[0].Trashed {
		t.Fatal("expected item to be trashed")
	}
	if got.items[0].PendingSync != imodel.SyncNone {
		t.Fatalf("PendingSync = %q, want cleared for unsynced trashed item", got.items[0].PendingSync)
	}
}

func TestRunUndoCommandRevertsNewItemCreation(t *testing.T) {
	store := storage.NewJSONStore(filepath.Join(t.TempDir(), "items.json"))
	m := New().(modelUI)
	m.store = store
	m.configManager = nil
	m.config = config.AppConfig{StorageMode: config.ModeLocal, DataFile: store.Path()}
	m.items = nil
	m.rebuildFiltered()
	m.form.isNew = true
	m.form.titleInput.SetValue("New item")
	m.form.projectInput.SetValue("project")
	m.form.repoInput.SetValue("")
	m.form.bodyInput.SetValue("body")
	m.form.typeIndex = 0
	m.form.stageIndex = 2

	saved, cmd := m.saveForm()
	if cmd != nil {
		t.Fatal("expected local save to complete without background command")
	}
	created := saved.(modelUI)
	if len(created.items) != 1 {
		t.Fatalf("expected created item, got %d items", len(created.items))
	}
	if created.undo == nil {
		t.Fatal("expected create to capture undo state")
	}

	undone := created.runUndoCommand().(modelUI)
	if len(undone.items) != 0 {
		t.Fatalf("expected undo to remove created item, got %d items", len(undone.items))
	}
	if !strings.Contains(undone.statusMessage, "Undid create") {
		t.Fatalf("statusMessage = %q, want undo confirmation", undone.statusMessage)
	}
}

func TestPerformBatchSyncClearsUndoState(t *testing.T) {
	m := New().(modelUI)
	m.config = config.AppConfig{StorageMode: config.ModeGitHub, Repo: "aloglu/triage-inbox"}
	m.githubClient = githubsync.NewClient()
	m.mode = modeConfirm
	m.confirm = &confirmState{action: confirmSync}
	m.items = []imodel.Item{{
		Title:       "Queued",
		Project:     "project",
		Type:        imodel.TypeFeature,
		Stage:       imodel.StageActive,
		Repo:        "aloglu/triage-inbox",
		PendingSync: imodel.SyncUpdate,
	}}
	m.undo = &undoState{label: "edit", items: cloneItems(m.items)}

	updated, cmd := m.performBatchSync()
	if cmd == nil {
		t.Fatal("expected batch sync command")
	}
	got := updated.(modelUI)
	if got.undo != nil {
		t.Fatal("expected batch sync to clear undo state")
	}
}

func TestRecordSuccessfulSyncDoesNotRequireItemStoreSave(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	manager, err := config.NewManager()
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	m := New().(modelUI)
	m.configManager = manager
	m.config = config.AppConfig{
		StorageMode: config.ModeGitHub,
		Repo:        "aloglu/triage-inbox",
		DataFile:    filepath.Join(t.TempDir(), "items.json"),
	}

	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	if err := m.recordSuccessfulSync(now); err != nil {
		t.Fatalf("recordSuccessfulSync() error = %v", err)
	}

	loaded, ok, err := manager.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok {
		t.Fatal("expected saved config")
	}
	if !loaded.LastSuccessfulSyncAt.Equal(now) {
		t.Fatalf("LastSuccessfulSyncAt = %v, want %v", loaded.LastSuccessfulSyncAt, now)
	}
}

func TestResolveConflictByOverwritingStartsLoadingAction(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	m := New().(modelUI)
	m.githubClient = githubsync.NewClient()
	m.mode = modeConflict
	m.conflict = &conflictState{
		local: imodel.Item{
			Title:       "Conflict",
			Project:     "project",
			Type:        imodel.TypeFeature,
			Stage:       imodel.StageActive,
			UpdatedAt:   now,
			CreatedAt:   now,
			Repo:        "aloglu/triage-inbox",
			IssueNumber: 7,
		},
	}

	updated, cmd := m.resolveConflictByOverwriting()
	got := updated.(modelUI)
	if cmd == nil {
		t.Fatal("expected conflict overwrite to return a background command")
	}
	if !got.saveInFlight {
		t.Fatal("expected conflict overwrite to mark save in flight")
	}
	if got.statusKind != statusLoading {
		t.Fatalf("statusKind = %v, want loading", got.statusKind)
	}
	if !strings.Contains(got.statusMessage, "Overwriting item in") {
		t.Fatalf("statusMessage = %q, want loading overwrite message", got.statusMessage)
	}
}

func TestRenderFooterShowsIdleHintAndMetadata(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeNormal
	m.config.StorageMode = config.ModeGitHub
	m.config.LastSuccessfulSyncAt = time.Now().Add(-2 * time.Minute)
	m.statusMessage = ""
	m.statusUntil = time.Time{}

	idle := stripANSI(m.renderFooter())
	if !strings.Contains(idle, ": command") {
		t.Fatalf("expected idle footer hint, got %q", idle)
	}
	if !strings.Contains(idle, "updated desc") || !strings.Contains(idle, "mode github") {
		t.Fatalf("expected footer metadata, got %q", idle)
	}
}

func TestRenderHeaderShowsProjectAndViewContext(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.projectFilter = "serein"
	m.viewMode = viewArchive

	header := stripANSI(m.renderHeader())
	if !strings.Contains(header, "project: serein") {
		t.Fatalf("expected project context in header, got %q", header)
	}
	if !strings.Contains(header, "view: archive") {
		t.Fatalf("expected view context in header, got %q", header)
	}
}

func TestRenderShortcutsModalMovesMoreCommandsToBottomSection(t *testing.T) {
	m := New().(modelUI)
	lines := m.shortcutsModalLines()
	modal := strings.Join(lines, "\n")

	commandIdx := strings.Index(modal, "Command")
	moreCommandsIdx := strings.Index(modal, "More Commands")
	stageIdx := strings.Index(modal, ":stage")

	if commandIdx == -1 || moreCommandsIdx == -1 || stageIdx == -1 {
		t.Fatalf("expected shortcuts modal to contain command and more-commands sections, got %q", modal)
	}
	if moreCommandsIdx <= commandIdx {
		t.Fatalf("expected More Commands section after Command section, got %q", modal)
	}
	if stageIdx <= moreCommandsIdx {
		t.Fatalf("expected :stage inside More Commands section, got %q", modal)
	}
	if strings.Contains(modal, ":stage active") {
		t.Fatalf("expected example-style stage command to be removed, got %q", modal)
	}
	if strings.Contains(modal, "esc              close panel") {
		t.Fatalf("expected old esc close row to be removed, got %q", modal)
	}
	if strings.Contains(modal, "esc close") {
		t.Fatalf("expected shortcuts modal to omit esc close hint, got %q", modal)
	}
}

func TestRenderReposModalShowsDefaultProjectsAndTrackedRepos(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeRepos
	m.config.StorageMode = config.ModeGitHub
	m.config.Repo = "aloglu/triage-inbox"
	m.config.ProjectRepos = map[string]string{"serein": "owner/serein"}
	m.items = []imodel.Item{
		{Title: "One", Project: "inkubator", Repo: "aloglu/triage-inbox"},
		{Title: "Two", Project: "serein", Repo: "owner/serein"},
	}

	rendered := stripANSI(m.renderReposModal())
	if !strings.Contains(rendered, "Default") || !strings.Contains(rendered, "aloglu/triage-inbox") {
		t.Fatalf("expected default repo section, got %q", rendered)
	}
	if !strings.Contains(rendered, "Project Defaults") ||
		!strings.Contains(rendered, "serein ->") ||
		!strings.Contains(rendered, "inkubator ->") ||
		!strings.Contains(rendered, "mapped") ||
		!strings.Contains(rendered, "fallback") ||
		!strings.Contains(rendered, "(owner/serein)") ||
		!strings.Contains(rendered, "(aloglu/triage-inbox)") {
		t.Fatalf("expected project repo section, got %q", rendered)
	}
	if !strings.Contains(rendered, "Tracked Repos") || !strings.Contains(rendered, "owner/serein") {
		t.Fatalf("expected tracked repos section, got %q", rendered)
	}
}

func TestShortcutsModalScrollsToExamples(t *testing.T) {
	m := New().(modelUI)
	m.width = 120
	m.height = 18

	initial := stripANSI(m.renderShortcutsModal())
	if strings.Contains(initial, ":export json") {
		t.Fatalf("expected export command to start below the initial viewport")
	}

	for i := 0; i < 40; i++ {
		updated, _ := m.updateShortcuts(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(modelUI)
	}

	scrolled := stripANSI(m.renderShortcutsModal())
	if !strings.Contains(scrolled, ":export json") {
		t.Fatalf("expected export command after scrolling, got %q", scrolled)
	}
	for i := 0; i < 12; i++ {
		updated, _ := m.updateShortcuts(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(modelUI)
	}

	scrolled = stripANSI(m.renderShortcutsModal())
	if !strings.Contains(scrolled, ":import json") {
		t.Fatalf("expected import command after scrolling, got %q", scrolled)
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

func TestMouseClickChangesFocusedPane(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeNormal

	itemsRect, detailsRect := m.mainPaneRects()
	if m.focus != focusItems {
		t.Fatalf("initial focus = %v, want %v", m.focus, focusItems)
	}

	updated, _ := m.Update(tea.MouseMsg{
		X:      detailsRect.x + 1,
		Y:      detailsRect.y + 1,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	m = updated.(modelUI)
	if m.focus != focusDetails {
		t.Fatalf("focus after details click = %v, want %v", m.focus, focusDetails)
	}

	updated, _ = m.Update(tea.MouseMsg{
		X:      itemsRect.x + 1,
		Y:      itemsRect.y + 1,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	m = updated.(modelUI)
	if m.focus != focusItems {
		t.Fatalf("focus after items click = %v, want %v", m.focus, focusItems)
	}
}

func TestMouseWheelUsesFocusedPane(t *testing.T) {
	m := New().(modelUI)
	m.width = 72
	m.height = 18
	m.mode = modeNormal
	for i := 0; i < 20; i++ {
		body := ""
		if i == 1 {
			body = strings.Repeat("This is a very long detail body line that should wrap heavily inside the details pane and force overflow.\n", 200)
		}
		m.items = append(m.items, imodel.Item{
			Title:   fmt.Sprintf("Item %02d", i),
			Project: "demo",
			Type:    imodel.TypeFeature,
			Stage:   imodel.StageIdea,
			Body:    body,
		})
	}
	m.filtered = make([]int, len(m.items))
	for i := range m.filtered {
		m.filtered[i] = i
	}
	m.focus = focusItems

	updated, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	m = updated.(modelUI)
	if m.selected == 0 {
		t.Fatalf("expected wheel down in items pane to move selection")
	}

	m.selected = 1
	m.focus = focusDetails
	m.detailScroll = 0
	updated, _ = m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	m = updated.(modelUI)
	if m.detailScroll == 0 {
		t.Fatalf("expected wheel down in details pane to scroll details")
	}
}

func TestFinishSyncShowsSuccessStatus(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.config.StorageMode = config.ModeGitHub
	m.config.Repo = "aloglu/triage-inbox"
	m.store = storage.NewJSONStore(filepath.Join(t.TempDir(), "items.json"))
	m.configManager = nil
	m.syncing = true

	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	updated := m.finishSync(syncResultMsg{
		repos: []string{"aloglu/triage-inbox"},
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
	if updated.config.LastSuccessfulSyncAt.IsZero() {
		t.Fatal("expected finishSync to record last successful sync time")
	}
}

func TestBeginEditDefaultsRepoToConfiguredGitHubRepo(t *testing.T) {
	m := New().(modelUI)
	m.config.StorageMode = config.ModeGitHub
	m.config.Repo = "aloglu/triage-inbox"

	m.beginEdit(-1)

	if got := m.form.repoInput.Value(); got != "aloglu/triage-inbox" {
		t.Fatalf("repoInput = %q, want %q", got, "aloglu/triage-inbox")
	}
}

func TestBeginEditDefaultsRepoToMappedProjectRepo(t *testing.T) {
	m := New().(modelUI)
	m.config.StorageMode = config.ModeGitHub
	m.config.Repo = "aloglu/triage-inbox"
	m.config.ProjectRepos = map[string]string{"serein": "owner/serein"}
	m.projectFilter = "serein"

	m.beginEdit(-1)

	if got := m.form.repoInput.Value(); got != "owner/serein" {
		t.Fatalf("repoInput = %q, want %q", got, "owner/serein")
	}
}

func TestBodyInputHasNoDefaultCharOrLineLimit(t *testing.T) {
	m := New().(modelUI)

	if got := m.form.bodyInput.CharLimit; got != 0 {
		t.Fatalf("bodyInput.CharLimit = %d, want 0", got)
	}
	if got := m.form.bodyInput.MaxHeight; got != 0 {
		t.Fatalf("bodyInput.MaxHeight = %d, want 0", got)
	}
}

func TestFocusFormFieldMovesCursorToEndOfFrontmatterValues(t *testing.T) {
	m := New().(modelUI)
	m.items = []imodel.Item{{
		Title:   "Showcase Responsiveness",
		Project: "inkubator",
		Repo:    "aloglu/triage-inbox",
		Type:    imodel.TypeFeature,
		Stage:   imodel.StagePlanned,
	}}
	m.beginEdit(0)

	m.form.focusIndex = 0
	focused, _ := m.focusFormField()
	got := focused.(modelUI)
	if got.form.titleInput.Position() != len([]rune(got.form.titleInput.Value())) {
		t.Fatalf("title cursor = %d, want %d", got.form.titleInput.Position(), len([]rune(got.form.titleInput.Value())))
	}

	got.form.focusIndex = 1
	focused, _ = got.focusFormField()
	got = focused.(modelUI)
	if got.form.projectInput.Position() != len([]rune(got.form.projectInput.Value())) {
		t.Fatalf("project cursor = %d, want %d", got.form.projectInput.Position(), len([]rune(got.form.projectInput.Value())))
	}

	got.form.focusIndex = 2
	focused, _ = got.focusFormField()
	got = focused.(modelUI)
	if got.form.repoInput.Position() != len([]rune(got.form.repoInput.Value())) {
		t.Fatalf("repo cursor = %d, want %d", got.form.repoInput.Position(), len([]rune(got.form.repoInput.Value())))
	}
}

func TestProjectEditUpdatesRepoWhenStillFollowingDefault(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeEdit
	m.config.StorageMode = config.ModeGitHub
	m.config.Repo = "aloglu/triage-inbox"
	m.config.ProjectRepos = map[string]string{
		"serein": "owner/serein",
	}
	m.form.focusIndex = 1
	m.form.projectInput.SetValue("serein")
	m.form.projectInput.CursorEnd()
	m.form.projectInput.Focus()
	m.form.repoInput.SetValue("owner/serein")

	updated, _ := m.updateEdit(tea.KeyMsg{Type: tea.KeyBackspace})
	got := updated.(modelUI)
	if got.form.projectInput.Value() != "serei" {
		t.Fatalf("projectInput = %q, want %q", got.form.projectInput.Value(), "serei")
	}
	if got.form.repoInput.Value() != "aloglu/triage-inbox" {
		t.Fatalf("repoInput = %q, want %q", got.form.repoInput.Value(), "aloglu/triage-inbox")
	}
}

func TestProjectEditKeepsRepoWhenCustomized(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeEdit
	m.config.StorageMode = config.ModeGitHub
	m.config.Repo = "aloglu/triage-inbox"
	m.config.ProjectRepos = map[string]string{
		"serein": "owner/serein",
	}
	m.form.focusIndex = 1
	m.form.projectInput.SetValue("serein")
	m.form.projectInput.CursorEnd()
	m.form.projectInput.Focus()
	m.form.repoInput.SetValue("owner/custom")

	updated, _ := m.updateEdit(tea.KeyMsg{Type: tea.KeyBackspace})
	got := updated.(modelUI)

	if got.form.repoInput.Value() != "owner/custom" {
		t.Fatalf("repoInput = %q, want custom repo to be preserved", got.form.repoInput.Value())
	}
}

func TestMergeSyncedItemsKeepsUnsyncedLocalItems(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	existing := []imodel.Item{
		{
			Title:       "Local draft",
			Project:     "drafts",
			Stage:       imodel.StagePlanned,
			CreatedAt:   now,
			UpdatedAt:   now,
			IssueNumber: 0,
			Repo:        "",
		},
		{
			Title:       "Remote cached",
			Project:     "project",
			Stage:       imodel.StageActive,
			CreatedAt:   now,
			UpdatedAt:   now,
			IssueNumber: 12,
			Repo:        "aloglu/triage-inbox",
		},
	}
	remote := []imodel.Item{{
		Title:       "Remote refreshed",
		Project:     "project",
		Stage:       imodel.StageBlocked,
		CreatedAt:   now,
		UpdatedAt:   now,
		IssueNumber: 12,
		Repo:        "aloglu/triage-inbox",
	}}

	merged := mergeSyncedItems(existing, remote, []string{"aloglu/triage-inbox"})
	if len(merged) != 2 {
		t.Fatalf("merged length = %d, want 2", len(merged))
	}
	if merged[0].Title != "Local draft" {
		t.Fatalf("expected local unsynced item to be preserved first, got %q", merged[0].Title)
	}
	if merged[1].Title != "Remote refreshed" {
		t.Fatalf("expected remote item to replace cached copy, got %q", merged[1].Title)
	}
}

func TestNormalizeImportedItemsClearsLegacyLocalRepoSentinel(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	items, err := normalizeImportedItems([]imodel.Item{{
		Title:     "Imported",
		Project:   "project",
		Stage:     imodel.StageActive,
		CreatedAt: now,
		UpdatedAt: now,
		Repo:      "local",
	}})
	if err != nil {
		t.Fatalf("normalizeImportedItems() error = %v", err)
	}
	if items[0].Repo != "" {
		t.Fatalf("Repo = %q, want empty after normalizing legacy local sentinel", items[0].Repo)
	}
}

func TestBuildEditedItemPreservesSyncedRemoteWhenRepoChanges(t *testing.T) {
	m := New().(modelUI)
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	remoteUpdated := now.Add(-time.Hour)
	m.items = []imodel.Item{{
		Title:           "Existing",
		Project:         "project",
		Type:            imodel.TypeFeature,
		Stage:           imodel.StageActive,
		Body:            "body",
		CreatedAt:       now.Add(-2 * time.Hour),
		UpdatedAt:       now.Add(-time.Hour),
		RemoteUpdatedAt: remoteUpdated,
		IssueNumber:     42,
		Repo:            "owner/old-repo",
		SyncedRepo:      "owner/old-repo",
		State:           "open",
	}}
	m.form.isNew = false
	m.form.editingIndex = 0

	edited := m.buildEditedItem("Existing", "project", "owner/new-repo", "body", imodel.TypeFeature, imodel.StageActive, now)
	if edited.Repo != "owner/new-repo" {
		t.Fatalf("Repo = %q, want %q", edited.Repo, "owner/new-repo")
	}
	if edited.IssueNumber != 42 {
		t.Fatalf("IssueNumber = %d, want %d after repo change", edited.IssueNumber, 42)
	}
	if !edited.RemoteUpdatedAt.Equal(remoteUpdated) {
		t.Fatalf("RemoteUpdatedAt = %v, want %v after repo change", edited.RemoteUpdatedAt, remoteUpdated)
	}
	if edited.SyncedRepo != "owner/old-repo" {
		t.Fatalf("SyncedRepo = %q, want %q after repo change", edited.SyncedRepo, "owner/old-repo")
	}
}

func TestSyncTargetReposIncludesTrackedAndItemRepos(t *testing.T) {
	m := New().(modelUI)
	m.config = config.AppConfig{
		StorageMode: config.ModeGitHub,
		Repo:        "aloglu/triage-inbox",
		TrackedRepos: []string{
			"owner/secondary",
		},
		ProjectRepos: map[string]string{
			"serein": "owner/serein",
		},
	}

	repos := m.syncTargetRepos([]imodel.Item{
		{Repo: "owner/third"},
		{Repo: "owner/secondary"},
		{Repo: "local"},
	})

	want := []string{"aloglu/triage-inbox", "owner/serein", "owner/third", "owner/secondary"}
	if len(repos) != len(want) {
		t.Fatalf("syncTargetRepos length = %d, want %d (%v)", len(repos), len(want), repos)
	}
	for idx := range want {
		if repos[idx] != want[idx] {
			t.Fatalf("syncTargetRepos[%d] = %q, want %q", idx, repos[idx], want[idx])
		}
	}
}

func TestSyncTargetReposIncludesTrackedRepoWithoutItemsOrMappings(t *testing.T) {
	m := New().(modelUI)
	m.config = config.AppConfig{
		StorageMode: config.ModeGitHub,
		Repo:        "aloglu/triage-inbox",
		TrackedRepos: []string{
			"owner/external",
		},
	}

	repos := m.syncTargetRepos(nil)
	want := []string{"aloglu/triage-inbox", "owner/external"}
	if len(repos) != len(want) {
		t.Fatalf("syncTargetRepos length = %d, want %d (%v)", len(repos), len(want), repos)
	}
	for idx := range want {
		if repos[idx] != want[idx] {
			t.Fatalf("syncTargetRepos[%d] = %q, want %q", idx, repos[idx], want[idx])
		}
	}
}

func TestMergeSyncedItemsPreservesPendingLocalEdits(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	existing := []imodel.Item{{
		Title:           "Local edit",
		Project:         "project",
		Type:            imodel.TypeFeature,
		Stage:           imodel.StageActive,
		Body:            "local body",
		CreatedAt:       now.Add(-2 * time.Hour),
		UpdatedAt:       now.Add(-time.Hour),
		RemoteUpdatedAt: now.Add(-time.Hour),
		IssueNumber:     7,
		Repo:            "aloglu/triage-inbox",
		SyncedRepo:      "aloglu/triage-inbox",
		PendingSync:     imodel.SyncUpdate,
	}}
	remote := []imodel.Item{{
		Title:           "Remote edit",
		Project:         "project",
		Type:            imodel.TypeFeature,
		Stage:           imodel.StageActive,
		Body:            "remote body",
		CreatedAt:       now.Add(-2 * time.Hour),
		UpdatedAt:       now,
		RemoteUpdatedAt: now,
		IssueNumber:     7,
		Repo:            "aloglu/triage-inbox",
		SyncedRepo:      "aloglu/triage-inbox",
	}}

	merged := mergeSyncedItems(existing, remote, []string{"aloglu/triage-inbox"})
	if len(merged) != 1 {
		t.Fatalf("len(merged) = %d, want 1", len(merged))
	}
	if merged[0].Body != "local body" {
		t.Fatalf("Body = %q, want local pending version to win", merged[0].Body)
	}
	if merged[0].PendingSync != imodel.SyncUpdate {
		t.Fatalf("PendingSync = %q, want %q", merged[0].PendingSync, imodel.SyncUpdate)
	}
}

func TestReconcileTrackedReposDropsUnreferencedRepo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	manager, err := config.NewManager()
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	m := New().(modelUI)
	m.configManager = manager
	m.config = config.AppConfig{
		StorageMode:  config.ModeGitHub,
		Repo:         "aloglu/triage-inbox",
		TrackedRepos: []string{"aloglu/triage-inbox", "aloglu/test"},
		DataFile:     filepath.Join(t.TempDir(), "items.json"),
		Density:      "comfortable",
	}
	m.applyConfig(m.config)

	items := []imodel.Item{{
		Title:   "Only inbox",
		Project: "project",
		Stage:   imodel.StageActive,
		Repo:    "aloglu/triage-inbox",
	}}
	if err := m.reconcileTrackedRepos(items); err != nil {
		t.Fatalf("reconcileTrackedRepos() error = %v", err)
	}

	if len(m.config.TrackedRepos) != 1 || m.config.TrackedRepos[0] != "aloglu/triage-inbox" {
		t.Fatalf("TrackedRepos = %v, want only default repo after pruning", m.config.TrackedRepos)
	}
}

func TestMergeSyncedItemsKeepsExistingItemWhenFreshRemoteListMissesIt(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	existing := []imodel.Item{{
		Title:           "Freshly created",
		Project:         "project",
		Type:            imodel.TypeFeature,
		Stage:           imodel.StageIdea,
		Body:            "body",
		CreatedAt:       now,
		UpdatedAt:       now,
		RemoteUpdatedAt: now,
		IssueNumber:     9,
		Repo:            "owner/new-repo",
		SyncedRepo:      "owner/new-repo",
	}}

	merged := mergeSyncedItems(existing, nil, []string{"owner/new-repo"})
	if len(merged) != 1 {
		t.Fatalf("len(merged) = %d, want 1", len(merged))
	}
	if merged[0].IssueNumber != 9 || merged[0].Repo != "owner/new-repo" {
		t.Fatalf("unexpected merged item: %+v", merged[0])
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

func TestRenderConfirmModalShowsQuitButtons(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeConfirm
	m.confirm = &confirmState{action: confirmQuit}

	rendered := m.renderConfirmModal()
	if !strings.Contains(rendered, "(Q)uit") {
		t.Fatalf("expected confirm modal to show quit button")
	}
	if !strings.Contains(rendered, "(C)ancel") {
		t.Fatalf("expected confirm modal to show cancel button")
	}
}

func TestUpdateConfirmQReturnsQuitCmdForQuitConfirm(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeConfirm
	m.confirm = &confirmState{action: confirmQuit}

	updated, cmd := m.updateConfirm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg from quit confirm command")
	}
	got := updated.(modelUI)
	if got.confirm != nil {
		t.Fatalf("expected confirm state to clear, got %#v", got.confirm)
	}
}

func TestUpdateConfirmEnterReturnsQuitCmdForQuitConfirm(t *testing.T) {
	m := New().(modelUI)
	m.mode = modeConfirm
	m.confirm = &confirmState{action: confirmQuit}

	updated, cmd := m.updateConfirm(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg from quit confirm command")
	}
	got := updated.(modelUI)
	if got.confirm != nil {
		t.Fatalf("expected confirm state to clear, got %#v", got.confirm)
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
