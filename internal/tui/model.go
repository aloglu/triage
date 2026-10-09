// Package tui is triage's interactive terminal app.
package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/aloglu/triage/internal/app"
	"github.com/aloglu/triage/internal/engine"
	"github.com/aloglu/triage/internal/issue"
	"github.com/aloglu/triage/internal/store"
)

type focusArea int

const (
	focusList focusArea = iota
	focusDetail
)

type flashKind int

const (
	flashInfo flashKind = iota
	flashOK
	flashWarn
	flashError
)

type flashState struct {
	id   int
	text string
	kind flashKind
}

type commentState struct {
	loading  bool
	comments []store.Comment
	err      error
	// forUpdate is the issue's UpdatedAt the comments belong to; a newer
	// issue means there may be new comments.
	forUpdate time.Time
}

// undoRecord remembers how an issue looked before a change.
type undoRecord struct {
	label  string
	opIDs  []string
	before issue.Issue
	// key and localID find the issue again, even after a new issue receives
	// its number.
	key, localID string
}

// Model is the root of the interactive app.
type Model struct {
	env  *app.Env
	eng  *engine.Engine
	th   theme
	keys keyMap

	width, height int

	snap engine.Snapshot
	me   string

	views   []view
	viewIdx int

	filterInput textinput.Model
	filtering   bool

	visible []issue.Issue
	cursor  int
	offset  int
	focus   focusArea
	// fullDetail shows the detail pane on its own (narrow terminals and the
	// board).
	fullDetail bool

	detail    viewport.Model
	detailKey string

	board    bool
	boardCol int
	boardRow [boardColumnCount]int

	overlays []overlay

	flashState flashState
	flashSeq   int

	refreshing, flushing, flushAgain bool
	refreshAgain, fullAgain          bool
	offline                          bool
	loadingFirst                     bool
	syncErr                          error
	lastSync                         time.Time

	undo     []undoRecord
	comments map[string]commentState
	md       *markdown

	// onboarding is set while the user hasn't chosen any repos yet.
	onboarding *onboarding
}

// New builds the app around env.
func New(env *app.Env) *Model {
	m := &Model{
		env:      env,
		eng:      env.Engine,
		th:       newTheme(true),
		keys:     newKeyMap(),
		comments: map[string]commentState{},
		md:       newMarkdown(),
		detail:   viewport.New(),
	}
	m.filterInput = textinput.New()
	m.filterInput.Prompt = "/ "
	m.filterInput.Placeholder = "filter: text, repo:, label:, type:, status:, assignee:@me"
	m.filterInput.SetStyles(textinput.DefaultStyles(true))
	m.me = m.eng.CachedMe()
	m.rebuildViews()
	m.reload()
	if len(env.Config.Repos) == 0 {
		m.onboarding = newOnboarding(m)
	} else {
		m.loadingFirst = len(m.snap.Issues) == 0
	}
	return m
}

// Run starts the app and blocks until it exits.
func Run(env *app.Env) error {
	_, err := tea.NewProgram(New(env)).Run()
	return err
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.RequestBackgroundColor}
	if m.env.ConfigErr != nil {
		cmds = append(cmds, m.flash("Problem in config.toml: "+m.env.ConfigErr.Error(), flashError))
	}
	if m.onboarding != nil {
		cmds = append(cmds, m.onboarding.init(m))
		return tea.Batch(cmds...)
	}
	cmds = append(cmds, m.selectStartView())
	if m.eng.Online() {
		cmds = append(cmds, meCmd(m.eng), m.startFlush(), m.startFullRefresh())
	} else if m.env.ClientErr != nil {
		cmds = append(cmds, m.flash("Offline: "+m.env.ClientErr.Error(), flashWarn))
	}
	cmds = append(cmds, tickCmd(m.env.Config.RefreshInterval()))
	return tea.Batch(cmds...)
}

