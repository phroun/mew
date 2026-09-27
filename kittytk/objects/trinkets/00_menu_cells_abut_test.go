package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Cells drawn side by side leave no gap between them: each one's background
// runs edge to edge, so the next begins exactly where it ended.
//
// DrawCell gives that for free. A scaled menu draws its cell glyphs as text
// runs instead, and a run inks only its own advance -- which at a scaled face
// is narrower than the cell it stands in. The menu bar's "[<]" and "[>]" came
// out with the bar showing through between their three cells, and read wider
// than they are for the same reason.
func TestMenuCellsAbutAtEveryScale(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil); core.SetMenuScale(1) })

	for _, scale := range []float64{1, 0.9, 0.5} {
		b, err := raster.NewScaled(200, 60, 1)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(b)
		core.SetMenuScale(scale)
		mm := MenuMetricsFor(core.DefaultCellMetrics(), core.DefaultFont(), true)

		// A ground nothing else paints, so anything left of it is a crack.
		ground := style.RGB(255, 0, 255)
		b.Clear(style.DefaultStyle().WithBg(ground))
		p := core.NewPainter(b)
		cell := style.DefaultStyle().WithBg(style.RGB(0, 0, 128)).WithFg(style.RGB(255, 255, 255))
		for i, ch := range []rune{'[', '<', ']'} {
			mm.DrawGlyph(p, core.Unit(i)*mm.CellW, 0, ch, cell)
		}

		gr, gg, gb := ground.RGBComponents()
		img := b.Image()
		wPx := p.UnitSpanPxX(0, mm.CellW*3)
		hPx := p.UnitSpanPxY(0, mm.RowH)
		for x := 0; x < wPx; x++ {
			for y := 0; y < hPx; y++ {
				if c := img.RGBAAt(x, y); c.R == gr && c.G == gg && c.B == gb {
					t.Errorf("scale %v: the ground shows through the run of cells at (%d,%d) of %dx%d",
						scale, x, y, wPx, hPx)
					break
				}
			}
		}
	}
}
