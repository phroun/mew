package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
)

// A button's cap is one row of text and stays one row, however deep the row it
// is put in. Beside something three rows tall it sits in the row rather than
// becoming a three-row slab, and its shadow keeps the row under the cap.
//
// The cap and the shadow arrive as one size, so a layout free to stretch the
// pair stretched the cap and left the shadow a row of its own at the bottom.
func TestAButtonsCapDoesNotStretch(t *testing.T) {
	p := NewPanel()
	p.SetLayoutManager(layout.NewBoxLayout(core.Horizontal))

	button := NewButton("OK")
	tall := NewListView() // something with real height beside it
	p.AddChild(button)
	p.AddChild(tall)

	m := core.FindEffectiveCellMetrics(p.Self())
	rows := core.Unit(3)
	p.SetBounds(core.UnitRect{Width: 400, Height: m.UnitsPerCellHeight * rows})

	got := button.Bounds()
	if want := m.UnitsPerCellHeight * 2; got.Height != want {
		t.Errorf("in a %d-row row the button is %d tall, want its cap and its shadow (%d)",
			rows, got.Height, want)
	}
	// Centred, the cap takes the middle row and the shadow the last.
	if want := m.UnitsPerCellHeight; got.Y != want {
		t.Errorf("the cap starts at y=%d, want the middle row (%d)", got.Y, want)
	}
}

// And its width is its caption's, not the column's.
func TestAButtonsCapDoesNotStretchAcross(t *testing.T) {
	p := NewPanel()
	p.SetLayoutManager(layout.NewBoxLayout(core.Vertical))

	button := NewButton("OK")
	p.AddChild(button)
	p.SetBounds(core.UnitRect{Width: 400, Height: 200})

	if got, want := button.Bounds().Width, button.SizeHint().Width; got != want {
		t.Errorf("in a 400-wide column the button is %d wide, want its caption's %d", got, want)
	}
}
