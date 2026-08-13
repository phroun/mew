package core

import "testing"

// Control shortcuts match regardless of which of the two accepted
// spellings (caret "^X" or prefix "C-x") the producer used.
func TestSameKeyAcceptsBothControlSpellings(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"^\\", "^\\", true},  // exact caret form (TUI backend, byte 0x1C)
		{"^\\", "C-\\", true}, // prefix form (SDL fallback path)
		{"C-\\", "^\\", true}, // reversed declaration
		{"^H", "C-h", true},   // letter case folds under control
		{"C-h", "^H", true},
		{"^]", "C-]", true},
		{"C-Up", "C-Up", true}, // named keys stay in prefix form
		{"M-^X", "M-C-x", true},
		{"^\\", "C-]", false},
		{"^\\", "\\", false}, // plain key is not the control chord
		{"C-Up", "Up", false},
		{"", "^\\", false},
		{"", "", false}, // nothing is not a key, so it matches nothing
	}
	for _, c := range cases {
		if got := SameKey(c.a, c.b); got != c.want {
			t.Errorf("SameKey(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// Modifiers are a SET, so the order they were written in does not make two
// spellings of one keystroke disagree. The old string-canonicalizer kept them
// in encounter order and said these were different keys.
func TestSameKeyIgnoresModifierOrder(t *testing.T) {
	for _, c := range []struct{ a, b string }{
		{"M-S-Tab", "S-M-Tab"},
		{"s-S-M-C-q", "C-M-S-s-q"},
		{"M-^X", "^M-x"},
	} {
		if !SameKey(c.a, c.b) {
			t.Errorf("SameKey(%q, %q) = false, want true - same modifiers, written in another order", c.a, c.b)
		}
	}
}

// Case folds under Control and nowhere else: M-a and M-A are two bindings
// told apart by Shift, and a modifier the other side does not carry is a
// different keystroke however it is spelled.
func TestSameKeyKeepsDistinctKeysApart(t *testing.T) {
	for _, c := range []struct{ a, b string }{
		{"M-a", "M-A"},
		{"m-x", "M-x"}, // Micro and Mega are different keys
		{"H-x", "x"},
		{"G-x", "S-x"},
		{"^A", "^B"},
	} {
		if SameKey(c.a, c.b) {
			t.Errorf("SameKey(%q, %q) = true, want false", c.a, c.b)
		}
	}
}
