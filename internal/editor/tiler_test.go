package editor

import (
	"fmt"
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

// TestMouseHitDistinguishesSideBySideTiles: two tiles share the same rows, so
// the click must resolve by column, not row alone.
func TestMouseHitDistinguishesSideBySideTiles(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "left\n")
	focusMainViewport(e, "doc2", "right\n") // doc (left) | doc2 (right)
	e.performRender()                       // stamp per-viewport geometry

	doc := e.ViewportManager.GetViewport("doc")
	doc2 := e.ViewportManager.GetViewport("doc2")
	if doc == nil || doc2 == nil {
		t.Fatal("both viewports should exist")
	}
	row := doc.ContentY + 1 // 1-based; both tiles span these rows

	if hit := e.viewportAt(doc.FrameX+2, row); hit != doc {
		t.Fatalf("a click in the left tile resolved to %v, want doc", vpID(hit))
	}
	if hit := e.viewportAt(doc2.FrameX+2, row); hit != doc2 {
		t.Fatalf("a click in the right tile resolved to %v, want doc2 (row-only match would wrongly pick doc)", vpID(hit))
	}
}

func vpID(w *viewport.Viewport) string {
	if w == nil {
		return "<nil>"
	}
	return w.ID
}

// TestTilesCoverWidthExactly guards the edge-rounding: at a fractional split
// width, the tiles' frames must tile the full width with no gap or overlap
// (truncating each tile's width independently used to leave a one-cell gap).
func TestTilesCoverWidthExactly(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "one\n")
	focusMainViewport(e, "doc2", "two\n") // two side-by-side tiles

	for _, W := range []int{80, 81, 83, 91} {
		e.Renderer.Width = W
		layout := viewport.Layout{MainHeight: 20, TopHeight: 0}
		e.applyTilerGeometry(&layout)

		type span struct{ x0, x1 int }
		spans := make([]span, 0, len(layout.MainLayout))
		for _, wl := range layout.MainLayout {
			spans = append(spans, span{wl.Viewport.FrameX, wl.Viewport.FrameX + wl.Viewport.FrameWidth})
		}
		// sort by start
		for i := 1; i < len(spans); i++ {
			for j := i; j > 0 && spans[j].x0 < spans[j-1].x0; j-- {
				spans[j], spans[j-1] = spans[j-1], spans[j]
			}
		}
		if len(spans) == 0 || spans[0].x0 != 0 {
			t.Fatalf("W=%d: main area does not start at column 0: %+v", W, spans)
		}
		for i := 1; i < len(spans); i++ {
			if spans[i].x0 != spans[i-1].x1 {
				t.Fatalf("W=%d: gap/overlap between tiles at %d vs %d: %+v", W, spans[i-1].x1, spans[i].x0, spans)
			}
		}
		if last := spans[len(spans)-1].x1; last != W {
			t.Fatalf("W=%d: tiles reach column %d, not the full width", W, last)
		}
	}
}

// TestSplitClonedRefPaintsBothTiles: viewport_split clones the origin tile's
// ref, so two tiles show the SAME viewport (tiles↔viewports is many-to-many).
// Geometry lives with the tile, so the viewport must paint in each tile at its
// own frame — not just the last one, which used to leave the other tile blank.
func TestSplitClonedRefPaintsBothTiles(t *testing.T) {
	e, _, out := newRenderedEditor(t, "HELLO\n")
	e.ensureTiler()
	// Explicit ref = the origin viewport, so both tiles show it (a mirror);
	// without a ref the split opens a fresh mew:/buffers pane instead.
	e.PawScript.ExecuteAsync("viewport_split #tile, right, doc")

	out.Reset()
	e.performRender()

	var left, right bool
	for _, cols := range glyphCols(out.String(), 'H') {
		for _, c := range cols {
			if c <= e.Renderer.Width/2 {
				left = true
			} else {
				right = true
			}
		}
	}
	if !left || !right {
		t.Fatalf("a cloned-ref split must paint content in BOTH tiles; left=%v right=%v", left, right)
	}
}

