package editor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/phroun/mew/internal/window"
)

// dragHarness renders an editor and returns cell->screen helpers plus a
// pseudo-key driver, shared by the drag/shift-click selection tests.
func dragHarness(t *testing.T, content string) (*Editor, *window.Window, func(key string)) {
	t.Helper()
	e, w, _ := newRenderedEditor(t, content)
	e.performRender() // establish geometry
	send := func(key string) {
		if !e.handleMouseKey(key) {
			t.Fatalf("pseudo-key %q should be consumed", key)
		}
	}
	return e, w, send
}

// col/row build 1-based screen coordinates for a document cell of w.
func screenAt(w *window.Window, line, cell int) (x, y int) {
	return w.ContentX + 1 + cell, w.ContentY + 1 + (line - w.ViewState.ViewOffsetY)
}

// A press-and-drag marks the block from the press origin, the end following
// the pointer cell by cell (caret too); release keeps the block. A click
// that never leaves its cell marks nothing.
func TestMouseDragMarksBlock(t *testing.T) {
	e, w, send := dragHarness(t, "aaaa\nbbbb\ncccc\n")

	// Press at (0,1), no drag, release: no block appears.
	x, y := screenAt(w, 0, 1)
	send(fmt.Sprintf("Mouse@%d,%d", x, y))
	send("MouseLeftPress")
	send("MouseLeftRelease")
	if w.Buffer.HasBlockMarks() {
		t.Fatal("a click without a drag must not mark a block")
	}

	// Press at (0,1), drag to (1,3): begin at origin, end at the drag cell.
	send(fmt.Sprintf("Mouse@%d,%d", x, y))
	send("MouseLeftPress")
	dx, dy := screenAt(w, 1, 3)
	send(fmt.Sprintf("MouseLeftDrag@%d,%d", dx, dy))
	if l, r := mark(t, w, "_block_begin"); l != 0 || r != 1 {
		t.Fatalf("_block_begin: (%d,%d), want (0,1)", l, r)
	}
	if l, r := mark(t, w, "_block_end"); l != 1 || r != 3 {
		t.Fatalf("_block_end: (%d,%d), want (1,3)", l, r)
	}
	if pos := w.CursorPos(); pos.Line != 1 || pos.Rune != 3 {
		t.Fatalf("caret should follow the drag: %+v", pos)
	}

	// Drag on to (2,2): only the end moves.
	dx2, dy2 := screenAt(w, 2, 2)
	send(fmt.Sprintf("MouseLeftDrag@%d,%d", dx2, dy2))
	if l, r := mark(t, w, "_block_begin"); l != 0 || r != 1 {
		t.Fatalf("_block_begin moved: (%d,%d)", l, r)
	}
	if l, r := mark(t, w, "_block_end"); l != 2 || r != 2 {
		t.Fatalf("_block_end after second drag: (%d,%d), want (2,2)", l, r)
	}

	// Release: the block stays.
	send("MouseLeftRelease")
	if l, r := mark(t, w, "_block_end"); l != 2 || r != 2 {
		t.Fatalf("block should survive release: end (%d,%d)", l, r)
	}
	if e.dragSel.active {
		t.Fatal("drag selection state should clear on release")
	}
}

// Shift+click extends from the caret's CURRENT document position — even one
// scrolled out of view — to the clicked cell; a continuing drag then moves
// only the end.
func TestMouseShiftClickExtends(t *testing.T) {
	// 40 lines so the caret can sit scrolled out of view.
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "line%02d\n", i)
	}
	e, w, send := dragHarness(t, b.String())

	// Caret parks at (0,2); the view scrolls to line 20 — the caret is
	// offscreen, but its DOCUMENT position anchors the selection.
	w.SetCursorPos(window.Position{Line: 0, Rune: 2})
	w.SetViewTop(20)
	e.performRender()

	x, y := screenAt(w, 22, 4)
	send(fmt.Sprintf("Mouse@%d,%d", x, y))
	send("S-MouseLeftPress")
	if l, r := mark(t, w, "_block_begin"); l != 0 || r != 2 {
		t.Fatalf("_block_begin should anchor at the OLD caret: (%d,%d), want (0,2)", l, r)
	}
	if l, r := mark(t, w, "_block_end"); l != 22 || r != 4 {
		t.Fatalf("_block_end: (%d,%d), want (22,4)", l, r)
	}
	if pos := w.CursorPos(); pos.Line != 22 || pos.Rune != 4 {
		t.Fatalf("caret should move to the shift-click: %+v", pos)
	}

	// A drag continuing from the shift+click moves only the end.
	dx, dy := screenAt(w, 23, 1)
	send(fmt.Sprintf("S-MouseLeftDrag@%d,%d", dx, dy))
	if l, r := mark(t, w, "_block_begin"); l != 0 || r != 2 {
		t.Fatalf("_block_begin must not move on the continuing drag: (%d,%d)", l, r)
	}
	if l, r := mark(t, w, "_block_end"); l != 23 || r != 1 {
		t.Fatalf("_block_end after continuing drag: (%d,%d), want (23,1)", l, r)
	}
	send("MouseLeftRelease")
}

