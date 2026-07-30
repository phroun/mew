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
	// A document that FITS has no thumb at all: the whole bar is track, and
	// there is nothing a drag could move. A full-length thumb would say "all of
	// it is visible" in the same language a scrollable bar uses for "nearly all
	// of it is visible", and would invite a gesture that cannot do anything.
	if pos, size := viewport.ScrollbarThumb(20, 20, 10, 0); pos != 0 || size != 0 {
		t.Fatalf("fitting doc: thumb (%d,%d), want no thumb (0,0)", pos, size)
	}
	// Exactly filling it is still not MORE than fits, so still no thumb.
	if _, size := viewport.ScrollbarThumb(20, 20, 20, 0); size != 0 {
		t.Fatalf("exactly-fitting doc: size %d, want no thumb", size)
	}
	// A long document: thumb proportional, endpoints exact. The bottom of the
	// travel is the bottom of the SCROLL RANGE, which reaches one blank line
	// past the end of the document.
	maxTop := viewport.MaxScrollTop(20, 1000)
	if maxTop != 1000-20+1 {
		t.Fatalf("MaxScrollTop = %d, want %d (one row past the end)", maxTop, 1000-20+1)
	}
	pos, size := viewport.ScrollbarThumb(20, 20, 1000, 0)
	if pos != 0 || size < 1 || size >= 20 {
		t.Fatalf("long doc at top: thumb (%d,%d)", pos, size)
	}
	if pos, _ := viewport.ScrollbarThumb(20, 20, 1000, maxTop); pos != 20-size {
		t.Fatalf("long doc at bottom: pos %d, want %d (track end)", pos, 20-size)
	}
	// The inverse round-trips the endpoints.
	if top := viewport.ScrollbarTopForThumb(0, 20, 20, 1000); top != 0 {
		t.Fatalf("inverse at track top: %d, want 0", top)
	}
	if top := viewport.ScrollbarTopForThumb(20-size, 20, 20, 1000); top != maxTop {
		t.Fatalf("inverse at track bottom: %d, want %d", top, maxTop)
	}

	// A track one row shorter than the page (the given-up bottom-right
	// corner): the thumb still bottoms out INSIDE the track at max scroll —
	// on the second-to-last content row — and the endpoints still invert to
	// the full scroll range.
	pos19, size19 := viewport.ScrollbarThumb(19, 20, 1000, maxTop)
	if pos19+size19 != 19 {
		t.Fatalf("corner track at bottom: thumb ends at %d, want 19 (never clipped)", pos19+size19)
	}
	if top := viewport.ScrollbarTopForThumb(19-size19, 19, 20, 1000); top != maxTop {
		t.Fatalf("corner track inverse at bottom: %d, want %d", top, maxTop)
	}
}

// How far down a viewport scrolls: far enough to reveal exactly one row past the
// end of the document — the one the gutter marks "~" — never far enough to leave
// a mostly-empty window with a line or two of text stranded at the top.
func TestMaxScrollTop(t *testing.T) {
	cases := []struct{ page, lines, want int }{
		{24, 1000, 977}, // one "~" row on the bottom row
		{24, 25, 2},     // one line taller than the view
		{24, 24, 0},     // exactly fills it: nothing hidden, nothing to scroll
		{24, 10, 0},     // shorter than the view
		{24, 0, 0},      // empty
		{0, 1000, 999},  // no layout yet: the old widest-possible answer
	}
	for _, c := range cases {
		if got := viewport.MaxScrollTop(c.page, c.lines); got != c.want {
			t.Errorf("MaxScrollTop(page=%d, lines=%d) = %d, want %d", c.page, c.lines, got, c.want)
		}
	}
}

