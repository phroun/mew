//go:build sdl

package trinkets

import (
	"math"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/style"
)

func pxToUnitLocal(px, denom, cellPx int) int {
	cells := px / cellPx
	rem := px - cells*cellPx
	if rem < 0 {
		cells--
		rem += cellPx
	}
	return cells*denom + (rem*denom+cellPx/2)/cellPx
}

func TestGfxMouseNoDrift(t *testing.T) {
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
		t.Skip("no term")
	}
	term.SetFont(&core.Font{Name: "ui-text", Size: 10})
	win := window.NewWindow("term")
	win.SetContent(term)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{Width: sz.Width, Height: sz.Height})
	win.SetActive(true)
	win.Layout()

	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b)) // caches hitK + sizes cols/rows
	cols, _ := term.Terminal().GetSize()
	baseCW, _ := term.cellDims()
	ppu := b.PxPerUnit()
	cellWPx := b.UnitToPxX(8) - b.UnitToPxX(0) // 8 units = one denom cell
	denomW := 8

	worst := 0
	for _, c := range []int{0, 1, 5, cols / 4, cols / 2, cols - 3, cols - 1} {
		if c < 0 || c >= cols {
			continue
		}
		// center pixel of column c as the render places it
		centerPx := int(math.Round((float64(c) + 0.5) * float64(baseCW) * ppu))
		ux := pxToUnitLocal(centerPx, denomW, cellWPx) // outer snapped inverse
		got, _ := term.screenToCellGfx(core.Unit(ux), 0)
		if e := got - c; e < 0 {
			e = -e
		} else if e > worst {
			worst = e
		}
		if got != c {
			t.Errorf("col %d: click at px %d -> unit %d -> screenToCellGfx col %d (drift %d)", c, centerPx, ux, got, got-c)
		}
	}
	t.Logf("cols=%d hitKX=%.4f cellWPx=%d ppu=%.4f worstDrift=%d", cols, term.gfx.hitKX, cellWPx, ppu, worst)
}
