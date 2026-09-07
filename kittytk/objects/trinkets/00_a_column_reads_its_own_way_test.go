package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

const (
	english = "Address"
	hebrew  = "שלום"
	figure  = "1972"
)

// mixedTree holds one data column beside the key, so a test can set that
// column's direction and alignment and ask where a cell's text begins.
func mixedTree(treeDir core.Direction) (*TreeView, *TreeColumn) {
	tv := NewTreeView()
	tv.SetShowHeader(true)
	tv.SetDirection(treeDir)
	col := NewTreeColumn("word", "Word", 20*cell)
	tv.AddColumn(col)
	for _, s := range []string{english, hebrew, figure} {
		it := NewTreeItem(s)
		it.SetValue("word", s)
		tv.AddRootItem(it)
	}
	tv.SetBounds(core.UnitRect{Width: 60 * 8, Height: 10 * 16})
	return tv, col
}

// A column's direction is its CONTENT's, and it takes the tree's when it says
// nothing -- so a column says something here only when it differs from the
// form around it.
func TestAColumnTakesTheTreesDirectionUnlessItSaysOtherwise(t *testing.T) {
	tv, col := mixedTree(core.DirLTR)
	if got := tv.colDirection(col); got != core.DirLTR {
		t.Errorf("a column in a left-to-right tree reads %v, want %v", got, core.DirLTR)
	}

	col.Direction = core.DirRTL
	if got := tv.colDirection(col); got != core.DirRTL {
		t.Errorf("a column that named its own direction reads %v, want %v", got, core.DirRTL)
	}

	// And the tree's own turns the ones that said nothing.
	col.Direction = core.DirInherit
	tv.SetDirection(core.DirRTL)
	if got := tv.colDirection(col); got != core.DirRTL {
		t.Errorf("a column inheriting a right-to-left tree reads %v, want %v", got, core.DirRTL)
	}
}

// textbegin is asked of each CELL's own text, so one column holds Hebrew and
// English together and reads each the way its own script does.
func TestTextBeginIsAskedOfEachCell(t *testing.T) {
	tv, col := mixedTree(core.DirLTR)
	col.Align = core.AlignTextBegin

	if got := tv.cellTextSide(col, english); got != core.SideLeft {
		t.Errorf("an English cell begins on the %v, want the left", got)
	}
	if got := tv.cellTextSide(col, hebrew); got != core.SideRight {
		t.Errorf("a Hebrew cell begins on the %v, want the right", got)
	}
	// Nothing strongly directional: the column's own direction answers.
	if got := tv.cellTextSide(col, figure); got != core.SideLeft {
		t.Errorf("a figure in a left-to-right column begins on the %v, want the left", got)
	}
	col.Direction = core.DirRTL
	if got := tv.cellTextSide(col, figure); got != core.SideRight {
		t.Errorf("a figure in a right-to-left column begins on the %v, want the right", got)
	}
	// The English cell is unmoved by the column turning: its own script is
	// what textbegin asks about.
	if got := tv.cellTextSide(col, english); got != core.SideLeft {
		t.Errorf("an English cell in a right-to-left column begins on the %v, want the left", got)
	}
}

// layoutbegin is asked of the COLUMN, so every cell in it matches whatever the
// column reads -- which is what a column of one language wants.
func TestLayoutBeginIsAskedOfTheColumn(t *testing.T) {
	tv, col := mixedTree(core.DirLTR)
	col.Align = core.AlignLayoutBegin
	col.Direction = core.DirRTL

	for _, text := range []string{english, hebrew, figure} {
		if got := tv.cellTextSide(col, text); got != core.SideRight {
			t.Errorf("%q under layoutbegin in a right-to-left column begins on the %v, want the right",
				text, got)
		}
	}
	col.Align = core.AlignLayoutEnd
	for _, text := range []string{english, hebrew, figure} {
		if got := tv.cellTextSide(col, text); got != core.SideLeft {
			t.Errorf("%q under layoutend in a right-to-left column ends on the %v, want the left",
				text, got)
		}
	}
}

// The optical pair names a side of the screen and no direction moves it.
func TestTheOpticalPairPinsAColumnsCells(t *testing.T) {
	tv, col := mixedTree(core.DirRTL)
	col.Direction = core.DirRTL
	col.Align = core.AlignOpticalLeft
	for _, text := range []string{english, hebrew, figure} {
		if got := tv.cellTextSide(col, text); got != core.SideLeft {
			t.Errorf("%q under opticalleft sits on the %v", text, got)
		}
	}
	col.Align = core.AlignOpticalRight
	for _, text := range []string{english, hebrew, figure} {
		if got := tv.cellTextSide(col, text); got != core.SideRight {
			t.Errorf("%q under opticalright sits on the %v", text, got)
		}
	}
}

// The column carrying the tree apparatus is not asked either question. Its
// caption has to start where the lines leading to it stop, so it reads the
// TREE's way whatever it says for itself -- which is the same rule that
// already made a right-aligned Size column go left when the key was hidden.
func TestTheColumnHostingTheTreeReadsTheTreesWay(t *testing.T) {
	tv, col := mixedTree(core.DirRTL)
	col.Direction = core.DirLTR
	col.Align = core.AlignOpticalRight

	// With the key column shown, this is an ordinary data column.
	if got := tv.cellTextSide(col, english); got != core.SideRight {
		t.Errorf("beside the key column it sits on the %v, want its own opticalright", got)
	}

	// Hide the key and it becomes the host: the tree's direction, at the
	// tree's leading edge.
	tv.SetShowKey(false)
	if tv.treeHostColumn() != col {
		t.Fatal("hiding the key did not make this column the host")
	}
	if got := tv.colDirection(col); got != core.DirRTL {
		t.Errorf("the host column reads %v, want the tree's %v", got, core.DirRTL)
	}
	if got := tv.cellTextSide(col, english); got != core.SideRight {
		t.Errorf("the host column's text begins on the %v, want the tree's leading edge", got)
	}
	tv.SetDirection(core.DirLTR)
	if got := tv.cellTextSide(col, english); got != core.SideLeft {
		t.Errorf("in a left-to-right tree the host column begins on the %v, want the left", got)
	}
}
