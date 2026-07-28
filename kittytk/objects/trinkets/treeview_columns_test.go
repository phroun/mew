package trinkets

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// newColumnsTree builds a 3-column tree (key + Size + Kind) sized to
// width cells x height rows at the base 8x16 metrics, with a few
// items carrying cell values.
func newColumnsTree(widthCells, heightRows int) *TreeView {
	tv := NewTreeView()
	tv.SetShowHeader(true)
	size := NewTreeColumn("size", "Size", 10)
	size.Align = "right"
	kind := NewTreeColumn("kind", "Kind", 12)
	tv.AddColumn(size)
	tv.AddColumn(kind)

	root := NewTreeItem("Folder")
	root.Expanded = true
	root.SetValue("size", "--")
	root.SetValue("kind", "Folder")
	child := NewTreeItem("file.png")
	child.SetValue("size", "311 KB")
	child.SetValue("kind", "PNG image")
	root.AddChild(child)
	tv.AddRootItem(root)
	for i := 0; i < 30; i++ {
		it := NewTreeItem("extra")
		it.SetValue("size", "1 KB")
		it.SetValue("kind", "File")
		tv.AddRootItem(it)
	}

	tv.SetBounds(core.UnitRect{Width: core.Unit(widthCells * 8), Height: core.Unit(heightRows * 16)})
	return tv
}

// Fit mode: spans tile the content area exactly - key column absorbs
// the slack, dividers one cell, no horizontal scrolling.
func TestTreeColumnLayoutFitMode(t *testing.T) {
	tv := newColumnsTree(60, 10)
	lay := tv.columnLayout()

	if len(lay.spans) != 3 {
		t.Fatalf("want 3 spans (key+2), got %d", len(lay.spans))
	}
	if lay.maxHScroll != 0 {
		t.Errorf("fit mode must not scroll horizontally, maxHScroll=%d", lay.maxHScroll)
	}
	// Content = 60 cells minus the scrollbar lane = 59 cells.
	// key + div + 10 + div + 12 = 59 -> key = 35 cells.
	if lay.spans[0].w != 35*8 {
		t.Errorf("key span width = %d units, want %d", lay.spans[0].w, 35*8)
	}
	if lay.spans[1].x != 36*8 || lay.spans[1].w != 10*8 {
		t.Errorf("size span = x%d w%d, want x%d w%d", lay.spans[1].x, lay.spans[1].w, 36*8, 10*8)
	}
	if lay.spans[2].x != 47*8 || lay.spans[2].w != 12*8 {
		t.Errorf("kind span = x%d w%d, want x%d w%d", lay.spans[2].x, lay.spans[2].w, 47*8, 12*8)
	}
	if lay.spans[0].divX != 35*8 || lay.spans[1].divX != 46*8 {
		t.Errorf("dividers at %d,%d; want %d,%d", lay.spans[0].divX, lay.spans[1].divX, 35*8, 46*8)
	}
	// Header consumes one row; no footer in fit mode.
	if tv.headerHeight() != 16 || tv.footerHeight() != 0 {
		t.Errorf("headerH=%d footerH=%d, want 16,0", tv.headerHeight(), tv.footerHeight())
	}
	if tv.visibleCount() != 9 {
		t.Errorf("visibleCount=%d, want 9 (10 rows minus header)", tv.visibleCount())
	}
}

// Fit mode under overflow: data columns shrink toward MinWidth so the
// key column keeps a usable minimum.
func TestTreeColumnLayoutFitShrinks(t *testing.T) {
	tv := newColumnsTree(20, 10) // 19 content cells for key+10+12+2 dividers
	lay := tv.columnLayout()
	total := core.Unit(0)
	for _, sp := range lay.spans {
		total += sp.w
	}
	// key(min 6) + shrunk data columns + 2 dividers must fit 19 cells.
	if lay.spans[0].w < 6*8 {
		t.Errorf("key span %d under minimum", lay.spans[0].w)
	}
	if total+2*8 > 19*8 {
		t.Errorf("fit-mode spans overflow: total %d + dividers > %d", total, 19*8)
	}
}

