package trinkets

import (
	"fmt"
	"testing"

	"github.com/phroun/kittytk/core"
)

// visualCaptions returns the flattened row captions in display order.
func visualCaptions(tv *TreeView) []string {
	out := make([]string, len(tv.flatList))
	for i, it := range tv.flatList {
		out[i] = it.Text
	}
	return out
}

func newSortableTree() *TreeView {
	tv := NewTreeView()
	tv.SetShowHeader(true)
	size := NewTreeColumn("size", "Size", 10)
	size.Sortable = true
	tv.AddColumn(size)

	for _, spec := range []struct{ name, size string }{
		{"banana", "20"},
		{"Apple", "30"},
		{"cherry", "10"},
	} {
		it := NewTreeItem(spec.name)
		it.SetValue("size", spec.size)
		tv.AddRootItem(it)
	}
	// A folder with children: children must sort within it, staying
	// grouped under their parent.
	folder := NewTreeItem("dir")
	folder.Expanded = true
	for _, n := range []string{"zeta", "alpha"} {
		folder.AddChild(NewTreeItem(n))
	}
	tv.AddRootItem(folder)
	tv.SetBounds(core.UnitRect{Width: 480, Height: 160})
	return tv
}

// Visual sorting reorders the ROW LIST only: ascending/descending by
// key (case-insensitive), children sorted within their parent, and
// the logical item order untouched throughout.
func TestTreeVisualSortByKey(t *testing.T) {
	tv := newSortableTree()

	logical := []string{"banana", "Apple", "cherry", "dir"}
	for i, it := range tv.RootItems() {
		if it.Text != logical[i] {
			t.Fatalf("precondition: logical[%d]=%q", i, it.Text)
		}
	}

	tv.SetSorted(true, -1, false)
	want := []string{"Apple", "banana", "cherry", "dir", "alpha", "zeta"}
	if got := visualCaptions(tv); !equalStrings(got, want) {
		t.Errorf("ascending visual order = %v, want %v", got, want)
	}

	tv.SetSorted(true, -1, true)
	want = []string{"dir", "zeta", "alpha", "cherry", "banana", "Apple"}
	if got := visualCaptions(tv); !equalStrings(got, want) {
		t.Errorf("descending visual order = %v, want %v", got, want)
	}
	// Note: children still sit under their parent, sorted descending
	// within it - the hierarchy is never flattened away.

	// The LOGICAL order never moved.
	for i, it := range tv.RootItems() {
		if it.Text != logical[i] {
			t.Errorf("logical order disturbed: [%d]=%q, want %q", i, it.Text, logical[i])
		}
	}

	// Turning sorting off restores the app's order visually.
	tv.SetSorted(false, -1, false)
	want = []string{"banana", "Apple", "cherry", "dir", "zeta", "alpha"}
	if got := visualCaptions(tv); !equalStrings(got, want) {
		t.Errorf("unsorted visual order = %v, want %v", got, want)
	}
}

// Sorting by a data column orders by cell values.
func TestTreeVisualSortByColumn(t *testing.T) {
	tv := newSortableTree()
	tv.SetSorted(true, 0, false) // by "size": 10, 20, 30, "" (dir)
	want := []string{"dir", "zeta", "alpha", "cherry", "banana", "Apple"}
	if got := visualCaptions(tv); !equalStrings(got, want) {
		t.Errorf("by-column visual order = %v, want %v", got, want)
	}
}

// Selection tracks the ITEM across resorts: the selected item's visual
// index changes but its identity does not, so an app reading the
// selection is none-the-wiser that sorting happened.
func TestTreeVisualSortKeepsSelectedItem(t *testing.T) {
	tv := newSortableTree()
	cherry := tv.RootItems()[2]
	tv.SetCurrentItem(cherry)
	if tv.CurrentItem() != cherry {
		t.Fatal("precondition: cherry selected")
	}

	tv.SetSorted(true, -1, false) // cherry moves to visual row 2
	if tv.CurrentItem() != cherry {
		t.Errorf("selection lost the item across resort: %v", tv.CurrentItem())
	}
	if tv.CurrentIndex() != 2 {
		t.Errorf("cherry's visual index = %d, want 2", tv.CurrentIndex())
	}
	tv.SetSorted(true, -1, true) // and back up under descending
	if tv.CurrentItem() != cherry {
		t.Errorf("selection lost the item on direction flip")
	}
}

// A header click on a sortable column resorts the view by itself - the
// trinket owns the reorder; the callback is notification, not a duty.
func TestTreeHeaderClickResortsBuiltIn(t *testing.T) {
	tv := newSortableTree()
	lay := tv.columnLayout()
	// Click the key column's caption: ascending by name.
	tv.HandleMousePress(core.MousePressEvent{X: lay.spans[0].x + 8, Y: 4, Button: core.LeftButton})
	if got := visualCaptions(tv)[0]; got != "Apple" {
		t.Errorf("first visual row after header click = %q, want Apple", got)
	}
	// Second click: descending.
	tv.HandleMousePress(core.MousePressEvent{X: lay.spans[0].x + 8, Y: 4, Button: core.LeftButton})
	if got := visualCaptions(tv)[0]; got != "dir" {
		t.Errorf("first visual row after second click = %q, want dir", got)
	}
}

// A resort (sort toggles, value-change reorders) follows the selected
// item vertically ONLY when it was in view beforehand; a viewport the
// user scrolled away from the selection stays where they put it.
func TestTreeResortFollowsVisibleSelection(t *testing.T) {
	tv := NewTreeView()
	tv.SetShowHeader(true)
	tv.AddColumn(NewTreeColumn("size", "Size", 10))
	for i := 0; i < 40; i++ {
		tv.AddRootItem(NewTreeItem(fmt.Sprintf("item%02d", i)))
	}
	tv.SetBounds(core.UnitRect{Width: 480, Height: 160})
	tv.SetCurrentIndex(0) // item00 selected, visible at the top

	// Descending: item00 drops to the last visual row; the viewport
	// follows because the selection WAS visible.
	tv.SetSorted(true, -1, true)
	vc := tv.visibleCount()
	if tv.currentIndex != 39 {
		t.Fatalf("selection index after sort = %d, want 39", tv.currentIndex)
	}
	if tv.currentIndex < tv.scrollOffset || tv.currentIndex >= tv.scrollOffset+vc {
		t.Errorf("viewport did not follow the visible selection: offset=%d idx=%d vc=%d",
			tv.scrollOffset, tv.currentIndex, vc)
	}

	// The user scrolls the selection OUT of view; a resort must not
	// yank the viewport back to it.
	tv.scrollOffset = 0
	tv.SetSorted(true, -1, true) // re-sort: selection stays at 39
	if tv.scrollOffset != 0 {
		t.Errorf("resort moved a user-scrolled viewport: offset=%d, want 0", tv.scrollOffset)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
