package trinkets

import "testing"

// The terminal encoder predates the home-row/keypad split and knows only the
// keypad's "Enter". Its last resort for a name it does not know is to send the
// name's LETTERS, so an untranslated "Return" typed the word into the child --
// which is exactly what a mew editor hosted in one showed.
func TestTerminalKeyNameRenamesTheHomeRowReturn(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Return", "Enter"},
		{"S-Return", "S-Enter"},
		{"C-Return", "C-Enter"},
		{"M-S-Return", "M-S-Enter"},
		// Already the encoder's own vocabulary, or nothing to do with it.
		{"Enter", "Enter"},
		{"Escape", "Escape"},
		{"^M", "^M"},
		{"a", "a"},
		{"M-Left", "M-Left"},
		{"F5", "F5"},
	} {
		if got := terminalKeyName(c.in); got != c.want {
			t.Errorf("terminalKeyName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
