package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// newPositioningManager builds a manager with one 320x160 window at
// (80, 80) on an 800x608 screen, ready for drag/resize simulation.
func newPositioningManager(smooth bool) (*WindowManager, *Window) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 608})
	m.SetSmoothPositioning(smooth)
	win := NewWindow("positioning")
	win.SetBounds(core.UnitRect{X: 80, Y: 80, Width: 320, Height: 160})
	m.AddWindow(win)
	return m, win
}

func TestDragSnapsToCellsOnCellSurfaces(t *testing.T) {
	m, win := newPositioningManager(false)

	// Grab the titlebar (y within the first cell row) away from any
	// buttons, then move by deltas that are not cell multiples.
	m.HandleMousePress(core.MousePressEvent{X: 200, Y: 88, Button: core.LeftButton})
	m.HandleMouseMove(core.MouseMoveEvent{X: 237, Y: 141})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: 237, Y: 141, Button: core.LeftButton})

	// Raw target would be (117, 133); a cell surface snaps to 8x16.
	b := win.Bounds()
	if b.X != 112 || b.Y != 128 {
		t.Errorf("cell surface drag: got (%d, %d), want snapped (112, 128)", b.X, b.Y)
	}
}

func TestDragTracksPointerOnSmoothSurfaces(t *testing.T) {
	m, win := newPositioningManager(true)

	m.HandleMousePress(core.MousePressEvent{X: 200, Y: 88, Button: core.LeftButton})
	m.HandleMouseMove(core.MouseMoveEvent{X: 237, Y: 141})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: 237, Y: 141, Button: core.LeftButton})

	// Grab offset was (120, 8), so the window lands exactly at the
	// pointer minus offset - no cell rounding.
	b := win.Bounds()
	if b.X != 117 || b.Y != 133 {
		t.Errorf("smooth surface drag: got (%d, %d), want unsnapped (117, 133)", b.X, b.Y)
	}
}

func TestResizeSnapsToCellsOnCellSurfaces(t *testing.T) {
	m, win := newPositioningManager(false)

	// Grab the right edge (within one cell of x=400) and pull it out
	// by 13 units - not a cell multiple.
	m.HandleMousePress(core.MousePressEvent{X: 396, Y: 160, Button: core.LeftButton})
	m.HandleMouseMove(core.MouseMoveEvent{X: 409, Y: 160})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: 409, Y: 160, Button: core.LeftButton})

	b := win.Bounds()
	if b.Width != 328 || b.Height != 160 {
		t.Errorf("cell surface resize: got %dx%d, want snapped 328x160", b.Width, b.Height)
	}
}

func TestResizeTracksPointerOnSmoothSurfaces(t *testing.T) {
	m, win := newPositioningManager(true)

	m.HandleMousePress(core.MousePressEvent{X: 396, Y: 160, Button: core.LeftButton})
	m.HandleMouseMove(core.MouseMoveEvent{X: 409, Y: 160})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: 409, Y: 160, Button: core.LeftButton})

	b := win.Bounds()
	if b.Width != 333 || b.Height != 160 {
		t.Errorf("smooth surface resize: got %dx%d, want unsnapped 333x160", b.Width, b.Height)
	}
}

func TestManagerStampsCapabilityOntoWindows(t *testing.T) {
	// Windows added before and after the capability is set both carry it.
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 608})

	early := NewWindow("early")
	early.SetBounds(core.UnitRect{X: 16, Y: 16, Width: 160, Height: 96})
	m.AddWindow(early)
	if early.SmoothWindowPositioning() {
		t.Error("window should default to snapped positioning")
	}

	m.SetSmoothPositioning(true)
	if !early.SmoothWindowPositioning() {
		t.Error("existing window not stamped when capability enabled")
	}

	late := NewWindow("late")
	late.SetBounds(core.UnitRect{X: 32, Y: 32, Width: 160, Height: 96})
	m.AddWindow(late)
	if !late.SmoothWindowPositioning() {
		t.Error("window added after enable not stamped")
	}
}
