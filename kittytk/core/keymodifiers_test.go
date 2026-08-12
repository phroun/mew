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
		{"M-x", MegaMetaModifier, "x"},
		{"s-x", MetaModifier, "x"},
		{"m-x", MicroMetaModifier, "x"},
		{"H-x", HyperModifier, "x"},
		{"G-€", GlyphModifier, "€"},

		// The caret is Control too, and hugs the base key.
		{"^A", ControlModifier, "A"},
		{"S-^A", ShiftModifier | ControlModifier, "A"},
		{"M-^A", MegaMetaModifier | ControlModifier, "A"},

		// Stacks, in canonical order.
		{"C-S-Up", ControlModifier | ShiftModifier, "Up"},
		{"M-S-s-Left", MegaMetaModifier | ShiftModifier | MetaModifier, "Left"},
		{"M-m-S-s-H-^A", MegaMetaModifier | MicroMetaModifier | ShiftModifier |
			MetaModifier | HyperModifier | ControlModifier, "A"},
	} {
		mods, name := ParseKeyModifiers(c.key)
		if mods != c.mods || name != c.name {
			t.Errorf("ParseKeyModifiers(%q) = (%d, %q), want (%d, %q)",
				c.key, mods, name, c.mods, c.name)
		}
	}
}

// Three constants carry the word "Meta" and each needs its own bit. "M-" is
// kitty's alt (MegaMetaModifier) and "m-" is kitty's meta (MicroMetaModifier)
// — two X11 keysyms that have always produced the same bytes, so neither is
// more genuinely Meta and both are named for the case of their prefix. "s-" is
// Super/Command (MetaModifier), which is not on that scale at all and is only
// called Meta because this toolkit always has.
func TestParseKeyModifiersKeepsTheMetasApart(t *testing.T) {
	mega, _ := ParseKeyModifiers("M-x")
	micro, _ := ParseKeyModifiers("m-x")
	super, _ := ParseKeyModifiers("s-x")
	if mega == micro {
		t.Error("M- and m- set the same bit; they are different keysyms")
	}
	if super == micro {
		t.Error("s- and m- set the same bit; Super/Command is not the Meta key")
	}
	if mega&micro != 0 || super&micro != 0 || mega&super != 0 {
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
