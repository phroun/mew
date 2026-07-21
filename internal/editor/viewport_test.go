package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/window"
)

// The viewport top is anchored to a garland cursor, so an edit that inserts or
// removes lines ABOVE the top slides the anchor and the view stays pinned to
// the same logical line (rather than the same raw line number).
func TestViewportPinsAcrossEditAbove(t *testing.T) {
	e, w := newTestEditor(t, strings.Repeat("x\n", 100))
	_ = e

	w.SetViewTop(50)
	if w.ViewState.ViewOffsetY != 50 {
		t.Fatalf("initial top: %d, want 50", w.ViewState.ViewOffsetY)
	}

	// Insert three lines at the very top (as a sibling window/edit would).
	w.Buffer.InsertLine(0, "a")
	w.Buffer.InsertLine(0, "b")
	w.Buffer.InsertLine(0, "c")

	w.RefreshViewTop()
	if w.ViewState.ViewOffsetY != 53 {
		t.Fatalf("top after inserting 3 lines above: %d, want 53", w.ViewState.ViewOffsetY)
	}

	// Delete two lines above the top: the view slides back up with them.
	w.Buffer.DeleteLine(0)
	w.Buffer.DeleteLine(0)
	w.RefreshViewTop()
	if w.ViewState.ViewOffsetY != 51 {
		t.Fatalf("top after deleting 2 lines above: %d, want 51", w.ViewState.ViewOffsetY)
	}
}

// An edit at or below the viewport top does not move it.
func TestViewportUnaffectedByEditBelow(t *testing.T) {
	_, w := newTestEditor(t, strings.Repeat("x\n", 100))
	w.SetViewTop(50)

	w.Buffer.InsertLine(70, "below") // well below the top
	w.RefreshViewTop()
	if w.ViewState.ViewOffsetY != 50 {
		t.Fatalf("top should be unchanged by an edit below: %d, want 50", w.ViewState.ViewOffsetY)
	}
}

// With no edits, RefreshViewTop is a no-op (anchor and offset agree).
func TestViewportStableWithoutEdits(t *testing.T) {
	_, w := newTestEditor(t, strings.Repeat("x\n", 100))
	w.SetViewTop(42)
	w.RefreshViewTop()
	if w.ViewState.ViewOffsetY != 42 {
		t.Fatalf("top drifted without edits: %d, want 42", w.ViewState.ViewOffsetY)
	}
}

// Two windows on the SAME buffer scroll independently: each has its own anchor.
func TestTwoWindowsIndependentViewports(t *testing.T) {
	e, w1 := newTestEditor(t, strings.Repeat("x\n", 100))

	id := e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "doc2", Type: window.DocWindow, Dock: window.DockNone,
		Buffer: w1.Buffer, SetFocus: false,
	})
	w2 := e.WindowManager.GetWindow(id)

	w1.SetViewTop(10)
	w2.SetViewTop(60)
	if w1.ViewState.ViewOffsetY != 10 || w2.ViewState.ViewOffsetY != 60 {
		t.Fatalf("independent tops: w1=%d w2=%d", w1.ViewState.ViewOffsetY, w2.ViewState.ViewOffsetY)
	}

	// An edit above both tops slides both anchors by the same amount.
	w1.Buffer.InsertLine(0, "top")
	w1.RefreshViewTop()
	w2.RefreshViewTop()
	if w1.ViewState.ViewOffsetY != 11 || w2.ViewState.ViewOffsetY != 61 {
		t.Fatalf("after insert above: w1=%d (want 11) w2=%d (want 61)",
			w1.ViewState.ViewOffsetY, w2.ViewState.ViewOffsetY)
	}
}

// window_clone opens a second window on the SAME buffer. Editing through one
// window's caret slides the other window's caret (both are live garland
// cursors), so switching between them lands on the right line — the payoff of
// per-window caret cursors.
func TestWindowCloneSharedBufferCaret(t *testing.T) {
	e, w1 := newTestEditor(t, "aaa\nbbb\nccc\nddd\n")
	w1.SetCursorPos(window.Position{Line: 3, Rune: 0}) // on "ddd"

	if !e.cloneCurrentWindow() {
		t.Fatal("window_clone failed")
	}
	w2 := e.WindowManager.GetFocusedWindow()
	if w2 == nil || w2.ID == w1.ID {
		t.Fatal("clone should be a new, focused window")
	}
	if w2.Buffer != w1.Buffer {
		t.Fatal("clone must share the same buffer")
	}
	if w2.CursorPos().Line != 3 {
		t.Fatalf("clone should start at the source caret line 3, got %d", w2.CursorPos().Line)
	}

	// Move the clone's caret up and insert two lines at the top through it.
	w2.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.insertText("X\nY\n") // clone now edits: two lines inserted above w1's caret

	// Switch focus back to w1: its caret must have slid down by 2 to stay on
	// "ddd" (now line 5), not remain at the stale line 3.
	e.WindowManager.SetFocus(w1.ID)
	if w1.CursorPos().Line != 5 {
		t.Fatalf("source caret should have slid to line 5, got %d", w1.CursorPos().Line)
	}
	if got := strings.TrimRight(w1.Buffer.GetLine(w1.CursorPos().Line), "\n\r"); got != "ddd" {
		t.Fatalf("source caret should still be on ddd, got %q", got)
	}
}

