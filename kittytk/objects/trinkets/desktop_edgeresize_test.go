package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// The drag arithmetic alone: armed edges and a pointer delta onto the OS
// window's pixel rectangle, with the minimum absorbed by the moving edge so
// the opposite edge stays pinned.
func TestApplyHostResize(t *testing.T) {
	const sx, sy, sw, sh = 50, 60, 800, 480
	const minW, minH = 96, 64
	for _, c := range []struct {
		name       string
		edges      int
		dx, dy     int
		x, y, w, h int
	}{
		{"right grows", window.ResizeEdgeRight, 40, 0, sx, sy, 840, sh},
		{"bottom grows", window.ResizeEdgeBottom, 0, 25, sx, sy, sw, 505},
		{"left grows and moves", window.ResizeEdgeLeft, -20, 0, 30, sy, 820, sh},
		{"top grows and moves", window.ResizeEdgeTop, 0, -10, sx, 50, sw, 490},
		{"corner", window.ResizeEdgeRight | window.ResizeEdgeBottom, 15, 10, sx, sy, 815, 490},
		// Clamped from the right: width floors, origin untouched.
		{"right clamps", window.ResizeEdgeRight, -900, 0, sx, sy, minW, sh},
		// Clamped from the left: the LEFT edge absorbs the clamp, so the
		// right edge (sx+sw) stays exactly where it was.
		{"left clamps, right edge pinned", window.ResizeEdgeLeft, 900, 0, sx + sw - minW, sy, minW, sh},
		{"top clamps, bottom edge pinned", window.ResizeEdgeTop, 0, 900, sx, sy + sh - minH, sw, minH},
	} {
		x, y, w, h := applyHostResize(c.edges, sx, sy, sw, sh, c.dx, c.dy, minW, minH)
		if x != c.x || y != c.y || w != c.w || h != c.h {
			t.Errorf("%s: got (%d,%d %dx%d), want (%d,%d %dx%d)",
				c.name, x, y, w, h, c.x, c.y, c.w, c.h)
		}
	}
}

// End to end on the fake platform: hovering the desktop's own edge lights
// the affordance, pressing in the zone resizes the OS WINDOW — global
// pointer deltas onto its pixel geometry, the same way a torn window
// resizes — and the left edge moves the origin so the right edge pins.
func TestDesktopEdgeResizesTheHostWindow(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	plat := &msPlatform{}
	plat.script = func() {
		surf := plat.surfaces[0] // the desktop window: 800x480 units at (50,60), scale 1
		h := surf.handler

		// The grab rule with border 0 at ppu 1: max(0 + quarter cell 2, 3px)
		// = 3 units. The corner reaches to the affordance width, 4 units.
		h.Event(core.MouseMoveEvent{X: 798, Y: 240})
		if edges := d.hostHoverEdges(); edges != window.ResizeEdgeRight {
			t.Fatalf("hover at the right edge = %d, want right", edges)
		}
		h.Event(core.MouseMoveEvent{X: 400, Y: 240})
		if edges := d.hostHoverEdges(); edges != 0 {
			t.Fatalf("hover mid-surface = %d, want none", edges)
		}
		h.Event(core.MouseMoveEvent{X: 797, Y: 477})
		if edges := d.hostHoverEdges(); edges != window.ResizeEdgeRight|window.ResizeEdgeBottom {
			t.Fatalf("hover in the corner = %d, want right|bottom", edges)
		}

		// Drag the right edge 40px out.
		plat.gx, plat.gy = 848, 300
		h.Event(core.MousePressEvent{X: 798, Y: 240, Button: core.LeftButton})
		plat.gx = 888
		h.Event(core.MouseMoveEvent{X: 798, Y: 240, Buttons: core.LeftButton})
		if w, _ := surf.ScreenSizePx(); w != 840 {
			t.Errorf("right-edge drag: width %d, want 840", w)
		}
		if surf.x != 50 {
			t.Errorf("right-edge drag moved the origin to %d, want 50", surf.x)
		}
		h.Event(core.MouseReleaseEvent{X: 798, Y: 240, Button: core.LeftButton})

		// Drag the left edge 20px out: origin follows, right edge pinned.
		rightEdge := surf.x + int(surf.size.Width)
		plat.gx, plat.gy = 51, 300
		h.Event(core.MousePressEvent{X: 1, Y: 240, Button: core.LeftButton})
		plat.gx = 31
		h.Event(core.MouseMoveEvent{X: 1, Y: 240, Buttons: core.LeftButton})
		if surf.x != 30 {
			t.Errorf("left-edge drag: origin %d, want 30", surf.x)
		}
		if got := surf.x + int(surf.size.Width); got != rightEdge {
			t.Errorf("left-edge drag moved the right edge: %d, want %d", got, rightEdge)
		}
		h.Event(core.MouseReleaseEvent{X: 1, Y: 240, Button: core.LeftButton})

		// After release the gesture is disarmed: a plain move mid-surface
		// clears the hover and further moves resize nothing.
		h.Event(core.MouseMoveEvent{X: 400, Y: 240})
		w0, _ := surf.ScreenSizePx()
		plat.gx = 500
		h.Event(core.MouseMoveEvent{X: 405, Y: 240})
		if w, _ := surf.ScreenSizePx(); w != w0 {
			t.Errorf("released gesture kept resizing: width %d, want %d", w, w0)
		}

		d.QuitWithCode(0)
	}
	d.RunOn(plat)
}

