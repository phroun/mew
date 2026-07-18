package editor

import (
	"testing"

	"github.com/phroun/pawscript"
)

// redoStatus runs buffer_redo and reports the boolean status it returned.
func redoStatus(t *testing.T, e *Editor) bool {
	t.Helper()
	res := e.PawScript.ExecuteAsync("buffer_redo")
	bs, ok := res.(pawscript.BoolStatus)
	if !ok {
		t.Fatalf("buffer_redo returned %T, want pawscript.BoolStatus", res)
	}
	return bool(bs)
}

// buffer_redo reports false whenever there is nothing to redo — at the newest
// revision, on a fresh buffer, and after a fresh edit has abandoned the redo
// branch — and true only when it actually moves forward in history.
func TestBufferRedoStatus(t *testing.T) {
	e, _ := newTestEditor(t, "hello\n")

	// Fresh buffer, no history: nothing to redo.
	if redoStatus(t, e) {
		t.Fatal("buffer_redo on a fresh buffer should be false")
	}

	// Make an edit; we are now at the newest revision.
	e.PawScript.ExecuteAsync(`insert "X"`)
	if redoStatus(t, e) {
		t.Fatal("buffer_redo at the newest revision should be false")
	}

	// Undo, then redo actually moves forward: true.
	if res := e.PawScript.ExecuteAsync("buffer_undo"); res != pawscript.BoolStatus(true) {
		t.Fatalf("buffer_undo should be true, got %v", res)
	}
	if !redoStatus(t, e) {
		t.Fatal("buffer_redo after an undo should be true")
	}
	// Back at the end again: false.
	if redoStatus(t, e) {
		t.Fatal("buffer_redo once back at the newest revision should be false")
	}

	// A new edit after an undo abandons the redo branch: redo is false.
	e.PawScript.ExecuteAsync("buffer_undo")
	e.PawScript.ExecuteAsync(`insert "Y"`)
	if redoStatus(t, e) {
		t.Fatal("buffer_redo after a new edit should be false (redo branch abandoned)")
	}
}
