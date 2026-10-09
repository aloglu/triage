package tui

import (
	"fmt"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/aloglu/triage/internal/issue"
	"github.com/aloglu/triage/internal/store"
)

// markdown renders Markdown with glamour, caching a renderer per width and
// background.
type markdown struct {
	key      string
	renderer *glamour.TermRenderer
	cache    map[string]string
}

func newMarkdown() *markdown { return &markdown{cache: map[string]string{}} }

func (md *markdown) render(text string, width int, dark bool) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\t", "    "))
	if text == "" {
		return ""
	}
	style := "light"
	if dark {
		style = "dark"
	}
	key := fmt.Sprintf("%s/%d", style, width)
	if md.key != key || md.renderer == nil {
		cfg := styles.LightStyleConfig
		if dark {
			cfg = styles.DarkStyleConfig
		}
		// The panes already have padding; drop glamour's own margin.
		zero := uint(0)
		cfg.Document.Margin = &zero
		r, err := glamour.NewTermRenderer(glamour.WithStyles(cfg), glamour.WithWordWrap(width), glamour.WithPreservedNewLines())
		if err != nil {
			return lipgloss.NewStyle().Width(width).Render(text)
		}
		md.key, md.renderer, md.cache = key, r, map[string]string{}
	}
	if out, ok := md.cache[text]; ok {
		return out
	}
	out, err := md.renderer.Render(text)
	if err != nil {
		out = lipgloss.NewStyle().Width(width).Render(text)
	}
	out = strings.ReplaceAll(strings.Trim(out, "\n"), "\t", "    ")
	md.cache[text] = out
	return out
}

func (m *Model) renderDetailPane(width, height int, focused bool) string {
	th := m.th
	style := th.pane
	if focused {
		style = th.paneFocused
	}
	innerW, innerH := width-4, height-2
	m.hit(hitRegion{x: 0, y: 0, w: width, h: height,
		click: func(m *Model, _ tea.MouseClickMsg) tea.Cmd {
			if _, ok := m.selected(); ok {
				m.focus = focusDetail
			}
			return nil
		},
		wheel: func(m *Model, delta int) tea.Cmd {
			if delta < 0 {
				m.detail.ScrollUp(3)
			} else {
				m.detail.ScrollDown(3)
			}
			return nil
		},
	})
	i, ok := m.selected()
	if !ok {
		return style.Width(width).Height(height).Render(fitLines(m.detailEmptyLines(), innerW, innerH))
	}

	contentKey := fmt.Sprintf("%s|%d|%d|%v|%d|%v", i.Key(), innerW, innerH, th.isDark, len(m.snap.Pending[i.Key()]), i.UpdatedAt)
	if c := m.comments[i.Key()]; c.loading || c.comments != nil || c.err != nil {
		contentKey += fmt.Sprintf("|c%d%v", len(c.comments), c.loading)
	}
	if contentKey != m.detailKey {
		sameIssue := strings.HasPrefix(m.detailKey, i.Key()+"|")
		m.detailKey = contentKey
		m.detail.SetWidth(innerW)
		m.detail.SetHeight(innerH)
		content := m.detailLines(i, innerW)
		m.detail.SetContent(strings.Join(content, "\n"))
		m.detailPlain = strings.Split(ansi.Strip(strings.Join(content, "\n")), "\n")
		if !sameIssue {
			m.detail.GotoTop()
		}
	}
	m.linkHits(innerW, innerH)
	return style.Width(width).Height(height).Render(m.detail.View())
}

func (m *Model) detailEmptyLines() []string {
	th := m.th
	return []string{
		"",
		th.subtle.Render("Select an issue to read it here."),
		"",
		th.key.Render("n") + th.keyDesc.Render(" new issue   ") + th.key.Render("?") + th.keyDesc.Render(" all shortcuts"),
	}
}

