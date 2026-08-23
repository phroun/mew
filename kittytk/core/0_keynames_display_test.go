package core

import "testing"

// With macOS-native rendering on, DisplayKey maps modifier prefixes to the
// native glyphs ⌃⌥⇧⌘ in canonical order, uppercases a single letter key, and
// leaves named keys alone.
func TestDisplayKeyMacNative(t *testing.T) {
	prev := MacNativeShortcuts()
	SetMacNativeShortcuts(true)
	defer SetMacNativeShortcuts(prev)

	cases := []struct{ in, want string }{
		{"^N", "⌃N"},
		{"^S", "⌃S"},           // caret notation never implies Shift
		{"^S-S", "⌃⇧S"},        // caret control + explicit shift
		{"M-a", "⌥A"},          // Mega -> option; lowercase letter, no Shift
		{"M-A", "⌥⇧A"},         // uppercase after hyphenated modifier implies Shift
		{"s-k", "⌘K"},          // super -> command
		{"C-x", "⌃X"},          // hyphenated control -> control
		{"s-S-M-C-q", "⌃⌥⇧⌘Q"}, // canonical order regardless of input order
		{"Delete", "Delete"},   // named key, no modifiers
		{"^Delete", "⌃Delete"}, // named key with a modifier
		{"F1", "F1"},
		{"", ""},

		// Micro is the other reading of the meta lineage, and a different key
		// from Mega: macOS has no glyph of its own for it, so it borrows the
		// alternative-key symbol and sits where its prefix sits.
		{"m-a", "⎇A"},
		{"M-m-a", "⌥⎇A"},

		// A modifier macOS has no glyph for does not draw, but it must still
		// PARSE -- an unrecognised prefix would otherwise ride along into the
		// key name and print as its own source text.
		{"H-x", "X"},
		{"G-€", "€"},

		// A- was never a modifier this vocabulary has; it is a key name that
		// happens to look like one.
		{"A-a", "A-a"},
	}
	for _, c := range cases {
		if got := DisplayKey(c.in); got != c.want {
			t.Errorf("DisplayKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// With native rendering off, DisplayKey returns the compact notation
// unchanged.
func TestDisplayKeyPlain(t *testing.T) {
	prev := MacNativeShortcuts()
	SetMacNativeShortcuts(false)
	defer SetMacNativeShortcuts(prev)

	for _, key := range []string{"^N", "M-a", "m-a", "^S-S", "Delete", ""} {
		if got := DisplayKey(key); got != key {
			t.Errorf("DisplayKey(%q) = %q, want unchanged", key, got)
		}
	}
}

// A Shortcut is a key string like any other, and its method is the same
// answer by another name.
func TestShortcutDisplayStringDelegates(t *testing.T) {
	prev := MacNativeShortcuts()
	SetMacNativeShortcuts(true)
	defer SetMacNativeShortcuts(prev)

	for _, key := range []string{"^N", "M-A", "s-k", "Delete", ""} {
		if got, want := Shortcut(key).DisplayString(), DisplayKey(key); got != want {
			t.Errorf("Shortcut(%q).DisplayString() = %q, want %q", key, got, want)
		}
	}
}
