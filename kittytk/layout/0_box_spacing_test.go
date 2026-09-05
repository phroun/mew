package layout

import (
	"fmt"
	"testing"

	"github.com/phroun/kittytk/core"
)

// sizedChild is a trinket with a size of its own. It is NOT a container, so it
// is what the toolkit calls an inline trinket.
type sizedChild struct {
	core.TrinketBase
	own core.UnitSize
}

func newSizedChild(w, h core.Unit) *sizedChild {
	c := &sizedChild{own: core.UnitSize{Width: w, Height: h}}
	c.TrinketBase = *core.NewTrinketBase()
	c.Init(c)
	return c
}

func (c *sizedChild) SizeHint() core.UnitSize { return c.own }

// blockChild is a container of a given size, which is what the toolkit calls a
// block. Spacing must treat it exactly as it treats an inline trinket.
type blockChild struct {
	sizedChild
}

func newBlockChild(w, h core.Unit) *blockChild {
	c := &blockChild{}
	c.own = core.UnitSize{Width: w, Height: h}
	c.TrinketBase = *core.NewTrinketBase()
	c.Init(c)
	return c
}

func (c *blockChild) Children() []core.Trinket            { return nil }
func (c *blockChild) AddChild(core.Trinket)               {}
func (c *blockChild) RemoveChild(core.Trinket)            {}
func (c *blockChild) ChildAt(core.UnitPoint) core.Trinket { return nil }
func (c *blockChild) Layout()                             {}
func (c *blockChild) LayoutManager() core.LayoutManager   { return nil }
func (c *blockChild) SetLayoutManager(core.LayoutManager) {}

// layOut runs a box at exactly the size it asks for -- what a parent placing it
// at its hint does -- and returns the hint and where each child landed.
func layOut(o core.Orientation, spacing core.Unit, kids ...core.Trinket) (core.UnitSize, []core.UnitRect) {
	l := NewBoxLayout(o)
	l.SetSpacing(spacing)
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
	return hint, out
}

// The gap between neighbours is the spacing asked for, and there is no gap at
// either end.
//
// A gap separates two things, and at either end of a run there is nothing to
// separate from: keeping the contents off a container's edge is the
// container's own business. A box opened a column at each end around inline
// trinkets, which no spacing could spell away, and which put a right-aligned
// row a column short of everything it was meant to line up with.
func TestABoxSpacesBetweenItsItemsOnly(t *testing.T) {
	const w, h = core.Unit(56), core.Unit(32)

	for _, spacing := range []core.Unit{0, 8, 24, 32} {
		t.Run(fmt.Sprintf("spacing=%d", spacing), func(t *testing.T) {
			kids := []core.Trinket{newSizedChild(w, h), newSizedChild(72, h), newSizedChild(40, h)}
			widths := []core.Unit{w, 72, 40}

			hint, got := layOut(core.Horizontal, spacing, kids...)

			// Nothing before the first.
			if got[0].X != 0 {
				t.Errorf("the first child starts at %d, want 0", got[0].X)
			}
			// Exactly the spacing between each pair.
			for i := 1; i < len(got); i++ {
				gap := got[i].X - (got[i-1].X + got[i-1].Width)
				if gap != spacing {
					t.Errorf("the gap before child %d is %d, want %d", i, gap, spacing)
				}
			}
			// Nothing after the last: the box ends where its last child does.
			right := got[len(got)-1].X + got[len(got)-1].Width
			if right != hint.Width {
				t.Errorf("the box asks for %d and its last child ends at %d", hint.Width, right)
			}
			// And the size it asks for is the children plus the gaps between.
			want := widths[0] + widths[1] + widths[2] + 2*spacing
			if hint.Width != want {
				t.Errorf("the box asks for %d, want %d (children plus two gaps of %d)",
					hint.Width, want, spacing)
			}
			// Every child at its own size, so no gap was taken out of one.
			for i, r := range got {
				if r.Width != widths[i] {
					t.Errorf("child %d is %d wide, want its own %d", i, r.Width, widths[i])
				}
			}
		})
	}
}

