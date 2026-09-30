package core

// KeyParts reads a key the way the macOS rendering does: every modifier
// prefix, the caret for Control, and Shift where an uppercase letter implies
// it -- except after the caret, where case says nothing.

import "testing"

func TestAKeySplitsIntoModifiersAndName(t *testing.T) {
	for _, tc := range []struct {
		key  string
		mods KeyModifiers
		name string
	}{
		{"s-c", SuperModifier, "c"},
		{"s-C", SuperModifier | ShiftModifier, "C"},
		{"^X", ControlModifier, "X"},
		{"C-M-x", ControlModifier | MegaModifier, "x"},
		{"M-S-F4", MegaModifier | ShiftModifier, "F4"},
		{"m-H-Left", MicroModifier | HyperModifier, "Left"},
		{"Return", 0, "Return"},
		{"A", ShiftModifier, "A"},
		{"M-", 0, "M-"},
	} {
		mods, name := KeyParts(tc.key)
		if mods != tc.mods || name != tc.name {
			t.Errorf("%q split into (%v, %q), want (%v, %q)", tc.key, mods, name, tc.mods, tc.name)
		}
	}
}
