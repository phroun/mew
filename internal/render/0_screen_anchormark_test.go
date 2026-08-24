package render

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/textwidth"
)

// A combining mark with no base is shown ANCHORED on a dotted circle in the
// substitutes color: the reader sees the real mark, composed exactly as a
// shaper composes a defective cluster, in one cell mew and the terminal agree
// on.
func TestNoBaseMarkAnchorsOnDottedCircle(t *testing.T) {
	sr, w := testRenderer()

	// Latin acute opening a line.
	out := stripAnsi(sr.prepareLineForDisplay("́abc", "\n", 20, 0, w, 0, selectionRange{}, nil, nil))
	if !strings.ContainsRune(out, textwidth.MarkAnchor) {
		t.Fatalf("a baseless acute should anchor on a dotted circle; got %q", out)
	}
	if !strings.ContainsRune(out, 0x0301) {
		t.Fatalf("the mark itself should still be painted; got %q", out)
	}
	// One cell for the anchored pair, then "abc" = 4 columns.
	if got := termCols(strings.TrimRight(out, " ")); got != 4 {
		t.Fatalf("anchored row measures %d columns, want 4 (out=%q)", got, out)
	}

	// Hebrew niqqud opening a line.
	out = stripAnsi(sr.prepareLineForDisplay("ְx", "\n", 20, 0, w, 0, selectionRange{}, nil, nil))
	if !strings.ContainsRune(out, textwidth.MarkAnchor) {
		t.Fatalf("a baseless niqqud should anchor; got %q", out)
	}

	// An NKo tone opening a line anchors too: which font can draw it is the
	// host terminal's business, or the user's font config — never something
	// mew's bundled set can answer.
	out = stripAnsi(sr.prepareLineForDisplay(nkoTone+"abc", "\n", 20, 0, w, 0, selectionRange{}, nil, nil))
	if !strings.ContainsRune(out, textwidth.MarkAnchor) {
		t.Fatalf("a baseless NKo tone should anchor; got %q", out)
	}
	if strings.Contains(out, "07ED") {
		t.Fatalf("it should show the mark, not its codepoint; got %q", out)
	}
}

// The width model agrees with the anchored paint.
func TestAnchoredMarkWidth(t *testing.T) {
	sr, w := testRenderer()
	runes := []rune("́abc")
	col := 0
	for i := range runes {
		col += sr.runeWidthAt(runes, i, col, w)
	}
	if col != 4 {
		t.Fatalf("width model says %d columns, paint produces 4", col)
	}
}

// A script-specific mark stranded on a base of another script is lifted onto
// a dotted circle too: it has no legitimate carrier where it sits, and no
// shaper would compose the pairing.
func TestMixedScriptMarkAnchors(t *testing.T) {
	sr, w := testRenderer()

	// Hebrew accent on a CJK ideograph.
	out := stripAnsi(sr.prepareLineForDisplay("日֗k", "\n", 20, 0, w, 0, selectionRange{}, nil, nil))
	if !strings.ContainsRune(out, textwidth.MarkAnchor) {
		t.Fatalf("a hebrew accent on a CJK base should anchor; got %q", out)
	}
	if strings.Contains(out, "0597") {
		t.Fatalf("it should show the mark, not its codepoint; got %q", out)
	}
	// CJK (2) + anchored accent (1) + "k" (1) = 4 columns.
	if got := termCols(strings.TrimRight(out, " ")); got != 4 {
		t.Fatalf("row measures %d columns, want 4 (out=%q)", got, out)
	}

	// Niqqud on a Latin letter.
	out = stripAnsi(sr.prepareLineForDisplay("xְy", "\n", 20, 0, w, 0, selectionRange{}, nil, nil))
	if !strings.ContainsRune(out, textwidth.MarkAnchor) {
		t.Fatalf("niqqud on a Latin base should anchor; got %q", out)
	}

	// Well-formed pointed Hebrew is untouched: same script on both sides.
	out = stripAnsi(sr.prepareLineForDisplay("אְב", "\n", 20, 0, w, 0, selectionRange{}, nil, nil))
	if strings.ContainsRune(out, textwidth.MarkAnchor) {
		t.Fatalf("well-formed pointed hebrew must not be anchored; got %q", out)
	}
	if got := termCols(strings.TrimRight(out, " ")); got != 2 {
		t.Fatalf("pointed hebrew measures %d columns, want 2 (out=%q)", got, out)
	}
}

