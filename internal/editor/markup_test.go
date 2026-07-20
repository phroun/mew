package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/window"
)

// Browse mode hides dokuwiki inline markers and keeps the styled text; the
// grammar's bold/italic/underline attribute rides the content.
func TestBrowseMarkupMarkersHidden(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t,
		"a **bold** b //it// c __un__ d\n", "[options]\nsyntax=dokuwiki\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	w.BrowseActive = true
	out.Reset()
	e.performRender()
	plain := stripSGR(out.String())
	for _, marker := range []string{"**", "//", "__"} {
		if strings.Contains(plain, marker) {
			t.Fatalf("browse mode should hide %q markers; got %q", marker, plain)
		}
	}
	for _, word := range []string{"bold", "it", "un"} {
		if !strings.Contains(plain, word) {
			t.Fatalf("styled word %q should remain; got %q", word, plain)
		}
	}
}

// Browse mode hides heading "=" and restyles by level: the equals go away, the
// heading color paints, and the per-level bold/underline attributes apply.
func TestBrowseHeadingLevels(t *testing.T) {
	// L1 ======, L3 ====, L5 == : bold on 1&3, underline on 1&3 (not 5).
	e, w, out := renderedEditorWithConfig(t,
		"====== Big ======\n==== Mid ====\n== Small ==\n", "[options]\nsyntax=dokuwiki\n")
	w.SetCursorPos(window.Position{Line: 2, Rune: 0}) // keep caret off the styled lines
	w.BrowseActive = true
	out.Reset()
	e.performRender()
	full := out.String()
	plain := stripSGR(full)
	if strings.Contains(plain, "=") {
		t.Fatalf("heading '=' markers should be hidden; got %q", plain)
	}
	for _, word := range []string{"Big", "Mid", "Small"} {
		if !strings.Contains(plain, word) {
			t.Fatalf("heading text %q should remain; got %q", word, plain)
		}
	}
	// The heading base color (bright cyan) paints, and bold+underline appear
	// somewhere (L1/L3).
	if !strings.Contains(full, "\x1b[0;96;40m") {
		t.Fatal("heading base color should paint")
	}
	if !strings.Contains(full, "\x1b[1m") || !strings.Contains(full, "\x1b[4m") {
		t.Fatal("bold and underline attributes should appear on higher levels")
	}
}

// L1/L2 headings render double-width: the row is emitted with DECDWL (ESC#6)
// and an erase-to-end; a level-5 heading (no double-width) is not.
func TestBrowseHeadingDoubleWidth(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t,
		"====== Big ======\n", "[options]\nsyntax=dokuwiki\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	w.BrowseActive = true
	out.Reset()
	e.performRender()
	full := out.String()
	if !strings.Contains(full, "\x1b#6") {
		t.Fatal("an L1 heading row should emit DECDWL (ESC#6)")
	}
	if !strings.Contains(full, "\x1b[0K") {
		t.Fatal("a double-width row should erase to end of line")
	}

	// A level-5 heading is not double-width.
	e2, w2, out2 := renderedEditorWithConfig(t, "== Small ==\n", "[options]\nsyntax=dokuwiki\n")
	w2.SetCursorPos(window.Position{Line: 0, Rune: 0})
	w2.BrowseActive = true
	out2.Reset()
	e2.performRender()
	if strings.Contains(out2.String(), "\x1b#6") {
		t.Fatal("a level-5 heading must not be double-width")
	}
}

// With line numbers on, a double-width row shows a single space in the gutter
// instead of its (oversized, doubled) number; a normal row shows its number.
func TestBrowseHeadingGutter(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t,
		"====== Big ======\nplain line two\n",
		"[options]\nsyntax=dokuwiki\nshowLineNumbers=yes\n")
	w.SetCursorPos(window.Position{Line: 1, Rune: 0})
	w.BrowseActive = true
	out.Reset()
	e.performRender()
	// Strip SGR and the DEC line-mode sequences (ESC#6 / ESC#5, whose "6"/"5"
	// are not content).
	plain := strings.NewReplacer("\x1b#6", "", "\x1b#5", "").Replace(stripSGR(out.String()))
	// The doubled heading is on doc line 1 (number "1"); the normal line 2
	// keeps its "2". So "2" appears but the heading's "1" gutter is gone.
	if !strings.Contains(plain, "2") {
		t.Fatal("a normal row should still show its line number")
	}
	// The heading text "Big" must not be preceded by a "1" gutter digit; find
	// "Big" and check the run right before it has no digit.
	i := strings.Index(plain, "Big")
	if i < 0 {
		t.Fatal("heading text missing")
	}
	before := plain[:i]
	if strings.ContainsAny(before[strings.LastIndexByte(before, '\n')+1:], "0123456789") {
		t.Fatalf("double-width heading gutter should show no number; got %q", before)
	}
}

