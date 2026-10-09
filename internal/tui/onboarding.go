package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aloglu/triage/internal/gh"
)

type onboardingStep int

const (
	stepSignIn onboardingStep = iota
	stepPickRepos
	stepFetching
)

// onboarding walks a new user from nothing to a populated inbox: check the
// GitHub login, pick repos, fetch their issues.
type onboarding struct {
	step   onboardingStep
	picker *picker
	err    error
	// adding is set when the user opened this from the palette to track
	// more repos, rather than on first run.
	adding  bool
	cwd     string
	loading bool
	repos   []gh.RepoSummary
}

func newOnboarding(m *Model) *onboarding {
	o := &onboarding{}
	if m.env.CurrentRepo != nil {
		if repo, err := m.env.CurrentRepo(); err == nil {
			o.cwd = repo
		}
	}
	return o
}

func (o *onboarding) init(m *Model) tea.Cmd {
	if m.env.Client == nil {
		o.step = stepSignIn
		return nil
	}
	// Show the picker right away so typing isn't lost while repos load.
	o.step = stepPickRepos
	o.loading = true
	o.buildPicker(m, nil)
	return tea.Batch(userReposCmd(m.env.Client), meCmd(m.eng))
}

func (o *onboarding) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case reposMsg:
		o.loading = false
		o.err = msg.err
		o.repos = msg.repos
		o.buildPicker(m, msg.repos)
		return nil
	case tea.KeyPressMsg:
		switch o.step {
		case stepSignIn:
			switch msg.String() {
			case "r":
				client, err := gh.New()
				if err != nil {
					o.err = err
					return nil
				}
				m.env.SetClient(client)
				m.eng = m.env.Engine
				return o.init(m)
			case "q", "ctrl+c", "esc":
				return o.quit(m)
			}
		case stepPickRepos:
			if msg.String() == "ctrl+c" || (msg.String() == "esc" && o.picker.input.Value() == "") {
				return o.quit(m)
			}
			_, cmd := o.picker.update(m, msg)
			return cmd
		case stepFetching:
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				return tea.Quit
			}
		}
	default:
		if o.step == stepPickRepos && o.picker != nil {
			_, cmd := o.picker.update(m, msg)
			return cmd
		}
	}
	return nil
}

// quit leaves onboarding: back to the app when adding repos, else exit.
func (o *onboarding) quit(m *Model) tea.Cmd {
	if o.adding && len(m.env.Config.Repos) > 0 {
		m.onboarding = nil
		return nil
	}
	return tea.Quit
}

// buildPicker (re)creates the repo picker, keeping what the user already
// typed and selected.
func (o *onboarding) buildPicker(m *Model, repos []gh.RepoSummary) {
	previous := o.picker
	wasSelected := map[string]bool{}
	if previous != nil {
		for _, item := range previous.items {
			wasSelected[strings.ToLower(item.id)] = item.selected
		}
	}
	var items []pickerItem
	seen := map[string]bool{}
	add := func(name, hint string, selected bool) {
		if seen[strings.ToLower(name)] {
			return
		}
		seen[strings.ToLower(name)] = true
		if was, ok := wasSelected[strings.ToLower(name)]; ok {
			selected = was
		}
		items = append(items, pickerItem{id: name, label: name, hint: hint, selected: selected, color: m.th.repoColor(name)})
	}
	if o.cwd != "" && !m.env.Config.HasRepo(o.cwd) {
		add(o.cwd, "this directory", true)
	}
	for _, repo := range repos {
		if m.env.Config.HasRepo(repo.FullName) {
			continue
		}
		hint := fmt.Sprintf("%d open", repo.OpenIssues)
		if repo.Description != "" {
			hint += " · " + repo.Description
		}
		add(repo.FullName, hint, false)
	}
	p := newPicker(m, "Which repos should triage track?", items)
	if previous != nil {
		p.input.SetValue(previous.input.Value())
		p.input.CursorEnd()
		p.refilter()
	}
	p.multi = true
	p.allowCreate = true
	p.createLabel = "Track"
	p.createValid = gh.ValidRepo
	p.note = "Type owner/name to add a repo that isn't listed. You can change this later."
	if o.loading {
		p.note = "Loading your repositories…"
	}
	p.onDone = func(m *Model, chosen []pickerItem, created string) tea.Cmd {
		var picked []string
		for _, item := range chosen {
			picked = append(picked, item.id)
		}
		if created != "" {
			picked = append(picked, created)
		}
		if len(picked) == 0 {
			o.err = fmt.Errorf("choose at least one repo (tab to select, enter to continue)")
			o.buildPicker(m, o.repos)
			return nil
		}
		for _, repo := range picked {
			normalized, err := gh.NormalizeRepo(repo)
			if err != nil {
				o.err = err
				o.buildPicker(m, o.repos)
				return nil
			}
			m.env.Config.AddRepo(normalized)
		}
		if err := m.saveConfig(); err != nil {
			o.err = err
			return nil
		}
		o.step = stepFetching
		m.loadingFirst = true
		return m.startRefresh()
	}
	o.picker = p
}

