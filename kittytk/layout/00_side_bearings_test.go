package layout

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// boxPanel is a container that lays its children out with a box of its own --
// what a Panel is, in the small.
type boxPanel struct {
	core.TrinketBase
	box  *BoxLayout
	kids []core.Trinket
}

func newBoxPanel(o core.Orientation) *boxPanel {
	p := &boxPanel{box: NewBoxLayout(o)}
	p.TrinketBase = *core.NewTrinketBase()
	p.Init(p)
	p.box.SetSpacing(0)
	p.box.SetMetricsSource(p)
	return p
}

func (p *boxPanel) Children() []core.Trinket { return p.kids }
func (p *boxPanel) AddChild(w core.Trinket) {
	p.kids = append(p.kids, w)
	w.SetParent(p)
	p.box.AddTrinket(w)
}
func (p *boxPanel) RemoveChild(core.Trinket)            {}
func (p *boxPanel) ChildAt(core.UnitPoint) core.Trinket { return nil }
func (p *boxPanel) LayoutManager() core.LayoutManager   { return p.box }
func (p *boxPanel) SetLayoutManager(core.LayoutManager) {}
func (p *boxPanel) SizeHint() core.UnitSize             { return p.box.SizeHint(p) }
func (p *boxPanel) Layout() {
	b := p.Bounds()
	p.box.Layout(p, core.UnitRect{Width: b.Width, Height: b.Height})
}

// gridBlock is a container of a given size: a block, so it carries no bearings
// of its own.
type gridBlock struct {
	boxPanel
	own core.UnitSize
}

func newGridBlock(w, h core.Unit) *gridBlock {
	b := &gridBlock{own: core.UnitSize{Width: w, Height: h}}
	b.TrinketBase = *core.NewTrinketBase()
	b.Init(b)
	b.box = NewBoxLayout(core.Horizontal)
	return b
}

func (b *gridBlock) SizeHint() core.UnitSize { return b.own }

// An inline trinket gets its side-bearings wherever it is put, so one placed
// straight into a grid cell lines up with one that reached the same column
// through a box nested in the cell below it.
//
// The bearing belongs to the trinket. A grid that did not apply it drew its own
// children flush against their cells while a nested box gave its children a
// column -- two children of one grid, at two different insets, which is what
// made a form's button row sit a column off from the fields above it.
func TestAGridChildAndABoxedChildLineUp(t *testing.T) {
	c := newDirContainer(core.DirLTR)
	grid := NewGridLayout()
	grid.SetSpacing(0)

	// Row 0: an inline child straight into the cell.
	direct := placed(50, 20, core.GridPlacement{Row: 0, Column: 0, ColumnStretch: 1})
	c.AddChild(direct)
	grid.AddTrinket(direct)

	// Row 1: the same kind of child, inside a box, inside the cell.
	panel := newBoxPanel(core.Horizontal)
	panel.SetLayoutGridPlacement(core.GridPlacement{Row: 1, Column: 0, RowSpan: 1, ColumnSpan: 1, ColumnStretch: 1})
	boxed := newFlexChild(50, 20)
	panel.AddChild(boxed)
	c.AddChild(panel)
	grid.AddTrinket(panel)

	grid.Layout(c, core.UnitRect{Width: 300, Height: 100})
	panel.Layout()

	// The boxed child's position is inside its panel; bring it out to the
	// grid's own coordinates before comparing.
	boxedX := panel.Bounds().X + boxed.Bounds().X
	if boxedX != direct.Bounds().X {
		t.Errorf("a child straight in a cell starts at %d and one inside a box in a cell at %d; "+
			"the bearing should have appeared once on each path",
			direct.Bounds().X, boxedX)
	}

	m := core.FindEffectiveCellMetrics(c.Self())
	if want := m.UnitsPerCellWidth; direct.Bounds().X != want {
		t.Errorf("the inset is %d, want one column of %d", direct.Bounds().X, want)
	}
}

