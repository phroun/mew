// Package core provides fundamental types for KittyTK.
package core

import (
	"math"
	"sync"

	"github.com/phroun/kittytk/style"
)

// TextMeasurer answers text measurement for the current render
// target (G1: measurement comes from the target, where the fonts
// are). The text-based system's answer is the built-in cell
// arithmetic - one character occupies one cell's worth of layout
// units - which is exact for terminals. A graphical display service
// installs its shaping engine here so measurement matches the
// proportional render; the same engine paints, so the two can never
// disagree.
type TextMeasurer interface {
	MeasureText(f *Font, text string) Unit
}

// DenominatedTextMeasurer is the optional extension a measurer implements
// when it can answer in an arbitrary denomination rather than only the
// default one. A measurer without it is asked at the default and its answer
// scaled, which rounds twice; one with it converts inside its own shaping,
// where the advance is still a real quantity.
type DenominatedTextMeasurer interface {
	MeasureTextIn(f *Font, text string, m CellMetrics) Unit
}

var (
	textMeasurerMu sync.RWMutex
	textMeasurer   TextMeasurer
)

// SetTextMeasurer installs the render target's text measurer. Pass
// nil to restore the text-mode cell arithmetic. Called by the
// display service when its backend provides measurement (pixel
// backends do); one render target per process.
func SetTextMeasurer(m TextMeasurer) {
	textMeasurerMu.Lock()
	textMeasurer = m
	textMeasurerMu.Unlock()
}

func currentTextMeasurer() TextMeasurer {
	textMeasurerMu.RLock()
	defer textMeasurerMu.RUnlock()
	return textMeasurer
}

// HasTextMeasurer reports whether a graphical render target has installed
// a text measurer - i.e. the process renders on a pixel backend where
// MeasureText answers with real font metrics rather than text-mode
// cell arithmetic. It is the process-wide graphical/text-mode
// signal (one render target per process).
func HasTextMeasurer() bool {
	return currentTextMeasurer() != nil
}

// FontStyle represents text styling attributes that can be combined.
type FontStyle uint16

const (
	// FontStyleNormal is the default style with no modifications.
	FontStyleNormal FontStyle = 0

	// FontStyleDim reduces the intensity of the text.
	FontStyleDim FontStyle = 1 << iota

	// FontStyleBright increases the intensity of the text.
	FontStyleBright

	// FontStyleBold makes the text bold/heavy.
	FontStyleBold

	// FontStyleItalic makes the text italic/oblique.
	FontStyleItalic

	// FontStyleUnderline adds an underline to the text.
	FontStyleUnderline

	// FontStyleStrikeThru adds a line through the text.
	FontStyleStrikeThru
)

// FontColor represents a color that can be default (inherit from scheme) or explicit.
type FontColor struct {
	IsDefault bool
	Color     style.Color
}

// DefaultFontColor returns a FontColor that uses the default/inherited color.
func DefaultFontColor() FontColor {
	return FontColor{IsDefault: true}
}

// ExplicitFontColor returns a FontColor with an explicit color value.
func ExplicitFontColor(c style.Color) FontColor {
	return FontColor{IsDefault: false, Color: c}
}

// Font represents a typeface with styling information.
// Fonts provide metrics for text measurement in units and control text attributes.
// Use MeasureText or MeasureRunes to determine the width of text in units.
type Font struct {
	// Name identifies the font family. "ui-text" is the internal
	// default: each renderer maps it to its own UI face (the
	// text-based system treats it as Monday, the standard cell font;
	// the graphical engine maps it to its default proportional
	// family). Controls that genuinely require monospace name a
	// concrete family ("Monday") explicitly.
	// Built-in fonts: "Monday" (standard width), "Tuesday" (double width)
	Name string

	// Style contains styling flags (bold, italic, underline, etc.)
	Style FontStyle

	// Size is the point size (e.g., 12 for 12pt)
	Size int

	// Foreground is the text color (default = inherit from color scheme)
	Foreground FontColor

	// Background is the background color (default = inherit from color scheme)
	Background FontColor
}

