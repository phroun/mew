package editor

import "testing"

// Consecutive typing collapses into a single undo step: garland coalesces the
// adjacent inserts (the commands flow bare, unwrapped) into one revision.
func TestUndoCoalescesTyping(t *testing.T) {
	e, w := newTestEditor(t, "")
	for _, ch := range []string{"h", "e", "y"} {
		e.executeCommand(`insert "` + ch + `"`)
	}
	if got := docContent(w); got != "hey" {
		t.Fatalf("typed content = %q, want hey", got)
	}
	if !w.Buffer.Undo() {
		t.Fatal("expected an undo step")
	}
	if got := docContent(w); got != "" {
		t.Fatalf("one undo should erase the whole typing run, got %q", got)
	}
}

// A cursor reposition between edits ends the run: the editor bakes the undo
// history after any non-editing command, so the next keystroke starts fresh.
func TestUndoBreaksOnCursorMove(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`insert "a"`)
	e.executeCommand(`insert "b"`)  // "ab" — one run
	e.executeCommand("go_line_beg") // reposition → bake
	e.executeCommand(`insert "X"`)  // "Xab" — a new run

	if got := docContent(w); got != "Xab" {
		t.Fatalf("content = %q, want Xab", got)
	}
	if !w.Buffer.Undo() {
		t.Fatal("undo 1")
	}
	if got := docContent(w); got != "ab" {
		t.Fatalf("a cursor move must break the run: after one undo got %q, want ab", got)
	}
	if !w.Buffer.Undo() {
		t.Fatal("undo 2")
	}
	if got := docContent(w); got != "" {
		t.Fatalf("the second undo should erase the first run, got %q", got)
	}
}

// Typing and deleting are different kinds and never merge into one undo step.
func TestUndoSeparatesTypingFromDeleting(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`insert "a"`)
	e.executeCommand(`insert "b"`)     // "ab"
	e.executeCommand("del_char_prior") // backspace → "a"

	if got := docContent(w); got != "a" {
		t.Fatalf("content = %q, want a", got)
	}
	if !w.Buffer.Undo() {
		t.Fatal("undo")
	}
	if got := docContent(w); got != "ab" {
		t.Fatalf("the delete should be its own undo step: got %q, want ab", got)
	}
}

// buffer_tx_start/commit wrap a whole edit sequence — even across a cursor move
// — into ONE undo revision.
func TestBufferTxGroupsEdits(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`buffer_tx_start "grp"; insert "a"; go_line_beg; insert "b"; buffer_tx_commit`)
	if got := docContent(w); got != "ba" {
		t.Fatalf("content = %q, want ba", got)
	}
	if !w.Buffer.Undo() {
		t.Fatal("undo")
	}
	if got := docContent(w); got != "" {
		t.Fatalf("the whole transaction should undo in one step, got %q", got)
	}
}

// buffer_tx_cancel rolls the whole grouped run back to the pre-transaction
// content.
func TestBufferTxCancelRollsBack(t *testing.T) {
	e, w := newTestEditor(t, "hello\n")
	e.executeCommand(`buffer_tx_start "grp"; insert "X"; insert "Y"; buffer_tx_cancel`)
	if got := docContent(w); got != "hello" {
		t.Fatalf("cancel should roll every grouped edit back, got %q", got)
	}
}

// A stray buffer_tx_start with no matching commit does not leak past its command
// dispatch: the editor closes it, so the next edit is not swallowed into it.
func TestBufferTxDanglingIsClosed(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`buffer_tx_start "leak"; insert "a"`) // no commit
	e.executeCommand(`insert "b"`)                         // must not join the dangling tx

	if got := docContent(w); got != "ab" {
		t.Fatalf("content = %q, want ab", got)
	}
	// "a" was committed when the dangling tx was force-closed; "b" is its own
	// step. Two undos, not one.
	if !w.Buffer.Undo() || docContent(w) != "a" {
		t.Fatalf("first undo should remove only b, got %q", docContent(w))
	}
	if !w.Buffer.Undo() || docContent(w) != "" {
		t.Fatalf("second undo should remove a, got %q", docContent(w))
	}
}
