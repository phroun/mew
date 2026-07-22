package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/phroun/mew/internal/window"
)

// blockFromFileWith drives the real command flow: mark a block, place the
// caret, run block_from_file, and answer its filename prompt with path. It
// returns whether a prompt was raised (the gate passed).
func runBlockFromFile(t *testing.T, e *Editor, path string) bool {
	t.Helper()
	e.PawScript.ExecuteAsync("block_from_file")
	if focusedPrompt(e) == nil {
		return false
	}
	answerPrompt(t, e, path)
	return true
}

// A file streamed over a marked block replaces exactly the block's contents and
// leaves the newly inserted text marked as the block.
func TestBlockFromFileReplacesAndRemarks(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "insert.txt")
	if err := os.WriteFile(src, []byte("XY"), 0o644); err != nil {
		t.Fatal(err)
	}

	e, w := newTestEditor(t, "aaa\nbbb\nccc\n")
	w.Buffer.SetMark("_block_begin", 1, 0)
	w.Buffer.SetMark("_block_end", 1, 3) // block is "bbb"
	w.SetCursorPos(window.Position{Line: 1, Rune: 1})

	if !runBlockFromFile(t, e, src) {
		t.Fatal("block_from_file did not prompt with caret inside the block")
	}

	if got := docContent(w); got != "aaa\nXY\nccc" {
		t.Fatalf("content after stream: %q, want %q", got, "aaa\\nXY\\nccc")
	}
	// The block now surrounds the streamed-in text, not the old "bbb".
	if l, r := mark(t, w, "_block_begin"); l != 1 || r != 0 {
		t.Fatalf("_block_begin after stream: (%d,%d), want (1,0)", l, r)
	}
	if l, r := mark(t, w, "_block_end"); l != 1 || r != 2 {
		t.Fatalf("_block_end after stream: (%d,%d), want (1,2)", l, r)
	}
}

// A multi-line file is streamed verbatim (line endings normalized), and one
// undo reverses the whole replace.
func TestBlockFromFileMultilineUndoesAsOne(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "insert.txt")
	if err := os.WriteFile(src, []byte("XX\r\nYY"), 0o644); err != nil { // CRLF normalized
		t.Fatal(err)
	}

	e, w := newTestEditor(t, "aaa\nbbb\nccc\n")
	w.Buffer.SetMark("_block_begin", 1, 0)
	w.Buffer.SetMark("_block_end", 1, 3)
	w.SetCursorPos(window.Position{Line: 1, Rune: 0}) // on the very edge

	if !runBlockFromFile(t, e, src) {
		t.Fatal("block_from_file did not prompt with caret on the block edge")
	}
	if got := docContent(w); got != "aaa\nXX\nYY\nccc" {
		t.Fatalf("content after multiline stream: %q", got)
	}

	e.PawScript.ExecuteAsync("buffer_undo")
	if got := docContent(w); got != "aaa\nbbb\nccc" {
		t.Fatalf("content after undo: %q, want the original block restored", got)
	}
}

// The gate refuses to prompt when the caret is outside the block, leaving the
// buffer untouched.
func TestBlockFromFileRefusesCaretOutsideBlock(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "insert.txt")
	if err := os.WriteFile(src, []byte("XY"), 0o644); err != nil {
		t.Fatal(err)
	}

	e, w := newTestEditor(t, "aaa\nbbb\nccc\n")
	w.Buffer.SetMark("_block_begin", 1, 0)
	w.Buffer.SetMark("_block_end", 1, 3)
	w.SetCursorPos(window.Position{Line: 0, Rune: 0}) // outside the block

	if runBlockFromFile(t, e, src) {
		t.Fatal("block_from_file prompted with the caret outside the block")
	}
	if got := docContent(w); got != "aaa\nbbb\nccc" {
		t.Fatalf("content changed despite refusal: %q", got)
	}
}

// With no block marked at all, the command refuses.
func TestBlockFromFileRefusesNoBlock(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb\nccc\n")
	w.SetCursorPos(window.Position{Line: 1, Rune: 1})
	if runBlockFromFile(t, e, "/nonexistent") {
		t.Fatal("block_from_file prompted with no block marked")
	}
	if got := docContent(w); got != "aaa\nbbb\nccc" {
		t.Fatalf("content changed with no block: %q", got)
	}
}
