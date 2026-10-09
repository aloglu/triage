package tui

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aloglu/triage/internal/config"
	"github.com/aloglu/triage/internal/editor"
	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/issue"
	"github.com/aloglu/triage/internal/store"
)

// handleIssueKey runs actions on the selected issue.
func (m *Model) handleIssueKey(msg tea.KeyPressMsg) tea.Cmd {
	i, ok := m.selected()
	if !ok {
		return nil
	}
	k := m.keys
	conv := m.eng.Convention()
	switch {
	case key.Matches(msg, k.Status):
		m.openStatusPicker(i)
	case key.Matches(msg, k.MoveRight):
		return m.changeStatus(i, nextStatus(conv.StatusOf(i), true))
	case key.Matches(msg, k.MoveLeft):
		return m.changeStatus(i, nextStatus(conv.StatusOf(i), false))
	case key.Matches(msg, k.Type):
		m.openTypePicker(i)
	case key.Matches(msg, k.Labels):
		m.openLabelPicker(i)
	case key.Matches(msg, k.AssignMe):
		return m.toggleAssignMe(i)
	case key.Matches(msg, k.Close):
		if i.IsOpen() {
			return m.changeStatus(i, issue.StatusDone)
		}
		return m.changeStatus(i, issue.StatusTodo)
	case key.Matches(msg, k.Edit):
		return m.openEditForm(i)
	case key.Matches(msg, k.EditExternal):
		return m.editExternally(i)
	case key.Matches(msg, k.Comment):
		return m.openCommentForm(i)
	case key.Matches(msg, k.Browser):
		if i.URL == "" {
			return m.flash("This issue hasn't reached GitHub yet.", flashWarn)
		}
		return openURLCmd(m.env.OpenURL, i.URL)
	case key.Matches(msg, k.CopyURL):
		if i.URL == "" {
			return m.flash("This issue hasn't reached GitHub yet.", flashWarn)
		}
		return tea.Batch(tea.SetClipboard(i.URL), m.flash("Copied "+i.URL, flashOK))
	default:
		return m.handleHeldKey(i, msg.String())
	}
	return nil
}

// handleHeldKey resolves changes GitHub rejected or that conflict.
func (m *Model) handleHeldKey(i issue.Issue, pressed string) tea.Cmd {
	for _, op := range m.snap.Pending[i.Key()] {
		var err error
		var note string
		switch {
		case op.Conflict != nil && pressed == "K":
			err, note = m.eng.KeepMine(op), "Keeping your version."
		case op.Conflict != nil && pressed == "T":
			err, note = m.eng.TakeTheirs(op), "Dropped your edit."
		case op.Error != "" && pressed == "R":
			err, note = m.eng.Retry(op), "Retrying…"
		case op.Error != "" && pressed == "D":
			err, note = m.eng.Discard(op), "Discarded the change."
		default:
			continue
		}
		if err != nil {
			return m.flash(err.Error(), flashError)
		}
		m.reload()
		return tea.Batch(m.flash(note, flashInfo), m.startFlush())
	}
	return nil
}

// record runs a change and remembers how to undo it.
func (m *Model) record(label string, before issue.Issue, ops []store.Op, err error) tea.Cmd {
	if err != nil {
		return m.flash(err.Error(), flashError)
	}
	if len(ops) == 0 {
		return nil
	}
	ids := make([]string, len(ops))
	for idx, op := range ops {
		ids[idx] = op.ID
	}
	m.undo = append(m.undo, undoRecord{label: label, opIDs: ids, before: before, key: before.Key(), localID: before.LocalID})
	if len(m.undo) > 50 {
		m.undo = m.undo[1:]
	}
	m.reload()
	return tea.Batch(m.flash(label+" · u to undo", flashOK), m.startFlush())
}

func (m *Model) changeStatus(i issue.Issue, s issue.Status) tea.Cmd {
	if m.eng.Convention().StatusOf(i) == s {
		return nil
	}
	ops, err := m.eng.SetStatus(i, s)
	return m.record(fmt.Sprintf("%s → %s", i.Ref(), s), i, ops, err)
}

