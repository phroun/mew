package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// A key reaches an item that names a COMMAND, without the item knowing which
// key that is: the context resolves, the bar matches the answer.
func TestBarActivatesTheItemNamingTheCommand(t *testing.T) {
	fired := 0
	bar := NewMenuBar()
	menu := NewMenu("File")
	menu.AddItem(NewMenuItem("&Quit").SetCommand(core.CmdAppQuit).
		SetOnTriggered(func() { fired++ }))
	bar.AddMenu(menu)

	if !bar.ActivateCommand(core.CmdAppQuit) {
		t.Fatal("the bar did not find the item naming app_quit")
	}
	if fired != 1 {
		t.Errorf("the item ran %d times, want 1", fired)
	}

	if bar.ActivateCommand(core.CmdWindowClose) {
		t.Error("the bar claimed a command no item names")
	}
}

// Submenus are looked through too, and an unavailable item is not a match:
// a disabled item advertises a key that would do nothing, and pressing it
// must do nothing rather than the wrong thing.
func TestActivateCommandSkipsUnavailableItems(t *testing.T) {
	fired := 0
	bar := NewMenuBar()
	menu := NewMenu("File")
	disabled := NewMenuItem("&Quit").SetCommand(core.CmdAppQuit).
		SetOnTriggered(func() { fired++ })
	disabled.Enabled = false
	menu.AddItem(disabled)
	bar.AddMenu(menu)

	if bar.ActivateCommand(core.CmdAppQuit) || fired != 0 {
		t.Error("a disabled item was triggered by its command")
	}

	sub := NewMenu("More")
	sub.AddItem(NewMenuItem("&Close").SetCommand(core.CmdWindowClose).
		SetOnTriggered(func() { fired++ }))
	menu.AddMenu(sub)

	if !bar.ActivateCommand(core.CmdWindowClose) || fired != 1 {
		t.Errorf("an item in a submenu was not reached (fired %d)", fired)
	}
}

// End to end through a window: pressing the key runs the item, with the key
// resolved once and the item matched by what it means.
func TestWindowKeyReachesACommandItem(t *testing.T) {
	fired := 0
	win := window.NewWindow("Doc")

	bar := NewMenuBar()
	menu := NewMenu("File")
	menu.AddItem(NewMenuItem("&Close").SetCommand(core.CmdWindowClose).
		SetOnTriggered(func() { fired++ }))
	bar.AddMenu(menu)
	win.SetWindowMenuBar(bar)

	if !win.HandleKeyPress(core.KeyPressEvent{Key: "^W"}) {
		t.Fatal("^W was not handled")
	}
	if fired != 1 {
		t.Errorf("the item ran %d times, want 1 - the window resolved ^W and the bar matched it", fired)
	}
}

// A guest holding the keyboard takes the key away from the menu item too: the
// item advertises nothing in that situation, and pressing the key it used to
// have does not reach it.
func TestCapturedKeyboardKeepsTheKeyFromTheItem(t *testing.T) {
	fired := 0
	win := window.NewWindow("Doc")
	guest := newGuestTrinket()
	win.SetContent(guest)

	bar := NewMenuBar()
	menu := NewMenu("File")
	item := NewMenuItem("&Close").SetCommand(core.CmdWindowClose).
		SetOnTriggered(func() { fired++ })
	menu.AddItem(item)
	bar.AddMenu(menu)
	bar.SetKeyResolver(func(command string) string {
		return win.KeyContext().KeyForCommand(command)
	})
	win.SetWindowMenuBar(bar)

	guest.SetKeyRegistry(core.NewKeyRegistry("captured", nil))
	win.FocusManager().SetFocusedTrinket(guest)

	if got := item.ShortcutDisplay(); got != "" {
		t.Errorf("the item advertises %q while a guest holds the keyboard, want nothing", got)
	}
	win.HandleKeyPress(core.KeyPressEvent{Key: "^W"})
	if fired != 0 {
		t.Error("^W reached the menu item although the guest had the keyboard")
	}
}
