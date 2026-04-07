package app

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	imodel "github.com/aloglu/triage/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestPaneHeightsStayAlignedWithItems(t *testing.T) {
	m := New().(modelUI)
	m.width = 128
	m.height = 40
	m.mode = modeNormal
	m.focus = focusItems
	m.config.StorageMode = "github"

	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{Title: "Third Issue From the App", Project: "personal", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
		{Title: "Issue from GitHub", Project: "inkubator", Stage: imodel.StageBlocked, UpdatedAt: now, CreatedAt: now},
		{Title: "Test Issue", Project: "serein", Stage: imodel.StagePlanned, UpdatedAt: now, CreatedAt: now},
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	contentHeight := max(12, m.height-7)
	listWidth, detailWidth := m.layoutWidths()

	items := m.renderItemsPane(listWidth, contentHeight)
	details := m.renderDetailPane(detailWidth, contentHeight)

	want := lipgloss.Height(items)
	if got := lipgloss.Height(details); got != want {
		t.Fatalf("details pane height = %d, want %d", got, want)
	}
}

func TestViewFitsTerminalWidth(t *testing.T) {
	m := New().(modelUI)
	m.width = 128
	m.height = 40
	m.mode = modeNormal
	m.focus = focusItems
	m.config.StorageMode = "github"

	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{Title: "Third Issue From the App", Project: "personal", Stage: imodel.StageActive, UpdatedAt: now, CreatedAt: now},
		{Title: "Issue from GitHub", Project: "inkubator", Stage: imodel.StageBlocked, UpdatedAt: now, CreatedAt: now},
		{Title: "Test Issue", Project: "serein", Stage: imodel.StagePlanned, UpdatedAt: now, CreatedAt: now},
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	view := m.View()
	for i, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("line %d width = %d, want <= %d", i, got, m.width)
		}
	}
}

