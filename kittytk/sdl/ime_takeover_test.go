//go:build sdl

package sdl

import "testing"

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
// method, and that is the whole of the evidence the replacement rests on.
//
// macOS commits the letter on the way down and opens its accent palette on the
// hold, so the character the palette will replace is the one that key already
// typed: exactly one rune, since only plain printables are held for text.
func TestAHeldLetterTakenOverArmsAReplacement(t *testing.T) {
	p, h := heldPlatform("o", true)
	p.flushPendingPress()

	if len(h.keys()) != 0 {
		t.Fatalf("the taken-over press was dispatched anyway: %v", h.events)
	}
	if !p.ime.holdsKeyboard() {
		t.Fatal("a letter handed to an input method left nothing armed")
	}
	if got := p.ime.takeReplace(); got != 1 {
		t.Errorf("replace = %d, want the one rune the key committed", got)
	}
}

// An Option chord is not a takeover. It is a chord that happens to compose, so
// it is dispatched whether or not anything came of it — and nothing it does
// replaces a character that is already in the document.
func TestAnOptionChordArmsNothing(t *testing.T) {
	p, h := pendingPlatform("M-e")
	p.flushPendingPress()

	if len(h.keys()) != 1 {
		t.Fatalf("dispatched %d key events, want the chord: %v", len(h.keys()), h.events)
	}
	if p.ime.holdsKeyboard() {
		t.Error("an Option chord armed a replacement it will never make")
	}
}

// The count is spent by the commit that uses it. A second commit arriving
// behind the first must not delete a second character.
func TestAReplacementIsSpentOnce(t *testing.T) {
	p, _ := heldPlatform("o", true)
	p.flushPendingPress()

	if got := p.ime.takeReplace(); got != 1 {
		t.Fatalf("first commit got replace = %d, want 1", got)
	}
	if got := p.ime.takeReplace(); got != 0 {
		t.Errorf("second commit got replace = %d, want nothing left to spend", got)
	}
	if p.ime.holdsKeyboard() {
		t.Error("the input method still holds the keyboard after committing")
	}
}

// Cancelling deletes NOTHING. The letter the palette opened over is what the
// user typed, and dismissing the palette leaves it exactly there — so a
// cancelled takeover is forgotten rather than acted on.
func TestCancellingDisarmsWithoutDeleting(t *testing.T) {
	p, _ := heldPlatform("o", true)
	p.flushPendingPress()

	p.ime.disarm()

	if p.ime.holdsKeyboard() {
		t.Error("a cancelled takeover still holds the keyboard")
	}
	if got := p.ime.takeReplace(); got != 0 {
		t.Errorf("a cancelled takeover left %d runes to delete, want none", got)
	}
}

// A composition in flight holds the keyboard on its own, with nothing armed:
// an ordinary input method appends its result rather than replacing anything.
func TestAPlainCompositionHoldsTheKeyboardAndReplacesNothing(t *testing.T) {
	p := &Platform{}
	p.ime.composing = true

	if !p.ime.holdsKeyboard() {
		t.Fatal("a composition in flight does not hold the keyboard")
	}
	if got := p.ime.takeReplace(); got != 0 {
		t.Errorf("replace = %d for a composition that replaces nothing", got)
	}
}

// A FIRST press taken over arms nothing, because nothing was committed for a
// palette to replace — and because that is the shape a dead key's completion
// has.
//
// Option+i arms a circumflex, and the "u" that completes it is a fresh press
// whose composition arrives by the same route as the palette's. Arming for it
// would delete the character standing before the accented one.
func TestAFirstPressTakenOverArmsNothing(t *testing.T) {
	p, _ := heldPlatform("u", false)
	p.flushPendingPress()

	if p.ime.holdsKeyboard() {
		t.Error("a fresh press armed a replacement; a dead key completing " +
			"would delete the character before it")
	}
}
