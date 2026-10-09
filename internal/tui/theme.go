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

func newTheme(isDark bool) theme {
	pick := lipgloss.LightDark(isDark)
	c := func(light, dark string) color.Color { return pick(lipgloss.Color(light), lipgloss.Color(dark)) }

	t := theme{
		isDark:      isDark,
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

// repoColor gives each repo a stable color so issues from different repos
// are easy to tell apart in the inbox.
func (t theme) repoColor(repo string) color.Color {
	darkPalette := []string{"#7ec699", "#d2a8ff", "#e6b673", "#79c0ff", "#f0883e", "#56d4dd", "#ff9bce", "#a5d6ff"}
	lightPalette := []string{"#1a7f37", "#8250df", "#9a6700", "#0969da", "#bc4c00", "#1b7c83", "#bf3989", "#0550ae"}
	var hash uint32 = 2166136261
	for _, b := range []byte(strings.ToLower(repo)) {
		hash ^= uint32(b)
		hash *= 16777619
	}
	idx := int(hash % uint32(len(darkPalette)))
	return lipgloss.LightDark(t.isDark)(lipgloss.Color(lightPalette[idx]), lipgloss.Color(darkPalette[idx]))
}

func (t theme) renderRepo(repo, text string) string {
	return lipgloss.NewStyle().Foreground(t.repoColor(repo)).Render(text)
}
