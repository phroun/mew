package layout

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The run of a horizontal box reads the way its direction does.
//
// The toolkit already sells AlignLayoutNatural as "the side the surrounding
// direction begins on", so a row whose own beginning is always the left edge
// contradicts a word every trinket can be given.
//
// The order the children were added in does not change. What changes is where
// each one lands: a child that stood d units in from the left of a
// left-to-right row stands d units in from the right of a right-to-left one.
func TestARowRunsTheWayItReads(t *testing.T) {
	const room = core.Unit(400)
	widths := []core.Unit{40, 24, 56}

	run := func(d core.Direction) []core.UnitRect {
		c := newDirContainer(d)
		l := NewBoxLayout(core.Horizontal)
		l.SetSpacing(8)
		out := make([]core.Trinket, len(widths))
		for i, w := range widths {
			// Blocks, so the boundary between two of them is the configured
			// spacing rather than the air an inline child brings.
			b := newBlock(w, 16)
			c.AddChild(b)
			l.AddTrinket(b)
			out[i] = b
		}
		l.Layout(c, core.UnitRect{Width: room, Height: 100})
		got := make([]core.UnitRect, len(out))
		for i, w := range out {
			got[i] = w.Bounds()
		}
		return got
	}

	ltr, rtl := run(core.DirLTR), run(core.DirRTL)

	// 0, then 40+8, then 48+24+8.
	for i, want := range []core.Unit{0, 48, 80} {
		if ltr[i].X != want {
			t.Fatalf("left to right: child %d at x=%d, want %d -- the run itself is wrong", i, ltr[i].X, want)
		}
	}

	for i := range widths {
		want := room - ltr[i].X - widths[i]
		if rtl[i].X != want {
			t.Errorf("right to left: child %d at x=%d, want %d", i, rtl[i].X, want)
		}
		if rtl[i].Width != widths[i] {
			t.Errorf("right to left: child %d is %d wide, want %d -- reflecting a run does not resize it",
				i, rtl[i].Width, widths[i])
		}
	}

	// The first child added is the one at the trailing edge, which is what
	// makes this a direction and not a reordering.
	if rtl[0].X+rtl[0].Width != room {
		t.Errorf("the first child ends at %d, want the room's far edge %d", rtl[0].X+rtl[0].Width, room)
	}
}

// A reflected row still lands on whole cells.
//
// Reflection is the one place a position is SUBTRACTED, so a room whose
// trailing edge is not itself on a cell hands its children back off the cells
// -- and a child standing a fraction of a cell in draws in one cell and answers
// the mouse in another. Margins are units and need not be whole cells, which is
// how a room like that arises. What puts the children back is placeChild, and
// this is the only run in the package that gives it anything to do across.
func TestAMirroredRowLandsOnWholeCells(t *testing.T) {
	m := core.DefaultCellMetrics()
	c := newCellContainer()
	c.SetDirection(core.DirRTL)

	l := NewBoxLayout(core.Horizontal)
	l.SetSpacing(8)
	// Three units off a whole cell on the trailing edge, so the reflection of
	// every child carries that fraction unless something takes it off.
	l.SetContentsMargins(core.UnitMargins{Right: 3})

	kids := make([]core.Trinket, 0, 3)
	for _, w := range []core.Unit{40, 24, 56} {
		b := newBlock(w, 16)
		c.AddChild(b)
		l.AddTrinket(b)
		kids = append(kids, b)
	}
	l.Layout(c, core.UnitRect{Width: 400, Height: 100})

	for i, k := range kids {
		if x := k.Bounds().X; x%m.UnitsPerCellWidth != 0 {
			t.Errorf("child %d starts at x=%d, which is %d units into a cell",
				i, x, x%m.UnitsPerCellWidth)
		}
	}
}

// The column of air an inline trinket keeps travels with it. A first child that
// brings its own air keeps that air on the outside of the run, which in a
// right-to-left row is the right-hand edge.
func TestTheAirAnInlineChildKeepsMirrorsWithIt(t *testing.T) {
	const room = core.Unit(400)
	m := core.DefaultCellMetrics()

	c := newDirContainer(core.DirRTL)
	first := newSized(40, 16) // not a container, so it reads as inline
	c.AddChild(first)
	l := NewBoxLayout(core.Horizontal)
	l.SetSpacing(8)
	l.AddTrinket(first)
	l.Layout(c, core.UnitRect{Width: room, Height: 100})

	want := room - m.UnitsPerCellWidth - 40
	if got := first.Bounds().X; got != want {
		t.Errorf("the inline child is at x=%d, want %d -- a column of air short of the far edge", got, want)
	}
}

