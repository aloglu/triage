package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aloglu/triage/internal/app"
	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/update"
)

// updateCheckedMsg reports a background check for a new release.
type updateCheckedMsg struct {
	release update.Release
	newer   bool
}

func updateCheckCmd(env *app.Env) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		release, newer, err := update.Check(ctx, env.Client, env.Engine.Store(), env.Version)
		if err != nil {
			return nil
		}
		return updateCheckedMsg{release: release, newer: newer}
	}
}

// checkForUpdate notices a newer release, asking GitHub at most once a day.
func (m *Model) checkForUpdate() tea.Cmd {
	if !update.IsRelease(m.env.Version) {
		return nil
	}
	if release, ok := update.Cached(m.eng.Store(), m.env.Version); ok {
		m.updateAvailable = release.Version
	}
	if m.eng.Online() && update.Due(m.eng.Store()) {
		return updateCheckCmd(m.env)
	}
	return nil
}

// --- triage update ---

type updatePhase int

const (
	phaseChecking updatePhase = iota
	phaseInstalling
	phaseDone
	phaseUpToDate
	phaseFailed
)

type (
	updateFoundMsg struct {
		release update.Release
		newer   bool
		err     error
	}
	updateInstalledMsg struct {
		dir string
		err error
	}
)

// updater is the small program behind `triage update`.
type updater struct {
	env     *app.Env
	th      theme
	spinner spinner.Model
	phase   updatePhase
	release update.Release
	dir     string
	err     error
	width   int
}

// RunUpdate implements `triage update`.
func RunUpdate(env *app.Env) error {
	if !update.IsRelease(env.Version) {
		fmt.Fprintf(env.Out, "This is a development build (%s); update it with git pull and make install.\n", env.Version)
		return nil
	}
	if !env.IsTerminal {
		return runUpdatePlain(env, env.Out)
	}
	u := &updater{env: env, th: newTheme(true, nil), spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)), width: 80}
	final, err := tea.NewProgram(u).Run()
	if err != nil {
		return err
	}
	if u, ok := final.(*updater); ok && u.phase == phaseFailed {
		return u.err
	}
	return nil
}

func runUpdatePlain(env *app.Env, out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	release, newer, err := update.Check(ctx, env.Client, env.Engine.Store(), env.Version)
	if err != nil {
		return errors.New(gh.UserMessage(err))
	}
	if !newer {
		fmt.Fprintf(out, "triage %s is up to date.\n", env.Version)
		return nil
	}
	fmt.Fprintf(out, "Updating triage %s → %s…\n", env.Version, release.Version)
	dir, err := update.Install(ctx, release.Version)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Updated to %s in %s.\n", release.Version, dir)
	return nil
}

func (u *updater) Init() tea.Cmd {
	env := u.env
	check := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		release, newer, err := update.Check(ctx, env.Client, env.Engine.Store(), env.Version)
		return updateFoundMsg{release: release, newer: newer, err: err}
	}
	return tea.Batch(tea.RequestBackgroundColor, u.spinner.Tick, check)
}

func (u *updater) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		u.width = msg.Width
	case tea.BackgroundColorMsg:
		u.th = newTheme(msg.IsDark(), msg.Color)
		u.spinner.Style = lipgloss.NewStyle().Foreground(u.th.accent)
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" || msg.String() == "esc" {
			return u, tea.Quit
		}
	case spinner.TickMsg:
		if u.phase == phaseChecking || u.phase == phaseInstalling {
			var cmd tea.Cmd
			u.spinner, cmd = u.spinner.Update(msg)
			return u, cmd
		}
	case updateFoundMsg:
		switch {
		case msg.err != nil:
			u.phase, u.err = phaseFailed, errors.New(gh.UserMessage(msg.err))
			return u, tea.Quit
		case !msg.newer:
			u.phase = phaseUpToDate
			return u, tea.Quit
		}
		u.phase, u.release = phaseInstalling, msg.release
		version := msg.release.Version
		return u, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			dir, err := update.Install(ctx, version)
			return updateInstalledMsg{dir: dir, err: err}
		}
	case updateInstalledMsg:
		if msg.err != nil {
			u.phase, u.err = phaseFailed, msg.err
		} else {
			u.phase, u.dir = phaseDone, msg.dir
		}
		return u, tea.Quit
	}
	return u, nil
}

func (u *updater) View() tea.View {
	th := u.th
	var b strings.Builder
	b.WriteString("\n  " + th.wordmark() + th.dim.Render(" "+u.env.Version) + "\n\n")
	switch u.phase {
	case phaseChecking:
		b.WriteString("  " + u.spinner.View() + th.subtle.Render(" Checking for a new version…") + "\n")
	case phaseUpToDate:
		b.WriteString("  " + th.statusOK.Render("✓") + " You're on the latest version.\n")
	case phaseInstalling, phaseDone:
		b.WriteString("  " + th.subtle.Render(u.env.Version) + th.dim.Render(" → ") + th.gradient(u.release.Version, true) + "\n\n")
		if highlights := releaseHighlights(u.release.Notes, 6); len(highlights) > 0 {
			for _, line := range highlights {
				b.WriteString("  " + th.dim.Render("• ") + line + "\n")
			}
			b.WriteString("\n")
		}
		if u.phase == phaseInstalling {
			b.WriteString("  " + u.spinner.View() + th.subtle.Render(" Installing…") + "\n")
		} else {
			b.WriteString("  " + th.statusOK.Render("✓") + " Updated to " + th.bold.Render(u.release.Version) + ".\n")
			if running := update.RunningFrom(); u.dir != "" && running != "" && running != u.dir {
				note := th.statusWarn.Render("Note: ") + th.subtle.Render("the new version is in "+u.dir+", but this triage ran from "+running+". Run the new one from there.")
				b.WriteString(lipgloss.NewStyle().PaddingLeft(2).Width(min(u.width, 90)).Render(note) + "\n")
			}
			if u.release.URL != "" {
				b.WriteString("  " + th.dim.Render("Release notes: "+u.release.URL) + "\n")
			}
		}
	case phaseFailed:
		b.WriteString("  " + th.statusError.Render("✗ Couldn't update: ") + u.err.Error() + "\n")
	}
	b.WriteString("\n")
	return tea.NewView(b.String())
}

// releaseHighlights pulls the first bullet points out of release notes.
func releaseHighlights(notes string, limit int) []string {
	var out []string
	for _, line := range strings.Split(notes, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "* ") {
			continue
		}
		text := strings.TrimSpace(line[2:])
		text = strings.NewReplacer("**", "", "`", "").Replace(text)
		if len([]rune(text)) > 90 {
			text = string([]rune(text)[:89]) + "…"
		}
		out = append(out, text)
		if len(out) == limit {
			break
		}
	}
	return out
}
