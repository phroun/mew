package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// Dragging a maximized MDI child's title bar restores it under the pointer,
// and the grab has to be re-expressed against the frame being held. On a
// capped child that frame sits in the middle of the pane it holds, so an
// offset measured from the pane's corner carries the frame's whole inset:
// the child lands with the pointer well below the title bar it grabbed and
// far across it, since the width it scaled by was the pane's.
func TestTheGrabSurvivesAnMDIChildsRestore(t *testing.T) {
	pane := NewMDIPane()
	pane.SetBounds(core.UnitRect{Width: 800, Height: 608})

	win := window.NewWindow("bounded")
	win.SetMaximumSize(core.UnitSize{Width: 320, Height: 208})
	win.SetBounds(core.UnitRect{X: 16, Y: 16, Width: 200, Height: 112})
	pane.AddWindow(win)
	pane.MaximizeWindow(win)

	fr := win.FrameRect()
	if fr.X == 0 && fr.Y == 0 {
		t.Fatal("the frame is not inset; this test proves nothing")
	}

	// Grab the middle of the frame's title bar and drag down into the pane.
	b := win.Bounds()
	grab := core.UnitPoint{X: b.X + fr.X + fr.Width/2, Y: b.Y + fr.Y + 4}
	pane.HandleMousePress(core.MousePressEvent{X: grab.X, Y: grab.Y, Button: core.LeftButton})
	drop := core.UnitPoint{X: grab.X, Y: grab.Y + 200}
	pane.HandleMouseMove(core.MouseMoveEvent{X: drop.X, Y: drop.Y, Buttons: core.LeftButton})
	pane.HandleMouseRelease(core.MouseReleaseEvent{X: drop.X, Y: drop.Y, Button: core.LeftButton})

	if win.IsMaximized() {
		t.Fatal("dragging the title bar down did not restore the child")
	}
	nb := win.Bounds()
	if nb.Width != 200 || nb.Height != 112 {
		t.Fatalf("it restored to %v, want its own 200x112", nb.Size())
	}

	// The pointer is still on the title bar it grabbed...
	local := core.UnitPoint{X: drop.X - nb.X, Y: drop.Y - nb.Y}
	if local.Y < 0 || local.Y >= win.EffectiveCellMetrics().UnitsPerCellHeight*2 {
		t.Errorf("the pointer is %d down the restored child, past its title bar", local.Y)
	}
	// ...and proportionally across it: the middle stays the middle.
	if want := nb.Width / 2; local.X < want-16 || local.X > want+16 {
		t.Errorf("the pointer is %d across a %d-wide child, want about the middle (%d)",
			local.X, nb.Width, want)
	}
}

// The MDI pane keeps the same double-click rule as the desktop and the torn
// host: a press the WINDOW took -- a caption button -- disarms the title
// bar's tracker rather than being recorded in it. Recorded, it lets the next
// single title click pair with a button press and toggle maximize again.
func TestAnMDIPanesTrackerIsDisarmedByACaptionButton(t *testing.T) {
	pane := NewMDIPane()
	pane.SetBounds(core.UnitRect{Width: 800, Height: 608})
	win := window.NewWindow("child")
	win.SetBounds(core.UnitRect{X: 32, Y: 32, Width: 320, Height: 208})
	pane.AddWindow(win)

	// The [^] button, which the window handles itself.
	b := win.Bounds()
	bx, by := core.Unit(62), core.Unit(7)
	pane.HandleMousePress(core.MousePressEvent{X: b.X + bx, Y: b.Y + by, Button: core.LeftButton})
	pane.HandleMouseRelease(core.MouseReleaseEvent{X: b.X + bx, Y: b.Y + by, Button: core.LeftButton})
	if !win.IsMaximized() {
		t.Fatal("the button press did not maximize the child; the test is aiming at the wrong place")
	}

	pane.mu.RLock()
	armed := pane.titleClicks.Armed()
	pane.mu.RUnlock()
	if armed {
		t.Error("a caption button left a click in the title bar's double-click tracker")
	}
}

// A real double-click on an MDI child's title bar maximizes it, and a single
// click on the maximized child does nothing. The pane owns this gesture for
// its children exactly as the manager owns it for top-level windows.
func TestAnMDIChildsTitleDoubleClickMaximizesAndRestores(t *testing.T) {
	pane := NewMDIPane()
	pane.SetBounds(core.UnitRect{Width: 800, Height: 608})
	win := window.NewWindow("child")
	win.SetBounds(core.UnitRect{X: 32, Y: 32, Width: 320, Height: 208})
	pane.AddWindow(win)

	click := func() {
		b := win.Bounds()
		fr := win.FrameRect()
		x, y := b.X+fr.X+fr.Width/2, b.Y+fr.Y+4
		pane.HandleMousePress(core.MousePressEvent{X: x, Y: y, Button: core.LeftButton})
		pane.HandleMouseRelease(core.MouseReleaseEvent{X: x, Y: y, Button: core.LeftButton})
	}

	click()
	click()
	if !win.IsMaximized() {
		t.Fatal("a double-click on the child's title bar did not maximize it")
	}
	click()
	if !win.IsMaximized() {
		t.Error("a single click after the maximize restored the child")
	}
	click()
	if win.IsMaximized() {
		t.Error("a second click did not restore the child")
	}
}
