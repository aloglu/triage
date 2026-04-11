package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/ansi"
	reflowtruncate "github.com/muesli/reflow/truncate"
	"github.com/muesli/reflow/wordwrap"

	"github.com/aloglu/triage/internal/config"
	"github.com/aloglu/triage/internal/fileutil"
	"github.com/aloglu/triage/internal/githubsync"
	"github.com/aloglu/triage/internal/model"
	"github.com/aloglu/triage/internal/storage"
)

const allProjectsLabel = "All projects"
const allStagesLabel = "all"
const (
	minWidth  = 72
	minHeight = 18
)

type focusArea int

const (
	focusItems focusArea = iota
	focusDetails
)

type mode int

const (
	modeSetup mode = iota
	modeNormal
	modeProjectPicker
	modeShortcuts
	modeRepos
	modeConfirm
	modeConflict
	modeSearch
	modeCommand
	modeEdit
)

type sortMode int

const (
	sortUpdated sortMode = iota
	sortCreated
)

type viewMode int

const (
	viewActive viewMode = iota
	viewArchive
	viewTrash
)

type listDensity int

const (
	densityComfortable listDensity = iota
	densityCompact
)

type itemForm struct {
	titleInput   textinput.Model
	projectInput textinput.Model
	repoInput    textinput.Model
	bodyInput    textarea.Model
	typeIndex    int
	stageIndex   int
	focusIndex   int
	editingIndex int
	isNew        bool
}

type setupForm struct {
	selectedMode int
	repoInput    textinput.Model
	enteringRepo bool
}

type conflictState struct {
	local        model.Item
	remote       model.Item
	editingIndex int
	isNew        bool
}

type confirmAction int

const (
	confirmPurge confirmAction = iota
	confirmImport
	confirmQuit
	confirmSync
)

type confirmState struct {
	action      confirmAction
	itemIndex   int
	importPath  string
	importItems []model.Item
}

type statusKind int

const (
	statusInfo statusKind = iota
	statusSuccess
	statusWarning
	statusError
	statusLoading
)

type statusSpinnerTickMsg time.Time

var statusSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type syncResultMsg struct {
	repos []string
	items []model.Item
	err   error
}

type saveResultMsg struct {
	local        model.Item
	saved        model.Item
	warning      string
	err          error
	isNew        bool
	editingIndex int
}

type conflictOverwriteResultMsg struct {
	saved   model.Item
	warning string
	err     error
}

type syncPushResult struct {
	index   int
	local   model.Item
	item    model.Item
	removed bool
	warning string
	err     error
}

type batchSyncResultMsg struct {
	items   []model.Item
	repos   []string
	results []syncPushResult
	err     error
}

type openURLResultMsg struct {
	url string
	err error
}

type undoState struct {
	items    []model.Item
	selected *model.Item
	label    string
}

type itemActionKind int

const (
	actionDelete itemActionKind = iota
	actionRestore
	actionPurge
)

type itemActionResultMsg struct {
	kind      itemActionKind
	itemIndex int
	saved     model.Item
	warning   string
	err       error
}

type modelUI struct {
	width               int
	height              int
	items               []model.Item
	filtered            []int
	selected            int
	itemOffset          int
	detailScroll        int
	shortcutsScroll     int
	reposScroll         int
	projectCursor       int
	projectFilter       string
	stageFilter         string
	viewMode            viewMode
	listDensity         listDensity
	sortAscending       bool
	commandSuggestIndex int
	queryInput          textinput.Model
	commandInput        textinput.Model
	mode                mode
	focus               focusArea
	sortMode            sortMode
	statusMessage       string
	statusUntil         time.Time
	statusKind          statusKind
	statusSticky        bool
	statusSpinnerFrame  int
	syncing             bool
	saveInFlight        bool
	actionInFlight      bool
	initSyncRepos       []string
	styles              styles
	form                itemForm
	setup               setupForm
	confirm             *confirmState
	conflict            *conflictState
	lastSearch          string
	searchOrigin        string
	undo                *undoState

	configManager *config.Manager
	config        config.AppConfig
	store         *storage.JSONStore
	githubClient  *githubsync.Client
}

type scrollState struct {
	offset int
	window int
	total  int
	topPad int
}

var openURLFn = openURL

type footerMetaSegment struct {
	label string
	value string
}

func New() tea.Model {
	queryInput := textinput.New()
	queryInput.Prompt = "/ "
	queryInput.Placeholder = "search title, project, stage, body"
	queryInput.CharLimit = 120

	commandInput := textinput.New()
	commandInput.Prompt = ":"
	commandInput.Placeholder = "new, edit, project all, sort updated desc, storage github owner/repo"
	commandInput.CharLimit = 80
	commandInput.ShowSuggestions = false

	titleInput := textinput.New()
	titleInput.Prompt = ""
	titleInput.Placeholder = "Issue title"
	titleInput.CharLimit = 120
	titleInput.SetCursorMode(textinput.CursorStatic)

	projectInput := textinput.New()
	projectInput.Prompt = ""
	projectInput.Placeholder = "Project name"
	projectInput.CharLimit = 60
	projectInput.SetCursorMode(textinput.CursorStatic)

	bodyInput := textarea.New()
	bodyInput.Prompt = ""
	bodyInput.Placeholder = "Write notes, links, and context..."
	bodyInput.ShowLineNumbers = false
	bodyInput.Cursor.SetChar(" ")
	bodyInput.Cursor.SetMode(cursor.CursorStatic)
	bodyInput.CharLimit = 0
	bodyInput.MaxHeight = 0
	bodyInput.SetHeight(8)
	bodyInput.Focus()
	bodyInput.Blur()

	editRepoInput := textinput.New()
	editRepoInput.Prompt = ""
	editRepoInput.Placeholder = "owner/repo"
	editRepoInput.CharLimit = 120
	editRepoInput.SetCursorMode(textinput.CursorStatic)

	setupRepoInput := textinput.New()
	setupRepoInput.Prompt = ""
	setupRepoInput.Placeholder = "owner/repo"
	setupRepoInput.CharLimit = 120

	titleInput.Focus()
	titleInput.Blur()
	projectInput.Focus()
	projectInput.Blur()
	editRepoInput.Focus()
	editRepoInput.Blur()
	setupRepoInput.Focus()
	setupRepoInput.Blur()

	manager, managerErr := config.NewManager()

	m := modelUI{
		items:         []model.Item{},
		projectFilter: allProjectsLabel,
		stageFilter:   allStagesLabel,
		viewMode:      viewActive,
		listDensity:   densityComfortable,
		queryInput:    queryInput,
		commandInput:  commandInput,
		mode:          modeNormal,
		focus:         focusItems,
		sortMode:      sortUpdated,
		sortAscending: false,
		styles:        newStyles(),
		form: itemForm{
			titleInput:   titleInput,
			projectInput: projectInput,
			repoInput:    editRepoInput,
			bodyInput:    bodyInput,
			typeIndex:    0,
			stageIndex:   0,
			focusIndex:   0,
			editingIndex: -1,
		},
		setup: setupForm{
			selectedMode: 0,
			repoInput:    setupRepoInput,
		},
		configManager: manager,
	}

	if managerErr != nil {
		m.mode = modeSetup
		m.statusMessage = fmt.Sprintf("Config setup unavailable: %v", managerErr)
		m.statusUntil = time.Now().Add(10 * time.Second)
		m.statusKind = statusError
		return m
	}

	cfg, ok, err := manager.Load()
	if err != nil {
		m.mode = modeSetup
		m.statusMessage = fmt.Sprintf("Config error: %v", err)
		m.statusUntil = time.Now().Add(10 * time.Second)
		m.statusKind = statusError
		return m
	}

	if !ok {
		m.mode = modeSetup
		m.statusMessage = "First run. Choose local-only or GitHub-backed storage."
		m.statusUntil = time.Now().Add(8 * time.Second)
		m.statusKind = statusInfo
		return m
	}

	m.applyConfig(cfg)
	if err := m.loadItems(); err != nil {
		m.statusMessage = userFacingError("Startup sync failed", err)
		m.statusUntil = time.Now().Add(10 * time.Second)
		m.statusKind = statusError
	} else {
		startupDraftsImported := 0
		startupDraftsFailed := 0
		draftErr := error(nil)
		startupDraftsImported, startupDraftsFailed, draftErr = m.importDrafts(false)
		if m.config.StorageMode == config.ModeGitHub && m.githubClient != nil && len(m.syncTargetRepos(m.items)) > 0 {
			m.initSyncRepos = append([]string(nil), m.syncTargetRepos(m.items)...)
			if len(m.initSyncRepos) == 1 {
				m = m.withStatus(fmt.Sprintf("Syncing GitHub issues from %s...", m.initSyncRepos[0]), statusLoading, 0, true)
			} else {
				m = m.withStatus(fmt.Sprintf("Syncing GitHub issues from %d repos...", len(m.initSyncRepos)), statusLoading, 0, true)
			}
		} else {
			switch {
			case draftErr != nil:
				m = m.setStatusWarning(fmt.Sprintf("Draft scan failed: %v", draftErr)).(modelUI)
			case startupDraftsImported > 0 && startupDraftsFailed > 0:
				m = m.setStatusWarning(fmt.Sprintf("Imported %d drafts, %d failed", startupDraftsImported, startupDraftsFailed)).(modelUI)
			case startupDraftsImported > 0:
				m = m.setStatusSuccess(fmt.Sprintf("Imported %d drafts", startupDraftsImported)).(modelUI)
			case startupDraftsFailed > 0:
				m = m.setStatusWarning(fmt.Sprintf("%d drafts failed to import", startupDraftsFailed)).(modelUI)
			default:
				m.postLoadStatus()
			}
		}
	}
	m.rebuildFiltered()

	return m
}

func (m modelUI) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink, statusSpinnerTickCmd()}
	if len(m.initSyncRepos) > 0 && m.config.StorageMode == config.ModeGitHub && m.githubClient != nil {
		cmds = append(cmds, syncRepoCmd(m.githubClient, m.initSyncRepos))
	}
	return tea.Batch(cmds...)
}

func (m modelUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeEditors()
		return m, nil
	case tea.MouseMsg:
		return m.updateMouse(msg)
	case syncResultMsg:
		return m.finishSync(msg), nil
	case saveResultMsg:
		return m.finishSave(msg), nil
	case conflictOverwriteResultMsg:
		return m.finishConflictOverwrite(msg), nil
	case batchSyncResultMsg:
		return m.finishBatchSync(msg), nil
	case openURLResultMsg:
		return m.finishOpenURL(msg), nil
	case itemActionResultMsg:
		return m.finishItemAction(msg), nil
	case statusSpinnerTickMsg:
		if m.statusKind == statusLoading && m.statusActive() {
			m.statusSpinnerFrame = (m.statusSpinnerFrame + 1) % len(statusSpinnerFrames)
		}
		return m, statusSpinnerTickCmd()
	case tea.KeyMsg:
		if m.saveInFlight || m.actionInFlight {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			}
			return m, nil
		}
		switch m.mode {
		case modeSetup:
			return m.updateSetup(msg)
		case modeProjectPicker:
			return m.updateProjectPicker(msg)
		case modeShortcuts:
			return m.updateShortcuts(msg)
		case modeRepos:
			return m.updateRepos(msg)
		case modeConfirm:
			return m.updateConfirm(msg)
		case modeConflict:
			return m.updateConflict(msg)
		case modeEdit:
			return m.updateEdit(msg)
		case modeSearch:
			return m.updateSearch(msg)
		case modeCommand:
			return m.updateCommand(msg)
		default:
			return m.updateNormal(msg)
		}
	}

	return m, nil
}

func (m modelUI) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.width == 0 || m.height == 0 || m.isTooSmall() {
		return m, nil
	}
	if m.saveInFlight || m.actionInFlight {
		return m, nil
	}

	if msg.Action == tea.MouseActionPress {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			return m.handleMouseWheel(-1), nil
		case tea.MouseButtonWheelDown:
			return m.handleMouseWheel(1), nil
		case tea.MouseButtonLeft:
			return m.handleMouseClick(msg), nil
		}
	}

	return m, nil
}

func (m modelUI) handleMouseWheel(delta int) tea.Model {
	switch m.mode {
	case modeNormal:
		if m.focus == focusDetails {
			m.scrollDetails(delta)
			return m
		}
		if delta > 0 {
			return m.moveDown()
		}
		return m.moveUp()
	case modeConflict:
		m.scrollConflict(delta)
	case modeShortcuts:
		m.scrollShortcuts(delta)
	case modeRepos:
		m.scrollRepos(delta)
	case modeConfirm:
		if m.confirm != nil && m.confirm.action == confirmSync {
			m.scrollConfirm(delta)
		}
	}
	return m
}

func (m modelUI) handleMouseClick(msg tea.MouseMsg) tea.Model {
	if m.mode != modeNormal {
		return m
	}

	items, details := m.mainPaneRects()
	switch {
	case items.contains(msg.X, msg.Y):
		m.focus = focusItems
	case details.contains(msg.X, msg.Y):
		m.focus = focusDetails
	}

	return m
}

type paneRect struct {
	x int
	y int
	w int
	h int
}

func (r paneRect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

func (m modelUI) mainPaneRects() (paneRect, paneRect) {
	listWidth, detailWidth := m.layoutWidths()
	contentY := m.styles.app.GetPaddingTop() + lipgloss.Height(m.renderHeader())
	contentX := m.styles.app.GetPaddingLeft()
	contentHeight := m.mainContentHeight()

	items := paneRect{x: contentX, y: contentY, w: listWidth, h: contentHeight}
	details := paneRect{x: contentX + listWidth, y: contentY, w: detailWidth, h: contentHeight}
	return items, details
}

func (m modelUI) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}
	if m.isTooSmall() {
		return m.renderTooSmall()
	}

	header := m.renderHeader()
	content := m.renderContent()
	footer := m.renderFooter()

	parts := []string{header, content}
	if footer != "" {
		parts = append(parts, footer)
	}
	body := lipgloss.JoinVertical(lipgloss.Left, parts...)
	appStyle := m.styles.app.
		Width(max(1, m.width-m.styles.app.GetHorizontalMargins()))
	return appStyle.Render(body)
}

func (m modelUI) appInnerWidth() int {
	return max(1, m.width-m.styles.app.GetHorizontalFrameSize())
}

func (m modelUI) mainContentHeight() int {
	available := m.height - m.styles.app.GetVerticalFrameSize() - lipgloss.Height(m.renderHeader()) - lipgloss.Height(m.renderFooter())
	return max(12, available)
}

func (m modelUI) updateSetup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.setup.enteringRepo {
		switch msg.String() {
		case "esc":
			m.setup.enteringRepo = false
			m.setup.repoInput.Blur()
			return m.setStatus("Setup cancelled."), nil
		case "enter":
			repo := strings.TrimSpace(m.setup.repoInput.Value())
			if !validRepoRef(repo) {
				return m.setStatus("Repository must be in owner/repo form."), nil
			}
			return m.finishSetup(config.ModeGitHub, repo)
		}

		var cmd tea.Cmd
		m.setup.repoInput, cmd = m.setup.repoInput.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "j", "down":
		if m.setup.selectedMode < 1 {
			m.setup.selectedMode++
		}
		return m, nil
	case "k", "up":
		if m.setup.selectedMode > 0 {
			m.setup.selectedMode--
		}
		return m, nil
	case "enter":
		if m.setup.selectedMode == 0 {
			return m.finishSetup(config.ModeLocal, "")
		}
		m.setup.enteringRepo = true
		return m, m.setup.repoInput.Focus()
	}

	return m, nil
}

func (m modelUI) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.syncing {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			m.confirm = &confirmState{action: confirmQuit}
			m.mode = modeConfirm
			return m, nil
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		m.confirm = &confirmState{action: confirmQuit}
		m.mode = modeConfirm
		return m, nil
	case "/":
		m.searchOrigin = m.lastSearch
		m.mode = modeSearch
		m.queryInput.SetValue(m.lastSearch)
		m.queryInput.CursorEnd()
		return m, m.queryInput.Focus()
	case ":":
		m.mode = modeCommand
		m.commandInput.SetValue("")
		m.commandSuggestIndex = 0
		return m, m.commandInput.Focus()
	case "?":
		m.mode = modeShortcuts
		m.shortcutsScroll = 0
		return m, nil
	case "tab":
		m.mode = modeProjectPicker
		m.projectCursor = m.currentProjectIndex()
		return m, nil
	case "l", "right":
		if m.focus < focusDetails {
			m.focus++
		}
		return m, nil
	case "shift+tab":
		m.mode = modeProjectPicker
		m.projectCursor = m.currentProjectIndex()
		return m, nil
	case "h", "left":
		if m.focus > focusItems {
			m.focus--
		}
		return m, nil
	case "j", "down":
		return m.moveDown(), nil
	case "k", "up":
		return m.moveUp(), nil
	case "n":
		return m.enterEdit(-1)
	case "e":
		if len(m.filtered) == 0 {
			return m.setStatus("No item selected."), nil
		}
		return m.enterEdit(m.filtered[m.selected])
	case "S":
		return m.runSyncCommand()
	case "u":
		return m.runUndoCommand(), nil
	case "D":
		m.toggleViewMode()
		return m, nil
	}

	return m, nil
}

func (m modelUI) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.scrollConfirm(1)
		return m, nil
	case "k", "up":
		m.scrollConfirm(-1)
		return m, nil
	case "esc", "n", "c":
		m.mode = modeNormal
		m.confirm = nil
		m.detailScroll = 0
		return m.setStatus("Confirmation cancelled."), nil
	case "q":
		if m.confirm != nil && m.confirm.action == confirmQuit {
			return m.confirmQuitNow()
		}
	case "s", "S":
		if m.confirm != nil && m.confirm.action == confirmSync {
			return m.confirmActionNow()
		}
	case "enter", "y", "p", "i":
		if m.confirm != nil && m.confirm.action == confirmQuit {
			return m.confirmQuitNow()
		}
		return m.confirmActionNow()
	}

	return m, nil
}

func (m modelUI) updateShortcuts(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc", "?", "enter":
		m.mode = modeNormal
		return m, nil
	case "j", "down":
		m.scrollShortcuts(1)
		return m, nil
	case "k", "up":
		m.scrollShortcuts(-1)
		return m, nil
	}

	return m, nil
}

func (m modelUI) updateRepos(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc", "enter":
		m.mode = modeNormal
		return m, nil
	case "j", "down":
		m.scrollRepos(1)
		return m, nil
	case "k", "up":
		m.scrollRepos(-1)
		return m, nil
	}

	return m, nil
}

func (m modelUI) updateProjectPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "tab", "shift+tab":
		m.mode = modeNormal
		return m, nil
	case "enter":
		projects := m.projectOptions()
		if len(projects) == 0 {
			m.mode = modeNormal
			return m, nil
		}
		if m.projectCursor < 0 {
			m.projectCursor = 0
		}
		if m.projectCursor >= len(projects) {
			m.projectCursor = len(projects) - 1
		}
		m.projectFilter = projects[m.projectCursor]
		m.mode = modeNormal
		m.rebuildFiltered()
		return m.setStatus(fmt.Sprintf("Project set to %s.", m.activeProjectLabel())), nil
	case "j", "down":
		projects := m.projectOptions()
		if m.projectCursor < len(projects)-1 {
			m.projectCursor++
		}
		return m, nil
	case "k", "up":
		if m.projectCursor > 0 {
			m.projectCursor--
		}
		return m, nil
	}

	return m, nil
}

func (m modelUI) updateConflict(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "j", "down":
		m.scrollConflict(1)
		return m, nil
	case "k", "up":
		m.scrollConflict(-1)
		return m, nil
	case "esc":
		m.mode = modeNormal
		m.conflict = nil
		return m.setStatus("Conflict resolution cancelled."), nil
	case "r":
		return m.resolveConflictWithRemote(), nil
	case "o":
		return m.resolveConflictByOverwriting()
	}

	return m, nil
}

func (m modelUI) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.lastSearch = m.searchOrigin
		m.rebuildFiltered()
		m.mode = modeNormal
		m.queryInput.Blur()
		return m, nil
	case "enter":
		m.lastSearch = strings.TrimSpace(m.queryInput.Value())
		m.mode = modeNormal
		m.queryInput.Blur()
		m.rebuildFiltered()
		return m, nil
	}

	var cmd tea.Cmd
	before := m.queryInput.Value()
	m.queryInput, cmd = m.queryInput.Update(msg)
	if shouldCloseEmptySearch(msg, before, m.queryInput.Value()) {
		m.lastSearch = m.searchOrigin
		m.rebuildFiltered()
		m.mode = modeNormal
		m.queryInput.Blur()
		return m, nil
	}
	m.lastSearch = strings.TrimSpace(m.queryInput.Value())
	m.rebuildFiltered()
	return m, cmd
}

func (m modelUI) updateCommand(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	matches := matchedCommandSuggestions(m.commandInput.Value(), m.commandSuggestions())
	selectedSuggestion := m.selectedCommandSuggestion(matches)

	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.commandInput.Blur()
		return m, nil
	case "enter":
		if selectedSuggestion != "" && normalizeCommandValue(m.commandInput.Value()) != selectedSuggestion {
			m.commandInput.SetValue(selectedSuggestion)
			m.commandInput.CursorEnd()
			m.commandSuggestIndex = 0
			return m, nil
		}
		command := strings.TrimSpace(m.commandInput.Value())
		m.mode = modeNormal
		m.commandInput.Blur()
		return m.runCommand(command)
	case "tab":
		if selectedSuggestion != "" {
			m.commandInput.SetValue(selectedSuggestion)
			m.commandInput.CursorEnd()
			m.commandSuggestIndex = 0
		}
		return m, nil
	case "down":
		if len(matches) > 0 {
			m.commandSuggestIndex = (m.commandSuggestIndex + 1) % len(matches)
			return m, nil
		}
	case "up":
		if len(matches) > 0 {
			m.commandSuggestIndex = (m.commandSuggestIndex - 1 + len(matches)) % len(matches)
			return m, nil
		}
	case "right":
		if m.commandInput.Position() == len([]rune(m.commandInput.Value())) {
			if selectedSuggestion != "" {
				m.commandInput.SetValue(selectedSuggestion)
				m.commandInput.CursorEnd()
				m.commandSuggestIndex = 0
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	before := m.commandInput.Value()
	m.commandInput, cmd = m.commandInput.Update(msg)
	if shouldCloseEmptyCommand(msg, before, m.commandInput.Value()) {
		m.mode = modeNormal
		m.commandInput.Blur()
		m.commandSuggestIndex = 0
		return m, nil
	}
	if m.commandInput.Value() != before {
		m.commandSuggestIndex = 0
	}
	return m, cmd
}

func (m modelUI) updateEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.form.titleInput.Blur()
		m.form.projectInput.Blur()
		m.form.repoInput.Blur()
		m.form.bodyInput.Blur()
		return m.setStatus("Edit cancelled."), nil
	case "ctrl+s":
		return m.saveForm()
	case "tab":
		m.form.focusIndex = (m.form.focusIndex + 1) % 6
		return m.focusFormField()
	case "shift+tab":
		m.form.focusIndex = (m.form.focusIndex + 5) % 6
		return m.focusFormField()
	}

	if m.form.focusIndex == 3 {
		switch msg.String() {
		case "j", "down":
			m.form.focusIndex = (m.form.focusIndex + 1) % 6
			return m.focusFormField()
		case "k", "up":
			m.form.focusIndex = (m.form.focusIndex + 5) % 6
			return m.focusFormField()
		case "l", "right":
			if m.form.typeIndex < len(model.Types)-1 {
				m.form.typeIndex++
			}
			return m, nil
		case "h", "left":
			if m.form.typeIndex > 0 {
				m.form.typeIndex--
			}
			return m, nil
		}
	}

	if m.form.focusIndex == 4 {
		switch msg.String() {
		case "j", "down":
			m.form.focusIndex = (m.form.focusIndex + 1) % 6
			return m.focusFormField()
		case "k", "up":
			m.form.focusIndex = (m.form.focusIndex + 5) % 6
			return m.focusFormField()
		case "l", "right":
			if m.form.stageIndex < len(model.Stages)-1 {
				m.form.stageIndex++
			}
			return m, nil
		case "h", "left":
			if m.form.stageIndex > 0 {
				m.form.stageIndex--
			}
			return m, nil
		}
	}

	if m.form.focusIndex != 5 {
		switch msg.String() {
		case "down":
			m.form.focusIndex = (m.form.focusIndex + 1) % 6
			return m.focusFormField()
		case "up":
			m.form.focusIndex = (m.form.focusIndex + 5) % 6
			return m.focusFormField()
		}
	}

	var cmd tea.Cmd
	switch m.form.focusIndex {
	case 0:
		m.form.titleInput, cmd = m.form.titleInput.Update(msg)
	case 1:
		beforeProject := m.form.projectInput.Value()
		beforeRepo := normalizeRepoRef(m.form.repoInput.Value())
		m.form.projectInput, cmd = m.form.projectInput.Update(msg)
		if beforeProject != m.form.projectInput.Value() {
			oldDefault := m.defaultRepoForProject(beforeProject)
			if beforeRepo == oldDefault {
				m.form.repoInput.SetValue(m.defaultRepoForProject(m.form.projectInput.Value()))
			}
		}
	case 2:
		m.form.repoInput, cmd = m.form.repoInput.Update(msg)
	case 5:
		m.form.bodyInput, cmd = m.form.bodyInput.Update(msg)
	}

	return m, cmd
}

