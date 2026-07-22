package editor

import (
	"fmt"
	"strings"
	"testing"

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
