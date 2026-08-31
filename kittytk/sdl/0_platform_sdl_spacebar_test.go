//go:build sdl

package sdl

import (
	"testing"

	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// The space bar is named, not spelled as its character.
//
// It is printable, so it used to fall through encodeKey's printable branch and
// come out " ". The keymap binds "Space", so on this host the space bar
// resolved to no command: a button, list, tree or checkbox got a key nothing
// was bound to and did nothing at all. A text field still typed a space (a
// one-rune key name IS the character) and the mew editor still saw it (it
// normalizes " " to its own "space"), which is why those two looked fine while
// everything else was dead -- and why it was this host only, since the terminal
// backend names the key itself.
func TestSpaceBarIsNamed(t *testing.T) {
	cases := []struct {
		mod  uint16
		want string
	}{
		{0, "Space"},
		{sdl3.KMOD_LSHIFT, "S-Space"},
		{sdl3.KMOD_LALT, "M-Space"},
		{sdl3.KMOD_LGUI, "s-Space"},
		// Control is the exception: NUL, spelled the way a terminal spells it.
		{sdl3.KMOD_LCTRL, "^@"},
		{sdl3.KMOD_LCTRL | sdl3.KMOD_LSHIFT, "^@"},
	}
	for _, c := range cases {
		if got := translateKey(sdl3.Keysym{Sym: ' ', Mod: c.mod}); got != c.want {
			t.Errorf("translateKey(space, mod %#x) = %q, want %q", c.mod, got, c.want)
		}
	}
}

// A press and its release have to agree on the name, or the release is filed
// under a key nothing went down as.
func TestSpaceBarPressAndReleaseAgree(t *testing.T) {
	sym := sdl3.Keysym{Sym: ' '}
	press := translateKey(sym)
	bare := bareKey(sym, false)
	if press != bare {
		t.Errorf("press names it %q but bareKey says %q", press, bare)
	}
	if bare != "Space" {
		t.Errorf("bareKey(space) = %q, want Space", bare)
	}
}