// Fit mode under pressure reclaims MEASURED slack: a column declared
// far wider than its content (measured with the effective font, not a
// rune count) gives that padding to the key column before anything
// truncates - the key column ends up far wider than its hard minimum.
func TestTreeColumnFitReclaimsMeasuredSlack(t *testing.T) {
	tv := NewTreeView()
	tv.SetShowHeader(true)
	wide := NewTreeColumn("pad", "Pad", 30) // declared 30 cells of mostly padding
	tv.AddColumn(wide)
	it := NewTreeItem("a-rather-long-file-name.png")
	it.SetValue("pad", "x") // content needs ~1 cell
	tv.AddRootItem(it)
	// 40 cells wide: 39 content; declared key(20 desired)+30+1 divider
	// overflows, so the pad column must shrink toward its measured need.
	tv.SetBounds(core.UnitRect{Width: 40 * 8, Height: 10 * 16})

	lay := tv.columnLayout()
	keyW := int(lay.spans[0].w / 8)
	padW := int(lay.spans[1].w / 8)
	// The key reaches its desired width (20 cells) by reclaiming the
	// padded column's measured slack; the pad keeps what is left (a
	// declared width is respected absent pressure). Under the old
	// MinWidth-only shrink the key would have been crushed to 6.
	if keyW < 20 {
		t.Errorf("key column got %d cells; measured reclaim should reach desired 20 (padW=%d)", keyW, padW)
	}
	if padW >= 30 {
		t.Errorf("padded column gave up nothing (%d cells)", padW)
	}
}

// With the key column hidden, the FIRST visible data column hosts the
// tree affordances: nesting indent, a working expander, and forced
// left alignment regardless of its Align setting.
func TestTreeColumnHostWhenKeyHidden(t *testing.T) {
	tv := newColumnsTree(60, 10)
	tv.SetShowKey(false)
	size := tv.ColumnByID("size") // first visible data column, align=right
	if tv.treeHostColumn() != size {
		t.Fatalf("host = %v, want the size column", tv.treeHostColumn())
	}
	// Hiding the first column moves the host to the next one.
	size.Hidden = true
	if tv.treeHostColumn() != tv.ColumnByID("kind") {
		t.Fatalf("host after hiding = %v, want kind", tv.treeHostColumn())
	}
	size.Hidden = false

	// The expander click works in the host span: the expanded root
	// folder (level 0, indicator in the span's first cell) collapses.
	lay := tv.columnLayout()
	if lay.spans[0].col != size {
		t.Fatalf("first span should be the size column, got %v", lay.spans[0].col)
	}
	root := tv.RootItems()[0]
	if !root.Expanded {
		t.Fatal("precondition: root expanded")
	}
	tv.HandleMousePress(core.MousePressEvent{
		X: lay.spans[0].x + core.Unit(treeLeftPadCells)*tv.EffectiveCellMetrics().CellWidth + 2,
		Y: tv.headerHeight() + 2, Button: core.LeftButton,
	})
	if root.Expanded {
		t.Error("expander click in the host data column did not collapse")
	}

	// With the key visible, the host is nil (the key hosts the tree).
	tv.SetShowKey(true)
	if tv.treeHostColumn() != nil {
		t.Errorf("host with key visible = %v, want nil", tv.treeHostColumn())
	}
}

// Scroll mode: natural widths, footer row reserved, hScroll pans the
// unfixed spans and clamps to the overflow.
func TestTreeColumnLayoutScrollMode(t *testing.T) {
	tv := newColumnsTree(30, 10)
	tv.SetFitWidth(false)
	tv.SetKeyWidth(20)

	if tv.footerHeight() != 16 {
		t.Fatalf("scroll mode must reserve the footer row")
	}
	if tv.visibleCount() != 8 {
		t.Errorf("visibleCount=%d, want 8 (10 rows minus header and footer)", tv.visibleCount())
	}

	lay := tv.columnLayout()
	// Natural: 20 + 1 + 10 + 1 + 12 = 44 cells in 29 content cells.
	if lay.maxHScroll != 44-29 {
		t.Errorf("maxHScroll=%d, want %d", lay.maxHScroll, 44-29)
	}

	if !tv.scrollHorizontally(5) {
		t.Fatal("scrollHorizontally(5) did nothing")
	}
	lay = tv.columnLayout()
	if lay.spans[0].x != -5*8 {
		t.Errorf("panned key span x=%d, want %d", lay.spans[0].x, -5*8)
	}
	// Clamp at the end.
	tv.scrollHorizontally(1000)
	lay = tv.columnLayout()
	if tv.hScroll != lay.maxHScroll {
		t.Errorf("hScroll=%d, want clamp at %d", tv.hScroll, lay.maxHScroll)
	}
}

