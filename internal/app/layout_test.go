package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	imodel "github.com/aloglu/triage/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
