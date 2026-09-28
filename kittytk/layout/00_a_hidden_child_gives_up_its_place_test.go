package layout

// A hidden child is absent from the arrangement: it takes no room, the spacing
// either side of it goes with it, and it adds nothing to what the layout asks
// for. So a layout holding a hidden child comes out exactly as the same layout
// would without that child at all -- which is what each test here compares.

import (
	"fmt"
	"testing"

	"github.com/phroun/kittytk/core"
)

// hiddenCase builds the same children twice: once with the middle one hidden,
// and once without it. Blocks and inline children are mixed, so the spacing a
// box opens between two blocks and the air around an inline child both have to
// come out right when the child between them leaves.
func hiddenCase() (all, without []core.Trinket, hidden core.Trinket) {
	mk := func() []core.Trinket {
		return []core.Trinket{
			newBandChild(40, 32),
			newSizedTrinket(24, 16),
			newBandChild(56, 48),
			newSizedTrinket(32, 16),
			newBandChild(16, 16),
		}
	}
	all = mk()
	full := mk()
	hidden = all[2]
	// A minimum larger than anything shown, so a box that still counted the
	// hidden child's minimum would say so.
	hidden.SetMinimumSize(core.UnitSize{Width: 200, Height: 200})
	hidden.SetVisible(false)
	without = append(append([]core.Trinket{}, full[:2]...), full[3:]...)
	return all, without, hidden
}

// same reports where the shown children of all landed against where the
// children of without did, index for index once the hidden one is skipped.
func same(t *testing.T, what string, all, without []core.Trinket, hidden core.Trinket) {
	t.Helper()
	j := 0
	for _, k := range all {
		if k == hidden {
			continue
		}
		if got, want := k.Bounds(), without[j].Bounds(); got != want {
			t.Errorf("%s: child %d landed at %+v with a hidden sibling, %+v without it",
				what, j, got, want)
		}
		j++
	}
}

func TestAHiddenChildGivesUpItsPlaceInABox(t *testing.T) {
	for _, o := range []core.Orientation{core.Horizontal, core.Vertical} {
		all, without, hidden := hiddenCase()
		build := func(kids []core.Trinket) (*BoxLayout, *dirContainer) {
			l := NewBoxLayout(o)
			l.SetSpacing(16)
			c := newDirContainer(core.DirLTR)
			for _, k := range kids {
				c.AddChild(k)
				l.AddTrinket(k)
			}
			return l, c
		}
		la, ca := build(all)
		lw, cw := build(without)
		room := core.UnitRect{Width: 400, Height: 400}
		la.Layout(ca, room)
		lw.Layout(cw, room)
		same(t, fmt.Sprintf("%v box", o), all, without, hidden)

		if got, want := la.SizeHint(ca), lw.SizeHint(cw); got != want {
			t.Errorf("%v box: asks for %+v with a hidden child, %+v without it", o, got, want)
		}
		if got, want := la.MinimumSize(ca), lw.MinimumSize(cw); got != want {
			t.Errorf("%v box: minimum %+v with a hidden child, %+v without it", o, got, want)
		}
		if got, want := la.HeightForWidth(400), lw.HeightForWidth(400); got != want {
			t.Errorf("%v box: %d tall at 400 with a hidden child, %d without it", o, got, want)
		}
	}
}

// A hidden child at either end takes the air it opened there with it: an
// inline child first in a row opens a column before itself.
func TestAHiddenChildAtTheEndOfARowTakesItsAirWithIt(t *testing.T) {
	lead := newSizedTrinket(24, 16)
	block := newBandChild(40, 16)
	lead.SetVisible(false)

	l := NewHBoxLayout()
	l.SetSpacing(0)
	c := newDirContainer(core.DirLTR)
	for _, k := range []core.Trinket{lead, block} {
		c.AddChild(k)
		l.AddTrinket(k)
	}
	l.Layout(c, core.UnitRect{Width: 400, Height: 16})
	if x := block.Bounds().X; x != 0 {
		t.Errorf("the block after a hidden inline child starts at %d, want 0", x)
	}
	if w := l.SizeHint(c).Width; w != 40 {
		t.Errorf("a row of a block and a hidden inline child asks for %d, want 40", w)
	}
}

// A hidden child is left where it was: nothing draws it while it is hidden,
// and moving it would only be undone when it is shown.
func TestAHiddenChildKeepsTheBoundsItHad(t *testing.T) {
	a, b := newBandChild(40, 16), newBandChild(40, 16)
	l := NewHBoxLayout()
	c := newDirContainer(core.DirLTR)
	for _, k := range []core.Trinket{a, b} {
		c.AddChild(k)
		l.AddTrinket(k)
	}
	l.Layout(c, core.UnitRect{Width: 400, Height: 16})
	was := b.Bounds()
	b.SetVisible(false)
	l.Layout(c, core.UnitRect{Width: 200, Height: 16})
	if b.Bounds() != was {
		t.Errorf("a hidden child was moved from %+v to %+v", was, b.Bounds())
	}
}

