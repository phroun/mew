//go:build sdl

package sdl

import (
	"testing"

	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// Control is spelled two ways, and which one is right follows the KEY, not
// what else is held.
//
// A key that is SHOWN — a character you can see on the cap — takes the caret,
// written against the character the key shows, with Shift absorbed into that
// character: Ctrl+5 is "^5" and Ctrl+Shift+5 is "^%". A key that is NAMED —
// Down, Tab, F5 — takes prefixes instead: "C-Down".
func TestControlOnShownKeysUsesTheCaret(t *testing.T) {
	const (
		scan5 = scan1 + 4
		scan6 = scan1 + 5
	)

	for _, tc := range []struct {
		name                  string
		sym                   sdl3.Keysym
		ctrl, alt, shift, gui bool
		want                  string
	}{
		{"ctrl on a digit", sdl3.Keysym{Sym: '5', Scancode: scan5}, true, false, false, false, "^5"},
		{"ctrl+shift absorbs into the shown character",
			sdl3.Keysym{Sym: '5', Scancode: scan5}, true, false, true, false, "^%"},
		{"ctrl+shift on six", sdl3.Keysym{Sym: '6', Scancode: scan6}, true, false, true, false, "^^"},
		{"meta keeps its prefix ahead of the caret",
			sdl3.Keysym{Sym: '5', Scancode: scan5}, true, true, false, false, "M-^5"},
		{"ctrl on punctuation", sdl3.Keysym{Sym: ';', Scancode: scanSemicolon}, true, false, false, false, "^;"},
		// A letter was already caret-spelled and must stay so; there Shift has
		// nowhere to go but a prefix, because the letter's case is already
		// spent on Control.
		{"ctrl on a letter", sdl3.Keysym{Sym: 'a', Scancode: scanA}, true, false, false, false, "^A"},
		{"ctrl+shift on a letter", sdl3.Keysym{Sym: 'a', Scancode: scanA}, true, false, true, false, "S-^A"},
	} {
		got := encodeKey(tc.sym, tc.ctrl, tc.alt, tc.shift, tc.gui, false)
		if got != tc.want {
			t.Errorf("%s: encodeKey(%q) = %q, want %q", tc.name, string(rune(tc.sym.Sym)), got, tc.want)
		}
	}
}

// The layout does not get a say in the name.
//
// Sym is layout-mapped, so the same physical key reports a different one under
// every layout: the key at the "a" position is Sym 'q' on AZERTY, and the key
// at the "2" position shows a quotation mark on a German board rather than an
// at-sign. A KeyName is defined by the POSITION, so all of those are the same
// key with the same name, and a keymap written once holds everywhere.
func TestTheLayoutDoesNotChangeTheName(t *testing.T) {
	const scan2 = scan1 + 1

	for _, tc := range []struct {
		what     string
		scancode uint32
		syms     []sdl3.Keycode
		shift    bool
		want     string
	}{
		{"the a position", scanA, []sdl3.Keycode{'a', 'q', 'ф'}, false, "a"},
		{"the a position, shifted", scanA, []sdl3.Keycode{'a', 'q', 'ф'}, true, "A"},
		{"the 2 position", scan2, []sdl3.Keycode{'2', 'é', '"'}, false, "2"},
		{"the 2 position, shifted", scan2, []sdl3.Keycode{'2', 'é', '"'}, true, "@"},
		{"the semicolon position, shifted", scanSemicolon, []sdl3.Keycode{';', 'm', 'ö'}, true, ":"},
	} {
		for _, sym := range tc.syms {
			mod := uint16(0)
			if tc.shift {
				mod = sdl3.KMOD_LSHIFT
			}
			got := translateKey(sdl3.Keysym{Sym: sym, Mod: mod, Scancode: tc.scancode})
			if got != tc.want {
				t.Errorf("%s reporting Sym %q: named %q, want %q", tc.what, rune(sym), got, tc.want)
			}
		}
	}
}
