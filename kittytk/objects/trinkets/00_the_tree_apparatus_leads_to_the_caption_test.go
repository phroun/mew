package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// nestedTree is one root with one child, so there is an elbow to draw and an
// expander to press. Wide enough that the caption never ellipsizes.
func nestedTree(t *testing.T, dir core.Direction, smooth bool) *TreeView {
	t.Helper()
	tv := NewTreeView()
	tv.SetDirection(dir)
	tv.SetTreeLines(true)
	root := NewTreeItem("Folder")
	root.Expanded = true
	root.AddChild(NewTreeItem("file.png"))
	tv.AddRootItem(root)
	if smooth {
		parent := &gfxSurface{smooth: true}
		parent.Panel = *NewPanel()
		parent.Init(parent)
		tv.SetParent(parent)
	}
	tv.SetBounds(core.UnitRect{Width: 40 * 8, Height: 6 * 16})
	return tv
}

// The apparatus leads to the caption, so it starts at the edge the tree reads
// from: the indent, then the expander, then the text running away from both.
func TestTheApparatusRunsFromTheLeadingEdge(t *testing.T) {
	for _, tc := range []struct {
		dir core.Direction
		// the expander's distance from the edge the tree reads from
		from func(tv *TreeView, x core.Unit) core.Unit
	}{
		{core.DirLTR, func(_ *TreeView, x core.Unit) core.Unit { return x }},
		{core.DirRTL, func(tv *TreeView, x core.Unit) core.Unit {
			return tv.Bounds().Width - x - 8
		}},
	} {
		tv := nestedTree(t, tc.dir, false)
		ink := newInk(t)
		tv.Paint(core.NewPainter(ink))

		open, ok := ink.cellAt('▼')
		if !ok {
			t.Fatalf("%v: the expanded root drew no expander", tc.dir)
		}
		// One column of pad, and the root is at level 0.
		if got := tc.from(tv, open); got != cell {
			t.Errorf("%v: the root's expander stands %d from the leading edge, want %d",
				tc.dir, got, cell)
		}

		caption, ok := ink.textAt("Folder")
		if !ok {
			t.Fatalf("%v: the root drew no caption", tc.dir)
		}
		// The caption follows the expander along the run, so it is drawn
		// further from the leading edge than the expander is.
		if tc.from(tv, caption) <= tc.from(tv, open) {
			t.Errorf("%v: the caption at %d does not follow the expander at %d",
				tc.dir, caption, open)
		}
	}
}

// The elbow opens towards the caption it leads to, so its glyph turns with the
// tree. Nothing left over from the other side is drawn.
func TestTheElbowOpensTowardsTheCaption(t *testing.T) {
	for _, tc := range []struct {
		dir   core.Direction
		elbow rune
		stale rune
	}{
		{core.DirLTR, '└', '┘'},
		{core.DirRTL, '┘', '└'},
	} {
		tv := nestedTree(t, tc.dir, false)
		ink := newInk(t)
		tv.Paint(core.NewPainter(ink))

		if _, ok := ink.cellAt(tc.elbow); !ok {
			t.Errorf("%v: the child's elbow is not %q", tc.dir, tc.elbow)
		}
		if _, ok := ink.cellAt(tc.stale); ok {
			t.Errorf("%v: the tree drew %q, which opens the other way", tc.dir, tc.stale)
		}
	}
}

// A collapsed item's arrow points the way its children will come from, which
// is the way the tree runs.
func TestTheCollapsedArrowPointsAlongTheTree(t *testing.T) {
	for _, tc := range []struct {
		dir   core.Direction
		arrow rune
		stale rune
	}{
		{core.DirLTR, '▸', '◂'},
		{core.DirRTL, '◂', '▸'},
	} {
		tv := nestedTree(t, tc.dir, false)
		tv.rootItems[0].Expanded = false
		tv.rebuildFlatList()
		ink := newInk(t)
		tv.Paint(core.NewPainter(ink))

		if _, ok := ink.cellAt(tc.arrow); !ok {
			t.Errorf("%v: the collapsed root's arrow is not %q", tc.dir, tc.arrow)
		}
		if _, ok := ink.cellAt(tc.stale); ok {
			t.Errorf("%v: the tree drew %q, which points the other way", tc.dir, tc.stale)
		}
	}
}

// The mouse looks for the expander where the painter put it: one arithmetic,
// asked by both, so a press on the glyph toggles the item whichever way the
// tree reads -- and a press on the caption beside it does not.
func TestPressingTheExpanderWhereItWasDrawn(t *testing.T) {
	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		tv := nestedTree(t, dir, false)
		ink := newInk(t)
		tv.Paint(core.NewPainter(ink))
		open, ok := ink.cellAt('▼')
		if !ok {
			t.Fatalf("%v: the expanded root drew no expander", dir)
		}

		press := func(x core.Unit) {
			tv.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: x, Y: 0})
		}
		press(open + cell/2)
		if tv.rootItems[0].Expanded {
			t.Errorf("%v: pressing the expander at %d did not collapse the root", dir, open)
		}
		press(open + cell/2)
		if !tv.rootItems[0].Expanded {
			t.Errorf("%v: pressing the expander again did not expand the root", dir)
		}

		// The caption is not the expander.
		caption, _ := ink.textAt("Folder")
		press(caption + cell/2)
		if !tv.rootItems[0].Expanded {
			t.Errorf("%v: pressing the caption at %d collapsed the root", dir, caption)
		}
	}
}

