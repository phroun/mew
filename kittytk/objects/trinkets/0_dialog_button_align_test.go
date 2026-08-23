package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// On a cell surface, the centered dialog button row must land on whole columns.
// Centering (bounds.Width - rowWidth)/2 can otherwise produce a half-column
// origin when the slack is an odd number of columns; the cell backend draws on
// whole columns, so the buttons' stored bounds (used for hit-testing) would
// drift half a column from where they paint. Pick a width that makes the raw
// origin a half column and assert the stored bounds snap to the grid.
func TestDialogButtonRowSnapsToCellGrid(t *testing.T) {
	b, err := raster.New(600, 300)
	if err != nil {
		t.Fatal(err)
	}
	p := core.NewPainter(b)

	m := NewMessageBox("T", "hi", ButtonOK|ButtonCancel)
	met := m.content.EffectiveCellMetrics()

	// Not parented to a smooth surface -> cell-surface layout. A 20-column
	// content width leaves an odd (3-column) slack around the 17-column button
	// row, so the raw origin is 1.5 columns.
	m.content.SetBounds(core.UnitRect{Width: met.CellWidth * 20, Height: met.CellHeight * 8})
	if core.FindSmoothPositioning(m.content.Self()) {
		t.Skip("content resolved to a smooth surface; cell-quantization not exercised")
	}

	b.Clear(style.DefaultStyle())
	m.content.Paint(p)

	for _, btn := range m.content.buttonTrinkets {
		if x := btn.Bounds().X; x%met.CellWidth != 0 {
			t.Errorf("button %q bounds.X=%d is not column-aligned (column=%d units)",
				btn.Text(), x, met.CellWidth)
		}
	}
}
