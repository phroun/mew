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
