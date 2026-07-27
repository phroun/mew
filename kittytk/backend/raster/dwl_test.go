package raster

import (
	"image"
	"testing"

	"github.com/phroun/kittytk/style"
)

// inkColumns reports the first and last framebuffer columns carrying any ink
// in the given row band, and how many columns did.
func inkColumns(img *image.RGBA, y0, y1 int) (first, last, count int) {
	first, last = -1, -1
	for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
		lit := false
		for y := y0; y < y1 && y < img.Rect.Max.Y; y++ {
			i := img.PixOffset(x, y)
			// The cell background is opaque black; ink is anything brighter.
			if img.Pix[i] > 40 || img.Pix[i+1] > 40 || img.Pix[i+2] > 40 {
				lit = true
				break
			}
		}
		if lit {
			if first < 0 {
				first = x
			}
			last = x
			count++
		}
	}
	return first, last, count
}

// A DEC double-width cell paints a glyph that is genuinely twice as wide as
// the same glyph at ordinary size. The fallback core.Painter used before this
// backend implemented DWLCellDrawer drew the ORDINARY glyph with a filler
// space beside it: right columns, right hit-testing, but a heading that read
// as loosely spaced normal text instead of a bigger line.
func TestDrawCellDWLDoublesGlyphWidth(t *testing.T) {
	s := style.DefaultStyle()

	normal, err := New(200, 40)
	if err != nil {
		t.Fatal(err)
	}
	normal.DrawCell(0, 0, 'M', s)
	nFirst, nLast, nCount := inkColumns(normal.Image(), 0, int(normal.metrics.CellHeight))
	if nCount == 0 {
		t.Fatal("the ordinary cell drew nothing to compare against")
	}

	wide, err := New(200, 40)
	if err != nil {
		t.Fatal(err)
	}
	if got := wide.DrawCellDWL(0, 0, 'M', "", s, dwlModeDouble, 0); got != 2 {
		t.Fatalf("DrawCellDWL consumed %d columns, want 2", got)
	}
	wFirst, wLast, wCount := inkColumns(wide.Image(), 0, int(wide.metrics.CellHeight))
	if wCount == 0 {
		t.Fatal("the doubled cell drew nothing")
	}

	nSpan, wSpan := nLast-nFirst+1, wLast-wFirst+1
	// Twice as wide, with a little slack for rasterization and the rounding
	// of a centred pad.
	if wSpan < nSpan*3/2 {
		t.Errorf("doubled glyph spans %d columns vs %d normal — it was not widened "+
			"(first/last: %d..%d vs %d..%d)", wSpan, nSpan, wFirst, wLast, nFirst, nLast)
	}
	if wSpan > nSpan*3 {
		t.Errorf("doubled glyph spans %d columns vs %d normal — far more than 2x", wSpan, nSpan)
	}
}

// The doubled glyph is CENTRED in its two-cell box, exactly as an ordinary
// glyph is centred in one — the doubling happens first, then the same pad.
func TestDrawCellDWLCentresInTwoCells(t *testing.T) {
	b, err := New(200, 40)
	if err != nil {
		t.Fatal(err)
	}
	b.DrawCellDWL(0, 0, 'M', "", style.DefaultStyle(), dwlModeDouble, 0)

	cellW := b.pxX(b.metrics.CellWidth) - b.pxX(0)
	boxW := 2 * cellW
	first, last, count := inkColumns(b.Image(), 0, int(b.metrics.CellHeight))
	if count == 0 {
		t.Fatal("nothing drawn")
	}
	leftGap, rightGap := first, boxW-1-last
	if diff := leftGap - rightGap; diff > 2 || diff < -2 {
		t.Errorf("glyph is not centred in the %d-px double cell: gaps %d left, %d right",
			boxW, leftGap, rightGap)
	}
}

