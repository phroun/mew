package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// Dragging an MDI child off the left, right, or bottom must NOT snap-maximize -
// only the pointer crossing above the pane's top edge does.
func TestMDIDragMaximizeOnlyOffTop(t *testing.T) {
	newDrag := func() (*MDIPane, *window.Window) {
		m := NewMDIPane()
		m.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 600})
		win := window.NewWindow("child")
		win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 240, Height: 160})
		m.AddWindow(win)
		m.mu.Lock()
		m.dragging = win
		m.dragOffsetX = 20
		m.dragOffsetY = 8
		m.mu.Unlock()
		return m, win
	}

	// Off the bottom.
	m, win := newDrag()
	m.HandleMouseMove(core.MouseMoveEvent{X: 120, Y: 700})
	if win.IsMaximized() {
		t.Error("dragging off the bottom maximized the MDI child")
	}

	// Off the left (pointer x negative, y mid-pane).
	m, win = newDrag()
	m.HandleMouseMove(core.MouseMoveEvent{X: -40, Y: 300})
	if win.IsMaximized() {
		t.Error("dragging off the left maximized the MDI child")
	}

	// Off the right.
	m, win = newDrag()
	m.HandleMouseMove(core.MouseMoveEvent{X: 900, Y: 300})
	if win.IsMaximized() {
		t.Error("dragging off the right maximized the MDI child")
	}

	// Pointer above the pane top: this one should maximize.
	m, win = newDrag()
	m.HandleMouseMove(core.MouseMoveEvent{X: 120, Y: -5})
	if !win.IsMaximized() {
		t.Error("dragging the pointer above the pane top did not maximize")
	}
}
