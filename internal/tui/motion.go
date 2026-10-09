package tui

import (
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aloglu/triage/internal/issue"
)

type animTickMsg time.Time

// motion reports whether animations are on.
func (m *Model) motion() bool { return !m.env.Config.ReduceMotion }

func (m *Model) applyThemeToWidgets() {
	m.filterInput.SetStyles(textinput.DefaultStyles(m.th.isDark))
	m.spinner.Style = lipgloss.NewStyle().Foreground(m.th.accent)
	m.progress = progress.New(progress.WithColors(m.th.wordmarkColors()...), progress.WithoutPercentage(), progress.WithWidth(32))
}

// startSpinner starts the sync spinner if it isn't already running.
func (m *Model) startSpinner() tea.Cmd {
	if m.spinning || !m.motion() {
		return nil
	}
	m.spinning = true
	return m.spinner.Tick
}

// glowRow makes an issue's row briefly light up in its new status color.
func (m *Model) glowRow(key string, s issue.Status) tea.Cmd {
	if !m.motion() {
		return nil
	}
	m.glows[key] = glow{start: time.Now(), color: m.th.statusColor(s)}
	return m.startAnim()
}

func (m *Model) startAnim() tea.Cmd {
	if m.animating {
		return nil
	}
	m.animating = true
	return tea.Tick(animFrame, func(t time.Time) tea.Msg { return animTickMsg(t) })
}

// animTick advances animations and schedules the next frame while any are
// running.
func (m *Model) animTick() tea.Cmd {
	for key, g := range m.glows {
		if time.Since(g.start) > glowDuration {
			delete(m.glows, key)
		}
	}
	checking := m.flashState.check && time.Since(m.flashState.start) < checkDuration
	if len(m.glows) == 0 && !checking {
		m.animating = false
		return nil
	}
	return tea.Tick(animFrame, func(t time.Time) tea.Msg { return animTickMsg(t) })
}

// glowTint returns the background tint for a row, or nil.
func (m *Model) glowTint(key string) (bg lipgloss.Style, ok bool) {
	g, found := m.glows[key]
	if !found {
		return lipgloss.Style{}, false
	}
	p := float64(time.Since(g.start)) / float64(glowDuration)
	if p >= 1 {
		return lipgloss.Style{}, false
	}
	// Ease out: bright at first, fading smoothly.
	strength := 0.35 * (1 - p) * (1 - p)
	return lipgloss.NewStyle().Background(m.th.tint(g.color, strength)), true
}

// checkGlyph returns the animated check for a "done" flash message.
func (m *Model) checkGlyph() string {
	frames := []string{"◌", "◔", "◑", "◕", "✓"}
	if !m.motion() {
		return "✓"
	}
	idx := int(time.Since(m.flashState.start) / (checkDuration / time.Duration(len(frames))))
	return frames[min(idx, len(frames)-1)]
}

// celebrate flashes a message with the animated check.
func (m *Model) celebrate(text string) tea.Cmd {
	cmd := m.flash(text, flashOK)
	m.flashState.check = true
	return tea.Batch(cmd, m.startAnim())
}