// TestSplitClonedRefHitTestsBothTiles: a click in either tile of a cloned-ref
// split resolves to the viewport, and the hit tile's content offset is applied
// so the cell→document mapping uses that tile's geometry (not the last-painted
// tile's).
func TestSplitClonedRefHitTestsBothTiles(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "HELLO WORLD\n")
	e.ensureTiler()
	e.PawScript.ExecuteAsync("viewport_split #tile, right, doc") // explicit ref → mirror
	e.performRender()

	doc := e.ViewportManager.GetViewport("doc")
	leftX, rightX := -1, -1
	for _, tl := range e.mainTiles {
		if tl.Viewport == doc {
			if tl.FrameX == 0 {
				leftX = tl.ContentX
			} else {
				rightX = tl.ContentX
			}
		}
	}
	if leftX < 0 || rightX <= leftX {
		t.Fatalf("expected two side-by-side tiles for doc; leftX=%d rightX=%d", leftX, rightX)
	}
	row := doc.ContentY + 1

	if hit := e.viewportAt(leftX+1, row); hit != doc {
		t.Fatalf("left-tile click resolved to %v, want doc", vpID(hit))
	}
	if doc.ContentX != leftX {
		t.Fatalf("left click must apply the left tile offset; ContentX=%d want %d", doc.ContentX, leftX)
	}
	if hit := e.viewportAt(rightX+1, row); hit != doc {
		t.Fatalf("right-tile click resolved to %v, want doc", vpID(hit))
	}
	if doc.ContentX != rightX {
		t.Fatalf("right click must apply the right tile offset; ContentX=%d want %d", doc.ContentX, rightX)
	}
}

// TestSplitScrollbarPressGrabsThePane: each tile of a cloned-ref split has its
// own scrollbar; pressing the LEFT tile's bar column must grab that bar and
// scroll, not fall through to a text selection (which happened when the
// scrollbar column was read from the last-painted tile only).
func TestSplitScrollbarPressGrabsThePane(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "line %02d\n", i)
	}
	e, w, _ := newRenderedEditor(t, b.String())
	if !e.setOption(w, "scrollbar", "true") {
		t.Fatal("set scrollbar=true failed")
	}
	e.ensureTiler()
	e.performRender()
	e.PawScript.ExecuteAsync("viewport_split #tile, right")
	e.performRender()

	doc := e.ViewportManager.GetViewport("doc")
	leftSb := -1
	for _, tl := range e.mainTiles {
		if tl.Viewport == doc && tl.FrameX == 0 {
			leftSb = tl.ScrollbarX
		}
	}
	if leftSb < 0 {
		t.Fatalf("left tile has no scrollbar column; tiles=%d", len(e.mainTiles))
	}

	send := func(key string) {
		if !e.handleMouseKey(key) {
			t.Fatalf("pseudo-key %q should be consumed", key)
		}
	}
	bottomY := doc.ContentY + doc.ContentHeight // near the track bottom
	send(fmt.Sprintf("Mouse@%d,%d", leftSb+1, bottomY))
	send("MouseLeft")
	if doc.ViewState.ViewOffsetY == 0 {
		t.Fatal("press on the LEFT tile's scrollbar did not scroll")
	}
	if doc.Buffer.HasBlockMarks() {
		t.Fatal("press on the LEFT tile's scrollbar started a text selection")
	}
}

// TestViewportAtStrictTileBounds: a tiled viewport is hit ONLY within a tile's
// actual rectangle. Even when its canonical (focused-tile) geometry is large, a
// click outside a short/narrow tile must not route to it — no bleed.
func TestViewportAtStrictTileBounds(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "hi\n")
	e.performRender()
	doc := e.ViewportManager.GetFocusedViewport()

	// Model a short tile whose viewport's canonical geometry (from a taller tile
	// it also appears in) is large.
	e.mainTiles = []viewport.ViewportLayout{{
		Viewport: doc, FrameX: 0, FrameWidth: e.Renderer.Width,
		ContentX: 0, ContentY: 0, ContentWidth: e.Renderer.Width, ContentHeight: 3,
	}}
	doc.ContentY, doc.ContentHeight = 0, 100 // canonical says "tall"
	doc.FrameX, doc.FrameWidth = 0, e.Renderer.Width

	if hit := e.viewportAt(1, 2); hit != doc { // inside the short tile
		t.Fatalf("click inside the tile should route to it, got %v", vpID(hit))
	}
	if hit := e.viewportAt(1, 50); hit != nil { // below the short tile
		t.Fatalf("click below the short tile must not bleed to it, got %v", vpID(hit))
	}
	// And narrow: shrink the tile's width, click past its right edge.
	e.mainTiles[0].FrameWidth = 10
	if hit := e.viewportAt(40, 2); hit != nil {
		t.Fatalf("click past the narrow tile's right edge must not bleed, got %v", vpID(hit))
	}
}

