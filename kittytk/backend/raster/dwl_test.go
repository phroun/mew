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
	if got := wide.DrawCellDWL(0, 0, 'M', "", s, dwlModeDouble); got != 2 {
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
	b.DrawCellDWL(0, 0, 'M', "", style.DefaultStyle(), dwlModeDouble)

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
		b.DrawCellDWL(0, 0, 'M', "", s, mode)
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
	if got := b.DrawCellDWL(0, 0, ' ', "", style.DefaultStyle(), dwlModeDouble); got != 2 {
		t.Fatalf("a doubled space should still consume 2 columns, got %d", got)
	}
	if _, _, count := inkColumns(b.Image(), 0, int(b.metrics.CellHeight)); count != 0 {
		t.Errorf("a doubled space painted %d columns of ink", count)
	}
}
