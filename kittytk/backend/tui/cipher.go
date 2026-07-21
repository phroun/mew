package tui

import "strings"

// Cipher pseudo-fonts for the text backend. Real fonts don't exist here, but we
// can fake several typographic styles by *ciphering* plain ASCII into the
// visually-similar code points of Unicode's Mathematical Alphanumeric Symbols
// block (U+1D400..1D7FF) plus the older Letterlike Symbols that fill its holes.
// The outer terminal renders whatever glyphs its own fonts carry; we only swap
// the characters. Every ciphered code point is width-1, so layout is unchanged.
//
// Only A-Z, a-z, and (for the styles Unicode gives styled digits) 0-9 are
// transformed; everything else — punctuation, spaces, accents, and digits in
// the styles without styled digits — passes through untouched.

// cipherStyle describes one style: the base code point that 'A' and 'a' map to,
// the base for '0' (0 = no styled digits, digits pass through), a table of
// per-letter exceptions for the code points that live in Letterlike Symbols,
// and the [tui] toggle group that can disable it ("" = always on).
type cipherStyle struct {
	upper, lower, digit rune
	holes               map[rune]rune
	group               string
}

// dblHoles / frakHoles: the uppercase letters unified into Letterlike Symbols
// instead of the contiguous math block.
var dblHoles = map[rune]rune{'C': 0x2102, 'H': 0x210D, 'N': 0x2115, 'P': 0x2119, 'Q': 0x211A, 'R': 0x211D, 'Z': 0x2124}
var frakHoles = map[rune]rune{'C': 0x212D, 'H': 0x210C, 'I': 0x2111, 'R': 0x211C, 'Z': 0x2128}

// cipherStyles maps a (case-insensitive) pseudo-font name to its style. Names
// follow the user's labels; a few aliases are accepted.
var cipherStyles = map[string]cipherStyle{
	"black serif":   {upper: 0x1D400, lower: 0x1D41A, digit: 0x1D7CE, group: "black_serif"}, // Math Bold
	"double-struck": {upper: 0x1D538, lower: 0x1D552, digit: 0x1D7D8, holes: dblHoles, group: "double"},
	"bold fraktur":  {upper: 0x1D56C, lower: 0x1D586, group: "fraktur"},
	"bold italic":   {upper: 0x1D468, lower: 0x1D482}, // Math Bold Italic (serif); always on
	"fraktur":       {upper: 0x1D504, lower: 0x1D51E, holes: frakHoles, group: "fraktur"},
	"bold script":   {upper: 0x1D4D0, lower: 0x1D4EA, group: "script"},              // Math Bold Script
	"black sans":    {upper: 0x1D5D4, lower: 0x1D5EE, digit: 0x1D7EC, group: "black_sans"}, // Math Sans Bold
	"black italic":  {upper: 0x1D63C, lower: 0x1D656}, // Math Sans Bold Italic; always on
	"italic":        {upper: 0x1D608, lower: 0x1D622}, // Math Sans Italic; always on
}

// cipherAliases accepts a few alternate spellings for the same style.
var cipherAliases = map[string]string{
	"double struck": "double-struck",
	"gothic":        "fraktur",
	"bold-fraktur":  "bold fraktur",
	"bold-italic":   "bold italic",
	"bold-script":   "bold script",
	"black-serif":   "black serif",
	"black-sans":    "black sans",
	"black-italic":  "black italic",
}

// CipherFontNames lists the canonical selectable cipher pseudo-font names, in a
// stable display order (for property pickers / the font menu).
var CipherFontNames = []string{
	"Black Serif", "Double-Struck", "Bold Fraktur", "Bold Italic", "Fraktur",
	"Bold Script", "Black Sans", "Black Italic", "Italic",
}

// Pseudo-font gating, configured once at host startup from the [tui] section.
// These are two INDEPENDENT knobs:
//   - disabledPseudoFonts turns a cipher group off, so that style renders plain.
//   - realFraktur is unrelated to the cipher: it controls whether a terminal's
//     VT100 fraktur (SGR 20) escape is ALSO passed through to the enclosing
//     terminal. Both pseudofont_fraktur and real_fraktur can be on at once.
var (
	disabledPseudoFonts = map[string]bool{}
	realFraktur         = false
)

// ConfigurePseudoFonts sets the [tui] pseudo-font gating. disabled maps a
// toggle group (black_serif, black_sans, double, fraktur, script) to true to
// turn it off. realFrakturOn passes real VT100 fraktur through to the enclosing
// terminal (independent of the fraktur cipher). Call once at startup.
func ConfigurePseudoFonts(disabled map[string]bool, realFrakturOn bool) {
	m := map[string]bool{}
	for k, v := range disabled {
		m[k] = v
	}
	disabledPseudoFonts = m
	realFraktur = realFrakturOn
}

// PseudoFontEnabled reports whether a cipher toggle group is active.
func PseudoFontEnabled(group string) bool { return !disabledPseudoFonts[group] }

// RealFrakturPassthrough reports whether a terminal's VT100 fraktur (SGR 20)
// escape should be forwarded to the enclosing terminal. Independent of whether
// the fraktur cipher is applied.
func RealFrakturPassthrough() bool { return realFraktur }

// active reports whether this style's cipher currently applies, honoring only
// its toggle group (real_fraktur is a separate, orthogonal knob).
func (st cipherStyle) active() bool {
	return st.group == "" || !disabledPseudoFonts[st.group]
}

// lookupCipherStyle resolves a font name to a cipher style, or (‑, false) when
// the name isn't a cipher pseudo-font.
func lookupCipherStyle(fontName string) (cipherStyle, bool) {
	key := strings.ToLower(strings.TrimSpace(fontName))
	if canon, ok := cipherAliases[key]; ok {
		key = canon
	}
	st, ok := cipherStyles[key]
	return st, ok
}

// cipherRune maps one rune through a style (A-Z, a-z, and 0-9 when the style
// has styled digits); everything else is returned unchanged.
func (st cipherStyle) cipherRune(r rune) rune {
	switch {
	case r >= 'A' && r <= 'Z':
		if h, ok := st.holes[r]; ok {
			return h
		}
		return st.upper + (r - 'A')
	case r >= 'a' && r <= 'z':
		return st.lower + (r - 'a')
	case r >= '0' && r <= '9' && st.digit != 0:
		return st.digit + (r - '0')
	}
	return r
}

// cipherText ciphers s through the pseudo-font named fontName. A non-cipher
// name (Monday, Tuesday, ui-term, a real family, …) returns s unchanged, so
// callers can apply it unconditionally.
func cipherText(fontName, s string) string {
	st, ok := lookupCipherStyle(fontName)
	if !ok || !st.active() {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		b.WriteRune(st.cipherRune(r))
	}
	return b.String()
}
