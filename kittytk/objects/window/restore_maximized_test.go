package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A window minimized while maximized must come back MAXIMIZED, not normal, and
// re-fit the client area. Regression for the dock / Show All round trip losing
// the maximized state (a NoTitleWhenMaximized window otherwise regained its
// frame and stale bounds until a manual resize/maximize).
func TestRestoreMinimizedWhileMaximizedStaysMaximized(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 600})

	win := NewWindow("root")
	m.AddWindow(win)
	floating := core.UnitRect{X: 20, Y: 20, Width: 200, Height: 150}
	win.SetBounds(floating)

	m.MaximizeWindow(win)
	if !win.IsMaximized() {
		t.Fatal("window did not maximize")
	}
	client := m.ClientArea()

	win.Minimize()
	if !win.IsMinimized() {
		t.Fatal("window did not minimize")
	}

	m.RestoreWindow(win)

	if !win.IsMaximized() {
		t.Fatal("restored window lost its maximized state (came back normal)")
	}
	if b := win.Bounds(); b != client {
		t.Errorf("restored maximized bounds = %v, want the client area %v", b, client)
	}

	// Un-maximizing now returns to the original floating size, proving Minimize
	// did not clobber normalBounds with the maximized rect.
	win.Restore()
	if win.IsMaximized() {
		t.Fatal("second Restore did not un-maximize")
	}
	if b := win.Bounds(); b != floating {
		t.Errorf("un-maximized bounds = %v, want the pre-maximize floating %v", b, floating)
	}
}
