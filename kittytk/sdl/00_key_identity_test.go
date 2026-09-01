//go:build sdl

package sdl

import (
	"runtime"
	"testing"

	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// A key press is named from the KEY, always.
//
// Identity and text are different things arriving in different SDL events, and
// a name taken from the text leaves an ordinary letter nameless until a text
// event turns up to supply one. The key is the POSITION it sits at, which the
// scancode gives (see gridKeys).
func TestPlainPrintablesAreNamedFromTheKey(t *testing.T) {
	for _, c := range []struct {
		name     string
		sym      sdl3.Keycode
		scancode uint32
		mod      uint16
		want     string
	}{
		{"unmodified letter", 'x', hidX, 0, "x"},
		{"shifted letter spends Shift on the case", 'x', hidX, sdl3.KMOD_LSHIFT, "X"},
		{"digit", '5', hid5, 0, "5"},
		{"punctuation", ';', hidSemicolon, 0, ";"},
		{"shifted punctuation spends Shift on the second name", ';', hidSemicolon, sdl3.KMOD_LSHIFT, ":"},
		// The space bar has a name of its own: direct-key-handler calls it
		// "Space" and so does the terminal backend, so " " here would be the
		// character's name standing in for the key's -- the very substitution
		// this test forbids -- and the keymap, which binds "Space", would
		// resolve the space bar to nothing on this host. See
		// 0_platform_sdl_spacebar_test.go.
		{"space", ' ', 44, 0, "Space"},
	} {
		got := translateKey(sdl3.Keysym{Sym: c.sym, Mod: c.mod, Scancode: c.scancode})
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
		sym      sdl3.Keycode
		scancode uint32
		shift    bool
		mod      uint16
	}{
		{'x', hidX, false, 0},
		{'x', hidX, true, sdl3.KMOD_LSHIFT},
		{'7', hid1 + 6, false, 0},
		{'7', hid1 + 6, true, sdl3.KMOD_LSHIFT},
	} {
		sym := sdl3.Keysym{Sym: c.sym, Mod: c.mod, Scancode: c.scancode}
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

// A plain key REPEATING with no text produced nothing.
//
// macOS hands a held letter over as marked text rather than committing it, and
// goes on delivering repeat key-downs behind its accent palette. Naming presses
// from the key made those real keystrokes, so the drain committed the very
// character the palette had opened to replace — and then one more per repeat.
//
// A FRESH press is not that, and used to be swallowed by the same rule: with a
// palette up, reaching for "." or "/" dismisses it and types the character, and
// that keystroke was eaten outright. It produced no text because the palette
// held the keyboard for a moment, not because it produced nothing. So the
// silence belongs to the repeats alone.
func TestOnlyARepeatWithNoTextIsSilence(t *testing.T) {
	for _, c := range []struct {
		repeat bool
		want   int
	}{
		{true, 0},  // the palette's own key, held down behind it
		{false, 1}, // somebody typing
	} {
		h := &optionEventLog{}
		s := &sdlSurface{handler: h}
		p := &Platform{}
		p.pendingPress = &pendingKeyPress{key: "e", scancode: 8, repeat: c.repeat, surface: s}

		p.flushPendingPress()

		if n := len(h.keys()); n != c.want {
			t.Errorf("repeat=%v: dispatched %d keys, want %d: %v",
				c.repeat, n, c.want, h.events)
		}
		if p.pendingPress != nil {
			t.Errorf("repeat=%v: the press is still held after being flushed", c.repeat)
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

// A dead key's accent is not typed text, whichever event carries it.
//
// The composition path was built because macOS reports these as an in-flight
// composition. The same accent also arrives as ordinary committed text, and
// there it was recorded as what the chord typed — so anything falling through
// to what M-i types inserted the accent, and the next keystroke composed the
// accented character behind it: "ˆû". The accent identifies the case, not the
// event it came on.
func TestAnArmedAccentIsNotTypedText(t *testing.T) {
	for _, c := range []struct {
		text  string
		armed bool
	}{
		{"´", true},  // acute, Option+e
		{"ˆ", true},  // circumflex, Option+i
		{"˜", true},  // tilde, Option+n
		{"¨", true},  // diaeresis, Option+u
		{"`", true},  // grave
		{"å", false}, // Option+a types a character of its own
		{"û", false}, // and the accented character itself is ordinary text
		{"", false},
	} {
		if got := isArmedAccent(c.text); got != c.armed {
			t.Errorf("isArmedAccent(%q) = %v, want %v", c.text, got, c.armed)
		}
	}
}

// A dead key is known to type nothing from the chord alone, before anything
// has been produced.
//
// Waiting to learn it from the keystroke's output means never learning it when
// there is no output — and macOS often delivers neither text nor a composition
// for these, keeping the armed accent to itself. The chord then went out
// unrecorded and a consumer fell through to a table, which inserted the accent.
//
// The signature was learning: the first presses of each dead key misbehaved and
// later ones did not, because the first thing to record the chord fixed it from
// then on, one chord at a time in the order they were pressed.
func TestTheDeadKeysAreKnownByName(t *testing.T) {
	for _, chord := range []string{"M-e", "M-i", "M-n", "M-u", "M-`"} {
		if !isDeadKeyChord(chord) {
			t.Errorf("%s is not known as a dead key", chord)
		}
	}
	for _, chord := range []string{"M-a", "M-x", "a", "M-E"} {
		if isDeadKeyChord(chord) {
			t.Errorf("%s was taken for a dead key", chord)
		}
	}
	// Read from the same table as the accents, so the two cannot disagree.
	if len(deadKeyChords) != len(macOSDeadKeys) {
		t.Errorf("%d chords from %d accents", len(deadKeyChords), len(macOSDeadKeys))
	}
}

// A macOS Option chord can be named from the key alone.
//
// translateKey does not always get the chance: macOS can report Option as a
// level-3 shift, and can report the key as the character it composed, either of
// which makes translateKey yield the key-down and leave the chord to be
// recovered from the text that follows. A dead key produces no text, so there
// is nothing to recover it from, and the press is the only moment it can be
// known.
func TestNamingAMacOptionChordFromTheKey(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the naming is macOS's Option behaviour and answers false elsewhere")
	}
	for _, c := range []struct {
		what string
		sym  sdl3.Keysym
		want string
	}{
		{"Option reported as Alt", sdl3.Keysym{Sym: 'i', Mod: sdl3.KMOD_LALT}, "M-i"},
		{"Option reported as a level-3 shift", sdl3.Keysym{Sym: 'i', Mod: sdl3.KMOD_MODE}, "M-i"},
		{"Control claims it first", sdl3.Keysym{Sym: 'i', Mod: sdl3.KMOD_LALT | sdl3.KMOD_LCTRL}, ""},
		{"Command claims it first", sdl3.Keysym{Sym: 'i', Mod: sdl3.KMOD_LALT | sdl3.KMOD_LGUI}, ""},
		{"no Option at all", sdl3.Keysym{Sym: 'i'}, ""},
		{"a key with no character to name", sdl3.Keysym{Sym: sdl3.K_F1, Mod: sdl3.KMOD_LALT}, ""},
	} {
		got, ok := macOptionChordFor(c.sym)
		if !ok {
			got = ""
		}
		if got != c.want {
			t.Errorf("%s: named %q, want %q", c.what, got, c.want)
		}
	}
}

// The rules every reported keystroke follows, in the one place that holds them.
//
// Five sites have a keystroke to report — a key-down, a held press paired with
// its text, text with no press behind it, an Option chord decoded from what it
// produced, and a dead key recovered from the composition it armed. All five
// answer to these.
func TestReportingOneKeystroke(t *testing.T) {
	press := func(k keyPress) (*Platform, *optionEventLog) {
		h := &optionEventLog{}
		p := &Platform{}
		p.emitKeyPress(&sdlSurface{handler: h}, k)
		return p, h
	}

	// What the keyboard produced is recorded, and empty IS a recording when
	// the site says it observed one.
	p, _ := press(keyPress{chord: "M-a", produced: "å", observed: true})
	if text, ok := p.KeyChordText("M-a"); !ok || text != "å" {
		t.Errorf("M-a observed as %q ok=%v, want å", text, ok)
	}
	p, _ = press(keyPress{chord: "M-i", observed: true})
	if text, ok := p.KeyChordText("M-i"); !ok || text != "" {
		t.Errorf("M-i observed as %q ok=%v, want an observation of nothing", text, ok)
	}
	// A site with nothing to say leaves the memo alone, which is not the same
	// as saying the chord types nothing.
	p, _ = press(keyPress{chord: "F1"})
	if _, ok := p.KeyChordText("F1"); ok {
		t.Error("a press that observed nothing wrote to the memo")
	}

	// The chord carries what was produced, or what its key shows when nothing
	// was.
	_, h := press(keyPress{chord: "M-a", produced: "å", observed: true})
	if keys := h.keys(); len(keys) != 1 || keys[0].Text != "å" {
		t.Errorf("dispatched %v, want Text å", h.events)
	}
	_, h = press(keyPress{chord: "M-i", observed: true})
	if keys := h.keys(); len(keys) != 1 || keys[0].Text != "i" {
		t.Errorf("dispatched %v, want the key's own character", h.events)
	}

	// A release names itself from the press only where a press was registered.
	p, _ = press(keyPress{chord: "a", produced: "a", observed: true, scancode: 4, held: true})
	if name, ok := p.takeHeldKey(4); !ok || name != "a" {
		t.Errorf("held key = %q ok=%v, want a", name, ok)
	}
	p, _ = press(keyPress{chord: "M-i", observed: true, scancode: 4})
	if _, ok := p.takeHeldKey(4); ok {
		t.Error("a keystroke with no key-down behind it was registered for a release")
	}
}

// A key that TYPES NOTHING, arriving while a composition is in flight, belongs
// to the input method rather than to the document.
//
// macOS goes on delivering key-downs from behind its Japanese candidate window
// even though it is consuming them itself. Return confirms the candidate — and
// also reached the document, so the caret walked a line down the screen for
// every word confirmed, while the composition stayed exactly where it belonged.
//
// The line is text, not the key's name: a "." typed to dismiss a palette, the
// digit that picks from one, the space that confirms, and the "u" completing a
// dead key's Option+i all produce a character, and all still go through.
func TestAKeyThatTypesNothingIsTheInputMethodsWhileComposing(t *testing.T) {
	for _, c := range []struct {
		what      string
		chord     string
		produced  string
		composing bool
		want      int
	}{
		{"Return confirming a candidate", "Return", "", true, 0},
		{"an arrow walking the candidate list", "down", "", true, 0},
		{"Return with nothing composing", "Return", "", false, 1},
		{"a character typed to dismiss a composition", ".", "", true, 1},
		{"a dead key's completion", "u", "û", true, 1},
	} {
		h := &optionEventLog{}
		s := &sdlSurface{handler: h}
		p := &Platform{}
		p.ime.composing = c.composing

		p.emitKeyPress(s, keyPress{chord: c.chord, produced: c.produced, scancode: 8})

		if n := len(h.keys()); n != c.want {
			t.Errorf("%s: dispatched %d keys, want %d: %v", c.what, n, c.want, h.events)
		}
	}
}

// And a swallowed press registers no hold, so its release is dropped as the
// orphan it is rather than reported as half a keystroke.
func TestASwallowedPressLeavesNothingHeld(t *testing.T) {
	h := &optionEventLog{}
	s := &sdlSurface{handler: h}
	p := &Platform{}
	p.ime.composing = true

	p.emitKeyPress(s, keyPress{chord: "Return", scancode: 8, held: true})

	if _, held := p.takeHeldKey(8); held {
		t.Error("a key the input method took was recorded as held")
	}
}

// A commit ends the composition, so the swallow above cannot outlive it.
//
// An input method that delivers its finished text without the empty update
// that usually follows would otherwise leave a composition standing forever —
// and Return would stop working for the rest of the session.
func TestACommitEndsTheComposition(t *testing.T) {
	p := &Platform{}
	p.ime.composing = true

	p.ime.spend()

	if p.ime.holdsKeyboard() {
		t.Error("a composition is still standing after its text was committed")
	}
}