// go_page_prior / go_page_next must move even near the edges, where the target
// line is out of range (garland rejects an out-of-range seek, so the setter
// clamps the line to the edge instead of leaving the caret put).
func TestPageUpDownClampAtEdges(t *testing.T) {
	e, w := newTestEditor(t, strings.Repeat("x\n", 200))
	w.ContentHeight = 20

	// Start a few lines from the top: page up must reach line 0, not no-op.
	w.SetCursorPos(window.Position{Line: 5, Rune: 0})
	e.PawScript.ExecuteAsync("go_page_prior")
	if w.CursorPos().Line != 0 {
		t.Fatalf("page up from line 5 should reach 0, got %d", w.CursorPos().Line)
	}

	// A page down moves down by ~a page.
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.PawScript.ExecuteAsync("go_page_next")
	if w.CursorPos().Line == 0 {
		t.Fatal("page down did not move")
	}
	moved := w.CursorPos().Line

	// Page down near the bottom clamps to the last line, not no-op.
	last := w.Buffer.GetLineCount() - 1
	w.SetCursorPos(window.Position{Line: last - 4, Rune: 0})
	e.PawScript.ExecuteAsync("go_page_next")
	if w.CursorPos().Line != last {
		t.Fatalf("page down near bottom should reach last line %d, got %d", last, w.CursorPos().Line)
	}
	_ = moved
}

// A buffer smaller than a page: page down still reaches the last line and
// page up still reaches the first (previously both no-op'd entirely).
func TestPageInSmallBuffer(t *testing.T) {
	e, w := newTestEditor(t, "a\nb\nc\n") // 4 lines (incl. trailing empty)
	w.ContentHeight = 20

	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.PawScript.ExecuteAsync("go_page_next")
	if w.CursorPos().Line != w.Buffer.GetLineCount()-1 {
		t.Fatalf("page down in small buffer should reach last line %d, got %d",
			w.Buffer.GetLineCount()-1, w.CursorPos().Line)
	}
	e.PawScript.ExecuteAsync("go_page_prior")
	if w.CursorPos().Line != 0 {
		t.Fatalf("page up in small buffer should reach line 0, got %d", w.CursorPos().Line)
	}
}

// Paging moves the viewport with the caret so relative screen position is
// preserved, with a one-blank-line cap at the end and no upward rewind.
func TestPageMovesViewport(t *testing.T) {
	e, w := newTestEditor(t, strings.Repeat("x\n", 100)) // 101 lines (0..100)
	w.ContentHeight = 20

	// Caret at line 5, viewport at top: page down moves both by a page.
	w.SetCursorPos(window.Position{Line: 5, Rune: 0})
	w.SetViewTop(0)
	e.PawScript.ExecuteAsync("go_page_next")
	// Default pageSize "100%-1" on a 20-row view moves 19 (one overlap line).
	if w.ViewState.ViewOffsetY != 19 {
		t.Fatalf("page down: viewport should move to 19, got %d", w.ViewState.ViewOffsetY)
	}
	// Caret kept its relative row: was row 5 (line5-top0), still row 5.
	if w.CursorPos().Line-w.ViewState.ViewOffsetY != 5 {
		t.Fatalf("caret relative row changed: caret %d top %d",
			w.CursorPos().Line, w.ViewState.ViewOffsetY)
	}

	// Page up brings both back up by a page.
	e.PawScript.ExecuteAsync("go_page_prior")
	if w.ViewState.ViewOffsetY != 0 {
		t.Fatalf("page up: viewport should return to 0, got %d", w.ViewState.ViewOffsetY)
	}
}

func TestPageDownCapsOneBlankLineAtEnd(t *testing.T) {
	e, w := newTestEditor(t, strings.Repeat("x\n", 100)) // 101 lines, last index 100
	w.ContentHeight = 20
	// maxTop = lineCount - height + 1 = 101 - 20 + 1 = 82. Top 82 shows lines
	// 82..101; line 100 (last) at row 18, row 19 shows line 101 = one blank.
	w.SetCursorPos(window.Position{Line: 90, Rune: 0})
	w.SetViewTop(75)
	e.PawScript.ExecuteAsync("go_page_next")
	if w.ViewState.ViewOffsetY != 82 {
		t.Fatalf("viewport should cap at 82 (one blank line), got %d", w.ViewState.ViewOffsetY)
	}
}

func TestPageDownNoRewindWhenDeeper(t *testing.T) {
	e, w := newTestEditor(t, strings.Repeat("x\n", 100))
	w.ContentHeight = 20
	// Viewport already at the cap (deeper than a page-down would compute from a
	// low caret): must not rewind upward.
	w.SetCursorPos(window.Position{Line: 95, Rune: 0})
	w.SetViewTop(82)
	e.PawScript.ExecuteAsync("go_page_next")
	if w.ViewState.ViewOffsetY < 82 {
		t.Fatalf("viewport must not rewind up, got %d (was 82)", w.ViewState.ViewOffsetY)
	}
	// And the caret (clamped to last line 100) is visible.
	if w.CursorPos().Line < w.ViewState.ViewOffsetY ||
		w.CursorPos().Line >= w.ViewState.ViewOffsetY+w.ContentHeight {
		t.Fatalf("caret %d not visible in [%d,%d)", w.CursorPos().Line,
			w.ViewState.ViewOffsetY, w.ViewState.ViewOffsetY+w.ContentHeight)
	}
}

func TestPageDownSmallBufferNoScroll(t *testing.T) {
	e, w := newTestEditor(t, "a\nb\nc\n")
	w.ContentHeight = 20
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	w.SetViewTop(0)
	e.PawScript.ExecuteAsync("go_page_next")
	if w.ViewState.ViewOffsetY != 0 {
		t.Fatalf("small buffer: viewport should stay at 0, got %d", w.ViewState.ViewOffsetY)
	}
}
