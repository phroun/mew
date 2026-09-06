package layout

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// cellContainer is a container on a CELL surface: it answers no to smooth
// positioning, which is what every terminal surface answers.
type cellContainer struct {
	core.TrinketBase
	kids []core.Trinket
}

func newCellContainer() *cellContainer {
	c := &cellContainer{}
	c.TrinketBase = *core.NewTrinketBase()
	c.Init(c)
	return c
}

func (c *cellContainer) SmoothWindowPositioning() bool       { return false }
func (c *cellContainer) Children() []core.Trinket            { return c.kids }
func (c *cellContainer) AddChild(w core.Trinket)             { c.kids = append(c.kids, w); w.SetParent(c) }
func (c *cellContainer) RemoveChild(core.Trinket)            {}
func (c *cellContainer) ChildAt(core.UnitPoint) core.Trinket { return nil }
func (c *cellContainer) Layout()                             {}
func (c *cellContainer) LayoutManager() core.LayoutManager   { return nil }
func (c *cellContainer) SetLayoutManager(core.LayoutManager) {}

// sized is a child with a size of its own that declines to fill, so where the
// layout put it is what its bounds report.
type sized struct {
	core.TrinketBase
	own core.UnitSize
}

func newSized(w, h core.Unit) *sized {
	s := &sized{own: core.UnitSize{Width: w, Height: h}}
	s.TrinketBase = *core.NewTrinketBase()
	s.Init(s)
	return s
}

func (s *sized) SizeHint() core.UnitSize { return s.own }

// block is a child that is a container, which is what makes it a block: no
// side-bearings, so the boundaries beside it are the configured spacing rather
// than the column of air an inline child brings with it.
type block struct {
	cellContainer
	own core.UnitSize
}

func newBlock(w, h core.Unit) *block {
	b := &block{own: core.UnitSize{Width: w, Height: h}}
	b.TrinketBase = *core.NewTrinketBase()
	b.Init(b)
	return b
}

func (b *block) SizeHint() core.UnitSize { return b.own }

// On a cell surface every child lands on the cell grid, on both axes.
//
// Drawing rounds and hit-testing does not, so a child standing between cells
// draws in one and answers the mouse in another -- a button whose clicks are a
// column out. Stretch is where it comes from: dividing what is left among
// tracks lands their boundaries wherever the arithmetic falls, and everything
// after a track that is not a whole number of cells is off the grid too.
func TestAGridPlacesEveryChildOnTheCellGrid(t *testing.T) {
	m := core.DefaultCellMetrics()
	// Four stretch bands in a room that does not divide evenly among them --
	// 624 units over four is 156, which is 19.5 cells.
	g := NewGridLayout()
	g.SetSpacing(4) // half a cell, which no cell surface can render
	for i := 0; i < 4; i++ {
		g.AddColumn(Band{Stretch: 1})
	}
	c := newCellContainer()
	var kids []*sized
	for i := 0; i < 4; i++ {
		k := newSized(24, 16)
		k.SetLayoutGridPlacement(core.GridPlacement{Row: 0, Column: i, RowSpan: 1, ColumnSpan: 1})
		k.SetLayoutAlignment(core.Alignment{}.WithFill(false, false))
		c.AddChild(k)
		g.AddTrinket(k)
		kids = append(kids, k)
	}
	g.Layout(c, core.UnitRect{Width: 624, Height: 64})

	for i, k := range kids {
		b := k.Bounds()
		if b.X%m.UnitsPerCellWidth != 0 {
			t.Errorf("child %d is at x=%d, %d units into a %d-unit cell",
				i, b.X, b.X%m.UnitsPerCellWidth, m.UnitsPerCellWidth)
		}
		if b.Y%m.UnitsPerCellHeight != 0 {
			t.Errorf("child %d is at y=%d, %d units into a %d-unit row",
				i, b.Y, b.Y%m.UnitsPerCellHeight, m.UnitsPerCellHeight)
		}
	}
}

