package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// listWith builds a list wide enough that nothing is ellipsized, with more
// items than fit so it carries a scrollbar.
func listWith(t *testing.T, d core.Direction, captions ...string) *ListView {
	t.Helper()
	l := NewListView()
	l.SetDirection(d)
	for _, c := range captions {
		l.AddTextItem(c)
	}
	for i := 0; i < 40; i++ {
		l.AddTextItem("filler")
	}
	l.SetBounds(core.UnitRect{Width: 400, Height: 96})
	if !l.showsScrollbar() {
		t.Fatal("the list holds fewer items than it shows; there is no bar to look at")
	}
	return l
}

// The scrollbar takes the trailing edge of the list, and the column it sits in
// is what a press has to land in to reach it -- not everything past its near
// edge, which in a right-to-left list is the whole row.
func TestAListPutsItsScrollbarOnTheTrailingEdge(t *testing.T) {
	lane := core.DefaultCellMetrics().UnitsPerCellWidth

	ltr := listWith(t, core.DirLTR, "one")
	if got, want := ltr.laneX(), core.Unit(400)-lane; got != want {
		t.Errorf("left to right: the lane is at x=%d, want %d", got, want)
	}

	rtl := listWith(t, core.DirRTL, "one")
	if got := rtl.laneX(); got != 0 {
		t.Errorf("right to left: the lane is at x=%d, want the left edge", got)
	}

	// A press in that column reaches the bar; unscrolled its thumb is at the
	// top of the track, so the press starts a drag rather than paging.
	rtl.HandleMousePress(core.MousePressEvent{X: 1, Y: 4, Button: core.LeftButton})
	if !rtl.scrollbarDragging {
		t.Error("right to left: a press on the left column did not reach the bar")
	}

	// Past that column it is content again. The lane is ONE column wherever it
	// sits, so a press to the right of a left-hand lane is a row, not a track.
	content := listWith(t, core.DirRTL, "one")
	content.HandleMousePress(core.MousePressEvent{X: 200, Y: 20, Button: core.LeftButton})
	if content.scrollbarDragging {
		t.Error("right to left: a press in the middle of a row reached the bar")
	}
	if content.CurrentIndex() != 1 {
		t.Errorf("right to left: a press on the second row selected item %d", content.CurrentIndex())
	}

	// And the same press in a left-to-right list is content, which selects.
	ltr.HandleMousePress(core.MousePressEvent{X: 1, Y: 4, Button: core.LeftButton})
	if ltr.scrollbarDragging {
		t.Error("left to right: a press on the left column reached the bar, which is on the other side")
	}
	if ltr.CurrentIndex() != 0 {
		t.Errorf("left to right: a press on the first row selected item %d", ltr.CurrentIndex())
	}
}

// The row's own chrome reads from the list's leading edge, and the arrow marking
// the current item points into the row rather than always to the right.
func TestAListsRowChromeReadsFromItsLeadingEdge(t *testing.T) {
	m := core.DefaultCellMetrics()

	arrowAt := func(d core.Direction) (core.Unit, rune) {
		ink := newInk(t)
		l := listWith(t, d, "one")
		l.SetFocus()
		l.SetCurrentIndex(0)
		l.Paint(core.NewPainter(ink))
		for _, want := range []rune{'▸', '◂'} {
			if x, ok := ink.cellAt(want); ok {
				return x, want
			}
		}
		t.Fatalf("%v: the list drew no current-item arrow", d)
		return 0, 0
	}

	x, glyph := arrowAt(core.DirLTR)
	if x != 0 || glyph != '▸' {
		t.Errorf("left to right: the arrow is %q at x=%d, want '▸' at 0", glyph, x)
	}

	x, glyph = arrowAt(core.DirRTL)
	if want := core.Unit(400) - m.UnitsPerCellWidth; x != want || glyph != '◂' {
		t.Errorf("right to left: the arrow is %q at x=%d, want '◂' at %d", glyph, x, want)
	}
}