func TestItemsPaneScrollKeepsSelectionVisible(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeNormal
	m.focus = focusItems
	m.config.StorageMode = "github"

	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	for i := 1; i <= 10; i++ {
		m.items = append(m.items, imodel.Item{
			Title:     fmt.Sprintf("Item %d", i),
			Project:   "project",
			Stage:     imodel.StageActive,
			UpdatedAt: now,
			CreatedAt: now,
		})
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	for i := 0; i < 5; i++ {
		m = m.moveDown()
	}

	listWidth, _ := m.layoutWidths()
	rendered := m.renderItemsPane(listWidth, max(12, m.height-7))
	if m.selected < m.itemOffset || m.selected >= m.itemOffset+m.itemVisibleCount() {
		t.Fatalf("expected selected index %d to be inside visible window starting at %d", m.selected, m.itemOffset)
	}
	if strings.Contains(rendered, "Item 1") {
		t.Fatalf("expected earliest items to be scrolled out of view")
	}
}

func TestDetailPaneScrollShowsLaterBodyLines(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeNormal
	m.focus = focusDetails
	m.config.StorageMode = "github"

	bodyLines := make([]string, 0, 20)
	for i := 1; i <= 20; i++ {
		bodyLines = append(bodyLines, fmt.Sprintf("body line %02d", i))
	}

	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{{
		Title:     "Scroll Test",
		Project:   "project",
		Stage:     imodel.StageActive,
		Body:      strings.Join(bodyLines, "\n"),
		UpdatedAt: now,
		CreatedAt: now,
	}}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	for i := 0; i < 12; i++ {
		m = m.moveDown()
	}

	_, detailWidth := m.layoutWidths()
	rendered := m.renderDetailPane(detailWidth, max(12, m.height-7))
	if !strings.Contains(rendered, "body line 12") {
		t.Fatalf("expected later body lines to become visible after scrolling")
	}
	if strings.Contains(rendered, "body line 01") {
		t.Fatalf("expected earliest body lines to scroll out of view")
	}
}

func TestItemsPaneShowsFilteredEmptyState(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeNormal
	m.config.StorageMode = "local"

	now := time.Date(2026, 4, 7, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{{
		Title:     "Planned item",
		Project:   "inkubator",
		Stage:     imodel.StagePlanned,
		UpdatedAt: now,
		CreatedAt: now,
	}}
	m.projectFilter = "inkubator"
	m.stageFilter = string(imodel.StageActive)
	m.rebuildFiltered()

	listWidth, _ := m.layoutWidths()
	rendered := m.renderItemsPane(listWidth, max(12, m.height-7))
	if !strings.Contains(rendered, "No items match the current filt") {
		t.Fatalf("expected filtered empty-state copy, got %q", rendered)
	}
	if !strings.Contains(rendered, "project: inkubator") {
		t.Fatalf("expected filtered empty state to mention active project filter")
	}
	if !strings.Contains(rendered, "stage: active") {
		t.Fatalf("expected filtered empty state to mention active stage filter")
	}
}

func TestEmptyPanesShowSyncingState(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeNormal
	m.syncing = true
	m.items = nil
	m.filtered = nil
	m.config.StorageMode = "github"
	m.config.Repo = "aloglu/triage-inbox"
	m.rebuildFiltered()

	listWidth, detailWidth := m.layoutWidths()
	items := m.renderItemsPane(listWidth, max(12, m.height-7))
	if !strings.Contains(items, "Syncing") || !strings.Contains(items, "Fetching items from GitHub") {
		t.Fatalf("expected syncing empty state in items pane, got %q", items)
	}

	details := m.renderDetailPane(detailWidth, max(12, m.height-7))
	if !strings.Contains(details, "Waiting for GitHub issues") {
		t.Fatalf("expected syncing empty state in details pane, got %q", details)
	}
}

func TestItemsPaneShowsScrollbarOnOverflow(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeNormal
	m.focus = focusItems

	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	for i := 1; i <= 10; i++ {
		m.items = append(m.items, imodel.Item{
			Title:     fmt.Sprintf("Item %d", i),
			Project:   "project",
			Stage:     imodel.StageActive,
			UpdatedAt: now,
			CreatedAt: now,
		})
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	listWidth, _ := m.layoutWidths()
	rendered := m.renderItemsPane(listWidth, max(12, m.height-7))
	if !strings.Contains(rendered, "┃") {
		t.Fatalf("expected overflowed items pane to show a scrollbar thumb")
	}
}

func TestDetailPaneHidesScrollbarWhenContentFits(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 32
	m.mode = modeNormal
	m.focus = focusDetails

	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{{
		Title:     "Short",
		Project:   "project",
		Stage:     imodel.StageActive,
		Body:      "short body",
		UpdatedAt: now,
		CreatedAt: now,
	}}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	_, detailWidth := m.layoutWidths()
	rendered := m.renderDetailPane(detailWidth, max(12, m.height-7))
	if strings.Contains(rendered, "┃") {
		t.Fatalf("expected non-overflowing details pane to hide scrollbar thumb")
	}
}

func TestEditBodyKeepsFocusOnArrowScroll(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.beginEdit(-1)
	m.form.focusIndex = 3

	updated, _ := m.updateEdit(tea.KeyMsg{Type: tea.KeyDown})
	got := updated.(modelUI)
	if got.form.focusIndex != 3 {
		t.Fatalf("body focus moved on down arrow: got %d", got.form.focusIndex)
	}
}

func TestEditTitleAllowsTypingJ(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.beginEdit(-1)
	m.form.focusIndex = 0
	focused, _ := m.focusFormField()
	m = focused.(modelUI)

	updated, _ := m.updateEdit(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	got := updated.(modelUI)
	if got.form.titleInput.Value() != "j" {
		t.Fatalf("expected title input to accept j, got %q", got.form.titleInput.Value())
	}
}

func TestRenderConflictViewShowsStructuredComparison(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeConflict
	m.conflict = &conflictState{
		local: imodel.Item{
			Title:     "Local title",
			Project:   "personal",
			Stage:     imodel.StageActive,
			Body:      "local body",
			UpdatedAt: time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC),
		},
		remote: imodel.Item{
			Title:     "Remote title",
			Project:   "inkubator",
			Stage:     imodel.StageBlocked,
			Body:      "remote body",
			UpdatedAt: time.Date(2026, 4, 6, 13, 20, 0, 0, time.UTC),
		},
	}

	rendered := m.renderConflictView(88)
	for _, want := range []string{"Local", "GitHub", "Title (changed)", "Project (changed)", "Stage (changed)", "Labels (changed)", "Body (changed)"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected conflict view to contain %q", want)
		}
	}
}

func TestRenderConflictViewDoesNotMarkEqualFieldAsChanged(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 24
	m.mode = modeConflict
	m.conflict = &conflictState{
		local: imodel.Item{
			Title:     "Same title",
			Project:   "personal",
			Stage:     imodel.StageActive,
			Body:      "local body",
			UpdatedAt: time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC),
		},
		remote: imodel.Item{
			Title:     "Same title",
			Project:   "personal",
			Stage:     imodel.StageBlocked,
			Body:      "remote body",
			UpdatedAt: time.Date(2026, 4, 6, 13, 20, 0, 0, time.UTC),
		},
	}

	rendered := m.renderConflictView(88)
	if strings.Contains(rendered, "Title (changed)") {
		t.Fatalf("did not expect unchanged title to be marked as changed")
	}
	if !strings.Contains(rendered, "Stage (changed)") {
		t.Fatalf("expected differing stage to be marked as changed")
	}
	if !strings.Contains(rendered, "Unchanged: Title, Project") {
		t.Fatalf("expected unchanged field summary, got %q", rendered)
	}
}

func TestRenderConflictViewShowsTrailingBodyChange(t *testing.T) {
	m := New().(modelUI)
	m.width = 120
	m.height = 28
	m.mode = modeConflict
	m.conflict = &conflictState{
		local: imodel.Item{
			Title:     "Same title",
			Project:   "inkubator",
			Stage:     imodel.StagePlanned,
			Body:      "The top bar of Activity Log section in the showcase does not adjust its size properly when viewed on a device with narrower viewport today",
			UpdatedAt: time.Date(2026, 4, 7, 13, 15, 0, 0, time.UTC),
		},
		remote: imodel.Item{
			Title:     "Same title",
			Project:   "inkubator",
			Stage:     imodel.StagePlanned,
			Body:      "The top bar of Activity Log section in the showcase does not adjust its size properly when viewed on a device with narrower viewport",
			UpdatedAt: time.Date(2026, 4, 7, 13, 20, 0, 0, time.UTC),
		},
	}

	rendered := m.renderConflictView(112)
	if !strings.Contains(rendered, "today") {
		t.Fatalf("expected conflict view to surface the trailing changed word, got %q", rendered)
	}
	if !strings.Contains(rendered, "(empty)") {
		t.Fatalf("expected conflict view to show the missing remote segment explicitly")
	}
}

func TestRenderConflictBodyPreservesLineBreaks(t *testing.T) {
	m := New().(modelUI)

	localLines, remoteLines := m.renderConflictBodyLines("l\nl\nl", "w\nw\nw", 16, 16)

	if got := stripANSI(strings.Join(localLines, "\n")); !strings.Contains(got, "l\nl") {
		t.Fatalf("expected local diff lines to preserve newlines, got %q", got)
	}
	if got := stripANSI(strings.Join(remoteLines, "\n")); !strings.Contains(got, "w\nw") {
		t.Fatalf("expected remote diff lines to preserve newlines, got %q", got)
	}
}

func TestRenderConflictBodyDoesNotCapChangedLines(t *testing.T) {
	m := New().(modelUI)

	localLines, remoteLines := m.renderConflictBodyLines("a\nb\nc\nd\ne", "v\nw\nx\ny\nz", 16, 16)

	localText := stripANSI(strings.Join(localLines, "\n"))
	remoteText := stripANSI(strings.Join(remoteLines, "\n"))
	if !strings.Contains(localText, "d\ne") {
		t.Fatalf("expected local diff to keep later changed lines, got %q", localText)
	}
	if !strings.Contains(remoteText, "y\nz") {
		t.Fatalf("expected remote diff to keep later changed lines, got %q", remoteText)
	}
}

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

func TestRenderContentUsesFullWidthConflictPane(t *testing.T) {
	m := New().(modelUI)
	m.width = 120
	m.height = 28
	m.mode = modeConflict
	m.conflict = &conflictState{
		local: imodel.Item{
			Title:     "Same title",
			Project:   "inkubator",
			Stage:     imodel.StagePlanned,
			Body:      "local body change",
			UpdatedAt: time.Date(2026, 4, 7, 13, 15, 0, 0, time.UTC),
		},
		remote: imodel.Item{
			Title:     "Same title",
			Project:   "inkubator",
			Stage:     imodel.StagePlanned,
			Body:      "remote body change",
			UpdatedAt: time.Date(2026, 4, 7, 13, 20, 0, 0, time.UTC),
		},
	}

	rendered := m.renderContent()
	if strings.Contains(rendered, "Items") {
		t.Fatalf("did not expect items pane to render during conflict mode")
	}
	if strings.Contains(rendered, "Details") {
		t.Fatalf("did not expect details pane title to render during conflict mode")
	}
	if !strings.Contains(rendered, "Conflict") {
		t.Fatalf("expected conflict pane to render")
	}
}

func TestConflictPaneShowsPinnedPromptAndButtons(t *testing.T) {
	m := New().(modelUI)
	m.width = 120
	m.height = 28
	m.mode = modeConflict
	m.conflict = &conflictState{
		local: imodel.Item{
			Title:     "Same title",
			Project:   "inkubator",
			Stage:     imodel.StagePlanned,
			Body:      "local body change",
			UpdatedAt: time.Date(2026, 4, 7, 13, 15, 0, 0, time.UTC),
		},
		remote: imodel.Item{
			Title:     "Same title",
			Project:   "inkubator",
			Stage:     imodel.StageBlocked,
			Body:      "remote body change",
			UpdatedAt: time.Date(2026, 4, 7, 13, 20, 0, 0, time.UTC),
		},
	}

	content := m.renderConflictPane(116, max(12, m.height-7))
	if strings.Contains(content, "r keep GitHub version") {
		t.Fatalf("did not expect inline conflict actions inside pane")
	}
	for _, want := range []string{"Choose which version to keep.", "(R)emote", "(O)verwrite", "(Esc) Cancel"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected conflict pane to contain %q", want)
		}
	}
	footer := m.renderFooter()
	if strings.Contains(footer, "keep GitHub") {
		t.Fatalf("did not expect old conflict footer hint to remain")
	}
}