func (m *Model) detailLines(i issue.Issue, width int) []string {
	th := m.th
	conv := m.eng.Convention()
	var lines []string
	lines = append(lines, lipgloss.NewStyle().Width(width).Render(th.title.Render(i.Title)))

	ref := th.renderRepo(i.Repo, i.Ref())
	if i.URL != "" {
		// A real terminal hyperlink, for terminals that handle link clicks
		// themselves.
		ref = ansi.SetHyperlink(i.URL) + ref + ansi.ResetHyperlink()
	}
	sub := []string{ref}
	if i.Author != "" {
		sub = append(sub, th.subtle.Render("opened by @"+i.Author))
	}
	if !i.UpdatedAt.IsZero() {
		sub = append(sub, th.dim.Render("updated "+relTime(i.UpdatedAt)))
	}
	lines = append(lines, strings.Join(sub, th.dim.Render(" · ")), "")

	meta := []string{th.statusPill(conv.StatusOf(i)), th.renderType(conv.TypeOf(i))}
	lines = append(lines, strings.Join(meta, "   "))
	var extra []string
	for _, label := range i.Labels {
		if !conv.IsConventionLabel(label) {
			extra = append(extra, label)
		}
	}
	if len(extra) > 0 {
		lines = append(lines, th.dim.Render("labels    ")+th.subtle.Render(strings.Join(extra, ", ")))
	}
	if len(i.Assignees) > 0 {
		lines = append(lines, th.dim.Render("assigned  ")+th.subtle.Render("@"+strings.Join(i.Assignees, ", @")))
	}

	if ops := m.snap.Pending[i.Key()]; len(ops) > 0 {
		lines = append(lines, "")
		lines = append(lines, m.pendingLines(ops, width)...)
	}

	lines = append(lines, "", th.dim.Render(strings.Repeat("─", width)), "")
	if body := strings.TrimSpace(i.Body); body != "" {
		lines = append(lines, strings.Split(m.md.render(body, width, th.isDark), "\n")...)
	} else {
		lines = append(lines, th.dim.Render("No description."))
	}

	if i.Comments > 0 || len(m.comments[i.Key()].comments) > 0 {
		lines = append(lines, "", th.dim.Render(strings.Repeat("─", width)), "")
		lines = append(lines, m.commentLines(i, width)...)
	}
	lines = append(lines, "", th.dim.Render("c comment · e edit · o open on GitHub"))
	return lines
}

func (m *Model) pendingLines(ops []store.Op, width int) []string {
	th := m.th
	box := lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).PaddingLeft(1).Width(width)
	var lines []string
	for _, op := range ops {
		switch {
		case op.Conflict != nil:
			var b strings.Builder
			b.WriteString(th.statusError.Render("This issue changed on GitHub while you were editing it."))
			if op.Conflict.RemoteTitle != nil {
				b.WriteString("\n" + th.dim.Render("Their title: ") + *op.Conflict.RemoteTitle)
				b.WriteString("\n" + th.dim.Render("Your title:  ") + deref(op.Title))
			}
			if op.Conflict.RemoteBody != nil {
				b.WriteString("\n" + th.dim.Render("Their description:\n") + truncateLines(*op.Conflict.RemoteBody, 6))
				b.WriteString("\n" + th.dim.Render("Your description:\n") + truncateLines(deref(op.Body), 6))
			}
			b.WriteString("\n" + th.key.Render("K") + th.keyDesc.Render(" keep mine  ") + th.key.Render("T") + th.keyDesc.Render(" take theirs"))
			lines = append(lines, box.BorderForeground(th.danger).Render(b.String()))
		case op.Error != "":
			text := th.statusError.Render(describeOp(op, m)+" failed: ") + op.Error +
				"\n" + th.key.Render("R") + th.keyDesc.Render(" retry  ") + th.key.Render("D") + th.keyDesc.Render(" discard")
			lines = append(lines, box.BorderForeground(th.danger).Render(text))
		default:
			state := "waiting to send"
			if op.Sending() {
				state = "sending"
			}
			lines = append(lines, th.statusWarn.Render("↑ ")+th.subtle.Render(describeOp(op, m))+th.dim.Render(" · "+state))
		}
	}
	return lines
}

