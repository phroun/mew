package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

// With the hosted app tracking the mouse (DECSET through Feed, as mew does),
// the trinket relays ENCODED reports straight to the input sink — the
// embedded cli.Terminal's own mouse path writes to its absent PTY, so the
// trinket owns this relay. With tracking off, nothing reaches the sink.
func TestMouseReportRelay(t *testing.T) {
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("no embedded terminal")
	}
	var got strings.Builder
	term.SetInputSink(func(b []byte) { got.Write(b) })

	// The hosted app enables tracking (mew's exact trio).
	term.Feed([]byte("\x1b[?1000h\x1b[?1002h\x1b[?1006h"))
	if mode, enc := term.mouseTracking(); mode != 1002 || enc != 1006 {
		t.Fatalf("tracking modes = %d/%d, want 1002/1006", mode, enc)
	}

	// A press at cell 1,1 relays an SGR press report — and marks the press
	// as SEEN, which is what licenses the drag relay: the input path only
	// reports drags for presses it witnessed (the implicit-grab rule), so a
	// stray move with a button held from elsewhere is not the app's business.
	term.HandleMousePress(core.MousePressEvent{X: 0, Y: 0, Button: core.LeftButton})
	if !strings.Contains(got.String(), "\x1b[<0;1;1M") {
		t.Fatalf("press should relay an SGR press report; got %q", got.String())
	}

	// The drag: SGR motion report at cell 1,1.
	term.HandleMouseMove(core.MouseMoveEvent{X: 0, Y: 0, Buttons: core.LeftButton})
	if !strings.Contains(got.String(), "\x1b[<32;1;1M") {
		t.Fatalf("drag should relay an SGR motion report; got %q", got.String())
	}

	// A release: SGR release report (lowercase m terminator).
	term.HandleMouseRelease(core.MouseReleaseEvent{X: 0, Y: 0, Button: core.LeftButton})
	if !strings.Contains(got.String(), "\x1b[<0;1;1m") {
		t.Fatalf("release should relay an SGR release report; got %q", got.String())
	}

	// The app turns tracking off: nothing further reaches the sink.
	term.Feed([]byte("\x1b[?1002l\x1b[?1000l"))
	got.Reset()
	term.HandleMousePress(core.MousePressEvent{X: 0, Y: 0, Button: core.LeftButton})
	term.HandleMouseRelease(core.MouseReleaseEvent{X: 0, Y: 0, Button: core.LeftButton})
	if strings.Contains(got.String(), "\x1b[<") {
		t.Fatalf("no reports with tracking off; got %q", got.String())
	}
}

// A drag reports the button actually held.
//
// SGR motion sets bit 32 on the button code, so a left drag is 32, a middle
// drag 33 and a right drag 34. The relay reported 32 for all three, telling a
// guest every drag was a left drag — and a guest that distinguishes them
// (paste on middle, a right-drag selection) acted on the wrong one.
func TestDragReportsTheButtonActuallyHeld(t *testing.T) {
	for _, c := range []struct {
		btn  core.MouseButton
		want string
		what string
	}{
		{core.LeftButton, "\x1b[<32;1;1M", "left drag"},
		{core.MiddleButton, "\x1b[<33;1;1M", "middle drag"},
		{core.RightButton, "\x1b[<34;1;1M", "right drag"},
	} {
		term := NewPurfecTerm()
		if term.Terminal() == nil {
			t.Skip("no embedded terminal")
		}
		var got strings.Builder
		term.SetInputSink(func(b []byte) { got.Write(b) })
		term.Feed([]byte("\x1b[?1003h\x1b[?1006h"))

		// The press is what the drag relay is licensed by, and what records
		// which button is down.
		term.HandleMousePress(core.MousePressEvent{X: 0, Y: 0, Button: c.btn})
		got.Reset()
		term.HandleMouseMove(core.MouseMoveEvent{X: 0, Y: 0, Buttons: c.btn})

		if !strings.Contains(got.String(), c.want) {
			t.Errorf("%s relayed %q, want it to contain %q", c.what, got.String(), c.want)
		}
	}
}
