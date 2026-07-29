package editor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// The scrollbar option (default on) reserves the viewport's outer column —
// the rightmost on an LTR screen — draws the '░' track and '█' thumb there,
// and makes it interactive: a press in the column belongs to the scrollbar
// (never to text selection), a track press jumps, and the drag follows the
// pointer through whole-line scroll positions with no snap-back.
func TestScrollbarReservedAndInteractive(t *testing.T) {
	e, w, out := newRenderedEditor(t, strings.Repeat("line\n", 200))
	// The launch paths (docViewportOptions and kin) seed the option from
	// e.Config.Scrollbar, which defaults on; the bare test harness creates
	// its viewport directly, so opt in the way a user would.
	if !e.Config.Scrollbar {
		t.Fatal("scrollbar option should default on")
	}
	if !e.setOption(w, "scrollbar", "true") {
		t.Fatal("set scrollbar=true failed")
	}
	e.performRender()
	if w.ScrollbarX != 79 {
		t.Fatalf("ScrollbarX = %d, want 79 (the rightmost 0-based column)", w.ScrollbarX)
	}
	if edge := w.ContentX + w.ContentWidth; edge > w.ScrollbarX {
		t.Fatalf("content (ends at %d) overlaps the reserved bar column %d", edge, w.ScrollbarX)
	}
	if s := out.String(); !strings.Contains(s, "░") || !strings.Contains(s, "█") {
		t.Fatal("rendered frame should contain the bar's track and thumb glyphs")
	}

	// A press near the BOTTOM of the track jumps the view deep into the
	// document — and is consumed by the bar, never starting a selection.
	barX := w.ScrollbarX + 1 // 1-based
	bottomY := w.ContentY + w.ContentHeight
	send := func(key string) {
		if !e.handleMouseKey(key) {
			t.Fatalf("pseudo-key %q should be consumed", key)
		}
	}
	send(fmt.Sprintf("Mouse@%d,%d", barX, bottomY))
	send("MouseLeftPress")
	if w.ViewState.ViewOffsetY == 0 {
		t.Fatal("track press near the bottom did not scroll — the bar is painted but dead")
	}
	if w.Buffer.HasBlockMarks() {
		t.Fatal("press in the scrollbar column started a text selection")
	}
	if !w.ViewState.ScrollDetached {
		t.Fatal("a scrollbar scroll should park the view like the wheel does")
	}

	// Dragging the thumb to the top of the track returns to line 0; the
	// release ends the gesture without disturbing the position.
	topY := w.ContentY + 1
	send(fmt.Sprintf("MouseLeftDrag@%d,%d", barX, topY))
	send("MouseLeftRelease")
	if w.ViewState.ViewOffsetY != 0 {
		t.Fatalf("drag to track top left ViewOffsetY=%d, want 0", w.ViewState.ViewOffsetY)
	}

	// The capture is over: a later press in the content area is an ordinary
	// text press, not a leaked scrollbar gesture.
	send(fmt.Sprintf("Mouse@%d,%d", w.ContentX+2, w.ContentY+2))
	send("MouseLeftPress")
	send("MouseLeftRelease")
	if w.ViewState.ViewOffsetY != 0 {
		t.Fatal("content press moved the scroll position after the bar gesture")
	}
}

// Under direction=rtl the bar mirrors to the LEFTMOST column and the content
// shifts right past it.
func TestScrollbarRTLMirrorsLeft(t *testing.T) {
	e, w, _ := newRenderedEditor(t, strings.Repeat("x\n", 100))
	if !e.setOption(w, "scrollbar", "true") {
		t.Fatal("set scrollbar=true failed")
	}
	if !e.setOption(w, "direction", "rtl") {
		t.Fatal("set direction=rtl failed")
	}
	e.performRender()

	if w.ScrollbarX != 0 {
		t.Fatalf("RTL ScrollbarX = %d, want 0 (the leftmost column)", w.ScrollbarX)
	}
	if w.ContentX < 1 {
		t.Fatalf("RTL content starts at %d, must sit past the bar column", w.ContentX)
	}

	// A press in the left column scrolls, exactly as on the right in LTR.
	send := func(key string) {
		if !e.handleMouseKey(key) {
			t.Fatalf("pseudo-key %q should be consumed", key)
		}
	}
	send(fmt.Sprintf("Mouse@%d,%d", 1, w.ContentY+w.ContentHeight))
	send("MouseLeftPress")
	send("MouseLeftRelease")
	if w.ViewState.ViewOffsetY == 0 {
		t.Fatal("press in the RTL bar column did not scroll")
	}
}

// Turning the option off releases the column back to content and removes the
// bar from hit-testing.
func TestScrollbarOptionOff(t *testing.T) {
	e, w, _ := newRenderedEditor(t, strings.Repeat("y\n", 50))
	if !e.setOption(w, "scrollbar", "true") {
		t.Fatal("set scrollbar=true failed")
	}
	e.performRender()
	widthOn := w.ContentWidth

	if !e.setOption(w, "scrollbar", "false") {
		t.Fatal("set scrollbar=false failed")
	}
	e.performRender()
	if w.ScrollbarX != -1 {
		t.Fatalf("ScrollbarX = %d with the option off, want -1", w.ScrollbarX)
	}
	if w.ContentWidth != widthOn+1 {
		t.Fatalf("content width %d with the option off, want %d (the column returned)",
			w.ContentWidth, widthOn+1)
	}
}

// A viewport whose buffer hosts a terminal session never shows the editor
// scrollbar — the terminal draws its own, and reserving the column would
// shrink its grid.
func TestScrollbarSuppressedOnPTYViewport(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	if !e.setOption(w, "scrollbar", "true") {
		t.Fatal("set scrollbar=true failed")
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	e.performRender()
	if w.ScrollbarX != -1 {
		t.Fatalf("ScrollbarX = %d on a session viewport, want -1 (suppressed)", w.ScrollbarX)
	}
}

// The thumb geometry contract both the renderer and the mouse rely on.
func TestScrollbarThumbGeometry(t *testing.T) {
	// A document that fits fills the track.
	if pos, size := viewport.ScrollbarThumb(20, 10, 0); pos != 0 || size != 20 {
		t.Fatalf("fitting doc: thumb (%d,%d), want (0,20)", pos, size)
	}
	// A long document: thumb proportional, endpoints exact.
	pos, size := viewport.ScrollbarThumb(20, 1000, 0)
	if pos != 0 || size < 1 || size >= 20 {
		t.Fatalf("long doc at top: thumb (%d,%d)", pos, size)
	}
	if pos, _ := viewport.ScrollbarThumb(20, 1000, 1000-20); pos != 20-size {
		t.Fatalf("long doc at bottom: pos %d, want %d (track end)", pos, 20-size)
	}
	// The inverse round-trips the endpoints.
	if top := viewport.ScrollbarTopForThumb(0, 20, 1000); top != 0 {
		t.Fatalf("inverse at track top: %d, want 0", top)
	}
	if top := viewport.ScrollbarTopForThumb(20-size, 20, 1000); top != 1000-20 {
		t.Fatalf("inverse at track bottom: %d, want %d", top, 1000-20)
	}
}
