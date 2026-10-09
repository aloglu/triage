package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aloglu/triage/internal/issue"
)

// theme holds every style the app uses, resolved for a light or dark
// terminal background.
type theme struct {
	isDark bool
	// bg is the terminal's background color, used to tint pills and glows.
	bg color.Color
	// repoColors holds each tracked repo's assigned color.
	repoColors map[string]color.Color

	text, muted, faint, accent, border, borderFocus color.Color
	ok, warn, danger                                color.Color

	title, subtle, dim, bold   lipgloss.Style
	selected, cursorBar        lipgloss.Style
	key, keyDesc               lipgloss.Style
	pane, paneFocused          lipgloss.Style
	modal                      lipgloss.Style
	statusInfo, statusOK       lipgloss.Style
	statusWarn, statusError    lipgloss.Style
	sectionHeading             lipgloss.Style
	tabActive, tabInactive     lipgloss.Style
	inputPrompt, inputDisabled lipgloss.Style
}

// newTheme builds the theme for a terminal background. bg may be nil when
// the terminal doesn't report its color.
func newTheme(isDark bool, bg color.Color) theme {
	pick := lipgloss.LightDark(isDark)
	c := func(light, dark string) color.Color { return pick(lipgloss.Color(light), lipgloss.Color(dark)) }
	if bg == nil {
		bg = c("#ffffff", "#0d1117")
	}

	t := theme{
		isDark:      isDark,
		bg:          bg,
		text:        c("#1f2328", "#e6edf3"),
		muted:       c("#59636e", "#9198a1"),
		faint:       c("#8c959f", "#5d646b"),
		accent:      c("#0969da", "#5ec4e8"),
		border:      c("#d1d9e0", "#3d444d"),
		borderFocus: c("#0969da", "#5ec4e8"),
		ok:          c("#1a7f37", "#7ec699"),
		warn:        c("#9a6700", "#e3b341"),
		danger:      c("#cf222e", "#f47067"),
	}
	t.title = lipgloss.NewStyle().Foreground(t.text).Bold(true)
	t.subtle = lipgloss.NewStyle().Foreground(t.muted)
	t.dim = lipgloss.NewStyle().Foreground(t.faint)
	t.bold = lipgloss.NewStyle().Foreground(t.text).Bold(true)
	t.selected = lipgloss.NewStyle().Foreground(t.accent).Bold(true)
	t.cursorBar = lipgloss.NewStyle().Foreground(t.accent)
	t.key = lipgloss.NewStyle().Foreground(t.accent).Bold(true)
	t.keyDesc = lipgloss.NewStyle().Foreground(t.muted)
	t.pane = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.border).Padding(0, 1)
	t.paneFocused = t.pane.BorderForeground(t.borderFocus)
	t.modal = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.borderFocus).Padding(1, 2)
	t.statusInfo = lipgloss.NewStyle().Foreground(t.muted)
	t.statusOK = lipgloss.NewStyle().Foreground(t.ok)
	t.statusWarn = lipgloss.NewStyle().Foreground(t.warn)
	t.statusError = lipgloss.NewStyle().Foreground(t.danger)
	t.sectionHeading = lipgloss.NewStyle().Foreground(t.muted).Bold(true)
	t.tabActive = lipgloss.NewStyle().Foreground(t.accent).Bold(true).Underline(true)
	t.tabInactive = lipgloss.NewStyle().Foreground(t.muted)
	t.inputPrompt = lipgloss.NewStyle().Foreground(t.accent).Bold(true)
	t.inputDisabled = lipgloss.NewStyle().Foreground(t.faint)
	return t
}

func (t theme) statusColor(s issue.Status) color.Color {
	pick := lipgloss.LightDark(t.isDark)
	switch s {
	case issue.StatusIdea:
		return pick(lipgloss.Color("#8250df"), lipgloss.Color("#b392f0"))
	case issue.StatusInProgress:
		return pick(lipgloss.Color("#9a6700"), lipgloss.Color("#e3b341"))
	case issue.StatusBlocked:
		return t.danger
	case issue.StatusDone:
		return t.ok
	case issue.StatusWontDo:
		return t.faint
	default:
		return t.muted
	}
}

func (t theme) statusIcon(s issue.Status) string {
	switch s {
	case issue.StatusIdea:
		return "◇"
	case issue.StatusInProgress:
		return "◐"
	case issue.StatusBlocked:
		return "⊘"
	case issue.StatusDone:
		return "✓"
	case issue.StatusWontDo:
		return "✕"
	default:
		return "○"
	}
}

func (t theme) renderStatus(s issue.Status) string {
	return lipgloss.NewStyle().Foreground(t.statusColor(s)).Render(t.statusIcon(s) + " " + s.String())
}

// tint mixes c into the background; amount 0 is the background, 1 is c.
func (t theme) tint(c color.Color, amount float64) color.Color {
	steps := lipgloss.Blend1D(101, t.bg, c)
	idx := int(amount*100 + 0.5)
	return steps[max(0, min(100, idx))]
}

// statusPill renders a status as a small tinted pill.
func (t theme) statusPill(s issue.Status) string {
	c := t.statusColor(s)
	return lipgloss.NewStyle().Foreground(c).Background(t.tint(c, 0.18)).Padding(0, 1).
		Render(t.statusIcon(s) + " " + s.String())
}

// wordmarkColors are the gradient stops of the "triage" wordmark.
func (t theme) wordmarkColors() []color.Color {
	pick := lipgloss.LightDark(t.isDark)
	return []color.Color{
		pick(lipgloss.Color("#0969da"), lipgloss.Color("#5ec4e8")),
		pick(lipgloss.Color("#8250df"), lipgloss.Color("#b392f0")),
		pick(lipgloss.Color("#bf3989"), lipgloss.Color("#ff9bce")),
	}
}

// gradient colors each rune of text along the wordmark gradient.
func (t theme) gradient(text string, bold bool) string {
	runes := []rune(text)
	colors := lipgloss.Blend1D(max(2, len(runes)), t.wordmarkColors()...)
	var b strings.Builder
	for i, r := range runes {
		style := lipgloss.NewStyle().Foreground(colors[i])
		if bold {
			style = style.Bold(true)
		}
		b.WriteString(style.Render(string(r)))
	}
	return b.String()
}

// wordmark is the small "triage" in the header.
func (t theme) wordmark() string { return t.gradient("triage", true) }

// bigWordmark is the block-letter "triage" for the welcome screen.
func (t theme) bigWordmark() string {
	lines := []string{
		"▀█▀ █▀█ █ ▄▀█ █▀▀ █▀▀",
		" █  █▀▄ █ █▀█ █▄█ ██▄",
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = t.gradient(line, false)
	}
	return strings.Join(out, "\n")
}

func (t theme) typeColor(ty issue.Type) color.Color {
	pick := lipgloss.LightDark(t.isDark)
	switch ty {
	case issue.TypeBug:
		return t.danger
	case issue.TypeFeature:
		return pick(lipgloss.Color("#0969da"), lipgloss.Color("#6cb6ff"))
	case issue.TypeDocs:
		return pick(lipgloss.Color("#1b7c83"), lipgloss.Color("#56d4dd"))
	default:
		return t.faint
	}
}

func (t theme) renderType(ty issue.Type) string {
	return lipgloss.NewStyle().Foreground(t.typeColor(ty)).Render(strings.ToLower(ty.String()))
}

func (t theme) renderRepo(repo, text string) string {
	return lipgloss.NewStyle().Foreground(t.repoColor(repo)).Render(text)
}
