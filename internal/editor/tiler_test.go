package editor

import (
	"regexp"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/textwidth"
	"github.com/phroun/mew/internal/viewport"
)

// glyphCols returns, per 1-based screen row, the set of 1-based columns at which
// the rune `target` was painted. It walks the frame's cursor-position (CUP)
// segments the way rowCells does, but records only cells carrying `target` —
// so background spaces that fill the rest of the row are ignored.
func glyphCols(frame string, target rune) map[int][]int {
	cup := regexp.MustCompile(`\x1b\[(\d+);(\d+)H`)
	sgr := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	out := map[int][]int{}
	idx := cup.FindAllStringSubmatchIndex(frame, -1)
	for i, m := range idx {
		row := 0
		for _, c := range frame[m[2]:m[3]] {
			row = row*10 + int(c-'0')
		}
		col := 0
		for _, c := range frame[m[4]:m[5]] {
			col = col*10 + int(c-'0')
		}
		end := len(frame)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		seg := sgr.ReplaceAllString(frame[m[1]:end], "")
		seg = strings.ReplaceAll(seg, "\x1b[K", "")
		n := col // 1-based column of the next glyph
		for _, r := range seg {
			if r == '\x1b' || r < 0x20 {
				continue
			}
			if r == target {
				out[row] = append(out[row], n)
			}
			n += textwidth.Rune(r)
		}
	}
	return out
}

// tileRefs returns the refs of the tiler's current visible tiles.
func tileRefs(e *Editor) []string {
	var refs []string
	if e.tiler == nil {
		return refs
	}
	for _, b := range e.tiler.Tiles() {
		refs = append(refs, b.Ref)
	}
	return refs
}

func focusMainViewport(e *Editor, id, content string) {
	e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		ID: id, Type: viewport.DocViewport, Dock: viewport.DockNone,
		Buffer: buffer.NewFromString(content), SetFocus: true, Visible: true,
	})
}

// TestFirstFocusFillsTheSingleTile: the tiler starts as one empty tile; mew's
// initial focused viewport fills it, and it renders across the whole main area.
func TestFirstFocusFillsTheSingleTile(t *testing.T) {
	e, _, out := newRenderedEditor(t, strings.Repeat("x", 80)+"\n")
	e.performRender()

	if refs := tileRefs(e); len(refs) != 1 || refs[0] != "doc" {
		t.Fatalf("tiler tiles = %v; want a single tile holding \"doc\"", refs)
	}

	// One tile → full-width paint (contrast the retired half-width placeholder).
	max := 0
	for _, c := range glyphCols(out.String(), 'x')[1] {
		if c > max {
			max = c
		}
	}
	if max < 70 {
		t.Fatalf("document glyphs reach only column %d; the single tile should fill the width", max)
	}
}

// TestFocusingSecondViewportMakesSecondTile: focusing another main viewport that
// has no tile splits a new tile off to the right (the "additional ones" path).
func TestFocusingSecondViewportMakesSecondTile(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "one\n")
	if refs := tileRefs(e); len(refs) != 1 || refs[0] != "doc" {
		t.Fatalf("initial tiles = %v; want [doc]", refs)
	}

	focusMainViewport(e, "doc2", "two\n")

	refs := tileRefs(e)
	if len(refs) != 2 {
		t.Fatalf("after focusing a second viewport, tiles = %v; want 2", refs)
	}
	var haveDoc, haveDoc2 bool
	for _, r := range refs {
		haveDoc = haveDoc || r == "doc"
		haveDoc2 = haveDoc2 || r == "doc2"
	}
	if !haveDoc || !haveDoc2 {
		t.Fatalf("tiles = %v; want both doc and doc2", refs)
	}
}

// TestRefocusingReusesExistingTile: focusing back to a viewport that already has
// a tile must NOT create another — the hook finds and activates the existing one.
func TestRefocusingReusesExistingTile(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "one\n")
	focusMainViewport(e, "doc2", "two\n") // 2 tiles now
	if got := len(tileRefs(e)); got != 2 {
		t.Fatalf("want 2 tiles, got %d", got)
	}
	e.ViewportManager.SetFocus("doc") // back to the first
	if got := len(tileRefs(e)); got != 2 {
		t.Fatalf("refocusing an already-tiled viewport changed tile count to %d; want 2", got)
	}
}

// TestBufferCloseDismissesTile: closing a viewport also removes its tile.
func TestBufferCloseDismissesTile(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "one\n")
	focusMainViewport(e, "doc2", "two\n")
	if got := len(tileRefs(e)); got != 2 {
		t.Fatalf("want 2 tiles, got %d", got)
	}

	e.finishCloseBuffer("doc2")

	for _, r := range tileRefs(e) {
		if r == "doc2" {
			t.Fatalf("doc2's tile survived buffer close: %v", tileRefs(e))
		}
	}
}
