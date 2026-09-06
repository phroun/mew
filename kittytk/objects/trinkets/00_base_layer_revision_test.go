package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// The room a bounded window leaves around itself when maximized is painted on
// the DESKTOP's layer, out of that window's geometry. The base layer's
// revision nets out the windows the compositor draws on layers of their own --
// so that a keystroke in a window does not repaint the whole desktop -- and
// that subtraction cancels exactly the bumps maximizing a window makes.
//
// A compositing host reads this number and repaints the base only when it
// moves. If it does not move, the shade is painted into a texture nobody asks
// for again, and the window maximizes over bare desktop.
func TestMaximizingMovesTheBaseLayerRevision(t *testing.T) {
	px, err := raster.New(1200, 800)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)
	d.SetBounds(core.UnitRect{Width: 1200, Height: 800})
	wm := d.WindowManager()

	win := window.NewWindow("Bounded")
	win.SetMaximumSize(core.UnitSize{Width: 480, Height: 320})
	win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 280, Height: 160})
	wm.AddWindow(win)

	base := func() uint64 {
		t.Helper()
		list := d.GetChildWindows()
		if list == nil || !list.HasBaseRevision {
			t.Fatal("the desktop offers no base revision")
		}
		return list.BaseRevision
	}

	// The number is a cache key: it must sit still while nothing changes, or
	// the base layer repaints every frame and the saving it exists for is
	// gone.
	if a, b := base(), base(); a != b {
		t.Fatalf("the base revision moved from %d to %d with nothing changed", a, b)
	}

	before := base()
	wm.MaximizeWindow(win)
	if got := win.Bounds(); got.Width != 480 {
		t.Fatalf("maximized it is %v, want its maximum of 480 wide", got.Size())
	}
	if after := base(); after == before {
		t.Errorf("the base revision stayed at %d across a maximize, so the shade "+
			"around the window is painted into a layer the host never asks for again", before)
	}

	// And back: restoring takes the room away again, which is just as much a
	// change to what the base layer paints.
	mid := base()
	wm.RestoreWindow(win)
	if after := base(); after == mid {
		t.Errorf("the base revision stayed at %d across a restore, so the shade "+
			"stays on screen around a window that is no longer maximized", mid)
	}
}
