package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aloglu/triage/internal/editor"
	"github.com/aloglu/triage/internal/engine"
	"github.com/aloglu/triage/internal/issue"
)

// issueDraft is the content of the new-issue form.
type issueDraft struct {
	repo   string
	title  string
	body   string
	typ    issue.Type
	status issue.Status
}

type formMode int

const (
	formNew formMode = iota
	formEdit
	formComment
)

type formField int

const (
	fieldTitle formField = iota
	fieldBody
	fieldRepo
	fieldType
	fieldStatus
)

// issueForm edits a new issue, an existing issue's text, or a comment.
type issueForm struct {
	mode   formMode
	target issue.Issue
	title  textinput.Model
	body   *mdEditor
	// preview shows the description rendered instead of the editor.
	preview       bool
	previewScroll int
	repos         []string
	repo          int
	typ           issue.Type
	status        issue.Status
	focus         formField
	// confirmDiscard is set after esc on a form with unsaved text.
	confirmDiscard bool
	err            string
}

func newIssueForm(m *Model, mode formMode) *issueForm {
	f := &issueForm{mode: mode, status: issue.StatusTodo}
	f.title = textinput.New()
	f.title.Prompt = ""
	f.title.Placeholder = "Title"
	f.title.CharLimit = 256
	// The title reads like the reading view's title: bold.
	titleStyles := m.inputStyles()
	titleStyles.Focused.Text = titleStyles.Focused.Text.Bold(true).Foreground(m.th.text)
	titleStyles.Blurred.Text = titleStyles.Blurred.Text.Bold(true).Foreground(m.th.text)
	f.title.SetStyles(titleStyles)
	f.body = newMDEditor()
	f.body.placeholder = "Describe it (Markdown works: **bold**, `code`, - lists, links)"
	if mode == formComment {
		f.focus = fieldBody
		f.body.placeholder = "Write a comment (Markdown works)"
		f.body.Focus()
	} else {
		f.title.Focus()
	}
	return f
}

// defaultRepo picks where a new issue goes: the repo of the current view,
// then the selected issue's repo, then the current directory, then the
// configured default.
func (m *Model) defaultRepo() string {
	if m.scope != "" {
		return m.scope
	}
	if i, ok := m.selected(); ok {
		return i.Repo
	}
	if repo, err := m.env.ResolveRepo(""); err == nil && m.env.Config.HasRepo(repo) {
		return repo
	}
	if len(m.env.Config.Repos) > 0 {
		return m.env.Config.Repos[0]
	}
	return ""
}

func (m *Model) openNewIssueForm() tea.Cmd {
	if len(m.env.Config.Repos) == 0 {
		return m.flash("Track a repo first.", flashWarn)
	}
	f := newIssueForm(m, formNew)
	f.repos = m.env.Config.Repos
	def := m.defaultRepo()
	for idx, repo := range f.repos {
		if strings.EqualFold(repo, def) {
			f.repo = idx
		}
	}
	m.openEditor(f)
	return textinput.Blink
}

func (m *Model) openEditForm(i issue.Issue) tea.Cmd {
	f := newIssueForm(m, formEdit)
	f.target = i
	f.title.SetValue(i.Title)
	f.body.SetValue(i.Body)
	m.openEditor(f)
	return textinput.Blink
}

func (m *Model) openCommentForm(i issue.Issue) tea.Cmd {
	f := newIssueForm(m, formComment)
	f.target = i
	m.openEditor(f)
	return nil
}

func (f *issueForm) fields() []formField {
	switch f.mode {
	case formNew:
		// Top to bottom, left to right, as they appear on screen.
		return []formField{fieldTitle, fieldRepo, fieldStatus, fieldType, fieldBody}
	case formEdit:
		return []formField{fieldTitle, fieldBody}
	default:
		return []formField{fieldBody}
	}
}

func (f *issueForm) dirty() bool {
	switch f.mode {
	case formEdit:
		return f.title.Value() != f.target.Title || strings.TrimSpace(f.body.Value()) != strings.TrimSpace(f.target.Body)
	default:
		return strings.TrimSpace(f.title.Value()) != "" || strings.TrimSpace(f.body.Value()) != ""
	}
}

func (f *issueForm) setFocus(field formField) tea.Cmd {
	f.focus = field
	f.title.Blur()
	f.body.Blur()
	switch field {
	case fieldTitle:
		return f.title.Focus()
	case fieldBody:
		f.body.Focus()
	}
	return nil
}

