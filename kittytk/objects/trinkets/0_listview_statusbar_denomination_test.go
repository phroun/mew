package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The size a list asks for when nothing sets one is three cells each way, in
// the denomination of the surface it is painted on -- so it is the same
// physical size whatever that denomination is, and it does not depend on the
// face or on what the list holds. It used to be Font.MeasureRunes(30): a text
// measurement, fixed at eight units per character, for a quantity that is
// neither text nor a fixed unit count.
func TestListViewAsksForThreeCellsWhenNothingSetsASize(t *testing.T) {
	for _, m := range capDenominations {
		l := NewListView()
		l.SetCellMetrics(&m)
		got := l.SizeHint()
		if got.Width != m.UnitsPerCellWidth*3 || got.Height != m.UnitsPerCellHeight*3 {
			t.Errorf("at %dx%d the list asks for %dx%d units, want %dx%d",
				m.UnitsPerCellWidth, m.UnitsPerCellHeight, got.Width, got.Height,
				m.UnitsPerCellWidth*3, m.UnitsPerCellHeight*3)
		}
	}
	// The face is not part of it.
	l := NewListView()
	l.SetFont(core.FontTuesday12)
	if got := l.SizeHint().Width; got != core.DefaultCellMetrics().UnitsPerCellWidth*3 {
		t.Errorf("in Tuesday the list asks for %d units, want %d",
			got, core.DefaultCellMetrics().UnitsPerCellWidth*3)
	}
}

// An item too long for the list is cut back until it fits the width left
// beside the icon column. That width is counted in the list's units and the
// item was measured in the default ones, so at 16x32 the text was let run to
// twice the room it has and at 4x8 it was cut to half.
func TestListViewElidesAnItemTheSameAtEveryDenomination(t *testing.T) {
	samePixels(t, "list view", capDenominations, func(p *core.Painter, m core.CellMetrics) {
		l := NewListView()
		l.AddTextItem("An item whose text is far too long for the width it is given")
		l.AddTextItem("Short")
		l.SetCellMetrics(&m)
		l.SetBounds(core.UnitRect{
			Width:  30 * m.UnitsPerCellWidth,
			Height: 3 * m.UnitsPerCellHeight,
		})
		l.Paint(p)
	})
}

// An auto-width status section is its measured text plus a cell either side,
// and where it ends is where the next section starts. The margin followed the
// denomination and the text did not, so the sections walked apart.
func TestStatusBarSectionsSitTheSameAtEveryDenomination(t *testing.T) {
	samePixels(t, "status bar", capDenominations, func(p *core.Painter, m core.CellMetrics) {
		s := NewStatusBar()
		s.SetSections([]StatusSection{
			{Text: "Ready"},
			{Spans: []StatusTextSpan{{Text: "Line 42"}, {Text: ", Col 7"}}},
			{Text: "UTF-8"},
			{Text: "", Width: -1},
		})
		s.SetCellMetrics(&m)
		s.SetBounds(core.UnitRect{Width: 40 * m.UnitsPerCellWidth, Height: m.UnitsPerCellHeight})
		s.Paint(p)
	})
}
