//go:build sdl

package sdl

import (
	"testing"

	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// pad builds a keysym for a physical keypad position. Scancode is what matters
// — an SDL scancode is a USB HID usage ID, so it names the position and means
// the same thing under every layout. Sym is left zero deliberately: if a test
// passes with no Sym at all, the pad is genuinely being read by position.
func pad(scancode uint32, numLock bool) sdl3.Keysym {
	k := sdl3.Keysym{Scancode: scancode}
	if numLock {
		k.Mod = sdl3.KMOD_NUM
	}
	return k
}

// NumLock decides which of a dual-legend cap's two keys was struck, and the pad
// is told apart from the main cluster either way.
//
// This is the rule the caps are printed with: locked gives the digit, unlocked
// gives the navigation action. Before this the graphical host had no keypad at
// all — an unlocked pad Home arrived as plain "Home", indistinguishable from the
// main cluster's, and the pad's digits arrived only as bare characters through
// the text path, so nothing could bind them apart on a desktop while the
// terminal host could.
func TestNumLockPicksWhichKeyThePadCapIs(t *testing.T) {
	for _, tc := range []struct {
		scancode         uint32
		locked, unlocked string
	}{
		{scanKP0, "P-0", "P-Insert"},
		{scanKP1, "P-1", "P-End"},
		{scanKP2, "P-2", "P-Down"},
		{scanKP3, "P-3", "P-PageDown"},
		{scanKP4, "P-4", "P-Left"},
		{scanKP5, "P-5", "P-Begin"},
		{scanKP6, "P-6", "P-Right"},
		{scanKP7, "P-7", "P-Home"},
		{scanKP8, "P-8", "P-Up"},
		{scanKP9, "P-9", "P-PageUp"},
		// The pad's own erase, which is a PAD ACTION rather than forward
		// delete: the cap says DEL and it lives on the pad.
		{scanKPPeriod, "P-.", "P-Delete"},
	} {
		if got := encodeKey(pad(tc.scancode, true), false, false, false, false, false); got != tc.locked {
			t.Errorf("scancode %d locked = %q, want %q", tc.scancode, got, tc.locked)
		}
		if got := encodeKey(pad(tc.scancode, false), false, false, false, false, false); got != tc.unlocked {
			t.Errorf("scancode %d unlocked = %q, want %q", tc.scancode, got, tc.unlocked)
		}
	}
}

// The keys that ignore the lock keep one meaning, whatever it says.
func TestTheLockDoesNotTouchTheOperators(t *testing.T) {
	for _, tc := range []struct {
		scancode uint32
		want     string
	}{
		{scanKPDivide, "P-/"},
		{scanKPMultiply, "P-*"},
		{scanKPMinus, "P--"},
		{scanKPPlus, "P-+"},
		{scanKPEquals, "P-="},
		// The pad's Enter is never the home row's Return, which is the
		// distinction this backend used to lose by naming both "Enter".
		{scanKPEnter, "P-Enter"},
	} {
		for _, numLock := range []bool{true, false} {
			if got := encodeKey(pad(tc.scancode, numLock), false, false, false, false, false); got != tc.want {
				t.Errorf("scancode %d with NumLock=%v = %q, want %q",
					tc.scancode, numLock, got, tc.want)
			}
		}
	}
}

// The home row's Return is still itself. The pad now owns the "Enter" name, and
// a backend that let that leak onto the home-row key would break every binding
// written for Return.
func TestTheHomeRowKeyIsUntouched(t *testing.T) {
	sym := sdl3.Keysym{Sym: sdl3.K_RETURN, Scancode: 40}
	if got := encodeKey(sym, false, false, false, false, false); got != "Return" {
		t.Errorf("Return = %q, want %q", got, "Return")
	}
	// And the main cluster's navigation keys keep their bare names, since the
	// prefix is what distinguishes the pad rather than a rename of everything.
	for _, tc := range []struct {
		sym  sdl3.Keycode
		scan uint32
		want string
	}{
		{sdl3.K_HOME, 74, "Home"},
		{sdl3.K_UP, 82, "Up"},
		{sdl3.K_DELETE, 76, "FDel"},
	} {
		got := encodeKey(sdl3.Keysym{Sym: tc.sym, Scancode: tc.scan}, false, false, false, false, false)
		if got != tc.want {
			t.Errorf("main-cluster %v = %q, want %q", tc.want, got, tc.want)
		}
	}
}

// This is the channel that can tell the duplicated pad characters apart.
//
// A terminal cannot: the kitty protocol resolves one KP_SEPARATOR from an xkb
// keysym, so every pad comma in existence arrives collapsed onto a single code.
// Reading HID usage IDs, they are simply different numbers — so the lowercase
// prefix finally means something here, and it is the only place it can.
func TestTheDuplicatedPadCharactersAreToldApart(t *testing.T) {
	for _, tc := range []struct {
		scancode uint32
		want     string
		what     string
	}{
		// The comma above Enter, which a DEC LK201 carries and an AS/400
		// column keeps beside its own equals — adjacent usages, 133 and 134.
		{scanKPComma, "p-,", "the archaic comma"},
		{scanKPEqualsAS400, "p-=", "and the equals beside it"},
		// A PC-98's comma sits in the bottom row next to the period, and HID
		// reaches it as International6 rather than as any keypad usage.
		{scanInternational6, "P-,", "the PC-98 comma"},
		{scanKPEquals, "P-=", "an ordinary pad's equals"},
	} {
		if got := encodeKey(pad(tc.scancode, true), false, false, false, false, false); got != tc.want {
			t.Errorf("%s (scancode %d) = %q, want %q", tc.what, tc.scancode, got, tc.want)
		}
	}

	// Nothing may collide: two pad keys under one spelling would be two
	// physical keys a keymap cannot separate, which is the entire reason the
	// lowercase form exists.
	seen := map[string]uint32{}
	for scancode := range keypadKeys {
		p, b, _, _ := keypadKey(pad(scancode, true), true)
		if prev, dup := seen[p+b]; dup {
			t.Errorf("scancodes %d and %d both spell themselves %q", prev, scancode, p+b)
		}
		seen[p+b] = scancode
	}
	for scancode := range archaicPadKeys {
		p, b, _, _ := keypadKey(pad(scancode, true), true)
		if prev, dup := seen[p+b]; dup {
			t.Errorf("scancodes %d and %d both spell themselves %q", prev, scancode, p+b)
		}
		seen[p+b] = scancode
	}
}

// Control follows the key, on the pad as everywhere else: a SHOWN key takes the
// caret against the character it shows, a NAMED key takes the "C-" prefix. The
// pad prefix sits outside the caret, where the canonical order puts it —
// C- G- M- m- S- s- H- P- p- ^Key.
func TestControlOnThePadFollowsTheKey(t *testing.T) {
	for _, tc := range []struct {
		sym                   sdl3.Keysym
		ctrl, alt, shift, gui bool
		want                  string
		what                  string
	}{
		{pad(scanKP7, true), true, false, false, false, "P-^7", "shown: the caret"},
		{pad(scanKPMinus, true), true, false, false, false, "P-^-", "punctuation is shown too"},
		{pad(scanKPComma, true), true, false, false, false, "p-^,", "and the archaic prefix behaves alike"},
		// Shift stays a prefix rather than being absorbed into the character,
		// because a pad character has no shifted form to absorb it into.
		{pad(scanKP7, true), true, false, true, false, "S-P-^7", "shift keeps its prefix"},
		{pad(scanKP7, true), false, false, true, false, "S-P-7", "and leaves the character alone"},
		// A name has no character for the caret to sit against.
		{pad(scanKP7, false), true, false, false, false, "C-P-Home", "named: the prefix"},
		{pad(scanKPEnter, true), true, false, false, false, "C-P-Enter", ""},
		{pad(scanKP5, false), true, false, false, false, "C-P-Begin", ""},
		{pad(scanKP7, true), false, true, false, false, "M-P-7", "Mega"},
		{pad(scanKP7, true), false, false, false, true, "s-P-7", "Super"},
	} {
		got := encodeKey(tc.sym, tc.ctrl, tc.alt, tc.shift, tc.gui, false)
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.what, got, tc.want)
		}
	}
}

