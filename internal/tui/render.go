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
	m.hits = m.hits[:0]
	var base string
	if m.onboarding != nil {
		base = m.onboarding.view(m)
	} else {
		header := m.renderHeader()
		footer, footerHits := m.captureHits(m.renderFooter)
		headerH, footerH := lipgloss.Height(header), lipgloss.Height(footer)
		bodyHeight := m.height - headerH - footerH
		body, bodyHits := m.captureHits(func() string { return m.renderBody(bodyHeight) })
		m.placeHits(bodyHits, 0, headerH)
		m.placeHits(footerHits, 0, m.height-footerH)
		base = lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	}
	if len(m.overlays) == 0 {
		return clip(base, m.width, m.height)
	}
	top := m.overlays[len(m.overlays)-1]
	// Clicking outside a pop-up closes it.
	m.hit(hitRegion{x: 0, y: 0, w: m.width, h: m.height,
		click: func(m *Model, _ tea.MouseClickMsg) tea.Cmd { m.removeOverlay(top); return nil },
		wheel: func(*Model, int) tea.Cmd { return nil }})
	modal, modalHits := m.captureHits(func() string { return top.view(m) })
	x, y := overlayPosition(modal, m.width, m.height)
	// Clicks inside the pop-up but not on anything do nothing.
	m.hit(hitRegion{x: x, y: y, w: lipgloss.Width(modal), h: lipgloss.Height(modal),
		click: func(*Model, tea.MouseClickMsg) tea.Cmd { return nil }})
	m.placeHits(modalHits, x, y)
	return clip(placeOverlay(base, modal, m.width, m.height), m.width, m.height)
}

// overlayPosition is where a pop-up is drawn: centered, a little above
// the middle.
func overlayPosition(modal string, width, height int) (int, int) {
	return max(0, (width-lipgloss.Width(modal))/2), max(0, (height-lipgloss.Height(modal))/3)
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
	x, y := overlayPosition(modal, width, height)
	modalLayer := lipgloss.NewLayer(modal).X(x).Y(y).Z(1)
	return lipgloss.NewCompositor(baseLayer, modalLayer).Render()
}

func (m *Model) renderHeader() string {
	th := m.th
	left := th.wordmark() + "  "
	if m.filtering || m.filterInput.Value() != "" {
		left += th.subtle.Render(m.views[m.viewIdx].name+" · "+m.scopeLabel()) + "  "
		m.filterInput.SetWidth(max(10, m.width-lipgloss.Width(left)-lipgloss.Width(m.syncIndicator())-4))
		return m.joinEnds(left+m.filterInput.View(), m.syncIndicator())
	}
	if !m.showSidebar() && !m.board {
		// Without the sidebar, the header says where you are.
		left += th.bold.Render(m.views[m.viewIdx].name) + th.dim.Render(" · ") + m.scopeChip()
	} else if m.board {
		left += th.bold.Render("Board") + th.dim.Render(" · ") + m.scopeChip()
	}
	return m.joinEnds(left, m.syncIndicator())
}

func (m *Model) scopeChip() string {
	if m.scope == "" {
		return m.th.subtle.Render("all repos")
	}
	return m.th.renderRepo(m.scope, gh.RepoName(m.scope))
}

func (m *Model) syncIndicator() string {
	th := m.th
	total, held := m.snap.PendingCount()
	var parts []string
	if m.updateAvailable != "" {
		parts = append(parts, th.selected.Render("↑ "+m.updateAvailable+" available"))
	}
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
	case m.refreshing || m.flushing:
		glyph := "↻"
		if m.spinning {
			glyph = m.spinner.View()
		}
		parts = append(parts, glyph+th.statusInfo.Render(" syncing"))
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
		text := m.flashState.text
		if m.flashState.check {
			text = m.checkGlyph() + " " + text
		}
		return truncate(style.Render(text), m.width)
	}
	if m.editor != nil {
		return m.editor.hints(m)
	}
	if m.focus == focusSidebar && m.showSidebar() {
		hint := func(k, desc string) string { return th.key.Render(k) + " " + th.keyDesc.Render(desc) }
		return strings.Join([]string{hint("↑↓", "move"), hint("enter", "choose"), hint("space", "apply"), hint("esc", "back")}, "  ")
	}
	_, hasIssue := m.selected()
	bindings := m.keys.footer(hasIssue)
	if m.filtering {
		return th.dim.Render("enter keep filter · esc clear · ") + th.dim.Render(strings.Join(issue.QueryKeys(), "  "))
	}
	line := ""
	for _, b := range bindings {
		part := th.key.Render(b.Help().Key) + " " + th.keyDesc.Render(b.Help().Desc)
		x := lipgloss.Width(line)
		if line != "" {
			x += 2
		}
		if x+lipgloss.Width(part) > m.width {
			break
		}
		if line != "" {
			line += "  "
		}
		line += part
		// Clicking a hint does what its key does.
		press := keyPress(b.Keys()[0])
		m.hit(hitRegion{x: x, y: 0, w: lipgloss.Width(part), h: 1, click: func(m *Model, _ tea.MouseClickMsg) tea.Cmd {
			return m.handleKey(press)
		}})
	}
	return line
}

