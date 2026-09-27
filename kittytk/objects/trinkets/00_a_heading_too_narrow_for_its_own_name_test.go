package trinkets

// A column narrow enough to cut its heading is exactly the one whose heading
// is worth asking about: it is the only thing saying what the cells under it
// are.

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// narrowHeadings is a tree whose one data column is far too narrow for the
// name over it, and whose key column is not.
func narrowHeadings(t *testing.T) *TreeView {
	t.Helper()
	px, err := raster.New(900, 200)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	core.SetTextMeasurer(px)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	tree := NewTreeView()
	tree.SetShowHeader(true)
	tree.SetKeyCaption("Name")
	if err := tree.AddColumn(&TreeColumn{
		ID: "seen", Caption: "Last Seen Speaking Protocol",
		Width: 40, MinWidth: 16, MaxWidth: 40,
	}); err != nil {
		t.Fatal(err)
	}
	tree.AddRootItem(NewTreeItem("a host"))
	tree.SetFitWidth(false)
	tree.SetKeyWidth(200)
	tree.SetBounds(core.UnitRect{Width: 400, Height: 120})
	return tree
}

// headingMid is the middle of the header cell over the named column.
func headingMid(t *testing.T, tree *TreeView, id string) core.UnitPoint {
	t.Helper()
	lay := tree.columnLayout()
	for _, sp := range lay.spans {
		named := sp.col == nil && id == ""
		if sp.col != nil {
			named = sp.col.ID == id
		}
		if !named {
			continue
		}
		clip, ok := lay.spanClip(sp, tree.headerHeight())
		if !ok {
			t.Fatalf("the column %q is not on the screen", id)
		}
		return core.UnitPoint{X: clip.X + clip.Width/2, Y: tree.headerHeight() / 2}
	}
	t.Fatalf("no column %q", id)
	return core.UnitPoint{}
}

func TestAHeadingTooNarrowForItsNameOffersIt(t *testing.T) {
	tree := narrowHeadings(t)

	text, at, ok := tree.TooltipAt(headingMid(t, tree, "seen"))
	if !ok {
		t.Fatal("a heading cut to a few units offered nothing")
	}
	if text != "Last Seen Speaking Protocol" {
		t.Errorf("it offered %q, not the heading", text)
	}
	if at.Height != tree.headerHeight() {
		t.Errorf("it named a rect %d tall rather than the header row", at.Height)
	}

	// A heading with room for itself is not missing anything.
	if _, _, ok := tree.TooltipAt(headingMid(t, tree, "")); ok {
		t.Error("a heading that fits offered itself anyway")
	}
}
