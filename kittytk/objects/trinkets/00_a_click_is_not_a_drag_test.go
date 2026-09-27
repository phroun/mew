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

// A drag that maximizes an MDI child undoes it as soon as the pointer comes
// back into the pane, and the child comes back under the pointer.
//
// The grab is the point of the child the pointer is holding, and the child is
// placed to keep that point under it. Maximizing changes the child's geometry
// under the pointer, so the grab has to be re-expressed in the frame it is now
// holding; the restore that unwinds it works from that number.
func TestComingBackIntoThePaneUndoesTheSnap(t *testing.T) {
	pane := NewMDIPane()
	pane.SetBounds(core.UnitRect{X: 0, Y: 32, Width: 800, Height: 600})

	win := window.NewWindow("child")
	pane.AddWindow(win)
	win.SetBounds(core.UnitRect{X: 40, Y: 64, Width: 320, Height: 240})

	client := pane.ClientArea()
	fr := win.FrameRect()
	b := win.Bounds()
	// A quarter of the way along the title bar.
	at := core.UnitPoint{X: b.X + fr.X + fr.Width/4, Y: b.Y + fr.Y + 8}
	want := float64(at.X-b.X-fr.X) / float64(fr.Width)

	pane.HandleMousePress(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	up := core.UnitPoint{X: 400, Y: client.Y - 8}
	pane.HandleMouseMove(core.MouseMoveEvent{X: up.X, Y: up.Y, Buttons: core.LeftButton})
	if !win.IsMaximized() {
		t.Fatal("dragging above the pane's top did not maximize the child")
	}

	down := core.UnitPoint{X: 400, Y: client.Y + 4}
	pane.HandleMouseMove(core.MouseMoveEvent{X: down.X, Y: down.Y, Buttons: core.LeftButton})
	if win.IsMaximized() {
		t.Fatal("coming back into the pane left the child maximized")
	}

	b, fr = win.Bounds(), win.FrameRect()
	got := float64(down.X-b.X-fr.X) / float64(fr.Width)
	if got < want-0.03 || got > want+0.03 {
		t.Errorf("the pointer is %.3f along the title bar; it grabbed at %.3f", got, want)
	}
	pane.HandleMouseRelease(core.MouseReleaseEvent{X: down.X, Y: down.Y, Button: core.LeftButton})
}
