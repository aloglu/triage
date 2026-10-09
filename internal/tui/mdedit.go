package tui

import (
	"regexp"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// mdEditor is a multi-line text editor that highlights Markdown as you
// type, so writing looks like reading. It soft-wraps at word boundaries and
// keeps the cursor in view.
type mdEditor struct {
	lines       [][]rune
	row, col    int // cursor, in logical lines and runes
	width       int
	height      int
	scroll      int // first visual row shown
	focused     bool
	placeholder string
	// wantX keeps the cursor's column steady while moving up and down
	// through lines of different lengths.
	wantX int
}

func newMDEditor() *mdEditor {
	return &mdEditor{lines: [][]rune{{}}, width: 40, height: 5, wantX: -1}
}

func (e *mdEditor) SetValue(s string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	e.lines = nil
	for _, line := range strings.Split(s, "\n") {
		e.lines = append(e.lines, []rune(line))
	}
	e.row, e.col, e.scroll, e.wantX = 0, 0, 0, -1
}

func (e *mdEditor) Value() string {
	parts := make([]string, len(e.lines))
	for i, line := range e.lines {
		parts[i] = string(line)
	}
	return strings.Join(parts, "\n")
}

func (e *mdEditor) SetSize(width, height int) {
	e.width, e.height = max(4, width), max(1, height)
}

func (e *mdEditor) Focus() { e.focused = true }
func (e *mdEditor) Blur()  { e.focused = false }

// MoveToEnd puts the cursor after the last character.
func (e *mdEditor) MoveToEnd() {
	e.row = len(e.lines) - 1
	e.col = len(e.lines[e.row])
}

// Update handles typing, editing keys, and pastes.
func (e *mdEditor) Update(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		e.insert(msg.Content)
	case tea.KeyPressMsg:
		e.handleKey(msg)
	}
	e.keepCursorVisible()
}

func (e *mdEditor) handleKey(msg tea.KeyPressMsg) {
	keepX := false
	switch msg.String() {
	case "enter":
		e.newline()
	case "backspace", "ctrl+h":
		e.deleteBack()
	case "alt+backspace", "ctrl+w":
		e.deleteWordBack()
	case "delete", "ctrl+d":
		e.deleteForward()
	case "left", "ctrl+b":
		e.moveLeft()
	case "right", "ctrl+f":
		e.moveRight()
	case "alt+left", "ctrl+left", "alt+b":
		e.wordLeft()
	case "alt+right", "ctrl+right", "alt+f":
		e.wordRight()
	case "up":
		e.moveVertical(-1)
		keepX = true
	case "down":
		e.moveVertical(1)
		keepX = true
	case "pgup":
		e.moveVertical(-e.height)
		keepX = true
	case "pgdown":
		e.moveVertical(e.height)
		keepX = true
	case "home", "ctrl+a":
		e.col = e.segmentStart()
	case "end":
		e.col = e.segmentEnd()
	case "ctrl+home":
		e.row, e.col = 0, 0
	case "ctrl+end":
		e.MoveToEnd()
	case "ctrl+k":
		e.lines[e.row] = e.lines[e.row][:e.col]
	case "ctrl+u":
		e.lines[e.row] = e.lines[e.row][e.col:]
		e.col = 0
	case "space":
		e.insert(" ")
	default:
		if msg.Text != "" && msg.Mod&^tea.ModShift == 0 {
			e.insert(msg.Text)
		}
	}
	if !keepX {
		e.wantX = -1
	}
}

func (e *mdEditor) insert(text string) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\t", "    ")
	for i, part := range strings.Split(text, "\n") {
		if i > 0 {
			e.splitLine()
		}
		runes := []rune(part)
		line := e.lines[e.row]
		e.lines[e.row] = append(append(append([]rune{}, line[:e.col]...), runes...), line[e.col:]...)
		e.col += len(runes)
	}
}

func (e *mdEditor) splitLine() {
	line := e.lines[e.row]
	rest := append([]rune{}, line[e.col:]...)
	e.lines[e.row] = line[:e.col]
	e.lines = append(e.lines[:e.row+1], append([][]rune{rest}, e.lines[e.row+1:]...)...)
	e.row++
	e.col = 0
}

