package input

import (
	"testing"

	"github.com/phroun/direct-key-handler/keyboard"
)

// Every key the handler can name must have a mew name. A key left out arrives
// under the handler's own spelling — "PageUp" rather than "pgup" — which no
// binding matches and nothing reports; before the vocabulary was declared this
// way, the keypad's Enter reached mew as the literal "Return" for exactly that
// reason. This test is the thing that notices.
func TestMewNamesCoverEveryKey(t *testing.T) {
	for _, k := range keyboard.AllKeys() {
		if _, ok := mewKeyNames[k]; !ok {
			t.Errorf("no mew name for %v (would arrive as %q)", k, k.DefaultName())
		}
	}
}

// mew's names are binding-syntax tokens: a sequence like "^B space" is split
// on spaces, so a name containing one could never be matched.
func TestMewNamesAreBindingTokens(t *testing.T) {
	for k, name := range mewKeyNames {
		if name == "" {
			t.Errorf("%v has an empty name", k)
		}
		for _, r := range name {
			if r == ' ' || r == '\t' {
				t.Errorf("%v is named %q, which cannot be a token in a key sequence", k, name)
				break
			}
		}
	}
}

// A host parses its own key events and feeds them in under
// direct-key-handler's naming, so the feed still translates — unlike the
// terminal path, where the handler is given mew's names and emits them
// directly. Both spellings of the two Enter keys have to land on "return".
func TestNormalizeHostKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Escape", "esc"},
		{"PageUp", "pgup"},
		{"Delete", "fdel"},
		{"Backspace", "back"},
		{"Return", "return"}, // the home-row key
		{"Enter", "return"},  // the keypad's, folded onto the same binding
		{" ", "space"},       // a host may send the literal character
		{"Space", "space"},   // ...or the name
		{"S-PageUp", "S-pgup"},
		{"M-Left", "M-left"},
		{"C-S-Home", "C-S-home"},
		{"F1", "F1"},   // unchanged: mew spells it the same way
		{"a", "a"},     // printable, no entry
		{"^K", "^K"},   // control chord, no entry
		{"G-€", "G-€"}, // Glyph chord carries its own character
	}
	for _, c := range cases {
		if got := normalizeHostKey(c.in); got != c.want {
			t.Errorf("normalizeHostKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The home-row key and the keypad's are deliberately folded onto one binding
// name — stated here rather than left to chance.
func TestReturnAndKeypadEnterFold(t *testing.T) {
	if got := mewKeyNames[keyboard.KeyReturn]; got != "return" {
		t.Errorf("KeyReturn named %q, want %q", got, "return")
	}
	if got := mewKeyNames[keyboard.KeyKeypadEnter]; got != "return" {
		t.Errorf("KeyKeypadEnter named %q, want %q", got, "return")
	}
}
