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

// Every terminal face must sit on the SAME baseline as the primary (Latin)
// face. A face splits its line budget between ascent and descent however its
// designer chose — Noto Kufi Arabic reports 9 of 16 units above the baseline,
// Noto Naskh 11, Latin and Noto Serif Hebrew 13 — so left alone each script
// rides at its own height in the row: Kufi sat low with its descenders cut
// off, Hebrew serif slightly high. The mask is shifted (never scaled) to put
// every baseline where the primary face puts its own.
func TestTerminalFacesShareABaseline(t *testing.T) {
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	term.SetTerminalFontFamily("ui-term-western-sans")
	eng := term.gfxEngine()
	if eng == nil {
		t.Skip("font engine unavailable")
	}

	const pt = 12
	ref := eng.ShapeRun(&core.Font{Name: "Noto Sans Mono", Size: pt}, "M")
	if len(ref.Lines) == 0 {
		t.Skip("reference face unavailable")
	}
	want := int(ref.Lines[0].Baseline)

	for _, c := range []struct{ name, fam, s string }{
		{"latin (the reference itself)", "Noto Sans Mono", "M"},
		{"hebrew sans", "Noto Sans Hebrew", "ש"},
		{"hebrew serif", "Noto Serif Hebrew", "ש"},
		{"arabic sans (kufi)", "Noto Kufi Arabic", "ح"},
		{"arabic serif (naskh)", "Noto Naskh Arabic", "ح"},
	} {
		sp := eng.ShapeRun(&core.Font{Name: c.fam, Size: pt}, c.s)
		if len(sp.Lines) == 0 {
			t.Errorf("%s: nothing shaped", c.name)
			continue
		}
		own := int(sp.Lines[0].Baseline)
		aligned := own + term.baselineShiftPx(c.fam, []rune(c.s)[0], pt, 0, sp, 1.0)
		if aligned != want {
			t.Errorf("%s: baseline %d shifts to %d, want the reference's %d",
				c.name, own, aligned, want)
		}
	}
}
