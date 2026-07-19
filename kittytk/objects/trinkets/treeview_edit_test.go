package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// newEditableTree: key + Size(editable) + Kind(editable) + Date(not),
// flat rows so index math is simple.
func newEditableTree() *TreeView {
	tv := NewTreeView()
	tv.SetShowHeader(true)
	size := NewTreeColumn("size", "Size", 10)
	size.Editable = true
	kind := NewTreeColumn("kind", "Kind", 12)
	kind.Editable = true
	date := NewTreeColumn("date", "Date", 12)
	tv.AddColumn(size)
	tv.AddColumn(kind)
	tv.AddColumn(date)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		it := NewTreeItem(name)
		it.SetValue("size", "1 KB")
		it.SetValue("kind", "File")
		it.SetValue("date", "Today")
		tv.AddRootItem(it)
	}
	tv.SetBounds(core.UnitRect{Width: 480, Height: 160})
	tv.SetCurrentIndex(0)
	return tv
}

// With no editable columns, Enter keeps its classic Space behavior
// (expand/collapse the current item).
func TestTreeEnterWithoutEditableActsLikeSpace(t *testing.T) {
	tv := newColumnsTree(60, 10) // size/kind are NOT editable here
	tv.SetCurrentIndex(0)        // "Folder", expanded
	folder := tv.CurrentItem()
	if !folder.Expanded {
		t.Fatal("precondition: folder expanded")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if tv.rowEditing {
		t.Fatal("row edit began with no editable columns")
	}
	if folder.Expanded {
		t.Error("Enter did not collapse (Space behavior)")
	}
}

// Enter opens the row editor on the first editable column; Tab commits
// and cycles; Enter commits the row and dismisses; re-entering resumes
// the last-edited column.
func TestTreeRowEditLifecycle(t *testing.T) {
	tv := newEditableTree()
	edits := 0
	tv.SetOnCellEdited(func(item *TreeItem, col *TreeColumn, v string) { edits++ })

	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if !tv.rowEditing || tv.editCol != tv.ColumnByID("size") {
		t.Fatalf("edit: rowEditing=%v col=%v, want size", tv.rowEditing, tv.editCol)
	}
	if tv.editBox.Text() != "1 KB" {
		t.Fatalf("editor prefill = %q", tv.editBox.Text())
	}
	// Change the value, Tab to the next editable column (kind - the
	// non-editable date column is skipped by construction of the Tab
	// ring). The size value commits on the way out.
	tv.editBox.SetText("2 KB")
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Tab"})
	if tv.editCol != tv.ColumnByID("kind") {
		t.Fatalf("after Tab: col=%v, want kind", tv.editCol)
	}
	if got := tv.CurrentItem().Value("size"); got != "2 KB" {
		t.Errorf("Tab did not commit the cell: size=%q", got)
	}
	if edits != 1 {
		t.Errorf("edits=%d, want 1", edits)
	}
	// S-Tab wraps back to size.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "S-Tab"})
	if tv.editCol != tv.ColumnByID("size") {
		t.Fatalf("after S-Tab: col=%v, want size", tv.editCol)
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Tab"}) // back onto kind
	tv.editBox.SetText("Document")
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if tv.rowEditing {
		t.Fatal("Enter did not dismiss the row editor")
	}
	if got := tv.CurrentItem().Value("kind"); got != "Document" {
		t.Errorf("Enter did not commit the row: kind=%q", got)
	}
	// Re-entering resumes the column edited last (kind).
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if !tv.rowEditing || tv.editCol != tv.ColumnByID("kind") {
		t.Fatalf("re-enter: col=%v, want kind (remembered)", tv.editCol)
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})
}

// Escape cancels: the cell keeps its original value.
func TestTreeRowEditEscapeCancels(t *testing.T) {
	tv := newEditableTree()
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	tv.editBox.SetText("garbage")
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})
	if tv.rowEditing {
		t.Fatal("Escape did not dismiss")
	}
	if got := tv.CurrentItem().Value("size"); got != "1 KB" {
		t.Errorf("Escape wrote the value: size=%q", got)
	}
}

