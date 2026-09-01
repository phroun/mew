package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Across, a graphical target's terminal grid follows the real monospace
// font's measured advance rather than the denomination's cell width: the
// default (no explicit terminal font) would otherwise take an 8-unit pitch
// while the glyphs render from the 7-wide "Monday" mono face, and the grid
// and the font would disagree.
//
// Down, a terminal row is a grid row like any other line of text, so it is
// UnitsPerCellHeight units. Nothing about the font enters into it.
func TestTerminalGridFollowsFont(t *testing.T) {
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

	// Default (no explicit font): the grid equals the measured advance of
	// the default mono face, not the 8-unit denomination.
	cw, ch := term.cellDims()
	wantCW := term.TerminalFont().MeasureText("M")
	if cw != wantCW {
		t.Errorf("default grid width = %d, want the measured font's %d", cw, wantCW)
	}
	if cw == 8 && wantCW != 8 {
		t.Errorf("default grid still pinned to the 8-unit denomination")
	}
	// The terminal face is the surface's own size here, so a row is a whole
	// grid row -- 16 units at the default denomination this test runs at.
	if ch != 16 {
		t.Errorf("default grid height = %d, want one grid row (16)", ch)
	}
}

// The point-size setting re-derives the grid: a larger size yields a
// taller, wider cell (fewer columns/rows fit).
func TestTerminalFontSizeResizesGrid(t *testing.T) {
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
	term.SetBounds(core.UnitRect{Width: 320, Height: 160})

	smallCols, smallRows := term.Terminal().GetSize()
	smallCW, smallCH := term.cellDims()

	term.SetTerminalFontSize(24)
	if term.TerminalFont().Size != 24 {
		t.Fatalf("SetTerminalFontSize did not stick: %d", term.TerminalFont().Size)
	}
	bigCW, bigCH := term.cellDims()
	bigCols, bigRows := term.Terminal().GetSize()

	if bigCW <= smallCW || bigCH <= smallCH {
		t.Errorf("24pt cell %dx%d should exceed default %dx%d", bigCW, bigCH, smallCW, smallCH)
	}
	if bigCols >= smallCols || bigRows >= smallRows {
		t.Errorf("24pt grid %dx%d should fit fewer cells than default %dx%d", bigCols, bigRows, smallCols, smallRows)
	}
}

// Cell backgrounds tile seamlessly at the font pitch: no trinket
// background shows between adjacent colored cells. This guards the
// "grid matches the font" invariant at the pixel level (SDL-like
// scale=2).
func TestTerminalCellsTileWithoutSeams(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	const scale = 2
	b, err := raster.NewScaled(560, 200, scale)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)

	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	term.SetBounds(core.UnitRect{Width: 560 / scale, Height: 200 / scale})

	// Alternate bright red / blue cell backgrounds; a seam would show the
	// dark trinket background between cells.
	var sb []byte
	for i := 0; i < 24; i++ {
		if i%2 == 0 {
			sb = append(sb, []byte("\x1b[48;2;200;0;0m")...)
		} else {
			sb = append(sb, []byte("\x1b[48;2;0;0;200m")...)
		}
		sb = append(sb, byte('A'+i%26))
	}
	sb = append(sb, []byte("\x1b[0m")...)
	term.Feed(sb)

	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b))

	img := b.Image()
	cw, ch := term.cellDims()
	pitch := int(cw) * scale
	rowMidY := int(ch) * scale / 2

	seams := 0
	for x := 0; x < pitch*20; x++ {
		c := img.RGBAAt(x, rowMidY)
		// Cell backgrounds are bright red/blue and glyphs are light; a
		// seam is the dark trinket background showing through.
		if c.R < 80 && c.G < 80 && c.B < 80 {
			seams++
		}
	}
	if seams > 0 {
		t.Errorf("found %d dark seam pixels between colored cells (want 0)", seams)
	}
}

// A terminal row is one grid row, so its height in units is whatever the
// denomination says a row is. Reading it from the font instead answered 16
// everywhere: two rows tall inside a container at row_units=8, half a row at
// row_units=32.
func TestTerminalRowHeightFollowsTheDenomination(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.New(640, 400)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)

	for _, rowUnits := range []core.Unit{8, 16, 32} {
		pan := NewPanel()
		pan.SetCellMetrics(&core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: rowUnits})
		term := NewPurfecTerm()
		pan.AddChild(term)
		if term.Terminal() == nil {
			t.Skip("terminal unavailable")
		}
		term.SetBounds(core.UnitRect{Width: 320, Height: 160})

		if _, ch := term.cellDims(); ch != rowUnits {
			t.Errorf("at row_units=%d the terminal cell is %d units tall, want %d",
				rowUnits, ch, rowUnits)
		}
	}
}
