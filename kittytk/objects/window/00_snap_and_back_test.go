package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// barredDesktop keeps a menu-bar strip above its client area, which is the
// strip a drag snap-maximizes into.
type barredDesktop struct {
	graphicalDesktop
	bar core.Unit
}

func (b *barredDesktop) ClientArea() core.UnitRect {
	r := b.Bounds()
	return core.UnitRect{X: r.X, Y: r.Y + b.bar, Width: r.Width, Height: r.Height - b.bar}
}

// snapHost is a manager on a SMOOTH surface with such a strip.
func snapHost(t *testing.T) (*WindowManager, *Window) {
	t.Helper()
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 1200, Height: 800})
	m.SetSmoothPositioning(true)
	m.SetDesktop(&barredDesktop{bar: 16})

	win := NewWindow("w")
	win.SetBounds(core.UnitRect{X: 200, Y: 200, Width: 320, Height: 200})
	m.AddWindow(win)
	return m, win
}

// grabFraction is how far along the title bar the pointer sits, as a fraction
// of the frame's width. It is what a drag holds on to: the window changing
// size under the pointer must not move the pointer along its own title bar.
func grabFraction(win *Window, at core.UnitPoint) float64 {
	b, fr := win.Bounds(), win.FrameRect()
	return float64(at.X-b.X-fr.X) / float64(fr.Width)
}

// Dragging a window into the menu bar maximizes it, and coming back below the
// menu bar in the SAME drag brings it down again -- no pull needed, because
// the gesture that maximized it is already a drag. The pointer keeps its place
// along the title bar across both.
//
// The grab is the point of the window the pointer is holding, and the window
// is placed to keep that point under it. A snap-maximize changes the window's
// geometry under the pointer, so the grab has to be re-expressed in the frame
// it is now holding; the restore that unwinds the snap works from that number,
// and inherits whatever is wrong with it.
func TestComingBackDownInTheSameDragUndoesTheSnap(t *testing.T) {
	m, win := snapHost(t)

	at := core.UnitPoint{X: 280, Y: 208}
	want := grabFraction(win, at)
	m.HandleMousePress(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})

	up := core.UnitPoint{X: 600, Y: 8}
	m.HandleMouseMove(core.MouseMoveEvent{X: up.X, Y: up.Y, Buttons: core.LeftButton})
	if !win.IsMaximized() {
		t.Fatal("dragging into the menu bar did not maximize")
	}

	// A single row back below the strip, which is where the snap fired.
	down := core.UnitPoint{X: 600, Y: m.ClientArea().Y + 4}
	m.HandleMouseMove(core.MouseMoveEvent{X: down.X, Y: down.Y, Buttons: core.LeftButton})
	if win.IsMaximized() {
		t.Fatal("coming back below the menu bar left the window maximized")
	}
	if got := grabFraction(win, down); got < want-0.02 || got > want+0.02 {
		t.Errorf("after coming back down the pointer is %.3f along the title bar; it grabbed at %.3f",
			got, want)
	}
	m.HandleMouseRelease(core.MouseReleaseEvent{X: down.X, Y: down.Y, Button: core.LeftButton})
}

// A window maximized some other way still needs a real pull: only the drag
// that snapped it up may put it down without one.
func TestAWindowNotSnappedByThisDragStillNeedsAPull(t *testing.T) {
	m, win := snapHost(t)
	m.MaximizeWindow(win)

	fr := win.FrameRect()
	b := win.Bounds()
	at := core.UnitPoint{X: b.X + fr.X + fr.Width/2, Y: b.Y + fr.Y + 8}
	m.HandleMousePress(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	m.HandleMouseMove(core.MouseMoveEvent{X: at.X + 8, Y: at.Y + 2, Buttons: core.LeftButton})
	if !win.IsMaximized() {
		t.Error("a nudge brought down a window this drag did not maximize")
	}
	m.HandleMouseRelease(core.MouseReleaseEvent{X: at.X + 8, Y: at.Y + 2, Button: core.LeftButton})
}