// TestSplitDownFocusedTileCanonical: with a viewport stacked in two tiles, the
// viewport rests on the FOCUSED tile's geometry after render (not whichever tile
// painted last), so the caret and paging use the focused pane.
func TestSplitDownFocusedTileCanonical(t *testing.T) {
	e, _, _ := newRenderedEditor(t, strings.Repeat("x\n", 60))
	e.ensureTiler()
	e.performRender()
	e.PawScript.ExecuteAsync("viewport_split #tile, down, doc") // explicit ref → mirror
	e.performRender()

	doc := e.ViewportManager.GetViewport("doc")
	var focused, other *viewport.ViewportLayout
	for i := range e.mainTiles {
		if e.mainTiles[i].Viewport != doc {
			continue
		}
		if e.mainTiles[i].Focused {
			focused = &e.mainTiles[i]
		} else {
			other = &e.mainTiles[i]
		}
	}
	if focused == nil || other == nil {
		t.Fatalf("want one focused + one non-focused tile of doc; focused=%v other=%v", focused != nil, other != nil)
	}
	if focused.ContentY == other.ContentY {
		t.Skip("tiles share a row — cannot distinguish canonical")
	}
	if doc.ContentY != focused.ContentY {
		t.Fatalf("viewport ContentY=%d is not the FOCUSED tile's %d (other tile's is %d) — canonical not applied",
			doc.ContentY, focused.ContentY, other.ContentY)
	}
}

// TestPressFocusesPressedTile: with a viewport stacked in two tiles, pressing in
// a tile makes IT canonical — so the caret, paging, scroll clamp, and any drag
// autoscroll follow the pane pressed in, not whichever tile last held focus.
func TestPressFocusesPressedTile(t *testing.T) {
	e, _, _ := newRenderedEditor(t, strings.Repeat("x\n", 60))
	e.ensureTiler()
	e.performRender()
	e.PawScript.ExecuteAsync("viewport_split #tile, down")
	e.performRender()

	doc := e.ViewportManager.GetViewport("doc")
	// Capture the top and bottom tiles' press coordinates and content rows.
	topY, botY := 1<<30, -1
	var topX, topRow, botX, botRow int
	for i := range e.mainTiles {
		if e.mainTiles[i].Viewport != doc {
			continue
		}
		cy := e.mainTiles[i].ContentY
		if cy < topY {
			topY, topX, topRow = cy, e.mainTiles[i].ContentX+1, cy+1
		}
		if cy > botY {
			botY, botX, botRow = cy, e.mainTiles[i].ContentX+1, cy+1
		}
	}
	if topY == botY {
		t.Skip("tiles share a row — cannot distinguish")
	}

	// Press in the TOP tile → top becomes canonical.
	e.mousePress(topX, topRow, false)
	e.performRender()
	if doc.ContentY != topY {
		t.Fatalf("press in top tile: canonical ContentY=%d, want top %d (bottom %d)", doc.ContentY, topY, botY)
	}
	// Press in the BOTTOM tile → bottom becomes canonical.
	e.mousePress(botX, botRow, false)
	e.performRender()
	if doc.ContentY != botY {
		t.Fatalf("press in bottom tile: canonical ContentY=%d, want bottom %d (top %d)", doc.ContentY, botY, topY)
	}
}

