package editor

import "testing"

// A composition is SHOWN and not stored.
//
// An input method rewrites the whole composition on every keystroke, so storing
// it would mean un-storing it just as often — through the undo history, which
// is where the damage would be. It is painted into the line instead, the way a
// control character is painted "^X" without the buffer holding two runes.
func TestAPreeditIsPaintedAndNotStored(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`insert "ab"`)

	e.executeCommand(`preedit 'きょう', 3`)

	if got := docContent(w); got != "ab" {
		t.Errorf("document = %q; a composition must not reach the buffer", got)
	}
	p := w.Preedit()
	if string(p.Text) != "きょう" || p.Caret != 3 {
		t.Errorf("composition = %q caret %d, want きょう at 3", string(p.Text), p.Caret)
	}
	line, from, ok := w.PreeditAt()
	if !ok || line != w.CursorPos().Line || from != w.CursorPos().Rune {
		t.Errorf("composition parked at %d,%d (ok=%v), want the caret at %d,%d",
			line, from, ok, w.CursorPos().Line, w.CursorPos().Rune)
	}
}

// It shows up in the line the renderer will paint, spliced at the caret, while
// the line the buffer holds is unchanged.
func TestAPreeditShowsInTheDisplayLineOnly(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`insert "abcd"`)
	e.executeCommand(`preedit 'ò', 1`)

	display, lo, hi := w.PreeditSplice(0, "abcd")
	if display != "abcdò" || lo != 4 || hi != 5 {
		t.Errorf("display line = %q [%d,%d), want the composition at the caret",
			display, lo, hi)
	}
	if got := w.Buffer.GetLine(0); got != "abcd" {
		t.Errorf("buffer line = %q, want it untouched", got)
	}
}

// An empty composition ends it, which is how an input method both finishes and
// cancels. Nothing provisional was in the document, so there is nothing to take
// back.
func TestAnEmptyPreeditEndsIt(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`insert "ab"`)
	e.executeCommand(`preedit 'きょう', 3`)

	e.executeCommand(`preedit ''`)

	if w.Preedit().Active() {
		t.Error("an empty composition did not end the one standing")
	}
	if got := docContent(w); got != "ab" {
		t.Errorf("document = %q after cancelling, want it as it always was", got)
	}
}

// The caret defaults to the end of the composition, which is where every input
// method that reports no position of its own leaves it.
func TestAPreeditCaretDefaultsToTheEnd(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`preedit 'きょう'`)

	if got := w.Preedit().Caret; got != 3 {
		t.Errorf("caret = %d, want the end of the composition", got)
	}
}

// A viewport running a child process paints the child's grid, not a document:
// there is no line to synthesize a composition into, so the command declines
// rather than parking one where it can never be drawn.
func TestAPreeditDeclinesInATerminalViewport(t *testing.T) {
	e, w := newTestEditor(t, "")
	stub := newStubPTY()
	e.Config.TerminalSurfaces = TerminalHooks{
		Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}

	e.executeCommand(`preedit 'きょう', 3`)

	if w.Preedit().Active() {
		t.Error("a composition was parked on a viewport showing a child's grid")
	}
}

// The extent rides on the composition, and mew paints over what it covers.
func TestAPreeditCoversWhatThePaletteOpenedOver(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`insert "ABCDo"`)

	e.executeCommand(`preedit 'ò', 1, 1`)

	if got := docContent(w); got != "ABCDo" {
		t.Errorf("document = %q; hiding must not touch the buffer", got)
	}
	display, lo, hi := w.PreeditSplice(0, "ABCDo")
	if display != "ABCDò" || lo != 4 || hi != 5 {
		t.Errorf("display = %q [%d,%d), want the accent in the letter's place",
			display, lo, hi)
	}
}

// A composition cannot stand over more than the caret has behind it on the
// line — the count arrives from a host and is not trusted past what is there.
func TestAPreeditCoversNoMoreThanIsBehindTheCaret(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`insert "ab"`)

	e.executeCommand(`preedit 'X', 1, 9`)

	if got := w.Preedit().Covers; got != 2 {
		t.Errorf("covers = %d, want it clamped to the two characters behind the caret", got)
	}
}

// A composition tracks its region with a cursor, so it still points at the
// letter after something else is typed.
//
// This is the case that broke: arrow around in the palette so a replacement is
// showing, then type to dismiss it. macOS commits the selection and the
// keystroke lands first, so a region measured back from the caret named the
// character that keystroke typed — the accent replaced THAT and the letter
// stayed, "oò" with the "." eaten.
func TestACompositionSurvivesTypingBesideIt(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`insert "o"`)
	e.executeCommand(`preedit 'ò', 1, 1`) // arrowed to an accent, standing over the "o"

	e.executeCommand(`insert "."`) // dismissing keystroke lands first
	e.executeCommand(`preedit_commit 'ò'`)

	if got := docContent(w); got != "ò." {
		t.Errorf("content = %q, want the accent in the letter's place and the "+
			"typed character kept", got)
	}
	if w.Preedit().Active() {
		t.Error("the commit did not end the composition")
	}
}

// Committing with nothing standing over anything is an ordinary insert, which
// is what a host that never opened a composition sends.
func TestCommittingWithNoCompositionInserts(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`insert "ab"`)

	e.executeCommand(`preedit_commit 'きょう'`)

	if got := docContent(w); got != "abきょう" {
		t.Errorf("content = %q, want the text inserted at the caret", got)
	}
}

// The numeric route: the palette types its selector, erases it, and commits.
// The composition's cursor rides through all of it.
func TestTheNumericRouteReplacesOnlyTheLetter(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`insert "i"`)
	e.executeCommand(`preedit 'i', 1, 1`) // opened over the letter it just typed

	e.executeCommand(`insert "6"`)          // the palette's selector
	e.executeCommand(`replace_prior 1, ''`) // and the palette taking it back out
	e.executeCommand(`preedit_commit 'ĩ'`)

	if got := docContent(w); got != "ĩ" {
		t.Errorf("content = %q, want the accent alone", got)
	}
}
