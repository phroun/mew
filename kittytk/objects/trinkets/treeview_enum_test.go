package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/style"
)

// newEnumTree: key + Kind enum column (store=key) with a popup
// controller so the combo editor can open its drop-down.
func newEnumTree(kindValue string) (*TreeView, *recordingPopupController) {
	tv := NewTreeView()
	tv.SetShowHeader(true)
	kind := NewTreeColumn("kind", "Kind", 14)
	kind.Editable = true
	kind.Enum = []TreeEnumOption{
		{Key: "png", Value: "PNG image"},
		{Key: "txt", Value: "Text"},
	}
	kind.EnumStore = "key"
	tv.AddColumn(kind)
	for _, name := range []string{"alpha", "beta"} {
		it := NewTreeItem(name)
		it.SetValue("kind", kindValue)
		tv.AddRootItem(it)
	}
	tv.SetBounds(core.UnitRect{Width: 480, Height: 160})
	tv.SetCurrentIndex(0)
	host := &recordingPopupController{}
	parent := NewPanel()
	parent.SetPopupController(host)
	tv.SetParent(parent)
	return tv, host
}

// A key-storing enum column DISPLAYS the option value; unknown keys
// fall back to the raw text; value-storing columns show raw text.
func TestTreeColumnDisplayValue(t *testing.T) {
	c := NewTreeColumn("kind", "Kind", 10)
	c.Enum = []TreeEnumOption{{Key: "png", Value: "PNG image"}}
	c.EnumStore = "key"
	if got := c.displayValue("png"); got != "PNG image" {
		t.Errorf("display(png) = %q", got)
	}
	if got := c.displayValue("weird"); got != "weird" {
		t.Errorf("display(weird) = %q", got)
	}
	c.EnumStore = "value"
	if got := c.displayValue("PNG image"); got != "PNG image" {
		t.Errorf("value-store display = %q", got)
	}
}

// The enum cell editor is a CLOSED ComboBox: Space pops it open, the
// open popup owns Up/Down (Space confirms and keeps editing, Enter
// confirms and commits the row), the closed box treats Up/Down as row
// navigation, and the committed value stores the option KEY here.
func TestTreeEnumComboEditLifecycle(t *testing.T) {
	tv, _ := newEnumTree("png")
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if tv.editCombo == nil || tv.editBox != nil {
		t.Fatal("enum column did not mount a combo editor")
	}
	// Direct Enter on the choice target pops the drop-down.
	if !tv.editCombo.IsOpen() {
		t.Fatal("direct Enter did not pop the drop-down")
	}
	if tv.editComboMagic {
		t.Fatal("stored value is a listed option: no magic entry expected")
	}
	if got := tv.editCombo.CurrentText(); got != "PNG image" {
		t.Fatalf("combo shows %q, want the option VALUE", got)
	}
	// Enum cells do not participate in the Edit menu.
	if _, active := tv.editActorTarget(); active {
		t.Error("combo cell claimed to be an Edit-menu target")
	}
	// Space confirms the current highlight and KEEPS the edit session
	// alive, leaving the closed-combo state to exercise below.
	tv.HandleKeyPress(core.KeyPressEvent{Key: " "})
	if tv.editCombo.IsOpen() || !tv.rowEditing {
		t.Fatal("popup Space should close the drop-down and keep editing")
	}
	// Closed combo: Down is ROW navigation (value untouched).
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Down"})
	if tv.CurrentIndex() != 1 || tv.editCombo == nil {
		t.Fatalf("Down on closed combo: index=%d combo=%v", tv.CurrentIndex(), tv.editCombo != nil)
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Up"})
	if tv.CurrentIndex() != 0 {
		t.Fatalf("Up on closed combo: index=%d", tv.CurrentIndex())
	}
	// Space pops the drop-down; while open, Down+Space pick "Text" and
	// KEEP the edit session alive (Enter there would commit the row).
	tv.HandleKeyPress(core.KeyPressEvent{Key: " "})
	if !tv.editCombo.IsOpen() {
		t.Fatal("Space did not open the drop-down")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Down"})
	tv.HandleKeyPress(core.KeyPressEvent{Key: " "})
	if tv.editCombo.IsOpen() {
		t.Fatal("Space did not confirm/close the drop-down")
	}
	if !tv.rowEditing {
		t.Fatal("Space-confirming the drop-down must keep the row edit alive")
	}
	if got := tv.editCombo.CurrentText(); got != "Text" {
		t.Fatalf("confirmed choice shows %q", got)
	}
	// Enter on the closed combo commits the row: the KEY is stored.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if tv.rowEditing {
		t.Fatal("row edit did not close")
	}
	if got := tv.RootItems()[0].Value("kind"); got != "txt" {
		t.Errorf("stored value = %q, want the option key %q", got, "txt")
	}
}

// A stored value that is NOT in the enum gets the magic head entry:
// visible and re-selectable during the session, keeping the cell
// unchanged - and gone once a listed option is stored.
func TestTreeEnumMagicEntry(t *testing.T) {
	tv, _ := newEnumTree("weird")
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"}) // edit + popup
	tv.HandleKeyPress(core.KeyPressEvent{Key: " "})     // confirm magic, stay editing
	if !tv.editComboMagic {
		t.Fatal("unlisted stored value did not create the magic entry")
	}
	if got := tv.editCombo.CurrentText(); got != "weird" {
		t.Fatalf("magic entry shows %q", got)
	}
	// Committing with the magic entry selected changes nothing.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if got := tv.RootItems()[0].Value("kind"); got != "weird" {
		t.Errorf("magic commit rewrote the value: %q", got)
	}
	// Pick a real option (magic sits at 0; options follow). Enter
	// reopens the editor with the drop-down already popped, and Enter
	// on the highlighted option confirms it AND commits the row.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Down"}) // onto "PNG image"
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if got := tv.RootItems()[0].Value("kind"); got != "png" {
		t.Fatalf("stored value = %q, want png", got)
	}
	// The magic entry is gone on the next session.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if tv.editComboMagic {
		t.Error("magic entry survived after a listed option was stored")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"}) // cancel the edit
}

