package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A surface size the host did NOT choose can fall between units — an OS
// configure event when the app is switched back in, a compositor adjusting a
// corner drag, a work-area zoom. Reading it back to the NEAREST unit answers
// one unit too many (at 2 device px per unit, 101px reads as 51 units = 102px
// of paint), and the frame then strokes its right edge against a column the
// surface does not have, so that edge draws a pixel thin.
//
// The window must never claim more extent than the surface can paint.
func TestResizedNeverClaimsMoreThanTheSurfaceCanPaint(t *testing.T) {
	for _, tc := range []struct {
		name         string
		pxW, pxH     int
		wantW, wantH core.Unit
	}{
		{"exact size round-trips", 200, 120, 100, 60},
		{"odd width floors", 201, 120, 100, 60},
		{"odd height floors", 200, 121, 100, 60},
		{"odd corner floors both", 201, 121, 100, 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			win := NewWindow("torn")
			surf := &nativeFakeSurface{
				size: core.UnitSize{Width: 100, Height: 60},
				pxW:  tc.pxW, pxH: tc.pxH,
			}
			h := NewTearOffHost(win, surf, ppu2, func() (int, int) { return 0, 0 }, nil)

			h.Resized(core.UnitSize{Width: core.Unit(tc.pxW), Height: core.Unit(tc.pxH)})

			b := win.Bounds()
			if b.Width != tc.wantW || b.Height != tc.wantH {
				t.Errorf("bounds %dx%d units, want %dx%d", b.Width, b.Height, tc.wantW, tc.wantH)
			}
			// The real invariant behind those numbers: what the frame paints
			// must fit inside the pixels the surface actually has.
			if px := h.pxHardX(b.Width); px > tc.pxW {
				t.Errorf("frame paints %dpx wide into a %dpx surface", px, tc.pxW)
			}
			if py := h.pxHardY(b.Height); py > tc.pxH {
				t.Errorf("frame paints %dpx tall into a %dpx surface", py, tc.pxH)
			}
		})
	}
}

// Stepping down from the nearest unit must not reintroduce the drift that
// flooring the raw ratio caused: a surface sized to exactly pxHardX(W) reads
// back as W, cycle after cycle, shedding nothing.
func TestResizedDoesNotDriftAcrossCycles(t *testing.T) {
	win := NewWindow("torn")
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 100, Height: 60}, pxW: 200, pxH: 120}
	h := NewTearOffHost(win, surf, ppu2, func() (int, int) { return 0, 0 }, nil)

	for i := 0; i < 20; i++ {
		b := win.Bounds()
		// Re-assert the size the window currently claims, as an undock/zoom
		// round-trip does, then let the surface report back.
		surf.SetScreenSizePx(h.pxHardX(b.Width), h.pxHardY(b.Height))
	}
	if b := win.Bounds(); b.Width != 100 || b.Height != 60 {
		t.Errorf("drifted to %dx%d units over 20 cycles, want 100x60", b.Width, b.Height)
	}
}

// The drag itself still rounds its REQUEST down, so we never ask the OS for a
// size we cannot paint in the first place. (The adoption-side floor above is
// the backstop for sizes we do not choose; this is the front door.)
func TestResizeDragRequestsAPaintableSize(t *testing.T) {
	for _, px := range []int{200, 201, 255, 4097} {
		if got := (&TearOffHost{ppu: ppu2}).paintablePxX(px); got%2 != 0 || got > px || px-got > 1 {
			t.Errorf("paintablePxX(%d) = %d, want the even extent at or just below it", px, got)
		}
	}
}
