//go:build sdl

package sdl

import (
	"testing"

	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// A key press is named from the KEY, always.
//
// It was not: a plain or shifted printable answered "" here and got its name
// from the SDLTextInput event that followed, because the character was being
// used as the name. Identity and text are different things arriving in
// different events, and taking one from the other left an ordinary letter with
// no name of its own until a text event turned up to supply one.
func TestPlainPrintablesAreNamedFromTheKey(t *testing.T) {
	for _, c := range []struct {
		name string
		sym  sdl3.Keycode
		mod  uint16
		want string
	}{
		{"unmodified letter", 'x', 0, "x"},
		{"shifted letter spends Shift on the case", 'x', sdl3.KMOD_LSHIFT, "X"},
		{"digit", '5', 0, "5"},
		{"punctuation", ';', 0, ";"},
		{"space", ' ', 0, " "},
	} {
		got := translateKey(sdl3.Keysym{Sym: c.sym, Mod: c.mod})
		if got != c.want {
			t.Errorf("%s: translateKey(%q, mod %#x) = %q, want %q",
				c.name, c.sym, c.mod, got, c.want)
		}
	}
}

// The one key that still yields its KEY_DOWN: Glyph.
//
// AltGr is a whole set of virtual keys, and the composed character IS the
// key's name — "G-€" is what that key is called. That name cannot come from
// the key event, so it is made where the character arrives, and this is not
// the same thing as a letter having had its name taken from its text.
func TestGlyphStillYieldsItsKeyDown(t *testing.T) {
	if got := translateKey(sdl3.Keysym{Sym: 'e', Mod: sdl3.KMOD_MODE}); got != "" {
		t.Errorf("glyph named %q on the key-down; the composed key is named "+
			"where its character arrives", got)
	}
}

// A press and its release name the same key. bareKey is what a key is called
// on its own, and a plain press now agrees with it.
func TestPressAgreesWithTheBareName(t *testing.T) {
	for _, c := range []struct {
		sym   sdl3.Keycode
		shift bool
		mod   uint16
	}{
		{'x', false, 0},
		{'x', true, sdl3.KMOD_LSHIFT},
		{'7', false, 0},
	} {
		sym := sdl3.Keysym{Sym: c.sym, Mod: c.mod}
		press := translateKey(sym)
		bare := bareKey(sym, c.shift)
		if press != bare {
			t.Errorf("%q pressed as %q, bare name %q", c.sym, press, bare)
		}
	}
}

// Which presses wait for text, and which are dispatched where they are read.
//
// The wait exists so the observation is recorded BEFORE the press goes out —
// anything reading KeyChordText for the chord then sees this keystroke rather
// than the last one. A press with no text coming must not wait, or it sits
// until some later event flushes it.
func TestWhichPressesWaitForText(t *testing.T) {
	for _, c := range []struct {
		what string
		sym  sdl3.Keysym
		want bool
	}{
		{"a plain letter produces the character it shows",
			sdl3.Keysym{Sym: 'a'}, true},
		{"a shifted letter likewise",
			sdl3.Keysym{Sym: 'a', Mod: sdl3.KMOD_LSHIFT}, true},
		{"a Control chord produces no text",
			sdl3.Keysym{Sym: 'a', Mod: sdl3.KMOD_LCTRL}, false},
		{"a Command chord produces no text",
			sdl3.Keysym{Sym: 'a', Mod: sdl3.KMOD_LGUI}, false},
		{"a named key produces no text",
			sdl3.Keysym{Sym: sdl3.K_F1}, false},
	} {
		if got := keyAwaitsText(c.sym); got != c.want {
			t.Errorf("%s: waits = %v, want %v", c.what, got, c.want)
		}
	}
}

// A plain key that produced no text produced nothing.
//
// macOS hands a held letter over as marked text rather than committing it, and
// goes on delivering repeat key-downs behind its accent palette. Naming presses
// from the key made those real keystrokes, so the drain committed the very
// character the palette had opened to replace — and then one more per repeat.
func TestAPlainKeyWithNoTextIsSilence(t *testing.T) {
	for _, repeat := range []bool{false, true} {
		h := &optionEventLog{}
		s := &sdlSurface{handler: h}
		p := &Platform{}
		p.pendingPress = &pendingKeyPress{key: "e", scancode: 8, repeat: repeat, surface: s}

		p.flushPendingPress()

		if n := len(h.keys()); n != 0 {
			t.Errorf("repeat=%v: dispatched %d keys with no text: %v", repeat, n, h.events)
		}
		if p.pendingPress != nil {
			t.Errorf("repeat=%v: the press is still held after being flushed", repeat)
		}
	}
}

// An Option chord is not that. It is a CHORD that happens to compose, so the
// keystroke stands whether or not anything came of it — M-e is the shortcut
// the user pressed either way.
func TestAnOptionChordStandsWithNoComposition(t *testing.T) {
	h := &optionEventLog{}
	s := &sdlSurface{handler: h}
	p := &Platform{}
	p.pendingPress = &pendingKeyPress{
		key: "M-e", scancode: 8, surface: s, optionChord: true,
	}

	p.flushPendingPress()

	keys := h.keys()
	if len(keys) != 1 || keys[0].Key != "M-e" {
		t.Errorf("flushed %v, want one M-e", h.events)
	}
}

// A composition that is not an Option chord's own output does not become one.
//
// Any held printable can meet a composition — hold a letter down on macOS and
// its accent palette opens one. That composition belongs to the input method
// taking the keystroke over, not to the key, and dispatching it as the key's
// output put a composed letter in after the one already typed.
func TestOnlyADeadKeyClaimsAComposition(t *testing.T) {
	for _, c := range []struct {
		what        string
		optionChord bool
		want        int
	}{
		{"a dead key's composition is its own output", true, 1},
		{"an ordinary held key's is not", false, 0},
	} {
		h := &optionEventLog{}
		s := &sdlSurface{handler: h}
		p := &Platform{}
		p.pendingPress = &pendingKeyPress{
			key: "M-e", scancode: 8, surface: s, optionChord: c.optionChord,
		}
		// What the pump does for a composition, without the SDL call it makes
		// beside it: a dead key's press is dispatched with the composition, and
		// anything else is dropped so the composition stands alone.
		if p.pendingPress.optionChord {
			p.dispatchPendingPress(p.takePendingPress(), "´")
		} else {
			p.pendingPress = nil
		}
		if got := len(h.keys()); got != c.want {
			t.Errorf("%s: dispatched %d keys, want %d: %v", c.what, got, c.want, h.events)
		}
	}
}

// A dead key TYPED nothing, and nothing is recorded against it.
//
// Option+i arms a circumflex for the next keystroke. The chord is still
// reported — it was named from its own key-down, so M-i stays bindable — but
// the accent it armed is not its output. Recorded as its output, the accent was
// inserted by anything falling through to what the chord types, and then the
// composition produced the accented character as well: Option+i, u came out
// "ˆû".
func TestADeadKeyRecordsNoText(t *testing.T) {
	h := &optionEventLog{}
	s := &sdlSurface{handler: h}
	p := &Platform{}
	p.pendingPress = &pendingKeyPress{
		key: "M-i", scancode: 8, surface: s, optionChord: true,
	}

	// What the pump does when a composition follows an Option chord.
	p.dispatchPendingPress(p.takePendingPress(), "")

	keys := h.keys()
	if len(keys) != 1 || keys[0].Key != "M-i" {
		t.Fatalf("dispatched %v, want one M-i", h.events)
	}
	if text, ok := p.KeyChordText("M-i"); ok {
		t.Errorf("M-i recorded as typing %q; the accent it armed is not its output", text)
	}
}
