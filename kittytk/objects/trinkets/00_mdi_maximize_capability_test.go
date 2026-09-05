package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// mdiChild is a pane holding one window with the given flags, ready to be
// dragged or double-clicked.
func mdiChild(flags window.WindowFlags) (*MDIPane, *window.Window) {
	m := NewMDIPane()
	m.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 600})
	win := window.NewWindow("child")
	win.SetFlags(flags)
	win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 240, Height: 160})
	m.AddWindow(win)
	return m, win
}

// Maximizing is a resize, so a window that asked not to be resized is not
// maximized -- by a call, by a drag to the top, or by a double-click on its
// title. A MessageBox is exactly such a window: it carries NoResize and says
// nothing about NoMaximize, so a check that reads only NoMaximize lets it
// through.
//
// The pane's own MaximizeWindow had no such check where the desktop's manager
// has one, and the two triggers asked about NoMaximize alone.
func TestAnMDIChildThatCannotMaximizeIsLeftAlone(t *testing.T) {
	for _, c := range []struct {
		name  string
		flags window.WindowFlags
	}{
		{"asked not to be resized", window.WindowFlagNoResize},
		{"asked not to be maximized", window.WindowFlagNoMaximize},
	} {
		// By a call.
		m, win := mdiChild(c.flags)
		before := win.Bounds()
		m.MaximizeWindow(win)
		if win.IsMaximized() || win.Bounds() != before {
			t.Errorf("%s: MaximizeWindow left it %v maximized=%v", c.name, win.Bounds(), win.IsMaximized())
		}

		// By dragging its title above the pane's top edge.
		m, win = mdiChild(c.flags)
		m.mu.Lock()
		m.dragging = win
		m.dragOffsetX, m.dragOffsetY = 20, 8
		m.mu.Unlock()
		before = win.Bounds()
		m.HandleMouseMove(core.MouseMoveEvent{X: 120, Y: -20})
		if win.IsMaximized() {
			t.Errorf("%s: dragging to the top maximized it", c.name)
		}
		// And it keeps following the pointer rather than sticking: the
		// maximize gesture swallows the move, so a window that cannot take
		// the gesture must not reach it.
		if win.Bounds() == before {
			t.Errorf("%s: dragging to the top froze it at %v", c.name, before)
		}
	}

	// And a window that may be maximized still is, so the check bounds the
	// refusal rather than the feature.
	m, win := mdiChild(0)
	m.MaximizeWindow(win)
	if !win.IsMaximized() {
		t.Error("a window with nothing against it was not maximized")
	}
}
