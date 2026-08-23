package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// graphicalDesktop is the minimum a window's ancestry has to answer for the
// graphical resize rule to apply: which kind of frame the surface paints.
// The manager used to be handed a grip WIDTH for this; it is one bit, and
// FindGraphicalFrames already asks for it.
type graphicalDesktop struct {
	core.TrinketBase
	kids []core.Trinket
}

func (g *graphicalDesktop) GraphicalWindowFrames() bool         { return true }
func (g *graphicalDesktop) Children() []core.Trinket            { return g.kids }
func (g *graphicalDesktop) AddChild(c core.Trinket)             { g.kids = append(g.kids, c) }
func (g *graphicalDesktop) RemoveChild(core.Trinket)            {}
func (g *graphicalDesktop) ChildAt(core.UnitPoint) core.Trinket { return nil }
func (g *graphicalDesktop) Layout()                             {}
func (g *graphicalDesktop) LayoutManager() core.LayoutManager   { return nil }
func (g *graphicalDesktop) SetLayoutManager(core.LayoutManager) {}

func newGraphicalManager() (*WindowManager, *Window) {
	m, win := newPositioningManager(true)
	d := &graphicalDesktop{}
	d.Init(d)
	m.SetDesktop(d)
	win.SetParent(d)
	return m, win
}

// Graphical frames narrow the resize grip to the outer edge sliver
// so trinkets at the window edge stay clickable; cell frames keep the
// classic full-cell zones.
func TestResizeGripNarrowsEdgeZones(t *testing.T) {
	m, win := newGraphicalManager()

	// The grab zone is a quarter column or 3 device pixels, whichever is
	// bigger, border included (ResizeHitGrip). At ppu 1 with an 8-unit cell
	// that is 3 units, so x=396 of 80..400 is outside it and the press
	// reaches the content.
	m.HandleMousePress(core.MousePressEvent{X: 396, Y: 160, Button: core.LeftButton})
	m.HandleMouseMove(core.MouseMoveEvent{X: 409, Y: 160})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: 409, Y: 160, Button: core.LeftButton})
	if b := win.Bounds(); b.Width != 320 {
		t.Errorf("press outside the grip resized the window: width %d", b.Width)
	}

	// 1 unit inside the right edge: within the grip - resizes.
	m.HandleMousePress(core.MousePressEvent{X: 399, Y: 160, Button: core.LeftButton})
	m.HandleMouseMove(core.MouseMoveEvent{X: 412, Y: 160})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: 412, Y: 160, Button: core.LeftButton})
	if b := win.Bounds(); b.Width != 333 {
		t.Errorf("press inside the grip did not resize: width %d, want 333", b.Width)
	}
}

// The bottom band narrows too: on cell frames a whole row grabbed
// the bottom edge; with a grip only the outer sliver does.
func TestResizeGripNarrowsBottomBand(t *testing.T) {
	m, win := newGraphicalManager()

	// 5 units above the bottom edge (y=235 of 80..240): outside the
	// grip - not a resize.
	m.HandleMousePress(core.MousePressEvent{X: 200, Y: 235, Button: core.LeftButton})
	m.HandleMouseMove(core.MouseMoveEvent{X: 200, Y: 250})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: 200, Y: 250, Button: core.LeftButton})
	if b := win.Bounds(); b.Height != 160 {
		t.Errorf("press outside the bottom grip resized: height %d", b.Height)
	}

	// 1 unit above the bottom edge: resizes.
	m.HandleMousePress(core.MousePressEvent{X: 200, Y: 239, Button: core.LeftButton})
	m.HandleMouseMove(core.MouseMoveEvent{X: 200, Y: 252})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: 200, Y: 252, Button: core.LeftButton})
	if b := win.Bounds(); b.Height != 173 {
		t.Errorf("press inside the bottom grip did not resize: height %d, want 173", b.Height)
	}
}

// On graphical frames the top edge is grabbable: pressing within the top
// grip and dragging up grows the window and moves its top edge up, mirroring
// the bottom edge. (The TUI reserves the top row for title dragging.)
func TestTopGripResizesGraphical(t *testing.T) {
	m, win := newGraphicalManager()

	// 1 unit below the top edge (y=81 of 80..240): within the grip.
	// Drag up 13 units.
	m.HandleMousePress(core.MousePressEvent{X: 200, Y: 81, Button: core.LeftButton})
	m.HandleMouseMove(core.MouseMoveEvent{X: 200, Y: 68})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: 200, Y: 68, Button: core.LeftButton})

	b := win.Bounds()
	if b.Y != 67 || b.Height != 173 {
		t.Errorf("top grip resize: got Y=%d Height=%d, want Y=67 Height=173", b.Y, b.Height)
	}
}

// The top corner grabs both edges: dragging the top-left corner up-and-left
// moves X and Y and grows both dimensions.
func TestTopLeftCornerResizesGraphical(t *testing.T) {
	m, win := newGraphicalManager()

	// Top-left corner (x=81 of 80..400, y=81 of 80..240): within both grips.
	m.HandleMousePress(core.MousePressEvent{X: 81, Y: 81, Button: core.LeftButton})
	m.HandleMouseMove(core.MouseMoveEvent{X: 71, Y: 68})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: 71, Y: 68, Button: core.LeftButton})

	b := win.Bounds()
	// dx=-10 -> X=70 Width=330; dy=-13 -> Y=67 Height=173.
	if b.X != 70 || b.Width != 330 || b.Y != 67 || b.Height != 173 {
		t.Errorf("top-left corner resize: got %v, want X=70 W=330 Y=67 H=173", b)
	}
}

// On cell frames (zero grip) the top row stays a title-drag zone: pressing
// there moves the window, it does not resize the top edge.
func TestTopEdgeIsTitleDragOnCellFrames(t *testing.T) {
	m, win := newPositioningManager(false)

	orig := win.Bounds()
	// Press in the top row (y=88, within the first cell row) and drag up.
	m.HandleMousePress(core.MousePressEvent{X: 200, Y: 88, Button: core.LeftButton})
	m.HandleMouseMove(core.MouseMoveEvent{X: 200, Y: 72})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: 200, Y: 72, Button: core.LeftButton})

	b := win.Bounds()
	if b.Height != orig.Height {
		t.Errorf("cell-frame top row resized (height %d -> %d); it should drag-move", orig.Height, b.Height)
	}
}

// Zero grip preserves the classic cell-frame zones untouched.
func TestZeroGripKeepsCellZones(t *testing.T) {
	m, win := newPositioningManager(false)

	// 5 units inside the right edge: within the classic one-cell zone.
	m.HandleMousePress(core.MousePressEvent{X: 395, Y: 160, Button: core.LeftButton})
	m.HandleMouseMove(core.MouseMoveEvent{X: 403, Y: 160})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: 403, Y: 160, Button: core.LeftButton})
	if b := win.Bounds(); b.Width == 320 {
		t.Error("cell-frame edge zone should still be one cell wide")
	}
}