func TestConflictModeScrollsBodyOnDown(t *testing.T) {
	m := New().(modelUI)
	m.width = 96
	m.height = 18
	m.mode = modeConflict
	longLocal := strings.Join([]string{
		"line 01", "line 02", "line 03", "line 04", "line 05", "line 06",
		"line 07", "line 08", "line 09", "line 10", "line 11", "line 12",
	}, "\n")
	longRemote := strings.Join([]string{
		"line a1", "line a2", "line a3", "line a4", "line a5", "line a6",
		"line a7", "line a8", "line a9", "line a10", "line a11", "line a12",
	}, "\n")
	m.conflict = &conflictState{
		local: imodel.Item{
			Title:     "Same title",
			Project:   "inkubator",
			Stage:     imodel.StagePlanned,
			Body:      longLocal,
			UpdatedAt: time.Date(2026, 4, 7, 13, 15, 0, 0, time.UTC),
		},
		remote: imodel.Item{
			Title:     "Remote title",
			Project:   "remote-project",
			Stage:     imodel.StageBlocked,
			Body:      longRemote,
			UpdatedAt: time.Date(2026, 4, 7, 13, 20, 0, 0, time.UTC),
		},
	}

	before := m.renderConflictPane(92, max(12, m.height-7))
	if !strings.Contains(before, "┃") {
		t.Fatalf("expected overflowing conflict pane to show a scrollbar")
	}
	updated, _ := m.updateConflict(tea.KeyMsg{Type: tea.KeyDown})
	got := updated.(modelUI)
	if got.detailScroll <= 0 {
		t.Fatalf("expected conflict mode down key to scroll body, got detailScroll=%d", got.detailScroll)
	}
	after := got.renderConflictPane(92, max(12, got.height-7))
	if before == after {
		t.Fatalf("expected rendered conflict pane to change after scrolling")
	}
}

