package render

import (
	"strings"
	"testing"
)

// The phantom column (ViewOffsetX -1) is a cell the caret follow opens beyond
// the content's leading edge so a caret that would otherwise sit off-screen has
// somewhere to land. It holds NO data, so a selection must never tint it — a
// highlighted space there reads as a selected character the line does not
// contain.
//
// Under rtl the pad shortens and the phantom opens at the RIGHT edge, which is
// exactly where a multi-line selection's trailing padding lands (the bar that
// carries the selected newline across the rest of the row).
func TestPhantomColumnNeverSelectedRTL(t *testing.T) {
	sr, w := testRenderer()
	w.ViewState.Direction = "rtl"
	textColor := sr.col(w, "text")
	selectionColor := sr.col(w, "selection")
	resetColor := sr.col(w, "reset")

	// docLine 1 sits strictly inside the selection, so its padding carries the
	// bar; the row is 12 columns and "abc" leaves exactly the phantom cell.
	sel := selectionRange{exists: true, startLine: 0, startRune: 0, endLine: 2, endRune: 1}
	out := sr.prepareLineForDisplay("abc", "\n", 12, -1, w, 1, sel, nil, nil)

	if plain := stripAnsi(out); len([]rune(plain)) != 12 {
		t.Fatalf("row is %d columns, want 12 (%q)", len([]rune(plain)), plain)
	}
	if !strings.Contains(out, selectionColor) {
		t.Fatalf("expected the selection bar somewhere in the row: %q", out)
	}
	if !strings.HasSuffix(out, textColor+" "+resetColor) {
		t.Errorf("the phantom column (last cell under rtl) must be plain text color, got %q", out)
	}
}

// The ltr phantom opens at column 0, written before any content — nothing may
// colour it but the plain text color.
func TestPhantomColumnNeverSelectedLTR(t *testing.T) {
	sr, w := testRenderer()
	w.ViewState.Direction = "ltr"
	textColor := sr.col(w, "text")
	selectionColor := sr.col(w, "selection")

	sel := selectionRange{exists: true, startLine: 0, startRune: 0, endLine: 2, endRune: 1}
	out := sr.prepareLineForDisplay("abc", "\n", 12, -1, w, 1, sel, nil, nil)

	if plain := stripAnsi(out); len([]rune(plain)) != 12 {
		t.Fatalf("row is %d columns, want 12 (%q)", len([]rune(plain)), plain)
	}
	// Everything emitted up to and including the first painted cell.
	head := out
	if i := strings.Index(stripAnsi(out), " "); i == 0 {
		// The phantom is that first cell: find where it ends in the raw row.
		for j := range out {
			if stripAnsi(out[:j]) == " " {
				head = out[:j]
				break
			}
		}
	}
	if strings.Contains(head, selectionColor) {
		t.Errorf("the phantom column (column 0 under ltr) must be plain text color, got %q", head)
	}
	if !strings.Contains(head, textColor) {
		t.Errorf("the phantom column should carry the text color, got %q", head)
	}
}