// Enter DIRECTLY on a choice Enter-target pops the drop-down; arriving
// at a choice cell via Tab from another column leaves it closed.
func TestTreeEnterPopsTargetedCombo(t *testing.T) {
	// Direct Enter on the (only-editable) enum column: pops open.
	tv, _ := newEnumTree("png")
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if tv.editCombo == nil || !tv.editCombo.IsOpen() {
		t.Fatal("Enter on a choice target did not pop the drop-down")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"}) // cancel the edit

	// Tab-arrival stays closed: text column first, then Tab to enum.
	tv2 := newEditableTree() // size, kind editable (text)
	kind := tv2.ColumnByID("kind")
	kind.Enum = []TreeEnumOption{{Key: "f", Value: "File"}}
	kind.EnumStore = "key"
	host := &recordingPopupController{}
	parent := NewPanel()
	parent.SetPopupController(host)
	tv2.SetParent(parent)
	tv2.HandleKeyPress(core.KeyPressEvent{Key: "Enter"}) // edits size (text)
	if tv2.editBox == nil {
		t.Fatal("precondition: text editor on size")
	}
	tv2.HandleKeyPress(core.KeyPressEvent{Key: "Tab"}) // onto the enum kind
	if tv2.editCombo == nil {
		t.Fatal("Tab did not mount the combo")
	}
	if tv2.editCombo.IsOpen() {
		t.Error("Tab-arrival popped the drop-down; it must stay closed")
	}
	tv2.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})
}

