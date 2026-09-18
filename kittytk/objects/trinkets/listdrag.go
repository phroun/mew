package trinkets

// Asking, while the reader is moving.
//
// A drag is a new place every frame. With the rows here that is nothing -- the
// answer arrives inside the call -- but with the rows somewhere that has to be
// asked, a question per frame is a hundred questions for a gesture, and by the
// time the ninety-ninth is answered nobody is looking at where it went.
//
// So there is at most ONE ask outstanding. Each one supersedes the last, and an
// answer that arrives for a place the reader has since left is dropped rather
// than written down -- which is what keeps the spine holding where the reader IS
// and not where it was passing through. Between asking and being answered the
// rows are blank, and blank rows are what make the gesture smooth.
//
// Nothing here is timed. A frame is the clock: one ask per paint, superseded by
// the next, which is the same rate the reader is generating positions at and
// needs no interval to be chosen or tuned.

import "github.com/phroun/serval"

// asking is which ask is current. An answer carrying an older number is an
// answer to a question nobody is waiting for.
//
// It is only ever compared for equality, never ordered against anything, so it
// wrapping round would cost one dropped answer in four thousand million.
type asking uint64

// Length is how long the list's sequence is, and how well that is known.
//
// Three answers, and they are three different things rather than three guesses.
// Exactly is a sequence counted, and a thumb drawn against it is true. AtLeast is
// a floor -- part of a sequence seen is at least that many -- and a thumb drawn
// against it is honest but will shrink as more is learned. Unknown is a source
// that would not say, and there is no scale to draw at all.
func (l *ListView) Length() serval.RecordCount {
	l.Count() // which is what learns it
	return l.bones.length()
}

// asks for a stretch, superseding whatever was asked before.
//
// The count does not move where the stretch is one the list already holds: there
// is nothing outstanding, so nothing to supersede, and bumping it would discard
// an answer still on its way to somewhere the reader has NOT left.
func (l *ListView) ask(at, n int) {
	if l.spineHolds(at, n) {
		return
	}
	l.asks++
	l.window(at, n)
}

// current reports whether an answer is still the one being waited for.
func (l *ListView) current(a asking) bool { return a == l.asks }

// thumbFloor reports whether the thumb must be kept off the bottom of its track.
//
// It must where the length is not exact. A thumb resting at the foot of its
// track says *this is the end of the sequence*, and a floor cannot say that: there
// is more below than anybody has counted. Keeping it a row short is a small true
// signal in place of a confident false one.
func (l *ListView) thumbFloor() bool {
	return !l.bones.length().Exact
}