// A held drag is CAPTURED: leaving the content area keeps adjusting the
// selection. The gutter resolves to the START of the row's line (drag into
// the line numbers to pin the selection to line beginnings), rows past the
// text clamp to the nearest text row, and the far edge clamps to the last
// visible cell (line end for a line that ends in view).
func TestMouseDragCapturedOutsideContent(t *testing.T) {
	e, w, send := dragHarness(t, "aaaa\nbbbb\ncccc\n")
	_ = e

	// Press mid-line-0, then drag into the GUTTER on row 1: the end pins to
	// the beginning of line 1.
	x, y := screenAt(w, 0, 2)
	send(fmt.Sprintf("Mouse@%d,%d", x, y))
	send("MouseLeftPress")
	_, gy := screenAt(w, 1, 0)
	send(fmt.Sprintf("MouseLeftDrag@%d,%d", 1, gy)) // x=1: over the line numbers
	if l, r := mark(t, w, "_block_end"); l != 1 || r != 0 {
		t.Fatalf("gutter drag should pin to line start: end (%d,%d), want (1,0)", l, r)
	}

	// Drag BELOW the last text row: clamps to the last line (still tracking).
	send(fmt.Sprintf("MouseLeftDrag@%d,%d", x, w.ContentY+w.ContentHeight+3))
	if l, _ := mark(t, w, "_block_end"); l != 3 {
		t.Fatalf("below-window drag should clamp to the last line: end line %d, want 3", l)
	}

	// Drag ABOVE the window (over the modebar): clamps to the first row.
	send(fmt.Sprintf("MouseLeftDrag@%d,%d", 1, 0))
	if l, r := mark(t, w, "_block_end"); l != 0 || r != 0 {
		t.Fatalf("above-window gutter drag should clamp to (0,0): end (%d,%d)", l, r)
	}

	// Drag far past the RIGHT edge on row 2: clamps to that line's end.
	_, ry := screenAt(w, 2, 0)
	send(fmt.Sprintf("MouseLeftDrag@%d,%d", 500, ry))
	if l, r := mark(t, w, "_block_end"); l != 2 || r != 4 {
		t.Fatalf("past-right drag should clamp to line end: end (%d,%d), want (2,4)", l, r)
	}
	send("MouseLeftRelease")

	// The begin anchor never moved through all of it.
	if l, r := mark(t, w, "_block_begin"); l != 0 || r != 2 {
		t.Fatalf("_block_begin drifted: (%d,%d), want (0,2)", l, r)
	}
}

// A press on the blank area BELOW the document's last line parks the caret
// at the end of the document, and dragging upward from there selects the
// document's tail.
func TestMousePressBelowDocSelectsFromEOF(t *testing.T) {
	e, w, send := dragHarness(t, "aaaa\nbbbb\ncccc\n")
	_ = e

	// The doc shows 4 lines (3 text + trailing empty); the window is taller.
	// Click two rows below the last line, still inside the content area.
	lineCount := w.Buffer.GetLineCount()
	x := w.ContentX + 3
	y := w.ContentY + 1 + lineCount + 1 // a blank row below the text
	if y > w.ContentY+w.ContentHeight {
		t.Fatalf("test setup: blank row %d outside the window", y)
	}
	send(fmt.Sprintf("Mouse@%d,%d", x, y))
	send("MouseLeftPress")
	// EOF: the trailing empty line (index 3), column 0.
	if pos := w.CursorPos(); pos.Line != 3 || pos.Rune != 0 {
		t.Fatalf("below-doc click should park at EOF: %+v", pos)
	}

	// Drag upward to (1,1): the tail of the document is selected.
	dx, dy := screenAt(w, 1, 1)
	send(fmt.Sprintf("MouseLeftDrag@%d,%d", dx, dy))
	send("MouseLeftRelease")
	if l, r := mark(t, w, "_block_begin"); l != 3 || r != 0 {
		t.Fatalf("_block_begin should anchor at EOF: (%d,%d), want (3,0)", l, r)
	}
	if l, r := mark(t, w, "_block_end"); l != 1 || r != 1 {
		t.Fatalf("_block_end after upward drag: (%d,%d), want (1,1)", l, r)
	}
	// The normalized range reads bottom-up correctly.
	sl, sr, el, er, ok := w.Buffer.GetBlockRange()
	if !ok || sl != 1 || sr != 1 || el != 3 || er != 0 {
		t.Fatalf("normalized range: (%d,%d)-(%d,%d) ok=%v", sl, sr, el, er, ok)
	}
}

