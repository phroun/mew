package render

import (
	"testing"

	"github.com/phroun/mew/internal/bidi"
)

// A Collapse span (a button): display text, forced-color cells, and the
// doc<->display maps that keep the caret and selection honest. "ab[[x]]cd" with
// [2,7) replaced by "<X>" + shadow. The button is bracketed by an FSI/PDI
// isolate pair (zero-width chrome cells) so it stays atomic under bidi.
func TestSubstituteButtonMapping(t *testing.T) {
	doc := []rune("ab[[x]]cd")
	span := ButtonSpan{
		Start: 2, End: 7, Runes: []rune("<X>"), Shadow: '█',
		Color: "B", ShadowColor: "S",
	}.Span()
	d := substituteSpans(doc, []DisplaySpan{span}, false)
	if d == nil {
		t.Fatal("substitution expected")
	}
	// display cells: a b FSI < X > █ PDI c d
	if d.Text != "ab⁨<X>█⁩cd" {
		t.Fatalf("display text = %q", d.Text)
	}
	// Doc boundaries: 0,1 unchanged; 2..6 (inside a Collapse span) park at the
	// button start — the FSI cell (display 2); 7 (span end) lands after the PDI
	// (display 8); 8,9.
	want := []int{0, 1, 2, 2, 2, 2, 2, 8, 9, 10}
	for p, wd := range want {
		if d.DocToDisp[p] != wd {
			t.Fatalf("DocToDisp[%d] = %d, want %d", p, d.DocToDisp[p], wd)
		}
	}
	// Content doc runes only reappear after the isolate: display 8,9 -> doc 7,8;
	// every chrome/isolate cell between is -1.
	wantDoc := []int{0, 1, -1, -1, -1, -1, -1, -1, 7, 8}
	for i, wd := range wantDoc {
		if d.DispToDoc[i] != wd {
			t.Fatalf("DispToDoc[%d] = %d, want %d", i, d.DispToDoc[i], wd)
		}
	}
	// FSI (2) and a cap (3) take the button color; the shadow (6) its own; the
	// leading doc cell (0) none.
	if d.Forced[3] != "B" || d.Forced[6] != "S" || d.Forced[0] != "" {
		t.Fatalf("forced colors wrong: %q", d.Forced)
	}
}

// The isolate wrapping does its job: a button set among RTL text under an RTL
// base keeps its caps and title in left-to-right logical order, instead of
// being reversed into the surrounding run (which is what happens without the
// FSI/PDI pair — the un-isolated brackets resolve to R and flip).
func TestButtonBidiIsolation(t *testing.T) {
	he := []rune("אב") // strong-RTL neighbours on both sides
	// doc: <he> LINK <he>, LINK occupying doc [2,4).
	doc := append([]rune{}, he...)
	doc = append(doc, 'X', 'X') // placeholder runes the button replaces
	doc = append(doc, he...)
	span := ButtonSpan{Start: 2, End: 4, Runes: []rune("[L]"), Color: "B"}.Span()
	d := substituteSpans(doc, []DisplaySpan{span}, false)
	if d == nil {
		t.Fatal("substitution expected")
	}
	layout := bidi.Compute(d.Runes, true) // base RTL
	if layout == nil {
		t.Fatal("mixed-direction line should need layout")
	}
	// Visual slot of each button glyph in logical order.
	slotOf := func(r rune) int {
		for vslot, log := range layout.Perm {
			if d.Runes[log] == r {
				return vslot
			}
		}
		return -1
	}
	l, mid, rgt := slotOf('['), slotOf('L'), slotOf(']')
	if l < 0 || mid < 0 || rgt < 0 {
		t.Fatalf("button glyphs missing from layout: %d %d %d", l, mid, rgt)
	}
	if !(l < mid && mid < rgt) {
		t.Fatalf("isolated button must keep left-to-right order; got slots [%d]<L>%d<]>%d", l, mid, rgt)
	}
}

// A non-Collapse span (markup marker-hiding): "**hi**" [0,6) with the doubled
// markers hidden keeps the content "hi" traversable and mapped to its own doc
// runes; hidden marker positions collapse onto the nearest visible cell.
func TestSubstituteMarkupMapping(t *testing.T) {
	doc := []rune("**hi**")
	span := DisplaySpan{
		Start: 0, End: 6,
		Runes: []rune("hi"),
		Doc:   []int{2, 3},
		Style: []string{"", ""},
	}
	d := substituteSpans(doc, []DisplaySpan{span}, false)
	if d == nil || d.Text != "hi" {
		t.Fatalf("markup display = %q", displayText(d))
	}
	// Content doc runes 2,3 map to display 0,1; leading markers 0,1 collapse to
	// display 0; trailing markers 4,5 collapse to display end (2); boundary 6→2.
	want := []int{0, 0, 0, 1, 2, 2, 2}
	for p, wd := range want {
		if d.DocToDisp[p] != wd {
			t.Fatalf("DocToDisp[%d] = %d, want %d", p, d.DocToDisp[p], wd)
		}
	}
	if d.DispToDoc[0] != 2 || d.DispToDoc[1] != 3 {
		t.Fatalf("content should map back to its doc runes: %v", d.DispToDoc)
	}
	if d.Forced[0] != "" || d.Forced[1] != "" {
		t.Fatal("markup content keeps its grammar color (Forced empty)")
	}
}

// A double-width line with no spans still substitutes (to carry the flag).
func TestSubstituteDoubleWidthFlag(t *testing.T) {
	d := substituteSpans([]rune("Head"), nil, true)
	if d == nil || !d.DoubleWide || d.Text != "Head" {
		t.Fatalf("double-width identity failed: %+v", d)
	}
}

// No transform is the identity (nil), and the exported editor-side form agrees.
func TestSubstituteIdentity(t *testing.T) {
	if substituteSpans([]rune("abc"), nil, false) != nil {
		t.Fatal("no transform must substitute nothing")
	}
	text, m := SubstituteButtons("abc", nil, false)
	if text != "abc" || m != nil {
		t.Fatal("exported identity form disagrees")
	}
}

func displayText(d *lineDisplay) string {
	if d == nil {
		return "<nil>"
	}
	return d.Text
}
