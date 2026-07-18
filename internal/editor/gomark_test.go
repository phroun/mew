package editor

import (
	"testing"

	"github.com/phroun/mew/internal/window"
)

// go_mark with no argument prompts for the mark identifier (like set_mark) and
// jumps to it; go_mark with an explicit argument still jumps directly.
func TestGoMarkPromptsWithoutArgument(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb\nccc\nddd\n")

	// Set mark "2" on line 2 via the explicit-argument form.
	w.SetCursorPos(window.Position{Line: 2, Rune: 1})
	e.PawScript.ExecuteAsync("set_mark '2'")

	// Move away, then go_mark with no argument opens a prompt.
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.PawScript.ExecuteAsync("go_mark")
	if focusedPrompt(e) == nil {
		t.Fatal("go_mark with no argument should open a prompt")
	}
	answerPrompt(t, e, "2")
	if got := w.CursorPos().Line; got != 2 {
		t.Fatalf("prompted go_mark should jump to the mark on line 2, got %d", got)
	}

	// The explicit-argument form still jumps directly (no prompt).
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.PawScript.ExecuteAsync("go_mark '2'")
	if focusedPrompt(e) != nil {
		t.Fatal("go_mark with an argument should not open a prompt")
	}
	if got := w.CursorPos().Line; got != 2 {
		t.Fatalf("go_mark '2' should jump to line 2, got %d", got)
	}
}

// Cancelling the prompt, or naming an unset mark, leaves the caret where it was.
func TestGoMarkPromptCancelOrUnset(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb\nccc\nddd\n")
	w.SetCursorPos(window.Position{Line: 1, Rune: 0})

	// Cancel the prompt: caret unchanged.
	e.PawScript.ExecuteAsync("go_mark")
	cancelPrompt(t, e)
	if got := w.CursorPos().Line; got != 1 {
		t.Fatalf("cancelling go_mark should leave the caret on line 1, got %d", got)
	}

	// Name an unset mark: warning, caret unchanged.
	e.PawScript.ExecuteAsync("go_mark")
	answerPrompt(t, e, "7")
	if got := w.CursorPos().Line; got != 1 {
		t.Fatalf("go_mark to an unset mark should leave the caret on line 1, got %d", got)
	}
	if !hasWarning(e, "Mark '7' not set") {
		t.Fatal("expected an unset-mark warning")
	}
}
