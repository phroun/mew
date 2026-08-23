//go:build sdl

package sdl

import (
	"testing"
)

// A key is released under the name it was pressed under, and the record is
// keyed by the PHYSICAL key so nothing about the modifiers can disturb it.
//
// A KEY_UP carries the modifier mask as it stands at that instant, so the name
// derived from it describes the chord held now rather than the one struck.
// Letting go of Control a few milliseconds before the letter — which is what
// fingers do — reported "^A" down and "a" up: two events nothing downstream
// could pair, leaving anything that tracks held keys holding "^A" for good.
func TestAKeyIsReleasedUnderThePressName(t *testing.T) {
	const scanA, scan5 = 4, 34
	p := &Platform{}

	// Pressed as a chord, let go after the modifiers are already gone.
	p.holdKey(scanA, "^A")
	if name, held := p.takeHeldKey(scanA); !held || name != "^A" {
		t.Errorf("release of the letter key = %q (held=%v), want %q", name, held, "^A")
	}
	// And it is consumed: an entry must not outlive the key being down.
	if _, held := p.takeHeldKey(scanA); held {
		t.Error("the entry survived its release; a second KEY_UP would replay it")
	}

	// Keys are independent — two down at once, released in either order.
	p.holdKey(scanA, "^A")
	p.holdKey(scan5, "^%")
	if name, _ := p.takeHeldKey(scan5); name != "^%" {
		t.Errorf("releasing the second key gave %q, want %q", name, "^%")
	}
	if name, _ := p.takeHeldKey(scanA); name != "^A" {
		t.Errorf("releasing the first key gave %q, want %q", name, "^A")
	}
}

// A press always starts fresh. It overwrites whatever was recorded for that
// physical key, so a press can never inherit an older chord — only a release
// consumes an entry, and only the one its own press wrote.
func TestAPressOverwritesTheOlderEntry(t *testing.T) {
	const scanA = 4
	p := &Platform{}

	p.holdKey(scanA, "^A")
	p.holdKey(scanA, "a") // the first release never arrived; press it again
	if name, _ := p.takeHeldKey(scanA); name != "a" {
		t.Errorf("release after a re-press gave %q, want %q — it matched the "+
			"stale entry rather than the second press", name, "a")
	}
}

// An unmatched release reports nothing at all.
//
// Safe by construction rather than by luck: the table holds exactly what was
// EMITTED, so no entry means no press was reported for that key and nothing
// downstream believes it is held. There is no key to strand.
func TestAnUnmatchedReleaseHasNothingToReport(t *testing.T) {
	p := &Platform{}
	if name, held := p.takeHeldKey(4); held || name != "" {
		t.Errorf("a release with no press gave %q (held=%v), want nothing", name, held)
	}
	// Nor does it disturb a real pair that follows.
	p.holdKey(4, "^A")
	if name, held := p.takeHeldKey(4); !held || name != "^A" {
		t.Errorf("the pair after an orphan gave %q (held=%v), want %q", name, held, "^A")
	}
}

// An empty name is not a held key.
//
// translateKey answers "" for a plain printable, whose press is reported from
// SDLTextInput instead. Recording that would put an entry under the scancode
// with no name in it, and the release would then report a key called "".
func TestAnEmptyNameIsNotHeld(t *testing.T) {
	p := &Platform{}
	p.holdKey(4, "")
	if _, held := p.takeHeldKey(4); held {
		t.Error(`"" was recorded as a held key; its release would report a key with no name`)
	}
}

// Focus loss releases everything still down.
//
// This is the one case dropping cannot cover: the KEY_UP for a key held across
// a focus change is delivered to whoever has the keyboard now, so waiting for
// it means waiting forever and the press stands. A browser does the same on
// blur.
func TestFocusLossReleasesEverythingStillDown(t *testing.T) {
	p := &Platform{}
	p.holdKey(4, "^A")
	p.holdKey(34, "^%")
	p.holdKey(40, "Return")

	// A nil surface must not panic — the table is still cleared, because the
	// keys are up whether or not anyone is listening.
	p.releaseHeldKeys(nil)
	if len(p.heldKeys) != 0 {
		t.Errorf("the table still holds %v after a flush", p.heldKeys)
	}
	// And a second flush finds nothing rather than replaying.
	p.releaseHeldKeys(nil)
	if len(p.heldKeys) != 0 {
		t.Errorf("a second flush left %v", p.heldKeys)
	}
}
