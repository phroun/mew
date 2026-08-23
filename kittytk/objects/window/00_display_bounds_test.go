package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The corral has to be readable from OUTSIDE the container, because not
// everything that positions a window goes through the container's paint loop.
// A GPU compositor is handed the windows themselves and places each one as a
// layer of its own; with no way to ask where the container would draw it, it
// positioned from Bounds() and drew windows off the edge of a shrunk desktop
// while hit-testing still corralled them.
//
// So this is the same assertion TestProvisionalCorralRespread makes, asked the
// way an outsider has to ask it.
func TestWindowReportsTheCorralledDisplayBounds(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 608})
	win := NewWindow("corral")
	win.SetBounds(core.UnitRect{X: 600, Y: 100, Width: 160, Height: 120})
	m.AddWindow(win)

	// Fits: the window reports exactly where it sits.
	if got := win.DisplayBounds(); got.X != 600 {
		t.Fatalf("fits: DisplayBounds X = %d, want 600", got.X)
	}

	// Shrunk: it reports the corral, and agrees with the container's own
	// paint loop to the pixel — two answers here means draw and hit disagree.
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 300, Height: 608})
	got, want := win.DisplayBounds(), m.displayBounds(win)
	if got != want {
		t.Errorf("shrunk: DisplayBounds %+v, container draws %+v", got, want)
	}
	if got.X >= 600 {
		t.Errorf("shrunk: DisplayBounds X = %d, want corralled into view", got.X)
	}
	if win.Bounds().X != 600 {
		t.Errorf("shrunk: logical X moved to %d; the corral must stay provisional", win.Bounds().X)
	}

	// Regrown: it re-spreads, which is the whole point of not writing back.
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 608})
	if got := win.DisplayBounds(); got.X != 600 {
		t.Errorf("regrown: DisplayBounds X = %d, want re-spread to 600", got.X)
	}
}

// A window with no container answers for itself. A torn-off window owns its
// OS geometry and has no corral to report; reporting an empty rect instead
// would erase it from any compositor that asked.
func TestDisplayBoundsWithoutAContainer(t *testing.T) {
	win := NewWindow("loose")
	win.SetBounds(core.UnitRect{X: 40, Y: 20, Width: 100, Height: 80})
	if got := win.DisplayBounds(); got != win.Bounds() {
		t.Errorf("DisplayBounds = %+v with no container, want Bounds %+v", got, win.Bounds())
	}
}

// Maximized windows are exempt: they already track the client area, and
// corralling one would fight the container that just sized it.
func TestDisplayBoundsExemptsMaximized(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 608})
	win := NewWindow("max")
	win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 200, Height: 150})
	m.AddWindow(win)
	m.MaximizeWindow(win)

	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 300, Height: 608})
	if got := win.DisplayBounds(); got != win.Bounds() {
		t.Errorf("maximized: DisplayBounds %+v != Bounds %+v", got, win.Bounds())
	}
}
