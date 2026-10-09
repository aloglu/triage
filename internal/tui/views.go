package tui

import (
	"strings"
	"time"

	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/issue"
)

// view is a named saved filter shown as a tab.
type view struct {
	name  string
	query string
	// repo is set for per-repo views.
	repo string
}

func (m *Model) rebuildViews() {
	current := ""
	if m.viewIdx < len(m.views) {
		current = m.views[m.viewIdx].name
	}
	m.views = []view{
		{name: "Inbox", query: "is:open"},
		{name: "Mine", query: "is:open assignee:@me"},
	}
	for _, repo := range m.env.Config.Repos {
		m.views = append(m.views, view{name: gh.RepoName(repo), query: "is:open repo:" + repo, repo: repo})
	}
	for _, custom := range m.env.Config.Views {
		m.views = append(m.views, view{name: custom.Name, query: custom.Query})
	}
	m.views = append(m.views, view{name: "Closed", query: "is:closed"})
	m.viewIdx = 0
	for i, v := range m.views {
		if v.name == current {
			m.viewIdx = i
		}
	}
}

func (m *Model) setView(idx int) {
	n := len(m.views)
	m.viewIdx = (idx%n + n) % n
	m.cursor, m.offset = 0, 0
	m.boardRow = [boardColumnCount]int{}
	m.detail.GotoTop()
	m.refilter("")
}

func (m *Model) viewByRepo(repo string) int {
	for i, v := range m.views {
		if strings.EqualFold(v.repo, repo) {
			return i
		}
	}
	return -1
}

func (m *Model) currentQuery() issue.Query {
	q := issue.ParseQuery(m.views[m.viewIdx].query)
	return q.And(issue.ParseQuery(m.filterInput.Value()))
}

// refilter recomputes the visible issues and puts the cursor back on
// keepKey when it's still visible.
func (m *Model) refilter(keepKey string) {
	q := m.currentQuery()
	// A status filter decides open/closed by itself.
	if q.Has("status") {
		q = q.Without("is")
	}
	ctx := m.matchContext()
	m.visible = m.visible[:0]
	for _, i := range m.snap.Issues {
		if q.Match(i, ctx) {
			m.visible = append(m.visible, i)
		}
	}
	if keepKey != "" {
		for idx, i := range m.visible {
			if i.Key() == keepKey {
				m.cursor = idx
				break
			}
		}
	}
	if m.cursor >= len(m.visible) {
		m.cursor = max(0, len(m.visible)-1)
	}
	m.detailKey = ""
}

const boardColumnCount = 5

// boardColumns are the board's columns in workflow order.
var boardColumns = [boardColumnCount]issue.Status{issue.StatusIdea, issue.StatusTodo, issue.StatusInProgress, issue.StatusBlocked, issue.StatusDone}

// boardDoneWindow is how long closed issues stay on the board.
const boardDoneWindow = 14 * 24 * time.Hour

// boardIssues groups the current view's issues (open and recently closed)
// into board columns.
func (m *Model) boardIssues() [][]issue.Issue {
	q := m.currentQuery().Without("is").Without("status")
	ctx := m.matchContext()
	conv := m.eng.Convention()
	columns := make([][]issue.Issue, len(boardColumns))
	cutoff := time.Now().Add(-boardDoneWindow)
	for _, i := range m.snap.Issues {
		if !q.Match(i, ctx) {
			continue
		}
		status := conv.StatusOf(i)
		if status == issue.StatusWontDo || (status == issue.StatusDone && i.UpdatedAt.Before(cutoff)) {
			continue
		}
		for c, col := range boardColumns {
			if col == status {
				columns[c] = append(columns[c], i)
			}
		}
	}
	return columns
}

func (m *Model) toggleBoard() {
	selected := m.selectedKey()
	m.board = !m.board
	m.fullDetail = false
	m.focus = focusList
	if m.board {
		for c, col := range m.boardIssues() {
			for r, i := range col {
				if i.Key() == selected {
					m.boardCol, m.boardRow[c] = c, r
				}
			}
		}
		return
	}
	m.refilter(selected)
}

// nextStatus steps along the main workflow: Idea → Todo → In progress → Done.
func nextStatus(s issue.Status, forward bool) issue.Status {
	flow := []issue.Status{issue.StatusIdea, issue.StatusTodo, issue.StatusInProgress, issue.StatusDone}
	pos := map[issue.Status]int{
		issue.StatusIdea: 0, issue.StatusTodo: 1, issue.StatusInProgress: 2, issue.StatusBlocked: 2,
		issue.StatusDone: 3, issue.StatusWontDo: 3,
	}[s]
	if s == issue.StatusBlocked {
		// Unblocking resumes work; going back from blocked returns to todo.
		if forward {
			return issue.StatusInProgress
		}
		return issue.StatusTodo
	}
	if forward {
		pos = min(pos+1, len(flow)-1)
	} else {
		pos = max(pos-1, 0)
	}
	return flow[pos]
}