func (f *issueForm) moveFocus(delta int) tea.Cmd {
	fields := f.fields()
	pos := 0
	for idx, field := range fields {
		if field == f.focus {
			pos = idx
		}
	}
	pos = (pos + delta + len(fields)) % len(fields)
	return f.setFocus(fields[pos])
}

func (f *issueForm) update(m *Model, msg tea.Msg) (bool, tea.Cmd) {
	press, isKey := msg.(tea.KeyPressMsg)
	if !isKey {
		return false, f.updateInputs(msg)
	}
	pressed := press.String()
	if pressed != "esc" {
		f.confirmDiscard = false
	}
	switch pressed {
	case "alt+esc":
		// Terminals report two quick escapes as one alt+esc: discard.
		return true, nil
	case "esc":
		if f.dirty() && !f.confirmDiscard {
			f.confirmDiscard = true
			return false, nil
		}
		return true, nil
	case "ctrl+s":
		return f.submit(m)
	case "ctrl+p":
		f.preview = !f.preview
		f.previewScroll = 0
		if !f.preview {
			return false, f.setFocus(fieldBody)
		}
		return false, nil
	case "ctrl+e":
		return true, f.openInEditor(m)
	case "tab":
		return false, f.moveFocus(1)
	case "shift+tab":
		return false, f.moveFocus(-1)
	case "enter":
		if f.focus == fieldTitle {
			return false, f.moveFocus(1)
		}
		if f.focus != fieldBody {
			return f.submit(m)
		}
	}
	if f.preview {
		// The preview scrolls; typing goes back to writing.
		switch pressed {
		case "up", "k":
			f.previewScroll = max(0, f.previewScroll-1)
			return false, nil
		case "down", "j":
			f.previewScroll++
			return false, nil
		}
		f.preview = false
		_ = f.setFocus(fieldBody)
	}
	switch f.focus {
	case fieldRepo:
		switch pressed {
		case "left", "h":
			f.repo = (f.repo - 1 + len(f.repos)) % len(f.repos)
		case "right", "l", "space":
			f.repo = (f.repo + 1) % len(f.repos)
		}
		return false, nil
	case fieldType:
		f.typ = issue.Type(cycle(int(f.typ), len(issue.Types), pressed))
		for _, t := range issue.Types {
			if pressed == strings.ToLower(t.String()[:1]) {
				f.typ = t
			}
		}
		return false, nil
	case fieldStatus:
		open := []issue.Status{issue.StatusIdea, issue.StatusTodo, issue.StatusInProgress, issue.StatusBlocked}
		pos := 0
		for idx, s := range open {
			if s == f.status {
				pos = idx
			}
		}
		f.status = open[cycle(pos, len(open), pressed)]
		return false, nil
	}
	return false, f.updateInputs(msg)
}

func cycle(pos, n int, pressed string) int {
	switch pressed {
	case "left", "h":
		return (pos - 1 + n) % n
	case "right", "l", "space":
		return (pos + 1) % n
	}
	return pos
}

func (f *issueForm) updateInputs(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch f.focus {
	case fieldTitle:
		f.title, cmd = f.title.Update(msg)
	case fieldBody:
		f.body.Update(msg)
	}
	return cmd
}

func (f *issueForm) submit(m *Model) (bool, tea.Cmd) {
	title := strings.TrimSpace(f.title.Value())
	body := f.body.Value()
	switch f.mode {
	case formComment:
		if strings.TrimSpace(body) == "" {
			f.err = "Write something first."
			return false, nil
		}
		current := m.currentVersion(f.target)
		ops, err := m.eng.Comment(current, body)
		return true, m.record("Commented on "+current.Ref(), current, ops, err)
	case formEdit:
		if title == "" {
			f.err = "The title can't be empty."
			return false, f.setFocus(fieldTitle)
		}
		current := m.currentVersion(f.target)
		ops, err := m.eng.Edit(current, title, body)
		if err == nil && len(ops) == 0 {
			return true, nil
		}
		return true, m.record("Saved "+current.Ref(), current, ops, err)
	default:
		if title == "" {
			f.err = "Give the issue a title."
			return false, f.setFocus(fieldTitle)
		}
		return true, m.createFromDraft(f.draft())
	}
}

