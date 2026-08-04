package editor

import (
	"fmt"
	"testing"

	"github.com/phroun/pawscript"
)

// TestTilingCommandsFire checks that the ifitfits command surface is registered
// and wired: commands parse their arguments, fire the library method, return
// values via the formal result, and report a failure (Usage warning) when a
// required argument is missing.
func TestTilingCommandsFire(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "hello\n")
	e.ensureTiler()
	main := uint64(e.tilerMain)
	if main == 0 {
		t.Fatal("tiler main handle unset")
	}

	// A query returns its value via the formal result — both with an explicit
	// #tile and (the idiom's point) with the tile omitted, defaulting to #tile.
	for _, cmd := range []string{"viewport_content #tile", "viewport_content"} {
		if res := e.PawScript.ExecuteAsync(cmd); res != pawscript.BoolStatus(true) {
			t.Fatalf("%q: %v", cmd, res)
		}
		if got := fmt.Sprintf("%v", e.PawScript.GetResultValue()); got != "main" {
			t.Fatalf("%q result = %q, want \"main\"", cmd, got)
		}
	}

	// A structural command fires the library method and returns the new tile's
	// handle (nonzero, distinct from the origin) via the formal result.
	if res := e.PawScript.ExecuteAsync("viewport_split #tile, right"); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_split #tile: %v", res)
	}
	newHandle := fmt.Sprintf("%v", e.PawScript.GetResultValue())
	if newHandle == "0" || newHandle == fmt.Sprintf("%d", main) {
		t.Fatalf("viewport_split returned handle %q; want a new nonzero tile", newHandle)
	}

	// The renamed reading-order cycle commands exist (viewport_next/prior are
	// mew's own; the tiler's are tile_next/tile_prior).
	for _, cmd := range []string{"tile_next #tile", "tile_prior #tile"} {
		if res := e.PawScript.ExecuteAsync(cmd); res != pawscript.BoolStatus(true) {
			t.Fatalf("%q: %v", cmd, res)
		}
	}

	// With the idiom the tile defaults, so a toggling command needs no argument
	// at all — it toggles #tile.
	for _, cmd := range []string{"viewport_zoom #tile", "viewport_zoom"} {
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
	// An explicit #-symbol that names an unset variable fails to resolve a tile.
	if res := e.PawScript.ExecuteAsync("viewport_flip #nope"); res != pawscript.BoolStatus(false) {
		t.Fatalf("viewport_flip #nope (unset var) should fail, got %v", res)
	}
}

// TestViewportGoFollowsTileDefault checks the seek/go split: viewport_seek is
// the raw navigation (no side effects), while viewport_go additionally moves the
// #tile default to the destination tile.
func TestViewportGoFollowsTileDefault(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "hi\n")
	e.ensureTiler() // main (left) | blank (right)

	// seek right resolves the destination but must NOT move #tile.
	if res := e.PawScript.ExecuteAsync("viewport_seek #tile, right"); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_seek: %v", res)
	}
	e.PawScript.ExecuteAsync("viewport_content") // default #tile
	if got := fmt.Sprintf("%v", e.PawScript.GetResultValue()); got != "main" {
		t.Fatalf("after seek, #tile content = %q, want \"main\" (seek must not move #tile)", got)
	}

	// go right moves #tile to the destination tile (the right "blank" tile).
	if res := e.PawScript.ExecuteAsync("viewport_go #tile, right"); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_go: %v", res)
	}
	e.PawScript.ExecuteAsync("viewport_content") // default #tile, now the destination
	if got := fmt.Sprintf("%v", e.PawScript.GetResultValue()); got != "blank" {
		t.Fatalf("after go, #tile content = %q, want \"blank\" (go must move #tile to the destination)", got)
	}
}

// TestTilingSplitHashIdiom exercises viewport_split's #-handle idiom: an explicit
// leading #-symbol names the tile, an omitted handle falls back to the seeded
// #tile default, a missing direction fails, and a script-assigned #tile is not
// clobbered by the default seeding.
func TestTilingSplitHashIdiom(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "hi\n")
	e.ensureTiler()

	// A tiny probe to read #tile back from a command context.
	e.PawScript.RegisterCommand("zz_readtile", func(ctx *pawscript.Context) pawscript.Result {
		ctx.SetResult(ctx.ResolveHashArg(tileDefaultVar))
		return pawscript.BoolStatus(true)
	})

	// Explicit #tile: resolves the seeded default (main) and returns a new tile.
	if res := e.PawScript.ExecuteAsync("viewport_split #tile, right"); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_split #tile: %v", res)
	}
	if h := fmt.Sprintf("%v", e.PawScript.GetResultValue()); h == "0" {
		t.Fatal("viewport_split #tile returned handle 0")
	}

	// Omitted handle: falls back to the #tile default; arg 0 is the direction.
	if res := e.PawScript.ExecuteAsync("viewport_split down"); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_split down (default #tile): %v", res)
	}
	if h := fmt.Sprintf("%v", e.PawScript.GetResultValue()); h == "0" {
		t.Fatal("viewport_split down returned handle 0")
	}

	// Missing direction fails, like other commands.
	if res := e.PawScript.ExecuteAsync("viewport_split"); res != pawscript.BoolStatus(false) {
		t.Fatalf("viewport_split with no direction should fail, got %v", res)
	}

	// #tile was seeded to the main tile.
	e.PawScript.ExecuteAsync("zz_readtile")
	if got := fmt.Sprintf("%v", e.PawScript.GetResultValue()); got != fmt.Sprintf("%d", uint64(e.tilerMain)) {
		t.Fatalf("#tile = %q, want main handle %d", got, e.tilerMain)
	}

	// A script that reassigns #tile is not clobbered by later default seeding.
	e.PawScript.ExecuteAsync("#tile: 2")
	e.PawScript.ExecuteAsync("viewport_split right") // triggers seedTileDefault (guarded)
	e.PawScript.ExecuteAsync("zz_readtile")
	if got := fmt.Sprintf("%v", e.PawScript.GetResultValue()); got != "2" {
		t.Fatalf("after script set #tile:2, #tile = %q; seeding must not clobber it", got)
	}
}
