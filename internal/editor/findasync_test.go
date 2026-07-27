package editor

import (
	"strings"
	"testing"

	"github.com/phroun/pawscript"

	"github.com/phroun/mew/internal/viewport"
)

// find_next hands its scan to a goroutine: it returns having only STARTED the
// pass, and the caret moves when the result is applied on the main loop.
func TestFindNextRunsOffTheMainLoop(t *testing.T) {
	e, w := newTestEditor(t, "aa bb aa\n")
	w.Find = viewport.FindState{Term: "aa"}
	w.SetCursorPos(viewport.Position{})

	e.executeCommand("find_next")
	if !hasTaggedTransient(e, findToastTag) {
		t.Fatal("a running find should raise the progress toast")
	}

	findSettle(t, e)

	if got := w.CursorPos(); got.Rune != 6 {
		t.Fatalf("caret %v, want the col-6 match", got)
	}
	if hasTaggedTransient(e, findToastTag) {
		t.Fatal("the toast must clear once the find is caught up")
	}
}

// The progress toast names the live cancel key, expanded from its TFC code.
func TestFindToastNamesCancelKey(t *testing.T) {
	e, w := newTestEditor(t, "aa bb aa\n")
	w.Find = viewport.FindState{Term: "aa"}
	e.executeCommand("find_next")

	var msg string
	for _, vw := range e.ViewportManager.AllViewports() {
		if vw.Tag == findToastTag {
			msg = vw.MessageTopInner
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
	findSettle(t, e)
}

// A miss reports "Not found" from the pump, once the pass has actually run.
func TestFindAsyncNotFound(t *testing.T) {
	e, w := newTestEditor(t, "aa bb\n")
	w.Find = viewport.FindState{Term: "zz"}

	e.executeCommand("find_next")
	findSettle(t, e)

	if !hasNotification(e, "Not found: zz") {
		t.Fatal("a miss should report Not found once the pass completes")
	}
	if hasTaggedTransient(e, findToastTag) {
		t.Fatal("the toast should clear on a miss too")
	}
}

// cancel stops a running find and reports success, so the ^C chain never falls
// through to buffer_close and its LOSE CHANGES question.
func TestCancelStopsRunningFind(t *testing.T) {
	e, w := newTestEditor(t, "aa bb aa\n")
	e.executeCommand("insert 'x'") // modified: buffer_close WOULD prompt
	w.Find = viewport.FindState{Term: "aa"}

	e.executeCommand("find_next")
	fr := e.findRun
	if !fr.running() {
		t.Fatal("the find should be running")
	}

	e.dispatchKey("^C") // cancel|buffer_close

	if fr.stop == nil || !fr.stop.Load() {
		t.Fatal("cancel should have told the find thread to stop")
	}
	if hasTaggedTransient(e, findToastTag) {
		t.Fatal("cancel should clear the progress toast")
	}
	if focusedPrompt(e) != nil {
		t.Fatal("cancel must not fall through to buffer_close's LOSE CHANGES prompt")
	}
	// The thread winds down on its own and its answer is discarded as stale.
	fr.done.Wait()
	e.findPump()
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 1 {
		t.Fatalf("caret %v, want it left where the cancelled search found it", got)
	}
}

// With no find running, cancel still reports failure so ^C reaches
// buffer_close as it always has.
func TestCancelWithoutFindFallsThrough(t *testing.T) {
	e, _ := newTestEditor(t, "aa\n")
	if e.cancelFind() {
		t.Fatal("cancelFind should report false with no find running")
	}
	if res := e.PawScript.ExecuteAsync("cancel"); res != pawscript.BoolStatus(false) {
		t.Fatalf("cancel = %v, want false so the ^C chain reaches buffer_close", res)
	}
}

// A second find supersedes the first: only the newest pass's answer lands.
func TestFindSupersedesRunningPass(t *testing.T) {
	e, w := newTestEditor(t, "aa bb aa\ncc\n")
	w.SetCursorPos(viewport.Position{})

	w.Find = viewport.FindState{Term: "aa"}
	e.executeCommand("find_next")
	first := e.findRun.stop

	w.Find = viewport.FindState{Term: "cc"}
	e.executeCommand("find_next")
	if first == nil || !first.Load() {
		t.Fatal("the superseded pass should have been told to stop")
	}

	findSettle(t, e)

	if got := w.CursorPos(); got.Line != 1 {
		t.Fatalf("caret %v, want the NEWEST search's match on line 1", got)
	}
}