// Fixed columns stay pinned while the middle pans.
func TestTreeColumnFixedLeft(t *testing.T) {
	tv := newColumnsTree(30, 10)
	tv.SetFitWidth(false)
	tv.SetKeyWidth(15)
	tv.SetFixedColumns(1, 0)
	tv.scrollHorizontally(4)

	lay := tv.columnLayout()
	if !lay.spans[0].fixed || lay.spans[0].x != 0 {
		t.Errorf("fixed key span moved: fixed=%v x=%d", lay.spans[0].fixed, lay.spans[0].x)
	}
	if lay.spans[1].fixed {
		t.Errorf("span 1 should scroll")
	}
	// Scroll region starts after the fixed key span + divider.
	if lay.scrollL != 16*8 {
		t.Errorf("scrollL=%d, want %d", lay.scrollL, 16*8)
	}
	if lay.spans[1].x != lay.scrollL-4*8 {
		t.Errorf("panned span 1 x=%d, want %d", lay.spans[1].x, lay.scrollL-4*8)
	}
}

// The pinned-right boundary divider sizes the PINNED column (inverted),
// not the scrolling column to its left - the right flank is laid out
// from the window's right edge, so that divider IS the pinned column's
// left edge.
func TestTreeColumnPinnedRightDividerSizesPinned(t *testing.T) {
	tv := newColumnsTree(30, 10)
	tv.SetFitWidth(false)
	tv.SetKeyWidth(10)
	tv.SetFixedColumns(0, 1) // pin the last column (Kind)

	lay := tv.columnLayout()
	n := len(lay.spans)
	if !lay.spans[n-1].fixed {
		t.Fatal("precondition: last span pinned")
	}
	divX := lay.spans[n-2].divX // the pinned boundary divider
	col, startW, invert, ok := tv.dividerAt(divX+2, lay)
	if !ok || col != tv.ColumnByID("kind") || !invert {
		t.Fatalf("pinned boundary: col=%v invert=%v ok=%v, want kind/inverted", col, invert, ok)
	}
	if startW != 12 {
		t.Errorf("startW=%d, want 12", startW)
	}

	// The cursor over that divider is the horizontal resizer.
	if got := tv.CursorShapeAt(divX+2, 4); got != core.CursorResizeH {
		t.Errorf("cursor over divider = %v, want CursorResizeH", got)
	}
	if got := tv.CursorShapeAt(divX+2, 40); got != core.CursorDefault {
		t.Errorf("cursor below header = %v, want default", got)
	}
}

// Hidden columns drop out of the layout; the chooser toggles them back.
func TestTreeColumnHiddenAndChooser(t *testing.T) {
	tv := newColumnsTree(60, 10)
	tv.ColumnByID("kind").Hidden = true
	lay := tv.columnLayout()
	if len(lay.spans) != 2 {
		t.Fatalf("hidden column still laid out: %d spans", len(lay.spans))
	}

	// The [=] button exists (optional columns present) and sits in the
	// scrollbar lane's header cell.
	r, ok := tv.chooserButtonRect()
	if !ok {
		t.Fatal("chooser button missing")
	}
	if r.X != tv.Bounds().Width-8 || r.Y != 0 {
		t.Errorf("chooser at %v", r)
	}

	// Press on the button opens the REAL Menu popup on the ancestor's
	// controller (same overlay path as combobox/context menus).
	host := &recordingPopupController{}
	parent := NewPanel()
	parent.SetPopupController(host)
	tv.SetParent(parent)
	if !tv.HandleMousePress(core.MousePressEvent{X: r.X, Y: r.Y, Button: core.LeftButton}) {
		t.Fatal("chooser press not handled")
	}
	if host.popup == nil || tv.chooserMenu == nil {
		t.Fatal("no menu popup registered")
	}
	if len(tv.chooserMenu.Items()) != 2 {
		t.Fatalf("chooser menu items = %d, want 2", len(tv.chooserMenu.Items()))
	}
	// The tree retains focus and forwards keys (the menu bar pattern):
	// Down, Down, Enter toggles the second item - the hidden "kind".
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Down"})
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Down"})
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if tv.ColumnByID("kind").Hidden {
		t.Error("chooser keyboard toggle did not unhide the column")
	}
	// Chooser items are InPlace: the menu STAYS open (users flip
	// several columns per visit) with its checkmark re-rendered.
	if !tv.chooserOpen {
		t.Fatal("InPlace toggle must keep the menu open")
	}
	if !tv.chooserMenu.Items()[1].Checked {
		t.Error("checkmark did not re-render in place")
	}
	// Toggle it right back without reopening.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if !tv.ColumnByID("kind").Hidden {
		t.Error("second in-place toggle did not re-hide")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})
	if tv.chooserOpen || host.popup != nil {
		t.Error("Escape did not dismiss the chooser")
	}
}

