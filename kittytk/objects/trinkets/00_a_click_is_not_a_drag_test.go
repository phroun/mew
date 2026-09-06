package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// A click on a maximized MDI child's title bar leaves it maximized.
//
// A click carries a position report of its own, so a stationary one arrives as
// move, press, move, release with both moves at the point the press was. A
// drag begins where the pointer leaves that point, so none of those moves is
// one, and a maximized child comes down only for a drag that pulls it down.
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

	// The position report carries no button; only a drag names one.
	pane.HandleMouseMove(core.MouseMoveEvent{X: at.X, Y: at.Y})
	pane.HandleMousePress(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	pane.HandleMouseMove(core.MouseMoveEvent{X: at.X, Y: at.Y})
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
