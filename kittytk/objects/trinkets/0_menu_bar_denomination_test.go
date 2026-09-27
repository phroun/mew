package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// glyphColumns returns the device-pixel columns carrying TITLE ink: pixels
// inside the bar's row that differ from the bar's own background fill.
//
// Comparing against the surface background instead would compare nothing --
// the bar fills every column it covers, so all three renders come out
// identical whatever the titles do. That version of this test passed with
// the bug still in place.
func glyphColumns(b *raster.Backend, w, barW, rowPx int) []int {
	img := b.Image()
	barBG := img.RGBAAt(barW-20, rowPx/2) // inside the bar, past the last title
	var cols []int
	for x := 0; x < w; x++ {
		for y := 0; y < rowPx; y++ {
			if img.RGBAAt(x, y) != barBG {
				cols = append(cols, x)
				break
			}
		}
	}
	return cols
}

// A window's denomination says how many units make one cell. A cell is a
// fixed physical size at a given zoom, so changing the denomination changes
// only the currency the layout is counted in -- nothing a menu bar draws
// should move or resize, because a menu specifies no margin, no padding, no
// width and no height of its own.
//
// It did move. The pad either side of a title is a cell and was already
// right, but the title itself was measured by Font.MeasureText, which
// answers at the DEFAULT denomination -- so at an interior denomination of
// 16 the titles were counted in half-size units while the pads around them
// stayed put, and the bar came out visibly stretched.
//
// Ink is compared rather than arithmetic: the arithmetic agreed with itself
// throughout, which is why the hit-testing looked correct while the picture
// was wrong.
func TestMenuBarPaintsTheSameAtEveryDenomination(t *testing.T) {
	const W, H = 420, 40
	outer := core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}

	var base []int
	for _, interior := range []core.CellMetrics{
		{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16},
		{UnitsPerCellWidth: 16, UnitsPerCellHeight: 32},
		{UnitsPerCellWidth: 4, UnitsPerCellHeight: 8},
	} {
		b, err := raster.New(W, H)
		if err != nil {
			t.Fatal(err)
		}
		b.SetCellMetrics(outer)
		b.Clear(style.DefaultStyle())

		m := NewMenuBar()
		m.SetHideCalendar(true)
		for _, title := range []string{"Demo", "Edit", "View"} {
			m.AddMenu(NewMenu(title))
		}
		m.SetCellMetrics(&interior)
		m.SetBounds(core.UnitRect{Width: 400 * interior.UnitsPerCellWidth / outer.UnitsPerCellWidth, Height: interior.UnitsPerCellHeight})

		p := core.NewPainter(b).WithDenomination(outer, interior)
		m.Paint(p)

		got := glyphColumns(b, W, 400, 16)
		if base == nil {
			base = got
			continue
		}
		if len(got) != len(base) {
			t.Errorf("interior %dx%d painted %d ink columns, want %d (the 8x16 picture)",
				interior.UnitsPerCellWidth, interior.UnitsPerCellHeight, len(got), len(base))
			continue
		}
		for i := range base {
			if got[i] != base[i] {
				t.Errorf("interior %dx%d: ink column %d at px %d, want %d",
					interior.UnitsPerCellWidth, interior.UnitsPerCellHeight, i, got[i], base[i])
				break
			}
		}
	}
}