// The gaps are taken out of the room BEFORE it is shared among the items that
// stretch, so a stretching neighbour grows into what is left and not into the
// gap. Sharing out the whole width instead only shows when something stretches:
// with nothing to stretch, every item is at its own size either way.
func TestStretchingDoesNotEatTheGaps(t *testing.T) {
	const spacing = core.Unit(24)

	l := NewHBoxLayout()
	l.SetSpacing(spacing)
	fixed, growing := newSizedChild(56, 32), newSizedChild(72, 32)
	l.AddTrinket(fixed)
	l.AddTrinketWithStretch(growing, 1)

	c := newDirContainer(core.DirLTR)
	c.AddChild(fixed)
	c.AddChild(growing)

	const width = core.Unit(400)
	l.Layout(c, core.UnitRect{Width: width, Height: 32})

	if gap := growing.Bounds().X - (fixed.Bounds().X + fixed.Bounds().Width); gap != spacing {
		t.Errorf("the gap beside a stretching child is %d, want %d", gap, spacing)
	}
	if got := fixed.Bounds().Width; got != 56 {
		t.Errorf("the fixed child is %d wide, want its own 56", got)
	}
	if want := width - 56 - spacing; growing.Bounds().Width != want {
		t.Errorf("the stretching child is %d wide, want %d -- the room left after the gap",
			growing.Bounds().Width, want)
	}
	right := growing.Bounds().X + growing.Bounds().Width
	if right != width {
		t.Errorf("the row ends at %d, want the %d it was given", right, width)
	}
}

// Zero means zero: neighbours touch, with nothing between them.
func TestSpacingOfZeroButtsNeighboursTogether(t *testing.T) {
	a, b := newSizedChild(56, 32), newSizedChild(72, 32)
	hint, got := layOut(core.Horizontal, 0, a, b)

	if got[0].X != 0 {
		t.Errorf("the first child starts at %d, want 0", got[0].X)
	}
	if got[1].X != got[0].Width {
		t.Errorf("the second child starts at %d, want the first's right edge %d", got[1].X, got[0].Width)
	}
	if want := core.Unit(56 + 72); hint.Width != want {
		t.Errorf("the box asks for %d, want %d with nothing between", hint.Width, want)
	}
}

// Blocks and inline trinkets are spaced alike. A box used to substitute a fixed
// column wherever either neighbour was inline, so a row of controls ignored the
// spacing written on it -- ask for 24 and get 8.
func TestSpacingIsTheSameForBlocksAndInlineTrinkets(t *testing.T) {
	const spacing = core.Unit(24)

	_, inline := layOut(core.Horizontal, spacing, newSizedChild(56, 32), newSizedChild(72, 32))
	_, blocks := layOut(core.Horizontal, spacing, newBlockChild(56, 32), newBlockChild(72, 32))
	_, mixed := layOut(core.Horizontal, spacing, newSizedChild(56, 32), newBlockChild(72, 32))

	for name, got := range map[string][]core.UnitRect{"inline": inline, "blocks": blocks, "mixed": mixed} {
		gap := got[1].X - (got[0].X + got[0].Width)
		if gap != spacing {
			t.Errorf("%s: the gap is %d, want the %d asked for", name, gap, spacing)
		}
	}
}

// A vertical box spaces the same way down.
func TestAVerticalBoxSpacesBetweenItsItemsOnly(t *testing.T) {
	for _, spacing := range []core.Unit{0, 16, 48} {
		t.Run(fmt.Sprintf("spacing=%d", spacing), func(t *testing.T) {
			kids := []core.Trinket{newSizedChild(56, 32), newSizedChild(56, 16)}
			hint, got := layOut(core.Vertical, spacing, kids...)

			if got[0].Y != 0 {
				t.Errorf("the first child starts at y=%d, want 0", got[0].Y)
			}
			if gap := got[1].Y - (got[0].Y + got[0].Height); gap != spacing {
				t.Errorf("the gap between the two is %d, want %d", gap, spacing)
			}
			if bottom := got[1].Y + got[1].Height; bottom != hint.Height {
				t.Errorf("the box asks for %d and its last child ends at %d", hint.Height, bottom)
			}
			if want := core.Unit(32 + 16 + spacing); hint.Height != want {
				t.Errorf("the box asks for %d, want %d", hint.Height, want)
			}
		})
	}
}

// One item has no gaps at all, whatever the spacing says: there is nothing to
// space it from.
func TestOneItemHasNoSpacing(t *testing.T) {
	only := newSizedChild(56, 32)
	hint, got := layOut(core.Horizontal, 24, only)

	if got[0].X != 0 || got[0].Width != 56 {
		t.Errorf("the only child is at x=%d w=%d, want x=0 w=56", got[0].X, got[0].Width)
	}
	if hint.Width != 56 {
		t.Errorf("a box of one child asks for %d, want its child's 56", hint.Width)
	}
}

// Spacing is counted in whole cells: a box on a character grid cannot open half
// a column, so a spacing under one cell rounds down to none.
func TestSpacingIsCountedInWholeCells(t *testing.T) {
	a, b := newSizedChild(56, 32), newSizedChild(72, 32)
	hint, got := layOut(core.Horizontal, 4, a, b)

	if gap := got[1].X - (got[0].X + got[0].Width); gap != 0 {
		t.Errorf("half a column of spacing opened %d, want 0", gap)
	}
	if want := core.Unit(56 + 72); hint.Width != want {
		t.Errorf("the box asks for %d, want %d", hint.Width, want)
	}
}
