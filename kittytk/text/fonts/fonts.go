// Package fonts embeds the toolkit's default graphical typefaces:
// Noto Sans (UI text) and Noto Sans Mono (monospace), chosen for
// their broad Unicode coverage. Both are licensed under the SIL Open
// Font License 1.1 (see OFL.txt).
package fonts

import _ "embed"

//go:embed NotoSans-Regular.ttf
var SansRegular []byte

//go:embed NotoSans-Bold.ttf
var SansBold []byte

//go:embed NotoSans-Italic.ttf
var SansItalic []byte

//go:embed NotoSans-BoldItalic.ttf
var SansBoldItalic []byte

//go:embed NotoSansMono-Regular.ttf
var MonoRegular []byte

//go:embed NotoSansMono-Bold.ttf
var MonoBold []byte

// Fallback-only faces: never selected by name in the UI, but they
// extend per-rune coverage (geometric shapes and other symbols used
// by trinket chrome; Hebrew; Arabic).

//go:embed NotoSansSymbols2-Regular.ttf
var Symbols2Regular []byte

//go:embed NotoSansHebrew-Regular.ttf
var HebrewRegular []byte

//go:embed NotoSansHebrew-Bold.ttf
var HebrewBold []byte

// Noto Serif Hebrew: the DEFAULT Hebrew fallback — the more legible body face,
// with the clearest niqqud. Registered before the sans Hebrew (above) in
// fontdb so it wins the fallback; the sans stays addressable by name
// ("Noto Sans Hebrew") for a sans look.

//go:embed NotoSerifHebrew-Regular.ttf
var HebrewSerifRegular []byte

//go:embed NotoSerifHebrew-Bold.ttf
var HebrewSerifBold []byte

// Two Arabic faces. Both carry the Arabic Presentation Forms-A/B blocks
// (U+FB50-FDFF, U+FE70-FEFF) as real cmap entries, which the terminal's
// per-cell shaper needs — it emits a precomposed presentation-form codepoint
// per cell (isolated/initial/medial/final + lam-alef ligature), so a face
// must map those codepoints to render joined text at all.
//
// Naskh is the default Arabic fallback: the standard body-text style, most
// legible at terminal sizes and with the clearest harakat (vowel marks) — the
// right choice for reading and typing. Kufi is a geometric DISPLAY style,
// embedded and addressable by name ("Noto Kufi Arabic") for a retro look when
// wanted, but not the default because it tires the eye for running text.
// Registration order (Naskh first, in fontdb) makes Naskh win the fallback.

//go:embed NotoNaskhArabic-Regular.ttf
var ArabicRegular []byte

//go:embed NotoNaskhArabic-Bold.ttf
var ArabicBold []byte

//go:embed NotoKufiArabic-Regular.ttf
var ArabicKufiRegular []byte

//go:embed NotoKufiArabic-Bold.ttf
var ArabicKufiBold []byte
