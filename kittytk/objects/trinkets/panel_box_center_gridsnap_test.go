package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
)

// A one-row trinket centered beside a taller one in an hbox must land on a
// cell-row boundary. A sub-row offset (half a row, when a 1-row input sits
// next to a 2-row button) is drawn snapped to a row on a cell surface but
// hit-tested at the raw half-row bounds, so clicks land a row off. The box
// layout grid-snaps the centering offset to keep draw and hit together.
func TestHBoxCenteringSnapsToCellRow(t *testing.T) {
	p := NewPanel()
	p.SetLayoutManager(layout.NewBoxLayout(core.Horizontal))

	input := NewTextInput() // one row tall
	button := NewButton("Browse...")
	p.AddChild(input)
	p.AddChild(button)

	metrics := core.FindEffectiveCellMetrics(p.Self())
	// A row two cells tall, so the one-row input would center half a row down.
	p.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 400, Height: metrics.CellHeight * 2})

	y := input.Bounds().Y
	if metrics.CellHeight > 0 && y%metrics.CellHeight != 0 {
		t.Errorf("centered text input Y=%d is not on a cell-row boundary (CellHeight=%d)",
			y, metrics.CellHeight)
	}
	// It must still be one row tall (not stretched to the button's height).
	if h := input.Bounds().Height; h != metrics.CellHeight {
		t.Errorf("text input height=%d, want one row (%d)", h, metrics.CellHeight)
	}
}
