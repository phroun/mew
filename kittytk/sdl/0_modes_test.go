//go:build sdl

package sdl

import (
	"reflect"
	"testing"

	"github.com/phroun/kittytk/core"
	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// This host answers the same capability the terminal one does, so an indicator
// is written once and drawn under either.
var _ core.ModeSource = (*Platform)(nil)

// A state this host cannot see is ABSENT, not off.
//
// Before any key or focus event nothing here has looked at the window system's
// modifier state, and drawing Caps Lock as off would claim knowledge we do not
// have. The pad's lock is the exception: it is answerable from the start, since
// a pad with no latch is permanently locked and one with a latch almost always
// is.
func TestUnseenModesAreAbsent(t *testing.T) {
	p := &Platform{padLock: newPadLock()}

	want := []core.Mode{{Name: core.ModeNumLock, Value: core.ModeOn}}
	if got := p.Modes(); !reflect.DeepEqual(got, want) {
		t.Errorf("a fresh platform knows %v, want %v", got, want)
	}
	for _, name := range []string{core.ModeCapsLock, core.ModeFocus, "kana"} {
		if v, ok := p.Mode(name); ok {
			t.Errorf("%s reported %q before anything said so", name, v)
		}
	}
}

// Caps Lock becomes known from the window system's modifier state, which this
// host can ask for rather than wait for.
func TestCapsLockFromTheModifierState(t *testing.T) {
	p := &Platform{padLock: newPadLock()}

	if !p.noteCapsLock(sdl3.KMOD_CAPS) {
		t.Fatal("the first look at the modifier state reported no change")
	}
	if v, ok := p.Mode(core.ModeCapsLock); !ok || v != core.ModeOn {
		t.Errorf("caps = %q ok=%v, want on", v, ok)
	}
	if p.noteCapsLock(sdl3.KMOD_CAPS) {
		t.Error("an unchanged modifier state reported a change")
	}
	if !p.noteCapsLock(0) {
		t.Error("the latch going off reported no change")
	}
	if v, _ := p.Mode(core.ModeCapsLock); v != core.ModeOff {
		t.Errorf("caps = %q after the latch went off, want off", v)
	}
}

// Focus is a state as much as an event, readable from the same list as the
// latches — a program that pauses when the window goes away does not need a
// second path to find out.
func TestFocusIsAMode(t *testing.T) {
	p := &Platform{padLock: newPadLock()}

	if !p.noteFocus(false) {
		t.Fatal("the first focus report was taken as no change")
	}
	if v, ok := p.Mode(core.ModeFocus); !ok || v != core.ModeOff {
		t.Errorf("focus = %q ok=%v, want off", v, ok)
	}
	if !p.noteFocus(true) {
		t.Error("focus coming back reported no change")
	}
	if v, _ := p.Mode(core.ModeFocus); v != core.ModeOn {
		t.Errorf("focus = %q after it came back, want on", v)
	}
}

// A host can publish a state of its own, valued by any token it likes, and it
// is reported beside the states this host keeps.
func TestAHostCanAddItsOwnMode(t *testing.T) {
	p := &Platform{padLock: newPadLock()}

	if !p.SetMode("overtype", core.ModeOn) {
		t.Fatal("setting a new mode reported no change")
	}
	if p.SetMode("overtype", core.ModeOn) {
		t.Error("setting a mode to what it already is reported a change")
	}
	p.SetMode("kana", "hiragana")

	want := []core.Mode{
		{Name: "kana", Value: "hiragana"},
		{Name: core.ModeNumLock, Value: core.ModeOn},
		{Name: "overtype", Value: core.ModeOn},
	}
	if got := p.Modes(); !reflect.DeepEqual(got, want) {
		t.Errorf("modes = %v, want %v (sorted, so a status bar drawn from the "+
			"list does not reshuffle itself)", got, want)
	}

	if !p.SetMode("overtype", "") {
		t.Error("removing a mode reported no change")
	}
	if _, ok := p.Mode("overtype"); ok {
		t.Error("overtype survived being set to the empty value")
	}
}

// Writing the pad's lock moves it, which is what a Mac needs: there is no latch
// behind that cap, so this host IS the lock. The latches take only their two
// tokens.
func TestWritingTheStatesThisHostKeeps(t *testing.T) {
	p := &Platform{padLock: newPadLock()}

	if !p.SetMode(core.ModeNumLock, core.ModeOff) {
		t.Fatal("turning the pad's lock off reported no change")
	}
	if p.padLock.locked() {
		t.Error("the pad still reports locked after being set off")
	}
	if p.SetMode(core.ModeNumLock, "maybe") {
		t.Error("a latch accepted a value that is not one of its two")
	}
	if v, _ := p.Mode(core.ModeNumLock); v != core.ModeOff {
		t.Errorf("num = %q after a rejected write, want the value it had", v)
	}
}