// Divider drags size the side the slack dictates. Fit mode runs the
// composite two-sided drag: with the auto-fill key column left of the
// line, dragging LEFT widens the RIGHT column - funded by the key's
// spare width first, then by narrowing the LEFT column - and dragging
// RIGHT narrows the right column, the cells returning to the key. The
// total is capped so the layout never reclaims from unrelated columns
// mid-drag: no line ever moves contrary to the drag direction. Scroll
// mode has no slack column: the classic left-column sizing applies.
func TestTreeColumnDividerDrag(t *testing.T) {
	tv := newColumnsTree(60, 10)
	lay := tv.columnLayout()
	divX := lay.spans[1].divX // divider between Size and Kind
	// TUI, 60 cells wide: content 59, key auto-width 35, defended
	// floor 20 -> a 15-cell slack pool; Size can give 10-3=7 more.

	if !tv.HandleMousePress(core.MousePressEvent{X: divX + 2, Y: 4, Button: core.LeftButton}) {
		t.Fatal("divider press not handled")
	}
	if !tv.colDragging || !tv.colDragFit || tv.colDragSlackRight {
		t.Fatalf("fit-mode drag: dragging=%v fit=%v slackRight=%v, want true/true/false",
			tv.colDragging, tv.colDragFit, tv.colDragSlackRight)
	}
	// Drag RIGHT 4 cells -> Kind narrows by 4 and Size widens by the
	// same 4, so the key's width - and every line but the grabbed one,
	// including Kind's right edge - stays put by construction.
	tv.HandleMouseMove(core.MouseMoveEvent{X: divX + 2 + 4*8, Y: 4, Buttons: 1})
	if got := tv.ColumnByID("kind").Width; got != 8 {
		t.Errorf("right drag: kind width=%d, want 8", got)
	}
	if got := tv.ColumnByID("size").Width; got != 14 {
		t.Errorf("right drag: size=%d, want 14 (compensates kind's give)", got)
	}
	if lr := tv.columnLayout(); lr.spans[0].divX != lay.spans[0].divX ||
		lr.spans[1].divX != divX+4*8 || lr.spans[2].divX != lay.spans[2].divX {
		t.Errorf("right drag moved a line it must not: key|size %d->%d, grabbed %d->%d (want %d), kind| %d->%d",
			lay.spans[0].divX, lr.spans[0].divX, divX, lr.spans[1].divX, divX+4*8,
			lay.spans[2].divX, lr.spans[2].divX)
	}
	// Far right: stops at Kind's minimum (3); Size holds the transfer.
	tv.HandleMouseMove(core.MouseMoveEvent{X: divX + 2 + 100*8, Y: 4, Buttons: 1})
	if got := tv.ColumnByID("kind").Width; got != 3 {
		t.Errorf("right drag clamp: kind width=%d, want 3", got)
	}
	if got := tv.ColumnByID("size").Width; got != 19 {
		t.Errorf("right drag clamp: size=%d, want 19", got)
	}
	// LEFT 10 cells (from the press point): all funded by the key's
	// slack pool - Kind widens, Size untouched.
	tv.HandleMouseMove(core.MouseMoveEvent{X: divX + 2 - 10*8, Y: 4, Buttons: 1})
	if got := tv.ColumnByID("kind").Width; got != 22 {
		t.Errorf("left drag (slack): kind width=%d, want 22", got)
	}
	if got := tv.ColumnByID("size").Width; got != 10 {
		t.Errorf("left drag (slack) touched the left column: size=%d, want 10", got)
	}
	// LEFT 20: the 15-cell pool is spent; the remaining 5 comes from
	// narrowing Size.
	tv.HandleMouseMove(core.MouseMoveEvent{X: divX + 2 - 20*8, Y: 4, Buttons: 1})
	if got := tv.ColumnByID("kind").Width; got != 32 {
		t.Errorf("left drag (pool+left): kind width=%d, want 32", got)
	}
	if got := tv.ColumnByID("size").Width; got != 5 {
		t.Errorf("left drag (pool+left): size=%d, want 5", got)
	}
	// Far left: capped at pool(15) + Size's room(7) = 22 - the key
	// stays at its defended floor, so the layout never starts
	// reclaiming from other columns (which would move their lines
	// AGAINST the drag).
	tv.HandleMouseMove(core.MouseMoveEvent{X: divX + 2 - 60*8, Y: 4, Buttons: 1})
	if got := tv.ColumnByID("kind").Width; got != 34 {
		t.Errorf("left drag cap: kind width=%d, want 34", got)
	}
	if got := tv.ColumnByID("size").Width; got != 3 {
		t.Errorf("left drag cap: size=%d, want 3", got)
	}
	// The grabbed line resolved exactly 22 cells left of where it
	// started, and the key|Size line moved only WITH the drag.
	lay2 := tv.columnLayout()
	if lay2.spans[1].divX != divX-22*8 {
		t.Errorf("grabbed line at %d, want %d", lay2.spans[1].divX, divX-22*8)
	}
	if lay2.spans[0].divX > lay.spans[0].divX {
		t.Errorf("key|Size line moved AGAINST the drag: %d > %d", lay2.spans[0].divX, lay.spans[0].divX)
	}
	tv.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})
	if tv.colDragging {
		t.Error("release did not end the drag")
	}
	tv.ColumnByID("size").Width = 10 // restore for the scroll-mode leg
	tv.ColumnByID("kind").Width = 12

	// Scroll mode (no slack column): the divider sizes the column to
	// its LEFT, classic semantics.
	tv.SetFitWidth(false)
	tv.SetKeyWidth(15)
	lay = tv.columnLayout()
	divX = lay.spans[1].divX
	tv.HandleMousePress(core.MousePressEvent{X: divX + 2, Y: 4, Button: core.LeftButton})
	if !tv.colDragging || tv.colDragInvert {
		t.Fatalf("scroll-mode drag: dragging=%v invert=%v, want true/false", tv.colDragging, tv.colDragInvert)
	}
	tv.HandleMouseMove(core.MouseMoveEvent{X: divX + 2 + 4*8, Y: 4, Buttons: 1})
	if got := tv.ColumnByID("size").Width; got != 14 {
		t.Errorf("scroll-mode drag: size width=%d, want 14", got)
	}
	tv.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})

	// Non-resizable right column refuses the inverted drag.
	tv.SetFitWidth(true)
	tv.ColumnByID("kind").Resizable = false
	lay = tv.columnLayout()
	tv.HandleMousePress(core.MousePressEvent{X: lay.spans[1].divX + 2, Y: 4, Button: core.LeftButton})
	if tv.colDragging {
		t.Error("divider left of a non-resizable column started a drag in fit mode")
	}
}

