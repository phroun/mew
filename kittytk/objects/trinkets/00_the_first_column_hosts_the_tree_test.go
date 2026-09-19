package trinkets

// With the key column HIDDEN, the first visible data column hosts the tree.
//
// Nesting has to stay visible with the key off, so that column carries the
// indent, the twisty, the connector lines and the icon, and its value begins past
// all of it. That is what `treeHostColumn` is for, and five places consult it.
//
// **The invariant is that they all measure the same cell the same way.** The
// painter draws the value at one inset, the tooltip offers a cut word at another,
// the editor opens over a third, and the mouse resolves a click against a fourth
// -- and every one of those is the same number or the cell is wrong in a way that
// only shows on screen. This file pins it.

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// hostingTree is a two-level tree whose key column is OFF, so its Kind column is
// what hosts the apparatus.
func hostingTree(t *testing.T) (*TreeView, *TreeItem, *TreeItem) {
	t.Helper()
	tree := NewTreeView()
	if err := tree.AddColumn(&TreeColumn{ID: "kind", Caption: "Kind", Width: 200}); err != nil {
		t.Fatal(err)
	}
	parent := NewTreeItem("a parent")
	child := NewTreeItem("a child")
	parent.AddChild(child)
	parent.Expanded = true
	tree.AddRootItem(parent)
	parent.SetValue("kind", "folder")
	child.SetValue("kind", "file")

	tree.SetTreeLines(true)
	tree.SetShowKey(false)
	tree.SetFitWidth(false)
	tree.SetBounds(core.UnitRect{Width: 300, Height: 120})
	return tree, parent, child
}

// **A hosting data column's value begins past the apparatus**, exactly as the key
// column's caption does. Reading the inset as an ordinary data cell's half-pitch
// pad draws the value ON TOP of the indent and the twisty, which is the column
// painting as though it hosted nothing.
func TestAHostingColumnsValueBeginsPastTheApparatus(t *testing.T) {
	tree, parent, child := hostingTree(t)
	sp, ok := spanFor(tree, "kind")
	if !ok {
		t.Fatal("the Kind column is not in the layout")
	}
	if tree.treeHostColumn() != sp.col {
		t.Fatal("the Kind column does not host the tree")
	}

	pad := tree.EffectiveCellMetrics().UnitsPerCellWidth / 2
	for _, item := range []*TreeItem{parent, child} {
		got := tree.cellTextInset(sp, item)
		want := tree.treeCellTextInset(item)
		if got != want {
			t.Errorf("%q begins %d into the span, want %d -- past the indent, "+
				"the twisty and the icon", item.Text, got, want)
		}
		if got <= pad {
			t.Errorf("%q begins %d in, which is no further than an ordinary "+
				"data cell's pad of %d", item.Text, got, pad)
		}
	}
	// And the child stands further in than its parent, which is the whole of what
	// makes the nesting visible with the key column off.
	if tree.cellTextInset(sp, child) <= tree.cellTextInset(sp, parent) {
		t.Error("the child's value begins no further in than its parent's")
	}
}

// **The painter and the editor measure the same cell the same way.** The editor
// already opened past the apparatus; the painted value did not, so a click landed
// the editor somewhere the text had never been.
func TestAHostingColumnsPainterAndEditorAgree(t *testing.T) {
	tree, parent, _ := hostingTree(t)
	sp, ok := spanFor(tree, "kind")
	if !ok {
		t.Fatal("the Kind column is not in the layout")
	}
	inset := tree.cellTextInset(sp, parent)
	zoneX, _ := tree.treeCellEditZone(sp, parent)
	if want := tree.treeRunX(sp, inset, 0); zoneX != want {
		t.Errorf("the editor opens at %d and the value is drawn at %d", zoneX, want)
	}
}

// A HEADING stands behind no indent, whichever column it heads: a caption is not
// a row and has no level to be at.
func TestAHostingColumnsHeadingIsPaddedLikeAnyOther(t *testing.T) {
	tree, _, _ := hostingTree(t)
	sp, ok := spanFor(tree, "kind")
	if !ok {
		t.Fatal("the Kind column is not in the layout")
	}
	pad := tree.EffectiveCellMetrics().UnitsPerCellWidth / 2
	if got := tree.cellTextInset(sp, nil); got != pad {
		t.Errorf("the heading begins %d into the span, want the pad of %d", got, pad)
	}
}

// And with the key column SHOWN, the first data column hosts nothing and is
// padded like any other -- which is what keeps this from becoming a rule about
// first columns.
func TestWithTheKeyShownTheFirstDataColumnHostsNothing(t *testing.T) {
	tree, parent, _ := hostingTree(t)
	tree.SetShowKey(true)
	tree.SetBounds(core.UnitRect{Width: 300, Height: 120})

	sp, ok := spanFor(tree, "kind")
	if !ok {
		t.Fatal("the Kind column is not in the layout")
	}
	if tree.treeHostColumn() != nil {
		t.Fatal("a data column hosts the tree while the key column is shown")
	}
	pad := tree.EffectiveCellMetrics().UnitsPerCellWidth / 2
	if got := tree.cellTextInset(sp, parent); got != pad {
		t.Errorf("the Kind cell begins %d in, want the pad of %d", got, pad)
	}
}
