package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// ClientArea is what a dropdown from the window's own menu bar divides by
// its row height to decide how many items fit, and so whether to scroll at
// all. Its doc says it is "expressed in that menu bar's local coordinate
// space" -- and the bar works in the window's INTERIOR denomination, while
// the bounds and chrome rect it is built from are in the OUTER one.
//
// Handing the outer number over unconverted made the dropdown divide a
// height in one currency by a row height in another, so the row count came
// out wrong by the ratio between them: too few rows in a finely denominated
// window (scroll bumpers on a menu that fits) and too many in a coarse one
// (no scrolling on a menu that overflows).
//
// The claim is that the space stays the same size however it is counted.
func TestWindowClientAreaIsInTheMenuBarsCurrency(t *testing.T) {

	var wantRows core.Unit
	for _, interior := range []core.CellMetrics{
		{CellWidth: 8, CellHeight: 16},
		{CellWidth: 16, CellHeight: 32},
		{CellWidth: 4, CellHeight: 8},
		{CellWidth: 16, CellHeight: 16},
	} {
		w := NewWindow("t")
		w.SetDetached(true)
		mb := &ibeamContent{}
		mb.TrinketBase = *core.NewTrinketBase()
		w.SetWindowMenuBar(mb)
		w.SetBounds(core.UnitRect{Width: 400, Height: 320})
		w.SetCellMetrics(&interior)
		w.Layout()

		ca := w.ClientArea()

		// How many of the bar's OWN rows fit -- the number the dropdown
		// actually computes. It must not depend on the denomination.
		rows := ca.Height / interior.CellHeight
		if wantRows == 0 {
			wantRows = rows
			continue
		}
		if d := rows - wantRows; d > 1 || d < -1 {
			t.Errorf("at interior %dx%d the client area holds %d rows, want %d",
				interior.CellWidth, interior.CellHeight, rows, wantRows)
		}
	}
}
