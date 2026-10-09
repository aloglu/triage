package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/issue"
)

// sidebarWidth is the sidebar's outer width.
const sidebarWidth = 24

// showSidebar reports whether the terminal is wide enough for the sidebar
// next to the list and detail panes.
func (m *Model) showSidebar() bool {
	return !m.board && !m.fullDetail && m.width >= 110
}

// sidebarItem is one selectable row: a view, "All repos", or a repo.
type sidebarItem struct {
	view int    // index into m.views, or -1
	repo string // "" with view == -1 means all repos
}

func (m *Model) sidebarItems() []sidebarItem {
	items := make([]sidebarItem, 0, len(m.views)+1+len(m.env.Config.Repos))
	for i := range m.views {
		items = append(items, sidebarItem{view: i})
	}
	items = append(items, sidebarItem{view: -1})
	for _, repo := range m.env.Config.Repos {
		items = append(items, sidebarItem{view: -1, repo: repo})
	}
	return items
}

func (m *Model) applySidebarItem(item sidebarItem) {
	if item.view >= 0 {
		m.setView(item.view)
	} else {
		m.setScope(item.repo)
	}
}

// handleSidebarKey moves the sidebar highlight. Moving only highlights;
// enter, l, or → applies the highlighted view or repo and returns to the
// list, and space applies it without leaving. (Applying on every move
// would switch through every view on the way down to the repos.)
func (m *Model) handleSidebarKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	items := m.sidebarItems()
	switch msg.String() {
	case "up", "k":
		m.sidebarCursor = max(0, m.sidebarCursor-1)
	case "down", "j":
		m.sidebarCursor = min(len(items)-1, m.sidebarCursor+1)
	case "g", "home":
		m.sidebarCursor = 0
	case "G", "end":
		m.sidebarCursor = len(items) - 1
	case "space":
		m.applySidebarItem(items[m.sidebarCursor])
	case "enter", "l", "right":
		m.applySidebarItem(items[m.sidebarCursor])
		m.focus = focusList
	case "esc":
		m.focus = focusList
	default:
		return nil, false
	}
	return nil, true
}

// countMatching counts issues matching q in the snapshot.
func (m *Model) countMatching(q issue.Query) int {
	ctx := m.matchContext()
	n := 0
	for _, i := range m.snap.Issues {
		if q.Match(i, ctx) {
			n++
		}
	}
	return n
}

func (m *Model) renderSidebar(height int) string {
	th := m.th
	innerW := sidebarWidth - 4
	focused := m.focus == focusSidebar
	style := th.pane
	if focused {
		style = th.paneFocused
	}

	row := func(idx int, marker, label, count string, labelStyle lipgloss.Style, active bool) string {
		bar := "  "
		if focused && idx == m.sidebarCursor {
			bar = th.cursorBar.Render("▌ ")
		} else if active {
			bar = th.selected.Render("▸ ")
		}
		if active {
			labelStyle = labelStyle.Bold(true)
		}
		left := bar + marker + labelStyle.Render(truncate(label, innerW-lipgloss.Width(bar+marker)-len(count)-1))
		return left + strings.Repeat(" ", max(1, innerW-lipgloss.Width(left)-lipgloss.Width(count))) + th.dim.Render(count)
	}
	count := func(n int) string {
		if n == 0 {
			return ""
		}
		return fmt.Sprint(n)
	}

	lines := []string{th.sectionHeading.Render("VIEWS")}
	scope := m.scopeQuery()
	for i, v := range m.views {
		n := m.countMatching(issue.ParseQuery(v.query).And(scope))
		labelStyle := lipgloss.NewStyle().Foreground(th.text)
		if i != m.viewIdx {
			labelStyle = labelStyle.Foreground(th.muted)
		}
		lines = append(lines, row(i, "", v.name, count(n), labelStyle, i == m.viewIdx))
	}

	lines = append(lines, "", th.sectionHeading.Render("REPOS"))
	viewQuery := issue.ParseQuery(m.views[m.viewIdx].query)
	base := len(m.views)
	allStyle := lipgloss.NewStyle().Foreground(th.muted)
	if m.scope == "" {
		allStyle = lipgloss.NewStyle().Foreground(th.text)
	}
	lines = append(lines, row(base, "", "All repos", count(m.countMatching(viewQuery)), allStyle, m.scope == ""))
	for i, repo := range m.env.Config.Repos {
		n := m.countMatching(viewQuery.And(issue.ParseQuery("repo:" + repo)))
		dot := lipgloss.NewStyle().Foreground(th.repoColor(repo)).Render("● ")
		labelStyle := lipgloss.NewStyle().Foreground(th.muted)
		active := strings.EqualFold(repo, m.scope)
		if active {
			labelStyle = lipgloss.NewStyle().Foreground(th.repoColor(repo))
		}
		lines = append(lines, row(base+1+i, dot, gh.RepoName(repo), count(n), labelStyle, active))
	}

	footer := m.momentumLines(innerW)
	innerH := height - 2
	body := fitLines(lines, innerW, max(0, innerH-len(footer)))
	if len(footer) > 0 && innerH > len(lines)+len(footer) {
		body += "\n" + strings.Join(footer, "\n")
	} else {
		body = fitLines(lines, innerW, innerH)
	}
	m.sidebarHits(height)
	return style.Width(sidebarWidth).Height(height).Render(body)
}

