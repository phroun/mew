package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// On a cell-quantized (TUI) surface, TileWindows must place every window on the
// cell grid - TileLayout's proportional boundaries would otherwise land at
// fractional cell positions and break terminal rendering.
func TestTileWindowsQuantizesToCellGrid(t *testing.T) {
	m := NewWindowManager()
	// A non-smooth manager (the default) is the TUI case. Use an area whose
	// dimensions are not multiples of the cell size so the proportional split
	// yields off-grid boundaries.
	m.SetScreenBounds(core.UnitRect{Width: 803, Height: 617})

	for i := 0; i < 5; i++ {
		w := NewWindow("w")
		w.SetBounds(core.UnitRect{X: 3, Y: 5, Width: 200, Height: 120}) // resizable
		m.AddWindow(w)
	}

	m.TileWindows()

	met := core.DefaultCellMetrics()
	for i, w := range m.Windows() {
		b := w.Bounds()
		if b.X%met.CellWidth != 0 || b.Y%met.CellHeight != 0 {
			t.Errorf("window %d origin (%d,%d) not on the cell grid (%dx%d)",
				i, b.X, b.Y, met.CellWidth, met.CellHeight)
		}
		if b.Width%met.CellWidth != 0 || b.Height%met.CellHeight != 0 {
			t.Errorf("window %d size (%dx%d) not a whole number of cells",
				i, b.Width, b.Height)
		}
	}
}

// A smooth (pixel/SDL) manager keeps the exact proportional layout - no
// snapping - so at least one window may sit off the coarse cell grid.
func TestTileWindowsSmoothKeepsProportional(t *testing.T) {
	m := NewWindowManager()
	m.SetSmoothPositioning(true)
	m.SetScreenBounds(core.UnitRect{Width: 803, Height: 617})

	for i := 0; i < 3; i++ {
		w := NewWindow("w")
		w.SetBounds(core.UnitRect{Width: 200, Height: 120})
		m.AddWindow(w)
	}

	m.TileWindows()

	// The three windows split 803 wide as 401/402-ish and rows of 617/2 - none
	// of which are multiples of 8/16, so the layout must NOT be grid-snapped.
	offGrid := false
	met := core.DefaultCellMetrics()
	for _, w := range m.Windows() {
		b := w.Bounds()
		if b.X%met.CellWidth != 0 || b.Y%met.CellHeight != 0 ||
			b.Width%met.CellWidth != 0 || b.Height%met.CellHeight != 0 {
			offGrid = true
		}
	}
	if !offGrid {
		t.Error("smooth positioning should keep the exact proportional layout, not snap to cells")
	}
}
