package editor

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// runeHexSubstitute mirrors the renderer's substitute text for a defective
// combining mark: the codepoint's hex form, one ASCII column per character.
func runeHexSubstitute(r rune) string {
	v := int(r)
	if v <= 0xFF {
		return fmt.Sprintf("%02X", v) // one byte reads fine bare: FE
	}
	if v <= 0xFFFF {
		return fmt.Sprintf("(%04X)", v) // past one byte, bracket it: (0123)
	}
	return fmt.Sprintf("(%06X)", v)
}

// bidiControlRune maps a short bidi-control name to its Unicode code point.
// The set is deliberately the surgical marks and isolates (not the overrides
// or legacy embeddings): lrm/rlm/alm pin neutral punctuation, and the isolate
// group (fsi/lri/rli + pdi) brackets a foreign-direction span without leaking.
func bidiControlRune(name string) (rune, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "lrm":
		return '‎', true // LEFT-TO-RIGHT MARK
	case "rlm":
		return '‏', true // RIGHT-TO-LEFT MARK
	case "alm":
		return '؜', true // ARABIC LETTER MARK
	case "fsi":
		return '⁨', true // FIRST STRONG ISOLATE
	case "lri":
		return '⁦', true // LEFT-TO-RIGHT ISOLATE
	case "rli":
		return '⁧', true // RIGHT-TO-LEFT ISOLATE
	case "pdi":
		return '⁩', true // POP DIRECTIONAL ISOLATE
	}
	return 0, false
}

// parseCodePoint reads a Unicode scalar written the way a user writes one:
// "U+05D0", "u+05d0", "05D0", "0x05D0" - hex by default, because that is how
// code points are spelled everywhere they appear. "#" forces decimal for the
// rare case someone has one in that form ("#1488").
//
// Decimal is "#" and NOT "d": "d" is a hex digit, so a "d" prefix would eat
// the leading digit of "D800" and read the rest as decimal - quietly turning a
// surrogate (which this rejects) into U+0320 (which it would not).
//
// Surrogates (U+D800..U+DFFF) are rejected: they are not scalars, they only
// exist inside UTF-16, and Go would silently substitute U+FFFD for one.
func parseCodePoint(in string) (rune, bool) {
	if r, ok := literalControl(in); ok {
		return r, true
	}
	t := strings.TrimSpace(in)
	if t == "" {
		return 0, false
	}
	// \uNNNN is how a code point is written in source, so accept it (and the
	// rest of the C escapes) here too.
	if r, ok := parseCEscape(t); ok && r <= unicode.MaxRune && !(r >= 0xD800 && r <= 0xDFFF) {
		return r, true
	}
	base := 16
	switch {
	case len(t) > 2 && (strings.HasPrefix(t, "U+") || strings.HasPrefix(t, "u+")):
		t = t[2:]
	case len(t) > 2 && (strings.HasPrefix(t, "0x") || strings.HasPrefix(t, "0X")):
		t = t[2:]
	case len(t) > 1 && t[0] == '#':
		t, base = t[1:], 10
	}
	n, err := strconv.ParseInt(t, base, 64)
	if err != nil || n < 0 || n > unicode.MaxRune {
		return 0, false
	}
	if n >= 0xD800 && n <= 0xDFFF {
		return 0, false // surrogate half: not a scalar value
	}
	return rune(n), true
}

// asciiControlNames maps the conventional names of the C0 controls (plus SP and
// DEL) to their values, so a byte can be asked for by the name it is known by
// rather than by a number the user has to look up.
var asciiControlNames = map[string]rune{
	"NUL": 0x00, "SOH": 0x01, "STX": 0x02, "ETX": 0x03, "EOT": 0x04, "ENQ": 0x05,
	"ACK": 0x06, "BEL": 0x07, "BS": 0x08, "HT": 0x09, "TAB": 0x09, "LF": 0x0A,
	"NL": 0x0A, "VT": 0x0B, "FF": 0x0C, "CR": 0x0D, "SO": 0x0E, "SI": 0x0F,
	"DLE": 0x10, "DC1": 0x11, "XON": 0x11, "DC2": 0x12, "DC3": 0x13, "XOFF": 0x13,
	"DC4": 0x14, "NAK": 0x15, "SYN": 0x16, "ETB": 0x17, "CAN": 0x18, "EM": 0x19,
	"SUB": 0x1A, "ESC": 0x1B, "FS": 0x1C, "GS": 0x1D, "RS": 0x1E, "US": 0x1F,
	"SP": 0x20, "SPACE": 0x20, "DEL": 0x7F,
}

