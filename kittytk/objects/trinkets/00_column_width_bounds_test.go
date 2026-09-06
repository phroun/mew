package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A column's maximum spells "no limit" as -1, the way the rest of the toolkit
// does. Zero is no limit either -- the one place a size here does not read
// zero as a real answer -- because reading it as a cap makes every struct
// literal that leaves MaxWidth out collapse to nothing, which is how the event
// viewer lost every column but its widest.
func TestAColumnsMaximumIsMinusOneWhenThereIsNone(t *testing.T) {
	c := NewTreeColumn("size", "Size", 10*cell)
	if c.MaxWidth != core.Unbounded {
		t.Errorf("a new column's MaxWidth is %d, want %d", c.MaxWidth, core.Unbounded)
	}
	// Nothing bounds it from above, so a wide drag stands.
	if got := c.clampWidth(400 * cell); got != 400*cell {
		t.Errorf("with no maximum a drag to %d gave %d", 400*cell, got)
	}

	c.MaxWidth = 12 * cell
	if got := c.clampWidth(400 * cell); got != 12*cell {
		t.Errorf("with a maximum of %d a wide drag gave %d", 12*cell, got)
	}
	if got := c.clampWidth(6 * cell); got != 6*cell {
		t.Errorf("a width inside the bounds came back as %d", got)
	}
}

// cell is one column of the default denomination, which is what the widths in
// these tests are written in: they were cell counts before a column's width
// became a measurement like every other in the toolkit.
const cell = core.Unit(8)

// Where the two conflict the minimum wins, which is the rule everywhere a
// minimum meets a maximum.
//
// The maximum was applied last, so a maximum below the minimum overrode it and
// a column could be clamped under the width its own content needs.
func TestAColumnsMinimumBeatsItsMaximum(t *testing.T) {
	c := NewTreeColumn("size", "Size", 10*cell)
	c.MinWidth, c.MaxWidth = 8*cell, 3*cell

	if got := c.clampWidth(20 * cell); got != 8*cell {
		t.Errorf("a minimum of %d against a maximum of %d gave %d, want the minimum",
			8*cell, 3*cell, got)
	}
	if got := c.clampWidth(cell); got != 8*cell {
		t.Errorf("a drag below both gave %d, want the minimum's %d", got, 8*cell)
	}
}

// The maximum bounds the divider drag too, and reads zero as no maximum
// exactly as clampWidth does -- the same column must not answer two ways
// depending on which one asked.
func TestAColumnsMaximumBoundsTheDividerDrag(t *testing.T) {
	for _, c := range []struct {
		max  core.Unit
		want core.Unit
	}{
		{core.Unbounded, 14 * cell}, // no limit: the drag's full four cells land
		{12 * cell, 12 * cell},      // bounded above: it stops where it was told
		{0, 14 * cell},              // zero is no maximum: the drag's full four cells land
	} {
		tv := newColumnsTree(60, 10)
		tv.ColumnByID("size").MaxWidth = c.max
		lay := tv.columnLayout()
		divX := lay.spans[1].divX

		if !tv.HandleMousePress(core.MousePressEvent{X: divX + 2, Y: 4, Button: core.LeftButton}) {
			t.Fatalf("max %d: divider press not handled", c.max)
		}
		tv.HandleMouseMove(core.MouseMoveEvent{X: divX + 2 + 4*8, Y: 4, Buttons: 1})

		if got := tv.ColumnByID("size").Width; got != c.want {
			t.Errorf("with a maximum of %d a four-cell drag left the column %d wide, want %d",
				c.max, got, c.want)
		}
	}

	// The other direction widens the column on the RIGHT of the divider, and
	// its own maximum bounds it the same way.
	for _, c := range []struct {
		max  core.Unit
		want core.Unit
	}{
		{core.Unbounded, 16 * cell},
		{10 * cell, 10 * cell},
		{0, 16 * cell}, // zero is no maximum
	} {
		tv := newColumnsTree(60, 10)
		tv.ColumnByID("kind").MaxWidth = c.max
		lay := tv.columnLayout()
		divX := lay.spans[1].divX

		if !tv.HandleMousePress(core.MousePressEvent{X: divX + 2, Y: 4, Button: core.LeftButton}) {
			t.Fatalf("max %d: divider press not handled", c.max)
		}
		tv.HandleMouseMove(core.MouseMoveEvent{X: divX + 2 - 4*8, Y: 4, Buttons: 1})

		if got := tv.ColumnByID("kind").Width; got != c.want {
			t.Errorf("with a maximum of %d a four-cell drag the other way left the column %d wide, want %d",
				c.max, got, c.want)
		}
	}
}

