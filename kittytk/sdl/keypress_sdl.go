//go:build sdl

package sdl

import "github.com/phroun/kittytk/core"

// Reporting one keystroke.
//
// A keystroke's identity reaches this host on any of three SDL events, and on
// two of them it has to be RECOVERED rather than read: SDL delivers a key-down,
// or text with no key-down behind it, or — for a dead key, which produces no
// text at all — only a composition. Five places therefore have a keystroke to
// report, and which of them runs depends on the platform and the input source:
// on macOS an Option chord usually does not arrive as a key-down at all.
//
// What those five do is the same. Each is left with the part that differs —
// what the chord is, and what was observed — and the rest is here.

// keyPress is one keystroke about to be reported, as its site knows it.
type keyPress struct {
	// chord is the KeyName — what a binding is written against.
	chord string

	// produced is the text the keyboard made for this chord, and observed is
	// whether that is a report at all.
	//
	// The two are separate because empty is an answer: a dead key arms an
	// accent and types NOTHING, and recording that is what stops a consumer
	// falling back to a table of what such a chord types elsewhere. A site with
	// nothing to say leaves observed false and the memo untouched.
	produced string
	observed bool

	// scancode names the physical key, and held asks for this press to be
	// registered under it so the release can be reported by the same name.
	//
	// Not every site has one. A dead key recovered from its composition never
	// had a key-down and will get no key-up, so registering it would leave an
	// entry nothing ever claims.
	scancode uint32
	held     bool

	repeat bool

	// origin says which of the five reported this, for the trace: whether a
	// chord was dispatched at all, and by which path, is the first thing worth
	// knowing about a keystroke that behaved oddly.
	origin string
}

// emitKeyPress reports one keystroke: the observation, then the press.
func (p *Platform) emitKeyPress(s *sdlSurface, k keyPress) {
	// What the keyboard produced is recorded before anything else. It is a fact
	// about the keyboard and stays true whether or not there is still a surface
	// to deliver the keystroke to.
	if k.observed {
		if k.produced == "" {
			p.noteKeyChordTypesNothing(k.chord)
		} else {
			p.noteKeyChordText(k.chord, k.produced)
		}
	}
	// A keystroke reaching the application means the input method no longer has
	// the keyboard: whatever palette was open is gone. This DELETES NOTHING —
	// the character the held key committed is what the user typed, and a
	// dismissed palette leaves it exactly there.
	//
	// Unconditional, because the keys that drive an input method never arrive
	// here: a candidate list's arrows, digits and Return are consumed by the
	// input method itself. A key that reaches this far is one it did not take.
	//
	// Only a takeover of ours is ended (see cancelComposition), so a dead key's
	// deliberately-standing composition is untouched — the "u" completing
	// Option+i comes through here and must still find the circumflex waiting.
	p.cancelComposition(s)
	if s == nil || s.handler == nil {
		return
	}

	mods, name := core.ParseKeyModifiers(k.chord)
	text := k.produced
	if text == "" && len(name) == 1 && name[0] >= 32 && name[0] < 127 {
		// Nothing was produced, so the chord carries what the key itself shows.
		text = name
	}

	core.KeyTracef("1 sdl      press   key=%q (%s)", k.chord, k.origin)
	if k.held {
		p.holdKey(k.scancode, k.chord)
	}
	s.handler.Event(core.KeyPressEvent{
		Key: k.chord, Modifiers: mods, Text: text, Repeat: k.repeat,
	})
}
