package layout

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// dirContainer is a container that is a Trinket, so a layout can read a
// direction off it.
type dirContainer struct {
	core.TrinketBase
	kids []core.Trinket
}

func newDirContainer(d core.Direction) *dirContainer {
	c := &dirContainer{}
	c.TrinketBase = *core.NewTrinketBase()
	c.Init(c)
	c.SetDirection(d)
	return c
}

func (c *dirContainer) Children() []core.Trinket            { return c.kids }
func (c *dirContainer) AddChild(w core.Trinket)             { c.kids = append(c.kids, w); w.SetParent(c) }
func (c *dirContainer) RemoveChild(core.Trinket)            {}
func (c *dirContainer) ChildAt(core.UnitPoint) core.Trinket { return nil }
func (c *dirContainer) Layout()                             {}
func (c *dirContainer) LayoutManager() core.LayoutManager   { return nil }
func (c *dirContainer) SetLayoutManager(core.LayoutManager) {}

// alignedTrinket asks for a placement and, optionally, reports which way its
// own text runs.
type alignedTrinket struct {
	core.TrinketBase
	own     core.UnitSize
	align   core.Alignment
	textDir core.Direction
	speaks  bool
}

func newAlignedTrinket(w, h core.Unit, a core.Alignment) *alignedTrinket {
	x := &alignedTrinket{own: core.UnitSize{Width: w, Height: h}, align: a}
	x.TrinketBase = *core.NewTrinketBase()
	x.Init(x)
	return x
}

func (x *alignedTrinket) SizeHint() core.UnitSize { return x.own }
func (x *alignedTrinket) LayoutAlignment() (core.Alignment, bool) {
	return x.align, true
}
func (x *alignedTrinket) TextDirection() (core.Direction, bool) {
	return x.textDir, x.speaks
}

// placeInVBox lays one item out in a vertical box inside the given container
// and returns where it landed. The cross axis is horizontal, which is the one
// alignment speaks for.
func placeInVBox(c *dirContainer, item core.Trinket) core.UnitRect {
	c.AddChild(item)
	l := NewBoxLayout(core.Vertical)
	l.SetSpacing(0)
	l.AddTrinket(item)
	l.Layout(c, core.UnitRect{Width: 400, Height: 400})
	return item.Bounds()
}

// layoutbegin follows the room the item sits in: the same item, asking for the
// same thing, lands on opposite sides of a left-to-right and a right-to-left
// form.
func TestLayoutBeginFollowsTheContainer(t *testing.T) {
	const w = core.Unit(24)
	a := core.Alignment{H: core.AlignLayoutBegin, V: core.AlignMiddle}

	got := placeInVBox(newDirContainer(core.DirLTR), newAlignedTrinket(w, 16, a))
	if got.X != 0 || got.Width != w {
		t.Errorf("left-to-right: item at x=%d w=%d, want x=0 w=%d", got.X, got.Width, w)
	}

	got = placeInVBox(newDirContainer(core.DirRTL), newAlignedTrinket(w, 16, a))
	if got.X != 400-w || got.Width != w {
		t.Errorf("right-to-left: item at x=%d w=%d, want x=%d w=%d", got.X, got.Width, 400-w, w)
	}
}

// textbegin follows the item's OWN text, so an English caption keeps its left
// edge in a right-to-left form while everything around it begins on the right.
func TestTextBeginFollowsTheItemsOwnText(t *testing.T) {
	const w = core.Unit(24)
	a := core.Alignment{H: core.AlignTextBegin, V: core.AlignMiddle}

	english := newAlignedTrinket(w, 16, a)
	english.textDir, english.speaks = core.DirLTR, true
	got := placeInVBox(newDirContainer(core.DirRTL), english)
	if got.X != 0 {
		t.Errorf("an item whose text runs left to right sits at x=%d, want 0", got.X)
	}

	hebrew := newAlignedTrinket(w, 16, a)
	hebrew.textDir, hebrew.speaks = core.DirRTL, true
	got = placeInVBox(newDirContainer(core.DirLTR), hebrew)
	if got.X != 400-w {
		t.Errorf("an item whose text runs right to left sits at x=%d, want %d", got.X, 400-w)
	}

	// An item with nothing to say about its text takes the room's direction,
	// which is what puts an unmarked number where everything else begins.
	quiet := newAlignedTrinket(w, 16, a)
	got = placeInVBox(newDirContainer(core.DirRTL), quiet)
	if got.X != 400-w {
		t.Errorf("an item with no text direction sits at x=%d, want the room's %d", got.X, 400-w)
	}
}

// The optical pair is the escape hatch and no direction moves it.
func TestOpticalAlignmentDoesNotTurnOver(t *testing.T) {
	const w = core.Unit(24)
	for _, c := range []struct {
		align core.HAlign
		dir   core.Direction
		wantX core.Unit
	}{
		{core.AlignOpticalLeft, core.DirLTR, 0},
		{core.AlignOpticalLeft, core.DirRTL, 0},
		{core.AlignOpticalRight, core.DirLTR, 400 - w},
		{core.AlignOpticalRight, core.DirRTL, 400 - w},
	} {
		item := newAlignedTrinket(w, 16, core.Alignment{H: c.align, V: core.AlignMiddle})
		item.textDir, item.speaks = core.DirRTL, true
		got := placeInVBox(newDirContainer(c.dir), item)
		if got.X != c.wantX {
			t.Errorf("%v in a %v form sits at x=%d, want %d", c.align, c.dir, got.X, c.wantX)
		}
	}
}

// Filling is per axis: an item can take the whole of its grid column and still
// sit at the top of its row, which one value covering both axes could not say.
func TestAGridItemFillsOneAxisAndSitsOnTheOther(t *testing.T) {
	item := newAlignedTrinket(24, 16, core.Alignment{
		H: core.AlignCenter, V: core.AlignTop, FillH: true, FillV: false,
	})
	c := newDirContainer(core.DirLTR)
	c.AddChild(item)

	l := NewGridLayout()
	l.SetSpacing(0)
	l.AddTrinketAt(item, 0, 0)
	// A stretching row and column, so the cell is bigger than the item and
	// there is something for one axis to fill and the other to sit in.
	l.SetColumnStretch(0, 1)
	l.SetRowStretch(0, 1)
	l.Layout(c, core.UnitRect{Width: 400, Height: 400})

	got := item.Bounds()
	// The whole column less the item's own side-bearings, which come off the
	// cell before anything is placed in it.
	m := core.FindEffectiveCellMetrics(c.Self())
	if want := core.Unit(400) - 2*m.UnitsPerCellWidth; got.Width != want {
		t.Errorf("the item is %d wide, want %d -- the column less its bearings", got.Width, want)
	}
	if got.Y != 0 || got.Height != 16 {
		t.Errorf("the item is at y=%d h=%d, want y=0 h=16 -- it asked to sit at the top, not to fill", got.Y, got.Height)
	}
}
