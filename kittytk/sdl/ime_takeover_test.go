//go:build sdl

package sdl

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// heldPlatform makes a platform holding one PLAIN printable, as the key-down
// path does for any key whose text arrives in the next event. repeat says
// whether this is a press behind a held key rather than the first one.
func heldPlatform(key string, repeat bool) (*Platform, *optionEventLog) {
	h := &optionEventLog{}
	s := &sdlSurface{handler: h}
	p := &Platform{}
	p.pendingPress = &pendingKeyPress{key: key, scancode: 4, surface: s, repeat: repeat}
	return p, h
}

// A held plain printable whose text never came has been taken over by an input
// method, and that is the whole of the evidence this rests on.
//
// macOS commits the letter on the way down and opens its palette on the hold,
// so the composition stands OVER the character that key already typed: exactly
// one rune, since only plain printables are held for text.
func TestAHeldLetterTakenOverOpensACompositionOverIt(t *testing.T) {
	p, h := heldPlatform("o", true)
	p.flushPendingPress()

	if len(h.keys()) != 0 {
		t.Fatalf("the taken-over press was dispatched anyway: %v", h.events)
	}
	if !p.ime.holdsKeyboard() {
		t.Fatal("a letter handed to an input method opened no composition")
	}
	if got := p.ime.covers; got != 1 {
		t.Errorf("composition covers %d, want the one rune the key committed", got)
	}
	// And the sink is TOLD, showing the letter in its own place — which is
	// what makes the route where SDL reports no composition at all behave
	// like the one where it does.
	var opened []core.TextEditingEvent
	for _, ev := range h.events {
		if c, ok := ev.(core.TextEditingEvent); ok {
			opened = append(opened, c)
		}
	}
	if len(opened) != 1 {
		t.Fatalf("announced %d compositions, want one: %v", len(opened), h.events)
	}
	if opened[0].Text != "o" || opened[0].Covers != 1 {
		t.Errorf("composition = %q covering %d, want the letter over itself",
			opened[0].Text, opened[0].Covers)
	}
}

// An Option chord is not a takeover. It is a chord that happens to compose, so
// it is dispatched whether or not anything came of it — and nothing it does
// replaces a character that is already in the document.
func TestAnOptionChordOpensNothing(t *testing.T) {
	p, h := pendingPlatform("M-e")
	p.flushPendingPress()

	if len(h.keys()) != 1 {
		t.Fatalf("dispatched %d key events, want the chord: %v", len(h.keys()), h.events)
	}
	if p.ime.holdsKeyboard() {
		t.Error("an Option chord opened a composition over a letter it never typed")
	}
}

// The composition is spent by the commit that ends it. A second commit arriving
// behind the first must not open over a second character.
func TestACompositionIsSpentOnce(t *testing.T) {
	p, _ := heldPlatform("o", true)
	p.flushPendingPress()

	if got := p.ime.covers; got != 1 {
		t.Fatalf("composition covers %d, want 1", got)
	}
	p.ime.spend()
	if p.ime.holdsKeyboard() {
		t.Error("the input method still holds the keyboard after committing")
	}
	if got := p.ime.covers; got != 0 {
		t.Errorf("a spent composition still covers %d, want nothing", got)
	}
}

// Cancelling deletes NOTHING. The letter the palette opened over is what the
// user typed, and dismissing the palette leaves it exactly there — so a
// cancelled takeover is forgotten rather than acted on.
func TestCancellingDisarmsWithoutDeleting(t *testing.T) {
	p, h := heldPlatform("o", true)
	p.flushPendingPress()

	p.cancelComposition(&sdlSurface{handler: h})

	if p.ime.holdsKeyboard() {
		t.Error("a cancelled takeover still holds the keyboard")
	}
	if got := p.ime.covers; got != 0 {
		t.Errorf("a cancelled takeover still covers %d runes, want none", got)
	}
	// And the sink is told to END it, or the letter it was hiding stays hidden
	// for good. Nothing is deleted: the letter comes back as it was typed.
	last, ok := h.events[len(h.events)-1].(core.TextEditingEvent)
	if !ok || last.Text != "" {
		t.Errorf("last event %#v, want an empty composition ending it", h.events[len(h.events)-1])
	}
}

// A composition in flight holds the keyboard on its own, covering nothing: an
// ordinary input method builds text that was never in the document.
func TestAPlainCompositionCoversNothing(t *testing.T) {
	p := &Platform{}
	p.ime.composing = true

	if !p.ime.holdsKeyboard() {
		t.Fatal("a composition in flight does not hold the keyboard")
	}
	if got := p.ime.covers; got != 0 {
		t.Errorf("covers = %d for a composition opened over nothing", got)
	}
}

