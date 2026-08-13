package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// A window carrying its OWN menu bar - a detached, solo or torn-off main
// window - must answer its chord accelerators above the focused trinket, the
// same way the desktop does for a docked window.
//
// It published them into its own key context and then never read them back,
// so the accelerator drew lit while the focused trinket ate the chord: M-a on
// a window with an &Al&phabet menu selected all the text in a text field
// instead of opening the menu.
func TestWindowOwnMenuBarAcceleratorBeatsFocusedTrinket(t *testing.T) {
	win := window.NewWindow("Protocol Demo")
	mb := NewMenuBar()
	mb.AddMenu(NewMenu("&Al&phabet"))
	win.SetWindowMenuBar(mb)

	ti := NewTextInput()
	ti.SetText("hello world")
	win.SetContent(ti)
	ti.SetFocus()

	if !win.HandleKeyPress(core.KeyPressEvent{Key: "M-a"}) {
		t.Fatal("M-a was not handled")
	}
	if mb.ActiveMenu() == nil {
		t.Error("M-a did not open the Alphabet menu")
	}
	if got := ti.SelectedText(); got != "" {
		t.Errorf("the focused text field ate the accelerator and selected %q", got)
	}
}

// The accelerator answers on the FIRST keystroke, before anything has painted
// and published it into the context.
func TestWindowOwnMenuBarAcceleratorWorksBeforeFirstPaint(t *testing.T) {
	win := window.NewWindow("Fresh")
	mb := NewMenuBar()
	mb.AddMenu(NewMenu("&Help"))
	win.SetWindowMenuBar(mb)
	win.SetContent(NewTextInput())

	if !win.HandleKeyPress(core.KeyPressEvent{Key: "M-h"}) {
		t.Fatal("M-h was not handled")
	}
	if mb.ActiveMenu() == nil {
		t.Error("M-h did not open the Help menu")
	}
}

// A key that is NOT an accelerator still reaches the focused trinket.
func TestWindowOwnMenuBarLetsOtherKeysThrough(t *testing.T) {
	win := window.NewWindow("Protocol Demo")
	mb := NewMenuBar()
	mb.AddMenu(NewMenu("&Al&phabet"))
	win.SetWindowMenuBar(mb)

	ti := NewTextInput()
	ti.SetText("hello world")
	win.SetContent(ti)
	ti.SetFocus()

	win.HandleKeyPress(core.KeyPressEvent{Key: "End"})
	if mb.ActiveMenu() != nil {
		t.Error("an ordinary key opened a menu")
	}
	if ti.CursorPosition() != len("hello world") {
		t.Errorf("End did not reach the text field (caret at %d)", ti.CursorPosition())
	}
}
