package core

import "strings"

// A key is a STRING, and everything that reads one reads the same vocabulary.
//
// The spellings are compact because they are typed into keymaps by hand: C-
// G- M- m- S- s- H- and the caret form ^X, in that canonical order. Only two
// things are ever done with one: it is parsed into modifiers plus a base key
// name, or it is rendered for a human — on screen, or aloud to a screen
// reader. All three live here, over one table, because the failure mode of
// having several is a prefix that one of them has never heard of: it stops
// being a modifier and silently becomes part of the key name, so a chord comes
// back "unmodified" or prints as its own raw source text.

// keyModifierPrefixes is the modifier vocabulary in canonical spelling order,
// with what each one means, how macOS draws it, and how it is spoken.
//
// A prefix only counts when something follows it, so a key literally named
// "M-" is that key rather than a modifier with nothing modified.
//
// Mega and Micro (M- and m-) are told apart by the CASE of the prefix, so
// every test here is case-sensitive. M- is spoken "Meta", the name Emacs and
// every PC keyboard give it; m- is spoken "Micro", its own constant's name,
// because the name it would otherwise claim is taken. See KeyModifiers for why
// both keys have a real claim to it.
var keyModifierPrefixes = []struct {
	prefix string
	mod    KeyModifiers
	glyph  string // how macOS draws it; empty when macOS has no glyph for it
	spoken string
}{
	{"C-", ControlModifier, "⌃", "Control"},
	{"G-", GlyphModifier, "", "Glyph"},
	{"M-", MegaModifier, "⌥", "Meta"},
	{"m-", MicroModifier, "⎇", "Micro"},
	{"S-", ShiftModifier, "⇧", "Shift"},
	{"s-", SuperModifier, "⌘", "Super"},
	{"H-", HyperModifier, "", "Hyper"},
}

// parseKeyName peels every modifier prefix off a key string, returning the
// modifiers, whether Control was written in CARET form, and the bare key name.
//
// The caret matters to display and to nothing else: a hyphenated modifier
// followed by an uppercase letter means Shift (M-A is Option+Shift+A), while
// caret notation says nothing about case at all (^X is Control+X, not
// Control+Shift+X).
func parseKeyName(key string) (mods KeyModifiers, caret bool, name string) {
	name = key
	for {
		matched := false
		for _, p := range keyModifierPrefixes {
			if len(name) > len(p.prefix) && strings.HasPrefix(name, p.prefix) {
				mods |= p.mod
				name = name[len(p.prefix):]
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		if len(name) > 1 && name[0] == '^' {
			mods |= ControlModifier
			caret = true
			name = name[1:]
			continue
		}
		return mods, caret, name
	}
}

// DisplayKey returns what a key spelling should look like in a menu or a
// tooltip: the compact notation unchanged, or — when macOS-native rendering is
// on — the native modifier glyphs.
func DisplayKey(key string) string {
	if key == "" {
		return ""
	}
	if macNativeShortcuts {
		return macNativeKeyDisplay(key)
	}
	// The key handler format is already compact and readable.
	return key
}

// macNativeKeyDisplay renders a key with macOS modifier glyphs in canonical
// order — ⌃⌥⇧⌘, which the vocabulary's own order already produces — followed
// by the key, with no separators. Micro joins them as ⎇, between Option and
// Shift, since macOS has no glyph of its own for it.
//
// A single uppercase letter after a hyphenated modifier implies Shift, matching
// the notation elsewhere (M-a = Option+A, M-A = Option+Shift+A); caret notation
// (^X) never implies Shift. The letter key is uppercased to match how macOS
// menus present keys (⌘S, not ⌘s). Named keys (Tab, Delete, F1) are passed
// through, and a modifier macOS has no glyph for (Hyper, Glyph) simply does not
// draw.
func macNativeKeyDisplay(key string) string {
	mods, caret, name := parseKeyName(key)

	if len(name) == 1 && name[0] >= 'A' && name[0] <= 'Z' && !caret {
		// Uppercase letter after a hyphenated modifier implies Shift.
		mods |= ShiftModifier
	}
	if len(name) == 1 && name[0] >= 'a' && name[0] <= 'z' {
		// macOS menus present letter keys uppercased.
		name = strings.ToUpper(name)
	}

	var b strings.Builder
	for _, p := range keyModifierPrefixes {
		if mods&p.mod != 0 {
			b.WriteString(p.glyph)
		}
	}
	b.WriteString(name)
	return b.String()
}

// spokenKeyNames maps punctuation and whitespace keys to words a speech
// engine can pronounce, so shortcuts like ^\ announce as "Control
// Backslash" rather than a silent or literal glyph.
var spokenKeyNames = map[string]string{
	"\\": "Backslash",
	"/":  "Slash",
	"`":  "Backtick",
	"~":  "Tilde",
	"!":  "Exclamation",
	"@":  "At Sign",
	"#":  "Number Sign",
	"$":  "Dollar Sign",
	"%":  "Percent",
	"^":  "Caret",
	"&":  "Ampersand",
	"*":  "Asterisk",
	"(":  "Left Paren",
	")":  "Right Paren",
	"-":  "Minus",
	"_":  "Underscore",
	"=":  "Equals",
	"+":  "Plus",
	"[":  "Left Bracket",
	"]":  "Right Bracket",
	"{":  "Left Brace",
	"}":  "Right Brace",
	";":  "Semicolon",
	":":  "Colon",
	"'":  "Apostrophe",
	"\"": "Quote",
	",":  "Comma",
	".":  "Period",
	"<":  "Less Than",
	">":  "Greater Than",
	"?":  "Question Mark",
	"|":  "Pipe",
	" ":  "Space",
}

// SpeakKey returns a key spelling fully spelled out for a screen reader:
// modifiers as words in canonical order, then the key, with punctuation named
// rather than left as a glyph a speech engine would swallow.
//
// An uppercase final letter implies Shift for hyphenated modifiers (M-O is
// "Meta Shift O") but not for caret notation (^X is "Control X" — case carries
// no meaning there).
func SpeakKey(key string) string {
	if key == "" {
		return ""
	}
	mods, caret, name := parseKeyName(key)

	if len(name) == 1 && name[0] >= 'A' && name[0] <= 'Z' && !caret {
		mods |= ShiftModifier
	}
	if spoken, ok := spokenKeyNames[name]; ok {
		name = spoken
	}

	var words []string
	for _, p := range keyModifierPrefixes {
		if mods&p.mod != 0 {
			words = append(words, p.spoken)
		}
	}
	if len(words) == 0 {
		return name
	}
	return strings.Join(words, " ") + " " + name
}
