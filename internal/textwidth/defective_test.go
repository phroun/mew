package textwidth

import "testing"

// DefectiveMark separates combining marks mew can safely paint as zero-width
// from those it cannot. The zero-width answer is a PROMISE that the terminal
// will compose the mark into the preceding cell and advance nothing; when mew
// has no glyph for the mark, or there is no base at all, the terminal breaks
// that promise with a spacing fallback and the row overruns its window.
func TestDefectiveMark(t *testing.T) {
	const (
		acute     = '́'      // combining acute (Inherited)
		niqqud    = 'ְ'      // hebrew point sheva
		hebAccnt  = '֗'      // hebrew accent revia
		harakat   = 'َ'      // arabic fatha
		nkoTone   = '\u07ed' // NKo combining short rising tone
		nkoLetter = '\u07ca' // NKo letter A
		tibetanA  = '\u0f71' // Tibetan vowel sign AA
		tibetanKa = '\u0f40' // Tibetan letter KA
		thaiMark  = '\u0e31' // Thai character mai han-akat
		dakuten   = '゙'      // combining katakana-hiragana voiced sound mark
	)
	cases := []struct {
		name string
		prev rune
		mark rune
		want bool
	}{
		// Well-formed, renderable: never touched.
		{"acute on latin", 'e', acute, false},
		{"acute on cyrillic", 'д', acute, false},
		{"niqqud on hebrew", 'א', niqqud, false},
		{"harakat on arabic", 'ب', harakat, false},
		{"dakuten on kana", 'か', dakuten, false},

		// Script-specific mark on a base of another script: ill-formed, and
		// no shaper will compose it, so the terminal falls back to a spacing
		// glyph. Defective regardless of whether a glyph for the mark exists.
		{"hebrew accent on cjk", '日', hebAccnt, true},
		{"hebrew accent on latin", 'G', hebAccnt, true},
		{"niqqud on latin", 'x', niqqud, true},
		{"hebrew accent on space", ' ', hebAccnt, true},

		// The Arabic vowel marks are Script=Inherited in Unicode — shared
		// with Syriac and others, script-neutral by definition — so the
		// mixed-script rule does not reach them. They ride any base.
		{"harakat on punctuation", '.', harakat, false},
		{"harakat on latin", 'x', harakat, false},

		// A script-specific mark on a foreign base, regardless of whether
		// any installed font can draw it.
		{"nko tone on period", '.', nkoTone, true},
		{"nko tone on latin", 'x', nkoTone, true},
		{"tibetan vowel on latin", 'x', tibetanA, true},
		{"thai mark on latin", 'x', thaiMark, true},

		// LEGITIMATE text in a script mew ships no face for: the sequence is
		// well-formed, so it is left alone to render with whatever font the
		// user has. Font inventory is never consulted (see DefectiveMark).
		{"nko tone on nko letter", nkoLetter, nkoTone, false},
		{"tibetan vowel on tibetan letter", tibetanKa, tibetanA, false},

		// Nothing to anchor onto.
		{"acute with no base", 0, acute, true},
		{"niqqud with no base", 0, niqqud, true},

		// Not a mark at all.
		{"plain letter", 'a', 'b', false},
		{"wide char", 'a', '日', false},
	}
	for _, c := range cases {
		if got := DefectiveMark(c.prev, c.mark); got != c.want {
			t.Errorf("%s: DefectiveMark(%q, U+%04X) = %v, want %v",
				c.name, string(c.prev), c.mark, got, c.want)
		}
	}
}

func TestIsMark(t *testing.T) {
	for _, r := range []rune{'́', 'ְ', '߭', '゙'} {
		if !IsMark(r) {
			t.Errorf("U+%04X should be a mark", r)
		}
	}
	for _, r := range []rune{'a', '日', ' ', '​'} {
		if IsMark(r) {
			t.Errorf("U+%04X should not be a mark", r)
		}
	}
}

// C1 controls (U+0080..U+009F) must be classified. They arrive as ordinary
// two-byte UTF-8 and no width table calls them special, but a terminal
// decoding UTF-8 honors them — and the range holds the string introducers
// (DCS/SOS/CSI/ST/OSC/PM/APC), any of which makes the terminal swallow the
// rest of the line.
func TestIsControl(t *testing.T) {
	for _, r := range []rune{0x00, 0x01, 0x0B, 0x1B, 0x1C, 0x1F, 0x7F} {
		if !IsControl(r) {
			t.Errorf("U+%04X (C0/DEL) should be a control", r)
		}
	}
	for _, r := range []rune{0x80, 0x90, 0x94, 0x9B, 0x9C, 0x9D, 0x9F} {
		if !IsControl(r) {
			t.Errorf("U+%04X (C1) should be a control", r)
		}
	}
	for _, r := range []rune{' ', 'a', 0x7E, 0xA0, 0xFF, 0x65E5, 0xFFFD} {
		if IsControl(r) {
			t.Errorf("U+%04X should not be a control", r)
		}
	}
}
