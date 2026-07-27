package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// isearchEditor opens content, starts the incremental search, and returns the
// editor, the target doc viewport, and the search prompt.
func isearchEditor(t *testing.T, content string) (*Editor, *viewport.Viewport, *viewport.Viewport) {
	t.Helper()
	e, w := newTestEditor(t, content)
	e.executeCommand("search")
	fp := focusedPrompt(e)
	if fp == nil {
		t.Fatal("search should open a focused prompt")
	}
	if fp.AfterKey != "isearch_key" {
		t.Fatalf("prompt AfterKey = %q, want isearch_key", fp.AfterKey)
	}
	return e, w, fp
}

// Typing drives the caret to the first match of the growing pattern; the
// prompt keeps the keyboard throughout.
func TestIsearchTypingTracksMatches(t *testing.T) {
	e, w, fp := isearchEditor(t, "cargo\nbanana\nbend\n")

	e.dispatchKey("b")
	if got := w.CursorPos(); got.Line != 1 || got.Rune != 0 {
		t.Fatalf("after b: caret %v, want banana at 1:0", got)
	}
	e.dispatchKey("e")
	if got := w.CursorPos(); got.Line != 2 || got.Rune != 0 {
		t.Fatalf("after be: caret %v, want bend at 2:0", got)
	}
	if e.ViewportManager.GetFocusedViewport() != fp {
		t.Fatal("prompt must keep focus while searching")
	}
	if w.Find.Term != "be" {
		t.Fatalf("find state term = %q, want be", w.Find.Term)
	}
}

// Backspace shortens the pattern and pops the caret back to where the shorter
// pattern matched.
func TestIsearchBackspacePops(t *testing.T) {
	e, w, _ := isearchEditor(t, "cargo\nbanana\nbend\n")

	e.dispatchKey("b") // banana 1:0
	e.dispatchKey("e") // bend   2:0
	e.dispatchKey("back")

	if got := w.CursorPos(); got.Line != 1 || got.Rune != 0 {
		t.Fatalf("after backspace: caret %v, want back at banana 1:0", got)
	}
	if w.Find.Term != "b" {
		t.Fatalf("find state term = %q, want b", w.Find.Term)
	}
}

// find_next during the search rotates to the next occurrence of the current
// increment without stealing focus from the prompt.
func TestIsearchFindNextRotates(t *testing.T) {
	e, w, fp := isearchEditor(t, "cat cat cat\n")

	e.dispatchKey("c")
	e.dispatchKey("a")
	e.dispatchKey("t")
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 0 {
		t.Fatalf("caret %v, want first cat at 0:0", got)
	}

	e.executeCommand("find_next")
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 4 {
		t.Fatalf("after find_next: caret %v, want second cat at 0:4", got)
	}
	e.executeCommand("find_next")
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 8 {
		t.Fatalf("after find_next x2: caret %v, want third cat at 0:8", got)
	}
	if e.ViewportManager.GetFocusedViewport() != fp {
		t.Fatal("find_next must not steal focus from the search prompt")
	}
}

// Enter accepts: the prompt closes, the caret stays on the match, and
// find_next keeps rotating the committed pattern afterwards.
func TestIsearchEnterAcceptsAndFindNextContinues(t *testing.T) {
	e, w, _ := isearchEditor(t, "cat cat cat\n")

	e.dispatchKey("c")
	e.dispatchKey("a")
	e.dispatchKey("t")
	e.dispatchKey("return")

	if focusedPrompt(e) != nil {
		t.Fatal("prompt should be closed")
	}
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 0 {
		t.Fatalf("caret %v, want to stay on the match at 0:0", got)
	}
	if w.Find.Term != "cat" {
		t.Fatalf("find state term = %q, want cat", w.Find.Term)
	}
	e.executeCommand("find_next")
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 4 {
		t.Fatalf("after find_next: caret %v, want 0:4", got)
	}
}

// Cancel (^C) closes the prompt and returns the caret to where the search
// began.
func TestIsearchCancelRestoresOrigin(t *testing.T) {
	e, w, _ := isearchEditor(t, "cargo\nbanana\nbend\n")

	e.dispatchKey("b") // caret moves to banana
	e.dispatchKey("^C")

	if focusedPrompt(e) != nil {
		t.Fatal("prompt should be closed")
	}
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 0 {
		t.Fatalf("caret %v, want restored origin 0:0", got)
	}
}

// search_reverse / search_forward step an occurrence in their direction and
// commit it as the find state's "b" option; the prompt label follows.
func TestIsearchDirectionKeys(t *testing.T) {
	e, w, fp := isearchEditor(t, "cat cat cat\n")

	e.dispatchKey("c")
	e.dispatchKey("a")
	e.dispatchKey("t")
	e.executeCommand("find_next") // 0:4

	e.executeCommand("search_reverse")
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 0 {
		t.Fatalf("after search_reverse: caret %v, want previous cat at 0:0", got)
	}
	if w.Find.Options != "b" {
		t.Fatalf("find options = %q, want b committed", w.Find.Options)
	}
	if len(fp.RowMessages) == 0 || !strings.Contains(fp.RowMessages[0], "back") {
		t.Fatalf("prompt label = %v, want a reverse label", fp.RowMessages)
	}

	e.executeCommand("search_forward")
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 4 {
		t.Fatalf("after search_forward: caret %v, want next cat at 0:4", got)
	}
	if w.Find.Options != "" {
		t.Fatalf("find options = %q, want forward committed", w.Find.Options)
	}
}

// A fresh search inherits the direction stored in the find state (the same
// "b" option find/find_next use).
func TestIsearchInheritsStoredDirection(t *testing.T) {
	e, w := newTestEditor(t, "aba\n")
	w.Find = viewport.FindState{Term: "old", Options: "b"}
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 1})

	e.executeCommand("search")
	if e.isearch == nil || !e.isearch.backwards {
		t.Fatal("search should start backwards per the stored find direction")
	}
	e.dispatchKey("a")
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 0 {
		t.Fatalf("caret %v, want the a BEFORE the origin at 0:0", got)
	}
}

// The ^R/^F direction keys resolve through the prompt's class keymap — active
// only while the isearch prompt is focused.
func TestIsearchDirectionKeyBinding(t *testing.T) {
	e, _, _ := isearchEditor(t, "cat cat\n")
	e.reconcileFocusedOptions() // apply the isearch class keymap refinement

	e.dispatchKey("^R")
	if e.isearch == nil || !e.isearch.backwards {
		t.Fatal("^R in the search prompt should run search_reverse")
	}
	e.dispatchKey("^F")
	if e.isearch == nil || e.isearch.backwards {
		t.Fatal("^F in the search prompt should run search_forward")
	}
}

// A pattern with no match leaves the caret where it was and warns; erasing
// back to empty restores the pre-search find state.
func TestIsearchNotFoundAndEmptyRestore(t *testing.T) {
	e, w, _ := isearchEditor(t, "cargo\nbanana\n")
	w.Find = viewport.FindState{} // (already zero; explicit for the assert below)

	e.dispatchKey("z")
	if !hasWarning(e, "Not found") {
		t.Fatal("missing Not found warning")
	}
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 0 {
		t.Fatalf("caret %v, want unmoved 0:0", got)
	}

	e.dispatchKey("back")
	if w.Find.Term != "" {
		t.Fatalf("find term = %q, want pre-search state restored", w.Find.Term)
	}
	if focusedPrompt(e) == nil {
		t.Fatal("empty pattern keeps the prompt open")
	}
}
