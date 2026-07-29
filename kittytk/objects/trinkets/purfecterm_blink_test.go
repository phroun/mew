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

	// Input bound for the child — a tinput, a mouse report, a paste.
	dark()
	term.SetInputSink(func([]byte) {})
	term.toChild([]byte("x"))
	if !lit() {
		t.Error("input to the child should restart the blink")
	}

	// A click, which need not send the child anything at all.
	dark()
	term.HandleMousePress(core.MousePressEvent{X: 40, Y: 40, Button: core.LeftButton})
	if !lit() {
		t.Error("a click should restart the blink")
	}
	term.HandleMouseRelease(core.MouseReleaseEvent{X: 40, Y: 40, Button: core.LeftButton})

	// A hosted terminal's caret is driven by what it is FED: its input never
	// passes through toChild, so this is tinput's only route to the blink.
	dark()
	term.Feed([]byte("hello"))
	if !lit() {
		t.Error("fed output should restart the blink for a hosted terminal")
	}

	// An untouched terminal keeps whatever phase it was in: the reset is for
	// action, not for merely existing.
	dark()
	if lit() {
		t.Error("nothing happened; the blink should not have restarted")
	}
}
