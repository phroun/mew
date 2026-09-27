package trinkets

// A tree's cell editor is deliberately not parented into the tree -- the tree
// draws it over the cell it is editing -- so the ordinary walk up from it
// reaches nothing. What is above still has to hear about a keystroke: a window
// whose revision never moves looks unchanged to a compositor caching its
// texture, and the letter just typed waits for whatever dirties that window
// next.

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/objects/window"
)

// treeInAWindow is an editable one-row tree, in a panel, in a window.
func treeInAWindow(t *testing.T) (*window.Window, *TreeView, *TreeItem) {
	t.Helper()
	tree := NewTreeView()
	tree.SetEditable(true)
	// A second column: the row editor is the multi-column apparatus, and the
	// window this stands in for has three.
	if err := tree.AddColumn(&TreeColumn{ID: "detail", Caption: "Detail"}); err != nil {
		t.Fatal(err)
	}
	item := NewTreeItem("nickname")
	tree.AddRootItem(item)

	content := NewPanel()
	content.SetLayoutManager(layout.NewVBoxLayout())
	content.AddChild(tree)

	win := window.NewWindow("Connections")
	win.SetContent(content)
	win.SetBounds(core.UnitRect{Width: 400, Height: 200})
	return win, tree, item
}

// Typing into the editor moves the window's revision, which is the signal that
// what it holds is not what was last drawn.
func TestAKeystrokeInACellEditorReachesTheWindow(t *testing.T) {
	win, tree, item := treeInAWindow(t)
	tree.SetCurrentItem(item)
	tree.beginCellEdit(item, treeKeyColumn)
	if tree.editBox == nil {
		t.Fatal("no editor was mounted, so nothing was typed into")
	}

	before := win.SubtreeRepaintRevision()
	tree.editBox.HandleKeyPress(core.KeyPressEvent{Key: "x", Text: "x"})

	if tree.editBox.Text() == "nickname" {
		t.Fatal("the key never reached the editor, so nothing was proved")
	}
	if got := win.SubtreeRepaintRevision(); got == before {
		t.Error("the window's revision did not move, so the compositor keeps the " +
			"texture it already has and the typed letter waits for something else")
	}
}

// The same for a choice cell's editor, which is embedded the same way.
func TestAChoiceCellEditorReachesTheWindowToo(t *testing.T) {
	win, tree, item := treeInAWindow(t)
	col := &TreeColumn{
		ID:       "verdict",
		Caption:  "Verdict",
		Editable: true,
		Enum:     []TreeEnumOption{{Key: "a", Value: "allowed"}, {Key: "d", Value: "denied"}},
	}
	if err := tree.AddColumn(col); err != nil {
		t.Fatal(err)
	}
	item.SetValue("verdict", "allowed")
	tree.SetCurrentItem(item)
	tree.beginCellEdit(item, col)
	if tree.editCombo == nil {
		t.Fatal("no choice editor was mounted")
	}

	before := win.SubtreeRepaintRevision()
	tree.editCombo.Update()
	if got := win.SubtreeRepaintRevision(); got == before {
		t.Error("a choice editor's repaint stops at the editor")
	}
}

// An editor with no host and no parent is nobody's business but its own: it
// updates without reaching for anything, rather than panicking.
func TestAnEditorStandingAloneIsNoTrouble(t *testing.T) {
	NewTextInput().SetText("alone")
	NewComboBox().Update()
}

// A row can be held out of the editor while its column stays editable, since a
// list often holds one row that is not the same kind of thing as the rest.
func TestAReadOnlyRowIsNotWrittenIn(t *testing.T) {
	_, tree, item := treeInAWindow(t)
	fixed := NewTreeItem("this host")
	fixed.ReadOnly = true
	tree.AddRootItem(fixed)

	// The ordinary row still edits, by keyboard and by click alike.
	tree.SetCurrentItem(item)
	if !tree.startRowEdit() {
		t.Fatal("an editable row refused the editor")
	}
	tree.endRowEdit(false)

	tree.SetCurrentItem(fixed)
	if tree.startRowEdit() {
		t.Error("a read-only row opened the editor")
	}
	if tree.rowEditing {
		t.Error("the tree went into edit mode over a read-only row")
	}

	// And the mouse path, which is a separate gate.
	tree.clickEditItem, tree.clickEditCol = fixed, treeKeyColumn
	tree.armClickEdit(core.MouseReleaseEvent{})
	if tree.rowEditing {
		t.Error("a click opened the editor on a read-only row")
	}
}

// Walking from one row to the next while editing stops at a row that is not
// written in, rather than opening an editor over it.
func TestWalkingIntoAReadOnlyRowEndsTheEdit(t *testing.T) {
	_, tree, item := treeInAWindow(t)
	fixed := NewTreeItem("this host")
	fixed.ReadOnly = true
	tree.AddRootItem(fixed)

	tree.SetCurrentItem(item)
	if !tree.startRowEdit() {
		t.Fatal("the first row refused the editor")
	}
	tree.stepEditRow(1)

	if tree.CurrentItem() != fixed {
		t.Fatalf("the walk landed on %q", tree.CurrentItem().Text)
	}
	if tree.rowEditing {
		t.Error("the walk opened an editor over the read-only row it landed on")
	}
}

// The wire says it the way it reads: a row is editable unless it says it is
// not, so nothing already written changes meaning.
func TestTheWireHoldsARowOutOfTheEditor(t *testing.T) {
	tree := NewTreeView()
	tree.SetEditable(true)
	built := &wireItem{caption: "this host", readOnly: true}
	node := built.bind(tree)
	if !node.ReadOnly {
		t.Error("!editable did not reach the row it was written on")
	}
	if ordinary := (&wireItem{caption: "a client"}).bind(tree); ordinary.ReadOnly {
		t.Error("a row that said nothing came back read-only")
	}
}