// Fit mode with the key hidden: the slack is the BLANK width right of
// the last column, and the drag mirrors - rightward widens the LEFT
// column into the blank first, then the right column gives way;
// leftward narrows the left column and the blank grows back.
func TestTreeFitDragKeyHiddenBlankPool(t *testing.T) {
	tv := newColumnsTree(60, 10)
	tv.SetShowKey(false)
	lay := tv.columnLayout()
	// Visible: Size(10) | Kind(12); content 59 cells, one divider ->
	// 23 used, 36 blank at the right; Kind can give 12-3=9 more.
	divX := lay.spans[0].divX

	if !tv.HandleMousePress(core.MousePressEvent{X: divX + 2, Y: 4, Button: core.LeftButton}) {
		t.Fatal("divider press not handled")
	}
	if !tv.colDragging || !tv.colDragFit || !tv.colDragSlackRight {
		t.Fatalf("no-key fit drag: dragging=%v fit=%v slackRight=%v, want true/true/true",
			tv.colDragging, tv.colDragFit, tv.colDragSlackRight)
	}
	// Rightward 10: Size widens into the blank; Kind untouched (its
	// lines move right WITH the drag).
	tv.HandleMouseMove(core.MouseMoveEvent{X: divX + 2 + 10*8, Y: 4, Buttons: 1})
	if got := tv.ColumnByID("size").Width; got != 20 {
		t.Errorf("right drag (blank): size=%d, want 20", got)
	}
	if got := tv.ColumnByID("kind").Width; got != 12 {
		t.Errorf("right drag (blank) touched kind: %d, want 12", got)
	}
	// Rightward 40: the 36-cell blank is spent; Kind gives the rest.
	tv.HandleMouseMove(core.MouseMoveEvent{X: divX + 2 + 40*8, Y: 4, Buttons: 1})
	if got := tv.ColumnByID("size").Width; got != 50 {
		t.Errorf("right drag (blank+kind): size=%d, want 50", got)
	}
	if got := tv.ColumnByID("kind").Width; got != 8 {
		t.Errorf("right drag (blank+kind): kind=%d, want 8", got)
	}
	// Far right: capped at blank(36) + Kind's room(9) = 45.
	tv.HandleMouseMove(core.MouseMoveEvent{X: divX + 2 + 100*8, Y: 4, Buttons: 1})
	if got := tv.ColumnByID("size").Width; got != 55 {
		t.Errorf("right drag cap: size=%d, want 55", got)
	}
	if got := tv.ColumnByID("kind").Width; got != 3 {
		t.Errorf("right drag cap: kind=%d, want 3", got)
	}
	// Back LEFT past the origin: Size narrows, Kind restored.
	tv.HandleMouseMove(core.MouseMoveEvent{X: divX + 2 - 6*8, Y: 4, Buttons: 1})
	if got := tv.ColumnByID("size").Width; got != 4 {
		t.Errorf("left drag: size=%d, want 4", got)
	}
	if got := tv.ColumnByID("kind").Width; got != 12 {
		t.Errorf("left drag did not restore kind: %d, want 12", got)
	}
	tv.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})
}

