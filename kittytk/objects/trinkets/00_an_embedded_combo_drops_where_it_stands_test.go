package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// gfxSurface answers the way a graphical one does: it draws frames itself and
// can place between cells.
type gfxSurface struct {
	Panel
	smooth bool
}

func (h *gfxSurface) GraphicalWindowFrames() bool   { return true }
func (h *gfxSurface) SmoothWindowPositioning() bool { return h.smooth }

// offsetPopupController maps to screen through an offset that is neither a
// whole column nor a whole row -- which is what a window's own chrome comes to
// on the pixel path, where an inset need not be a multiple of anything.
type offsetPopupController struct {
	recordingPopupController
	dx, dy core.Unit
}

func (h *offsetPopupController) MapToScreen(_ core.Trinket, p core.UnitPoint) core.UnitPoint {
	return core.UnitPoint{X: p.X + h.dx, Y: p.Y + h.dy}
}

// editingTreeOn builds a tree whose columns over-commit the width -- so the
// reclaim runs and the spans come out at their measured sizes rather than at
// round numbers -- with a choice column to edit.
func editingTreeOn(smooth bool, pc core.PopupController) *TreeView {
	tv := NewTreeView()
	tv.SetShowHeader(true)
	for _, c := range []struct {
		id, cap string
		w       core.Unit
	}{{"size", "Size", 80}, {"kind", "Kind", 112}, {"modified", "Date Modified", 192}, {"tags", "Tags", 64}} {
		col := NewTreeColumn(c.id, c.cap, c.w)
		if c.id == "kind" {
			col.Editable = true
			col.Enum = []TreeEnumOption{
				{Key: "png", Value: "PNG image"}, {Key: "folder", Value: "Folder"},
				{Key: "arj", Value: "ARJ Archive"}, {Key: "txt", Value: "Text"},
			}
		}
		tv.AddColumn(col)
	}
	for _, name := range []string{"Screenshot 2026-07-10 at 1.21.28 AM.png", "PC12"} {
		it := NewTreeItem(name)
		it.SetValue("size", "311 KB")
		it.SetValue("kind", "PNG image")
		it.SetValue("modified", "Today at 8:20 AM")
		tv.AddRootItem(it)
	}
	parent := &gfxSurface{smooth: smooth}
	parent.Panel = *NewPanel()
	parent.Init(parent)
	parent.SetPopupController(pc)
	tv.SetParent(parent)
	tv.SetBounds(core.UnitRect{Width: 464, Height: 224})
	tv.SetCurrentIndex(0)
	return tv
}

// openChoiceEditor starts editing the choice column and returns the cell.
func openChoiceEditor(t *testing.T, tv *TreeView) core.UnitRect {
	t.Helper()
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Return"})
	for i := 0; i < 6 && (tv.editCol == nil || len(tv.editCol.Enum) == 0); i++ {
		tv.HandleKeyPress(core.KeyPressEvent{Key: "Tab"})
	}
	if tv.editCombo == nil {
		t.Fatal("no choice editor was mounted")
	}
	r, ok := tv.editorRect()
	if !ok {
		t.Fatal("the editor has no rect")
	}
	return r
}

// A drop-down opened from a tree's cell editor lines up with the cell.
//
// The editor is deliberately UNPARENTED -- it borrows the tree's ancestry
// through SetEmbedHost -- so every question asked up the chain from the box
// itself answers with the default instead of the truth. Asked about the box,
// "can this surface place between cells" came back no, and the list was put on
// the cell grid while the box it belongs to was not: a fraction of a column to
// one side, and up to a row high, over the control it dropped from.
func TestAnEmbeddedCombosDropDownLinesUpWithItsCell(t *testing.T) {
	// An offset that is neither a whole column nor a whole row, so a list
	// put on the grid could not land on the box by luck.
	host := &offsetPopupController{dx: 13, dy: 9}
	tv := editingTreeOn(true, host)
	cell := openChoiceEditor(t, tv)

	if cell.X%8 == 0 && cell.Width%8 == 0 {
		t.Fatal("the cell came out on whole columns; this test needs one that did not")
	}
	if host.popup == nil {
		t.Fatal("no drop-down was registered")
	}

	wantX := cell.X + host.dx
	wantY := cell.Y + cell.Height + host.dy
	if got := host.popup.Bounds; got.X != wantX || got.Y != wantY {
		t.Errorf("the drop-down opened at (%d,%d); want (%d,%d) -- the cell's own left edge, under its bottom",
			got.X, got.Y, wantX, wantY)
	}
	if got := host.popup.Bounds.Width; got != cell.Width {
		t.Errorf("the drop-down is %d wide against the cell's %d", got, cell.Width)
	}
}

