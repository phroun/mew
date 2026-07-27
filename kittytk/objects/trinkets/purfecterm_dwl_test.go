package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// paintTermInk feeds one line to a graphical PurfecTerm and reports the ink on
// the top row band: the span of lit columns and how many were lit.
func paintTermInk(t *testing.T, feed string) (first, last, lit int) {
	t.Helper()
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.New(640, 400)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	term.SetBounds(core.UnitRect{Width: 640, Height: 400})
	term.Feed([]byte(feed))
	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b))

	img := b.Image()
	first, last = -1, -1
	for x := 0; x < 640; x++ {
		on := false
		for y := 0; y < 40; y++ {
			c := img.RGBAAt(x, y)
			if int(c.R)+int(c.G)+int(c.B) > 120 {
				on = true
				break
			}
		}
		if on {
			if first < 0 {
				first = x
			}
			last = x
			lit++
		}
	}
	return first, last, lit
}

// A DECDWL row on a PIXEL surface must draw genuinely wider glyphs, not
// ordinary ones spaced out. The graphical path takes its point size from the
// cell box's HEIGHT alone (see cellTextImage), so doubling the box width on
// its own only re-centred the same glyph — the row occupied twice the width
// with the same amount of ink in it, which is what a doubled heading looked
// like. The span doubles either way, so INK COVERAGE is the signal: doubled
// glyphs light up most of the wider span, spaced-out ones light up no more
// columns than they did at normal size.
func TestGraphicalDECDWLWidensGlyphs(t *testing.T) {
	_, nLast, nLit := paintTermInk(t, "MMMM")
	dFirst, dLast, dLit := paintTermInk(t, "\x1b#6MMMM")

	if nLit == 0 || dLit == 0 {
		t.Fatal("nothing rendered")
	}
	if dLast < nLast*3/2 {
		t.Fatalf("the doubled row spans %d..%d vs %d normal: the cells were not doubled",
			dFirst, dLast, nLast)
	}
	// Twice the glyph means close to twice the ink. Spaced-out ordinary glyphs
	// carry the SAME ink as the normal row (they are the same rasterization).
	if dLit < nLit*3/2 {
		t.Errorf("doubled row lit %d columns vs %d normal across a %d-column span: "+
			"the glyphs were not widened, only spaced out", dLit, nLit, dLast-dFirst+1)
	}
}

// columnPeaks reports the peak alpha of each screen column across the top row
// band — the vertical-stroke profile of whatever was painted there.
func columnPeaks(t *testing.T, feed string, cols int) []int {
	t.Helper()
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.New(640, 400)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	term.SetBounds(core.UnitRect{Width: 640, Height: 400})
	term.Feed([]byte(feed))
	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b))

	img := b.Image()
	peaks := make([]int, cols)
	for x := 0; x < cols; x++ {
		for y := 0; y < 20; y++ {
			c := img.RGBAAt(x, y)
			if v := (int(c.R) + int(c.G) + int(c.B)) / 3; v > peaks[x] {
				peaks[x] = v
			}
		}
	}
	return peaks
}

// Doubling a row must not cost the glyph any pixel density. The default path
// rasterizes at the ORDINARY size — the very raster the glyph gets on a normal
// row, hinted for this cell height — and repeats each column, so the widened
// profile is the normal profile with every intensity appearing twice and no
// new value in between.
//
// Rasterizing at 2x and resampling back down (KITTYTK_DWL=supersample) instead
// re-derives every pixel: it introduces intensities the glyph never had and
// weakens thin strokes, which on Hebrew shows up as a stroke that muddles
// almost to a gap.
func TestGraphicalDECDWLPreservesPixelDensity(t *testing.T) {
	const glyph = "ש" // shin: three uprights, the thinnest test in the alphabet
	normal := columnPeaks(t, glyph, 40)

	// Every distinct intensity the ordinary raster produces.
	seen := map[int]bool{}
	for _, p := range normal {
		seen[p] = true
	}

	doubled := columnPeaks(t, "\x1b#6"+glyph, 40)
	for x, p := range doubled {
		if p > 30 && !seen[p] {
			t.Errorf("column %d peaks at %d, an intensity the ordinary raster never "+
				"produces: the doubling resampled the glyph instead of widening it\n"+
				" normal  %v\n doubled %v", x, p, normal, doubled)
			break
		}
	}

	// And it really is wider: at least as many strong columns as before.
	strong := func(v []int) (n int) {
		for _, p := range v {
			if p > 200 {
				n++
			}
		}
		return n
	}
	if strong(doubled) < strong(normal)*3/2 {
		t.Errorf("doubled row has %d strong columns vs %d normal: not widened",
			strong(doubled), strong(normal))
	}
}