// Every child hidden is an empty box, not one sized by children nobody sees.
func TestABoxWithEveryChildHiddenAsksForNothing(t *testing.T) {
	for _, o := range []core.Orientation{core.Horizontal, core.Vertical} {
		l := NewBoxLayout(o)
		l.SetSpacing(16)
		l.SetContentsMargins(core.UnitMargins{Left: 8, Right: 8, Top: 16, Bottom: 16})
		c := newDirContainer(core.DirLTR)
		for _, k := range []core.Trinket{newBandChild(40, 16), newSizedTrinket(24, 16)} {
			k.SetVisible(false)
			c.AddChild(k)
			l.AddTrinket(k)
		}
		want := core.UnitSize{Width: 16, Height: 32}
		if got := l.SizeHint(c); got != want {
			t.Errorf("%v box: asks for %+v with nothing shown, want its margins %+v", o, got, want)
		}
		if got := l.MinimumSize(c); got != want {
			t.Errorf("%v box: minimum %+v with nothing shown, want its margins %+v", o, got, want)
		}
		if l.HasHeightForWidth() {
			t.Errorf("%v box: claims a height for width with nothing shown", o)
		}
	}
}

func TestAHiddenChildGivesUpItsPlaceInAFlex(t *testing.T) {
	for _, wrap := range []FlexWrap{FlexNoWrap, FlexWrapNormal} {
		for _, dir := range []FlexDirection{FlexRow, FlexColumn, FlexRowReverse} {
			all, without, hidden := hiddenCase()
			build := func(kids []core.Trinket) (*FlexLayout, *dirContainer) {
				l := NewFlexLayout()
				l.SetDirection(dir)
				l.SetWrap(wrap)
				l.SetSpacing(8)
				c := newDirContainer(core.DirLTR)
				for _, k := range kids {
					c.AddChild(k)
					l.AddTrinket(k)
				}
				return l, c
			}
			la, ca := build(all)
			lw, cw := build(without)
			// Narrow enough that a wrapping row breaks into lines, so where the
			// hidden child would have broken one matters.
			room := core.UnitRect{Width: 160, Height: 400}
			la.Layout(ca, room)
			lw.Layout(cw, room)
			same(t, "flex", all, without, hidden)

			if got, want := la.SizeHint(ca), lw.SizeHint(cw); got != want {
				t.Errorf("flex %v/%v: asks for %+v with a hidden child, %+v without it", dir, wrap, got, want)
			}
			if got, want := la.MinimumSize(ca), lw.MinimumSize(cw); got != want {
				t.Errorf("flex %v/%v: minimum %+v with a hidden child, %+v without it", dir, wrap, got, want)
			}
			if got, want := la.HeightForWidth(160), lw.HeightForWidth(160); got != want {
				t.Errorf("flex %v/%v: %d tall at 160 with a hidden child, %d without it", dir, wrap, got, want)
			}
		}
	}
}

// In a grid a hidden child gives up its cell: it raises no column and no row,
// makes no claim across a span, and adds no track only it was in.
func TestAHiddenChildGivesUpItsCellInAGrid(t *testing.T) {
	shown := func() []core.Trinket {
		return []core.Trinket{
			placedBlock(40, 16, core.GridPlacement{Row: 0, Column: 0}),
			placedBlock(40, 16, core.GridPlacement{Row: 0, Column: 1}),
			placedBlock(40, 16, core.GridPlacement{Row: 1, Column: 0}),
		}
	}
	inline := newSizedTrinket(24, 16)
	inline.SetLayoutGridPlacement(core.GridPlacement{Row: 1, Column: 1, RowSpan: 1, ColumnSpan: 1})
	hidden := []core.Trinket{
		// Taller than its row, and wider than its column.
		placedBlock(10, 64, core.GridPlacement{Row: 0, Column: 0}),
		placedBlock(120, 16, core.GridPlacement{Row: 1, Column: 0}),
		// Spanning both rows, taller than both together.
		placedBlock(40, 200, core.GridPlacement{Row: 0, Column: 1, RowSpan: 2}),
		// Spanning both columns, wider than both together, in a row of its own.
		placedBlock(300, 16, core.GridPlacement{Row: 2, Column: 0, ColumnSpan: 2}),
		// Alone in a column of its own.
		placedBlock(40, 16, core.GridPlacement{Row: 0, Column: 2}),
		// Inline beside a block, which would close the boundary between them up.
		inline,
	}
	build := func(kids []core.Trinket) (*GridLayout, *dirContainer) {
		l := NewGridLayout()
		l.SetSpacing(8)
		c := newDirContainer(core.DirLTR)
		for _, k := range kids {
			c.AddChild(k)
			l.AddTrinket(k)
		}
		return l, c
	}
	with := shown()
	for _, k := range hidden {
		k.SetVisible(false)
	}
	l, c := build(append(append([]core.Trinket{}, with...), hidden...))
	bareKids := shown()
	bare, bc := build(bareKids)
	room := core.UnitRect{Width: 400, Height: 100}
	l.Layout(c, room)
	bare.Layout(bc, room)

	for i := range with {
		if got, want := with[i].Bounds(), bareKids[i].Bounds(); got != want {
			t.Errorf("child %d landed at %+v beside hidden children, %+v without them", i, got, want)
		}
	}
	for i, k := range hidden {
		if b := k.Bounds(); b != (core.UnitRect{}) {
			t.Errorf("hidden child %d was placed at %+v; it should be left where it was", i, b)
		}
	}
	if got, want := l.SizeHint(c), bare.SizeHint(bc); got != want {
		t.Errorf("the grid asks for %+v with hidden children, %+v without them", got, want)
	}
	if got, want := l.MinimumSize(c), bare.MinimumSize(bc); got != want {
		t.Errorf("the grid's minimum is %+v with hidden children, %+v without them", got, want)
	}
	if l.RowCount() != 2 || l.ColumnCount() != 2 {
		t.Errorf("the grid counts %d rows and %d columns, want the 2 and 2 its shown children use",
			l.RowCount(), l.ColumnCount())
	}
}

