package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/issue"
	"github.com/aloglu/triage/internal/store"
)

// rowHeight is the number of lines an issue takes in the list.
const rowHeight = 3

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "triage"
	return v
}

func (m *Model) render() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.width < 40 || m.height < 12 {
		msg := lipgloss.NewStyle().Width(m.width).Align(lipgloss.Center).Render(m.th.subtle.Render("Make the window a little bigger for triage."))
		return clip(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg), m.width, m.height)
	}
	var base string
	if m.onboarding != nil {
		base = m.onboarding.view(m)
	} else {
		header := m.renderHeader()
		footer := m.renderFooter()
		bodyHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
		base = lipgloss.JoinVertical(lipgloss.Left, header, m.renderBody(bodyHeight), footer)
	}
	if len(m.overlays) == 0 {
		return clip(base, m.width, m.height)
	}
	modal := m.overlays[len(m.overlays)-1].view(m)
	return clip(placeOverlay(base, modal, m.width, m.height), m.width, m.height)
}

// clip cuts s to the terminal size so an oversized element can never wrap
// and scroll the screen.
func clip(s string, width, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		if lipgloss.Width(line) > width {
			lines[i] = ansi.Truncate(line, width, "")
		}
	}
	return strings.Join(lines, "\n")
}

// placeOverlay draws modal centered on top of base.
func placeOverlay(base, modal string, width, height int) string {
	baseLayer := lipgloss.NewLayer(base)
	mw, mh := lipgloss.Width(modal), lipgloss.Height(modal)
	modalLayer := lipgloss.NewLayer(modal).X(max(0, (width-mw)/2)).Y(max(0, (height-mh)/3)).Z(1)
	return lipgloss.NewCompositor(baseLayer, modalLayer).Render()
}

func (m *Model) renderHeader() string {
	th := m.th
	left := th.title.Render("triage") + "  "
	if m.filtering || m.filterInput.Value() != "" {
		left += th.tabActive.Render(m.views[m.viewIdx].name) + "  "
		m.filterInput.SetWidth(max(10, m.width-lipgloss.Width(left)-lipgloss.Width(m.syncIndicator())-4))
		return m.joinEnds(left+m.filterInput.View(), m.syncIndicator())
	}
	right := m.syncIndicator()
	avail := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	return m.joinEnds(left+m.renderTabs(avail), right)
}

// renderTabs shows as many view tabs as fit, keeping the active one visible.
func (m *Model) renderTabs(avail int) string {
	th := m.th
	labels := make([]string, len(m.views))
	for i, v := range m.views {
		if i == m.viewIdx {
			labels[i] = th.tabActive.Render(v.name)
		} else {
			labels[i] = th.tabInactive.Render(v.name)
		}
	}
	sep := th.dim.Render(" · ")
	start, end := m.viewIdx, m.viewIdx+1
	width := lipgloss.Width(labels[m.viewIdx])
	for {
		grew := false
		if end < len(labels) && width+lipgloss.Width(sep+labels[end])+2 <= avail {
			width += lipgloss.Width(sep + labels[end])
			end++
			grew = true
		}
		if start > 0 && width+lipgloss.Width(labels[start-1]+sep)+2 <= avail {
			start--
			width += lipgloss.Width(labels[start] + sep)
			grew = true
		}
		if !grew {
			break
		}
	}
	out := strings.Join(labels[start:end], sep)
	if start > 0 {
		out = th.dim.Render("‹ ") + out
	}
	if end < len(labels) {
		out += th.dim.Render(" ›")
	}
	return out
}

func (m *Model) syncIndicator() string {
	th := m.th
	total, held := m.snap.PendingCount()
	var parts []string
	switch {
	case held > 0:
		parts = append(parts, th.statusError.Render(fmt.Sprintf("⚠ %d need attention", held)))
	case total > 0 && (m.offline || !m.eng.Online()):
		parts = append(parts, th.statusWarn.Render(fmt.Sprintf("↑%d queued", total)))
	case total > 0:
		parts = append(parts, th.statusWarn.Render(fmt.Sprintf("↑%d sending", total)))
	}
	switch {
	case !m.eng.Online():
		parts = append(parts, th.statusWarn.Render("● not signed in"))
	case m.refreshing:
		parts = append(parts, th.statusInfo.Render("↻ syncing"))
	case m.offline:
		parts = append(parts, th.statusWarn.Render("● offline"))
	case !m.lastSync.IsZero():
		parts = append(parts, th.statusOK.Render("●")+th.statusInfo.Render(" synced "+relTime(m.lastSync)))
	}
	return strings.Join(parts, th.dim.Render("  "))
}

