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
