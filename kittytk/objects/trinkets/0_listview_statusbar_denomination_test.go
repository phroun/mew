package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A list's width is a layout width: a count of denomination units, the same
// number whatever the list holds and whatever face it draws with. It went
// through Font.MeasureRunes -- thirty characters at eight units each, sixteen
// for the double-width demo face -- which is a text measurement, and a layout
// width is not text.
func TestListViewWidthIsAUnitCount(t *testing.T) {
	for _, m := range capDenominations {
		l := NewListView()
		l.SetCellMetrics(&m)
		if got := l.SizeHint().Width; got != listViewWidthUnits {
			t.Errorf("at %dx%d the list asks for %d units, want %d",
				m.CellWidth, m.CellHeight, got, listViewWidthUnits)
		}
	}
	// The face is not part of it either.
	l := NewListView()
	l.SetFont(core.FontTuesday12)
	if got := l.SizeHint().Width; got != listViewWidthUnits {
		t.Errorf("in Tuesday the list asks for %d units, want %d", got, listViewWidthUnits)
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
			Width:  30 * m.CellWidth,
			Height: 3 * m.CellHeight,
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
		s.SetBounds(core.UnitRect{Width: 40 * m.CellWidth, Height: m.CellHeight})
		s.Paint(p)
	})
}
