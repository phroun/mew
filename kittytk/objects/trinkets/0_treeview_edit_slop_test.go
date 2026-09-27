package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The wobble a click is allowed is the same DISTANCE in every tree.
//
// It is compared against pointer deltas, which arrive in the tree's own units,
// so a raw unit count is a different distance in every denomination: 4 units is
// half a cell across at 8x16 and an eighth at 32x64, and the same hand movement
// read as a click in one tree and a drag in the next.
func TestClickEditSlopIsTheSameDistanceAtEveryDenomination(t *testing.T) {
	for _, c := range []struct {
		m              core.CellMetrics
		wantDX, wantDY core.Unit
	}{
		{core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}, 4, 4},
		{core.CellMetrics{UnitsPerCellWidth: 16, UnitsPerCellHeight: 32}, 8, 8},
		{core.CellMetrics{UnitsPerCellWidth: 4, UnitsPerCellHeight: 8}, 2, 2},
		{core.CellMetrics{UnitsPerCellWidth: 32, UnitsPerCellHeight: 64}, 16, 16},
	} {
		mm := c.m
		tv := NewTreeView()
		tv.SetCellMetrics(&mm)

		dx, dy := tv.clickEditSlop()
		if dx != c.wantDX || dy != c.wantDY {
			t.Errorf("at %dx%d the slop is %dx%d units, want %dx%d",
				c.m.UnitsPerCellWidth, c.m.UnitsPerCellHeight, dx, dy, c.wantDX, c.wantDY)
		}
		// The distance itself: the same fraction of a cell on both axes as at
		// the default, which is what "the same wobble" means when the units
		// underneath are being subdivided differently.
		if got, want := dx*8, c.m.UnitsPerCellWidth*treeClickEditSlop; got != want {
			t.Errorf("at %dx%d the slop is %d/%d of a cell across, want 4/8",
				c.m.UnitsPerCellWidth, c.m.UnitsPerCellHeight, dx, c.m.UnitsPerCellWidth)
		}
		if got, want := dy*16, c.m.UnitsPerCellHeight*treeClickEditSlop; got != want {
			t.Errorf("at %dx%d the slop is %d/%d of a cell down, want 4/16",
				c.m.UnitsPerCellWidth, c.m.UnitsPerCellHeight, dy, c.m.UnitsPerCellHeight)
		}
	}
}

// Where a unit IS a cell there is no sub-cell wobble to allow: the pointer
// cannot report one, so a release in the same cell is a click and a release in
// the next one is a drag.
func TestClickEditSlopOnASquareDenomination(t *testing.T) {
	m := core.SquareCellMetrics()
	tv := NewTreeView()
	tv.SetCellMetrics(&m)

	dx, dy := tv.clickEditSlop()
	if dx != 0 || dy != 0 {
		t.Errorf("slop %dx%d on square metrics, want no sub-cell tolerance", dx, dy)
	}
}
