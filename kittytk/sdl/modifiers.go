//go:build sdl

package sdl

// One place writes a modifier prefix, and it writes them in one order.
//
// There were six, each assembled by hand at the site that needed it, and they
// did not agree. The keypad branch wrote C- before M-; the special-key branch
// wrote M- before C-; and Hyper was not written by any of them — it was glued
// on in front of the finished string by translateKey, which put it ahead of
// every modifier that outranks it.
//
// Order is not meaning: the sequence processor sorts a stack before comparing,
// so a keymap written either way still matched. But a producer that emits one
// fixed order is the thing that lets a consumer compare strings without knowing
// which producer it is listening to, and two hosts that spell one chord two
// ways cost a day of finding out which. So the spelling lives here now, and
// nowhere else.

// keyMods is the set of modifiers held with a key, as this vocabulary spells
// them. Control is included even where it will be written as a caret instead,
// so a caller that spends it says so by clearing it rather than by omitting the
// field.
type keyMods struct {
	ctrl  bool // C-
	glyph bool // G-  AltGr / ISO_Level3_Shift
	mega  bool // M-
	micro bool // m-  the modifier a Space Cadet keyboard had its own key for
	shift bool // S-
	super bool // s-  Super / Command
	hyper bool // H-
}

// prefix spells a modifier set in the canonical order: C- G- M- m- S- s- H-.
//
// It follows the order macOS renders modifiers, extended with the ones a Mac
// keyboard has no cap for, and it matches modifierRank in the sequence
// processor exactly. What comes AFTER this prefix is the key's own origin and
// then the key: P- or p- for the pad, then the caret if Control is being
// written against a character, then the base — C- G- M- m- S- s- H- P- p- ^Key.
func (m keyMods) prefix() string {
	s := ""
	if m.ctrl {
		s += "C-"
	}
	if m.glyph {
		s += "G-"
	}
	if m.mega {
		s += "M-"
	}
	if m.micro {
		s += "m-"
	}
	if m.shift {
		s += "S-"
	}
	if m.super {
		s += "s-"
	}
	if m.hyper {
		s += "H-"
	}
	return s
}