var listMarker = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])( \[[ xX]\])? `)

// newline breaks the line. On a list item it starts the next item; on an
// empty item it ends the list instead.
func (e *mdEditor) newline() {
	line := string(e.lines[e.row])
	m := listMarker.FindStringSubmatch(line)
	if m == nil || e.col < len([]rune(m[0])) {
		e.splitLine()
		return
	}
	if strings.TrimSpace(line) == strings.TrimSpace(m[0]) {
		// An empty item: end the list.
		e.lines[e.row] = []rune{}
		e.col = 0
		return
	}
	marker := m[2]
	if n := strings.TrimRight(marker, ".)"); n != marker {
		var num int
		for _, r := range n {
			num = num*10 + int(r-'0')
		}
		marker = strings.Replace(marker, n, itoa(num+1), 1)
	}
	next := m[1] + marker
	if m[3] != "" {
		next += " [ ]"
	}
	e.splitLine()
	e.insert(next + " ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []rune
	for ; n > 0; n /= 10 {
		digits = append([]rune{rune('0' + n%10)}, digits...)
	}
	return string(digits)
}

func (e *mdEditor) deleteBack() {
	if e.col > 0 {
		line := e.lines[e.row]
		e.lines[e.row] = append(append([]rune{}, line[:e.col-1]...), line[e.col:]...)
		e.col--
		return
	}
	if e.row > 0 {
		prev := e.lines[e.row-1]
		e.col = len(prev)
		e.lines[e.row-1] = append(append([]rune{}, prev...), e.lines[e.row]...)
		e.lines = append(e.lines[:e.row], e.lines[e.row+1:]...)
		e.row--
	}
}

func (e *mdEditor) deleteForward() {
	line := e.lines[e.row]
	if e.col < len(line) {
		e.lines[e.row] = append(append([]rune{}, line[:e.col]...), line[e.col+1:]...)
		return
	}
	if e.row < len(e.lines)-1 {
		e.lines[e.row] = append(append([]rune{}, line...), e.lines[e.row+1]...)
		e.lines = append(e.lines[:e.row+1], e.lines[e.row+2:]...)
	}
}

func (e *mdEditor) deleteWordBack() {
	if e.col == 0 {
		e.deleteBack()
		return
	}
	start := e.col
	e.wordLeft()
	line := e.lines[e.row]
	e.lines[e.row] = append(append([]rune{}, line[:e.col]...), line[start:]...)
}

func (e *mdEditor) moveLeft() {
	if e.col > 0 {
		e.col--
	} else if e.row > 0 {
		e.row--
		e.col = len(e.lines[e.row])
	}
}

func (e *mdEditor) moveRight() {
	if e.col < len(e.lines[e.row]) {
		e.col++
	} else if e.row < len(e.lines)-1 {
		e.row++
		e.col = 0
	}
}

func (e *mdEditor) wordLeft() {
	line := e.lines[e.row]
	if e.col == 0 {
		e.moveLeft()
		return
	}
	i := e.col
	for i > 0 && unicode.IsSpace(line[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(line[i-1]) {
		i--
	}
	e.col = i
}

func (e *mdEditor) wordRight() {
	line := e.lines[e.row]
	if e.col == len(line) {
		e.moveRight()
		return
	}
	i := e.col
	for i < len(line) && !unicode.IsSpace(line[i]) {
		i++
	}
	for i < len(line) && unicode.IsSpace(line[i]) {
		i++
	}
	e.col = i
}

// --- wrapping ---

// segments splits a logical line into visual rows at word boundaries, as
// rune offsets [start, end).
func (e *mdEditor) segments(line []rune) [][2]int {
	if len(line) == 0 {
		return [][2]int{{0, 0}}
	}
	var out [][2]int
	start := 0
	for start < len(line) {
		width, end, lastSpace := 0, start, -1
		for end < len(line) {
			w := ansi.StringWidth(string(line[end]))
			if width+w > e.width {
				break
			}
			if line[end] == ' ' {
				lastSpace = end
			}
			width += w
			end++
		}
		if end < len(line) && lastSpace > start {
			end = lastSpace + 1
		}
		if end == start {
			end = start + 1
		}
		out = append(out, [2]int{start, end})
		start = end
	}
	return out
}

// cursorSegment returns the index of the visual row holding the cursor
// within its line, and that row's bounds.
func (e *mdEditor) cursorSegment() (int, [2]int) {
	segs := e.segments(e.lines[e.row])
	for i, seg := range segs {
		if e.col < seg[1] || i == len(segs)-1 {
			return i, seg
		}
	}
	return 0, segs[0]
}

func (e *mdEditor) segmentStart() int { _, seg := e.cursorSegment(); return seg[0] }

func (e *mdEditor) segmentEnd() int {
	idx, seg := e.cursorSegment()
	if idx < len(e.segments(e.lines[e.row]))-1 {
		return seg[1] - 1
	}
	return seg[1]
}

// visualRow returns the cursor's row counting wrapped rows from the top.
func (e *mdEditor) visualRow() int {
	rows := 0
	for i := 0; i < e.row; i++ {
		rows += len(e.segments(e.lines[i]))
	}
	idx, _ := e.cursorSegment()
	return rows + idx
}

// moveVertical moves the cursor by delta visual rows, keeping its column.
func (e *mdEditor) moveVertical(delta int) {
	_, seg := e.cursorSegment()
	if e.wantX < 0 {
		e.wantX = ansi.StringWidth(string(e.lines[e.row][seg[0]:e.col]))
	}
	type pos struct{ row, start, end int }
	var rows []pos
	current := 0
	for r, line := range e.lines {
		for _, s := range e.segments(line) {
			if r == e.row && s == seg {
				current = len(rows)
			}
			rows = append(rows, pos{r, s[0], s[1]})
		}
	}
	target := max(0, min(len(rows)-1, current+delta))
	p := rows[target]
	line := e.lines[p.row]
	col, x := p.start, 0
	last := p.end
	if target < len(rows)-1 && rows[target+1].row == p.row {
		last = p.end - 1 // don't land past a wrap point
	}
	for col < last {
		w := ansi.StringWidth(string(line[col]))
		if x+w > e.wantX {
			break
		}
		x += w
		col++
	}
	e.row, e.col = p.row, col
}

func (e *mdEditor) keepCursorVisible() {
	row := e.visualRow()
	if row < e.scroll {
		e.scroll = row
	}
	if row >= e.scroll+e.height {
		e.scroll = row - e.height + 1
	}
}

// --- rendering ---

// View draws the visible rows with Markdown highlighting and the cursor.
func (e *mdEditor) View(th theme) string {
	if len(e.lines) == 1 && len(e.lines[0]) == 0 && e.placeholder != "" {
		text := th.dim.Render(truncate(e.placeholder, e.width))
		if e.focused {
			text = lipgloss.NewStyle().Reverse(true).Render(" ") + th.dim.Render(truncate(e.placeholder, e.width-1))
		}
		return text
	}
	e.keepCursorVisible()
	styles := newMDStyles(th)
	cursorSeg, _ := e.cursorSegment()
	var out []string
	visual := 0
	inFence := false
	for r, line := range e.lines {
		spans := highlightLine(string(line), &inFence)
		for i, seg := range e.segments(line) {
			if visual >= e.scroll && visual < e.scroll+e.height {
				cursor := -1
				if e.focused && r == e.row && i == cursorSeg {
					cursor = e.col - seg[0]
				}
				out = append(out, renderSpans(line[seg[0]:seg[1]], spans[seg[0]:seg[1]], cursor, styles))
			}
			visual++
		}
	}
	return strings.Join(out, "\n")
}

// Markdown token kinds for highlighting.
type mdKind uint8

const (
	mdText mdKind = iota
	mdMarker
	mdHeading
	mdBold
	mdItalic
	mdCode
	mdFence
	mdQuote
	mdLink
	mdURL
	mdMention
)

type mdStyles [mdMention + 1]lipgloss.Style

func newMDStyles(th theme) mdStyles {
	var s mdStyles
	accent := th.wordmarkColors()
	s[mdText] = lipgloss.NewStyle().Foreground(th.text)
	s[mdMarker] = lipgloss.NewStyle().Foreground(th.accent).Bold(true)
	s[mdHeading] = lipgloss.NewStyle().Foreground(accent[1]).Bold(true)
	s[mdBold] = lipgloss.NewStyle().Foreground(th.text).Bold(true)
	s[mdItalic] = lipgloss.NewStyle().Foreground(th.text).Italic(true)
	s[mdCode] = lipgloss.NewStyle().Foreground(accent[2]).Background(th.tint(accent[2], 0.12))
	s[mdFence] = lipgloss.NewStyle().Foreground(th.muted)
	s[mdQuote] = lipgloss.NewStyle().Foreground(th.muted).Italic(true)
	s[mdLink] = lipgloss.NewStyle().Foreground(th.accent).Underline(true)
	s[mdURL] = lipgloss.NewStyle().Foreground(th.faint)
	s[mdMention] = lipgloss.NewStyle().Foreground(th.accent).Bold(true)
	return s
}

var (
	mdHeadingRe = regexp.MustCompile(`^(#{1,6})\s`)
	mdQuoteRe   = regexp.MustCompile(`^\s*>`)
	mdFenceRe   = regexp.MustCompile("^\\s*(```|~~~)")
	mdRuleRe    = regexp.MustCompile(`^\s*([-*_])(\s*[-*_]){2,}\s*$`)
	mdCodeRe    = regexp.MustCompile("`[^`]+`")
	mdBoldRe    = regexp.MustCompile(`\*\*[^*]+\*\*|__[^_]+__`)
	mdItalicRe  = regexp.MustCompile(`(^|[^*\w])\*[^*\s][^*]*\*|(^|[^_\w])_[^_\s][^_]*_`)
	mdLinkRe    = regexp.MustCompile(`\[[^\]]+\]\([^)\s]+\)`)
	mdURLRe     = regexp.MustCompile(`https?://[^\s<>()]+`)
	mdMentionRe = regexp.MustCompile(`(^|[^\w])(@[\w-]+|#\d+)`)
)

