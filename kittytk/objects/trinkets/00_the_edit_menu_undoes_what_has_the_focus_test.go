package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// editMenuOver builds the system Edit menu over a window holding the given
// trinket, focused.
func editMenuOver(t *testing.T, content core.Trinket) (*Desktop, *Menu) {
	t.Helper()
	d := NewDesktop()
	d.SetBackend(&nullBackend{})
	win := window.NewWindow("host")
	win.SetContent(content)
	d.WindowManager().AddWindow(win)
	d.WindowManager().ActivateWindow(win)
	content.SetFocus()
	menu := d.systemEditMenu(nil, nil)
	return d, menu
}

func itemNamed(t *testing.T, menu *Menu, command string) *MenuItem {
	t.Helper()
	for _, it := range menu.Items() {
		if it != nil && it.Command == command {
			return it
		}
	}
	t.Fatalf("the Edit menu has no item for %s", command)
	return nil
}

// Undo and Redo lead the system Edit menu, apart from the clipboard.
func TestUndoAndRedoLeadTheEditMenu(t *testing.T) {
	_, menu := editMenuOver(t, NewTextInput())
	got := captions(menu)
	want := []string{"Undo", "Redo", "---", "Cut", "Copy", "Paste", "---", "Select All"}
	if len(got) != len(want) {
		t.Fatalf("menu = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("menu = %v, want %v", got, want)
		}
	}
}

// They enable from what the focused trinket says of its history, and act on it.
func TestTheEditMenuUndoesTheFocusedField(t *testing.T) {
	ti := NewTextInput()
	_, menu := editMenuOver(t, ti)
	undo, redo := itemNamed(t, menu, core.CmdTrinketUndo), itemNamed(t, menu, core.CmdTrinketRedo)
	show := menu.OnAboutToShow()

	show()
	if undo.Enabled || redo.Enabled {
		t.Errorf("with nothing typed: Undo %v, Redo %v, want both off", undo.Enabled, redo.Enabled)
	}
	typeWord(ti, "ab")
	show()
	if !undo.Enabled || redo.Enabled {
		t.Errorf("after typing: Undo %v, Redo %v, want Undo only", undo.Enabled, redo.Enabled)
	}
	undo.Trigger()
	if ti.Text() != "" {
		t.Errorf("the Undo item left %q", ti.Text())
	}
	show()
	if undo.Enabled || !redo.Enabled {
		t.Errorf("after undoing: Undo %v, Redo %v, want Redo only", undo.Enabled, redo.Enabled)
	}
	redo.Trigger()
	if ti.Text() != "ab" {
		t.Errorf("the Redo item left %q", ti.Text())
	}
}

// A terminal keeps no history of its own: both items stay off over it.
func TestATerminalOffersNoUndo(t *testing.T) {
	_, menu := editMenuOver(t, NewPurfecTerm())
	menu.OnAboutToShow()()
	if itemNamed(t, menu, core.CmdTrinketUndo).Enabled || itemNamed(t, menu, core.CmdTrinketRedo).Enabled {
		t.Error("Undo or Redo is enabled over a terminal")
	}
}

// The Undo item shows the key the keymap ranks higher of undo's and simple
// undo's: here, off a Mac, that is ^Z.
func TestTheUndoItemShowsSimpleUndosKey(t *testing.T) {
	ti := NewTextInput()
	d, menu := editMenuOver(t, ti)
	menu.SetKeyResolver(d.keyForCommandInFocus)
	undo := itemNamed(t, menu, core.CmdTrinketUndo)
	want := core.DefaultKeyRegistry().KeyForCommand(core.CmdTrinketSimpleUndo)
	if core.CurrentKeymapEnvironment().OS == "darwin" {
		want = core.DefaultKeyRegistry().KeyForCommand(core.CmdTrinketUndo)
	}
	if got := undo.ShortcutDisplay(); got != core.DisplayKey(want) {
		t.Errorf("the Undo item shows %q, want %q", got, core.DisplayKey(want))
	}
}