func describeOp(op store.Op, m *Model) string {
	switch op.Kind {
	case store.OpCreate:
		return "Create issue"
	case store.OpEdit:
		return "Edit"
	case store.OpLabels:
		var parts []string
		for _, label := range op.Add {
			parts = append(parts, "+"+label)
		}
		for _, label := range op.Remove {
			parts = append(parts, "−"+label)
		}
		return "Labels " + strings.Join(parts, " ")
	case store.OpState:
		if op.State == issue.StateClosed {
			if op.StateReason == issue.ReasonNotPlanned {
				return "Close as not planned"
			}
			return "Close"
		}
		return "Reopen"
	case store.OpAssign:
		if len(op.Add) > 0 {
			return "Assign @" + strings.Join(op.Add, ", @")
		}
		return "Unassign @" + strings.Join(op.Remove, ", @")
	case store.OpComment:
		return "Comment"
	}
	return string(op.Kind)
}

func (m *Model) commentLines(i issue.Issue, width int) []string {
	th := m.th
	state := m.comments[i.Key()]
	header := th.sectionHeading.Render(fmt.Sprintf("Comments (%d)", max(i.Comments, len(state.comments))))
	lines := []string{header, ""}
	switch {
	case state.loading && state.comments == nil:
		return append(lines, th.dim.Render("Loading…"))
	case state.err != nil && state.comments == nil:
		return append(lines, th.statusError.Render("Couldn't load comments. Press r to retry."))
	case state.comments == nil && (m.offline || !m.eng.Online()):
		return append(lines, th.dim.Render("Comments will load when you're back online."))
	case state.comments == nil:
		return append(lines, th.dim.Render("Loading…"))
	}
	for _, c := range state.comments {
		lines = append(lines, th.bold.Render("@"+c.Author)+th.dim.Render(" · "+relTime(c.CreatedAt)))
		body := m.md.render(c.Body, width-2, th.isDark)
		for _, line := range strings.Split(body, "\n") {
			lines = append(lines, line)
		}
		lines = append(lines, "")
	}
	return lines
}

func truncateLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "…")
	}
	return strings.Join(lines, "\n")
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

var urlPattern = regexp.MustCompile(`https?://[^\s<>"'\x60]+`)

// linkHits makes links in the visible part of the detail pane clickable
// with ctrl (or cmd) held, the usual terminal convention. A link that was
// wrapped onto the next line is followed in full.
func (m *Model) linkHits(innerW, innerH int) {
	top := m.detail.YOffset()
	for row := 0; row < innerH && top+row < len(m.detailPlain); row++ {
		line := m.detailPlain[top+row]
		for _, loc := range urlPattern.FindAllStringIndex(line, -1) {
			url := strings.TrimRight(line[loc[0]:loc[1]], ".,;:!?)]")
			// Continue onto the next line when the link ran to the wrap edge.
			if ansi.StringWidth(line[:loc[1]]) >= innerW-1 && top+row+1 < len(m.detailPlain) {
				next := strings.TrimSpace(m.detailPlain[top+row+1])
				if fields := strings.Fields(next); len(fields) > 0 && !strings.Contains(fields[0], "://") {
					url += strings.TrimRight(fields[0], ".,;:!?)]")
				}
			}
			target := url
			x := ansi.StringWidth(line[:loc[0]])
			w := ansi.StringWidth(line[loc[0]:loc[1]])
			// Inside the pane's border and padding.
			m.hit(hitRegion{x: 2 + x, y: 1 + row, w: w, h: 1, click: func(m *Model, msg tea.MouseClickMsg) tea.Cmd {
				if !openLinkClick(msg) {
					m.focus = focusDetail
					return m.flash("Ctrl+click to open the link.", flashInfo)
				}
				return openURLCmd(m.env.OpenURL, target)
			}})
		}
	}
	// The issue reference under the title opens the issue itself. The
	// title may wrap, so find the line.
	i, ok := m.selected()
	if !ok || i.URL == "" {
		return
	}
	refLine := -1
	for idx, line := range m.detailPlain {
		if strings.HasPrefix(line, i.Ref()) {
			refLine = idx
			break
		}
	}
	if row := refLine - top; refLine >= 0 && row >= 0 && row < innerH {
		url := i.URL
		m.hit(hitRegion{x: 2, y: 1 + row, w: ansi.StringWidth(i.Ref()), h: 1, click: func(m *Model, msg tea.MouseClickMsg) tea.Cmd {
			if !openLinkClick(msg) {
				m.focus = focusDetail
				return m.flash("Ctrl+click to open the issue on GitHub (or press o).", flashInfo)
			}
			return openURLCmd(m.env.OpenURL, url)
		}})
	}
}
