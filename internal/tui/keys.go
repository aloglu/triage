package tui

import "charm.land/bubbles/v2/key"

// keyMap is the single source of truth for key bindings: the footer and the
// help screen are generated from it.
type keyMap struct {
	Up, Down, Top, Bottom, PageUp, PageDown key.Binding
	Open, Back, NextView, PrevView          key.Binding
	Filter, Palette, Help, Quit, Board      key.Binding
	JumpIssue, SwitchRepo, Sidebar          key.Binding

	New, Edit, EditExternal, Comment key.Binding
	Status, Type, Labels, AssignMe   key.Binding
	Close, Browser, CopyURL, Undo    key.Binding
	Refresh, MoveLeft, MoveRight     key.Binding
}

func newKeyMap() keyMap {
	b := func(help, desc string, keys ...string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
	}
	return keyMap{
		Up:         b("↑/k", "up", "up", "k"),
		Down:       b("↓/j", "down", "down", "j"),
		Top:        b("g", "first", "g", "home"),
		Bottom:     b("G", "last", "G", "end"),
		PageUp:     b("pgup", "page up", "pgup", "ctrl+u"),
		PageDown:   b("pgdn", "page down", "pgdown", "ctrl+d"),
		Open:       b("enter", "read", "enter", "l", "right"),
		Back:       b("esc", "back", "esc", "h", "left"),
		NextView:   b("tab", "next view", "tab"),
		PrevView:   b("shift+tab", "previous view", "shift+tab"),
		Filter:     b("/", "filter", "/"),
		Palette:    b(":", "commands", ":", "ctrl+k", "ctrl+p"),
		Help:       b("?", "help", "?"),
		Quit:       b("q", "quit", "q", "ctrl+c"),
		Board:      b("b", "board/list", "b"),
		JumpIssue:  b("#", "jump to issue", "#"),
		SwitchRepo: b("R", "pick repo", "R"),
		Sidebar:    b("h/←", "sidebar", "h", "left"),

		New:          b("n", "new issue", "n"),
		Edit:         b("e", "edit", "e"),
		EditExternal: b("E", "edit in $EDITOR", "E"),
		Comment:      b("c", "comment", "c"),
		Status:       b("s", "status", "s"),
		Type:         b("t", "type", "t"),
		Labels:       b("L", "labels", "L"),
		AssignMe:     b("a", "assign me", "a"),
		Close:        b("x", "close/reopen", "x"),
		Browser:      b("o", "open in browser", "o"),
		CopyURL:      b("y", "copy link", "y"),
		Undo:         b("u", "undo", "u"),
		Refresh:      b("r", "refresh", "r"),
		MoveLeft:     b("<", "status back", "<", "["),
		MoveRight:    b(">", "status forward", ">", "]"),
	}
}

// footer lists the handful of bindings worth showing at all times.
func (k keyMap) footer(hasIssue bool) []key.Binding {
	if !hasIssue {
		return []key.Binding{k.New, k.Filter, k.NextView, k.Palette, k.Help}
	}
	return []key.Binding{k.New, k.Status, k.Edit, k.Comment, k.Filter, k.Palette, k.Help}
}

type helpSection struct {
	title    string
	bindings []key.Binding
}

func (k keyMap) sections() []helpSection {
	return []helpSection{
		{"Move", []key.Binding{k.Up, k.Down, k.Top, k.Bottom, k.Open, k.Back, k.Sidebar, k.NextView, k.PrevView, k.JumpIssue, k.SwitchRepo}},
		{"Change", []key.Binding{k.New, k.Edit, k.EditExternal, k.Comment, k.Status, k.MoveRight, k.MoveLeft, k.Type, k.Labels, k.AssignMe, k.Close, k.Undo}},
		{"View", []key.Binding{k.Filter, k.Board, k.Refresh, k.Browser, k.CopyURL, k.Palette, k.Help, k.Quit}},
	}
}
