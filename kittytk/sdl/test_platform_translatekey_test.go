//go:build sdl

package sdl

import (
	"testing"

	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// Control-punctuation combinations keep their terminal caret
// spellings so shortcuts declared as "^\\" etc. fire under SDL too.
func TestTranslateKeyControlPunctuation(t *testing.T) {
	cases := []struct {
		sym  sdl3.Keycode
		mod  uint16
		want string
	}{
		{'\\', sdl3.KMOD_LCTRL, "^\\"},
		{']', sdl3.KMOD_LCTRL, "^]"},
		{'[', sdl3.KMOD_LCTRL, "Escape"},
		{' ', sdl3.KMOD_LCTRL, "^@"},
		{'6', sdl3.KMOD_LCTRL | sdl3.KMOD_LSHIFT, "^^"},
		{'-', sdl3.KMOD_LCTRL | sdl3.KMOD_LSHIFT, "^_"},
		{'2', sdl3.KMOD_LCTRL | sdl3.KMOD_LSHIFT, "^@"},
		{'\\', sdl3.KMOD_LCTRL | sdl3.KMOD_LALT, "M-^\\"},
		{'h', sdl3.KMOD_LCTRL, "^H"}, // letters unchanged
	}
	for _, c := range cases {
		got := translateKey(sdl3.Keysym{Sym: c.sym, Mod: c.mod})
		if got != c.want {
			t.Errorf("translateKey(%q, mod %#x) = %q, want %q", c.sym, c.mod, got, c.want)
		}
	}
}

// Holding BOTH the left and right of a modifier promotes the chord to Hyper.
// The doubled modifier is consumed; any single-side modifier still held keeps
// its normal role.
func TestTranslateKeyHyper(t *testing.T) {
	const (
		bothCtrl = sdl3.KMOD_LCTRL | sdl3.KMOD_RCTRL
		bothAlt  = sdl3.KMOD_LALT | sdl3.KMOD_RALT
	)
	cases := []struct {
		name string
		sym  sdl3.Keycode
		mod  uint16
		want string
	}{
		{"both-ctrl letter", 'x', bothCtrl, "H-x"},
		{"both-alt letter", 'x', bothAlt, "H-x"},
		{"both-ctrl shifted letter", 'x', bothCtrl | sdl3.KMOD_LSHIFT, "H-X"},
		{"both-alt + single ctrl", 'x', bothAlt | sdl3.KMOD_LCTRL, "H-^X"},
		{"both-ctrl + single Mega key", 'x', bothCtrl | sdl3.KMOD_LALT, "H-M-x"},
		{"both-ctrl special key", sdl3.K_DOWN, bothCtrl, "H-Down"},
		{"both-ctrl + single Mega key special", sdl3.K_DOWN, bothCtrl | sdl3.KMOD_LALT, "H-M-Down"},
		{"both-ctrl digit", '5', bothCtrl, "H-5"},
		// A single side of a modifier does NOT promote to Hyper.
		{"single ctrl stays plain", 'x', sdl3.KMOD_LCTRL, "^X"},
		{"AltGr (a single right-hand cap) stays Mega", 'x', sdl3.KMOD_RALT, "M-x"},
		// AltGr / ISO_Level3_Shift (Glyph) yields the KEY_DOWN entirely — the
		// composed character is delivered (and tagged G-) on the TextInput path.
		{"glyph (KMOD_MODE) yields keydown", 'x', sdl3.KMOD_MODE, ""},
		{"glyph + ctrl still yields", 'x', sdl3.KMOD_MODE | sdl3.KMOD_LCTRL, ""},
	}
	for _, c := range cases {
		got := translateKey(sdl3.Keysym{Sym: c.sym, Mod: c.mod})
		if got != c.want {
			t.Errorf("%s: translateKey(%q, mod %#x) = %q, want %q", c.name, c.sym, c.mod, got, c.want)
		}
	}
}
