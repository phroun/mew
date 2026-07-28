package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
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
