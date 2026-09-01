package editor

import (
	"testing"
)

// confirmResult captures a confirmation prompt's outcome.
type confirmResult struct {
	settled   bool
	accepted  bool
	confirmed bool
}

// openConfirm opens a yes/no confirmation and returns its captured outcome.
func openConfirm(e *Editor, def bool) *confirmResult {
	res := &confirmResult{}
	e.PromptMgr.PromptForConfirmation("13: OVERWRITE EXISTING FILE x?", def, func(accepted, confirmed bool) {
		res.settled = true
		res.accepted = accepted
		res.confirmed = confirmed
	})
	return res
}

// Pressing y alone answers yes — no Enter needed.
func TestConfirmSingleKeyYes(t *testing.T) {
	e, _ := newTestEditor(t, "")
	res := openConfirm(e, false)

	e.dispatchKey("y")

	if !res.settled || !res.accepted || !res.confirmed {
		t.Fatalf("result = %+v, want accepted yes", res)
	}
	if focusedPrompt(e) != nil {
		t.Fatal("prompt should be closed")
	}
}

// Pressing n alone answers no (an answer, not a cancel).
func TestConfirmSingleKeyNo(t *testing.T) {
	e, _ := newTestEditor(t, "")
	res := openConfirm(e, true)

	e.dispatchKey("n")

	if !res.settled || !res.accepted || res.confirmed {
		t.Fatalf("result = %+v, want accepted no", res)
	}
}

// Enter alone takes the default (through the accept binding's cursor-line
// read: the input line is blank, so the default applies).
func TestConfirmEnterTakesDefault(t *testing.T) {
	for _, def := range []bool{true, false} {
		e, _ := newTestEditor(t, "")
		res := openConfirm(e, def)

		e.dispatchKey("return")

		if !res.settled || !res.accepted || res.confirmed != def {
			t.Fatalf("default %v: result = %+v, want the default back", def, res)
		}
	}
}

// Arrowing through the option list does not settle the prompt; Enter on the
// arrowed-to option answers with it.
func TestConfirmArrowUpEnterChoosesOption(t *testing.T) {
	e, _ := newTestEditor(t, "")
	res := openConfirm(e, false) // content "Y\nN\n": arrow up twice reaches "Y"

	e.dispatchKey("up")
	e.dispatchKey("up")
	if res.settled {
		t.Fatal("arrow movement must not settle the prompt")
	}
	e.dispatchKey("return")

	if !res.settled || !res.accepted || !res.confirmed {
		t.Fatalf("result = %+v, want yes (the arrowed-to Y)", res)
	}
}

// The classifier's own newline analysis (for setups where Enter inserts
// rather than accepts): a newline landing at the end of the buffer reads the
// blank input line above it — the default; a newline landing mid-list reads
// the option line above the caret.
func TestConfirmInsertedNewlineDefaultAndOption(t *testing.T) {
	// End-of-buffer newline: previous line is the blank input line -> default.
	e, _ := newTestEditor(t, "")
	e.KeyProcessor.MapKey("F35", "insert '\\n'")
	res := openConfirm(e, true)
	e.dispatchKey("F35")
	if !res.settled || !res.accepted || !res.confirmed {
		t.Fatalf("end newline: result = %+v, want the default (yes)", res)
	}

	// Mid-list newline after arrowing up: previous line is the chosen option.
	e2, _ := newTestEditor(t, "")
	e2.KeyProcessor.MapKey("F35", "insert '\\n'")
	res2 := openConfirm(e2, false) // "Y\nN\n": two ups land at end of "Y"
	e2.dispatchKey("up")
	e2.dispatchKey("up")
	e2.dispatchKey("F35")
	if !res2.settled || !res2.accepted || !res2.confirmed {
		t.Fatalf("mid-list newline: result = %+v, want yes (the Y line above)", res2)
	}
}

// Growth by more than one rune (a paste) is not an answer: cancel.
func TestConfirmMultiRuneGrowthCancels(t *testing.T) {
	e, _ := newTestEditor(t, "")
	e.KeyProcessor.MapKey("F34", "insert 'ab'")
	res := openConfirm(e, false)

	e.dispatchKey("F34")

	if !res.settled || res.accepted {
		t.Fatalf("result = %+v, want cancelled", res)
	}
	if focusedPrompt(e) != nil {
		t.Fatal("prompt should be closed")
	}
}

// Shrinkage (backspace eating into the option list) is not an answer: cancel.
func TestConfirmShrinkCancels(t *testing.T) {
	e, _ := newTestEditor(t, "")
	res := openConfirm(e, false)

	e.dispatchKey("back")

	if !res.settled || res.accepted {
		t.Fatalf("result = %+v, want cancelled", res)
	}
}

// The LOSE CHANGES confirmation on viewport_close answers to a single n:
// the buffer stays open.
func TestLoseChangesSingleKey(t *testing.T) {
	e, w := newTestEditor(t, "hello\n")
	e.executeCommand("insert 'x'")
	e.executeCommand("viewport_close")
	if focusedPrompt(e) == nil {
		t.Fatal("LOSE CHANGES prompt should be open")
	}

	e.dispatchKey("n")

	if focusedPrompt(e) != nil {
		t.Fatal("prompt should be closed")
	}
	if e.ViewportManager.GetFocusedViewport() != w {
		t.Fatal("declining should keep the buffer open and focused")
	}
}

// A key that is not an answer cancels; it never stands in for the default.
//
// One keystroke settles this prompt, so every key on the keyboard arrives here
// as a candidate answer. Reading an unrecognized one as the default lets a
// mistyped key ACT -- and on "04: LOSE CHANGES TO x?", whose default is yes,
// acting means the changes are gone. Cancel costs nothing to be wrong about.
func TestConfirmUnrecognizedKeyCancels(t *testing.T) {
	for _, def := range []bool{true, false} {
		e, _ := newTestEditor(t, "")
		res := openConfirm(e, def)

		e.dispatchKey("k")

		if !res.settled {
			t.Fatalf("default %v: an unrecognized key left the prompt open", def)
		}
		if res.accepted {
			t.Fatalf("default %v: result = %+v, want cancelled", def, res)
		}
		if focusedPrompt(e) != nil {
			t.Fatalf("default %v: prompt should be closed", def)
		}
	}
}

// The dangerous case in full: a mistyped key at the LOSE CHANGES prompt, whose
// default is yes.
//
// Closing the LAST buffer quits instead of removing a viewport, so what a
// confirmed yes does here is stop the editor -- which is why the assertion is
// on Running and not on the viewport. Run() is what normally sets it, and the
// test editor does not run, so it is set here to give the flag something to
// change.
func TestLoseChangesDoesNotQuitOnAMistypedKey(t *testing.T) {
	e, w := newTestEditor(t, "hello\n")
	e.Running = true
	e.executeCommand("insert 'x'")
	e.executeCommand("viewport_close")
	if focusedPrompt(e) == nil {
		t.Fatal("LOSE CHANGES prompt should be open")
	}

	e.dispatchKey("k")

	if focusedPrompt(e) != nil {
		t.Fatal("prompt should be closed")
	}
	if !e.Running {
		t.Fatal("a mistyped key threw the changes away and quit")
	}
	if e.ViewportManager.GetFocusedViewport() != w {
		t.Fatal("the buffer should still be focused")
	}
}
