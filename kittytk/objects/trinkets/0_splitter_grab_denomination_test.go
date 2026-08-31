package trinkets

import (
	"image/color"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// paintSideBySideSplitter draws the divider of a side-by-side splitter -- the
// vertical band with the ':' grab dots at its center.
func paintSideBySideSplitter(p *core.Painter, m core.CellMetrics) {
	sp := NewSplitter(core.Horizontal)
	sp.SetCellMetrics(&m)
	sp.SetBounds(core.UnitRect{
		Width:  200 * m.CellWidth / 8,
		Height: 100 * m.CellHeight / 16,
	})
	sp.Paint(p)
}

// The band's hairline and the grab dots are screen-space sizes converted into
// local units, so they hold their physical size under re-denomination. The
// dots were then multiplied by CellWidth/8 and CellHeight/16 as well, meant to
// track font_size -- but those are the denomination, not the zoom, so the
// handle grew with the denomination instead: two device pixels across and four
// down at 8x16, four and eight at 16x32, eight and sixteen at 32x64.
//
// A screen unit is already a fixed number of device pixels at a given zoom, so
// the conversion alone is what tracks font_size.
func TestSplitterGrabHandlePaintsTheSameAtEveryDenomination(t *testing.T) {
	// Denominations finer than the outer: at a coarser one a local unit spans
	// more than a device pixel, so the one-unit hairline running the length of
	// the band is genuinely thicker and there is nothing to compare it to.
	samePixels(t, "side-by-side splitter", capDenominations[:3], paintSideBySideSplitter)
}

// And what the size actually is, rather than only that it agrees with itself:
// two dots, each one screen unit tall and two across, either side of the gap.
func TestSplitterGrabHandleIsTwoDotsOfOneScreenUnit(t *testing.T) {
	const W, H = 200, 120

	for _, m := range capDenominations[:3] {
		b, err := raster.New(W, H)
		if err != nil {
			t.Fatal(err)
		}
		b.SetCellMetrics(capOuter)
		b.Clear(style.DefaultStyle())
		paintSideBySideSplitter(core.NewPainter(b).WithDenomination(capOuter, m), m)

		// The band's own fill is the reference: the hairline and the dots are
		// the only things drawn over it, and the hairline is one screen unit
		// across where the dots are two, so the widest rows ARE the dots.
		img := b.Image()
		band := img.RGBAAt(97, 3)
		widest, rows := 0, 0
		for y := 0; y < 100; y++ {
			n := 0
			for x := 96; x < 104; x++ {
				if img.RGBAAt(x, y) != band {
					n++
				}
			}
			switch {
			case n > widest:
				widest, rows = n, 1
			case n == widest && n > 0:
				rows++
			}
		}
		if widest != 2 || rows != 4 {
			t.Errorf("%dx%d: grab dots are %dpx across over %d rows, want 2 over 4",
				m.CellWidth, m.CellHeight, widest, rows)
		}
	}
}

// bandInk returns the first and last device column of the ink in row y of the
// splitter's band, measured against the band's own fill.
func bandInk(img interface{ RGBAAt(x, y int) color.RGBA }, y int) (lo, hi int) {
	band := img.RGBAAt(97, 3)
	lo, hi = -1, -1
	for x := 96; x < 104; x++ {
		if img.RGBAAt(x, y) != band {
			if lo < 0 {
				lo = x
			}
			hi = x
		}
	}
	return lo, hi
}

// The dots sit ON the hairline, so they have to share its center. The line was
// centered by (Width-hairW)/2 and the dots placed at Width/2-dotW/2 -- two
// halvings truncated on a grid whose coarseness is the denomination's, which
// at 4x8 left the dots a whole local unit (two device pixels) to the right of
// the line they straddle.
func TestSplitterGrabDotsShareTheLinesCenter(t *testing.T) {
	const W, H = 200, 120

	for _, m := range []core.CellMetrics{
		{CellWidth: 8, CellHeight: 16},
		{CellWidth: 16, CellHeight: 32},
		{CellWidth: 32, CellHeight: 64},
		{CellWidth: 4, CellHeight: 8},
		{CellWidth: 16, CellHeight: 16},
	} {
		b, err := raster.New(W, H)
		if err != nil {
			t.Fatal(err)
		}
		b.SetCellMetrics(capOuter)
		b.Clear(style.DefaultStyle())
		paintSideBySideSplitter(core.NewPainter(b).WithDenomination(capOuter, m), m)

		img := b.Image()
		lineLo, lineHi := bandInk(img, 10) // well above the handle
		dotLo, dotHi := bandInk(img, 47)   // the upper dot
		if lineLo < 0 || dotLo < 0 {
			t.Fatalf("%dx%d: nothing painted (line %d..%d, dot %d..%d)",
				m.CellWidth, m.CellHeight, lineLo, lineHi, dotLo, dotHi)
		}
		// Centers doubled, so half a pixel reads as 1 rather than rounding
		// away. Half a pixel is the floor: a two-pixel dot cannot sit exactly
		// centered on a one-pixel line, it has to overhang one side. Anything
		// past that is the two halvings disagreeing.
		if off := (lineLo + lineHi) - (dotLo + dotHi); off < -1 || off > 1 {
			t.Errorf("%dx%d: line spans %d..%d but the dots span %d..%d -- %.1f pixels off center",
				m.CellWidth, m.CellHeight, lineLo, lineHi, dotLo, dotHi, float64(off)/2)
		}
	}
}
