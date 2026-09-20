package trinkets

// A cell being edited says nothing, and every other cell says what it always did.
//
// A note over the open editor competes with the editor's own selection: the pointer
// is in there dragging through the text, and a panel arriving under it is in the way
// of the very thing the hand is doing. So the edited cell is quiet.
//
// **It is the edited CELL and not the tree.** A note over any other cell is as
// useful while an editor is up as it was before -- the reader is looking at a value
// that was cut to fit its column, which has nothing to do with what is being typed
// three columns over.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// cutCells is a tree of two editable data columns whose values are both too long
// for them, so both offer a note and either may be the one being edited.
func cutCells(t *testing.T) (*TreeView, *TreeItem) {
	t.Helper()
	px, err := raster.New(900, 200)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	core.SetTextMeasurer(px)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	tree := NewTreeView()
	for _, id := range []string{"kind", "tags"} {
		if err := tree.AddColumn(&TreeColumn{
			ID: id, Caption: strings.ToUpper(id), Width: 60, MinWidth: 8, MaxWidth: 60,
			Editable: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	item := NewTreeItem("a row whose caption is also far too long to draw in full")
	item.SetValue("kind", "a kind name far too long for sixty units of column")
	item.SetValue("tags", "a tag list far too long for sixty units of column")
	tree.AddRootItem(item)
	tree.SetEditable(true)
	tree.SetFitWidth(false)
	tree.SetKeyWidth(120)
	tree.SetBounds(core.UnitRect{Width: 400, Height: 120})
	return tree, item
}

// noteOn is what the cell in one column offers, at a point inside it. An empty id
// is the key column.
func noteOn(t *testing.T, tree *TreeView, id string) (string, bool) {
	t.Helper()
	sp, ok := spanFor(tree, id)
	if !ok {
		t.Fatalf("column %q is not in the layout", id)
	}
	metrics := tree.EffectiveCellMetrics()
	// Well inside the cell's text, which for the key column is past the apparatus.
	x := sp.x + tree.cellTextInset(sp, tree.rowAt(0)) + 2
	text, _, got := tree.TooltipAt(core.UnitPoint{
		X: x, Y: tree.headerHeight() + metrics.UnitsPerCellHeight/2,
	})
	return text, got
}

func TestANoteDoesNotStandOverTheOpenEditor(t *testing.T) {
	tree, item := cutCells(t)

	// Before anything is edited, every cell offers its own cut value. Checked first,
	// because a test where nothing offered a note would pass for the wrong reason.
	for _, id := range []string{"", "kind", "tags"} {
		if _, ok := noteOn(t, tree, id); !ok {
			t.Fatalf("column %q offers nothing with no editor open", id)
		}
	}

	tree.beginCellEdit(item, tree.columns[0]) // the Kind cell
	if !tree.rowEditing {
		t.Fatal("the editor did not open")
	}

	if text, ok := noteOn(t, tree, "kind"); ok {
		t.Errorf("the edited cell offered %q over its own editor", text)
	}
	// And the two it is not covering are untouched.
	for _, id := range []string{"", "tags"} {
		if _, ok := noteOn(t, tree, id); !ok {
			t.Errorf("column %q went quiet, and it is not the one being edited", id)
		}
	}

	// Closing the editor gives the cell its note back: this is a state and not a
	// property of the cell.
	tree.endRowEdit(false)
	if tree.rowEditing {
		t.Fatal("the editor did not close")
	}
	if _, ok := noteOn(t, tree, "kind"); !ok {
		t.Error("the cell stayed quiet after its editor closed")
	}
}

// The KEY column is the one this is easiest to get wrong, because it is the nil span
// on the layout's side and a sentinel on the edit ring's. Asked with the predicate
// the editor places itself by, the two agree.
func TestTheEditedKeyCellIsQuietToo(t *testing.T) {
	tree, item := cutCells(t)

	tree.beginCellEdit(item, treeKeyColumn)
	if !tree.rowEditing {
		t.Fatal("the editor did not open")
	}
	if text, ok := noteOn(t, tree, ""); ok {
		t.Errorf("the edited key cell offered %q over its own editor", text)
	}
	// A data column is not the key column, whatever the sentinel matches.
	if _, ok := noteOn(t, tree, "kind"); !ok {
		t.Error("a data cell went quiet while the key cell was being edited")
	}
}

// A note over a cell of ANOTHER row is nobody's business but that row's, even in the
// column the editor is open in -- the editor covers one cell, not a column.
func TestAnotherRowsCellInTheEditedColumnStillSpeaks(t *testing.T) {
	tree, item := cutCells(t)
	other := NewTreeItem("another row whose caption is far too long to draw in full")
	other.SetValue("kind", "another kind name far too long for sixty units")
	other.SetValue("tags", "another tag list far too long for sixty units")
	tree.AddRootItem(other)
	tree.moved()

	tree.beginCellEdit(item, tree.columns[0])

	sp, ok := spanFor(tree, "kind")
	if !ok {
		t.Fatal("the Kind column is not in the layout")
	}
	metrics := tree.EffectiveCellMetrics()
	row1 := tree.headerHeight() + metrics.UnitsPerCellHeight + metrics.UnitsPerCellHeight/2
	if _, _, got := tree.TooltipAt(core.UnitPoint{X: sp.x + 2, Y: row1}); !got {
		t.Error("the second row's Kind cell went quiet; the editor is on the first row")
	}
}