// TestSwitchIntoMirroredViewportFocusesClickedTile: switching focus by clicking
// a tile that shows an UNFOCUSED viewport shown in several tiles must land on
// the tile under the pointer, not the first mirror. FocusViewportAsCycle runs
// tilerFollowFocus (which picks the first tile of the ref); the press then has
// to override that to the clicked tile.
func TestSwitchIntoMirroredViewportFocusesClickedTile(t *testing.T) {
	e, _, _ := newRenderedEditor(t, strings.Repeat("x\n", 60))
	e.ensureTiler()
	e.performRender()
	// doc2 becomes the focused viewport in its own tile, then splits into two
	// tiles (a mirror): doc2 now appears twice.
	focusMainViewport(e, "doc2", strings.Repeat("y\n", 60))
	e.performRender()
	e.PawScript.ExecuteAsync("viewport_split #tile, down, doc2") // explicit ref → mirror
	e.performRender()

	doc2 := e.ViewportManager.GetViewport("doc2")
	// Collect doc2's two tiles (distinct rows from a down-split).
	var tiles []viewport.ViewportLayout
	for i := range e.mainTiles {
		if e.mainTiles[i].Viewport == doc2 {
			tiles = append(tiles, e.mainTiles[i])
		}
	}
	if len(tiles) != 2 {
		t.Fatalf("want doc2 shown in two tiles, got %d", len(tiles))
	}
	if tiles[0].ContentY == tiles[1].ContentY {
		t.Skip("tiles share a row — cannot distinguish the clicked pane")
	}

	// Click each of doc2's tiles in turn, each time FROM the other viewport
	// (doc), so the press is always the unfocused-switch path. Focus must land
	// on the tile clicked — including the one tilerFollowFocus would not pick.
	for _, want := range tiles {
		e.ViewportManager.SetFocus("doc") // doc2 is unfocused again
		e.performRender()
		if e.ViewportManager.GetFocusedViewport().ID != "doc" {
			t.Fatalf("setup: expected doc focused before the click")
		}
		e.mousePress(want.ContentX+1, want.ContentY+1, false)
		e.performRender()
		if e.ViewportManager.GetFocusedViewport() != doc2 {
			t.Fatalf("click did not switch focus to doc2")
		}
		if got := uint64(e.tiler.GetFocus()); got != want.TileHandle {
			t.Errorf("clicked tile at row %d (handle %d): tiler focus is handle %d — landed on a different tile of the same ref",
				want.ContentY, want.TileHandle, got)
		}
	}
}

// TestScrollbarDragUsesGrabbedTile: grabbing a NON-focused tile's scrollbar and
// dragging must compute the thumb against THAT tile's geometry, not the
// viewport's canonical (focused) tile — the press doesn't steal focus.
func TestScrollbarDragUsesGrabbedTile(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&b, "line %03d\n", i)
	}
	e, w0, _ := newRenderedEditor(t, b.String())
	if !e.setOption(w0, "scrollbar", "true") {
		t.Fatal("set scrollbar=true failed")
	}
	e.ensureTiler()
	e.performRender()
	e.PawScript.ExecuteAsync("viewport_split #tile, down")
	e.performRender()

	doc := e.ViewportManager.GetViewport("doc")
	topY, botY := 1<<30, -1
	var botSbX, botSbRow, botContentY int
	for i := range e.mainTiles {
		if e.mainTiles[i].Viewport != doc {
			continue
		}
		cy := e.mainTiles[i].ContentY
		if cy < topY {
			topY = cy
		}
		if cy > botY {
			botY, botSbX, botSbRow, botContentY = cy, e.mainTiles[i].ScrollbarX, cy+1, cy
		}
	}
	if topY == botY || botSbX < 0 {
		t.Skip("need two stacked tiles with scrollbars")
	}

	// Grab the BOTTOM tile's bar (does not steal focus; canonical stays the top).
	if !e.scrollbarPressAt(botSbX+1, botSbRow) {
		t.Fatal("press on the bottom tile's scrollbar was not consumed")
	}
	if e.sbDrag.tileHandle == 0 {
		t.Fatal("the drag did not capture the grabbed tile")
	}
	e.scrollbarDrag(botSbRow + 2)
	if doc.ContentY != botContentY {
		t.Fatalf("scrollbar drag geometry ContentY=%d, want the grabbed bottom tile's %d (top is %d)",
			doc.ContentY, botContentY, topY)
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
