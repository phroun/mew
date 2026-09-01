package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A click has to reach the item it looks like it is over, at any
// denomination -- and that is a claim about two paths agreeing, not about
// either one alone.
//
// Both halves have now been wrong in turn. The bar's geometry was in the
// default denomination while it painted in the interior one, so the picture
// was stretched and the clicks matched the picture's arithmetic rather than
// the picture. Fixing the paint moved the error to the other side: the
// chrome mouse path subtracted the bar's origin in OUTER units and stopped
// there, handing the bar a position in the window's currency while its
// geometry was in the interior's.
//
// paintChrome offsets in outer units and then changes denomination. The
// mouse path has to do both as well, which is what chromeLocal is.
func TestChromeMousePositionCrossesIntoTheInteriorDenomination(t *testing.T) {
	outer := core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}

	for _, interior := range []core.CellMetrics{
		{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16},
		{UnitsPerCellWidth: 16, UnitsPerCellHeight: 32},
		{UnitsPerCellWidth: 4, UnitsPerCellHeight: 8},
		{UnitsPerCellWidth: 32, UnitsPerCellHeight: 32},
	} {
		r := core.UnitRect{X: 2, Y: 3, Width: 400, Height: 16}

		// A point 80 outer units into the bar is 80 outer units into the
		// bar whatever the bar counts in; only the number changes.
		gotX, gotY := chromeLocal(82, 3, r, outer, interior)
		wantX := core.ExchangeX(80, outer, interior)
		wantY := core.Unit(0)
		if gotX != wantX || gotY != wantY {
			t.Errorf("interior %dx%d: chromeLocal gave (%d,%d), want (%d,%d)",
				interior.UnitsPerCellWidth, interior.UnitsPerCellHeight, gotX, gotY, wantX, wantY)
		}

		// The bar's own origin maps to its own origin, always.
		if x, y := chromeLocal(r.X, r.Y, r, outer, interior); x != 0 || y != 0 {
			t.Errorf("interior %dx%d: the bar's origin mapped to (%d,%d), want (0,0)",
				interior.UnitsPerCellWidth, interior.UnitsPerCellHeight, x, y)
		}

		// And a position stated in the interior currency lands inside the
		// width the bar was given in that same currency -- the two are set
		// from one conversion, so they cannot disagree.
		bar := inInterior(r, outer, interior)
		if x, _ := chromeLocal(r.X+r.Width-1, r.Y, r, outer, interior); x >= bar.Width {
			t.Errorf("interior %dx%d: the bar's last outer column maps to %d, "+
				"outside its own width %d", interior.UnitsPerCellWidth, interior.UnitsPerCellHeight, x, bar.Width)
		}
	}
}