func (m *Model) toggleAssignMe(i issue.Issue) tea.Cmd {
	if m.me == "" {
		return m.flash("Still finding out who you are; try again in a moment.", flashWarn)
	}
	if i.IsAssigned(m.me) {
		ops, err := m.eng.Assign(i, nil, []string{m.me})
		return m.record("Unassigned you from "+i.Ref(), i, ops, err)
	}
	ops, err := m.eng.Assign(i, []string{m.me}, nil)
	return m.record("Assigned you to "+i.Ref(), i, ops, err)
}

func (m *Model) undoLast() tea.Cmd {
	if len(m.undo) == 0 {
		return m.flash("Nothing to undo.", flashInfo)
	}
	rec := m.undo[len(m.undo)-1]
	m.undo = m.undo[:len(m.undo)-1]
	current, ok := m.findIssue(rec.key, rec.localID)
	if !ok {
		current = rec.before
	}
	if _, err := m.eng.Restore(rec.opIDs, rec.before, current); err != nil {
		return m.flash("Couldn't undo: "+err.Error(), flashError)
	}
	m.reload()
	return tea.Batch(m.flash("Undid: "+rec.label, flashInfo), m.startFlush())
}

func (m *Model) openStatusPicker(i issue.Issue) {
	conv := m.eng.Convention()
	current := conv.StatusOf(i)
	keys := map[issue.Status]string{issue.StatusIdea: "i", issue.StatusTodo: "t", issue.StatusInProgress: "p", issue.StatusBlocked: "b", issue.StatusDone: "d", issue.StatusWontDo: "w"}
	var items []pickerItem
	for _, s := range issue.Statuses {
		hint := ""
		if label := conv.StatusLabel(s); label != "" {
			hint = "label “" + label + "”"
		} else if s == issue.StatusDone {
			hint = "close as completed"
		} else if s == issue.StatusWontDo {
			hint = "close as not planned"
		} else {
			hint = "open, no status label"
		}
		if s == current {
			hint = "current · " + hint
		}
		items = append(items, pickerItem{id: fmt.Sprint(int(s)), label: m.th.statusIcon(s) + " " + s.String(), hint: hint, key: keys[s], color: m.th.statusColor(s)})
	}
	p := newPicker(m, "Status of "+i.Ref(), items)
	for idx, s := range issue.Statuses {
		if s == current {
			p.cursor = idx
		}
	}
	p.onDone = func(m *Model, chosen []pickerItem, _ string) tea.Cmd {
		for _, s := range issue.Statuses {
			if chosen[0].id == fmt.Sprint(int(s)) {
				return m.changeStatus(i, s)
			}
		}
		return nil
	}
	m.pushOverlay(p)
}

func (m *Model) openTypePicker(i issue.Issue) {
	conv := m.eng.Convention()
	current := conv.TypeOf(i)
	keys := map[issue.Type]string{issue.TypeTask: "t", issue.TypeBug: "b", issue.TypeFeature: "f", issue.TypeDocs: "d"}
	var items []pickerItem
	for _, t := range issue.Types {
		hint := "no type label"
		if label := conv.TypeLabel(t); label != "" {
			hint = "label “" + label + "”"
		}
		if t == current {
			hint = "current · " + hint
		}
		items = append(items, pickerItem{id: fmt.Sprint(int(t)), label: t.String(), hint: hint, key: keys[t], color: m.th.typeColor(t)})
	}
	p := newPicker(m, "Type of "+i.Ref(), items)
	p.cursor = int(current)
	p.onDone = func(m *Model, chosen []pickerItem, _ string) tea.Cmd {
		for _, t := range issue.Types {
			if chosen[0].id == fmt.Sprint(int(t)) && t != current {
				ops, err := m.eng.SetType(i, t)
				return m.record(fmt.Sprintf("%s is now a %s", i.Ref(), strings.ToLower(t.String())), i, ops, err)
			}
		}
		return nil
	}
	m.pushOverlay(p)
}