// A press OUTSIDE every popup makes the HOST clear the overlay list
// itself - the popup's own press handler never runs. OnDismiss is the
// owner's notification: the chooser must drop its open-state, or the
// button stays painted "focused" and every keystroke keeps being
// swallowed for a menu that no longer exists.
func TestTreeColumnChooserHostDismiss(t *testing.T) {
	tv := newColumnsTree(60, 10)
	host := &recordingPopupController{}
	parent := NewPanel()
	parent.SetPopupController(host)
	tv.SetParent(parent)
	r, _ := tv.chooserButtonRect()
	tv.HandleMousePress(core.MousePressEvent{X: r.X, Y: r.Y, Button: core.LeftButton})
	if host.popup == nil || !tv.chooserOpen {
		t.Fatal("precondition: chooser open")
	}
	if host.popup.OnDismiss == nil {
		t.Fatal("chooser popup registered without OnDismiss")
	}
	// Simulate the WindowManager's outside-press force-clear: the list
	// is dropped first, then the owner is notified, and the press is
	// NOT consumed - it falls through to whatever was clicked (here, a
	// row in the tree's own item list).
	dismissed := host.popup
	host.popup = nil
	dismissed.OnDismiss()
	if tv.chooserOpen || tv.chooserMenu != nil {
		t.Error("chooser open-state survived the host dismissal")
	}
	tv.HandleMousePress(core.MousePressEvent{X: 20, Y: 36, Button: core.LeftButton}) // row 1
	if tv.CurrentIndex() != 1 {
		t.Fatalf("fall-through row click: index=%d, want 1", tv.CurrentIndex())
	}
	// Keys route normally again instead of feeding the dead menu.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Down"})
	if tv.CurrentIndex() != 2 {
		t.Errorf("Down after dismissal: index=%d, want 2", tv.CurrentIndex())
	}
}

// Activating a sortable header cycles ascending -> descending ->
// unsorted and fires the observer; non-sortable data captions do
// nothing; the key column is always sortable (sortedBy -1).
func TestTreeColumnSortClick(t *testing.T) {
	tv := newColumnsTree(60, 10)
	tv.ColumnByID("size").Sortable = true

	var gotSorted, gotDesc bool
	var gotBy int
	calls := 0
	tv.SetOnSortRequested(func(sorted bool, by int, desc bool) {
		gotSorted, gotBy, gotDesc = sorted, by, desc
		calls++
	})

	lay := tv.columnLayout()
	x := lay.spans[1].x + 8 // inside the Size caption (data index 0)
	click := func(cx core.Unit) {
		tv.HandleMousePress(core.MousePressEvent{X: cx, Y: 4, Button: core.LeftButton})
	}
	click(x)
	if calls != 1 || !gotSorted || gotBy != 0 || gotDesc {
		t.Fatalf("first click: calls=%d sorted=%v by=%d desc=%v", calls, gotSorted, gotBy, gotDesc)
	}
	click(x)
	if calls != 2 || !gotSorted || !gotDesc {
		t.Fatalf("second click should reverse: calls=%d sorted=%v desc=%v", calls, gotSorted, gotDesc)
	}
	click(x)
	if calls != 3 || gotSorted {
		t.Fatalf("third click should unsort: calls=%d sorted=%v", calls, gotSorted)
	}
	if sorted, _, _ := tv.Sorted(); sorted {
		t.Errorf("view still sorted after the cycle completed")
	}

	// Kind (data index 1) is not sortable: no callback.
	click(lay.spans[2].x + 8)
	if calls != 3 {
		t.Errorf("non-sortable caption fired the callback")
	}

	// The key column is always sortable: sortedBy -1.
	click(lay.spans[0].x + 8)
	if calls != 4 || !gotSorted || gotBy != -1 || gotDesc {
		t.Errorf("key caption click: calls=%d sorted=%v by=%d desc=%v", calls, gotSorted, gotBy, gotDesc)
	}
}