// Up/Down accept the edit and continue editing the SAME column on the
// neighboring row.
func TestTreeRowEditUpDownContinues(t *testing.T) {
	tv := newEditableTree()
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Tab"}) // onto kind
	tv.editBox.SetText("Folder")
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Down"})
	if got := tv.RootItems()[0].Value("kind"); got != "Folder" {
		t.Errorf("Down did not accept the edit: kind=%q", got)
	}
	if tv.CurrentIndex() != 1 {
		t.Fatalf("Down did not move the selection: index=%d", tv.CurrentIndex())
	}
	if !tv.rowEditing || tv.editCol != tv.ColumnByID("kind") {
		t.Fatalf("Down did not continue editing kind on the next row")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Up"})
	if tv.CurrentIndex() != 0 || !tv.rowEditing || tv.editCol != tv.ColumnByID("kind") {
		t.Errorf("Up did not continue editing kind on the row above")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})
}

// Ordinary keys reach the text box while editing (they must not fall
// through to tree navigation).
func TestTreeRowEditForwardsTyping(t *testing.T) {
	tv := newEditableTree()
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	// The prefill is selected; typing replaces it.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "x", Text: "x"})
	tv.HandleKeyPress(core.KeyPressEvent{Key: "y", Text: "y"})
	if got := tv.editBox.Text(); got != "xy" {
		t.Errorf("typed text = %q, want %q", got, "xy")
	}
	// The selection did not move (Up/Down are edit navigation, but a
	// plain letter must never reach the tree).
	if tv.CurrentIndex() != 0 {
		t.Errorf("typing moved the tree selection")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})
}

// Clicking off the editor accepts the value; the click then acts
// normally (here: selects the clicked row).
func TestTreeRowEditClickOffAccepts(t *testing.T) {
	tv := newEditableTree()
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	tv.editBox.SetText("42 KB")
	// Row 2 sits at y = header(16) + 2*16 + mid.
	tv.HandleMousePress(core.MousePressEvent{X: 20, Y: 16 + 2*16 + 8, Button: core.LeftButton})
	if tv.rowEditing {
		t.Fatal("click off did not dismiss the editor")
	}
	if got := tv.RootItems()[0].Value("size"); got != "42 KB" {
		t.Errorf("click off did not accept the value: size=%q", got)
	}
	if tv.CurrentIndex() != 2 {
		t.Errorf("the click did not proceed to select row 2: index=%d", tv.CurrentIndex())
	}
}

// The tree-hosting cell's editor starts where the caption text starts
// (past the indent, expander, and icon), lining up with the value it
// replaces - for the key column and, with the key hidden, the host
// data column.
func TestTreeKeyEditorRespectsIndent(t *testing.T) {
	tv := newEditableTree()
	tv.SetEditable(true)
	parent := tv.RootItems()[0]
	parent.Expanded = true
	child := NewTreeItem("nested")
	child.SetValue("size", "9 KB")
	child.SetValue("kind", "File")
	parent.AddChild(child)
	tv.rebuildFlatList()
	tv.SetCurrentItem(child) // level 1
	cw := tv.EffectiveCellMetrics().CellWidth

	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"}) // key column first
	if tv.editCol != treeKeyColumn {
		t.Fatal("precondition: editing the key column")
	}
	r, ok := tv.editorRect()
	if !ok {
		t.Fatal("editor rect unavailable")
	}
	lay := tv.columnLayout()
	wantX := lay.spans[0].x + core.Unit(1*tv.indentWidth+1+treeLeftPadCells)*cw
	if r.X != wantX {
		t.Errorf("key editor X = %d, want %d (indent+expander inset)", r.X, wantX)
	}
	if r.X+r.Width != lay.spans[0].x+lay.spans[0].w {
		t.Errorf("key editor right edge moved: %d", r.X+r.Width)
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})

	// Key hidden: the first data column hosts the apparatus, and ITS
	// editor gets the same inset.
	tv.SetShowKey(false)
	tv.SetCurrentItem(child)
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if tv.editCol != tv.ColumnByID("size") {
		t.Fatalf("edit ring without key did not start on size")
	}
	r, ok = tv.editorRect()
	if !ok {
		t.Fatal("host editor rect unavailable")
	}
	lay = tv.columnLayout()
	wantX = lay.spans[0].x + core.Unit(1*tv.indentWidth+1+treeLeftPadCells)*cw
	if r.X != wantX {
		t.Errorf("host editor X = %d, want %d", r.X, wantX)
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})
}

