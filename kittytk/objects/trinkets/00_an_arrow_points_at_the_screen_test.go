package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A horizontal arrow names a SIDE OF THE SCREEN -- the left arrow always
// points left -- and what lies that way is the tree's to answer. A tree grows
// away from the edge it reads from, so the left arrow closes an item in one
// tree and opens it in the other.
func TestAnArrowOpensTheWayTheTreeGrows(t *testing.T) {
	for _, tc := range []struct {
		dir core.Direction
		// the arrow that steps ON into the tree, and the one that steps back
		on, back string
	}{
		{core.DirLTR, "Right", "Left"},
		{core.DirRTL, "Left", "Right"},
	} {
		tv := nestedTree(t, tc.dir, false)
		root := tv.rootItems[0]
		root.Expanded = false
		tv.rebuildFlatList()
		tv.SetCurrentIndex(0)

		key := func(k string) { tv.HandleKeyPress(core.KeyPressEvent{Key: k}) }

		key(tc.on)
		if !root.Expanded {
			t.Errorf("%v: %s did not open the root", tc.dir, tc.on)
		}
		// Open already: the same arrow descends into it.
		key(tc.on)
		if got := tv.CurrentItem(); got != root.Children[0] {
			t.Errorf("%v: %s did not step into the open root", tc.dir, tc.on)
		}
		// And back out to the item enclosing it, then closed.
		key(tc.back)
		if got := tv.CurrentItem(); got != root {
			t.Errorf("%v: %s did not climb back to the enclosing item", tc.dir, tc.back)
		}
		key(tc.back)
		if root.Expanded {
			t.Errorf("%v: %s did not close the root", tc.dir, tc.back)
		}
	}
}

// The SHIFTED arrows arrive as their own commands, which name the act rather
// than a side of the screen -- so they mean the same thing in either tree.
func TestTheNamedCommandsMeanTheSameEitherWay(t *testing.T) {
	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		tv := nestedTree(t, dir, false)
		root := tv.rootItems[0]
		root.Expanded = false
		tv.rebuildFlatList()
		tv.SetCurrentIndex(0)

		// S-Right and S-Left carry expand_or_descend and
		// collapse_or_enclosing, whichever way the tree reads.
		tv.HandleKeyPress(core.KeyPressEvent{Key: "S-Right"})
		if !root.Expanded {
			t.Errorf("%v: expand_or_descend did not open the root", dir)
		}
		tv.HandleKeyPress(core.KeyPressEvent{Key: "S-Left"})
		if root.Expanded {
			t.Errorf("%v: collapse_or_enclosing did not close the root", dir)
		}
	}
}

// The edit ring stands in the columns' own order, so an arrow walks it the way
// it points on the screen: the target moves towards the arrow, not away.
func TestTheEditRingWalksTowardsTheArrow(t *testing.T) {
	for _, tc := range []struct {
		dir core.Direction
		// the arrow that walks ON through the ring
		on string
	}{
		{core.DirLTR, "Right"},
		{core.DirRTL, "Left"},
	} {
		tv := newColumnsTree(60, 10)
		tv.SetDirection(tc.dir)
		tv.SetCurrentIndex(0)
		tv.ColumnByID("size").Editable = true
		tv.ColumnByID("kind").Editable = true
		tv.SetEditable(true) // the ring: key, size, kind

		if got := tv.enterTargetColumn(); got != treeKeyColumn {
			t.Fatalf("%v: the ring opens on %v, want the key column", tc.dir, got)
		}
		tv.HandleKeyPress(core.KeyPressEvent{Key: tc.on})
		if got := tv.enterTargetColumn(); got != tv.ColumnByID("size") {
			t.Errorf("%v: %s moved the target to %v, want the next column in the ring",
				tc.dir, tc.on, got)
		}
		if tv.rowEditing {
			t.Errorf("%v: walking the ring opened the editor", tc.dir)
		}
	}
}

// The header's stops stand in the order the columns run, so an arrow walks
// them the way it points too -- and the stop before the first is the bar.
func TestTheHeaderStopsWalkTowardsTheArrow(t *testing.T) {
	for _, tc := range []struct {
		dir      core.Direction
		on, back string
	}{
		{core.DirLTR, "Right", "Left"},
		{core.DirRTL, "Left", "Right"},
	} {
		tv := newColumnsTree(60, 10)
		tv.SetDirection(tc.dir)
		host := &recordingPopupController{}
		parent := NewPanel()
		parent.SetPopupController(host)
		tv.SetParent(parent)

		key := func(k string) { tv.HandleKeyPress(core.KeyPressEvent{Key: k}) }
		tv.HandleFocusIn()
		key("Return") // drill into the first caption
		if tv.headerZone != hzItems || tv.headerFocusIdx != 0 {
			t.Fatalf("%v: Enter left zone=%d idx=%d", tc.dir, tv.headerZone, tv.headerFocusIdx)
		}

		key(tc.on)
		if tv.headerFocusIdx != 1 {
			t.Errorf("%v: %s moved to stop %d, want the next one along",
				tc.dir, tc.on, tv.headerFocusIdx)
		}
		key(tc.back)
		if tv.headerFocusIdx != 0 {
			t.Errorf("%v: %s moved to stop %d, want back to the first",
				tc.dir, tc.back, tv.headerFocusIdx)
		}
		// Back past the first stop climbs out to the bar.
		key(tc.back)
		if tv.headerZone != hzBar {
			t.Errorf("%v: %s past the first stop left zone %d, want the bar",
				tc.dir, tc.back, tv.headerZone)
		}
	}
}

// The column-movement pair -- a left that never collapses -- names sides of
// the screen just as the plain arrows do, so it walks the target the same way.
func TestTheColumnMovementFollowsTheArrow(t *testing.T) {
	core.DefaultKeyRegistry().Bind("C-Left", core.CmdTrinketColumnLeft)
	core.DefaultKeyRegistry().Bind("C-Right", core.CmdTrinketColumnRight)
	t.Cleanup(func() {
		core.DefaultKeyRegistry().Bind("C-Left", core.CmdWindowMoveLeft)
		core.DefaultKeyRegistry().Bind("C-Right", core.CmdWindowMoveRight)
	})

	for _, tc := range []struct {
		dir core.Direction
		on  string // the key that walks ON through the ring
	}{
		{core.DirLTR, "C-Right"},
		{core.DirRTL, "C-Left"},
	} {
		tv := newColumnsTree(60, 10)
		tv.SetDirection(tc.dir)
		tv.SetCurrentIndex(0)
		tv.ColumnByID("size").Editable = true
		tv.SetEditable(true) // the ring: key, size

		tv.AbandonKeySequence()
		tv.HandleKeyPress(core.KeyPressEvent{Key: tc.on})
		if got := tv.enterTargetColumn(); got != tv.ColumnByID("size") {
			t.Errorf("%v: %s moved the target to %v, want the next column in the ring",
				tc.dir, tc.on, got)
		}
		if tv.rootItems[0].Expanded != true {
			t.Errorf("%v: %s collapsed the root; it must never touch the tree",
				tc.dir, tc.on)
		}
	}
}