// Predefined fonts
var (
	// FontUIText12 is the UI default: the renderer picks the face
	// ("ui-text" maps to Monday in text mode, to the service's
	// proportional UI family in graphical mode).
	FontUIText12 = &Font{
		Name:       "ui-text",
		Size:       12,
		Foreground: DefaultFontColor(), // Use scheme colors
	}

	// FontMonday12 is the standard fixed-width font (8 units per character).
	FontMonday12 = &Font{
		Name:       "Monday",
		Size:       12,
		Foreground: DefaultFontColor(), // Use scheme colors
	}

	// FontUITerm12 is the terminal grid's primary face — purfecterm's font
	// slot 0 (SGR 10). It starts aliased to the monospace default and is meant
	// to be re-pointed live (SetFontAlias via [fonts]/[window] ui_term or the
	// set_font command). In the text-based system fonts are the outer
	// terminal's, so the name is inert there.
	FontUITerm12 = &Font{
		Name:       "ui-term",
		Size:       12,
		Foreground: DefaultFontColor(),
	}

	// FontTuesday12 is a proportional-style font (16 units for letters/digits,
	// 8 units for punctuation and symbols).
	// DEBUG: Uses bright red foreground to distinguish text from decorative elements.
	FontTuesday12 = &Font{
		Name:       "Tuesday",
		Size:       12,
		Foreground: ExplicitFontColor(style.ColorBrightRed),
	}
)

// DefaultFont returns the default UI font ("ui-text" 12pt; each
// renderer maps the name to its own face).
func DefaultFont() *Font {
	return FontUIText12
}

// LineUnits is how many units one line of text occupies.
//
// A line is a grid row, and a grid row is UnitsPerCellHeight units: that is
// what the denomination says. Point size does not enter into it -- it sets
// how big the cell is on the glass, not how finely a layout divides it.
//
// The exception is a face deliberately smaller or larger than the one the
// surface is laid out in: the 75% caption in a separator's title gap, the
// 80% shortcut in a menu row, the menu bar's clock. Such a face fills that
// same fraction of a row, which is what surface is for. Pass the same font
// twice, or nil, for a full row.
func LineUnits(f, surface *Font, m CellMetrics) Unit {
	if f == nil || surface == nil || surface.Size <= 0 || f.Size == surface.Size {
		return m.UnitsPerCellHeight
	}
	return Unit(int(m.UnitsPerCellHeight) * f.Size / surface.Size)
}

// LineBudgetMeasurer is the optional extension a measurer implements when it
// can answer a font's own line box. Satisfied by the pixel backends, whose
// shaping engine derives the em size to fill it.
type LineBudgetMeasurer interface {
	LineHeight(f *Font) Unit
}

// FontLineBudget is the font's own line box in DEFAULT-denomination units --
// Size * 4/3, so 12pt is 16, one default cell row -- or 0 where the render
// target cannot answer.
//
// A font metric, not a row height. How many units a line of a LAYOUT occupies
// is the denomination's answer and nothing to do with the font: see LineUnits.
// This is for the one thing that needs the glyph box itself, a terminal's cell
// grid, whose pitch is the font's and whose consumers convert it with the
// backend's pixels-per-unit -- which counts default-denomination units too, so
// the two agree.
func FontLineBudget(f *Font) Unit {
	if lb, ok := currentTextMeasurer().(LineBudgetMeasurer); ok {
		return lb.LineHeight(f)
	}
	return 0
}

// BaselineMeasurer is the optional extension a measurer implements when it
// can answer where a face's baseline sits. Satisfied by the pixel backends,
// whose shaping engine has the face's real ascent.
type BaselineMeasurer interface {
	Baseline(f *Font) Unit
}

// FontBaseline is how far below the top of its line a face's baseline sits,
// in DEFAULT-denomination units, or 0 where the render target cannot answer.
//
// What two faces of different sizes need to share a line. Centring the
// smaller one's line BOX in the row is not the same thing and is not close
// enough to pass: a box's ascent is not half of it, so the smaller face lands
// off the line the bigger one sits on -- lower, in the usual case, by the
// difference between half a box and a real ascent.
func FontBaseline(f *Font) Unit {
	if bm, ok := currentTextMeasurer().(BaselineMeasurer); ok {
		return bm.Baseline(f)
	}
	return 0
}

