package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func typeInto(e *mdEditor, keys ...string) {
	for _, k := range keys {
		switch k {
		case "enter", "backspace", "left", "right", "up", "down", "home", "end", "ctrl+w", "delete":
			code := map[string]rune{"enter": tea.KeyEnter, "backspace": tea.KeyBackspace, "left": tea.KeyLeft, "right": tea.KeyRight,
				"up": tea.KeyUp, "down": tea.KeyDown, "home": tea.KeyHome, "end": tea.KeyEnd, "delete": tea.KeyDelete}[k]
			if k == "ctrl+w" {
				e.Update(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
				continue
			}
			e.Update(tea.KeyPressMsg{Code: code})
		default:
			for _, r := range k {
				if r == ' ' {
					e.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
					continue
				}
				e.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
		}
	}
}

func TestMDEditorEditing(t *testing.T) {
	e := newMDEditor()
	e.SetSize(40, 10)
	typeInto(e, "Hello world", "enter", "second", "backspace", "backspace", "ctrl+w", "line")
	if got := e.Value(); got != "Hello world\nline" {
		t.Fatalf("value = %q", got)
	}
	typeInto(e, "home", "backspace")
	if got := e.Value(); got != "Hello worldline" {
		t.Fatalf("backspace at line start should join lines, got %q", got)
	}
	e.Update(tea.PasteMsg{Content: "\r\npasted\ttext"})
	if got := e.Value(); got != "Hello world\npasted    textline" {
		t.Fatalf("paste = %q", got)
	}
}

func TestMDEditorLists(t *testing.T) {
	e := newMDEditor()
	e.SetSize(40, 10)
	typeInto(e, "- one", "enter", "two", "enter", "enter", "after")
	if got := e.Value(); got != "- one\n- two\nafter" {
		t.Fatalf("bullets = %q", got)
	}
	e.SetValue("")
	typeInto(e, "9. nine", "enter", "ten", "enter")
	if got := e.Value(); got != "9. nine\n10. ten\n11. " {
		t.Fatalf("numbers = %q", got)
	}
	e.SetValue("")
	typeInto(e, "- [x] done", "enter", "todo")
	if got := e.Value(); got != "- [x] done\n- [ ] todo" {
		t.Fatalf("tasks = %q", got)
	}
}

func TestMDEditorWrapsAndMovesByVisualRow(t *testing.T) {
	e := newMDEditor()
	e.SetSize(10, 10)
	e.SetValue("aaaa bbbb cccc dddd")
	segs := e.segments(e.lines[0])
	if len(segs) != 2 || string(e.lines[0][segs[0][0]:segs[0][1]]) != "aaaa bbbb " {
		t.Fatalf("segments = %v", segs)
	}
	typeInto(e, "right", "right", "down")
	if e.row != 0 || e.col != 12 {
		t.Fatalf("down within a wrapped line: row %d col %d", e.row, e.col)
	}
	typeInto(e, "up")
	if e.col != 2 {
		t.Fatalf("up should return to the same column, col %d", e.col)
	}
	for _, line := range strings.Split(e.View(newTheme(true, nil)), "\n") {
		if w := ansi.StringWidth(line); w > 10+1 {
			t.Fatalf("rendered line too wide (%d): %q", w, line)
		}
	}
}

func TestMDEditorScrollsToCursor(t *testing.T) {
	e := newMDEditor()
	e.SetSize(20, 3)
	e.Focus()
	e.SetValue(strings.Repeat("line\n", 10) + "last")
	typeInto(e, "down", "down", "down", "down", "down", "down", "down", "down", "down", "down")
	view := ansi.Strip(e.View(newTheme(true, nil)))
	if !strings.Contains(view, "last") || strings.Count(view, "\n") != 2 {
		t.Fatalf("view should follow the cursor:\n%s", view)
	}
}

func TestHighlightLine(t *testing.T) {
	fence := false
	kinds := highlightLine("# Title", &fence)
	if kinds[0] != mdMarker || kinds[3] != mdHeading {
		t.Fatalf("heading kinds = %v", kinds)
	}
	line := "Use `go test`, **bold**, *it*, [docs](https://x.y) and @octo #12"
	kinds = highlightLine(line, &fence)
	at := func(sub string) mdKind { return kinds[len([]rune(line[:strings.Index(line, sub)]))] }
	for sub, want := range map[string]mdKind{"go test": mdCode, "bold": mdBold, "it*": mdItalic, "docs": mdLink, "https": mdURL, "@octo": mdMention, "#12": mdMention, "Use": mdText} {
		if got := at(sub); got != want {
			t.Errorf("%q: kind %d, want %d", sub, got, want)
		}
	}
	highlightLine("```go", &fence)
	if !fence || highlightLine("x := 1", &fence)[0] != mdCode {
		t.Fatal("code fence state not tracked")
	}
	highlightLine("```", &fence)
	if fence {
		t.Fatal("fence not closed")
	}
}
