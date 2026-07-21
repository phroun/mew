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

// Fraktur mode constants ([tui] fraktur_mode).
const (
	FrakturNative = "native" // forward the terminal's VT100 fraktur (SGR 20) to the enclosing terminal
	FrakturPseudo = "pseudo" // render fraktur with the pseudo-fraktur cipher (default)
	FrakturOff    = "off"    // don't honor fraktur: use the normal terminal font
)

// Two independent [tui] knobs, configured once at host startup:
//
//   - disabledPseudoFonts gates the CIPHER pseudo-fonts when selected BY NAME
//     (a UI element's font = "Fraktur", "Black Serif", …): a disabled group
//     (black_serif, black_sans, double, fraktur, script) renders plain. This is
//     what cipherText / active() consult.
//
//   - frakturMode is a SEPARATE concern: how a terminal's VT100 fraktur REQUEST
//     (font 20 / SGR 20) is handled by the terminal-render path — native (pass
//     the escape to the enclosing terminal), pseudo (render it via the fraktur
//     cipher), or off (normal font). It does NOT gate the by-name cipher; the
//     two only share the word "fraktur".
var (
	disabledPseudoFonts = map[string]bool{}
	frakturMode         = FrakturPseudo
)

// ConfigurePseudoFonts sets the [tui] pseudo-font gating. disabled maps a
// cipher toggle group to true to turn it off (includes fraktur, for the
// by-name cipher); fMode is the separate VT fraktur-request mode (native /
// pseudo / off — empty keeps the current). Call once at startup.
func ConfigurePseudoFonts(disabled map[string]bool, fMode string) {
	m := map[string]bool{}
	for k, v := range disabled {
		m[k] = v
	}
	disabledPseudoFonts = m
	switch fMode {
	case FrakturNative, FrakturPseudo, FrakturOff:
		frakturMode = fMode
	}
}

// PseudoFontEnabled reports whether a cipher toggle group is active (by-name).
func PseudoFontEnabled(group string) bool { return !disabledPseudoFonts[group] }

// FrakturMode returns the VT fraktur-request mode (native / pseudo / off) — the
// terminal-render path consults it for a font-20 (SGR 20) cell. Independent of
// the by-name fraktur cipher toggle above.
func FrakturMode() string { return frakturMode }

// active reports whether this style's by-name cipher applies, honoring its
// toggle group (fraktur included).
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

// apply ciphers every rune of s through the style.
func (st cipherStyle) apply(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		b.WriteRune(st.cipherRune(r))
	}
	return b.String()
}

// VTFrakturName is the reserved family name for the VT100 fraktur request
// (font 20 / SGR 20). It is DISTINCT from the by-name "Fraktur" cipher: a
// terminal that requests fraktur resolves its font-20 cell to this name, and
// the text backend renders it per fraktur_mode. The name itself is the SGR-20
// signal — no separate detection hook.
const VTFrakturName = "VTFRAKTUR"

// vtFrakturNative reports whether fontName is the VTFRAKTUR request AND
// fraktur_mode is native — meaning the draw path should tag the cells with the
// fraktur attribute so the flush emits real SGR-20 fraktur (rather than
// ciphering). In pseudo/off modes cipherText handles the characters instead.
func vtFrakturNative(fontName string) bool {
	return frakturMode == FrakturNative &&
		strings.EqualFold(strings.TrimSpace(fontName), VTFrakturName)
}

// cipherText ciphers s through the pseudo-font named fontName. A non-cipher
// name (Monday, Tuesday, ui-term, a real family, …) returns s unchanged, so
// callers can apply it unconditionally.
func cipherText(fontName, s string) string {
	// VTFRAKTUR = the VT100 fraktur request, governed by fraktur_mode (not the
	// pseudofont_fraktur by-name toggle): pseudo renders it via the fraktur
	// cipher; native/off leave the characters plain (native additionally
	// forwards the SGR-20 escape — the terminal-output path's job).
	if strings.EqualFold(strings.TrimSpace(fontName), VTFrakturName) {
		if frakturMode == FrakturPseudo {
			return cipherStyles["fraktur"].apply(s)
		}
		return s
	}
	st, ok := lookupCipherStyle(fontName)
	if !ok || !st.active() {
		return s
	}
	return st.apply(s)
}
