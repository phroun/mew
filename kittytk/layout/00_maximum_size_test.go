package layout

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// boundedChild is a BLOCK child with a size of its own and a ceiling on how
// far it may be grown. A block carries no side-bearings, so what a run comes
// to is what its children came to and a test can say so in numbers.
func boundedChild(w, h core.Unit, max core.UnitSize) *bandChild {
	c := newBandChild(w, h)
	c.SetMaximumSize(max)
	return c
}

// noMax is a ceiling that does not bound, on either axis.
func noMax() core.UnitSize {
	return core.UnitSize{Width: core.Unbounded, Height: core.Unbounded}
}

// boxRun lays children out along a horizontal box and returns where each
// landed.
func boxRun(width core.Unit, kids ...core.Trinket) []core.UnitRect {
	l := NewBoxLayout(core.Horizontal)
	l.SetSpacing(0)
	c := newDirContainer(core.DirLTR)
	for _, k := range kids {
		c.AddChild(k)
		l.AddTrinketWithStretch(k, 1)
	}
	l.Layout(c, core.UnitRect{Width: width, Height: 60})
	out := make([]core.UnitRect, len(kids))
	for i, k := range kids {
		out[i] = k.Bounds()
	}
	return out
}

// A child stops growing at its maximum, and what it turns down goes to the
// others sharing the run rather than being left as a hole.
//
// Nothing read MaximumSize, so max_width and max_height were applied to a
// trinket and then consulted by nobody.
func TestABoxChildStopsAtItsMaximum(t *testing.T) {
	capped := boundedChild(20, 20, core.UnitSize{Width: 60, Height: core.Unbounded})
	open := boundedChild(20, 20, noMax())

	got := boxRun(400, capped, open)
	if got[0].Width != 60 {
		t.Errorf("a child with a maximum of 60 came out %d wide", got[0].Width)
	}
	// The other took everything the capped one did not, so the run is full.
	if w := got[0].Width + got[1].Width; w != 400 {
		t.Errorf("the two children come to %d of the 400 they share", w)
	}
}

// Where every child is capped, what none of them will take is simply left
// over: a maximum bounds a child and does not bind the container.
func TestABoxWhoseChildrenAreAllCappedLeavesTheRest(t *testing.T) {
	got := boxRun(400,
		boundedChild(20, 20, core.UnitSize{Width: 60, Height: core.Unbounded}),
		boundedChild(20, 20, core.UnitSize{Width: 40, Height: core.Unbounded}),
	)
	if got[0].Width != 60 || got[1].Width != 40 {
		t.Errorf("two capped children came out %d and %d, want 60 and 40", got[0].Width, got[1].Width)
	}
}

// Sharing goes round again: when one child stops, its share is divided among
// those still growing in the proportion THEY asked for, not left where it fell.
func TestWhatACappedChildTurnsDownIsSharedByTheRest(t *testing.T) {
	l := NewBoxLayout(core.Horizontal)
	l.SetSpacing(0)
	capped := boundedChild(0, 20, core.UnitSize{Width: 40, Height: core.Unbounded})
	one := boundedChild(0, 20, noMax())
	three := boundedChild(0, 20, noMax())

	c := newDirContainer(core.DirLTR)
	for _, k := range []core.Trinket{capped, one, three} {
		c.AddChild(k)
	}
	l.AddTrinketWithStretch(capped, 6)
	l.AddTrinketWithStretch(one, 1)
	l.AddTrinketWithStretch(three, 3)
	l.Layout(c, core.UnitRect{Width: 240, Height: 60})

	// The capped one would have taken 6/10 of 240; it takes 40 instead, and
	// the remaining 200 goes one part to three between the others.
	if got := capped.Bounds().Width; got != 40 {
		t.Errorf("the capped child is %d wide, want its maximum of 40", got)
	}
	if got, want := one.Bounds().Width, core.Unit(50); got != want {
		t.Errorf("the child asking for one part is %d wide, want %d", got, want)
	}
	if got, want := three.Bounds().Width, core.Unit(150); got != want {
		t.Errorf("the child asking for three parts is %d wide, want %d", got, want)
	}
}

