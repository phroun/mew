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

// textnatural is asked of each CELL's own text, so one column holds Hebrew and
// English together and reads each the way its own script does.
func TestTextBeginIsAskedOfEachCell(t *testing.T) {
	tv, col := mixedTree(core.DirLTR)
	col.Align = core.AlignTextNatural

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
	// what textnatural asks about.
	if got := tv.cellTextSide(col, english); got != core.SideLeft {
		t.Errorf("an English cell in a right-to-left column begins on the %v, want the left", got)
	}
}

// layoutnatural is asked of the COLUMN, so every cell in it matches whatever the
// column reads -- which is what a column of one language wants.
func TestLayoutBeginIsAskedOfTheColumn(t *testing.T) {
	tv, col := mixedTree(core.DirLTR)
	col.Align = core.AlignLayoutNatural
	col.Direction = core.DirRTL

	for _, text := range []string{english, hebrew, figure} {
		if got := tv.cellTextSide(col, text); got != core.SideRight {
			t.Errorf("%q under layoutnatural in a right-to-left column begins on the %v, want the right",
				text, got)
		}
	}
	col.Align = core.AlignLayoutOpposite
	for _, text := range []string{english, hebrew, figure} {
		if got := tv.cellTextSide(col, text); got != core.SideLeft {
			t.Errorf("%q under layoutopposite in a right-to-left column ends on the %v, want the left",
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

// A choice cell's arrow belongs to the CELL, so it stands at the end the
// column's own text runs to -- and the value keeps only what is left before
// it, never the room the arrow took.
func TestTheChoiceArrowFollowsItsColumn(t *testing.T) {
	// A value that all but fills its cell, so text drawn into the arrow's
	// room would run right over it.
	const long = "PNG image (compressed)"

	place := func(colDir core.Direction) (arrow core.Unit, value struct {
		x, w core.Unit
	}, span colSpan) {
		tv := NewTreeView()
		tv.SetShowHeader(true)
		kind := NewTreeColumn("kind", "Kind", 22*cell)
		kind.Editable = true
		kind.Direction = colDir
		kind.Enum = []TreeEnumOption{{Key: "png", Value: long}, {Key: "txt", Value: "Text"}}
		kind.EnumStore = "key"
		tv.AddColumn(kind)
		it := NewTreeItem("alpha")
		it.SetValue("kind", "png")
		tv.AddRootItem(it)
		tv.SetBounds(core.UnitRect{Width: 60 * 8, Height: 10 * 16})
		tv.SetCurrentIndex(0)
		tv.SetFocus()
		tv.headerZone = hzContent
		tv.editLastCol = kind

		ink := newInk(t)
		tv.Paint(core.NewPainter(ink))
		a, ok := ink.textAt(choiceArrowGlyph)
		if !ok {
			t.Fatalf("%v: the choice target drew no arrow", colDir)
		}
		var drawn string
		for _, tx := range ink.texts {
			if tx.s != choiceArrowGlyph && tx.s != "Kind" && tx.s != "alpha" {
				drawn, value.x = tx.s, tx.x
			}
		}
		if drawn == "" {
			t.Fatalf("%v: the choice cell drew no value", colDir)
		}
		value.w = tv.MeasureText(drawn)
		for _, sp := range tv.columnLayout().spans {
			if sp.col != nil {
				span = sp
			}
		}
		return a, value, span
	}

	for _, tc := range []struct {
		dir core.Direction
		// whether the arrow takes the far end of the cell
		far bool
	}{
		{core.DirLTR, true},
		{core.DirRTL, false},
	} {
		arrow, value, sp := place(tc.dir)
		if far := arrow > value.x; far != tc.far {
			t.Errorf("%v: the arrow at %d against a value at %d took the wrong end of the cell",
				tc.dir, arrow, value.x)
		}
		if arrow < sp.x || arrow >= sp.x+sp.w {
			t.Errorf("%v: the arrow at %d is outside its cell [%d,%d)",
				tc.dir, arrow, sp.x, sp.x+sp.w)
		}
		// The value stops short of the arrow's room rather than running
		// under it.
		aw := tv0MeasureArrow()
		if value.x < arrow+aw && arrow < value.x+value.w {
			t.Errorf("%v: the value [%d,%d) runs over the arrow at [%d,%d)",
				tc.dir, value.x, value.x+value.w, arrow, arrow+aw)
		}
	}
}

// tv0MeasureArrow is the arrow's own width, measured the way the tree does.
func tv0MeasureArrow() core.Unit {
	return NewTreeView().MeasureText(choiceArrowGlyph)
}
