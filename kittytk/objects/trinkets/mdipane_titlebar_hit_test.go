package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// graphicalFrameStub is a container parent that reports graphical window
// frames with a nonzero border, so a child window's titlebar sits inside
// the top border (the window.go client-area contract).
type graphicalFrameStub struct {
	*Panel
	border core.Unit
}

func (g *graphicalFrameStub) GraphicalWindowFrames() bool       { return true }
func (g *graphicalFrameStub) WindowFrameBorderUnits() core.Unit { return g.border }

// On a graphical frame the titlebar is drawn inside the top border, so the
// draggable row runs from the border down a full cell. The MDI press path
// used to cut it off a border-thickness short of the titlebar's bottom, so
// grabbing the lower strip of the visible title did nothing. A press there
// must now begin a window drag.
func TestMDITitleBarDragCoversBorderOffset(t *testing.T) {
	stub := &graphicalFrameStub{Panel: NewPanel(), border: 2}

	pane := NewMDIPane()
	pane.SetParent(stub)
	pane.SetBounds(core.UnitRect{Width: 800, Height: 600})

	win := window.NewWindow("child")
	pane.AddWindow(win)
	win.SetBounds(core.UnitRect{X: 40, Y: 40, Width: 320, Height: 240})

	metrics := pane.EffectiveCellMetrics()
	border := core.FindFrameBorderUnits(win)
	if border <= 0 {
		t.Fatalf("expected a nonzero graphical frame border, got %d", border)
	}

	// A point in the bottom strip of the titlebar: below the old cutoff
	// (bounds.Y + CellHeight) but within the real titlebar
	// (bounds.Y + border + CellHeight). Mid-width so it is not a resize
	// grip.
	y := 40 + metrics.CellHeight + border - 1
	pane.HandleMousePress(core.MousePressEvent{X: 40 + 160, Y: y, Button: core.LeftButton})

	if pane.dragging != win {
		t.Errorf("press at titlebar-bottom (y=%d) did not start a drag; dragging=%v", y, pane.dragging)
	}
}

// Clicking the minimize button of an MDI child (press then release on the
// button) must fire the pane's minimize handler, which the demo turns into
// a dock entry.
func TestMDIMinimizeButtonFiresHandler(t *testing.T) {
	stub := &graphicalFrameStub{Panel: NewPanel(), border: 2}

	pane := NewMDIPane()
	pane.SetParent(stub)
	pane.SetBounds(core.UnitRect{Width: 800, Height: 600})

	minimized := 0
	pane.SetOnWindowMinimized(func(*window.Window) { minimized++ })

	win := window.NewWindow("child")
	pane.AddWindow(win)
	win.SetBounds(core.UnitRect{X: 40, Y: 40, Width: 320, Height: 240})
	pane.ActivateWindow(win)

	metrics := pane.EffectiveCellMetrics()
	border := core.FindFrameBorderUnits(win)
	bw := metrics.TextWidth(3) // titlebar button width [x]/[_]/[^]

	// Minimize is the second control button after the close button; both
	// sit inside the left border, offset by the frame border on graphical
	// frames. Aim at its center.
	bx := 40 + border + metrics.CellWidth + bw + bw/2
	by := 40 + border + metrics.CellHeight/2

	pane.HandleMousePress(core.MousePressEvent{X: bx, Y: by, Button: core.LeftButton})
	pane.HandleMouseRelease(core.MouseReleaseEvent{X: bx, Y: by, Button: core.LeftButton})

	if minimized != 1 {
		t.Errorf("minimize handler fired %d times, want 1 (button at %d,%d)", minimized, bx, by)
	}
	if !win.IsMinimized() {
		t.Error("window is not minimized after clicking its minimize button")
	}
}

// Resizing an MDI child window shows the translucent white edge overlay
// (SetResizeHoverRects), the same one desktop windows get, and drops it on
// release.
func TestMDIResizeShowsEdgeOverlay(t *testing.T) {
	stub := &graphicalFrameStub{Panel: NewPanel(), border: 2}

	pane := NewMDIPane()
	pane.SetParent(stub)
	pane.SetBounds(core.UnitRect{Width: 800, Height: 600})

	win := window.NewWindow("child")
	pane.AddWindow(win)
	win.SetBounds(core.UnitRect{X: 60, Y: 60, Width: 320, Height: 240})

	// Grab the bottom-right corner to start a resize.
	bx := win.Bounds().X + win.Bounds().Width - 1
	by := win.Bounds().Y + win.Bounds().Height - 1
	pane.HandleMousePress(core.MousePressEvent{X: bx, Y: by, Button: core.LeftButton})
	if len(win.ResizeHoverRects()) == 0 {
		t.Fatal("starting an MDI resize did not set the edge overlay")
	}

	// Drag keeps it; release drops it.
	pane.HandleMouseMove(core.MouseMoveEvent{X: bx + 20, Y: by + 20, Buttons: core.LeftButton})
	if len(win.ResizeHoverRects()) == 0 {
		t.Error("overlay vanished mid-resize")
	}
	pane.HandleMouseRelease(core.MouseReleaseEvent{X: bx + 20, Y: by + 20, Button: core.LeftButton})
	if len(win.ResizeHoverRects()) != 0 {
		t.Error("overlay not cleared after the resize ended")
	}
}
