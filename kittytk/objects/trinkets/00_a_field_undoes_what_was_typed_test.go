package trinkets

// A text field keeps a history of its last hundred changes. Typing joins
// typing and deleting joins deleting, so a word typed is one step and a word
// backspaced over is another; a caret movement ends a step; a paste, a cut, an
// input method committing several characters and clearing the line are each a
// step of their own.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

func typeKeys(ti *TextInput, keys ...string) {
	for _, k := range keys {
		ti.HandleKeyPress(core.KeyPressEvent{Key: k})
	}
}

func typeWord(ti *TextInput, word string) {
	for _, r := range word {
		ti.HandleKeyPress(core.KeyPressEvent{Key: string(r)})
	}
}

// undoAll undoes until nothing is left, and returns each text passed through.
func undoAll(ti *TextInput) []string {
	var seen []string
	for ti.UndoEnabled() {
		ti.Undo()
		seen = append(seen, ti.Text())
	}
	return seen
}

func TestTypingIsOneStepAndDeletingAnother(t *testing.T) {
	ti, _ := newClippedInput("")
	typeWord(ti, "hello")
	typeKeys(ti, "Backspace", "Backspace")
	if got := ti.Text(); got != "hel" {
		t.Fatalf("typed and backspaced: %q", got)
	}
	if got := undoAll(ti); strings.Join(got, "|") != "hello|" {
		t.Errorf("undoing passed through %q, want hello then empty", got)
	}
}

func TestUndoPutsTheCaretBack(t *testing.T) {
	ti, _ := newClippedInput("")
	typeWord(ti, "abc")
	typeKeys(ti, "Left", "Left")
	typeWord(ti, "X")
	ti.Undo()
	if ti.Text() != "abc" || ti.CursorPosition() != 1 {
		t.Errorf("after undo: %q with the caret at %d, want abc at 1", ti.Text(), ti.CursorPosition())
	}
}

// A caret movement ends the step: typing in two places is two steps, even
// when the caret comes back to where it was.
func TestACaretMoveEndsTheStep(t *testing.T) {
	ti, _ := newClippedInput("")
	typeWord(ti, "ab")
	typeKeys(ti, "Left", "Right")
	typeWord(ti, "cd")
	if got := undoAll(ti); strings.Join(got, "|") != "ab|" {
		t.Errorf("undoing passed through %q, want ab then empty", got)
	}

	ti, _ = newClippedInput("")
	typeWord(ti, "ab")
	ti.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: 1000, Y: 1})
	ti.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton, X: 1000, Y: 1})
	typeWord(ti, "cd")
	if got := undoAll(ti); strings.Join(got, "|") != "ab|" {
		t.Errorf("after a click, undoing passed through %q, want ab then empty", got)
	}
}

// Typing over a selection is one step with the first character typed, and the
// rest of the run joins it: one undo brings the selection's text back.
func TestTypingOverASelectionIsOneStep(t *testing.T) {
	ti, _ := newClippedInput("hello world")
	ti.selStart, ti.selEnd, ti.cursorPos = 6, 11, 11
	typeWord(ti, "there")
	if ti.Text() != "hello there" {
		t.Fatalf("typed over the selection: %q", ti.Text())
	}
	ti.Undo()
	if ti.Text() != "hello world" || ti.selStart != 6 || ti.selEnd != 11 {
		t.Errorf("one undo: %q selecting %d..%d, want hello world selecting 6..11",
			ti.Text(), ti.selStart, ti.selEnd)
	}
	if ti.UndoEnabled() {
		t.Error("the replacement took more than one step")
	}
}

// A paste and a cut are steps of their own, apart from the typing around them.
func TestPasteAndCutAreStepsOfTheirOwn(t *testing.T) {
	ti, clip := newClippedInput("")
	typeWord(ti, "ab")
	clip.SetClipboard("XY")
	ti.pasteText("XY")
	typeWord(ti, "cd")
	if got := undoAll(ti); strings.Join(got, "|") != "abXY|ab|" {
		t.Errorf("undoing a paste between typing passed through %q, want abXY, ab, empty", got)
	}

	ti, _ = newClippedInput("")
	typeWord(ti, "abcd")
	ti.selStart, ti.selEnd, ti.cursorPos = 1, 3, 3
	ti.Cut()
	typeWord(ti, "e")
	if got := undoAll(ti); strings.Join(got, "|") != "ad|abcd|" {
		t.Errorf("undoing a cut passed through %q, want ad, abcd, empty", got)
	}
}