func (m modelUI) moveDown() modelUI {
	switch m.focus {
	case focusItems:
		if m.selected < len(m.filtered)-1 {
			m.selected++
			m.detailScroll = 0
			m.ensureSelectedVisible()
		}
	case focusDetails:
		m.scrollDetails(1)
	}
	return m
}

func (m modelUI) moveUp() modelUI {
	switch m.focus {
	case focusItems:
		if m.selected > 0 {
			m.selected--
			m.detailScroll = 0
			m.ensureSelectedVisible()
		}
	case focusDetails:
		m.scrollDetails(-1)
	}
	return m
}

func (m *modelUI) scrollDetails(delta int) {
	if len(m.filtered) == 0 {
		m.detailScroll = 0
		return
	}

	_, detailWidth := m.layoutWidths()
	contentHeight := m.mainContentHeight()
	panelStyle := m.panelStyle(m.styles.panel, detailWidth, contentHeight)
	innerWidth := max(1, detailWidth-panelStyle.GetHorizontalFrameSize())
	innerHeight := max(1, contentHeight-panelStyle.GetVerticalFrameSize())
	lines := m.renderDetailLines(m.items[m.filtered[m.selected]], innerWidth)
	maxScroll := max(0, len(lines)-innerHeight)
	m.detailScroll = clamp(m.detailScroll+delta, 0, maxScroll)
}

func (m *modelUI) scrollConflict(delta int) {
	if m.conflict == nil {
		m.detailScroll = 0
		return
	}

	width := m.appInnerWidth()
	contentHeight := m.mainContentHeight()
	panelStyle := m.panelStyle(m.styles.panelFocused, width, contentHeight)
	innerWidth := max(1, width-panelStyle.GetHorizontalFrameSize())
	innerHeight := max(1, contentHeight-panelStyle.GetVerticalFrameSize())
	actionHeight := len(m.renderConflictActionLines(innerWidth))
	bodyHeight := max(1, innerHeight-actionHeight)
	lines := m.conflictDisplayLines(innerWidth, bodyHeight)
	maxScroll := max(0, len(lines)-bodyHeight)
	m.detailScroll = clamp(m.detailScroll+delta, 0, maxScroll)
}

func (m *modelUI) scrollShortcuts(delta int) {
	width := min(max(52, m.width-20), 74)
	height := min(max(14, len(m.shortcutsModalLines())+2), m.height-8)
	panel := m.styles.panelFocused.Width(max(1, width-m.styles.panelFocused.GetHorizontalFrameSize()))
	innerHeight := max(1, height-panel.GetVerticalFrameSize())
	lines := m.shortcutsModalLines()
	maxScroll := max(0, len(lines)-innerHeight)
	m.shortcutsScroll = clamp(m.shortcutsScroll+delta, 0, maxScroll)
}

func (m *modelUI) scrollRepos(delta int) {
	width := min(max(56, m.width-20), 84)
	height := min(max(14, len(m.reposModalLines())+2), m.height-8)
	panel := m.panelStyle(m.styles.panelFocused, width, height)
	innerHeight := max(1, height-panel.GetVerticalFrameSize())
	lines := m.reposModalLines()
	maxScroll := max(0, len(lines)-innerHeight)
	m.reposScroll = clamp(m.reposScroll+delta, 0, maxScroll)
}

func (m *modelUI) scrollConfirm(delta int) {
	if m.confirm == nil {
		m.detailScroll = 0
		return
	}

	width := m.confirmModalWidth()
	panel := m.panelStyle(m.styles.panelFocused, width, m.confirmModalHeight(width))
	innerWidth := max(1, width-panel.GetHorizontalFrameSize())
	innerHeight := max(1, m.confirmModalHeight(width)-panel.GetVerticalFrameSize())
	actionHeight := len(m.confirmActionLines(innerWidth))
	bodyHeight := max(1, innerHeight-actionHeight)
	lines := m.confirmBodyLines(innerWidth)
	maxScroll := max(0, len(lines)-bodyHeight)
	m.detailScroll = clamp(m.detailScroll+delta, 0, maxScroll)
}

func (m *modelUI) ensureSelectedVisible() {
	visible := m.itemVisibleCount()
	if visible <= 0 {
		m.itemOffset = 0
		return
	}

	maxOffset := max(0, len(m.filtered)-visible)
	if m.itemOffset > maxOffset {
		m.itemOffset = maxOffset
	}
	if m.selected < m.itemOffset {
		m.itemOffset = m.selected
	}
	if m.selected >= m.itemOffset+visible {
		m.itemOffset = m.selected - visible + 1
	}
	m.itemOffset = clamp(m.itemOffset, 0, maxOffset)
}

func (m modelUI) itemVisibleCount() int {
	contentHeight := m.mainContentHeight()
	listWidth, _ := m.layoutWidths()
	panelStyle := m.panelStyle(m.styles.panelMuted, listWidth, contentHeight)
	innerHeight := max(1, contentHeight-panelStyle.GetVerticalFrameSize())
	available := max(1, innerHeight-2)
	if m.listDensity == densityCompact {
		return max(1, available/2)
	}
	return max(1, (available+1)/3)
}

func (m *modelUI) beginEdit(itemIndex int) {
	m.mode = modeEdit
	m.detailScroll = 0
	m.form.editingIndex = itemIndex
	m.form.focusIndex = 0
	m.form.isNew = itemIndex == -1

	if itemIndex == -1 {
		m.form.titleInput.SetValue("")
		m.form.projectInput.SetValue(m.projectFilter)
		if m.projectFilter == allProjectsLabel {
			m.form.projectInput.SetValue("")
		}
		m.form.repoInput.SetValue(m.defaultRepoForProject(m.form.projectInput.Value()))
		m.form.bodyInput.SetValue("")
		m.form.typeIndex = 0
		m.form.stageIndex = 0
	} else {
		item := m.items[itemIndex]
		m.form.titleInput.SetValue(item.Title)
		m.form.projectInput.SetValue(item.Project)
		m.form.repoInput.SetValue(displayRepoValue(item.Repo, m.defaultRepoForProject(item.Project)))
		m.form.bodyInput.SetValue(item.Body)
		m.form.typeIndex = typeIndex(item.Type)
		m.form.stageIndex = stageIndex(item.Stage)
	}
	m.resizeEditors()
}

func (m modelUI) enterEdit(itemIndex int) (tea.Model, tea.Cmd) {
	m.beginEdit(itemIndex)
	return m.focusFormField()
}

func (m modelUI) focusFormField() (tea.Model, tea.Cmd) {
	m.form.titleInput.Blur()
	m.form.projectInput.Blur()
	m.form.repoInput.Blur()
	m.form.bodyInput.Blur()

	switch m.form.focusIndex {
	case 0:
		m.form.titleInput.CursorEnd()
		cmd := m.form.titleInput.Focus()
		return m, cmd
	case 1:
		m.form.projectInput.CursorEnd()
		cmd := m.form.projectInput.Focus()
		return m, cmd
	case 2:
		m.form.repoInput.CursorEnd()
		cmd := m.form.repoInput.Focus()
		return m, cmd
	case 5:
		m.form.bodyInput.CursorEnd()
		cmd := m.form.bodyInput.Focus()
		return m, cmd
	default:
		return m, nil
	}
}

func (m modelUI) saveForm() (tea.Model, tea.Cmd) {
	title := strings.TrimSpace(m.form.titleInput.Value())
	project := strings.TrimSpace(m.form.projectInput.Value())
	repo := normalizeRepoRef(m.form.repoInput.Value())
	body := strings.TrimSpace(m.form.bodyInput.Value())
	stage := model.Stages[m.form.stageIndex]

	if title == "" {
		return m.setStatusWarning("Title is required."), nil
	}
	if project == "" {
		return m.setStatusWarning("Project is required."), nil
	}
	if repo != "" && !validRepoRef(repo) {
		return m.setStatusWarning("Repo must be in owner/repo form."), nil
	}
	if m.config.StorageMode == config.ModeGitHub && repo == "" {
		repo = m.defaultRepoForProject(project)
		if repo == "" {
			return m.setStatusWarning("A GitHub repo is required in GitHub mode."), nil
		}
	}

	now := time.Now()
	itemType := model.Types[m.form.typeIndex]
	var previous *model.Item
	moveWarning := ""
	if !m.form.isNew {
		original := m.items[m.form.editingIndex]
		previous = &original
	}
	candidate := m.buildEditedItem(title, project, repo, body, itemType, stage, now)
	if m.form.isNew {
		m = m.captureUndo("create")
	} else {
		m = m.captureUndo("edit")
	}

	if m.config.StorageMode == config.ModeGitHub {
		candidate.PendingSync = pendingSyncForEdit(candidate, previous, m.form.isNew)
		candidate.SyncConflict = false
		candidate.SyncError = ""
		if m.form.isNew {
			m.items = append([]model.Item{candidate}, m.items...)
		} else {
			m.items[m.form.editingIndex] = candidate
		}
		if err := m.persistItems(); err != nil {
			return m.setStatusError(fmt.Sprintf("Save failed: %v", err)), nil
		}
		m.mode = modeNormal
		m.conflict = nil
		m.detailScroll = 0
		m.rebuildFiltered()
		m.selectItem(candidate)
		if m.form.isNew {
			return m.setStatusSuccess("Saved locally. Press S to sync."), nil
		}
		return m.setStatusSuccess("Saved locally. Press S to sync."), nil
	}

	if m.form.isNew {
		m.items = append([]model.Item{candidate}, m.items...)
	} else {
		m.items[m.form.editingIndex] = candidate
	}

	if err := m.persistItems(); err != nil {
		return m.setStatusError(fmt.Sprintf("Save failed: %v", err)), nil
	}

	m.mode = modeNormal
	m.conflict = nil
	m.detailScroll = 0
	m.rebuildFiltered()
	m.selectItem(candidate)

	if moveWarning != "" {
		return m.setStatusWarning(moveWarning), nil
	}
	if m.form.isNew {
		return m.setStatusSuccess("Item created."), nil
	}
	return m.setStatusSuccess("Item updated."), nil
}

func (m *modelUI) rebuildFiltered() {
	m.sortItems()

	if m.projectFilter == "" {
		m.projectFilter = allProjectsLabel
	}
	if m.stageFilter == "" {
		m.stageFilter = allStagesLabel
	}

	filtered := make([]int, 0, len(m.items))
	for i, item := range m.items {
		if item.IsLocallyPurged() {
			continue
		}
		if m.viewMode == viewActive && (item.IsDone() || item.IsTrashed()) {
			continue
		}
		if m.viewMode == viewArchive && (!item.IsDone() || item.IsTrashed()) {
			continue
		}
		if m.viewMode == viewTrash && !item.IsTrashed() {
			continue
		}
		if m.projectFilter != "" && m.projectFilter != allProjectsLabel && item.Project != m.projectFilter {
			continue
		}
		if m.stageFilter != "" && m.stageFilter != allStagesLabel && string(item.Stage) != m.stageFilter {
			continue
		}
		if !item.Matches(m.lastSearch) {
			continue
		}
		filtered = append(filtered, i)
	}

	m.filtered = filtered

	if len(m.filtered) == 0 {
		m.selected = 0
		m.itemOffset = 0
		m.detailScroll = 0
		return
	}
	if m.selected >= len(m.filtered) {
		m.selected = len(m.filtered) - 1
	}

	projects := m.projectOptions()
	if m.projectCursor >= len(projects) {
		m.projectCursor = max(0, len(projects)-1)
	}
	m.detailScroll = 0
	m.ensureSelectedVisible()
}

func (m *modelUI) sortItems() {
	sort.SliceStable(m.items, func(i, j int) bool {
		cmp := 0
		switch m.sortMode {
		case sortCreated:
			cmp = compareTime(m.items[i].CreatedAt, m.items[j].CreatedAt)
		default:
			cmp = compareTime(m.items[i].UpdatedAt, m.items[j].UpdatedAt)
		}
		if cmp == 0 {
			cmp = compareTime(m.items[i].CreatedAt, m.items[j].CreatedAt)
		}
		if cmp == 0 {
			cmp = strings.Compare(m.items[i].Title, m.items[j].Title)
		}
		if m.sortAscending {
			return cmp < 0
		}
		return cmp > 0
	})
}

func (m *modelUI) selectItem(target model.Item) {
	for idx, itemIdx := range m.filtered {
		if itemsMatchForSelection(m.items[itemIdx], target) {
			m.selected = idx
			m.detailScroll = 0
			m.ensureSelectedVisible()
			return
		}
	}
}

func itemsMatchForSelection(item, target model.Item) bool {
	if key := itemRemoteKey(target); key != "" {
		return itemRemoteKey(item) == key
	}
	return item.Title == target.Title &&
		item.Project == target.Project &&
		normalizeRepoRef(item.Repo) == normalizeRepoRef(target.Repo) &&
		item.CreatedAt.Equal(target.CreatedAt) &&
		item.UpdatedAt.Equal(target.UpdatedAt)
}

func cloneItems(items []model.Item) []model.Item {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]model.Item, len(items))
	copy(cloned, items)
	return cloned
}

type fileSnapshot struct {
	path   string
	data   []byte
	exists bool
}

func snapshotFile(path string) (fileSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileSnapshot{path: path}, nil
		}
		return fileSnapshot{}, fmt.Errorf("read snapshot %s: %w", path, err)
	}
	return fileSnapshot{
		path:   path,
		data:   data,
		exists: true,
	}, nil
}

func restoreFileSnapshot(snapshot fileSnapshot) error {
	if snapshot.path == "" {
		return nil
	}
	if !snapshot.exists {
		if err := os.Remove(snapshot.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", snapshot.path, err)
		}
		return nil
	}
	if err := fileutil.AtomicWriteFile(snapshot.path, snapshot.data, 0o700, 0o600); err != nil {
		return fmt.Errorf("restore %s: %w", snapshot.path, err)
	}
	return nil
}

func (m modelUI) captureUndo(label string) modelUI {
	state := &undoState{
		items: cloneItems(m.items),
		label: label,
	}
	if itemIndex, ok := m.selectedItemIndex(); ok && itemIndex >= 0 && itemIndex < len(m.items) {
		selected := m.items[itemIndex]
		state.selected = &selected
	}
	m.undo = state
	return m
}

func (m *modelUI) restoreUndoSelection(selected *model.Item) {
	if selected == nil {
		if len(m.filtered) == 0 {
			m.selected = 0
			return
		}
		m.selected = clamp(m.selected, 0, len(m.filtered)-1)
		m.ensureSelectedVisible()
		return
	}

	itemIndex := findItemIndex(m.items, *selected)
	if itemIndex == -1 {
		if len(m.filtered) == 0 {
			m.selected = 0
			return
		}
		m.selected = clamp(m.selected, 0, len(m.filtered)-1)
		m.ensureSelectedVisible()
		return
	}

	for idx, filteredIndex := range m.filtered {
		if filteredIndex == itemIndex {
			m.selected = idx
			m.ensureSelectedVisible()
			return
		}
	}

	if len(m.filtered) == 0 {
		m.selected = 0
		return
	}
	m.selected = clamp(m.selected, 0, len(m.filtered)-1)
	m.ensureSelectedVisible()
}

func findItemIndex(items []model.Item, target model.Item) int {
	targetRepo := normalizeRepoRef(target.RemoteRepo())
	if target.IssueNumber > 0 && targetRepo != "" {
		for idx, item := range items {
			if item.IssueNumber == target.IssueNumber && normalizeRepoRef(item.RemoteRepo()) == targetRepo {
				return idx
			}
		}
	}

	for idx, item := range items {
		if item.CreatedAt.Equal(target.CreatedAt) &&
			item.Title == target.Title &&
			item.Project == target.Project &&
			normalizeRepoRef(item.Repo) == normalizeRepoRef(target.Repo) {
			return idx
		}
	}
	return -1
}

func (m modelUI) renderHeader() string {
	segments := []string{m.styles.title.Render("triage")}
	for _, label := range m.headerContextLabels() {
		segments = append(segments, m.styles.muted.Render(" | "), m.styles.subtitle.Render(label))
	}
	left := lipgloss.JoinHorizontal(lipgloss.Top, segments...)

	rightText := ""
	switch m.mode {
	case modeSetup:
		rightText = "setup"
	default:
		if m.mode != modeConfirm && m.statusActive() && m.statusMessage != "" {
			rightText = m.renderStatusText()
		}
	}

	headerWidth := m.appInnerWidth()
	leftWidth := lipgloss.Width(left)
	availableRight := max(0, headerWidth-leftWidth-2)
	rightStyle := m.styles.muted
	if rightText == m.statusMessage && rightText != "" {
		rightStyle = m.statusStyle()
	}
	right := rightStyle.Render(truncatePlain(rightText, availableRight))
	spacerWidth := max(0, headerWidth-leftWidth-lipgloss.Width(right))
	line := lipgloss.NewStyle().Width(headerWidth).Render(
		lipgloss.JoinHorizontal(lipgloss.Top,
			left,
			lipgloss.NewStyle().Width(spacerWidth).Render(""),
			right,
		),
	)

	return line
}

func (m modelUI) renderStatusText() string {
	if m.statusKind != statusLoading {
		return m.statusMessage
	}
	if len(statusSpinnerFrames) == 0 {
		return m.statusMessage
	}
	frame := statusSpinnerFrames[m.statusSpinnerFrame%len(statusSpinnerFrames)]
	if m.statusMessage == "" {
		return frame
	}
	return frame + " " + m.statusMessage
}

func (m modelUI) headerContextLabels() []string {
	switch m.mode {
	case modeConflict:
		return []string{"conflict"}
	case modeSetup:
		return []string{"setup"}
	default:
		labels := []string{fmt.Sprintf("project: %s", m.activeProjectLabel())}
		if m.viewMode != viewActive {
			labels = append(labels, fmt.Sprintf("view: %s", m.viewMode.String()))
		}
		if m.stageFilter != "" && m.stageFilter != allStagesLabel {
			labels = append(labels, fmt.Sprintf("stage: %s", m.stageFilterLabel()))
		}
		return labels
	}
}

func (m modelUI) renderContent() string {
	contentWidth := m.appInnerWidth()
	contentHeight := m.mainContentHeight()

	if m.mode == modeSetup {
		return m.renderSetupPane(contentHeight)
	}
	if m.mode == modeConflict {
		return m.renderConflictPane(contentWidth, contentHeight)
	}

	listWidth, detailWidth := m.layoutWidths()
	center := m.renderItemsPane(listWidth, contentHeight)
	right := m.renderDetailPane(detailWidth, contentHeight)
	if m.mode == modeProjectPicker {
		return lipgloss.Place(
			contentWidth,
			contentHeight,
			lipgloss.Center,
			lipgloss.Center,
			m.renderProjectPicker(),
		)
	}
	if m.mode == modeConfirm {
		return lipgloss.Place(
			contentWidth,
			contentHeight,
			lipgloss.Center,
			lipgloss.Center,
			m.renderConfirmModal(),
		)
	}
	if m.mode == modeShortcuts {
		return lipgloss.Place(
			contentWidth,
			contentHeight,
			lipgloss.Center,
			lipgloss.Center,
			m.renderShortcutsModal(),
		)
	}
	if m.mode == modeRepos {
		return lipgloss.Place(
			contentWidth,
			contentHeight,
			lipgloss.Center,
			lipgloss.Center,
			m.renderReposModal(),
		)
	}
	content := lipgloss.JoinHorizontal(lipgloss.Top, center, right)
	if m.mode == modeCommand {
		if overlay := m.renderCommandOverlay(contentWidth); overlay != "" {
			return overlayBottom(content, overlay)
		}
	}
	return content
}

func (m modelUI) renderConflictPane(width, height int) string {
	panelStyle := m.panelStyle(m.styles.panelFocused, width, height)
	innerWidth := max(1, width-panelStyle.GetHorizontalFrameSize())
	innerHeight := max(1, height-panelStyle.GetVerticalFrameSize())
	actionLines := m.renderConflictActionLines(innerWidth)
	actionHeight := len(actionLines)
	bodyHeight := max(1, innerHeight-actionHeight)
	lines := m.conflictDisplayLines(innerWidth, bodyHeight)
	maxScroll := max(0, len(lines)-bodyHeight)
	scroll := clamp(m.detailScroll, 0, maxScroll)
	body := m.renderScrollableContentBox(lines, innerWidth, bodyHeight, scrollState{
		offset: scroll,
		window: bodyHeight,
		total:  len(lines),
	})
	actions := m.renderContentBox(strings.Join(actionLines, "\n"), innerWidth, actionHeight)
	return panelStyle.Render(lipgloss.JoinVertical(lipgloss.Left, body, actions))
}

func (m modelUI) renderFooter() string {
	footerWidth := m.appInnerWidth()
	if m.mode == modeConfirm {
		return ""
	}

	left := ""
	switch m.mode {
	case modeSearch:
		left = m.queryInput.View()
	case modeCommand:
		left = m.renderCommandInputLine()
	default:
		left = truncateANSI(m.renderFooterHint(), footerWidth)
	}

	if m.mode == modeSearch || m.mode == modeCommand {
		return left
	}

	leftWidth := lipgloss.Width(left)
	availableRight := max(0, footerWidth-leftWidth-2)
	rightText := m.footerMetaForWidth(availableRight)
	if rightText == "" {
		return left
	}

	right := rightText
	spacerWidth := max(0, footerWidth-leftWidth-lipgloss.Width(right))
	return lipgloss.NewStyle().Width(footerWidth).Render(
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			left,
			lipgloss.NewStyle().Width(spacerWidth).Render(""),
			right,
		),
	)
}

func (m modelUI) renderTooSmall() string {
	panel := m.styles.panelFocused.
		Width(max(24, min(m.width-8, 56)-m.styles.panelFocused.GetHorizontalFrameSize())).
		Height(max(5, min(m.height-6, 10)-m.styles.panelFocused.GetVerticalFrameSize()))

	content := strings.Join([]string{
		m.styles.title.Render("triage"),
		"",
		m.styles.muted.Render(fmt.Sprintf("Window too small. Need at least %dx%d.", minWidth, minHeight)),
		m.styles.muted.Render(fmt.Sprintf("Current size: %dx%d", m.width, m.height)),
		"",
		m.styles.help.Render("Resize the terminal or press q to quit."),
	}, "\n")

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		panel.Render(content),
	)
}

func (m modelUI) renderSetupPane(height int) string {
	width := max(56, min(88, m.width-8))
	panel := m.styles.panelFocused.
		Width(max(1, width-m.styles.panelFocused.GetHorizontalFrameSize())).
		Height(max(1, min(height, 18)-m.styles.panelFocused.GetVerticalFrameSize()))

	lines := []string{
		m.styles.subtitle.Render("First Run Setup"),
		"",
		"Choose how triage should store your items.",
		"",
	}

	options := []string{
		"Local-only JSON",
		"GitHub Issues sync",
	}

	for idx, option := range options {
		if idx == m.setup.selectedMode && !m.setup.enteringRepo {
			lines = append(lines, m.styles.selected.Render(option))
		} else {
			lines = append(lines, option)
		}
	}

	lines = append(lines, "")
	if m.setup.selectedMode == 0 && !m.setup.enteringRepo {
		lines = append(lines, m.styles.muted.Render("Items stay in a local JSON file under your config directory."))
	}

	if m.setup.selectedMode == 1 {
		lines = append(lines, m.styles.muted.Render("Items sync through GitHub Issues, with a local cache kept on disk."))
		lines = append(lines, "")
		lines = append(lines, m.renderSetupLabel("Repo"))
		lines = append(lines, m.setup.repoInput.View())
	}

	return lipgloss.Place(m.width-4, height, lipgloss.Center, lipgloss.Center, panel.Render(strings.Join(lines, "\n")))
}

func (m modelUI) renderItemsPane(width, height int) string {
	panelStyle := m.panelStyle(m.styles.panelMuted, width, height)
	if m.focus == focusItems && m.mode == modeNormal {
		panelStyle = m.panelStyle(m.styles.panelFocused, width, height)
	}

	lines := []string{m.renderItemsTitle(), ""}

	if len(m.filtered) == 0 {
		lines = append(lines, m.renderItemsEmptyLines()...)
		return panelStyle.Render(m.renderPaneContent(strings.Join(lines, "\n"), width, height, panelStyle))
	}

	visibleCount := m.itemVisibleCount()
	start := clamp(m.itemOffset, 0, max(0, len(m.filtered)-visibleCount))
	end := min(len(m.filtered), start+visibleCount)

	for idx := start; idx < end; idx++ {
		if idx > start && m.listDensity == densityComfortable {
			lines = append(lines, "")
		}
		item := m.items[m.filtered[idx]]
		lines = append(lines, m.renderItemRow(item, width, idx == m.selected))
	}

	content := strings.Join(lines, "\n")
	scroll := scrollState{
		offset: start,
		window: visibleCount,
		total:  len(m.filtered),
	}
	return panelStyle.Render(m.renderPaneContentWithScrollbar(content, width, height, panelStyle, scroll))
}

func (m modelUI) renderItemsTitle() string {
	allCount, archiveCount, trashCount, pendingCount := m.itemsPaneCounts()
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.styles.subtitle.Render("Items"),
		m.styles.muted.Render("  "),
		m.renderItemsTitleCount("≡", allCount, m.styles.itemCountValue),
		m.styles.muted.Render("  "),
		m.renderItemsTitleCount("✓", archiveCount, m.styles.itemArchiveValue),
		m.styles.muted.Render("  "),
		m.renderItemsTitleCount("⌫", trashCount, m.styles.itemTrashValue),
		m.styles.muted.Render("  "),
		m.renderItemsTitleCount("●", pendingCount, m.styles.itemPendingValue),
	)
}

