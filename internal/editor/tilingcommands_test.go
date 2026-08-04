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

	// A structural command fires the library method and returns the new tile's
	// handle (nonzero, distinct from the origin) via the formal result.
	if res := e.PawScript.ExecuteAsync(fmt.Sprintf("viewport_split %d, right", main)); res != pawscript.BoolStatus(true) {
		t.Fatalf("viewport_split: %v", res)
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
