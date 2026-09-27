package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// cellContainerTrinket is a bare parent a tree can hang from, so the tree can
// ask its ancestry what kind of surface it is on. The cell path is what every
// other tree test exercises; a smooth one is what says a column may be a
// fraction of a cell where the surface can draw it.
type cellContainerTrinket struct {
	core.TrinketBase
	kids   []core.Trinket
	smooth bool
}

func (c *cellContainerTrinket) SmoothWindowPositioning() bool       { return c.smooth }
func (c *cellContainerTrinket) Children() []core.Trinket            { return c.kids }
func (c *cellContainerTrinket) AddChild(w core.Trinket)             { c.kids = append(c.kids, w); w.SetParent(c) }
func (c *cellContainerTrinket) RemoveChild(core.Trinket)            {}
func (c *cellContainerTrinket) ChildAt(core.UnitPoint) core.Trinket { return nil }
func (c *cellContainerTrinket) Layout()                             {}
func (c *cellContainerTrinket) LayoutManager() core.LayoutManager   { return nil }
func (c *cellContainerTrinket) SetLayoutManager(core.LayoutManager) {}

func newSurface(smooth bool) *cellContainerTrinket {
	c := &cellContainerTrinket{smooth: smooth}
	c.TrinketBase = *core.NewTrinketBase()
	c.Init(c)
	return c
}

// treeOn builds the three-column tree on a surface of the given kind.
func treeOn(t *testing.T, smooth bool) *TreeView {
	t.Helper()
	tv := newColumnsTree(60, 10)
	newSurface(smooth).AddChild(tv)
	return tv
}

// A column's width is a MEASUREMENT, counted in units like every other size in
// this toolkit -- so what it means in columns follows the denomination in
// force, and a subtree that re-denominates itself re-expresses its columns
// with everything else in it.
func TestAColumnsWidthIsInUnits(t *testing.T) {
	c := NewTreeColumn("size", "Size", 10*cell)
	if c.Width != 80 {
		t.Errorf("a column declared 10 cells wide is %d units, want 80", c.Width)
	}
	if c.MinWidth != 3*cell {
		t.Errorf("a new column's minimum is %d, want %d -- three cells of the default", c.MinWidth, 3*cell)
	}

	// Half a cell is a width a column may hold. Whether it can be DRAWN is
	// the surface's question, asked at layout time and not here.
	c.Width = 84
	if got := c.clampWidth(c.Width); got != 84 {
		t.Errorf("a width of half a cell came back as %d", got)
	}
}

// On a cell surface every span is a whole number of cells and starts on one.
// A column that is not draws in one cell and answers the mouse in another.
func TestATreesColumnsLandOnWholeCellsInTheTUI(t *testing.T) {
	tv := treeOn(t, false)
	// Widths that are not whole cells: 10 and a half, 12 and a quarter.
	tv.ColumnByID("size").Width = 10*cell + 4
	tv.ColumnByID("kind").Width = 12*cell + 2

	lay := tv.columnLayout()
	for i, sp := range lay.spans {
		if sp.x%cell != 0 {
			t.Errorf("span %d starts at x=%d, %d units into a cell", i, sp.x, sp.x%cell)
		}
		if sp.w%cell != 0 {
			t.Errorf("span %d is %d wide, %d units past a cell", i, sp.w, sp.w%cell)
		}
		if sp.divX >= 0 && sp.divX%cell != 0 {
			t.Errorf("the divider after span %d is at x=%d, %d units into a cell",
				i, sp.divX, sp.divX%cell)
		}
	}

	// And a width over a cell boundary takes the whole cell it needs: an
	// extent ceils, so nothing is clipped to make it fit.
	var size colSpan
	for _, sp := range lay.spans {
		if sp.col != nil && sp.col.ID == "size" {
			size = sp
		}
	}
	if want := 11 * cell; size.w != want {
		t.Errorf("a column asking for %d units got %d, want %d -- rounded up to the cell it needs",
			10*cell+4, size.w, want)
	}
}

// A surface that can place between cells keeps the width it was given: this is
// where a drag lands exactly where the pointer did.
func TestATreesColumnsKeepTheirFractionsOnASmoothSurface(t *testing.T) {
	tv := treeOn(t, true)
	tv.SetFitWidth(false)
	tv.ColumnByID("size").Width = 10*cell + 4

	var size colSpan
	for _, sp := range tv.columnLayout().spans {
		if sp.col != nil && sp.col.ID == "size" {
			size = sp
		}
	}
	if want := 10*cell + 4; size.w != want {
		t.Errorf("the span is %d wide, want the %d the column asked for", size.w, want)
	}
}

// A divider drag moves the column the pointer moved: to the cell the pointer
// is in where the surface draws in cells, and to the pointer itself where it
// does not.
func TestADividerDragFollowsTheSurfaceItIsOn(t *testing.T) {
	// Scroll mode, where the divider sizes the column left of it outright.
	drag := func(smooth bool, by core.Unit) core.Unit {
		tv := treeOn(t, smooth)
		tv.SetFitWidth(false)
		tv.SetKeyWidth(20 * cell)
		lay := tv.columnLayout()
		divX := lay.spans[1].divX // between Size and Kind

		if !tv.HandleMousePress(core.MousePressEvent{X: divX + 2, Y: 4, Button: core.LeftButton}) {
			t.Fatalf("smooth=%v: divider press not handled", smooth)
		}
		tv.HandleMouseMove(core.MouseMoveEvent{X: divX + 2 + by, Y: 4, Buttons: 1})
		return tv.ColumnByID("size").Width
	}

	// Three and a half cells. The cell surface takes the three it can draw.
	if got, want := drag(false, 3*cell+4), 10*cell+3*cell; got != want {
		t.Errorf("on a cell surface a drag of %d left the column %d wide, want %d",
			3*cell+4, got, want)
	}
	// The smooth one takes all of it.
	if got, want := drag(true, 3*cell+4), 10*cell+3*cell+4; got != want {
		t.Errorf("on a smooth surface a drag of %d left the column %d wide, want %d",
			3*cell+4, got, want)
	}
	// And a drag too small for a cell moves nothing where cells are all
	// there is, while the smooth surface takes it.
	if got, want := drag(false, 3), 10*cell; got != want {
		t.Errorf("on a cell surface a drag of 3 units left the column %d wide, want %d", got, want)
	}
	if got, want := drag(true, 3), 10*cell+3; got != want {
		t.Errorf("on a smooth surface a drag of 3 units left the column %d wide, want %d", got, want)
	}
}

// The key column's width is a measurement too, and its floor is counted in
// CHARACTERS -- it is about how much of a name is readable -- so it follows
// the denomination rather than standing at a fixed number of units.
func TestTheKeyColumnsWidthIsInUnitsAndItsFloorInCharacters(t *testing.T) {
	tv := newColumnsTree(60, 10)
	tv.SetKeyWidth(30 * cell)
	if got := tv.keyWidth; got != 30*cell {
		t.Errorf("key width = %d, want %d", got, 30*cell)
	}

	// Below the floor it stops there rather than at the number asked for.
	tv.SetKeyWidth(cell)
	if got := tv.keyWidth; got != tv.keyMinWidth() {
		t.Errorf("key width = %d, want the floor %d", got, tv.keyMinWidth())
	}
	if want := core.Unit(treeKeyMinCells) * cell; tv.keyMinWidth() != want {
		t.Errorf("the floor is %d units, want %d -- %d characters of the denomination in force",
			tv.keyMinWidth(), want, treeKeyMinCells)
	}
}