// A block gets no bearings of its own -- it is the run inside it that has them,
// which is exactly what makes the two paths above agree.
func TestABlockInAGridGetsNoBearings(t *testing.T) {
	c := newDirContainer(core.DirLTR)
	grid := NewGridLayout()
	grid.SetSpacing(0)

	block := newGridBlock(50, 20)
	block.SetLayoutGridPlacement(core.GridPlacement{Row: 0, Column: 0, RowSpan: 1, ColumnSpan: 1})
	c.AddChild(block)
	grid.AddTrinket(block)

	grid.Layout(c, core.UnitRect{Width: 300, Height: 100})
	if block.Bounds().X != 0 {
		t.Errorf("a block in a grid starts at %d, want the cell's own edge at 0", block.Bounds().X)
	}
}

// Two inline children side by side are as far apart in a grid as the same two
// are in a row. A grid's columns are a layout, not a different kind of space.
//
// A cell kept its child's bearings inside it and the next cell kept its own, so
// a boundary opened two columns of air where a row opens one -- and the
// configured spacing on top of that. The boundary now closes up by the column
// the two bearings collapse into.
func TestAGridBoundaryIsAsWideAsARowsGap(t *testing.T) {
	const spacing = core.Unit(8)

	// The same two children, in a row.
	box := NewHBoxLayout()
	box.SetSpacing(spacing)
	bc := newDirContainer(core.DirLTR)
	ba, bb := newFlexChild(64, 20), newFlexChild(64, 20)
	bc.AddChild(ba)
	bc.AddChild(bb)
	box.AddTrinket(ba)
	box.AddTrinket(bb)
	box.Layout(bc, core.UnitRect{Width: 300, Height: 40})
	rowGap := bb.Bounds().X - (ba.Bounds().X + ba.Bounds().Width)

	// And in two columns of a grid.
	grid := NewGridLayout()
	grid.SetSpacing(spacing)
	gc := newDirContainer(core.DirLTR)
	ga := placed(64, 20, core.GridPlacement{Row: 0, Column: 0})
	gb := placed(64, 20, core.GridPlacement{Row: 0, Column: 1})
	gc.AddChild(ga)
	gc.AddChild(gb)
	grid.AddTrinket(ga)
	grid.AddTrinket(gb)
	grid.Layout(gc, core.UnitRect{Width: 300, Height: 40})
	gridGap := gb.Bounds().X - (ga.Bounds().X + ga.Bounds().Width)

	cell := core.FindEffectiveCellMetrics(gc.Self()).UnitsPerCellWidth
	if rowGap != cell {
		t.Fatalf("test setup: a row puts %d between two inline children, want one column of %d",
			rowGap, cell)
	}
	if gridGap != rowGap {
		t.Errorf("a grid puts %d between two inline children where a row puts %d", gridGap, rowGap)
	}
}

// Two blocks have no bearings to collapse, so the configured spacing is the
// whole of the air between their columns.
func TestAGridBoundaryBetweenBlocksIsTheSpacing(t *testing.T) {
	const spacing = core.Unit(24)

	grid := NewGridLayout()
	grid.SetSpacing(spacing)
	c := newDirContainer(core.DirLTR)
	a, b := newGridBlock(64, 20), newGridBlock(64, 20)
	a.SetLayoutGridPlacement(core.GridPlacement{Row: 0, Column: 0, RowSpan: 1, ColumnSpan: 1})
	b.SetLayoutGridPlacement(core.GridPlacement{Row: 0, Column: 1, RowSpan: 1, ColumnSpan: 1})
	c.AddChild(a)
	c.AddChild(b)
	grid.AddTrinket(a)
	grid.AddTrinket(b)
	grid.Layout(c, core.UnitRect{Width: 300, Height: 40})

	if gap := b.Bounds().X - (a.Bounds().X + a.Bounds().Width); gap != spacing {
		t.Errorf("two blocks are %d apart, want the %d asked for", gap, spacing)
	}
	if a.Bounds().X != 0 {
		t.Errorf("a block starts at %d, want the grid's own edge at 0", a.Bounds().X)
	}
}

// One inline side and one block: the single bearing is the whole of the air,
// and the boundary adds nothing on top of it.
func TestAGridBoundaryWithOneInlineSide(t *testing.T) {
	grid := NewGridLayout()
	grid.SetSpacing(24)
	c := newDirContainer(core.DirLTR)
	inline := placed(64, 20, core.GridPlacement{Row: 0, Column: 0})
	block := newGridBlock(64, 20)
	block.SetLayoutGridPlacement(core.GridPlacement{Row: 0, Column: 1, RowSpan: 1, ColumnSpan: 1})
	c.AddChild(inline)
	c.AddChild(block)
	grid.AddTrinket(inline)
	grid.AddTrinket(block)
	grid.Layout(c, core.UnitRect{Width: 300, Height: 40})

	cell := core.FindEffectiveCellMetrics(c.Self()).UnitsPerCellWidth
	gap := block.Bounds().X - (inline.Bounds().X + inline.Bounds().Width)
	if gap != cell {
		t.Errorf("an inline child and a block are %d apart, want the one column of %d the bearing opens",
			gap, cell)
	}
}

