package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// The menu key does two things, and both matter: the bar takes the keyboard,
// and the active window gives it up. They used to live in only one of the two
// places the key arrives -- and the window manager, which resolves the
// desktop's context ABOVE any window, is the path a real keystroke takes. So
// the bar lit up over a window that still held the keyboard.
func TestMenuKeyDeactivatesTheActiveWindow(t *testing.T) {
	for _, c := range []struct {
		name string
		send func(d *Desktop, ev core.KeyPressEvent) bool
	}{
		{"window manager", func(d *Desktop, ev core.KeyPressEvent) bool {
			return d.windowManager.HandleKeyPress(ev)
		}},
		{"desktop", func(d *Desktop, ev core.KeyPressEvent) bool {
			return d.HandleKeyPress(ev)
		}},
	} {
		d := NewDesktop()
		d.windowManager = window.NewWindowManager()
		d.windowManager.SetDesktop(d)
		win := window.NewWindow("Doc")
		win.SetContent(NewTextInput())
		d.windowManager.AddWindow(win)
		d.windowManager.ActivateWindow(win)
		d.AddApplication(&mockApp{
			name: "Demo", main: win, windows: []*window.Window{win},
			menus: []*Menu{NewMenu("&File")},
		})

		if d.windowManager.ActiveWindow() == nil {
			t.Fatalf("%s: precondition, a window should be active", c.name)
		}
		if !c.send(d, core.KeyPressEvent{Key: "F10"}) {
			t.Errorf("%s: the menu key was not handled", c.name)
		}
		if !d.menuBar.HasFocus() {
			t.Errorf("%s: the bar did not take focus", c.name)
		}
		if d.windowManager.ActiveWindow() != nil {
			t.Errorf("%s: the bar has focus but the window is still active", c.name)
		}
	}
}

// Pressing it again gives the keyboard back rather than deactivating
// something else.
func TestMenuKeyAgainReleasesTheBar(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	d.windowManager.SetDesktop(d)
	d.AddApplication(&mockApp{name: "Demo", menus: []*Menu{NewMenu("&File")}})

	d.HandleKeyPress(core.KeyPressEvent{Key: "F10"})
	if !d.menuBar.HasFocus() {
		t.Fatal("the bar did not take focus")
	}
	d.HandleKeyPress(core.KeyPressEvent{Key: "F10"})
	if d.menuBar.HasFocus() {
		t.Error("the second press did not release the bar")
	}
}
