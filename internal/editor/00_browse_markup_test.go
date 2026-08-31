package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// Browse mode hides dokuwiki inline markers and keeps the styled text; the
// grammar's bold/italic/underline attribute rides the content.
func TestBrowseMarkupMarkersHidden(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t,
		"a **bold** b //it// c __un__ d\n", "[options]\nsyntax=dokuwiki\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
	w.BrowseActive = true
	out.Reset()
	e.performRender()
	plain := stripANSI(out.String())
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

// Browse mode still hides the markers when emphasis nests, even though the
// grammar splits the run at each inner toggle. Only the true open/close markers
// are hidden; the inner words survive.
func TestBrowseNestedMarkupMarkersHidden(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // use the embedded grammar, not a dev ~/.mew shadow
	e, w, out := renderedEditorWithConfig(t,
		"x //it **bo** more// y\n", "[options]\nsyntax=dokuwiki\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
	w.BrowseActive = true
	out.Reset()
	e.performRender()
	plain := stripANSI(out.String())
	for _, marker := range []string{"**", "//"} {
		if strings.Contains(plain, marker) {
			t.Fatalf("browse mode should hide %q even when nested; got %q", marker, plain)
		}
	}
	// The words on every nesting level survive the marker hiding.
	for _, word := range []string{"it", "bo", "more"} {
		if !strings.Contains(plain, word) {
			t.Fatalf("nested word %q should remain; got %q", word, plain)
		}
	}
}

// Browse mode hides %%nowiki%% markers and shows the content verbatim — the
// grammar never sub-parses links or emphasis inside it, so a bracketed run stays
// literal rather than becoming a button, and a reserved character survives.
func TestBrowseNowikiMarkersHidden(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // use the embedded grammar, not a dev ~/.mew shadow
	e, w, out := renderedEditorWithConfig(t,
		"a %%[[x]]%% b %%{%% c\n", "[options]\nsyntax=dokuwiki\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
	w.BrowseActive = true
	out.Reset()
	e.performRender()
	plain := stripANSI(out.String())
	if strings.Contains(plain, "%") {
		t.Fatalf("browse mode should hide the nowiki markers (no stray %%); got %q", plain)
	}
	if !strings.Contains(plain, "[[x]]") {
		t.Fatalf("nowiki content should show verbatim, with the link suppressed; got %q", plain)
	}
	if !strings.Contains(plain, "{") {
		t.Fatalf("nowiki literal brace should survive; got %q", plain)
	}
}

// A link title decodes numeric / HTML entities, so a link can display the very
// characters the link syntax reserves (| ] [ {) as clickable button text. The
// target is left raw.
func TestLinkTitleEntityDecode(t *testing.T) {
	cases := []struct{ in, target, title string }{
		{"[[keys#|&#93;]]", "keys#", "]"},
		{"[[keys#|&#124;]]", "keys#", "|"},
		{"[[keys#|&#91;]]", "keys#", "["},
		{"[[keys#|&#123;]]", "keys#", "{"},
		{"[[keys#|&#x5D;]]", "keys#", "]"}, // hex form
		{"[[foo|&amp;]]", "foo", "&"},
		{"[[plain|Title]]", "plain", "Title"}, // no entity: unchanged
	}
	for _, c := range cases {
		gotTarget, gotTitle := parseDokuLink(c.in)
		if gotTarget != c.target || gotTitle != c.title {
			t.Errorf("parseDokuLink(%q) = (%q, %q), want (%q, %q)", c.in, gotTarget, gotTitle, c.target, c.title)
		}
	}
}

// Browse mode hides heading "=" and restyles by level: the equals go away, the
// heading color paints, and the per-level bold/underline attributes apply.
func TestBrowseHeadingLevels(t *testing.T) {
	// L1 ======, L3 ====, L5 == : bold on 1&3, underline on 1&3 (not 5).
	e, w, out := renderedEditorWithConfig(t,
		"====== Big ======\n==== Mid ====\n== Small ==\n", "[options]\nsyntax=dokuwiki\n")
	w.SetCursorPos(viewport.Position{Line: 2, Rune: 0}) // keep caret off the styled lines
	w.BrowseActive = true
	out.Reset()
	e.performRender()
	full := out.String()
	plain := stripANSI(full)
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
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
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
	w2.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
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
	w.SetCursorPos(viewport.Position{Line: 1, Rune: 0})
	w.BrowseActive = true
	out.Reset()
	e.performRender()
	// Strip SGR and the DEC line-mode sequences (ESC#6 / ESC#5, whose "6"/"5"
	// are not content).
	plain := strings.NewReplacer("\x1b#6", "", "\x1b#5", "").Replace(stripANSI(out.String()))
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
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
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
	w.SetCursorPos(viewport.Position{Line: 1, Rune: 3})
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
	w.SetCursorPos(viewport.Position{Line: 0, Rune: lineLen - 9})
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
	w2.SetCursorPos(viewport.Position{Line: 0, Rune: 52})
	e2.ensureCursorVisibleHorizontal(w2)
	if w2.ViewState.ViewOffsetX != 0 {
		t.Fatalf("a normal 60-col line should fit without scrolling; off=%d", w2.ViewState.ViewOffsetX)
	}
}

// dwHeadingEditor renders a document whose first line is a level-6 heading
// (painted double-width in browse mode) over a plain second line.
func dwHeadingEditor(t *testing.T, heading, second, extra string) (*Editor, *viewport.Viewport) {
	t.Helper()
	e, w, out := renderedEditorWithConfig(t, heading+"\n"+second+"\n",
		"[options]\nsyntax=dokuwiki\n"+extra)
	w.BrowseActive = true
	out.Reset()
	e.performRender()
	if _, dw := e.lineDisplaySpans(w, 0); !dw {
		t.Fatal("the heading should paint double-width in browse mode")
	}
	return e, w
}

// A double-width row is addressed in CELLS (each spans two physical columns),
// but the mouse reports a PHYSICAL column. Subtracting the cell-space base
// from the physical column and halving the difference charged the gutter's
// cells at physical width, landing every click a cell right of the pointer —
// further right the wider the gutter. Round-tripping the renderer's own caret
// placement is the check: click where the caret is painted, land on its rune.
func TestDoubleWidthMouseRoundTrip(t *testing.T) {
	for _, ln := range []string{"no", "yes"} {
		e, w := dwHeadingEditor(t, "====== Like This ======", strings.Repeat("y", 40), "")
		e.setOption(w, "showLineNumbers", ln)
		e.performRender()

		for _, rune_ := range []int{7, 8, 9, 12, 15} {
			w.SetCursorPos(viewport.Position{Line: 0, Rune: rune_})
			cols := e.Renderer.CursorColumns(w)
			if len(cols) == 0 {
				t.Fatalf("lineNumbers=%s rune %d: caret is not on screen", ln, rune_)
			}
			// Both physical columns of the cell resolve to the same rune.
			for _, x := range []int{cols[0], cols[0] + 1} {
				_, line, got, _, ok := e.mouseHit(x, w.ContentY+1)
				if !ok || line != 0 || got != rune_ {
					t.Errorf("lineNumbers=%s: click x=%d (caret painted at %d) hit rune %d (line %d, ok=%v), want %d",
						ln, x, cols[0], got, line, ok, rune_)
				}
			}
		}
	}
}

// Under direction=rtl the hit test maps the click through the line's
// right-anchored geometry, which needs THIS row's scroll offset — halved on a
// doubled row, exactly like its cell count. Mixing the raw offset with halved
// cells walked the hit further off with every column scrolled, which is why
// only RTL drifted (the LTR path already used the halved offset).
func TestDoubleWidthMouseRoundTripRTLScrolled(t *testing.T) {
	e, w := dwHeadingEditor(t, "====== "+strings.Repeat("abcdefghij", 8)+" ======",
		strings.Repeat("y", 40), "direction=rtl\n")
	e.setOption(w, "showLineNumbers", "yes")
	e.performRender()

	for _, off := range []int{0, 4, 12, 30} {
		w.ViewState.ViewOffsetX = off
		for _, rune_ := range []int{10, 25, 45} {
			w.SetCursorPos(viewport.Position{Line: 0, Rune: rune_})
			cols := e.Renderer.CursorColumns(w)
			if len(cols) == 0 {
				continue // scrolled off screen at this offset
			}
			if _, _, got, _, ok := e.mouseHit(cols[0], w.ContentY+1); !ok || got != rune_ {
				t.Errorf("rtl off=%d: click x=%d (caret painted there) hit rune %d (ok=%v), want %d",
					off, cols[0], got, ok, rune_)
			}
		}
	}
}

// The sticky ideal column holds the caret's SCREEN X across a vertical move.
// A double-width line's own column space is half as dense, and its "======"
// markers take no display columns at all, so the ideal is stored in normal
// screen columns measured on the line as DISPLAYED — and converted back on
// arrival. Measuring the raw line at single density moved the caret by twice
// the marker width on every crossing.
func TestDoubleWidthIdealColumnHoldsScreenX(t *testing.T) {
	e, w := dwHeadingEditor(t, "====== Like This Heading ======", strings.Repeat("y", 60), "")

	for _, clickX := range []int{11, 15, 21} {
		_, _, r, _, ok := e.mouseHit(clickX, w.ContentY+1)
		if !ok {
			t.Fatalf("click x=%d missed the heading", clickX)
		}
		w.SetCursorPos(viewport.Position{Line: 0, Rune: r})
		e.afterHorizontalMovement(w)
		if cols := e.Renderer.CursorColumns(w); len(cols) == 0 || cols[0] != clickX {
			t.Fatalf("click x=%d landed the caret at %v", clickX, cols)
		}

		e.executeCommand("go_line_next")
		if cols := e.Renderer.CursorColumns(w); len(cols) == 0 || cols[0] != clickX {
			t.Errorf("down from the heading: caret at %v, want screen X %d held", cols, clickX)
		}
		// And coming back up onto the doubled line holds it too.
		e.executeCommand("go_line_prior")
		if cols := e.Renderer.CursorColumns(w); len(cols) == 0 || cols[0] != clickX {
			t.Errorf("back up onto the heading: caret at %v, want screen X %d held", cols, clickX)
		}
	}
}
