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