// Down a column it is the same rule on the other axis.
func TestAVerticalBoxChildStopsAtItsMaximum(t *testing.T) {
	l := NewBoxLayout(core.Vertical)
	l.SetSpacing(0)
	capped := boundedChild(20, 20, core.UnitSize{Width: core.Unbounded, Height: 60})
	open := boundedChild(20, 20, noMax())

	c := newDirContainer(core.DirLTR)
	for _, k := range []core.Trinket{capped, open} {
		c.AddChild(k)
		l.AddTrinketWithStretch(k, 1)
	}
	l.Layout(c, core.UnitRect{Width: 100, Height: 400})

	if got := capped.Bounds().Height; got != 60 {
		t.Errorf("a child with a maximum height of 60 came out %d tall", got)
	}
	if h := capped.Bounds().Height + open.Bounds().Height; h != 400 {
		t.Errorf("the two children come to %d of the 400 they share", h)
	}
}

// Where a maximum and a minimum conflict the minimum wins: a control squeezed
// under its minimum stops being usable, where one that overruns a maximum is
// merely bigger than asked.
func TestAMinimumBeatsAMaximum(t *testing.T) {
	child := boundedChild(20, 20, core.UnitSize{Width: 10, Height: core.Unbounded})
	child.SetMinimumSize(core.UnitSize{Width: 80, Height: 20})

	got := boxRun(400, child, boundedChild(20, 20, noMax()))
	if got[0].Width != 80 {
		t.Errorf("a minimum of 80 against a maximum of 10 gave %d, want the minimum", got[0].Width)
	}
}

// A maximum of zero is a real answer: it collapses the child while it keeps
// its place in the run.
func TestAMaximumOfZeroCollapsesAChild(t *testing.T) {
	collapsed := boundedChild(80, 20, core.UnitSize{Width: 0, Height: core.Unbounded})
	beside := boundedChild(20, 20, noMax())

	got := boxRun(400, collapsed, beside)
	if got[0].Width != 0 {
		t.Errorf("a child with a maximum of zero is %d wide", got[0].Width)
	}
	if got[1].Width != 400 {
		t.Errorf("the child beside it is %d wide, want the whole 400", got[1].Width)
	}
}

// In a grid a maximum stops a child filling its cell, and where it stops short
// it sits by its own alignment -- which is the same question a child that
// asked not to fill has always answered.
func TestAGridChildStopsFillingAtItsMaximum(t *testing.T) {
	// An inline child, because only one that states an alignment can show
	// where it lands -- so its own side-bearings come off the cell first, and
	// what it is placed in runs from one bearing to the other.
	m := core.DefaultCellMetrics()
	bearing := m.UnitsPerCellWidth
	room := core.Unit(400) - 2*bearing

	for _, c := range []struct {
		name  string
		align core.HAlign
		wantX core.Unit
	}{
		{"beginning", core.AlignOpticalLeft, bearing},
		{"middle", core.AlignCenter, bearing + (room-100)/2},
		{"end", core.AlignOpticalRight, bearing + room - 100},
	} {
		item := newAlignedTrinket(20, 16, core.Alignment{
			H: c.align, V: core.AlignMiddle, FillH: true, FillV: true,
		})
		item.SetMaximumSize(core.UnitSize{Width: 100, Height: core.Unbounded})

		container := newDirContainer(core.DirLTR)
		container.AddChild(item)
		l := NewGridLayout()
		l.SetSpacing(0)
		l.AddTrinketAt(item, 0, 0)
		l.SetColumnStretch(0, 1)
		l.Layout(container, core.UnitRect{Width: 400, Height: 100})

		got := item.Bounds()
		if got.Width != 100 {
			t.Errorf("%s: a filling child with a maximum of 100 is %d wide", c.name, got.Width)
		}
		if got.X != c.wantX {
			t.Errorf("%s: it sits at x=%d, want %d", c.name, got.X, c.wantX)
		}
	}
}