// An input method committing one character joins the typing; committing
// several at once is a step of its own.
func TestACommitOfSeveralCharactersIsAStep(t *testing.T) {
	ti, _ := newClippedInput("")
	typeWord(ti, "a")
	ti.HandleTextCommit(core.TextCommitEvent{Text: "b"})
	ti.HandleTextCommit(core.TextCommitEvent{Text: "日本"})
	typeWord(ti, "c")
	if got := undoAll(ti); strings.Join(got, "|") != "ab日本|ab|" {
		t.Errorf("undoing commits passed through %q, want ab日本, ab, empty", got)
	}
}

// Clearing the line is a step of its own.
func TestClearingTheLineIsAStep(t *testing.T) {
	ti, _ := newClippedInput("")
	typeWord(ti, "abc")
	typeKeys(ti, core.DefaultKeyRegistry().KeyForCommand(core.CmdTrinketDelLine))
	if ti.Text() != "" {
		t.Fatalf("the clear-line key left %q", ti.Text())
	}
	typeWord(ti, "d")
	if got := undoAll(ti); strings.Join(got, "|") != "|abc|" {
		t.Errorf("undoing a cleared line passed through %q, want empty, abc, empty", got)
	}
}

// Redo puts back what undo took, and a new change forgets it.
func TestRedoAndANewChange(t *testing.T) {
	ti, _ := newClippedInput("")
	typeWord(ti, "ab")
	typeKeys(ti, "Backspace")
	ti.Undo()
	ti.Undo()
	ti.Redo()
	if ti.Text() != "ab" {
		t.Fatalf("undo, undo, redo: %q, want ab", ti.Text())
	}
	typeWord(ti, "z")
	if ti.RedoEnabled() {
		t.Error("a new change left something to redo")
	}
}

// Simple undo redoes where it can and undoes once otherwise, so pressing it
// twice takes a change away and puts it back.
func TestSimpleUndoFlipsTheLastChange(t *testing.T) {
	ti, _ := newClippedInput("")
	typeWord(ti, "ab")
	typeKeys(ti, "Left")
	typeWord(ti, "X")
	ti.SimpleUndo()
	if ti.Text() != "ab" {
		t.Fatalf("simple undo: %q, want ab", ti.Text())
	}
	ti.SimpleUndo()
	if ti.Text() != "aXb" {
		t.Fatalf("simple undo again: %q, want aXb back", ti.Text())
	}
	ti.SimpleUndo()
	if ti.Text() != "ab" {
		t.Errorf("and again: %q, want ab", ti.Text())
	}
}

// The keymap's keys reach the field: ^Z is simple undo, ^_ the plain one.
func TestTheUndoKeysReachTheField(t *testing.T) {
	ti, _ := newClippedInput("")
	typeWord(ti, "ab")
	typeKeys(ti, "Left")
	typeWord(ti, "X")
	typeKeys(ti, "^Z")
	if ti.Text() != "ab" {
		t.Fatalf("^Z: %q, want ab", ti.Text())
	}
	typeKeys(ti, "^Z")
	if ti.Text() != "aXb" {
		t.Fatalf("^Z again: %q, want aXb (simple undo redoes)", ti.Text())
	}
	typeKeys(ti, "^_", "^_")
	if ti.Text() != "" {
		t.Errorf("^_ twice: %q, want everything undone", ti.Text())
	}
}

// The clipboard keys reach the field as well.
func TestTheClipboardKeysReachTheField(t *testing.T) {
	ti, clip := newClippedInput("hello")
	ti.SelectAll()
	typeKeys(ti, "^C")
	if clip.Clipboard() != "hello" {
		t.Errorf("^C put %q on the clipboard, want hello", clip.Clipboard())
	}
	typeKeys(ti, "^X")
	if ti.Text() != "" {
		t.Errorf("^X left %q", ti.Text())
	}
}

// The history keeps the last hundred steps.
func TestTheHistoryKeepsAHundredSteps(t *testing.T) {
	ti, _ := newClippedInput("")
	for i := 0; i < 120; i++ {
		typeWord(ti, "a")
		typeKeys(ti, "Left", "Right") // each character its own step
	}
	n := len(undoAll(ti))
	if n != undoDepth {
		t.Errorf("undid %d steps, want %d", n, undoDepth)
	}
	if got := ti.Text(); got != strings.Repeat("a", 20) {
		t.Errorf("after undoing all it keeps, the field holds %d characters, want 20", len(got))
	}
}

// Text the program sets is where the history starts.
func TestSetTextStartsTheHistory(t *testing.T) {
	ti, _ := newClippedInput("")
	typeWord(ti, "attempt")
	ti.SetText("")
	if ti.UndoEnabled() || ti.RedoEnabled() {
		t.Error("undo reaches past text the program set")
	}
}

