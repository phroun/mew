package render

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/textwidth"
	"github.com/phroun/mew/internal/viewport"
)

const nkoTone = "߭" // NKo combining short rising tone

// termCols is what a real terminal advances for a rendered row.
func termCols(s string) int {
	total := 0
	for _, r := range s {
		total += textwidth.Rune(r)
	}
	return total
}

// A mark mew cannot render is classified like a control code: painted as its
// hex codepoint in the substitutes color, with a width both mew and the
// terminal agree on — instead of being emitted raw as zero-width, which a
// terminal answers with a spacing fallback glyph that pushes the rest of the
// row off the window's edge.
func TestDefectiveMarkRendersAsSubstitute(t *testing.T) {
	sr, w := testRenderer()
	out := stripAnsi(sr.prepareLineForDisplay("a"+nkoTone+"b", "\n", 20, 0, w, 0, selectionRange{}, nil, nil))
	if !strings.Contains(out, "07ED") {
		t.Fatalf("defective mark should paint as its hex codepoint; got %q", out)
	}
	if strings.ContainsRune(out, '߭') {
		t.Fatalf("the raw mark must not reach the terminal; got %q", out)
	}
	// It is measured, not free: "a" + "07ED" + "b" = 6 columns of content.
	if got := termCols(strings.TrimRight(out, " ")); got != 6 {
		t.Fatalf("substituted row measures %d columns, want 6 (out=%q)", got, out)
	}
}

// The width model and the painted row must agree, or the caret drifts.
func TestDefectiveMarkWidthMatchesPaint(t *testing.T) {
	sr, w := testRenderer()
	runes := []rune("a" + nkoTone + "b")
	col := 0
	for i := range runes {
		col += sr.runeWidthAt(runes, i, col, w)
	}
	if col != 6 {
		t.Fatalf("width model says %d columns, paint produces 6", col)
	}
	// A well-formed mark still measures zero.
	ok := []rune("é")
	if got := sr.runeWidthAt(ok, 1, 1, w); got != 0 {
		t.Fatalf("a well-formed combining acute measures %d, want 0", got)
	}
}

// Every row a viewport paints must occupy exactly the width it was given —
// the invariant a spacing fallback glyph breaks.
func TestDefectiveMarkRowsStayInBounds(t *testing.T) {
	line := "9 ." + nkoTone + "Q$;& &=G֣;y"
	for _, cfg := range []struct {
		name string
		mut  func(w *viewport.Viewport)
	}{
		{"default", func(w *viewport.Viewport) {}},
		{"invisibles", func(w *viewport.Viewport) { w.ViewState.ShowInvisibles = true }},
		{"rtl", func(w *viewport.Viewport) { w.ViewState.Direction = "rtl" }},
	} {
		for _, width := range []int{8, 12, 20, 40} {
			for voff := 0; voff < 12; voff++ {
				sr, w := testRenderer()
				cfg.mut(w)
				out := stripAnsi(sr.prepareLineForDisplay(line, "\n", width, voff, w, 0, selectionRange{}, nil, nil))
				if got := termCols(out); got != width {
					t.Errorf("%s width=%d voff=%d: row occupies %d columns (out=%q)",
						cfg.name, width, voff, got, out)
				}
			}
		}
	}
}
