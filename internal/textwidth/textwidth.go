// Package textwidth centralizes display-width decisions so every place that
// measures screen columns — cursor math, line rendering, label padding —
// agrees with what the terminal will actually do.
package textwidth

import (
	"sync"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// Rune returns the number of terminal columns a printable rune occupies:
// 0 for combining and zero-width characters (they overlay or attach to the
// previous cell), 2 for wide and fullwidth characters (CJK, emoji), and 1
// for everything else. Tabs and control characters are not handled here —
// their display width is mew-specific (tab stops, ^X substitutes) and is
// decided at the call sites.
//
// Combining marks (Unicode categories Mn and Me) are forced to zero via the
// standard library's tables: go-runewidth's zero-width set does not cover
// every combining mark (e.g. Hebrew accents), but terminals following
// wcwidth render them all into the preceding cell.
func Rune(r rune) int {
	if unicode.In(r, unicode.Mn, unicode.Me) {
		return 0
	}
	if isBidiControl(r) {
		// Explicit bidirectional format controls (LRM/RLM/ALM, the
		// embedding/override controls, and the isolate controls) advance no
		// cells — they steer layout, they don't print. go-runewidth's table
		// zeroes the older ones but reports 1 for ALM (U+061C) and the Unicode
		// 6.3 isolates (U+2066..U+2069); zero them here so column math agrees
		// with the terminal wherever these appear (including the isolates the
		// browse-mode button substitution wraps around each button).
		return 0
	}
	return runewidth.RuneWidth(r)
}

// renderableMarkScripts are the scripts whose combining marks mew can
// actually put on screen: it shapes them, ships or resolves faces for them,
// and terminal fonts carry them. General diacritics (script=Inherited, the
// U+0300..U+036F block and kin) are not listed — they belong to no script and
// are handled as always-renderable.
//
// This mirrors mew's own font architecture (the ui-{text,term}-{script} face
// tree: Latin, Hebrew, Arabic, CJK). A mark outside it is one mew has no
// glyph for and no shaping rules about.
var renderableMarkScripts = []*unicode.RangeTable{
	unicode.Hebrew,
	unicode.Arabic,
	unicode.Han,
	unicode.Hiragana,
	unicode.Katakana,
	unicode.Hangul,
	unicode.Greek,
	unicode.Cyrillic,
	unicode.Latin,
}

// IsControl reports whether r is a control character that must never reach the
// terminal as itself: the C0 set and DEL, and — just as important — the C1 set
// U+0080..U+009F.
//
// C1 is easy to miss because those codepoints arrive as ordinary two-byte
// UTF-8 (U+0094 is C2 94) and no width table calls them special. But a
// terminal decoding UTF-8 honors them as controls, and the range contains the
// STRING INTRODUCERS: DCS (U+0090), SOS (U+0098), CSI (U+009B), ST (U+009C),
// OSC (U+009D), PM (U+009E), APC (U+009F). One of those emitted from a binary
// file makes the terminal swallow everything after it as a control string —
// the rest of the line vanishes — until a terminator that may never come.
// mew renders every one of these as a visible substitute instead.
func IsControl(r rune) bool {
	return r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F)
}

// IsMark reports whether r is a combining mark — a codepoint that carries no
// cell of its own and paints into the preceding one.
func IsMark(r rune) bool {
	return unicode.In(r, unicode.Mn, unicode.Mc, unicode.Me)
}

// scriptCache memoizes scriptOf: a combining mark's script is fixed, and the
// lookup below scans every script table, which is too much to repeat for each
// mark of a fully pointed Hebrew or Arabic line.
var scriptCache sync.Map // rune -> string

// scriptOf names the Unicode script a rune belongs to ("Hebrew", "Han",
// "Common", ...), or "" when no script claims it.
func scriptOf(r rune) string {
	if v, ok := scriptCache.Load(r); ok {
		return v.(string)
	}
	name := ""
	for n, table := range unicode.Scripts {
		if unicode.Is(table, r) {
			name = n
			break
		}
	}
	scriptCache.Store(r, name)
	return name
}