// The clamp is enforced where the scrolling happens, so the wheel, the
// drag-autoscroll, mew's own bar, every scroll_* command and a host's
// scroll_viewport all stop at the same place.
func TestScrollViewToStopsOneRowPastTheEnd(t *testing.T) {
	e, w, _ := newRenderedEditor(t, strings.Repeat("line\n", 200))
	e.performRender()
	page, lines := w.ContentHeight, w.Buffer.GetLineCount()
	if page <= 0 {
		t.Fatalf("ContentHeight = %d, want a laid-out viewport", page)
	}
	want := lines - page + 1

	e.scrollViewTo(w, 100000) // as far as anything could ask
	if got := w.ViewState.ViewOffsetY; got != want {
		t.Fatalf("top = %d after scrolling past the end, want %d", got, want)
	}
	// Exactly one row past the end of the document is on screen: the bottom one.
	if past := (want + page) - lines; past != 1 {
		t.Fatalf("%d rows past the end of the document, want exactly 1", past)
	}

	// A document shorter than the view does not scroll at all.
	short, sw, _ := newRenderedEditor(t, "a\nb\nc\n")
	short.performRender()
	short.scrollViewTo(sw, 100000)
	if got := sw.ViewState.ViewOffsetY; got != 0 {
		t.Fatalf("short document scrolled to %d, want 0", got)
	}
}

// A bar with no thumb still OWNS its column: a press on it is consumed so it
// cannot start selecting text there, and it scrolls nothing.
func TestScrollbarPressInertWhenDocumentFits(t *testing.T) {
	e, w, _ := newRenderedEditor(t, "a\nb\nc\n")
	if !e.setOption(w, "scrollbar", "true") {
		t.Fatal("set scrollbar=true failed")
	}
	e.performRender()
	if w.ScrollbarX < 0 {
		t.Skip("no scrollbar column in this layout")
	}
	if _, size := viewport.ScrollbarThumb(w.ScrollbarTrackH, w.ContentHeight,
		w.Buffer.GetLineCount(), w.ViewState.ViewOffsetY); size != 0 {
		t.Fatalf("thumb size %d over a fitting document, want none", size)
	}

	send := func(key string) {
		if !e.handleMouseKey(key) {
			t.Fatalf("pseudo-key %q should be consumed", key)
		}
	}
	send(fmt.Sprintf("Mouse@%d,%d", w.ScrollbarX+1, w.ContentY+1))
	send("MouseLeftPress")
	send("MouseLeftRelease")

	if w.ViewState.ViewOffsetY != 0 {
		t.Fatalf("top = %d, want 0: a bar with no thumb scrolls nothing", w.ViewState.ViewOffsetY)
	}
	if w.Buffer.HasBlockMarks() {
		t.Fatal("press on an inert bar started a text selection")
	}
}

// The bottom-most LTR window gives its bar's bottom cell up to the screen's
// unwritable corner: the track is one row shorter than the content, and at
// maximum scroll the thumb sits on the second-to-last row instead of being
// clipped away with the corner.
func TestScrollbarCornerShortensTrack(t *testing.T) {
	e, w, _ := newRenderedEditor(t, strings.Repeat("line\n", 200))
	if !e.setOption(w, "scrollbar", "true") {
		t.Fatal("set scrollbar=true failed")
	}
	e.performRender()

	if w.ContentY+w.ContentHeight != e.Renderer.Height {
		t.Skipf("layout does not reach the bottom row (ContentY=%d h=%d); corner not in play",
			w.ContentY, w.ContentHeight)
	}
	if w.ScrollbarTrackH != w.ContentHeight-1 {
		t.Fatalf("ScrollbarTrackH = %d, want %d (one row given to the corner)",
			w.ScrollbarTrackH, w.ContentHeight-1)
	}

	// Scrolled to the very bottom, the thumb's last cell is the track's last
	// cell — the second-to-last content row — never the corner row.
	e.scrollViewTo(w, viewport.MaxScrollTop(w.ContentHeight, w.Buffer.GetLineCount()))
	pos, size := viewport.ScrollbarThumb(w.ScrollbarTrackH, w.ContentHeight,
		w.Buffer.GetLineCount(), w.ViewState.ViewOffsetY)
	if pos+size != w.ScrollbarTrackH {
		t.Fatalf("thumb ends at %d at max scroll, want %d", pos+size, w.ScrollbarTrackH)
	}

	// A press on the corner row itself still belongs to the bar: it acts as
	// the bottom of the track (deep scroll), not a text click.
	e.scrollViewTo(w, 0)
	send := func(key string) {
		if !e.handleMouseKey(key) {
			t.Fatalf("pseudo-key %q should be consumed", key)
		}
	}
	send(fmt.Sprintf("Mouse@%d,%d", w.ScrollbarX+1, w.ContentY+w.ContentHeight))
	send("MouseLeftPress")
	send("MouseLeftRelease")
	if w.ViewState.ViewOffsetY == 0 {
		t.Fatal("press on the corner row did not act as a track-bottom press")
	}
	if w.Buffer.HasBlockMarks() {
		t.Fatal("press on the corner row started a text selection")
	}
}