func (m modelUI) renderItemsTitleCount(icon string, count int, valueStyle lipgloss.Style) string {
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.styles.itemStatIcon.Render(icon),
		m.styles.muted.Render(" "),
		valueStyle.Render(fmt.Sprintf("%d", count)),
	)
}

func (m modelUI) renderItemsEmptyLines() []string {
	lines := []string{""}
	switch {
	case m.syncing && m.config.StorageMode == config.ModeGitHub:
		lines = append(lines,
			m.styles.subtitle.Render("Sync in progress"),
			m.styles.muted.Render("Fetching items from GitHub Issues."),
			m.styles.muted.Render("The list refreshes when sync completes."),
		)
	case m.viewMode == viewTrash:
		if m.projectFilter != "" && m.projectFilter != allProjectsLabel {
			projectLine := m.emptyStateProjectLine("has no deleted items.")
			lines = append(lines,
				projectLine,
				m.styles.muted.Render("Items moved to trash appear here."),
				m.styles.muted.Render("Use :purge to remove trashed items permanently."),
			)
		} else if m.viewHasVisibleItems(viewTrash) && m.hasActiveItemFilters() {
			lines = append(lines,
				m.styles.subtitle.Render("No trash items match the current filters"),
			)
			for _, line := range m.filterSummaryLines() {
				lines = append(lines, m.styles.muted.Render(line))
			}
			lines = append(lines, m.styles.muted.Render("Try :project all, :stage all, or clear the search."))
		} else {
			lines = append(lines,
				m.styles.subtitle.Render("Trash is empty"),
				m.styles.muted.Render("Deleted items land here."),
				m.styles.muted.Render("Items moved to trash appear here."),
				m.styles.muted.Render("Use :purge to remove trashed items permanently."),
			)
		}
	case m.viewMode == viewArchive:
		if m.projectFilter != "" && m.projectFilter != allProjectsLabel {
			projectLine := m.emptyStateProjectLine("has no archived items.")
			lines = append(lines,
				projectLine,
				m.styles.muted.Render("Items marked as done appear here."),
			)
		} else if m.viewHasVisibleItems(viewArchive) && m.hasActiveItemFilters() {
			lines = append(lines,
				m.styles.subtitle.Render("No archived items match the current filters"),
			)
			for _, line := range m.filterSummaryLines() {
				lines = append(lines, m.styles.muted.Render(line))
			}
			lines = append(lines, m.styles.muted.Render("Try :project all, :stage all, or clear the search."))
		} else {
			lines = append(lines,
				m.styles.subtitle.Render("Archive is empty"),
				m.styles.muted.Render("Completed items land here."),
				m.styles.muted.Render("Items marked as done appear here."),
			)
		}
	case len(m.items) == 0:
		lines = append(lines,
			m.styles.subtitle.Render("No items yet"),
		)
		if m.config.StorageMode == config.ModeGitHub {
			lines = append(lines,
				m.styles.muted.Render("Create one with n or sync with s."),
				m.styles.muted.Render("GitHub issues appear here after sync."),
			)
		} else {
			lines = append(lines,
				m.styles.muted.Render("Create one with n to get started."),
				m.styles.muted.Render("Items are stored in your local JSON file."),
			)
		}
	case m.hasActiveItemFilters():
		lines = append(lines,
			m.styles.subtitle.Render("No items match the current filters"),
		)
		for _, line := range m.filterSummaryLines() {
			lines = append(lines, m.styles.muted.Render(line))
		}
		lines = append(lines,
			m.styles.muted.Render("Try :project all, :stage all, or clear the search."),
		)
	default:
		lines = append(lines,
			m.styles.subtitle.Render("No active items"),
			m.styles.muted.Render("Active items exclude archive and trash."),
			m.styles.muted.Render("Press D to switch views."),
		)
	}
	return lines
}

func (m modelUI) renderProjectPicker() string {
	projects := m.projectOptions()
	lines := []string{
		m.styles.subtitle.Render("Projects"),
		"",
	}

	for idx, project := range projects {
		label := project
		if project == allProjectsLabel {
			label = "All"
		}
		switch {
		case idx == m.projectCursor:
			lines = append(lines, m.styles.selected.Render(label))
		case project == m.projectFilter:
			lines = append(lines, m.styles.subtitle.Render(label))
		default:
			lines = append(lines, m.styles.muted.Render(label))
		}
	}

	lines = append(lines, "", m.styles.help.Render("enter apply  esc cancel"))

	maxLabelWidth := lipgloss.Width("Projects")
	for _, project := range projects {
		label := project
		if project == allProjectsLabel {
			label = "All"
		}
		if w := lipgloss.Width(label); w > maxLabelWidth {
			maxLabelWidth = w
		}
	}

	width := max(28, min(m.width-12, maxLabelWidth+8))
	height := min(max(8, len(lines)+2), m.height-10)
	panel := m.styles.panelFocused.
		Width(max(1, width-m.styles.panelFocused.GetHorizontalFrameSize()))
	return panel.Render(m.renderPaneContent(strings.Join(lines, "\n"), width, height, panel))
}

func (m modelUI) renderShortcutsModal() string {
	lines := m.shortcutsModalLines()
	width := min(max(52, m.width-20), 76)
	height := min(max(14, len(lines)+2), m.height-8)
	panel := m.panelStyle(m.styles.panelFocused, width, height)
	innerHeight := max(1, height-panel.GetVerticalFrameSize())
	scroll := scrollState{
		offset: clamp(m.shortcutsScroll, 0, max(0, len(lines)-innerHeight)),
		window: innerHeight,
		total:  len(lines),
	}
	visible := strings.Join(lines[scroll.offset:], "\n")
	return panel.Render(m.renderPaneContentWithScrollbar(visible, width, height, panel, scroll))
}

func (m modelUI) renderReposModal() string {
	lines := m.reposModalLines()
	width := min(max(52, m.width-24), 76)
	height := min(max(18, len(lines)+2), m.height-4)
	panel := m.panelStyle(m.styles.panelFocused, width, height)
	innerHeight := max(1, height-panel.GetVerticalFrameSize())
	scroll := scrollState{
		offset: clamp(m.reposScroll, 0, max(0, len(lines)-innerHeight)),
		window: innerHeight,
		total:  len(lines),
	}
	visible := strings.Join(lines[scroll.offset:], "\n")
	return panel.Render(m.renderPaneContentWithScrollbar(visible, width, height, panel, scroll))
}

func (m modelUI) renderShortcutRow(key, desc string) string {
	const keyWidth = 18
	keyCol := m.styles.shortcutKey.Width(keyWidth).Render(key)
	return lipgloss.JoinHorizontal(lipgloss.Top, keyCol, m.styles.shortcutDesc.Render(desc))
}

func (m modelUI) shortcutsModalLines() []string {
	lines := []string{
		m.styles.subtitle.Render("Shortcuts"),
		"",
		m.styles.subtitle.Render("Navigation"),
		m.renderShortcutRow("j/k or ↑/↓", "move list or scroll details"),
		m.renderShortcutRow("h/l or ←/→", "switch panes"),
		m.renderShortcutRow("tab", "open project picker"),
		"",
		m.styles.subtitle.Render("Items"),
		m.renderShortcutRow("n", "new item"),
		m.renderShortcutRow("e", "edit selected item"),
		m.renderShortcutRow("u", "undo last local change"),
		m.renderShortcutRow("S", "sync pending changes"),
		m.renderShortcutRow("D", "cycle all/archive/trash"),
		"",
		m.styles.subtitle.Render("Command"),
		m.renderShortcutRow(":", "open command palette"),
		m.renderShortcutRow("/", "search input"),
		m.renderShortcutRow(":new", "new item"),
		m.renderShortcutRow(":edit", "edit selected item"),
		m.renderShortcutRow(":sync", "review and sync"),
		m.renderShortcutRow(":search", "search items"),
		m.renderShortcutRow(":project", "filter by project"),
		m.renderShortcutRow(":view", "switch all/archive/trash"),
		m.renderShortcutRow(":sort", "change sort order"),
		m.renderShortcutRow(":open", "open selected issue"),
		m.renderShortcutRow(":undo", "undo last local change"),
		m.renderShortcutRow(":drafts", "scan drafts folder"),
		m.renderShortcutRow(":storage", "switch local/GitHub mode"),
		m.renderShortcutRow(":repos", "show repo overview"),
		m.renderShortcutRow(":delete", "move selected item to trash"),
		m.renderShortcutRow(":restore", "restore selected trash item"),
		m.renderShortcutRow(":purge", "permanently delete trash item"),
		m.renderShortcutRow(":shortcuts", "open this panel"),
		m.renderShortcutRow(":quit", "quit triage"),
		"",
		m.styles.subtitle.Render("More Commands"),
		m.renderShortcutRow(":stage", "filter by stage"),
		m.renderShortcutRow(":density", "change TUI density"),
		m.renderShortcutRow(":project-repo", "set project repo"),
		m.renderShortcutRow(":project-label", "project label sync"),
		m.renderShortcutRow(":drafts folder", "set drafts folder"),
		m.renderShortcutRow(":export json", "export local data"),
		m.renderShortcutRow(":import json", "import local data"),
	}

	return lines
}

func (m modelUI) reposModalLines() []string {
	lines := []string{
		m.styles.subtitle.Render("Repos"),
		"",
		m.styles.subtitle.Render("Default"),
	}

	defaultRepo := normalizeRepoRef(m.config.Repo)
	if defaultRepo == "" {
		lines = append(lines, m.styles.muted.Render("No default GitHub repo configured."))
	} else {
		lines = append(lines, m.styles.muted.Render(defaultRepo))
	}

	lines = append(lines, "", m.styles.subtitle.Render("Project Defaults"))
	projects := m.repoOverviewProjects()
	if len(projects) == 0 {
		lines = append(lines, m.styles.muted.Render("No project defaults yet."))
	} else {
		for _, project := range projects {
			repo := m.defaultRepoForProject(project)
			source := "fallback"
			if mapped := normalizeRepoRef(m.config.ProjectRepos[normalizeProjectKey(project)]); mapped != "" {
				source = "mapped"
				repo = mapped
			}
			repoDisplay := displayRepoValue(repo, "")
			if repoDisplay == "" {
				repoDisplay = "no repo"
			}
			line := lipgloss.JoinHorizontal(
				lipgloss.Top,
				m.renderProjectText(project, project),
				m.styles.muted.Render(" "),
				m.styles.muted.Render("->"),
				m.styles.muted.Render(" "),
				m.styles.labelMuted.Render(source),
				m.styles.muted.Render(" "),
				m.styles.labelMuted.Render("("+repoDisplay+")"),
			)
			lines = append(lines, line)
		}
	}

	lines = append(lines, "", m.styles.subtitle.Render("Tracked Repos"))
	tracked := m.syncTargetRepos(m.items)
	if len(tracked) == 0 {
		lines = append(lines, m.styles.muted.Render("No tracked repos yet."))
	} else {
		mappedRepos := map[string]struct{}{}
		for _, repo := range m.config.ProjectRepos {
			repo = normalizeRepoRef(repo)
			if repo != "" {
				mappedRepos[repo] = struct{}{}
			}
		}
		for _, repo := range tracked {
			roles := make([]string, 0, 2)
			if repo == defaultRepo && repo != "" {
				roles = append(roles, "default")
			}
			if _, ok := mappedRepos[repo]; ok {
				roles = append(roles, "mapped")
			}
			line := lipgloss.JoinHorizontal(
				lipgloss.Top,
				m.styles.muted.Render("• "),
				m.styles.muted.Render(repo),
			)
			if len(roles) > 0 {
				line = lipgloss.JoinHorizontal(
					lipgloss.Top,
					line,
					m.styles.muted.Render(" "),
					m.styles.labelMuted.Render("("+strings.Join(roles, ", ")+")"),
				)
			}
			lines = append(lines, line)
		}
	}

	return lines
}

func (m modelUI) repoOverviewProjects() []string {
	seen := map[string]struct{}{}
	projects := make([]string, 0, len(m.items)+len(m.config.ProjectRepos))
	for _, item := range m.items {
		project := strings.TrimSpace(item.Project)
		if project == "" {
			continue
		}
		key := normalizeProjectKey(project)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		projects = append(projects, project)
	}
	for project := range m.config.ProjectRepos {
		if project == "" {
			continue
		}
		if _, ok := seen[project]; ok {
			continue
		}
		seen[project] = struct{}{}
		projects = append(projects, project)
	}
	sort.Slice(projects, func(i, j int) bool {
		return strings.ToLower(projects[i]) < strings.ToLower(projects[j])
	})
	return projects
}

func (m modelUI) renderConfirmModal() string {
	width := m.confirmModalWidth()
	height := m.confirmModalHeight(width)
	panel := m.panelStyle(m.styles.panelFocused, width, height)
	innerWidth := max(1, width-panel.GetHorizontalFrameSize())
	innerHeight := max(1, height-panel.GetVerticalFrameSize())
	bodyLines := m.confirmBodyLines(innerWidth)
	actionLines := m.confirmActionLines(innerWidth)
	actionHeight := len(actionLines)
	bodyHeight := max(1, innerHeight-actionHeight)
	scroll := clamp(m.detailScroll, 0, max(0, len(bodyLines)-bodyHeight))
	body := m.renderScrollableContentBox(bodyLines, innerWidth, bodyHeight, scrollState{
		offset: scroll,
		window: bodyHeight,
		total:  len(bodyLines),
	})
	actions := m.renderContentBox(strings.Join(actionLines, "\n"), innerWidth, actionHeight)
	return panel.Render(lipgloss.JoinVertical(lipgloss.Left, body, actions))
}

func (m modelUI) confirmModalWidth() int {
	return min(max(48, m.width-24), 72)
}

func (m modelUI) confirmModalHeight(width int) int {
	panel := m.panelStyle(m.styles.panelFocused, width, 1)
	innerWidth := max(1, width-panel.GetHorizontalFrameSize())
	bodyLines := m.confirmBodyLines(innerWidth)
	actionLines := m.confirmActionLines(innerWidth)
	desired := len(bodyLines) + len(actionLines) + panel.GetVerticalFrameSize()
	return min(max(10, desired), max(10, m.height-8))
}

func (m modelUI) confirmBodyLines(innerWidth int) []string {
	lines := []string{
		m.styles.subtitle.Render("Confirm"),
		"",
		m.styles.muted.Render("No confirmation pending."),
	}

	if m.confirm != nil && m.confirm.itemIndex >= 0 && m.confirm.itemIndex < len(m.items) {
		item := m.items[m.confirm.itemIndex]
		switch m.confirm.action {
		case confirmPurge:
			lines = []string{
				m.styles.subtitle.Render("Purge Item"),
				"",
				m.styles.title.Render(item.Title),
				m.styles.muted.Render("This will permanently delete the item locally."),
				m.styles.muted.Render("If synced, it will also delete the GitHub issue."),
			}
		}
	}

	if m.confirm == nil {
		return lines
	}

	switch m.confirm.action {
	case confirmImport:
		lines = []string{
			m.styles.subtitle.Render("Import Items"),
			"",
			m.styles.muted.Render(fmt.Sprintf("Replace current local items with %d imported items.", len(m.confirm.importItems))),
			m.styles.muted.Render("This only changes local data."),
		}
		if m.confirm.importPath != "" {
			lines = append(lines, m.styles.muted.Render(truncatePlain(m.confirm.importPath, innerWidth)))
		}
	case confirmQuit:
		lines = []string{
			m.styles.subtitle.Render("Quit triage"),
			"",
			m.styles.muted.Render("Exit the application?"),
			m.styles.muted.Render("Press q or enter to quit, or esc to stay."),
		}
	case confirmSync:
		lines = append([]string{
			m.styles.subtitle.Render("Sync Pending Changes"),
			"",
			m.styles.muted.Render("Review the local changes queued for GitHub sync."),
			"",
		}, m.pendingSyncReviewLines(max(1, innerWidth))...)
	}

	return lines
}

func (m modelUI) confirmActionLines(innerWidth int) []string {
	leftButton := m.styles.confirmDangerButton.Render("(P)urge")
	switch {
	case m.confirm != nil && m.confirm.action == confirmImport:
		leftButton = m.styles.conflictOverwriteButton.Render("(I)mport")
	case m.confirm != nil && m.confirm.action == confirmQuit:
		leftButton = m.styles.confirmDangerButton.Render("(Q)uit")
	case m.confirm != nil && m.confirm.action == confirmSync:
		leftButton = m.styles.conflictOverwriteButton.Render("(S)ync")
	}
	buttons := lipgloss.JoinHorizontal(lipgloss.Top, leftButton, "  ", m.styles.confirmCancelButton.Render("(C)ancel"))
	buttonLine := lipgloss.Place(innerWidth, 1, lipgloss.Center, lipgloss.Top, buttons)
	return []string{"", buttonLine}
}

func (m modelUI) renderCommandOverlay(width int) string {
	matches := matchedCommandSuggestions(m.commandInput.Value(), m.commandSuggestions())
	if len(matches) <= 1 {
		return ""
	}

	selected := clamp(m.commandSuggestIndex, 0, len(matches)-1)
	start, end := commandSuggestionWindow(len(matches), selected, 5)
	maxInnerWidth := max(1, width-m.styles.commandMenuBox.GetHorizontalFrameSize())
	preferredInnerWidth := min(56, maxInnerWidth)
	if preferredInnerWidth < 40 && maxInnerWidth >= 40 {
		preferredInnerWidth = 40
	}
	maxLabelWidth := max(12, maxInnerWidth-2)
	lines := make([]string, 0, end-start)
	for idx := start; idx < end; idx++ {
		label := truncatePlain(matches[idx], maxLabelWidth)
		if idx == selected {
			lines = append(lines, m.styles.commandMenuSelected.Render("› "+label))
			continue
		}
		lines = append(lines, m.styles.commandMenuItem.Render("  "+label))
	}

	menuWidth := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > menuWidth {
			menuWidth = w
		}
	}
	menuInnerWidth := min(maxInnerWidth, max(preferredInnerWidth, menuWidth))

	box := m.styles.commandMenuBox.
		Width(max(1, menuInnerWidth)).
		Render(strings.Join(lines, "\n"))
	return box
}

func (m modelUI) renderDetailPane(width, height int) string {
	panelStyle := m.panelStyle(m.styles.panel, width, height)
	if m.focus == focusDetails && m.mode == modeNormal {
		panelStyle = m.panelStyle(m.styles.panelFocused, width, height)
	}

	if m.mode == modeEdit {
		return panelStyle.Render(m.renderPaneContent(m.renderEditView(), width, height, panelStyle))
	}

	if len(m.filtered) == 0 {
		return panelStyle.Render(m.renderPaneContent(strings.Join(m.renderDetailEmptyLines(), "\n"), width, height, panelStyle))
	}

	item := m.items[m.filtered[m.selected]]
	innerWidth := max(1, width-panelStyle.GetHorizontalFrameSize())
	lines := m.renderDetailLines(item, innerWidth)
	innerHeight := max(1, height-panelStyle.GetVerticalFrameSize())
	maxScroll := max(0, len(lines)-innerHeight)
	scroll := clamp(m.detailScroll, 0, maxScroll)
	visible := strings.Join(lines[scroll:], "\n")
	return panelStyle.Render(m.renderPaneContentWithScrollbar(visible, width, height, panelStyle, scrollState{
		offset: scroll,
		window: innerHeight,
		total:  len(lines),
	}))
}

func (m modelUI) renderDetailEmptyLines() []string {
	lines := []string{}

	switch {
	case m.syncing && m.config.StorageMode == config.ModeGitHub:
		lines = append(lines,
			m.styles.subtitle.Render("Sync in progress"),
			m.styles.muted.Render("Waiting for GitHub issues."),
			m.styles.muted.Render("Details appear after sync completes."),
		)
	case m.viewMode == viewTrash:
		if m.projectFilter != "" && m.projectFilter != allProjectsLabel {
			lines = append(lines,
				m.styles.muted.Render("No item selected."),
			)
		} else if m.viewHasVisibleItems(viewTrash) && m.hasActiveItemFilters() {
			lines = append(lines,
				m.styles.muted.Render("No item selected."),
			)
		} else {
			lines = append(lines,
				m.styles.muted.Render("No item selected."),
			)
		}
	case m.viewMode == viewArchive:
		if m.projectFilter != "" && m.projectFilter != allProjectsLabel {
			lines = append(lines,
				m.styles.muted.Render("No item selected."),
			)
		} else if m.viewHasVisibleItems(viewArchive) && m.hasActiveItemFilters() {
			lines = append(lines,
				m.styles.muted.Render("No item selected."),
			)
		} else {
			lines = append(lines,
				m.styles.muted.Render("No item selected."),
			)
		}
	case len(m.items) == 0:
		lines = append(lines,
			m.styles.muted.Render("Create an item with n."),
			m.styles.muted.Render("The selected item appears here."),
		)
	case m.hasActiveItemFilters():
		lines = append(lines,
			m.styles.muted.Render("No item is selected because the list is empty."),
			m.styles.muted.Render("Current filters:"),
		)
		for _, line := range m.filterSummaryLines() {
			lines = append(lines, m.styles.muted.Render(line))
		}
	default:
		lines = append(lines,
			m.styles.muted.Render("No active item is selected."),
			m.styles.muted.Render("Switch views or create a new item."),
		)
	}

	lines = append(lines,
		"",
		m.styles.subtitle.Render("Current Storage"),
		m.styles.muted.Render(m.storageSummary()),
	)

	return lines
}

func (m modelUI) renderConflictView(contentWidth int) string {
	return strings.Join(m.renderConflictViewLines(contentWidth), "\n")
}

func (m modelUI) renderConflictDisplayLines(contentWidth int) []string {
	return strings.Split(m.renderConflictView(contentWidth), "\n")
}

func (m modelUI) conflictDisplayLines(innerWidth, bodyHeight int) []string {
	contentWidth := max(24, innerWidth)
	lines := m.renderConflictDisplayLines(contentWidth)
	if innerWidth >= 4 && len(lines) > bodyHeight {
		contentWidth = max(24, innerWidth-2)
		lines = m.renderConflictDisplayLines(contentWidth)
	}
	return lines
}

func (m modelUI) renderConflictViewLines(contentWidth int) []string {
	if m.conflict == nil {
		return []string{
			m.styles.subtitle.Render("Conflict"),
			"",
			m.styles.muted.Render("No active conflict."),
		}
	}

	local := m.conflict.local
	remote := m.conflict.remote
	gutter := 4
	columnWidth := max(10, (contentWidth-gutter)/2)
	rightWidth := max(10, contentWidth-gutter-columnWidth)

	localBodyLines, remoteBodyLines := m.renderConflictBodyLines(local.Body, remote.Body, columnWidth, rightWidth)

	type conflictField struct {
		name        string
		changed     bool
		localLines  []string
		remoteLines []string
	}

	fields := []conflictField{
		{
			name:        "Body",
			changed:     local.Body != remote.Body,
			localLines:  localBodyLines,
			remoteLines: remoteBodyLines,
		},
		{
			name:        "Title",
			changed:     local.Title != remote.Title,
			localLines:  []string{m.styles.title.Render(local.Title)},
			remoteLines: []string{m.styles.title.Render(remote.Title)},
		},
		{
			name:        "Project",
			changed:     local.Project != remote.Project,
			localLines:  []string{m.renderProjectLabel(local.Project)},
			remoteLines: []string{m.renderProjectLabel(remote.Project)},
		},
		{
			name:        "Type",
			changed:     normalizeType(local.Type) != normalizeType(remote.Type),
			localLines:  []string{m.renderType(local.Type)},
			remoteLines: []string{m.renderType(remote.Type)},
		},
		{
			name:        "Stage",
			changed:     local.Stage != remote.Stage,
			localLines:  []string{m.renderStage(local.Stage)},
			remoteLines: []string{m.renderStage(remote.Stage)},
		},
		{
			name:        "Labels",
			changed:     !sameLabels(local.Labels(), remote.Labels()),
			localLines:  []string{strings.Join(m.renderLabels(local.Labels()), " ")},
			remoteLines: []string{strings.Join(m.renderLabels(remote.Labels()), " ")},
		},
	}

	changedSections := make([]string, 0, len(fields))
	changedNames := make([]string, 0, len(fields))
	unchangedNames := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.changed {
			changedNames = append(changedNames, field.name)
			changedSections = append(changedSections, m.renderConflictField(field.name, true, field.localLines, field.remoteLines, columnWidth, rightWidth))
			continue
		}
		unchangedNames = append(unchangedNames, field.name)
	}

	lines := []string{
		m.styles.subtitle.Render("Conflict"),
	}

	if local.Title == remote.Title && strings.TrimSpace(local.Title) != "" {
		lines = append(lines, m.styles.title.Render(local.Title))
	} else {
		lines = append(lines, m.styles.muted.Render("Resolve differences between local and GitHub versions."))
	}

	lines = append(lines,
		"",
		m.styles.muted.Render("GitHub changed since your last sync."),
	)
	if len(changedNames) > 0 {
		lines = append(lines, m.styles.conflictChanged.Render("Changed: "+strings.Join(changedNames, ", ")))
	}
	if len(unchangedNames) > 0 {
		lines = append(lines, m.styles.muted.Render("Unchanged: "+strings.Join(unchangedNames, ", ")))
	}

	lines = append(lines,
		"",
		m.renderConflictColumns(
			[]string{
				m.styles.conflictLocal.Render("Local"),
				m.styles.muted.Render(fmt.Sprintf("updated %s", local.UpdatedAt.Format(time.RFC822))),
			},
			[]string{
				m.styles.conflictRemote.Render("GitHub"),
				m.styles.muted.Render(fmt.Sprintf("updated %s", remote.UpdatedAt.Format(time.RFC822))),
			},
			columnWidth,
			rightWidth,
		),
	)

	if len(changedSections) == 0 {
		lines = append(lines,
			"",
			m.styles.muted.Render("No field-level differences detected."),
		)
	} else {
		lines = append(lines, "")
		for idx, section := range changedSections {
			if idx > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, section)
		}
	}

	return lines
}

