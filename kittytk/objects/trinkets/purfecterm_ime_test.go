package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// paintTerminalCaretRequest paints a focused terminal onto a graphical
// surface and returns the platform-caret request the frame produced.
func paintTerminalCaretRequest(t *testing.T) core.TextCaret {
	t.Helper()
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	px, err := raster.New(400, 200)
	if err != nil {
		t.Fatalf("raster.New: %v", err)
	}
	term := NewPurfecTerm()
	term.SetBounds(core.UnitRect{Width: 400, Height: 200})
	term.SetFocus()
	if !term.HasFocus() {
		t.Skip("terminal did not take focus in this harness")
	}

	p := core.NewPainter(px)
	p.ResetTextCaretRequest()
	term.Paint(p)
	return p.TextCaretRequest()
}

// On a GRAPHICAL surface the terminal must report where the text is
// WITHOUT asking for a platform caret.
//
// There is no outer terminal to draw one — a window cannot — so the
// drawn-caret request was a dead end there, and because it was the only
// request the terminal made, the OS was never told where typing happens
// and the input method put its candidate window in a corner. The
// terminal draws its own caret; only the position needed reporting.
func TestTerminalReportsInputAreaOnGraphicalSurface(t *testing.T) {
	caret := paintTerminalCaretRequest(t)

	if !caret.Requested() {
		t.Fatal("a focused terminal asked for nothing; an input method has " +
			"nowhere to anchor and falls back to a corner of the window")
	}
	if !caret.InputArea {
		t.Error("the request does not mark an insertion point")
	}
	if caret.Visible {
		t.Error("the terminal asked a graphical surface to DRAW a platform caret; " +
			"nothing there draws one, and it paints its own")
	}
}

// The DECSCUSR round trip still matters on cell surfaces, where the
// outer terminal draws the caret in that shape — a bar stays a bar,
// which no character grid can represent. That path is what
// RequestTextCaret is still for.
func TestTerminalKeepsDECSCUSRForCellSurfaces(t *testing.T) {
	for _, tc := range []struct {
		shape, blink, want int
	}{
		{0, 1, 1}, {0, 0, 2}, // block
		{1, 1, 3}, {1, 0, 4}, // underline
		{2, 1, 5}, {2, 0, 6}, // bar
	} {
		if got := decscusrFor(tc.shape, tc.blink); got != tc.want {
			t.Errorf("shape %d blink %d -> %d, want %d", tc.shape, tc.blink, got, tc.want)
		}
	}
}
