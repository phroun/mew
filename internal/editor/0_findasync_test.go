package editor

import (
	"strings"
	"sync/atomic"
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
	// The toast waits out the grace period, so a search this small never
	// raises one at all.
	if hasTaggedTransient(e, findToastTag) {
		t.Fatal("a fast find should not flash a progress toast")
	}
	// Stand in for the grace timer firing on a search that IS slow.
	e.armFindToast(e.findRun.seq, e.findRun.stop, &atomic.Bool{})
	if !hasTaggedTransient(e, findToastTag) {
		t.Fatal("a find still running past the grace period should raise the toast")
	}

	findSettle(t, e)

	if got := w.CursorPos(); got.Rune != 6 {
		t.Fatalf("caret %v, want the col-6 match", got)
	}
	if hasTaggedTransient(e, findToastTag) {
		t.Fatal("the toast must clear once the find is caught up")
	}
}

// The grace timer's payload stays quiet once its pass is over, so a toast can
// never appear after the search it belongs to has landed.
func TestFindToastSuppressedAfterPass(t *testing.T) {
	e, w := newTestEditor(t, "aa bb aa\n")
	w.Find = viewport.FindState{Term: "aa"}
	e.executeCommand("find_next")
	seq, stop := e.findRun.seq, e.findRun.stop
	findSettle(t, e)

	settled := &atomic.Bool{}
	settled.Store(true) // the pass finished before the timer fired
	e.armFindToast(seq, stop, settled)
	if hasTaggedTransient(e, findToastTag) {
		t.Fatal("a finished pass must not raise a toast")
	}

	// Nor may a superseded one.
	e.armFindToast(seq-1, stop, &atomic.Bool{})
	if hasTaggedTransient(e, findToastTag) {
		t.Fatal("a superseded pass must not raise a toast")
	}
}

