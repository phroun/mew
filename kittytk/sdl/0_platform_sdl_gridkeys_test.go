//go:build sdl

package sdl

import (
	"testing"

	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// The USB HID keyboard usage IDs of the keys these tests press, written out
// rather than looked up: a test that asked gridKeys where "x" is would be
// asking the table under test to state its own answer.
const (
	hidA = 4
	hidH = 11
	hidQ = 20
	hidX = 27
	hidZ = 29

	hid1            = 30
	hid2            = 31
	hid5            = 34
	hid6            = 35
	hid0            = 39
	hidMinus        = 45
	hidEquals       = 46
	hidLeftBracket  = 47
	hidRightBracket = 48
	hidBackslash    = 49
	hidZig          = 50
	hidSemicolon    = 51
	hidApostrophe   = 52
	hidGrave        = 53
	hidComma        = 54
	hidPeriod       = 55
	hidSlash        = 56
	hidZag          = 100
	hidRo           = 135
	hidYen          = 137
)

// The two layers of the main cluster, from the Regular Keys and Shifted Keys
// tables in the direct-key-handler wiki. Shift is spent on the second name
// rather than stated as a prefix, so "1" and "!" are the two names of one key.
//
// Only the letters spend it on case, which is why naming a shifted symbol from
// the character its key shows UNSHIFTED went unnoticed: the letters were right,
// so Shift+1 answering "1" was the only thing wrong, and it was wrong all the
// way through to a terminal, which types the name it is handed.
func TestBothLayersOfEveryShownPosition(t *testing.T) {
	for _, c := range []struct {
		scancode         uint32
		regular, shifted string
	}{
		{hidGrave, "`", "~"},
		{hid1, "1", "!"},
		{hid1 + 1, "2", "@"},
		{hid1 + 2, "3", "#"},
		{hid1 + 3, "4", "$"},
		{hid1 + 4, "5", "%"},
		{hid1 + 5, "6", "^"},
		{hid1 + 6, "7", "&"},
		{hid1 + 7, "8", "*"},
		{hid1 + 8, "9", "("},
		{hid0, "0", ")"},
		{hidMinus, "-", "_"},
		{hidEquals, "=", "+"},

		{hidLeftBracket, "[", "{"},
		{hidRightBracket, "]", "}"},
		{hidBackslash, "\\", "|"},
		{hidSemicolon, ";", ":"},
		{hidApostrophe, "'", "\""},
		{hidComma, ",", "<"},
		{hidPeriod, ".", ">"},
		{hidSlash, "/", "?"},

		{hidA, "a", "A"},
		{hidQ, "q", "Q"},
		{hidX, "x", "X"},
		{hidZ, "z", "Z"},
	} {
		plain := sdl3.Keysym{Sym: sdl3.Keycode(c.regular[0]), Scancode: c.scancode}
		if got := translateKey(plain); got != c.regular {
			t.Errorf("scancode %d unshifted = %q, want %q", c.scancode, got, c.regular)
		}
		shifted := plain
		shifted.Mod = sdl3.KMOD_LSHIFT
		if got := translateKey(shifted); got != c.shifted {
			t.Errorf("scancode %d shifted = %q, want %q", c.scancode, got, c.shifted)
		}
		// A press and its release name the same key, in whichever layer.
		if got := bareKey(plain, false); got != c.regular {
			t.Errorf("scancode %d bare = %q, want %q", c.scancode, got, c.regular)
		}
		if got := bareKey(shifted, true); got != c.shifted {
			t.Errorf("scancode %d bare shifted = %q, want %q", c.scancode, got, c.shifted)
		}
	}
}

// The positions the grid has no character for. Their characters belong to other
// positions -- Zag prints "<" and ">" on a German board, which are Shift+comma
// and Shift+period -- so they are NAMED, and a named key takes Shift as a prefix
// because it has no second character to spend it on.
func TestPositionsWithNoCharacterAreNamed(t *testing.T) {
	for _, c := range []struct {
		scancode uint32
		sym      sdl3.Keycode
		want     string
		shifted  string
	}{
		{hidZig, '#', "Zig", "S-Zig"},
		{hidZag, '<', "Zag", "S-Zag"},
		{hidRo, '\\', "Ro", "S-Ro"},
		{hidYen, '\\', "Yen", "S-Yen"},
	} {
		sym := sdl3.Keysym{Sym: c.sym, Scancode: c.scancode}
		if got := translateKey(sym); got != c.want {
			t.Errorf("scancode %d = %q, want %q", c.scancode, got, c.want)
		}
		sym.Mod = sdl3.KMOD_LSHIFT
		if got := translateKey(sym); got != c.shifted {
			t.Errorf("scancode %d shifted = %q, want %q", c.scancode, got, c.shifted)
		}
	}
}

// The C0 positions, which keep the caret spellings the control table in the
// direct-key-handler wiki gives them, so a key string matches the TUI backend's.
//
// Each layer picks its own: "^" is the shifted 6 and takes "^^", while the
// unshifted 6 is "^6" and the two are different chords.
func TestControlSpellsTheC0Positions(t *testing.T) {
	for _, c := range []struct {
		what     string
		scancode uint32
		sym      sdl3.Keycode
		shift    bool
		want     string
	}{
		{"the [ position", hidLeftBracket, '[', false, "Escape"},
		{"the { layer of it", hidLeftBracket, '[', true, "^{"},
		{"the backslash position", hidBackslash, '\\', false, "^\\"},
		{"the | layer of it", hidBackslash, '\\', true, "^|"},
		{"the ] position", hidRightBracket, ']', false, "^]"},
		{"the } layer of it", hidRightBracket, ']', true, "^}"},
		{"the 6 position", hid6, '6', false, "^6"},
		{"the ^ layer of it", hid6, '6', true, "^^"},
		{"the - position", hidMinus, '-', false, "^-"},
		{"the _ layer of it", hidMinus, '-', true, "^_"},
		{"the 2 position", hid2, '2', false, "^2"},
		{"the @ layer of it", hid2, '2', true, "^@"},
		// Ctrl+/ collapses onto ^_ by the xterm convention, so it reaches a
		// terminal app; shifted, the position is "?" and keeps its own name.
		{"the / position", hidSlash, '/', false, "^_"},
		{"the ? layer of it", hidSlash, '/', true, "^?"},
	} {
		mod := uint16(sdl3.KMOD_LCTRL)
		if c.shift {
			mod |= sdl3.KMOD_LSHIFT
		}
		got := translateKey(sdl3.Keysym{Sym: c.sym, Mod: mod, Scancode: c.scancode})
		if got != c.want {
			t.Errorf("Control on %s = %q, want %q", c.what, got, c.want)
		}
	}
}

// A shifted symbol reaches a terminal under its own name.
//
// PurfecTerm forwards event.Key to the child and types what it is handed, so a
// key named for its unshifted layer put a "1" in the child for every "!" the
// person pressed. Letters were unaffected -- their two layers are a case
// change -- which is what made it look like a terminal problem rather than a
// naming one.
func TestShiftedSymbolsAreNamedForWhatWasPressed(t *testing.T) {
	for _, c := range []struct {
		scancode uint32
		sym      sdl3.Keycode
		want     string
	}{
		{hid1, '1', "!"},
		{hid2, '2', "@"},
		{hid5, '5', "%"},
		{hidSemicolon, ';', ":"},
		{hidSlash, '/', "?"},
		{hidGrave, '`', "~"},
		{hidApostrophe, '\'', "\""},
		{hidX, 'x', "X"},
	} {
		got := translateKey(sdl3.Keysym{Sym: c.sym, Mod: sdl3.KMOD_LSHIFT, Scancode: c.scancode})
		if got != c.want {
			t.Errorf("Shift on the %q key = %q, want %q", rune(c.sym), got, c.want)
		}
	}
}

// Every position is held for the character it types, so the memo records what
// this keyboard actually produced there.
//
// That is what keeps text right while the NAME follows the position: Shift+2 on
// a German board is named "@" and observed typing a quotation mark, and a text
// field reads the observation. A position not held is a position the memo never
// learns, and a key named "Zag" would then type nothing at all.
func TestEveryPositionWaitsForItsText(t *testing.T) {
	for _, scancode := range []uint32{hidA, hidX, hid1, hid5, hidSemicolon, hidSlash, hidZag, hidRo, hidYen} {
		for _, mod := range []uint16{0, sdl3.KMOD_LSHIFT} {
			// Sym 0 stands for a layout this host has no keycode for: the
			// position is still a key, and it still types.
			sym := sdl3.Keysym{Scancode: scancode, Mod: mod}
			if !keyAwaitsText(sym) {
				t.Errorf("scancode %d (mod %#x) is not held for its text", scancode, mod)
			}
		}
	}
}
