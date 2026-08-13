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
// so the accelerator drew lit while the focused trinket ate the chord.
//
// Which letter it lands on is the other half of the scheme: M-a already means
// select-all in the keymap, so "&Al&phabet" does not take it. It takes its
// backup candidate, and M-a goes on meaning what the keymap says it means.
func TestWindowOwnMenuBarAcceleratorBeatsFocusedTrinket(t *testing.T) {
	win := window.NewWindow("Protocol Demo")
	mb := NewMenuBar()
	menu := NewMenu("&Al&phabet")
	mb.AddMenu(menu)
	win.SetWindowMenuBar(mb)

	ti := NewTextInput()
	ti.SetText("hello world")
	win.SetContent(ti)
	ti.SetFocus()

	if got := mb.acceleratorAssignmentFor(menu).Char; got != 'p' {
		t.Fatalf("the menu took %q; M-a is spoken for, so it should fall back to p", got)
	}

	if !win.HandleKeyPress(core.KeyPressEvent{Key: "M-p"}) {
		t.Fatal("M-p was not handled")
	}
	if mb.ActiveMenu() == nil {
		t.Error("M-p did not open the Alphabet menu")
	}
	if got := ti.SelectedText(); got != "" {
		t.Errorf("the focused text field ate the accelerator and selected %q", got)
	}
}

// ...and the chord the menu did NOT take goes on doing what the keymap says:
// M-a reaches the focused text field and selects everything in it.
func TestUntakenChordStillMeansWhatTheKeymapSays(t *testing.T) {
	win := window.NewWindow("Protocol Demo")
	mb := NewMenuBar()
	mb.AddMenu(NewMenu("&Al&phabet"))
	win.SetWindowMenuBar(mb)

	ti := NewTextInput()
	ti.SetText("hello world")
	win.SetContent(ti)
	ti.SetFocus()

	win.HandleKeyPress(core.KeyPressEvent{Key: "M-a"})

	if mb.ActiveMenu() != nil {
		t.Error("M-a opened a menu; the menu yielded that chord to the keymap")
	}
	if got := ti.SelectedText(); got != "hello world" {
		t.Errorf("the text field selected %q, want the whole line", got)
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