// The two DECDHL halves come from one 2x rasterization, so together they show
// a single glyph at twice the size: each half must carry ink, and the top half
// must differ from the bottom (the same band twice would mean the mode was
// ignored).
func TestDrawCellDHLHalvesDiffer(t *testing.T) {
	s := style.DefaultStyle()
	rows := map[byte]*image.RGBA{}
	for _, mode := range []byte{dwlModeTop, dwlModeBottom} {
		b, err := New(200, 40)
		if err != nil {
			t.Fatal(err)
		}
		b.DrawCellDWL(0, 0, 'M', "", s, mode, 0)
		rows[mode] = b.Image()
		if _, _, count := inkColumns(b.Image(), 0, int(b.metrics.CellHeight)); count == 0 {
			t.Fatalf("DECDHL mode %q drew nothing", mode)
		}
	}
	same := true
	top, bottom := rows[dwlModeTop], rows[dwlModeBottom]
	for i := range top.Pix {
		if top.Pix[i] != bottom.Pix[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("the DECDHL top and bottom halves are identical: the mode was ignored")
	}
}

// A space consumes its two columns and paints only background — no glyph work,
// no stray ink from the scaler.
func TestDrawCellDWLSpaceIsBlank(t *testing.T) {
	b, err := New(200, 40)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.DrawCellDWL(0, 0, ' ', "", style.DefaultStyle(), dwlModeDouble, 0); got != 2 {
		t.Fatalf("a doubled space should still consume 2 columns, got %d", got)
	}
	if _, _, count := inkColumns(b.Image(), 0, int(b.metrics.CellHeight)); count != 0 {
		t.Errorf("a doubled space painted %d columns of ink", count)
	}
}

// purfecterm's flex-width attribute (Cell.CellWidth: 0.5, 1.0, 1.5, 2.0) is
// authoritative for the cell's box, exactly as cellVisualWidth is in the GTK
// and Qt renderers: the doubled box is cellVisualWidth * charWidth * 2.
func TestDrawCellDWLHonoursFlexWidth(t *testing.T) {
	s := style.DefaultStyle()
	for _, c := range []struct {
		flex float64
		want int // columns consumed
	}{
		{0, 2},   // unset: falls back to the rune's own width (narrow -> 2)
		{1.0, 2}, // one cell doubled
		{1.5, 3},
		{2.0, 4}, // a wide cell doubled: four columns
	} {
		b, err := New(400, 40)
		if err != nil {
			t.Fatal(err)
		}
		got := b.DrawCellDWL(0, 0, 'M', "", s, dwlModeDouble, c.flex)
		if got != c.want {
			t.Errorf("flex %.1f: consumed %d columns, want %d", c.flex, got, c.want)
		}
		// The background fill spans the whole doubled box.
		cellW := b.pxX(b.metrics.CellWidth) - b.pxX(0)
		flex := c.flex
		if flex <= 0 {
			flex = 1
		}
		wantPx := int(float64(cellW) * flex * 2)
		if px := filledWidth(b.Image()); px != wantPx {
			t.Errorf("flex %.1f: box painted %d px, want %d", c.flex, px, wantPx)
		}
	}
}

// filledWidth reports how many leading columns of row 0 carry a painted
// (non-zero-alpha) background.
func filledWidth(img *image.RGBA) int {
	n := 0
	for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
		if img.Pix[img.PixOffset(x, 2)+3] == 0 {
			break
		}
		n++
	}
	return n
}

// A glyph wider than its doubled box is squeezed to fit rather than spilling
// into the neighbouring cell — the rule GTK and Qt apply as
// textScaleX *= targetCellWidth / actualWidth.
func TestDrawCellDWLSqueezesOverWideGlyph(t *testing.T) {
	b, err := New(400, 40)
	if err != nil {
		t.Fatal(err)
	}
	// A wide CJK glyph in a box sized for a NARROW cell: at 2x it is wider
	// than the two columns it is given, so it must be squeezed into them.
	b.DrawCellDWL(0, 0, '日', "", style.DefaultStyle(), dwlModeDouble, 1.0)

	cellW := b.pxX(b.metrics.CellWidth) - b.pxX(0)
	boxW := 2 * cellW
	first, last, count := inkColumns(b.Image(), 0, int(b.metrics.CellHeight))
	if count == 0 {
		t.Fatal("nothing drawn")
	}
	if first < 0 || last >= boxW {
		t.Errorf("glyph spans %d..%d, past its %d-px box: it was not squeezed to fit",
			first, last, boxW)
	}
}
