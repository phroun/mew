package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// A desktop always carries a menu bar, created with the desktop itself. Only
// the bar handed to SetMenuBar used to be given an accelerator chord and a key
// context -- so an application declaring its menus the ordinary way, through
// SetMenuBarContent, got a bar with neither.
//
// The symptom was quiet: a nil context claims nothing, so every accelerator
// drew LIT, and there was nowhere to publish them, so the chord did nothing
// and the underlying key went through to whatever had focus. On macOS, where
// Option+V is decoded back to M-v carrying its letter as text, that meant a
// literal "v" appearing in the focused text field.
func TestBuiltInDesktopBarFormsWorkingAccelerators(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	wm := d.windowManager
	wm.SetDesktop(d)

	if d.menuBar.acceleratorChord == "" {
		t.Error("the built-in bar has no accelerator chord")
	}
	if d.menuBar.keyContext == nil {
		t.Fatal("the built-in bar has no key context")
	}

	win := window.NewWindow("Protocol Demo")
	ti := NewTextInput()
	ti.SetText("hello")
	win.SetContent(ti)
	app := &mockApp{
		name:    "Demo",
		windows: []*window.Window{win},
		menus:   []*Menu{NewMenu("&View")},
	}
	d.AddApplication(app)
	wm.AddWindow(win)
	wm.ActivateWindow(win)
	ti.SetFocus()

	// Text carried alongside, as the macOS Option decoding delivers it.
	if !d.dispatchEvent(core.KeyPressEvent{Key: "M-v", Text: "v"}) {
		t.Fatal("M-v was not handled")
	}
	if d.menuBar.ActiveMenu() == nil {
		t.Error("M-v did not open the View menu")
	}
	if got := ti.Text(); got != "hello" {
		t.Errorf("the accelerator's letter was typed into the field: %q", got)
	}
}
