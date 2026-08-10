package editor

import (
	"fmt"
	"testing"

	"github.com/phroun/pawscript"
)

func activeTileContent(e *Editor) string {
	e.PawScript.ExecuteAsync("viewport_content")
	return fmt.Sprintf("%v", e.PawScript.GetResultValue())
}

// TestTileModeToggleAndPending: `viewport_<op> mode` sets the persistent tiling
// mode and toggles back to "go" when reselected; `viewport_<op> pending` arms a
// one-shot operator without disturbing the mode.
func TestTileModeToggleAndPending(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "one\n")

	if got := e.tileModeOrGo(); got != "go" {
		t.Fatalf("default tiling mode = %q, want go", got)
	}
	e.PawScript.ExecuteAsync("viewport_swap mode")
	if got := e.tileModeOrGo(); got != "swap" {
		t.Fatalf("after `viewport_swap mode`, mode = %q, want swap", got)
	}
	// Re-selecting the active mode toggles back to go.
	e.PawScript.ExecuteAsync("viewport_swap mode")
	if got := e.tileModeOrGo(); got != "go" {
		t.Fatalf("reselecting swap should toggle to go, got %q", got)
	}
	// Switching to a different mode does not toggle.
	e.PawScript.ExecuteAsync("viewport_split mode")
	if got := e.tileModeOrGo(); got != "split" {
		t.Fatalf("after `viewport_split mode`, mode = %q, want split", got)
	}
	// pending arms a one-shot without changing the mode.
	e.PawScript.ExecuteAsync("viewport_swap pending")
	if e.tilePending != "swap" {
		t.Fatalf("pending = %q, want swap", e.tilePending)
	}
	if got := e.tileModeOrGo(); got != "split" {
		t.Fatalf("arming pending must not change the mode, got %q", got)
	}
}

// TestTileDispatchGoAndSplit: the directional dispatch (viewport_left/right/…)
// carries out the armed mode — a focus move under the default "go", a split when
// the mode is "split".
func TestTileDispatchGoAndSplit(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "one\n")
	focusMainViewport(e, "doc2", "two\n") // doc | doc2 ; doc2 active (right)

	// Default go: viewport_left moves focus to the left tile (doc).
	if res := e.PawScript.ExecuteAsync("viewport_left"); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_left: %v", res)
	}
	if got := activeTileContent(e); got != "doc" {
		t.Fatalf("go-mode viewport_left should focus doc, got %q", got)
	}

	// Split mode: viewport_right splits the active tile, adding a tile.
	before := len(tileRefs(e))
	e.PawScript.ExecuteAsync("viewport_split mode")
	if res := e.PawScript.ExecuteAsync("viewport_right"); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_right (split mode): %v", res)
	}
	if after := len(tileRefs(e)); after != before+1 {
		t.Fatalf("split-mode viewport_right should add a tile: %d -> %d", before, after)
	}
}

// TestTilePendingOneShot: an armed pending operator is consumed by a single
// directional dispatch and then reverts to the persistent mode.
func TestTilePendingOneShot(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "one\n")
	focusMainViewport(e, "doc2", "two\n")

	e.PawScript.ExecuteAsync("viewport_swap pending")
	if e.tilePending != "swap" || e.tileModeOrGo() != "go" {
		t.Fatalf("after arm: pending=%q mode=%q, want swap/go", e.tilePending, e.tileModeOrGo())
	}
	refsBefore := len(tileRefs(e))

	// One dispatch performs the swap (reorders, does not add/remove tiles) and
	// consumes the pending, restoring the mode.
	if res := e.PawScript.ExecuteAsync("viewport_left"); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_left (pending swap): %v", res)
	}
	if e.tilePending != "" {
		t.Fatalf("pending should clear after one dispatch, got %q", e.tilePending)
	}
	if e.tileModeOrGo() != "go" {
		t.Fatalf("mode should remain go after a pending one-shot, got %q", e.tileModeOrGo())
	}
	if got := len(tileRefs(e)); got != refsBefore {
		t.Fatalf("a swap must not change the tile count: %d -> %d", refsBefore, got)
	}
}

// TestSeekVsGoFamilies: viewport_seek_* is the raw directional (no focus move),
// while viewport_go_* moves focus like viewport_go.
func TestSeekVsGoFamilies(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "one\n")
	focusMainViewport(e, "doc2", "two\n") // doc | doc2 ; doc2 active

	e.PawScript.ExecuteAsync("viewport_seek_left")
	if got := activeTileContent(e); got != "doc2" {
		t.Fatalf("viewport_seek_left must not move focus, active=%q", got)
	}
	e.PawScript.ExecuteAsync("viewport_go_left")
	if got := activeTileContent(e); got != "doc" {
		t.Fatalf("viewport_go_left should focus doc, got %q", got)
	}
}
