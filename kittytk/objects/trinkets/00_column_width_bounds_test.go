package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A column's maximum spells "no limit" as -1, the way the rest of the toolkit
// does. Zero is no limit either: a column is measured in whole text cells and
// never renders narrower than one, so a maximum of zero bounds it to exactly
// what a maximum of one does -- while reading zero as a cap makes every
// struct literal that leaves MaxWidth out collapse to a single cell, which is
// how the event viewer lost every column but its widest.
func TestAColumnsMaximumIsMinusOneWhenThereIsNone(t *testing.T) {
	c := NewTreeColumn("size", "Size", 10)
	if c.MaxWidth != -1 {
		t.Errorf("a new column's MaxWidth is %d, want -1", c.MaxWidth)
	}
	// Nothing bounds it from above, so a wide drag stands.
	if got := c.clampWidth(400); got != 400 {
		t.Errorf("with no maximum a drag to 400 gave %d", got)
	}

	c.MaxWidth = 12
	if got := c.clampWidth(400); got != 12 {
		t.Errorf("with a maximum of 12 a drag to 400 gave %d", got)
	}
	if got := c.clampWidth(6); got != 6 {
		t.Errorf("a width inside the bounds came back as %d", got)
	}
}

// Where the two conflict the minimum wins, which is the rule everywhere a
// minimum meets a maximum.
//
// The maximum was applied last, so a maximum below the minimum overrode it and
// a column could be clamped under the width its own content needs.
func TestAColumnsMinimumBeatsItsMaximum(t *testing.T) {
	c := NewTreeColumn("size", "Size", 10)
	c.MinWidth, c.MaxWidth = 8, 3

	if got := c.clampWidth(20); got != 8 {
		t.Errorf("min 8 against max 3 gave %d, want the minimum's 8", got)
	}
	if got := c.clampWidth(1); got != 8 {
		t.Errorf("a drag below both gave %d, want the minimum's 8", got)
	}
}

// The maximum bounds the divider drag too, and reads zero as no maximum
// exactly as clampWidth does -- the same column must not answer two ways
// depending on which one asked.
func TestAColumnsMaximumBoundsTheDividerDrag(t *testing.T) {
	for _, c := range []struct {
		max  int
		want int
	}{
		{-1, 14}, // no limit: the drag's full four cells land
		{12, 12}, // bounded above: it stops where it was told
		{0, 14},  // zero is no maximum: the drag's full four cells land
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
		max  int
		want int
	}{
		{-1, 16},
		{10, 10},
		{0, 16}, // zero is no maximum
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
		max  int
		want int
	}{
		{-1, 20},
		{16, 16},
		{0, 20}, // zero is no maximum
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
		{ID: "seq", Caption: "#", Width: 7},
		{ID: "event", Caption: "Event", Width: 14, Resizable: true},
		{ID: "detail", Caption: "Detail", Width: 40, Resizable: true},
	} {
		if got := c.clampWidth(c.Width); got != c.Width {
			t.Errorf("column %q declared %d cells and clamped to %d", c.ID, c.Width, got)
		}
	}
}

// End to end: the desktop's own event viewer, whose columns are the reason
// this was noticed.
func TestTheEventViewersColumnsKeepTheirWidths(t *testing.T) {
	want := map[string]int{
		"seq": 7, "event": 14, "key": 16, "mods": 22,
		"repeat": 7, "text": 8, "detail": 40,
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
			t.Errorf("the %q column is %d cells wide, want the %d it declared", id, got, w)
		}
	}
}
