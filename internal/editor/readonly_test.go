package editor

import (
	"testing"

	"github.com/phroun/mew/internal/window"
)

// readOnly is a per-window boolean, default no.
func TestReadOnlyOption(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	if v, _ := e.getOption(w, "readOnly"); v != "no" {
		t.Fatalf("default readOnly = %q, want no", v)
	}
	e.setOption(w, "readOnly", "yes")
	if !w.ViewState.ReadOnly {
		t.Fatal("setOption should set the window ReadOnly flag")
	}
	if v, _ := e.getOption(w, "readOnly"); v != "yes" {
		t.Fatalf("readOnly after set = %q, want yes", v)
	}
	// Per window: the editor default is untouched.
	if e.Config.ReadOnly {
		t.Fatal("a per-window override must not change the editor default")
	}
}

// A read-only window rejects content-mutating commands (typing, deleting,
// pasting) with a warning and leaves the buffer unchanged.
func TestReadOnlyBlocksEdits(t *testing.T) {
	e, w := newTestEditor(t, "abc\n")
	w.ViewState.ReadOnly = true
	w.SetCursorPos(window.Position{Line: 0, Rune: 1})

	for _, cmd := range []string{`insert "X"`, "del_char_next", "del_char_prior", "del_line", "find_replace"} {
		e.executeCommand(cmd)
		if got := docContent(w); got != "abc" {
			t.Fatalf("after %q the read-only buffer changed to %q", cmd, got)
		}
	}
	if !hasWarning(e, "read-only") {
		t.Fatal("a blocked edit should warn that the buffer is read-only")
	}

	// The lock lives in the mutation's implementation, not a command-name gate,
	// so a mutation reached through a fallthrough chain (the way return/tab are
	// bound) is blocked too — the exact case a name-parsing gate would miss,
	// and it holds no matter what name the script reaches the mutation by.
	for _, cmd := range []string{`nav_follow|accept|insert "\n"`, `nav_next|completion|insert "\t"`} {
		e.executeCommand(cmd)
		if got := docContent(w); got != "abc" {
			t.Fatalf("chain %q must not edit a read-only buffer (got %q)", cmd, got)
		}
	}

	// Bracketed paste is gated on its own path.
	e.insertPasteChunk([]byte("paste"))
	if got := docContent(w); got != "abc" {
		t.Fatalf("paste into a read-only buffer changed it to %q", got)
	}
}

// Read-only allows navigation, marks, and search; only edits are blocked. And
// clearing readOnly restores editing.
func TestReadOnlyAllowsNavAndMarksAndUnlock(t *testing.T) {
	e, w := newTestEditor(t, "hello world\n")
	w.ViewState.ReadOnly = true

	// Cursor movement works.
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand("go_char_next")
	if w.CursorPos().Rune != 1 {
		t.Fatalf("cursor should move under read-only, at %d", w.CursorPos().Rune)
	}
	// Setting a block mark works (marks are not content).
	e.executeCommand("set_block_begin")
	if _, _, ok := w.Buffer.GetMark("_block_begin"); !ok {
		t.Fatal("set_block_begin should work under read-only")
	}

	// Turning read-only off restores editing.
	e.setOption(w, "readOnly", "no")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand(`insert "Z"`)
	if got := docContent(w); got != "Zhello world" {
		t.Fatalf("editing after unlock failed, content = %q", got)
	}
}
