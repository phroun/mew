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

	// A query returns its value via the formal result.
	if res := e.PawScript.ExecuteAsync(fmt.Sprintf("viewport_content %d", main)); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_content: %v", res)
	}
	if got := fmt.Sprintf("%v", e.PawScript.GetResultValue()); got != "main" {
		t.Fatalf("viewport_content result = %q, want \"main\"", got)
	}

	// viewport_split uses the #-handle idiom: the tile comes from a leading
	// #-symbol (here the seeded #tile default -> main) and it returns the new
	// tile's handle (nonzero, distinct from the origin) via the formal result.
	if res := e.PawScript.ExecuteAsync("viewport_split #tile, right"); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_split #tile: %v", res)
	}
	newHandle := fmt.Sprintf("%v", e.PawScript.GetResultValue())
	if newHandle == "0" || newHandle == fmt.Sprintf("%d", main) {
		t.Fatalf("viewport_split returned handle %q; want a new nonzero tile", newHandle)
	}

	// The renamed reading-order cycle commands exist (viewport_next/prior are
	// mew's own; the tiler's are tile_next/tile_prior).
	for _, cmd := range []string{"tile_next", "tile_prior"} {
		if res := e.PawScript.ExecuteAsync(fmt.Sprintf("%s %d", cmd, main)); res != pawscript.BoolStatus(true) {
			t.Fatalf("%s: %v", cmd, res)
		}
	}

	// A toggling command with the state omitted defaults to toggle (no error).
	if res := e.PawScript.ExecuteAsync(fmt.Sprintf("viewport_zoom %d", main)); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_zoom <tile>: %v", res)
	}

	// Missing REQUIRED arguments fail, like other commands.
	if res := e.PawScript.ExecuteAsync("viewport_zoom"); res != pawscript.BoolStatus(false) {
		t.Fatalf("viewport_zoom with no tile should fail, got %v", res)
	}
	if res := e.PawScript.ExecuteAsync(fmt.Sprintf("viewport_go %d", main)); res != pawscript.BoolStatus(false) {
		t.Fatalf("viewport_go with no direction should fail, got %v", res)
	}
	if res := e.PawScript.ExecuteAsync("viewport_set_caret"); res != pawscript.BoolStatus(false) {
		t.Fatalf("viewport_set_caret with no args should fail, got %v", res)
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
