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