// parseByteSpec reads a byte 0..255 in whichever notation the value already
// exists in the user's head, because the reason to reach for this command at
// all is that the character cannot be typed:
//
//	^[  ^A  ^@  ^?   caret notation - the key you would press. ^? is DEL.
//	ESC BEL TAB CR   the control's conventional name (see asciiControlNames)
//	x1b 0x1b $1b     hex
//	o33 0o33         octal
//	b11011 0b11011   binary
//	#27              decimal ("#", not "d": "d" is a hex digit)
//	1b               bare, read as HEX - the convention for a byte
//	\n \r \t \e \0    the C escape, plus \xNN hex and \NNN octal
//
// A leading "b" is binary only when everything after it is 0 or 1 ("b11011");
// otherwise it is a hex digit as usual ("be" = 0xBE). Ambiguous only for
// values like "b0", which is read as binary - write "xb0" for the hex one.
//
// Hex is tried before the name table, so "ff" is 255 rather than FF (form
// feed) - the one name that is also a valid hex byte. Write "^L" or "x0c" for
// that character.
//
// The value is returned as a rune only because that is a convenient carrier for
// 0..255; it is a BYTE, and insertRawByteAt writes exactly that byte. High
// bytes are not reinterpreted as Latin-1 scalars - see insertRawByteAt for why
// the invalid-UTF-8 consequences are the caller's to own.
// cEscapes are the single-letter backslash escapes a C, Go, Python or shell
// programmer already has in their fingers. "\\e" is not ISO C, but every
// toolchain worth using accepts it for ESC and it is the one people reach for.
var cEscapes = map[byte]rune{
	'a': 0x07, 'b': 0x08, 'e': 0x1B, 'f': 0x0C, 'n': 0x0A,
	'r': 0x0D, 't': 0x09, 'v': 0x0B, '0': 0x00,
	'\\': 0x5C, '\'': 0x27, '"': 0x22, '?': 0x3F,
}

// parseCEscape reads a backslash escape in its C spelling: \n \r \t \0 \e and
// friends, \xNN hex, \NNN octal (1-3 digits), \uNNNN and \UNNNNNNNN. Returns
// the scalar and whether the whole string was consumed by one escape - a
// trailing anything makes it not an escape at all, so "\n\n" is rejected
// rather than quietly inserting one of them.
func parseCEscape(t string) (rune, bool) {
	if len(t) < 2 || t[0] != '\\' {
		return 0, false
	}
	body := t[1:]
	switch body[0] {
	case 'x', 'X':
		n, err := strconv.ParseInt(body[1:], 16, 64)
		if err != nil || n < 0 {
			return 0, false
		}
		return rune(n), true
	case 'u', 'U':
		n, err := strconv.ParseInt(body[1:], 16, 64)
		if err != nil || n < 0 || n > unicode.MaxRune {
			return 0, false
		}
		return rune(n), true
	}
	// Octal: \NNN, 1-3 digits. Checked before the single-letter table so \012
	// is octal 12 rather than \0 with stray digits after it.
	if body[0] >= '0' && body[0] <= '7' && len(body) > 1 {
		if len(body) > 3 {
			return 0, false
		}
		n, err := strconv.ParseInt(body, 8, 64)
		if err != nil || n < 0 {
			return 0, false
		}
		return rune(n), true
	}
	if len(body) != 1 {
		return 0, false
	}
	r, ok := cEscapes[body[0]]
	return r, ok
}

