package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// hitRegion is a clickable rectangle on screen, recorded while rendering so
// clicks always match what's drawn.
type hitRegion struct {
	x, y, w, h int
	click      func(m *Model, msg tea.MouseClickMsg) tea.Cmd
	wheel      func(m *Model, delta int) tea.Cmd
}

func (r hitRegion) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// hit records a region with absolute screen coordinates.
func (m *Model) hit(r hitRegion) {
	if r.w > 0 && r.h > 0 {
		m.hits = append(m.hits, r)
	}
}

// captureHits runs render with regions recorded relative to the rendered
// block's top-left corner, and returns them instead of keeping them, so
// the caller can place them once it knows where the block goes.
func (m *Model) captureHits(render func() string) (string, []hitRegion) {
	start := len(m.hits)
	out := render()
	captured := append([]hitRegion(nil), m.hits[start:]...)
	m.hits = m.hits[:start]
	return out, captured
}

// placeHits records captured regions offset to where their block landed.
func (m *Model) placeHits(regions []hitRegion, dx, dy int) {
	for _, r := range regions {
		r.x += dx
		r.y += dy
		m.hit(r)
	}
}

// regionAt returns the topmost region at a point: later regions are drawn
// over earlier ones.
func (m *Model) regionAt(x, y int) (hitRegion, bool) {
	for i := len(m.hits) - 1; i >= 0; i-- {
		if m.hits[i].contains(x, y) {
			return m.hits[i], true
		}
	}
	return hitRegion{}, false
}

// handleMouse routes clicks and the wheel to whatever is under the pointer.
func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	mouse := msg.Mouse()
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return nil
		}
		if r, ok := m.regionAt(mouse.X, mouse.Y); ok && r.click != nil {
			return r.click(m, msg)
		}
	case tea.MouseWheelMsg:
		delta := 0
		switch msg.Button {
		case tea.MouseWheelUp:
			delta = -1
		case tea.MouseWheelDown:
			delta = 1
		default:
			return nil
		}
		for i := len(m.hits) - 1; i >= 0; i-- {
			if r := m.hits[i]; r.contains(mouse.X, mouse.Y) && r.wheel != nil {
				return r.wheel(m, delta)
			}
		}
	}
	return nil
}

// doubleClick reports whether a click on target follows a click on the
// same target closely enough to count as a double click.
func (m *Model) doubleClick(target string) bool {
	now := time.Now()
	double := m.lastClickTarget == target && now.Sub(m.lastClickAt) < 400*time.Millisecond
	m.lastClickTarget, m.lastClickAt = target, now
	if double {
		m.lastClickTarget = ""
	}
	return double
}

// openLinkClick reports whether a click asks to follow a link: ctrl+click,
// or cmd+click where the terminal passes it through.
func openLinkClick(msg tea.MouseClickMsg) bool {
	return msg.Mod&(tea.ModCtrl|tea.ModSuper|tea.ModMeta) != 0
}