// selectStartView opens the current directory's repo when it's tracked.
func (m *Model) selectStartView() tea.Cmd {
	if m.env.CurrentRepo == nil {
		return nil
	}
	repo, err := m.env.CurrentRepo()
	if err != nil || repo == "" {
		return nil
	}
	for i, v := range m.views {
		if strings.EqualFold(v.repo, repo) {
			m.setView(i)
			return nil
		}
	}
	return m.flash(fmt.Sprintf("%s isn't tracked yet. Press : and choose “Track this directory's repo”.", repo), flashInfo)
}

// reload re-reads the cache and outbox and recomputes what's visible,
// keeping the selection on the same issue.
func (m *Model) reload() {
	selected := m.selectedKey()
	snap, err := m.eng.Snapshot(m.env.Config.Repos)
	if err != nil {
		m.flash(err.Error(), flashError)
		return
	}
	engine.SortIssues(snap.Issues)
	m.snap = snap
	m.refilter(selected)
}

func (m *Model) selectedKey() string {
	if i, ok := m.selected(); ok {
		return i.Key()
	}
	return ""
}

func (m *Model) selected() (issue.Issue, bool) {
	if m.board {
		col := m.boardIssues()[m.boardCol]
		row := m.boardRow[m.boardCol]
		if row < len(col) {
			return col[row], true
		}
		return issue.Issue{}, false
	}
	if m.cursor >= 0 && m.cursor < len(m.visible) {
		return m.visible[m.cursor], true
	}
	return issue.Issue{}, false
}

// findIssue looks an issue up by key, following new issues to their number.
func (m *Model) findIssue(key, localID string) (issue.Issue, bool) {
	for _, i := range m.snap.Issues {
		if i.Key() == key || (localID != "" && i.LocalID == localID) {
			return i, true
		}
	}
	return issue.Issue{}, false
}

func (m *Model) matchContext() issue.MatchContext {
	return issue.MatchContext{Me: m.me, Convention: m.eng.Convention()}
}

func (m *Model) flash(text string, kind flashKind) tea.Cmd {
	m.flashSeq++
	m.flashState = flashState{id: m.flashSeq, text: text, kind: kind}
	id := m.flashSeq
	wait := 4 * time.Second
	if kind == flashError {
		wait = 8 * time.Second
	}
	return tea.Tick(wait, func(time.Time) tea.Msg { return clearFlashMsg{id: id} })
}

