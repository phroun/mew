//go:build sdl

package trinkets

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/style"
)

// Under SGR-Pixels (?1016) the hosted app receives a PIXEL coordinate and
// recovers the cell by dividing it by the cell size the terminal told it —
// the answer to CSI 16 t. Those two numbers are a contract: whatever space
// the report is encoded in has to be the space the reported cell size
// measures, or every click lands somewhere else entirely.
//
// This walks the app's side of that contract: enable ?1016, click a known
// column, and divide the report by the advertised cell size.
func TestPixelReportDividesByTheAdvertisedCellSize(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, _ := raster.NewScaled(1200, 640, 2)
	b.SetFontSize(10)
	core.SetTextMeasurer(b)
	d := NewDesktop()
	d.SetBackend(b)
	sz := b.Size()
	d.SetBounds(core.UnitRect{Width: sz.Width, Height: sz.Height})
	d.SetFont(&core.Font{Name: "ui-text", Size: 10})
	d.WindowManager().SetScreenBounds(core.UnitRect{Width: sz.Width, Height: sz.Height})

	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("no embedded terminal")
	}
	term.SetFont(&core.Font{Name: "ui-text", Size: 10})
	win := window.NewWindow("term")
	win.SetContent(term)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{Width: sz.Width, Height: sz.Height})
	win.SetActive(true)
	win.Layout()

	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b)) // sizes the grid and pushes the cell metrics

	var sink strings.Builder
	term.SetInputSink(func(p []byte) { sink.Write(p) })
	term.Feed([]byte("\x1b[?1000h\x1b[?1016h"))
	if _, enc := term.mouseTracking(); enc != 1016 {
		t.Skipf("terminal did not take ?1016 (encoding %d)", enc)
	}

	// What the app is told a cell measures — exactly what CSI 16 t answers.
	cellW, cellH := term.Terminal().Buffer().GetCellPixelSize()
	if cellW <= 0 || cellH <= 0 {
		t.Fatalf("no cell pixel size advertised (%dx%d)", cellW, cellH)
	}

	cols, _ := term.Terminal().GetSize()
	baseCW, _ := term.cellDims()
	ppu := b.PxPerUnit()
	cellWPx := b.UnitToPxX(8) - b.UnitToPxX(0) // 8 units = one denom cell

	for _, col := range []int{0, 1, 5, cols / 2, cols - 2} {
		if col < 0 || col >= cols {
			continue
		}
		// The centre of that column, located exactly as the paint places it,
		// then mapped back into the trinket's unit space (as an event is).
		centerPx := int(math.Round((float64(col) + 0.5) * float64(baseCW) * ppu))
		ux := core.Unit(pxToUnitLocal(centerPx, 8, cellWPx))
		sink.Reset()
		term.HandleMousePress(core.MousePressEvent{X: ux, Y: 0, Button: core.LeftButton})

		px, ok := sgrReportX(sink.String())
		if !ok {
			t.Fatalf("col %d: no SGR report emitted (%q)", col, sink.String())
		}
		// The app's arithmetic, verbatim: 1-based pixel / advertised cell.
		if got := (px - 1) / cellW; got != col {
			t.Errorf("col %d: report %d px / advertised cell %d px = col %d",
				col, px, cellW, got)
		}
	}
}

// sgrReportX pulls the X coordinate out of an SGR mouse report
// (ESC [ < btn ; x ; y M/m).
func sgrReportX(s string) (int, bool) {
	i := strings.Index(s, "\x1b[<")
	if i < 0 {
		return 0, false
	}
	rest := s[i+3:]
	end := strings.IndexAny(rest, "Mm")
	if end < 0 {
		return 0, false
	}
	parts := strings.Split(rest[:end], ";")
	if len(parts) != 3 {
		return 0, false
	}
	x, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, false
	}
	return x, true
}

// The child's pixel window is the pane's MEASURED extent, and it is NOT
// columns times the reported cell size. Two independent reasons:
//
//   - the grid is fitted on the UNSCALED cell (contentWpx / (baseCW*ppu))
//     while the size reported to the child is the SCALED one, so the product
//     carries the horizontal scale the fit never applied;
//   - the scrollbar lane is already out of the fitted extent, and not out of
//     the product.
//
// A program that draws pictures sizes its whole rendering from this number,
// so either error puts the page at the wrong scale and spills it past the
// pane. Assert the measured extent fits within the widget and that the naive
// product does not silently stand in for it.
func TestContentPixelSizeIsMeasuredNotDerived(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, _ := raster.NewScaled(1200, 640, 2)
	b.SetFontSize(10)
	core.SetTextMeasurer(b)
	d := NewDesktop()
	d.SetBackend(b)
	sz := b.Size()
	d.SetBounds(core.UnitRect{Width: sz.Width, Height: sz.Height})
	d.SetFont(&core.Font{Name: "ui-text", Size: 10})
	d.WindowManager().SetScreenBounds(core.UnitRect{Width: sz.Width, Height: sz.Height})

	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("no embedded terminal")
	}
	term.SetFont(&core.Font{Name: "ui-text", Size: 10})
	term.SetLockstepPitch(true) // how a hosted (mew) terminal is laid out
	win := window.NewWindow("term")
	win.SetContent(term)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{Width: sz.Width, Height: sz.Height})
	win.SetActive(true)
	win.Layout()

	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b))

	wpx, hpx := term.ContentPixelSize()
	if wpx <= 0 || hpx <= 0 {
		t.Fatalf("no content pixel size after a paint (%dx%d)", wpx, hpx)
	}
	// It must fit inside the surface it is painted on. A number larger than
	// the widget is exactly the overflow this replaced.
	if maxW := b.UnitToPxX(sz.Width) - b.UnitToPxX(0); wpx > maxW {
		t.Errorf("content width %dpx exceeds the surface's %dpx", wpx, maxW)
	}

	// And it is a real measurement, not the product: the product uses the
	// SCALED cell the child is told about, which the fit never applied.
	cols, rows := term.Terminal().GetSize()
	cellW, cellH := term.Terminal().Buffer().GetCellPixelSize()
	t.Logf("measured=%dx%d product=%dx%d cols=%d rows=%d cell=%dx%d",
		wpx, hpx, cols*cellW, rows*cellH, cols, rows, cellW, cellH)
	if cols*cellW > wpx && cols*cellW > maxSurfaceW(b, sz.Width) {
		t.Errorf("the product %dpx overflows the surface where the measurement %dpx does not",
			cols*cellW, wpx)
	}
}

func maxSurfaceW(b *raster.Backend, w core.Unit) int {
	return b.UnitToPxX(w) - b.UnitToPxX(0)
}
