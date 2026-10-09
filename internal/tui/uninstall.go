package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aloglu/triage/internal/app"
	"github.com/aloglu/triage/internal/uninstall"
)

type uninstallPhase int

const (
	uninstallConfirm uninstallPhase = iota
	uninstallRemoving
	uninstallDone
	uninstallCancelled
	uninstallFailed
)

type targetRemovedMsg struct {
	index int
	err   error
}

// uninstaller is the screen behind `triage uninstall` in a terminal.
type uninstaller struct {
	env     *app.Env
	plan    uninstall.Plan
	targets []uninstall.Target
	th      theme
	spinner spinner.Model
	phase   uninstallPhase
	// removed counts targets already gone; the next one is in progress.
	removed int
	err     error
	width   int
}

// RunUninstall shows the plan, asks for confirmation, and removes each
// target in turn.
func RunUninstall(env *app.Env, plan uninstall.Plan) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	u := &uninstaller{
		env: env, plan: plan, targets: plan.Targets(),
		th:      newTheme(true, nil),
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		width:   80,
	}
	final, err := tea.NewProgram(u).Run()
	if err != nil {
		return err
	}
	if u, ok := final.(*uninstaller); ok && u.phase == uninstallFailed {
		return u.err
	}
	return nil
}

func (u *uninstaller) Init() tea.Cmd { return tea.RequestBackgroundColor }

// removeNext deletes the next target. A short pause between items lets
// each check mark land visibly; it's skipped when motion is reduced.
func (u *uninstaller) removeNext() tea.Cmd {
	index, plan := u.removed, u.plan
	pause := 120 * time.Millisecond
	if u.env.Config.ReduceMotion {
		pause = 0
	}
	return func() tea.Msg {
		time.Sleep(pause)
		return targetRemovedMsg{index: index, err: plan.Remove(index)}
	}
}

func (u *uninstaller) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		u.width = msg.Width
	case tea.BackgroundColorMsg:
		u.th = newTheme(msg.IsDark(), msg.Color)
		u.spinner.Style = lipgloss.NewStyle().Foreground(u.th.accent)
	case tea.KeyPressMsg:
		if u.phase != uninstallConfirm {
			if msg.String() == "ctrl+c" && u.phase != uninstallRemoving {
				return u, tea.Quit
			}
			return u, nil
		}
		switch msg.String() {
		case "y", "Y":
			u.phase = uninstallRemoving
			return u, tea.Batch(u.spinner.Tick, u.removeNext())
		case "n", "N", "esc", "q", "ctrl+c", "enter":
			u.phase = uninstallCancelled
			return u, tea.Quit
		}
	case spinner.TickMsg:
		if u.phase == uninstallRemoving {
			var cmd tea.Cmd
			u.spinner, cmd = u.spinner.Update(msg)
			return u, cmd
		}
	case targetRemovedMsg:
		if msg.err != nil {
			u.phase, u.err = uninstallFailed, msg.err
			return u, tea.Quit
		}
		u.removed++
		if u.removed == len(u.targets) {
			u.phase = uninstallDone
			return u, tea.Quit
		}
		return u, u.removeNext()
	}
	return u, nil
}

func (u *uninstaller) View() tea.View {
	th := u.th
	var b strings.Builder
	b.WriteString("\n")
	for _, line := range strings.Split(th.bigWordmark(), "\n") {
		b.WriteString("  " + line + "\n")
	}
	b.WriteString("\n")

	switch u.phase {
	case uninstallConfirm:
		b.WriteString("  " + th.bold.Render("This removes triage from this computer:") + "\n\n")
	case uninstallRemoving:
		b.WriteString("  " + th.bold.Render("Removing triage…") + "\n\n")
	default:
		b.WriteString("\n")
	}

	labelWidth := 0
	for _, t := range u.targets {
		labelWidth = max(labelWidth, lipgloss.Width(t.Kind))
	}
	for i, t := range u.targets {
		var icon string
		switch {
		case i < u.removed:
			icon = th.statusOK.Render("✓")
		case i == u.removed && u.phase == uninstallRemoving:
			icon = u.spinner.View()
		case i == u.removed && u.phase == uninstallFailed:
			icon = th.statusError.Render("✗")
		default:
			icon = th.dim.Render("○")
		}
		label := th.subtle.Render(fmt.Sprintf("%-*s", labelWidth, t.Kind))
		path := truncate(tildePath(t.Path), max(20, u.width-labelWidth-10))
		b.WriteString(fmt.Sprintf("  %s %s  %s\n", icon, label, th.dim.Render(path)))
	}
	b.WriteString("\n")

	switch u.phase {
	case uninstallConfirm:
		if u.plan.Unsent > 0 {
			b.WriteString("  " + th.statusWarn.Render(fmt.Sprintf("⚠ %d change(s) haven't reached GitHub yet and will be lost.", u.plan.Unsent)) + "\n")
			b.WriteString("  " + th.dim.Render("  Open triage while online first to send them.") + "\n\n")
		}
		b.WriteString("  " + th.subtle.Render("Your issues and labels on GitHub stay exactly as they are.") + "\n\n")
		b.WriteString("  Remove triage? " + th.key.Render("y") + th.dim.Render(" / ") + th.key.Render("N") + "\n")
	case uninstallDone:
		b.WriteString("  " + th.statusOK.Render("✓") + " " + th.bold.Render("triage is uninstalled.") + " " + th.subtle.Render("Thanks for using it.") + "\n\n")
		b.WriteString("  " + th.dim.Render("If you ever want it back:") + "\n")
		b.WriteString("  " + th.dim.Render("curl -fsSL https://raw.githubusercontent.com/aloglu/triage/main/install.sh | sh") + "\n")
	case uninstallCancelled:
		b.WriteString("  " + th.subtle.Render("Nothing was removed.") + "\n")
	case uninstallFailed:
		b.WriteString("  " + th.statusError.Render("✗ Stopped: ") + u.err.Error() + "\n")
		if u.removed > 0 {
			b.WriteString("  " + th.dim.Render("Items marked ✓ were removed; run triage uninstall again after fixing the problem.") + "\n")
		}
	}
	b.WriteString("\n")
	return tea.NewView(b.String())
}