func (m *Model) openLabelPicker(i issue.Issue) {
	conv := m.eng.Convention()
	seen := map[string]bool{}
	var items []pickerItem
	add := func(name, hint string, c string) {
		if seen[strings.ToLower(name)] || conv.IsConventionLabel(name) {
			return
		}
		seen[strings.ToLower(name)] = true
		item := pickerItem{id: name, label: name, hint: hint, selected: i.HasLabel(name)}
		if c != "" {
			item.color = lipgloss.Color("#" + c)
		}
		items = append(items, item)
	}
	for _, label := range i.Labels {
		add(label, "", "")
	}
	for _, label := range m.snap.Labels[i.Repo] {
		add(label.Name, label.Description, label.Color)
	}
	p := newPicker(m, "Labels on "+i.Ref(), items)
	p.multi = true
	p.allowCreate = true
	p.createLabel = "Add new label"
	p.note = "Type and status labels are set with s and t."
	p.onDone = func(m *Model, chosen []pickerItem, created string) tea.Cmd {
		want := map[string]bool{}
		for _, item := range chosen {
			want[strings.ToLower(item.id)] = true
		}
		var ch issue.Change
		for _, item := range items {
			has := i.HasLabel(item.id)
			if want[strings.ToLower(item.id)] && !has {
				ch.AddLabels = append(ch.AddLabels, item.id)
			}
			if !want[strings.ToLower(item.id)] && has {
				ch.RemoveLabels = append(ch.RemoveLabels, item.id)
			}
		}
		if created != "" {
			ch.AddLabels = append(ch.AddLabels, created)
		}
		if ch.Empty() {
			return nil
		}
		ops, err := m.eng.Apply(i, ch)
		return m.record("Updated labels on "+i.Ref(), i, ops, err)
	}
	m.pushOverlay(p)
}

func (m *Model) openJumpPicker() {
	var items []pickerItem
	for _, i := range m.snap.Issues {
		items = append(items, pickerItem{id: i.Key(), label: fmt.Sprintf("%s %s", i.Ref(), i.Title), color: nil})
	}
	p := newPicker(m, "Jump to issue", items)
	p.input.Placeholder = "number or words from the title"
	p.onDone = func(m *Model, chosen []pickerItem, _ string) tea.Cmd {
		m.jumpTo(chosen[0].id)
		return m.ensureComments()
	}
	m.pushOverlay(p)
}

// jumpTo selects an issue, switching to a view that shows it if needed.
func (m *Model) jumpTo(issueKey string) {
	target, ok := m.findIssue(issueKey, "")
	if !ok {
		return
	}
	m.board = false
	m.filterInput.SetValue("")
	inView := func() bool {
		for _, i := range m.visible {
			if i.Key() == issueKey {
				return true
			}
		}
		return false
	}
	m.refilter(issueKey)
	if !inView() {
		name := "Inbox"
		if !target.IsOpen() {
			name = "Closed"
		}
		for idx, v := range m.views {
			if v.name == name {
				m.setView(idx)
			}
		}
		m.refilter(issueKey)
	}
	m.focus = focusList
}

func (m *Model) openRepoPicker() {
	var items []pickerItem
	for _, repo := range m.env.Config.Repos {
		open := 0
		for _, i := range m.snap.Issues {
			if strings.EqualFold(i.Repo, repo) && i.IsOpen() {
				open++
			}
		}
		hint := fmt.Sprintf("%d open", open)
		if strings.EqualFold(repo, m.env.Config.DefaultRepo) {
			hint += " · default for new issues"
		}
		items = append(items, pickerItem{id: repo, label: repo, hint: hint, color: m.th.repoColor(repo)})
	}
	p := newPicker(m, "Go to repo", items)
	p.allowCreate = true
	p.createLabel = "Track"
	p.createValid = gh.ValidRepo
	p.note = "Type owner/name to track another repo."
	p.onDone = func(m *Model, chosen []pickerItem, created string) tea.Cmd {
		if created != "" {
			return m.trackRepos([]string{created})
		}
		if idx := m.viewByRepo(chosen[0].id); idx >= 0 {
			m.board = m.board && true
			m.setView(idx)
		}
		return nil
	}
	m.pushOverlay(p)
}

