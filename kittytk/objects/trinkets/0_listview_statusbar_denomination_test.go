package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A list view asks for thirty characters' width, and a character is one cell
// -- so the number is however many units make thirty cells, in the list's own
// denomination. Font.MeasureRunes fixes a rune at eight units (sixteen for the
// double-width demo face), which is only right at 8x16.
func TestListViewAsksForThirtyCharactersAtEveryDenomination(t *testing.T) {
	sameWidthAtEveryDenomination(t, "list view", func(m core.CellMetrics) core.Trinket {
		l := NewListView()
		l.SetCellMetrics(&m)
		return l
	})
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
