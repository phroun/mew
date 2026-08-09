package editor

import (
	"strconv"
	"testing"
)

// Following a buffers-surface link from an UNfocused pane focuses that pane
// first, so the chosen buffer opens there — not in whatever other viewport
// happened to hold focus. Regression for surfaceTargetViewport resolving to the
// wrong pane when the list is shown somewhere other than the focused viewport.
func TestBuffersSurfaceFollowFocusesItsOwnPane(t *testing.T) {
	e, w1 := newTestEditor(t, "AAA\n")
	fileA := w1.Buffer

	// A second document viewport showing the mew:/buffers surface, left unfocused.
	surf := e.newSurfaceViewport("buffers")
	if surf == nil {
		t.Fatal("could not open the buffers surface")
	}
	e.ViewportManager.SetFocus(w1.ID) // focus stays on the ORIGINAL pane
	if surf.ID == w1.ID {
		t.Fatal("precondition: surface should be its own viewport")
	}

	// Follow the entry for fileA (its handle) as a click in the surface pane would.
	handled, isSurface := e.followGeneratedSurfaceLink(surf, strconv.FormatUint(fileA.Handle(), 10))
	if !isSurface || !handled {
		t.Fatalf("follow not handled as a surface: handled=%v isSurface=%v", handled, isSurface)
	}

	if e.ViewportManager.GetFocusedViewport() != surf {
		t.Error("following the link should have focused the surface's own pane")
	}
	if surf.Buffer != fileA {
		t.Errorf("chosen buffer should open into the surface's pane; got %q", surf.Buffer.GetFilename())
	}
	if w1.Buffer != fileA {
		t.Error("the originally-focused pane should be left untouched (still showing its own buffer)")
	}
}