func (m modelUI) renderConflictActionLines(width int) []string {
	prompt := lipgloss.PlaceHorizontal(width, lipgloss.Center, m.styles.muted.Render(truncatePlain("Choose which version to keep.", width)))
	buttons := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.styles.conflictRemoteButton.Render("(R)emote"),
		"  ",
		m.styles.conflictOverwriteButton.Render("(O)verwrite"),
		"  ",
		m.styles.confirmCancelButton.Render("(Esc) Cancel"),
	)
	buttonLine := lipgloss.PlaceHorizontal(width, lipgloss.Center, truncate(buttons, width))
	return []string{"", prompt, buttonLine}
}

func (m modelUI) renderConflictField(name string, changed bool, localLines, remoteLines []string, localWidth, remoteWidth int) string {
	lines := []string{
		m.renderConflictFieldTitle(name, changed),
		m.renderConflictColumns(localLines, remoteLines, localWidth, remoteWidth),
	}
	return strings.Join(lines, "\n")
}

func (m modelUI) renderConflictFieldTitle(name string, changed bool) string {
	label := name
	if changed {
		label += " (changed)"
		return m.styles.conflictChanged.Render(label)
	}
	return m.styles.subtitle.Render(label)
}

func (m modelUI) renderConflictColumns(localLines, remoteLines []string, localWidth, remoteWidth int) string {
	left := lipgloss.NewStyle().
		Width(localWidth).
		MaxWidth(localWidth).
		Render(strings.Join(localLines, "\n"))
	right := lipgloss.NewStyle().
		Width(remoteWidth).
		MaxWidth(remoteWidth).
		Render(strings.Join(remoteLines, "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, "    ", right)
}

func conflictBodyPreview(body string, width int) []string {
	lines := wrapPlainLines(body, max(1, width))
	if len(lines) == 0 {
		return []string{""}
	}
	const maxLines = 4
	if len(lines) > maxLines {
		lines = append([]string(nil), lines[:maxLines]...)
		lines[maxLines-1] = truncatePlain(lines[maxLines-1], max(1, width-1)) + "…"
	}
	return lines
}

func (m modelUI) renderConflictBodyLines(localBody, remoteBody string, localWidth, remoteWidth int) ([]string, []string) {
	if localBody == remoteBody {
		return conflictBodyPreview(localBody, localWidth), conflictBodyPreview(remoteBody, remoteWidth)
	}

	if strings.Contains(localBody, "\n") || strings.Contains(remoteBody, "\n") {
		prefix, localDelta, remoteDelta, suffix := splitConflictBodyLines(localBody, remoteBody)
		return m.renderConflictBodyLineBlock(prefix, localDelta, suffix, localWidth, m.styles.conflictLocal),
			m.renderConflictBodyLineBlock(prefix, remoteDelta, suffix, remoteWidth, m.styles.conflictRemote)
	}

	prefix, localDelta, remoteDelta, suffix := splitConflictBody(localBody, remoteBody)
	return m.renderConflictBodyExcerpt(prefix, localDelta, suffix, localWidth, m.styles.conflictLocal),
		m.renderConflictBodyExcerpt(prefix, remoteDelta, suffix, remoteWidth, m.styles.conflictRemote)
}

func (m modelUI) renderConflictBodyExcerpt(prefix, delta, suffix string, width int, deltaStyle lipgloss.Style) []string {
	width = max(1, width)

	lines := make([]string, 0, 5)
	before := compactConflictContext(lastRunes(prefix, 48))
	if before != "" {
		lines = append(lines, m.styles.muted.Render(truncatePlain("... "+before, width)))
	}

	deltaLines := conflictDeltaLines(delta, width)
	for _, line := range deltaLines {
		lines = append(lines, deltaStyle.Render(line))
	}

	after := compactConflictContext(firstRunes(suffix, 48))
	if after != "" {
		lines = append(lines, m.styles.muted.Render(truncatePlain(after+" ...", width)))
	}

	if len(lines) == 0 {
		return []string{m.styles.muted.Render("(empty)")}
	}
	return lines
}

func splitConflictBody(local, remote string) (string, string, string, string) {
	localRunes := []rune(local)
	remoteRunes := []rune(remote)

	prefixLen := 0
	for prefixLen < len(localRunes) && prefixLen < len(remoteRunes) && localRunes[prefixLen] == remoteRunes[prefixLen] {
		prefixLen++
	}

	suffixLen := 0
	for suffixLen < len(localRunes)-prefixLen &&
		suffixLen < len(remoteRunes)-prefixLen &&
		localRunes[len(localRunes)-1-suffixLen] == remoteRunes[len(remoteRunes)-1-suffixLen] {
		suffixLen++
	}

	return string(localRunes[:prefixLen]),
		string(localRunes[prefixLen : len(localRunes)-suffixLen]),
		string(remoteRunes[prefixLen : len(remoteRunes)-suffixLen]),
		string(localRunes[len(localRunes)-suffixLen:])
}

func splitConflictBodyLines(local, remote string) ([]string, []string, []string, []string) {
	localLines := strings.Split(local, "\n")
	remoteLines := strings.Split(remote, "\n")

	prefixLen := 0
	for prefixLen < len(localLines) && prefixLen < len(remoteLines) && localLines[prefixLen] == remoteLines[prefixLen] {
		prefixLen++
	}

	suffixLen := 0
	for suffixLen < len(localLines)-prefixLen &&
		suffixLen < len(remoteLines)-prefixLen &&
		localLines[len(localLines)-1-suffixLen] == remoteLines[len(remoteLines)-1-suffixLen] {
		suffixLen++
	}

	return append([]string(nil), localLines[:prefixLen]...),
		append([]string(nil), localLines[prefixLen:len(localLines)-suffixLen]...),
		append([]string(nil), remoteLines[prefixLen:len(remoteLines)-suffixLen]...),
		append([]string(nil), localLines[len(localLines)-suffixLen:]...)
}

func compactConflictContext(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func conflictDeltaLines(s string, width int) []string {
	if s == "" {
		return []string{"(empty)"}
	}

	lines := wrapPlainLines(s, width)
	if len(lines) == 0 {
		return []string{"(empty)"}
	}

	blank := true
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			blank = false
			break
		}
	}
	if blank {
		return []string{""}
	}

	return lines
}

func (m modelUI) renderConflictBodyLineBlock(prefix, delta, suffix []string, width int, deltaStyle lipgloss.Style) []string {
	width = max(1, width)

	lines := make([]string, 0, len(prefix)+len(delta)+len(suffix))
	appendWrapped := func(src []string, style lipgloss.Style) {
		for _, line := range src {
			if line == "" {
				lines = append(lines, "")
				continue
			}
			for _, wrapped := range wrapPlainLines(line, width) {
				lines = append(lines, style.Render(wrapped))
			}
		}
	}

	appendWrapped(prefix, m.styles.muted)
	appendWrapped(delta, deltaStyle)
	appendWrapped(suffix, m.styles.muted)

	if len(lines) == 0 {
		return []string{m.styles.muted.Render("(empty)")}
	}
	return lines
}

func firstRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

func lastRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

func sameLabels(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}

	leftCopy := append([]string(nil), left...)
	rightCopy := append([]string(nil), right...)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	for i := range leftCopy {
		if leftCopy[i] != rightCopy[i] {
			return false
		}
	}
	return true
}

func (m modelUI) renderDetailLines(item model.Item, width int) []string {
	now := time.Now()
	lines := m.renderDetailTitleLines(item.Title, width)
	if m.listDensity == densityComfortable {
		lines = append(lines, "")
	}
	lines = append(lines, m.renderDetailIdentityLines(item, width)...)
	lines = append(lines, m.renderDetailMetaLines("Repo", detailRepoLabel(item.Repo), width)...)
	lines = append(lines, m.renderDetailMetaLines("Updated", fmt.Sprintf("%s (%s)", item.UpdatedAt.Format(time.RFC822), relativeTimeLabel(now, item.UpdatedAt)), width)...)
	if m.listDensity == densityComfortable {
		lines = append(lines, "")
	}
	lines = append(lines, m.styles.subtitle.Render("Body"))

	bodyLines := m.renderMarkdownBodyLines(item.Body, max(1, width))
	if len(bodyLines) == 0 {
		bodyLines = []string{""}
	}
	lines = append(lines, bodyLines...)

	return lines
}

func (m modelUI) renderDetailTitleLines(title string, width int) []string {
	wrapped := wrapPlainLines(title, max(1, width))
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}
	lines := make([]string, 0, len(wrapped))
	for _, line := range wrapped {
		lines = append(lines, m.styles.title.Render(line))
	}
	return lines
}

func (m modelUI) renderDetailIdentityLines(item model.Item, width int) []string {
	projectText := m.renderProjectText(item.Project, item.Project)
	typeText := m.renderTypeText(item.Type, false)
	stageText := m.renderStageText(item.Stage, false)
	separator := m.styles.muted.Render(" • ")
	line := lipgloss.JoinHorizontal(
		lipgloss.Left,
		projectText,
		separator,
		typeText,
		separator,
		stageText,
	)
	if lipgloss.Width(line) <= width {
		return []string{line}
	}

	lines := m.renderDetailMetaLines("Project", item.Project, width)
	lines = append(lines, m.renderDetailMetaLines("Type", string(normalizeType(item.Type)), width)...)
	lines = append(lines, m.renderDetailMetaLines("Stage", string(item.Stage), width)...)
	return lines
}

func (m modelUI) renderEditView() string {
	lines := []string{}
	lines = append(lines,
		m.renderEditMetaTextLines("Title", m.form.titleInput.Value(), m.form.titleInput.View(), 0)...,
	)

	lines = append(lines,
		m.renderEditMetaTextLines("Project", m.form.projectInput.Value(), m.form.projectInput.View(), 1)...,
	)
	lines = append(lines,
		m.renderEditMetaTextLines("Repo", displayRepoValue(normalizeRepoRef(m.form.repoInput.Value()), m.defaultRepoForProject(m.form.projectInput.Value())), m.form.repoInput.View(), 2)...,
	)
	lines = append(lines,
		m.renderEditMetaChoiceLine("Type", string(normalizeType(model.Types[m.form.typeIndex])), m.renderTypeOptions(), 3),
		m.renderEditMetaChoiceLine("Stage", string(model.Stages[m.form.stageIndex]), m.renderStageOptions(), 4),
		"",
		m.renderEditRow("Body", "", 5),
		m.styles.editValue.PaddingLeft(0).Render(m.form.bodyInput.View()),
	)

	return strings.Join(lines, "\n")
}

func (m modelUI) renderCommandInputLine() string {
	line := m.commandInput.View()
	if m.commandInput.Position() == len([]rune(m.commandInput.Value())) {
		suffix := m.commandCompletionSuffix()
		hint := commandArgumentHint(m.commandInput.Value(), suffix)
		switch {
		case suffix != "":
			ghost := suffix
			if hint != "" {
				if !strings.HasSuffix(ghost, " ") {
					ghost += " "
				}
				ghost += hint
			}
			line += m.styles.commandGhost.Render(ghost)
		case hint != "":
			if strings.HasSuffix(m.commandInput.Value(), " ") {
				line += m.styles.commandGhost.Render(hint)
			} else {
				line += m.styles.commandGhost.Render(" " + hint)
			}
		}
	}
	return line
}

func (m modelUI) renderFooterHint() string {
	segments := m.footerHintSegments()
	if len(segments) == 0 {
		return ""
	}

	parts := make([]string, 0, len(segments)*2-1)
	for idx, segment := range segments {
		if idx > 0 {
			parts = append(parts, m.styles.footerSeparator.Render("  "))
		}
		parts = append(parts,
			m.styles.footerKey.Render(segment[0])+" "+m.styles.footerText.Render(segment[1]),
		)
	}
	return strings.Join(parts, "")
}

func (m modelUI) footerHintSegments() [][2]string {
	switch m.mode {
	case modeSetup:
		if m.setup.enteringRepo {
			return [][2]string{{"enter", "save repo"}, {"esc", "back"}}
		}
		return [][2]string{{"j/k", "move"}, {"enter", "select"}}
	case modeProjectPicker:
		return [][2]string{{"enter", "apply"}, {"esc", "cancel"}}
	case modeShortcuts:
		return [][2]string{{"j/k or ↑/↓", "scroll"}, {"q/?", "close"}}
	case modeRepos:
		return [][2]string{{"j/k or ↑/↓", "scroll"}, {"q/esc", "close"}}
	case modeConfirm:
		if m.confirm != nil && m.confirm.action == confirmQuit {
			return [][2]string{{"q/enter", "quit"}, {"c/esc", "cancel"}}
		}
		if m.confirm != nil && m.confirm.action == confirmSync {
			return [][2]string{{"s/enter", "sync"}, {"c/esc", "cancel"}}
		}
		return [][2]string{{"p/enter", "purge"}, {"c/esc", "cancel"}}
	case modeConflict:
		return nil
	case modeEdit:
		return [][2]string{{"ctrl+s", "save"}, {"esc", "cancel"}}
	default:
		segments := [][2]string{{":", "command"}, {"/", "search"}, {"S", "sync"}}
		if m.undo != nil {
			segments = append(segments, [2]string{"u", "undo"})
		}
		segments = append(segments, [2]string{"?", "shortcuts"}, [2]string{"tab", "projects"})
		return segments
	}
}

func (m modelUI) footerMeta() string {
	segments := m.footerMetaSegments()
	rendered := make([]string, 0, len(segments))
	for _, segment := range segments {
		rendered = append(rendered, m.renderFooterMetaSegment(segment))
	}
	return strings.Join(rendered, m.styles.footerSeparator.Render("  "))
}

func (m modelUI) footerMetaForWidth(width int) string {
	if width <= 0 {
		return ""
	}
	segments := m.footerMetaSegments()
	if len(segments) == 0 {
		return ""
	}
	parts := make([]footerMetaSegment, 0, len(segments))
	used := 0
	for _, segment := range segments {
		segWidth := lipgloss.Width(segment.plain())
		addWidth := segWidth
		if len(parts) > 0 {
			addWidth += 2
		}
		if used+addWidth > width {
			break
		}
		parts = append(parts, segment)
		used += addWidth
	}
	if len(parts) == 0 {
		return m.styles.footerMetaValue.Render(truncatePlain(segments[0].plain(), width))
	}
	rendered := make([]string, 0, len(parts))
	for _, segment := range parts {
		rendered = append(rendered, m.renderFooterMetaSegment(segment))
	}
	return strings.Join(rendered, m.styles.footerSeparator.Render("  "))
}

func (m modelUI) footerMetaSegments() []footerMetaSegment {
	switch m.mode {
	case modeSetup, modeSearch, modeCommand, modeConfirm, modeRepos:
		return nil
	default:
		_, conflictCount, failedCount := m.syncCounts()
		parts := []footerMetaSegment{}
		parts = append(parts, footerMetaSegment{label: m.sortMode.String(), value: m.sortDirectionLabel()})
		switch m.config.StorageMode {
		case config.ModeGitHub:
			parts = append(parts, footerMetaSegment{label: "mode", value: "github"})
		case config.ModeLocal:
			parts = append(parts, footerMetaSegment{label: "mode", value: "local"})
		default:
			parts = append(parts, footerMetaSegment{label: "mode", value: "setup"})
		}
		if m.config.StorageMode == config.ModeGitHub && !m.config.LastSuccessfulSyncAt.IsZero() {
			parts = append(parts, footerMetaSegment{label: "last sync", value: relativeTimeLabel(time.Now(), m.config.LastSuccessfulSyncAt)})
		}
		if conflictCount > 0 {
			parts = append(parts, footerMetaSegment{label: "conflicts", value: fmt.Sprintf("%d", conflictCount)})
		}
		if failedCount > 0 {
			parts = append(parts, footerMetaSegment{label: "failed", value: fmt.Sprintf("%d", failedCount)})
		}
		if m.lastSearch != "" {
			parts = append(parts, footerMetaSegment{label: "search", value: fmt.Sprintf("%q", m.lastSearch)})
		}
		if m.viewMode != viewActive {
			parts = append(parts, footerMetaSegment{label: "view", value: m.viewMode.String()})
		}
		if m.stageFilter != "" && m.stageFilter != allStagesLabel {
			parts = append(parts, footerMetaSegment{label: "stage", value: m.stageFilterLabel()})
		}
		if m.listDensity == densityCompact {
			parts = append(parts, footerMetaSegment{label: "density", value: "compact"})
		}
		return parts
	}
}

func (m modelUI) renderFooterMetaSegment(segment footerMetaSegment) string {
	if segment.label == "" {
		return m.styles.footerMetaValue.Render(segment.value)
	}
	if segment.value == "" {
		return m.styles.footerMetaLabel.Render(segment.label)
	}
	return m.styles.footerMetaLabel.Render(segment.label) + " " + m.styles.footerMetaValue.Render(segment.value)
}

func (s footerMetaSegment) plain() string {
	if s.label == "" {
		return s.value
	}
	if s.value == "" {
		return s.label
	}
	return s.label + " " + s.value
}

func (m modelUI) syncCounts() (pending, conflicts, failed int) {
	for _, item := range m.items {
		if item.IsLocallyPurged() {
			pending++
			continue
		}
		if item.SyncConflict {
			conflicts++
		}
		if item.SyncError != "" {
			failed++
		}
		if item.HasPendingSync() {
			pending++
		}
	}
	return pending, conflicts, failed
}

func (m modelUI) itemsPaneCounts() (all, archive, trash, pending int) {
	for _, item := range m.items {
		if !m.itemsPaneCountsMatchProject(item) {
			continue
		}
		if item.IsLocallyPurged() {
			pending++
			continue
		}
		all++
		if item.IsTrashed() {
			trash++
		} else if item.IsDone() {
			archive++
		}
		if item.HasPendingSync() {
			pending++
		}
	}
	return all, archive, trash, pending
}

func (m modelUI) itemsPaneCountsMatchProject(item model.Item) bool {
	if m.projectFilter == "" || m.projectFilter == allProjectsLabel {
		return true
	}
	return item.Project == m.projectFilter
}

func (m modelUI) pendingSyncItems() []model.Item {
	items := make([]model.Item, 0)
	for _, item := range m.items {
		if item.HasPendingSync() {
			items = append(items, item)
		}
	}
	return items
}

func (m modelUI) pendingSyncReviewLines(width int) []string {
	items := m.pendingSyncItems()
	if len(items) == 0 {
		return []string{m.styles.muted.Render("No local changes are waiting to sync.")}
	}

	lines := []string{
		m.styles.muted.Render(fmt.Sprintf("%d local changes will be synced to GitHub.", len(items))),
		"",
	}
	for _, item := range items {
		repo := displayRepoValue(item.Repo, m.defaultRepoForProject(item.Project))
		titleLine := lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.styles.muted.Render("• "),
			m.styles.title.Render(item.Title),
		)
		detailLine := lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.styles.muted.Render("  "),
			m.renderPendingSyncOperation(item.PendingSync),
			m.styles.muted.Render("  "),
			m.styles.muted.Render(repo),
		)
		lines = append(lines, truncateANSI(titleLine, width))
		lines = append(lines, truncateANSI(detailLine, width))
	}
	return lines
}

func (m modelUI) renderPendingSyncOperation(op model.SyncOperation) string {
	label := string(op)
	if label == "" {
		label = "update"
	}
	switch op {
	case model.SyncDelete, model.SyncPurge:
		return m.styles.statusWarning.Render(label)
	case model.SyncRestore:
		return m.styles.statusSuccess.Render(label)
	default:
		return m.styles.statusPending.Render(label)
	}
}

func (m modelUI) renderEditFieldBlock(label, value string, focusIndex int) string {
	return lipgloss.NewStyle().
		Height(2).
		Render(m.renderEditRow(label, value, focusIndex))
}

func (m modelUI) renderEditRow(label, value string, focusIndex int) string {
	labelWidth := 10
	separatorWidth := 1
	valueWidth := m.editFieldValueWidth()
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(labelWidth).Render(m.renderEditLabel(label, focusIndex)),
		lipgloss.NewStyle().Width(separatorWidth).Render(" "),
		lipgloss.NewStyle().Width(valueWidth).MaxWidth(valueWidth).Render(m.styles.editValue.Render(value)),
	)
}

func (m modelUI) renderEditLabel(label string, focusIndex int) string {
	if m.form.focusIndex == focusIndex {
		return m.styles.editLabelActive.Render(label)
	}
	return m.styles.editLabel.Render(label)
}

func (m modelUI) renderSetupLabel(label string) string {
	if m.setup.enteringRepo {
		return m.styles.editLabelActive.Render(label)
	}
	return m.styles.editLabel.Render(label)
}

func (m modelUI) renderStageOptions() string {
	parts := make([]string, 0, len(model.Stages))
	for idx, stage := range model.Stages {
		parts = append(parts, m.renderEditStageOption(stage, idx == m.form.stageIndex))
	}
	return strings.Join(parts, " ")
}

func (m modelUI) renderTypeOptions() string {
	parts := make([]string, 0, len(model.Types))
	for idx, itemType := range model.Types {
		parts = append(parts, m.renderEditTypeOption(itemType, idx == m.form.typeIndex))
	}
	return strings.Join(parts, " ")
}

func (m modelUI) renderType(itemType model.Type) string {
	switch normalizeType(itemType) {
	case model.TypeFeature:
		return m.styles.typeFeature.Render(string(model.TypeFeature))
	case model.TypeBug:
		return m.styles.typeBug.Render(string(model.TypeBug))
	case model.TypeChore:
		return m.styles.typeChore.Render(string(model.TypeChore))
	default:
		return m.styles.labelMuted.Render(string(normalizeType(itemType)))
	}
}

func (m modelUI) renderTypeText(itemType model.Type, active bool) string {
	style := m.typeTextStyle(itemType)
	if active {
		style = style.Bold(true)
	}
	return style.Render(string(normalizeType(itemType)))
}

func (m modelUI) renderEditTypeOption(itemType model.Type, active bool) string {
	style := m.typeTextStyle(itemType).Padding(0, 1)
	if active {
		style = style.Background(lipgloss.Color("236")).Bold(true)
	}
	return style.Render(string(normalizeType(itemType)))
}

func (m modelUI) renderStage(stage model.Stage) string {
	switch stage {
	case model.StageIdea:
		return m.styles.stageIdea.Render(string(stage))
	case model.StagePlanned:
		return m.styles.stagePlanned.Render(string(stage))
	case model.StageActive:
		return m.styles.stageActive.Render(string(stage))
	case model.StageBlocked:
		return m.styles.stageBlocked.Render(string(stage))
	case model.StageDone:
		return m.styles.stageDone.Render(string(stage))
	default:
		return m.styles.labelMuted.Render(string(stage))
	}
}

func (m modelUI) renderStageText(stage model.Stage, active bool) string {
	style := m.stageTextStyle(stage)
	if active {
		style = style.Bold(true)
	}
	return style.Render(string(stage))
}

func (m modelUI) typeTextStyle(itemType model.Type) lipgloss.Style {
	var style lipgloss.Style
	switch normalizeType(itemType) {
	case model.TypeFeature:
		style = m.styles.typeFeatureText.Copy()
	case model.TypeBug:
		style = m.styles.typeBugText.Copy()
	case model.TypeChore:
		style = m.styles.typeChoreText.Copy()
	default:
		style = m.styles.muted.Copy()
	}

	return style.
		Bold(false).
		Underline(false).
		Background(lipgloss.NoColor{})
}

func (m modelUI) renderEditStageOption(stage model.Stage, active bool) string {
	style := m.stageTextStyle(stage)
	switch stage {
	case model.StageIdea, model.StagePlanned, model.StageActive, model.StageBlocked, model.StageDone:
	default:
		return style.Render(string(stage))
	}

	style = style.Padding(0, 1)
	if active {
		style = style.
			Background(lipgloss.Color("236")).
			Bold(true)
	}

	return style.Render(string(stage))
}

func (m modelUI) stageTextStyle(stage model.Stage) lipgloss.Style {
	var style lipgloss.Style
	switch stage {
	case model.StageIdea:
		style = m.styles.stageIdeaText.Copy()
	case model.StagePlanned:
		style = m.styles.stagePlannedText.Copy()
	case model.StageActive:
		style = m.styles.stageActiveText.Copy()
	case model.StageBlocked:
		style = m.styles.stageBlockedText.Copy()
	case model.StageDone:
		style = m.styles.stageDoneText.Copy()
	default:
		style = m.styles.muted.Copy()
	}

	return style.
		Bold(false).
		Underline(false).
		Background(lipgloss.NoColor{})
}

func (m modelUI) renderProjectLabel(label string) string {
	return m.styles.label.Render(label)
}

func (m modelUI) renderProjectText(project, text string) string {
	color := githubsync.ProjectLabelColor(project)
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#" + color)).
		Render(text)
}

func (m modelUI) renderDetailMetaLine(label, value string) string {
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(8).Render(label),
		" ",
		value,
	)
}

