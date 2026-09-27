package layout

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// hinted is a child that carries layout hints, which is how every trinket
// carries them: on itself, not on the call that placed it.
type hinted struct {
	core.TrinketBase
	own core.UnitSize
}

func newHinted(w, h core.Unit) *hinted {
	t := &hinted{own: core.UnitSize{Width: w, Height: h}}
	t.TrinketBase = *core.NewTrinketBase()
	t.Init(t)
	return t
}

func (t *hinted) SizeHint() core.UnitSize { return t.own }

// It declines to fill, so its own size is what shows -- a filled item takes
// whatever the layout gives it, which would hide the hint under test.
func (t *hinted) LayoutAlignment() (core.Alignment, bool) {
	a, set := t.TrinketBase.LayoutAlignment()
	if set {
		return a, true
	}
	return core.Alignment{H: core.AlignOpticalLeft, V: core.AlignMiddle}, true
}

// A hint set on a child that is ALREADY in a layout has to reach it. Over the
// wire that is `set k stretch=3` on a trinket an earlier build placed --
// accepted, stored on the trinket, and then read by nobody if the layout only
// looks when the child is added.
func TestABoxReadsStretchSetAfterTheChildWasAdded(t *testing.T) {
	box := NewBoxLayout(core.Vertical)
	fixed := newHinted(100, 40)
	grower := newHinted(100, 40)
	box.AddTrinket(fixed)
	box.AddTrinket(grower)

	var c core.Container
	box.Layout(c, core.UnitRect{Width: 200, Height: 400})
	if got := grower.Bounds().Height; got != 40 {
		t.Fatalf("with nothing stretching, the second child is %d tall, want its own 40", got)
	}

	// Said after it was placed.
	grower.SetLayoutStretch(1)
	box.Layout(c, core.UnitRect{Width: 200, Height: 400})
	if got := grower.Bounds().Height; got <= 40 {
		t.Errorf("after stretch=1 the second child is still %d tall; the hint never reached the box", got)
	}
	if got := fixed.Bounds().Height; got != 40 {
		t.Errorf("the child that stretches nothing is %d tall, want its own 40", got)
	}

	// And taken away again: a stretch of zero is a real answer, not silence.
	grower.SetLayoutStretch(0)
	box.Layout(c, core.UnitRect{Width: 200, Height: 400})
	if got := grower.Bounds().Height; got != 40 {
		t.Errorf("after stretch=0 the child is %d tall, want back to its own 40", got)
	}
}

// The same for a flex layout's grow, shrink and basis.
func TestAFlexReadsItsHintsSetAfterTheChildWasAdded(t *testing.T) {
	flex := NewFlexLayout()
	flex.SetDirection(FlexRow)
	a := newHinted(100, 40)
	b := newHinted(100, 40)
	flex.AddTrinket(a)
	flex.AddTrinket(b)

	var c core.Container
	flex.Layout(c, core.UnitRect{Width: 400, Height: 100})
	if got := a.Bounds().Width; got != 100 {
		t.Fatalf("with nothing growing, the first child is %d wide, want its own 100", got)
	}

	a.SetLayoutFlex(core.FlexHints{Grow: 1, Shrink: 1, ShrinkSet: true, Basis: core.BasisAuto})
	flex.Layout(c, core.UnitRect{Width: 400, Height: 100})
	if got := a.Bounds().Width; got <= 100 {
		t.Errorf("after grow=1 the first child is still %d wide; the hint never reached the flex", got)
	}

	// A basis said afterwards is the size it starts from.
	a.SetLayoutFlex(core.FlexHints{Grow: 0, Shrink: 1, ShrinkSet: true, Basis: 250})
	flex.Layout(c, core.UnitRect{Width: 400, Height: 100})
	if got := a.Bounds().Width; got != 250 {
		t.Errorf("after basis=250 the first child is %d wide, want 250", got)
	}
}

