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
