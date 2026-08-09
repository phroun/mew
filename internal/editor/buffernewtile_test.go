package editor

import (
	"strings"
	"testing"
)

// buffer_new opens the fresh buffer in the focused tile (replacing it), not in a
// new tile beside it — the auto-new-tile behavior is gone. The viewport formerly
// in the tile stays open, just untiled.
func TestBufferNewReplacesFocusedTile(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "orig\n")
	e.ensureTiler()
	e.performRender()

	before := len(e.tiler.Tiles())
	if before == 0 {
		t.Fatal("precondition: expected at least one tile")
	}

	e.createNewBuffer() // buffer_new
	e.performRender()

	if after := len(e.tiler.Tiles()); after != before {
		t.Errorf("buffer_new should reuse the focused tile, not add one: %d -> %d", before, after)
	}

	focused := e.ViewportManager.GetFocusedViewport()
	if focused == nil {
		t.Fatal("a viewport should be focused after buffer_new")
	}
	if got := e.tiler.Content(e.tiler.GetFocus()); got != focused.ID {
		t.Errorf("the focused tile should show the focused viewport: tile ref %q, focused %q", got, focused.ID)
	}
	if strings.Contains(focused.Buffer.GetContent(), "orig") {
		t.Error("buffer_new should focus a fresh empty buffer, not the original")
	}
}

// Cycling (buffer_next/prior) onto an untiled background viewport reseats the
// focused tile onto it — showing it in the current pane — rather than splitting
// a new tile. buffer_new above orphans the original doc (untiles it); cycling
// back to it should reuse the pane.
func TestCycleOntoUntiledViewportReseatsTile(t *testing.T) {
	e, doc, _ := newRenderedEditor(t, "orig\n")
	e.ensureTiler()
	e.performRender()

	e.createNewBuffer() // reseats the tile to a fresh buffer; doc becomes untiled
	e.performRender()

	// Precondition: doc is no longer referenced by any tile.
	for _, b := range e.tiler.Tiles() {
		if b.Ref == doc.ID {
			t.Fatal("precondition: doc should be untiled after buffer_new")
		}
	}
	tiles := len(e.tiler.Tiles())

	e.cycleBuffer(1) // buffer_next — onto the untiled doc
	e.performRender()

	if got := len(e.tiler.Tiles()); got != tiles {
		t.Errorf("cycling onto an untiled viewport should reseat, not add a tile: %d -> %d", tiles, got)
	}
	if e.ViewportManager.GetFocusedViewport() != doc {
		t.Fatal("cycling should have focused doc")
	}
	if e.tiler.Content(e.tiler.GetFocus()) != doc.ID {
		t.Error("the focused tile should now show doc")
	}
}
