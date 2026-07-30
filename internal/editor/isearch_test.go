package editor

import (
	"strings"
	"sync/atomic"
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

// isearchSettle waits for the background search pass to finish and applies its
// result the way the editor's action port would on the main loop. The headless
// harness runs no event loop, so tests pump it here instead.
func isearchSettle(t *testing.T, e *Editor) {
	t.Helper()
	if st := e.isearch; st != nil {
		st.done.Wait()
	}
	e.isearchPump()
}

// hasTaggedTransient reports whether a transient carrying tag is on screen.
func hasTaggedTransient(e *Editor, tag string) bool {
	for _, w := range e.ViewportManager.AllViewports() {
		if w.Tag == tag {
			return true
		}
	}
	return false
}

// Typing drives the caret to the first match of the growing pattern; the
// prompt keeps the keyboard throughout.
func TestIsearchTypingTracksMatches(t *testing.T) {
	e, w, fp := isearchEditor(t, "cargo\nbanana\nbend\n")

	e.dispatchKey("b")
	isearchSettle(t, e)
	if got := w.CursorPos(); got.Line != 1 || got.Rune != 0 {
		t.Fatalf("after b: caret %v, want banana at 1:0", got)
	}
	e.dispatchKey("e")
	isearchSettle(t, e)
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
	isearchSettle(t, e)
	e.dispatchKey("e") // bend   2:0
	isearchSettle(t, e)
	e.dispatchKey("back")
	isearchSettle(t, e)

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
	isearchSettle(t, e)
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 0 {
		t.Fatalf("caret %v, want first cat at 0:0", got)
	}

	e.executeCommand("find_next")
	findSettle(t, e)
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 4 {
		t.Fatalf("after find_next: caret %v, want second cat at 0:4", got)
	}
	e.executeCommand("find_next")
	findSettle(t, e)
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
	isearchSettle(t, e)
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
	findSettle(t, e)
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 4 {
		t.Fatalf("after find_next: caret %v, want 0:4", got)
	}
}

// Cancel (^C) closes the prompt and returns the caret to where the search
// began.
func TestIsearchCancelRestoresOrigin(t *testing.T) {
	e, w, _ := isearchEditor(t, "cargo\nbanana\nbend\n")

	e.dispatchKey("b") // caret moves to banana
	isearchSettle(t, e)
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
	isearchSettle(t, e)
	e.executeCommand("find_next") // 0:4
	findSettle(t, e)

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
	isearchSettle(t, e)
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
	isearchSettle(t, e)
	if !hasWarning(e, "Not found") {
		t.Fatal("missing Not found warning")
	}
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 0 {
		t.Fatalf("caret %v, want unmoved 0:0", got)
	}

	e.dispatchKey("back")
	isearchSettle(t, e)
	if w.Find.Term != "" {
		t.Fatalf("find term = %q, want pre-search state restored", w.Find.Term)
	}
	if focusedPrompt(e) == nil {
		t.Fatal("empty pattern keeps the prompt open")
	}
}

// While a pass is running the tagged "Searching…" toast is up, naming the
// cancel key; once the newest pass lands the toast comes straight down rather
// than lingering out its expiry.
func TestIsearchToastShownThenCleared(t *testing.T) {
	e, _, _ := isearchEditor(t, "cargo\nbanana\n")

	e.dispatchKey("b")
	// The toast waits out the grace period, so typing in a small document
	// never strobes one per keystroke.
	if hasTaggedTransient(e, isearchToastTag) {
		t.Fatal("a fast search should not flash a progress toast")
	}
	// Stand in for the grace timer firing on a pattern that IS slow to place.
	e.armIsearchToast(e.isearch.seq, e.isearch.stop, &atomic.Bool{})
	if !hasTaggedTransient(e, isearchToastTag) {
		t.Fatal("a search still running past the grace period should raise the toast")
	}
	if !hasNotification(e, "Searching") {
		t.Fatal("the toast should name what it is waiting on")
	}

	isearchSettle(t, e)

	if hasTaggedTransient(e, isearchToastTag) {
		t.Fatal("the toast must clear once the search is caught up")
	}
}

// The toast's TFC code resolves to the live cancel binding, so it tells the
// reader which key actually stops the search.
func TestIsearchToastNamesCancelKey(t *testing.T) {
	e, _, _ := isearchEditor(t, "cargo\n")
	e.dispatchKey("c")
	e.armIsearchToast(e.isearch.seq, e.isearch.stop, &atomic.Bool{})

	var msg string
	for _, w := range e.ViewportManager.AllViewports() {
		if w.Tag == isearchToastTag {
			msg = w.MessageTopInner
		}
	}
	if msg == "" {
		t.Fatal("no progress toast found")
	}
	if strings.Contains(msg, "%keys#") {
		t.Fatalf("toast TFC left unexpanded: %q", msg)
	}
	if !strings.Contains(msg, "^C") {
		t.Fatalf("toast = %q, want the live cancel key named", msg)
	}
	isearchSettle(t, e)
}

// The prompt's cleanup — the natural action of ^C — stops the thread and takes
// the toast down with it.
func TestIsearchCancelStopsThreadAndClearsToast(t *testing.T) {
	e, _, _ := isearchEditor(t, "cargo\nbanana\n")
	e.dispatchKey("b")
	st := e.isearch
	if st == nil {
		t.Fatal("search state should be live")
	}
	e.armIsearchToast(st.seq, st.stop, &atomic.Bool{}) // a slow search: toast up

	e.dispatchKey("^C")

	if st.stop == nil || !st.stop.Load() {
		t.Fatal("cancel should have told the search thread to stop")
	}
	if hasTaggedTransient(e, isearchToastTag) {
		t.Fatal("cancel should clear the progress toast")
	}
	if e.isearch != nil {
		t.Fatal("cancel should end the search")
	}
	// The thread winds down on its own, and its answer is discarded as stale.
	st.done.Wait()
	e.isearchPump()
}

// A superseding keystroke abandons the pass already running: only the newest
// pattern's answer is applied.
func TestIsearchKeystrokeSupersedesRunningPass(t *testing.T) {
	e, w, _ := isearchEditor(t, "cargo\nbanana\nbend\n")

	e.dispatchKey("b")
	first := e.isearch.stop
	e.dispatchKey("e") // supersedes before the first pass is pumped
	if first == nil || !first.Load() {
		t.Fatal("the superseded pass should have been told to stop")
	}

	isearchSettle(t, e)

	if got := w.CursorPos(); got.Line != 2 || got.Rune != 0 {
		t.Fatalf("caret %v, want the NEWEST pattern's match (bend at 2:0)", got)
	}
}

// Erasing back to an empty pattern retires the pass and its toast.
func TestIsearchEmptyPatternClearsToast(t *testing.T) {
	e, _, _ := isearchEditor(t, "cargo\n")
	e.dispatchKey("c")
	isearchSettle(t, e)

	e.dispatchKey("back")
	e.armIsearchToast(e.isearch.seq, e.isearch.stop, &atomic.Bool{})

	if hasTaggedTransient(e, isearchToastTag) {
		t.Fatal("an empty pattern has nothing to wait for: the toast should go")
	}
	isearchSettle(t, e)
}
