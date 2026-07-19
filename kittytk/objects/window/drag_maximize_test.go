package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Snap-maximize during a title drag must key off the POINTER entering the menu
// bar strip, not the window's top edge being lifted there by the grab offset.
func TestDragSnapMaximizeFollowsPointerNotEdge(t *testing.T) {
	newDrag := func() (*WindowManager, *Window) {
		m := NewWindowManager()
		m.SetSmoothPositioning(true) // graphical path
		// Client area starts at Y=16, so 0..15 is the menu-bar strip.
		m.SetScreenBounds(core.UnitRect{X: 0, Y: 16, Width: 800, Height: 600})
		win := NewWindow("w")
		win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 240, Height: 160})
		m.AddWindow(win)
		// Simulate an in-progress title drag grabbed 5 units into the titlebar.
		m.mu.Lock()
		m.dragging = win
		m.dragOffsetX = 20
		m.dragOffsetY = 5
		m.mu.Unlock()
		return m, win
	}

	// Pointer still below the menu bar (event.Y == clientArea.Y), even though
	// the window's top edge (event.Y-offsetY = 11) is up in the strip: must NOT
	// maximize.
	m, win := newDrag()
	m.HandleMouseMove(core.MouseMoveEvent{X: 120, Y: 16})
	if win.IsMaximized() {
		t.Error("window maximized while the pointer was still below the menu bar")
	}

	// Pointer itself moves into the menu-bar strip (event.Y < clientArea.Y):
	// now it maximizes.
	m, win = newDrag()
	m.HandleMouseMove(core.MouseMoveEvent{X: 120, Y: 8})
	if !win.IsMaximized() {
		t.Error("window did not maximize when the pointer entered the menu bar")
	}
}
