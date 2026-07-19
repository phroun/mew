//go:build sdl

package sdl

import (
	"testing"

	sdl2 "github.com/veandco/go-sdl2/sdl"
)

// Control-punctuation combinations keep their terminal caret
// spellings so shortcuts declared as "^\\" etc. fire under SDL too.
func TestTranslateKeyControlPunctuation(t *testing.T) {
	cases := []struct {
		sym  sdl2.Keycode
		mod  uint16
		want string
	}{
		{'\\', sdl2.KMOD_LCTRL, "^\\"},
		{']', sdl2.KMOD_LCTRL, "^]"},
		{'[', sdl2.KMOD_LCTRL, "Escape"},
		{' ', sdl2.KMOD_LCTRL, "^@"},
		{'6', sdl2.KMOD_LCTRL | sdl2.KMOD_LSHIFT, "^^"},
		{'-', sdl2.KMOD_LCTRL | sdl2.KMOD_LSHIFT, "^_"},
		{'2', sdl2.KMOD_LCTRL | sdl2.KMOD_LSHIFT, "^@"},
		{'\\', sdl2.KMOD_LCTRL | sdl2.KMOD_LALT, "M-^\\"},
		{'h', sdl2.KMOD_LCTRL, "^H"}, // letters unchanged
	}
	for _, c := range cases {
		got := translateKey(sdl2.Keysym{Sym: c.sym, Mod: c.mod})
		if got != c.want {
			t.Errorf("translateKey(%q, mod %#x) = %q, want %q", c.sym, c.mod, got, c.want)
		}
	}
}
