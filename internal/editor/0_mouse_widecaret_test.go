package editor

import "testing"

// Clicking a character wider than one cell must split by which CELL was hit,
// even with no sub-cell (pixel) info: the trailing cell(s) of a wide glyph put
// the caret AFTER it. A single-cell glyph still needs a pixel half to split.
func TestWideCharNearestEdge(t *testing.T) {
	// "世界" — two width-2 CJK ideographs. Doc runes: 世=0, 界=1, EOL=2.
	// Visual cells: 世 at 0,1 ; 界 at 2,3.
	e, w, _ := newRenderedEditor(t, "世界\n")
	e.performRender()
	if vw := e.lineVisualWidth(w, "世界", e.tabSize(w)); vw != 4 {
		t.Fatalf("expected the two CJK glyphs to span 4 cells, got %d", vw)
	}
	base, row := w.ContentX+1, w.ContentY+1
	caretAt := func(cell, subX int) int {
		e.mouseSubX = subX
		_, _, _, caret, ok := e.mouseHit(base+cell, row)
		if !ok {
			t.Fatalf("mouseHit(cell %d) not ok", cell)
		}
		return caret
	}

	// Cell resolution (subX = -1): the wide glyph still splits by cell.
	for _, c := range []struct {
		cell, want int
		what       string
	}{
		{0, 0, "left cell of 世 → before 世"},
		{1, 1, "right cell of 世 → after 世"},
		{2, 1, "left cell of 界 → before 界"},
		{3, 2, "right cell of 界 → after 界 (EOL)"},
	} {
		if got := caretAt(c.cell, -1); got != c.want {
			t.Errorf("cell mode, %s: caret %d, want %d", c.what, got, c.want)
		}
	}

	// Pixel sub-cell refines WITHIN a cell of the wide glyph: the far-left of
	// its left cell is still before; the far-right of its right cell, after.
	if got := caretAt(0, 100); got != 0 {
		t.Errorf("pixel left-of-左cell of 世: caret %d, want 0", got)
	}
	if got := caretAt(1, 900); got != 1 {
		t.Errorf("pixel right-of-右cell of 世: caret %d, want 1", got)
	}

	// Overwrite mode ignores the split — a click selects the char to type over.
	w.ViewState.OverwriteMode = true
	if got := caretAt(1, -1); got != 0 {
		t.Errorf("overwrite mode, right cell of 世: caret %d, want 0 (on 世)", got)
	}
	w.ViewState.OverwriteMode = false
}

// A single-cell glyph splits only with a pixel half; in cell mode it keeps the
// classic before-the-character landing (no regression for plain terminals).
func TestNarrowCharNeedsPixelHalf(t *testing.T) {
	e, w, _ := newRenderedEditor(t, "abc\n")
	e.performRender()
	base, row := w.ContentX+1, w.ContentY+1
	caretAt := func(cell, subX int) int {
		e.mouseSubX = subX
		_, _, _, caret, ok := e.mouseHit(base+cell, row)
		if !ok {
			t.Fatalf("mouseHit(cell %d) not ok", cell)
		}
		return caret
	}
	if got := caretAt(1, -1); got != 1 { // cell mode: before 'b'
		t.Errorf("cell mode narrow: caret %d, want 1 (before b)", got)
	}
	if got := caretAt(1, 200); got != 1 { // pixel left half: before 'b'
		t.Errorf("pixel left half: caret %d, want 1", got)
	}
	if got := caretAt(1, 800); got != 2 { // pixel right half: after 'b'
		t.Errorf("pixel right half: caret %d, want 2 (after b)", got)
	}
}
