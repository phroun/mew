package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
)

// A panel's frame is drawn inside its own bounds and Layout hands its manager
// what is left, so a bordered panel asks for its content plus the two rows and
// two columns the frame takes -- and an unbordered one asks for its content and
// nothing more.
//
// Asking only for the content is what drew a border through its own children:
// a panel sitting at its own hint had no room for the frame it was about to
// paint.
func TestABorderedPanelAsksForItsFrame(t *testing.T) {
	build := func(border bool) *Panel {
		p := NewPanel()
		p.SetLayoutManager(layout.NewVBoxLayout())
		p.SetBorder(border)
		p.AddChild(NewLabel("content"))
		p.AddChild(NewLabel("more content"))
		return p
	}

	plain, bordered := build(false), build(true)
	m := core.FindEffectiveCellMetrics(plain.Self())

	inner := plain.LayoutManager().SizeHint(plain)
	if got := plain.SizeHint(); got != inner {
		t.Errorf("an unbordered panel asks for %+v against its content's %+v; it has no frame to pay for",
			got, inner)
	}

	want := core.UnitSize{
		Width:  inner.Width + 2*m.UnitsPerCellWidth,
		Height: inner.Height + 2*m.UnitsPerCellHeight,
	}
	if got := bordered.SizeHint(); got != want {
		t.Errorf("a bordered panel asks for %+v, want its content's %+v plus a cell on every side (%+v)",
			got, inner, want)
	}

	// The same for the floor it reports, which is what a box will not squeeze
	// it below.
	innerMin := plain.LayoutManager().MinimumSize(plain)
	if got := plain.MinimumSize(); got != innerMin {
		t.Errorf("an unbordered panel's minimum is %+v, want its content's %+v", got, innerMin)
	}
	wantMin := core.UnitSize{
		Width:  innerMin.Width + 2*m.UnitsPerCellWidth,
		Height: innerMin.Height + 2*m.UnitsPerCellHeight,
	}
	if got := bordered.MinimumSize(); got != wantMin {
		t.Errorf("a bordered panel's minimum is %+v, want %+v", got, wantMin)
	}
}