// literalByteRune accepts the input when it is ALREADY the character itself and
// names a byte. Wider than literalControl because PawScript resolves "\xfe" to
// the SCALAR U+00FE before this command ever runs, so a high byte arrives as one
// non-ASCII rune - and for a byte command that means the byte, not the scalar.
//
// Everything ASCII-printable is excluded, because that is where the numeric and
// named spellings live: "a" has to stay hex 0x0A, "FF" has to stay 255, "ESC"
// has to stay a name. What is left - controls, DEL, and U+0080..U+00FF - cannot
// be written any of those ways, so taking it literally is unambiguous. A rune
// past U+00FF is not a byte at all and is refused.
func literalByteRune(in string) (rune, bool) {
	rs := []rune(in)
	if len(rs) != 1 {
		return 0, false
	}
	r := rs[0]
	if r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0xFF) {
		return r, true
	}
	return 0, false
}

// literalControl accepts the input when it is ALREADY the character itself -
// one control rune, untrimmed. A keymap or PawScript argument written "\n" has
// its escape resolved by the parser before this command ever runs, so by the
// time the value arrives it is a real newline. Only control characters qualify:
// a printable single character like "a" stays a hex digit.
func literalControl(in string) (rune, bool) {
	rs := []rune(in)
	if len(rs) != 1 {
		return 0, false
	}
	if rs[0] < 0x20 || rs[0] == 0x7F {
		return rs[0], true
	}
	return 0, false
}

func parseByteSpec(in string) (rune, bool) {
	// Before trimming: TrimSpace would eat a literal tab or newline, and those
	// are exactly the values someone is most likely to be inserting.
	if r, ok := literalByteRune(in); ok {
		return r, true
	}
	t := strings.TrimSpace(in)
	if t == "" {
		return 0, false
	}
	if r, ok := parseCEscape(t); ok && r <= 0xFF {
		return r, true
	}
	// Caret notation: the key you would actually press.
	if len(t) == 2 && t[0] == '^' {
		c := t[1]
		if c == '?' {
			return 0x7F, true // ^? is DEL by convention, not 0x1F
		}
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		if c >= '@' && c <= '_' { // @A-Z[\]^_ cover the whole C0 range
			return rune(c & 0x1F), true
		}
		return 0, false
	}

	base := 16 // a byte is spelled in hex unless told otherwise
	switch {
	case len(t) > 2 && t[0] == '0' && (t[1] == 'x' || t[1] == 'X'):
		t = t[2:]
	case len(t) > 2 && t[0] == '0' && (t[1] == 'o' || t[1] == 'O'):
		t, base = t[2:], 8
	case len(t) > 2 && t[0] == '0' && (t[1] == 'b' || t[1] == 'B') && isBinaryDigits(t[2:]):
		t, base = t[2:], 2
	case len(t) > 1 && (t[0] == 'x' || t[0] == 'X' || t[0] == '$'):
		t = t[1:]
	case len(t) > 1 && (t[0] == 'o' || t[0] == 'O'):
		t, base = t[1:], 8
	case len(t) > 1 && (t[0] == 'b' || t[0] == 'B') && isBinaryDigits(t[1:]):
		t, base = t[1:], 2
	case len(t) > 1 && t[0] == '#':
		t, base = t[1:], 10
	}

	// Bare input is hex, and hex is tried BEFORE the name table so "ff" is 255
	// rather than FF (form feed) - the only name that is also valid hex.
	if n, err := strconv.ParseInt(t, base, 64); err == nil && n >= 0 && n <= 0xFF {
		return rune(n), true
	}
	// Names are tried on the ORIGINAL text, after any numeric reading failed:
	// "XOFF" and "XON" begin with the hex prefix letter, so the switch above
	// has already eaten their X by the time we get here.
	if r, ok := asciiControlNames[strings.ToUpper(strings.TrimSpace(in))]; ok {
		return r, true
	}
	return 0, false
}

// isBinaryDigits reports whether s is a non-empty run of 0s and 1s, which is
// what distinguishes a "b" binary prefix from a "b" hex digit.
func isBinaryDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '0' && s[i] != '1' {
			return false
		}
	}
	return true
}