func (m *Model) joinEnds(left, right string) string {
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return truncate(left, m.width-lipgloss.Width(right)-1) + " " + right
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) renderFooter() string {
	th := m.th
	if m.flashState.text != "" {
		style := th.statusInfo
		switch m.flashState.kind {
		case flashOK:
			style = th.statusOK
		case flashWarn:
			style = th.statusWarn
		case flashError:
			style = th.statusError
		}
		return truncate(style.Render(m.flashState.text), m.width)
	}
	_, hasIssue := m.selected()
	bindings := m.keys.footer(hasIssue)
	if m.filtering {
		return th.dim.Render("enter keep filter · esc clear · ") + th.dim.Render(strings.Join(issue.QueryKeys(), "  "))
	}
	var parts []string
	for _, b := range bindings {
		parts = append(parts, th.key.Render(b.Help().Key)+" "+th.keyDesc.Render(b.Help().Desc))
	}
	line := ""
	for _, part := range parts {
		candidate := line
		if candidate != "" {
			candidate += "  "
		}
		candidate += part
		if lipgloss.Width(candidate) > m.width {
			break
		}
		line = candidate
	}
	return line
}

func (m *Model) paneWidths() (int, int) {
	if m.isNarrow() {
		return m.width, m.width
	}
	list := max(36, m.width*2/5)
	return list, m.width - list
}

func (m *Model) renderBody(height int) string {
	if m.board && !m.fullDetail {
		return m.renderBoard(height)
	}
	listWidth, detailWidth := m.paneWidths()
	if m.fullDetail || (m.isNarrow() && m.focus == focusDetail) {
		return m.renderDetailPane(m.width, height, true)
	}
	if m.isNarrow() {
		return m.renderListPane(m.width, height)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderListPane(listWidth, height),
		m.renderDetailPane(detailWidth, height, m.focus == focusDetail))
}

func (m *Model) listPageSize() int {
	return max(1, (m.height-4)/rowHeight)
}

func (m *Model) renderListPane(width, height int) string {
	th := m.th
	style := th.pane
	if m.focus == focusList {
		style = th.paneFocused
	}
	innerW, innerH := width-4, height-2
	var lines []string
	if len(m.visible) == 0 {
		lines = m.emptyListLines(innerW)
	} else {
		perPage := max(1, innerH/rowHeight)
		if m.cursor < m.offset {
			m.offset = m.cursor
		}
		if m.cursor >= m.offset+perPage {
			m.offset = m.cursor - perPage + 1
		}
		m.offset = max(0, min(m.offset, len(m.visible)-1))
		for idx := m.offset; idx < len(m.visible) && idx < m.offset+perPage; idx++ {
			lines = append(lines, m.renderRow(m.visible[idx], innerW, idx == m.cursor)...)
		}
	}
	content := fitLines(lines, innerW, innerH)
	return style.Width(width).Height(height).Render(content)
}

func (m *Model) emptyListLines(width int) []string {
	th := m.th
	if m.loadingFirst {
		return []string{"", th.subtle.Render("Fetching issues from GitHub…")}
	}
	lines := []string{""}
	if m.filterInput.Value() != "" {
		lines = append(lines, th.subtle.Render("Nothing matches this filter."), th.dim.Render("Press esc to clear it."))
		return lines
	}
	v := m.views[m.viewIdx]
	switch {
	case v.name == "Mine":
		lines = append(lines, th.subtle.Render("Nothing assigned to you."), th.dim.Render("Press a on an issue to take it."))
	case v.name == "Closed":
		lines = append(lines, th.subtle.Render("No closed issues yet."))
	default:
		lines = append(lines, th.subtle.Render("No open issues. Nice."), "", th.key.Render("n")+th.keyDesc.Render(" create one"))
	}
	return lines
}

