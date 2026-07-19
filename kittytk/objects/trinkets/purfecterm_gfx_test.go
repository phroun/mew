package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// The graphical terminal path: fed ANSI content renders as pixels
// with true colors, and the terminal-font cell grid drives sizing.
func TestPurfecTermGraphicalPaint(t *testing.T) {
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
	// Red-on-blue text via SGR truecolor, then reset.
	term.Feed([]byte("\x1b[38;2;255;0;0m\x1b[48;2;0;0;255mHELLO\x1b[0m plain"))

	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b))

	img := b.Image()
	// The first five cells carry the blue background...
	blue := 0
	red := 0
	for y := 0; y < 16; y++ {
		for x := 0; x < 5*8; x++ {
			c := img.RGBAAt(x, y)
			if c.B > 200 && c.R < 60 && c.G < 60 {
				blue++
			}
			if c.R > 200 && c.B < 60 {
				red++
			}
		}
	}
	if blue < 200 {
		t.Errorf("blue SGR background missing (%d px)", blue)
	}
	// ...and the glyphs composite in red over it.
	if red < 20 {
		t.Errorf("red SGR glyphs missing (%d px)", red)
	}
}

// A custom terminal font resizes the terminal's cell grid on
// graphical targets (measurement answered by the render target).
func TestPurfecTermCustomFontResizesGrid(t *testing.T) {
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
	defCols, defRows := term.Terminal().GetSize()

	// Double-size font: roughly half the columns and rows fit.
	term.SetTerminalFont(&core.Font{Name: "Monday", Size: 24})
	bigCols, bigRows := term.Terminal().GetSize()
	if bigCols >= defCols || bigRows >= defRows {
		t.Errorf("24pt font should shrink the grid: %dx%d -> %dx%d", defCols, defRows, bigCols, bigRows)
	}
	if bigCols < defCols/3 || bigRows < defRows/3 {
		t.Errorf("grid shrank too far: %dx%d -> %dx%d", defCols, defRows, bigCols, bigRows)
	}
}
