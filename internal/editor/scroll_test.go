package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/window"
)

// A free scroll (mouse wheel or scroll_* command) detaches the viewport from
// the caret: the per-frame follow leaves it parked even with the caret
// off-screen, until a cursor-movement command re-engages following.
func TestScrollDetachesUntilCursorMoves(t *testing.T) {
	e, w := newTestEditor(t, strings.Repeat("x\n", 100))
	w.ContentHeight = 20
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	w.SetViewTop(0)

	// Wheel-scroll ten lines down: the caret stays on line 0 (now off-screen).
	e.scrollViewByLines(w, 10)
	if w.ViewState.ViewOffsetY != 10 {
		t.Fatalf("scroll should park the view at 10, got %d", w.ViewState.ViewOffsetY)
	}
	if !w.ViewState.ScrollDetached {
		t.Fatal("a wheel scroll should detach the view from the caret")
	}
	if w.CursorPos().Line != 0 {
		t.Fatalf("scrolling must not move the caret, got line %d", w.CursorPos().Line)
	}

	// The per-frame follow must honor the detached scroll: the view stays put.
	e.renderFollowCaret(w)
	if w.ViewState.ViewOffsetY != 10 {
		t.Fatalf("render follow must not snap a detached view back, got %d", w.ViewState.ViewOffsetY)
	}

	// A cursor movement re-engages following: the view snaps back onto the caret.
	e.PawScript.ExecuteAsync("go_line_next")
	if w.ViewState.ScrollDetached {
		t.Fatal("a cursor movement must re-engage caret following")
	}
	if w.ViewState.ViewOffsetY != 1 {
		t.Fatalf("after re-engaging, the view should follow the caret to line 1, got %d", w.ViewState.ViewOffsetY)
	}
}

// Without a detached scroll, the per-frame follow snaps the caret back into
// view as before — the flag is the only thing that suspends it.
func TestRenderFollowStillSnapsWhenAttached(t *testing.T) {
	e, w := newTestEditor(t, strings.Repeat("x\n", 100))
	w.ContentHeight = 20
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	w.SetViewTop(10) // moved away, but NOT via a detaching scroll

	e.renderFollowCaret(w)
	if w.ViewState.ViewOffsetY != 0 {
		t.Fatalf("an attached view should follow the caret back to 0, got %d", w.ViewState.ViewOffsetY)
	}
}

// An edit also re-engages caret following.
func TestEditReengagesFollow(t *testing.T) {
	e, w := newTestEditor(t, strings.Repeat("x\n", 100))
	w.ContentHeight = 20
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.scrollViewByLines(w, 10)
	if !w.ViewState.ScrollDetached {
		t.Fatal("precondition: the view should be detached")
	}
	e.PawScript.ExecuteAsync(`insert "z"`)
	if w.ViewState.ScrollDetached {
		t.Fatal("an edit must re-engage caret following")
	}
}

// The scroll_* command family parks the viewport without moving the caret,
// mirroring the go_* movement family.
func TestScrollCommands(t *testing.T) {
	e, w := newTestEditor(t, strings.Repeat("x\n", 100))
	w.ContentHeight = 20
	lineCount := w.Buffer.GetLineCount()

	reset := func() {
		w.SetCursorPos(window.Position{Line: 0, Rune: 0})
		w.SetViewTop(0)
		w.ViewState.ScrollDetached = false
	}
	caretLine := func() int { return w.CursorPos().Line }

	cases := []struct {
		cmd     string
		wantTop int
	}{
		{"scroll_line_next", 1},
		{"scroll_page_next", 19},              // default page "100%-1" on 20 rows
		{"scroll_buffer_end", lineCount - 20}, // last line on the bottom row
		{"scroll_line 50", 49},                // 1-based line 50 to the top
	}
	for _, tc := range cases {
		reset()
		e.PawScript.ExecuteAsync(tc.cmd)
		if w.ViewState.ViewOffsetY != tc.wantTop {
			t.Errorf("%q: view top = %d, want %d", tc.cmd, w.ViewState.ViewOffsetY, tc.wantTop)
		}
		if caretLine() != 0 {
			t.Errorf("%q: must not move the caret, got line %d", tc.cmd, caretLine())
		}
		if !w.ViewState.ScrollDetached {
			t.Errorf("%q: should detach the view from the caret", tc.cmd)
		}
	}

	// scroll_buffer_beg / scroll_line_prior return toward the top.
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	w.SetViewTop(40)
	e.PawScript.ExecuteAsync("scroll_line_prior")
	if w.ViewState.ViewOffsetY != 39 {
		t.Errorf("scroll_line_prior: view top = %d, want 39", w.ViewState.ViewOffsetY)
	}
	e.PawScript.ExecuteAsync("scroll_buffer_beg")
	if w.ViewState.ViewOffsetY != 0 {
		t.Errorf("scroll_buffer_beg: view top = %d, want 0", w.ViewState.ViewOffsetY)
	}
	if caretLine() != 0 {
		t.Errorf("scroll commands must not move the caret, got line %d", caretLine())
	}
}
