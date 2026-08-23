package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A torn window must NOT jolt when the drag begins: with the pointer
// stationary, the first dragMove keeps the window exactly where the press
// found it.
//
// The old anchor lost this by construction. The pointer arrives in pixels,
// the surface divides it into UNITS for the event (dropping the sub-unit
// part), and the drag multiplied it back — so the grab point was off by up
// to one pixels-per-unit, and the window snapped sideways by that much on
// the first motion. At the default zoom ppu is 2 and it hid inside a
// pixel; zoomed in it is 3+ and visible. That is exactly "the tracking of
// the anchor when dragging a torn off window seems slightly off after
// rescaling".
func TestTornDragDoesNotJoltAtHigherZoom(t *testing.T) {
	for _, ppuVal := range []float64{2, 3, 10.0 / 3} {
		surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}, pxW: 600, pxH: 300}
		surf.x, surf.y = 500, 400

		// The pointer pressed at global (517, 411): 17 px into the title bar.
		gx, gy := 517, 411
		global := func() (int, int) { return gx, gy }

		win := NewWindow("torn")
		h := NewTearOffHost(win, surf, func() float64 { return ppuVal }, global, nil)

		// The press reaches the window in UNITS — the platform divides, and
		// the sub-unit remainder is gone before anyone sees it. beginDragAt
		// is the internal arming every real title-bar press goes through.
		h.beginDragAt(core.Unit(float64(17)/ppuVal), core.Unit(float64(11)/ppuVal))

		// Pointer has not moved: the window must not either.
		h.dragMove()
		if surf.x != 500 || surf.y != 400 {
			t.Errorf("ppu %.2f: window jolted to (%d,%d) on a stationary pointer, want (500,400)",
				ppuVal, surf.x, surf.y)
		}

		// And a real motion tracks pixel-for-pixel.
		gx, gy = 525, 419
		h.dragMove()
		if surf.x != 508 || surf.y != 408 {
			t.Errorf("ppu %.2f: moved to (%d,%d), want (508,408) — pixel-exact tracking", ppuVal, surf.x, surf.y)
		}
	}
}

// Without a placed OS window at arm time the pixel anchor cannot be captured;
// the drag falls back to the unit conversion — the old behaviour — rather
// than pinning the window to a garbage anchor.
func TestTornDragFallsBackWithoutANative(t *testing.T) {
	surf := &fakeSurface{size: core.UnitSize{Width: 200, Height: 100}}
	win := NewWindow("torn")
	h := NewTearOffHost(win, surf, func() float64 { return 2 }, func() (int, int) { return 100, 100 }, nil)
	h.beginDragAt(5, 3)
	if h.grabPxValid {
		t.Error("no native surface: the pixel anchor must not claim validity")
	}
	if x, y := h.grabPx(); x != 10 || y != 6 {
		t.Errorf("fallback grab = (%d,%d), want the unit conversion (10,6)", x, y)
	}
}
