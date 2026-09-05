package layout

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// flexChild is a trinket with a size of its own, a minimum, and the hints a
// flex layout reads.
type flexChild struct {
	core.TrinketBase
	own core.UnitSize
}

func newFlexChild(w, h core.Unit) *flexChild {
	c := &flexChild{own: core.UnitSize{Width: w, Height: h}}
	c.TrinketBase = *core.NewTrinketBase()
	c.Init(c)
	return c
}

func (c *flexChild) SizeHint() core.UnitSize { return c.own }

// flexBounds lays the children out in the given flex layout and returns where
// each landed, in the order they were added.
func flexBounds(l *FlexLayout, kids ...core.Trinket) []core.UnitRect {
	c := newDirContainer(core.DirLTR)
	for _, k := range kids {
		c.AddChild(k)
		l.AddTrinket(k)
	}
	l.Layout(c, core.UnitRect{Width: 300, Height: 100})
	out := make([]core.UnitRect, len(kids))
	for i, k := range kids {
		out[i] = k.Bounds()
	}
	return out
}

// A run that does not fit breaks into further lines, and the lines stack across
// the layout instead of overlapping.
//
// The wrap setting was stored and never read, so everything was laid out on one
// line at any setting and a run that overflowed simply ran off the end.
func TestAFlexRunWrapsWhenItOverflows(t *testing.T) {
	l := NewFlexLayout()
	l.SetWrap(FlexWrapNormal)
	l.SetSpacing(0)

	// Four 100-wide children in a 300-wide box: three fit, the fourth does not.
	kids := []core.Trinket{
		newFlexChild(100, 20), newFlexChild(100, 20),
		newFlexChild(100, 20), newFlexChild(100, 20),
	}
	got := flexBounds(l, kids...)

	for i := 0; i < 3; i++ {
		if got[i].Y != 0 {
			t.Errorf("child %d is at y=%d, want the first line at 0", i, got[i].Y)
		}
	}
	if got[3].Y != got[0].Height {
		t.Errorf("the fourth child is at y=%d, want the second line at %d", got[3].Y, got[0].Height)
	}
	if got[3].X != 0 {
		t.Errorf("the fourth child is at x=%d, want the start of its own line", got[3].X)
	}

	// And without wrapping it is one line, however far it overflows.
	l = NewFlexLayout()
	l.SetSpacing(0)
	got = flexBounds(l, newFlexChild(100, 20), newFlexChild(100, 20),
		newFlexChild(100, 20), newFlexChild(100, 20))
	for i, r := range got {
		if r.Y != 0 {
			t.Errorf("no wrapping: child %d is at y=%d, want one line", i, r.Y)
		}
	}
}

// Every line holds at least one child, however small the box: a child that fits
// nowhere still has to go somewhere.
func TestAFlexLineIsNeverEmpty(t *testing.T) {
	l := NewFlexLayout()
	l.SetWrap(FlexWrapNormal)
	// A spacing, so a line opened before the first child would show as an
	// offset rather than costing nothing.
	l.SetSpacing(4)

	got := flexBounds(l, newFlexChild(400, 20), newFlexChild(400, 20))
	if got[0].Y != 0 {
		t.Errorf("the first child is at y=%d, want 0; a line was opened before it", got[0].Y)
	}
	if got[0].Y == got[1].Y {
		t.Fatalf("two oversized children share line y=%d; each should have its own", got[0].Y)
	}
	if got[0].X != 0 || got[1].X != 0 {
		t.Errorf("oversized children start at x=%d and x=%d, want both at 0", got[0].X, got[1].X)
	}
}

// Shrinking stops at a child's own minimum. Squeezing past it is what a minimum
// is there to prevent, and the shrink pass floored at zero instead -- so a
// child with a shrink factor could be reduced to nothing.
func TestFlexShrinkingStopsAtTheMinimum(t *testing.T) {
	l := NewFlexLayout()
	l.SetSpacing(0)

	// 600 of children in a 300 box, shrinking equally, is 150 off each -- which
	// would put the wide one at 250, below the 350 it says it needs. The floor
	// has to bind here or the test would pass without one.
	wide := newFlexChild(400, 20)
	wide.SetMinimumSize(core.UnitSize{Width: 350, Height: 20})
	other := newFlexChild(200, 20)

	got := flexBounds(l, wide, other)
	if got[0].Width < 350 {
		t.Errorf("a child with a minimum of 350 was shrunk to %d", got[0].Width)
	}

	// A child with no minimum is still shrunk, so the floor is the minimum and
	// not a refusal to shrink at all.
	l = NewFlexLayout()
	l.SetSpacing(0)
	got = flexBounds(l, newFlexChild(400, 20), newFlexChild(200, 20))
	if got[0].Width >= 400 {
		t.Errorf("a child with no minimum kept its whole %d; shrinking did not happen", got[0].Width)
	}
}

// A flex child is placed across its line by the same alignment every other
// manager reads, and the container's align_items is what a child that states
// none falls back to.
func TestAFlexChildIsPlacedByItsOwnAlignment(t *testing.T) {
	l := NewFlexLayout()
	l.SetAlignItems(FlexAlignStretch)

	stretched := newFlexChild(100, 20)
	top := newFlexChild(100, 20)
	top.SetLayoutAlignment(core.Alignment{H: core.AlignCenter, V: core.AlignTop})
	bottom := newFlexChild(100, 20)
	bottom.SetLayoutAlignment(core.Alignment{H: core.AlignCenter, V: core.AlignBottom})

	got := flexBounds(l, stretched, top, bottom)

	if got[0].Height != 100 {
		t.Errorf("a child stating no alignment is %d tall, want the container's stretch to 100", got[0].Height)
	}
	if got[1].Y != 0 || got[1].Height != 20 {
		t.Errorf("valign=top sits at y=%d h=%d, want y=0 h=20", got[1].Y, got[1].Height)
	}
	if got[2].Y != 80 || got[2].Height != 20 {
		t.Errorf("valign=bottom sits at y=%d h=%d, want y=80 h=20", got[2].Y, got[2].Height)
	}
}

// Grow shares out what is left over, in proportion.
func TestFlexGrowSharesTheLeftover(t *testing.T) {
	l := NewFlexLayout()
	l.SetSpacing(0)

	one := newFlexChild(50, 20)
	two := newFlexChild(50, 20)
	l.AddTrinketWithFlex(one, 1, 1, 0)
	l.AddTrinketWithFlex(two, 3, 1, 0)

	c := newDirContainer(core.DirLTR)
	c.AddChild(one)
	c.AddChild(two)
	l.Layout(c, core.UnitRect{Width: 300, Height: 100})

	// 200 left over, split one part to three.
	if one.Bounds().Width != 100 {
		t.Errorf("grow=1 got %d, want 50 plus a quarter of 200", one.Bounds().Width)
	}
	if two.Bounds().Width != 200 {
		t.Errorf("grow=3 got %d, want 50 plus three quarters of 200", two.Bounds().Width)
	}
}
