package trinkets

import "testing"

// السلام stored in visual order (RTL reversed into cells): each cell resolves
// to its contextual presentation form, with the mandatory lam-alef ligature
// drawn in the lam's cell and its alef suppressed.
func TestShapeArabicVisualSalaam(t *testing.T) {
	// Logical ا ل س ل ا م -> visual cells left-to-right: م ا ل س ل ا
	cells := []rune{0x0645, 0x0627, 0x0644, 0x0633, 0x0644, 0x0627}
	at := func(i int) rune {
		if i < 0 || i >= len(cells) {
			return 0
		}
		return cells[i]
	}
	want := []struct {
		glyph    rune
		suppress bool
	}{
		{0xFEE1, false}, // م isolated (the alef before it does not join forward)
		{0, true},       // ا of lam-alef: suppressed
		{0xFEFC, false}, // ل + ا -> lam-alef ligature, final (joined to س)
		{0xFEB4, false}, // س medial
		{0xFEDF, false}, // ل initial
		{0xFE8D, false}, // ا isolated (word-initial, nothing joins into it)
	}
	for i := range cells {
		g, s := shapeArabicVisual(at(i-1), cells[i], at(i+1))
		if g != want[i].glyph || s != want[i].suppress {
			t.Errorf("cell %d (%U): got %U suppress=%v, want %U suppress=%v",
				i, cells[i], g, s, want[i].glyph, want[i].suppress)
		}
	}
}

// Non-Arabic content passes through untouched.
func TestShapeArabicVisualPassthrough(t *testing.T) {
	for _, r := range []rune{'a', 'ש', '日', ' ', 0} {
		if g, s := shapeArabicVisual('x', r, 'y'); g != r || s {
			t.Errorf("%U should pass through, got %U suppress=%v", r, g, s)
		}
	}
}