func (m modelUI) renderDetailMetaLines(label, value string, width int) []string {
	valueWidth := max(1, width-9)
	wrapped := wrapPlainLines(value, valueWidth)
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}

	lines := make([]string, 0, len(wrapped))
	for idx, line := range wrapped {
		lineLabel := label
		if idx > 0 {
			lineLabel = ""
		}
		lines = append(lines, m.renderDetailMetaLine(lineLabel, m.styles.muted.Render(line)))
	}
	return lines
}

type markdownRenderSegment struct {
	text         string
	style        lipgloss.Style
	keepTogether bool
}

func (m modelUI) renderMarkdownBodyLines(body string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	if strings.TrimSpace(body) == "" {
		return []string{""}
	}

	lines := []string{}
	inCodeBlock := false
	for _, raw := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}

		if inCodeBlock {
			lines = append(lines, m.renderMarkdownCodeBlockLines(raw, width)...)
			continue
		}

		if trimmed == "" {
			lines = append(lines, "")
			continue
		}

		if level, text, ok := parseMarkdownHeading(raw); ok {
			lines = append(lines, m.renderMarkdownHeadingLines(level, text, width)...)
			continue
		}

		if text, ok := parseMarkdownQuote(raw); ok {
			prefix := m.styles.markdownQuote.Render("│ ")
			lines = append(lines, m.renderMarkdownPrefixedLines(prefix, prefix, text, width, nil)...)
			continue
		}

		if prefix, text, ok := m.parseMarkdownListPrefix(raw); ok {
			continuation := strings.Repeat(" ", lipgloss.Width(prefix))
			lines = append(lines, m.renderMarkdownPrefixedLines(prefix, continuation, text, width, m.markdownInlineSegments(text))...)
			continue
		}

		lines = append(lines, wrapMarkdownSegments(m.markdownInlineSegments(raw), width)...)
	}

	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func (m modelUI) renderMarkdownHeadingLines(level int, text string, width int) []string {
	style := m.styles.markdownHeading2
	if level <= 1 {
		style = m.styles.markdownHeading1
	}
	wrapped := wrapPlainLines(text, max(1, width))
	if len(wrapped) == 0 {
		return []string{style.Render("")}
	}
	lines := make([]string, 0, len(wrapped))
	for _, line := range wrapped {
		lines = append(lines, style.Render(line))
	}
	return lines
}

func (m modelUI) renderMarkdownCodeBlockLines(raw string, width int) []string {
	chunks := splitPlainByWidth(raw, max(1, width))
	if len(chunks) == 0 {
		return []string{m.styles.markdownCodeBlock.Render("")}
	}
	lines := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		lines = append(lines, m.styles.markdownCodeBlock.Render(chunk))
	}
	return lines
}

func (m modelUI) renderMarkdownPrefixedLines(prefix, continuation, text string, width int, segments []markdownRenderSegment) []string {
	contentWidth := max(1, width-lipgloss.Width(prefix))
	if segments == nil {
		segments = m.markdownInlineSegments(text)
	}
	wrapped := wrapMarkdownSegments(segments, contentWidth)
	if len(wrapped) == 0 {
		return []string{prefix}
	}

	lines := make([]string, 0, len(wrapped))
	for idx, line := range wrapped {
		currentPrefix := prefix
		if idx > 0 {
			currentPrefix = continuation
		}
		lines = append(lines, currentPrefix+line)
	}
	return lines
}

func (m modelUI) parseMarkdownListPrefix(raw string) (string, string, bool) {
	trimmed := strings.TrimLeft(raw, " \t")
	if trimmed == "" {
		return "", "", false
	}

	if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ ") {
		return m.styles.markdownListMarker.Render("• "), strings.TrimSpace(trimmed[2:]), true
	}

	idx := 0
	for idx < len(trimmed) && trimmed[idx] >= '0' && trimmed[idx] <= '9' {
		idx++
	}
	if idx == 0 || idx+1 >= len(trimmed) || trimmed[idx] != '.' || trimmed[idx+1] != ' ' {
		return "", "", false
	}

	prefix := trimmed[:idx+1] + " "
	return m.styles.markdownListMarker.Render(prefix), strings.TrimSpace(trimmed[idx+1:]), true
}

func (m modelUI) markdownInlineSegments(raw string) []markdownRenderSegment {
	segments := []markdownRenderSegment{}
	for len(raw) > 0 {
		nextCode := strings.Index(raw, "`")
		nextLink := strings.Index(raw, "[")
		start := nextInlineMarkdownStart(nextCode, nextLink)
		if start < 0 {
			segments = append(segments, markdownRenderSegment{text: raw})
			break
		}
		if start > 0 {
			segments = append(segments, markdownRenderSegment{text: raw[:start]})
			raw = raw[start:]
		}

		switch raw[0] {
		case '`':
			rest := raw[1:]
			end := strings.Index(rest, "`")
			if end < 0 {
				segments = append(segments, markdownRenderSegment{text: raw})
				return segments
			}
			code := rest[:end]
			if code != "" {
				segments = append(segments, markdownRenderSegment{text: code, style: m.styles.markdownInlineCode})
			}
			raw = rest[end+1:]
		case '[':
			labelEnd := strings.Index(raw, "](")
			if labelEnd < 0 {
				segments = append(segments, markdownRenderSegment{text: raw[:1]})
				raw = raw[1:]
				continue
			}
			urlStart := labelEnd + 2
			urlEnd := strings.Index(raw[urlStart:], ")")
			if urlEnd < 0 {
				segments = append(segments, markdownRenderSegment{text: raw[:1]})
				raw = raw[1:]
				continue
			}
			label := raw[1:labelEnd]
			url := raw[urlStart : urlStart+urlEnd]
			if label == "" || url == "" {
				segments = append(segments, markdownRenderSegment{text: raw[:1]})
				raw = raw[1:]
				continue
			}
			segments = append(segments, markdownRenderSegment{
				text:         label,
				style:        m.styles.markdownLinkText,
				keepTogether: true,
			})
			raw = raw[urlStart+urlEnd+1:]
		default:
			segments = append(segments, markdownRenderSegment{text: raw[:1]})
			raw = raw[1:]
		}
	}
	return segments
}

func (m modelUI) renderEditMetaTextLines(label, plainValue, focusedValue string, focusIndex int) []string {
	if m.form.focusIndex == focusIndex {
		return []string{m.renderDetailMetaLine(m.renderEditLabel(label, focusIndex), focusedValue)}
	}
	width := max(1, m.editFieldValueWidth())
	value := truncatePlain(strings.TrimSpace(plainValue), width)
	return m.renderDetailMetaLines(m.renderEditLabel(label, focusIndex), value, width+9)
}

func (m modelUI) renderEditMetaChoiceLine(label, currentValue, focusedValue string, focusIndex int) string {
	value := m.styles.muted.Render(currentValue)
	if m.form.focusIndex == focusIndex {
		value = focusedValue
	}
	return m.renderDetailMetaLine(m.renderEditLabel(label, focusIndex), value)
}

func (m modelUI) renderLabels(labels []string) []string {
	rendered := make([]string, 0, len(labels))
	for _, label := range labels {
		if label == "trashed" {
			rendered = append(rendered, m.styles.labelMuted.Render(label))
			continue
		}
		if itemType, ok := parseTypeLabel(label); ok {
			rendered = append(rendered, m.renderType(itemType))
			continue
		}
		if stage, ok := parseStageLabel(label); ok {
			rendered = append(rendered, m.renderStage(stage))
			continue
		}
		rendered = append(rendered, m.renderProjectLabel(label))
	}
	return rendered
}

func (m modelUI) renderIssueLine(item model.Item) string {
	if item.IssueNumber > 0 {
		return m.styles.muted.Render(fmt.Sprintf("Issue    #%d", item.IssueNumber))
	}
	if normalizeRepoRef(item.Repo) != "" {
		return m.styles.muted.Render("Issue    pending sync")
	}
	return m.styles.muted.Render("Issue    local-only")
}

func (m modelUI) renderItemRow(item model.Item, width int, selected bool) string {
	rowWidth := max(8, width-10)
	marker := "  "
	if selected {
		marker = m.styles.selected.Render("▍ ")
	}

	title := truncate(item.Title, rowWidth)
	if selected {
		title = m.styles.selected.Render(title)
	}

	dateText := relativeTimeLabel(time.Now(), item.UpdatedAt)
	typeText := m.renderTypeText(item.Type, false)
	stageText := m.renderStageText(item.Stage, false)
	typeWidth := lipgloss.Width(typeText)
	stageWidth := lipgloss.Width(stageText)
	dateWidth := lipgloss.Width(dateText)
	sep := "  "
	if m.listDensity == densityCompact {
		sep = " "
	}
	sepWidth := lipgloss.Width(sep) * 3
	projectWidth := max(4, rowWidth-typeWidth-stageWidth-dateWidth-sepWidth)
	projectText := truncatePlain(item.Project, projectWidth)

	metaRendered := lipgloss.JoinHorizontal(
		lipgloss.Left,
		m.renderProjectText(item.Project, projectText),
		sep,
		typeText,
		sep,
		stageText,
		sep,
		m.styles.muted.Render(dateText),
	)

	statusText := ""
	switch {
	case item.SyncConflict:
		statusText = m.styles.statusWarning.Render("⚠")
	case item.SyncError != "":
		statusText = m.styles.statusError.Render("✖")
	case item.HasPendingSync():
		statusText = m.styles.statusPending.Render("●")
	}
	if statusText != "" {
		metaRendered = lipgloss.JoinHorizontal(lipgloss.Left, metaRendered, sep, statusText)
	}

	return strings.Join([]string{marker + title, marker + metaRendered}, "\n")
}

func (m modelUI) panelStyle(base lipgloss.Style, width, height int) lipgloss.Style {
	return base.
		Width(max(1, width-base.GetHorizontalBorderSize()))
}

func (m modelUI) renderPaneContentWithScrollbar(content string, width, height int, panelStyle lipgloss.Style, scroll scrollState) string {
	innerWidth := max(1, width-panelStyle.GetHorizontalFrameSize())
	innerHeight := max(1, height-panelStyle.GetVerticalFrameSize())
	if scroll.total <= scroll.window || innerWidth < 4 {
		return m.renderContentBox(content, innerWidth, innerHeight)
	}

	contentWidth := max(1, innerWidth-2)
	return m.renderScrollJoinedContent(content, contentWidth, innerHeight, scroll)
}

func (m modelUI) renderPaneContent(content string, width, height int, panelStyle lipgloss.Style) string {
	innerWidth := max(1, width-panelStyle.GetHorizontalFrameSize())
	innerHeight := max(1, height-panelStyle.GetVerticalFrameSize())
	return m.renderContentBox(content, innerWidth, innerHeight)
}

func (m modelUI) renderScrollableContentBox(lines []string, innerWidth, innerHeight int, scroll scrollState) string {
	if innerWidth <= 0 || innerHeight <= 0 {
		return ""
	}
	if len(lines) == 0 {
		lines = []string{""}
	}

	offset := clamp(scroll.offset, 0, max(0, len(lines)-1))
	end := min(len(lines), offset+innerHeight)
	visible := strings.Join(lines[offset:end], "\n")

	if scroll.total <= scroll.window || innerWidth < 4 {
		return m.renderContentBox(visible, innerWidth, innerHeight)
	}

	contentWidth := max(1, innerWidth-2)
	return m.renderScrollJoinedContent(visible, contentWidth, innerHeight, scroll)
}

func (m modelUI) renderContentBox(content string, innerWidth, innerHeight int) string {
	fitted := fitContentBox(content, innerWidth, innerHeight)

	return lipgloss.NewStyle().
		Width(innerWidth).
		Height(innerHeight).
		MaxWidth(innerWidth).
		MaxHeight(innerHeight).
		AlignVertical(lipgloss.Top).
		Render(fitted)
}

func (m modelUI) renderScrollJoinedContent(content string, contentWidth, innerHeight int, scroll scrollState) string {
	contentLines := strings.Split(fitContentBox(content, contentWidth, innerHeight), "\n")
	scrollbarLines := strings.Split(m.renderScrollbar(innerHeight, scroll), "\n")
	joined := make([]string, 0, innerHeight)
	for idx := 0; idx < innerHeight; idx++ {
		line := ""
		if idx < len(contentLines) {
			line = contentLines[idx]
		}
		scrollbar := ""
		if idx < len(scrollbarLines) {
			scrollbar = scrollbarLines[idx]
		}
		joined = append(joined, line+" "+scrollbar)
	}
	return strings.Join(joined, "\n")
}

func (m modelUI) renderScrollbar(height int, scroll scrollState) string {
	lines := make([]string, height)
	for i := range lines {
		lines[i] = m.styles.scrollTrack.Render("│")
	}

	trackStart := clamp(scroll.topPad, 0, height)
	trackHeight := max(1, height-trackStart)
	if scroll.total <= scroll.window || trackHeight <= 0 {
		return strings.Join(lines, "\n")
	}

	thumbHeight := max(1, trackHeight*scroll.window/max(1, scroll.total))
	if thumbHeight > trackHeight {
		thumbHeight = trackHeight
	}

	maxOffset := max(1, scroll.total-scroll.window)
	thumbTop := trackStart
	if trackHeight > thumbHeight {
		thumbTop += (trackHeight - thumbHeight) * scroll.offset / maxOffset
	}

	for i := 0; i < thumbHeight && thumbTop+i < len(lines); i++ {
		lines[thumbTop+i] = m.styles.scrollThumb.Render("┃")
	}

	return strings.Join(lines, "\n")
}

func (m *modelUI) resizeEditors() {
	_, detailWidth := m.layoutWidths()
	contentHeight := m.mainContentHeight()
	detailInnerWidth := max(20, detailWidth-m.styles.panel.GetHorizontalFrameSize())
	detailInnerHeight := max(10, contentHeight-m.styles.panel.GetVerticalFrameSize())
	inputWidth := m.editFieldValueWidth()

	m.form.titleInput.Width = inputWidth
	m.form.projectInput.Width = inputWidth
	m.form.repoInput.Width = inputWidth
	m.form.bodyInput.SetWidth(max(20, detailInnerWidth))
	m.form.bodyInput.SetHeight(max(4, detailInnerHeight-7))

	setupWidth := max(24, min(40, m.width-20))
	m.setup.repoInput.Width = setupWidth
}

func (m modelUI) layoutWidths() (int, int) {
	total := max(60, m.appInnerWidth())
	listWidth := max(30, total*2/5)
	if total < 90 {
		listWidth = max(28, total*9/20)
	}
	if listWidth > total-28 {
		listWidth = total - 28
	}
	return listWidth, total - listWidth
}

func (m modelUI) detailPaneWidth() int {
	_, detail := m.layoutWidths()
	return detail
}

func (m modelUI) editFieldValueWidth() int {
	labelWidth := 10
	separatorWidth := 1
	return max(10, m.detailPaneWidth()-labelWidth-separatorWidth-6)
}

func (m modelUI) projectOptions() []string {
	seen := map[string]struct{}{}
	projects := []string{allProjectsLabel}
	for _, item := range m.items {
		if item.IsLocallyPurged() {
			continue
		}
		if _, ok := seen[item.Project]; ok {
			continue
		}
		seen[item.Project] = struct{}{}
		projects = append(projects, item.Project)
	}
	sort.Strings(projects[1:])
	return projects
}

func (m modelUI) runCommand(command string) (tea.Model, tea.Cmd) {
	if strings.TrimSpace(command) == "" {
		return m, nil
	}
	return m.runExtendedCommand(command)
}

func (m modelUI) runExtendedCommand(command string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return m, nil
	}

	switch strings.ToLower(parts[0]) {
	case "new":
		return m.enterEdit(-1)
	case "edit":
		if len(m.filtered) == 0 {
			return m.setStatus("No item selected."), nil
		}
		return m.enterEdit(m.filtered[m.selected])
	case "sync":
		return m.runSyncCommand()
	case "delete", "trash":
		return m.runDeleteCommand()
	case "restore":
		return m.runRestoreCommand()
	case "purge":
		return m.runPurgeCommand(), nil
	case "quit", "exit":
		return m, tea.Quit
	case "shortcuts", "help":
		m.shortcutsScroll = 0
		m.mode = modeShortcuts
		return m, nil
	case "repos":
		m.reposScroll = 0
		m.mode = modeRepos
		return m, nil
	case "drafts":
		return m.runDraftsCommand(strings.TrimSpace(command[len(parts[0]):])), nil
	case "open":
		return m.runOpenCommand()
	case "undo":
		return m.runUndoCommand(), nil
	case "search":
		return m.runSearchCommand(strings.TrimSpace(command[len(parts[0]):])), nil
	case "project":
		return m.runProjectCommand(strings.TrimSpace(command[len(parts[0]):])), nil
	case "stage":
		return m.runStageCommand(strings.TrimSpace(command[len(parts[0]):])), nil
	case "density":
		return m.runDensityCommand(strings.TrimSpace(command[len(parts[0]):])), nil
	case "project-label":
		return m.runProjectLabelCommand(strings.TrimSpace(command[len(parts[0]):])), nil
	case "project-repo":
		return m.runProjectRepoCommand(strings.TrimSpace(command[len(parts[0]):])), nil
	case "storage":
		return m.runStorageCommand(parts[1:])
	case "view":
		return m.runViewCommand(parts[1:]), nil
	case "sort":
		return m.runSortCommand(parts[1:]), nil
	case "export":
		return m.runExportCommand(strings.TrimSpace(command[len(parts[0]):])), nil
	case "import":
		return m.runImportCommand(strings.TrimSpace(command[len(parts[0]):])), nil
	default:
		return m.setStatusWarning(fmt.Sprintf("Unknown command: %s", command)), nil
	}
}

func (m modelUI) runSearchCommand(query string) tea.Model {
	query = strings.TrimSpace(query)
	if query == "" {
		return m.setStatusWarning("Usage: search <query> | search clear")
	}
	if strings.EqualFold(query, "clear") {
		m.lastSearch = ""
		m.rebuildFiltered()
		return m.setStatusInfo("Search cleared.")
	}

	m.lastSearch = query
	m.rebuildFiltered()
	return m.setStatusInfo(fmt.Sprintf("Search set to %q.", m.lastSearch))
}

func (m modelUI) runProjectCommand(project string) tea.Model {
	project = strings.TrimSpace(project)
	if project == "" {
		return m.setStatusWarning("Usage: project all | project <name>")
	}

	if strings.EqualFold(project, "all") {
		m.projectFilter = allProjectsLabel
		m.rebuildFiltered()
		return m.setStatusInfo("Project set to all.")
	}

	for _, option := range m.projectOptions() {
		if option == allProjectsLabel {
			continue
		}
		if strings.EqualFold(option, project) {
			m.projectFilter = option
			m.rebuildFiltered()
			return m.setStatusInfo(fmt.Sprintf("Project set to %s.", option))
		}
	}

	return m.setStatusWarning(fmt.Sprintf("Unknown project: %s", project))
}

func (m modelUI) runOpenCommand() (tea.Model, tea.Cmd) {
	itemIndex, ok := m.selectedItemIndex()
	if !ok {
		return m.setStatusWarning("No item selected."), nil
	}
	url, ok := githubIssueURL(m.items[itemIndex])
	if !ok {
		return m.setStatusWarning("Selected item is not on GitHub yet."), nil
	}
	return m.setStatusLoading("Opening issue on GitHub..."), openURLCmd(url)
}

func (m modelUI) runUndoCommand() tea.Model {
	if m.undo == nil {
		return m.setStatusInfo("Nothing to undo")
	}

	snapshot := m.undo
	m.items = cloneItems(snapshot.items)
	m.undo = nil
	if err := m.persistItems(); err != nil {
		return m.setStatusError(fmt.Sprintf("Undo failed: %v", err))
	}
	m.detailScroll = 0
	m.rebuildFiltered()
	m.restoreUndoSelection(snapshot.selected)
	return m.setStatusSuccess(fmt.Sprintf("Undid %s", snapshot.label))
}

func (m modelUI) runStageCommand(stage string) tea.Model {
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return m.setStatusWarning("Usage: stage all | stage <idea|planned|active|blocked|done>")
	}

	if strings.EqualFold(stage, "all") {
		m.stageFilter = allStagesLabel
		m.rebuildFiltered()
		return m.setStatusInfo("Stage filter cleared.")
	}

	for _, option := range model.Stages {
		if strings.EqualFold(string(option), stage) {
			m.stageFilter = string(option)
			m.rebuildFiltered()
			return m.setStatusInfo(fmt.Sprintf("Stage filter set to %s.", option))
		}
	}

	return m.setStatusWarning("Usage: stage all | stage <idea|planned|active|blocked|done>")
}

func (m modelUI) runDensityCommand(value string) tea.Model {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return m.setStatusWarning("Usage: density comfortable | density compact")
	}

	var density listDensity
	switch value {
	case "comfortable":
		density = densityComfortable
	case "compact":
		density = densityCompact
	default:
		return m.setStatusWarning("Usage: density comfortable | density compact")
	}

	cfg := m.config
	cfg.Density = density.String()
	if err := m.saveConfigAndApply(cfg); err != nil {
		return m.setStatusError(fmt.Sprintf("Density change failed: %v", err))
	}

	m.rebuildFiltered()
	return m.setStatusInfo(fmt.Sprintf("Density set to %s.", density.String()))
}

func (m modelUI) runProjectLabelCommand(value string) tea.Model {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return m.setStatusWarning("Usage: project-label always | project-label auto | project-label never")
	}

	switch value {
	case config.ProjectLabelAlways, config.ProjectLabelAuto, config.ProjectLabelNever:
	default:
		return m.setStatusWarning("Usage: project-label always | project-label auto | project-label never")
	}

	cfg := m.config
	cfg.ProjectLabelSync = value
	if err := m.saveConfigAndApply(cfg); err != nil {
		return m.setStatusError(fmt.Sprintf("Project label setting failed: %v", err))
	}

	return m.setStatusInfo(fmt.Sprintf("Project label sync set to %s.", value))
}

func (m modelUI) runProjectRepoCommand(args string) tea.Model {
	args = strings.TrimSpace(args)
	if args == "" {
		return m.setStatusWarning("Usage: project-repo <project> <owner/repo> | project-repo clear <project>")
	}

	if strings.HasPrefix(strings.ToLower(args), "clear ") {
		project := strings.TrimSpace(args[len("clear"):])
		if project == "" {
			return m.setStatusWarning("Usage: project-repo clear <project>")
		}
		key := normalizeProjectKey(project)
		cfg := m.config
		if len(cfg.ProjectRepos) == 0 || cfg.ProjectRepos[key] == "" {
			return m.setStatusWarning(fmt.Sprintf("No repo mapping set for %s.", project))
		}
		cfg.ProjectRepos = cloneProjectRepos(cfg.ProjectRepos)
		delete(cfg.ProjectRepos, key)
		if err := m.saveConfigAndApply(cfg); err != nil {
			return m.setStatusError(fmt.Sprintf("Project repo change failed: %v", err))
		}
		return m.setStatusInfo(fmt.Sprintf("Cleared repo mapping for %s.", project))
	}

	parts := strings.Fields(args)
	if len(parts) < 2 {
		return m.setStatusWarning("Usage: project-repo <project> <owner/repo>")
	}
	repo := normalizeRepoRef(parts[len(parts)-1])
	if !validRepoRef(repo) {
		return m.setStatusWarning("Repository must be in owner/repo form.")
	}
	project := strings.TrimSpace(strings.TrimSuffix(args, parts[len(parts)-1]))
	if project == "" {
		return m.setStatusWarning("Usage: project-repo <project> <owner/repo>")
	}

	cfg := m.config
	cfg.ProjectRepos = cloneProjectRepos(cfg.ProjectRepos)
	cfg.ProjectRepos[normalizeProjectKey(project)] = repo
	if err := m.saveConfigAndApply(cfg); err != nil {
		return m.setStatusError(fmt.Sprintf("Project repo change failed: %v", err))
	}
	return m.setStatusInfo(fmt.Sprintf("Mapped %s to %s.", project, repo))
}

func (m modelUI) runViewCommand(args []string) tea.Model {
	if len(args) == 0 {
		return m.setStatusWarning("Usage: view all | view archive | view trash")
	}

	switch args[0] {
	case "all", "active":
		m.viewMode = viewActive
		m.rebuildFiltered()
		return m.setStatusInfo("Switched to all items.")
	case "archive":
		m.viewMode = viewArchive
		m.rebuildFiltered()
		return m.setStatusInfo("Switched to archive.")
	case "trash":
		m.viewMode = viewTrash
		m.rebuildFiltered()
		return m.setStatusInfo("Switched to trash.")
	default:
		return m.setStatusWarning("Usage: view all | view archive | view trash")
	}
}

func (m modelUI) runSyncCommand() (tea.Model, tea.Cmd) {
	if m.config.StorageMode != config.ModeGitHub {
		return m.setStatusWarning("Local mode is already current."), nil
	}
	if len(m.pendingSyncItems()) == 0 {
		return m.beginSync()
	}
	m.mode = modeConfirm
	m.detailScroll = 0
	m.confirm = &confirmState{action: confirmSync}
	return m, nil
}