// CapHeightMeasurer is the optional extension a measurer implements when it
// can answer how tall a face's capitals are. Satisfied by the pixel backends.
type CapHeightMeasurer interface {
	CapHeight(f *Font) Unit
}

// FontCapHeight is how far a capital's ink reaches above the baseline, in
// DEFAULT-denomination units, or 0 where the render target cannot answer.
//
// The size of the TYPE, which is what sitting one face beside another wants.
// A line box carries leading the letters do not use; a baseline says nothing
// about how tall they are; and the ink of a particular string depends on
// whether that string has descenders in it. Measured once per face, from the
// capital, none of those apply.
func FontCapHeight(f *Font) Unit {
	if cm, ok := currentTextMeasurer().(CapHeightMeasurer); ok {
		return cm.CapHeight(f)
	}
	return 0
}

// MeasureTextIn returns the width of text in the units of the given
// denomination -- how many of THOSE units the same text occupies.
//
// Text is proportional and its physical size does not depend on the
// denomination. The denomination is the currency the answer is counted in:
// a cell is a fixed physical size, so a higher denomination divides it into
// smaller units and the same text measures more of them.
//
// MeasureText is this at the default denomination, which is what every
// caller that has not been told otherwise means.
func (f *Font) MeasureTextIn(text string, m CellMetrics) Unit {
	cw := m.UnitsPerCellWidth
	if cw < 1 {
		cw = DefaultCellMetrics().UnitsPerCellWidth
	}
	if d, ok := currentTextMeasurer().(DenominatedTextMeasurer); ok {
		return d.MeasureTextIn(f, text, CellMetrics{UnitsPerCellWidth: cw, UnitsPerCellHeight: m.UnitsPerCellHeight})
	}
	if mm := currentTextMeasurer(); mm != nil {
		// No denominated answer available: scale the default-denomination
		// one. Rounds twice; the extension above exists to avoid it.
		base := DefaultCellMetrics().UnitsPerCellWidth
		return Unit(math.Round(float64(mm.MeasureText(f, text)) * float64(cw) / float64(base)))
	}
	if f == nil {
		f = DefaultFont()
	}

	// The text-based system: one character occupies one cell, two for a
	// wide one -- and a cell is cw units by definition of the denomination.
	total := Unit(0)
	isTuesday := f.Name == "Tuesday"
	for _, ch := range text {
		if isTuesday && isAlphabetic(ch) {
			total += 2 * cw
		} else {
			total += cw
		}
		if isWideChar(ch) {
			total += cw
		}
	}
	return total
}

// MeasureText returns the width in units needed to display the given text.
// The answer comes from the render target. On the text-based system a
// character occupies one cell's worth of layout units (two for wide
// CJK characters) - the arithmetic below. A graphical target installs
// its shaping engine via SetTextMeasurer and answers with real
// proportional advances instead.
// For Tuesday font, only alphabetic characters are double-width; punctuation
// and symbols remain standard width to simulate proportional font behavior.
func (f *Font) MeasureText(text string) Unit {
	if m := currentTextMeasurer(); m != nil {
		return m.MeasureText(f, text)
	}
	if f == nil {
		f = DefaultFont()
	}

	total := Unit(0)
	isTuesday := f.Name == "Tuesday"

	for _, ch := range text {
		// Determine base width for this character
		if isTuesday && isAlphabetic(ch) {
			total += 16
		} else {
			total += 8
		}

		// Wide characters (CJK, etc.) need additional width
		if isWideChar(ch) {
			total += 8
		}
	}

	return total
}

// MeasureRunes returns the width in units for a given number of runes.
// This assumes all characters are alphabetic (full width for Tuesday font).
// For mixed content, use MeasureText instead.
//
// A rune is one cell of the default denomination: 8 units wide (16 for
// the double-width Tuesday demo face). This is a unit count, so it does
// NOT vary with font_size - font_size scales the pixel size of a unit,
// not the number of units per character.
func (f *Font) MeasureRunes(runeCount int) Unit {
	if f == nil {
		f = DefaultFont()
	}
	perRune := 8
	if f.Name == "Tuesday" {
		perRune = 16 // double-width demo face
	}
	return Unit(runeCount * perRune)
}

