package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
)

// addDocViewport opens a second document viewport, so a session can be swept.
func addDocViewport(t *testing.T, e *Editor, id, content string) *viewport.Viewport {
	t.Helper()
	e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Visible: true, ID: id, Type: viewport.DocViewport, Dock: viewport.DockNone,
		Buffer: buffer.NewFromString(content),
	})
	w := e.ViewportManager.GetViewport(id)
	if w == nil {
		t.Fatalf("could not create viewport %q", id)
	}
	return w
}

// session_close on clean work closes everything and ends the session — the
// window that holds it can go.
func TestSessionCloseEndsACleanSession(t *testing.T) {
	e, _ := newTestEditor(t, "hello\n")
	e.Running = true // a live session, as the editor loop leaves it
	addDocViewport(t, e, "doc2", "second\n")

	e.PawScript.ExecuteAsync("session_close")

	if focusedPrompt(e) != nil {
		t.Fatal("nothing was modified: closing should not have asked anything")
	}
	if e.Running {
		t.Error("closing every viewport should end the session")
	}
}

// Unsaved work is asked about, and the session survives a no: the window stays
// open with the work still in it.
func TestSessionCloseDeclinedKeepsTheSession(t *testing.T) {
	e, w := newTestEditor(t, "hello\n")
	e.Running = true
	w.Buffer.SetModified(true)

	e.PawScript.ExecuteAsync(`session_close | verbose_log "refused"`)

	if focusedPrompt(e) == nil {
		t.Fatal("modified work should have raised a lose-changes prompt")
	}
	if !e.Running {
		t.Fatal("the session must not end while the prompt is unanswered")
	}

	answerPrompt(t, e, "N")

	if !e.Running {
		t.Error("a declined close must leave the session running")
	}
	if !strings.Contains(verboseLogContent(e), "refused") {
		t.Error("a declined session_close should resolve false for the | branch")
	}
}

// ...and a yes carries the sweep through to the end of the session.
func TestSessionCloseConfirmedEndsTheSession(t *testing.T) {
	e, w := newTestEditor(t, "hello\n")
	e.Running = true
	w.Buffer.SetModified(true)

	e.PawScript.ExecuteAsync(`session_close & verbose_log "closed"`)
	if focusedPrompt(e) == nil {
		t.Fatal("expected a lose-changes prompt")
	}
	answerPrompt(t, e, "Y")

	if e.Running {
		t.Error("confirming should have closed the last viewport and ended the session")
	}
	if !strings.Contains(verboseLogContent(e), "closed") {
		t.Error("a completed session_close should resolve true for the & branch")
	}
}

// The sweep stops at the FIRST refusal: viewports it already closed stay
// closed, and the ones behind the refusal are left alone.
func TestSessionCloseStopsAtTheFirstRefusal(t *testing.T) {
	e, first := newTestEditor(t, "clean\n")
	e.Running = true
	second := addDocViewport(t, e, "doc2", "dirty\n")
	second.Buffer.SetModified(true)
	third := addDocViewport(t, e, "doc3", "clean too\n")

	e.PawScript.ExecuteAsync("session_close")
	if focusedPrompt(e) == nil {
		t.Fatal("the modified viewport should have asked")
	}
	answerPrompt(t, e, "N")

	if !e.Running {
		t.Fatal("a refusal must leave the session running")
	}
	if e.ViewportManager.GetViewport(first.ID) != nil {
		t.Error("the clean viewport swept before the refusal should have closed")
	}
	if e.ViewportManager.GetViewport(second.ID) == nil {
		t.Error("the refused viewport must survive")
	}
	if e.ViewportManager.GetViewport(third.ID) == nil {
		t.Error("the sweep should have stopped at the refusal, not gone past it")
	}
}

// viewport_close reports its own outcome now: a declined prompt resolves the
// SEQUENCE false rather than claiming a close it did not perform.
func TestViewportClosePromptResolvesTheSequence(t *testing.T) {
	e, w := newTestEditor(t, "hello\n")
	e.Running = true
	addDocViewport(t, e, "doc2", "second\n")
	w.Buffer.SetModified(true)

	e.PawScript.ExecuteAsync(`viewport_close & verbose_log "gone"`)
	if strings.Contains(verboseLogContent(e), "gone") {
		t.Fatal("the sequence must stay suspended while the prompt is open")
	}
	answerPrompt(t, e, "N")
	if strings.Contains(verboseLogContent(e), "gone") {
		t.Error("a declined close must not run the & branch")
	}
	if e.ViewportManager.GetViewport(w.ID) == nil {
		t.Error("a declined close must leave the viewport open")
	}
}

// The host-facing question: is there modified work anywhere in this session -
// including work stacked behind a link follow, which is invisible on screen.
func TestUnsavedStateFollowsEveryOpenBuffer(t *testing.T) {
	var seen []bool
	e, w := newTestEditor(t, "hello\n")
	e.Config.UnsavedState = func(unsaved bool) { seen = append(seen, unsaved) }

	e.notifyUnsavedState()
	if len(seen) != 1 || seen[0] {
		t.Fatalf("a clean session should push exactly one false, got %v", seen)
	}

	// A second viewport's buffer goes modified: still the same session.
	second := addDocViewport(t, e, "doc2", "second\n")
	second.Buffer.SetModified(true)
	e.notifyUnsavedState()
	if len(seen) != 2 || !seen[1] {
		t.Fatalf("modified work anywhere should push true, got %v", seen)
	}

	// Nothing changed: nothing is pushed. The host is told about transitions.
	e.notifyUnsavedState()
	if len(seen) != 2 {
		t.Fatalf("an unchanged answer should push nothing, got %v", seen)
	}

	second.Buffer.SetModified(false)
	_ = w
	e.notifyUnsavedState()
	if len(seen) != 3 || seen[2] {
		t.Fatalf("saving the work should push false, got %v", seen)
	}
}
