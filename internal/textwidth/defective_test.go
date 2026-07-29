package textwidth

import "testing"

// DefectiveMark separates combining marks mew can safely paint as zero-width
// from those it cannot. The zero-width answer is a PROMISE that the terminal
// will compose the mark into the preceding cell and advance nothing; when mew
// has no glyph for the mark, or there is no base at all, the terminal breaks
// that promise with a spacing fallback and the row overruns its window.
func TestDefectiveMark(t *testing.T) {
	const (
		acute    = '́' // combining acute (Inherited)
		niqqud   = 'ְ' // hebrew point sheva
		hebAccnt = '֗' // hebrew accent revia
		harakat  = 'َ' // arabic fatha
		nkoTone  = '߭' // NKo combining short rising tone
		dakuten  = '゙' // combining katakana-hiragana voiced sound mark
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

		// Ill-formed pairing but a mark mew HAS a glyph for: still
		// zero-width, matching wcwidth (see the doc comment).
		{"hebrew accent on cjk", '日', hebAccnt, false},
		{"hebrew accent on latin", 'G', hebAccnt, false},

		// A mark from a script mew cannot render, anywhere.
		{"nko tone on period", '.', nkoTone, true},
		{"nko tone on latin", 'x', nkoTone, true},

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
