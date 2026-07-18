package editor

import (
	"testing"

	"github.com/phroun/mew/internal/window"
)

// mark reads a named mark as (line, rune), failing if it is unset.
func mark(t *testing.T, w *window.Window, name string) (int, int) {
	t.Helper()
	line, rune_, ok := w.Buffer.GetMark(name)
	if !ok {
		t.Fatalf("mark %q not set", name)
	}
	return line, rune_
}

// These tests pin the behavior that motivated removing SetLine: an edit to a
// line must let garland SLIDE the surrounding decorations by the net delta,
// not collapse them. Under the old whole-line rewrite a mark inside an edited
// line landed at the line start; here it tracks the text.

func TestLineJoinSlidesMarks(t *testing.T) {
	e, w := newTestEditor(t, "ab\ncd\n")
	if err := w.Buffer.SetMark("m", 1, 1); err != nil { // the 'd'
		t.Fatal(err)
	}
	w.SetCursorPos(window.Position{Line: 1, Rune: 0})
	e.PawScript.ExecuteAsync("del_char_prior") // backspace at col 0 -> join

	if got := docContent(w); got != "abcd" {
		t.Fatalf("join content: %q", got)
	}
	if l, r := mark(t, w, "m"); l != 0 || r != 3 { // 'd' now at (0,3)
		t.Fatalf("mark after join: (%d,%d), want (0,3)", l, r)
	}
	if w.CursorPos().Line != 0 || w.CursorPos().Rune != 2 {
		t.Fatalf("cursor after join: %v, want (0,2)", w.CursorPos())
	}
}

func TestLineJoinForwardSlidesMarks(t *testing.T) {
	e, w := newTestEditor(t, "ab\ncd\n")
	if err := w.Buffer.SetMark("m", 1, 1); err != nil {
		t.Fatal(err)
	}
	w.SetCursorPos(window.Position{Line: 0, Rune: 2}) // end of "ab"
	e.PawScript.ExecuteAsync("del_char_next")         // delete at EOL -> join next

	if got := docContent(w); got != "abcd" {
		t.Fatalf("forward join content: %q", got)
	}
	if l, r := mark(t, w, "m"); l != 0 || r != 3 {
		t.Fatalf("mark after forward join: (%d,%d), want (0,3)", l, r)
	}
}

func TestCRLFJoinDeletesWholeTerminator(t *testing.T) {
	e, w := newTestEditor(t, "ab\r\ncd\n")
	w.SetCursorPos(window.Position{Line: 1, Rune: 0})
	e.PawScript.ExecuteAsync("del_char_prior")
	// The whole "\r\n" terminator is removed, not just the "\n" (which would
	// orphan a "\r").
	if got := w.Buffer.GetContent(); got != "abcd\n" {
		t.Fatalf("CRLF join content: %q", got)
	}
}

func TestBlockIndentSlidesMarks(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb\nccc\n")
	// Block covers all three lines; a probe mark sits mid-line-1.
	w.Buffer.SetMark("_block_begin", 0, 0)
	w.Buffer.SetMark("_block_end", 2, 3)
	w.Buffer.SetMark("m", 1, 1)

	e.PawScript.ExecuteAsync("block_indent") // tabSize 4 -> 4 spaces per line

	if got := docContent(w); got != "    aaa\n    bbb\n    ccc" {
		t.Fatalf("indent content: %q", got)
	}
	if l, r := mark(t, w, "m"); l != 1 || r != 5 { // slid right by 4
		t.Fatalf("probe mark after indent: (%d,%d), want (1,5)", l, r)
	}
	// Block-begin at column 0 stays at the line start; block-end grows with
	// the indented text on its line.
	if l, r := mark(t, w, "_block_begin"); l != 0 || r != 0 {
		t.Fatalf("_block_begin after indent: (%d,%d), want (0,0)", l, r)
	}
	if l, r := mark(t, w, "_block_end"); l != 2 || r != 7 {
		t.Fatalf("_block_end after indent: (%d,%d), want (2,7)", l, r)
	}
}

