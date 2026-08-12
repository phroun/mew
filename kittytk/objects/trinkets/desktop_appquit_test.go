package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

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

	if d.QuitRequested() {
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

	if !d.QuitRequested() {
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

	if d.QuitRequested() {
		t.Error("another app still has a window: the display should stay up")
	}
}

// The whole ^Q invariant in one place: it ends the APPLICATION, it does not
// close just a window, and it does not take the desktop with it.
func TestCtrlQEndsAppOnlyNotWindowNotDesktop(t *testing.T) {
	d := NewDesktop()
	winA := window.NewWindow("A")
	winB := window.NewWindow("B")
	app := &mockApp{name: "Demo", windows: []*window.Window{winA, winB}}
	other := &mockApp{name: "Other", windows: []*window.Window{window.NewWindow("C")}}
	d.AddApplication(app)
	d.AddApplication(other)
	winA.SetParent(d)

	if !winA.HandleKeyPress(core.KeyPressEvent{Key: "^Q"}) {
		t.Fatal("^Q was not handled")
	}

	// The whole application went, both of its windows with it...
	if got := len(d.Applications()); got != 1 {
		t.Errorf("%d applications remain, want 1 (the other app)", got)
	}
	for _, w := range []*window.Window{winA, winB} {
		if w.IsVisible() {
			t.Errorf("%s survived ^Q: the app's windows all close", w.Title())
		}
	}
	// ...and the desktop did not.
	if d.QuitRequested() {
		t.Error("^Q quit the desktop; it must only end the application")
	}
}

// ^W is the other half of the split: one window, application untouched.
func TestCtrlWClosesOneWindowOnly(t *testing.T) {
	d := NewDesktop()
	winA := window.NewWindow("A")
	winB := window.NewWindow("B")
	app := &mockApp{name: "Demo", windows: []*window.Window{winA, winB}}
	d.AddApplication(app)
	winA.SetParent(d)

	if !winA.HandleKeyPress(core.KeyPressEvent{Key: "^W"}) {
		t.Fatal("^W was not handled")
	}
	if got := len(d.Applications()); got != 1 {
		t.Errorf("^W removed the application (%d left), it should close a window", got)
	}
	if winB.IsVisible() == false {
		t.Error("^W closed a sibling window; it closes only the focused one")
	}
	if d.QuitRequested() {
		t.Error("^W quit the desktop")
	}
}

// Quitting is an ATTEMPT. A window that refuses to close -- the usual reason
// being unsaved work, asked through its close handler -- cancels the quit,
// and the application stays on the desktop rather than being torn off it with
// a window still open.
func TestAppQuitRespectsARefusedClose(t *testing.T) {
	d := NewDesktop()
	keep := window.NewWindow("Unsaved")
	keep.SetOnClose(func() bool { return false })
	app := &mockApp{name: "Demo", windows: []*window.Window{keep}}
	d.AddApplication(app)
	keep.SetParent(d)

	keep.HandleKeyPress(core.KeyPressEvent{Key: "^Q"})

	if got := len(d.Applications()); got != 1 {
		t.Errorf("the application was removed despite a refused close (%d left)", got)
	}
	if !keep.IsVisible() {
		t.Error("the window closed even though its handler refused")
	}
}

// A refusal partway through stops there: the app keeps its place, and the
// windows that already agreed to close stay closed (which is what every
// desktop does -- the quit is abandoned, not rolled back).
func TestAppQuitStopsAtTheFirstRefusal(t *testing.T) {
	d := NewDesktop()
	first := window.NewWindow("Saved")
	second := window.NewWindow("Unsaved")
	third := window.NewWindow("Untouched")
	second.SetOnClose(func() bool { return false })
	app := &mockApp{name: "Demo", windows: []*window.Window{first, second, third}}
	d.AddApplication(app)
	first.SetParent(d)

	first.HandleKeyPress(core.KeyPressEvent{Key: "^Q"})

	if got := len(d.Applications()); got != 1 {
		t.Errorf("the application was removed despite a refusal (%d left)", got)
	}
	if first.IsVisible() {
		t.Error("the window that agreed to close should have stayed closed")
	}
	if !second.IsVisible() || !third.IsVisible() {
		t.Error("the refusal should stop the sweep where it stood")
	}
}