func TestItemsPaneDoesNotShiftWhenEnteringEditMode(t *testing.T) {
	m := New().(modelUI)
	m.width = 120
	m.height = 28
	m.mode = modeNormal
	m.focus = focusItems

	now := time.Date(2026, 4, 7, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{
			Title:     "Showcase Responsiveness",
			Project:   "inkubator",
			Stage:     imodel.StagePlanned,
			UpdatedAt: now,
			CreatedAt: now,
		},
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	listWidth, _ := m.layoutWidths()
	before := m.renderItemsPane(listWidth, max(12, m.height-7))

	m.beginEdit(m.filtered[m.selected])
	focused, _ := m.focusFormField()
	got := focused.(modelUI)
	after := got.renderItemsPane(listWidth, max(12, got.height-7))

	if before != after {
		t.Fatalf("expected items pane to remain stable when entering edit mode")
	}
}

func TestEditViewFitsTerminalWidth(t *testing.T) {
	m := New().(modelUI)
	m.width = 120
	m.height = 28
	m.mode = modeNormal
	m.focus = focusItems

	now := time.Date(2026, 4, 7, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{
			Title:     "Showcase Responsiveness",
			Project:   "inkubator",
			Stage:     imodel.StagePlanned,
			Body:      "Body text",
			UpdatedAt: now,
			CreatedAt: now,
		},
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	updated, _ := m.enterEdit(m.filtered[m.selected])
	got := updated.(modelUI)
	view := got.View()
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > got.width {
			t.Fatalf("edit view line %d width = %d, want <= %d", i, w, got.width)
		}
	}
}

func TestItemMetaAlignmentStaysStableAcrossEditTransition(t *testing.T) {
	m := New().(modelUI)
	m.width = 120
	m.height = 28
	m.mode = modeNormal
	m.focus = focusItems

	now := time.Date(2026, 4, 7, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{
			Title:     "Showcase Responsiveness",
			Project:   "inkubator",
			Stage:     imodel.StagePlanned,
			Body:      "Body text",
			UpdatedAt: now,
			CreatedAt: now,
		},
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	listWidth, _ := m.layoutWidths()
	before := stripANSI(m.renderItemRow(m.items[m.filtered[m.selected]], listWidth, true))

	updated, _ := m.enterEdit(m.filtered[m.selected])
	got := updated.(modelUI)
	after := stripANSI(got.renderItemRow(got.items[got.filtered[got.selected]], listWidth, true))

	beforeLines := strings.Split(before, "\n")
	afterLines := strings.Split(after, "\n")
	if len(beforeLines) < 2 || len(afterLines) < 2 {
		t.Fatalf("expected item row to have two lines")
	}

	beforeProject := strings.Index(beforeLines[1], "inkubator")
	afterProject := strings.Index(afterLines[1], "inkubator")
	beforeStage := strings.Index(beforeLines[1], "planned")
	afterStage := strings.Index(afterLines[1], "planned")

	if beforeProject != afterProject || beforeStage != afterStage {
		t.Fatalf("expected item meta alignment to stay stable, before=%q after=%q", beforeLines[1], afterLines[1])
	}
}

func TestFullViewKeepsItemMetaPositionOnFirstEdit(t *testing.T) {
	m := New().(modelUI)
	m.width = 120
	m.height = 28
	m.mode = modeNormal
	m.focus = focusItems

	now := time.Date(2026, 4, 7, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{
			Title:     "Showcase Responsiveness",
			Project:   "inkubator",
			Stage:     imodel.StagePlanned,
			Body:      "The top bar of Activity Log section in the showcase does not adjust its size properly when viewed on a device with narrower viewport.",
			UpdatedAt: now,
			CreatedAt: now,
		},
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()

	_ = m.View()
	updated, _ := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	got := updated.(modelUI)

	beforeLine := findLineContaining(stripANSI(m.View()), "inkubator", "planned")
	afterLine := findLineContaining(stripANSI(got.View()), "inkubator", "planned")
	if beforeLine == "" || afterLine == "" {
		t.Fatalf("expected to find item meta line before and after edit transition")
	}

	beforeProject := strings.Index(beforeLine, "inkubator")
	afterProject := strings.Index(afterLine, "inkubator")
	beforeStage := strings.Index(beforeLine, "planned")
	afterStage := strings.Index(afterLine, "planned")
	if beforeProject != afterProject || beforeStage != afterStage {
		t.Fatalf("expected full-view item meta position to stay stable, before=%q after=%q", beforeLine, afterLine)
	}
}

func TestFullViewKeepsItemMetaPositionAcrossEditFocusChange(t *testing.T) {
	m := New().(modelUI)
	m.width = 80
	m.height = 20
	m.mode = modeNormal
	m.focus = focusItems

	now := time.Date(2026, 4, 7, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{
			Title:     "TTS Issues",
			Project:   "serein",
			Stage:     imodel.StageActive,
			Body:      "Details here.",
			UpdatedAt: now,
			CreatedAt: now,
		},
		{
			Title:     "Showcase Responsiveness",
			Project:   "inkubator",
			Stage:     imodel.StagePlanned,
			Body:      "The top bar of Activity Log section in the showcase does not adjust its size properly when viewed on a device with narrower viewport.",
			UpdatedAt: now,
			CreatedAt: now,
		},
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()
	m = m.moveDown()

	updated, _ := m.enterEdit(m.filtered[m.selected])
	edit0 := updated.(modelUI)
	focus0View := stripANSI(edit0.View())
	updated, _ = edit0.updateEdit(tea.KeyMsg{Type: tea.KeyTab})
	edit1 := updated.(modelUI)
	focus1View := stripANSI(edit1.View())

	beforeLine := findLineContaining(focus0View, "serein", "active")
	afterLine := findLineContaining(focus1View, "2026-04-07", "active")
	if beforeLine == "" || afterLine == "" {
		t.Fatalf("expected to find item meta line before and after tab")
	}

	beforeProject := strings.Index(beforeLine, "serein")
	afterProject := strings.Index(afterLine, "ser")
	beforeStage := strings.Index(beforeLine, "active")
	afterStage := strings.Index(afterLine, "active")
	if beforeProject != afterProject || beforeStage != afterStage {
		t.Fatalf("expected item meta position to stay stable across edit focus change, before=%q after=%q", beforeLine, afterLine)
	}
}
func TestItemsPaneKeepsItemMetaAcrossEditFocusChange(t *testing.T) {
	m := New().(modelUI)
	m.width = 80
	m.height = 20
	m.mode = modeNormal
	m.focus = focusItems

	now := time.Date(2026, 4, 7, 13, 15, 0, 0, time.UTC)
	m.items = []imodel.Item{
		{
			Title:     "TTS Issues",
			Project:   "serein",
			Stage:     imodel.StageActive,
			Body:      "Details here.",
			UpdatedAt: now,
			CreatedAt: now,
		},
		{
			Title:     "Showcase Responsiveness",
			Project:   "inkubator",
			Stage:     imodel.StagePlanned,
			Body:      "The top bar of Activity Log section in the showcase does not adjust its size properly when viewed on a device with narrower viewport.",
			UpdatedAt: now,
			CreatedAt: now,
		},
	}
	m.projectFilter = allProjectsLabel
	m.rebuildFiltered()
	m = m.moveDown()

	updated, _ := m.enterEdit(m.filtered[m.selected])
	edit0 := updated.(modelUI)
	listWidth, _ := edit0.layoutWidths()
	focus0Pane := stripANSI(edit0.renderItemsPane(listWidth, max(12, edit0.height-7)))

	updated, _ = edit0.updateEdit(tea.KeyMsg{Type: tea.KeyTab})
	edit1 := updated.(modelUI)
	focus1Pane := stripANSI(edit1.renderItemsPane(listWidth, max(12, edit1.height-7)))

	beforeLine := findLineContaining(focus0Pane, "serein", "active")
	afterLine := findLineContaining(focus1Pane, "2026-04-07", "active")
	if beforeLine == "" || afterLine == "" {
		t.Fatalf("expected to find item meta line before and after tab")
	}
	if beforeLine != afterLine {
		t.Fatalf("expected items pane to stay stable across edit focus change, before=%q after=%q", beforeLine, afterLine)
	}
}

func findLineContaining(view string, needles ...string) string {
	for _, line := range strings.Split(view, "\n") {
		match := true
		for _, needle := range needles {
			if !strings.Contains(line, needle) {
				match = false
				break
			}
		}
		if match {
			return line
		}
	}
	return ""
}