// Space on a CHOICE Enter-target also enters edit and pops the
// drop-down; on a TEXT target it keeps the classic toggle (Space
// never begins a text edit).
func TestTreeSpacePopsTargetedCombo(t *testing.T) {
	tv, _ := newEnumTree("png")
	tv.HandleKeyPress(core.KeyPressEvent{Key: " "})
	if !tv.rowEditing || tv.editCombo == nil || !tv.editCombo.IsOpen() {
		t.Fatal("Space on a choice target did not open the combo editor")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"}) // cancel the edit

	tv2 := newEditableTree() // Enter-target starts on size (text)
	alpha := tv2.RootItems()[0]
	alpha.AddChild(NewTreeItem("a1"))
	alpha.Expanded = true
	tv2.rebuildFlatList()
	tv2.SetCurrentIndex(0)
	tv2.HandleKeyPress(core.KeyPressEvent{Key: " "})
	if tv2.rowEditing {
		t.Fatal("Space began a text edit; it must keep the classic toggle")
	}
	if alpha.Expanded {
		t.Error("Space on a text target did not collapse the folder")
	}
}

// Escape cancels the WHOLE edit in one press: from the open popup it
// reverts the highlight and dismisses the row edit; from the closed
// combo it dismisses too. Nothing is written either way.
func TestTreeEnumEscapeCancels(t *testing.T) {
	tv, _ := newEnumTree("png")
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})  // edit + popup
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Down"})   // highlight "Text"
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"}) // revert + cancel edit
	if tv.rowEditing {
		t.Fatal("popup Escape should cancel the whole edit")
	}
	if got := tv.RootItems()[0].Value("kind"); got != "png" {
		t.Errorf("popup Escape wrote a value: %q", got)
	}

	// Closed combo (a Space-confirm keeps the session alive): Escape
	// dismisses without writing the confirmed-but-uncommitted choice.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"}) // edit + popup
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Down"})  // highlight "Text"
	tv.HandleKeyPress(core.KeyPressEvent{Key: " "})     // confirm, stay editing
	if !tv.rowEditing || tv.editCombo.IsOpen() {
		t.Fatal("precondition: live edit on a closed combo")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})
	if tv.rowEditing {
		t.Fatal("closed-combo Escape did not dismiss")
	}
	if got := tv.RootItems()[0].Value("kind"); got != "png" {
		t.Errorf("Escape wrote a value: %q", got)
	}
}

// Enter in the OPEN popup confirms the highlighted value AND commits
// the whole row edit in one press.
func TestTreeEnumPopupEnterCommitsRow(t *testing.T) {
	tv, _ := newEnumTree("png")
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"}) // edit + popup
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Down"})  // highlight "Text"
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if tv.rowEditing {
		t.Fatal("popup Enter did not commit the row edit")
	}
	if got := tv.RootItems()[0].Value("kind"); got != "txt" {
		t.Fatalf("stored %q, want the option key txt", got)
	}
}

// SetEditable puts the KEY column in the edit ring (first): the text
// editor edits the item's caption, and the observer reports the key
// sentinel (wire consumers see column -1).
func TestTreeKeyColumnEditable(t *testing.T) {
	tv := newEditableTree()
	tv.SetEditable(true)
	var gotCol *TreeColumn
	tv.SetOnCellEdited(func(_ *TreeItem, col *TreeColumn, _ string) { gotCol = col })

	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if tv.editCol != treeKeyColumn || tv.editBox == nil {
		t.Fatalf("edit ring did not start on the key column")
	}
	if got := tv.editBox.Text(); got != "alpha" {
		t.Fatalf("key editor prefill = %q", got)
	}
	tv.editBox.SetText("omega")
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Tab"}) // commit, onto size
	if got := tv.RootItems()[0].Text; got != "omega" {
		t.Errorf("key edit did not write the caption: %q", got)
	}
	if gotCol != treeKeyColumn {
		t.Errorf("observer column = %v, want the key sentinel", gotCol)
	}
	if tv.editCol != tv.ColumnByID("size") {
		t.Fatalf("Tab after key did not reach size")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "S-Tab"}) // back to the key
	if tv.editCol != treeKeyColumn {
		t.Fatalf("S-Tab did not wrap back to the key column")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})

	// Key hidden: the key leaves the ring.
	tv.SetShowKey(false)
	for _, c := range tv.editableColumns() {
		if c == treeKeyColumn {
			t.Error("hidden key column still in the edit ring")
		}
	}
}

