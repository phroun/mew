package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// A dialog's width is one sum, so every term in it is counted in the dialog's
// own denomination.
//
// The icon gutter and the right margin are UnitsPerCellWidth multiples and
// follow it. The text's share came from MeasureText, which answers in the
// DEFAULT denomination, so at any other one that term alone did not follow --
// and the dialog came out 27 cells wide at 16x32 and 68 at 4x8 where 8x16 gave
// 44. The spread that remains is per-line rounding, which is coarser the
// coarser the units are.
//
// Init(box) is here because NewMessageBox copies the Window it builds
// (m.Window = *window.NewWindow(...)), and the copy's Self still points at the
// original: without it, EffectiveCellMetrics walks from a different object,
// finds no override, and every dialog measures at 8x16 whatever it was set to.
func TestDialogWidthMeasuresInItsOwnDenomination(t *testing.T) {
	b, err := raster.New(640, 400)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)
	defer core.SetTextMeasurer(nil)

	const text = "A reasonably long message line for measuring"
	base := core.Unit(0)
	for _, m := range []core.CellMetrics{
		{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16},
		{UnitsPerCellWidth: 16, UnitsPerCellHeight: 32},
		{UnitsPerCellWidth: 4, UnitsPerCellHeight: 8},
	} {
		mm := m
		box := NewMessageBox("Title", text, ButtonOK)
		box.Init(box)
		box.SetCellMetrics(&mm)
		if em := box.EffectiveCellMetrics(); em != mm {
			t.Fatalf("the dialog did not take the denomination: %+v", em)
		}
		box.ResizeToFitContent()

		// The same dialog holds the same text, so it spans the same number of
		// CELLS whatever the denomination divides them into.
		cells := box.Bounds().Width / m.UnitsPerCellWidth
		if base == 0 {
			base = cells
			continue
		}
		if d := cells - base; d < -3 || d > 3 {
			t.Errorf("at %dx%d the dialog is %d cells wide, want about %d",
				m.UnitsPerCellWidth, m.UnitsPerCellHeight, cells, base)
		}
	}
}