func TestBlockUnindentSlidesMarks(t *testing.T) {
	e, w := newTestEditor(t, "    aaa\n    bbb\n    ccc\n")
	w.Buffer.SetMark("_block_begin", 0, 0)
	w.Buffer.SetMark("_block_end", 2, 7)
	w.Buffer.SetMark("m", 1, 5) // a 'b' after the indent

	e.PawScript.ExecuteAsync("block_unindent")

	if got := docContent(w); got != "aaa\nbbb\nccc" {
		t.Fatalf("unindent content: %q", got)
	}
	if l, r := mark(t, w, "m"); l != 1 || r != 1 { // slid left by 4
		t.Fatalf("probe mark after unindent: (%d,%d), want (1,1)", l, r)
	}
	if l, r := mark(t, w, "_block_end"); l != 2 || r != 3 {
		t.Fatalf("_block_end after unindent: (%d,%d), want (2,3)", l, r)
	}
}

func TestReplaceSlidesDownstreamMark(t *testing.T) {
	e, w := newTestEditor(t, "foo bar foo\n")
	w.SetCursorPos(window.Position{})
	w.Buffer.SetMark("m", 0, 8) // start of the SECOND "foo"

	// Replace the first "foo" with "XY" (3 runes -> 2), skip the second.
	e.startFind("foo", "", "XY", true, true, true)
	answerPrompt(t, e, "y")
	answerPrompt(t, e, "n")

	if got := docContent(w); got != "XY bar foo" {
		t.Fatalf("replace content: %q", got)
	}
	if l, r := mark(t, w, "m"); l != 0 || r != 7 { // slid left by 1
		t.Fatalf("downstream mark after replace: (%d,%d), want (0,7)", l, r)
	}
}

// A replacement that spans into a newline still lands correctly and keeps a
// downstream mark on the (now next) line coherent.
func TestReplaceWithNewlineKeepsMark(t *testing.T) {
	e, w := newTestEditor(t, "aXb tail\n")
	w.SetCursorPos(window.Position{})
	w.Buffer.SetMark("m", 0, 4) // 't' of "tail"

	e.startFind("X", "", `\n`, true, true, true) // replace X with a line break
	answerPrompt(t, e, "y")                      // only one match; loop then ends

	if got := docContent(w); got != "a\nb tail" {
		t.Fatalf("newline-replace content: %q", got)
	}
	// "tail" moved to line 1; the mark should follow onto that line.
	if l, _ := mark(t, w, "m"); l != 1 {
		t.Fatalf("mark should follow onto line 1, got line %d", l)
	}
}

// The caret rides the buffer's dedicated block cursor: a caret inside the
// block slides with its line's indent; a caret outside the block does not.
func TestBlockIndentCaretInsideSlides(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb\nccc\n")
	w.Buffer.SetMark("_block_begin", 0, 0)
	w.Buffer.SetMark("_block_end", 2, 3)
	w.SetCursorPos(window.Position{Line: 1, Rune: 2}) // inside the block

	e.PawScript.ExecuteAsync("block_indent") // 4 spaces per line

	if w.CursorPos().Line != 1 || w.CursorPos().Rune != 6 { // slid right by 4
		t.Fatalf("caret inside block: %v, want (1,6)", w.CursorPos())
	}
}

func TestBlockIndentCaretOutsideUnmoved(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb\nccc\nZZZ\n")
	w.Buffer.SetMark("_block_begin", 0, 0)
	w.Buffer.SetMark("_block_end", 1, 3)              // block is only lines 0-1
	w.SetCursorPos(window.Position{Line: 3, Rune: 2}) // below the block

	e.PawScript.ExecuteAsync("block_indent")

	// Line 3 was not indented; the caret's line/rune are unchanged even though
	// its absolute byte offset shifted.
	if w.CursorPos().Line != 3 || w.CursorPos().Rune != 2 {
		t.Fatalf("caret below block: %v, want (3,2)", w.CursorPos())
	}
	if got := docContent(w); got != "    aaa\n    bbb\nccc\nZZZ" {
		t.Fatalf("partial-block indent content: %q", got)
	}
}

