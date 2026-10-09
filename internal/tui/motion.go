package tui

import (
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/aloglu/triage/internal/issue"
)

type animTickMsg time.Time

// motion reports whether animations are on.
func (m *Model) motion() bool { return !m.env.Config.ReduceMotion }

func (m *Model) applyThemeToWidgets() {
	m.assignRepoColors()
	m.filterInput.SetStyles(m.inputStyles())
	m.spinner.Style = lipgloss.NewStyle().Foreground(m.th.accent)
	m.progress = progress.New(progress.WithColors(m.th.wordmarkColors()...), progress.WithoutPercentage(), progress.WithWidth(32))
}

// assignRepoColors recomputes repo colors after the theme, the tracked
// repos, or the config change.
func (m *Model) assignRepoColors() {
	if err := m.th.assignRepoColors(m.env.Config.Repos, m.env.Config.RepoColors); err != nil {
		m.flash(err.Error(), flashWarn)
	}
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
	return after(animFrame, func(t time.Time) tea.Msg { return animTickMsg(t) })
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
	return after(animFrame, func(t time.Time) tea.Msg { return animTickMsg(t) })
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

// after schedules a message, like tea.Tick. Every timer in the app goes
// through it so tests can turn timers off and wait for real work instead.
var after = tea.Tick

// inputStyles are the text input styles for the theme; the cursor only
// blinks when motion is on.
func (m *Model) inputStyles() textinput.Styles {
	s := textinput.DefaultStyles(m.th.isDark)
	s.Cursor.Blink = m.motion()
	return s
}

// areaStyles are inputStyles for multi-line text.
func (m *Model) areaStyles() textarea.Styles {
	s := textarea.DefaultStyles(m.th.isDark)
	s.Cursor.Blink = m.motion()
	return s
}