func (m modelUI) runDeleteCommand() (tea.Model, tea.Cmd) {
	itemIndex, ok := m.selectedItemIndex()
	if !ok {
		return m.setStatusWarning("No item selected."), nil
	}

	item := m.items[itemIndex]
	if item.IsTrashed() {
		return m.setStatusWarning("Item is already in trash."), nil
	}

	updated := item
	updated.Trashed = true
	updated.UpdatedAt = time.Now()
	updated.SyncConflict = false
	updated.SyncError = ""

	if m.config.StorageMode == config.ModeGitHub {
		m = m.captureUndo("trash")
		if item.PendingSync == model.SyncCreate || (item.IssueNumber == 0 && item.RemoteRepo() == "") {
			updated.PendingSync = model.SyncNone
		} else {
			updated.PendingSync = model.SyncDelete
		}
		m.items[itemIndex] = updated
		if err := m.persistItems(); err != nil {
			return m.setStatusError(fmt.Sprintf("Delete failed: %v", err)), nil
		}
		m.detailScroll = 0
		m.rebuildFiltered()
		return m.setStatusSuccess("Moved item to trash locally. Press S to sync."), nil
	}

	m = m.captureUndo("trash")
	m.items[itemIndex] = updated
	if err := m.persistItems(); err != nil {
		return m.setStatusError(fmt.Sprintf("Delete failed: %v", err)), nil
	}

	m.detailScroll = 0
	m.rebuildFiltered()
	return m.setStatusSuccess("Moved item to trash."), nil
}

func (m modelUI) runRestoreCommand() (tea.Model, tea.Cmd) {
	itemIndex, ok := m.selectedItemIndex()
	if !ok {
		return m.setStatusWarning("No item selected."), nil
	}

	item := m.items[itemIndex]
	if !item.IsTrashed() {
		return m.setStatusWarning("Selected item is not in trash."), nil
	}

	updated := item
	updated.Trashed = false
	updated.UpdatedAt = time.Now()
	updated.SyncConflict = false
	updated.SyncError = ""

	if m.config.StorageMode == config.ModeGitHub {
		m = m.captureUndo("restore")
		if item.PendingSync == model.SyncCreate || (item.IssueNumber == 0 && item.RemoteRepo() == "") {
			updated.PendingSync = model.SyncCreate
		} else {
			updated.PendingSync = model.SyncRestore
		}
		m.items[itemIndex] = updated
		if err := m.persistItems(); err != nil {
			return m.setStatusError(fmt.Sprintf("Restore failed: %v", err)), nil
		}
		m.detailScroll = 0
		m.rebuildFiltered()
		if updated.IsDone() {
			return m.setStatusSuccess("Restored item locally to archive. Press S to sync."), nil
		}
		return m.setStatusSuccess("Restored item locally. Press S to sync."), nil
	}

	m = m.captureUndo("restore")
	m.items[itemIndex] = updated
	if err := m.persistItems(); err != nil {
		return m.setStatusError(fmt.Sprintf("Restore failed: %v", err)), nil
	}

	m.detailScroll = 0
	m.rebuildFiltered()
	if updated.IsDone() {
		return m.setStatusSuccess("Restored item to archive."), nil
	}
	return m.setStatusSuccess("Restored item."), nil
}

func (m modelUI) runPurgeCommand() tea.Model {
	itemIndex, ok := m.selectedItemIndex()
	if !ok {
		return m.setStatusWarning("No item selected.")
	}
	if !m.items[itemIndex].IsTrashed() {
		return m.setStatusWarning("Purge is only available in trash.")
	}

	m.mode = modeConfirm
	m.detailScroll = 0
	m.confirm = &confirmState{
		action:    confirmPurge,
		itemIndex: itemIndex,
	}
	return m
}

func (m modelUI) runSortCommand(args []string) tea.Model {
	if len(args) == 0 {
		return m.setStatusWarning("Usage: sort updated|created asc|desc")
	}

	switch args[0] {
	case "updated":
		m.sortMode = sortUpdated
	case "created":
		m.sortMode = sortCreated
	default:
		return m.setStatusWarning("Usage: sort updated|created asc|desc")
	}

	m.sortAscending = false
	if len(args) > 1 {
		switch args[1] {
		case "asc":
			m.sortAscending = true
		case "desc":
			m.sortAscending = false
		default:
			return m.setStatusWarning("Usage: sort updated|created asc|desc")
		}
	}

	m.rebuildFiltered()
	return m.setStatusInfo(fmt.Sprintf("Sorting by %s %s.", m.sortMode.String(), m.sortDirectionLabel()))
}

func (m modelUI) runExportCommand(args string) tea.Model {
	if m.config.StorageMode != config.ModeLocal {
		return m.setStatusWarning("Export is only available in local mode.")
	}

	args = strings.TrimSpace(args)
	if args == "" {
		return m.setStatusWarning("Usage: export json <path>")
	}

	parts := strings.Fields(args)
	if len(parts) == 0 || !strings.EqualFold(parts[0], "json") {
		return m.setStatusWarning("Usage: export json <path>")
	}

	path := strings.TrimSpace(args[len(parts[0]):])
	if path == "" {
		return m.setStatusWarning("Usage: export json <path>")
	}

	payload, err := json.MarshalIndent(m.items, "", "  ")
	if err != nil {
		return m.setStatusError(fmt.Sprintf("Export failed: %v", err))
	}

	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		return m.setStatusError(fmt.Sprintf("Export failed: %v", err))
	}

	return m.setStatusSuccess(fmt.Sprintf("Exported %d items to %s.", len(m.items), path))
}

func (m modelUI) runImportCommand(args string) tea.Model {
	if m.config.StorageMode != config.ModeLocal {
		return m.setStatusWarning("Import is only available in local mode.")
	}

	args = strings.TrimSpace(args)
	if args == "" {
		return m.setStatusWarning("Usage: import json <path>")
	}

	parts := strings.Fields(args)
	if len(parts) == 0 || !strings.EqualFold(parts[0], "json") {
		return m.setStatusWarning("Usage: import json <path>")
	}

	path := strings.TrimSpace(args[len(parts[0]):])
	if path == "" {
		return m.setStatusWarning("Usage: import json <path>")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return m.setStatusError(fmt.Sprintf("Import failed: %v", err))
	}

	var items []model.Item
	if err := json.Unmarshal(data, &items); err != nil {
		return m.setStatusError(fmt.Sprintf("Import failed: %v", err))
	}

	normalized, err := normalizeImportedItems(items)
	if err != nil {
		return m.setStatusError(fmt.Sprintf("Import failed: %v", err))
	}

	m.mode = modeConfirm
	m.detailScroll = 0
	m.confirm = &confirmState{
		action:      confirmImport,
		importPath:  path,
		importItems: normalized,
	}
	return m
}

func (m modelUI) runDraftsCommand(args string) tea.Model {
	args = strings.TrimSpace(args)
	switch {
	case args == "":
		return m.runDraftsScan(true)
	case strings.EqualFold(args, "show"):
		return m.setStatusInfo(fmt.Sprintf("Drafts folder: %s", m.config.DraftsFolder))
	case strings.EqualFold(args, "reset"):
		folder, err := config.DefaultDraftsFolder()
		if err != nil {
			return m.setStatusError(fmt.Sprintf("Drafts folder reset failed: %v", err))
		}
		cfg := m.config
		cfg.DraftsFolder = folder
		if err := m.saveConfigOnly(cfg); err != nil {
			return m.setStatusError(fmt.Sprintf("Drafts folder reset failed: %v", err))
		}
		return m.setStatusSuccess(fmt.Sprintf("Drafts folder reset to %s", m.config.DraftsFolder))
	case strings.HasPrefix(strings.ToLower(args), "folder "):
		folder := strings.TrimSpace(args[len("folder"):])
		if folder == "" {
			return m.setStatusWarning("Usage: drafts | drafts show | drafts reset | drafts folder <path>")
		}
		cfg := m.config
		cfg.DraftsFolder = folder
		if err := m.saveConfigOnly(cfg); err != nil {
			return m.setStatusError(fmt.Sprintf("Drafts folder change failed: %v", err))
		}
		return m.setStatusSuccess(fmt.Sprintf("Drafts folder set to %s", m.config.DraftsFolder))
	default:
		return m.setStatusWarning("Usage: drafts | drafts show | drafts reset | drafts folder <path>")
	}
}

func (m modelUI) runDraftsScan(captureUndo bool) tea.Model {
	imported, failed, err := m.importDrafts(captureUndo)
	if err != nil {
		return m.setStatusError(fmt.Sprintf("Draft scan failed: %v", err))
	}
	switch {
	case imported == 0 && failed == 0:
		return m.setStatusInfo("No drafts found")
	case imported > 0 && failed > 0:
		return m.setStatusWarning(fmt.Sprintf("Imported %d drafts, %d failed", imported, failed))
	case failed > 0:
		return m.setStatusWarning(fmt.Sprintf("%d drafts failed to import", failed))
	default:
		return m.setStatusSuccess(fmt.Sprintf("Imported %d drafts", imported))
	}
}

type draftImportCandidate struct {
	item          model.Item
	sourcePath    string
	processedPath string
}

func (m *modelUI) importDrafts(captureUndo bool) (int, int, error) {
	folder := strings.TrimSpace(m.config.DraftsFolder)
	if folder == "" {
		defaultFolder, err := config.DefaultDraftsFolder()
		if err != nil {
			return 0, 0, err
		}
		folder = defaultFolder
	}

	candidates, failed, err := m.collectDraftCandidates(folder)
	if err != nil {
		return 0, failed, err
	}
	if len(candidates) == 0 {
		return 0, failed, nil
	}

	if captureUndo {
		*m = m.captureUndo("draft import")
	}

	originalItems := cloneItems(m.items)
	importedItems := make([]model.Item, 0, len(candidates))
	for _, candidate := range candidates {
		importedItems = append(importedItems, candidate.item)
	}
	m.items = append(importedItems, m.items...)
	if err := m.persistItems(); err != nil {
		m.items = originalItems
		for _, candidate := range candidates {
			_ = os.Rename(candidate.processedPath, candidate.sourcePath)
		}
		return 0, failed, err
	}

	m.detailScroll = 0
	m.rebuildFiltered()
	return len(candidates), failed, nil
}

func (m modelUI) collectDraftCandidates(folder string) ([]draftImportCandidate, int, error) {
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return nil, 0, err
	}

	processedDir := filepath.Join(folder, "processed")
	if err := os.MkdirAll(processedDir, 0o700); err != nil {
		return nil, 0, err
	}

	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, 0, err
	}

	now := time.Now().UTC()
	candidates := make([]draftImportCandidate, 0, len(entries))
	failed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		sourcePath := filepath.Join(folder, entry.Name())
		raw, err := os.ReadFile(sourcePath)
		if err != nil {
			failed++
			continue
		}

		meta, body, err := githubsync.ParseDraft(string(raw))
		if err != nil {
			failed++
			continue
		}

		repo := normalizeRepoRef(meta.Repo)
		if m.config.StorageMode == config.ModeGitHub && repo == "" {
			repo = m.defaultRepoForProject(meta.Project)
		}
		if repo != "" && !validRepoRef(repo) {
			failed++
			continue
		}
		if m.config.StorageMode == config.ModeGitHub && repo == "" {
			failed++
			continue
		}

		item := model.Item{
			Title:     strings.TrimSpace(meta.Title),
			Project:   strings.TrimSpace(meta.Project),
			Type:      normalizeType(meta.Type),
			Stage:     meta.Stage,
			Body:      body,
			CreatedAt: now,
			UpdatedAt: now,
			Repo:      repo,
		}
		if m.config.StorageMode == config.ModeGitHub {
			item.PendingSync = model.SyncCreate
		}

		processedPath := nextProcessedDraftPath(processedDir, entry.Name())
		if err := os.Rename(sourcePath, processedPath); err != nil {
			failed++
			continue
		}

		candidates = append(candidates, draftImportCandidate{
			item:          item,
			sourcePath:    sourcePath,
			processedPath: processedPath,
		})
	}

	return candidates, failed, nil
}

func nextProcessedDraftPath(processedDir, name string) string {
	target := filepath.Join(processedDir, name)
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return target
	}

	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for idx := 2; ; idx++ {
		candidate := filepath.Join(processedDir, fmt.Sprintf("%s-%d%s", base, idx, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func (m modelUI) runStorageCommand(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		return m.setStatusWarning("Usage: storage local | storage github owner/repo"), nil
	}

	switch args[0] {
	case "local":
		cfg := m.config
		cfg.StorageMode = config.ModeLocal
		cfg.Repo = ""
		cfg.Density = m.listDensity.String()
		if err := m.saveConfigAndApply(cfg); err != nil {
			return m.setStatusError(fmt.Sprintf("Storage switch failed: %v", err)), nil
		}
		return m.setStatusSuccess("Switched to local storage."), nil
	case "github":
		if len(args) < 2 {
			return m.setStatusWarning("Usage: storage github owner/repo"), nil
		}
		repo := strings.TrimSpace(args[1])
		if !validRepoRef(repo) {
			return m.setStatusWarning("Repository must be in owner/repo form."), nil
		}

		cfg := m.config
		cfg.StorageMode = config.ModeGitHub
		cfg.Repo = repo
		cfg.TrackedRepos = append(cfg.TrackedRepos, repo)
		cfg.Density = m.listDensity.String()
		if err := m.saveConfigAndApply(cfg); err != nil {
			return m.setStatusError(fmt.Sprintf("Storage switch failed: %v", err)), nil
		}
		return m.beginSync()
	default:
		return m.setStatusWarning("Usage: storage local | storage github owner/repo"), nil
	}
}

func (m *modelUI) applyConfig(cfg config.AppConfig) {
	cfg = config.Normalize(cfg)
	m.config = cfg
	m.store = storage.NewJSONStore(cfg.DataFile)
	m.githubClient = githubsync.NewClient()
	m.githubClient.SetProjectLabelSync(cfg.ProjectLabelSync)
	m.listDensity = parseDensity(cfg.Density)
	if m.mode == modeSetup {
		m.mode = modeNormal
	}
}

func (m *modelUI) recordSuccessfulSync(now time.Time) error {
	cfg := m.config
	cfg.LastSuccessfulSyncAt = now.UTC()
	if m.configManager == nil {
		m.applyConfig(cfg)
		return nil
	}
	return m.saveConfigOnly(cfg)
}

func (m *modelUI) loadItems() error {
	if m.store == nil {
		return nil
	}

	items, ok, err := m.store.LoadItems()
	if err != nil {
		return err
	}
	if !ok {
		items = []model.Item{}
	} else {
		items, err = normalizeImportedItems(items)
		if err != nil {
			return err
		}
	}

	m.items = items
	if err := m.reconcileTrackedRepos(items); err != nil {
		return err
	}
	if m.config.StorageMode != config.ModeGitHub || m.githubClient == nil {
		if !ok {
			return m.persistItems()
		}
		return nil
	}
	if !ok {
		return m.persistItems()
	}
	return nil
}

func (m modelUI) persistItems() error {
	if m.store == nil {
		return fmt.Errorf("store is not configured")
	}
	itemsSnapshot, err := snapshotFile(m.store.Path())
	if err != nil {
		return err
	}
	if err := m.store.SaveItems(m.items); err != nil {
		return err
	}
	if err := m.reconcileTrackedRepos(m.items); err != nil {
		if rollbackErr := restoreFileSnapshot(itemsSnapshot); rollbackErr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", err, rollbackErr)
		}
		return err
	}
	return nil
}

func (m modelUI) buildEditedItem(title, project, repo, body string, itemType model.Type, stage model.Stage, now time.Time) model.Item {
	if m.form.isNew {
		return model.Item{
			Title:           title,
			Project:         project,
			Type:            itemType,
			Stage:           stage,
			Body:            body,
			CreatedAt:       now,
			UpdatedAt:       now,
			IssueNumber:     0,
			Repo:            repo,
			SyncedRepo:      "",
			RemoteUpdatedAt: time.Time{},
		}
	}

	item := m.items[m.form.editingIndex]
	item.Title = title
	item.Project = project
	item.Type = itemType
	if item.SyncedRepo == "" && item.IssueNumber > 0 {
		item.SyncedRepo = normalizeRepoRef(item.Repo)
	}
	item.Repo = repo
	item.Stage = stage
	item.Body = body
	item.UpdatedAt = now
	return item
}

func pendingSyncForEdit(item model.Item, previous *model.Item, isNew bool) model.SyncOperation {
	if isNew || (item.IssueNumber == 0 && item.RemoteRepo() == "") {
		return model.SyncCreate
	}
	if previous != nil && previous.PendingSync == model.SyncCreate {
		return model.SyncCreate
	}
	if item.Trashed {
		return model.SyncDelete
	}
	if previous != nil && previous.Trashed {
		return model.SyncRestore
	}
	return model.SyncUpdate
}

func saveItemCmd(client *githubsync.Client, repo string, item model.Item, previous *model.Item, isNew bool, editingIndex int) tea.Cmd {
	return func() tea.Msg {
		saved, warning, err := remoteSaveEditedItem(client, repo, item, previous)
		return saveResultMsg{
			local:        item,
			saved:        saved,
			warning:      warning,
			err:          err,
			isNew:        isNew,
			editingIndex: editingIndex,
		}
	}
}

func conflictOverwriteCmd(client *githubsync.Client, repo string, item model.Item) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return conflictOverwriteResultMsg{err: fmt.Errorf("github client is not configured")}
		}
		saved, warning, err := client.ForceUpsertItem(repo, item)
		return conflictOverwriteResultMsg{
			saved:   saved,
			warning: warning,
			err:     err,
		}
	}
}

func batchSyncCmd(client *githubsync.Client, repos []string, items []model.Item) tea.Cmd {
	return func() tea.Msg {
		working := append([]model.Item(nil), items...)
		results := make([]syncPushResult, 0)
		for idx, item := range working {
			if !item.HasPendingSync() {
				continue
			}
			saved, removed, warning, err := syncPendingItem(client, item)
			results = append(results, syncPushResult{
				index:   idx,
				local:   item,
				item:    saved,
				removed: removed,
				warning: warning,
				err:     err,
			})
		}

		applied := applySyncPushResults(working, results)
		remoteItems, err := syncRepos(client, repos)
		if err != nil {
			return batchSyncResultMsg{
				items:   applied,
				repos:   repos,
				results: results,
				err:     err,
			}
		}

		return batchSyncResultMsg{
			items:   mergeSyncedItems(applied, remoteItems, repos),
			repos:   repos,
			results: results,
		}
	}
}

func syncPendingItem(client *githubsync.Client, item model.Item) (model.Item, bool, string, error) {
	if client == nil {
		return model.Item{}, false, "", fmt.Errorf("github client is not configured")
	}

	desiredRepo := normalizeRepoRef(item.Repo)
	sourceRepo := normalizeRepoRef(item.RemoteRepo())
	clean := item
	clean.PendingSync = model.SyncNone
	clean.SyncConflict = false
	clean.SyncError = ""

	switch item.PendingSync {
	case model.SyncCreate:
		clean.IssueNumber = 0
		clean.RemoteUpdatedAt = time.Time{}
		clean.SyncedRepo = ""
		saved, warning, err := client.UpsertItem(desiredRepo, clean)
		return saved, false, warning, err
	case model.SyncUpdate, model.SyncDelete, model.SyncRestore:
		if item.IssueNumber > 0 && sourceRepo != "" && sourceRepo != desiredRepo {
			create := clean
			create.IssueNumber = 0
			create.RemoteUpdatedAt = time.Time{}
			create.SyncedRepo = ""
			saved, warning, err := client.UpsertItem(desiredRepo, create)
			if err != nil {
				return model.Item{}, false, "", err
			}
			if err := client.DeleteIssue(sourceRepo, item.IssueNumber); err != nil {
				warning = joinWarnings(warning, fmt.Sprintf("Moved item to %s, but could not delete the old issue in %s.", desiredRepo, sourceRepo))
			}
			return saved, false, warning, nil
		}
		clean.SyncedRepo = sourceRepo
		saved, warning, err := client.UpsertItem(desiredRepo, clean)
		return saved, false, warning, err
	case model.SyncPurge:
		if item.IssueNumber == 0 || sourceRepo == "" {
			return model.Item{}, true, "", nil
		}
		err := client.DeleteIssue(sourceRepo, item.IssueNumber)
		return model.Item{}, true, "", err
	default:
		return item, false, "", nil
	}
}

func applySyncPushResults(items []model.Item, results []syncPushResult) []model.Item {
	updated := append([]model.Item(nil), items...)
	remove := make(map[int]struct{})
	for _, result := range results {
		if result.index < 0 || result.index >= len(updated) {
			continue
		}
		current := updated[result.index]
		if result.err != nil {
			var conflictErr *githubsync.ConflictError
			if errors.As(result.err, &conflictErr) {
				current.SyncConflict = true
				current.SyncError = ""
			} else {
				current.SyncConflict = false
				current.SyncError = strings.TrimSpace(githubsync.UserMessage(result.err))
				if current.SyncError == "" {
					current.SyncError = result.err.Error()
				}
			}
			updated[result.index] = current
			continue
		}
		if result.removed {
			remove[result.index] = struct{}{}
			continue
		}
		saved := result.item
		saved.PendingSync = model.SyncNone
		saved.SyncConflict = false
		saved.SyncError = ""
		if saved.SyncedRepo == "" {
			saved.SyncedRepo = normalizeRepoRef(saved.Repo)
		}
		updated[result.index] = saved
	}

	finalItems := make([]model.Item, 0, len(updated))
	for idx, item := range updated {
		if _, ok := remove[idx]; ok {
			continue
		}
		finalItems = append(finalItems, item)
	}
	return finalItems
}

func remoteSaveEditedItem(client *githubsync.Client, repo string, item model.Item, previous *model.Item) (model.Item, string, error) {
	if client == nil {
		return model.Item{}, "", fmt.Errorf("github client is not configured")
	}
	if repo == "" {
		return model.Item{}, "", fmt.Errorf("github repo is not configured")
	}

	var moveWarning string
	saved, warning, err := client.UpsertItem(repo, item)
	if err != nil {
		return model.Item{}, "", err
	}
	moveWarning = joinWarnings(moveWarning, warning)

	if previous != nil {
		oldRepo := normalizeRepoRef(previous.Repo)
		if previous.IssueNumber > 0 && oldRepo != "" && oldRepo != repo {
			if err := client.DeleteIssue(oldRepo, previous.IssueNumber); err != nil {
				moveWarning = joinWarnings(moveWarning, fmt.Sprintf("Moved item to %s, but could not delete the old issue in %s.", repo, oldRepo))
			}
		}
	}
	return saved, moveWarning, nil
}

func itemActionCmd(kind itemActionKind, client *githubsync.Client, repo string, item model.Item, previous *model.Item, itemIndex int) tea.Cmd {
	return func() tea.Msg {
		switch kind {
		case actionDelete, actionRestore:
			saved, warning, err := remoteSaveEditedItem(client, repo, item, previous)
			return itemActionResultMsg{
				kind:      kind,
				itemIndex: itemIndex,
				saved:     saved,
				warning:   warning,
				err:       err,
			}
		case actionPurge:
			var err error
			if client != nil {
				err = client.DeleteIssue(repo, item.IssueNumber)
			}
			return itemActionResultMsg{
				kind:      kind,
				itemIndex: itemIndex,
				err:       err,
			}
		default:
			return itemActionResultMsg{
				kind:      kind,
				itemIndex: itemIndex,
				err:       fmt.Errorf("unknown action"),
			}
		}
	}
}

func (m modelUI) finishSave(msg saveResultMsg) tea.Model {
	m.saveInFlight = false

	if msg.err != nil {
		var conflictErr *githubsync.ConflictError
		if errors.As(msg.err, &conflictErr) {
			return m.enterConflict(conflictErr, msg.local)
		}
		return m.setStatusError(userFacingError("GitHub save failed", msg.err))
	}

	repo := m.resolvedItemRepo(msg.saved)
	if err := m.trackRepo(repo); err != nil {
		return m.setStatusError(fmt.Sprintf("Save failed: %v", err))
	}

	if msg.isNew {
		m.items = append([]model.Item{msg.saved}, m.items...)
	} else if msg.editingIndex >= 0 && msg.editingIndex < len(m.items) {
		m.items[msg.editingIndex] = msg.saved
	}

	if err := m.persistItems(); err != nil {
		return m.setStatusError(fmt.Sprintf("Save failed: %v", err))
	}

	m.mode = modeNormal
	m.conflict = nil
	m.detailScroll = 0
	m.rebuildFiltered()
	m.selectItem(msg.saved)

	if msg.warning != "" {
		return m.setStatusWarning(msg.warning)
	}
	if msg.isNew {
		return m.setStatusSuccess("Item created.")
	}
	return m.setStatusSuccess("Item updated.")
}

func (m modelUI) finishOpenURL(msg openURLResultMsg) tea.Model {
	if msg.err != nil {
		return m.setStatusError(userFacingError("Open failed", msg.err))
	}
	return m.setStatusSuccess("Opened issue on GitHub.")
}

func (m modelUI) finishConflictOverwrite(msg conflictOverwriteResultMsg) tea.Model {
	m.saveInFlight = false

	if msg.err != nil {
		return m.setStatusError(userFacingError("Overwrite failed", msg.err))
	}
	if m.conflict == nil {
		return m.setStatusSuccess("Overwrote GitHub with the local version.")
	}

	repo := m.resolvedItemRepo(msg.saved)
	if err := m.trackRepo(repo); err != nil {
		return m.setStatusError(fmt.Sprintf("Overwrite failed: %v", err))
	}

	if m.conflict.isNew {
		m.items = append([]model.Item{msg.saved}, m.items...)
	} else if m.conflict.editingIndex >= 0 && m.conflict.editingIndex < len(m.items) {
		m.items[m.conflict.editingIndex] = msg.saved
	}

	if err := m.persistItems(); err != nil {
		return m.setStatusError(fmt.Sprintf("Conflict save failed: %v", err))
	}

	m.mode = modeNormal
	m.conflict = nil
	m.detailScroll = 0
	m.rebuildFiltered()
	m.selectItem(msg.saved)
	if msg.warning != "" {
		return m.setStatusWarning(msg.warning)
	}
	return m.setStatusSuccess("Overwrote GitHub with the local version.")
}

