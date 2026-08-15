package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// A focused control in a quasi-active window (lit, heavy single border because
// OS focus lives on another torn-off surface) still counts as focus-chain
// active, so its caret keeps showing - it must not go dark like a truly
// inactive/background window.
func TestFocusChainActiveThroughQuasiActiveWindow(t *testing.T) {
	input := NewTextInput()
	win := window.NewWindow("host")
	win.SetContent(input)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 200, Height: 120})
	win.Layout()
	input.SetFocus()

	// Active window: the chain is active.
	win.SetActive(true)
	if !core.FocusChainActive(input.Self()) {
		t.Fatal("active window: focus chain should be active")
	}

	// Quasi-active (torn surface yielded OS focus): still active for caret
	// purposes even though IsActive() is false.
	win.SetActive(false)
	win.SetQuasiActive(true)
	if win.IsActive() {
		t.Fatal("precondition: quasi-active window should report IsActive() == false")
	}
	if !core.FocusChainActive(input.Self()) {
		t.Error("quasi-active window: focus chain should still be active (caret shows)")
	}

	// Truly inactive/background: the caret must go dark.
	win.SetQuasiActive(false)
	if core.FocusChainActive(input.Self()) {
		t.Error("inactive window: focus chain should be inactive (caret hidden)")
	}
}