// trackRepos adds repos to the config and fetches their issues.
func (m *Model) trackRepos(repos []string) tea.Cmd {
	var added []string
	for _, value := range repos {
		repo, err := gh.NormalizeRepo(value)
		if err != nil {
			return m.flash(err.Error(), flashError)
		}
		if m.env.Config.AddRepo(repo) {
			added = append(added, repo)
		}
	}
	if len(added) == 0 {
		return m.flash("Already tracked.", flashInfo)
	}
	if err := m.saveConfig(); err != nil {
		return m.flash(err.Error(), flashError)
	}
	if idx := m.viewByRepo(added[0]); idx >= 0 {
		m.setView(idx)
	}
	m.loadingFirst = true
	return tea.Batch(m.flash("Tracking "+strings.Join(added, ", ")+". Fetching issues…", flashOK), m.startRefresh())
}

func (m *Model) untrackRepo(repo string) tea.Cmd {
	if !m.env.Config.RemoveRepo(repo) {
		return nil
	}
	if err := m.saveConfig(); err != nil {
		return m.flash(err.Error(), flashError)
	}
	_ = m.eng.Store().ForgetRepo(repo)
	m.reload()
	if len(m.env.Config.Repos) == 0 {
		m.onboarding = newOnboarding(m)
		return m.onboarding.init(m)
	}
	return m.flash("Stopped tracking "+repo+". Its issues on GitHub are untouched.", flashInfo)
}

func (m *Model) saveConfig() error {
	if err := m.env.SaveConfig(); err != nil {
		return err
	}
	m.eng = m.env.Engine
	m.rebuildViews()
	m.reload()
	return nil
}

// paletteCommand is an entry in the command palette.
type paletteCommand struct {
	name, hint string
	run        func(m *Model) tea.Cmd
}

