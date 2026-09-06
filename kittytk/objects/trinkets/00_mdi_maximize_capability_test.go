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

// The pane re-fits its maximized children when it is resized, and a child that
// says how far it grows keeps its cap through that -- as it does on the
// desktop, and for the same reason: the re-fit happens at layout time, so a cap
// applied only by MaximizeWindow is wiped before anyone sees it.
func TestAMaximizedMDIChildsCapSurvivesAPaneResize(t *testing.T) {
	m, win := mdiChild(0)
	win.SetMaximumSize(core.UnitSize{Width: 320, Height: 200})
	m.MaximizeWindow(win)

	if got := win.Bounds(); got != m.ClientArea() {
		t.Fatalf("maximized in the pane its surface is %v, want the whole %v", got, m.ClientArea())
	}

	m.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 600, Height: 500})
	pane := m.ClientArea()
	if got := win.Bounds(); got != pane {
		t.Errorf("after a pane resize its surface is %v, want the whole %v", got, pane)
	}
	fr := window.MaximizedFrameRect(win, pane.Size())
	if fr.Width != 320 || fr.Height != 200 {
		t.Errorf("after a pane resize its frame is %v, want its maximum of 320x200", fr.Size())
	}
	// Centred, then floored onto the cell grid the pane renders on.
	cm := core.DefaultCellMetrics()
	wantX := cm.RoundDownToCellX((pane.Width - 320) / 2)
	wantY := cm.RoundDownToCellY((pane.Height - 200) / 2)
	if fr.X != wantX || fr.Y != wantY {
		t.Errorf("after a pane resize its frame sits at %d,%d, want %d,%d -- centered in %v "+
			"and floored onto the cell grid", fr.X, fr.Y, wantX, wantY, pane)
	}
}
