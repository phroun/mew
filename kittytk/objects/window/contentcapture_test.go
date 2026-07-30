package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// captureContent records the mouse events routed to it, standing in for an
// editor surface whose drag must survive the pointer crossing chrome.
type captureContent struct {
	core.TrinketBase
	presses, moves, releases int
	lastMoveY                core.Unit
}

func (c *captureContent) HandleMousePress(core.MousePressEvent) bool { c.presses++; return true }
func (c *captureContent) HandleMouseMove(e core.MouseMoveEvent) bool {
	c.moves++
	c.lastMoveY = e.Y
	return true
}
func (c *captureContent) HandleMouseRelease(core.MouseReleaseEvent) bool { c.releases++; return true }

// statusChrome is a minimal status-bar trinket that records what reaches it.
type statusChrome struct {
	core.TrinketBase
	presses, moves, releases int
}

func (s *statusChrome) HandleMousePress(core.MousePressEvent) bool     { s.presses++; return true }
func (s *statusChrome) HandleMouseMove(core.MouseMoveEvent) bool       { s.moves++; return true }
func (s *statusChrome) HandleMouseRelease(core.MouseReleaseEvent) bool { s.releases++; return true }

// A drag that PRESSED in the content keeps flowing to the content when the
// pointer crosses the window's own status bar — the chrome must not swallow
// the moves (that froze a mew drag-selection's downward edge autoscroll) —
// and the release routes to the content too, ending the capture. Without a
// content press, status-bar moves go to the chrome exactly as before.
func TestContentCapturedDragCrossesStatusBar(t *testing.T) {
	w := NewWindow("cap")
	w.SetDetached(true)
	content := &captureContent{}
	content.TrinketBase = *core.NewTrinketBase()
	w.SetContent(content)
	sb := &statusChrome{}
	sb.TrinketBase = *core.NewTrinketBase()
	w.SetWindowStatusBar(sb)
	w.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 300, Height: 200})

	sbr := w.statusBarRect()
	if sbr.IsEmpty() {
		t.Fatal("precondition: the detached window should show a status bar")
	}
	cb := w.contentBounds()
	inContentX, inContentY := cb.X+10, cb.Y+10
	overSbX, overSbY := sbr.X+5, sbr.Y+sbr.Height/2

	// Press in the content: the gesture is captured.
	w.HandleMousePress(core.MousePressEvent{X: inContentX, Y: inContentY, Button: core.LeftButton})
	if content.presses != 1 {
		t.Fatalf("press should reach the content; got %d", content.presses)
	}

	// Drag down ONTO the status bar: the content still receives the move
	// (with a Y beyond its own height — the overshoot mew's autoscroll needs),
	// and the chrome sees nothing.
	w.HandleMouseMove(core.MouseMoveEvent{X: overSbX, Y: overSbY, Buttons: core.LeftButton})
	if content.moves != 1 {
		t.Fatalf("captured drag must keep flowing to the content; moves=%d", content.moves)
	}
	if sb.moves != 0 {
		t.Fatal("the status bar must not swallow a captured drag")
	}
	if content.lastMoveY <= cb.Height {
		t.Fatalf("the content should see the beyond-its-bottom coordinate; y=%v (h=%v)",
			content.lastMoveY, cb.Height)
	}

	// Release over the status bar ends the gesture IN THE CONTENT.
	w.HandleMouseRelease(core.MouseReleaseEvent{X: overSbX, Y: overSbY, Button: core.LeftButton})
	if content.releases != 1 || sb.releases != 0 {
		t.Fatalf("release belongs to the captured content; content=%d sb=%d",
			content.releases, sb.releases)
	}

	// Capture over: an uncaptured move over the status bar goes to the chrome.
	w.HandleMouseMove(core.MouseMoveEvent{X: overSbX, Y: overSbY})
	if sb.moves != 1 {
		t.Fatalf("without a captured gesture the chrome owns its rows; sb.moves=%d", sb.moves)
	}
	if content.moves != 1 {
		t.Fatalf("the content must not receive uncaptured chrome moves; moves=%d", content.moves)
	}

	// And a press ON the status bar routes to the chrome, not the content.
	w.HandleMousePress(core.MousePressEvent{X: overSbX, Y: overSbY, Button: core.LeftButton})
	if sb.presses != 1 || content.presses != 1 {
		t.Fatalf("chrome press should stay chrome; sb=%d content=%d", sb.presses, content.presses)
	}
}
