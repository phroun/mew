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

// Noto Naskh Arabic carries the Arabic Presentation Forms-A/B blocks
// (U+FB50-FDFF, U+FE70-FEFF) as real cmap entries, so the terminal's
// per-cell Arabic shaper — which emits a precomposed presentation-form
// codepoint per cell (isolated/initial/medial/final + lam-alef ligature) —
// resolves to properly connecting glyphs deterministically, without
// depending on a system Arabic face being installed or winning fallback.

//go:embed NotoNaskhArabic-Regular.ttf
var ArabicRegular []byte

//go:embed NotoNaskhArabic-Bold.ttf
var ArabicBold []byte
