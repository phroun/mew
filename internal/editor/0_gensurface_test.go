package editor

import (
	"strconv"
	"strings"
	"testing"
)

// A buffer entry links by the buffer's stable handle, and following it
// navigates the focused viewport to that buffer in place (back returns to the
// list).
func TestBuffersSurfaceLinkFollows(t *testing.T) {
	e, _ := newTestEditor(t, "the document\n")
	w := e.ViewportManager.GetFocusedViewport()
	doc := w.Buffer
	handle := doc.Handle()

	if !e.openGeneratedSurface("buffers") {
		t.Fatal("could not open buffers surface")
	}
	// The rendered content links to the document by its handle.
	if body := w.Buffer.GetContent(); !strings.Contains(body, "[["+strconv.FormatUint(handle, 10)+"|") {
		t.Fatalf("buffers surface has no link to the open document handle %d:\n%s", handle, body)
	}

	// Follow that link: the surface must navigate the viewport back to the doc.
	handled, isSurface := e.followGeneratedSurfaceLink(w, strconv.FormatUint(handle, 10))
	if !isSurface || !handled {
		t.Fatalf("follow not handled as a surface link (isSurface=%v handled=%v)", isSurface, handled)
	}
	if w.Buffer != doc {
		t.Fatalf("following the entry did not navigate to the buffer (got %q)", w.Buffer.GetFilename())
	}
}

// A followed link that names a vanished buffer reports cleanly and is still
// treated as a surface link (never falls through to wiki resolution).
func TestBuffersSurfaceLinkStaleHandle(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	w := e.ViewportManager.GetFocusedViewport()
	e.openGeneratedSurface("buffers")
	handled, isSurface := e.followGeneratedSurfaceLink(w, "999999")
	if !isSurface || !handled {
		t.Fatalf("stale handle should be handled as a surface link (isSurface=%v handled=%v)", isSurface, handled)
	}
}

// Opening the buffers surface lands the caret on the entry for the document
// being navigated away from — its stable handle — not merely at the top.
func TestBuffersSurfaceCaretOnCurrentEntry(t *testing.T) {
	e, _ := newTestEditor(t, "the document\n")
	w := e.ViewportManager.GetFocusedViewport()
	handle := strconv.FormatUint(w.Buffer.Handle(), 10)

	if !e.openGeneratedSurface("buffers") {
		t.Fatal("could not open buffers surface")
	}
	cur := e.caretLinkSpan(w)
	if cur == nil {
		t.Fatalf("caret did not land inside a link; pos=%+v", w.CursorPos())
	}
	if cur.Target != handle {
		t.Fatalf("caret on entry %q, want the current document %q", cur.Target, handle)
	}
}

// Opening the viewports surface lands the caret on the entry for the viewport
// it opened in.
func TestViewportsSurfaceCaretOnCurrentEntry(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	w := e.ViewportManager.GetFocusedViewport()

	if !e.openGeneratedSurface("viewports") {
		t.Fatal("could not open viewports surface")
	}
	cur := e.caretLinkSpan(w)
	if cur == nil {
		t.Fatalf("caret did not land inside a link; pos=%+v", w.CursorPos())
	}
	if cur.Target != w.ID {
		t.Fatalf("caret on entry %q, want the current viewport %q", cur.Target, w.ID)
	}
}

// Following an entry out of the buffers surface must not leave the read-only
// the surface imposed stuck on the ordinary buffer navigated to.
func TestBuffersSurfaceFollowClearsReadOnly(t *testing.T) {
	e, _ := newTestEditor(t, "the document\n")
	w := e.ViewportManager.GetFocusedViewport()
	doc := w.Buffer
	handle := strconv.FormatUint(doc.Handle(), 10)

	if !e.openGeneratedSurface("buffers") {
		t.Fatal("could not open buffers surface")
	}
	if !w.ViewState.ReadOnly {
		t.Fatal("the surface should present as read-only")
	}

	e.followGeneratedSurfaceLink(w, handle)
	if w.Buffer != doc {
		t.Fatalf("following the entry did not return to the document")
	}
	if w.ViewState.ReadOnly || e.viewportEditLocked(w) {
		t.Error("read-only leaked onto the document after following from the surface")
	}
}

// A generated surface is transient in the nav history: opening one over a
// document and then navigating back returns to the document, and the surface
// is not forward-reachable — it was released, never parked as a destination.
func TestGeneratedSurfaceIsTransientInNav(t *testing.T) {
	e, _ := newTestEditor(t, "the document\n")
	w := e.ViewportManager.GetFocusedViewport()
	doc := w.Buffer

	if !e.openGeneratedSurface("buffers") {
		t.Fatal("could not open buffers surface")
	}
	if !w.Buffer.IsTransient() {
		t.Fatal("generated surface buffer should be transient")
	}

	// Back returns to the document...
	if !w.NavHistoryPrior() {
		t.Fatal("expected back history to the document")
	}
	if w.Buffer != doc {
		t.Fatalf("back did not return to the document (got %q)", w.Buffer.GetFilename())
	}
	// ...and the surface is gone: nothing to re-advance to.
	if w.NavHistoryNext() {
		t.Fatalf("surface must not be a forward destination (got %q)", w.Buffer.GetFilename())
	}
}

// Selecting the surface's OWN viewport in the viewports list closes the surface
// and reverts that viewport to what it was showing before — no tile change.
func TestViewportsSurfaceSelfSelectReverts(t *testing.T) {
	e, _ := newTestEditor(t, "the document\n")
	w := e.ViewportManager.GetFocusedViewport()
	doc := w.Buffer

	if !e.openGeneratedSurface("viewports") {
		t.Fatal("could not open viewports surface")
	}
	if w.Buffer == doc {
		t.Fatal("surface should have replaced the document")
	}
	// Pick this viewport's own id: it should just go back to the document.
	e.followGeneratedSurfaceLink(w, w.ID)
	if w.Buffer != doc {
		t.Fatalf("self-select should revert to the document, got %q", w.Buffer.GetFilename())
	}
}

// A viewport entry links by the viewport id; following it dispatches to the
// switch operation (which, with no tiler, focuses the target viewport).
func TestViewportsSurfaceLinkFollows(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	w := e.ViewportManager.GetFocusedViewport()
	if !e.openGeneratedSurface("viewports") {
		t.Fatal("could not open viewports surface")
	}
	if body := w.Buffer.GetContent(); !strings.Contains(body, "[["+w.ID+"|") {
		t.Fatalf("viewports surface has no link to viewport id %q:\n%s", w.ID, body)
	}
	handled, isSurface := e.followGeneratedSurfaceLink(w, w.ID)
	if !isSurface || !handled {
		t.Fatalf("viewport follow not handled (isSurface=%v handled=%v)", isSurface, handled)
	}
}
