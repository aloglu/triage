package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aloglu/triage/internal/engine"
	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/issue"
	"github.com/aloglu/triage/internal/store"
)

type (
	refreshDoneMsg struct {
		results []engine.RefreshResult
		err     error
	}
	flushDoneMsg struct {
		result engine.FlushResult
		err    error
	}
	meMsg       struct{ login string }
	commentsMsg struct {
		key      string
		comments []store.Comment
		err      error
	}
	tickMsg       time.Time
	retryFlushMsg struct{}
	clearFlashMsg struct{ id int }
	reposMsg      struct {
		repos []gh.RepoSummary
		err   error
	}
	openedMsg struct{ err error }
)

const offlineRetry = 30 * time.Second

// repoRefreshedMsg reports one repo's refresh; refreshDoneMsg follows once
// every repo has reported.
type repoRefreshedMsg struct {
	result engine.RefreshResult
	err    error
}

// refreshSlots limits how many repos refresh at once, to stay well within
// GitHub's limits on concurrent requests.
var refreshSlots = make(chan struct{}, 4)

func refreshRepoCmd(eng *engine.Engine, repo string, full bool) tea.Cmd {
	return func() tea.Msg {
		refreshSlots <- struct{}{}
		defer func() { <-refreshSlots }()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		result, err := eng.Refresh(ctx, repo, full)
		return repoRefreshedMsg{result: result, err: err}
	}
}

func flushCmd(eng *engine.Engine) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		result, err := eng.Flush(ctx)
		return flushDoneMsg{result: result, err: err}
	}
}

func meCmd(eng *engine.Engine) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return meMsg{login: eng.Me(ctx)}
	}
}

func commentsCmd(eng *engine.Engine, i issue.Issue) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		comments, err := eng.Comments(ctx, i)
		return commentsMsg{key: i.Key(), comments: comments, err: err}
	}
}

func tickCmd(every time.Duration) tea.Cmd {
	return after(every, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func userReposCmd(client *gh.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		repos, err := client.ListUserRepos(ctx, 300)
		return reposMsg{repos: repos, err: err}
	}
}

func openURLCmd(open func(string) error, url string) tea.Cmd {
	return func() tea.Msg { return openedMsg{err: open(url)} }
}

// startRefresh fetches updates for every tracked repo unless a refresh is
// already running.
func (m *Model) startRefresh() tea.Cmd { return m.refresh(false) }

// startFullRefresh also checks for issues deleted on GitHub. It runs at
// startup and when the user asks for a refresh.
func (m *Model) startFullRefresh() tea.Cmd { return m.refresh(true) }

func (m *Model) refresh(full bool) tea.Cmd {
	if !m.eng.Online() || len(m.env.Config.Repos) == 0 {
		return nil
	}
	if m.refreshing {
		// The repo list may have changed; go again when this one finishes.
		m.refreshAgain = true
		m.fullAgain = m.fullAgain || full
		return nil
	}
	m.refreshing = true
	m.refreshTotal, m.refreshDone = len(m.env.Config.Repos), 0
	m.refreshResults, m.refreshErrs = nil, nil
	cmds := []tea.Cmd{m.startSpinner()}
	for _, repo := range m.env.Config.Repos {
		cmds = append(cmds, refreshRepoCmd(m.eng, repo, full))
	}
	return tea.Batch(cmds...)
}

// handleRepoRefreshed collects per-repo results; when the last repo
// reports, the refresh as a whole is done.
func (m *Model) handleRepoRefreshed(msg repoRefreshedMsg) tea.Cmd {
	m.refreshDone++
	m.refreshResults = append(m.refreshResults, msg.result)
	if msg.err != nil {
		m.refreshErrs = append(m.refreshErrs, msg.err)
	}
	if m.refreshDone < m.refreshTotal {
		return nil
	}
	_, cmd := m.update(refreshDoneMsg{results: m.refreshResults, err: errors.Join(m.refreshErrs...)})
	return cmd
}

// refreshProgress returns how far the current refresh is, from 0 to 1.
func (m *Model) refreshProgress() float64 {
	if m.refreshTotal == 0 {
		return 0
	}
	return float64(m.refreshDone) / float64(m.refreshTotal)
}

// startFlush sends queued changes; a flush requested while one runs is
// remembered and started when it finishes.
func (m *Model) startFlush() tea.Cmd {
	if !m.eng.Online() {
		return nil
	}
	if m.flushing {
		m.flushAgain = true
		return nil
	}
	m.flushing = true
	return tea.Batch(flushCmd(m.eng), m.startSpinner())
}

func (m *Model) handleRefreshDone(msg refreshDoneMsg) tea.Cmd {
	m.refreshing = false
	changed := 0
	for _, r := range msg.results {
		changed += r.Changed
	}
	if msg.err != nil {
		m.syncErr = msg.err
		m.offline = gh.IsOffline(msg.err)
		if !m.offline {
			m.flash(gh.UserMessage(msg.err), flashError)
		}
	} else {
		m.syncErr = nil
		m.offline = false
		m.lastSync = time.Now()
	}
	if changed > 0 || msg.err == nil {
		m.reload()
	}
	if m.loadingFirst && msg.err == nil {
		m.loadingFirst = false
	}
	if m.manualRefresh && !m.refreshAgain {
		m.manualRefresh = false
		switch {
		case msg.err != nil && m.offline:
			m.flash("Can't reach GitHub right now.", flashWarn)
		case msg.err != nil:
			// Already reported above.
		case changed == 0:
			m.flash("Up to date.", flashOK)
		case changed == 1:
			m.flash("1 issue updated.", flashOK)
		default:
			m.flash(fmt.Sprintf("%d issues updated.", changed), flashOK)
		}
	}
	if m.refreshAgain {
		full := m.fullAgain
		m.refreshAgain, m.fullAgain = false, false
		return m.refresh(full)
	}
	return nil
}

func (m *Model) handleFlushDone(msg flushDoneMsg) tea.Cmd {
	m.flushing = false
	var cmds []tea.Cmd
	r := msg.result
	switch {
	case msg.err != nil:
		m.flash(gh.UserMessage(msg.err), flashError)
	case r.Offline:
		m.offline = true
		cmds = append(cmds, after(offlineRetry, func(time.Time) tea.Msg { return retryFlushMsg{} }))
	case r.Held > 0 && r.Err != nil:
		m.offline = false
		m.flash(gh.UserMessage(r.Err)+" Open the issue to retry or discard.", flashError)
	default:
		if r.Sent > 0 {
			m.offline = false
		}
	}
	if r.Dropped > 0 {
		cmds = append(cmds, m.flash(fmt.Sprintf("Dropped %d change(s) to issues that were deleted on GitHub.", r.Dropped), flashWarn))
	}
	if r.Sent > 0 || r.Held > 0 || r.Dropped > 0 {
		m.reload()
	}
	if m.flushAgain {
		m.flushAgain = false
		cmds = append(cmds, m.startFlush())
	}
	return tea.Batch(cmds...)
}