// Drag-edge autoscroll: with the pointer held past the bottom edge, ticks
// scroll the view (speed from overshoot, after the delay gate) and keep
// extending the selection; horizontal ticks ride the scroll_right command.
func TestMouseDragAutoScrollTick(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "line%02d-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n", i)
	}
	e, w, send := dragHarness(t, b.String())

	// Start a drag and park the pointer below the window's bottom edge.
	x, y := screenAt(w, 0, 1)
	send(fmt.Sprintf("Mouse@%d,%d", x, y))
	send("MouseLeftPress")
	belowY := w.ContentY + w.ContentHeight + 2
	send(fmt.Sprintf("MouseLeftDrag@%d,%d", x, belowY))
	if e.dragScroll.vert != 2 {
		t.Fatalf("overshoot rows: %d, want 2", e.dragScroll.vert)
	}

	// Before the delay elapses, a tick does nothing (accident guard).
	topBefore := w.ViewState.ViewOffsetY
	e.dragScrollTick()
	if w.ViewState.ViewOffsetY != topBefore {
		t.Fatal("a tick inside the delay window must not scroll")
	}

	// Age the engagement past the delay: ticks now scroll by the overshoot
	// and extend the selection to the newly revealed rows.
	e.dragScroll.since = e.dragScroll.since.Add(-time.Second)
	e.dragScrollTick()
	if got := w.ViewState.ViewOffsetY; got != topBefore+2 {
		t.Fatalf("tick should scroll by the overshoot: top %d, want %d", got, topBefore+2)
	}
	endL, _ := mark(t, w, "_block_end")
	if endL <= 0 {
		t.Fatalf("tick should extend the selection downward: end line %d", endL)
	}
	e.dragScrollTick()
	if got := w.ViewState.ViewOffsetY; got != topBefore+4 {
		t.Fatalf("second tick should keep scrolling: top %d, want %d", got, topBefore+4)
	}

	// Park the pointer ON the far (right) column of a long line: horizontal
	// ticks ride scroll_right (8-column steps, the keyboard's own increment).
	send(fmt.Sprintf("MouseLeftDrag@%d,%d", w.ContentX+w.ContentWidth, w.ContentY+2))
	if e.dragScroll.horiz == 0 {
		t.Fatal("far-column park should engage horizontal overshoot")
	}
	e.dragScroll.since = e.dragScroll.since.Add(-time.Second)
	xBefore := w.ViewState.ViewOffsetX
	e.dragScrollTick()
	if got := w.ViewState.ViewOffsetX; got != xBefore+8 {
		t.Fatalf("horizontal tick should step by scroll_right's 8: %d, want %d", got, xBefore+8)
	}

	send("MouseLeftRelease")
	if e.dragScroll.stop != nil || e.dragScroll.vert != 0 {
		t.Fatal("release must stop and clear the autoscroll state")
	}
}

// M-/alt+left-click stands in for a right-click (a terminal may never
// deliver the real right button), and right-click works on the blank area
// below the document too — same as within it.
func TestMouseAltClickAndBelowDocContextMenu(t *testing.T) {
	e, w, send := dragHarness(t, "hello\nworld\n")
	var popped int
	e.Config.ShowContextMenu = func(col, row int) { popped++ }

	// Alt+left-click in the content area pops the menu and moves no caret.
	w.SetCursorPos(window.Position{Line: 1, Rune: 2})
	x, y := screenAt(w, 0, 1)
	send(fmt.Sprintf("Mouse@%d,%d", x, y))
	send("M-MouseLeftPress")
	if popped != 1 {
		t.Fatalf("alt+click should pop the context menu (popped=%d)", popped)
	}
	if pos := w.CursorPos(); pos.Line != 1 || pos.Rune != 2 {
		t.Fatalf("alt+click must not move the caret: %+v", pos)
	}

	// Right-click below the document's last line pops too.
	lineCount := w.Buffer.GetLineCount()
	by := w.ContentY + 1 + lineCount + 1
	send(fmt.Sprintf("Mouse@%d,%d", x, by))
	send("MouseRightPress")
	if popped != 2 {
		t.Fatalf("below-doc right-click should pop the menu (popped=%d)", popped)
	}

	// And alt+click below the doc as well.
	send(fmt.Sprintf("Mouse@%d,%d", x, by))
	send("M-MouseLeftPress")
	if popped != 3 {
		t.Fatalf("below-doc alt+click should pop the menu (popped=%d)", popped)
	}

	// Ctrl+click (and any other non-shift modifier, even combined with
	// shift) triggers the menu too — terminals vary in which modified
	// clicks they let through.
	send(fmt.Sprintf("Mouse@%d,%d", x, y))
	send("C-MouseLeftPress")
	if popped != 4 {
		t.Fatalf("ctrl+click should pop the menu (popped=%d)", popped)
	}
	send(fmt.Sprintf("Mouse@%d,%d", x, y))
	send("S-C-MouseLeftPress")
	if popped != 5 {
		t.Fatalf("shift+ctrl+click should still pop the menu (popped=%d)", popped)
	}
	// Plain shift+click stays a selection extension, never a menu.
	send(fmt.Sprintf("Mouse@%d,%d", x, y))
	send("S-MouseLeftPress")
	if popped != 5 {
		t.Fatalf("plain shift+click must not pop the menu (popped=%d)", popped)
	}
	send("MouseLeftRelease")
}

