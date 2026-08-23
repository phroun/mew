package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// Where text is being typed does not blink.
//
// The report used to sit inside the DRAWN cursor's test, blink phase and all,
// so a focused terminal told the platform "the insertion point is here" and
// "there is no insertion point" in alternation, twice a second. Nothing
// noticed until something downstream started acting on the change, and then
// macOS's press-and-hold accent palette closed a moment after it opened.
//
// The two questions are separate: whether to paint a cursor this frame, and
// where the text is. Only the first one blinks.
func TestInputAreaSurvivesTheCursorBlink(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	term := NewPurfecTerm()
	defer term.Close()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	term.SetBounds(core.UnitRect{Width: 400, Height: 200})
	term.SetFocus()
	if !term.HasFocus() {
		t.Skip("terminal did not take focus in this harness")
	}

	px, err := raster.New(400, 200)
	if err != nil {
		t.Fatalf("raster.New: %v", err)
	}

	// A headless paint forces the cursor steadily on when nothing is driving
	// the blink, so stand a timer up to hold the dark half in place. It is
	// never started; only its presence is read.
	term.gfx.blinkTimer = &DesktopTimer{}
	term.gfx.cursorBlinkOn = false

	p := core.NewPainter(px)
	p.ResetTextCaretRequest()
	term.Paint(p)
	caret := p.TextCaretRequest()

	if term.gfx.cursorBlinkOn {
		t.Fatal("the harness could not hold the cursor in its dark phase")
	}
	if !caret.InputArea {
		t.Error("a focused terminal reported no insertion point while its " +
			"cursor was in the dark half of a blink")
	}
	if caret.Visible {
		t.Error("the terminal asked a graphical surface to draw a platform caret")
	}
}
