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

// Backslash escapes, in the spelling a C, Go, Python or shell programmer
// already has in their fingers. The whole reason to reach for this command is
// that the character cannot be typed, so it should accept the form the user
// already knows rather than making them convert it.
func TestParseByteSpecAcceptsCEscapes(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want rune
	}{
		{`\n`, 0x0A}, {`\r`, 0x0D}, {`\t`, 0x09}, {`\0`, 0x00},
		{`\a`, 0x07}, {`\b`, 0x08}, {`\f`, 0x0C}, {`\v`, 0x0B},
		{`\e`, 0x1B}, // not ISO C, but universal and what people reach for
		{`\\`, 0x5C}, {`\'`, 0x27}, {`\"`, 0x22}, {`\?`, 0x3F},
		{`\x1b`, 0x1B}, {`\xff`, 0xFF}, {`\x0`, 0x00},
		{`\033`, 0x1B}, {`\012`, 0x0A}, {`\177`, 0x7F}, // octal, 1-3 digits
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

// A trailing anything makes it not a single escape, rather than quietly
// inserting the first one and dropping the rest.
func TestParseByteSpecRejectsMalformedEscapes(t *testing.T) {
	for _, in := range []string{`\`, `\z`, `\nn`, `\n\n`, `\x`, `\xzz`, `\0777`, `\400`} {
		if got, ok := parseByteSpec(in); ok {
			t.Errorf("parseByteSpec(%q) = %#x, want rejection", in, got)
		}
	}
}

// A keymap or PawScript argument written "\n" has its escape resolved by the
// parser before this command runs, so the value arrives as a real newline.
// Only CONTROL characters are taken literally — "a" stays a hex digit.
func TestParseByteSpecTakesAlreadyResolvedControlsLiterally(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want rune
	}{{"\n", 0x0A}, {"\t", 0x09}, {"\x1b", 0x1B}, {"\x7f", 0x7F}} {
		got, ok := parseByteSpec(tc.in)
		if !ok || got != tc.want {
			t.Errorf("parseByteSpec(%q) = %#x,%v want %#x", tc.in, got, ok, tc.want)
		}
	}
	// Printable single characters keep their numeric reading.
	if got, _ := parseByteSpec("a"); got != 0x0A {
		t.Errorf(`parseByteSpec("a") = %#x, want 0x0A (hex digit, not the letter)`, got)
	}
}

// \uNNNN is how a code point is written in source, so it works there too.
func TestParseCodePointAcceptsUnicodeEscapes(t *testing.T) {
	bs := string(rune(92)) // a literal backslash, built so no editor can eat it
	for _, tc := range []struct {
		in   string
		want rune
	}{
		{bs + "u05D0", 0x5D0},
		{bs + "u200E", 0x200E},
		{bs + "U0001F600", 0x1F600},
		{bs + "n", 0x0A},
	} {
		got, ok := parseCodePoint(tc.in)
		if !ok || got != tc.want {
			t.Errorf("parseCodePoint(%q) = %#x,%v want %#x", tc.in, got, ok, tc.want)
		}
	}
	// A surrogate stays rejected however it is spelled.
	if got, ok := parseCodePoint(bs + "uD800"); ok {
		t.Errorf("parseCodePoint(%q) = %#x, want rejection", bs+"uD800", got)
	}
}

// How the value survives PawScript, verified rather than assumed. All three
// spellings must reach the same byte, because all three are things a user will
// reasonably write:
//
//	insert_raw_byte "\\n"   doubled - the backslash survives as text
//	insert_raw_byte (\n)    parenthesised - held absolutely intact
//	insert_raw_byte "\n"    single - PawScript resolves it to a real newline
//	                        BEFORE the command sees it, which is why the
//	                        already-resolved case has to be handled too
//
// Unquoted (insert_raw_byte \n) is lossy - the backslash is eaten and only "n"
// arrives - so it is not a supported spelling.
func TestInsertRawByteSurvivesPawScriptQuoting(t *testing.T) {
	bs := string(rune(92))
	for _, script := range []string{
		`insert_raw_byte "` + bs + bs + `n"`,
		`insert_raw_byte (` + bs + `n)`,
		`insert_raw_byte "` + bs + `n"`,
	} {
		e, w := newTestEditor(t, "ab\n")
		w.SetCursorPos(viewport.Position{Line: 0, Rune: 1})
		e.executeCommand(script)
		if got := docContent(w); got != "a\nb" {
			t.Errorf("%s inserted %q, want a newline between a and b", script, got)
		}
	}
}