// A box does the same down its axis, where the sizes come from the same
// distribution.
func TestABoxPlacesEveryChildOnTheCellGrid(t *testing.T) {
	m := core.DefaultCellMetrics()
	l := NewBoxLayout(core.Vertical)
	l.SetSpacing(4)
	c := newCellContainer()
	var kids []*sized
	for i := 0; i < 3; i++ {
		k := newSized(40, 20) // 20 units is a row and a quarter
		k.SetLayoutStretch(1)
		k.SetLayoutAlignment(core.Alignment{}.WithFill(false, false))
		c.AddChild(k)
		l.AddTrinket(k)
		kids = append(kids, k)
	}
	l.Layout(c, core.UnitRect{Width: 200, Height: 250})

	for i, k := range kids {
		if got := k.Bounds().Y; got%m.UnitsPerCellHeight != 0 {
			t.Errorf("child %d is at y=%d, %d units into a row",
				i, got, got%m.UnitsPerCellHeight)
		}
	}
}

// And the children do not overlap, which is what snapping each child's origin
// after the fact would have cost: a floored origin pulls a child up into the
// one above it. The tracks are sized in whole cells instead, so the layout's
// own spacing survives.
func TestCellGridPlacementDoesNotOverlap(t *testing.T) {
	l := NewBoxLayout(core.Vertical)
	l.SetSpacing(8)
	c := newCellContainer()
	var kids []*sized
	for i := 0; i < 4; i++ {
		k := newSized(40, 20)
		k.SetLayoutStretch(1)
		k.SetLayoutAlignment(core.Alignment{}.WithFill(false, false))
		c.AddChild(k)
		l.AddTrinket(k)
		kids = append(kids, k)
	}
	l.Layout(c, core.UnitRect{Width: 200, Height: 253})

	for i := 1; i < len(kids); i++ {
		prev, cur := kids[i-1].Bounds(), kids[i].Bounds()
		if cur.Y < prev.Y+prev.Height {
			t.Errorf("child %d starts at y=%d, before child %d ends at %d",
				i, cur.Y, i-1, prev.Y+prev.Height)
		}
	}
}

// The spacing between tracks is on the grid too, and a gap that is not a whole
// number of cells does not survive being put on it: two and a half cells of
// spacing lands as two cells between the first pair and three between the
// next, because where a track starts is rounded and the gap it was given is
// not. Rounding the spacing down instead gives every boundary the same width.
func TestSpacingBetweenTracksIsWholeCells(t *testing.T) {
	m := core.DefaultCellMetrics()
	g := NewGridLayout()
	g.SetSpacing(20) // two cells and a half
	for i := 0; i < 3; i++ {
		g.AddColumn(Band{Stretch: 1})
	}
	c := newCellContainer()
	var kids []*block
	for i := 0; i < 3; i++ {
		k := newBlock(24, 16)
		k.SetLayoutGridPlacement(core.GridPlacement{Row: 0, Column: i, RowSpan: 1, ColumnSpan: 1})
		k.SetLayoutAlignment(core.Alignment{}.WithFill(true, false))
		c.AddChild(k)
		g.AddTrinket(k)
		kids = append(kids, k)
	}
	g.Layout(c, core.UnitRect{Width: 400, Height: 64})

	for i, k := range kids {
		b := k.Bounds()
		if b.X%m.UnitsPerCellWidth != 0 {
			t.Errorf("child %d is at x=%d, %d units into a cell",
				i, b.X, b.X%m.UnitsPerCellWidth)
		}
		if b.Width%m.UnitsPerCellWidth != 0 {
			t.Errorf("child %d is %d wide, %d units of a cell",
				i, b.Width, b.Width%m.UnitsPerCellWidth)
		}
	}

	first := kids[1].Bounds().X - (kids[0].Bounds().X + kids[0].Bounds().Width)
	for i := 2; i < len(kids); i++ {
		gap := kids[i].Bounds().X - (kids[i-1].Bounds().X + kids[i-1].Bounds().Width)
		if gap != first {
			t.Errorf("the boundary before child %d is %d units wide, the first was %d",
				i, gap, first)
		}
	}
}