func (m *Model) paletteCommands() []paletteCommand {
	k := m.keys
	withIssue := func(fn func(issue.Issue) tea.Cmd) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd {
			if i, ok := m.selected(); ok {
				return fn(i)
			}
			return m.flash("Select an issue first.", flashInfo)
		}
	}
	keyHint := func(b key.Binding) string { return b.Help().Key }
	cmds := []paletteCommand{
		{"New issue", keyHint(k.New), func(m *Model) tea.Cmd { return m.openNewIssueForm() }},
		{"Change status", keyHint(k.Status), withIssue(func(i issue.Issue) tea.Cmd { m.openStatusPicker(i); return nil })},
		{"Change type", keyHint(k.Type), withIssue(func(i issue.Issue) tea.Cmd { m.openTypePicker(i); return nil })},
		{"Edit labels", keyHint(k.Labels), withIssue(func(i issue.Issue) tea.Cmd { m.openLabelPicker(i); return nil })},
		{"Edit issue", keyHint(k.Edit), withIssue(m.openEditForm)},
		{"Edit issue in $EDITOR", keyHint(k.EditExternal), withIssue(m.editExternally)},
		{"Comment", keyHint(k.Comment), withIssue(m.openCommentForm)},
		{"Assign / unassign me", keyHint(k.AssignMe), withIssue(m.toggleAssignMe)},
		{"Close / reopen", keyHint(k.Close), withIssue(func(i issue.Issue) tea.Cmd {
			if i.IsOpen() {
				return m.changeStatus(i, issue.StatusDone)
			}
			return m.changeStatus(i, issue.StatusTodo)
		})},
		{"Close as not planned", "", withIssue(func(i issue.Issue) tea.Cmd { return m.changeStatus(i, issue.StatusWontDo) })},
		{"Open in browser", keyHint(k.Browser), withIssue(func(i issue.Issue) tea.Cmd {
			if i.URL == "" {
				return nil
			}
			return openURLCmd(m.env.OpenURL, i.URL)
		})},
		{"Copy link", keyHint(k.CopyURL), withIssue(func(i issue.Issue) tea.Cmd {
			return tea.Batch(tea.SetClipboard(i.URL), m.flash("Copied "+i.URL, flashOK))
		})},
		{"Undo last change", keyHint(k.Undo), func(m *Model) tea.Cmd { return m.undoLast() }},
		{"Jump to issue", keyHint(k.JumpIssue), func(m *Model) tea.Cmd { m.openJumpPicker(); return nil }},
		{"Go to repo", keyHint(k.SwitchRepo), func(m *Model) tea.Cmd { m.openRepoPicker(); return nil }},
		{"Toggle board view", keyHint(k.Board), func(m *Model) tea.Cmd { m.toggleBoard(); return nil }},
		{"Filter", keyHint(k.Filter), func(m *Model) tea.Cmd { m.filtering = true; return m.filterInput.Focus() }},
		{"Refresh from GitHub", keyHint(k.Refresh), func(m *Model) tea.Cmd { return tea.Batch(m.startFlush(), m.startRefresh()) }},
		{"Track repos…", "pick from your GitHub repos", func(m *Model) tea.Cmd {
			m.onboarding = newOnboarding(m)
			m.onboarding.adding = true
			return m.onboarding.init(m)
		}},
		{"Track this directory's repo", "", func(m *Model) tea.Cmd {
			repo, err := m.env.CurrentRepo()
			if err != nil || repo == "" {
				return m.flash("This directory isn't a GitHub repository.", flashWarn)
			}
			return m.trackRepos([]string{repo})
		}},
		{"Edit config file", m.env.Paths.ConfigFile(), func(m *Model) tea.Cmd { return m.editConfig() }},
		{"Show keyboard shortcuts", keyHint(k.Help), func(m *Model) tea.Cmd { m.pushOverlay(&helpOverlay{}); return nil }},
		{"Quit", keyHint(k.Quit), func(*Model) tea.Cmd { return tea.Quit }},
	}
	if v := m.views[m.viewIdx]; v.repo != "" {
		repo := v.repo
		cmds = append(cmds,
			paletteCommand{"Make " + repo + " the default for new issues", "", func(m *Model) tea.Cmd {
				m.env.Config.DefaultRepo = repo
				if err := m.saveConfig(); err != nil {
					return m.flash(err.Error(), flashError)
				}
				return m.flash("New issues go to "+repo+" by default.", flashOK)
			}},
			paletteCommand{"Stop tracking " + repo, "", func(m *Model) tea.Cmd { return m.untrackRepo(repo) }},
		)
	}
	for idx, v := range m.views {
		idx := idx
		cmds = append(cmds, paletteCommand{"View: " + v.name, v.query, func(m *Model) tea.Cmd { m.setView(idx); return nil }})
	}
	return cmds
}

func (m *Model) openPalette() {
	commands := m.paletteCommands()
	items := make([]pickerItem, len(commands))
	for idx, c := range commands {
		items[idx] = pickerItem{id: fmt.Sprint(idx), label: c.name, hint: c.hint}
	}
	p := newPicker(m, "Commands", items)
	p.onDone = func(m *Model, chosen []pickerItem, _ string) tea.Cmd {
		var idx int
		fmt.Sscan(chosen[0].id, &idx)
		return commands[idx].run(m)
	}
	m.pushOverlay(p)
}

// --- $EDITOR ---

type editorDoneMsg struct {
	path string
	err  error
	// target is the issue being edited; nil for a new issue.
	target *issue.Issue
	draft  issueDraft
}

type configEditedMsg struct{ err error }

func (m *Model) editExternally(i issue.Issue) tea.Cmd {
	doc := editor.Template(i.Title, i.Body, "Editing "+i.Ref())
	return m.runEditor(doc, func(path string, err error) tea.Msg {
		return editorDoneMsg{path: path, err: err, target: &i}
	})
}