// refreshed finishes onboarding once the first fetch completes.
func (o *onboarding) refreshed(m *Model, msg refreshDoneMsg) tea.Cmd {
	if o.step != stepFetching {
		return nil
	}
	m.onboarding = nil
	m.loadingFirst = false
	cmds := []tea.Cmd{m.selectStartView(), m.startFlush()}
	if !o.adding {
		// First run: the periodic refresh starts now.
		cmds = append(cmds, tickCmd(m.env.Config.RefreshInterval()), meCmd(m.eng))
	}
	if msg.err != nil {
		return tea.Batch(append(cmds, m.flash(gh.UserMessage(msg.err), flashError))...)
	}
	total := 0
	for _, r := range msg.results {
		total += r.Changed
	}
	return tea.Batch(append(cmds, m.flash(fmt.Sprintf("Fetched %d issues. Press ? any time for shortcuts.", total), flashOK))...)
}

func (o *onboarding) view(m *Model) string {
	th := m.th
	width := min(72, m.width-4)
	var b strings.Builder
	if !o.adding {
		b.WriteString(th.title.Render("Welcome to triage") + "\n\n")
		b.WriteString(th.subtle.Render("A fast keyboard home for the issues in your GitHub repos.") + "\n")
		b.WriteString(th.subtle.Render("Everything you change here is a normal GitHub issue edit.") + "\n\n")
	} else {
		b.WriteString(th.title.Render("Track more repos") + "\n\n")
	}
	switch o.step {
	case stepSignIn:
		b.WriteString(th.statusWarn.Render("You're not signed in to GitHub.") + "\n\n")
		b.WriteString("triage uses the GitHub CLI's login. In another terminal run:\n\n")
		b.WriteString("    " + th.key.Render("gh auth login") + "\n\n")
		b.WriteString("then press " + th.key.Render("r") + " to continue, or " + th.key.Render("q") + " to quit.\n")
		if o.err != nil {
			b.WriteString("\n" + th.statusError.Render(gh.UserMessage(o.err)) + "\n")
		}
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, th.modal.Width(width).Render(b.String()))
	case stepFetching:
		b.WriteString(th.subtle.Render("Fetching issues from "+strings.Join(m.env.Config.Repos, ", ")+"…") + "\n")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, th.modal.Width(width).Render(b.String()))
	}
	intro := b.String()
	if m.me != "" && !o.adding {
		intro += th.statusOK.Render("✓") + " Signed in as " + th.bold.Render("@"+m.me) + "\n"
	}
	if o.err != nil {
		intro += th.statusError.Render(gh.UserMessage(o.err)) + "\n"
	}
	introBlock := lipgloss.NewStyle().Width(o.picker.width(m)).PaddingLeft(3).Render(intro)
	o.picker.reserve = lipgloss.Height(introBlock)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Left, introBlock, o.picker.view(m)))
}
