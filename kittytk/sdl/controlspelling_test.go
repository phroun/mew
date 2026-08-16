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
//
// This branch used to emit "C-" plus the UNSHIFTED character for every shown
// key, so Ctrl+Shift+5 came out "C-S-5": a spelling nothing else in the system
// produces or reads, invented in this function. A binding written "^%" could
// never match, and the graphical host disagreed with the terminal one about
// what the same chord was called.
func TestControlOnShownKeysUsesTheCaret(t *testing.T) {
	// The layout answers for itself; SDL is not running in a test, so stand in
	// for it with the US answers for the keys under test.
	saved := shiftedShownKey
	defer func() { shiftedShownKey = saved }()
	shiftedShownKey = func(scancode uint32) rune {
		switch scancode {
		case 5: // stands for the "5" key
			return '%'
		case 6: // stands for the "6" key
			return '^'
		}
		return 0
	}

	for _, tc := range []struct {
		name                  string
		sym                   sdl3.Keysym
		ctrl, alt, shift, gui bool
		want                  string
	}{
		{"ctrl on a digit", sdl3.Keysym{Sym: '5', Scancode: 5}, true, false, false, false, "^5"},
		{"ctrl+shift absorbs into the shown character",
			sdl3.Keysym{Sym: '5', Scancode: 5}, true, false, true, false, "^%"},
		{"ctrl+shift on six", sdl3.Keysym{Sym: '6', Scancode: 6}, true, false, true, false, "^^"},
		{"meta keeps its prefix ahead of the caret",
			sdl3.Keysym{Sym: '5', Scancode: 5}, true, true, false, false, "M-^5"},
		{"ctrl on punctuation", sdl3.Keysym{Sym: ';', Scancode: 99}, true, false, false, false, "^;"},
		// A letter was already caret-spelled and must stay so; there Shift has
		// nowhere to go but a prefix, because the letter's case is already
		// spent on Control.
		{"ctrl on a letter", sdl3.Keysym{Sym: 'a', Scancode: 4}, true, false, false, false, "^A"},
		{"ctrl+shift on a letter", sdl3.Keysym{Sym: 'a', Scancode: 4}, true, false, true, false, "S-^A"},
	} {
		got := encodeKey(tc.sym, tc.ctrl, tc.alt, tc.shift, tc.gui)
		if got != tc.want {
			t.Errorf("%s: encodeKey(%q) = %q, want %q", tc.name, string(rune(tc.sym.Sym)), got, tc.want)
		}
	}
}

// A shown key with no shifted character of its own keeps the one it has, rather
// than losing the keystroke to a layout that answered nothing.
func TestShiftWithNoShiftedCharacterKeepsTheKey(t *testing.T) {
	saved := shiftedShownKey
	defer func() { shiftedShownKey = saved }()
	shiftedShownKey = func(uint32) rune { return 0 }

	if got := encodeKey(sdl3.Keysym{Sym: '5', Scancode: 5}, true, false, true, false); got != "^5" {
		t.Errorf("encodeKey with no layout answer = %q, want %q", got, "^5")
	}
}
