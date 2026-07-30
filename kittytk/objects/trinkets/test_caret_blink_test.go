package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// A keystroke restarts the terminal cursor's blink phase so the
// cursor is visible immediately while typing.
func TestGfxKeyPressResetsCursorBlink(t *testing.T) {
	_, _, term, _ := gfxHarness(t)

	term.gfx.cursorBlinkOn = false
	term.gfx.blinkTick = 7
	term.HandleKeyPress(core.KeyPressEvent{Key: "a", Text: "a"})

	if !term.gfx.cursorBlinkOn {
		t.Error("cursor still hidden after keypress")
	}
	if term.gfx.blinkTick != 0 {
		t.Errorf("blink phase not restarted (tick %d)", term.gfx.blinkTick)
	}
}

// The TextInput bar caret blinks on graphical desktops: a timer
// toggles visibility, keystrokes restart the phase visible, and
// losing focus stops the timer (steady caret without one).
func TestTextInputCaretBlink(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	ti := NewTextInput()
	win := window.NewWindow("host")
	win.SetContent(ti)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 300, Height: 100})
	win.Layout()

	// No timer: the caret is steadily visible (cell surfaces).
	if !ti.caretVisible() {
		t.Error("caret hidden with no blink timer running")
	}

	ti.ensureCaretTimer()
	if ti.caretTimer == nil {
		t.Fatal("caret timer did not start (desktop not reachable?)")
	}
	if !ti.caretVisible() {
		t.Error("caret not visible right after the timer starts")
	}

	// Blink phase hides the caret; a keystroke restores it at once.
	ti.caretOn = false
	if ti.caretVisible() {
		t.Error("caret visible while blinked off")
	}
	ti.HandleKeyPress(core.KeyPressEvent{Key: "x", Text: "x"})
	if !ti.caretVisible() {
		t.Error("keystroke did not make the caret immediately visible")
	}
	if ti.caretTimer == nil {
		t.Error("blink timer not restarted after keystroke")
	}

	ti.HandleFocusOut()
	if ti.caretTimer != nil {
		t.Error("blink timer still running after focus out")
	}
	if !ti.caretVisible() {
		t.Error("caret should be steady (visible) once the timer stops")
	}
}