// A hidden child reserves no decoration room: a line whose only shadow belongs
// to a hidden button sets nothing aside for it.
func TestAHiddenShadowSetsNothingAside(t *testing.T) {
	button := newShadowedTrinket(60, 16, core.UnitMargins{Right: 8, Bottom: 16})
	button.SetVisible(false)
	l := NewHBoxLayout()
	l.AddTrinket(button)
	l.AddTrinket(newSizedTrinket(24, 16))
	if allow := l.styleAllowance(); allow != (core.UnitMargins{}) {
		t.Errorf("a hidden button's shadow set aside %+v", allow)
	}
}

// wrapper is a child whose height depends on its width.
type wrapper struct{ sizedTrinket }

func (w *wrapper) HasHeightForWidth() bool                  { return true }
func (w *wrapper) HeightForWidth(width core.Unit) core.Unit { return 16 * (1 + 400/(width+1)) }

func newWrapper() *wrapper {
	w := &wrapper{sizedTrinket{own: core.UnitSize{Width: 80, Height: 16}}}
	w.TrinketBase = *core.NewTrinketBase()
	w.Init(w)
	return w
}

// A layout whose only width-dependent child is hidden has no height for width
// to report, and one with nothing shown has none either.
func TestAHiddenWrapperGivesTheLayoutNoHeightForWidth(t *testing.T) {
	w := newWrapper()
	w.SetVisible(false)
	box := NewVBoxLayout()
	box.AddTrinket(newSizedTrinket(24, 16))
	box.AddTrinket(w)
	if box.HasHeightForWidth() {
		t.Errorf("a box whose only wrapping child is hidden claims a height for width")
	}

	flex := NewFlexLayout()
	flex.SetWrap(FlexWrapNormal)
	k := newSizedTrinket(24, 16)
	k.SetVisible(false)
	flex.AddTrinket(k)
	if flex.HasHeightForWidth() {
		t.Errorf("a wrapping flex with nothing shown claims a height for width")
	}
}

// The spacers a box adds for itself are shown: a stretch pushes what follows
// it to the far end, and fixed spacing opens a gap.
func TestABoxsOwnSpacersAreShown(t *testing.T) {
	a, b := newBandChild(40, 16), newBandChild(40, 16)
	l := NewHBoxLayout()
	l.SetSpacing(0)
	l.AddTrinket(a)
	l.AddStretch(1)
	l.AddTrinket(b)
	l.Layout(newDirContainer(core.DirLTR), core.UnitRect{Width: 400, Height: 16})
	if x := b.Bounds().X; x != 360 {
		t.Errorf("after a stretch the last child starts at %d, want 360", x)
	}

	c, d := newBandChild(40, 16), newBandChild(40, 16)
	l = NewHBoxLayout()
	l.SetSpacing(0)
	l.AddTrinket(c)
	l.AddSpacing(24)
	l.AddTrinket(d)
	l.Layout(newDirContainer(core.DirLTR), core.UnitRect{Width: 400, Height: 16})
	// At least the spacing: the box reads a spacer as inline and opens air
	// around it too, which is its own rule and not this test's.
	if x := d.Bounds().X; x < 64 {
		t.Errorf("after 24 units of spacing the last child starts at %d, want at least 64", x)
	}
}