func (m modelUI) finishBatchSync(msg batchSyncResultMsg) tea.Model {
	m.actionInFlight = false
	m.items = msg.items
	if err := m.persistItems(); err != nil {
		return m.setStatusError(fmt.Sprintf("Sync save failed: %v", err))
	}
	m.detailScroll = 0
	m.rebuildFiltered()

	syncedCount := 0
	conflictCount := 0
	failedCount := 0
	warnings := []string{}
	var firstConflict *githubsync.ConflictError
	var firstConflictLocal model.Item
	for _, result := range msg.results {
		if result.warning != "" {
			warnings = append(warnings, result.warning)
		}
		if result.err != nil {
			var conflictErr *githubsync.ConflictError
			if errors.As(result.err, &conflictErr) {
				conflictCount++
				if firstConflict == nil {
					firstConflict = conflictErr
					firstConflictLocal = result.local
				}
			} else {
				failedCount++
			}
			continue
		}
		syncedCount++
	}

	if msg.err != nil {
		return m.setStatusError(userFacingError("Refresh failed after sync", msg.err))
	}
	syncMetadataErr := m.recordSuccessfulSync(time.Now())
	if firstConflict != nil {
		return m.enterConflict(firstConflict, firstConflictLocal)
	}
	if syncMetadataErr != nil {
		warnings = append(warnings, fmt.Sprintf("Failed to record last sync time: %v", syncMetadataErr))
	}
	if failedCount > 0 {
		return m.setStatusWarning(fmt.Sprintf("Synced %d changes, %d conflicted, %d failed.", syncedCount, conflictCount, failedCount))
	}
	if conflictCount > 0 {
		return m.setStatusWarning(fmt.Sprintf("Synced %d changes. %d items need conflict review.", syncedCount, conflictCount))
	}
	if len(warnings) > 0 {
		return m.setStatusWarning(joinWarnings(warnings...))
	}
	return m.setStatusSuccess(fmt.Sprintf("Synced %d local changes to GitHub.", syncedCount))
}

func (m modelUI) finishItemAction(msg itemActionResultMsg) tea.Model {
	m.actionInFlight = false

	if msg.err != nil {
		switch msg.kind {
		case actionDelete:
			return m.setStatusError(userFacingError("Delete failed", msg.err))
		case actionRestore:
			return m.setStatusError(userFacingError("Restore failed", msg.err))
		case actionPurge:
			return m.setStatusError(userFacingError("Purge failed", msg.err))
		default:
			return m.setStatusError(fmt.Sprintf("Action failed: %v", msg.err))
		}
	}

	switch msg.kind {
	case actionDelete, actionRestore:
		if msg.itemIndex < 0 || msg.itemIndex >= len(m.items) {
			return m.setStatusWarning("Item no longer exists.")
		}
		repo := m.resolvedItemRepo(msg.saved)
		if err := m.trackRepo(repo); err != nil {
			label := "Delete"
			if msg.kind == actionRestore {
				label = "Restore"
			}
			return m.setStatusError(fmt.Sprintf("%s failed: %v", label, err))
		}
		m.items[msg.itemIndex] = msg.saved
		if err := m.persistItems(); err != nil {
			label := "Delete"
			if msg.kind == actionRestore {
				label = "Restore"
			}
			return m.setStatusError(fmt.Sprintf("%s failed: %v", label, err))
		}
		m.detailScroll = 0
		m.rebuildFiltered()
		if msg.warning != "" {
			return m.setStatusWarning(msg.warning)
		}
		if msg.kind == actionDelete {
			return m.setStatusSuccess("Moved item to trash.")
		}
		if msg.saved.IsDone() {
			return m.setStatusSuccess("Restored item to archive.")
		}
		return m.setStatusSuccess("Restored item.")
	case actionPurge:
		if msg.itemIndex < 0 || msg.itemIndex >= len(m.items) {
			return m.setStatusWarning("Item no longer exists.")
		}
		m.items = append(m.items[:msg.itemIndex], m.items[msg.itemIndex+1:]...)
		if err := m.persistItems(); err != nil {
			return m.setStatusError(fmt.Sprintf("Purge failed: %v", err))
		}
		m.detailScroll = 0
		m.rebuildFiltered()
		return m.setStatusSuccess("Item purged permanently.")
	default:
		return m
	}
}

func (m modelUI) enterConflict(conflictErr *githubsync.ConflictError, local model.Item) tea.Model {
	editingIndex := m.form.editingIndex
	isNew := m.form.isNew
	if m.mode != modeEdit {
		editingIndex = -1
		for idx, item := range m.items {
			if itemRemoteKey(item) != "" && itemRemoteKey(item) == itemRemoteKey(local) {
				editingIndex = idx
				break
			}
			if item.Title == local.Title && item.Project == local.Project && item.Repo == local.Repo {
				editingIndex = idx
			}
		}
		isNew = local.IssueNumber == 0
	}

	m.mode = modeConflict
	m.focus = focusDetails
	m.detailScroll = 0
	m.conflict = &conflictState{
		local:        local,
		remote:       conflictErr.Remote,
		editingIndex: editingIndex,
		isNew:        isNew,
	}
	return m.setStatus(conflictErr.Error())
}

func (m modelUI) resolveConflictWithRemote() tea.Model {
	if m.conflict == nil {
		m.mode = modeNormal
		return m
	}

	remote := m.conflict.remote

	if !m.conflict.isNew && m.conflict.editingIndex >= 0 && m.conflict.editingIndex < len(m.items) {
		m.items[m.conflict.editingIndex] = remote
	}
	if err := m.trackRepo(m.resolvedItemRepo(remote)); err != nil {
		return m.setStatusError(fmt.Sprintf("Conflict save failed: %v", err))
	}

	if err := m.persistItems(); err != nil {
		return m.setStatus(fmt.Sprintf("Conflict save failed: %v", err))
	}

	m.mode = modeNormal
	m.conflict = nil
	m.detailScroll = 0
	m.rebuildFiltered()
	m.selectItem(remote)
	return m.setStatus("Kept the GitHub version.")
}

func (m modelUI) resolveConflictByOverwriting() (tea.Model, tea.Cmd) {
	if m.conflict == nil {
		m.mode = modeNormal
		return m, nil
	}

	repo := m.resolvedItemRepo(m.conflict.local)
	m.saveInFlight = true
	m = m.setStatusLoading(fmt.Sprintf("Overwriting item in %s...", repo)).(modelUI)
	return m, conflictOverwriteCmd(m.githubClient, repo, m.conflict.local)
}

func (m modelUI) finishSetup(storageMode, repo string) (tea.Model, tea.Cmd) {
	if m.configManager == nil {
		return m.setStatusError("Config manager is unavailable."), nil
	}

	dataFile, err := config.DefaultDataFile()
	if err != nil {
		return m.setStatusError(fmt.Sprintf("Setup failed: %v", err)), nil
	}

	cfg := config.AppConfig{
		StorageMode:  storageMode,
		Repo:         repo,
		TrackedRepos: []string{repo},
		DataFile:     dataFile,
		Density:      m.listDensity.String(),
	}
	if err := m.saveConfigAndApply(cfg); err != nil {
		return m.setStatusError(fmt.Sprintf("Setup failed: %v", err)), nil
	}

	m.mode = modeNormal
	m.setup.enteringRepo = false
	m.setup.repoInput.Blur()
	m.rebuildFiltered()
	if storageMode == config.ModeGitHub {
		return m.beginSync()
	}
	m.postLoadStatus()
	return m, nil
}

func (m *modelUI) saveConfigAndApply(cfg config.AppConfig) error {
	if m.configManager == nil {
		return fmt.Errorf("config manager is unavailable")
	}

	oldCfg := m.config
	configSnapshot, err := snapshotFile(m.configManager.Path())
	if err != nil {
		return err
	}
	itemsSnapshot, err := snapshotFile(cfg.DataFile)
	if err != nil {
		return err
	}

	m.applyConfig(cfg)
	if err := m.store.SaveItems(m.items); err != nil {
		m.applyConfig(oldCfg)
		return err
	}

	cfg = config.Normalize(cfg)
	cfg.TrackedRepos = trackedReposForItems(cfg.Repo, m.items, cfg.ProjectRepos)
	cfg = config.Normalize(cfg)
	if err := m.configManager.Save(cfg); err != nil {
		itemsRollbackErr := restoreFileSnapshot(itemsSnapshot)
		configRollbackErr := restoreFileSnapshot(configSnapshot)
		m.applyConfig(oldCfg)
		if itemsRollbackErr != nil || configRollbackErr != nil {
			return fmt.Errorf("%w (item rollback: %v, config rollback: %v)", err, itemsRollbackErr, configRollbackErr)
		}
		return err
	}

	m.applyConfig(cfg)
	return nil
}

func (m *modelUI) saveConfigOnly(cfg config.AppConfig) error {
	if m.configManager == nil {
		return fmt.Errorf("config manager is unavailable")
	}
	cfg = config.Normalize(cfg)
	if err := m.configManager.Save(cfg); err != nil {
		return err
	}
	m.applyConfig(cfg)
	return nil
}

func (m *modelUI) postLoadStatus() {
	switch m.config.StorageMode {
	case config.ModeGitHub:
		if len(m.syncTargetRepos(m.items)) > 1 {
			m.statusMessage = fmt.Sprintf("GitHub mode active for %s (+%d more repos).", m.config.Repo, len(m.syncTargetRepos(m.items))-1)
		} else {
			m.statusMessage = fmt.Sprintf("GitHub mode active for %s.", m.config.Repo)
		}
	case config.ModeLocal:
		m.statusMessage = "Local mode active."
	default:
		m.statusMessage = "Storage ready."
	}
	m.statusUntil = time.Now().Add(6 * time.Second)
	m.statusKind = statusInfo
	m.statusSticky = false
}

func (m modelUI) defaultItemRepo() string {
	if m.config.StorageMode != config.ModeGitHub {
		return ""
	}
	return normalizeRepoRef(m.config.Repo)
}

func (m modelUI) defaultRepoForProject(project string) string {
	if m.config.StorageMode != config.ModeGitHub {
		return ""
	}
	if repo := normalizeRepoRef(m.config.ProjectRepos[normalizeProjectKey(project)]); repo != "" {
		return repo
	}
	return m.defaultItemRepo()
}

func (m modelUI) resolvedItemRepo(item model.Item) string {
	repo := normalizeRepoRef(item.Repo)
	if repo != "" {
		return repo
	}
	return m.defaultRepoForProject(item.Project)
}

func (m modelUI) syncTargetRepos(items []model.Item) []string {
	repos := trackedReposForItems(m.config.Repo, items, m.config.ProjectRepos)
	normalized := config.Normalize(m.config)
	for _, repo := range normalized.TrackedRepos {
		repos = append(repos, repo)
	}
	return uniqueValidRepos(repos)
}

func (m *modelUI) trackRepo(repo string) error {
	repo = normalizeRepoRef(repo)
	if !validRepoRef(repo) || m.configManager == nil {
		return nil
	}

	cfg := m.config
	cfg.TrackedRepos = append(cfg.TrackedRepos, repo)
	cfg = config.Normalize(cfg)
	if slices.Equal(cfg.TrackedRepos, m.config.TrackedRepos) && cfg.Repo == m.config.Repo {
		return nil
	}

	if err := m.configManager.Save(cfg); err != nil {
		return err
	}
	m.applyConfig(cfg)
	return nil
}

func (m *modelUI) reconcileTrackedRepos(items []model.Item) error {
	if m.configManager == nil {
		return nil
	}

	cfg := m.config
	cfg.TrackedRepos = trackedReposForItems(cfg.Repo, items, cfg.ProjectRepos)
	cfg = config.Normalize(cfg)
	if slices.Equal(cfg.TrackedRepos, m.config.TrackedRepos) && cfg.Repo == m.config.Repo && cfg.Density == m.config.Density {
		return nil
	}

	if err := m.configManager.Save(cfg); err != nil {
		return err
	}
	m.applyConfig(cfg)
	return nil
}

func (m modelUI) storageModeLabel() string {
	switch m.config.StorageMode {
	case config.ModeGitHub:
		return "github"
	case config.ModeLocal:
		return "local"
	default:
		return "unconfigured"
	}
}

func (m modelUI) storageSummary() string {
	switch m.config.StorageMode {
	case config.ModeGitHub:
		repos := m.syncTargetRepos(m.items)
		if m.config.Repo != "" && len(repos) > 1 {
			return fmt.Sprintf("GitHub sync: %s +%d more", m.config.Repo, len(repos)-1)
		}
		if m.config.Repo != "" {
			return "GitHub sync: " + m.config.Repo
		}
		if len(repos) > 0 {
			return fmt.Sprintf("GitHub sync: %d repos", len(repos))
		}
		return "GitHub sync"
	case config.ModeLocal:
		return "Local JSON file"
	default:
		return "Not configured"
	}
}

func syncRepoCmd(client *githubsync.Client, repos []string) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return syncResultMsg{repos: repos, err: fmt.Errorf("github client is not configured")}
		}

		items, err := syncRepos(client, repos)
		return syncResultMsg{
			repos: repos,
			items: items,
			err:   err,
		}
	}
}

func syncRepos(client *githubsync.Client, repos []string) ([]model.Item, error) {
	synced := make([]model.Item, 0)
	for _, repo := range repos {
		items, err := client.SyncRepo(repo)
		if err != nil {
			return nil, err
		}
		synced = append(synced, items...)
	}
	return synced, nil
}

func mergeSyncedItems(existing, remote []model.Item, syncedRepos []string) []model.Item {
	syncedSet := map[string]struct{}{}
	for _, repo := range syncedRepos {
		repo = normalizeRepoRef(repo)
		if repo != "" {
			syncedSet[repo] = struct{}{}
		}
	}

	remoteByKey := make(map[string]model.Item, len(remote))
	for _, item := range remote {
		if key := itemRemoteKey(item); key != "" {
			remoteByKey[key] = item
		}
	}

	claimed := map[string]struct{}{}
	merged := make([]model.Item, 0, len(existing)+len(remote))
	for _, item := range existing {
		if item.IsLocallyPurged() || item.HasPendingSync() || item.SyncConflict || item.SyncError != "" {
			if key := itemRemoteKey(item); key != "" {
				claimed[key] = struct{}{}
			}
			merged = append(merged, item)
			continue
		}

		repo := normalizeRepoRef(item.RemoteRepo())
		if item.IssueNumber == 0 {
			merged = append(merged, item)
			continue
		}
		if _, ok := syncedSet[repo]; !ok {
			merged = append(merged, item)
			continue
		}

		key := itemRemoteKey(item)
		if remoteItem, ok := remoteByKey[key]; ok {
			claimed[key] = struct{}{}
			merged = append(merged, remoteItem)
		} else {
			merged = append(merged, item)
		}
	}

	for _, item := range remote {
		key := itemRemoteKey(item)
		if key == "" {
			merged = append(merged, item)
			continue
		}
		if _, ok := claimed[key]; ok {
			continue
		}
		merged = append(merged, item)
	}
	return merged
}

func itemRemoteKey(item model.Item) string {
	repo := normalizeRepoRef(item.RemoteRepo())
	if repo == "" || item.IssueNumber == 0 {
		return ""
	}
	return fmt.Sprintf("%s#%d", repo, item.IssueNumber)
}

func (m modelUI) beginSync() (tea.Model, tea.Cmd) {
	if m.githubClient == nil {
		return m.setStatusError("GitHub client is not configured."), nil
	}
	if m.syncing {
		return m, nil
	}
	repos := m.syncTargetRepos(m.items)
	if len(repos) == 0 {
		return m.setStatusWarning("No GitHub repos are configured for sync."), nil
	}

	m.syncing = true
	if len(repos) == 1 {
		m = m.withStatus(fmt.Sprintf("Syncing GitHub issues from %s...", repos[0]), statusLoading, 0, true)
	} else {
		m = m.withStatus(fmt.Sprintf("Syncing GitHub issues from %d repos...", len(repos)), statusLoading, 0, true)
	}
	return m, syncRepoCmd(m.githubClient, repos)
}

func (m modelUI) performBatchSync() (tea.Model, tea.Cmd) {
	if m.config.StorageMode != config.ModeGitHub {
		m.mode = modeNormal
		m.confirm = nil
		return m.setStatusWarning("Local mode does not use GitHub sync."), nil
	}
	if m.githubClient == nil {
		m.mode = modeNormal
		m.confirm = nil
		return m.setStatusError("GitHub client is not configured."), nil
	}
	repos := m.syncTargetRepos(m.items)
	if len(repos) == 0 {
		m.mode = modeNormal
		m.confirm = nil
		return m.setStatusWarning("No GitHub repos are configured for sync."), nil
	}

	pending := len(m.pendingSyncItems())
	m.mode = modeNormal
	m.confirm = nil
	m.undo = nil
	m.actionInFlight = true
	if pending == 1 {
		m = m.setStatusLoading("Syncing 1 local change to GitHub...").(modelUI)
	} else {
		m = m.setStatusLoading(fmt.Sprintf("Syncing %d local changes to GitHub...", pending)).(modelUI)
	}
	return m, batchSyncCmd(m.githubClient, repos, m.items)
}

func (m modelUI) finishSync(msg syncResultMsg) tea.Model {
	m.syncing = false
	m.initSyncRepos = nil
	if msg.err != nil {
		return m.setStatusError(userFacingError("Sync failed", msg.err))
	}

	m.items = mergeSyncedItems(m.items, msg.items, msg.repos)
	if err := m.persistItems(); err != nil {
		return m.setStatusError(fmt.Sprintf("Sync save failed: %v", err))
	}
	syncMetadataErr := m.recordSuccessfulSync(time.Now())

	m.rebuildFiltered()
	if syncMetadataErr != nil {
		return m.setStatusWarning(fmt.Sprintf("Synced %d issues, but failed to record last sync time: %v", len(msg.items), syncMetadataErr))
	}
	if len(msg.repos) == 1 {
		return m.setStatusSuccess(fmt.Sprintf("Synced %d issues from %s.", len(msg.items), msg.repos[0]))
	}
	return m.setStatusSuccess(fmt.Sprintf("Synced %d issues from %d repos.", len(msg.items), len(msg.repos)))
}

func (m modelUI) isTooSmall() bool {
	return m.width < minWidth || m.height < minHeight
}

func (m *modelUI) toggleViewMode() {
	switch m.viewMode {
	case viewArchive:
		m.viewMode = viewTrash
		m.rebuildFiltered()
		m.setStatus("Switched to trash.")
		return
	case viewTrash:
		m.viewMode = viewActive
		m.rebuildFiltered()
		m.setStatus("Switched to all items.")
		return
	default:
		m.viewMode = viewArchive
		m.rebuildFiltered()
		m.setStatus("Switched to archive.")
		return
	}
}

func (m modelUI) activeProjectLabel() string {
	if m.projectFilter == "" || m.projectFilter == allProjectsLabel {
		return "all"
	}
	return m.projectFilter
}

func (m modelUI) emptyStateProjectLine(suffix string) string {
	project := m.activeProjectLabel()
	if project == "all" {
		return m.styles.subtitle.Render("All projects " + suffix)
	}
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.renderProjectText(project, project),
		m.styles.subtitle.Render(" "+suffix),
	)
}

func (m modelUI) viewHasVisibleItems(view viewMode) bool {
	for _, item := range m.items {
		if item.IsLocallyPurged() {
			continue
		}
		switch view {
		case viewTrash:
			if item.IsTrashed() {
				return true
			}
		case viewArchive:
			if item.IsDone() && !item.IsTrashed() {
				return true
			}
		default:
			if !item.IsDone() && !item.IsTrashed() {
				return true
			}
		}
	}
	return false
}

func (m modelUI) currentProjectIndex() int {
	projects := m.projectOptions()
	target := m.projectFilter
	if target == "" {
		target = allProjectsLabel
	}
	for idx, project := range projects {
		if project == target {
			return idx
		}
	}
	return 0
}

func (m modelUI) stageFilterLabel() string {
	if m.stageFilter == "" || m.stageFilter == allStagesLabel {
		return "all"
	}
	return m.stageFilter
}

func (m modelUI) hasActiveItemFilters() bool {
	return m.projectFilter != "" && m.projectFilter != allProjectsLabel ||
		m.stageFilter != "" && m.stageFilter != allStagesLabel ||
		m.lastSearch != ""
}

func (m modelUI) filterSummaryLines() []string {
	lines := []string{}
	if m.projectFilter != "" && m.projectFilter != allProjectsLabel {
		lines = append(lines, fmt.Sprintf("project: %s", m.projectFilter))
	}
	if m.stageFilter != "" && m.stageFilter != allStagesLabel {
		lines = append(lines, fmt.Sprintf("stage: %s", m.stageFilter))
	}
	if m.lastSearch != "" {
		lines = append(lines, fmt.Sprintf("search: %q", m.lastSearch))
	}
	if len(lines) == 0 {
		lines = append(lines, "no active filters")
	}
	return lines
}

func (m modelUI) withStatus(message string, kind statusKind, duration time.Duration, sticky bool) modelUI {
	m.statusMessage = normalizeStatusMessage(message)
	m.statusKind = kind
	m.statusSticky = sticky
	m.statusSpinnerFrame = 0
	if sticky || duration <= 0 {
		m.statusUntil = time.Time{}
	} else {
		m.statusUntil = time.Now().Add(duration)
	}
	return m
}

func (m modelUI) setStatusInfo(message string) tea.Model {
	return m.withStatus(message, statusInfo, 4*time.Second, false)
}

func (m modelUI) setStatusSuccess(message string) tea.Model {
	return m.withStatus(message, statusSuccess, 4*time.Second, false)
}

func (m modelUI) setStatusWarning(message string) tea.Model {
	return m.withStatus(message, statusWarning, 5*time.Second, false)
}

func (m modelUI) setStatusError(message string) tea.Model {
	return m.withStatus(message, statusError, 6*time.Second, false)
}

func (m modelUI) setStatusLoading(message string) tea.Model {
	return m.withStatus(message, statusLoading, 0, true)
}

func (m modelUI) statusActive() bool {
	return m.statusMessage != "" && (m.statusSticky || time.Now().Before(m.statusUntil))
}

func (m modelUI) statusStyle() lipgloss.Style {
	switch m.statusKind {
	case statusSuccess:
		return m.styles.statusSuccess
	case statusWarning:
		return m.styles.statusWarning
	case statusError:
		return m.styles.statusError
	case statusLoading:
		return m.styles.statusLoading
	default:
		return m.styles.statusInfo
	}
}

func (m modelUI) setStatus(message string) tea.Model {
	return m.setStatusInfo(message)
}

func normalizeStatusMessage(message string) string {
	message = strings.TrimSpace(message)
	return strings.TrimRight(message, ".")
}

func statusSpinnerTickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
		return statusSpinnerTickMsg(t)
	})
}

func userFacingError(action string, err error) string {
	if message := githubsync.UserMessage(err); message != "" {
		return message
	}
	return fmt.Sprintf("%s: %v", action, err)
}

func joinWarnings(values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, " ")
}

func normalizeImportedItems(items []model.Item) ([]model.Item, error) {
	normalized := make([]model.Item, 0, len(items))
	now := time.Now()
	for idx, item := range items {
		item.Title = strings.TrimSpace(item.Title)
		item.Project = strings.TrimSpace(item.Project)
		item.Repo = normalizeRepoRef(item.Repo)
		item.SyncedRepo = normalizeRepoRef(item.SyncedRepo)
		item.SyncError = strings.TrimSpace(item.SyncError)
		if item.Title == "" {
			return nil, fmt.Errorf("item %d is missing a title", idx+1)
		}
		if item.Project == "" {
			return nil, fmt.Errorf("item %d is missing a project", idx+1)
		}
		item.Type = normalizeType(item.Type)
		if item.Repo != "" && !validRepoRef(item.Repo) {
			return nil, fmt.Errorf("item %d has an invalid repo", idx+1)
		}
		if item.SyncedRepo != "" && !validRepoRef(item.SyncedRepo) {
			return nil, fmt.Errorf("item %d has an invalid synced repo", idx+1)
		}
		if item.IssueNumber > 0 && item.SyncedRepo == "" {
			item.SyncedRepo = item.Repo
		}
		if !validType(item.Type) {
			return nil, fmt.Errorf("item %d has an invalid type", idx+1)
		}
		if !validStage(item.Stage) {
			return nil, fmt.Errorf("item %d has an invalid stage", idx+1)
		}
		if !validSyncOperation(item.PendingSync) {
			return nil, fmt.Errorf("item %d has an invalid pending sync operation", idx+1)
		}
		if item.CreatedAt.IsZero() {
			item.CreatedAt = now
		}
		if item.UpdatedAt.IsZero() {
			item.UpdatedAt = item.CreatedAt
		}
		normalized = append(normalized, item)
	}
	return normalized, nil
}

func normalizeRepoRef(repo string) string {
	repo = strings.TrimSpace(repo)
	if strings.EqualFold(repo, "local") {
		return ""
	}
	return repo
}

