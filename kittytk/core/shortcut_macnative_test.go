package core

import "testing"

// With macOS-native rendering on, DisplayString maps modifier prefixes to the
// native glyphs ⌃⌥⇧⌘ in canonical order, uppercases a single letter key, and
// leaves named keys alone.
func TestMacNativeDisplayString(t *testing.T) {
	prev := MacNativeShortcuts()
	SetMacNativeShortcuts(true)
	defer SetMacNativeShortcuts(prev)

	cases := []struct {
		in   Shortcut
		want string
	}{
		{"^N", "⌃N"},
		{"^S", "⌃S"},           // caret notation never implies Shift
		{"^S-S", "⌃⇧S"},        // caret control + explicit shift
		{"M-a", "⌥A"},          // meta/alt -> option; lowercase letter, no Shift
		{"M-A", "⌥⇧A"},         // uppercase after hyphenated modifier implies Shift
		{"A-a", "⌥A"},          // alt -> option too
		{"s-k", "⌘K"},          // super -> command
		{"C-x", "⌃X"},          // hyphenated control -> control
		{"s-S-M-C-q", "⌃⌥⇧⌘Q"}, // canonical order regardless of input order
		{"Delete", "Delete"},   // named key, no modifiers
		{"^Delete", "⌃Delete"}, // named key with a modifier
		{"F1", "F1"},
		{"", ""},
	}
	for _, c := range cases {
		if got := c.in.DisplayString(); got != c.want {
			t.Errorf("DisplayString(%q) = %q, want %q", string(c.in), got, c.want)
		}
	}
}

// With native rendering off, DisplayString returns the compact notation
// unchanged.
func TestMacNativeDisplayStringDisabled(t *testing.T) {
	prev := MacNativeShortcuts()
	SetMacNativeShortcuts(false)
	defer SetMacNativeShortcuts(prev)

	for _, s := range []Shortcut{"^N", "M-a", "^S-S", "Delete"} {
		if got := s.DisplayString(); got != string(s) {
			t.Errorf("DisplayString(%q) = %q, want unchanged", string(s), got)
		}
	}
}
