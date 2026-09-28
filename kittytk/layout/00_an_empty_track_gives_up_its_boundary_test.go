package layout

// A grid track nothing shown stands in is taken out of the arrangement, the
// boundary beside it with it -- unless its band asks for the track on purpose,
// with a Minimum or a Stretch. Most tests here compare a grid holding an empty
// track against the same children renumbered so there is none.

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

type cell struct{ row, col, rowSpan, colSpan int }

// gridOf lays blocks (or inline children, where inline says so) out at the
// given cells and returns the layout, its container and the children.
func gridOf(rows []Band, cells []cell, inline bool) (*GridLayout, *dirContainer, []core.Trinket) {
	l := NewGridLayout()
	l.SetSpacing(8)
	for _, b := range rows {
		l.AddRow(b)
	}
	c := newDirContainer(core.DirLTR)
	var kids []core.Trinket
	for _, at := range cells {
		p := core.GridPlacement{Row: at.row, Column: at.col, RowSpan: at.rowSpan, ColumnSpan: at.colSpan}
		if p.RowSpan == 0 {
			p.RowSpan = 1
		}
		if p.ColumnSpan == 0 {
			p.ColumnSpan = 1
		}
		var k core.Trinket
		if inline {
			s := newSizedTrinket(24, 16)
			s.SetLayoutGridPlacement(p)
			k = s
		} else {
			k = placedBlock(40, 16, p)
		}
		c.AddChild(k)
		l.AddTrinket(k)
		kids = append(kids, k)
	}
	l.Layout(c, core.UnitRect{Width: 400, Height: 200})
	return l, c, kids
}

// sameGrid reports every difference between two grids that should have come
// out alike.
func sameGrid(t *testing.T, what string, a, b []core.Trinket, la, lb *GridLayout, ca, cb *dirContainer) {
	t.Helper()
	for i := range a {
		if a[i].Bounds() != b[i].Bounds() {
			t.Errorf("%s: child %d landed at %+v, want %+v as with no empty track",
				what, i, a[i].Bounds(), b[i].Bounds())
		}
	}
	if got, want := la.SizeHint(ca), lb.SizeHint(cb); got != want {
		t.Errorf("%s: asks for %+v, want %+v as with no empty track", what, got, want)
	}
	if got, want := la.MinimumSize(ca), lb.MinimumSize(cb); got != want {
		t.Errorf("%s: minimum %+v, want %+v as with no empty track", what, got, want)
	}
}

func TestAnEmptyRowGivesUpItsBoundary(t *testing.T) {
	la, ca, a := gridOf(nil, []cell{{row: 0}, {row: 2}}, false)
	lb, cb, b := gridOf(nil, []cell{{row: 0}, {row: 1}}, false)
	sameGrid(t, "middle row", a, b, la, lb, ca, cb)

	la, ca, a = gridOf(nil, []cell{{row: 1}}, false)
	lb, cb, b = gridOf(nil, []cell{{row: 0}}, false)
	sameGrid(t, "leading row", a, b, la, lb, ca, cb)
}

func TestAnEmptyColumnGivesUpItsBoundary(t *testing.T) {
	for _, inline := range []bool{false, true} {
		// Inline on both sides: the boundary that is kept closes the two
		// bearings up into one, as it would between neighbours.
		la, ca, a := gridOf(nil, []cell{{col: 0}, {col: 2}}, inline)
		lb, cb, b := gridOf(nil, []cell{{col: 0}, {col: 1}}, inline)
		sameGrid(t, "middle column", a, b, la, lb, ca, cb)

		la, ca, a = gridOf(nil, []cell{{col: 1}, {col: 3}}, inline)
		lb, cb, b = gridOf(nil, []cell{{col: 0}, {col: 1}}, inline)
		sameGrid(t, "leading and middle columns", a, b, la, lb, ca, cb)
	}
}

// Hiding the only child in a row is how a row comes to be empty in practice:
// the form closes up over it.
func TestHidingARowClosesTheGridUp(t *testing.T) {
	l, c, kids := gridOf(nil, []cell{{row: 0}, {row: 1}, {row: 2}}, false)
	where := kids[1].Bounds().Y
	kids[1].SetVisible(false)
	l.Layout(c, core.UnitRect{Width: 400, Height: 200})
	if got := kids[2].Bounds().Y; got != where {
		t.Errorf("with the middle row hidden the last row is at %d, want %d", got, where)
	}
}

// A band with a Minimum or a Stretch asks for its track, and keeps it -- and
// both boundaries beside it -- with nothing in it.
func TestABandThatAsksForItsTrackKeepsIt(t *testing.T) {
	_, _, kids := gridOf([]Band{{}, {Minimum: 16}, {}}, []cell{{row: 0}, {row: 2}}, false)
	if got := kids[1].Bounds().Y; got != 16+8+16+8 {
		t.Errorf("past an empty row with a minimum the last row is at %d, want %d", got, 16+8+16+8)
	}

	l, c, kids := gridOf([]Band{{}, {Stretch: 1}, {}}, []cell{{row: 0}, {row: 2}}, false)
	if got := kids[1].Bounds().Y; got != 200-16 {
		t.Errorf("past an empty stretching row the last row is at %d, want %d", got, 200-16)
	}
	// At its own size the stretching row is nothing, and both its boundaries
	// are still charged.
	if got := l.SizeHint(c).Height; got != 16+8+8+16 {
		t.Errorf("a grid with an empty stretching row asks for %d tall, want %d", got, 16+8+8+16)
	}
}

// A spacer is the other way to ask for an empty row: it is something standing
// in it.
func TestASpacerKeepsARow(t *testing.T) {
	l, c, kids := gridOf(nil, []cell{{row: 0}, {row: 2}}, false)
	sp := NewSpacer(0, 16)
	c.AddChild(sp)
	l.AddTrinketAt(sp, 1, 0)
	l.Layout(c, core.UnitRect{Width: 400, Height: 200})
	if got := kids[1].Bounds().Y; got != 16+8+16+8 {
		t.Errorf("past a row holding a spacer the last row is at %d, want %d", got, 16+8+16+8)
	}
}

// A child spanning across a column stands in it, so that column keeps its
// boundaries even with nothing of its own in it.
func TestASpanKeepsTheTracksItCrosses(t *testing.T) {
	l, c, _ := gridOf(nil, []cell{{row: 0, col: 0}, {row: 0, col: 2}, {row: 1, col: 0, colSpan: 3}}, false)
	if got, want := l.SizeHint(c).Width, core.Unit(40+8+0+8+40); got != want {
		t.Errorf("columns under a span ask for %d wide, want %d with both boundaries of the crossed column", got, want)
	}
}
