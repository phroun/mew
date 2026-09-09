package trinkets

// The painter and the tooltip must measure a cell against the same room. A
// cell one of them thinks fits and the other cuts draws an ellipsis and then
// offers nothing to expand.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// barelyCut builds a tree whose data column is wide enough for the word in it
// and whose ROOM inside that column is not -- the band a word cut to "co…"
// falls into, where a tooltip measuring the span says it fits while the
// painter cuts it.
//
// The band is only as wide as the padding and a column snaps to whole cells,
// so not every word can sit in one. The word is grown until one does.
func barelyCut(t *testing.T) (*TreeView, colSpan, *TreeItem, string) {
	t.Helper()
	px, err := raster.New(900, 200)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	core.SetTextMeasurer(px)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	tree := NewTreeView()
	if err := tree.AddColumn(&TreeColumn{ID: "kind", Caption: "Kind", Width: 400, MinWidth: 8, MaxWidth: 400}); err != nil {
		t.Fatal(err)
	}
	item := NewTreeItem("a host")
	tree.AddRootItem(item)
	tree.SetFitWidth(false)
	tree.SetKeyWidth(200)
	tree.SetBounds(core.UnitRect{Width: 600, Height: 120})

	metrics := tree.EffectiveCellMetrics()
	font := tree.EffectiveFont()
	for n := 2; n <= 24; n++ {
		word := strings.Repeat("code ", n)[:n]
		item.SetValue("kind", word)
		textW := font.MeasureTextIn(word, metrics)
		for w := textW; w <= textW+metrics.UnitsPerCellWidth*2; w++ {
			tree.columns[0].Width = w
			tree.columns[0].MaxWidth = w
			sp, ok := spanFor(tree, "kind")
			if !ok {
				continue
			}
			if room := tree.cellTextRoom(sp, item); room < textW && textW <= sp.w {
				return tree, sp, item, word
			}
		}
	}
	t.Fatal("no word lands between a column's width and the room inside it")
	return nil, colSpan{}, nil, ""
}

func spanFor(tree *TreeView, id string) (colSpan, bool) {
	for _, sp := range tree.columnLayout().spans {
		if id == "" && sp.col == nil {
			return sp, true
		}
		if sp.col != nil && sp.col.ID == id {
			return sp, true
		}
	}
	return colSpan{}, false
}

// A word cut to "co…" is still a word the reader cannot read.
func TestACellCutByASingleGlyphStillOffersItself(t *testing.T) {
	tree, sp, item, word := barelyCut(t)

	metrics := tree.EffectiveCellMetrics()
	shown := ellipsizeText(tree.EffectiveFont(), metrics, word, tree.cellTextRoom(sp, item))
	if !strings.HasSuffix(shown, core.Ellipsis) {
		t.Fatalf("the painter drew %q, so nothing is being tested", shown)
	}

	y := tree.headerHeight() + metrics.UnitsPerCellHeight/2
	text, _, ok := tree.TooltipAt(core.UnitPoint{X: sp.x + 2, Y: y})
	if !ok {
		t.Fatalf("the painter cut %q down to %q and the cell offered nothing", word, shown)
	}
	if text != word {
		t.Errorf("it offered %q", text)
	}
}

// The note stands where the TEXT does. In the key column the caption begins
// past the indent, the expander and the icon, so a note at the span's own
// edge would sit out to the left of the word it expands.
func TestTheKeyColumnsNoteStandsOnItsCaption(t *testing.T) {
	px, err := raster.New(900, 200)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	core.SetTextMeasurer(px)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	tree := NewTreeView()
	if err := tree.AddColumn(&TreeColumn{ID: "kind", Caption: "Kind", Width: 80}); err != nil {
		t.Fatal(err)
	}
	parent := NewTreeItem("a host with a very long name indeed, far too long")
	child := NewTreeItem("a child whose name is also much too long to show")
	parent.AddChild(child)
	parent.Expanded = true
	tree.AddRootItem(parent)
	tree.SetFitWidth(false)
	tree.SetKeyWidth(120)
	tree.SetBounds(core.UnitRect{Width: 300, Height: 120})

	sp, ok := spanFor(tree, "")
	if !ok {
		t.Fatal("the key column is not in the layout")
	}
	metrics := tree.EffectiveCellMetrics()
	rowY := func(i int) core.Unit {
		return tree.headerHeight() + core.Unit(i)*metrics.UnitsPerCellHeight + 1
	}

	_, at, ok := tree.TooltipAt(core.UnitPoint{X: sp.x + 4, Y: rowY(0)})
	if !ok {
		t.Fatal("the key cell offered nothing")
	}
	inset := tree.cellTextInset(sp, parent)
	if at.X != sp.x+inset {
		t.Errorf("the note stands at %d; the caption begins at %d", at.X, sp.x+inset)
	}

	// A child is indented further, and its note follows its caption.
	_, childAt, ok := tree.TooltipAt(core.UnitPoint{X: sp.x + 4, Y: rowY(1)})
	if !ok {
		t.Fatal("the child's cell offered nothing")
	}
	if childAt.X <= at.X {
		t.Errorf("the child's note stands at %d, no further in than its parent's %d",
			childAt.X, at.X)
	}
}
