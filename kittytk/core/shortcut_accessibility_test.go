package core

import "testing"

func TestAccessibilityStringSpellsPunctuation(t *testing.T) {
	cases := []struct {
		shortcut string
		want     string
	}{
		{"^\\", "Control Backslash"},
		{"C-\\", "Control Backslash"},
		{"^/", "Control Slash"},
		{"M-[", "Meta Left Bracket"},
		{"^X", "Control X"},         // letters unchanged
		{"M-F10", "Meta F10"},       // named keys unchanged
		{"S-Tab", "Shift Tab"},      // named keys unchanged
	}
	for _, c := range cases {
		if got := Shortcut(c.shortcut).AccessibilityString(); got != c.want {
			t.Errorf("AccessibilityString(%q) = %q, want %q", c.shortcut, got, c.want)
		}
	}
}
