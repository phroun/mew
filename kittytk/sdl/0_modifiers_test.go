//go:build sdl

package sdl

import (
	"strings"
	"testing"

	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// The canonical order, stated once so it can be checked rather than believed.
// It matches modifierRank in the sequence processor exactly.
var canonicalOrder = []string{"C-", "G-", "M-", "m-", "S-", "s-", "H-"}

// Every modifier prefix comes out in canonical order, in every combination.
//
// All 128 of them, because the bug this replaces was not in the common cases:
// six sites assembled prefixes by hand and two of them disagreed about whether
// C- or M- came first, which only showed up on the one chord that held both.
func TestEveryCombinationIsInCanonicalOrder(t *testing.T) {
	for bits := 0; bits < 128; bits++ {
		m := keyMods{
			ctrl:  bits&1 != 0,
			glyph: bits&2 != 0,
			mega:  bits&4 != 0,
			micro: bits&8 != 0,
			shift: bits&16 != 0,
			super: bits&32 != 0,
			hyper: bits&64 != 0,
		}
		got := m.prefix()
		rest, at := got, 0
		for _, p := range canonicalOrder {
			if strings.HasPrefix(rest, p) {
				rest, at = rest[len(p):], at+1
				continue
			}
			// Not this one; it must not appear later either.
			if strings.Contains(rest, p) {
				t.Errorf("%#v spelled %q, which has %q out of order", m, got, p)
				break
			}
		}
		if rest != "" {
			t.Errorf("%#v spelled %q, with %q left over — an unknown prefix or a "+
				"repeat", m, got, rest)
		}
	}
}

// Hyper reaches the key through encodeKey, not glued onto its answer.
//
// Prepending put it in front of every modifier that outranks it. Threading it
// through is what lets the one assembler place it, and the difference only
// shows when something else survives the promotion.
func TestHyperIsPlacedNotPrepended(t *testing.T) {
	for _, tc := range []struct {
		sym                          sdl3.Keycode
		ctrl, alt, shift, gui, hyper bool
		want, what                   string
	}{
		{sym: 'x', alt: true, hyper: true, want: "M-H-x",
			what: "Hyper beside a surviving Mega"},
		{sym: sdl3.K_DOWN, alt: true, hyper: true, want: "M-H-Down",
			what: "and on a named key"},
		{sym: 'x', hyper: true, ctrl: true, want: "H-^X",
			what: "Hyper beside a Control that keeps its caret"},
		{sym: sdl3.K_DOWN, hyper: true, want: "H-Down",
			what: "Hyper alone"},
	} {
		// The shown key needs its position, which is what names it; a named key
		// is found by Sym and takes no scancode here.
		scancode := uint32(0)
		if tc.sym == 'x' {
			scancode = hidX
		}
		got := encodeKey(sdl3.Keysym{Sym: tc.sym, Scancode: scancode}, tc.ctrl, tc.alt, tc.shift, tc.gui, tc.hyper)
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.what, got, tc.want)
		}
	}
}

// The keypad's prefix sits between the held modifiers and the key, which is
// where the canonical order puts it: C- G- M- m- S- s- H- P- p- ^Key.
func TestThePadPrefixFollowsTheHeldModifiers(t *testing.T) {
	sym := sdl3.Keysym{Scancode: scanKP7, Mod: sdl3.KMOD_NUM}
	for _, tc := range []struct {
		ctrl, alt, shift, gui, hyper bool
		want                         string
	}{
		{alt: true, want: "M-P-7"},
		{alt: true, hyper: true, want: "M-H-P-7"},
		{ctrl: true, want: "P-^7"}, // a SHOWN pad key spends Control on the caret
		{ctrl: true, alt: true, want: "M-P-^7"},
	} {
		got := encodeKey(sym, tc.ctrl, tc.alt, tc.shift, tc.gui, tc.hyper)
		if got != tc.want {
			t.Errorf("pad with ctrl=%v alt=%v hyper=%v -> %q, want %q",
				tc.ctrl, tc.alt, tc.hyper, got, tc.want)
		}
	}
}
