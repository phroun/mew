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

// The child's pixel window must be the pane's own pixels — exactly, once the
// density it is quoted in is divided back out.
//
// Not columns times the advertised cell. Those two look like the same quantity
// and are rounded against different things: the grid is fitted and PAINTED on
// the exact fractional cell (a 11.67px row pitch, walked per row, so fifty rows
// really do fit in 591px) while the cell the child is told must be whole, and
// 50 x 12 is 600. A child sizes its whole rendering from what it is told, so
// the 9px it cannot see are simply cut off — and the density scales the gap up
// with everything else. The other rounding goes the other way and leaves a
// strip of the pane the child never draws into.
//
// The measured extent has neither gap by construction, which is what this
// pins: reported / density == the pane, to the pixel, at every combination of
// magnification and screen density.
func TestChildWindowPixelsAreExactlyThePane(t *testing.T) {
	for _, c := range []struct {
		scale   int
		density float64
	}{{1, 1}, {1, 2}, {2, 2}, {2, 1}} {
		func() {
			t.Cleanup(func() { core.SetTextMeasurer(nil) })
			b, _ := raster.NewScaled(1200, 640, c.scale)
			b.SetFontSize(10)
			b.SetDisplayDensity(c.density)
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

			paneW, paneH := term.ContentPixelSize()
			if paneW <= 0 || paneH <= 0 {
				t.Fatalf("scale=%d density=%v: pane measures %dx%d", c.scale, c.density, paneW, paneH)
			}
			wpx, hpx := term.ChildWindowPixels()
			f := term.gfx.oversample
			if f <= 0 {
				t.Fatalf("scale=%d density=%v: oversample %v", c.scale, c.density, f)
			}
			if gotW, gotH := float64(wpx)/f, float64(hpx)/f; gotW != float64(paneW) || gotH != float64(paneH) {
				t.Errorf("scale=%d density=%v: reported %dx%d / %v = %.1fx%.1f, want the pane's %dx%d",
					c.scale, c.density, wpx, hpx, f, gotW, gotH, paneW, paneH)
			}
			// And it is quoted in the same pixels as the cell, so a client
			// dividing one by the other counts the columns it really has.
			cw, _ := term.Terminal().Buffer().GetCellPixelSize()
			cols, _ := term.Terminal().GetSize()
			if cw <= 0 || wpx/cw < cols {
				t.Errorf("scale=%d density=%v: window %dpx / cell %dpx = %d columns, fewer than the %d it has",
					c.scale, c.density, wpx, cw, wpx/max(cw, 1), cols)
			}
		}()
	}
}