func (m *Model) renderRow(i issue.Issue, width int, active bool) []string {
	th := m.th
	conv := m.eng.Convention()
	bar := "  "
	titleStyle := lipgloss.NewStyle().Foreground(th.text)
	if active {
		bar = th.cursorBar.Render("▌ ")
		titleStyle = th.selected
	}
	if !i.IsOpen() {
		titleStyle = titleStyle.Foreground(th.muted)
	}
	title := bar + titleStyle.Render(truncate(i.Title, width-2))

	meta := []string{th.renderRepo(i.Repo, i.Ref()), th.renderStatus(conv.StatusOf(i))}
	if t := conv.TypeOf(i); t != issue.TypeTask {
		meta = append(meta, th.renderType(t))
	}
	meta = append(meta, th.dim.Render(relTime(i.UpdatedAt)))
	if ops := m.snap.Pending[i.Key()]; len(ops) > 0 {
		meta = append(meta, m.pendingBadge(ops))
	}
	if i.Comments > 0 {
		meta = append(meta, th.dim.Render(fmt.Sprintf("💬%d", i.Comments)))
	}
	line := "  " + strings.Join(meta, "  ")
	return []string{title, truncate(line, width), ""}
}

func (m *Model) pendingBadge(ops []store.Op) string {
	for _, op := range ops {
		if op.Conflict != nil {
			return m.th.statusError.Render("⚠ conflict")
		}
		if op.Error != "" {
			return m.th.statusError.Render("⚠ failed")
		}
	}
	return m.th.statusWarn.Render("↑ unsent")
}

// fitLines pads or cuts lines to exactly height lines of at most width cells.
func fitLines(lines []string, width, height int) string {
	out := make([]string, 0, height)
	for _, line := range lines {
		if len(out) == height {
			break
		}
		if lipgloss.Width(line) > width {
			line = truncate(line, width)
		}
		out = append(out, line)
	}
	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}

func (m *Model) renderBoard(height int) string {
	th := m.th
	columns := m.boardIssues()
	// Show as many columns as fit, keeping the active one in view.
	shown := max(1, min(len(boardColumns), m.width/24))
	first := max(0, min(m.boardCol-shown/2, len(boardColumns)-shown))
	colWidth := m.width / shown
	innerH := height - 2
	var rendered []string
	for c := first; c < first+shown; c++ {
		status := boardColumns[c]
		style := th.pane
		if c == m.boardCol {
			style = th.paneFocused
		}
		w := colWidth
		if c == first+shown-1 {
			w = m.width - colWidth*(shown-1)
		}
		innerW := w - 4
		heading := th.renderStatus(status) + th.dim.Render(fmt.Sprintf("  %d", len(columns[c])))
		if c == first && first > 0 {
			heading = th.dim.Render("‹ ") + heading
		}
		if c == first+shown-1 && c < len(boardColumns)-1 {
			heading += th.dim.Render(" ›")
		}
		lines := []string{heading, ""}
		row := m.boardRow[c]
		if row >= len(columns[c]) {
			row = max(0, len(columns[c])-1)
			m.boardRow[c] = row
		}
		perPage := max(1, (innerH-2)/3)
		start := 0
		if row >= perPage {
			start = row - perPage + 1
		}
		for r := start; r < len(columns[c]) && r < start+perPage; r++ {
			i := columns[c][r]
			active := c == m.boardCol && r == row
			bar := "  "
			titleStyle := lipgloss.NewStyle().Foreground(th.text)
			if active {
				bar = th.cursorBar.Render("▌ ")
				titleStyle = th.selected
			}
			lines = append(lines, bar+titleStyle.Render(truncate(i.Title, innerW-2)))
			meta := th.renderRepo(i.Repo, i.Ref())
			if t := m.eng.Convention().TypeOf(i); t != issue.TypeTask {
				meta += " " + th.renderType(t)
			}
			if ops := m.snap.Pending[i.Key()]; len(ops) > 0 {
				meta += " " + m.pendingBadge(ops)
			}
			lines = append(lines, "  "+meta, "")
		}
		if len(columns[c]) == 0 {
			lines = append(lines, th.dim.Render("  empty"))
		}
		rendered = append(rendered, style.Width(w).Height(height).Render(fitLines(lines, innerW, innerH)))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case t.Year() == time.Now().Year():
		return t.Local().Format("Jan 2")
	default:
		return t.Local().Format("Jan 2, 2006")
	}
}

func repoLabel(repo string) string { return gh.RepoName(repo) }