// isAlphabetic returns true if the character is a letter or digit.
func isAlphabetic(ch rune) bool {
	// Basic Latin letters
	if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
		return true
	}
	// Digits
	if ch >= '0' && ch <= '9' {
		return true
	}
	// Latin Extended-A
	if ch >= 0x0100 && ch <= 0x017F {
		return true
	}
	// Latin Extended-B
	if ch >= 0x0180 && ch <= 0x024F {
		return true
	}
	// Greek and Coptic
	if ch >= 0x0370 && ch <= 0x03FF {
		return true
	}
	// Cyrillic
	if ch >= 0x0400 && ch <= 0x04FF {
		return true
	}
	return false
}

// HasStyle returns true if the font has the given style flag set.
func (f *Font) HasStyle(s FontStyle) bool {
	if f == nil {
		return false
	}
	return f.Style&s != 0
}

// WithStyle returns a copy of the font with additional style flags.
func (f *Font) WithStyle(s FontStyle) *Font {
	if f == nil {
		return &Font{Name: "ui-text", Size: 12, Style: s}
	}
	copy := *f
	copy.Style |= s
	return &copy
}

// WithForeground returns a copy of the font with the specified foreground color.
func (f *Font) WithForeground(c FontColor) *Font {
	if f == nil {
		return &Font{Name: "ui-text", Size: 12, Foreground: c}
	}
	copy := *f
	copy.Foreground = c
	return &copy
}

// WithBackground returns a copy of the font with the specified background color.
func (f *Font) WithBackground(c FontColor) *Font {
	if f == nil {
		return &Font{Name: "ui-text", Size: 12, Background: c}
	}
	copy := *f
	copy.Background = c
	return &copy
}

// isWideChar returns true if the character is a wide character (CJK, emoji, etc.)
func isWideChar(ch rune) bool {
	// CJK Unified Ideographs
	if ch >= 0x4E00 && ch <= 0x9FFF {
		return true
	}
	// CJK Unified Ideographs Extension A
	if ch >= 0x3400 && ch <= 0x4DBF {
		return true
	}
	// CJK Unified Ideographs Extension B
	if ch >= 0x20000 && ch <= 0x2A6DF {
		return true
	}
	// CJK Compatibility Ideographs
	if ch >= 0xF900 && ch <= 0xFAFF {
		return true
	}
	// Hangul Syllables
	if ch >= 0xAC00 && ch <= 0xD7AF {
		return true
	}
	// Hiragana
	if ch >= 0x3040 && ch <= 0x309F {
		return true
	}
	// Katakana
	if ch >= 0x30A0 && ch <= 0x30FF {
		return true
	}
	// Fullwidth Forms
	if ch >= 0xFF00 && ch <= 0xFFEF {
		return true
	}
	return false
}

// FontProvider is implemented by trinkets that can provide a font.
type FontProvider interface {
	// Font returns the font set on this provider, or nil if not set.
	Font() *Font
}

// FindEffectiveFont walks up the trinket tree to find the effective font.
// It checks the trinket, then its parent window, then the desktop/MDI pane.
// Returns DefaultFont() if no font is set anywhere in the chain.
func FindEffectiveFont(w Trinket) *Font {
	if w == nil {
		return DefaultFont()
	}

	// Check if the trinket itself has a font
	if fp, ok := w.(FontProvider); ok {
		if f := fp.Font(); f != nil {
			return f
		}
	}

	// Walk up the parent chain
	current := w.Parent()
	for current != nil {
		if fp, ok := current.(FontProvider); ok {
			if f := fp.Font(); f != nil {
				return f
			}
		}
		if trinket, ok := current.(Trinket); ok {
			current = trinket.Parent()
		} else {
			break
		}
	}

	return DefaultFont()
}
