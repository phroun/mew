package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// A child window carries no chrome, so its app's accelerators live in the
// app's MAIN window. When one of them opens a menu there, the keyboard has to
// follow: torn off, the main window is a different OS window entirely, and
// leaving focus on the child drops every arrow and Enter meant for the menu
// that just came down.
func TestChildAcceleratorMovesFocusToTheWindowHoldingTheMenu(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	wm := d.windowManager
	wm.SetDesktop(d)

	main := window.NewWindow("KittyTK Demo")
	mb := NewMenuBar()
	mb.AddMenu(NewMenu("&View"))
	main.SetWindowMenuBar(mb)

	child := window.NewWindow("Protocol Demo")
	child.SetContent(NewTextInput())

	wm.AddWindow(main)
	wm.AddWindow(child)
	wm.ActivateWindow(child)
	d.setChildShortcutResolver(child, main)

	if wm.ActiveWindow() != child {
		t.Fatalf("precondition: the child should hold the focus, got %v", wm.ActiveWindow())
	}

	if !child.HandleKeyPress(core.KeyPressEvent{Key: "M-v"}) {
		t.Fatal("M-v from the child was not handled")
	}
	if mb.ActiveMenu() == nil {
		t.Fatal("M-v did not open the View menu in the main window")
	}
	if got := wm.ActiveWindow(); got != main {
		t.Errorf("focus stayed on %v; the menu is in the main window", got.Title())
	}
}

// An app SHORTCUT (Cut/Copy/Paste) is not a menu opening, so it must NOT drag
// the focus off the child that is being edited.
func TestChildAppShortcutLeavesFocusAlone(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	wm := d.windowManager
	wm.SetDesktop(d)

	main := window.NewWindow("KittyTK Demo")
	mb := NewMenuBar()
	mb.AddMenu(NewMenu("&View"))
	main.SetWindowMenuBar(mb)

	child := window.NewWindow("Protocol Demo")
	child.SetContent(NewTextInput())

	wm.AddWindow(main)
	wm.AddWindow(child)
	wm.ActivateWindow(child)
	d.setChildShortcutResolver(child, main)

	// A key that is neither an accelerator nor an item shortcut.
	child.HandleKeyPress(core.KeyPressEvent{Key: "M-q"})
	if got := wm.ActiveWindow(); got != child {
		t.Errorf("focus moved to %v for a key the main window did not act on", got.Title())
	}
}

// The menu key summons the app's bar, and a child has no bar of its own -- the
// app's is in the main window. Pressing it from a child must focus that bar
// AND bring its window forward, which torn off is a different OS window.
func TestChildAppMenuKeyFocusesTheMainWindowBar(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	wm := d.windowManager
	wm.SetDesktop(d)

	main := window.NewWindow("KittyTK Demo")
	mb := NewMenuBar()
	mb.AddMenu(NewMenu("&View"))
	main.SetWindowMenuBar(mb)

	child := window.NewWindow("Protocol Demo")
	child.SetContent(NewTextInput())

	wm.AddWindow(main)
	wm.AddWindow(child)
	wm.ActivateWindow(child)
	d.setChildShortcutResolver(child, main)

	if !child.HandleKeyPress(core.KeyPressEvent{Key: "F10"}) {
		t.Fatal("the menu key from the child was not handled")
	}
	if !mb.HasFocus() {
		t.Error("the app's menu bar did not take focus")
	}
	if got := wm.ActiveWindow(); got != main {
		t.Errorf("focus stayed on %v; the bar is in the main window", got.Title())
	}
}