func (f *issueForm) draft() issueDraft {
	return issueDraft{repo: f.repos[f.repo], title: f.title.Value(), body: f.body.Value(), typ: f.typ, status: f.status}
}

// currentVersion returns the latest copy of i, which may have changed while
// a form was open.
func (m *Model) currentVersion(i issue.Issue) issue.Issue {
	if current, ok := m.findIssue(i.Key(), i.LocalID); ok {
		return current
	}
	return i
}

func (m *Model) createFromDraft(d issueDraft) tea.Cmd {
	ops, err := m.eng.Create(engine.Draft{Repo: d.repo, Title: d.title, Body: d.body, Type: d.typ, Status: d.status})
	if err != nil {
		return m.flash(err.Error(), flashError)
	}
	before := issue.Issue{Repo: d.repo, LocalID: ops[0].LocalID}
	cmd := m.record("Created “"+strings.TrimSpace(d.title)+"”", issue.Issue{}, ops, nil)
	m.undo[len(m.undo)-1].key = before.Key()
	m.undo[len(m.undo)-1].localID = before.LocalID
	// Show the new issue.
	if !m.board {
		m.filterInput.SetValue("")
		if m.scope != "" && !strings.EqualFold(m.scope, d.repo) {
			m.setScope(d.repo)
		}
		m.refilter(before.Key())
	}
	return cmd
}

func (f *issueForm) openInEditor(m *Model) tea.Cmd {
	switch f.mode {
	case formNew:
		d := f.draft()
		help := fmt.Sprintf("New issue in %s · Type: %s · Status: %s", d.repo, d.typ, d.status)
		return m.runEditor(editor.Template(d.title, d.body, help), func(path string, err error) tea.Msg {
			return editorDoneMsg{path: path, err: err, draft: d}
		})
	case formEdit:
		target := f.target
		target.Title, target.Body = f.title.Value(), f.body.Value()
		original := f.target
		return m.runEditor(editor.Template(target.Title, target.Body, "Editing "+original.Ref()), func(path string, err error) tea.Msg {
			return editorDoneMsg{path: path, err: err, target: &original}
		})
	default:
		target := f.target
		doc := strings.TrimSpace(f.body.Value()) + "\n\n<!-- triage:\nWrite your comment above. Save and close to post it; leave it empty to cancel.\n-->\n"
		return m.runEditor(doc, func(path string, err error) tea.Msg {
			return commentEditorMsg{path: path, err: err, target: target}
		})
	}
}

type commentEditorMsg struct {
	path   string
	err    error
	target issue.Issue
}

func (m *Model) handleCommentEditor(msg commentEditorMsg) tea.Cmd {
	doc, readErr := editor.ReadAndRemove(msg.path)
	if msg.err != nil || readErr != nil {
		return m.flash("Editor failed.", flashError)
	}
	title, body, ok := editor.Parse(doc)
	if !ok {
		return m.flash("Empty comment; nothing posted.", flashInfo)
	}
	text := strings.TrimSpace(title + "\n" + body)
	current := m.currentVersion(msg.target)
	ops, err := m.eng.Comment(current, text)
	return m.record("Commented on "+current.Ref(), current, ops, err)
}

// openEditor shows f in the right-hand pane.
func (m *Model) openEditor(f *issueForm) {
	m.editor = f
	m.fullDetail = false
	m.focus = focusDetail
}

// closeEditor returns from the editor to where the user was.
func (m *Model) closeEditor() {
	editing := m.editor
	m.editor = nil
	m.detailKey = ""
	if editing != nil && editing.mode == formNew {
		m.focus = focusList
	} else {
		m.focus = focusDetail
	}
}

// renderEditorPane draws the editor in place of the detail pane.
func (m *Model) renderEditorPane(width, height int) string {
	style := m.th.paneFocused
	innerW, innerH := width-4, height-2
	content, hits := m.captureHits(func() string { return m.editor.render(m, innerW, innerH) })
	// Content starts inside the border and padding.
	m.placeHits(hits, 2, 1)
	return style.Width(width).Height(height).Render(fitLines(strings.Split(content, "\n"), innerW, innerH))
}