// A Tibetan vowel sign — a script mew ships no face for — anchors on a foreign
// base like any other ill-formed sequence, and is left ALONE on a Tibetan
// base, where the sequence is well-formed and the user's font decides.
func TestTibetanMarkAnchorsOnlyWhenIllFormed(t *testing.T) {
	sr, w := testRenderer()

	out := stripAnsi(sr.prepareLineForDisplay("xཱy", "\n", 20, 0, w, 0, selectionRange{}, nil, nil))
	if !strings.ContainsRune(out, textwidth.MarkAnchor) {
		t.Fatalf("a tibetan vowel on a Latin base should anchor; got %q", out)
	}
	if strings.Contains(out, "0F71") {
		t.Fatalf("it should show the mark, not its codepoint; got %q", out)
	}

	// Well-formed Tibetan: untouched, zero-width, no circle.
	out = stripAnsi(sr.prepareLineForDisplay("ཀཱ", "\n", 20, 0, w, 0, selectionRange{}, nil, nil))
	if strings.ContainsRune(out, textwidth.MarkAnchor) {
		t.Fatalf("well-formed tibetan must not be anchored; got %q", out)
	}
	if got := termCols(strings.TrimRight(out, " ")); got != 1 {
		t.Fatalf("well-formed tibetan measures %d columns, want 1 (out=%q)", got, out)
	}
}

// SPACING combining marks (category Mc) take a cell of their own, so an
// anchored one is TWO columns — the circle plus the mark. Budgeting one would
// overrun the row, which is the whole failure this classification prevents.
func TestAnchoredSpacingMarkTakesTwoCells(t *testing.T) {
	sr, w := testRenderer()
	const khmerAA = "ា" // KHMER VOWEL SIGN AA, category Mc
	if textwidth.Rune([]rune(khmerAA)[0]) == 0 {
		t.Skip("this mark is not spacing in the current width table")
	}
	out := stripAnsi(sr.prepareLineForDisplay("x"+khmerAA+"y", "\n", 20, 0, w, 0, selectionRange{}, nil, nil))
	if !strings.ContainsRune(out, textwidth.MarkAnchor) {
		t.Fatalf("a khmer vowel on a Latin base should anchor; got %q", out)
	}
	// "x" + (circle + spacing mark) + "y" = 4 columns.
	if got := termCols(strings.TrimRight(out, " ")); got != 4 {
		t.Fatalf("row measures %d columns, want 4 (out=%q)", got, out)
	}
	runes := []rune("x" + khmerAA + "y")
	col := 0
	for i := range runes {
		col += sr.runeWidthAt(runes, i, col, w)
	}
	if col != 4 {
		t.Fatalf("width model says %d columns, paint produces 4", col)
	}
}

// Hex substitutes are bracketed once they exceed one byte, so a run of them
// cannot read as a single long number. One byte stands bare.
func TestHexSubstituteBracketing(t *testing.T) {
	cases := []struct {
		r    rune
		want string
	}{
		{0x01, "^A"}, // C0 keeps the caret form
		{0x00, "^@"}, // NUL
		{0x1B, "^["}, // ESC
		{0x7F, "7F"}, // DEL: one byte, bare
		{0x94, "94"}, // C1: one byte, bare
		{0x9D, "9D"}, // C1 OSC
		{0x07ED, "(07ED)"},
		{0x0F71, "(0F71)"},
		{0x10FFF, "(010FFF)"},
	}
	for _, c := range cases {
		if got := runeToHexOrCtrl(c.r); got != c.want {
			t.Errorf("runeToHexOrCtrl(U+%04X) = %q, want %q", c.r, got, c.want)
		}
		// The width model measures the substitute by its own text, so the
		// brackets are counted.
		if got := substituteWidth(runeToHexOrCtrl(c.r)); got != len([]rune(c.want)) {
			t.Errorf("width of U+%04X substitute = %d, want %d", c.r, got, len([]rune(c.want)))
		}
	}
}
