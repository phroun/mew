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
	return runewidth.RuneWidth(r)
}