// render draws the form for a width×height area and records the click
// targets of its fields. It is the reading view with the editable parts
// swapped in: the same title, reference line, pills, spacing, and rule,
// with the description written in place.
func (f *issueForm) render(m *Model, width, height int) string {
	th := m.th
	conv := m.eng.Convention()
	// This pane's click targets start here; only these move if the top of
	// the pane scrolls off.
	firstHit := len(m.hits)
	var lines []string
	add := func(line string) { lines = append(lines, line) }
	focusField := func(field formField) func(*Model, tea.MouseClickMsg) tea.Cmd {
		return func(m *Model, _ tea.MouseClickMsg) tea.Cmd { f.preview = false; return f.setFocus(field) }
	}
	// chooser wraps a value in ‹ › while its field has focus, so it's clear
	// where tab landed and that ←/→ change it.
	chooser := func(field formField, value string) string {
		if f.focus != field {
			return value
		}
		return th.key.Render("‹ ") + value + th.key.Render(" ›")
	}

	// Write / Preview, shown at the right of the reference line.
	write, previewTab := th.tabInactive.Render("Write"), th.tabInactive.Render("Preview")
	if f.preview {
		previewTab = th.tabActive.Render("Preview")
	} else {
		write = th.tabActive.Render("Write")
	}
	tabs := write + th.dim.Render(" · ") + previewTab
	tabHits := func(y int) {
		x := width - lipgloss.Width(tabs)
		m.hit(hitRegion{x: x, y: y, w: 5, h: 1, click: func(*Model, tea.MouseClickMsg) tea.Cmd {
			f.preview = false
			return f.setFocus(fieldBody)
		}})
		m.hit(hitRegion{x: x + 8, y: y, w: 7, h: 1, click: func(*Model, tea.MouseClickMsg) tea.Cmd {
			f.preview, f.previewScroll = true, 0
			return nil
		}})
	}

	if f.mode == formComment {
		// The issue exactly as it reads, then the new comment where it will
		// appear: at the end.
		context := m.detailLines(f.target, width)
		if len(context) > 0 {
			context = context[:len(context)-1] // the reading view's key hints
		}
		var rows []string
		for _, line := range context {
			rows = append(rows, strings.Split(line, "\n")...)
		}
		if f.target.Comments == 0 && len(m.comments[f.target.Key()].comments) == 0 {
			rows = append(rows, "", th.dim.Render(strings.Repeat("─", width)))
		}
		rows = append(rows, "")
		me := m.me
		if me == "" {
			me = "you"
		}
		tabHits(len(rows))
		rows = append(rows, m.joinWithin(th.bold.Render("@"+me)+th.dim.Render(" · new comment"), tabs, width))
		lines = rows
	} else {
		// Title, like the reading view's bold title.
		f.title.SetWidth(max(10, width-1))
		m.hit(hitRegion{x: 0, y: len(lines), w: width, h: 1, click: focusField(fieldTitle)})
		add(f.title.View())

		// Reference line.
		y := len(lines)
		var ref string
		if f.mode == formNew {
			repo := f.repos[f.repo]
			name := lipgloss.NewStyle().Foreground(th.repoColor(repo)).Render(repo)
			ref = chooser(fieldRepo, name)
			if len(f.repos) > 1 {
				m.hit(hitRegion{x: 0, y: y, w: lipgloss.Width(ref), h: 1, click: func(m *Model, _ tea.MouseClickMsg) tea.Cmd {
					m.openFormRepoPicker(f)
					return f.setFocus(fieldRepo)
				}})
			}
			ref += th.dim.Render(" · new issue")
		} else {
			ref = th.renderRepo(f.target.Repo, f.target.Ref()) + th.dim.Render(" · editing")
		}
		tabHits(y)
		add(m.joinWithin(ref, tabs, width))
		add("")

		// Status and type, as the same pills.
		status, typ := f.status, f.typ
		if f.mode == formEdit {
			status, typ = conv.StatusOf(f.target), conv.TypeOf(f.target)
		}
		pill := chooser(fieldStatus, th.statusPill(status))
		kind := chooser(fieldType, th.renderType(typ))
		y = len(lines)
		if f.mode == formNew {
			m.hit(hitRegion{x: 0, y: y, w: lipgloss.Width(pill), h: 1, click: func(m *Model, _ tea.MouseClickMsg) tea.Cmd {
				m.openFormStatusPicker(f)
				return f.setFocus(fieldStatus)
			}})
			m.hit(hitRegion{x: lipgloss.Width(pill) + 3, y: y, w: lipgloss.Width(kind), h: 1, click: func(m *Model, _ tea.MouseClickMsg) tea.Cmd {
				m.openFormTypePicker(f)
				return f.setFocus(fieldType)
			}})
		}
		add(pill + "   " + kind)
		if f.mode == formEdit {
			var extra []string
			for _, label := range f.target.Labels {
				if !conv.IsConventionLabel(label) {
					extra = append(extra, label)
				}
			}
			if len(extra) > 0 {
				add(th.dim.Render("labels    ") + th.subtle.Render(strings.Join(extra, ", ")))
			}
			if len(f.target.Assignees) > 0 {
				add(th.dim.Render("assigned  ") + th.subtle.Render("@"+strings.Join(f.target.Assignees, ", @")))
			}
		}
		add("")
		add(th.dim.Render(strings.Repeat("─", width)))
	}
	add("")

	var notes []string
	if f.err != "" {
		notes = append(notes, th.statusError.Render(f.err))
	}
	if f.confirmDiscard {
		notes = append(notes, th.statusWarn.Render("Discard what you wrote? Press esc again to discard, or keep typing."))
	}
	reserved := len(notes)
	if reserved > 0 {
		reserved++
	}
	// The description or comment gets the rest; in a long comment thread
	// the issue scrolls off the top so the composer keeps enough room.
	minBody := min(8, max(3, height/3))
	if over := len(lines) + minBody + reserved - height; over > 0 {
		cut := min(len(lines), over+1)
		lines = append([]string{th.dim.Render("⋮")}, lines[cut:]...)
		kept := m.hits[:firstHit]
		for _, r := range m.hits[firstHit:] {
			r.y -= cut - 1
			if r.y >= 1 {
				kept = append(kept, r)
			}
		}
		m.hits = kept
	}
	bodyH := max(3, height-len(lines)-reserved)
	m.hit(hitRegion{x: 0, y: len(lines), w: width, h: bodyH, click: focusField(fieldBody),
		wheel: func(m *Model, delta int) tea.Cmd {
			if f.preview {
				f.previewScroll = max(0, f.previewScroll+delta)
			}
			return nil
		}})
	if f.preview {
		rendered := []string{th.dim.Render("Nothing to preview yet.")}
		if strings.TrimSpace(f.body.Value()) != "" {
			rendered = strings.Split(m.md.render(f.body.Value(), width, th.isDark), "\n")
		}
		f.previewScroll = min(f.previewScroll, max(0, len(rendered)-bodyH))
		lines = append(lines, rendered[f.previewScroll:min(len(rendered), f.previewScroll+bodyH)]...)
	} else {
		f.body.SetSize(width, bodyH)
		lines = append(lines, strings.Split(f.body.View(th), "\n")...)
	}
	for len(lines) < height-reserved {
		lines = append(lines, "")
	}
	if len(notes) > 0 {
		lines = append(lines, "")
		lines = append(lines, notes...)
	}
	return strings.Join(lines, "\n")
}

