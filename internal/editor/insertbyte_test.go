package editor

import (
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// The point of this command is that the character CANNOT be typed, so the
// value is whatever the user already has in their head: a key they would
// press, a name they know it by, or a number in whichever base it came in.
func TestParseByteSpecAcceptsEveryNotation(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want rune
	}{
		// Caret notation - the key you would actually press.
		{"^[", 0x1B}, {"^A", 0x01}, {"^a", 0x01}, {"^@", 0x00}, {"^_", 0x1F},
		{"^?", 0x7F}, // DEL by convention, NOT 0x1F
		// Conventional names, case-insensitive.
		{"ESC", 0x1B}, {"esc", 0x1B}, {"NUL", 0x00}, {"BEL", 0x07},
		{"TAB", 0x09}, {"HT", 0x09}, {"LF", 0x0A}, {"CR", 0x0D},
		{"DEL", 0x7F}, {"SP", 0x20}, {"XOFF", 0x13},
		// Explicit radix prefixes.
		{"x1b", 0x1B}, {"0x1b", 0x1B}, {"$1b", 0x1B}, {"X1B", 0x1B},
		{"o33", 0x1B}, {"0o33", 0x1B},
		{"b11011", 0x1B}, {"0b11011", 0x1B},
		{"#27", 0x1B},
		// Bare is hex: the convention for a byte.
		{"1b", 0x1B}, {"ff", 0xFF}, {"00", 0x00}, {"7f", 0x7F},
		// A leading "b" is a hex digit when what follows is not binary.
		{"be", 0xBE}, {"bd", 0xBD},
		// "d" is a hex digit, not a decimal prefix.
		{"d", 0x0D}, {"dd", 0xDD},
		// Hex beats the name table for the one name that is also valid hex.
		{"ff", 0xFF}, {"FF", 0xFF},
	} {
		got, ok := parseByteSpec(tc.in)
		if !ok {
			t.Errorf("parseByteSpec(%q) rejected it", tc.in)
			continue
		}
		if got != tc.want {
			t.Errorf("parseByteSpec(%q) = %#x, want %#x", tc.in, got, tc.want)
		}
	}
}

// Out of range, malformed, or not a byte at all.
func TestParseByteSpecRejects(t *testing.T) {
	for _, in := range []string{
		"", "   ", "100", // 0x100 is 256: past a byte
		"#256", "o400", "b100000000",
		"zz", "^", "^^^", "x", "o",
		"^é",          // not a C0 control
		"NOTACONTROL", // not a name
		"-1", "#-1",   // negative
	} {
		if got, ok := parseByteSpec(in); ok {
			t.Errorf("parseByteSpec(%q) = %#x, want rejection", in, got)
		}
	}
}

// The documented ambiguity, pinned so it cannot drift: a leading "b" followed
// by only 0s and 1s is BINARY. "xb0" is how you write the hex one.
func TestParseByteSpecBinaryPrefixBeatsHexDigit(t *testing.T) {
	if got, _ := parseByteSpec("b0"); got != 0 {
		t.Errorf(`parseByteSpec("b0") = %#x, want 0 (binary)`, got)
	}
	if got, _ := parseByteSpec("xb0"); got != 0xB0 {
		t.Errorf(`parseByteSpec("xb0") = %#x, want 0xB0 (hex)`, got)
	}
}

// Code points are spelled in hex wherever they appear, so that is the default;
// surrogates are not scalars and Go would silently swap in U+FFFD for one.
func TestParseCodePoint(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want rune
	}{
		{"05D0", 0x5D0}, {"U+05D0", 0x5D0}, {"u+05d0", 0x5D0}, {"0x5d0", 0x5D0},
		{"#1488", 0x5D0},
		{"200E", 0x200E}, {"10FFFF", 0x10FFFF},
	} {
		got, ok := parseCodePoint(tc.in)
		if !ok || got != tc.want {
			t.Errorf("parseCodePoint(%q) = %#x,%v want %#x", tc.in, got, ok, tc.want)
		}
	}
	for _, in := range []string{"", "110000", "D800", "DFFF", "U+D800", "zz", "-1"} {
		if got, ok := parseCodePoint(in); ok {
			t.Errorf("parseCodePoint(%q) = %#x, want rejection", in, got)
		}
	}
}

// Both commands insert exactly one scalar and refuse a read-only buffer.
func TestInsertRuneAndByteCommands(t *testing.T) {
	e, w := newTestEditor(t, "ab\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 1})

	e.executeCommand(`insert_rune "05D0"`)
	if got := docContent(w); got != "aאb" {
		t.Errorf("after insert_rune: %q, want %q", got, "aאb")
	}

	e.executeCommand(`insert_raw_byte "^["`)
	if got := docContent(w); got != "aא\x1bb" {
		t.Errorf("after insert_raw_byte: %q", got)
	}

	// A bad value warns and changes nothing.
	before := docContent(w)
	e.executeCommand(`insert_raw_byte "zz"`)
	if got := docContent(w); got != before {
		t.Errorf("a rejected byte must not edit the buffer: %q", got)
	}
}