// A pad key comes UP under the name it went down with.
//
// A release takes the bareKey path, which was a Sym lookup — and an unlocked pad
// cap can arrive carrying the navigation Sym, so the pad's Home would have gone
// down as "P-Home" and come up as "Home". A release that does not match its
// press is a key held forever by anything tracking state.
func TestAPadKeyComesUpUnderTheNameItWentDownWith(t *testing.T) {
	for _, tc := range []struct {
		scancode uint32
		numLock  bool
	}{
		{scanKP7, true}, {scanKP7, false},
		{scanKPEnter, true}, {scanKPPeriod, false},
		{scanKPComma, true}, {scanInternational6, true},
	} {
		sym := pad(tc.scancode, tc.numLock)
		down := encodeKey(sym, false, false, false, false, false)
		up := bareKey(sym, false)
		if down != up {
			t.Errorf("scancode %d (NumLock=%v) went down as %q and came up as %q",
				tc.scancode, tc.numLock, down, up)
		}
	}
}

// Nothing on the pad is left to the text path.
//
// Every other text-producing key answers "" here and lets SDLTextInput deliver the
// character. A pad key cannot: the point is to report WHICH 7 was struck, and a
// bare "7" has no room to say. So every pad position must name itself on the way
// down — and the ones that would ALSO have produced text are flagged, so the
// character can be dropped instead of arriving as a second press.
func TestEveryPadPositionNamesItselfAndFlagsItsText(t *testing.T) {
	all := map[uint32]bool{scanInternational6: true}
	for s := range keypadKeys {
		all[s] = true
	}
	for s := range archaicPadKeys {
		all[s] = true
	}
	for scancode := range all {
		for _, numLock := range []bool{true, false} {
			sym := pad(scancode, numLock)
			if got := encodeKey(sym, false, false, false, false, false); got == "" {
				t.Errorf("scancode %d (NumLock=%v) yielded \"\"; it would be left "+
					"to the text path, where the prefix is lost", scancode, numLock)
			}
			// shown is what the suppression latch reads. A shown key produces a
			// character SDL will also send; a named one does not.
			_, base, shown, ok := keypadKey(sym, numLock)
			if !ok {
				t.Fatalf("scancode %d is not on the pad", scancode)
			}
			if shown != (len([]rune(base)) == 1) {
				t.Errorf("scancode %d (NumLock=%v) base %q reports shown=%v; a "+
					"single character is shown and a word is not, and the text "+
					"suppression depends on that being right", scancode, numLock, base, shown)
			}
		}
	}
}

// A key that is not on the pad is not claimed by it. The pad branch runs first,
// before every other classifier, so a false positive there would rename a key
// nothing could then bind.
func TestOffPadScancodesAreNotClaimed(t *testing.T) {
	for _, scancode := range []uint32{
		4,       // 'a'
		34,      // '5' on the number row
		40,      // Return
		42,      // the erase-left key
		74,      // Home, main cluster
		82,      // Up, main cluster
		100,     // Zag, the ISO key beside Z
		135,     // Ro
		140 + 1, // International7, one past the PC-98 comma
		102,     // Power
	} {
		if _, _, _, ok := keypadKey(sdl3.Keysym{Scancode: scancode}, true); ok {
			t.Errorf("scancode %d was claimed as a keypad key", scancode)
		}
	}
	// NumLock itself is on the pad physically but is not a pad ACTION — it is
	// the latch that decides what the other caps are, and giving it a "P-"
	// name would make the lock look like something to bind as a key.
	if _, _, _, ok := keypadKey(sdl3.Keysym{Scancode: scanNumLock}, true); ok {
		t.Error("NumLock was claimed as a keypad key; it is the latch, not a key on it")
	}
}
