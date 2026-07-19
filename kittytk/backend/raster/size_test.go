package raster

import "testing"

// Size must invert the same cell-snapped mapping content is placed with,
// so the surface's right/bottom edge in units maps back to a pixel AT or
// just within the true surface edge - never beyond it (which clipped
// right-aligned content like the menu-bar clock at fractional cell sizes).
func TestSizeRoundTripsWithinSurface(t *testing.T) {
	for _, fs := range []int{10, 12, 13, 18} {
		for _, wh := range [][2]int{{1024, 768}, {1000, 500}, {1365, 911}} {
			b, err := New(wh[0], wh[1])
			if err != nil {
				t.Fatal(err)
			}
			b.SetFontSize(fs)
			sz := b.Size()
			rx := b.pxX(sz.Width)
			ry := b.pxY(sz.Height)
			if rx > b.w {
				t.Errorf("fs=%d w=%d: pxX(Size().Width)=%d exceeds surface width %d", fs, wh[0], rx, b.w)
			}
			if b.w-rx >= b.cellWPx() {
				t.Errorf("fs=%d w=%d: pxX(Size().Width)=%d is more than a cell short of %d", fs, wh[0], rx, b.w)
			}
			if ry > b.h {
				t.Errorf("fs=%d h=%d: pxY(Size().Height)=%d exceeds surface height %d", fs, wh[1], ry, b.h)
			}
		}
	}
}
