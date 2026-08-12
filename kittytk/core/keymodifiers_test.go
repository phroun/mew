package core

import "testing"

// The parser knew four prefixes and none of the ones the input layer learned to
// emit, so a Hyper, Meta or Glyph chord came back with no modifiers set at all
// — the key name kept its prefix and the bitfield silently said "unmodified".
func TestParseKeyModifiersKnowsEveryPrefix(t *testing.T) {
	for _, c := range []struct {
		key  string
		mods KeyModifiers
		name string
	}{
		{"x", 0, "x"},
		{"S-Tab", ShiftModifier, "Tab"},
		{"C-Up", ControlModifier, "Up"},
		{"M-x", AltModifier, "x"},
		{"s-x", MetaModifier, "x"},
		{"m-x", MetaProperModifier, "x"},
		{"H-x", HyperModifier, "x"},
		{"G-€", GlyphModifier, "€"},

		// The caret is Control too, and hugs the base key.
		{"^A", ControlModifier, "A"},
		{"S-^A", ShiftModifier | ControlModifier, "A"},
		{"M-^A", AltModifier | ControlModifier, "A"},

		// Stacks, in canonical order.
		{"C-S-Up", ControlModifier | ShiftModifier, "Up"},
		{"M-S-s-Left", AltModifier | ShiftModifier | MetaModifier, "Left"},
		{"M-m-S-s-H-^A", AltModifier | MetaProperModifier | ShiftModifier |
			MetaModifier | HyperModifier | ControlModifier, "A"},
	} {
		mods, name := ParseKeyModifiers(c.key)
		if mods != c.mods || name != c.name {
			t.Errorf("ParseKeyModifiers(%q) = (%d, %q), want (%d, %q)",
				c.key, mods, name, c.mods, c.name)
		}
	}
}

// Three prefixes crowd around the word "meta" and each needs its own bit:
// "M-" is the Meta a PC's Alt key induces (AltModifier), "s-" is Super/Command
// (MetaModifier, which is what this toolkit has always called it), and "m-" is
// the separate Meta key a terminal can report on its own bit
// (MetaProperModifier).
func TestParseKeyModifiersKeepsTheMetasApart(t *testing.T) {
	alt, _ := ParseKeyModifiers("M-x")
	metaProper, _ := ParseKeyModifiers("m-x")
	super, _ := ParseKeyModifiers("s-x")
	if alt == metaProper {
		t.Error("M- and m- set the same bit; they are different modifiers")
	}
	if super == metaProper {
		t.Error("s- and m- set the same bit; Super/Command is not the Meta key")
	}
	if alt&metaProper != 0 || super&metaProper != 0 || alt&super != 0 {
		t.Error("the three overlap; each modifier needs its own bit")
	}
}

// A bare caret is the character, not Control-of-nothing, and a lone modifier
// prefix is not a modifier either — both have to survive as key names.
func TestParseKeyModifiersLeavesBareTokensAlone(t *testing.T) {
	for _, key := range []string{"^", "-", "M-", "s-"} {
		mods, name := ParseKeyModifiers(key)
		if mods != 0 || name != key {
			t.Errorf("ParseKeyModifiers(%q) = (%d, %q), want (0, %q)", key, mods, name, key)
		}
	}
}
