package layout

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// spanGrid lays the children out in a grid with the given columns and returns
// the grid and where each child landed. Blocks throughout, so the columns are
// exactly where the bands put them.
func spanGrid(spacing core.Unit, columns []Band, kids ...core.Trinket) (*GridLayout, *dirContainer, []core.UnitRect) {
	l := NewGridLayout()
	l.SetSpacing(spacing)
	for _, b := range columns {
		l.AddColumn(b)
	}
	c := newDirContainer(core.DirLTR)
	for _, k := range kids {
		c.AddChild(k)
		l.AddTrinket(k)
	}
	l.Layout(c, core.UnitRect{Width: 400, Height: 200})
	out := make([]core.UnitRect, len(kids))
	for i, k := range kids {
		out[i] = k.Bounds()
	}
	return l, c, out
}

// spanGridSnug lays the children out at exactly the size the grid asks for, so
// no column takes any slack and each is left at the floor the bands and the
// spans put it at.
func spanGridSnug(columns []Band, kids ...core.Trinket) []core.UnitRect {
	l := NewGridLayout()
	l.SetSpacing(0)
	for _, b := range columns {
		l.AddColumn(b)
	}
	c := newDirContainer(core.DirLTR)
	for _, k := range kids {
		c.AddChild(k)
		l.AddTrinket(k)
	}
	hint := l.SizeHint(c)
	l.Layout(c, core.UnitRect{Width: hint.Width, Height: hint.Height})
	out := make([]core.UnitRect, len(kids))
	for i, k := range kids {
		out[i] = k.Bounds()
	}
	return out
}

// A span is a claim on the run it covers, so columns holding nothing else are
// raised until it fits.
//
// All three passes measured only children whose span was one, so a spanning
// child was invisible to every one of them: columns holding nothing else
// collapsed to zero and the span came out at the width of the boundaries it
// crossed. The filed case was a 120-wide child across two empty columns in a
// grid 400 wide, laid out 8 units.
func TestASpanRaisesTheColumnsItCovers(t *testing.T) {
	l, c, got := spanGrid(0, nil,
		placedBlock(120, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 2}),
	)
	if got[0].Width != 120 {
		t.Errorf("a 120-wide span across two empty columns is %d wide", got[0].Width)
	}
	if hint := l.SizeHint(c); hint.Width != 120 {
		t.Errorf("the grid asks for %d, and lays the span out at 120", hint.Width)
	}

	// It is a claim on the RUN, not on each column: two columns under a
	// 120-wide span are 120 between them, not 120 each.
	_, _, got = spanGrid(0, nil,
		placedBlock(120, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 2}),
		placedBlock(10, 20, core.GridPlacement{Row: 1, Column: 0}),
		placedBlock(10, 20, core.GridPlacement{Row: 1, Column: 1}),
	)
	if w := got[1].Width + got[2].Width; w != 120 {
		t.Errorf("the two columns under a 120-wide span come to %d", w)
	}
}

// A span that already fits asks for nothing.
func TestASpanThatFitsRaisesNothing(t *testing.T) {
	_, _, got := spanGrid(0, []Band{{Minimum: 80}, {Minimum: 80}},
		placedBlock(120, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 2}),
		placedBlock(10, 20, core.GridPlacement{Row: 1, Column: 0}),
		placedBlock(10, 20, core.GridPlacement{Row: 1, Column: 1}),
	)
	if got[1].Width != 80 || got[2].Width != 80 {
		t.Errorf("columns under a span narrower than they are came out %d and %d, want 80 and 80",
			got[1].Width, got[2].Width)
	}
}

// The boundaries the span crosses count toward what it already has: a span
// across two columns with a gap between them needs that much less from them.
func TestASpanCountsTheBoundariesItCrosses(t *testing.T) {
	// Blocks on both sides of the boundary, so the gap is the spacing.
	_, _, got := spanGrid(20, nil,
		placedBlock(120, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 2}),
		placedBlock(10, 20, core.GridPlacement{Row: 1, Column: 0}),
		placedBlock(10, 20, core.GridPlacement{Row: 1, Column: 1}),
	)
	if w := got[1].Width + got[2].Width; w != 100 {
		t.Errorf("under a 120-wide span with a 20-unit boundary the columns come to %d, want 100", w)
	}
	if got[0].Width != 120 {
		t.Errorf("the span is %d wide, want the 120 it asked for", got[0].Width)
	}
}

