package editor

import (
	"fmt"
	"strings"
	"testing"
)

// The cursor ring remembers where the caret has recently edited, so go_pos_prior
// and go_pos_next walk the caret back and forth through that history.
func TestCursorRingWalksEditHistory(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb\nccc\nddd\neee\n")

	// Edit on line 1, then edit on line 3.
	e.PawScript.ExecuteAsync("go_line_next") // -> line 1
	e.PawScript.ExecuteAsync(`insert "X"`)
	e.PawScript.ExecuteAsync("go_line_next")
	e.PawScript.ExecuteAsync("go_line_next") // -> line 3
	e.PawScript.ExecuteAsync(`insert "Y"`)

	// The caret is on the latest edit (line 3). Walking prior visits the
	// previous edit (line 1), then the original position (line 0).
	e.PawScript.ExecuteAsync("go_pos_prior")
	if got := w.CursorPos().Line; got != 1 {
		t.Fatalf("first go_pos_prior: line %d, want 1", got)
	}
	e.PawScript.ExecuteAsync("go_pos_prior")
	if got := w.CursorPos().Line; got != 0 {
		t.Fatalf("second go_pos_prior: line %d, want 0", got)
	}
	// Nothing older to go to: the caret stays put.
	e.PawScript.ExecuteAsync("go_pos_prior")
	if got := w.CursorPos().Line; got != 0 {
		t.Fatalf("go_pos_prior past the oldest should stay on line 0, got %d", got)
	}

	// Walking forward retraces: line 1, then the latest edit on line 3.
	e.PawScript.ExecuteAsync("go_pos_next")
	if got := w.CursorPos().Line; got != 1 {
		t.Fatalf("first go_pos_next: line %d, want 1", got)
	}
	e.PawScript.ExecuteAsync("go_pos_next")
	if got := w.CursorPos().Line; got != 3 {
		t.Fatalf("second go_pos_next: line %d, want 3", got)
	}
	// Nothing newer: the caret stays put.
	e.PawScript.ExecuteAsync("go_pos_next")
	if got := w.CursorPos().Line; got != 3 {
		t.Fatalf("go_pos_next past the newest should stay on line 3, got %d", got)
	}
}

// When the caret has been moved away from the last edit without editing, the
// first go_pos_prior returns it to that most recent edit (rather than skipping
// straight to the previous one).
func TestCursorRingPriorReturnsToLastEditWhenMovedAway(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb\nccc\nddd\neee\n")

	e.PawScript.ExecuteAsync("go_line_next")
	e.PawScript.ExecuteAsync("go_line_next") // -> line 2
	e.PawScript.ExecuteAsync(`insert "Z"`)   // last edit is line 2

	// Navigate away without editing.
	e.PawScript.ExecuteAsync("go_line_next")
	e.PawScript.ExecuteAsync("go_line_next") // -> line 4
	if got := w.CursorPos().Line; got != 4 {
		t.Fatalf("setup: caret should be on line 4, got %d", got)
	}

	e.PawScript.ExecuteAsync("go_pos_prior")
	if got := w.CursorPos().Line; got != 2 {
		t.Fatalf("go_pos_prior after moving away should return to line 2, got %d", got)
	}
	// Continuing prior reaches the original position.
	e.PawScript.ExecuteAsync("go_pos_prior")
	if got := w.CursorPos().Line; got != 0 {
		t.Fatalf("second go_pos_prior should reach line 0, got %d", got)
	}
}

// Returning the caret exactly to the last edit point clears hasMoved, so the
// next edit does not record a new ring entry — the history stays compact.
func TestCursorRingNoEntryWhenEditAtSamePoint(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb\nccc\nddd\neee\n")

	e.PawScript.ExecuteAsync("go_line_next") // -> line 1
	e.PawScript.ExecuteAsync(`insert "A"`)   // edit at line 1, rune 1

	// Move down and back to the exact same spot (the ideal column carries the
	// rune), then edit again. The caret returned to the last edit point, so no
	// new ring entry is pushed.
	e.PawScript.ExecuteAsync("go_line_next")
	e.PawScript.ExecuteAsync("go_line_prior") // back to line 1, rune 1
	if got := w.CursorPos().Line; got != 1 {
		t.Fatalf("setup: caret should be back on line 1, got %d", got)
	}
	e.PawScript.ExecuteAsync(`insert "B"`)

	// The only ring entry is the original position: one prior reaches line 0,
	// and there is nothing older. (A spurious entry would have stopped on
	// line 1 first.)
	e.PawScript.ExecuteAsync("go_pos_prior")
	if got := w.CursorPos().Line; got != 0 {
		t.Fatalf("go_pos_prior should reach line 0, got %d", got)
	}
	e.PawScript.ExecuteAsync("go_pos_prior")
	if got := w.CursorPos().Line; got != 0 {
		t.Fatalf("there should be no entry behind line 0, got %d", got)
	}
}

// The ring holds a fixed number of entries; once full it overwrites the oldest,
// so navigation can only reach back as far as the retained window of history.
func TestCursorRingOverwritesOldest(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 16; i++ {
		fmt.Fprintf(&sb, "l%d\n", i)
	}
	e, w := newTestEditor(t, sb.String())

	// Edit on lines 1..12 in order (12 distinct edit sites, more than the ring
	// can hold), pushing the earliest positions (lines 0 and 1) out.
	for i := 1; i <= 12; i++ {
		e.PawScript.ExecuteAsync("go_line_next")
		e.PawScript.ExecuteAsync(`insert "."`)
	}

	// Walk prior far more than the ring depth; it must settle on the oldest
	// still-remembered edit, never reaching the overwritten lines 0 or 1.
	for i := 0; i < 20; i++ {
		e.PawScript.ExecuteAsync("go_pos_prior")
	}
	if got := w.CursorPos().Line; got != 2 {
		t.Fatalf("oldest reachable edit should be line 2, got %d", got)
	}
}

// Every buffer window has its own ring, prompts included: go_pos_* on a fresh
// prompt (no history yet) is a well-behaved no-op, and the prompt's ring is
// independent of the document's.
func TestCursorRingPerWindowIncludingPrompt(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb\nccc\n")

	// Give the document some edit history.
	e.PawScript.ExecuteAsync("go_line_next")
	e.PawScript.ExecuteAsync(`insert "X"`)

	e.PromptForInput("x: ", "", func(string, bool) {})
	if focusedPrompt(e) == nil {
		t.Fatal("expected a focused prompt")
	}
	// The prompt has its own empty ring: navigation is inert and must not touch
	// the document's caret.
	e.PawScript.ExecuteAsync("go_pos_prior")
	e.PawScript.ExecuteAsync("go_pos_next")

	cancelPrompt(t, e)
	// The document's own history is intact: go_pos_prior still reaches line 0.
	e.PawScript.ExecuteAsync("go_pos_prior")
	if got := w.CursorPos().Line; got != 0 {
		t.Fatalf("document ring should be unaffected by the prompt, got line %d", got)
	}
}
