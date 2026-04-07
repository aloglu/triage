package app

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/ansi"
	"github.com/muesli/reflow/wordwrap"

	"github.com/aloglu/triage/internal/config"
	"github.com/aloglu/triage/internal/githubsync"
	"github.com/aloglu/triage/internal/model"
	"github.com/aloglu/triage/internal/storage"
)

const allProjectsLabel = "All projects"
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

type itemForm struct {
	titleInput   textinput.Model
	projectInput textinput.Model
	bodyInput    textarea.Model
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
)

type confirmState struct {
	action    confirmAction
	itemIndex int
}

type modelUI struct {
	width               int
	height              int
	items               []model.Item
	filtered            []int
	selected            int
	itemOffset          int
	detailScroll        int
	projectCursor       int
	projectFilter       string
	viewMode            viewMode
	sortAscending       bool
	commandSuggestIndex int
	queryInput          textinput.Model
	commandInput        textinput.Model
	mode                mode
	focus               focusArea
	sortMode            sortMode
	statusMessage       string
	statusUntil         time.Time
	styles              styles
	form                itemForm
	setup               setupForm
	confirm             *confirmState
	conflict            *conflictState
	lastSearch          string

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

	projectInput := textinput.New()
	projectInput.Prompt = ""
	projectInput.Placeholder = "Project name"
	projectInput.CharLimit = 60

	bodyInput := textarea.New()
	bodyInput.Placeholder = "Write notes, links, and context..."
	bodyInput.ShowLineNumbers = false
	bodyInput.SetHeight(8)
	bodyInput.Focus()
	bodyInput.Blur()

	repoInput := textinput.New()
	repoInput.Prompt = ""
	repoInput.Placeholder = "owner/repo"
	repoInput.CharLimit = 120

	titleInput.Focus()
	titleInput.Blur()
	projectInput.Focus()
	projectInput.Blur()
	repoInput.Focus()
	repoInput.Blur()

	manager, managerErr := config.NewManager()

	m := modelUI{
		items:         []model.Item{},
		projectFilter: allProjectsLabel,
		viewMode:      viewActive,
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
			bodyInput:    bodyInput,
			stageIndex:   0,
			focusIndex:   0,
			editingIndex: -1,
		},
		setup: setupForm{
			selectedMode: 0,
			repoInput:    repoInput,
		},
		configManager: manager,
	}

	if managerErr != nil {
		m.mode = modeSetup
		m.statusMessage = fmt.Sprintf("Config setup unavailable: %v", managerErr)
		m.statusUntil = time.Now().Add(10 * time.Second)
		return m
	}

	cfg, ok, err := manager.Load()
	if err != nil {
		m.mode = modeSetup
		m.statusMessage = fmt.Sprintf("Config error: %v", err)
		m.statusUntil = time.Now().Add(10 * time.Second)
		return m
	}

	if !ok {
		m.mode = modeSetup
		m.statusMessage = "First run. Choose local-only or GitHub-backed storage."
		m.statusUntil = time.Now().Add(8 * time.Second)
		return m
	}

	m.applyConfig(cfg)
	if err := m.loadItems(); err != nil {
		m.statusMessage = userFacingError("Startup sync failed", err)
		m.statusUntil = time.Now().Add(10 * time.Second)
	} else {
		m.postLoadStatus()
	}
	m.rebuildFiltered()

	return m
}

func (m modelUI) Init() tea.Cmd {
	return textinput.Blink
}

func (m modelUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeEditors()
		return m, nil
	case tea.KeyMsg:
		switch m.mode {
		case modeSetup:
			return m.updateSetup(msg)
		case modeProjectPicker:
			return m.updateProjectPicker(msg)
		case modeShortcuts:
			return m.updateShortcuts(msg)
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

func (m modelUI) updateSetup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.setup.enteringRepo {
		switch msg.String() {
		case "esc":
			m.setup.enteringRepo = false
			m.setup.repoInput.Blur()
			return m.setStatus("Setup cancelled."), nil
		case "enter":
			repo := strings.TrimSpace(m.setup.repoInput.Value())
			if repo == "" || !strings.Contains(repo, "/") {
				return m.setStatus("Repository must be in owner/repo form."), nil
			}
			return m.finishSetup(config.ModeGitHub, repo), nil
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
			return m.finishSetup(config.ModeLocal, ""), nil
		}
		m.setup.enteringRepo = true
		return m, m.setup.repoInput.Focus()
	}

	return m, nil
}

func (m modelUI) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "/":
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
		m.beginEdit(-1)
		return m.focusFormField()
	case "e":
		if len(m.filtered) == 0 {
			return m.setStatus("No item selected."), nil
		}
		m.beginEdit(m.filtered[m.selected])
		return m.focusFormField()
	case "s":
		if m.config.StorageMode == config.ModeGitHub {
			return m.syncNow(), nil
		}
		return m.setStatus("Local mode is already current."), nil
	case "D":
		m.toggleViewMode()
		return m, nil
	}

	return m, nil
}

func (m modelUI) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "n", "c":
		m.mode = modeNormal
		m.confirm = nil
		return m.setStatus("Confirmation cancelled."), nil
	case "enter", "y", "p":
		return m.confirmActionNow(), nil
	}

	return m, nil
}

func (m modelUI) updateShortcuts(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "?", "enter":
		m.mode = modeNormal
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
	case "esc":
		m.mode = modeNormal
		m.conflict = nil
		return m.setStatus("Conflict resolution cancelled."), nil
	case "r":
		return m.resolveConflictWithRemote(), nil
	case "o":
		return m.resolveConflictByOverwriting(), nil
	}

	return m, nil
}

