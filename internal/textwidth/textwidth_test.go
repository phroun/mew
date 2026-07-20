package textwidth

import "testing"

// Bidi format controls advance no cells. go-runewidth already zeroes the older
// ones; the point of this test is the runes its table misses — ALM (U+061C) and
// the Unicode 6.3 isolates (U+2066..U+2069), the latter being what the
// browse-mode button substitution wraps around every button.
func TestBidiControlsAreZeroWidth(t *testing.T) {
	controls := []rune{
		0x200E, 0x200F, 0x061C, // LRM, RLM, ALM
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E, // LRE, RLE, PDF, LRO, RLO
		0x2066, 0x2067, 0x2068, 0x2069, // LRI, RLI, FSI, PDI
	}
	for _, r := range controls {
		if w := Rune(r); w != 0 {
			t.Errorf("Rune(U+%04X) = %d, want 0", r, w)
		}
	}
}

// Ordinary and wide runes are unaffected by the control-zeroing.
func TestOrdinaryWidthsUnchanged(t *testing.T) {
	cases := map[rune]int{
		'a':    1,
		' ':    1,
		'世':    2, // fullwidth CJK
		0x0301: 0, // combining acute (Mn)
		'​':    0, // zero-width space
	}
	for r, want := range cases {
		if got := Rune(r); got != want {
			t.Errorf("Rune(U+%04X) = %d, want %d", r, got, want)
		}
	}
}
