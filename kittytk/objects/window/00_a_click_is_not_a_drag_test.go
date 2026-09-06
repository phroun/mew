package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A click on a maximized window's title bar leaves it maximized.
//
// A terminal reports where the pointer is before every button action, so one
// click arrives as move, press, move, release -- and the move between the
// press and the release is at the point the press was, which is no motion at
// all. Taken as a drag it asks the window to move out of the menu bar, and a
// maximized window answers that by restoring: one click on the title bar and
// the window came down.
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

	m.HandleMouseMove(core.MouseMoveEvent{X: at.X, Y: at.Y})
	m.HandleMousePress(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	m.HandleMouseMove(core.MouseMoveEvent{X: at.X, Y: at.Y, Buttons: core.LeftButton})
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
