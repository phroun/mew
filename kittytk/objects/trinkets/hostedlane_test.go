package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

// The hosted terminal's own scrollbar lane must report the arrow. It is not
// in editor mode, so its lanes are live once there is scrollback.
func TestHostedTerminalLaneReportsArrow(t *testing.T) {
	term := NewPurfecTerm()
	defer term.Close()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	term.SetBounds(core.UnitRect{Width: 320, Height: 192}) // 40x12 at 8x16
	term.Feed([]byte(strings.Repeat("line\r\n", 200)))
	if term.Terminal().Buffer().GetMaxScrollOffset() == 0 {
		t.Fatal("no scrollback; the lane under test never appears")
	}
	if !term.scrollLanesActive() {
		t.Fatal("a hosted (non-editor-mode) terminal should have live lanes")
	}
	// Over the content: the text I-beam.
	if got := term.CursorShapeAt(40, 40); got != core.CursorText {
		t.Errorf("over terminal content: cursor %v, want I-beam", got)
	}
	// Over the vertical lane at the right edge: the arrow.
	if got := term.CursorShapeAt(317, 40); got != core.CursorDefault {
		t.Errorf("over the scrollbar lane: cursor %v, want arrow", got)
	}
}