// A direction and a reversed flex_direction are two statements, and both hold.
//
// The direction settles where the line begins and which way it grows. Reverse
// settles which end of the children is walked into it first, and moves no slot
// -- so a right-to-left line still begins at the right whether or not it is
// reversed, and what reverse changes is that the first child is walked in last
// and lands furthest along.
func TestReverseAndDirectionAreSeparateStatements(t *testing.T) {
	const room = core.Unit(400)

	run := func(dir FlexDirection) []core.UnitRect {
		c := newDirContainer(core.DirRTL)
		l := NewFlexLayout()
		l.SetDirection(dir)
		l.SetSpacing(0)
		out := make([]core.Trinket, 3)
		for i := range out {
			b := newBlock(40, 16)
			c.AddChild(b)
			l.AddTrinket(b)
			out[i] = b
		}
		l.Layout(c, core.UnitRect{Width: room, Height: 100})
		got := make([]core.UnitRect, len(out))
		for i, w := range out {
			got[i] = w.Bounds()
		}
		return got
	}

	plain := run(FlexRow)
	if plain[0].X < plain[2].X {
		t.Errorf("right to left: the first child is at x=%d and the last at x=%d; the run did not turn over",
			plain[0].X, plain[2].X)
	}
	if want := room - 40; plain[0].X != want {
		t.Errorf("right to left: the first child is at x=%d, want the far edge %d", plain[0].X, want)
	}

	reversed := run(FlexRowReverse)
	if reversed[0].X > reversed[2].X {
		t.Errorf("row_reverse in a right-to-left panel: the first child is at x=%d and the last at x=%d; want them left to right",
			reversed[0].X, reversed[2].X)
	}
	// The line begins where the direction says regardless, so the child walked
	// in first is the one against the far edge.
	if got := reversed[2].X + reversed[2].Width; got != room {
		t.Errorf("row_reverse in a right-to-left panel: the line ends at %d, want the far edge %d", got, room)
	}
}

// A wrapping column stacks its lines the way the direction reads. The axis
// lines stack across is the one text runs along, so a column that wraps in a
// right-to-left panel puts its first line at the right.
func TestAWrappingColumnStacksItsLinesTheWayItReads(t *testing.T) {
	const room = core.Unit(400)

	run := func(d core.Direction) (core.UnitRect, core.UnitRect) {
		c := newDirContainer(d)
		l := NewFlexLayout()
		l.SetDirection(FlexColumn)
		l.SetWrap(FlexWrapNormal)
		l.SetSpacing(0)
		out := make([]core.Trinket, 2)
		for i := range out {
			b := newBlock(40, 60)
			c.AddChild(b)
			l.AddTrinket(b)
			out[i] = b
		}
		// Room for one 60-deep child per line, so the second starts a line.
		l.Layout(c, core.UnitRect{Width: room, Height: 60})
		return out[0].Bounds(), out[1].Bounds()
	}

	firstLTR, secondLTR := run(core.DirLTR)
	if firstLTR.X >= secondLTR.X {
		t.Fatalf("left to right: the lines are at x=%d and x=%d; they did not stack across",
			firstLTR.X, secondLTR.X)
	}

	firstRTL, secondRTL := run(core.DirRTL)
	if firstRTL.X <= secondRTL.X {
		t.Errorf("right to left: the first line is at x=%d and the second at x=%d; the lines stack the wrong way",
			firstRTL.X, secondRTL.X)
	}
}

// A grid's columns run the way the direction reads, so column 0 is the
// rightmost in a right-to-left form and a label written first still sits beside
// the field written after it.
//
// What is reflected is the CELL. Where a child sits inside its cell was already
// settled against the direction, and an optical alignment says a side of the
// screen outright: reflecting that as well would land it on the other one.
func TestAGridsColumnsRunTheWayItReads(t *testing.T) {
	const room = core.Unit(400)

	c := newDirContainer(core.DirRTL)
	// Narrower than its cell, and pinned to the left of wherever that cell
	// lands, so the test can tell a reflected cell from a reflected child.
	label := newAlignedTrinket(24, 16, core.Alignment{H: core.AlignOpticalLeft, V: core.AlignMiddle})
	field := newBlock(24, 16)
	c.AddChild(label)
	c.AddChild(field)

	l := NewGridLayout()
	l.SetSpacing(0)
	l.AddTrinketAt(label, 0, 0)
	l.AddTrinketAt(field, 0, 1)
	l.SetColumnStretch(0, 1)
	l.SetColumnStretch(1, 1)
	l.Layout(c, core.UnitRect{Width: room, Height: 100})

	got, other := label.Bounds(), field.Bounds()
	if got.X <= other.X {
		t.Errorf("column 0 is at x=%d and column 1 at x=%d; the columns did not turn over", got.X, other.X)
	}
	// The right-hand half of the room, and the child at the left of it: the
	// cell moved and the alignment inside it did not.
	if want := room / 2; got.X != want {
		t.Errorf("the optical_left child is at x=%d, want the left edge of the far column, %d", got.X, want)
	}
}