// On a pixel surface the elbow is drawn as real strokes, and its horizontal
// leg reaches out towards the caption -- so it lies on the caption's side of
// the vertical stroke.
func TestTheElbowsLegReachesTowardsTheCaption(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	for _, tc := range []struct {
		dir core.Direction
		// whether the leg should lie past the stroke, along the run
		past bool
	}{
		{core.DirLTR, true},
		{core.DirRTL, false},
	} {
		px, err := raster.New(400, 200)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)
		rec := &stripeRecorder{RenderBackend: px}
		tv := nestedTree(t, tc.dir, true)
		tv.Paint(core.NewPainter(rec))

		// The vertical stroke is the 1px-wide stripe; the leg is the
		// 1px-tall one drawn in the same cell.
		var stroke, leg struct {
			x, w int
			ok   bool
		}
		for _, s := range rec.stripes {
			switch {
			case s.w == 1 && s.h > 1 && !stroke.ok:
				stroke.x, stroke.w, stroke.ok = s.x, s.w, true
			case s.h == 1 && s.w > 1 && !leg.ok:
				leg.x, leg.w, leg.ok = s.x, s.w, true
			}
		}
		if !stroke.ok || !leg.ok {
			t.Fatalf("%v: the elbow drew stroke=%v leg=%v", tc.dir, stroke.ok, leg.ok)
		}
		if past := leg.x >= stroke.x; past != tc.past {
			t.Errorf("%v: the leg runs [%d,%d) against a stroke at %d; it reaches the wrong way",
				tc.dir, leg.x, leg.x+leg.w, stroke.x)
		}
	}
}

// editableTree is a tree whose key column can be edited, with a data column
// beside it so the tree is in its multi-column presentation.
func editableTree(t *testing.T, dir core.Direction) *TreeView {
	t.Helper()
	tv := nestedTree(t, dir, false)
	tv.SetShowHeader(true)
	tv.SetEditable(true)
	col := NewTreeColumn("kind", "Kind", 10*cell)
	col.Editable = true
	tv.AddColumn(col)
	tv.SetBounds(core.UnitRect{Width: 40 * 8, Height: 6 * 16})
	return tv
}

// A caption is edited where it is READ, so the zone that opens the editor sits
// on the caption and the apparatus beside it opens nothing. Both are the same
// arithmetic the painter used.
func TestTheEditZoneSitsOnTheCaption(t *testing.T) {
	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		tv := editableTree(t, dir)
		ink := newInk(t)
		tv.Paint(core.NewPainter(ink))
		item := tv.rootItems[0]

		caption, ok := ink.textAt("Folder")
		if !ok {
			t.Fatalf("%v: the root drew no caption", dir)
		}
		if col := tv.editableColumnAt(caption+cell/2, item); col != treeKeyColumn {
			t.Errorf("%v: the caption at %d resolves to %v, want the key column",
				dir, caption, col)
		}
		expander, _ := ink.cellAt('▼')
		if col := tv.editableColumnAt(expander+cell/2, item); col != nil {
			t.Errorf("%v: the expander at %d resolves to %v, want nothing to edit",
				dir, expander, col)
		}
	}
}

// The editor stands over the caption it replaces: past the apparatus, never on
// top of the lines leading to it.
func TestTheEditorStandsOverTheCaption(t *testing.T) {
	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		tv := editableTree(t, dir)
		ink := newInk(t)
		tv.Paint(core.NewPainter(ink))
		expander, ok := ink.cellAt('▼')
		if !ok {
			t.Fatalf("%v: the expanded root drew no expander", dir)
		}

		tv.SetCurrentIndex(0)
		tv.beginCellEdit(tv.rootItems[0], treeKeyColumn)
		r, ok := tv.editorRect()
		if !ok {
			t.Fatalf("%v: the key column's editor has no place to stand", dir)
		}
		if r.Width <= 0 {
			t.Errorf("%v: the editor is %d units wide", dir, r.Width)
		}
		if r.X <= expander && expander < r.X+r.Width {
			t.Errorf("%v: the editor [%d,%d) covers the expander at %d",
				dir, r.X, r.X+r.Width, expander)
		}
		caption, _ := ink.textAt("Folder")
		if caption < r.X || caption >= r.X+r.Width {
			t.Errorf("%v: the editor [%d,%d) does not stand over the caption at %d",
				dir, r.X, r.X+r.Width, caption)
		}
	}
}