// And down the other axis of a cell: a maximum stops the fill, valign says
// where it sits in what is left, and a minimum still beats the maximum.
func TestAGridChildStopsFillingDownwardsToo(t *testing.T) {
	for _, c := range []struct {
		name  string
		align core.VAlign
		wantY core.Unit
	}{
		{"top", core.AlignTop, 0},
		{"middle", core.AlignMiddle, 150},
		{"bottom", core.AlignBottom, 300},
	} {
		item := newAlignedTrinket(20, 16, core.Alignment{
			H: core.AlignCenter, V: c.align, FillH: true, FillV: true,
		})
		item.SetMaximumSize(core.UnitSize{Width: core.Unbounded, Height: 100})

		container := newDirContainer(core.DirLTR)
		container.AddChild(item)
		l := NewGridLayout()
		l.SetSpacing(0)
		l.AddTrinketAt(item, 0, 0)
		l.SetRowStretch(0, 1)
		l.Layout(container, core.UnitRect{Width: 200, Height: 400})

		got := item.Bounds()
		if got.Height != 100 {
			t.Errorf("%s: a filling child with a maximum of 100 is %d tall", c.name, got.Height)
		}
		if got.Y != c.wantY {
			t.Errorf("%s: it sits at y=%d, want %d", c.name, got.Y, c.wantY)
		}
	}

	// A minimum taller than the maximum wins, and the child fills past it.
	item := newAlignedTrinket(20, 16, core.Alignment{
		H: core.AlignCenter, V: core.AlignTop, FillH: true, FillV: true,
	})
	item.SetMaximumSize(core.UnitSize{Width: core.Unbounded, Height: 40})
	item.SetMinimumSize(core.UnitSize{Width: 0, Height: 160})

	container := newDirContainer(core.DirLTR)
	container.AddChild(item)
	l := NewGridLayout()
	l.SetSpacing(0)
	l.AddTrinketAt(item, 0, 0)
	l.SetRowStretch(0, 1)
	l.Layout(container, core.UnitRect{Width: 200, Height: 400})

	if got := item.Bounds().Height; got != 160 {
		t.Errorf("a minimum of 160 against a maximum of 40 gave %d, want the minimum", got)
	}
}

// The same in a flex line, on both axes: growing stops at the maximum along
// the run, and stretching stops at it across.
func TestAFlexChildStopsAtItsMaximumOnEitherAxis(t *testing.T) {
	l := NewFlexLayout()
	l.SetSpacing(0)
	capped := boundedChild(20, 20, core.UnitSize{Width: 60, Height: 40})
	open := boundedChild(20, 20, noMax())
	l.AddTrinketWithFlex(capped, 1, 1, core.BasisAuto)
	l.AddTrinketWithFlex(open, 1, 1, core.BasisAuto)

	c := newDirContainer(core.DirLTR)
	c.AddChild(capped)
	c.AddChild(open)
	l.Layout(c, core.UnitRect{Width: 400, Height: 200})

	if got := capped.Bounds().Width; got != 60 {
		t.Errorf("along the run the capped child is %d wide, want its maximum of 60", got)
	}
	if got := capped.Bounds().Height; got != 40 {
		t.Errorf("across the line the capped child is %d tall, want its maximum of 40", got)
	}
	// The one beside it stretches the whole way, so the cap is the child's and
	// not the line's.
	if got := open.Bounds().Height; got != 200 {
		t.Errorf("the child beside it is %d tall, want the line's whole 200", got)
	}
	// And what the capped one turned down along the run went to the other.
	if w := capped.Bounds().Width + open.Bounds().Width; w != 400 {
		t.Errorf("the two children come to %d of the 400 they share", w)
	}
}