// Content clicks land on the right rows with the header row present.
func TestTreeColumnHeaderRowOffset(t *testing.T) {
	tv := newColumnsTree(60, 10)
	// Row 0 of content = y in [16,32).
	tv.HandleMousePress(core.MousePressEvent{X: 30 * 8, Y: 20, Button: core.LeftButton})
	if tv.CurrentIndex() != 0 {
		t.Errorf("click on first content row selected %d", tv.CurrentIndex())
	}
	tv.HandleMousePress(core.MousePressEvent{X: 30 * 8, Y: 40, Button: core.LeftButton})
	if tv.CurrentIndex() != 1 {
		t.Errorf("click on second content row selected %d", tv.CurrentIndex())
	}
}

// Rendering smoke + optional visual proof on the pixel path.
func TestTreeColumnPaintSmoke(t *testing.T) {
	b, err := raster.New(640, 240)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(b)
	tv := newColumnsTree(60, 10)
	tv.SetParent(d)
	tv.ColumnByID("size").Sortable = true
	tv.SetSorted(true, 0, false)
	b.Clear(style.DefaultStyle())
	tv.Paint(core.NewPainter(b))

	if dir := os.Getenv("KITTYTK_PROOF_DIR"); dir != "" {
		out := filepath.Join(dir, "treeview_columns.png")
		if err := b.WritePNG(out); err != nil {
			t.Fatal(err)
		}
		t.Logf("proof -> %s", out)
	}
}

// The last column (before any pinned-right flank) stretches over
// trailing blank width instead of ellipsizing its content while free
// space sits unused to its right.
func TestTreeLastColumnStretchesOverBlank(t *testing.T) {
	tv := newColumnsTree(80, 10) // content 79 cells, natural 44
	tv.SetFitWidth(false)
	tv.SetKeyWidth(20)
	lay := tv.columnLayout()
	last := lay.spans[len(lay.spans)-1]
	if last.x+last.w != lay.scrollR {
		t.Errorf("last span ends at %d, want scrollR %d", last.x+last.w, lay.scrollR)
	}
	if lay.blankCells != 79-44 {
		t.Errorf("blankCells = %d, want %d", lay.blankCells, 79-44)
	}
	// The natural column width is untouched (only the SPAN stretched).
	if got := tv.ColumnByID("kind").Width; got != 12 {
		t.Errorf("kind natural width = %d, want 12", got)
	}

	// With a pinned-right column, the last SCROLLING span claims the
	// blank up to the pinned divider instead.
	tv.SetFixedColumns(0, 1)
	lay = tv.columnLayout()
	n := len(lay.spans)
	mid := lay.spans[n-2] // size: the last scrolling span
	if mid.x+mid.w != lay.scrollR {
		t.Errorf("last scrolling span ends at %d, want scrollR %d", mid.x+mid.w, lay.scrollR)
	}
}

// The footer horizontal scrollbar's thumb lights in the hover color
// under the pointer and stays lit through a drag - the vertical bar's
// convention.
func TestTreeHBarThumbHover(t *testing.T) {
	tv := newColumnsTree(30, 10)
	tv.SetFitWidth(false)
	tv.SetKeyWidth(20)
	lay := tv.columnLayout()
	_, _, x0, x1, ok := tv.hScrollbarGeometry(lay)
	if !ok {
		t.Fatal("no horizontal scrollbar geometry")
	}
	y := tv.Bounds().Height - tv.footerHeight() + 2

	tv.HandleMouseMove(core.MouseMoveEvent{X: (x0 + x1) / 2, Y: y})
	if !tv.hbarThumbHovered {
		t.Fatal("thumb hover not detected")
	}
	tv.HandleMouseMove(core.MouseMoveEvent{X: x1 + 16, Y: y})
	if tv.hbarThumbHovered {
		t.Error("hover did not clear off the thumb")
	}
	// While dragging, the thumb stays lit wherever the pointer goes.
	tv.HandleMousePress(core.MousePressEvent{X: (x0 + x1) / 2, Y: y, Button: core.LeftButton})
	if !tv.hbarDragging {
		t.Fatal("thumb press did not start the drag")
	}
	tv.HandleMouseMove(core.MouseMoveEvent{X: x0, Y: 8, Buttons: 1})
	if !tv.hbarThumbHovered {
		t.Error("dragging thumb lost the hover state")
	}
	// Ending the drag must not strand a lit thumb: with no graphical
	// environment (TUI - no free mouse moves arrive to clear it), the
	// hover state clears on release.
	tv.HandleMouseRelease(core.MouseReleaseEvent{X: x0, Y: 8, Button: core.LeftButton})
	if tv.hbarThumbHovered {
		t.Error("hover state stranded lit after the drag ended (TUI leak)")
	}
}

