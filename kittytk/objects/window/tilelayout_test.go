package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

func distinctYTops(cells []core.UnitRect) map[core.Unit]int {
	m := map[core.Unit]int{}
	for _, c := range cells {
		m[c.Y]++
	}
	return m
}

// Seven windows tile as rows of 2, 2, 3 (remainder to the bottom), and each
// row divides its own width: the two-window rows split in half, the
// three-window row into thirds.
func TestTileLayoutBalancesBottomHeavy(t *testing.T) {
	area := core.UnitRect{X: 0, Y: 0, Width: 1200, Height: 900}
	items := make([]TileItem, 7)
	for i := range items {
		items[i] = TileItem{Resizable: true}
	}
	cells := TileLayout(area, items)

	rows := distinctYTops(cells)
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d (%v)", len(rows), rows)
	}
	// Count windows per row by their Y.
	perRow := map[core.Unit]int{}
	for _, c := range cells {
		perRow[c.Y]++
	}
	if perRow[0] != 2 || perRow[300] != 2 || perRow[600] != 3 {
		t.Errorf("row counts = top:%d mid:%d bot:%d, want 2/2/3",
			perRow[0], perRow[300], perRow[600])
	}
	// Top rows split in half (600 wide); the bottom row into thirds (400).
	for _, c := range cells {
		if c.Y < 600 && c.Width != 600 {
			t.Errorf("two-window row cell width = %d, want 600", c.Width)
		}
		if c.Y == 600 && c.Width != 400 {
			t.Errorf("three-window row cell width = %d, want 400", c.Width)
		}
	}
}

// The First window always occupies the top-left cell.
func TestTileLayoutFirstIsUpperLeft(t *testing.T) {
	area := core.UnitRect{X: 0, Y: 0, Width: 1200, Height: 900}
	items := []TileItem{
		{Resizable: true},
		{Resizable: true},
		{Resizable: true, First: true}, // not first in the slice
	}
	cells := TileLayout(area, items)
	if cells[2].X != 0 || cells[2].Y != 0 {
		t.Errorf("First window at (%d,%d), want upper-left (0,0)", cells[2].X, cells[2].Y)
	}
}

// A non-resizable window too big for the narrow cells claims a roomy cell;
// a small fixed window is left a narrow cell so resizable windows keep room.
func TestTileLayoutFitsFixedWindows(t *testing.T) {
	area := core.UnitRect{X: 0, Y: 0, Width: 1200, Height: 900}
	items := []TileItem{
		{Resizable: true},
		{Resizable: false, Size: core.UnitSize{Width: 900, Height: 400}}, // big fixed
		{Resizable: false, Size: core.UnitSize{Width: 100, Height: 100}}, // small fixed
	}
	// 3 -> rows of 1 (full width, 1200) then 2 (600 each).
	cells := TileLayout(area, items)
	if cells[1].Width != 1200 {
		t.Errorf("big fixed window got width %d, want the 1200-wide cell", cells[1].Width)
	}
	if cells[2].Width != 600 {
		t.Errorf("small fixed window got width %d, want a 600-wide cell", cells[2].Width)
	}
}