// The I-beam region a graphical host receives must EXCLUDE the reserved
// scrollbar column, so the pointer shows the ordinary arrow over the bar
// rather than the text I-beam.
func TestScrollbarExcludedFromPointerRegion(t *testing.T) {
	e, w, _ := newRenderedEditor(t, strings.Repeat("line\n", 100))
	if !e.setOption(w, "scrollbar", "true") {
		t.Fatal("set scrollbar=true failed")
	}
	var rect [4]int
	e.Config.PointerRegion = func(col, row, width, height int, _ []PointerArrowSpan) {
		rect = [4]int{col, row, width, height}
	}
	e.performRender()

	if rect[2] <= 0 {
		t.Skip("no region published (viewport not focused)")
	}
	// 1-based: the region's last column must stop before the bar's column.
	lastCol := rect[0] + rect[2] - 1
	barCol := w.ScrollbarX + 1
	if barCol <= 0 {
		t.Fatal("no scrollbar laid out")
	}
	if lastCol >= barCol {
		t.Fatalf("I-beam region reaches column %d, but the scrollbar is at %d", lastCol, barCol)
	}
}

// With a graphical host drawing the bars, mew still RESERVES the column — the
// layout must be identical on both targets — but paints nothing into it, stops
// hit-testing it, and publishes its geometry and scroll state instead.
func TestScrollbarHostDrawnPublishesAndYields(t *testing.T) {
	e, w, out := newRenderedEditor(t, strings.Repeat("line\n", 300))
	var got []ScrollbarRegion
	e.Config.ScrollbarRegions = func(r []ScrollbarRegion) { got = r }
	if !e.setOption(w, "scrollbar", "true") {
		t.Fatal("set scrollbar=true failed")
	}
	e.performRender()

	// The column is still reserved.
	if w.ScrollbarX < 0 || w.ScrollbarTrackH <= 0 {
		t.Fatal("the column must still be reserved when the host draws")
	}
	// ...but nothing is painted into it.
	if s := out.String(); strings.Contains(s, "░") || strings.Contains(s, "█") {
		t.Fatal("mew must not paint bar glyphs when the host draws them")
	}
	// The geometry and scroll state are published.
	var region *ScrollbarRegion
	for i := range got {
		if got[i].ViewportID == w.ID {
			region = &got[i]
		}
	}
	if region == nil {
		t.Fatalf("no region published for %q; got %v", w.ID, got)
	}
	if region.Col != w.ScrollbarX+1 || region.TrackH != w.ScrollbarTrackH {
		t.Errorf("region %+v does not match the reserved bar (col %d, track %d)",
			*region, w.ScrollbarX+1, w.ScrollbarTrackH)
	}
	if region.Page != w.ContentHeight || region.LineCount != w.Buffer.GetLineCount() {
		t.Errorf("region %+v does not match the view (page %d, lines %d)",
			*region, w.ContentHeight, w.Buffer.GetLineCount())
	}

	// A press in the column is no longer mew's: the host owns it.
	send := func(key string) { e.handleMouseKey(key) }
	send(fmt.Sprintf("Mouse@%d,%d", w.ScrollbarX+1, w.ContentY+w.ContentHeight))
	send("MouseLeftPress")
	send("MouseLeftRelease")
	if w.ViewState.ViewOffsetY != 0 {
		t.Error("mew hit-tested a bar the host owns")
	}

	// scroll_viewport is how the host scrolls, and it lands on a whole line.
	e.executeCommand(fmt.Sprintf("scroll_viewport %q, %q", w.ID, "42"))
	if w.ViewState.ViewOffsetY != 42 {
		t.Errorf("scroll_viewport left top line %d, want 42", w.ViewState.ViewOffsetY)
	}
	if !w.ViewState.ScrollDetached {
		t.Error("scroll_viewport should park the view like the wheel does")
	}
}

