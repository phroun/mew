package input

import "testing"

// The spacebar arrives as its literal character " "; it must normalize to the
// "space" token so bindings that spell it out (e.g. "^B space") can match.
// Regression: a missing mapping left it as " ", so space bindings never fired
// while typing a space still worked (it fell through to the default insert).
func TestNormalizeKeySpace(t *testing.T) {
	cases := map[string]string{
		" ":     "space",   // bare spacebar
		"M- ":   "M-space", // Alt+Space, via the prefix logic
		"S- ":   "S-space", // Shift+Space
		"Tab":   "tab",     // unchanged: a named special still maps
		"Enter": "return",
		"a":     "a", // a plain printable is left alone
		"^A":    "^A",
	}
	for in, want := range cases {
		if got := normalizeKey(in); got != want {
			t.Errorf("normalizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}
