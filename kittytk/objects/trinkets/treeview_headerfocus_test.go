package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The internal header focus machine: focus lands on the header BAR as
// one stop; Enter drills into the columns; Tab cycles caption stops,
// then the chooser, then exits to the content zone; S-Tab walks back;
// Escape climbs out one level. The tree remains ONE trinket in the app
// tab order throughout - it only consumes Tab while moving internally.
func TestTreeHeaderFocusZones(t *testing.T) {
	tv := newColumnsTree(60, 10)
	tv.ColumnByID("size").Sortable = true
	host := &recordingPopupController{}
	parent := NewPanel()
	parent.SetPopupController(host)
	tv.SetParent(parent)

	key := func(k string) bool { return tv.HandleKeyPress(core.KeyPressEvent{Key: k}) }

	tv.HandleFocusIn()
	if tv.headerZone != hzBar {
		t.Fatalf("focus-in zone = %d, want hzBar", tv.headerZone)
	}

	// Enter drills in at the first stop (the key column caption).
	key("Enter")
	if tv.headerZone != hzItems || tv.headerFocusIdx != 0 {
		t.Fatalf("after Enter: zone=%d idx=%d", tv.headerZone, tv.headerFocusIdx)
	}

	// Tab to the Size caption; Enter cycles its sort (ascending).
	key("Tab")
	key("Enter")
	if sorted, by, desc := tv.Sorted(); !sorted || by != 0 || desc {
		t.Errorf("keyboard sort state = %v/%d/%v", sorted, by, desc)
	}
	// Enter again: descending; a third time: unsorted.
	key("Enter")
	if _, _, desc := tv.Sorted(); !desc {
		t.Errorf("second Enter should reverse")
	}
	key("Enter")
	if sorted, _, _ := tv.Sorted(); sorted {
		t.Errorf("third Enter should unsort")
	}

	// Tab through Kind to the chooser stop; Enter opens the menu.
	key("Tab") // -> kind
	key("Tab") // -> chooser stop (3 spans + chooser = idx 3)
	if tv.headerFocusIdx != 3 {
		t.Fatalf("chooser stop idx = %d, want 3", tv.headerFocusIdx)
	}
	key("Enter")
	if !tv.chooserOpen {
		t.Fatal("Enter on the chooser stop did not open the menu")
	}
	key("Escape") // close the menu (still in hzItems)
	if tv.chooserOpen {
		t.Fatal("Escape did not close the menu")
	}

	// Tab past the chooser lands in the content zone; the next Tab is
	// NOT consumed (released to the focus manager).
	key("Tab")
	if tv.headerZone != hzContent {
		t.Fatalf("zone after tabbing past chooser = %d, want hzContent", tv.headerZone)
	}
	if tv.HandleKeyPress(core.KeyPressEvent{Key: "Tab"}) {
		t.Error("content-zone Tab must be released to the focus manager")
	}

	// S-Tab from content backs into the header bar; S-Tab again
	// releases backward out of the trinket.
	if !tv.HandleKeyPress(core.KeyPressEvent{Key: "Tab", Modifiers: core.ShiftModifier}) {
		t.Error("content S-Tab should re-enter the header bar")
	}
	if tv.headerZone != hzBar {
		t.Fatalf("zone after content S-Tab = %d, want hzBar", tv.headerZone)
	}
	if tv.HandleKeyPress(core.KeyPressEvent{Key: "Tab", Modifiers: core.ShiftModifier}) {
		t.Error("bar S-Tab must be released to the focus manager")
	}

	// Escape from the bar drops to content; arrow keys then drive the
	// tree as always.
	tv.setHeaderZone(hzBar, 0)
	key("Escape")
	if tv.headerZone != hzContent {
		t.Errorf("Escape from bar should land in content")
	}
	before := tv.CurrentIndex()
	key("Down")
	if tv.CurrentIndex() != before+1 {
		t.Errorf("content Down did not move the selection")
	}
}

// Drilled into the header items, an initial S-Tab wraps around to the
// LAST stop (the chooser) instead of climbing back to the bar, so the
// machine keeps the focus; a Tab from there exits down into the rows.
// Left before the first stop keeps its climb back to the bar.
func TestTreeHeaderItemsShiftTabWraps(t *testing.T) {
	tv := newColumnsTree(60, 10)
	host := &recordingPopupController{}
	parent := NewPanel()
	parent.SetPopupController(host)
	tv.SetParent(parent)
	key := func(k string) { tv.HandleKeyPress(core.KeyPressEvent{Key: k}) }

	tv.HandleFocusIn() // -> hzBar
	key("Enter")       // -> hzItems, first stop
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Tab", Modifiers: core.ShiftModifier})
	last := tv.headerStopCount() - 1
	if tv.headerZone != hzItems || tv.headerFocusIdx != last {
		t.Fatalf("S-Tab at the first stop: zone=%d idx=%d, want items/%d",
			tv.headerZone, tv.headerFocusIdx, last)
	}
	key("Tab")
	if tv.headerZone != hzContent {
		t.Fatalf("Tab from the wrapped-to last stop should exit to content, zone=%d", tv.headerZone)
	}

	tv.setHeaderZone(hzItems, 0)
	key("Left")
	if tv.headerZone != hzBar {
		t.Errorf("Left before the first stop should climb to the bar, zone=%d", tv.headerZone)
	}
}
