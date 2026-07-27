package editor

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Vertical drag-past-bottom autoscroll keeps working after horizontal
// scrolling of every kind has occurred — far-column park (scroll_right
// ticks), the horizontal wheel, and fresh drags thereafter. Regression guard
// for a reported stall; the tick machinery is exercised directly.
func TestVertAutoscrollSurvivesHorizScroll(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&b, "line%02d-%s\n", i, strings.Repeat("x", 150))
	}
	e, w, send := dragHarness(t, b.String())

	x, y := screenAt(w, 0, 1)
	send(fmt.Sprintf("Mouse@%d,%d", x, y))
	send("MouseLeftPress")
	belowY := w.ContentY + w.ContentHeight + 2

	// Phase 1: vertical works.
	send(fmt.Sprintf("MouseLeftDrag@%d,%d", x, belowY))
	e.dragScroll.since = e.dragScroll.since.Add(-time.Second)
	top0 := w.ViewState.ViewOffsetY
	e.dragScrollTick()
	if w.ViewState.ViewOffsetY <= top0 {
		t.Fatalf("phase1: vertical tick should scroll (top %d)", w.ViewState.ViewOffsetY)
	}

	// Phase 2: park on the far column -> horizontal ticks (scroll_right).
	send(fmt.Sprintf("MouseLeftDrag@%d,%d", w.ContentX+w.ContentWidth, w.ContentY+2))
	e.dragScroll.since = e.dragScroll.since.Add(-time.Second)
	e.dragScrollTick()
	t.Logf("after horiz tick: offX=%d", w.ViewState.ViewOffsetX)

	// Phase 3: back below the bottom -> vertical must still scroll.
	send(fmt.Sprintf("MouseLeftDrag@%d,%d", x, belowY))
	if e.dragScroll.vert == 0 {
		t.Fatalf("phase3: overshoot should re-engage; dragScroll=%+v dragSel=%+v", e.dragScroll, e.dragSel)
	}
	e.dragScroll.since = e.dragScroll.since.Add(-time.Second)
	top1 := w.ViewState.ViewOffsetY
	e.dragScrollTick()
	if w.ViewState.ViewOffsetY <= top1 {
		t.Fatalf("phase3: vertical tick should still scroll after horiz (top %d, was %d, dragScroll=%+v)",
			w.ViewState.ViewOffsetY, top1, e.dragScroll)
	}

	// Phase 4: release; new drag after a horizontal WHEEL scroll on the row.
	send("MouseLeftRelease")
	send(fmt.Sprintf("Mouse@%d,%d", x, w.ContentY+1))
	for i := 0; i < 10; i++ {
		send("MouseScrollRight") // wheel horizontal (barrier + steps)
	}
	t.Logf("after wheel horiz: offX=%d", w.ViewState.ViewOffsetX)
	send(fmt.Sprintf("Mouse@%d,%d", x, w.ContentY+1))
	send("MouseLeftPress")
	send(fmt.Sprintf("MouseLeftDrag@%d,%d", x, belowY))
	if e.dragScroll.stop == nil {
		t.Fatalf("phase4: ticker should be armed; dragScroll=%+v dragSel=%+v", e.dragScroll, e.dragSel)
	}
	e.dragScroll.since = e.dragScroll.since.Add(-time.Second)
	top2 := w.ViewState.ViewOffsetY
	e.dragScrollTick()
	if w.ViewState.ViewOffsetY <= top2 {
		t.Fatalf("phase4: vertical tick should scroll on a fresh drag after wheel-horiz (top %d, was %d, dragScroll=%+v dragSel=%+v)",
			w.ViewState.ViewOffsetY, top2, e.dragScroll, e.dragSel)
	}
}

// A host that cannot deliver drag coordinates beyond its grid (an SDL
// surface, a torn-off window: motion clips at the window edge) pins the
// pointer on the LAST grid row. When the viewport's content reaches that row,
// parking there is the down-edge gesture — autoscroll must engage, exactly as
// parking on the far column does horizontally. (Reported on the KittyTK SDL
// host: selection kept updating but downward autoscroll never engaged.)
func TestDragAutoscrollEngagesOnPinnedGridEdge(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&b, "line%02d\n", i)
	}
	e, w, send := dragHarness(t, b.String())

	gridBottom := e.Renderer.Height
	if w.ContentY+w.ContentHeight < gridBottom {
		// Make the scene edge-touching: the harness may reserve a bottom row.
		// Force the geometry the SDL/torn-off report describes.
		w.ContentHeight = gridBottom - w.ContentY
	}

	x, y := screenAt(w, 0, 1)
	send(fmt.Sprintf("Mouse@%d,%d", x, y))
	send("MouseLeftPress")
	// The pointer parks ON the last grid row — the host cannot report beyond.
	send(fmt.Sprintf("MouseLeftDrag@%d,%d", x, gridBottom))
	if e.dragScroll.vert != 1 {
		t.Fatalf("parking on the grid's last row should read as +1 down overshoot; got %d", e.dragScroll.vert)
	}
	e.dragScroll.since = e.dragScroll.since.Add(-time.Second)
	topBefore := w.ViewState.ViewOffsetY
	e.dragScrollTick()
	if w.ViewState.ViewOffsetY != topBefore+1 {
		t.Fatalf("the pinned-edge gesture should scroll down; top %d, want %d",
			w.ViewState.ViewOffsetY, topBefore+1)
	}
	// And it keeps scrolling on subsequent ticks.
	e.dragScrollTick()
	if w.ViewState.ViewOffsetY != topBefore+2 {
		t.Fatalf("autoscroll should continue; top %d", w.ViewState.ViewOffsetY)
	}
	send("MouseLeftRelease")
}

// The pinned-edge rule stays DORMANT when rows exist beyond the viewport (a
// bottom-docked modebar): parking on the viewport's own last row must not
// autoscroll — only genuinely crossing its bottom edge does.
func TestDragAutoscrollDormantWhenRowsExistBeyond(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&b, "line%02d\n", i)
	}
	e, w, send := dragHarness(t, b.String())
	if w.ContentY+w.ContentHeight >= e.Renderer.Height {
		// The harness scene must have a row beyond (it reserves one); if not,
		// shrink the viewport to model the modebar-at-bottom layout.
		w.ContentHeight = e.Renderer.Height - w.ContentY - 1
	}

	x, y := screenAt(w, 0, 1)
	send(fmt.Sprintf("Mouse@%d,%d", x, y))
	send("MouseLeftPress")
	// Park on the viewport's LAST CONTENT row (not beyond it).
	send(fmt.Sprintf("MouseLeftDrag@%d,%d", x, w.ContentY+w.ContentHeight))
	if e.dragScroll.vert != 0 {
		t.Fatalf("parking on the last content row with rows beyond must not engage; got %d", e.dragScroll.vert)
	}
	send("MouseLeftRelease")
}
