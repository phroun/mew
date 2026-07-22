package editor

import (
	"testing"

	"github.com/phroun/mew/internal/window"
)

// stubClipboard wires Config.ClipboardWrite/ClipboardRead to an in-memory
// string, with a synchronous inline read (delivery still marshals through
// PostAction in os_paste; tests that need the applied result call osPasteText
// directly, since the headless harness does not run the main loop).
func stubClipboard(e *Editor) *string {
	clip := new(string)
	e.Config.ClipboardWrite = func(s string) { *clip = s }
	e.Config.ClipboardRead = func(deliver func(string)) { deliver(*clip) }
	return clip
}

func markBlock(w *window.Window, sl, sr, el, er int) {
	w.Buffer.SetMark("_block_begin", sl, sr)
	w.Buffer.SetMark("_block_end", el, er)
}

// os_copy places the block text on the host clipboard and touches nothing.
func TestOSCopy(t *testing.T) {
	e, w := newTestEditor(t, "hello world\n")
	clip := stubClipboard(e)
	markBlock(w, 0, 0, 0, 5)

	e.executeCommand("os_copy")
	if *clip != "hello" {
		t.Fatalf("clipboard after os_copy: %q, want %q", *clip, "hello")
	}
	if got := docContent(w); got != "hello world" {
		t.Fatalf("os_copy must not modify the buffer: %q", got)
	}
	if len(e.killRing) != 0 {
		t.Fatalf("os_copy must not touch the kill ring, has %d entries", len(e.killRing))
	}
}

// os_copy with no block marked warns and writes nothing.
func TestOSCopyNoBlock(t *testing.T) {
	e, w := newTestEditor(t, "hello\n")
	clip := stubClipboard(e)
	*clip = "sentinel"
	_ = w

	e.executeCommand("os_copy")
	if *clip != "sentinel" {
		t.Fatalf("no block: clipboard must be untouched, got %q", *clip)
	}
	if !hasWarning(e, "No block marked") {
		t.Fatal("expected the no-block warning")
	}
}

// os_cut removes the block WITHOUT killing it into the ring: a prior kill
// stays the newest ring entry, so yank after os_cut inserts the OLD kill,
// never the cut text. One undo reverses the cut.
func TestOSCutBypassesKillRing(t *testing.T) {
	e, w := newTestEditor(t, "AAA\nworld\n")
	clip := stubClipboard(e)

	// Prime the kill ring with a real kill (block_delete of "AAA\n").
	markBlock(w, 0, 0, 1, 0)
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand("block_delete")
	if len(e.killRing) != 1 {
		t.Fatalf("precondition: one kill entry, have %d", len(e.killRing))
	}

	// os_cut "world" — clipboard gets it, ring must NOT.
	markBlock(w, 0, 0, 0, 5)
	e.executeCommand("os_cut")
	if *clip != "world" {
		t.Fatalf("clipboard after os_cut: %q", *clip)
	}
	if got := docContent(w); got != "" {
		t.Fatalf("buffer after os_cut: %q, want empty", got)
	}
	if len(e.killRing) != 1 {
		t.Fatalf("os_cut leaked into the kill ring: %d entries", len(e.killRing))
	}
	if _, _, ok := w.Buffer.GetMark("_block_begin"); ok {
		t.Fatal("block marks should be cleared by os_cut")
	}

	// Yank must reproduce the PRIOR kill ("AAA\n"), not the cut text.
	e.executeCommand("kill_ring_yank")
	if got := docContent(w); got != "AAA" {
		t.Fatalf("yank after os_cut should insert the old kill: %q", got)
	}

	// Undo the yank, then undo the cut: "world" returns.
	e.executeCommand("buffer_undo")
	e.executeCommand("buffer_undo")
	if got := docContent(w); got != "world" {
		t.Fatalf("undo of os_cut should restore the block in one step: %q", got)
	}
}

// osPasteText with the caret inside the marked block replaces the block
// (block_from_file semantics) and re-marks the pasted text.
func TestOSPasteReplacesEngagedBlock(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb\nccc\n")
	markBlock(w, 1, 0, 1, 3)
	w.SetCursorPos(window.Position{Line: 1, Rune: 1})

	e.osPasteText("XY")
	if got := docContent(w); got != "aaa\nXY\nccc" {
		t.Fatalf("engaged paste content: %q", got)
	}
	if l, r := mark(t, w, "_block_begin"); l != 1 || r != 0 {
		t.Fatalf("_block_begin after paste: (%d,%d)", l, r)
	}
	if l, r := mark(t, w, "_block_end"); l != 1 || r != 2 {
		t.Fatalf("_block_end after paste: (%d,%d)", l, r)
	}
}

// osPasteText with no block (or the caret away from it) inserts at the caret
// (buffer_insert_file semantics), normalizing line endings, one undo step.
func TestOSPasteInsertsAtCaret(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb\n")

	// No block at all: plain insert.
	w.SetCursorPos(window.Position{Line: 1, Rune: 0})
	e.osPasteText("X\r\nY")
	if got := docContent(w); got != "aaa\nX\nYbbb" {
		t.Fatalf("no-block paste: %q", got)
	}
	e.executeCommand("buffer_undo")
	if got := docContent(w); got != "aaa\nbbb" {
		t.Fatalf("paste should undo as one step: %q", got)
	}

	// Block marked but caret OUTSIDE it: still a plain insert, block kept.
	markBlock(w, 0, 0, 0, 3)
	w.SetCursorPos(window.Position{Line: 1, Rune: 3})
	e.osPasteText("!")
	if got := docContent(w); got != "aaa\nbbb!" {
		t.Fatalf("caret-outside paste: %q", got)
	}
	if l, r := mark(t, w, "_block_begin"); l != 0 || r != 0 {
		t.Fatalf("existing block should survive a caret-outside paste: (%d,%d)", l, r)
	}
}

// os_paste wires the host read: the deliver callback is marshaled via
// PostAction. In the headless harness there is no postable source, so here we
// only assert the read is invoked; the applied semantics are covered above.
func TestOSPasteInvokesHostRead(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	read := false
	e.Config.ClipboardRead = func(deliver func(string)) { read = true; deliver("ignored") }
	e.executeCommand("os_paste")
	if !read {
		t.Fatal("os_paste must invoke Config.ClipboardRead")
	}
}

// os_select_all marks the whole buffer as the block without moving the caret.
func TestOSSelectAll(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbb\n")
	w.SetCursorPos(window.Position{Line: 1, Rune: 1})

	e.executeCommand("os_select_all")
	if l, r := mark(t, w, "_block_begin"); l != 0 || r != 0 {
		t.Fatalf("_block_begin: (%d,%d)", l, r)
	}
	// The last content line is the trailing empty line (index 2, length 0).
	if l, r := mark(t, w, "_block_end"); !(l == 2 && r == 0) {
		t.Fatalf("_block_end: (%d,%d), want (2,0)", l, r)
	}
	if pos := w.CursorPos(); pos.Line != 1 || pos.Rune != 1 {
		t.Fatalf("caret must not move: %+v", pos)
	}
}

// With no host clipboard wired, the os_* commands warn instead of acting.
func TestOSClipboardUnwired(t *testing.T) {
	e, w := newTestEditor(t, "hello\n")
	markBlock(w, 0, 0, 0, 5)
	e.executeCommand("os_cut")
	if got := docContent(w); got != "hello" {
		t.Fatalf("os_cut without a host clipboard must not delete: %q", got)
	}
	if !hasWarning(e, "No host clipboard") {
		t.Fatal("expected the no-host-clipboard warning")
	}
}