// The shortfall goes by stretch where the covered columns stretch: a band
// pinned at no stretch was pinned on purpose, and widening it behind the
// author's back is what they pinned it to prevent.
//
// Measured with the grid laid out at the size it asks for, so there is no room
// left over -- a stretching column in a roomier grid takes the slack as well,
// which would hide where the span's own shortfall went.
func TestASpansShortfallGoesByStretch(t *testing.T) {
	// Both columns start at 10, what their own child needs. The 100 missing
	// from the span goes entirely to the one that stretches.
	got := spanGridSnug([]Band{{}, {Stretch: 1}},
		placedBlock(120, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 2}),
		placedBlock(10, 20, core.GridPlacement{Row: 1, Column: 0}),
		placedBlock(10, 20, core.GridPlacement{Row: 1, Column: 1}),
	)
	if got[1].Width != 10 {
		t.Errorf("the pinned column is %d wide; the span should not have widened it", got[1].Width)
	}
	if got[2].Width != 110 {
		t.Errorf("the stretching column is %d wide, want 110 -- its own 10 and all 100 missing",
			got[2].Width)
	}

	// Weights are shares: three parts to one, over and above their own 10.
	got = spanGridSnug([]Band{{Stretch: 3}, {Stretch: 1}},
		placedBlock(120, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 2}),
		placedBlock(10, 20, core.GridPlacement{Row: 1, Column: 0}),
		placedBlock(10, 20, core.GridPlacement{Row: 1, Column: 1}),
	)
	if got[1].Width != 85 || got[2].Width != 35 {
		t.Errorf("three parts to one put %d and %d, want 85 and 35 -- 75 and 25 over their own 10",
			got[1].Width, got[2].Width)
	}

	// A shortfall that does not divide by the weights still has to land whole:
	// 110 in three parts to one is 82 and a half against 27 and a half, and
	// the unit left over goes to a column that took a share.
	got = spanGridSnug([]Band{{Stretch: 3}, {Stretch: 1}},
		placedBlock(130, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 2}),
		placedBlock(10, 20, core.GridPlacement{Row: 1, Column: 0}),
		placedBlock(10, 20, core.GridPlacement{Row: 1, Column: 1}),
	)
	if w := got[1].Width + got[2].Width; w != 130 {
		t.Errorf("the two columns come to %d, want the span's 130 -- the rounding was dropped", w)
	}
	if got[1].Width != 93 || got[2].Width != 37 {
		t.Errorf("three parts to one of 110 put %d and %d, want 93 and 37",
			got[1].Width, got[2].Width)
	}
}

// Where nothing under the span stretches, the columns share it evenly, and the
// span still fits exactly however the division rounds.
func TestASpansShortfallIsSharedEvenlyWhenNothingStretches(t *testing.T) {
	// 100 across three columns is 33 and a third: the remainder has to land
	// somewhere or the span comes out a unit short of what it asked for.
	got := spanGridSnug(nil,
		placedBlock(100, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 3}),
		placedBlock(0, 20, core.GridPlacement{Row: 1, Column: 0}),
		placedBlock(0, 20, core.GridPlacement{Row: 1, Column: 1}),
		placedBlock(0, 20, core.GridPlacement{Row: 1, Column: 2}),
	)
	if got[0].Width != 100 {
		t.Errorf("a 100-wide span across three columns is %d wide", got[0].Width)
	}
	widths := []core.Unit{got[1].Width, got[2].Width, got[3].Width}
	var total core.Unit
	for _, w := range widths {
		total += w
	}
	if total != 100 {
		t.Errorf("the three columns come to %d, want the span's 100", total)
	}
	for _, w := range widths {
		if w < 33 || w > 34 {
			t.Errorf("the columns came out %v; an even share of 100 is 33 or 34 each", widths)
			break
		}
	}
}

