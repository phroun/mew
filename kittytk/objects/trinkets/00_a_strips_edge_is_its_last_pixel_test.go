package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// edgeLineRows is where a strip drew its edge line, in device pixels, and how
// tall the strip's row is -- so a test can ask whether the line reaches the
// row's own edge.
type edgeLineTape struct {
	core.RenderBackend
	rows []struct{ y, h int }
}

func (r *edgeLineTape) GraphicalMode() bool { return true }
func (r *edgeLineTape) FillRectPx(x, y, w, h int, s style.CellStyle) {
	// The edge line is the wide, shallow one: it runs most of the strip.
	if w > 40 && h <= 4 {
		r.rows = append(r.rows, struct{ y, h int }{y, h})
	}
	if f, ok := r.RenderBackend.(interface {
		FillRectPx(int, int, int, int, style.CellStyle)
	}); ok {
		f.FillRectPx(x, y, w, h, s)
	}
}

// A strip's edge line lies ALONG the row's boundary, so it reaches that
// boundary exactly: on a top strip its last pixel is the row's last pixel, and
// nothing of the bar is left showing under it.
//
// A row boundary is a cell edge and converts to a device pixel exactly. A unit
// position inside the row does not, so a line placed by its top -- at the row
// height less its own thickness -- floated clear of the bottom wherever a unit
// was worth more pixels than the line was thick.
func TestAStripsEdgeLineReachesTheRowsEdge(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	// A unit is worth a whole pixel only at some pairings of denomination and
	// font size; at the others it is worth a fraction or several, and that is
	// where a line placed by its top parts company with the row's edge.
	for _, c := range []struct {
		m    core.CellMetrics
		size int
	}{
		{core.DefaultCellMetrics(), 12},
		{core.CellMetrics{UnitsPerCellWidth: 4, UnitsPerCellHeight: 8}, 12},
		{core.CellMetrics{UnitsPerCellWidth: 4, UnitsPerCellHeight: 8}, 24},
		{core.CellMetrics{UnitsPerCellWidth: 4, UnitsPerCellHeight: 8}, 16},
		{core.CellMetrics{UnitsPerCellWidth: 16, UnitsPerCellHeight: 32}, 18},
		{core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}, 18},
	} {
		m := c.m
		for _, pos := range []TabPosition{TabsTop, TabsBottom} {
			px, err := raster.New(900, 300)
			if err != nil {
				t.Fatal(err)
			}
			px.SetFontSize(c.size)
			core.SetTextMeasurer(px)
			rec := &edgeLineTape{RenderBackend: px}
			tt := NewTabTrinket()
			tt.SetTabPosition(pos)
			mm := m
			tt.SetCellMetrics(&mm)
			for _, name := range []string{"Alpha", "Beta", "Gamma"} {
				tt.AddTab(name, NewPanel())
			}
			tt.SetCurrentIndex(1)
			tt.SetBounds(core.UnitRect{
				Width:  40 * m.UnitsPerCellWidth,
				Height: 10 * m.UnitsPerCellHeight,
			})
			// The denomination reaches a trinket as a re-denominated
			// painter, the way a container hands one down.
			p := core.NewPainter(rec).WithDenomination(core.DefaultCellMetrics(), m)
			tt.Paint(p)

			if len(rec.rows) == 0 {
				t.Fatalf("%dx%d size=%d %v: the strip drew no edge line", m.UnitsPerCellWidth, m.UnitsPerCellHeight, c.size, pos)
			}
			rowPx := p.UnitSpanPxY(0, m.UnitsPerCellHeight)
			// The bar's own edge is the one against the content: the bottom of
			// a top strip, the top of a bottom one.
			var want int
			for _, r := range rec.rows {
				if pos == TabsTop && r.y+r.h > want {
					want = r.y + r.h
				}
			}
			if pos == TabsTop {
				if want != rowPx {
					t.Errorf("%dx%d size=%d top: the edge line ends at pixel %d of a %d-pixel row, leaving %d of bar below it",
						m.UnitsPerCellWidth, m.UnitsPerCellHeight, c.size, want, rowPx, rowPx-want)
				}
				continue
			}
			// A bottom strip's bar edge is its first pixel.
			top := rec.rows[0].y
			for _, r := range rec.rows {
				if r.y < top {
					top = r.y
				}
			}
			strip := p.UnitSpanPxY(0, tt.Bounds().Height-m.UnitsPerCellHeight)
			if top != strip {
				t.Errorf("%dx%d size=%d bottom: the edge line starts at pixel %d, want the strip's first at %d",
					m.UnitsPerCellWidth, m.UnitsPerCellHeight, c.size, top, strip)
			}
		}
	}
}
