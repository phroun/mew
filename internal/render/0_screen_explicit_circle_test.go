package render

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/textwidth"
)

// An explicitly typed dotted circle followed by a mark is a well-formed cluster:
// the mark rides the user's circle (one cell), not lifted onto a SECOND
// substitute circle. If it were re-anchored we'd see two circles across three
// columns (typed ◌, then anchored ◌+mark, then x); here it is one circle, two
// columns (◌+mark as one cell, then x).
func TestExplicitDottedCircleIsABase(t *testing.T) {
	sr, w := testRenderer()
	const holam = "ֹ" // U+05B9
	out := stripAnsi(sr.prepareLineForDisplay("◌"+holam+"x", "\n", 20, 0, w, 0, selectionRange{}, nil, nil))

	if n := strings.Count(out, string(textwidth.MarkAnchor)); n != 1 {
		t.Fatalf("want exactly one dotted circle (the typed one), got %d in %q", n, out)
	}
	if got := termCols(strings.TrimRight(out, " ")); got != 2 {
		t.Fatalf("explicit circle+mark should be one cell + x = 2 cols, got %d (%q)", got, out)
	}
	// The mark still rides the previous cell (the cluster is not defective).
	runes := []rune("◌" + holam + "x")
	if textwidth.DefectiveMark(textwidth.PrevBase(runes, 1), runes[1]) {
		t.Fatalf("a mark on an explicit dotted circle must not be defective")
	}
}
