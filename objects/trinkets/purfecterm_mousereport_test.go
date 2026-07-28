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

	// A drag while the left button is held: SGR motion report at cell 1,1.
	term.heldButton = core.LeftButton
	term.HandleMouseMove(core.MouseMoveEvent{X: 0, Y: 0})
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
	term.heldButton = core.LeftButton
	term.HandleMouseRelease(core.MouseReleaseEvent{X: 0, Y: 0, Button: core.LeftButton})
	if strings.Contains(got.String(), "\x1b[<") {
		t.Fatalf("no reports with tracking off; got %q", got.String())
	}
}