// openFormStatusPicker chooses a new issue's status, like s does for an
// existing issue.
func (m *Model) openFormStatusPicker(f *issueForm) {
	open := []issue.Status{issue.StatusIdea, issue.StatusTodo, issue.StatusInProgress, issue.StatusBlocked}
	keys := map[issue.Status]string{issue.StatusIdea: "i", issue.StatusTodo: "t", issue.StatusInProgress: "p", issue.StatusBlocked: "b"}
	var items []pickerItem
	for _, st := range open {
		items = append(items, pickerItem{id: st.String(), label: m.th.statusIcon(st) + " " + st.String(), key: keys[st], color: m.th.statusColor(st)})
	}
	p := newPicker(m, "Status", items)
	for i, st := range open {
		if st == f.status {
			p.cursor = i
		}
	}
	p.onDone = func(m *Model, chosen []pickerItem, _ string) tea.Cmd {
		for _, st := range open {
			if chosen[0].id == st.String() {
				f.status = st
			}
		}
		return nil
	}
	m.pushOverlay(p)
}

// openFormTypePicker chooses a new issue's type.
func (m *Model) openFormTypePicker(f *issueForm) {
	keys := map[issue.Type]string{issue.TypeTask: "t", issue.TypeBug: "b", issue.TypeFeature: "f", issue.TypeDocs: "d"}
	var items []pickerItem
	for _, ty := range issue.Types {
		items = append(items, pickerItem{id: ty.String(), label: ty.String(), key: keys[ty], color: m.th.typeColor(ty)})
	}
	p := newPicker(m, "Type", items)
	p.cursor = int(f.typ)
	p.onDone = func(m *Model, chosen []pickerItem, _ string) tea.Cmd {
		for _, ty := range issue.Types {
			if chosen[0].id == ty.String() {
				f.typ = ty
			}
		}
		return nil
	}
	m.pushOverlay(p)
}

