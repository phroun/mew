package editor

import (
	"strings"
	"testing"
)

// hasBarGlyphAt reports whether a scrollbar track '░' or thumb '█' is painted at
// a 1-based screen cell.
func hasBarGlyphAt(frame string, col, row int) bool {
	for _, g := range []rune{'░', '█'} {
		for r, cols := range glyphCols(frame, g) {
			if r != row {
				continue
			}
			for _, c := range cols {
				if c == col {
					return true
				}
			}
		}
	}
	return false
}

// In the TUI (mew draws its own '░'/'█' bar), a viewport mirrored across tiles of
// DIFFERENT heights must draw each tile's bar at that tile's OWN track height.
// The draw read the viewport's shared ScrollbarTrackH — which holds only the
// LAST tile's value — so a tall tile drew the short tile's bar and its lower rows
// went unrendered (and the drag, which resolves per tile, disagreed with the
// paint). The SDL host was already correct because it draws from the per-tile
// regions mew publishes; this brings the TUI's own draw in line.
func TestTUIMirroredTilesEachDrawTheirOwnBarHeight(t *testing.T) {
	e, w, out := newRenderedEditor(t, strings.Repeat("line\n", 400))
	if !e.setOption(w, "scrollbar", "true") {
		t.Fatal("enable scrollbar")
	}
	e.ensureTiler()
	// One viewport across three tiles of unequal heights; the short tile's
	// geometry is stamped last, which is what exposed the bug.
	e.PawScript.ExecuteAsync("viewport_split #tile, down")
	e.PawScript.ExecuteAsync("viewport_split #tile, right")
	out.Reset()
	e.performRender()
	frame := out.String()

	checked := 0
	for i := range e.mainTiles {
		mt := &e.mainTiles[i]
		if mt.Viewport == nil || mt.ScrollbarX < 0 || mt.ScrollbarTrackH <= 0 {
			continue
		}
		checked++
		col := mt.ScrollbarX + 1              // 1-based screen column
		bottom := mt.ContentY + mt.ScrollbarTrackH // 1-based bottom row of this tile's track
		if !hasBarGlyphAt(frame, col, bottom) {
			t.Errorf("tile %d (col %d, ContentY=%d, TrackH=%d): no bar glyph at its bottom row %d — "+
				"the bar was drawn shorter than the tile (a taller tile borrowed a shorter tile's height)",
				i, col, mt.ContentY, mt.ScrollbarTrackH, bottom)
		}
	}
	if checked < 3 {
		t.Fatalf("expected 3 mirrored tiles with bars, checked %d", checked)
	}
}
