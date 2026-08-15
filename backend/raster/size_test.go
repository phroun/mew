package raster

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The no-drift guarantee for a torn window is PIXEL STABILITY: a surface
// sized to UnitToPxX(W), read back to units (PxToUnitX) and re-mapped to
// pixels, lands on the SAME pixel width. Because the read-back never moves
// the pixel size, the surface can round-trip through units on every
// undock/zoom without creeping — unlike the floor inverse (Size), which
// sheds a pixel each cycle. This holds even in sub-pixel regimes (cell
// narrower than the denomination), where snapAxis is not injective and no
// inverse can be exact in units, but the pixel still settles.
func TestHardenedRoundTripIsPixelStable(t *testing.T) {
	for _, scale := range []int{1, 2, 3} {
		for _, fs := range []int{10, 12, 13, 15, 18, 25} {
			b, err := NewScaled(2048, 1536, scale)
			if err != nil {
				t.Fatal(err)
			}
			b.SetFontSize(fs)
			for _, w := range []core.Unit{1, 7, 8, 9, 100, 199, 200, 216, 800, 1000} {
				px := b.UnitToPxX(w)
				if got := b.UnitToPxX(b.PxToUnitX(px)); got != px {
					t.Errorf("scale=%d fs=%d w=%d: px %d -> units %d -> px %d (not stable)",
						scale, fs, w, px, b.PxToUnitX(px), got)
				}
			}
			for _, h := range []core.Unit{1, 15, 16, 17, 100, 240, 400, 768} {
				px := b.UnitToPxY(h)
				if got := b.UnitToPxY(b.PxToUnitY(px)); got != px {
					t.Errorf("scale=%d fs=%d h=%d: px %d -> units %d -> px %d (not stable)",
						scale, fs, h, px, b.PxToUnitY(px), got)
				}
			}
		}
	}
}

// In the normal regime (a cell is at least as wide as the denomination, so
// snapAxis is injective — every real HiDPI zoom), the hardened round-trip
// is exact in UNITS too: UnitToPxX(W) reads back as exactly W. This is the
// strong form of no-drift the floor inverse cannot provide.
func TestHardenedUnitRoundTripExactWhenInjective(t *testing.T) {
	for _, scale := range []int{2, 3} {
		for _, fs := range []int{12, 13, 15, 18, 25} {
			b, err := NewScaled(2048, 1536, scale)
			if err != nil {
				t.Fatal(err)
			}
			b.SetFontSize(fs)
			for _, w := range []core.Unit{1, 7, 8, 9, 100, 199, 200, 216, 800, 1000} {
				if got := b.PxToUnitX(b.UnitToPxX(w)); got != w {
					t.Errorf("scale=%d fs=%d: PxToUnitX(UnitToPxX(%d))=%d, want %d", scale, fs, w, got, w)
				}
			}
			for _, h := range []core.Unit{1, 15, 16, 17, 100, 240, 400, 768} {
				if got := b.PxToUnitY(b.UnitToPxY(h)); got != h {
					t.Errorf("scale=%d fs=%d: PxToUnitY(UnitToPxY(%d))=%d, want %d", scale, fs, h, got, h)
				}
			}
		}
	}
}

// SizeRounded round-trips the surface's own pixel extent back to units
// exactly for a surface sized on the hardened pitch, so the font-zoom
// re-size path preserves the unit size instead of flooring it away.
func TestSizeRoundedRecoversHardenedSize(t *testing.T) {
	for _, fs := range []int{12, 13, 15, 25} {
		for _, wh := range [][2]core.Unit{{200, 100}, {216, 137}, {1000, 500}} {
			b, err := New(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			b.SetFontSize(fs)
			wPx, hPx := b.UnitToPxX(wh[0]), b.UnitToPxY(wh[1])
			b2, err := New(wPx, hPx)
			if err != nil {
				t.Fatal(err)
			}
			b2.SetFontSize(fs)
			if sz := b2.SizeRounded(); sz.Width != wh[0] || sz.Height != wh[1] {
				t.Errorf("fs=%d: SizeRounded()=%dx%d, want %dx%d", fs, sz.Width, sz.Height, wh[0], wh[1])
			}
		}
	}
}

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
