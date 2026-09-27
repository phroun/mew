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

// A line of text is a grid row, so it is UnitsPerCellHeight units -- the
// denomination's answer, not the font's. Point size sets how big the cell is
// on the glass; it does not change how many units divide it.
func TestLineUnitsIsTheDenominationNotThePointSize(t *testing.T) {
	base := &Font{Name: "ui-text", Size: 12}
	huge := &Font{Name: "ui-text", Size: 48}

	for _, rowUnits := range []Unit{1, 8, 16, 32} {
		m := CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: rowUnits}
		if got := LineUnits(base, base, m); got != rowUnits {
			t.Errorf("row_units=%d: a line of the surface's own face = %d, want %d",
				rowUnits, got, rowUnits)
		}
		// The surface laid out in the same face it renders: still one row.
		if got := LineUnits(huge, huge, m); got != rowUnits {
			t.Errorf("row_units=%d: 48pt everywhere = %d units, want one row (%d)",
				rowUnits, got, rowUnits)
		}
		if got := LineUnits(nil, base, m); got != rowUnits {
			t.Errorf("row_units=%d: no face named = %d, want one row (%d)",
				rowUnits, got, rowUnits)
		}
	}
}

// A face deliberately off the surface's own size -- a 75% separator caption,
// an 80% menu shortcut -- fills that fraction of the row, so the surface it
// sits in still governs the scale.
func TestLineUnitsScalesAFaceThatDiffersFromTheSurface(t *testing.T) {
	base := &Font{Name: "ui-text", Size: 12}
	caption := &Font{Name: "ui-text", Size: 9} // 75%

	for _, tc := range []struct{ rowUnits, want Unit }{{8, 6}, {16, 12}, {32, 24}} {
		m := CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: tc.rowUnits}
		if got := LineUnits(caption, base, m); got != tc.want {
			t.Errorf("row_units=%d: 75%% caption line = %d, want %d", tc.rowUnits, got, tc.want)
		}
	}
}
