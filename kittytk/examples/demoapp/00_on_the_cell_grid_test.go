package main

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Everything the demo lays out stands on the cell grid: every trinket in every
// tab starts on a cell boundary and is a whole number of cells across and
// down.
//
// A cell surface draws by dividing units by the cell size and hit-tests in
// units, so a trinket standing between cells draws in one and answers the
// mouse in another -- a button whose clicks are a column and a row out of the
// box it is painted in. The demo is the widest spread of layouts there is:
// boxes inside grids inside flex runs inside tabs, at spacings that are not
// whole cells and in rooms that do not divide evenly.
func TestEveryTrinketLandsOnTheCellGrid(t *testing.T) {
	for _, caption := range allTabCaptions(t) {
		tabs := openTab(t, caption)

		var walk func(w core.Trinket, origin core.UnitPoint)
		walk = func(w core.Trinket, origin core.UnitPoint) {
			b := w.Bounds()
			// Where the trinket is on the surface, not in its parent: a child
			// on a whole cell inside a parent that is half a cell out is half
			// a cell out itself.
			at := core.UnitPoint{X: origin.X + b.X, Y: origin.Y + b.Y}
			m := core.FindEffectiveCellMetrics(w)

			if at.X%m.UnitsPerCellWidth != 0 || at.Y%m.UnitsPerCellHeight != 0 {
				t.Errorf("%s: a %T starts at %d,%d, %d columns and %d rows into a cell",
					caption, w, at.X, at.Y,
					at.X%m.UnitsPerCellWidth, at.Y%m.UnitsPerCellHeight)
			}
			if b.Width%m.UnitsPerCellWidth != 0 || b.Height%m.UnitsPerCellHeight != 0 {
				t.Errorf("%s: a %T is %dx%d, not a whole number of %dx%d cells",
					caption, w, b.Width, b.Height,
					m.UnitsPerCellWidth, m.UnitsPerCellHeight)
			}

			if c, ok := w.(core.Container); ok {
				for _, k := range c.Children() {
					walk(k, at)
				}
			}
		}
		walk(tabs, core.UnitPoint{})
	}
}

// allTabCaptions is every tab the demo's main window carries, so a tab added
// to the script is checked without anything here being edited.
func allTabCaptions(t *testing.T) []string {
	t.Helper()
	tabs := openTab(t, "Basic Trinkets")
	var out []string
	for i := 0; i < tabs.Count(); i++ {
		out = append(out, tabs.TabText(i))
	}
	return out
}
