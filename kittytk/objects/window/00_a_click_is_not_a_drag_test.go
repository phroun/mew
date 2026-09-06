package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A click on a maximized window's title bar leaves it maximized.
//
// A click carries a position report of its own, so a stationary one arrives as
// move, press, move, release with both moves at the point the press was. A
// drag begins where the pointer leaves that point, so none of those moves is
// one, and a maximized window comes down only for a drag that pulls it down.
func TestAClickOnAMaximizedTitleBarIsNotADrag(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})

	win := NewWindow("w")
	win.SetBounds(core.UnitRect{X: 80, Y: 80, Width: 320, Height: 200})
	m.AddWindow(win)
	m.MaximizeWindow(win)
	if !win.IsMaximized() {
		t.Fatal("the window did not maximize")
	}

	frame := win.FrameRect()
	at := core.UnitPoint{X: frame.X + frame.Width/2, Y: frame.Y + 8}

	// The position report carries no button; only a drag names one.
	m.HandleMouseMove(core.MouseMoveEvent{X: at.X, Y: at.Y})
	m.HandleMousePress(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	m.HandleMouseMove(core.MouseMoveEvent{X: at.X, Y: at.Y})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: at.X, Y: at.Y, Button: core.LeftButton})

	if !win.IsMaximized() {
		t.Error("one click on the title bar restored the window")
	}
}

// Dragging one down still restores it, so the click that does nothing has not
// cost the gesture that does.
func TestDraggingAMaximizedTitleBarDownRestoresIt(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})

	win := NewWindow("w")
	win.SetBounds(core.UnitRect{X: 80, Y: 80, Width: 320, Height: 200})
	m.AddWindow(win)
	m.MaximizeWindow(win)

	frame := win.FrameRect()
	at := core.UnitPoint{X: frame.X + frame.Width/2, Y: frame.Y + 8}

	m.HandleMousePress(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	m.HandleMouseMove(core.MouseMoveEvent{X: at.X + 48, Y: at.Y + 64, Buttons: core.LeftButton})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: at.X + 48, Y: at.Y + 64, Button: core.LeftButton})

	if win.IsMaximized() {
		t.Error("dragging the title bar down left the window maximized")
	}
}
