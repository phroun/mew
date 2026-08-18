//go:build sdl

package sdl

import (
	"testing"

	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// The lock cap alone is not a key; with a modifier held it is Clear.
//
// Its unmodified meaning is entirely the state, and that state already decides
// what eleven other pad caps are called. Nobody writes a binding against it.
func TestTheLockCapIsEatenAloneAndNamedWhenModified(t *testing.T) {
	if !eatsLockCap(sdl3.Keysym{Scancode: scanNumLock}) {
		t.Error("the lock cap pressed alone was not eaten")
	}
	for _, tc := range []struct {
		ctrl, alt, shift, gui bool
		want                  string
	}{
		{shift: true, want: "S-Clear"},
		{ctrl: true, want: "C-Clear"},
		{alt: true, want: "M-Clear"},
		{gui: true, want: "s-Clear"},
		// Canonical order, C- before M-, which is what the terminal host emits
		// for the same chord.
		{ctrl: true, alt: true, want: "C-M-Clear"},
	} {
		sym := sdl3.Keysym{Scancode: scanNumLock}
		if got := encodeKey(sym, tc.ctrl, tc.alt, tc.shift, tc.gui, false); got != tc.want {
			t.Errorf("encodeKey(lock cap) = %q, want %q", got, tc.want)
		}
	}

	// Unprefixed. It is a lock, filed with CapsLock and ScrollLock — kitty puts
	// it at 57360 with them rather than in its 57399+ keypad block — and a
	// keymap is one file for both hosts, so "P-Clear" here against "Clear"
	// there is a split it cannot afford.
	if got := encodeKey(sdl3.Keysym{Scancode: scanNumLock}, false, true, false, false, false); got != "M-Clear" {
		t.Errorf("the lock cap took a pad prefix: %q", got)
	}
}

// A LATCH is not a modifier, and reading one as such breaks every second press.
//
// Pressing the cap while the lock is already on arrives with KMOD_NUM set, in
// the same field the held modifiers use. Read naively, the first press is eaten
// and the second becomes "Clear", alternating forever. CapsLock does the same
// to anyone typing in capitals, and SDL carries a third latch beyond those two.
func TestALatchIsNotAModifier(t *testing.T) {
	for _, tc := range []struct {
		mod  uint16
		what string
	}{
		{sdl3.KMOD_NUM, "numlock latched"},
		{sdl3.KMOD_CAPS, "capslock latched"},
		{sdl3.KMOD_NUM | sdl3.KMOD_CAPS, "both latched"},
	} {
		if !eatsLockCap(sdl3.Keysym{Scancode: scanNumLock, Mod: tc.mod}) {
			t.Errorf("with %s the lock cap was treated as a chord", tc.what)
		}
	}
	// And a real modifier alongside a latch is still that modifier.
	if eatsLockCap(sdl3.Keysym{Scancode: scanNumLock, Mod: sdl3.KMOD_NUM | sdl3.KMOD_SHIFT}) {
		t.Error("Shift with the lock latched was eaten; it is a chord")
	}
}

// The tracked lock is stamped onto the keysym, so one place decides and every
// namer downstream reads the corrected bit.
func TestTheTrackedLockIsStampedOntoTheKeysym(t *testing.T) {
	// A system with no latch: the pad is permanently locked, whatever SDL's
	// (never-set) bit says, so the stamp turns it on.
	l := &padLock{hasLatch: false, on: true}
	sym := sdl3.Keysym{Scancode: scanKP7}
	l.resolve(&sym)
	if sym.Mod&sdl3.KMOD_NUM == 0 {
		t.Error("a latchless system left the pad unlocked; its caps say otherwise")
	}
	if _, base, _, _ := keypadKey(sym, sym.Mod&sdl3.KMOD_NUM != 0); base != "7" {
		t.Errorf("the pad's 7 was named %q on a system with no NumLock", base)
	}

	// The cap is ours there, so it toggles — and the stamp follows it.
	if changed, on := l.toggle(); !changed || on {
		t.Fatalf("toggle -> changed=%v on=%v, want true/false", changed, on)
	}
	sym = sdl3.Keysym{Scancode: scanKP7}
	l.resolve(&sym)
	if _, base, _, _ := keypadKey(sym, sym.Mod&sdl3.KMOD_NUM != 0); base != "Home" {
		t.Errorf("after unlocking, the pad's 7 was named %q, want Home", base)
	}
}

// A real latch overrules the seed, permanently, and resyncs from every key.
//
// Only a real one can set KMOD_NUM, so seeing it once settles that this system
// has one — which is what gets a Mac keyboard on Linux right. And the user can
// toggle it while this process is not focused, so the bit is re-read on every
// key rather than counted.
func TestARealLatchOverrulesTheSeed(t *testing.T) {
	l := &padLock{hasLatch: false, on: true} // seeded as if on a Mac

	sym := sdl3.Keysym{Scancode: scanKP7, Mod: sdl3.KMOD_NUM}
	l.resolve(&sym)
	if !l.hasLatch {
		t.Fatal("KMOD_NUM arrived and the seed still claims there is no latch")
	}

	// From here the cap no longer moves our copy: the OS has already moved the
	// real one and says so on the next key.
	if changed, _ := l.toggle(); changed {
		t.Error("the cap toggled our copy on a system that keeps its own latch")
	}

	// And an ordinary letter with the bit clear resyncs us to unlocked, with no
	// pad key involved at all.
	sym = sdl3.Keysym{Sym: 'a'}
	l.resolve(&sym)
	if l.locked() {
		t.Error("the latch bit went away and the pad still reports locked")
	}
}

// The pad starts locked, before anything has been typed: what the legends
// promise, what a latchless pad does permanently, and the overwhelmingly common
// state of one with a latch.
func TestThePadStartsLocked(t *testing.T) {
	if !newPadLock().locked() {
		t.Error("a fresh pad lock reports unlocked")
	}
}