// A change that changes nothing is no step.
func TestNothingChangedIsNoStep(t *testing.T) {
	ti, _ := newClippedInput("")
	typeKeys(ti, "Backspace", "FDel")
	if ti.UndoEnabled() {
		t.Error("backspacing an empty field made a step")
	}
}

// A read-only field neither undoes nor redoes.
func TestAReadOnlyFieldHasNoHistoryToStep(t *testing.T) {
	ti, _ := newClippedInput("")
	typeWord(ti, "abc")
	typeKeys(ti, "Backspace")
	ti.Undo()
	ti.SetReadOnly(true)
	if ti.UndoEnabled() || ti.RedoEnabled() {
		t.Error("a read-only field offers undo or redo")
	}
	ti.Redo()
	if ti.Text() != "abc" {
		t.Errorf("redo changed a read-only field to %q", ti.Text())
	}
	ti.Undo()
	if ti.Text() != "abc" {
		t.Errorf("undo changed a read-only field to %q", ti.Text())
	}
}

// Undo tells of its change as any edit does.
func TestUndoTellsOfTheChange(t *testing.T) {
	ti, _ := newClippedInput("")
	typeWord(ti, "ab")
	var told []string
	ti.SetOnTextChanged(func(s string) { told = append(told, s) })
	ti.Undo()
	ti.Redo()
	if strings.Join(told, "|") != "|ab" {
		t.Errorf("told %q, want the empty text then ab", told)
	}
}

// A concealed field keeps its history, and undo never shows what it brings
// back: in password_on_edit only a key just typed is revealed.
func TestAConcealedFieldUndoesWithoutRevealing(t *testing.T) {
	_, ti := onEditField(t)
	typeWord(ti, "ab")
	typeKeys(ti, "Left")
	typeWord(ti, "X")
	ti.Undo()
	if ti.Text() != "ab" {
		t.Fatalf("undo in a masked field: %q, want ab", ti.Text())
	}
	if ti.revealed {
		t.Error("undo revealed a character")
	}
	ti.Redo()
	if got := shownAs(ti); got != "•••" {
		t.Errorf("after redo the field shows %q, want all masked", got)
	}
}

// A caret or selection changed by the program, not by a key or a click, still
// ends the step: what is typed next is a change of its own.
func TestAMoveThePlaceDidNotMakeStillEndsTheStep(t *testing.T) {
	ti, _ := newClippedInput("")
	typeWord(ti, "abc")
	ti.SetCursorPosition(1)
	ti.SetCursorPosition(3)
	ti.cursorPos = 1
	typeWord(ti, "x")
	if got := undoAll(ti); strings.Join(got, "|") != "abc|" {
		t.Errorf("after the program moved the caret, undoing passed through %q, want abc then empty", got)
	}

	ti, _ = newClippedInput("")
	typeWord(ti, "abc")
	ti.SelectAll()
	typeWord(ti, "x")
	ti.Undo()
	if ti.Text() != "abc" {
		t.Errorf("typing over a selection the program made undid to %q, want abc", ti.Text())
	}
}

// Two pastes in a row are two steps.
func TestTwoPastesAreTwoSteps(t *testing.T) {
	ti, _ := newClippedInput("")
	ti.pasteText("ab")
	ti.pasteText("cd")
	if got := undoAll(ti); strings.Join(got, "|") != "ab|" {
		t.Errorf("undoing two pastes passed through %q, want ab then empty", got)
	}
}

// A cut, or clearing the line, does not join the deleting around it.
func TestACutOrAClearDoesNotJoinDeleting(t *testing.T) {
	ti, _ := newClippedInput("")
	typeWord(ti, "abcd")
	ti.selStart, ti.selEnd, ti.cursorPos = 2, 4, 4
	ti.Cut()
	typeKeys(ti, "Backspace")
	ti.Undo()
	if ti.Text() != "ab" {
		t.Errorf("undoing a backspace after a cut: %q, want ab", ti.Text())
	}

	ti, _ = newClippedInput("")
	typeWord(ti, "abc")
	typeKeys(ti, "Backspace", "^U")
	ti.Undo()
	if ti.Text() != "ab" {
		t.Errorf("undoing a clear after a backspace: %q, want ab", ti.Text())
	}
}

// After a redo, typing starts a step of its own rather than joining the step
// the redo put back.
func TestTypingAfterARedoIsANewStep(t *testing.T) {
	ti, _ := newClippedInput("")
	typeWord(ti, "a")
	typeKeys(ti, "Left", "Right")
	typeWord(ti, "b")
	ti.Undo()
	ti.Redo()
	typeWord(ti, "c")
	ti.Undo()
	if ti.Text() != "ab" {
		t.Errorf("undoing what was typed after a redo: %q, want ab", ti.Text())
	}
}
