package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
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
	body   textarea.Model
	repos  []string
	repo   int
	typ    issue.Type
	status issue.Status
	focus  formField
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
	f.title.SetStyles(textinput.DefaultStyles(m.th.isDark))
	f.body = textarea.New()
	f.body.ShowLineNumbers = false
	f.body.Prompt = ""
	f.body.Placeholder = "Description (Markdown, optional)"
	f.body.CharLimit = 65536
	f.body.SetStyles(textarea.DefaultStyles(m.th.isDark))
	if mode == formComment {
		f.focus = fieldBody
		f.body.Placeholder = "Write a comment (Markdown)"
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
	m.pushOverlay(f)
	return textinput.Blink
}

func (m *Model) openEditForm(i issue.Issue) tea.Cmd {
	f := newIssueForm(m, formEdit)
	f.target = i
	f.title.SetValue(i.Title)
	f.body.SetValue(i.Body)
	m.pushOverlay(f)
	return textinput.Blink
}

func (m *Model) openCommentForm(i issue.Issue) tea.Cmd {
	f := newIssueForm(m, formComment)
	f.target = i
	m.pushOverlay(f)
	return textarea.Blink
}

func (f *issueForm) fields() []formField {
	switch f.mode {
	case formNew:
		return []formField{fieldTitle, fieldBody, fieldRepo, fieldType, fieldStatus}
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
		return f.body.Focus()
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
	case "esc":
		if f.dirty() && !f.confirmDiscard {
			f.confirmDiscard = true
			return false, nil
		}
		return true, nil
	case "ctrl+s":
		return f.submit(m)
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
		f.body, cmd = f.body.Update(msg)
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

func (f *issueForm) view(m *Model) string {
	th := m.th
	width := min(84, m.width-6)
	innerW := width - 6
	f.title.SetWidth(innerW - 8)
	f.body.SetWidth(innerW)
	bodyHeight := max(4, min(16, m.height-22))
	if f.mode == formComment {
		bodyHeight = max(4, min(14, m.height-14))
	}
	f.body.SetHeight(bodyHeight)

	label := func(name string, field formField) string {
		style := th.dim
		if f.focus == field {
			style = th.inputPrompt
		}
		return style.Width(8).Render(name)
	}

	var heading string
	switch f.mode {
	case formNew:
		heading = "New issue"
	case formEdit:
		heading = "Edit " + f.target.Ref()
	default:
		heading = "Comment on " + f.target.Ref()
	}
	lines := []string{th.title.Render(heading), ""}

	if f.mode != formComment {
		lines = append(lines, label("Title", fieldTitle)+f.title.View(), "")
	}
	bodyBox := lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).PaddingLeft(1).BorderForeground(th.border)
	if f.focus == fieldBody {
		bodyBox = bodyBox.BorderForeground(th.borderFocus)
	}
	lines = append(lines, bodyBox.Render(f.body.View()))

	if f.mode == formNew {
		lines = append(lines, "")
		repo := f.repos[f.repo]
		repoValue := chip(th, repo, true, f.focus == fieldRepo, th.repoColor(repo))
		if len(f.repos) > 1 {
			repoValue = th.dim.Render("‹") + repoValue + th.dim.Render("›") + th.dim.Render(fmt.Sprintf("  %d of %d", f.repo+1, len(f.repos)))
		}
		lines = append(lines, label("Repo", fieldRepo)+truncate(repoValue, innerW-8))
		var typeChips []string
		for _, t := range issue.Types {
			typeChips = append(typeChips, chip(th, strings.ToLower(t.String()), t == f.typ, f.focus == fieldType, th.typeColor(t)))
		}
		lines = append(lines, label("Type", fieldType)+strings.Join(typeChips, " "))
		var statusChips []string
		for _, s := range []issue.Status{issue.StatusIdea, issue.StatusTodo, issue.StatusInProgress, issue.StatusBlocked} {
			statusChips = append(statusChips, chip(th, strings.ToLower(s.String()), s == f.status, f.focus == fieldStatus, th.statusColor(s)))
		}
		lines = append(lines, label("Status", fieldStatus)+strings.Join(statusChips, " "))
	}

	lines = append(lines, "")
	if f.err != "" {
		lines = append(lines, th.statusError.Render(f.err))
	}
	if f.confirmDiscard {
		lines = append(lines, th.statusWarn.Render("Discard what you wrote? Press esc again to discard, or keep typing."))
	}
	action := "save"
	switch f.mode {
	case formNew:
		action = "create"
	case formComment:
		action = "post"
	}
	hints := []string{th.key.Render("ctrl+s") + " " + th.keyDesc.Render(action)}
	if len(f.fields()) > 1 {
		hints = append(hints, th.key.Render("tab")+" "+th.keyDesc.Render("next field"))
	}
	if f.focus == fieldRepo || f.focus == fieldType || f.focus == fieldStatus {
		hints = append(hints, th.key.Render("←→")+" "+th.keyDesc.Render("choose"))
	}
	hints = append(hints, th.key.Render("ctrl+e")+" "+th.keyDesc.Render("$EDITOR"), th.key.Render("esc")+" "+th.keyDesc.Render("cancel"))
	lines = append(lines, strings.Join(hints, "  "))
	return th.modal.Width(width).Render(strings.Join(lines, "\n"))
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