func (m *Model) pushOverlay(o overlay) {
	m.overlays = append(m.overlays, o)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	if m.onboarding != nil {
		return m, cmd
	}
	// Whatever happened, make sure the selected issue's comments are loaded.
	return m, tea.Batch(cmd, m.ensureComments())
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.detailKey = ""
		return m, nil
	case tea.BackgroundColorMsg:
		m.th = newTheme(msg.IsDark())
		m.filterInput.SetStyles(textinput.DefaultStyles(m.th.isDark))
		m.detailKey = ""
		return m, nil
	case clearFlashMsg:
		if msg.id == m.flashState.id {
			m.flashState = flashState{}
		}
		return m, nil
	case refreshDoneMsg:
		cmd := m.handleRefreshDone(msg)
		if m.onboarding != nil {
			return m, tea.Batch(cmd, m.onboarding.refreshed(m, msg))
		}
		return m, cmd
	case flushDoneMsg:
		return m, m.handleFlushDone(msg)
	case retryFlushMsg:
		return m, tea.Batch(m.startFlush(), m.startRefresh())
	case tickMsg:
		m.reload()
		return m, tea.Batch(m.startFlush(), m.startRefresh(), tickCmd(m.env.Config.RefreshInterval()))
	case meMsg:
		if msg.login != "" {
			m.me = msg.login
			m.refilter(m.selectedKey())
		}
		return m, nil
	case commentsMsg:
		state := m.comments[msg.key]
		m.comments[msg.key] = commentState{comments: msg.comments, err: msg.err, forUpdate: state.forUpdate}
		m.detailKey = ""
		return m, nil
	case openedMsg:
		if msg.err != nil {
			return m, m.flash("Couldn't open the browser: "+msg.err.Error(), flashError)
		}
		return m, nil
	case editorDoneMsg:
		return m, m.handleEditorDone(msg)
	case commentEditorMsg:
		return m, m.handleCommentEditor(msg)
	case configEditedMsg:
		return m, m.handleConfigEdited(msg)
	}

	if m.onboarding != nil {
		return m, m.onboarding.update(m, msg)
	}

	if len(m.overlays) > 0 {
		top := m.overlays[len(m.overlays)-1]
		if _, isKey := msg.(tea.KeyPressMsg); isKey || !isMouse(msg) {
			done, cmd := top.update(m, msg)
			if done {
				m.removeOverlay(top)
			}
			return m, cmd
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	case tea.MouseWheelMsg:
		return m, m.handleWheel(msg)
	case tea.MouseClickMsg:
		return m, m.handleClick(msg)
	}

	if m.filtering {
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) removeOverlay(o overlay) {
	for i := len(m.overlays) - 1; i >= 0; i-- {
		if m.overlays[i] == o {
			m.overlays = append(m.overlays[:i], m.overlays[i+1:]...)
			return
		}
	}
}

func isMouse(msg tea.Msg) bool {
	_, ok := msg.(tea.MouseMsg)
	return ok
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.filtering {
		return m.handleFilterKey(msg)
	}
	k := m.keys
	switch {
	case msg.String() == "ctrl+c":
		return tea.Quit
	case key.Matches(msg, k.Quit):
		if m.fullDetail {
			m.fullDetail = false
			m.focus = focusList
			return nil
		}
		return tea.Quit
	case key.Matches(msg, k.Help):
		m.pushOverlay(&helpOverlay{})
		return nil
	case key.Matches(msg, k.Palette):
		m.openPalette()
		return nil
	case key.Matches(msg, k.Filter):
		m.filtering = true
		m.focus = focusList
		m.fullDetail = false
		return m.filterInput.Focus()
	case key.Matches(msg, k.NextView):
		m.setView(m.viewIdx + 1)
		return nil
	case key.Matches(msg, k.PrevView):
		m.setView(m.viewIdx - 1)
		return nil
	case key.Matches(msg, k.Board):
		m.toggleBoard()
		return nil
	case key.Matches(msg, k.Refresh):
		cmds := []tea.Cmd{m.startFlush(), m.startFullRefresh()}
		if !m.eng.Online() {
			return m.flash("Not connected to GitHub. Run `gh auth login` and restart triage.", flashWarn)
		}
		m.flash("Refreshing…", flashInfo)
		if i, ok := m.selected(); ok {
			delete(m.comments, i.Key())
			m.detailKey = ""
		}
		return tea.Batch(cmds...)
	case key.Matches(msg, k.New):
		return m.openNewIssueForm()
	case key.Matches(msg, k.JumpIssue):
		m.openJumpPicker()
		return nil
	case key.Matches(msg, k.SwitchRepo):
		m.openRepoPicker()
		return nil
	case key.Matches(msg, k.Undo):
		return m.undoLast()
	}

	if m.fullDetail || m.focus == focusDetail {
		if cmd, handled := m.handleDetailKey(msg); handled {
			return cmd
		}
	} else if m.board {
		if cmd, handled := m.handleBoardKey(msg); handled {
			return cmd
		}
	} else if cmd, handled := m.handleListKey(msg); handled {
		return cmd
	}
	return m.handleIssueKey(msg)
}

func (m *Model) handleFilterKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.filtering = false
		m.filterInput.Blur()
		m.filterInput.SetValue("")
		m.refilter(m.selectedKey())
		return nil
	case "enter", "down", "tab":
		m.filtering = false
		m.filterInput.Blur()
		if err := issue.ParseQuery(m.filterInput.Value()).Validate(m.eng.Convention()); err != nil {
			return m.flash(err.Error(), flashWarn)
		}
		return nil
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.cursor, m.offset = 0, 0
	m.refilter("")
	return cmd
}

