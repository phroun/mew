package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A caret in its dark half when the user acts reads as a hang, so every path
// that carries user action restarts the blink phase — the same contract the
// editor and the key-input trinket honour.
func TestCaretBlinkRestartsOnUserAction(t *testing.T) {
	term := NewPurfecTerm()
	defer term.Close()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	term.SetBounds(core.UnitRect{Width: 320, Height: 192})

	dark := func() { term.gfx.cursorBlinkOn = false; term.gfx.blinkTick = 7 }
	lit := func() bool { return term.gfx.cursorBlinkOn && term.gfx.blinkTick == 0 }

	// Input bound for the child — a tinput, a paste, an encoded key.
	dark()
	term.SetInputSink(func([]byte) {})
	term.toChild([]byte("x"))
	if !lit() {
		t.Error("input to the child should restart the blink")
	}

	// Mouse MOTION sent onward is NOT user action for this purpose. An
	// editor surface encodes pointer movement and forwards it exactly as a
	// terminal does, so waking here means the caret never blinks while the
	// mouse is moving — which is the bug this filter exists for.
	dark()
	term.toChild([]byte("\x1b[<35;10;5M"))
	if lit() {
		t.Error("forwarded mouse motion must not restart the blink")
	}

	// A forwarded press still counts: that is a deliberate act.
	dark()
	term.toChild([]byte("\x1b[<0;10;5M"))
	if !lit() {
		t.Error("a forwarded mouse press should restart the blink")
	}

	// A click, which need not send the child anything at all.
	dark()
	term.HandleMousePress(core.MousePressEvent{X: 40, Y: 40, Button: core.LeftButton})
	if !lit() {
		t.Error("a click should restart the blink")
	}
	term.HandleMouseRelease(core.MouseReleaseEvent{X: 40, Y: 40, Button: core.LeftButton})

	// Fed bytes are NOT user action. A host re-renders for its own reasons —
	// a mouse move is enough — so waking the caret here would hold it
	// permanently lit and it would never blink at all.
	dark()
	term.Feed([]byte("hello"))
	if lit() {
		t.Error("fed output must not restart the blink; the caret would never blink")
	}

	// An untouched terminal keeps whatever phase it was in: the reset is for
	// action, not for merely existing.
	dark()
	if lit() {
		t.Error("nothing happened; the blink should not have restarted")
	}
}