// Block provenance decides whether a plain click dissolves the block: a
// plain mouse DRAG makes a transient block (mouseBlock on -> the next plain
// click deletes the marks), while keyboard-set marks and shift+click
// selections are deliberate (mouseBlock off -> clicks leave them alone).
func TestMouseBlockDissolvesOnClick(t *testing.T) {
	e, w, send := dragHarness(t, "aaaa\nbbbb\ncccc\n")
	press := func(kind string, line, cell int) {
		x, y := screenAt(w, line, cell)
		send(fmt.Sprintf("Mouse@%d,%d", x, y))
		send(kind)
	}
	drag := func(line, cell int) {
		x, y := screenAt(w, line, cell)
		send(fmt.Sprintf("MouseLeftDrag@%d,%d", x, y))
	}

	// Plain drag: transient. The next plain click dissolves the block.
	press("MouseLeftPress", 0, 0)
	drag(1, 2)
	send("MouseLeftRelease")
	if !w.Buffer.MouseBlock() {
		t.Fatal("a plain drag selection must set the mouse-block flag")
	}
	press("MouseLeftPress", 2, 1)
	if w.Buffer.HasBlockMarks() {
		t.Fatal("a plain click must dissolve a mouse-dragged block")
	}
	if w.Buffer.MouseBlock() {
		t.Fatal("the flag must clear with the dissolved marks")
	}
	send("MouseLeftRelease")

	// Keyboard-set marks: deliberate. A plain click leaves them.
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand("set_block_begin")
	w.SetCursorPos(window.Position{Line: 1, Rune: 2})
	e.executeCommand("set_block_end")
	if w.Buffer.MouseBlock() {
		t.Fatal("keyboard-set marks must leave the mouse-block flag off")
	}
	press("MouseLeftPress", 2, 1)
	send("MouseLeftRelease")
	if !w.Buffer.HasBlockMarks() {
		t.Fatal("a plain click must NOT dissolve a keyboard-made block")
	}

	// A keyboard set_block_end ADJUSTING a mouse-dragged block also makes it
	// deliberate.
	press("MouseLeftPress", 0, 0)
	drag(1, 1)
	send("MouseLeftRelease")
	w.SetCursorPos(window.Position{Line: 2, Rune: 2})
	e.executeCommand("set_block_end")
	press("MouseLeftPress", 0, 3)
	send("MouseLeftRelease")
	if !w.Buffer.HasBlockMarks() {
		t.Fatal("a keyboard-adjusted block must survive a plain click")
	}

	// Shift+click: a DELIBERATE mouse selection — flag off, survives clicks —
	// including a drag that continues the shift gesture.
	w.Buffer.ClearBlockMarks()
	w.SetCursorPos(window.Position{Line: 0, Rune: 1})
	press("S-MouseLeftPress", 1, 3)
	if w.Buffer.MouseBlock() {
		t.Fatal("shift+click must leave the mouse-block flag OFF")
	}
	x, y := screenAt(w, 2, 2)
	send(fmt.Sprintf("S-MouseLeftDrag@%d,%d", x, y))
	if w.Buffer.MouseBlock() {
		t.Fatal("a drag continuing a shift+click must keep the flag OFF")
	}
	send("MouseLeftRelease")
	press("MouseLeftPress", 0, 0)
	send("MouseLeftRelease")
	if !w.Buffer.HasBlockMarks() {
		t.Fatal("a shift+click selection must survive a plain click")
	}
}
