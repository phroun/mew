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
	mods, name := core.ParseKeyModifiers(k.chord)
	text := k.produced
	if text == "" && len(name) == 1 && name[0] >= 32 && name[0] < 127 {
		// Nothing was produced, so the chord carries what the key itself shows.
		text = name
	}

	// A key that types NOTHING, arriving while a composition is in flight, is
	// the INPUT METHOD'S: Return confirms the candidate, the arrows walk the
	// list, Escape gives it up. macOS goes on delivering those key-downs from
	// behind its candidate window even though it is consuming them itself, and
	// dispatching them put a newline in the document for every Japanese word
	// confirmed — the caret walking down the screen while the composition
	// stayed exactly where it belonged.
	//
	// A key that TYPES is the user's, whatever is in flight, and goes through
	// as it always has: the "." or "/" that dismisses a palette by typing, the
	// digit that picks from one, the space that confirms, and the "u" that
	// completes a dead key's Option+i. That is the whole of the line — text or
	// no text — and it is drawn on `composing`, which is SDL SAYING a
	// composition is up, rather than on the takeover this platform infers for
	// the accent palette (see imeState.armed). An inference that went wrong
	// would swallow every Return from here on.
	if text == "" && p.ime.composing {
		core.KeyTracef("1 sdl      ime     key %q (%s) is the input method's",
			k.chord, k.origin)
		return
	}

	// A keystroke reaching the application usually means the input method no
	// longer has the keyboard: whatever palette was open is gone. Ending it
	// DELETES NOTHING — the letter the held key committed is what the user
	// typed, and a dismissed palette leaves it exactly there.
	//
	// The exception is the palette's own SELECTOR. Picking by number types the
	// digit into the document and backspaces it, so that digit arrives here as
	// an ordinary keystroke; ending the takeover on it threw away the extent
	// before the palette's own accent turned up, and the accent appended:
	// "oœ". A palette takes exactly one selector, so the first typed keystroke
	// is spared and the second is somebody typing on.
	//
	// Confirming with SPACE therefore leaves the space in the document, and
	// that is deliberate rather than a case still to be closed: macOS erases
	// the digit it types for a numeric pick and does not erase the space, so
	// there is nothing to tell us it was a confirmation rather than a word
	// ending. Keeping it is also what the person meant — a space is pressed to
	// confirm because the word is finished, so the space they get is the space
	// they were about to type. Do not "fix" this back into a swallow.
	//
	// Only a takeover of ours is ended (see cancelComposition), so a dead key's
	// deliberately-standing composition is untouched — the "u" completing
	// Option+i comes through here and must still find the circumflex waiting.
	selector := text != "" && p.ime.noteTyped()
	if !selector {
		if p.ime.armed {
			core.KeyTracef("1 sdl      ime     key %q (%s) ends the composition",
				k.chord, k.origin)
		}
		p.cancelComposition(s)
	}
	if s == nil || s.handler == nil {
		return
	}

	core.KeyTracef("1 sdl      press   key=%q (%s)", k.chord, k.origin)
	if k.held {
		p.holdKey(k.scancode, k.chord)
	}
	s.handler.Event(core.KeyPressEvent{
		Key: k.chord, Modifiers: mods, Text: text, Repeat: k.repeat,
	})

	if selector {
		core.KeyTracef("1 sdl      ime     selector %q kept the composition", k.chord)
	}
}