// And for a grid's row, column and spans.
func TestAGridReadsPlacementSetAfterTheChildWasAdded(t *testing.T) {
	grid := NewGridLayout()
	first := newHinted(40, 20)
	second := newHinted(40, 20)
	grid.AddTrinket(first)
	grid.AddTrinket(second)

	var c core.Container
	grid.Layout(c, core.UnitRect{Width: 400, Height: 400})
	// Stating nothing, each lands in a row of its own.
	if first.Bounds().Y == second.Bounds().Y {
		t.Fatalf("both children are at y=%d; they should be in rows of their own",
			first.Bounds().Y)
	}

	// Move the second one up beside the first, after it was placed.
	second.SetLayoutGridPlacement(core.GridPlacement{Row: 0, Column: 1, RowSpan: 1, ColumnSpan: 1})
	grid.Layout(c, core.UnitRect{Width: 400, Height: 400})
	if first.Bounds().Y != second.Bounds().Y {
		t.Errorf("the second child is at y=%d against the first at y=%d; the placement never reached the grid",
			second.Bounds().Y, first.Bounds().Y)
	}
	if second.Bounds().X <= first.Bounds().X {
		t.Errorf("the second child is at x=%d, want it in the column to the right of %d",
			second.Bounds().X, first.Bounds().X)
	}

	// A span said afterwards is read with the rest of the placement.
	if got := grid.ColumnCount(); got != 2 {
		t.Fatalf("the grid has %d columns, want the 2 its children sit in", got)
	}
	second.SetLayoutGridPlacement(core.GridPlacement{Row: 0, Column: 1, RowSpan: 1, ColumnSpan: 3})
	if got := grid.ColumnCount(); got != 4 {
		t.Errorf("spanning three columns from column 1 the grid has %d columns, want 4; "+
			"the span never reached it", got)
	}
}

// A child that states nothing keeps the row it was given when it was added:
// the autonumber is the item's, and re-reading the hint must not lose it.
func TestAGridKeepsTheAutonumberedRowAcrossPasses(t *testing.T) {
	grid := NewGridLayout()
	var kids []*hinted
	for i := 0; i < 3; i++ {
		k := newHinted(40, 20)
		kids = append(kids, k)
		grid.AddTrinket(k)
	}
	var c core.Container
	grid.Layout(c, core.UnitRect{Width: 400, Height: 400})
	first := []core.Unit{kids[0].Bounds().Y, kids[1].Bounds().Y, kids[2].Bounds().Y}
	grid.Layout(c, core.UnitRect{Width: 400, Height: 400})
	for i, k := range kids {
		if got := k.Bounds().Y; got != first[i] {
			t.Errorf("child %d moved from y=%d to y=%d on a second pass", i, first[i], got)
		}
	}
	if first[0] == first[1] || first[1] == first[2] {
		t.Errorf("the three children share rows: %v", first)
	}
}

// And the axis a child never mentioned is left to whatever the layout was
// holding for it, rather than being replaced wholesale.
//
// A horizontal box places its items against the vertical -- its cross axis --
// so a child that states only what it FILLS has said nothing about where it
// sits, and the box's own answer for that is what should stand.
func TestALayoutKeepsTheAxisTheChildNeverMentioned(t *testing.T) {
	box := NewBoxLayout(core.Horizontal)
	kid := newHinted(40, 20)
	box.AddTrinket(kid)

	// The Go caller's answer for the item: sit at the BOTTOM. The bottom, not
	// the top, because the top is VAlign's zero value and an answer dropped
	// on the floor would look exactly like it.
	box.ItemAt(0).Align = core.Alignment{H: core.AlignCenter, V: core.AlignBottom}

	// The child asks about filling, and about nothing else.
	kid.SetLayoutAlignment(core.Alignment{}.WithFill(false, false))

	var c core.Container
	box.Layout(c, core.UnitRect{Width: 400, Height: 400})

	// It filled neither axis, as it asked.
	if got := kid.Bounds().Height; got != 20 {
		t.Errorf("the child is %d tall, want its own 20 -- it asked to fill nothing", got)
	}
	// And kept the vertical placement it never mentioned: the bottom.
	if got := kid.Bounds().Y; got != 400-20 {
		t.Errorf("the child is at y=%d, want the bottom (%d) the layout was holding for it",
			got, 400-20)
	}
}
