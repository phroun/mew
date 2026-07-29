package render

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/textwidth"
)

// A combining mark with no base is shown ANCHORED on a dotted circle in the
// substitutes color: the reader sees the real mark, composed exactly as a
// shaper composes a defective cluster, in one cell mew and the terminal agree
// on. Marks mew has no glyph for get no anchor \u2014 the circle would not save
// them from a spacing .notdef \u2014 so those keep the hex form.
func TestNoBaseMarkAnchorsOnDottedCircle(t *testing.T) {
	sr, w := testRenderer()

	// Latin acute opening a line: anchored.
	out := stripAnsi(sr.prepareLineForDisplay("\u0301abc", "\n", 20, 0, w, 0, selectionRange{}, nil, nil))
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

	// Hebrew niqqud opening a line: mew draws Hebrew, so it anchors too.
	out = stripAnsi(sr.prepareLineForDisplay("\u05b0x", "\n", 20, 0, w, 0, selectionRange{}, nil, nil))
	if !strings.ContainsRune(out, textwidth.MarkAnchor) {
		t.Fatalf("a baseless niqqud should anchor; got %q", out)
	}

	// NKo tone opening a line: no glyph in mew\u0027s faces, so hex instead.
	out = stripAnsi(sr.prepareLineForDisplay(nkoTone+"abc", "\n", 20, 0, w, 0, selectionRange{}, nil, nil))
	if strings.ContainsRune(out, textwidth.MarkAnchor) {
		t.Fatalf("an undrawable mark must not be anchored; got %q", out)
	}
	if !strings.Contains(out, "07ED") {
		t.Fatalf("an undrawable baseless mark should show its codepoint; got %q", out)
	}
}

// The width model agrees with the anchored paint.
func TestAnchoredMarkWidth(t *testing.T) {
	sr, w := testRenderer()
	runes := []rune("\u0301abc")
	col := 0
	for i := range runes {
		col += sr.runeWidthAt(runes, i, col, w)
	}
	if col != 4 {
		t.Fatalf("width model says %d columns, paint produces 4", col)
	}
}
