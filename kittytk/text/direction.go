package text

import (
	xbidi "golang.org/x/text/unicode/bidi"

	"github.com/phroun/kittytk/core"
)

// FirstStrongDirection is UAX #9 rules P2/P3 read as a question about a
// string: the first strongly directional character in it names the direction.
//
// A string with none -- digits, punctuation, a symbol, nothing at all --
// returns core.DirInherit rather than a guess, which is the caller's cue to
// take the direction from around it. That is the answer for a great many
// captions, so it is a reported result and not a failure.
//
// This is what a trinket answers core.TextDirectioner with. It says which way
// the text RUNS, for stating logical alignments against; how the text is
// shaped and painted is the paragraph path's own business and is decided
// there.
func FirstStrongDirection(s string) core.Direction {
	for _, r := range s {
		props, _ := xbidi.LookupRune(r)
		switch props.Class() {
		case xbidi.L:
			return core.DirLTR
		case xbidi.R, xbidi.AL:
			return core.DirRTL
		}
	}
	return core.DirInherit
}