// In an editable grid, Left/Right rotate the Enter-target column
// (with wrap) without opening the editor, bringing it into horizontal
// view; a grid with nothing editable keeps the classic tree keys.
func TestTreeArrowsRotateEnterTarget(t *testing.T) {
	tv := newEditableTree() // size and kind editable, date not
	tv.SetEditable(true)    // the key column joins the ring: key, size, kind

	if got := tv.enterTargetColumn(); got != treeKeyColumn {
		t.Fatalf("initial target = %v, want the key column", got)
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Right"})
	if got := tv.enterTargetColumn(); got != tv.ColumnByID("size") {
		t.Fatalf("target after Right = %v, want size", got)
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Right"})
	if got := tv.enterTargetColumn(); got != tv.ColumnByID("kind") {
		t.Fatalf("target after Right,Right = %v, want kind", got)
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Right"}) // wraps
	if got := tv.enterTargetColumn(); got != treeKeyColumn {
		t.Fatalf("target after wrap = %v, want the key column", got)
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Left"}) // wraps back
	if got := tv.enterTargetColumn(); got != tv.ColumnByID("kind") {
		t.Fatalf("target after Left wrap = %v, want kind", got)
	}
	if tv.rowEditing {
		t.Fatal("rotation opened the editor")
	}
	// Enter now edits the rotated-to column.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if !tv.rowEditing || tv.editCol != tv.ColumnByID("kind") {
		t.Fatal("Enter did not edit the rotated-to column")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})

	// A non-editable grid keeps the classic Left/Right tree keys.
	plain := newColumnsTree(60, 10)
	plain.SetCurrentIndex(0) // "Folder", expanded
	folder := plain.CurrentItem()
	plain.HandleKeyPress(core.KeyPressEvent{Key: "Left"})
	if folder.Expanded {
		t.Error("Left did not collapse in a non-editable grid")
	}
}

// A selection click also selects the COLUMN as the Enter target when
// it lands on an editable cell; on a non-editable cell only the row
// changes. Drag-selection tracks both too, and never auto-enters edit
// mode.
func TestTreeMouseSelectsTargetColumn(t *testing.T) {
	tv := newEditableTree() // size and kind editable, date not
	lay := tv.columnLayout()
	spanX := func(id string) core.Unit {
		for _, sp := range lay.spans {
			if sp.col != nil && sp.col.ID == id {
				return sp.x + sp.w/2
			}
		}
		t.Fatalf("no span for %q", id)
		return 0
	}
	rowY := func(i int) core.Unit { return 16 + core.Unit(i)*16 + 8 }

	// Click row 1's Kind cell: row AND column select.
	tv.HandleMousePress(core.MousePressEvent{X: spanX("kind"), Y: rowY(1), Button: core.LeftButton})
	tv.HandleMouseRelease(core.MouseReleaseEvent{X: spanX("kind"), Y: rowY(1), Button: core.LeftButton})
	if tv.CurrentIndex() != 1 || tv.enterTargetColumn() != tv.ColumnByID("kind") {
		t.Fatalf("kind click: row=%d target=%v", tv.CurrentIndex(), tv.enterTargetColumn())
	}
	if tv.rowEditing {
		t.Fatal("first click on an unselected row entered edit mode")
	}
	// Click row 0's Date cell (non-editable): row changes, target stays.
	tv.HandleMousePress(core.MousePressEvent{X: spanX("date"), Y: rowY(0), Button: core.LeftButton})
	tv.HandleMouseRelease(core.MouseReleaseEvent{X: spanX("date"), Y: rowY(0), Button: core.LeftButton})
	if tv.CurrentIndex() != 0 || tv.enterTargetColumn() != tv.ColumnByID("kind") {
		t.Fatalf("date click: row=%d target=%v, want 0/kind", tv.CurrentIndex(), tv.enterTargetColumn())
	}
	// Drag from row 0's Date down over row 2's Size: selection follows
	// the row, the target follows the editable column, and NO edit
	// mode opens on the moved release.
	tv.HandleMousePress(core.MousePressEvent{X: spanX("date"), Y: rowY(0), Button: core.LeftButton})
	tv.HandleMouseMove(core.MouseMoveEvent{X: spanX("size"), Y: rowY(2), Buttons: 1})
	tv.HandleMouseRelease(core.MouseReleaseEvent{X: spanX("size"), Y: rowY(2), Button: core.LeftButton})
	if tv.CurrentIndex() != 2 || tv.enterTargetColumn() != tv.ColumnByID("size") {
		t.Fatalf("drag: row=%d target=%v, want 2/size", tv.CurrentIndex(), tv.enterTargetColumn())
	}
	if tv.rowEditing {
		t.Fatal("drag-selection auto-entered edit mode")
	}
}

// Rotating the target scrolls it into view with the conservative rule.
func TestTreeArrowRotationEnsuresVisible(t *testing.T) {
	tv := newColumnsTree(30, 10) // content 29 cells
	tv.SetFitWidth(false)
	tv.SetKeyWidth(20) // natural: key 20 |1| size 10 |1| kind 12 = 44
	tv.ColumnByID("size").Editable = true
	tv.ColumnByID("kind").Editable = true
	tv.SetCurrentIndex(0)

	// The target starts on size (first editable); Right rotates to
	// kind (cells 32..44), revealed at the max scroll of 15.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Right"})
	if tv.enterTargetColumn() != tv.ColumnByID("kind") || tv.hScroll != 15 {
		t.Fatalf("after Right: target=%v hScroll=%d, want kind/15", tv.enterTargetColumn(), tv.hScroll)
	}
	// Left back to size (cells 21..31): already inside the view at
	// scroll 15 - conservative, no movement.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Left"})
	if tv.enterTargetColumn() != tv.ColumnByID("size") || tv.hScroll != 15 {
		t.Fatalf("after Left: target=%v hScroll=%d, want size/15 (no movement)", tv.enterTargetColumn(), tv.hScroll)
	}
}

// Shift+Left/Right keep the classic expand/collapse everywhere: on an
// editable grid (where the plain arrows rotate the Enter-target) and
// in either spelling ("S-Right" or Right + the shift modifier).
func TestTreeShiftArrowsExpandCollapse(t *testing.T) {
	tv := newEditableTree()
	alpha := tv.RootItems()[0]
	alpha.AddChild(NewTreeItem("a1"))
	tv.rebuildFlatList()
	tv.SetCurrentIndex(0)

	// Plain Right rotates the target and must NOT expand the folder.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Right"})
	if alpha.Expanded {
		t.Fatal("plain Right expanded the folder on an editable grid")
	}
	if tv.enterTargetColumn() != tv.ColumnByID("kind") {
		t.Fatalf("plain Right did not rotate the target: %v", tv.enterTargetColumn())
	}

	tv.HandleKeyPress(core.KeyPressEvent{Key: "S-Right"})
	if !alpha.Expanded {
		t.Fatal("S-Right did not expand the folder")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Left", Modifiers: core.ShiftModifier})
	if alpha.Expanded {
		t.Fatal("shifted Left did not collapse the folder")
	}
	if tv.enterTargetColumn() != tv.ColumnByID("kind") {
		t.Errorf("shifted arrows moved the Enter-target: %v", tv.enterTargetColumn())
	}
}

// Committing an edit keeps the edited row in view even when the user
// scrolled elsewhere mid-edit and the new value re-sorts the row far
// away - an explicit edit is an action ON that row.
func TestTreeEditCommitScrollsIntoView(t *testing.T) {
	tv := NewTreeView()
	tv.SetShowHeader(true)
	tv.SetEditable(true)
	tv.AddColumn(NewTreeColumn("size", "Size", 10))
	for i := 0; i < 40; i++ {
		tv.AddRootItem(NewTreeItem(fmtItem(i)))
	}
	tv.SetBounds(core.UnitRect{Width: 480, Height: 160})
	tv.SetSorted(true, -1, false)
	tv.SetCurrentIndex(0)
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	tv.editBox.SetText("zzz-last")
	tv.scrollOffset = 20 // the user wheels away mid-edit
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if tv.currentIndex != 39 {
		t.Fatalf("edited row index = %d, want 39", tv.currentIndex)
	}
	vc := tv.visibleCount()
	if tv.currentIndex < tv.scrollOffset || tv.currentIndex >= tv.scrollOffset+vc {
		t.Errorf("edited row out of view after commit: offset=%d idx=%d vc=%d",
			tv.scrollOffset, tv.currentIndex, vc)
	}
}

func fmtItem(i int) string {
	return string([]byte{'i', 't', 'e', 'm', byte('0' + i/10), byte('0' + i%10)})
}

// While the row editor is up, the Edit menu's focus inspection sees
// the CELL EDITOR as the edit target (pass-through via the tree, which
// holds the real focus); with the editor closed the tree is no target.
func TestTreeEditActorPassThrough(t *testing.T) {
	b, _ := raster.New(320, 200) // raster backend: real internal clipboard
	d := NewDesktop()
	d.SetBackend(b)
	tv := newEditableTree()
	tv.SetParent(d)
	tv.SetFocus()

	// The Edit menu inspects the FOCUSED trinket (the tree - it holds
	// real focus while editing) through editActorProvider; exercise
	// that seam exactly as focusedEditActor consumes it.
	prov, isProvider := core.Trinket(tv.Self()).(editActorProvider)
	if !isProvider {
		t.Fatal("TreeView does not implement editActorProvider")
	}
	if _, active := prov.editActorTarget(); active {
		t.Fatal("tree with no open editor claimed to be an edit target")
	}
	// And a closed-editor tree is no plain editActor either: every
	// standard Edit item stays disabled.
	if _, isActor := core.Trinket(tv.Self()).(editActor); isActor {
		t.Fatal("TreeView must not be an unconditional editActor")
	}

	tv.HandleKeyPress(core.KeyPressEvent{Key: "Down"})  // header bar -> content
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"}) // open the editor
	if !tv.rowEditing {
		t.Fatal("precondition: row editor open")
	}
	ea, ok := prov.editActorTarget()
	if !ok {
		t.Fatal("editActorTarget did not surface the cell editor")
	}
	// The pass-through target IS the TextInput: Select All + Copy land
	// on it, and the clipboard resolves through the tree's desktop.
	ea.SelectAll()
	if tv.editBox.SelectedText() != "1 KB" {
		t.Errorf("SelectAll via Edit-menu target selected %q", tv.editBox.SelectedText())
	}
	ea.Copy()
	if got := d.Clipboard(); got != "1 KB" {
		t.Errorf("clipboard after Copy = %q, want %q", got, "1 KB")
	}
	// Paste replaces the selection through the same bridge.
	d.SetClipboard("2 MB")
	ea.Paste()
	if got := tv.editBox.Text(); got != "2 MB" {
		t.Errorf("editor text after Paste = %q, want %q", got, "2 MB")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})
	if _, active := prov.editActorTarget(); active {
		t.Error("closed editor still reported as the edit target")
	}
}

// Right-clicking inside the row editor opens the TextInput's own
// context menu on the tree's popup controller, positioned through the
// tree's ancestry (the unparented editor borrows it).
func TestTreeEditContextMenu(t *testing.T) {
	tv := newEditableTree()
	host := &recordingPopupController{}
	parent := NewPanel()
	parent.SetPopupController(host)
	tv.SetParent(parent)
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if !tv.rowEditing {
		t.Fatal("precondition: row editor open")
	}
	r, ok := tv.editorRect()
	if !ok {
		t.Fatal("precondition: editor visible")
	}
	press := core.MousePressEvent{X: r.X + 4, Y: r.Y + 4, Button: core.RightButton}
	if !tv.HandleMousePress(press) {
		t.Fatal("right-click inside the editor not handled")
	}
	if host.popup == nil {
		t.Fatal("no context menu popup registered")
	}
	// The recording controller's MapToScreen is identity, so the menu
	// opens exactly at the click point - proving the editor's local
	// coordinates were mapped through the tree with the cell origin.
	if host.popup.Bounds.X != press.X || host.popup.Bounds.Y != press.Y {
		t.Errorf("context menu at %d,%d want %d,%d",
			host.popup.Bounds.X, host.popup.Bounds.Y, press.X, press.Y)
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})
}

// Switching edit columns brings the editor into view with the
// ScrollArea's conservative rule: scroll the minimum needed, and not
// at all when the cell is already fully visible.
func TestTreeEditEnsuresColumnVisible(t *testing.T) {
	tv := newColumnsTree(30, 10) // TUI: content 29 cells
	tv.SetFitWidth(false)
	tv.SetKeyWidth(20) // natural: key 20 |1| size 10 |1| kind 12 = 44
	tv.ColumnByID("size").Editable = true
	tv.ColumnByID("kind").Editable = true
	tv.SetCurrentIndex(0)

	// Entering edit on Size (cells 21..31, view 29 wide, hScroll 0):
	// the right edge is 2 cells past the view - scroll exactly 2.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if !tv.rowEditing || tv.editCol != tv.ColumnByID("size") {
		t.Fatal("precondition: editing size")
	}
	if tv.hScroll != 2 {
		t.Errorf("hScroll after edit start = %d, want 2", tv.hScroll)
	}
	// Tab to Kind (cells 32..44): right-align to it, clamped to the
	// max scroll (15).
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Tab"})
	if tv.hScroll != 15 {
		t.Errorf("hScroll after Tab to kind = %d, want 15", tv.hScroll)
	}
	// S-Tab back to Size: at hScroll 15 its cells 21..31 already sit
	// inside the view (15..44) - conservative: NO movement.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "S-Tab"})
	if tv.editCol != tv.ColumnByID("size") {
		t.Fatal("S-Tab did not return to size")
	}
	if tv.hScroll != 15 {
		t.Errorf("hScroll moved for an already-visible column: %d, want 15", tv.hScroll)
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})

	// A pinned column is always in view: editing it never scrolls.
	tv.SetFixedColumns(0, 1) // pin kind
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Tab"}) // onto pinned kind
	if tv.editCol != tv.ColumnByID("kind") {
		t.Fatal("Tab did not reach kind")
	}
	if tv.hScroll != 15 {
		t.Errorf("editing a pinned column scrolled the region: %d, want 15", tv.hScroll)
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})
}

