package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// On a DOUBLE-WIDTH row (an L1/L2 heading in browse mode) each cell spans two
// physical columns, so the pixel report's sub-cell fraction — which is within
// ONE physical column — must be folded with the clicked column's parity to
// span the whole doubled cell. Without that fold the two physical columns are
// indistinguishable and the nearest-edge split lands unpredictably. Here the
// split must fall exactly at the cell's center (the seam between its two
// physical columns): anything in the left column is "before", the right column
// "after".
func TestDoubleWidthSubCell(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t,
		"====== Big ======\n", "[options]\nsyntax=dokuwiki\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
	w.BrowseActive = true
	w.ViewState.LinkBrowsing = true
	out.Reset()
	e.performRender()
	if s := out.String(); !strings.Contains(s, "\x1b#6") {
		t.Fatal("expected a double-width (DECDWL) heading row")
	}

	row := w.ContentY + 1
	// Cell 0 is the first heading glyph; its two physical columns.
	leftCol := 2*w.ContentX + 1
	rightCol := 2*w.ContentX + 2

	caretAt := func(x, subX int) (idx, caret int) {
		e.mouseSubX = subX
		_, _, rp, cr, ok := e.mouseHit(x, row)
		if !ok {
			t.Fatalf("mouseHit(x=%d) not ok", x)
		}
		return rp, cr
	}

	idx, _ := caretAt(leftCol, 0)

	for _, c := range []struct {
		x, subX, wantCaret int
		what               string
	}{
		{leftCol, 0, idx, "left column, far-left → before"},
		{leftCol, 999, idx, "left column, far-right (cell 49.9%) → before"},
		{rightCol, 0, idx + 1, "right column, far-left (cell 50%) → after"},
		{rightCol, 999, idx + 1, "right column, far-right → after"},
	} {
		if _, caret := caretAt(c.x, c.subX); caret != c.wantCaret {
			t.Errorf("%s: caret %d, want %d (idx=%d)", c.what, caret, c.wantCaret, idx)
		}
	}
}