// DefectiveMark reports whether the combining mark r is one mew must NOT paint
// as zero-width after the base character prev. Three cases:
//
//   - No base at all (prev == 0): the mark opens the line with nothing to
//     anchor onto.
//   - The mark belongs to a script mew cannot render (see
//     renderableMarkScripts) — NKo tone marks, for instance.
//   - The mark is SCRIPT-SPECIFIC and the base belongs to a different script:
//     a Hebrew accent over a CJK ideograph, niqqud on a Latin letter, harakat
//     on punctuation. Unicode calls such a sequence ill-formed, and no shaper
//     will compose it.
//
// Both mew and wcwidth call every combining mark zero-width, which is a
// promise about what the terminal will do: paint the mark INTO the preceding
// cell and advance nothing. A terminal that has no glyph for the mark, nothing
// to compose it onto, or a base its shaper refuses to attach the mark to,
// breaks that promise and falls back to a SPACING glyph — .notdef, or
// dotted-circle + mark. That glyph advances a column mew never budgeted, so
// the rest of the row slides right, overruns the window and bleeds past its
// edge. This is the class of corruption the renderer already handles for
// control codes, so these marks are classified the same way and painted as a
// definite-width visible substitute.
//
// General diacritics (script=Inherited/Common — the U+0300..U+036F block, the
// kana voicing marks) belong to no script and legitimately attach to any base,
// so they are never defective on this rule.
func DefectiveMark(prev, r rune) bool {
	if !IsMark(r) {
		return false
	}
	if prev == 0 {
		return true // nothing to anchor onto
	}
	markScript := scriptOf(r)
	if markScript == "" || markScript == "Inherited" || markScript == "Common" {
		return false // general diacritic: attaches to any base
	}
	if !RenderableMark(r) {
		return true // a mark from a script mew has no glyph for
	}
	// Script-specific mark: it is well-formed only on a base of its own
	// script.
	return scriptOf(prev) != markScript
}

// RenderableMark reports whether mew can actually DRAW the combining mark r:
// it is script-neutral (a general diacritic), or it belongs to a script in
// mew's face tree. A mark outside that set has no glyph anywhere in reach, so
// nothing can be composed from it.
func RenderableMark(r rune) bool {
	if !IsMark(r) {
		return false
	}
	if s := scriptOf(r); s == "" || s == "Inherited" || s == "Common" {
		return true
	}
	for _, s := range renderableMarkScripts {
		if unicode.Is(s, r) {
			return true
		}
	}
	return false
}

// AnchorMark reports whether a defective mark should be shown ANCHORED on a
// dotted circle (U+25CC) — the Unicode convention for displaying an isolated
// combining mark — rather than reduced to a hex substitute.
//
// It applies when the mark has no base of its own AND mew can draw it: the
// dotted circle supplies the missing base, the mark composes onto it exactly
// as shapers already do for defective clusters, and the pair occupies one
// cell that both mew and the terminal agree on. The reader sees the real mark
// instead of a number.
//
// A mark mew CANNOT draw gets no anchor: composing onto the circle would still
// leave the terminal drawing .notdef for the mark itself, which is a spacing
// glyph, and the overrun this whole classification exists to prevent would be
// back. Those keep the hex form.
func AnchorMark(prev, r rune) bool {
	return prev == 0 && IsMark(r) && RenderableMark(r)
}

// MarkAnchor is the base character an anchored mark is composed onto.
const MarkAnchor = '◌' // DOTTED CIRCLE

// isBidiControl mirrors bidi.IsDirectionControl. It is duplicated here rather
// than imported because the bidi package depends on this one for its cluster
// width math, and an import back would cycle.
func isBidiControl(r rune) bool {
	switch {
	case r == 0x200E || r == 0x200F || r == 0x061C:
		return true
	case r >= 0x202A && r <= 0x202E:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}
