package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The manager path with a CELL surface (the TUI): no smooth positioning, no
// graphical frames.
func TestProbeTUIDoubleClickThenSingle(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 800, Height: 608})
	win := NewWindow("w")
	win.SetBounds(core.UnitRect{X: 96, Y: 96, Width: 304, Height: 208})
	m.AddWindow(win)

	click := func(x, y core.Unit) {
		m.HandleMousePress(core.MousePressEvent{X: x, Y: y, Button: core.LeftButton})
		m.HandleMouseRelease(core.MouseReleaseEvent{X: x, Y: y, Button: core.LeftButton})
	}
	titlePt := func() (core.Unit, core.Unit) {
		b := win.Bounds()
		fr := win.FrameRect()
		return b.X + fr.X + fr.Width/2, b.Y + fr.Y + 4
	}

	x, y := titlePt()
	click(x, y)
	click(x, y)
	t.Logf("after a double-click: maximized=%v bounds=%v", win.IsMaximized(), win.Bounds())

	x, y = titlePt()
	click(x, y)
	t.Logf("after ONE more click:  maximized=%v bounds=%v", win.IsMaximized(), win.Bounds())
	if !win.IsMaximized() {
		t.Error("a single click after the maximize restored the window")
	}
	x, y = titlePt()
	click(x, y)
	t.Logf("after a SECOND more:   maximized=%v", win.IsMaximized())
}
