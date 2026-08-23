//go:build mew

package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The two erase keys must reach a guest as a terminal sends them, and as two
// DIFFERENT keys.
//
// mew's capture map used to name both and send a hardcoded ^H for each, which
// is what Ctrl-H sends rather than what either key does — and one byte for two
// keys leaves a guest unable to tell them apart at all. readline and vim
// forgive it, because terminfo often maps erase to ^H; a guest that maps
// terminal input to real key events reads it as Ctrl-H and ignores it, which
// is how a browser in a pane ends up with no Backspace. This is the encoding
// the wildcard uses instead.
func TestEraseKeysEncodeAsATerminalSendsThem(t *testing.T) {
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	term.SetBounds(core.UnitRect{Width: 640, Height: 400})
	var sink []byte
	term.SetInputSink(func(b []byte) { sink = append(sink, b...) })
	term.Terminal().SetFocused(true)

	for _, c := range []struct {
		mewKey string
		want   string
		what   string
	}{
		{"back", "\x7f", "Backspace sends DEL"},
		// del is mew's own name for backspace — its default binding is
		// del_char_prior, the same as back — so it encodes the same way.
		{"del", "\x7f", "del is backspace in mew's vocabulary"},
		{"fdel", "\x1b[3~", "forward Delete sends CSI 3 ~"},
	} {
		sink = nil
		term.Terminal().HandleKeyString(dkhKeyName(c.mewKey))
		if got := string(sink); got != c.want {
			t.Errorf("%s: %q -> %q, want %q", c.what, c.mewKey, got, c.want)
		}
		if string(sink) == "\x08" {
			t.Errorf("%s: still sending ^H, which a guest reads as Ctrl-H", c.what)
		}
	}

	// And the two are distinguishable, which one hardcoded byte made impossible.
	sink = nil
	term.Terminal().HandleKeyString(dkhKeyName("back"))
	back := string(sink)
	sink = nil
	term.Terminal().HandleKeyString(dkhKeyName("fdel"))
	if back == string(sink) {
		t.Error("backspace and forward delete send the same bytes; a guest cannot " +
			"tell them apart, which is what one hardcoded byte for both did")
	}
}
