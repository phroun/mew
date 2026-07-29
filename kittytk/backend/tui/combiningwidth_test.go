package tui

import (
	"testing"
	"unicode"
)

// A combining mark must never take a cell of its own: it rides the base cell's
// Combining string. purfecterm's IsCombiningMark is a hand-written range list
// that misses the great majority of Unicode's non-spacing marks, and each one
// it misses pushes every later cell on the row a column to the right — the row
// drifts and its tail is left unpainted.
func TestNonSpacingMarksAreZeroWidth(t *testing.T) {
	// Spot checks across the scripts the range list omits.
	for _, r := range []rune{
		0x07ED, // NKo combining short rising tone
		0x0F71, // Tibetan vowel sign AA
		0x102E, // Myanmar vowel sign II
		0x09BC, // Bengali nukta
		0x1B34, // Balinese sign rerekan
		0x1885, // Mongolian letter Ali Gali baluda
		0x0951, // Devanagari stress sign udatta
		0x0E31, // Thai character mai han-akat
	} {
		if got := cellRuneWidth(r); got != 0 {
			t.Errorf("U+%04X is a non-spacing mark but measures %d columns", r, got)
		}
	}

	// And exhaustively: no Mn/Me codepoint may claim a cell.
	bad := 0
	for r := rune(0); r <= 0x2FFFF; r++ {
		if !unicode.In(r, unicode.Mn, unicode.Me) {
			continue
		}
		if cellRuneWidth(r) != 0 {
			bad++
		}
	}
	if bad != 0 {
		t.Errorf("%d non-spacing marks still claim a cell", bad)
	}
}

// Ordinary text is unaffected: letters stay one column, wide glyphs two.
func TestOrdinaryRunesKeepTheirWidth(t *testing.T) {
	cases := map[rune]int{
		'a': 1, ' ': 1, '9': 1,
		0x65E5:  2, // CJK
		0x25CC:  1, // dotted circle (ambiguous -> narrow)
		0x05D0:  1, // hebrew alef
		0x0F40:  1, // tibetan ka
		0x1F642: 2, // emoji
	}
	for r, want := range cases {
		if got := cellRuneWidth(r); got != want {
			t.Errorf("U+%04X measures %d columns, want %d", r, got, want)
		}
	}
}