func validRepoRef(repo string) bool {
	if repo == "" {
		return false
	}
	parts := strings.Split(repo, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

func detailRepoLabel(repo string) string {
	repo = normalizeRepoRef(repo)
	if repo == "" {
		return "local-only"
	}
	return repo
}

func displayRepoValue(repo, fallback string) string {
	repo = normalizeRepoRef(repo)
	if repo != "" {
		return repo
	}
	return normalizeRepoRef(fallback)
}

func trackedReposForItems(defaultRepo string, items []model.Item, projectRepos map[string]string) []string {
	seen := map[string]struct{}{}
	repos := make([]string, 0, len(items)+1+len(projectRepos))
	add := func(repo string) {
		repo = normalizeRepoRef(repo)
		if !validRepoRef(repo) {
			return
		}
		if _, ok := seen[repo]; ok {
			return
		}
		seen[repo] = struct{}{}
		repos = append(repos, repo)
	}

	add(defaultRepo)
	for _, repo := range sortedProjectRepoValues(projectRepos) {
		add(repo)
	}
	for _, item := range items {
		add(item.Repo)
		add(item.RemoteRepo())
	}
	return repos
}

func uniqueValidRepos(repos []string) []string {
	seen := map[string]struct{}{}
	deduped := make([]string, 0, len(repos))
	for _, repo := range repos {
		repo = normalizeRepoRef(repo)
		if !validRepoRef(repo) {
			continue
		}
		if _, ok := seen[repo]; ok {
			continue
		}
		seen[repo] = struct{}{}
		deduped = append(deduped, repo)
	}
	return deduped
}

func normalizeProjectKey(project string) string {
	return strings.ToLower(strings.TrimSpace(project))
}

func sortedProjectRepoValues(projectRepos map[string]string) []string {
	if len(projectRepos) == 0 {
		return nil
	}
	values := make([]string, 0, len(projectRepos))
	for _, repo := range projectRepos {
		repo = normalizeRepoRef(repo)
		if !validRepoRef(repo) {
			continue
		}
		values = append(values, repo)
	}
	sort.Strings(values)
	return values
}

func cloneProjectRepos(projectRepos map[string]string) map[string]string {
	if len(projectRepos) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(projectRepos))
	for project, repo := range projectRepos {
		cloned[project] = repo
	}
	return cloned
}

func validStage(stage model.Stage) bool {
	for _, candidate := range model.Stages {
		if candidate == stage {
			return true
		}
	}
	return false
}

func validType(itemType model.Type) bool {
	for _, candidate := range model.Types {
		if candidate == itemType {
			return true
		}
	}
	return false
}

func validSyncOperation(op model.SyncOperation) bool {
	switch op {
	case model.SyncNone, model.SyncCreate, model.SyncUpdate, model.SyncDelete, model.SyncRestore, model.SyncPurge:
		return true
	default:
		return false
	}
}

func normalizeType(itemType model.Type) model.Type {
	itemType = model.Type(strings.TrimSpace(string(itemType)))
	if itemType == "" {
		return model.TypeFeature
	}
	return itemType
}

func stageIndex(stage model.Stage) int {
	for idx, candidate := range model.Stages {
		if candidate == stage {
			return idx
		}
	}
	return 0
}

func typeIndex(itemType model.Type) int {
	itemType = normalizeType(itemType)
	for idx, candidate := range model.Types {
		if candidate == itemType {
			return idx
		}
	}
	return 0
}

func parseStageLabel(label string) (model.Stage, bool) {
	for _, stage := range model.Stages {
		if string(stage) == label {
			return stage, true
		}
	}
	return "", false
}

func parseTypeLabel(label string) (model.Type, bool) {
	for _, itemType := range model.Types {
		if string(itemType) == label {
			return itemType, true
		}
	}
	return "", false
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return s[:width]
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

func truncatePlain(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return s[:width]
	}
	runes := []rune(s)
	if len(runes) > width-1 {
		runes = runes[:width-1]
	}
	return string(runes) + "…"
}

func wrapPlainLines(s string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	if s == "" {
		return []string{""}
	}

	rawLines := strings.Split(s, "\n")
	wrapped := make([]string, 0, len(rawLines))
	for _, raw := range rawLines {
		if raw == "" {
			wrapped = append(wrapped, "")
			continue
		}
		chunk := wordwrap.String(raw, width)
		wrapped = append(wrapped, strings.Split(chunk, "\n")...)
	}
	return wrapped
}

func parseMarkdownHeading(raw string) (int, string, bool) {
	trimmed := strings.TrimLeft(raw, " \t")
	if !strings.HasPrefix(trimmed, "#") {
		return 0, "", false
	}

	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level >= len(trimmed) || trimmed[level] != ' ' {
		return 0, "", false
	}

	text := strings.TrimSpace(trimmed[level:])
	if text == "" {
		return 0, "", false
	}
	return level, text, true
}

func parseMarkdownQuote(raw string) (string, bool) {
	trimmed := strings.TrimLeft(raw, " \t")
	if !strings.HasPrefix(trimmed, ">") {
		return "", false
	}
	text := strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
	return text, true
}

func wrapMarkdownSegments(segments []markdownRenderSegment, width int) []string {
	if width <= 0 {
		return []string{""}
	}

	tokens := tokenizeMarkdownSegments(segments)
	if len(tokens) == 0 {
		return []string{""}
	}

	lines := []string{}
	current := []string{}
	currentWidth := 0
	pendingSpace := false

	flush := func() {
		if len(current) == 0 {
			lines = append(lines, "")
		} else {
			lines = append(lines, strings.Join(current, ""))
		}
		current = nil
		currentWidth = 0
		pendingSpace = false
	}

	for _, token := range tokens {
		if strings.TrimSpace(token.text) == "" {
			if currentWidth > 0 {
				pendingSpace = true
			}
			continue
		}

		wordWidth := runewidth.StringWidth(token.text)
		if wordWidth > width {
			chunks := splitStyledTokenByWidth(token, width)
			for _, chunk := range chunks {
				if currentWidth > 0 {
					flush()
				}
				if runewidth.StringWidth(chunk.text) == width {
					lines = append(lines, chunk.style.Render(chunk.text))
				} else {
					current = append(current, chunk.style.Render(chunk.text))
					currentWidth = runewidth.StringWidth(chunk.text)
				}
			}
			pendingSpace = false
			continue
		}

		extra := 0
		if pendingSpace && currentWidth > 0 {
			extra = 1
		}
		if currentWidth > 0 && currentWidth+extra+wordWidth > width {
			flush()
		}

		if pendingSpace && currentWidth > 0 {
			current = append(current, " ")
			currentWidth++
		}
		current = append(current, token.style.Render(token.text))
		currentWidth += wordWidth
		pendingSpace = false
	}

	if len(current) > 0 || len(lines) == 0 {
		flush()
	}

	return lines
}

func tokenizeMarkdownSegments(segments []markdownRenderSegment) []markdownRenderSegment {
	tokens := []markdownRenderSegment{}
	for _, segment := range segments {
		if segment.text == "" {
			continue
		}
		if segment.keepTogether {
			tokens = append(tokens, segment)
			continue
		}
		runes := []rune(segment.text)
		start := 0
		isSpace := unicode.IsSpace(runes[0])
		for idx := 1; idx < len(runes); idx++ {
			if unicode.IsSpace(runes[idx]) == isSpace {
				continue
			}
			tokens = append(tokens, markdownRenderSegment{text: string(runes[start:idx]), style: segment.style})
			start = idx
			isSpace = unicode.IsSpace(runes[idx])
		}
		tokens = append(tokens, markdownRenderSegment{text: string(runes[start:]), style: segment.style})
	}
	return tokens
}

func splitStyledTokenByWidth(token markdownRenderSegment, width int) []markdownRenderSegment {
	if width <= 0 {
		return []markdownRenderSegment{{text: token.text, style: token.style}}
	}

	chunks := []markdownRenderSegment{}
	var builder strings.Builder
	currentWidth := 0
	for _, r := range token.text {
		runeWidth := runewidth.RuneWidth(r)
		if runeWidth <= 0 {
			runeWidth = 1
		}
		if currentWidth+runeWidth > width && builder.Len() > 0 {
			chunks = append(chunks, markdownRenderSegment{text: builder.String(), style: token.style})
			builder.Reset()
			currentWidth = 0
		}
		builder.WriteRune(r)
		currentWidth += runeWidth
	}
	if builder.Len() > 0 {
		chunks = append(chunks, markdownRenderSegment{text: builder.String(), style: token.style})
	}
	if len(chunks) == 0 {
		return []markdownRenderSegment{{text: token.text, style: token.style}}
	}
	return chunks
}

func nextInlineMarkdownStart(nextCode, nextLink int) int {
	switch {
	case nextCode < 0:
		return nextLink
	case nextLink < 0:
		return nextCode
	case nextCode < nextLink:
		return nextCode
	default:
		return nextLink
	}
}

func splitPlainByWidth(s string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	if s == "" {
		return []string{""}
	}

	lines := []string{}
	var builder strings.Builder
	currentWidth := 0
	for _, r := range s {
		runeWidth := runewidth.RuneWidth(r)
		if runeWidth <= 0 {
			runeWidth = 1
		}
		if currentWidth+runeWidth > width && builder.Len() > 0 {
			lines = append(lines, builder.String())
			builder.Reset()
			currentWidth = 0
		}
		builder.WriteRune(r)
		currentWidth += runeWidth
	}
	if builder.Len() > 0 {
		lines = append(lines, builder.String())
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func truncateBlock(s string, width, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = truncate(lines[maxLines-1], max(0, width-1)) + "…"
	}
	for i := range lines {
		lines[i] = truncate(lines[i], width)
	}
	return strings.Join(lines, "\n")
}

func fitContentBox(content string, width, height int) string {
	if height <= 0 || width <= 0 {
		return ""
	}

	lines := strings.Split(content, "\n")
	fitted := make([]string, 0, height)

	for _, line := range lines {
		if len(fitted) >= height {
			break
		}
		fitted = append(fitted, padANSIRight(truncateANSI(line, width), width))
	}

	for len(fitted) < height {
		fitted = append(fitted, strings.Repeat(" ", width))
	}

	return strings.Join(fitted, "\n")
}

func padANSIRight(s string, width int) string {
	padding := max(0, width-lipgloss.Width(s))
	if padding == 0 {
		return s
	}
	return s + strings.Repeat(" ", padding)
}

func truncateANSI(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return reflowtruncate.String(s, uint(width))
	}
	return reflowtruncate.StringWithTail(s, uint(width), "…")
}

func overlayBottom(base, overlay string) string {
	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")
	if len(overlayLines) == 0 {
		return base
	}

	start := max(0, len(baseLines)-len(overlayLines))
	for idx, line := range overlayLines {
		target := start + idx
		if target >= len(baseLines) {
			baseLines = append(baseLines, line)
			continue
		}
		tail := ansiCutLeft(baseLines[target], lipgloss.Width(line))
		baseLines[target] = line + tail
	}

	return strings.Join(baseLines, "\n")
}

func ansiCutLeft(s string, width int) string {
	if width <= 0 || s == "" {
		return s
	}

	var (
		out         strings.Builder
		seq         strings.Builder
		activeSeq   strings.Builder
		inANSI      bool
		cutComplete bool
		visible     int
	)

	for _, r := range s {
		if r == ansi.Marker {
			inANSI = true
			seq.Reset()
			seq.WriteRune(r)
			continue
		}

		if inANSI {
			seq.WriteRune(r)
			if ansi.IsTerminator(r) {
				code := seq.String()
				if strings.HasSuffix(code, "[0m") {
					activeSeq.Reset()
				} else if r == 'm' {
					activeSeq.WriteString(code)
				}
				if cutComplete {
					out.WriteString(code)
				}
				inANSI = false
			}
			continue
		}

		runeWidth := runewidth.RuneWidth(r)
		if !cutComplete {
			visible += runeWidth
			if visible <= width {
				continue
			}
			cutComplete = true
			if activeSeq.Len() > 0 {
				out.WriteString(activeSeq.String())
			}
		}

		out.WriteRune(r)
	}

	return out.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func compareTime(a, b time.Time) int {
	switch {
	case a.Before(b):
		return -1
	case a.After(b):
		return 1
	default:
		return 0
	}
}

func relativeTimeLabel(now, t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}

	diff := now.Sub(t)
	future := diff < 0
	if future {
		diff = -diff
	}

	var label string
	switch {
	case diff < 45*time.Second:
		if future {
			return "soon"
		}
		return "just now"
	case diff < 90*time.Second:
		label = "1m"
	case diff < 60*time.Minute:
		label = fmt.Sprintf("%dm", int(diff/time.Minute))
	case diff < 90*time.Minute:
		label = "1h"
	case diff < 24*time.Hour:
		label = fmt.Sprintf("%dh", int(diff/time.Hour))
	case diff < 48*time.Hour:
		label = "1d"
	case diff < 7*24*time.Hour:
		label = fmt.Sprintf("%dd", int(diff/(24*time.Hour)))
	case diff < 30*24*time.Hour:
		label = fmt.Sprintf("%dw", int(diff/(7*24*time.Hour)))
	case diff < 365*24*time.Hour:
		label = fmt.Sprintf("%dmo", int(diff/(30*24*time.Hour)))
	default:
		label = fmt.Sprintf("%dy", int(diff/(365*24*time.Hour)))
	}

	if future {
		return "in " + label
	}
	return label + " ago"
}

func baseCommandSuggestions() []string {
	return []string{
		"new",
		"edit",
		"delete",
		"undo",
		"restore",
		"purge",
		"sync",
		"drafts",
		"drafts show",
		"drafts reset",
		"drafts folder ",
		"shortcuts",
		"repos",
		"open",
		"search ",
		"search clear",
		"stage all",
		"stage idea",
		"stage planned",
		"stage active",
		"stage blocked",
		"stage done",
		"density comfortable",
		"density compact",
		"project-repo ",
		"project-repo clear ",
		"project-label always",
		"project-label auto",
		"project-label never",
		"project all",
		"view all",
		"view archive",
		"view trash",
		"sort updated desc",
		"sort updated asc",
		"sort created desc",
		"sort created asc",
		"export json ",
		"import json ",
		"storage github ",
		"storage local",
		"quit",
	}
}

func (m modelUI) commandSuggestions() []string {
	suggestions := append([]string(nil), baseCommandSuggestions()...)
	for _, project := range m.projectOptions() {
		if project == allProjectsLabel {
			continue
		}
		suggestions = append(suggestions, "project "+project)
		suggestions = append(suggestions, "project-repo "+project+" ")
		suggestions = append(suggestions, "project-repo clear "+project)
	}
	sort.Strings(suggestions)
	return suggestions
}

func matchedCommandSuggestions(value string, suggestions []string) []string {
	prefix := normalizeCommandValue(value)
	if prefix == "" {
		return append([]string(nil), suggestions...)
	}

	matches := make([]string, 0, len(suggestions))
	for _, suggestion := range suggestions {
		if strings.HasPrefix(strings.ToLower(suggestion), prefix) {
			matches = append(matches, suggestion)
		}
	}
	return matches
}

func normalizeCommandValue(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func shouldCloseEmptyCommand(msg tea.KeyMsg, before, after string) bool {
	if strings.TrimSpace(after) != "" {
		return false
	}
	if strings.TrimSpace(before) != "" {
		return false
	}
	return msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete || msg.Type == tea.KeyCtrlH
}

func shouldCloseEmptySearch(msg tea.KeyMsg, before, after string) bool {
	if after != "" {
		return false
	}
	if before == "" {
		return msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete || msg.Type == tea.KeyCtrlH
	}

	switch msg.Type {
	case tea.KeyBackspace, tea.KeyDelete, tea.KeyCtrlH, tea.KeyCtrlW, tea.KeyCtrlU:
		return true
	default:
		return false
	}
}

func commandSuggestionWindow(total, index, maxVisible int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	if maxVisible <= 0 || total <= maxVisible {
		return 0, total
	}

	index = clamp(index, 0, total-1)
	start := index - maxVisible + 1
	if start < 0 {
		start = 0
	}
	if start > total-maxVisible {
		start = total - maxVisible
	}
	return start, start + maxVisible
}

func (s sortMode) String() string {
	switch s {
	case sortCreated:
		return "created"
	default:
		return "updated"
	}
}

func (d listDensity) String() string {
	switch d {
	case densityCompact:
		return "compact"
	default:
		return "comfortable"
	}
}

func parseDensity(value string) listDensity {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "compact":
		return densityCompact
	default:
		return densityComfortable
	}
}

func (v viewMode) String() string {
	switch v {
	case viewTrash:
		return "trash"
	case viewArchive:
		return "archive"
	default:
		return "all"
	}
}

func (m modelUI) sortDirectionLabel() string {
	if m.sortAscending {
		return "asc"
	}
	return "desc"
}

func (m modelUI) selectedCommandSuggestion(matches []string) string {
	if len(matches) == 0 {
		return ""
	}
	index := clamp(m.commandSuggestIndex, 0, len(matches)-1)
	return matches[index]
}

func (m modelUI) commandCompletionSuffix() string {
	matches := matchedCommandSuggestions(m.commandInput.Value(), m.commandSuggestions())
	if len(matches) != 1 {
		return ""
	}

	suggestion := matches[0]
	current := normalizeCommandValue(m.commandInput.Value())
	if current == strings.ToLower(suggestion) || !strings.HasPrefix(strings.ToLower(suggestion), current) {
		return ""
	}

	currentRunes := []rune(current)
	suggestionRunes := []rune(suggestion)
	if len(currentRunes) >= len(suggestionRunes) {
		return ""
	}
	return string(suggestionRunes[len(currentRunes):])
}

func commandArgumentHint(value, suffix string) string {
	raw := strings.ToLower(value)
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return ""
	}

	fields := strings.Fields(normalized)
	if len(fields) == 0 {
		return ""
	}

	hasCompletion := suffix != ""
	trailing := strings.HasSuffix(raw, " ")
	command := fields[0]
	args := fields[1:]

	switch command {
	case "search":
		if len(args) == 0 {
			return "<query>"
		}
	case "project":
		if len(args) == 0 {
			return "all|<project>"
		}
	case "stage":
		options := []string{"all", "idea", "planned", "active", "blocked", "done"}
		if len(args) == 0 {
			return strings.Join(options, "|")
		}
	case "density":
		options := []string{"comfortable", "compact"}
		if len(args) == 0 {
			return strings.Join(options, "|")
		}
	case "project-label":
		options := []string{"always", "auto", "never"}
		if len(args) == 0 {
			return strings.Join(options, "|")
		}
	case "drafts":
		if len(args) == 0 {
			return "show|reset|folder <path>"
		}
		if len(args) == 1 {
			options := []string{"show", "reset", "folder"}
			if strings.EqualFold(args[0], "folder") {
				if trailing {
					return "<path>"
				}
				return ""
			}
			if !trailing && !commandOptionExact(args[0], options) {
				if hasCompletion {
					return ""
				}
				return strings.Join(options, "|")
			}
		}
		if len(args) >= 2 && strings.EqualFold(args[0], "folder") {
			return ""
		}
	case "project-repo":
		if len(args) == 0 {
			return "<project> <owner/repo>"
		}
		if strings.EqualFold(args[0], "clear") {
			if len(args) == 1 {
				return "<project>"
			}
			return ""
		}
		if len(args) == 1 && trailing {
			return "<owner/repo>"
		}
	case "storage":
		options := []string{"local", "github"}
		if len(args) == 0 {
			return strings.Join(options, "|")
		}
		if len(args) == 1 && strings.EqualFold(args[0], "github") {
			return "owner/repo"
		}
		if len(args) == 1 && !trailing && !commandOptionExact(args[0], options) {
			if hasCompletion {
				return ""
			}
			return strings.Join(options, "|")
		}
	case "view":
		options := []string{"all", "archive", "trash"}
		if len(args) == 0 {
			return strings.Join(options, "|")
		}
	case "sort":
		firstOptions := []string{"updated", "created"}
		secondOptions := []string{"asc", "desc"}
		if len(args) == 0 {
			return strings.Join(firstOptions, "|")
		}
		if len(args) == 1 {
			if commandOptionExact(args[0], firstOptions) {
				return strings.Join(secondOptions, "|")
			}
		}
	case "export", "import":
		if len(args) == 0 {
			return "json <path>"
		}
		if len(args) == 1 {
			if strings.EqualFold(args[0], "json") {
				return "<path>"
			}
			return "json <path>"
		}
	}

	return ""
}

func commandOptionExact(value string, options []string) bool {
	for _, option := range options {
		if strings.EqualFold(value, option) {
			return true
		}
	}
	return false
}

func githubIssueURL(item model.Item) (string, bool) {
	repo := normalizeRepoRef(item.RemoteRepo())
	if item.IssueNumber <= 0 || repo == "" {
		return "", false
	}
	return fmt.Sprintf("https://github.com/%s/issues/%d", repo, item.IssueNumber), true
}

func openURLCmd(url string) tea.Cmd {
	return func() tea.Msg {
		return openURLResultMsg{
			url: url,
			err: openURLFn(url),
		}
	}
}

func openURL(url string) error {
	commands := openURLCommands(url)
	if len(commands) == 0 {
		return errors.New("no supported URL opener found")
	}
	var failures []string
	for _, candidate := range commands {
		if err := candidate.cmd.Run(); err == nil {
			return nil
		} else {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate.name, err))
		}
	}
	return fmt.Errorf("no opener succeeded: %s", strings.Join(failures, "; "))
}

type openCommand struct {
	name string
	cmd  *exec.Cmd
}

func openURLCommands(url string) []openCommand {
	var commands []openCommand
	add := func(name string, args ...string) {
		if _, err := exec.LookPath(name); err != nil {
			return
		}
		commands = append(commands, openCommand{
			name: name,
			cmd:  exec.Command(name, args...),
		})
	}

	switch runtime.GOOS {
	case "darwin":
		add("open", url)
	case "windows":
		add("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		if isWSL() {
			add("cmd.exe", "/c", "start", "", url)
			add("powershell.exe", "-NoProfile", "-Command", "Start-Process", url)
			add("wslview", url)
		}
		add("xdg-open", url)
		add("gio", "open", url)
		add("sensible-browser", url)
		add("open", url)
	}
	return commands
}

func isWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	return os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != ""
}

func openURLCommand(url string) (*exec.Cmd, error) {
	commands := openURLCommands(url)
	if len(commands) == 0 {
		return nil, errors.New("no supported URL opener found")
	}
	return commands[0].cmd, nil
}

func (m modelUI) selectedItemIndex() (int, bool) {
	if len(m.filtered) == 0 || m.selected < 0 || m.selected >= len(m.filtered) {
		return 0, false
	}
	return m.filtered[m.selected], true
}

func (m modelUI) confirmActionNow() (tea.Model, tea.Cmd) {
	if m.confirm == nil {
		m.mode = modeNormal
		return m, nil
	}

	switch m.confirm.action {
	case confirmPurge:
		return m.performPurge()
	case confirmImport:
		return m.performImport(), nil
	case confirmSync:
		return m.performBatchSync()
	default:
		m.mode = modeNormal
		m.confirm = nil
		return m, nil
	}
}

func (m modelUI) confirmQuitNow() (tea.Model, tea.Cmd) {
	m.mode = modeNormal
	m.confirm = nil
	return m, tea.Quit
}

func (m modelUI) performPurge() (tea.Model, tea.Cmd) {
	if m.confirm == nil {
		m.mode = modeNormal
		return m, nil
	}

	itemIndex := m.confirm.itemIndex
	if itemIndex < 0 || itemIndex >= len(m.items) {
		m.mode = modeNormal
		m.confirm = nil
		m.rebuildFiltered()
		return m.setStatusWarning("Purge target no longer exists."), nil
	}

	item := m.items[itemIndex]
	if m.config.StorageMode == config.ModeGitHub {
		m.mode = modeNormal
		m.confirm = nil
		m = m.captureUndo("purge")
		if item.PendingSync == model.SyncCreate || (item.IssueNumber == 0 && item.RemoteRepo() == "") {
			m.items = append(m.items[:itemIndex], m.items[itemIndex+1:]...)
			if err := m.persistItems(); err != nil {
				return m.setStatusError(fmt.Sprintf("Purge failed: %v", err)), nil
			}
			m.detailScroll = 0
			m.rebuildFiltered()
			return m.setStatusSuccess("Purged local item."), nil
		}
		item.PendingSync = model.SyncPurge
		item.SyncConflict = false
		item.SyncError = ""
		m.items[itemIndex] = item
		if err := m.persistItems(); err != nil {
			return m.setStatusError(fmt.Sprintf("Purge failed: %v", err)), nil
		}
		m.detailScroll = 0
		m.rebuildFiltered()
		return m.setStatusSuccess("Queued item for purge. Press S to sync."), nil
	}

	m = m.captureUndo("purge")
	m.items = append(m.items[:itemIndex], m.items[itemIndex+1:]...)
	if err := m.persistItems(); err != nil {
		m.mode = modeNormal
		m.confirm = nil
		return m.setStatusError(fmt.Sprintf("Purge failed: %v", err)), nil
	}

	m.mode = modeNormal
	m.confirm = nil
	m.detailScroll = 0
	m.rebuildFiltered()
	return m.setStatusSuccess("Item purged permanently."), nil
}

func (m modelUI) performImport() tea.Model {
	if m.confirm == nil {
		m.mode = modeNormal
		return m
	}

	items := append([]model.Item(nil), m.confirm.importItems...)
	path := m.confirm.importPath

	m.items = items
	if err := m.persistItems(); err != nil {
		m.mode = modeNormal
		m.confirm = nil
		return m.setStatusError(fmt.Sprintf("Import failed: %v", err))
	}

	m.mode = modeNormal
	m.confirm = nil
	m.selected = 0
	m.itemOffset = 0
	m.detailScroll = 0
	m.rebuildFiltered()
	return m.setStatusSuccess(fmt.Sprintf("Imported %d items from %s.", len(items), path))
}