// Pixel surfaces paint row bands (selection/ledger) under the vertical
// scrollbar lane; the slim scrollbar overlays them. (The TUI keeps the
// whole lane reserved.)
func TestTreeRowFillUnderScrollbarLane(t *testing.T) {
	b, _ := raster.New(480, 160)
	d := NewDesktop()
	d.SetBackend(b)
	tv := NewTreeView()
	tv.SetParent(d)
	tv.SetShowHeader(true)
	tv.AddColumn(NewTreeColumn("size", "Size", 10))
	for _, name := range []string{"aaa", "bbb", "ccc"} {
		tv.AddRootItem(NewTreeItem(name))
	}
	tv.SetLedger(true)
	tv.SetCurrentIndex(0)
	tv.SetBounds(core.UnitRect{Width: 480, Height: 160})
	b.Clear(style.DefaultStyle())
	tv.Paint(core.NewPainter(b))

	scheme := tv.GetScheme()
	selR, selG, selB := scheme.GetSelectedListItem().Bg.RGBComponents()
	evenR, evenG, evenB := scheme.GetLedgerEven().Bg.RGBComponents()
	// x=474 sits in the scrollbar lane (last cell column of 480px);
	// sample just inside the right edge where the slim bar's stripe
	// and thumb do not cover every pixel.
	c := b.Image().RGBAAt(479, 16+8) // selected row 0
	if c.R != selR || c.G != selG || c.B != selB {
		t.Errorf("selected row under scrollbar lane = %d,%d,%d want %d,%d,%d",
			c.R, c.G, c.B, selR, selG, selB)
	}
	c = b.Image().RGBAAt(479, 32+8) // ledger-even row 1
	if c.R != evenR || c.G != evenG || c.B != evenB {
		t.Errorf("ledger row under scrollbar lane = %d,%d,%d want %d,%d,%d",
			c.R, c.G, c.B, evenR, evenG, evenB)
	}
}

// A double click on an EDITABLE cell enters edit mode WITHOUT also
// expanding/collapsing; the classic toggle survives on non-editable
// cells and on the tree cell's indent/expander region.
func TestTreeDoubleClickEditableSuppressed(t *testing.T) {
	tv := NewTreeView()
	tv.SetShowHeader(true)
	tv.SetEditable(true) // the key column itself is editable
	size := NewTreeColumn("size", "Size", 10)
	size.Editable = true
	kind := NewTreeColumn("kind", "Kind", 12) // NOT editable
	tv.AddColumn(size)
	tv.AddColumn(kind)
	// A level-1 folder, so its key cell has an indent PAD region left
	// of the expander arrow (level 0 has no padding before the arrow).
	top := NewTreeItem("top")
	top.Expanded = true
	folder := NewTreeItem("Folder")
	folder.AddChild(NewTreeItem("inner"))
	folder.SetValue("size", "--")
	folder.SetValue("kind", "Folder")
	top.AddChild(folder)
	tv.AddRootItem(top)
	tv.SetBounds(core.UnitRect{Width: 480, Height: 160})

	cw := tv.EffectiveCellMetrics().CellWidth
	lay := tv.columnLayout()
	var sizeX, kindX, keyTextX, keyPadX core.Unit
	for _, sp := range lay.spans {
		switch {
		case sp.col == nil:
			keyPadX = sp.x + cw + 4 // inside the level-1 indent, left of the arrow
			// Text starts after pad + indent(2 cells) + expander(1 cell).
			keyTextX = sp.x + core.Unit(3+treeLeftPadCells)*cw + 4
		case sp.col.ID == "size":
			sizeX = sp.x + sp.w/2
		case sp.col.ID == "kind":
			kindX = sp.x + sp.w/2
		}
	}
	rowY := core.Unit(16 + 1*16 + 8) // the folder's visual row (index 1)
	dbl := func(x core.Unit) {
		tv.HandleMousePress(core.MousePressEvent{X: x, Y: rowY, Button: core.LeftButton})
		tv.HandleMouseRelease(core.MouseReleaseEvent{X: x, Y: rowY, Button: core.LeftButton})
		tv.HandleMousePress(core.MousePressEvent{X: x, Y: rowY, Button: core.LeftButton})
		tv.HandleMouseRelease(core.MouseReleaseEvent{X: x, Y: rowY, Button: core.LeftButton})
	}

	// Editable Size cell: the double click edits, never toggles.
	dbl(sizeX)
	if folder.Expanded {
		t.Error("double click on an editable cell also expanded the row")
	}
	if !tv.rowEditing || tv.editCol != tv.ColumnByID("size") {
		t.Fatal("double click on editable cell did not enter edit mode")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})

	// Non-editable Kind cell: classic toggle.
	dbl(kindX)
	if !folder.Expanded {
		t.Error("double click on a non-editable cell did not expand")
	}
	if tv.rowEditing {
		t.Fatal("double click on non-editable cell entered edit mode")
	}

	// The key cell's indent padding (left of the arrow): classic
	// toggle even though the key column is editable.
	dbl(keyPadX)
	if folder.Expanded {
		t.Error("double click on the indent padding did not collapse")
	}
	if tv.rowEditing {
		t.Fatal("indent-padding double click entered edit mode")
	}

	// The key cell's TEXT: edits (the key is editable here).
	dbl(keyTextX)
	if folder.Expanded {
		t.Error("double click on the key text also expanded")
	}
	if !tv.rowEditing || tv.editCol != treeKeyColumn {
		t.Fatal("double click on the key text did not edit the caption")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})

	// RIGHT of the caption text (past its displayed width): classic
	// toggle again - the Finder/Explorer convention.
	font := tv.EffectiveFont()
	beyondX := lay.spans[0].x + core.Unit(3+treeLeftPadCells)*cw + font.MeasureText("Folder") + 8
	dbl(beyondX)
	if !folder.Expanded {
		t.Error("double click right of the text did not expand")
	}
	if tv.rowEditing {
		t.Fatal("double click right of the text entered edit mode")
	}
}