func (m modelUI) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.queryInput.Blur()
		return m, nil
	case "enter":
		m.lastSearch = strings.TrimSpace(m.queryInput.Value())
		m.mode = modeNormal
		m.queryInput.Blur()
		m.rebuildFiltered()
		return m.setStatus(fmt.Sprintf("Search set to %q.", m.lastSearch)), nil
	}

	var cmd tea.Cmd
	m.queryInput, cmd = m.queryInput.Update(msg)
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
		m.form.bodyInput.Blur()
		return m.setStatus("Edit cancelled."), nil
	case "ctrl+s":
		return m.saveForm(), nil
	case "tab":
		m.form.focusIndex = (m.form.focusIndex + 1) % 4
		return m.focusFormField()
	case "shift+tab":
		m.form.focusIndex = (m.form.focusIndex + 3) % 4
		return m.focusFormField()
	}

	if m.form.focusIndex == 2 {
		switch msg.String() {
		case "j", "down":
			m.form.focusIndex = (m.form.focusIndex + 1) % 4
			return m.focusFormField()
		case "k", "up":
			m.form.focusIndex = (m.form.focusIndex + 3) % 4
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

	if m.form.focusIndex != 3 {
		switch msg.String() {
		case "down":
			m.form.focusIndex = (m.form.focusIndex + 1) % 4
			return m.focusFormField()
		case "up":
			m.form.focusIndex = (m.form.focusIndex + 3) % 4
			return m.focusFormField()
		}
	}

	var cmd tea.Cmd
	switch m.form.focusIndex {
	case 0:
		m.form.titleInput, cmd = m.form.titleInput.Update(msg)
	case 1:
		m.form.projectInput, cmd = m.form.projectInput.Update(msg)
	case 3:
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
	contentHeight := max(12, m.height-7)
	panelStyle := m.panelStyle(m.styles.panel, detailWidth, contentHeight)
	innerWidth := max(1, detailWidth-panelStyle.GetHorizontalFrameSize())
	innerHeight := max(1, contentHeight-panelStyle.GetVerticalFrameSize())
	lines := m.renderDetailLines(m.items[m.filtered[m.selected]], innerWidth)
	maxScroll := max(0, len(lines)-innerHeight)
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
	contentHeight := max(12, m.height-7)
	listWidth, _ := m.layoutWidths()
	panelStyle := m.panelStyle(m.styles.panelMuted, listWidth, contentHeight)
	innerHeight := max(1, contentHeight-panelStyle.GetVerticalFrameSize())
	available := max(1, innerHeight-2)
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
		m.form.bodyInput.SetValue("")
		m.form.stageIndex = 0
	} else {
		item := m.items[itemIndex]
		m.form.titleInput.SetValue(item.Title)
		m.form.projectInput.SetValue(item.Project)
		m.form.bodyInput.SetValue(item.Body)
		m.form.stageIndex = stageIndex(item.Stage)
	}
	m.resizeEditors()
}

func (m modelUI) focusFormField() (tea.Model, tea.Cmd) {
	m.form.titleInput.Blur()
	m.form.projectInput.Blur()
	m.form.bodyInput.Blur()

	switch m.form.focusIndex {
	case 0:
		cmd := m.form.titleInput.Focus()
		return m, cmd
	case 1:
		cmd := m.form.projectInput.Focus()
		return m, cmd
	case 3:
		cmd := m.form.bodyInput.Focus()
		return m, cmd
	default:
		return m, nil
	}
}

func (m modelUI) saveForm() tea.Model {
	title := strings.TrimSpace(m.form.titleInput.Value())
	project := strings.TrimSpace(m.form.projectInput.Value())
	body := strings.TrimSpace(m.form.bodyInput.Value())
	stage := model.Stages[m.form.stageIndex]

	if title == "" {
		return m.setStatus("Title is required.")
	}
	if project == "" {
		return m.setStatus("Project is required.")
	}

	now := time.Now()
	candidate := m.buildEditedItem(title, project, body, stage, now)

	if m.config.StorageMode == config.ModeGitHub {
		saved, err := m.pushEditedItem(candidate)
		if err != nil {
			var conflictErr *githubsync.ConflictError
			if errors.As(err, &conflictErr) {
				return m.enterConflict(conflictErr, candidate)
			}
			return m.setStatus(userFacingError("GitHub save failed", err))
		}
		candidate = saved
	}

	if m.form.isNew {
		m.items = append([]model.Item{candidate}, m.items...)
	} else {
		m.items[m.form.editingIndex] = candidate
	}

	if err := m.persistItems(); err != nil {
		return m.setStatus(fmt.Sprintf("Save failed: %v", err))
	}

	m.mode = modeNormal
	m.conflict = nil
	m.detailScroll = 0
	m.rebuildFiltered()
	m.selectByTitle(title)

	if m.form.isNew {
		return m.setStatus("Item created.")
	}
	return m.setStatus("Item updated.")
}

func (m *modelUI) rebuildFiltered() {
	m.sortItems()

	if m.projectFilter == "" {
		m.projectFilter = allProjectsLabel
	}

	filtered := make([]int, 0, len(m.items))
	for i, item := range m.items {
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

func (m *modelUI) selectByTitle(title string) {
	for idx, itemIdx := range m.filtered {
		if m.items[itemIdx].Title == title {
			m.selected = idx
			m.detailScroll = 0
			m.ensureSelectedVisible()
			return
		}
	}
}

func (m modelUI) renderHeader() string {
	left := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.styles.title.Render("triage"),
		m.styles.muted.Render(" | "),
		m.styles.subtitle.Render(m.activeProjectLabel()),
	)

	rightText := ""
	switch m.mode {
	case modeSetup:
		rightText = "setup"
	default:
		if m.mode != modeConfirm && time.Now().Before(m.statusUntil) && m.statusMessage != "" {
			rightText = m.statusMessage
		}
	}

	headerWidth := max(0, m.width-4)
	leftWidth := lipgloss.Width(left)
	availableRight := max(0, headerWidth-leftWidth-2)
	rightStyle := m.styles.muted
	if rightText == m.statusMessage && rightText != "" {
		rightStyle = m.styles.status
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

func (m modelUI) renderContent() string {
	contentHeight := max(12, m.height-7)

	if m.mode == modeSetup {
		return m.renderSetupPane(contentHeight)
	}

	listWidth, detailWidth := m.layoutWidths()
	center := m.renderItemsPane(listWidth, contentHeight)
	right := m.renderDetailPane(detailWidth, contentHeight)
	if m.mode == modeProjectPicker {
		return lipgloss.Place(
			max(1, m.width-4),
			contentHeight,
			lipgloss.Center,
			lipgloss.Center,
			m.renderProjectPicker(),
		)
	}
	if m.mode == modeConfirm {
		return lipgloss.Place(
			max(1, m.width-4),
			contentHeight,
			lipgloss.Center,
			lipgloss.Center,
			m.renderConfirmModal(),
		)
	}
	if m.mode == modeShortcuts {
		return lipgloss.Place(
			max(1, m.width-4),
			contentHeight,
			lipgloss.Center,
			lipgloss.Center,
			m.renderShortcutsModal(),
		)
	}
	content := lipgloss.JoinHorizontal(lipgloss.Top, center, right)
	if m.mode == modeCommand {
		if overlay := m.renderCommandOverlay(max(1, m.width-4)); overlay != "" {
			return overlayBottom(content, overlay)
		}
	}
	return content
}

func (m modelUI) renderFooter() string {
	footerWidth := max(0, m.width-4)
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
		left = m.styles.help.Render(truncatePlain(m.footerHint(), footerWidth))
	}

	if m.mode == modeSearch || m.mode == modeCommand {
		return left
	}

	rightText := m.footerMeta()
	if rightText == "" {
		return left
	}

	leftWidth := lipgloss.Width(left)
	availableRight := max(0, footerWidth-leftWidth-2)
	right := m.styles.muted.Render(truncatePlain(rightText, availableRight))
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
		lines = append(lines, m.styles.muted.Render("Items will be saved to a local JSON file under your config directory."))
	}

	if m.setup.selectedMode == 1 {
		lines = append(lines, m.styles.muted.Render("The GitHub repo is remembered now. Sync wiring comes next; local cache is still used today."))
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

	lines := []string{m.styles.subtitle.Render("Items"), ""}

	if len(m.filtered) == 0 {
		lines = append(lines, "")
		switch m.viewMode {
		case viewTrash:
			lines = append(lines,
				m.styles.muted.Render("Trash is empty."),
				m.styles.muted.Render("Delete items to move them here."),
			)
		case viewArchive:
			lines = append(lines,
				m.styles.muted.Render("Archive is empty."),
				m.styles.muted.Render("Items move here when stage is set to done."),
			)
		default:
			lines = append(lines,
				m.styles.muted.Render("No active items match filters."),
				m.styles.muted.Render("Press n to create one."),
			)
		}
		lines = append(lines,
			"",
			m.styles.subtitle.Render("Quick Start"),
			m.styles.muted.Render("n  create a new item"),
			m.styles.muted.Render("/  search items"),
			m.styles.muted.Render("D  cycle all/archive/trash"),
		)
		return panelStyle.Render(m.renderPaneContent(strings.Join(lines, "\n"), width, height, panelStyle))
	}

	visibleCount := m.itemVisibleCount()
	start := clamp(m.itemOffset, 0, max(0, len(m.filtered)-visibleCount))
	end := min(len(m.filtered), start+visibleCount)

	for idx := start; idx < end; idx++ {
		if idx > start {
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
	lines := []string{
		m.styles.subtitle.Render("Shortcuts"),
		"",
		m.styles.subtitle.Render("Navigation"),
		m.styles.muted.Render("j/k or ↑/↓       move list or scroll details"),
		m.styles.muted.Render("h/l or ←/→       switch panes"),
		m.styles.muted.Render("tab              open project picker"),
		"",
		m.styles.subtitle.Render("Items"),
		m.styles.muted.Render("n                new item"),
		m.styles.muted.Render("e                edit selected item"),
		m.styles.muted.Render("s                sync GitHub"),
		m.styles.muted.Render("D                cycle all/archive/trash"),
		"",
		m.styles.subtitle.Render("Command"),
		m.styles.muted.Render(":                open command palette"),
		m.styles.muted.Render("/                search input"),
		m.styles.muted.Render(":delete          move selected item to trash"),
		m.styles.muted.Render(":restore         restore selected trash item"),
		m.styles.muted.Render(":purge           permanently delete trash item"),
		m.styles.muted.Render(":shortcuts       open this panel"),
		m.styles.muted.Render("esc              close panel"),
	}

	width := min(max(52, m.width-20), 74)
	height := min(max(14, len(lines)+2), m.height-8)
	panel := m.styles.panelFocused.
		Width(max(1, width-m.styles.panelFocused.GetHorizontalFrameSize()))
	return panel.Render(m.renderPaneContent(strings.Join(lines, "\n"), width, height, panel))
}

func (m modelUI) renderConfirmModal() string {
	width := min(max(44, m.width-24), 68)
	height := min(max(10, m.height-8), 12)
	panel := m.styles.panelFocused.
		Width(max(1, width-m.styles.panelFocused.GetHorizontalFrameSize()))
	innerWidth := max(1, width-panel.GetHorizontalFrameSize())
	innerHeight := max(1, height-panel.GetVerticalFrameSize())

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

	buttons := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.styles.confirmDangerButton.Render("(P)urge"),
		"  ",
		m.styles.confirmCancelButton.Render("(C)ancel"),
	)
	buttonLine := lipgloss.Place(innerWidth, 1, lipgloss.Center, lipgloss.Top, buttons)
	for len(lines) < max(0, innerHeight-1) {
		lines = append(lines, "")
	}
	lines = append(lines, buttonLine)

	return panel.Render(m.renderPaneContent(strings.Join(lines, "\n"), width, height, panel))
}

func (m modelUI) renderCommandOverlay(width int) string {
	matches := matchedCommandSuggestions(m.commandInput.Value(), m.commandSuggestions())
	if len(matches) <= 1 {
		return ""
	}

	selected := clamp(m.commandSuggestIndex, 0, len(matches)-1)
	start, end := commandSuggestionWindow(len(matches), selected, 5)
	lines := make([]string, 0, end-start)
	for idx := start; idx < end; idx++ {
		label := truncatePlain(matches[idx], max(12, width-10))
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
	menuWidth = min(max(28, menuWidth+4), max(28, width))

	box := m.styles.commandMenuBox.
		Width(max(1, menuWidth-m.styles.commandMenuBox.GetHorizontalFrameSize())).
		Render(strings.Join(lines, "\n"))
	return box
}

func (m modelUI) renderDetailPane(width, height int) string {
	panelStyle := m.panelStyle(m.styles.panel, width, height)
	if m.focus == focusDetails && m.mode == modeNormal {
		panelStyle = m.panelStyle(m.styles.panelFocused, width, height)
	}

	if m.mode == modeConflict {
		return panelStyle.Render(m.renderPaneContent(m.renderConflictView(), width, height, panelStyle))
	}

	if m.mode == modeEdit {
		return panelStyle.Render(m.renderPaneContent(m.renderEditView(), width, height, panelStyle))
	}

	if len(m.filtered) == 0 {
		return panelStyle.Render(m.renderPaneContent(strings.Join([]string{
			m.styles.subtitle.Render("Details"),
			"",
			m.styles.muted.Render("Create an item with `n`."),
			m.styles.muted.Render("The selected item appears here."),
			"",
			m.styles.subtitle.Render("What Lands Here"),
			m.styles.muted.Render("title"),
			m.styles.muted.Render("project"),
			m.styles.muted.Render("stage"),
			m.styles.muted.Render("body"),
			"",
			m.styles.subtitle.Render("Current Storage"),
			m.styles.muted.Render(m.storageSummary()),
		}, "\n"), width, height, panelStyle))
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

func (m modelUI) renderConflictView() string {
	if m.conflict == nil {
		return strings.Join([]string{
			m.styles.subtitle.Render("Conflict"),
			"",
			m.styles.muted.Render("No active conflict."),
		}, "\n")
	}

	local := m.conflict.local
	remote := m.conflict.remote

	lines := []string{
		m.styles.subtitle.Render("Conflict"),
		"",
		m.styles.muted.Render("GitHub changed since your last sync."),
		"",
		m.styles.subtitle.Render("Local"),
		m.styles.title.Render(local.Title),
		m.styles.muted.Render(fmt.Sprintf("%s  %s", local.Project, local.Stage)),
		m.styles.muted.Render(fmt.Sprintf("updated %s", local.UpdatedAt.Format(time.RFC822))),
		"",
		truncateBlock(local.Body, max(1, m.detailPaneWidth()-8), 4),
		"",
		m.styles.subtitle.Render("GitHub"),
		m.styles.title.Render(remote.Title),
		m.styles.muted.Render(fmt.Sprintf("%s  %s", remote.Project, remote.Stage)),
		m.styles.muted.Render(fmt.Sprintf("updated %s", remote.UpdatedAt.Format(time.RFC822))),
		"",
		truncateBlock(remote.Body, max(1, m.detailPaneWidth()-8), 4),
		"",
		m.styles.help.Render("r keep GitHub version"),
		m.styles.help.Render("o overwrite GitHub with local"),
		m.styles.help.Render("esc cancel"),
	}

	return strings.Join(lines, "\n")
}

func (m modelUI) renderDetailLines(item model.Item, width int) []string {
	lines := []string{
		m.styles.subtitle.Render("Details"),
		"",
		m.styles.title.Render(item.Title),
		"",
		m.styles.muted.Render(fmt.Sprintf("Created  %s", item.CreatedAt.Format(time.RFC822))),
		m.styles.muted.Render(fmt.Sprintf("Updated  %s", item.UpdatedAt.Format(time.RFC822))),
		m.renderIssueLine(item),
		m.styles.muted.Render(fmt.Sprintf("Repo     %s", item.Repo)),
		"",
		m.styles.subtitle.Render("Body"),
	}

	bodyLines := wrapPlainLines(item.Body, max(1, width))
	if len(bodyLines) == 0 {
		bodyLines = []string{""}
	}
	lines = append(lines, bodyLines...)
	lines = append(lines,
		"",
		m.styles.subtitle.Render("Labels"),
		strings.Join(m.renderLabels(item.Labels()), " "),
	)

	return lines
}

func (m modelUI) renderEditView() string {
	lines := []string{
		m.styles.subtitle.Render("Edit Item"),
		"",
		m.renderEditFieldBlock("Title", m.form.titleInput.View(), 0),
		m.renderEditFieldBlock("Project", m.form.projectInput.View(), 1),
		m.renderEditFieldBlock("Stage", m.renderStageOptions(), 2),
		"",
		m.renderEditRow("Body", "", 3),
		m.styles.editValue.PaddingLeft(0).Render(m.form.bodyInput.View()),
	}

	return strings.Join(lines, "\n")
}

func (m modelUI) renderCommandInputLine() string {
	line := m.commandInput.View()
	if suffix := m.commandCompletionSuffix(); suffix != "" && m.commandInput.Position() == len([]rune(m.commandInput.Value())) {
		line += m.styles.commandGhost.Render(suffix)
	}
	return line
}

func (m modelUI) footerHint() string {
	switch m.mode {
	case modeSetup:
		if m.setup.enteringRepo {
			return "enter save repo  esc back"
		}
		return "j/k move  enter select"
	case modeProjectPicker:
		return "enter apply  esc cancel"
	case modeShortcuts:
		return "esc close"
	case modeConfirm:
		return "p/enter purge  c/esc cancel"
	case modeConflict:
		return "r keep GitHub  o overwrite GitHub  esc cancel"
	case modeEdit:
		return "ctrl+s save  esc cancel"
	default:
		return ": command  ? shortcuts  tab projects  q quit"
	}
}

func (m modelUI) footerMeta() string {
	switch m.mode {
	case modeSetup, modeSearch, modeCommand, modeConfirm:
		return ""
	default:
		parts := []string{
			fmt.Sprintf("%d items", len(m.filtered)),
			fmt.Sprintf("view: %s", m.viewMode.String()),
			fmt.Sprintf("sort: %s %s", m.sortMode.String(), m.sortDirectionLabel()),
			fmt.Sprintf("mode: %s", m.storageModeLabel()),
		}
		if m.lastSearch != "" {
			parts = append(parts, fmt.Sprintf("search: %q", m.lastSearch))
		}
		return strings.Join(parts, "  ")
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
	valueWidth := max(10, m.detailPaneWidth()-labelWidth-separatorWidth-6)
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
		style = m.styles.stageIdeaText
	case model.StagePlanned:
		style = m.styles.stagePlannedText
	case model.StageActive:
		style = m.styles.stageActiveText
	case model.StageBlocked:
		style = m.styles.stageBlockedText
	case model.StageDone:
		style = m.styles.stageDoneText
	default:
		style = m.styles.muted
	}

	return style.
		Bold(false).
		Underline(false).
		Background(lipgloss.NoColor{})
}

func (m modelUI) renderProjectLabel(label string) string {
	return m.styles.label.Render(label)
}

func (m modelUI) renderLabels(labels []string) []string {
	rendered := make([]string, 0, len(labels))
	for _, label := range labels {
		if label == "trashed" {
			rendered = append(rendered, m.styles.labelMuted.Render(label))
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
	return m.styles.muted.Render("Issue    local-only")
}

func (m modelUI) renderItemRow(item model.Item, width int, selected bool) string {
	rowWidth := max(10, width-8)

	title := truncate(item.Title, rowWidth)
	if selected {
		title = m.styles.selected.Render(title)
	}

	dateText := item.UpdatedAt.Format("2006-01-02")
	stageText := m.renderStageText(item.Stage, false)
	stageWidth := lipgloss.Width(stageText)
	dateWidth := lipgloss.Width(dateText)
	projectWidth := max(4, rowWidth-stageWidth-dateWidth-4)
	projectText := truncatePlain(item.Project, projectWidth)

	metaRendered := lipgloss.JoinHorizontal(
		lipgloss.Left,
		m.styles.subtitle.Render(projectText),
		"  ",
		m.styles.muted.Render(dateText),
		"  ",
		stageText,
	)

	return strings.Join([]string{title, metaRendered}, "\n")
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
	contentBox := m.renderContentBox(content, contentWidth, innerHeight)
	scrollbar := m.renderScrollbar(innerHeight, scroll)

	return lipgloss.JoinHorizontal(lipgloss.Top, contentBox, " ", scrollbar)
}

func (m modelUI) renderPaneContent(content string, width, height int, panelStyle lipgloss.Style) string {
	innerWidth := max(1, width-panelStyle.GetHorizontalFrameSize())
	innerHeight := max(1, height-panelStyle.GetVerticalFrameSize())
	return m.renderContentBox(content, innerWidth, innerHeight)
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
	contentHeight := max(12, m.height-7)
	detailInnerWidth := max(20, detailWidth-m.styles.panel.GetHorizontalFrameSize())
	detailInnerHeight := max(10, contentHeight-m.styles.panel.GetVerticalFrameSize())
	inputWidth := max(20, detailInnerWidth-11)

	m.form.titleInput.Width = inputWidth
	m.form.projectInput.Width = inputWidth
	m.form.bodyInput.SetWidth(max(20, detailInnerWidth-2))
	m.form.bodyInput.SetHeight(max(4, detailInnerHeight-13))

	setupWidth := max(24, min(40, m.width-20))
	m.setup.repoInput.Width = setupWidth
}

func (m modelUI) layoutWidths() (int, int) {
	total := max(60, m.width-4)
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

func (m modelUI) projectOptions() []string {
	seen := map[string]struct{}{}
	projects := []string{allProjectsLabel}
	for _, item := range m.items {
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
		m.beginEdit(-1)
		return m.focusFormField()
	case "edit":
		if len(m.filtered) == 0 {
			return m.setStatus("No item selected."), nil
		}
		m.beginEdit(m.filtered[m.selected])
		return m.focusFormField()
	case "sync":
		if m.config.StorageMode == config.ModeGitHub {
			return m.syncNow(), nil
		}
		return m.setStatus("Local mode is already current."), nil
	case "delete", "trash":
		return m.runDeleteCommand(), nil
	case "restore":
		return m.runRestoreCommand(), nil
	case "purge":
		return m.runPurgeCommand(), nil
	case "quit", "exit":
		return m, tea.Quit
	case "shortcuts", "help":
		m.mode = modeShortcuts
		return m, nil
	case "search":
		return m.runSearchCommand(strings.TrimSpace(command[len(parts[0]):])), nil
	case "project":
		return m.runProjectCommand(strings.TrimSpace(command[len(parts[0]):])), nil
	case "storage":
		return m.runStorageCommand(parts[1:]), nil
	case "view":
		return m.runViewCommand(parts[1:]), nil
	case "sort":
		return m.runSortCommand(parts[1:]), nil
	default:
		return m.setStatus(fmt.Sprintf("Unknown command: %s", command)), nil
	}
}

func (m modelUI) runSearchCommand(query string) tea.Model {
	query = strings.TrimSpace(query)
	if query == "" {
		return m.setStatus("Usage: search <query> | search clear")
	}
	if strings.EqualFold(query, "clear") {
		m.lastSearch = ""
		m.rebuildFiltered()
		return m.setStatus("Search cleared.")
	}

	m.lastSearch = query
	m.rebuildFiltered()
	return m.setStatus(fmt.Sprintf("Search set to %q.", m.lastSearch))
}

func (m modelUI) runProjectCommand(project string) tea.Model {
	project = strings.TrimSpace(project)
	if project == "" {
		return m.setStatus("Usage: project all | project <name>")
	}

	if strings.EqualFold(project, "all") {
		m.projectFilter = allProjectsLabel
		m.rebuildFiltered()
		return m.setStatus("Project set to all.")
	}

	for _, option := range m.projectOptions() {
		if option == allProjectsLabel {
			continue
		}
		if strings.EqualFold(option, project) {
			m.projectFilter = option
			m.rebuildFiltered()
			return m.setStatus(fmt.Sprintf("Project set to %s.", option))
		}
	}

	return m.setStatus(fmt.Sprintf("Unknown project: %s", project))
}

func (m modelUI) runViewCommand(args []string) tea.Model {
	if len(args) == 0 {
		return m.setStatus("Usage: view all | view archive | view trash")
	}

	switch args[0] {
	case "all", "active":
		m.viewMode = viewActive
		m.rebuildFiltered()
		return m.setStatus("Switched to all items.")
	case "archive":
		m.viewMode = viewArchive
		m.rebuildFiltered()
		return m.setStatus("Switched to archive.")
	case "trash":
		m.viewMode = viewTrash
		m.rebuildFiltered()
		return m.setStatus("Switched to trash.")
	default:
		return m.setStatus("Usage: view all | view archive | view trash")
	}
}

func (m modelUI) runDeleteCommand() tea.Model {
	itemIndex, ok := m.selectedItemIndex()
	if !ok {
		return m.setStatus("No item selected.")
	}

	item := m.items[itemIndex]
	if item.IsTrashed() {
		return m.setStatus("Item is already in trash.")
	}

	updated := item
	updated.Trashed = true
	updated.UpdatedAt = time.Now()

	if m.config.StorageMode == config.ModeGitHub {
		saved, err := m.pushEditedItem(updated)
		if err != nil {
			return m.setStatus(userFacingError("Delete failed", err))
		}
		updated = saved
	}

	m.items[itemIndex] = updated
	if err := m.persistItems(); err != nil {
		return m.setStatus(fmt.Sprintf("Delete failed: %v", err))
	}

	m.detailScroll = 0
	m.rebuildFiltered()
	return m.setStatus("Moved item to trash.")
}

func (m modelUI) runRestoreCommand() tea.Model {
	itemIndex, ok := m.selectedItemIndex()
	if !ok {
		return m.setStatus("No item selected.")
	}

	item := m.items[itemIndex]
	if !item.IsTrashed() {
		return m.setStatus("Selected item is not in trash.")
	}

	updated := item
	updated.Trashed = false
	updated.UpdatedAt = time.Now()

	if m.config.StorageMode == config.ModeGitHub {
		saved, err := m.pushEditedItem(updated)
		if err != nil {
			return m.setStatus(userFacingError("Restore failed", err))
		}
		updated = saved
	}

	m.items[itemIndex] = updated
	if err := m.persistItems(); err != nil {
		return m.setStatus(fmt.Sprintf("Restore failed: %v", err))
	}

	m.detailScroll = 0
	m.rebuildFiltered()
	if updated.IsDone() {
		return m.setStatus("Restored item to archive.")
	}
	return m.setStatus("Restored item.")
}

func (m modelUI) runPurgeCommand() tea.Model {
	itemIndex, ok := m.selectedItemIndex()
	if !ok {
		return m.setStatus("No item selected.")
	}
	if !m.items[itemIndex].IsTrashed() {
		return m.setStatus("Purge is only available in trash.")
	}

	m.mode = modeConfirm
	m.confirm = &confirmState{
		action:    confirmPurge,
		itemIndex: itemIndex,
	}
	return m
}

func (m modelUI) runSortCommand(args []string) tea.Model {
	if len(args) == 0 {
		return m.setStatus("Usage: sort updated|created asc|desc")
	}

	switch args[0] {
	case "updated":
		m.sortMode = sortUpdated
	case "created":
		m.sortMode = sortCreated
	default:
		return m.setStatus("Usage: sort updated|created asc|desc")
	}

	m.sortAscending = false
	if len(args) > 1 {
		switch args[1] {
		case "asc":
			m.sortAscending = true
		case "desc":
			m.sortAscending = false
		default:
			return m.setStatus("Usage: sort updated|created asc|desc")
		}
	}

	m.rebuildFiltered()
	return m.setStatus(fmt.Sprintf("Sorting by %s %s.", m.sortMode.String(), m.sortDirectionLabel()))
}

func (m modelUI) runStorageCommand(args []string) tea.Model {
	if len(args) == 0 {
		return m.setStatus("Usage: storage local | storage github owner/repo")
	}

	switch args[0] {
	case "local":
		cfg := m.config
		cfg.StorageMode = config.ModeLocal
		cfg.Repo = ""
		if err := m.saveConfigAndApply(cfg); err != nil {
			return m.setStatus(fmt.Sprintf("Storage switch failed: %v", err))
		}
		return m.setStatus("Switched to local storage.")
	case "github":
		if len(args) < 2 {
			return m.setStatus("Usage: storage github owner/repo")
		}
		repo := strings.TrimSpace(args[1])
		if !strings.Contains(repo, "/") {
			return m.setStatus("Repository must be in owner/repo form.")
		}

		cfg := m.config
		cfg.StorageMode = config.ModeGitHub
		cfg.Repo = repo
		if err := m.saveConfigAndApply(cfg); err != nil {
			return m.setStatus(fmt.Sprintf("Storage switch failed: %v", err))
		}

		synced := m.syncNow()
		if updated, ok := synced.(modelUI); ok {
			return updated
		}
		return synced
	default:
		return m.setStatus("Usage: storage local | storage github owner/repo")
	}
}

func (m *modelUI) applyConfig(cfg config.AppConfig) {
	m.config = cfg
	m.store = storage.NewJSONStore(cfg.DataFile)
	m.githubClient = githubsync.NewClient()
	if m.mode == modeSetup {
		m.mode = modeNormal
	}
}

func (m *modelUI) loadItems() error {
	if m.store == nil {
		return nil
	}

	var syncErr error
	if m.config.StorageMode == config.ModeGitHub && m.githubClient != nil {
		items, err := m.githubClient.SyncRepo(m.config.Repo)
		if err == nil {
			m.items = items
			return m.store.SaveItems(m.items)
		}
		syncErr = err
	}

	items, ok, err := m.store.LoadItems()
	if err != nil {
		return err
	}
	if !ok {
		m.items = []model.Item{}
		if err := m.store.SaveItems(m.items); err != nil {
			return err
		}
		return syncErr
	}

	m.items = items
	return syncErr
}

func (m modelUI) persistItems() error {
	if m.store == nil {
		return fmt.Errorf("store is not configured")
	}
	return m.store.SaveItems(m.items)
}

func (m modelUI) buildEditedItem(title, project, body string, stage model.Stage, now time.Time) model.Item {
	if m.form.isNew {
		return model.Item{
			Title:           title,
			Project:         project,
			Stage:           stage,
			Body:            body,
			CreatedAt:       now,
			UpdatedAt:       now,
			IssueNumber:     0,
			Repo:            m.itemRepoValue(),
			RemoteUpdatedAt: time.Time{},
		}
	}

	item := m.items[m.form.editingIndex]
	item.Title = title
	item.Project = project
	item.Stage = stage
	item.Body = body
	item.UpdatedAt = now
	return item
}

func (m modelUI) pushEditedItem(item model.Item) (model.Item, error) {
	if m.githubClient == nil {
		return model.Item{}, fmt.Errorf("github client is not configured")
	}

	return m.githubClient.UpsertItem(m.config.Repo, item)
}

func (m modelUI) enterConflict(conflictErr *githubsync.ConflictError, local model.Item) tea.Model {
	m.mode = modeConflict
	m.focus = focusDetails
	m.detailScroll = 0
	m.conflict = &conflictState{
		local:        local,
		remote:       conflictErr.Remote,
		editingIndex: m.form.editingIndex,
		isNew:        m.form.isNew,
	}
	return m.setStatus(conflictErr.Error())
}

func (m modelUI) resolveConflictWithRemote() tea.Model {
	if m.conflict == nil {
		m.mode = modeNormal
		return m
	}

	remoteTitle := m.conflict.remote.Title

	if !m.conflict.isNew && m.conflict.editingIndex >= 0 && m.conflict.editingIndex < len(m.items) {
		m.items[m.conflict.editingIndex] = m.conflict.remote
	}

	if err := m.persistItems(); err != nil {
		return m.setStatus(fmt.Sprintf("Conflict save failed: %v", err))
	}

	m.mode = modeNormal
	m.conflict = nil
	m.detailScroll = 0
	m.rebuildFiltered()
	m.selectByTitle(remoteTitle)
	return m.setStatus("Kept the GitHub version.")
}

func (m modelUI) resolveConflictByOverwriting() tea.Model {
	if m.conflict == nil {
		m.mode = modeNormal
		return m
	}

	saved, err := m.githubClient.ForceUpsertItem(m.config.Repo, m.conflict.local)
	if err != nil {
		return m.setStatus(userFacingError("Overwrite failed", err))
	}

	if m.conflict.isNew {
		m.items = append([]model.Item{saved}, m.items...)
	} else if m.conflict.editingIndex >= 0 && m.conflict.editingIndex < len(m.items) {
		m.items[m.conflict.editingIndex] = saved
	}

	if err := m.persistItems(); err != nil {
		return m.setStatus(fmt.Sprintf("Conflict save failed: %v", err))
	}

	m.mode = modeNormal
	m.conflict = nil
	m.detailScroll = 0
	m.rebuildFiltered()
	m.selectByTitle(saved.Title)
	return m.setStatus("Overwrote GitHub with the local version.")
}

func (m modelUI) syncNow() tea.Model {
	if m.githubClient == nil {
		return m.setStatus("GitHub client is not configured.")
	}

	items, err := m.githubClient.SyncRepo(m.config.Repo)
	if err != nil {
		return m.setStatus(userFacingError("Sync failed", err))
	}

	m.items = items
	if err := m.persistItems(); err != nil {
		return m.setStatus(fmt.Sprintf("Sync save failed: %v", err))
	}

	m.rebuildFiltered()
	return m.setStatus(fmt.Sprintf("Synced %d issues from %s.", len(items), m.config.Repo))
}

func (m modelUI) finishSetup(storageMode, repo string) tea.Model {
	if m.configManager == nil {
		return m.setStatus("Config manager is unavailable.")
	}

	dataFile, err := config.DefaultDataFile()
	if err != nil {
		return m.setStatus(fmt.Sprintf("Setup failed: %v", err))
	}

	cfg := config.AppConfig{
		StorageMode: storageMode,
		Repo:        repo,
		DataFile:    dataFile,
	}
	if err := m.saveConfigAndApply(cfg); err != nil {
		return m.setStatus(fmt.Sprintf("Setup failed: %v", err))
	}

	m.mode = modeNormal
	m.setup.enteringRepo = false
	m.setup.repoInput.Blur()
	m.rebuildFiltered()
	if storageMode == config.ModeGitHub {
		return m.syncNow()
	}
	m.postLoadStatus()
	return m
}

func (m *modelUI) saveConfigAndApply(cfg config.AppConfig) error {
	if m.configManager == nil {
		return fmt.Errorf("config manager is unavailable")
	}
	if err := m.configManager.Save(cfg); err != nil {
		return err
	}

	m.applyConfig(cfg)
	if err := m.persistItems(); err != nil {
		return err
	}
	return nil
}

func (m *modelUI) postLoadStatus() {
	switch m.config.StorageMode {
	case config.ModeGitHub:
		m.statusMessage = fmt.Sprintf("GitHub mode active for %s.", m.config.Repo)
	case config.ModeLocal:
		m.statusMessage = fmt.Sprintf("Local items loaded from %s.", m.store.Path())
	default:
		m.statusMessage = "Storage loaded."
	}
	m.statusUntil = time.Now().Add(6 * time.Second)
}

func (m modelUI) itemRepoValue() string {
	if m.config.StorageMode == config.ModeGitHub && m.config.Repo != "" {
		return m.config.Repo
	}
	return "local"
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
		if m.config.Repo != "" {
			return "GitHub cache: " + m.config.Repo
		}
		return "GitHub cache"
	case config.ModeLocal:
		return "Local JSON file"
	default:
		return "Not configured"
	}
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

func (m modelUI) setStatus(message string) tea.Model {
	m.statusMessage = message
	m.statusUntil = time.Now().Add(4 * time.Second)
	return m
}

func userFacingError(action string, err error) string {
	if message := githubsync.UserMessage(err); message != "" {
		return message
	}
	return fmt.Sprintf("%s: %v", action, err)
}

func stageIndex(stage model.Stage) int {
	for idx, candidate := range model.Stages {
		if candidate == stage {
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
		fitted = append(fitted, truncatePlain(line, width))
	}

	for len(fitted) < height {
		fitted = append(fitted, "")
	}

	return strings.Join(fitted, "\n")
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

func baseCommandSuggestions() []string {
	return []string{
		"new",
		"edit",
		"delete",
		"restore",
		"purge",
		"sync",
		"shortcuts",
		"search ",
		"search clear",
		"project all",
		"view all",
		"view archive",
		"view trash",
		"sort updated desc",
		"sort updated asc",
		"sort created desc",
		"sort created asc",
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
	}
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
	if strings.TrimSpace(before) == "" {
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

func (m modelUI) selectedItemIndex() (int, bool) {
	if len(m.filtered) == 0 || m.selected < 0 || m.selected >= len(m.filtered) {
		return 0, false
	}
	return m.filtered[m.selected], true
}

func (m modelUI) confirmActionNow() tea.Model {
	if m.confirm == nil {
		m.mode = modeNormal
		return m
	}

	switch m.confirm.action {
	case confirmPurge:
		return m.performPurge()
	default:
		m.mode = modeNormal
		m.confirm = nil
		return m
	}
}

func (m modelUI) performPurge() tea.Model {
	if m.confirm == nil {
		m.mode = modeNormal
		return m
	}

	itemIndex := m.confirm.itemIndex
	if itemIndex < 0 || itemIndex >= len(m.items) {
		m.mode = modeNormal
		m.confirm = nil
		m.rebuildFiltered()
		return m.setStatus("Purge target no longer exists.")
	}

	item := m.items[itemIndex]
	if m.config.StorageMode == config.ModeGitHub {
		if err := m.githubClient.DeleteIssue(m.config.Repo, item.IssueNumber); err != nil {
			m.mode = modeNormal
			m.confirm = nil
			return m.setStatus(userFacingError("Purge failed", err))
		}
	}

	m.items = append(m.items[:itemIndex], m.items[itemIndex+1:]...)
	if err := m.persistItems(); err != nil {
		m.mode = modeNormal
		m.confirm = nil
		return m.setStatus(fmt.Sprintf("Purge failed: %v", err))
	}

	m.mode = modeNormal
	m.confirm = nil
	m.detailScroll = 0
	m.rebuildFiltered()
	return m.setStatus("Item purged permanently.")
}