// A press in the desktop's edge zone belongs to the DESKTOP, even when a
// child window is corralled hard against that edge: the sliver is the
// outermost thing on the surface, like the OS resize border it extends.
func TestDesktopEdgeOutranksACorralledWindow(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	plat := &msPlatform{}
	plat.script = func() {
		surf := plat.surfaces[0]
		h := surf.handler

		win := window.NewWindow("edge")
		win.SetBounds(core.UnitRect{X: 640, Y: 100, Width: 160, Height: 120})
		d.WindowManager().AddWindow(win)

		// Press at the surface's outermost column, inside the window's own
		// footprint: the desktop's zone wins, and the OS window resizes.
		plat.gx, plat.gy = 849, 220
		h.Event(core.MousePressEvent{X: 799, Y: 160, Button: core.LeftButton})
		plat.gx = 879
		h.Event(core.MouseMoveEvent{X: 799, Y: 160, Buttons: core.LeftButton})
		if w, _ := surf.ScreenSizePx(); w != 830 {
			t.Errorf("desktop edge did not take the press: width %d, want 830", w)
		}
		// ...and the child window's logical bounds never moved.
		if win.Bounds().X != 640 || win.Bounds().Width != 160 {
			t.Errorf("child window geometry changed: %+v", win.Bounds())
		}
		h.Event(core.MouseReleaseEvent{X: 799, Y: 160, Button: core.LeftButton})
		d.QuitWithCode(0)
	}
	d.RunOn(plat)
}

// The zones engage only where they can act: a cell surface (no graphical
// frames) never answers, however close to the edge the pointer sits.
func TestDesktopEdgeStandsDownOnCellSurfaces(t *testing.T) {
	d := NewDesktop()
	d.SetBackend(&nullBackend{})
	if edges := d.hostEdgeAt(0, 100); edges != 0 {
		t.Errorf("cell surface answered %d at the edge, want 0", edges)
	}
}

// The OS's own resize strip sits just OUTSIDE the client area, where no
// surface events arrive — so when the pointer steps off across an edge, the
// affordance would go dark exactly where resizing is still possible. The
// leave handler reads the global pointer instead, keeps the band lit across
// the combined strip, and a poll keeps it honest until the pointer returns
// or wanders off.
func TestAffordancePersistsIntoTheOSResizeStrip(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	plat := &msPlatform{}
	plat.script = func() {
		surf := plat.surfaces[0] // client rect: 50,60 .. 850,540 in screen px
		h := surf.handler

		// Hover our inner sliver, then step off the right edge into the OS
		// strip: the band must stay lit.
		h.Event(core.MouseMoveEvent{X: 798, Y: 240})
		plat.gx, plat.gy = 853, 300 // 3px past the client edge
		h.Event(core.MouseLeaveEvent{})
		if edges := d.hostHoverEdges(); edges != window.ResizeEdgeRight {
			t.Fatalf("band after stepping into the OS strip = %d, want right", edges)
		}

		// Slide along the strip toward the bottom corner: both bits light.
		plat.gy = 538
		d.hostOutsidePoll()
		if edges := d.hostHoverEdges(); edges != window.ResizeEdgeRight|window.ResizeEdgeBottom {
			t.Fatalf("band near the outside corner = %d, want right|bottom", edges)
		}

		// Wander past the strip: the band clears and the poll retires.
		plat.gx = 900
		d.hostOutsidePoll()
		if edges := d.hostHoverEdges(); edges != 0 {
			t.Fatalf("band beyond the strip = %d, want none", edges)
		}
		d.mu.RLock()
		timer := d.hostEdge.outsideTimer
		d.mu.RUnlock()
		if timer != nil {
			t.Error("outside poll still armed after the pointer wandered off")
		}

		// Above the client area is the TITLE BAR on a decorated window — a
		// move, not a resize — so leaving across the top lights nothing.
		plat.gx, plat.gy = 400, 55
		h.Event(core.MouseLeaveEvent{})
		if edges := d.hostHoverEdges(); edges != 0 {
			t.Fatalf("band in the title bar area = %d, want none", edges)
		}

		d.QuitWithCode(0)
	}
	d.RunOn(plat)
}