// Rows are the same, and a child that spans two of them covers exactly the
// boundary the rows were charged for -- not the spacing they were asked for.
func TestRowsAndTheSpansAcrossThemAgreeOnTheBoundary(t *testing.T) {
	g := NewGridLayout()
	g.SetSpacing(24) // a row and a half
	g.AddColumn(Band{Stretch: 1})
	g.AddColumn(Band{Stretch: 1})

	c := newCellContainer()
	var stacked []*block
	for row := 0; row < 3; row++ {
		k := newBlock(24, 16)
		k.SetLayoutGridPlacement(core.GridPlacement{Row: row, Column: 0, RowSpan: 1, ColumnSpan: 1})
		c.AddChild(k)
		g.AddTrinket(k)
		stacked = append(stacked, k)
	}
	spanning := newBlock(24, 16)
	spanning.SetLayoutGridPlacement(core.GridPlacement{Row: 0, Column: 1, RowSpan: 2, ColumnSpan: 1})
	c.AddChild(spanning)
	g.AddTrinket(spanning)

	g.Layout(c, core.UnitRect{Width: 400, Height: 200})

	first := stacked[1].Bounds().Y - (stacked[0].Bounds().Y + stacked[0].Bounds().Height)
	for i := 2; i < len(stacked); i++ {
		gap := stacked[i].Bounds().Y - (stacked[i-1].Bounds().Y + stacked[i-1].Bounds().Height)
		if gap != first {
			t.Errorf("the boundary above row %d is %d units deep, the first was %d",
				i, gap, first)
		}
	}

	s, second := spanning.Bounds(), stacked[1].Bounds()
	if got, want := s.Y+s.Height, second.Y+second.Height; got != want {
		t.Errorf("a child spanning two rows ends at y=%d; the second row ends at %d", got, want)
	}
}

// A grid asks for whole cells, because whole cells is what it lays out. One
// that asked for the raw floors asked for less than it will give its rows and
// columns, and a bordered panel sitting at its own hint drew its frame through
// its children.
func TestAGridOnACellSurfaceAsksForWholeCells(t *testing.T) {
	m := core.DefaultCellMetrics()
	g := NewGridLayout()
	g.SetSpacing(0)
	g.AddColumn(Band{})
	c := newCellContainer()
	// Twenty units is two and a half cells across and a row and a quarter
	// down: neither axis is a whole number of anything.
	k := newBlock(20, 20)
	k.SetLayoutGridPlacement(core.GridPlacement{Row: 0, Column: 0, RowSpan: 1, ColumnSpan: 1})
	c.AddChild(k)
	g.AddTrinket(k)

	hint := g.SizeHint(c)
	if hint.Width%m.UnitsPerCellWidth != 0 {
		t.Errorf("the grid asks for %d units of width, %d into a cell",
			hint.Width, hint.Width%m.UnitsPerCellWidth)
	}
	if hint.Height%m.UnitsPerCellHeight != 0 {
		t.Errorf("the grid asks for %d units of height, %d into a row",
			hint.Height, hint.Height%m.UnitsPerCellHeight)
	}
}

// A cell is taller than it is wide, so a spacing that is a whole number of
// cells across the page is a fraction of one down it. A wrapping flex asks the
// two axes separately: the run gets the gap it can draw, and the lines below
// still start on a row.
func TestAWrappingFlexStacksItsLinesOnTheRowGrid(t *testing.T) {
	m := core.DefaultCellMetrics()
	l := NewFlexLayout()
	l.SetWrap(FlexWrapNormal)
	l.SetSpacing(m.UnitsPerCellWidth) // a whole cell across, half a row down
	c := newCellContainer()
	var kids []*block
	for i := 0; i < 4; i++ {
		k := newBlock(120, 16)
		c.AddChild(k)
		l.AddTrinket(k)
		kids = append(kids, k)
	}
	// Room for two of them across, so the run breaks into two lines.
	l.Layout(c, core.UnitRect{Width: 256, Height: 200})

	for i, k := range kids {
		if got := k.Bounds().Y; got%m.UnitsPerCellHeight != 0 {
			t.Errorf("child %d is at y=%d, %d units into a row",
				i, got, got%m.UnitsPerCellHeight)
		}
	}
}

