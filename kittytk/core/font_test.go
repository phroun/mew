package core

import "testing"

// MeasureRunes counts UNITS, not pixels: a rune is one cell of the
// default denomination (8 units; 16 for the double-width Tuesday face)
// regardless of font size. font_size scales the pixel size of a unit,
// not the number of units per character, so the unit count is invariant.
func TestMeasureRunesIsFontSizeInvariant(t *testing.T) {
	cases := []struct {
		name    string
		size    int
		perRune Unit // expected width of a single rune, in units
	}{
		{"ui-text", 12, 8},
		{"ui-text", 6, 8},
		{"ui-text", 18, 8},
		{"ui-text", 24, 8},
		{"Tuesday", 12, 16}, // double-width demo face
		{"Tuesday", 6, 16},
	}
	for _, tc := range cases {
		f := &Font{Name: tc.name, Size: tc.size}
		if got := f.MeasureRunes(1); got != tc.perRune {
			t.Errorf("%s@%dpt: per-rune = %d, want %d", tc.name, tc.size, got, tc.perRune)
		}
		// Linear in the rune count.
		if got := f.MeasureRunes(30); got != 30*tc.perRune {
			t.Errorf("%s@%dpt: MeasureRunes(30) = %d, want %d", tc.name, tc.size, got, 30*tc.perRune)
		}
	}
}