func TestBlockUnindentCaretSlidesLeft(t *testing.T) {
	e, w := newTestEditor(t, "    aaa\n    bbb\n    ccc\n")
	w.Buffer.SetMark("_block_begin", 0, 0)
	w.Buffer.SetMark("_block_end", 2, 7)
	w.SetCursorPos(window.Position{Line: 1, Rune: 6}) // a 'b'

	e.PawScript.ExecuteAsync("block_unindent")

	if w.CursorPos().Line != 1 || w.CursorPos().Rune != 2 { // slid left by 4
		t.Fatalf("caret after unindent: %v, want (1,2)", w.CursorPos())
	}
}

// A tab counts as one full indent step; unindent removes it whole.
func TestBlockUnindentTab(t *testing.T) {
	e, w := newTestEditor(t, "\taaa\n\tbbb\n")
	w.Buffer.SetMark("_block_begin", 0, 0)
	w.Buffer.SetMark("_block_end", 1, 4)
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})

	e.PawScript.ExecuteAsync("block_unindent")

	if got := docContent(w); got != "aaa\nbbb" {
		t.Fatalf("tab unindent content: %q", got)
	}
}

// A single-line block indents exactly that line.
func TestBlockIndentSingleLine(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb\nccc\n")
	w.Buffer.SetMark("_block_begin", 1, 0)
	w.Buffer.SetMark("_block_end", 1, 3)
	w.SetCursorPos(window.Position{Line: 1, Rune: 1})

	e.PawScript.ExecuteAsync("block_indent")

	if got := docContent(w); got != "aaa\n    bbb\nccc" {
		t.Fatalf("single-line indent content: %q", got)
	}
	if w.CursorPos().Line != 1 || w.CursorPos().Rune != 5 {
		t.Fatalf("caret: %v, want (1,5)", w.CursorPos())
	}
}

// The block's end mark on the LAST line (no trailing newline after it) still
// terminates the walk cleanly.
func TestBlockIndentLastLineNoTrailingNewline(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb")
	w.Buffer.SetMark("_block_begin", 0, 0)
	w.Buffer.SetMark("_block_end", 1, 3)
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})

	e.PawScript.ExecuteAsync("block_indent")

	if got := docContent(w); got != "    aaa\n    bbb" {
		t.Fatalf("no-trailing-newline indent: %q", got)
	}
}

// Block indent leaves empty and whitespace-only lines untouched (indenting
// them would only add trailing whitespace).
func TestBlockIndentSkipsBlankLines(t *testing.T) {
	e, w := newTestEditor(t, "aaa\n\n  \nbbb\n")
	w.Buffer.SetMark("_block_begin", 0, 0)
	w.Buffer.SetMark("_block_end", 3, 3)
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})

	e.PawScript.ExecuteAsync("block_indent")

	// Line 1 (empty) and line 2 ("  ") are unchanged; content lines gain indent.
	if got := docContent(w); got != "    aaa\n\n  \n    bbb" {
		t.Fatalf("blank-skip indent content: %q", got)
	}
}

// If every line in the block is blank, nothing is edited and the buffer is
// not marked modified.
func TestBlockIndentAllBlankNoOp(t *testing.T) {
	e, w := newTestEditor(t, "\n  \n\t\n")
	w.Buffer.SetMark("_block_begin", 0, 0)
	w.Buffer.SetMark("_block_end", 2, 0)
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	w.Buffer.SetModified(false)

	e.PawScript.ExecuteAsync("block_indent")

	if got := docContent(w); got != "\n  \n\t" {
		t.Fatalf("all-blank indent should be a no-op: %q", got)
	}
	if w.Buffer.IsModified() {
		t.Fatal("all-blank indent should not mark the buffer modified")
	}
}