// A line is as deep as the deepest thing in it, and that is an extent: it
// takes the whole cell it needs, so the line below starts on a row rather than
// a fraction of one down from it.
func TestAFlexLineIsAWholeNumberOfRowsDeep(t *testing.T) {
	m := core.DefaultCellMetrics()
	l := NewFlexLayout()
	l.SetWrap(FlexWrapNormal)
	l.SetSpacing(0)
	c := newCellContainer()
	var kids []*block
	for i := 0; i < 4; i++ {
		k := newBlock(120, 20) // a row and a quarter deep
		c.AddChild(k)
		l.AddTrinket(k)
		kids = append(kids, k)
	}
	l.Layout(c, core.UnitRect{Width: 256, Height: 200})

	for i, k := range kids {
		if got := k.Bounds().Y; got%m.UnitsPerCellHeight != 0 {
			t.Errorf("child %d is at y=%d, %d units into a row",
				i, got, got%m.UnitsPerCellHeight)
		}
	}
}

// A room too small for what it was handed stays too small. Quantizing settles
// where the cell boundaries fall; it does not decide who gets clipped, so a
// child in an over-committed column keeps the cells it was measured in and the
// column overflows exactly as it did before.
func TestAShortRoomDoesNotShrinkAChildPastItsMeasuredCells(t *testing.T) {
	m := core.DefaultCellMetrics()
	l := NewBoxLayout(core.Vertical)
	l.SetSpacing(0)
	c := newCellContainer()
	var kids []*sized
	for i := 0; i < 4; i++ {
		k := newSized(40, 40) // two and a half rows
		k.SetLayoutAlignment(core.Alignment{}.WithFill(false, false))
		c.AddChild(k)
		l.AddTrinket(k)
		kids = append(kids, k)
	}
	// A hundred units for four children asking for forty apiece.
	l.Layout(c, core.UnitRect{Width: 200, Height: 100})

	want := (40 / m.UnitsPerCellHeight) * m.UnitsPerCellHeight
	for i, k := range kids {
		if got := k.Bounds().Height; got < want {
			t.Errorf("child %d is %d units tall, cut below the %d it was measured in",
				i, got, want)
		}
	}
}

// A smooth surface keeps the exact arithmetic: it has no grid to stand on.
func TestASmoothSurfaceKeepsTheExactDistribution(t *testing.T) {
	g := NewGridLayout()
	g.SetSpacing(0)
	for i := 0; i < 4; i++ {
		g.AddColumn(Band{Stretch: 1})
	}
	c := newDirContainer(core.DirLTR) // smooth
	var kids []*sized
	for i := 0; i < 4; i++ {
		k := newSized(24, 16)
		k.SetLayoutGridPlacement(core.GridPlacement{Row: 0, Column: i, RowSpan: 1, ColumnSpan: 1})
		k.SetLayoutAlignment(core.Alignment{}.WithFill(false, false))
		c.AddChild(k)
		g.AddTrinket(k)
		kids = append(kids, k)
	}
	g.Layout(c, core.UnitRect{Width: 100, Height: 64})

	// A hundred units over four columns does not divide into whole cells, so
	// on a smooth surface at least one child lands off the cell grid -- which
	// is exactly what a smooth surface is allowed to do.
	m := core.DefaultCellMetrics()
	offGrid := false
	for _, k := range kids {
		if k.Bounds().X%m.UnitsPerCellWidth != 0 {
			offGrid = true
		}
	}
	if !offGrid {
		t.Error("every child landed on the cell grid; a smooth surface was quantized")
	}
}
