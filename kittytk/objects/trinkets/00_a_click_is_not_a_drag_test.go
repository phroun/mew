package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// A click on a maximized MDI child's title bar leaves it maximized.
//
// A terminal reports where the pointer is before every button action, so one
// click arrives as move, press, move, release -- and the move between the
// press and the release is at the point the press was, which is no motion at
// all. Taken as a drag it asks the child to move down into the pane, and a
// maximized child answers that by restoring.
func TestAClickOnAMaximizedMDITitleBarIsNotADrag(t *testing.T) {
	pane := NewMDIPane()
	pane.SetBounds(core.UnitRect{Width: 800, Height: 600})

	win := window.NewWindow("child")
	pane.AddWindow(win)
	win.SetBounds(core.UnitRect{X: 40, Y: 32, Width: 320, Height: 240})
	pane.MaximizeWindow(win)
	if !win.IsMaximized() {
		t.Fatal("the child did not maximize")
	}

	frame := win.FrameRect()
	at := core.UnitPoint{X: frame.X + frame.Width/2, Y: frame.Y + 8}

	pane.HandleMouseMove(core.MouseMoveEvent{X: at.X, Y: at.Y})
	pane.HandleMousePress(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	pane.HandleMouseMove(core.MouseMoveEvent{X: at.X, Y: at.Y, Buttons: core.LeftButton})
	pane.HandleMouseRelease(core.MouseReleaseEvent{X: at.X, Y: at.Y, Button: core.LeftButton})

	if !win.IsMaximized() {
		t.Error("one click on the title bar restored the child")
	}
}

// Dragging one down still restores it.
func TestDraggingAMaximizedMDITitleBarDownRestoresIt(t *testing.T) {
	pane := NewMDIPane()
	pane.SetBounds(core.UnitRect{Width: 800, Height: 600})

	win := window.NewWindow("child")
	pane.AddWindow(win)
	win.SetBounds(core.UnitRect{X: 40, Y: 32, Width: 320, Height: 240})
	pane.MaximizeWindow(win)

	frame := win.FrameRect()
	at := core.UnitPoint{X: frame.X + frame.Width/2, Y: frame.Y + 8}

	pane.HandleMousePress(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	pane.HandleMouseMove(core.MouseMoveEvent{X: at.X + 48, Y: at.Y + 64, Buttons: core.LeftButton})
	pane.HandleMouseRelease(core.MouseReleaseEvent{X: at.X + 48, Y: at.Y + 64, Button: core.LeftButton})

	if win.IsMaximized() {
		t.Error("dragging the title bar down left the child maximized")
	}
}