// The bottom-row corner cut is a TERMINAL concern: the screen's bottom-right
// cell scrolls it. A graphical host drawing the bar in pixels has no such
// cell, so its track keeps the full height rather than leaving a gap at the
// bottom of every bar.
func TestScrollbarHostDrawnKeepsTheBottomRow(t *testing.T) {
	e, w, _ := newRenderedEditor(t, strings.Repeat("line\n", 300))
	if !e.setOption(w, "scrollbar", "true") {
		t.Fatal("set scrollbar=true failed")
	}
	e.performRender()
	if w.ContentY+w.ContentHeight != e.Renderer.Height {
		t.Skip("layout does not reach the bottom row; the corner is not in play")
	}
	if w.ScrollbarTrackH != w.ContentHeight-1 {
		t.Fatalf("terminal-drawn track = %d, want %d (the corner row given up)",
			w.ScrollbarTrackH, w.ContentHeight-1)
	}

	// Hand the bars to a host: the corner row comes back.
	e.Config.ScrollbarRegions = func([]ScrollbarRegion) {}
	e.performRender()
	if w.ScrollbarTrackH != w.ContentHeight {
		t.Fatalf("host-drawn track = %d, want the full %d", w.ScrollbarTrackH, w.ContentHeight)
	}
}

// The rule stated the way it is seen: scrolled as far as it goes, the
// line-number gutter shows exactly ONE "~" row — the single row past the end of
// the document — and never a screenful of them with a line or two of text
// stranded at the top.
//
// Counted from the PAINTED frame rather than from the arithmetic, because the
// gutter's own idea of where the document ends is the thing being claimed: the
// empty line after a file's final newline is numbered, not marked "~", so
// stopping a line earlier would show none at all.
func TestMaxScrollLeavesExactlyOneGutterTilde(t *testing.T) {
	e, w, out := newRenderedEditor(t, strings.Repeat("line\n", 200))
	w.ViewState.ShowLineNumbers = true
	e.performRender()

	marker := e.LoadedConfig.Indicators.GutterEmpty
	if marker == "" {
		t.Skip("no gutter-empty indicator configured")
	}
	countTildes := func() int { return strings.Count(out.String(), marker) }

	// At the top of a document far taller than the view, no row is past its end.
	out.Reset()
	e.scrollViewTo(w, 0)
	e.performRender()
	if got := countTildes(); got != 0 {
		t.Fatalf("%d gutter markers at the top of a long document, want 0", got)
	}

	// Scrolled as far as anything can ask: exactly one.
	out.Reset()
	e.scrollViewTo(w, 1000000)
	e.performRender()
	if got := countTildes(); got != 1 {
		t.Fatalf("%d gutter markers at maximum scroll, want exactly 1", got)
	}

	// And one line back up there are none, which is what makes the limit the
	// RIGHT one rather than merely a limit.
	out.Reset()
	e.scrollViewTo(w, w.ViewState.ViewOffsetY-1)
	e.performRender()
	if got := countTildes(); got != 0 {
		t.Fatalf("%d gutter markers one line above maximum scroll, want 0", got)
	}
}
