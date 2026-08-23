package trinkets

import (
	"strings"
	"testing"
)

// A cell's combining marks must ride the base inside the kept segment for every
// join position — not only the medial (bookended) form — or the harakat vanish
// in the gfx path (drawCellText renders actx.s, the shaping window). Regression
// for missing vowels in SDL Arabic.
func TestArabicRenderContextCarriesMarks(t *testing.T) {
	const fatha = 'َ'
	base := 'ب' // beh (dual-joining), form irrelevant to the window's seq
	cases := []struct {
		name         string
		kashL, kashR bool
	}{
		{"isolated", false, false},
		{"initial", true, false},
		{"final", false, true},
		{"medial", true, true},
	}
	for _, c := range cases {
		actx := arabicRenderContext(base, base, 'ك', 'ل', c.kashL, c.kashR, []rune{fatha})
		// The mark is present in the window…
		if !strings.ContainsRune(actx.s, fatha) {
			t.Fatalf("%s: window %q missing the fatha", c.name, actx.s)
		}
		// …and inside the KEPT base segment [seg0,seg1), not the cropped tatweel/
		// neighbour region.
		win := []rune(actx.s)
		found := false
		for i := actx.seg0; i < actx.seg1 && i < len(win); i++ {
			if win[i] == fatha {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: fatha not in kept segment [%d,%d) of %q", c.name, actx.seg0, actx.seg1, actx.s)
		}
	}
}
