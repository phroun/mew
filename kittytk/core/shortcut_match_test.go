package core

import "testing"

// Control shortcuts match regardless of which of the two accepted
// spellings (caret "^X" or prefix "C-x") the producer used.
func TestShortcutMatchesBothControlSpellings(t *testing.T) {
	cases := []struct {
		shortcut string
		key      string
		want     bool
	}{
		{"^\\", "^\\", true},   // exact caret form (TUI backend, byte 0x1C)
		{"^\\", "C-\\", true},  // prefix form (SDL fallback path)
		{"C-\\", "^\\", true},  // reversed declaration
		{"^H", "C-h", true},    // letter case folds under control
		{"C-h", "^H", true},
		{"^]", "C-]", true},
		{"C-Up", "C-Up", true}, // named keys stay in prefix form
		{"M-^X", "M-C-x", true},
		{"^\\", "C-]", false},
		{"^\\", "\\", false}, // plain key is not the control chord
		{"C-Up", "Up", false},
		{"", "^\\", false},
	}
	for _, c := range cases {
		got := Shortcut(c.shortcut).Matches(KeyPressEvent{Key: c.key})
		if got != c.want {
			t.Errorf("Shortcut(%q).Matches(%q) = %v, want %v", c.shortcut, c.key, got, c.want)
		}
	}
}