// A blank (or tiny) caption still offers a two-cell edit zone after
// the text start; clicks past those two cells fall back to the
// classic behavior.
func TestTreeKeyBlankCaptionEditZone(t *testing.T) {
	tv := newEditableTree()
	tv.SetEditable(true)
	blank := NewTreeItem("")
	blank.SetValue("size", "1 KB")
	tv.AddRootItem(blank)
	tv.rebuildFlatList()
	tv.SetCurrentItem(blank)
	cw := tv.EffectiveCellMetrics().CellWidth
	lay := tv.columnLayout()
	textX := lay.spans[0].x + core.Unit(1+treeLeftPadCells)*cw // level 0: pad, expander cell, then text
	rowY := core.Unit(16 + 3*16 + 8) // blank sits at visual row 3

	tv.HandleMousePress(core.MousePressEvent{X: textX + cw, Y: rowY, Button: core.LeftButton})
	if tv.clickEditItem != blank || tv.clickEditCol != treeKeyColumn {
		t.Fatal("blank caption's two-cell zone did not arm click-to-edit")
	}
	tv.HandleMouseRelease(core.MouseReleaseEvent{X: textX + cw, Y: rowY, Button: core.LeftButton})
	if !tv.rowEditing || tv.editCol != treeKeyColumn {
		t.Fatal("blank-zone click did not enter edit mode")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})

	// Past the two-cell minimum: no edit candidate.
	tv.HandleMousePress(core.MousePressEvent{X: textX + 2*cw + 4, Y: rowY, Button: core.LeftButton})
	if tv.clickEditItem != nil {
		t.Error("click past the two-cell zone became an edit candidate")
	}
	tv.HandleMouseRelease(core.MouseReleaseEvent{X: textX + 2*cw + 4, Y: rowY, Button: core.LeftButton})
	if tv.rowEditing {
		t.Error("click past the two-cell zone entered edit mode")
	}
}

// An editable enum column's measured width keeps one extra cell for
// the combo editor's drop-down arrow, even while not editing.
func TestTreeEnumColumnReservesArrowRoom(t *testing.T) {
	tv := NewTreeView()
	plain := NewTreeColumn("a", "Kind", 8)
	choice := NewTreeColumn("b", "Kind", 8)
	choice.Editable = true
	choice.Enum = []TreeEnumOption{{Key: "x", Value: "X"}}
	tv.AddColumn(plain)
	tv.AddColumn(choice)
	it := NewTreeItem("row")
	it.SetValue("a", "PNG image")
	it.SetValue("b", "PNG image")
	tv.AddRootItem(it)
	tv.SetBounds(core.UnitRect{Width: 480, Height: 160})

	if got, want := tv.neededCells(choice), tv.neededCells(plain)+1; got != want {
		t.Errorf("choice column needs %d cells, want %d (plain+1 for the arrow)", got, want)
	}
}

