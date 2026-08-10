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

// The Hyper prefix (H-) and the other hyphenated modifier prefixes must be
// stripped so a named special key underneath still normalizes to its mew token.
func TestNormalizeKeyModifierPrefixes(t *testing.T) {
	cases := map[string]string{
		"H-x":         "H-x",       // Hyper + plain printable, left alone
		"H-Down":      "H-down",    // Hyper + named special
		"H-M-Down":    "H-M-down",  // Hyper + Meta + named special
		"H-C-PageUp":  "H-C-pgup",  // Hyper + Control-named + special
		"s-Left":      "s-left",    // Super + named special
		"C-Right":     "C-right",   // Control-named + special
		"A-Up":        "A-up",      // Alt-named + special
		"G-Home":      "G-home",    // Glyph (AltGr/Level3) + named special
		"H-^A":        "H-^A",      // Hyper + caret control letter, left alone
	}
	for in, want := range cases {
		if got := normalizeKey(in); got != want {
			t.Errorf("normalizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}
