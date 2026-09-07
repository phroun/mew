package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// An item in a list, and an item in a tree, are captions like any other: cut
// to fit first, then handed to the cell target in the order it stamps them.
func TestAListAndATreeReadTheirItemsOutInOrder(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	const shalom = "שלום"
	turned := string([]rune{'ם', 'ו', 'ל', 'ש'})

	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		// A list.
		px, err := raster.New(900, 400)
		if err != nil {
			t.Fatal(err)
		}
		ink := &cellInk{RenderBackend: px}
		form := NewPanel()
		form.SetDirection(dir)
		l := NewListView()
		form.AddChild(l)
		l.AddItem(NewListItem(shalom))
		l.SetBounds(core.UnitRect{Width: 40 * 8, Height: 5 * 16})
		l.Paint(core.NewPainter(ink))
		if !handedOver(ink.texts, turned) {
			t.Errorf("list %v: handed over %q, want the turned-over item %q", dir, ink.texts, turned)
		}

		// A tree.
		px, err = raster.New(900, 400)
		if err != nil {
			t.Fatal(err)
		}
		ink = &cellInk{RenderBackend: px}
		form = NewPanel()
		form.SetDirection(dir)
		tv := NewTreeView()
		form.AddChild(tv)
		tv.AddRootItem(NewTreeItem(shalom))
		tv.SetBounds(core.UnitRect{Width: 40 * 8, Height: 5 * 16})
		tv.Paint(core.NewPainter(ink))
		if !handedOver(ink.texts, turned) {
			t.Errorf("tree %v: handed over %q, want the turned-over item %q", dir, ink.texts, turned)
		}

		// A value under a heading is prepared in the COLUMN's direction, which
		// is what says how the values beneath that heading read -- not the
		// tree's, and not the value's own.
		px, err = raster.New(900, 400)
		if err != nil {
			t.Fatal(err)
		}
		ink = &cellInk{RenderBackend: px}
		form = NewPanel()
		form.SetDirection(dir)
		tv = NewTreeView()
		form.AddChild(tv)
		tv.AddColumn(NewTreeColumn("who", "Who", 20*8))
		item := NewTreeItem("row")
		item.SetValue("who", shalom)
		tv.AddRootItem(item)
		tv.SetBounds(core.UnitRect{Width: 60 * 8, Height: 5 * 16})
		tv.Paint(core.NewPainter(ink))
		if !handedOver(ink.texts, turned) {
			t.Errorf("tree column %v: handed over %q, want the turned-over value %q",
				dir, ink.texts, turned)
		}
	}

	// And it really is the COLUMN's direction that settles it, not the tree's.
	// A value of two runs comes out in a different order under each, so a
	// column that names one is answered even when the tree names the other.
	mixed := "abc " + shalom
	for _, colDir := range []core.Direction{core.DirLTR, core.DirRTL} {
		px, err := raster.New(900, 400)
		if err != nil {
			t.Fatal(err)
		}
		ink := &cellInk{RenderBackend: px}
		form := NewPanel()
		form.SetDirection(core.DirLTR)
		tv := NewTreeView()
		tv.SetDirection(core.DirLTR) // the tree reads one way ...
		form.AddChild(tv)
		col := NewTreeColumn("who", "Who", 30*8)
		col.Direction = colDir // ... and the column may read the other
		tv.AddColumn(col)
		item := NewTreeItem("row")
		item.SetValue("who", mixed)
		tv.AddRootItem(item)
		tv.SetBounds(core.UnitRect{Width: 70 * 8, Height: 5 * 16})
		tv.Paint(core.NewPainter(ink))

		want := core.CellRun(mixed, colDir)
		if !handedOver(ink.texts, want) {
			t.Errorf("a %v column in an LTR tree handed over %q, want the value read the "+
				"column's way (%q)", colDir, ink.texts, want)
		}
	}
}

func handedOver(texts []string, want string) bool {
	for _, s := range texts {
		if s == want {
			return true
		}
	}
	return false
}
