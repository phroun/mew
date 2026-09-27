package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Through a whole drag, the pointer stays over the cell of the window it
// grabbed.
//
// Under the kitty protocol the pointer reports where it is INSIDE a cell, so
// the grab offset carries a fraction of a cell. A cell surface can only put a
// window on a cell boundary, so that fraction is rounded away when the window
// lands -- and what is left is a gap between where the pointer is and where
// the window got to, which grows and shrinks as the pointer crosses cells.
func TestAPointerKeepsItsGripOnADraggedWindow(t *testing.T) {
	m, win := newPositioningManager(false)
	cells := core.DefaultCellMetrics()

	// Grabbed five units into a column -- a place only a sub-cell pointer can
	// report, and one no cell surface can put a window at.
	grab := core.UnitPoint{X: 205, Y: 88}
	m.HandleMousePress(core.MousePressEvent{X: grab.X, Y: grab.Y, Button: core.LeftButton})

	before := win.Bounds()
	wantCol := cells.UnitsToCellX(grab.X) - cells.UnitsToCellX(before.X)
	wantRow := cells.UnitsToCellY(grab.Y) - cells.UnitsToCellY(before.Y)

	// Across three columns, a unit at a time.
	for x := grab.X; x <= grab.X+24; x++ {
		m.HandleMouseMove(core.MouseMoveEvent{X: x, Y: grab.Y + 7})
		b := win.Bounds()
		col := cells.UnitsToCellX(x) - cells.UnitsToCellX(b.X)
		row := cells.UnitsToCellY(grab.Y+7) - cells.UnitsToCellY(b.Y)
		if col != wantCol || row != wantRow {
			t.Fatalf("at x=%d the pointer is over cell %d,%d of the window; it grabbed %d,%d",
				x, col, row, wantCol, wantRow)
		}
	}
	m.HandleMouseRelease(core.MouseReleaseEvent{X: grab.X + 24, Y: grab.Y + 7, Button: core.LeftButton})
}

// And on the edge it is dragging: the column the pointer is in is the column
// the edge is at, all the way along.
func TestAPointerKeepsItsGripOnAResizedEdge(t *testing.T) {
	m, win := newPositioningManager(false)
	cells := core.DefaultCellMetrics()

	// The right edge sits at x=400; grab it four units in.
	grab := core.UnitPoint{X: 396, Y: 160}
	m.HandleMousePress(core.MousePressEvent{X: grab.X, Y: grab.Y, Button: core.LeftButton})

	before := win.Bounds()
	want := cells.UnitsToCellX(grab.X) - cells.UnitsToCellX(before.X+before.Width)

	for x := grab.X; x <= grab.X+40; x++ {
		m.HandleMouseMove(core.MouseMoveEvent{X: x, Y: grab.Y})
		b := win.Bounds()
		got := cells.UnitsToCellX(x) - cells.UnitsToCellX(b.X+b.Width)
		if got != want {
			t.Fatalf("at x=%d the pointer is %d columns from the edge it is dragging; it grabbed %d",
				x, got, want)
		}
	}
	m.HandleMouseRelease(core.MouseReleaseEvent{X: grab.X + 40, Y: grab.Y, Button: core.LeftButton})
}

// A smooth surface has no cells to keep step with: the window follows the
// pointer exactly, to the unit.
func TestASmoothSurfaceFollowsThePointerExactly(t *testing.T) {
	m, win := newPositioningManager(true)

	grab := core.UnitPoint{X: 205, Y: 88}
	m.HandleMousePress(core.MousePressEvent{X: grab.X, Y: grab.Y, Button: core.LeftButton})
	before := win.Bounds()
	offX, offY := grab.X-before.X, grab.Y-before.Y

	for x := grab.X; x <= grab.X+24; x++ {
		m.HandleMouseMove(core.MouseMoveEvent{X: x, Y: grab.Y + 7})
		b := win.Bounds()
		if b.X != x-offX || b.Y != grab.Y+7-offY {
			t.Fatalf("at x=%d the window is at %d,%d; want %d,%d",
				x, b.X, b.Y, x-offX, grab.Y+7-offY)
		}
	}
	m.HandleMouseRelease(core.MouseReleaseEvent{X: grab.X + 24, Y: grab.Y + 7, Button: core.LeftButton})
}
