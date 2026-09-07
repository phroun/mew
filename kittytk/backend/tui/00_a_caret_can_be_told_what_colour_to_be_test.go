package tui

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// A terminal's caret colour is one global preference, chosen once against the
// terminal's own background. A trinket that paints a background of its own is
// the only thing that knows a thin caret in that preference would vanish into
// what it painted, so it can say what to draw instead -- and hand the reader's
// own colour back when it no longer has an opinion.
func TestACaretCanBeToldWhatColourToBe(t *testing.T) {
	b, out := newTestTUI(20, 2)
	s := style.DefaultStyle()

	// Nothing asked, so nothing is said: the caret keeps the colour the reader
	// chose, and no escape goes out about it.
	b.BeginFrame()
	b.DrawText(0, 0, "hello", s, nil)
	b.SetCursorPosition(core.Unit(cellX(b, 3)), 0)
	b.SetCursorVisible(true)
	b.EndFrame()
	if got := out.String(); strings.Contains(got, "\033]12;") || strings.Contains(got, "\033]112") {
		t.Errorf("an unasked caret colour reached the terminal: %q", got)
	}

	// Asked for one, it goes out as OSC 12 in hex, beside the cursor it
	// belongs to.
	out.Reset()
	b.BeginFrame()
	b.DrawText(0, 0, "world", s, nil)
	b.SetCursorColor(style.RGB(0xFF, 0xEE, 0x11))
	b.SetCursorPosition(core.Unit(cellX(b, 4)), 0)
	b.SetCursorVisible(true)
	b.EndFrame()
	frame := out.String()
	if !strings.Contains(frame, "\033]12;#FFEE11") {
		t.Errorf("the caret colour did not reach the terminal: %q", frame)
	}
	// It is written where the cursor is addressed, and before it is revealed:
	// a colour arriving after the reveal repaints a caret already on screen.
	if show := strings.Index(frame, "\033[?25h"); show >= 0 {
		if at := strings.Index(frame, "\033]12;"); at > show {
			t.Errorf("the colour landed after the reveal: %q", frame)
		}
	}

	// Asked again for the same, nothing more goes out.
	out.Reset()
	b.BeginFrame()
	b.DrawText(0, 1, "again", s, nil)
	b.SetCursorColor(style.RGB(0xFF, 0xEE, 0x11))
	b.SetCursorPosition(core.Unit(cellX(b, 4)), 1)
	b.SetCursorVisible(true)
	b.EndFrame()
	if got := out.String(); strings.Contains(got, "\033]12;") {
		t.Errorf("an unchanged caret colour was written again: %q", got)
	}

	// And handed back, the reader's own returns with OSC 112.
	out.Reset()
	b.BeginFrame()
	b.DrawText(0, 1, "plain", s, nil)
	b.SetCursorColor(style.ColorDefault)
	b.SetCursorPosition(core.Unit(cellX(b, 3)), 1)
	b.SetCursorVisible(true)
	b.EndFrame()
	if got := out.String(); !strings.Contains(got, "\033]112") {
		t.Errorf("the reader's own caret colour was not handed back: %q", got)
	}
}

// Whatever this process asked for, the terminal goes back to the reader's own
// caret colour on the way out -- otherwise it follows the shell that inherits
// the terminal.
func TestTheCaretColourIsHandedBackOnTheWayOut(t *testing.T) {
	b, out := newTestTUI(20, 2)

	b.BeginFrame()
	b.SetCursorColor(style.RGB(0x00, 0xFF, 0x00))
	b.SetCursorPosition(0, 0)
	b.SetCursorVisible(true)
	b.EndFrame()

	out.Reset()
	b.RestoreTerminal()
	if got := out.String(); !strings.Contains(got, "\033]112") {
		t.Errorf("the terminal was left wearing this process's caret colour: %q", got)
	}
}