// And in the mirrored arrangement -- the key hidden, so the slack is the blank
// width right of the last column -- the maximum bounds the drag the same way.
func TestAColumnsMaximumBoundsTheMirroredDrag(t *testing.T) {
	for _, c := range []struct {
		max  core.Unit
		want core.Unit
	}{
		{core.Unbounded, 20 * cell},
		{16 * cell, 16 * cell},
		{0, 20 * cell}, // zero is no maximum
	} {
		tv := newColumnsTree(60, 10)
		tv.SetShowKey(false)
		tv.ColumnByID("size").MaxWidth = c.max
		lay := tv.columnLayout()
		divX := lay.spans[0].divX

		if !tv.HandleMousePress(core.MousePressEvent{X: divX + 2, Y: 4, Button: core.LeftButton}) {
			t.Fatalf("max %d: divider press not handled", c.max)
		}
		tv.HandleMouseMove(core.MouseMoveEvent{X: divX + 2 + 10*8, Y: 4, Buttons: 1})

		if got := tv.ColumnByID("size").Width; got != c.want {
			t.Errorf("with a maximum of %d a ten-cell drag into the blank left the column %d wide, want %d",
				c.max, got, c.want)
		}
	}
}

// A column written as a struct literal keeps the width it declared. Every
// column in the toolkit that is not built by NewTreeColumn is written this
// way, so a maximum read out of the zero value collapses all of them at once
// -- which is what happened to the event viewer: seven columns one cell wide,
// and only the widest still readable.
func TestAColumnWrittenAsALiteralKeepsItsWidth(t *testing.T) {
	for _, c := range []*TreeColumn{
		{ID: "seq", Caption: "#", Width: 7 * cell},
		{ID: "event", Caption: "Event", Width: 14 * cell, Resizable: true},
		{ID: "detail", Caption: "Detail", Width: 40 * cell, Resizable: true},
	} {
		if got := c.clampWidth(c.Width); got != c.Width {
			t.Errorf("column %q declared %d units and clamped to %d", c.ID, c.Width, got)
		}
	}
}

// End to end: the desktop's own event viewer, whose columns are the reason
// this was noticed.
func TestTheEventViewersColumnsKeepTheirWidths(t *testing.T) {
	want := map[string]core.Unit{
		"seq": 7 * cell, "event": 14 * cell, "key": 16 * cell, "mods": 22 * cell,
		"repeat": 7 * cell, "text": 8 * cell, "detail": 40 * cell,
	}
	v := &eventViewer{}
	v.build()
	for id, w := range want {
		c := v.tree.ColumnByID(id)
		if c == nil {
			t.Errorf("the event viewer has no %q column", id)
			continue
		}
		if got := c.clampWidth(c.Width); got != w {
			t.Errorf("the %q column is %d units wide, want the %d it declared", id, got, w)
		}
	}
}

// The key column has no TreeColumn behind it, so at the FIRST divider the
// left neighbour is nil -- and asking a column that is not there for its
// maximum took the whole display down.
//
// Go evaluates an if-statement's initializer before its condition, so the
// `l != nil` standing in the condition never guarded the `l.maxWidth()`
// standing in the initializer.
func TestDraggingTheFirstDividerWithNoColumnLeftOfIt(t *testing.T) {
	tv := newColumnsTree(60, 10) // fit mode: span 0 is the auto key column
	lay := tv.columnLayout()
	if lay.spans[0].col != nil {
		t.Fatal("precondition: the first span is the key column, with no column behind it")
	}
	divX := lay.spans[0].divX
	if !tv.HandleMousePress(core.MousePressEvent{X: divX + 2, Y: 4, Button: core.LeftButton}) {
		t.Fatal("divider press not handled")
	}

	// Rightward is the direction that asks the left neighbour's maximum.
	tv.HandleMouseMove(core.MouseMoveEvent{X: divX + 2 + 4*cell, Y: 4, Buttons: 1})
	if got := tv.ColumnByID("size").Width; got != 6*cell {
		t.Errorf("a four-cell drag right left size %d wide, want %d", got, 6*cell)
	}

	// And with a maximum on the neighbour, which is the branch that reads it.
	tv.ColumnByID("size").MaxWidth = 12 * cell
	tv.HandleMouseMove(core.MouseMoveEvent{X: divX + 2 - 4*cell, Y: 4, Buttons: 1})
	if got := tv.ColumnByID("size").Width; got != 12*cell {
		t.Errorf("a four-cell drag left against a maximum of %d left size %d wide, want the maximum",
			12*cell, got)
	}
}