// Shortest span first, which is what keeps a grid from being inflated. Where a
// narrow span needs more per column than a wider one covering it, settling the
// wide one first spreads width the narrow one then has to add to anyway.
//
// A 200-wide span over columns 0-1, and a 120-wide span over columns 0-2:
//
//   - shortest first: 0 and 1 come to 100 each, and the wide span finds 200
//     across the three and asks for nothing. The grid is 200 wide.
//   - widest first: all three get 40, then the narrow span still needs 120
//     more across 0 and 1. The grid comes out 240 -- 40 of it wasted on a
//     column nothing asked to widen.
func TestSpansAreSettledShortestFirst(t *testing.T) {
	build := func(reversed bool) core.UnitSize {
		kids := []core.Trinket{
			placedBlock(200, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 2}),
			placedBlock(120, 20, core.GridPlacement{Row: 1, Column: 0, ColumnSpan: 3}),
			placedBlock(0, 20, core.GridPlacement{Row: 2, Column: 2}),
		}
		if reversed {
			kids[0], kids[1] = kids[1], kids[0]
		}
		l := NewGridLayout()
		l.SetSpacing(0)
		c := newDirContainer(core.DirLTR)
		for _, k := range kids {
			c.AddChild(k)
			l.AddTrinket(k)
		}
		return l.SizeHint(c)
	}

	if got := build(false); got.Width != 200 {
		t.Errorf("the grid asks for %d wide, want 200 -- the wide span fits in what the narrow one settled",
			got.Width)
	}
	// And the answer does not depend on the order the children were written
	// in, only on how far each span reaches.
	if got, reversed := build(false), build(true); got != reversed {
		t.Errorf("written one way the grid asks for %v and the other way %v", got, reversed)
	}
}

// Spans that reach equally far are settled in the order they were written, so
// the same script lays out the same way every time.
//
// Two spans of equal reach that overlap do not commute: each raises only what
// is missing, so whichever goes first leaves the other less to ask for. Order
// them any other way and a grid's columns depend on how the sort happened to
// break a tie.
func TestSpansReachingEquallyFarAreSettledAsWritten(t *testing.T) {
	// 100 across columns 0-1 and 100 across columns 1-2. Written in that
	// order: the first settles 0 and 1 at 50 each, then the second finds 50
	// in column 1 and puts the missing 50 across columns 1 and 2.
	build := func(reversed bool) [3]core.Unit {
		kids := []core.Trinket{
			placedBlock(100, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 2}),
			placedBlock(100, 20, core.GridPlacement{Row: 1, Column: 1, ColumnSpan: 2}),
			placedBlock(0, 20, core.GridPlacement{Row: 2, Column: 0}),
			placedBlock(0, 20, core.GridPlacement{Row: 2, Column: 1}),
			placedBlock(0, 20, core.GridPlacement{Row: 2, Column: 2}),
		}
		if reversed {
			kids[0], kids[1] = kids[1], kids[0]
		}
		got := spanGridSnug(nil, kids...)
		return [3]core.Unit{got[2].Width, got[3].Width, got[4].Width}
	}

	if got, want := build(false), [3]core.Unit{50, 75, 25}; got != want {
		t.Errorf("written 0-1 then 1-2 the columns are %v, want %v", got, want)
	}
	// Writing them the other way round mirrors it, which is what says the
	// order they were written in is what decided.
	if got, want := build(true), [3]core.Unit{25, 75, 50}; got != want {
		t.Errorf("written 1-2 then 0-1 the columns are %v, want %v", got, want)
	}
}

// Rows are the same rule down the other axis.
func TestASpanRaisesTheRowsItCovers(t *testing.T) {
	l := NewGridLayout()
	l.SetSpacing(10)
	c := newDirContainer(core.DirLTR)
	tall := placedBlock(20, 90, core.GridPlacement{Row: 0, Column: 0, RowSpan: 2})
	beside := placedBlock(20, 10, core.GridPlacement{Row: 0, Column: 1})
	for _, k := range []core.Trinket{tall, beside} {
		c.AddChild(k)
		l.AddTrinket(k)
	}
	l.Layout(c, core.UnitRect{Width: 400, Height: 400})

	if tall.Bounds().Height != 90 {
		t.Errorf("a 90-tall span across two rows is %d tall", tall.Bounds().Height)
	}
	// The two rows plus the boundary between them come to the span's height.
	if h := l.SizeHint(c).Height; h != 90 {
		t.Errorf("the grid asks for %d tall, and lays the span out at 90", h)
	}
}

