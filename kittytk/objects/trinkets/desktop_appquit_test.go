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

// Quitting the last app does NOT take a DESKTOP ENVIRONMENT with it: that
// desktop is a place in its own right - what the next app gets launched from,
// with a menu bar and a dock to offer an empty screen.
func TestQuitLastAppKeepsADesktopEnvironment(t *testing.T) {
	d := NewDesktop()
	d.SetDesktopEnvironment(true)
	win := window.NewWindow("Doc")
	app := &mockApp{name: "Demo", windows: []*window.Window{win}}
	d.AddApplication(app)

	d.quitApplication(app)

	if d.QuitRequested() {
		t.Error("a desktop environment should outlive the last app on it")
	}
}

// A desktop that is only the FRAME around one application is that application:
// quitting it leaves nothing to show, so the process ends. This is the mew host
// - launched to run mew, not to be a desktop.
func TestQuitLastAppEndsAnApplicationsFrame(t *testing.T) {
	d := NewDesktop()
	win := window.NewWindow("Doc")
	app := &mockApp{name: "Demo", windows: []*window.Window{win}}
	d.AddApplication(app)

	d.quitApplication(app)

	if !d.QuitRequested() {
		t.Error("the last app quitting should end a desktop that was only its frame")
	}
}

// Solo mode is not the deciding fact and never was: the app IS the display,
// but what ends the process is that there is no desktop environment behind it.
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

// Shutting the desktop down is the same act as closing everything on it, so
// it asks the same way: a window holding unsaved work refuses, and the quit is
// abandoned with the desktop still up.
func TestQuitIsRefusedByAWindowThatWillNotClose(t *testing.T) {
	d := NewDesktop()
	keep := window.NewWindow("Unsaved")
	keep.SetOnClose(func() bool { return false })
	app := &mockApp{name: "Demo", windows: []*window.Window{keep}}
	d.AddApplication(app)

	d.Quit()

	if d.QuitRequested() {
		t.Error("the desktop quit over a window that refused to close")
	}
	if !keep.IsVisible() {
		t.Error("the refusing window was closed anyway")
	}
}

// With nothing to object, quitting closes everything and goes through.
func TestQuitClosesEveryWindowAndProceeds(t *testing.T) {
	d := NewDesktop()
	first := window.NewWindow("A")
	second := window.NewWindow("B")
	d.AddApplication(&mockApp{name: "Demo", windows: []*window.Window{first}})
	d.AddApplication(&mockApp{name: "Other", windows: []*window.Window{second}})

	d.Quit()

	if !d.QuitRequested() {
		t.Fatal("nothing refused; the desktop should have quit")
	}
	for _, w := range []*window.Window{first, second} {
		if w.IsVisible() {
			t.Errorf("%s survived the shutdown sweep", w.Title())
		}
	}
}

// ForceQuit is the door with no question behind it, for the paths where there
// is nothing left to ask.
func TestForceQuitIgnoresARefusal(t *testing.T) {
	d := NewDesktop()
	keep := window.NewWindow("Unsaved")
	keep.SetOnClose(func() bool { return false })
	d.AddApplication(&mockApp{name: "Demo", windows: []*window.Window{keep}})

	d.ForceQuit()

	if !d.QuitRequested() {
		t.Error("ForceQuit must not be refusable")
	}
}
