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