// What the grid asks for is what it lays out. A pass that measured spans
// differently from the pass that places them would report a size the grid
// then fails to honour -- which is how the span bug hid.
func TestAGridAsksForWhatItLaysOut(t *testing.T) {
	for _, c := range []struct {
		name    string
		margins core.UnitMargins
		kids    []core.Trinket
	}{
		{"a span over empty columns", core.UnitMargins{}, []core.Trinket{
			placedBlock(120, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 2}),
		}},
		{"a span over columns with children of their own", core.UnitMargins{}, []core.Trinket{
			placedBlock(120, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 2}),
			placedBlock(30, 20, core.GridPlacement{Row: 1, Column: 0}),
			placedBlock(30, 20, core.GridPlacement{Row: 1, Column: 1}),
		}},
		{"nested spans", core.UnitMargins{}, []core.Trinket{
			placedBlock(60, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 2}),
			placedBlock(120, 20, core.GridPlacement{Row: 1, Column: 0, ColumnSpan: 3}),
			placedBlock(10, 20, core.GridPlacement{Row: 2, Column: 2}),
		}},
		{"a row span", core.UnitMargins{}, []core.Trinket{
			placedBlock(20, 90, core.GridPlacement{Row: 0, Column: 0, RowSpan: 2}),
			placedBlock(20, 10, core.GridPlacement{Row: 0, Column: 1}),
		}},
		// Inline children, whose bearings close a boundary up by a column
		// instead of opening it by the spacing. A pass that charged spacing
		// there asked for more than it laid out.
		{"inline children whose bearings collapse the boundary", core.UnitMargins{}, []core.Trinket{
			placed(64, 20, core.GridPlacement{Row: 0, Column: 0}),
			placed(64, 20, core.GridPlacement{Row: 0, Column: 1}),
		}},
		{"an inline span over inline columns", core.UnitMargins{}, []core.Trinket{
			placed(120, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 2}),
			placed(30, 20, core.GridPlacement{Row: 1, Column: 0}),
			placed(30, 20, core.GridPlacement{Row: 1, Column: 1}),
		}},
		{"a grid held in from its own edges", core.UnitMargins{Left: 8, Top: 4, Right: 16, Bottom: 12},
			[]core.Trinket{
				placedBlock(120, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 2}),
				placedBlock(30, 20, core.GridPlacement{Row: 1, Column: 1}),
			}},
	} {
		l := NewGridLayout()
		l.SetSpacing(8)
		l.SetContentsMargins(c.margins)
		container := newDirContainer(core.DirLTR)
		for _, k := range c.kids {
			container.AddChild(k)
			l.AddTrinket(k)
		}
		// Lay the grid out at exactly the size it asks for; every child must
		// then get the extent it asked for, with nothing squeezed.
		hint := l.SizeHint(container)
		l.Layout(container, core.UnitRect{Width: hint.Width, Height: hint.Height})

		metrics := core.FindEffectiveCellMetrics(container.Self())
		var right, bottom core.Unit
		for i, k := range c.kids {
			want := k.SizeHint()
			got := k.Bounds()
			if got.Width < want.Width || got.Height < want.Height {
				t.Errorf("%s: child %d asked for %v and was laid out %v in a grid given the %v it asked for",
					c.name, i, want, got.Size(), hint)
			}
			// The cell's own far edge: a child sits a bearing inside it.
			if edge := got.X + got.Width + sideBearing(k, metrics); edge > right {
				right = edge
			}
			if edge := got.Y + got.Height; edge > bottom {
				bottom = edge
			}
		}
		// And it asked for no more than that: the cells reach the far side of
		// the content box, which is everything it asked for less the margin
		// beyond it. A pass that charged a boundary differently from Layout
		// would leave room over at that edge.
		wantRight := hint.Width - c.margins.Right
		wantBottom := hint.Height - c.margins.Bottom
		if right != wantRight || bottom != wantBottom {
			t.Errorf("%s: the grid asked for %v and its cells reach %d,%d, want %d,%d",
				c.name, hint, right, bottom, wantRight, wantBottom)
		}
	}
}
