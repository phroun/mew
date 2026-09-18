package trinkets

// A tree's visible rows come out of a sequence, whether the rows are its own or
// somebody else's.
//
// Everything checked here was already true of a tree that walked its own items,
// and goes on being true now that it does not. That is the point: a tree given
// no source MAKES one, so there is no second code path for the plain case and
// every existing example goes on meaning exactly what it meant.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/phroun/serval"
)

// rowsOf is the flattened list as `text/depth`, which is what a reader of a tree
// actually sees.
func rowsOf(tv *TreeView) string {
	out := make([]string, len(tv.flatList))
	for i, it := range tv.flatList {
		out[i] = fmt.Sprintf("%s/%d", it.Text, it.Level())
	}
	return strings.Join(out, " ")
}

// kinTree is a two-level tree with a leaf beside a folder, which is enough shape
// for pre-order to be a claim rather than a coincidence.
//
//	alpha
//	  beta
//	    delta
//	gamma
func kinTree() (*TreeView, map[string]*TreeItem) {
	tv := NewTreeView()
	by := map[string]*TreeItem{}
	mk := func(name string, under *TreeItem) *TreeItem {
		it := NewTreeItem(name)
		by[name] = it
		if under == nil {
			tv.AddRootItem(it)
		} else {
			under.AddChild(it)
		}
		return it
	}
	alpha := mk("alpha", nil)
	beta := mk("beta", alpha)
	mk("delta", beta)
	mk("gamma", nil)
	tv.rebuildFlatList()
	return tv, by
}

// Nothing expanded: the top level, and nothing deeper.
func TestATreeStartsAtItsRoots(t *testing.T) {
	tv, _ := kinTree()
	if got, want := rowsOf(tv), "alpha/0 gamma/0"; got != want {
		t.Errorf("the tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// **The field is the authoring surface**, which eight test files and the wire's
// `expanded=` property depend on: setting it and rebuilding shows the children.
func TestTheExpandedFieldStillMeansWhatItMeant(t *testing.T) {
	tv, by := kinTree()
	by["alpha"].Expanded = true
	tv.rebuildFlatList()
	if got, want := rowsOf(tv), "alpha/0 beta/1 gamma/0"; got != want {
		t.Errorf("with alpha expanded the tree reads\n  %s\nwant\n  %s", got, want)
	}

	by["beta"].Expanded = true
	tv.rebuildFlatList()
	if got, want := rowsOf(tv), "alpha/0 beta/1 delta/2 gamma/0"; got != want {
		t.Errorf("with both expanded the tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// **The rows that come back are the very items the caller handed in.**
// Everything reading flatList compares POINTERS -- selection restores itself by
// pointer, the row editor holds one, the columns paint from one -- so a row that
// crossed a data layer as a record has to lead back to the same item.
func TestTheRowsAreTheCallersOwnItems(t *testing.T) {
	tv, by := kinTree()
	by["alpha"].Expanded = true
	tv.rebuildFlatList()

	if tv.flatList[0] != by["alpha"] {
		t.Error("the first row is not the item that was added")
	}
	if tv.flatList[1] != by["beta"] {
		t.Error("the child row is not the item that was added")
	}
	// And the data hung off it survives, which is what an application uses the
	// tree for at all.
	by["beta"].Data = "something of the application's"
	tv.rebuildFlatList()
	if tv.flatList[1].Data != "something of the application's" {
		t.Errorf("the row's Data is %v", tv.flatList[1].Data)
	}
}

// **Sorting stays where it is.** A tree sorts each level by its own columns, and
// the made source is not asked to reproduce that -- each row carries `seq`, and
// serval preserves the order it was handed.
func TestEachLevelKeepsTheTreesOwnSortOrder(t *testing.T) {
	tv := newSortableTree()
	tv.SetSortLevels(SortLevel{By: 0})
	before := visualCaptions(tv)

	tv.rebuildFlatList()
	if after := visualCaptions(tv); !sameRun(before, after) {
		t.Errorf("rebuilding reordered the rows:\n  %v\n  %v", before, after)
	}

	// And the order really is the TREE's rather than the source's. Sorted by
	// size ascending it is neither the order the items were added in nor the
	// order their keys were allocated in -- `dir` leads because it has no size
	// at all, which is the tree's own comparison and nothing a data layer was
	// told about. The folder's children stay grouped directly under it, which is
	// the pre-order.
	if got, want := strings.Join(before, " "),
		"dir zeta alpha cherry banana Apple"; got != want {
		t.Errorf("sorted by size the tree reads\n  %s\nwant\n  %s", got, want)
	}
	if got, want := strings.Join(visualCaptions(newSortableTree()), " "),
		"banana Apple cherry dir zeta alpha"; got != want {
		t.Errorf("unsorted the tree reads\n  %s\nwant the order they were added\n  %s",
			got, want)
	}
}

func sameRun(a, b []string) bool {
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

// A leaf does not expand however the field is set, which is what IsLeaf has
// always meant and is now also what the marks say.
func TestALeafDoesNotExpand(t *testing.T) {
	tv, by := kinTree()
	by["gamma"].Expanded = true // a leaf, so this says nothing
	tv.rebuildFlatList()
	if got, want := rowsOf(tv), "alpha/0 gamma/0"; got != want {
		t.Errorf("the tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// --- what the sequence buys ---------------------------------------------

// **The visible rows are a countable sequence now**, which is what a scrollbar
// wants and what the old walk could only answer by having already walked. It is
// exact, because the source holds its records.
func TestTheVisibleRowsAreACountableSequence(t *testing.T) {
	tv, by := kinTree()
	set := tv.sequence()
	if set == nil {
		t.Fatal("the tree stated no sequence")
	}
	if got := serval.CountOf(set); got != serval.Exactly(2) {
		t.Errorf("it counts %v visible rows, want two", got)
	}

	by["alpha"].Expanded = true
	by["beta"].Expanded = true
	tv.rebuildFlatList()
	if got := serval.CountOf(tv.sequence()); got != serval.Exactly(4) {
		t.Errorf("expanded it counts %v, want four", got)
	}
}

// And a scope of it reads from a position, which is a thumb dragged -- over a
// tree, which is the whole reason the flattening moved.
func TestAScopeReadsTheTreeFromAPosition(t *testing.T) {
	tv, by := kinTree()
	by["alpha"].Expanded = true
	by["beta"].Expanded = true
	tv.rebuildFlatList()

	var out treeRows
	if err := tv.sequence().Read(&serval.Scope{From: 2, Count: 2}, &out); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(out.ids))
	for _, id := range out.ids {
		got = append(got, tv.byID[objectIDOf(id)].Text)
	}
	if want := "delta gamma"; strings.Join(got, " ") != want {
		t.Errorf("from the third row it reads %q, want %q", strings.Join(got, " "), want)
	}
}

// An empty tree states an empty sequence rather than failing, which is ordinary
// and is what every reader here treats it as.
func TestAnEmptyTreeIsOrdinary(t *testing.T) {
	tv := NewTreeView()
	tv.rebuildFlatList()
	if len(tv.flatList) != 0 {
		t.Errorf("an empty tree drew %d rows", len(tv.flatList))
	}
	if got := serval.CountOf(tv.sequence()); got != serval.Exactly(0) {
		t.Errorf("it counts %v rows", got)
	}
}