// highlightLine returns a token kind for every rune of line. inFence
// carries code-block state from line to line.
func highlightLine(line string, inFence *bool) []mdKind {
	runes := []rune(line)
	kinds := make([]mdKind, len(runes))
	set := func(byteStart, byteEnd int, kind mdKind) {
		start := len([]rune(line[:byteStart]))
		end := start + len([]rune(line[byteStart:byteEnd]))
		for i := start; i < end && i < len(kinds); i++ {
			kinds[i] = kind
		}
	}
	all := func(kind mdKind) {
		for i := range kinds {
			kinds[i] = kind
		}
	}
	if mdFenceRe.MatchString(line) {
		*inFence = !*inFence
		all(mdFence)
		return kinds
	}
	if *inFence {
		all(mdCode)
		return kinds
	}
	if m := mdHeadingRe.FindStringIndex(line); m != nil {
		all(mdHeading)
		set(0, m[1], mdMarker)
		return kinds
	}
	if mdRuleRe.MatchString(line) {
		all(mdFence)
		return kinds
	}
	if m := mdQuoteRe.FindStringIndex(line); m != nil {
		all(mdQuote)
		set(m[0], m[1], mdMarker)
	}
	if m := listMarker.FindStringIndex(line); m != nil {
		set(m[0], m[1], mdMarker)
	}
	for _, m := range mdBoldRe.FindAllStringIndex(line, -1) {
		set(m[0], m[1], mdBold)
	}
	for _, m := range mdItalicRe.FindAllStringSubmatchIndex(line, -1) {
		start := m[0]
		if m[3] > m[2] {
			start = m[3]
		} else if m[5] > m[4] {
			start = m[5]
		}
		set(start, m[1], mdItalic)
	}
	for _, m := range mdMentionRe.FindAllStringSubmatchIndex(line, -1) {
		set(m[4], m[5], mdMention)
	}
	for _, m := range mdURLRe.FindAllStringIndex(line, -1) {
		set(m[0], m[1], mdURL)
	}
	for _, m := range mdLinkRe.FindAllStringIndex(line, -1) {
		text := strings.Index(line[m[0]:m[1]], "](")
		set(m[0], m[0]+text+1, mdLink)
		set(m[0]+text+1, m[1], mdURL)
	}
	for _, m := range mdCodeRe.FindAllStringIndex(line, -1) {
		set(m[0], m[1], mdCode)
	}
	return kinds
}

// renderSpans styles runes by kind, drawing the cursor at index cursor
// (-1 for none; len(runes) for just past the end).
func renderSpans(runes []rune, kinds []mdKind, cursor int, styles mdStyles) string {
	var b strings.Builder
	cursorStyle := lipgloss.NewStyle().Reverse(true)
	i := 0
	for i < len(runes) {
		if i == cursor {
			b.WriteString(cursorStyle.Render(string(runes[i])))
			i++
			continue
		}
		j := i + 1
		for j < len(runes) && kinds[j] == kinds[i] && j != cursor {
			j++
		}
		b.WriteString(styles[kinds[i]].Render(string(runes[i:j])))
		i = j
	}
	if cursor == len(runes) {
		b.WriteString(cursorStyle.Render(" "))
	}
	return b.String()
}
