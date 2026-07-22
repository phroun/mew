package editor

import (
	"fmt"
	"testing"

	"github.com/phroun/mew/internal/window"
)

// A right-click asks the host to pop its context menu ONLY within the
// editing area of the focused window: content cells pop (with the clicked
// cell passed through), the line-number gutter and other windows (the
// modebar) do not, and the caret never moves.
func TestMouseRightPressGatesOnEditingArea(t *testing.T) {
	e, w, _ := newRenderedEditor(t, "hello\nworld\n")
	w.ViewState.ShowLineNumbers = true
	e.performRender() // establish geometry (ContentX/Y, gutter width)

	var popped []string
	e.Config.ShowContextMenu = func(col, row int) {
		popped = append(popped, fmt.Sprintf("%d,%d", col, row))
	}

	rightClick := func(x, y int) {
		if !e.handleMouseKey(fmt.Sprintf("Mouse@%d,%d", x, y)) {
			t.Fatal("position pseudo-key should be consumed")
		}
		if !e.handleMouseKey("MouseRightPress") {
			t.Fatal("right-press pseudo-key should be consumed")
		}
	}

	w.SetCursorPos(window.Position{Line: 1, Rune: 2})
	caretBefore := w.CursorPos()

	// In the content area: pops, with the clicked cell.
	contentX := w.ContentX + 2 // a content cell, 1-based
	contentY := w.ContentY + 1 // buffer line 0, 1-based row
	rightClick(contentX, contentY)
	if len(popped) != 1 || popped[0] != fmt.Sprintf("%d,%d", contentX, contentY) {
		t.Fatalf("content-area right-click should pop at the cell: %v", popped)
	}
	if got := w.CursorPos(); got != caretBefore {
		t.Fatalf("right-click must not move the caret: %+v", got)
	}

	// On the line-number gutter: no pop.
	rightClick(1, contentY)
	if len(popped) != 1 {
		t.Fatalf("gutter right-click must not pop: %v", popped)
	}

	// On the modebar (a different window than the focused doc): no pop.
	modebarRow := 0
	for _, mw := range e.WindowManager.AllWindows() {
		if mw.Class == "modebar" {
			modebarRow = mw.ContentY + 1
		}
	}
	if modebarRow == contentY {
		t.Fatalf("test setup: modebar row %d collides with content row", modebarRow)
	}
	rightClick(w.ContentX+2, modebarRow)
	if len(popped) != 1 {
		t.Fatalf("modebar right-click must not pop: %v", popped)
	}

	// Below every window (past the buffer's lines): no pop.
	rightClick(contentX, w.ContentY+w.ContentHeight+1)
	if len(popped) != 1 {
		t.Fatalf("out-of-window right-click must not pop: %v", popped)
	}
}

// Without a host menu seam the right press is swallowed harmlessly.
func TestMouseRightPressUnwired(t *testing.T) {
	e, w, _ := newRenderedEditor(t, "hello\n")
	e.performRender()
	if !e.handleMouseKey(fmt.Sprintf("Mouse@%d,%d", w.ContentX+1, w.ContentY+1)) {
		t.Fatal("position pseudo-key should be consumed")
	}
	if !e.handleMouseKey("MouseRightPress") {
		t.Fatal("right press should be consumed even with no menu seam")
	}
}