// openFormRepoPicker chooses which repo a new issue goes to.
func (m *Model) openFormRepoPicker(f *issueForm) {
	var items []pickerItem
	for _, repo := range f.repos {
		items = append(items, pickerItem{id: repo, label: repo, color: m.th.repoColor(repo)})
	}
	p := newPicker(m, "Repository", items)
	p.cursor = f.repo
	p.onDone = func(m *Model, chosen []pickerItem, _ string) tea.Cmd {
		for i, repo := range f.repos {
			if repo == chosen[0].id {
				f.repo = i
			}
		}
		return nil
	}
	m.pushOverlay(p)
}

// hints are the editor's keys, shown in the footer while it's open, fitted
// to width.
func (f *issueForm) hints(m *Model, width int) string {
	th := m.th
	action := "save"
	switch f.mode {
	case formNew:
		action = "create"
	case formComment:
		action = "post"
	}
	hint := func(k, desc string) string { return th.key.Render(k) + " " + th.keyDesc.Render(desc) }
	parts := []string{hint("ctrl+s", action)}
	if len(f.fields()) > 1 {
		parts = append(parts, hint("tab", "next field"))
	}
	if f.focus == fieldRepo || f.focus == fieldType || f.focus == fieldStatus {
		parts = append(parts, hint("←→", "choose"))
	}
	preview := "preview"
	if f.preview {
		preview = "write"
	}
	parts = append(parts, hint("ctrl+p", preview), hint("ctrl+e", "$EDITOR"), hint("esc", "cancel"))
	return fitHints(parts, width)
}

func chip(th theme, text string, chosen, focused bool, c color.Color) string {
	style := lipgloss.NewStyle().Padding(0, 1)
	if chosen {
		style = style.Foreground(c).Bold(true).Border(lipgloss.HiddenBorder(), false)
		if focused {
			return style.Reverse(true).Render(text)
		}
		return style.Underline(true).Render(text)
	}
	return style.Foreground(th.faint).Render(text)
}

// --- board ---

func (m *Model) handleBoardKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := m.keys
	columns := m.boardIssues()
	switch {
	case msg.String() == "left" || msg.String() == "h":
		m.boardCol = max(0, m.boardCol-1)
	case msg.String() == "right" || msg.String() == "l":
		m.boardCol = min(len(boardColumns)-1, m.boardCol+1)
	case key.Matches(msg, k.Up):
		m.boardRow[m.boardCol] = max(0, m.boardRow[m.boardCol]-1)
	case key.Matches(msg, k.Down):
		m.boardRow[m.boardCol] = min(max(0, len(columns[m.boardCol])-1), m.boardRow[m.boardCol]+1)
	case key.Matches(msg, k.Top):
		m.boardRow[m.boardCol] = 0
	case key.Matches(msg, k.Bottom):
		m.boardRow[m.boardCol] = max(0, len(columns[m.boardCol])-1)
	case msg.String() == "enter":
		if _, ok := m.selected(); ok {
			m.fullDetail = true
			m.focus = focusDetail
			m.detailKey = ""
			return m.ensureComments(), true
		}
	case key.Matches(msg, k.MoveLeft), key.Matches(msg, k.MoveRight):
		i, ok := m.selected()
		if !ok {
			return nil, true
		}
		target := boardColumns[max(0, m.boardCol-1)]
		if key.Matches(msg, k.MoveRight) {
			target = boardColumns[min(len(boardColumns)-1, m.boardCol+1)]
		}
		cmd := m.changeStatus(i, target)
		// Follow the card to its new column.
		for c, col := range m.boardIssues() {
			for r, candidate := range col {
				if candidate.Key() == i.Key() {
					m.boardCol, m.boardRow[c] = c, r
				}
			}
		}
		return cmd, true
	default:
		return nil, false
	}
	return nil, true
}
