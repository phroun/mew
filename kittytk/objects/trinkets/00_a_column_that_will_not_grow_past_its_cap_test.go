package trinkets

// A column told how wide it may get keeps to it, and the cells under it read
// as much as fits and say the rest on hover.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

const cappedCell = "sha256:d223d09d37f7a426b58eff67d4bb3b7bd5700000000000000"

// capped builds a tree whose one data column is asked for more room than it is
// allowed, and returns it laid out.
func capped(t *testing.T, want, max core.Unit) (*TreeView, colSpan) {
	t.Helper()
	tree := NewTreeView()
	if err := tree.AddColumn(&TreeColumn{
		ID: "identity", Caption: "Identity",
		Width: want, MinWidth: 24, MaxWidth: max,
	}); err != nil {
		t.Fatal(err)
	}
	item := NewTreeItem("host")
	item.SetValue("identity", cappedCell)
	tree.AddRootItem(item)
	tree.SetFitWidth(false)
	tree.SetBounds(core.UnitRect{Width: 900, Height: 120})

	for _, sp := range tree.columnLayout().spans {
		if sp.col != nil && sp.col.ID == "identity" {
			return tree, sp
		}
	}
	t.Fatal("the column is not in the layout")
	return nil, colSpan{}
}

// The cap holds: a column asked for more room than it may have takes what it
// may have.
func TestAColumnStopsAtItsCap(t *testing.T) {
	_, sp := capped(t, 600, 160)
	if sp.w > 160 {
		t.Errorf("a column capped at 160 took %d", sp.w)
	}
	// And an uncapped one is not held back by the cap that is not there.
	_, wide := capped(t, 600, 0)
	if wide.w <= sp.w {
		t.Errorf("without a cap the column took %d, no more than the capped %d", wide.w, sp.w)
	}
}

// What no longer fits in the capped column is cut, and the cut is what the
// cell offers when the pointer arrives.
func TestACappedColumnCutsItsCellsAndSaysSo(t *testing.T) {
	tree, sp := capped(t, 600, 160)

	metrics := tree.EffectiveCellMetrics()
	font := tree.EffectiveFont()
	shown := ellipsizeText(font, metrics, cappedCell, sp.w-metrics.UnitsPerCellWidth/2)
	if shown == cappedCell {
		t.Fatal("the whole value fit inside the cap, so nothing is being tested")
	}
	if !strings.HasSuffix(shown, core.Ellipsis) {
		t.Errorf("the cut cell reads %q, with nothing to say it was cut", shown)
	}

	y := tree.headerHeight() + metrics.UnitsPerCellHeight/2
	text, _, ok := tree.TooltipAt(core.UnitPoint{X: sp.x + 2, Y: y})
	if !ok || text != cappedCell {
		t.Errorf("the cut cell offers %q (ok=%v), not the whole of what it holds", text, ok)
	}
}
