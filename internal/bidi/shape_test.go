package bidi

import (
	"fmt"
	"testing"
)

// "سلام" (seen-lam-alef-meem) shapes to seen-initial, the lam-alef LIGATURE
// (final form, since the lam joins the seen), its absorbed alef, then an
// isolated meem (the alef terminates joining before meem).
func TestShapeSalaam(t *testing.T) {
	in := []rune("سلام") // 0x633 0x644 0x627 0x645
	got := Shape(in)
	want := []rune{0xFEB3, 0xFEFC, LigatureAbsorbed, 0xFEE1}
	if got == nil {
		t.Fatal("expected shaping")
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rune %d: %v, want %v (seen=FEB3 lam-alef=FEFC absorbed meem=FEE1)",
				i, fmt.Sprintf("U+%04X", got[i]), fmt.Sprintf("U+%04X", want[i]))
		}
	}
}

// Lam-alef ligatures: isolated FEFB when the lam stands alone, final FEFC
// when it joins a preceding letter; the alef is absorbed either way. The
// hamza/madda alef variants pick their own ligature glyphs.
func TestShapeLamAlefLigature(t *testing.T) {
	cases := []struct {
		in   string
		lig  rune
		lamI int
	}{
		{"لا", 0xFEFB, 0},  // isolated
		{"بلا", 0xFEFC, 1}, // final (joins beh)
		{"لأ", 0xFEF7, 0},  // lam + alef-hamza-above
		{"لآ", 0xFEF5, 0},  // lam + alef-madda
	}
	for _, c := range cases {
		g := Shape([]rune(c.in))
		if g[c.lamI] != c.lig {
			t.Fatalf("%q: lam glyph U+%04X, want U+%04X", c.in, g[c.lamI], c.lig)
		}
		if g[c.lamI+1] != LigatureAbsorbed {
			t.Fatalf("%q: alef should be absorbed, got U+%04X", c.in, g[c.lamI+1])
		}
	}
}

// "الله" is alef-lam-lam-heh — the alef precedes the lam (not lam-alef), so
// no ligature: standalone alef, then a connected لله. (The calligraphic
// Allah ligature is a font feature we do not synthesize.)
func TestShapeAllahNoLigature(t *testing.T) {
	g := Shape([]rune("الله"))
	want := []rune{0xFE8D, 0xFEDF, 0xFEE0, 0xFEEA}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("الله rune %d: U+%04X, want U+%04X", i, g[i], want[i])
		}
	}
}

// Persian/Urdu letters shape too: gaf and farsi yeh take their FBxx forms.
func TestShapePersian(t *testing.T) {
	g := Shape([]rune("گل")) // gaf + lam
	if g[0] != 0xFB94 {      // gaf initial
		t.Fatalf("gaf initial U+%04X, want FB94", g[0])
	}
	g = Shape([]rune("یک")) // farsi yeh + keheh
	if g[0] != 0xFBFE {     // farsi yeh initial
		t.Fatalf("farsi yeh initial U+%04X, want FBFE", g[0])
	}
}

// A single letter stays isolated; a two-letter word is initial+final.
func TestShapeForms(t *testing.T) {
	if g := Shape([]rune("ب")); g[0] != 0xFE8F {
		t.Fatalf("lone beh should be isolated FE8F, got U+%04X", g[0])
	}
	// "بب": initial + final
	g := Shape([]rune("بب"))
	if g[0] != 0xFE91 || g[1] != 0xFE90 {
		t.Fatalf("bb -> U+%04X U+%04X, want FE91 FE90", g[0], g[1])
	}
	// "ببب": initial + medial + final
	g = Shape([]rune("ببب"))
	if g[0] != 0xFE91 || g[1] != 0xFE92 || g[2] != 0xFE90 {
		t.Fatalf("bbb -> %04X %04X %04X, want FE91 FE92 FE90", g[0], g[1], g[2])
	}
}

// A combining harakat between letters is transparent to the join context.
func TestShapeTransparentMark(t *testing.T) {
	// beh + fatha + beh: the two beh still join through the mark.
	in := []rune("بَب") // 0x628 0x64E 0x628
	g := Shape(in)
	if g[0] != 0xFE91 { // initial beh
		t.Fatalf("beh before mark should be initial FE91, got U+%04X", g[0])
	}
	if g[1] != 0x064E { // fatha unchanged
		t.Fatalf("mark should pass through, got U+%04X", g[1])
	}
	if g[2] != 0xFE90 { // final beh
		t.Fatalf("beh after mark should be final FE90, got U+%04X", g[2])
	}
}

// Non-Arabic lines shape to nil (fast path).
func TestShapeNoArabic(t *testing.T) {
	if Shape([]rune("hello שלום")) != nil {
		t.Fatal("no Arabic should return nil")
	}
}
