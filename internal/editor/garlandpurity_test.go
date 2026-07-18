package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/window"
)

// Decoration-only mutations must not advance the content change sequence:
// setting a mark cannot change any content-derived cache.
func TestChangeSeqIgnoresMarks(t *testing.T) {
	_, w := newTestEditor(t, "alpha\nbeta\n")
	b := w.Buffer

	b.InsertText(0, 0, "x")
	seq := b.ChangeSeq()

	if err := b.SetMark("1", 1, 0); err != nil {
		t.Fatal(err)
	}
	if b.ChangeSeq() != seq {
		t.Fatal("SetMark must not bump ChangeSeq")
	}
	b.ClearMark("1")
	if b.ChangeSeq() != seq {
		t.Fatal("ClearMark must not bump ChangeSeq")
	}

	b.InsertText(0, 0, "y")
	if b.ChangeSeq() == seq {
		t.Fatal("content mutation must bump ChangeSeq")
	}
}

// Setting a mark must not invalidate the highlight cache (the over-
// invalidation this fix removes): the cached line slices survive untouched.
func TestSynCacheSurvivesMarks(t *testing.T) {
	e, w := newTestEditor(t, "int a;\nint b;\nint c;\n", "syntax=cpp")
	c1 := e.ensureSynCache(w.Buffer, 2)
	if c1 == nil || len(c1.colors) != 3 {
		t.Fatal("expected a filled cache")
	}
	line0 := c1.colors[0]

	w.Buffer.SetMark("5", 1, 2)
	c2 := e.ensureSynCache(w.Buffer, 2)
	if c2 != c1 || len(c2.colors) != 3 || &c2.colors[0][0] != &line0[0] {
		t.Fatal("a mark must leave the highlight cache untouched")
	}
}

// The dirty watermark: an edit at line N truncates the cache at N, keeping
// the untouched prefix (same backing slices), and the recomputed tail is
// still correct — including multi-line state carried across the cut.
func TestSynCacheWatermarkTruncation(t *testing.T) {
	e, w := newTestEditor(t,
		"int a; /* open\nstill in comment\nmore comment\n*/ int b;\nint c;\n",
		"syntax=cpp")
	b := w.Buffer

	c1 := e.ensureSynCache(b, 4)
	if c1 == nil || len(c1.colors) != 5 {
		t.Fatalf("expected 5 cached lines, got %v", c1)
	}
	line0 := c1.colors[0]
	line1 := c1.colors[1]

	// Edit line 3 (the comment close): lines 0-2 must survive as-is.
	b.InsertText(3, 0, " ")
	c2 := e.ensureSynCache(b, 4)
	if c2 != c1 {
		t.Fatal("watermark truncation should reuse the cache, not rebuild it")
	}
	if &c2.colors[0][0] != &line0[0] || &c2.colors[1][0] != &line1[0] {
		t.Fatal("prefix lines must keep their original backing slices")
	}
	// Correctness across the cut: line 1 is still comment, line 4 still code.
	if !strings.Contains(strings.Join(c2.colors[1], ""), sgrComment) {
		t.Fatal("line 1 should still be comment-colored")
	}
	if !strings.Contains(strings.Join(c2.colors[4], ""), sgrType) {
		t.Fatal("line 4 should still color 'int' as a type")
	}
}

// Deleting the comment OPENER above cached lines recolors everything below
// the watermark — the truncation must not keep stale comment state.
func TestSynCacheWatermarkRecolorsBelow(t *testing.T) {
	e, w := newTestEditor(t, "/*\nint a;\nint b;\n", "syntax=cpp")
	b := w.Buffer

	c := e.ensureSynCache(b, 2)
	if !strings.Contains(strings.Join(c.colors[1], ""), sgrComment) {
		t.Fatal("line 1 starts as comment")
	}

	// Remove the opener on line 0: damage at line 0 -> full rebuild path.
	b.DeleteText(0, 0, 2)
	c2 := e.ensureSynCache(b, 2)
	if !strings.Contains(strings.Join(c2.colors[1], ""), sgrType) {
		t.Fatal("after deleting /*, line 1 must recolor as code")
	}
}

// An edit BELOW the cached area leaves the whole cache intact.
func TestSynCacheEditBelowKeepsCache(t *testing.T) {
	e, w := newTestEditor(t, "int a;\nint b;\nint c;\nint d;\n", "syntax=cpp")
	b := w.Buffer

	c1 := e.ensureSynCache(b, 1) // only lines 0-1 computed
	line0 := c1.colors[0]
	b.InsertText(3, 0, "x")
	c2 := e.ensureSynCache(b, 1)
	if c2 != c1 || len(c2.colors) != 2 || &c2.colors[0][0] != &line0[0] {
		t.Fatal("an edit below the cached area must not disturb it")
	}
}

// A backspace join at line start damages the previous line: the watermark
// must reach it (DeleteBackward reports line-1).
func TestWatermarkBackwardJoin(t *testing.T) {
	e, w := newTestEditor(t, "// note\nint b;\n", "syntax=cpp")
	b := w.Buffer
	e.ensureSynCache(b, 1)

	// Join line 1 into line 0 (backspace at start of line 1): "int b;"
	// becomes part of the comment line.
	w.SetCursorPos(window.Position{Line: 1, Rune: 0})
	w.Caret.DeleteBackward(1)
	c := e.ensureSynCache(b, 0)
	if !strings.Contains(strings.Join(c.colors[0], ""), sgrComment) {
		t.Fatal("joined line should be comment-colored")
	}
	if len(c.colors) != 1 {
		t.Fatalf("expected 1 cached line after the join, got %d", len(c.colors))
	}
}

// The prompted set_mark rides edits: text inserted above the buffer while
// the prompt is open slides the pending position, so the mark lands on the
// TEXT the caret was on, not on its old line number.
func TestSetMarkPromptSlides(t *testing.T) {
	e, w := newTestEditor(t, "alpha\ntarget\n")
	w.SetCursorPos(window.Position{Line: 1, Rune: 2}) // inside "target"

	e.PawScript.ExecuteAsync("set_mark")
	if focusedPrompt(e) == nil {
		t.Fatal("expected the set_mark prompt")
	}

	// While the prompt is up, something else edits above the caret.
	w.Buffer.InsertLine(0, "intruder one")
	w.Buffer.InsertLine(0, "intruder two")

	answerPrompt(t, e, "7")

	line, rune_, ok := w.Buffer.GetMark("7")
	if !ok {
		t.Fatal("mark 7 should be set")
	}
	if line != 3 || rune_ != 2 {
		t.Fatalf("mark landed at %d:%d, want 3:2 (slid with the insertions)", line, rune_)
	}
	lineText := strings.TrimRight(w.Buffer.GetLine(line), "\n\r")
	if lineText != "target" {
		t.Fatalf("mark should still sit on the target line, got %q", lineText)
	}
}
