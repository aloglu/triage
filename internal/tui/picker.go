package tui

import (
	"image/color"
	"sort"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// overlay is a modal drawn over the main screen. update returns done=true
// when the overlay should close.
type overlay interface {
	update(m *Model, msg tea.Msg) (done bool, cmd tea.Cmd)
	view(m *Model) string
}

type pickerItem struct {
	id       string
	label    string
	hint     string
	key      string // optional single-key shortcut shown and accepted while the filter is empty
	color    color.Color
	selected bool
}

// picker is a filterable list. In multi mode space toggles items and enter
// confirms; otherwise enter picks the highlighted item.
type picker struct {
	title       string
	items       []pickerItem
	multi       bool
	allowCreate bool
	createLabel string
	createValid func(string) bool
	note        string
	input       textinput.Model
	matches     []int
	cursor      int
	// hadSelection is set when items started out selected, so that
	// deselecting everything means "none" rather than "the highlighted one".
	hadSelection bool
	// reserve is how many screen lines other content around the picker needs.
	reserve int
	onDone  func(m *Model, chosen []pickerItem, created string) tea.Cmd
}

func newPicker(m *Model, title string, items []pickerItem) *picker {
	for i := range items {
		items[i].hint = strings.Join(strings.Fields(items[i].hint), " ")
	}
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "type to filter"
	in.SetStyles(textinput.DefaultStyles(m.th.isDark))
	in.Focus()
	p := &picker{title: title, items: items, input: in}
	for _, item := range items {
		p.hadSelection = p.hadSelection || item.selected
	}
	p.refilter()
	return p
}

func (p *picker) refilter() {
	query := strings.TrimSpace(p.input.Value())
	p.matches = p.matches[:0]
	type scored struct{ idx, score int }
	var hits []scored
	for i, item := range p.items {
		if query == "" {
			hits = append(hits, scored{i, 0})
			continue
		}
		if score, ok := fuzzyScore(item.label, query); ok {
			hits = append(hits, scored{i, score + 2000})
		} else if strings.Contains(strings.ToLower(item.hint), strings.ToLower(query)) {
			hits = append(hits, scored{i, 0})
		}
	}
	if query != "" {
		sort.SliceStable(hits, func(a, b int) bool { return hits[a].score > hits[b].score })
	}
	for _, hit := range hits {
		p.matches = append(p.matches, hit.idx)
	}
	if p.cursor >= p.visibleCount() {
		p.cursor = max(0, p.visibleCount()-1)
	}
}

func (p *picker) createOption() string {
	if !p.allowCreate {
		return ""
	}
	value := strings.TrimSpace(p.input.Value())
	if value == "" || (p.createValid != nil && !p.createValid(value)) {
		return ""
	}
	for _, item := range p.items {
		if strings.EqualFold(item.label, value) {
			return ""
		}
	}
	return value
}

func (p *picker) visibleCount() int {
	n := len(p.matches)
	if p.createOption() != "" {
		n++
	}
	return n
}

func (p *picker) selectCursor(idx int) {
	if n := p.visibleCount(); n > 0 {
		p.cursor = (idx%n + n) % n
	}
}

func (p *picker) update(m *Model, msg tea.Msg) (bool, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		return false, cmd
	}
	switch key.String() {
	case "esc":
		if p.input.Value() != "" {
			p.input.SetValue("")
			p.refilter()
			return false, nil
		}
		return true, nil
	case "ctrl+c":
		return true, nil
	case "up", "ctrl+p", "ctrl+k":
		p.selectCursor(p.cursor - 1)
		return false, nil
	case "down", "ctrl+n", "ctrl+j":
		p.selectCursor(p.cursor + 1)
		return false, nil
	case "space":
		if p.multi && p.input.Value() == "" {
			p.toggleCursor()
			return false, nil
		}
	case "tab":
		if p.multi {
			p.toggleCursor()
			p.selectCursor(p.cursor + 1)
			return false, nil
		}
	case "enter":
		return true, p.finish(m)
	}
	if p.input.Value() == "" && !p.multi {
		for _, idx := range p.matches {
			if item := p.items[idx]; item.key != "" && item.key == key.String() {
				return true, p.onDone(m, []pickerItem{item}, "")
			}
		}
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.refilter()
	return false, cmd
}

func (p *picker) toggleCursor() {
	if p.cursor < len(p.matches) {
		idx := p.matches[p.cursor]
		p.items[idx].selected = !p.items[idx].selected
	}
}

func (p *picker) finish(m *Model) tea.Cmd {
	if p.onDone == nil {
		return nil
	}
	if created := p.createOption(); created != "" && p.cursor == len(p.matches) {
		var chosen []pickerItem
		if p.multi {
			chosen = p.selectedItems()
		}
		return p.onDone(m, chosen, created)
	}
	if p.multi {
		chosen := p.selectedItems()
		// Enter with nothing toggled picks the highlighted item.
		if len(chosen) == 0 && p.cursor < len(p.matches) && !p.hadSelection {
			chosen = []pickerItem{p.items[p.matches[p.cursor]]}
		}
		return p.onDone(m, chosen, "")
	}
	if p.cursor < len(p.matches) {
		return p.onDone(m, []pickerItem{p.items[p.matches[p.cursor]]}, "")
	}
	return nil
}

func (p *picker) selectedItems() []pickerItem {
	var out []pickerItem
	for _, item := range p.items {
		if item.selected {
			out = append(out, item)
		}
	}
	return out
}

func (p *picker) view(m *Model) string {
	th := m.th
	width := p.width(m)
	inner := width - 6 // border and padding
	p.input.SetWidth(inner - 2)
	listHeight := max(3, min(14, m.height-12-p.reserve))

	var b strings.Builder
	b.WriteString(th.title.Render(p.title))
	b.WriteString("\n\n")
	b.WriteString(p.input.View())
	b.WriteString("\n\n")

	start := 0
	if p.cursor >= listHeight {
		start = p.cursor - listHeight + 1
	}
	rows := 0
	for i := start; i < p.visibleCount() && rows < listHeight; i++ {
		rows++
		active := i == p.cursor
		bar := "  "
		if active {
			bar = th.cursorBar.Render("▌ ")
		}
		if i == len(p.matches) {
			label := p.createLabel
			if label == "" {
				label = "Create"
			}
			b.WriteString(bar + th.selected.Render(label+" “"+p.createOption()+"”") + "\n")
			continue
		}
		item := p.items[p.matches[i]]
		var line string
		if p.multi {
			box := th.dim.Render("○ ")
			if item.selected {
				box = th.selected.Render("● ")
			}
			line = box
		} else if item.key != "" {
			line = th.key.Render(item.key) + " "
		}
		labelStyle := lipgloss.NewStyle().Foreground(th.text)
		if item.color != nil {
			labelStyle = labelStyle.Foreground(item.color)
		}
		if active {
			labelStyle = labelStyle.Bold(true)
		}
		line += labelStyle.Render(item.label)
		if item.hint != "" {
			if avail := inner - 2 - lipgloss.Width(line) - 2; avail > 4 {
				line += "  " + th.dim.Render(truncate(item.hint, avail))
			}
		}
		b.WriteString(bar + line + "\n")
	}
	if p.visibleCount() == 0 {
		b.WriteString(th.dim.Render("  No matches") + "\n")
	}
	hints := []string{"↑↓ move", "enter choose", "esc close"}
	if p.multi {
		hints = []string{"↑↓ move", "tab/space toggle", "enter apply", "esc close"}
	}
	b.WriteString("\n")
	if p.note != "" {
		b.WriteString(th.dim.Render(p.note) + "\n")
	}
	b.WriteString(th.dim.Render(strings.Join(hints, " · ")))
	return th.modal.Width(width).Render(b.String())
}

func (p *picker) width(m *Model) int { return min(72, max(36, m.width-8)) }

// fuzzyScore reports whether every rune of query appears in text in order,
// scoring contiguous runs and word starts higher.
func fuzzyScore(text, query string) (int, bool) {
	text = strings.ToLower(text)
	query = strings.ToLower(query)
	if strings.Contains(text, query) {
		score := 1000 - strings.Index(text, query)
		return score, true
	}
	tr := []rune(text)
	score, ti, run := 0, 0, 0
	for _, qr := range query {
		if unicode.IsSpace(qr) {
			continue
		}
		found := false
		for ; ti < len(tr); ti++ {
			if tr[ti] == qr {
				run++
				score += run * 2
				if ti == 0 || !unicode.IsLetter(tr[ti-1]) {
					score += 5
				}
				ti++
				found = true
				break
			}
			run = 0
		}
		if !found {
			return 0, false
		}
	}
	return score, true
}

func truncate(s string, width int) string {
	if width <= 1 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}