// An item's text follows its OWN language. A list of names may hold Hebrew and
// English together, and each begins on the side its own script does -- which is
// what the toolkit already says about a label's caption, said again for a row.
func TestAListsItemsEachReadTheirOwnLanguage(t *testing.T) {
	const english = "Address"
	const hebrew = "שלום"

	drawn := func(d core.Direction) (e, h core.Unit) {
		ink := newInk(t)
		l := listWith(t, d, english, hebrew)
		l.Paint(core.NewPainter(ink))
		ex, ok := ink.textAt(english)
		if !ok {
			t.Fatalf("%v: the English item was not drawn", d)
		}
		hx, ok := ink.textAt(hebrew)
		if !ok {
			t.Fatalf("%v: the Hebrew item was not drawn", d)
		}
		return ex, hx
	}

	// In a left-to-right list the English name starts at the near edge of the
	// text column and the Hebrew one at its far edge.
	e, h := drawn(core.DirLTR)
	if e >= h {
		t.Errorf("left to right: English at x=%d and Hebrew at x=%d; the Hebrew one should sit further along",
			e, h)
	}

	// In a right-to-left list they change places, and neither item moved
	// because of the other.
	e, h = drawn(core.DirRTL)
	if h <= e {
		t.Errorf("right to left: Hebrew at x=%d and English at x=%d; the English one should sit further back",
			h, e)
	}
}

// A number is not a language. An item with nothing strongly directional in it
// takes the list's direction, so a column of figures lines up with the rest.
func TestAListsFiguresTakeTheListsDirection(t *testing.T) {
	l := listWith(t, core.DirRTL, "1972")
	if got := l.itemTextSide(l.Item(0)); got != core.SideRight {
		t.Errorf("in a right-to-left list a figure begins on the %v side, want the right", got)
	}
	l = listWith(t, core.DirLTR, "1972")
	if got := l.itemTextSide(l.Item(0)); got != core.SideLeft {
		t.Errorf("in a left-to-right list a figure begins on the %v side, want the left", got)
	}
}

// The bar's column is not the text's to draw in. A list narrow enough that its
// captions fill the row cuts them at the lane rather than under it.
func TestAListsTextStopsAtTheScrollbarsColumn(t *testing.T) {
	const caption = "an item whose caption is far too long to fit in this narrow list"
	lane := core.DefaultCellMetrics().UnitsPerCellWidth

	for _, d := range []core.Direction{core.DirLTR, core.DirRTL} {
		ink := newInk(t)
		l := NewListView()
		l.SetDirection(d)
		for i := 0; i < 40; i++ {
			l.AddTextItem(caption)
		}
		l.SetBounds(core.UnitRect{Width: 200, Height: 96})
		l.Paint(core.NewPainter(ink))

		laneAt := l.laneX()
		for _, tx := range ink.texts {
			end := tx.x + l.MeasureText(tx.s)
			if tx.x < laneAt+lane && end > laneAt {
				t.Errorf("%v: a caption spans %d..%d, which runs through the lane at %d..%d",
					d, tx.x, end, laneAt, laneAt+lane)
				break
			}
		}
	}
}

// A list built before it is laid out still starts at its first item.
//
// The first item added makes itself current, and a script builds a whole list
// before anything gives it bounds -- so a list with no room yet is asked to
// bring item 0 into view. There is no view to bring it into, and doing the
// arithmetic anyway put the list one row down before it was ever drawn.
func TestAListBuiltBeforeItIsLaidOutStartsAtTheTop(t *testing.T) {
	l := NewListView()
	for i := 0; i < 20; i++ {
		l.AddTextItem("item")
	}
	if got := l.scrollOffset; got != 0 {
		t.Errorf("a list with no bounds has scrolled to %d", got)
	}
	l.SetBounds(core.UnitRect{Width: 200, Height: 96})
	if got := l.scrollOffset; got != 0 {
		t.Errorf("laid out, the list starts at item %d rather than its first", got)
	}
}
