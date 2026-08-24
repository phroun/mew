package render

import (
	"testing"
)

// s builds a string from explicit codepoints — the precomposed presentation form
// and the bare base+mark cluster look identical but are different bytes, so the
// expectations must be spelled out by codepoint.
func s(rs ...rune) string { return string(rs) }

// Under rtlMarkMode "iterm2" on a plain terminal, a Hebrew base + dagesh/dot/rafe
// is emitted as the single Alphabetic-Presentation-Form glyph that bakes the
// point in, so no free-standing point is left for iTerm2 to drift. Vowels ride on
// top the usual way; a dagesh with no presentation form is dropped.
func TestPrecombineHebrewForITerm2(t *testing.T) {
	const (
		bet, shin, alef, he, vav, het = 0x05D1, 0x05E9, 0x05D0, 0x05D4, 0x05D5, 0x05D7
		dagesh, shinDot, sinDot, rafe = 0x05BC, 0x05C1, 0x05C2, 0x05BF
		qamats, holamHaser            = 0x05B8, 0x05BA
	)
	cases := []struct {
		name  string
		runes []rune
		want  string
		ok    bool
	}{
		{"bet+dagesh", []rune{bet, dagesh}, s(0xFB31), true},
		{"bet+dagesh+qamats", []rune{bet, dagesh, qamats}, s(0xFB31, qamats), true},
		{"shin+shindot", []rune{shin, shinDot}, s(0xFB2A), true},
		{"shin+sindot", []rune{shin, sinDot}, s(0xFB2B), true},
		{"shin+dagesh+shindot", []rune{shin, dagesh, shinDot}, s(0xFB2C), true},
		{"shin+dagesh+sindot", []rune{shin, dagesh, sinDot}, s(0xFB2D), true},
		{"shin+dagesh(bare)", []rune{shin, dagesh}, s(0xFB49), true},
		{"alef+mapiq", []rune{alef, dagesh}, s(0xFB30), true},
		{"he+mapiq", []rune{he, dagesh}, s(0xFB34), true},
		{"vav+dagesh(shuruk)", []rune{vav, dagesh}, s(0xFB35), true},
		{"bet+rafe", []rune{bet, rafe}, s(0xFB4C), true},
		{"vav+holam-haser", []rune{vav, holamHaser}, s(0xFB4B), true},
		// holam haser is only meaningful on vav; elsewhere it rides as a vowel.
		{"bet+holam-haser stays vowel", []rune{bet, holamHaser}, "", false},
		// no presentation form for het+dagesh -> dagesh dropped, bare letter kept.
		{"het+dagesh omits", []rune{het, dagesh}, s(het), true},
		{"het+dagesh+qamats keeps vowel", []rune{het, dagesh, qamats}, s(het, qamats), true},
		// nothing to precombine -> unchanged (false).
		{"bet+qamats only", []rune{bet, qamats}, "", false},
		{"bare letter", []rune{bet}, "", false},
		{"latin base", []rune{'a', dagesh}, "", false},
	}
	for _, c := range cases {
		got, ok := precomposeCell(bbCell{runes: c.runes})
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%s: got (%q,%v), want (%q,%v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

// End to end through the cell emitter: iterm2 mode on a plain terminal
// precombines; normal mode and the flex-width host emit the bare cluster.
func TestEmitCellTextPrecombine(t *testing.T) {
	cell := bbCell{runes: []rune{0x05D1, 0x05BC}, width: 1} // bet + dagesh
	composed := s(0xFB31)
	bare := s(0x05D1, 0x05BC)

	b := &backBuffer{rtlMarkMode: "iterm2"} // plain terminal (logicalCUP=false)
	if got := b.emitCellText(cell); got != composed {
		t.Errorf("iterm2 plain: got %q, want the precomposed FB31 %q", got, composed)
	}
	bNorm := &backBuffer{rtlMarkMode: "normal"}
	if got := bNorm.emitCellText(cell); got != bare {
		t.Errorf("normal: got %q, want the bare cluster %q", got, bare)
	}
	bFlex := &backBuffer{rtlMarkMode: "iterm2", logicalCUP: true}
	if got := bFlex.emitCellText(cell); got != bare {
		t.Errorf("flex host: got %q, want the bare cluster %q", got, bare)
	}
}