// keyPress builds the key event for a key name such as "n" or "enter".
func keyPress(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	r := []rune(name)[0]
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// paneLayout holds the widths of the main panes; sidebar is 0 when hidden.
type paneLayout struct {
	sidebar, list, detail int
}

func (m *Model) layout() paneLayout {
	if m.isNarrow() {
		return paneLayout{list: m.width, detail: m.width}
	}
	var l paneLayout
	rest := m.width
	if m.showSidebar() {
		l.sidebar = sidebarWidth
		rest -= sidebarWidth
	}
	l.list = max(36, rest*2/5)
	l.detail = rest - l.list
	return l
}

// renderBody draws the panes between header and footer. Each pane records
// its click targets relative to its own corner; they're shifted here to
// where the pane lands.
func (m *Model) renderBody(height int) string {
	if m.editor != nil && (m.board || m.isNarrow()) {
		return m.renderEditorPane(m.width, height)
	}
	if m.board && !m.fullDetail {
		return m.renderBoard(height)
	}
	if m.fullDetail || (m.isNarrow() && m.focus == focusDetail) {
		return m.renderDetailPane(m.width, height, true)
	}
	if m.isNarrow() {
		return m.renderListPane(m.width, height)
	}
	l := m.layout()
	var panes []string
	x := 0
	place := func(render func() string, width int) {
		pane, hits := m.captureHits(render)
		m.placeHits(hits, x, 0)
		panes = append(panes, pane)
		x += width
	}
	if l.sidebar > 0 {
		place(func() string { return m.renderSidebar(height) }, l.sidebar)
	}
	place(func() string { return m.renderListPane(l.list, height) }, l.list)
	if m.editor != nil {
		place(func() string { return m.renderEditorPane(l.detail, height) }, l.detail)
	} else {
		place(func() string { return m.renderDetailPane(l.detail, height, m.focus == focusDetail) }, l.detail)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, panes...)
}

func (m *Model) listPageSize() int {
	// Header, footer, the pane's borders, and its heading.
	return max(1, (m.height-6)/rowHeight)
}

func (m *Model) renderListPane(width, height int) string {
	th := m.th
	style := th.pane
	if m.focus == focusList {
		style = th.paneFocused
	}
	innerW, innerH := width-4, height-2
	heading := th.bold.Render(m.views[m.viewIdx].name) + th.dim.Render(" · ") + m.scopeChip()
	if n := len(m.visible); n > 0 {
		heading = m.joinWithin(heading, th.dim.Render(fmt.Sprint(n)), innerW)
	}
	lines := []string{heading, ""}
	if len(m.visible) == 0 {
		lines = append(lines, m.emptyListLines(innerW)...)
	} else {
		perPage := max(1, (innerH-2)/rowHeight)
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
	m.listHits(width, height)
	return style.Width(width).Height(height).Render(content)
}

// listHits records the list pane's click targets: the whole pane focuses
// the list and scrolls it; each row selects its issue, and a double click
// opens it.
func (m *Model) listHits(width, height int) {
	m.hit(hitRegion{x: 0, y: 0, w: width, h: height,
		click: func(m *Model, _ tea.MouseClickMsg) tea.Cmd {
			if m.editor == nil {
				m.focus = focusList
			}
			return nil
		},
		wheel: func(m *Model, delta int) tea.Cmd {
			if m.editor == nil {
				m.moveCursor(delta)
			}
			return nil
		},
	})
	perPage := max(1, (height-2-2)/rowHeight)
	for idx := m.offset; idx < len(m.visible) && idx < m.offset+perPage; idx++ {
		key := m.visible[idx].Key()
		// Rows start below the border, heading, and blank line.
		m.hit(hitRegion{x: 1, y: 3 + (idx-m.offset)*rowHeight, w: width - 2, h: rowHeight - 1,
			click: func(m *Model, _ tea.MouseClickMsg) tea.Cmd {
				if m.editor != nil {
					return m.flash("Save (ctrl+s) or cancel (esc) the edit first.", flashInfo)
				}
				for i, candidate := range m.visible {
					if candidate.Key() == key {
						m.cursor = i
					}
				}
				m.focus = focusList
				if m.doubleClick("row:" + key) {
					if m.isNarrow() {
						m.fullDetail = true
					}
					m.focus = focusDetail
				}
				return nil
			},
		})
	}
}

// joinWithin puts right at the end of a width-wide line starting with left.
func (m *Model) joinWithin(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) emptyListLines(width int) []string {
	th := m.th
	if m.loadingFirst || (m.refreshing && len(m.snap.Issues) == 0) {
		m.progress.SetWidth(min(32, width-2))
		return []string{
			"",
			th.subtle.Render("Fetching your issues…"),
			"",
			m.progress.ViewAs(m.refreshProgress()),
			th.dim.Render(fmt.Sprintf("%d of %d repos", m.refreshDone, max(1, m.refreshTotal))),
		}
	}
	if m.filterInput.Value() != "" {
		return []string{"", th.subtle.Render("Nothing matches this filter."), th.dim.Render("Press esc to clear it.")}
	}
	art, title, hint := m.emptyState()
	lines := []string{""}
	for _, line := range art {
		lines = append(lines, th.gradient(line, false))
	}
	lines = append(lines, "", th.bold.Render(title))
	if hint != "" {
		lines = append(lines, th.dim.Render(hint))
	}
	return lines
}

// emptyState picks art and copy for an empty view. The line of copy
// changes daily so the empty inbox stays a small reward.
func (m *Model) emptyState() (art []string, title, hint string) {
	day := time.Now().YearDay()
	pick := func(options ...string) string { return options[day%len(options)] }
	switch m.views[m.viewIdx].name {
	case "Mine":
		return []string{"   .--.  ", "  ( ‿‿ ) ", "   `--´  "},
			"Nothing on your plate.",
			"Press a on an issue to take it."
	case "Closed":
		return []string{"  ┌───┐  ", "  │ ✓ │  ", "  └───┘  "},
			"Nothing closed yet.",
			"The first one always feels good."
	case "Open":
		return []string{`   \ │ /   `, " ── ( ) ── ", `   / │ \   `},
			pick("Inbox zero.", "All clear.", "Nothing open. Nice work.", "Clean slate."),
			pick("Go outside for a bit.", "Press n when the next idea strikes.", "Enjoy it while it lasts.")
	default:
		return []string{"  · · ·  "}, "Nothing here right now.", ""
	}
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

	// Most important first: the end of the line is cut on narrow panes.
	meta := []string{th.statusPill(conv.StatusOf(i)), th.renderRepo(i.Repo, i.Ref())}
	if ops := m.snap.Pending[i.Key()]; len(ops) > 0 {
		meta = append(meta, m.pendingBadge(ops))
	}
	if t := conv.TypeOf(i); t != issue.TypeTask {
		meta = append(meta, th.renderType(t))
	}
	meta = append(meta, th.dim.Render(relTime(i.UpdatedAt)))
	if i.Comments > 0 {
		meta = append(meta, th.dim.Render(fmt.Sprintf("💬%d", i.Comments)))
	}
	line := "  " + strings.Join(meta, " ")
	lines := []string{title, truncate(line, width), ""}
	if tint, ok := m.glowTint(i.Key()); ok {
		for idx := range lines[:2] {
			lines[idx] = tint.Width(width).Render(lines[idx])
		}
	}
	return lines
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
	x := 0
	for c := first; c < first+shown; c++ {
		status := boardColumns[c]
		col := c
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
		// The column itself, under its cards: clicking selects it, the
		// wheel moves within it.
		count := len(columns[c])
		m.hit(hitRegion{x: x, y: 0, w: w, h: height,
			click: func(m *Model, _ tea.MouseClickMsg) tea.Cmd { m.boardCol = col; return nil },
			wheel: func(m *Model, delta int) tea.Cmd {
				m.boardCol = col
				m.boardRow[col] = max(0, min(count-1, m.boardRow[col]+delta))
				return nil
			},
		})
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
			rowIdx, key := r, i.Key()
			// Cards start below the border, the column heading, and a blank line.
			m.hit(hitRegion{x: x + 1, y: 3 + (r-start)*3, w: colWidth - 2, h: 2, click: func(m *Model, _ tea.MouseClickMsg) tea.Cmd {
				m.boardCol, m.boardRow[col] = col, rowIdx
				if m.doubleClick("card:" + key) {
					m.fullDetail = true
					m.focus = focusDetail
				}
				return nil
			}})
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
		x += w
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