// MOUSE click-to-edit on a choice cell pops the drop-down immediately
// (keyboard entry leaves it closed for Tab/arrow pass-through).
func TestTreeClickToEditComboPopsOpen(t *testing.T) {
	tv, _ := newEnumTree("png")
	lay := tv.columnLayout()
	var kindX core.Unit
	for _, sp := range lay.spans {
		if sp.col == tv.ColumnByID("kind") {
			kindX = sp.x + sp.w/2
		}
	}
	rowY := core.Unit(16 + 8) // row 0, already selected
	tv.HandleMousePress(core.MousePressEvent{X: kindX, Y: rowY, Button: core.LeftButton})
	tv.HandleMouseRelease(core.MouseReleaseEvent{X: kindX, Y: rowY, Button: core.LeftButton})
	if tv.editCombo == nil {
		t.Fatal("click-to-edit did not mount the combo")
	}
	if !tv.editCombo.IsOpen() {
		t.Error("mouse entry did not pop the drop-down open")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"}) // close popup
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"}) // end edit

	// Direct keyboard Enter on the choice target also pops it open
	// (only Tab-arrival from another column stays closed).
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if tv.editCombo == nil || !tv.editCombo.IsOpen() {
		t.Error("keyboard Enter on a choice target should pop the drop-down")
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})
}

// Enum options travel the wire: a collection of options built first,
// then pointed at by the column's enum= (a pointer property resolved
// through the connection's reference registry); enum_store selects the
// stored side. The treeview-level editable flag applies too.
func TestTreeEnumOverWire(t *testing.T) {
	ctx := &protocol.BindContext{}
	f := &captureFactory{inner: protocol.NewRegistryFactory(ctx)}
	s := protocol.NewSession()

	build := `
kinds=new collection of=options children={
	new option key=png value="PNG image"
	new option key=txt value="Text"
}
tree=new treeview editable children={
	kindc=new column id=kind caption="Kind" editable
	a=new item caption="file"
}
`
	script, err := protocol.Parse(build)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := s.Execute(script, f)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var tv *TreeView
	for _, target := range f.targets {
		if v, ok := target.(*TreeView); ok {
			tv = v
		}
	}
	if tv == nil {
		t.Fatal("no treeview built")
	}
	if !tv.keyEditable {
		t.Error("treeview editable flag not applied")
	}

	bind, err := protocol.Parse(`set tree.kindc enum=` + itoa(reply.IDs["kinds"]) + ` enum_store=key`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(bind, f); err != nil {
		t.Fatalf("enum bind: %v", err)
	}
	col := tv.ColumnByID("kind")
	if len(col.Enum) != 2 || col.Enum[0].Key != "png" || col.Enum[1].Value != "Text" {
		t.Fatalf("enum options misapplied: %+v", col.Enum)
	}
	if col.EnumStore != "key" {
		t.Errorf("enum_store = %q", col.EnumStore)
	}
}

// A wire-side caption change under an active visual sort re-sorts the
// rows (the name IS the sort key here) instead of leaving stale order.
func TestTreeWireCaptionResorts(t *testing.T) {
	ctx := &protocol.BindContext{}
	f := &captureFactory{inner: protocol.NewRegistryFactory(ctx)}
	s := protocol.NewSession()
	script, err := protocol.Parse(`
tree=new treeview sorted sortedby=-1 children={
	b=new item caption="bbb"
	a=new item caption="aaa"
	c=new item caption="ccc"
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(script, f); err != nil {
		t.Fatal(err)
	}
	tv := f.targets[0].(*TreeView)
	if got := visualCaptions(tv); !equalStrings(got, []string{"aaa", "bbb", "ccc"}) {
		t.Fatalf("precondition order = %v", got)
	}
	rename, err := protocol.Parse(`set tree.a caption="zzz"`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(rename, f); err != nil {
		t.Fatal(err)
	}
	if got := visualCaptions(tv); !equalStrings(got, []string{"bbb", "ccc", "zzz"}) {
		t.Errorf("order after wire rename = %v, want [bbb ccc zzz]", got)
	}
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