// A FIRST press taken over opens nothing, because nothing was committed for a
// composition to stand over — and because that is the shape a dead key's
// completion has.
//
// Option+i arms a circumflex, and the "u" that completes it is a fresh press
// whose composition arrives by the same route as the palette's. Opening over it
// would hide, and then replace, the character standing before the accented one.
func TestAFirstPressTakenOverOpensNothing(t *testing.T) {
	p, _ := heldPlatform("u", false)
	p.flushPendingPress()

	if p.ime.holdsKeyboard() {
		t.Error("a fresh press opened a composition; a dead key completing " +
			"would replace the character before it")
	}
}

// A FRESH press with a palette up is somebody TYPING, and it must not be eaten.
//
// The swallowing exists for the repeats behind a held key: each of those
// committed a copy of the very character the palette had opened to replace.
// A different key is not that. Reaching for "." or "/" dismisses the palette
// and types the character, and swallowing it lost the keystroke outright — it
// produced no text because the palette held the keyboard for a moment, not
// because it produced nothing.
func TestAFreshPressWithAPaletteUpIsStillTyped(t *testing.T) {
	p, h := heldPlatform("o", true)
	p.flushPendingPress() // the palette takes the held letter

	// Now a different key, pressed once.
	s := &sdlSurface{handler: h}
	p.pendingPress = &pendingKeyPress{key: ".", scancode: 55, surface: s}
	p.flushPendingPress()

	var typed []string
	for _, k := range h.keys() {
		typed = append(typed, k.Key)
	}
	if len(typed) != 1 || typed[0] != "." {
		t.Errorf("dispatched %v, want the \".\" that was pressed", typed)
	}
}

// The repeats behind the palette's OWN key are still swallowed, which is the
// case the swallowing was built for.
func TestThePalettesOwnRepeatsAreStillSwallowed(t *testing.T) {
	p, h := heldPlatform("o", true)
	p.flushPendingPress()

	s := &sdlSurface{handler: h}
	for i := 0; i < 3; i++ {
		p.pendingPress = &pendingKeyPress{key: "o", scancode: 18, surface: s, repeat: true}
		p.flushPendingPress()
	}

	if n := len(h.keys()); n != 0 {
		t.Errorf("dispatched %d presses from behind the palette, want none", n)
	}
}

// A palette DISMISSED by typing sends no ending update of its own, so the
// keystroke that dismissed it is what has to end the composition.
//
// Left standing, the letter under it kept painting as provisional text while
// the typing ran on past it — "Hrllo. What?" with the "o" still underlined and
// the composition armed behind everything that followed.
func TestTypingOnEndsAPaletteThatWasNeverConfirmed(t *testing.T) {
	p, h := heldPlatform("o", true)
	p.flushPendingPress()
	// SDL reported a composition, and then nothing more: the palette was
	// dismissed rather than confirmed.
	p.ime.composing = true

	s := &sdlSurface{handler: h}
	p.emitKeyPress(s, keyPress{chord: ".", produced: ".", observed: true, origin: "test"})

	if p.ime.holdsKeyboard() {
		t.Error("the composition outlived the keystroke that dismissed its palette")
	}
	last, ok := h.events[len(h.events)-1].(core.KeyPressEvent)
	if !ok || last.Key != "." {
		t.Errorf("last event %#v, want the keystroke itself", h.events[len(h.events)-1])
	}
	var ended bool
	for _, ev := range h.events {
		if c, ok := ev.(core.TextEditingEvent); ok && c.Text == "" {
			ended = true
		}
	}
	if !ended {
		t.Error("the sink was never told to end the composition, so the letter " +
			"it was hiding stays painted as provisional")
	}
}

// A composition this platform did NOT open is left alone. A dead key
// deliberately leaves one standing for the next keystroke to compose against,
// and the "u" completing Option+i arrives through the same door.
func TestAKeystrokeLeavesADeadKeysCompositionStanding(t *testing.T) {
	h := &optionEventLog{}
	s := &sdlSurface{handler: h}
	p := &Platform{}
	p.ime.composing = true // SDL's own, from Option+i — nothing armed here

	p.emitKeyPress(s, keyPress{chord: "u", produced: "û", observed: true, origin: "test"})

	for _, ev := range h.events {
		if _, ok := ev.(core.TextEditingEvent); ok {
			t.Errorf("ended a composition that was not ours: %v", h.events)
		}
	}
}
