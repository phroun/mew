// Package textwidth centralizes display-width decisions so every place that
// measures screen columns — cursor math, line rendering, label padding —
// agrees with what the terminal will actually do.
package textwidth

import (
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
