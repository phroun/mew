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