// The progress toast names the live cancel key, expanded from its TFC code.
func TestFindToastNamesCancelKey(t *testing.T) {
	e, w := newTestEditor(t, "aa bb aa\n")
	w.Find = viewport.FindState{Term: "aa"}
	e.executeCommand("find_next")
	e.armFindToast(e.findRun.seq, e.findRun.stop, &atomic.Bool{})

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

// pretendSlowFind puts the find into the state a search still grinding away
// presents to the main loop: a pass under way, past its grace period, with the
// progress message up. A real one cannot be held open deterministically — the
// scan of a test-sized document is over before the next line could assert
// anything — and everything the cancel path reads is this state, never the
// goroutine. armFindToast is the real one, so "the toast is up" is established
// the way the editor establishes it.
func pretendSlowFind(e *Editor) *findRun {
	fr := &findRun{seq: 1, stop: &atomic.Bool{}, settled: &atomic.Bool{}}
	e.findRun = fr
	e.armFindToast(fr.seq, fr.stop, fr.settled)
	if !fr.toasted {
		panic("the progress toast should have gone up")
	}
	return fr
}

// cancel stops a find that is SHOWING the message naming the cancel key, and
// reports success so the ^C chain never falls through to viewport_close and its
// LOSE CHANGES question. That message is the whole warrant: the user was told
// this key stops the search, so it must.
func TestCancelStopsSearchOfferingTheKey(t *testing.T) {
	e, _ := newTestEditor(t, "aa bb aa\n")
	e.executeCommand("insert 'x'") // modified: viewport_close WOULD prompt
	fr := pretendSlowFind(e)
	if !fr.cancelable() {
		t.Fatal("a search showing the cancel key should be cancelable")
	}

	e.dispatchKey("^C") // cancel|viewport_close

	if fr.stop == nil || !fr.stop.Load() {
		t.Fatal("cancel should have told the find thread to stop")
	}
	if hasTaggedTransient(e, findToastTag) {
		t.Fatal("cancel should clear the progress toast")
	}
	if !hasNotification(e, "Search cancelled") {
		t.Error("cancel should say the search was called off")
	}
	if focusedPrompt(e) != nil {
		t.Fatal("cancel must not fall through to viewport_close's LOSE CHANGES prompt")
	}
}

// A find that has FINISHED is not something to cancel. It has no prompt, no
// mode and nothing running; ^L simply goes to the next match. So ^C belongs to
// whatever else it is bound to, exactly as it did before background find
// existed — anything else would swallow the key forever after one search.
func TestCompletedFindIsNotCancelable(t *testing.T) {
	e, w := newTestEditor(t, "aa bb aa\n")
	w.Find = viewport.FindState{Term: "aa"}
	w.SetCursorPos(viewport.Position{})

	e.executeCommand("find_next")
	findSettle(t, e)

	if e.findRun.cancelable() {
		t.Fatal("a finished find should not be cancelable")
	}
	if e.cancelFind() {
		t.Fatal("cancelFind should report false once the search is over")
	}
	if res := e.PawScript.ExecuteAsync("cancel"); res != pawscript.BoolStatus(false) {
		t.Fatalf("cancel = %v, want false so the ^C chain reaches viewport_close", res)
	}
	if hasNotification(e, "Search cancelled") {
		t.Error("nothing was cancelled, so nothing should say so")
	}
}

// Nor is a search cancelable BEFORE its grace period is up: it has said nothing
// yet, so there is no promise to keep, and nearly every search in a real
// document lands inside that window. ^C during one is ^C in an ordinary buffer.
func TestFindNotCancelableBeforeItsToast(t *testing.T) {
	e, w := newTestEditor(t, "aa bb aa\n")
	w.Find = viewport.FindState{Term: "aa"}

	// A pass under way, its grace timer not yet fired.
	fr := &findRun{seq: 1, stop: &atomic.Bool{}, settled: &atomic.Bool{}}
	e.findRun = fr
	if !fr.running() {
		t.Fatal("the pass should read as running")
	}
	if fr.cancelable() {
		t.Fatal("a silent search should not be cancelable")
	}
	if e.cancelFind() {
		t.Fatal("cancelFind should report false before the toast is up")
	}
	findSettle(t, e)
}

// The promise ends with the message. Once the toast is down — the pass caught up,
// or its answer applied — a later ^C is nobody's cancel.
func TestFindToastDownEndsTheOffer(t *testing.T) {
	e, _ := newTestEditor(t, "aa bb aa\n")
	fr := pretendSlowFind(e)

	e.dropFindToast()

	if fr.cancelable() {
		t.Fatal("with the message gone there is nothing to honour")
	}
	if e.cancelFind() {
		t.Fatal("cancelFind should report false with the toast down")
	}
}

// A new pass starts silent, whatever the previous one had shown: it has made no
// promise of its own until its own grace period runs out.
func TestNewFindPassStartsSilent(t *testing.T) {
	e, w := newTestEditor(t, "aa bb aa\ncc\n")
	pretendSlowFind(e)

	w.Find = viewport.FindState{Term: "cc"}
	e.executeCommand("find_next")
	if e.findRun.toasted {
		t.Fatal("a fresh pass should not inherit the previous one's toast")
	}
	if e.findRun.cancelable() {
		t.Fatal("a fresh pass should not be cancelable")
	}
	findSettle(t, e)
}

// A pass that was cancelled has its answer discarded: findPump leaves the caret
// alone and takes the toast down.
func TestFindPumpDiscardsCancelledResult(t *testing.T) {
	e, w := newTestEditor(t, "aa bb aa\n")
	w.Find = viewport.FindState{Term: "aa"}
	e.executeCommand("find_next")
	fr := e.findRun
	findSettle(t, e)

	before := w.CursorPos()
	e.armFindToast(fr.seq, fr.stop, &atomic.Bool{})
	fr.stopPass() // a cancelled answer always follows a raised stop flag
	fr.mu.Lock()
	fr.pending = append(fr.pending, findRunResult{
		seq: fr.seq, term: "aa", cancelled: true, found: true, viewportID: w.ID, line: 0, col: 0,
	})
	fr.mu.Unlock()

	e.findPump()

	if got := w.CursorPos(); got != before {
		t.Fatalf("caret %v, want it untouched at %v by a cancelled answer", got, before)
	}
	if hasTaggedTransient(e, findToastTag) {
		t.Fatal("the toast should not survive a pump with nothing outstanding")
	}
}

// With no find running, cancel still reports failure so ^C reaches
// viewport_close as it always has.
func TestCancelWithoutFindFallsThrough(t *testing.T) {
	e, _ := newTestEditor(t, "aa\n")
	if e.cancelFind() {
		t.Fatal("cancelFind should report false with no find running")
	}
	if res := e.PawScript.ExecuteAsync("cancel"); res != pawscript.BoolStatus(false) {
		t.Fatalf("cancel = %v, want false so the ^C chain reaches viewport_close", res)
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
