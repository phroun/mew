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

// The child's pixel window must be its OWN grid times the cell it was told,
// and both of those have to fit inside the pane that paints it.
//
// The trap is that a hosted terminal runs on a different pitch from its host
// (SetLockstepPitch exists for exactly that), so the cols mew measures on the
// HOST grid are not the cols this terminal settled on. Multiplying host
// columns by this terminal's cell overstates the window by a difference that
// grows with width - a wider pane loses proportionally more off its right
// edge, while the pointer, which reports against this same self-consistent
// pair, stays accurate.
func TestChildWindowPixelsFitThePane(t *testing.T) {
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

	cols, rows := term.Terminal().GetSize()
	cw, ch := term.Terminal().Buffer().GetCellPixelSize()
	if cols <= 0 || rows <= 0 || cw <= 0 || ch <= 0 {
		t.Fatalf("grid %dx%d cell %dx%d", cols, rows, cw, ch)
	}
	wpx, hpx := float64(cols*cw), float64(rows*ch)

	// The window is stated in the child's OVERSAMPLED pixels: it is told a
	// cell is deviceScale times its real size so it renders at the display's
	// density, and the image it sends back is divided by the same factor on
	// the way to the screen. So the thing that must fit the pane is the
	// window after that division.
	if f := term.gfx.oversample; f > 1 {
		wpx /= f
		hpx /= f
	}

	paneW, paneH := term.ContentPixelSize()
	t.Logf("child %dx%d cells * %dx%d px / oversample %.2f = %.0fx%.0f; pane measures %dx%d",
		cols, rows, cw, ch, term.gfx.oversample, wpx, hpx, paneW, paneH)

	if paneW > 0 && int(wpx) > paneW {
		t.Errorf("child window %.0fpx wide overflows the pane's %dpx", wpx, paneW)
	}
	if paneH > 0 && int(hpx) > paneH {
		t.Errorf("child window %.0fpx tall overflows the pane's %dpx", hpx, paneH)
	}
	if surf := b.UnitToPxX(sz.Width) - b.UnitToPxX(0); int(wpx) > surf {
		t.Errorf("child window %.0fpx wide overflows the surface's %dpx", wpx, surf)
	}
}