// deleteBlock rides the block cursor: the caret slides with the deletion
// instead of being repositioned by hand.
func TestDeleteBlockCaretAfter(t *testing.T) {
	e, w := newTestEditor(t, "abcdefgh\n")
	w.Buffer.SetMark("_block_begin", 0, 2)
	w.Buffer.SetMark("_block_end", 0, 5)              // deletes "cde"
	w.SetCursorPos(window.Position{Line: 0, Rune: 7}) // 'h', after the block

	e.PawScript.ExecuteAsync("block_delete")

	if got := docContent(w); got != "abfgh" {
		t.Fatalf("content: %q", got)
	}
	if w.CursorPos().Line != 0 || w.CursorPos().Rune != 4 { // slid back by 3
		t.Fatalf("caret after delete: %v, want (0,4)", w.CursorPos())
	}
}

func TestDeleteBlockCaretInside(t *testing.T) {
	e, w := newTestEditor(t, "abcdefgh\n")
	w.Buffer.SetMark("_block_begin", 0, 2)
	w.Buffer.SetMark("_block_end", 0, 5)
	w.SetCursorPos(window.Position{Line: 0, Rune: 3}) // inside the block

	e.PawScript.ExecuteAsync("block_delete")

	if w.CursorPos().Line != 0 || w.CursorPos().Rune != 2 { // collapsed to deletion point
		t.Fatalf("caret inside deleted block: %v, want (0,2)", w.CursorPos())
	}
}

func TestDeleteBlockCaretBefore(t *testing.T) {
	e, w := newTestEditor(t, "abcdefgh\n")
	w.Buffer.SetMark("_block_begin", 0, 2)
	w.Buffer.SetMark("_block_end", 0, 5)
	w.SetCursorPos(window.Position{Line: 0, Rune: 1}) // before the block

	e.PawScript.ExecuteAsync("block_delete")

	if w.CursorPos().Line != 0 || w.CursorPos().Rune != 1 { // unchanged
		t.Fatalf("caret before deleted block: %v, want (0,1)", w.CursorPos())
	}
}

func TestDeleteBlockCaretAfterMultiline(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb\nccc\n")
	w.Buffer.SetMark("_block_begin", 0, 1)
	w.Buffer.SetMark("_block_end", 1, 2)              // deletes "aa\nbb"
	w.SetCursorPos(window.Position{Line: 2, Rune: 1}) // 'c' on line 2

	e.PawScript.ExecuteAsync("block_delete")

	if got := docContent(w); got != "ab\nccc" {
		t.Fatalf("content: %q", got)
	}
	if w.CursorPos().Line != 1 || w.CursorPos().Rune != 1 {
		t.Fatalf("caret: %v, want (1,1)", w.CursorPos())
	}
}

// moveBlock: the block is removed and reinserted at the (slid) caret, which
// lands at the start of the moved text.
func TestMoveBlockCaretAfter(t *testing.T) {
	e, w := newTestEditor(t, "AAA\nBBB\nCCC\n")
	w.Buffer.SetMark("_block_begin", 0, 0)
	w.Buffer.SetMark("_block_end", 1, 0)              // block is "AAA\n"
	w.SetCursorPos(window.Position{Line: 2, Rune: 0}) // start of CCC, after the block

	e.PawScript.ExecuteAsync("block_move")

	if got := docContent(w); got != "BBB\nAAA\nCCC" {
		t.Fatalf("moved content: %q", got)
	}
	if w.CursorPos().Line != 1 || w.CursorPos().Rune != 0 { // start of the moved block
		t.Fatalf("caret after move: %v, want (1,0)", w.CursorPos())
	}
}
