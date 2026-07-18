package editor

import (
	"testing"

	"github.com/phroun/mew/internal/window"
)

// repeat_next with a count arms the window; the next keybound command runs
// wrapped in a PawScript repeat(...) that many times, then the arm clears.
func TestRepeatNextWrapsNextCommand(t *testing.T) {
	e, w := newTestEditor(t, "abcdef\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})

	e.executeCommand("repeat_next 3")
	if !w.Repeat.Pending || w.Repeat.Count != 3 {
		t.Fatalf("repeat_next should arm {Pending:true Count:3}, got %+v", w.Repeat)
	}

	e.executeCommand("del_char_next")
	if got := docContent(w); got != "def" {
		t.Fatalf("wrapped repeat should delete 3 chars -> %q, got %q", "def", got)
	}
	if w.Repeat.Pending {
		t.Fatal("the arm should be consumed (one-shot)")
	}

	// A following command is no longer wrapped: deletes just one.
	e.executeCommand("del_char_next")
	if got := docContent(w); got != "ef" {
		t.Fatalf("un-armed del should remove 1 char -> %q, got %q", "ef", got)
	}
}

// The count is clamped to the maxRepeat option.
func TestRepeatNextClampsToMax(t *testing.T) {
	e, w := newTestEditor(t, "abcdefghij\n", "maxRepeat=4")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})

	e.executeCommand("repeat_next 100")
	if w.Repeat.Count != 4 {
		t.Fatalf("count should clamp to maxRepeat 4, got %d", w.Repeat.Count)
	}
	e.executeCommand("del_char_next")
	if got := docContent(w); got != "efghij" {
		t.Fatalf("clamped repeat should delete 4 -> %q, got %q", "efghij", got)
	}
}

// repeat_next with no argument prompts for the count, then arms.
func TestRepeatNextPrompts(t *testing.T) {
	e, w := newTestEditor(t, "abcdef\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})

	e.executeCommand("repeat_next")
	if focusedPrompt(e) == nil {
		t.Fatal("repeat_next without an argument should open a prompt")
	}
	answerPrompt(t, e, "2")
	if !w.Repeat.Pending || w.Repeat.Count != 2 {
		t.Fatalf("prompt should arm {Pending:true Count:2}, got %+v", w.Repeat)
	}

	e.executeCommand("del_char_next")
	if got := docContent(w); got != "cdef" {
		t.Fatalf("prompted repeat should delete 2 -> %q, got %q", "cdef", got)
	}
}

// A repeat_next while one is already armed re-arms rather than wrapping itself.
func TestRepeatNextReArms(t *testing.T) {
	e, w := newTestEditor(t, "abcdefgh\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})

	e.executeCommand("repeat_next 5")
	e.executeCommand("repeat_next 2") // must not be wrapped; re-arms to 2
	if !w.Repeat.Pending || w.Repeat.Count != 2 {
		t.Fatalf("second repeat_next should re-arm to 2, got %+v", w.Repeat)
	}
	e.executeCommand("del_char_next")
	if got := docContent(w); got != "cdefgh" {
		t.Fatalf("should delete 2 -> %q, got %q", "cdefgh", got)
	}
}

// An invalid or non-positive count does not arm.
func TestRepeatNextRejectsBadCount(t *testing.T) {
	e, w := newTestEditor(t, "abc\n")
	e.executeCommand("repeat_next 0")
	if w.Repeat.Pending {
		t.Fatal("count 0 should not arm")
	}
	e.executeCommand("repeat_next xyz")
	if w.Repeat.Pending {
		t.Fatal("non-numeric count should not arm")
	}
}