// treeLinePrefix fills the indent region with tree-command connector
// segments: the item's own ├/└ elbow in its last chunk dash-filled to
// the glyph cell, and │ continuations for ancestors that still have
// siblings below. Length is exactly level*indentWidth - tree lines
// never change spacing.
func TestTreeLinePrefix(t *testing.T) {
	tv := NewTreeView() // indentWidth 2
	r1, r2 := NewTreeItem("r1"), NewTreeItem("r2")
	c1, c2 := NewTreeItem("c1"), NewTreeItem("c2")
	g1, g2 := NewTreeItem("g1"), NewTreeItem("g2")
	h1 := NewTreeItem("h1")
	r1.AddChild(c1)
	r1.AddChild(c2)
	c1.AddChild(g1)
	c1.AddChild(g2)
	c2.AddChild(h1)
	r1.Expanded, c1.Expanded, c2.Expanded = true, true, true
	tv.AddRootItem(r1)
	tv.AddRootItem(r2)
	tv.SetTreeLines(true)

	cases := []struct {
		item *TreeItem
		want string
	}{
		{r1, ""},     // roots have no indent to fill
		{c1, "├─"},   // sibling below
		{c2, "└─"},   // last child
		{g1, "│ ├─"}, // c1's chain continues past it
		{g2, "│ └─"},
		{h1, "  └─"}, // c2 is last: no continuation line
	}
	for _, c := range cases {
		if got := string(tv.treeLinePrefix(c.item)); got != c.want {
			t.Errorf("prefix(%s) = %q, want %q", c.item.Text, got, c.want)
		}
	}

	// The elbow follows the VISUAL (sorted) order, not declaration
	// order: descending by name flips c1/c2.
	tv.SetSorted(true, -1, true)
	tv.rebuildFlatList()
	if got := string(tv.treeLinePrefix(c1)); got != "└─" {
		t.Errorf("sorted prefix(c1) = %q, want └─", got)
	}
	if got := string(tv.treeLinePrefix(c2)); got != "├─" {
		t.Errorf("sorted prefix(c2) = %q, want ├─", got)
	}
}

// Growing the tree while scrolled down pulls the content back into the
// freed space (the scrollbar must not vanish leaving a stale blank).
func TestTreeResizeReclampsScroll(t *testing.T) {
	tv := NewTreeView()
	for i := 0; i < 30; i++ {
		tv.AddRootItem(NewTreeItem(fmtItem(i)))
	}
	tv.SetBounds(core.UnitRect{Width: 200, Height: 160})
	tv.scrollOffset = 30 - tv.visibleCount() // scrolled to the bottom
	if tv.scrollOffset <= 0 {
		t.Fatal("precondition: view smaller than the list")
	}
	tv.SetBounds(core.UnitRect{Width: 200, Height: 400})
	if want := 30 - tv.visibleCount(); tv.scrollOffset != want {
		t.Errorf("scrollOffset after grow = %d, want %d", tv.scrollOffset, want)
	}
	// Tall enough for everything: the offset snaps to 0.
	tv.SetBounds(core.UnitRect{Width: 200, Height: 640})
	if tv.scrollOffset != 0 {
		t.Errorf("scrollOffset with everything visible = %d, want 0", tv.scrollOffset)
	}
}

// recordingPopupController captures the registered popup so tests can
// drive its handlers.
type recordingPopupController struct {
	popup *core.PopupRequest
}

func (h *recordingPopupController) RegisterPopup(r *core.PopupRequest) { h.popup = r }
func (h *recordingPopupController) UnregisterPopup(string)             { h.popup = nil }
func (h *recordingPopupController) MapToScreen(_ core.Trinket, p core.UnitPoint) core.UnitPoint {
	return p
}
func (h *recordingPopupController) ScreenBounds() core.UnitRect {
	return core.UnitRect{Width: 800, Height: 480}
}
