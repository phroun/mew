package editor

import (
	"fmt"
	"testing"

	"github.com/phroun/pawscript"
)

// TestTilingCommandsFire checks the ifitfits command surface is registered and
// wired: commands parse their arguments, fire the library method, return values
// via the formal result, and fail (Usage warning) when a required argument is
// missing. After startup the tiler's active tile holds mew's focused viewport
// ("doc"), which the #tile default resolves to.
func TestTilingCommandsFire(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "hello\n")
	e.performRender()

	// A query returns its value via the formal result — with an explicit #tile
	// and (the idiom's point) with the tile omitted, defaulting to the active tile.
	for _, cmd := range []string{"viewport_content #tile", "viewport_content"} {
		if res := e.PawScript.ExecuteAsync(cmd); res != pawscript.BoolStatus(true) {
			t.Fatalf("%q: %v", cmd, res)
		}
		if got := fmt.Sprintf("%v", e.PawScript.GetResultValue()); got != "doc" {
			t.Fatalf("%q result = %q, want \"doc\"", cmd, got)
		}
	}

	// A structural command fires and returns the new tile's handle.
	if res := e.PawScript.ExecuteAsync("viewport_split #tile, right"); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_split #tile: %v", res)
	}
	if h := fmt.Sprintf("%v", e.PawScript.GetResultValue()); h == "0" {
		t.Fatal("viewport_split returned handle 0")
	}

	// The renamed reading-order cycle commands, and a defaulting toggle.
	for _, cmd := range []string{"tile_next #tile", "tile_prior #tile", "viewport_zoom #tile", "viewport_zoom"} {
		if res := e.PawScript.ExecuteAsync(cmd); res != pawscript.BoolStatus(true) {
			t.Fatalf("%q: %v", cmd, res)
		}
	}

	// A genuinely required NON-tile argument still fails when missing.
	if res := e.PawScript.ExecuteAsync("viewport_go #tile"); res != pawscript.BoolStatus(false) {
		t.Fatalf("viewport_go with no direction should fail, got %v", res)
	}
	if res := e.PawScript.ExecuteAsync("viewport_set_caret"); res != pawscript.BoolStatus(false) {
		t.Fatalf("viewport_set_caret with no x,y should fail, got %v", res)
	}
	// An explicit #-symbol naming an unset variable fails to resolve a tile.
	if res := e.PawScript.ExecuteAsync("viewport_flip #nope"); res != pawscript.BoolStatus(false) {
		t.Fatalf("viewport_flip #nope (unset var) should fail, got %v", res)
	}
}

// TestViewportSeekVsGo: viewport_seek is raw navigation (no side effects), while
// viewport_go additionally makes the destination the active tile — so the #tile
// default (and mew's view) follows it.
func TestViewportSeekVsGo(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "one\n")
	focusMainViewport(e, "doc2", "two\n") // doc (left) | doc2 (right); doc2 active
	if got := len(tileRefs(e)); got != 2 {
		t.Fatalf("want 2 tiles, got %d (%v)", got, tileRefs(e))
	}

	// seek left resolves the destination but must NOT move the active tile.
	if res := e.PawScript.ExecuteAsync("viewport_seek #tile, left"); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_seek: %v", res)
	}
	e.PawScript.ExecuteAsync("viewport_content") // default = active tile
	if got := fmt.Sprintf("%v", e.PawScript.GetResultValue()); got != "doc2" {
		t.Fatalf("after seek, active tile content = %q, want \"doc2\" (seek must not move focus)", got)
	}

	// go left moves the active tile to the left neighbor (doc).
	if res := e.PawScript.ExecuteAsync("viewport_go #tile, left"); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_go: %v", res)
	}
	e.PawScript.ExecuteAsync("viewport_content")
	if got := fmt.Sprintf("%v", e.PawScript.GetResultValue()); got != "doc" {
		t.Fatalf("after go left, active tile content = %q, want \"doc\"", got)
	}
}

// TestTilingSplitHashIdiom: an explicit leading #-symbol names the tile, an
// omitted handle defaults to the active tile, and a missing direction fails.
func TestTilingSplitHashIdiom(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "hi\n")
	e.performRender()

	// Explicit #tile: resolves the active tile and returns a new tile.
	if res := e.PawScript.ExecuteAsync("viewport_split #tile, right"); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_split #tile: %v", res)
	}
	if h := fmt.Sprintf("%v", e.PawScript.GetResultValue()); h == "0" {
		t.Fatal("viewport_split #tile returned handle 0")
	}

	// Omitted handle: defaults to the active tile; arg 0 is the direction.
	if res := e.PawScript.ExecuteAsync("viewport_split down"); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_split down (default): %v", res)
	}
	if h := fmt.Sprintf("%v", e.PawScript.GetResultValue()); h == "0" {
		t.Fatal("viewport_split down returned handle 0")
	}

	// Missing direction fails, like other commands.
	if res := e.PawScript.ExecuteAsync("viewport_split"); res != pawscript.BoolStatus(false) {
		t.Fatalf("viewport_split with no direction should fail, got %v", res)
	}
}
