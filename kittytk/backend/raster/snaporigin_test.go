package raster

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// With the snap origin anchored at a window's origin, a content point's
// pixel offset from that origin is invariant to where the window sits -
// which is exactly the "no jitter when dragging" property: moving the
// window (changing its unit origin) must not shift the interior relative
// to the window's top-left. Without the origin (absolute snapping) this
// offset wobbles by a pixel at fractional cell sizes.
func TestSnapOriginKeepsInteriorStable(t *testing.T) {
	for _, fs := range []int{10, 12, 13, 18} {
		b, err := New(4000, 200)
		if err != nil {
			t.Fatal(err)
		}
		b.SetFontSize(fs)

		// A representative set of sub-cell content offsets.
		for _, lx := range []core.Unit{0, 1, 3, 5, 8, 13, 20, 37} {
			var want int
			for bx := core.Unit(0); bx < 40; bx++ {
				b.SetSnapOrigin(bx, 0)
				got := b.pxX(bx+lx) - b.pxX(bx) // content offset from window origin, px
				if bx == 0 {
					want = got
					continue
				}
				if got != want {
					t.Errorf("fs=%d lx=%d: content offset from origin = %d at bx=%d, want stable %d",
						fs, lx, got, bx, want)
				}
			}
		}
		b.SetSnapOrigin(0, 0)
	}
}

// SetSnapOrigin round-trips: setting an origin then restoring (0,0) leaves
// the mapping exactly where it started (so a window paint cleanly restores
// the global snapping for the chrome painted after it).
func TestSnapOriginRestores(t *testing.T) {
	b, err := New(1000, 200)
	if err != nil {
		t.Fatal(err)
	}
	b.SetFontSize(13)
	before := b.pxX(37)
	prevX, prevY := b.SetSnapOrigin(19, 5)
	b.SetSnapOrigin(prevX, prevY)
	if got := b.pxX(37); got != before {
		t.Errorf("pxX(37) = %d after set+restore, want %d", got, before)
	}
}