func (m *Model) handleListKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := m.keys
	switch {
	case key.Matches(msg, k.Up):
		m.moveCursor(-1)
	case key.Matches(msg, k.Down):
		m.moveCursor(1)
	case key.Matches(msg, k.Top):
		m.moveCursor(-len(m.visible))
	case key.Matches(msg, k.Bottom):
		m.moveCursor(len(m.visible))
	case key.Matches(msg, k.PageUp):
		m.moveCursor(-m.listPageSize())
	case key.Matches(msg, k.PageDown):
		m.moveCursor(m.listPageSize())
	case key.Matches(msg, k.Open):
		if _, ok := m.selected(); !ok {
			return nil, true
		}
		if m.isNarrow() {
			m.fullDetail = true
		}
		m.focus = focusDetail
		return m.ensureComments(), true
	case msg.String() == "esc":
		if m.filterInput.Value() != "" {
			m.filterInput.SetValue("")
			m.refilter(m.selectedKey())
		}
	default:
		return nil, false
	}
	return m.ensureComments(), true
}

func (m *Model) handleDetailKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := m.keys
	switch {
	case key.Matches(msg, k.Back):
		m.focus = focusList
		m.fullDetail = false
	case key.Matches(msg, k.Up):
		m.detail.ScrollUp(1)
	case key.Matches(msg, k.Down):
		m.detail.ScrollDown(1)
	case key.Matches(msg, k.PageUp):
		m.detail.PageUp()
	case key.Matches(msg, k.PageDown), msg.String() == "space":
		m.detail.PageDown()
	case key.Matches(msg, k.Top):
		m.detail.GotoTop()
	case key.Matches(msg, k.Bottom):
		m.detail.GotoBottom()
	default:
		return nil, false
	}
	return nil, true
}

func (m *Model) moveCursor(delta int) {
	if len(m.visible) == 0 {
		m.cursor = 0
		return
	}
	m.cursor = max(0, min(len(m.visible)-1, m.cursor+delta))
	m.detail.GotoTop()
}

func (m *Model) handleWheel(msg tea.MouseWheelMsg) tea.Cmd {
	delta := 0
	switch msg.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return nil
	}
	listWidth, _ := m.paneWidths()
	if m.fullDetail || (!m.board && !m.isNarrow() && msg.X >= listWidth) {
		if delta < 0 {
			m.detail.ScrollUp(3)
		} else {
			m.detail.ScrollDown(3)
		}
		return nil
	}
	if !m.board {
		m.moveCursor(delta)
		return m.ensureComments()
	}
	return nil
}

func (m *Model) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	if m.board || m.fullDetail || msg.Button != tea.MouseLeft {
		return nil
	}
	listWidth, _ := m.paneWidths()
	if msg.X >= listWidth {
		m.focus = focusDetail
		return nil
	}
	// The list starts below the header (1 line) and the pane border.
	row := (msg.Y-2)/rowHeight + m.offset
	if msg.Y >= 2 && row >= 0 && row < len(m.visible) {
		m.cursor = row
		m.focus = focusList
		m.detail.GotoTop()
		return m.ensureComments()
	}
	return nil
}

// ensureComments starts loading the selected issue's comments if they
// aren't loaded for the issue's current version.
func (m *Model) ensureComments() tea.Cmd {
	i, ok := m.selected()
	if !ok || i.Number == 0 || i.Comments == 0 {
		return nil
	}
	state, ok := m.comments[i.Key()]
	if ok && (state.loading || state.forUpdate.Equal(i.UpdatedAt)) {
		return nil
	}
	if cached, fresh := m.eng.CachedComments(i); fresh || !m.eng.Online() {
		if cached != nil || !m.eng.Online() {
			m.comments[i.Key()] = commentState{comments: cached, forUpdate: i.UpdatedAt}
			return nil
		}
	}
	m.comments[i.Key()] = commentState{loading: true, comments: state.comments, forUpdate: i.UpdatedAt}
	return commentsCmd(m.eng, i)
}

func (m *Model) isNarrow() bool { return m.width < 90 }
