package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// hoverTracker records the last move it saw so a test can assert it was
// sent an out-of-bounds clearing move.
type hoverTracker struct {
	core.TrinketBase
	lastX, lastY core.Unit
}

func (h *hoverTracker) HandleMouseMove(e core.MouseMoveEvent) bool {
	h.lastX, h.lastY = e.X, e.Y
	return false
}

// Moving the pointer onto an MDI child sends the pane's background content
// an out-of-bounds move, so a parent-window item it had hovered doesn't stay
// stuck.
func TestMDIChildHoverClearsParentContent(t *testing.T) {
	m := NewMDIPane()
	m.SetBounds(core.UnitRect{Width: 800, Height: 600})

	tracker := &hoverTracker{}
	tracker.TrinketBase = *core.NewTrinketBase()
	tracker.Init(tracker)
	tracker.SetBounds(core.UnitRect{Width: 800, Height: 600})
	m.SetContent(tracker)

	child := window.NewWindow("Child")
	child.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
	m.AddWindow(child)
	m.ActivateWindow(child)

	// A hover move squarely inside the child window.
	m.HandleMouseMove(core.MouseMoveEvent{X: 200, Y: 180})

	if tracker.lastX != -1 || tracker.lastY != -1 {
		t.Errorf("background content should get a clearing (-1,-1) move, got (%d,%d)",
			tracker.lastX, tracker.lastY)
	}
}