// A drag-free click on an editable cell of the ALREADY selected row
// flips straight into edit mode - no settle delay.
func TestTreeClickToEdit(t *testing.T) {
	tv := newEditableTree()
	lay := tv.columnLayout()
	var sizeX core.Unit
	for _, sp := range lay.spans {
		if sp.col == tv.ColumnByID("size") {
			sizeX = sp.x + sp.w/2
		}
	}
	rowY := core.Unit(16 + 8) // row 0, already selected

	press := core.MousePressEvent{X: sizeX, Y: rowY, Button: core.LeftButton}
	release := core.MouseReleaseEvent{X: sizeX, Y: rowY, Button: core.LeftButton}
	tv.HandleMousePress(press)
	if tv.clickEditItem == nil {
		t.Fatal("press on selected editable cell did not become a candidate")
	}
	if tv.rowEditing {
		t.Fatal("edit began on press; it must wait for the drag-free release")
	}
	tv.HandleMouseRelease(release)
	if !tv.rowEditing || tv.editCol != tv.ColumnByID("size") {
		t.Fatal("release did not flip the cell straight into edit mode")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})

	// A drag between press and release never triggers it.
	tv.HandleMousePress(press)
	tv.HandleMouseRelease(core.MouseReleaseEvent{X: sizeX + 40, Y: rowY, Button: core.LeftButton})
	if tv.rowEditing {
		t.Error("dragged release entered edit mode")
	}

	// A click on a NOT-yet-selected row never triggers (it selects).
	tv.SetCurrentIndex(0)
	tv.HandleMousePress(core.MousePressEvent{X: sizeX, Y: 16 + 2*16 + 8, Button: core.LeftButton})
	if tv.clickEditItem != nil {
		t.Error("press on an unselected row became a click-to-edit candidate")
	}
	tv.HandleMouseRelease(core.MouseReleaseEvent{X: sizeX, Y: 16 + 2*16 + 8, Button: core.LeftButton})
	if tv.rowEditing {
		t.Error("selecting click entered edit mode")
	}
	// But the NEXT click on that now-selected row does.
	tv.HandleMousePress(core.MousePressEvent{X: sizeX, Y: 16 + 2*16 + 8, Button: core.LeftButton})
	tv.HandleMouseRelease(core.MouseReleaseEvent{X: sizeX, Y: 16 + 2*16 + 8, Button: core.LeftButton})
	if !tv.rowEditing {
		t.Error("second click on the newly selected row did not enter edit mode")
	}
}
