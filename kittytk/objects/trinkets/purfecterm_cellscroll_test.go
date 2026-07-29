package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

// On a CELL surface the scrollbar lanes are reserved whole cells (see
// updateTerminalSize) — and they must be INTERACTIVE, not just painted: a
// press in the lane belongs to the scrollbar, never to text selection.
// This runs in the default build (no graphical frames), which is exactly
// the TUI input situation.
func TestCellSurfaceScrollbarInteracts(t *testing.T) {
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	defer term.Close()

	// 40x12 cells at the default 8x16 metrics.
	term.SetBounds(core.UnitRect{Width: 320, Height: 192})
	buf := term.Terminal().Buffer()

	// Build scrollback so the vertical lane exists.
	term.Feed([]byte(strings.Repeat("line\r\n", 200)))
	if buf.GetMaxScrollOffset() == 0 {
		t.Fatal("no scrollback accumulated; the lane under test never appears")
	}

	// The vertical lane is the rightmost cell column. A press near the TOP
	// of its track jumps the view deep into history (thumb-center-to-pointer),
	// and must be consumed by the scrollbar — not start a selection.
	laneX := core.Unit(316)
	if !term.HandleMousePress(core.MousePressEvent{X: laneX, Y: 1, Button: core.LeftButton}) {
		t.Fatal("press in the scrollbar lane was not consumed")
	}
	if buf.GetScrollOffset() == 0 {
		t.Fatal("track press did not scroll into history — the lane is painted but dead")
	}
	if buf.HasSelection() {
		t.Fatal("press in the scrollbar lane started a text selection")
	}

	// Dragging the thumb to the bottom of the track returns to the present.
	term.HandleMouseMove(core.MouseMoveEvent{X: laneX, Y: 191, Buttons: core.LeftButton})
	term.HandleMouseRelease(core.MouseReleaseEvent{X: laneX, Y: 191, Button: core.LeftButton})
	if off := buf.GetScrollOffset(); off != 0 {
		t.Fatalf("drag to track bottom left scroll offset %d, want 0", off)
	}

	// After release, a press in the CONTENT area behaves normally (the
	// drag state did not leak).
	term.HandleMousePress(core.MousePressEvent{X: 40, Y: 40, Button: core.LeftButton})
	term.HandleMouseRelease(core.MouseReleaseEvent{X: 40, Y: 40, Button: core.LeftButton})
	if buf.GetScrollOffset() != 0 {
		t.Fatal("content press moved the scroll position")
	}
}
