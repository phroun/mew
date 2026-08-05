package editor

import (
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// countOptionsViewports returns how many top-dock viewports carry Class "options".
func countOptionsViewports(e *Editor) int {
	n := 0
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockTop) {
		if w.Class == "options" {
			n++
		}
	}
	return n
}

// editor_options opens the options display on first invocation and dismisses
// it on the second, rather than stacking a second identical viewport.
func TestEditorOptionsToggle(t *testing.T) {
	e, _ := newTestEditor(t, "")

	if got := countOptionsViewports(e); got != 0 {
		t.Fatalf("no options viewport should exist yet, got %d", got)
	}

	e.executeCommand("editor_options")
	if got := countOptionsViewports(e); got != 1 {
		t.Fatalf("first invocation should open the options viewport, got %d", got)
	}

	e.executeCommand("editor_options")
	if got := countOptionsViewports(e); got != 0 {
		t.Fatalf("second invocation should dismiss the options viewport, got %d", got)
	}

	// And it can be reopened again (toggle is stateless).
	e.executeCommand("editor_options")
	if got := countOptionsViewports(e); got != 1 {
		t.Fatalf("third invocation should reopen the options viewport, got %d", got)
	}
}

// buffer_list navigates the focused document IN PLACE to the generated
// mew:/buffers surface: read-only, in link-browse (navigation) mode, rendered
// as dokuwiki — and pressing back restores the original document, editable, with
// none of the surface's fixed options leaked onto it.
func TestBufferListNavigatesInPlace(t *testing.T) {
	e, _ := newTestEditor(t, "hello\nworld\n")
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Type != viewport.DocViewport {
		t.Fatalf("expected a focused document viewport")
	}
	orig := w.Buffer

	if !e.openGeneratedSurface("buffers") {
		t.Fatal("buffer_list should navigate the focused document")
	}
	if w.Buffer == orig {
		t.Fatal("the surface should have replaced the document in place")
	}
	if got := w.Buffer.GetFilename(); got != "mew:/buffers" {
		t.Fatalf("surface identity = %q, want mew:/buffers", got)
	}
	if !w.ViewState.ReadOnly || !e.viewportEditLocked(w) {
		t.Error("the generated surface must be read-only")
	}
	if !w.BrowseActive {
		t.Error("the generated surface must open in navigation mode")
	}
	if in, _ := e.bufferGrammar(w.Buffer); in == nil {
		t.Error("the generated surface should carry the dokuwiki grammar")
	}

	// Back to the document: options must NOT leak (re-resolved per buffer).
	if !e.navHistory(-1) {
		t.Fatal("back should return to the original document")
	}
	if w.Buffer != orig {
		t.Fatalf("back should restore the original buffer")
	}
	e.reconcileGrammarOptions(w) // the per-frame re-resolution the render loop runs
	if w.ViewState.ReadOnly || e.viewportEditLocked(w) {
		t.Error("read-only leaked onto the document after returning from the surface")
	}
}

// viewport_list is the mew:/viewports companion, same in-place/read-only shape.
func TestViewportListNavigatesInPlace(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	w := e.ViewportManager.GetFocusedViewport()
	if !e.openGeneratedSurface("viewports") {
		t.Fatal("viewport_list should navigate the focused document")
	}
	if got := w.Buffer.GetFilename(); got != "mew:/viewports" {
		t.Fatalf("surface identity = %q, want mew:/viewports", got)
	}
	if !e.viewportEditLocked(w) {
		t.Error("the viewports surface must be read-only")
	}
}
