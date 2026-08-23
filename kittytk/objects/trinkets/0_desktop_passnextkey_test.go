package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// Pass-next-key mode means the NEXT key bypasses everything — and F10 is the
// key most worth bypassing with, because the desktop claims it for the menu
// bar before any shortcut handling. Arming raw key input and pressing F10
// opened the outer menu instead of sending it on, which is precisely the key
// someone arms raw input to deliver.
func TestPassNextKeyIsCheckedBeforeTheMenuBarTakesF10(t *testing.T) {
	d := NewDesktop()
	a := &mockApp{name: "mew"}
	d.AddApplication(a)

	// Unarmed: the desktop declines, and F10 stays the menu bar's.
	if d.takePassNextKey(core.KeyPressEvent{Key: "F10"}, nil) {
		t.Fatal("unarmed: the key should not be claimed")
	}

	a.ActivatePassNextKeyToTrinket()
	if !d.takePassNextKey(core.KeyPressEvent{Key: "F10"}, nil) {
		t.Error("armed: F10 belongs to the trinket, not the menu bar")
	}
	if a.PassNextKeyToTrinket() {
		t.Error("the arm should be spent by the key that used it")
	}
	// One shot: the next F10 is the menu bar's again.
	if d.takePassNextKey(core.KeyPressEvent{Key: "F10"}, nil) {
		t.Error("the arm should not survive the key it was spent on")
	}
}

// HandleKeyPress is a second door into the same handling, and the check has to
// be at both: a key claimed by pass-next-key must not be taken by whatever
// that door does first.
func TestPassNextKeyAtTheKeyPressDoor(t *testing.T) {
	d := NewDesktop()
	a := &mockApp{name: "mew"}
	d.AddApplication(a)

	a.ActivatePassNextKeyToTrinket()
	if !d.HandleKeyPress(core.KeyPressEvent{Key: "F10"}) {
		t.Error("HandleKeyPress should consume the armed key")
	}
	if a.PassNextKeyToTrinket() {
		t.Error("the arm should be spent")
	}
}

// The DETACHED main window is a different arming path and was the one still
// broken. ActivatePassNextKeyToTrinket arms the WINDOW there, not the app,
// because the key stream and the prompt both live on that window — but the
// window manager routes F10 to the desktop for the menu bar before any window
// sees it, so the window never got the chance to spend its own one-shot, and
// the torn-off window's menu bar lit up instead.
func TestDetachedWindowRawKeyBeatsTheMenuBarOnF10(t *testing.T) {
	d := NewDesktop()
	win := window.NewWindow("mew")
	win.SetDetached(true)
	a := &mockApp{name: "mew", main: win}
	d.AddApplication(a)

	// Unarmed, F10 is the menu bar's.
	if d.takePassNextKey(core.KeyPressEvent{Key: "F10"}, nil) {
		t.Fatal("unarmed: the key should not be claimed")
	}

	// Armed the way ActivatePassNextKeyToTrinket arms a detached window.
	restored := false
	win.BeginRawKeyInput(func() { restored = true })
	if !win.RawKeyInputPending() {
		t.Fatal("BeginRawKeyInput did not arm the window")
	}
	if !d.takePassNextKey(core.KeyPressEvent{Key: "F10"}, nil) {
		t.Error("armed: F10 belongs to the detached window's trinket, not the menu bar")
	}
	if win.RawKeyInputPending() {
		t.Error("the window's one-shot should be spent by the key that used it")
	}
	if !restored {
		t.Error("the done callback should have run, so the status bar prompt is cleared")
	}
	// One shot: the next F10 is the menu bar's again.
	if d.takePassNextKey(core.KeyPressEvent{Key: "F10"}, nil) {
		t.Error("the arm should not survive the key it was spent on")
	}
}

// Same door, through HandleKeyPress — which is where the window manager sends
// F10 (manager.HandleKeyPress: "F10 always goes to desktop for menu bar
// toggle"), and so is the exact path the reported bug took.
func TestDetachedWindowRawKeyAtTheKeyPressDoor(t *testing.T) {
	d := NewDesktop()
	win := window.NewWindow("mew")
	win.SetDetached(true)
	a := &mockApp{name: "mew", main: win}
	d.AddApplication(a)

	win.BeginRawKeyInput(nil)
	if !d.HandleKeyPress(core.KeyPressEvent{Key: "F10"}) {
		t.Error("HandleKeyPress should consume the armed key")
	}
	if win.RawKeyInputPending() {
		t.Error("the window's one-shot should be spent")
	}
}

// A window that is NOT detached keeps the app-level path: arming the window
// alone must not make the desktop hand keys to a docked window behind the
// app's back.
func TestDockedWindowRawKeyDoesNotClaimKeys(t *testing.T) {
	d := NewDesktop()
	win := window.NewWindow("mew")
	a := &mockApp{name: "mew", main: win}
	d.AddApplication(a)

	win.BeginRawKeyInput(nil)
	if d.takePassNextKey(core.KeyPressEvent{Key: "F10"}, nil) {
		t.Error("a docked window's one-shot is not the desktop's business")
	}
}

// THE SDL DOOR. A window on its own OS surface is driven by
// window.SurfaceHost, whose Event hands straight to Window.HandleKeyPress —
// the desktop, and with it takePassNextKey, is never on that path. That door
// only exists on a multi-surface backend, which is exactly why arming the app
// alone worked on the single-surface TUI and lost F10 to the menu bar under
// SDL.
//
// So arming must reach the WINDOW's own one-shot too, whether or not the
// window is detached.
func TestArmingReachesAWindowOnItsOwnSurface(t *testing.T) {
	for _, detached := range []bool{false, true} {
		name := "in-surface"
		if detached {
			name = "detached"
		}
		t.Run(name, func(t *testing.T) {
			d := NewDesktop()
			d.windowManager = window.NewWindowManager()
			win := window.NewWindow("mew")
			win.SetDetached(detached)
			a := &mockApp{name: "mew", main: win}
			d.AddApplication(a)

			d.ActivatePassNextKeyToTrinket()
			if !win.RawKeyInputPending() {
				t.Fatal("the window's own one-shot was not armed; a key arriving by its surface is lost to the menu bar")
			}
			if !a.PassNextKeyToTrinket() {
				t.Error("the app flag must be armed too — the desktop door is still a door")
			}

			// Spending it at the WINDOW (the SurfaceHost path) must also clear
			// the app flag, or the key after the raw one would be claimed
			// again by the desktop.
			win.HandleKeyPress(core.KeyPressEvent{Key: "F10"})
			if win.RawKeyInputPending() {
				t.Error("the window's one-shot survived the key that spent it")
			}
			if a.PassNextKeyToTrinket() {
				t.Error("the app flag outlived the key: the NEXT key would be raw too")
			}
		})
	}
}

// And spending it at the DESKTOP door leaves nothing armed either.
func TestSpendingAtTheDesktopDoorClearsBoth(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	win := window.NewWindow("mew")
	d.windowManager.AddWindow(win)
	a := &mockApp{name: "mew", main: win}
	d.AddApplication(a)

	d.ActivatePassNextKeyToTrinket()
	if !d.takePassNextKey(core.KeyPressEvent{Key: "F10"}, nil) {
		t.Fatal("the desktop door should have claimed the armed key")
	}
	if a.PassNextKeyToTrinket() {
		t.Error("app flag still armed after the key was spent")
	}
	if win.RawKeyInputPending() {
		t.Error("window one-shot still armed after the key was spent")
	}
}