// The caret on a double-width row is placed against the halved gutter and
// content: with the browse-mode gutter rounded to an even width, the doubled
// content begins at the same physical column as a normal row (no notch), and
// the reported caret column reflects the 2x cell mapping. A normal caret line
// is unaffected.
func TestDoubleWidthCaretColumnAligns(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t,
		"====== Big ======\nplain line\n",
		"[options]\nsyntax=dokuwiki\nshowLineNumbers=yes\n")
	w.BrowseActive = true
	out.Reset()
	e.performRender()

	if w.LineNumWidth%2 != 0 {
		t.Fatalf("browse-mode gutter width should be rounded even; got %d", w.LineNumWidth)
	}

	contains := func(cols []int, v int) bool {
		for _, c := range cols {
			if c == v {
				return true
			}
		}
		return false
	}

	// Caret at the first content cell of the double-width heading. base is in
	// cell space (half gutter); the ruler column is its physical (2x) position.
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	base := 1 + w.MarginInner + w.LineNumWidth/2
	want := 2*base - 1
	if cols := e.Renderer.CursorColumns(w); !contains(cols, want) {
		t.Fatalf("double-width caret column = %v, want %d", cols, want)
	}
	// No notch: with a zero inner margin the doubled content begins at the same
	// physical column as a normal row's content (just past the gutter).
	if w.MarginInner == 0 && want != 1+w.LineNumWidth {
		t.Fatalf("double-width content start %d should align with normal %d", want, 1+w.LineNumWidth)
	}

	// A normal caret line is placed with the full gutter and no 2x mapping.
	w.SetCursorPos(window.Position{Line: 1, Rune: 3})
	normWant := 1 + w.MarginInner + w.LineNumWidth + 3
	if cols := e.Renderer.CursorColumns(w); !contains(cols, normWant) {
		t.Fatalf("normal caret column = %v, want %d", cols, normWant)
	}
}

// ensureCursorVisibleHorizontal treats the screen as half as wide on a
// double-width caret line: a heading wider than half the content scrolls where
// the same-length normal line would still fit.
func TestDoubleWidthHorizontalScroll(t *testing.T) {
	head := "====== " + strings.Repeat("x", 60) + " ======\n"
	e, w, out := renderedEditorWithConfig(t, head, "[options]\nsyntax=dokuwiki\n")
	w.BrowseActive = true
	out.Reset()
	e.performRender() // establish ContentWidth
	// caret near the end of the (60-char) heading content
	lineLen := len([]rune(strings.TrimRight(head, "\n")))
	w.SetCursorPos(window.Position{Line: 0, Rune: lineLen - 9})
	e.ensureCursorVisibleHorizontal(w)
	if w.ViewState.ViewOffsetX == 0 {
		t.Fatal("a double-width heading wider than half the screen should scroll")
	}

	// The same length as a normal line fits without scrolling.
	e2, w2, out2 := renderedEditorWithConfig(t, strings.Repeat("x", 60)+"\n",
		"[options]\nsyntax=dokuwiki\n")
	w2.BrowseActive = true
	out2.Reset()
	e2.performRender()
	w2.SetCursorPos(window.Position{Line: 0, Rune: 52})
	e2.ensureCursorVisibleHorizontal(w2)
	if w2.ViewState.ViewOffsetX != 0 {
		t.Fatalf("a normal 60-col line should fit without scrolling; off=%d", w2.ViewState.ViewOffsetX)
	}
}