// momentum summarizes the past week in the current scope.
func (m *Model) momentum() (closed, opened int) {
	since := time.Now().Add(-7 * 24 * time.Hour)
	scope := m.scopeQuery()
	ctx := m.matchContext()
	for _, i := range m.snap.Issues {
		if !scope.Match(i, ctx) {
			continue
		}
		if !i.CreatedAt.IsZero() && i.CreatedAt.After(since) {
			opened++
		}
		if !i.IsOpen() && i.StateReason != issue.ReasonNotPlanned {
			at := i.ClosedAt
			if at.IsZero() {
				at = i.UpdatedAt
			}
			if at.After(since) {
				closed++
			}
		}
	}
	return closed, opened
}

func (m *Model) momentumLines(width int) []string {
	th := m.th
	closed, opened := m.momentum()
	if closed == 0 && opened == 0 {
		return []string{th.dim.Render("A quiet week.")}
	}
	parts := []string{}
	if closed > 0 {
		parts = append(parts, th.statusOK.Render(fmt.Sprintf("✓ %d closed", closed)))
	}
	if opened > 0 {
		parts = append(parts, th.subtle.Render(fmt.Sprintf("+ %d opened", opened)))
	}
	return []string{th.dim.Render("this week"), truncate(strings.Join(parts, th.dim.Render(" · ")), width)}
}

// sidebarHits records the sidebar's click targets: clicking an item
// highlights the sidebar and applies the item, like enter.
func (m *Model) sidebarHits(height int) {
	m.hit(hitRegion{x: 0, y: 0, w: sidebarWidth, h: height,
		click: func(m *Model, _ tea.MouseClickMsg) tea.Cmd {
			if m.editor == nil {
				m.focus = focusSidebar
			}
			return nil
		},
		wheel: func(m *Model, delta int) tea.Cmd {
			if m.editor == nil {
				m.sidebarCursor = max(0, min(len(m.sidebarItems())-1, m.sidebarCursor+delta))
			}
			return nil
		},
	})
	items := m.sidebarItems()
	// Content starts below the border: "VIEWS", the views, a blank line,
	// "REPOS", then "All repos" and the repos.
	rowOf := func(i int) int {
		if i < len(m.views) {
			return 2 + i
		}
		return 2 + len(m.views) + 2 + (i - len(m.views))
	}
	for i := range items {
		idx := i
		m.hit(hitRegion{x: 1, y: rowOf(i), w: sidebarWidth - 2, h: 1,
			click: func(m *Model, _ tea.MouseClickMsg) tea.Cmd {
				if m.editor != nil {
					return m.flash("Save (ctrl+s) or cancel (esc) the edit first.", flashInfo)
				}
				m.focus = focusSidebar
				m.sidebarCursor = idx
				m.applySidebarItem(m.sidebarItems()[idx])
				return nil
			},
		})
	}
}
