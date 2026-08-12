package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// quitRequested reports whether the desktop has been asked to exit.
func quitRequested(d *Desktop) bool {
	select {
	case <-d.quitChan:
		return true
	default:
		return false
	}
}

// M-F4 and ^Q end the APPLICATION the focused window belongs to: its windows
// close and the app comes off the desktop. The window itself knows nothing
// about applications - it walks up to the desktop, which owns the registry.
func TestAppQuitKeyEndsTheOwningApplication(t *testing.T) {
	for _, key := range []string{"M-F4", "^Q", "C-Q"} {
		d := NewDesktop()
		win := window.NewWindow("Doc")
		app := &mockApp{name: "Demo", windows: []*window.Window{win}}
		d.AddApplication(app)
		win.SetParent(d)

		if !win.HandleKeyPress(core.KeyPressEvent{Key: key}) {
			t.Errorf("%s: not handled", key)
		}
		if got := len(d.Applications()); got != 0 {
			t.Errorf("%s: %d applications remain, want 0", key, got)
		}
	}
}

// Closing ONE window is a different key. ^W and ^F4 close the window and
// leave the application running.
func TestWindowCloseKeyLeavesTheApplication(t *testing.T) {
	for _, key := range []string{"^W", "^F4", "C-F4"} {
		d := NewDesktop()
		win := window.NewWindow("Doc")
		app := &mockApp{name: "Demo", windows: []*window.Window{win}}
		d.AddApplication(app)
		win.SetParent(d)

		if !win.HandleKeyPress(core.KeyPressEvent{Key: key}) {
			t.Errorf("%s: not handled", key)
		}
		if got := len(d.Applications()); got != 1 {
			t.Errorf("%s: closing a window removed the application (%d left)", key, got)
		}
	}
}

// Quitting the last app does NOT take the desktop with it: the desktop is
// what the next app gets launched from, and it still has a menu bar and a
// dock to offer.
func TestQuitLastAppKeepsTheDesktop(t *testing.T) {
	d := NewDesktop()
	win := window.NewWindow("Doc")
	app := &mockApp{name: "Demo", windows: []*window.Window{win}}
	d.AddApplication(app)

	d.quitApplication(app)

	if quitRequested(d) {
		t.Error("quitting the last app should leave the desktop running")
	}
}

// Solo mode is the exception: the app IS the display, so there is no desktop
// to go back to and quitting the last app ends the process.
func TestQuitLastAppInSoloModeEndsTheDesktop(t *testing.T) {
	d := NewDesktop()
	d.mu.Lock()
	d.solo = true
	d.mu.Unlock()

	win := window.NewWindow("Doc")
	app := &mockApp{name: "Demo", windows: []*window.Window{win}}
	d.AddApplication(app)

	d.quitApplication(app)

	if !quitRequested(d) {
		t.Error("in solo mode the last app quitting should end the desktop")
	}
}

// ...but only the LAST one. A second app still holding a window keeps the
// solo display alive.
func TestQuitOneOfTwoAppsInSoloModeKeepsRunning(t *testing.T) {
	d := NewDesktop()
	d.mu.Lock()
	d.solo = true
	d.mu.Unlock()

	first := &mockApp{name: "First", windows: []*window.Window{window.NewWindow("A")}}
	second := &mockApp{name: "Second", windows: []*window.Window{window.NewWindow("B")}}
	d.AddApplication(first)
	d.AddApplication(second)

	d.quitApplication(first)

	if quitRequested(d) {
		t.Error("another app still has a window: the display should stay up")
	}
}