// A child that SPANS a boundary straddles it and brings no bearing to it, but
// it does bring one to the boundary at the far END of its span -- that is where
// its trailing bearing falls.
//
// The columns get their width from the blocks below, because a spanning child
// contributes to no column's width (see OPEN-FINDINGS); what is being read here
// is the boundary, not the widths.
func TestASpansBearingFallsAtTheEndOfItsSpan(t *testing.T) {
	grid := NewGridLayout()
	grid.SetSpacing(24)
	c := newDirContainer(core.DirLTR)

	span := placed(120, 20, core.GridPlacement{Row: 0, Column: 0, ColumnSpan: 2})
	last := placed(64, 20, core.GridPlacement{Row: 0, Column: 2})
	fill0 := newGridBlock(64, 20)
	fill0.SetLayoutGridPlacement(core.GridPlacement{Row: 1, Column: 0, RowSpan: 1, ColumnSpan: 1})
	fill1 := newGridBlock(64, 20)
	fill1.SetLayoutGridPlacement(core.GridPlacement{Row: 1, Column: 1, RowSpan: 1, ColumnSpan: 1})

	for _, k := range []core.Trinket{span, last, fill0, fill1} {
		c.AddChild(k)
		grid.AddTrinket(k)
	}
	grid.Layout(c, core.UnitRect{Width: 400, Height: 80})

	cell := core.FindEffectiveCellMetrics(c.Self()).UnitsPerCellWidth
	// The far boundary: the span's trailing bearing meets the last child's
	// leading one, and the two are one column. Two columns of air here would
	// mean the span had been taken to end where it starts.
	if gap := last.Bounds().X - (span.Bounds().X + span.Bounds().Width); gap != cell {
		t.Errorf("the span and the child after it are %d apart, want the one column of %d "+
			"their bearings collapse to", gap, cell)
	}
}

// A grid fills the width it is given: what the boundaries take, or give back
// where two bearings close up, is not the columns' to divide, so the last
// column still reaches the far edge.
func TestAGridFillsTheWidthItIsGiven(t *testing.T) {
	grid := NewGridLayout()
	grid.SetSpacing(8)
	c := newDirContainer(core.DirLTR)
	first := placed(64, 20, core.GridPlacement{Row: 0, Column: 0})
	last := placed(64, 20, core.GridPlacement{Row: 0, Column: 1, ColumnStretch: 1})
	c.AddChild(first)
	c.AddChild(last)
	grid.AddTrinket(first)
	grid.AddTrinket(last)

	const width = core.Unit(300)
	grid.Layout(c, core.UnitRect{Width: width, Height: 40})

	cell := core.FindEffectiveCellMetrics(c.Self()).UnitsPerCellWidth
	if want := width - cell; last.Bounds().X+last.Bounds().Width != want {
		t.Errorf("the stretching column's child ends at %d, want %d -- the grid's edge less its bearing",
			last.Bounds().X+last.Bounds().Width, want)
	}
}

// A flex run gives its inline children the same bearings a box does: a column
// before the first, one between, one after the last.
func TestAFlexRunKeepsSideBearings(t *testing.T) {
	l := NewFlexLayout()
	l.SetSpacing(0)
	a, b := newFlexChild(56, 32), newFlexChild(72, 32)
	got := flexBounds(l, a, b)

	c := newDirContainer(core.DirLTR)
	cell := core.FindEffectiveCellMetrics(c.Self()).UnitsPerCellWidth

	if got[0].X != cell {
		t.Errorf("the first child starts at %d, want a column in at %d", got[0].X, cell)
	}
	if gap := got[1].X - (got[0].X + got[0].Width); gap != cell {
		t.Errorf("the gap between two inline children is %d, want the one column they collapse to", gap)
	}
}
