package layout

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// gridBounds attaches the children to a grid the way a container does -- one at
// a time, with nothing but the child to go on -- and returns where each landed.
func gridBounds(l *GridLayout, bounds core.UnitRect, kids ...core.Trinket) []core.UnitRect {
	c := newDirContainer(core.DirLTR)
	for _, k := range kids {
		c.AddChild(k)
		l.AddTrinket(k)
	}
	l.Layout(c, bounds)
	out := make([]core.UnitRect, len(kids))
	for i, k := range kids {
		out[i] = k.Bounds()
	}
	return out
}

// placed returns a child carrying a grid placement, as a build script gives it.
func placed(w, h core.Unit, p core.GridPlacement) *flexChild {
	c := newFlexChild(w, h)
	if p.RowSpan == 0 {
		p.RowSpan = 1
	}
	if p.ColumnSpan == 0 {
		p.ColumnSpan = 1
	}
	c.SetLayoutGridPlacement(p)
	return c
}

// A grid reads the cell off the child, which is the only way it can be built by
// a container that attaches children one at a time knowing nothing about them.
func TestAGridTakesItsPlacementFromTheChild(t *testing.T) {
	l := NewGridLayout()
	l.SetSpacing(0)

	got := gridBounds(l, core.UnitRect{Width: 200, Height: 100},
		placed(50, 20, core.GridPlacement{Row: 0, Column: 0}),
		placed(50, 20, core.GridPlacement{Row: 0, Column: 1}),
		placed(50, 20, core.GridPlacement{Row: 1, Column: 0}),
	)

	if got[0].Y != got[1].Y {
		t.Errorf("two children in row 0 are at y=%d and y=%d", got[0].Y, got[1].Y)
	}
	if got[0].X == got[1].X {
		t.Errorf("two children in different columns share x=%d", got[0].X)
	}
	if got[2].Y == got[0].Y {
		t.Errorf("a child in row 1 shares row 0's y=%d", got[2].Y)
	}
	if got[2].X != got[0].X {
		t.Errorf("two children in column 0 are at x=%d and x=%d", got[0].X, got[2].X)
	}
}

// A child that states no placement gets a row of its own, so a grid nobody has
// placed anything in reads down the page like a column.
func TestAnUnplacedGridChildGetsItsOwnRow(t *testing.T) {
	l := NewGridLayout()
	l.SetSpacing(0)

	got := gridBounds(l, core.UnitRect{Width: 200, Height: 90},
		newFlexChild(50, 20), newFlexChild(50, 20), newFlexChild(50, 20))

	for i := 1; i < len(got); i++ {
		if got[i].Y <= got[i-1].Y {
			t.Errorf("child %d is at y=%d, not below child %d at y=%d", i, got[i].Y, i-1, got[i-1].Y)
		}
		if got[i].X != got[0].X {
			t.Errorf("child %d is at x=%d, want column zero's %d", i, got[i].X, got[0].X)
		}
	}
}

// A span covers the cells it says it does.
func TestAGridSpanCoversItsCells(t *testing.T) {
	l := NewGridLayout()
	l.SetSpacing(0)

	got := gridBounds(l, core.UnitRect{Width: 200, Height: 100},
		placed(50, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 2}),
		placed(50, 20, core.GridPlacement{Row: 1, Column: 0}),
		placed(50, 20, core.GridPlacement{Row: 1, Column: 1}),
	)

	// It covers both columns: it starts no later than the child in the first
	// and ends no earlier than the child in the second. Comparing widths would
	// not say that -- a spanning child carries two side-bearings where two
	// separate children carry four.
	span, first, second := got[0], got[1], got[2]
	if span.X > first.X {
		t.Errorf("the spanning child starts at %d, after the first column's child at %d", span.X, first.X)
	}
	if span.X+span.Width < second.X+second.Width {
		t.Errorf("the spanning child ends at %d, before the second column's child at %d",
			span.X+span.Width, second.X+second.Width)
	}
	if span.Width <= first.Width || span.Width <= second.Width {
		t.Errorf("the spanning child is %d wide, no wider than the %d and %d it spans",
			span.Width, first.Width, second.Width)
	}
}

// The stretch a child asks for is applied to the row and column it sits in, and
// where two children in one column disagree the larger is what the column gets:
// a column is one thing and cannot take two answers.
func TestAGridColumnTakesTheLargestStretchAskedOfIt(t *testing.T) {
	l := NewGridLayout()
	l.SetSpacing(0)

	got := gridBounds(l, core.UnitRect{Width: 300, Height: 100},
		placed(50, 20, core.GridPlacement{Row: 0, Column: 0}),
		placed(50, 20, core.GridPlacement{Row: 0, Column: 1, ColumnStretch: 1}),
	)
	if got[1].Width <= got[0].Width {
		t.Errorf("the stretching column is %d wide against the fixed one's %d", got[1].Width, got[0].Width)
	}

	// The largest holds even when a LATER child in the same column asks for
	// less, which is what tells "largest wins" apart from "last one wins".
	l = NewGridLayout()
	l.SetSpacing(0)
	got = gridBounds(l, core.UnitRect{Width: 300, Height: 100},
		placed(50, 20, core.GridPlacement{Row: 0, Column: 0, ColumnStretch: 3}),
		placed(50, 20, core.GridPlacement{Row: 0, Column: 1, ColumnStretch: 1}),
		placed(50, 20, core.GridPlacement{Row: 1, Column: 0, ColumnStretch: 0}),
	)
	if got[0].Width <= got[1].Width {
		t.Errorf("column 0 asked for 3 and then 0 against column 1's 1, and came out %d against %d",
			got[0].Width, got[1].Width)
	}
}

// A minimum written on a child reaches the column it sits in, as it reaches the
// line it sits in inside a box. A grid measured size hints alone, so a grid
// reported a floor it did not then apply to its own columns.
func TestAGridColumnHonorsAChildsMinimum(t *testing.T) {
	l := NewGridLayout()
	l.SetSpacing(0)

	small := placed(20, 20, core.GridPlacement{Row: 0, Column: 0})
	small.SetMinimumSize(core.UnitSize{Width: 120, Height: 20})

	got := gridBounds(l, core.UnitRect{Width: 200, Height: 100},
		small,
		placed(20, 20, core.GridPlacement{Row: 0, Column: 1}),
	)
	if got[0].Width < 120 {
		t.Errorf("a child with a minimum of 120 got a column %d wide", got[0].Width)
	}
}
