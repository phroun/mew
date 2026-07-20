package render

import "testing"

// substituteButtons: display text, forced-color cells, and the doc<->display
// maps that keep the caret and selection honest. "ab[[x]]cd" with the span
// [2,7) replaced by "<X>" + shadow.
func TestSubstituteButtonsMapping(t *testing.T) {
	doc := []rune("ab[[x]]cd")
	d := substituteButtons(doc, []ButtonSpan{{
		Start: 2, End: 7, Runes: []rune("<X>"), Shadow: '█',
		Color: "B", ShadowColor: "S",
	}})
	if d == nil {
		t.Fatal("substitution expected")
	}
	if d.Text != "ab<X>█cd" {
		t.Fatalf("display text = %q", d.Text)
	}
	// Doc boundaries: 0,1 unchanged; 2..6 (inside) park at the button start
	// (display 2); 7 (span end) lands after the shadow (display 6); 8, 9 follow.
	want := []int{0, 1, 2, 2, 2, 2, 2, 6, 7, 8}
	for p, wd := range want {
		if d.DocToDisp[p] != wd {
			t.Fatalf("DocToDisp[%d] = %d, want %d", p, d.DocToDisp[p], wd)
		}
	}
	// Display cells: chrome cells are -1 with forced colors; doc cells map back.
	wantDoc := []int{0, 1, -1, -1, -1, -1, 7, 8}
	for i, wd := range wantDoc {
		if d.DispToDoc[i] != wd {
			t.Fatalf("DispToDoc[%d] = %d, want %d", i, d.DispToDoc[i], wd)
		}
	}
	if d.Forced[2] != "B" || d.Forced[5] != "S" || d.Forced[0] != "" {
		t.Fatalf("forced colors wrong: %q", d.Forced)
	}
}

// No spans is the identity (nil), and the exported editor-side form agrees.
func TestSubstituteButtonsIdentity(t *testing.T) {
	if substituteButtons([]rune("abc"), nil) != nil {
		t.Fatal("no spans must substitute nothing")
	}
	text, m := SubstituteButtons("abc", nil)
	if text != "abc" || m != nil {
		t.Fatal("exported identity form disagrees")
	}
}
