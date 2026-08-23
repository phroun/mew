package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The home-row key and the keypad's reach a child as two different things.
//
// This replaces a rename that used to sit in front of the encoder: it knew
// only the keypad's "Enter", and its last resort for an unknown name is to
// send the name's LETTERS, so an untranslated "Return" typed the word at the
// child — which is what a mew editor hosted in one actually showed. purfecterm
// v0.2.48 knows both names and encodes them apart, so the rename would now do
// the opposite harm and hand the home-row key the keypad's sequence.
//
// Asserting bytes rather than the name is the point. A name test could pass
// with the pair mapped either way round; only the bytes say which key the
// child was told about.
func TestBothEnterKeysReachTheChildApart(t *testing.T) {
	for _, c := range []struct{ key, want, what string }{
		{"Return", "\r", "home-row key sends CR"},
		{"Enter", "\x1bOM", "keypad's sends SS3 M, as application keypad mode does"},
	} {
		term := NewPurfecTerm()
		if term.Terminal() == nil {
			t.Skip("terminal unavailable")
		}
		term.SetBounds(core.UnitRect{Width: 640, Height: 400})
		var sink []byte
		term.SetInputSink(func(b []byte) { sink = append(sink, b...) })
		term.Terminal().SetFocused(true)
		term.Terminal().HandleKeyString(c.key)

		if got := string(sink); got != c.want {
			t.Errorf("%s: %q -> %q, want %q", c.what, c.key, got, c.want)
		}
		// The failure this file was created for: an unknown name arrives at the
		// child as its own letters, which looks like typing rather than an error.
		if string(sink) == c.key {
			t.Errorf("%q was typed at the child as text; the encoder does not "+
				"know the name", c.key)
		}
	}
}