// On a surface that draws only in cells the list still lands on them: a popup
// standing between cells there paints its rows one cell from where it answers
// the pointer, which is what putting it on the grid is for.
func TestAnEmbeddedCombosDropDownStaysOnTheGridInTheTUI(t *testing.T) {
	host := &offsetPopupController{dx: 13, dy: 9}
	tv := editingTreeOn(false, host)
	openChoiceEditor(t, tv)

	if host.popup == nil {
		t.Fatal("no drop-down was registered")
	}
	m := core.DefaultCellMetrics()
	got := host.popup.Bounds
	if got.X%m.UnitsPerCellWidth != 0 || got.Y%m.UnitsPerCellHeight != 0 {
		t.Errorf("the drop-down opened at (%d,%d), which is not on a cell", got.X, got.Y)
	}
}

// scrollingChoiceTree puts the choice column past the right edge, so reaching
// it scrolls the region and the cell the editor lands in is not the cell that
// was there when the key was pressed.
func scrollingChoiceTree(pc core.PopupController) *TreeView {
	tv := NewTreeView()
	tv.SetShowHeader(true)
	ids := []string{"a", "b", "c", "far"}
	for i, id := range ids {
		col := NewTreeColumn(id, id, 20*cell)
		if i == len(ids)-1 {
			col.Editable = true
			col.Enum = []TreeEnumOption{{Key: "x", Value: "Ex"}, {Key: "y", Value: "Why"}}
		}
		tv.AddColumn(col)
	}
	for _, n := range []string{"one", "two"} {
		it := NewTreeItem(n)
		for _, id := range ids {
			it.SetValue(id, "v")
		}
		tv.AddRootItem(it)
	}
	parent := NewPanel()
	parent.SetPopupController(pc)
	tv.SetParent(parent)
	tv.SetBounds(core.UnitRect{Width: 40 * 8, Height: 10 * 16})
	tv.SetCurrentIndex(0)
	tv.SetFitWidth(false)
	tv.SetKeyWidth(20 * cell)
	return tv
}

// A drop-down opened the moment its editor is mounted is measured against the
// cell the editor actually stands in.
//
// Reaching a choice column off the right edge scrolls the region to reveal it,
// and the editor is mounted into the cell that scroll produced. The editor's
// own size was set only while PAINTING, though -- so the box the drop-down was
// measured against still held whatever the last frame left it, which for a box
// just built is nothing at all.
func TestADropDownOpenedOnMountIsMeasuredAfterTheScroll(t *testing.T) {
	host := &offsetPopupController{}
	tv := scrollingChoiceTree(host)
	if tv.columnLayout().maxHScroll <= 0 {
		t.Fatal("the columns fit; reaching the choice one scrolls nothing")
	}

	cell := openChoiceEditor(t, tv)
	if tv.hScroll == 0 {
		t.Fatal("reaching the choice column did not scroll the region")
	}
	if !tv.editCombo.IsOpen() {
		t.Fatal("entering a choice cell did not open its drop-down")
	}
	if host.popup == nil {
		t.Fatal("no drop-down was registered")
	}

	if got := host.popup.Bounds.Width; got != cell.Width {
		t.Errorf("the drop-down is %d wide against the cell's %d", got, cell.Width)
	}
	if got := host.popup.Bounds.X; got != cell.X {
		t.Errorf("the drop-down opened at x=%d against the cell's %d", got, cell.X)
	}
}
