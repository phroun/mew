package core

import "testing"

func TestSpeakKeySpellsPunctuation(t *testing.T) {
	cases := []struct{ key, want string }{
		{"^\\", "Control Backslash"},
		{"C-\\", "Control Backslash"},
		{"^/", "Control Slash"},
		{"M-[", "Meta Left Bracket"},
		{"^X", "Control X"},    // letters unchanged
		{"M-F10", "Meta F10"},  // named keys unchanged
		{"S-Tab", "Shift Tab"}, // named keys unchanged
		{"", ""},

		// A bare caret is the character, not Control of nothing.
		{"^", "Caret"},
	}
	for _, c := range cases {
		if got := SpeakKey(c.key); got != c.want {
			t.Errorf("SpeakKey(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

// Every modifier in the vocabulary has a word, and they are spoken in
// canonical order however the key was written. A prefix with no word here is
// worse than unspoken: it stays part of the key name and gets read out as its
// own source text.
func TestSpeakKeySpellsEveryModifier(t *testing.T) {
	cases := []struct{ key, want string }{
		{"C-x", "Control x"},
		{"G-x", "Glyph x"},
		{"M-x", "Meta x"},
		{"m-x", "Micro x"}, // its own name: the one it would claim is taken
		{"S-x", "Shift x"},
		{"s-x", "Super x"},
		{"H-x", "Hyper x"},
		{"s-C-Left", "Control Super Left"}, // canonical order, not written order
		{"M-O", "Meta Shift O"},            // uppercase implies Shift...
		{"^O", "Control O"},                // ...but never after a caret
	}
	for _, c := range cases {
		if got := SpeakKey(c.key); got != c.want {
			t.Errorf("SpeakKey(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

// A Shortcut is a key string like any other, and its method is the same
// answer by another name.
func TestShortcutAccessibilityStringDelegates(t *testing.T) {
	for _, key := range []string{"^\\", "M-[", "S-Tab", ""} {
		if got, want := Shortcut(key).AccessibilityString(), SpeakKey(key); got != want {
			t.Errorf("Shortcut(%q).AccessibilityString() = %q, want %q", key, got, want)
		}
	}
}