func (m *Model) runEditor(doc string, done func(path string, err error) tea.Msg) tea.Cmd {
	path, err := editor.TempFile(doc)
	if err != nil {
		return m.flash(err.Error(), flashError)
	}
	return tea.ExecProcess(editor.Cmd(path), func(err error) tea.Msg { return done(path, err) })
}

func (m *Model) handleEditorDone(msg editorDoneMsg) tea.Cmd {
	doc, readErr := editor.ReadAndRemove(msg.path)
	if msg.err != nil {
		return m.flash("Editor failed: "+msg.err.Error(), flashError)
	}
	if readErr != nil {
		return m.flash(readErr.Error(), flashError)
	}
	title, body, ok := editor.Parse(doc)
	if !ok {
		return m.flash("Nothing written; no changes made.", flashInfo)
	}
	if msg.target == nil {
		d := msg.draft
		d.title, d.body = title, body
		return m.createFromDraft(d)
	}
	current, found := m.findIssue(msg.target.Key(), msg.target.LocalID)
	if !found {
		current = *msg.target
	}
	ops, err := m.eng.Edit(current, title, body)
	if err == nil && len(ops) == 0 {
		return m.flash("No changes.", flashInfo)
	}
	return m.record("Saved "+current.Ref(), current, ops, err)
}

func (m *Model) editConfig() tea.Cmd {
	path := m.env.Paths.ConfigFile()
	if _, err := os.Stat(path); err != nil {
		if err := config.Save(path, m.env.Config); err != nil {
			return m.flash(err.Error(), flashError)
		}
	}
	return tea.ExecProcess(editor.Cmd(path), func(err error) tea.Msg { return configEditedMsg{err: err} })
}

func (m *Model) handleConfigEdited(msg configEditedMsg) tea.Cmd {
	if msg.err != nil {
		return m.flash("Editor failed: "+msg.err.Error(), flashError)
	}
	cfg, _, err := config.Load(m.env.Paths.ConfigFile())
	if err != nil {
		return m.flash("Config not applied: "+err.Error(), flashError)
	}
	m.env.Config = cfg
	if err := m.saveConfig(); err != nil {
		return m.flash(err.Error(), flashError)
	}
	return tea.Batch(m.flash("Config reloaded.", flashOK), m.startRefresh())
}

// --- help ---

type helpOverlay struct{}

func (h *helpOverlay) update(m *Model, msg tea.Msg) (bool, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "?", "esc", "q", "enter":
			return true, nil
		}
	}
	return false, nil
}

func (h *helpOverlay) view(m *Model) string {
	th := m.th
	var columns []string
	for _, section := range m.keys.sections() {
		lines := []string{th.sectionHeading.Render(section.title), ""}
		for _, b := range section.bindings {
			lines = append(lines, th.key.Width(10).Render(b.Help().Key)+th.keyDesc.Render(b.Help().Desc))
		}
		columns = append(columns, strings.Join(lines, "\n"))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, columns[0], "    ", columns[1], "    ", columns[2])
	if lipgloss.Width(body)+8 > m.width {
		body = strings.Join(columns, "\n\n")
	}
	filters := th.sectionHeading.Render("Filters") + "\n\n" + th.subtle.Render(strings.Join(issue.QueryKeys(), "  ")) +
		"\n" + th.dim.Render(`e.g. type:bug status:"in progress" assignee:@me crash`)
	held := th.sectionHeading.Render("When a change can't be sent") + "\n\n" +
		th.subtle.Render("R retry · D discard · K keep mine · T take theirs")
	content := th.title.Render("Keyboard shortcuts") + "\n\n" + body + "\n\n" + filters + "\n\n" + held + "\n\n" + th.dim.Render("? or esc to close")
	return th.modal.Width(min(m.width-2, lipgloss.Width(body)+6)).Render(content)
}
