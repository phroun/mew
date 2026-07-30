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

// The KITTYTK_DWL=widen strategy reproduces the ordinary raster exactly: it
// rasterizes at the normal size — the very raster the glyph gets on a normal
// row, hinted for this cell height — and repeats each column, so the widened
// profile is the normal profile with every intensity appearing twice and no
// new value in between. That is the guarantee this path exists to offer; the
// default 2x-and-resample path deliberately trades it for finer outline
// detail, so this test selects the widen path rather than the default.
func TestGraphicalDECDWLWidenPreservesPixelDensity(t *testing.T) {
	defer func(prev bool) { dwlWiden = prev }(dwlWiden)
	dwlWiden = true

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

// Every script must sit on the SAME baseline as the primary face.
//
// This drives the call the RENDERER makes, which is the whole point: a
// terminal cell resolves its font once — cellFamily returns the primary for
// every cell mew draws — and the engine picks the script face per glyph inside
// ShapeRun. An earlier version of this test passed concrete family names
// instead, which no paint ever does, and so happily passed while the shift was
// returning zero for every cell on screen. Ask it the way the cell asks it.
func TestTerminalFacesShareABaseline(t *testing.T) {
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	term.SetTerminalFontFamily("ui-term")
	eng := term.gfxEngine()
	if eng == nil {
		t.Skip("font engine unavailable")
	}

	const pt = 12
	primary := term.primaryTermFamily() // what every cell passes
	ref := eng.ShapeRun(&core.Font{Name: primary, Size: pt}, "M")
	if len(ref.Lines) == 0 {
		t.Skip("reference face unavailable")
	}
	want := int(ref.Lines[0].Baseline)

	for _, c := range []struct {
		name string
		ch   rune
	}{
		{"latin", 'M'},
		{"hebrew", 'ש'},
		{"arabic", 'ح'},
		{"cjk", '日'},
	} {
		sp := eng.ShapeRun(&core.Font{Name: primary, Size: pt}, string(c.ch))
		if len(sp.Lines) == 0 {
			t.Errorf("%s: nothing shaped", c.name)
			continue
		}
		own := int(sp.Lines[0].Baseline)
		aligned := own + term.baselineShiftPx(primary, c.ch, pt, 0, sp, 1.0)
		if aligned != want {
			t.Errorf("%s: baseline %d aligns to %d, want the primary's %d",
				c.name, own, aligned, want)
		}
	}
}

// A configured per-face correction reaches the face through the alias chain
// and rides on top of the automatic alignment, again via the renderer's own
// call shape. Guards the lookup going to the face that actually paints rather
// than the primary family the cell nominally requested.
func TestConfiguredCorrectionRidesOnAlignment(t *testing.T) {
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	eng := term.gfxEngine()
	if eng == nil {
		t.Skip("font engine unavailable")
	}
	eng.UseFont("ui-term-arabic", "ui-term-arabic-sans") // Kufi as the arabic face
	term.SetTerminalFontFamily("ui-term")
	primary := term.primaryTermFamily()

	const pt = 12
	sp := eng.ShapeRun(&core.Font{Name: primary, Size: pt}, "ح")
	if len(sp.Lines) == 0 {
		t.Skip("arabic face unavailable")
	}
	before := term.baselineShiftPx(primary, 'ح', pt, 0, sp, 1.0)

	eng.SetBaselineAdjust("Noto Kufi Arabic", -12)
	defer eng.SetBaselineAdjust("Noto Kufi Arabic", 0)

	after := term.baselineShiftPx(primary, 'ح', pt, 0, sp, 1.0)
	if after != before-12 {
		t.Errorf("correction did not reach the face: shift %+d -> %+d, want %+d",
			before, after, before-12)
	}
}
