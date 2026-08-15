package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// Modal-surfacing must be wired on single-surface platforms (the TUI) too, not
// only where tear-off is available: clicking a modally-blocked window raises its
// app's modal back to the top. setupTearOff(nil, nil) takes the single-surface
// path (no tear handler), which must still wire the blocked-click callback.
func TestBlockedClickSurfacesModalSingleSurface(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	d.windowManager.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})
	// Single-surface path: wires the modal callbacks, no tear handler.
	d.setupTearOff(nil, nil)

	base := window.NewWindow("base")
	base.SetAppID(1)
	base.SetBounds(core.UnitRect{X: 8, Y: 16, Width: 200, Height: 120})
	d.windowManager.AddWindow(base)

	modal := window.NewWindow("modal")
	modal.SetType(window.WindowTypeModal)
	modal.SetAppID(1)
	modal.SetBounds(core.UnitRect{X: 400, Y: 300, Width: 200, Height: 120})
	d.windowManager.AddWindow(modal) // app modal blocking app 1; becomes active/top

	// Bury the modal and drop focus, so surfacing it is observable.
	d.windowManager.RaiseWindow(base)
	d.windowManager.DeactivateActiveWindow()

	// Click the blocked base window.
	d.windowManager.HandleMousePress(core.MousePressEvent{X: 20, Y: 40, Button: core.LeftButton})

	if got := d.windowManager.ActiveWindow(); got != modal {
		t.Fatalf("blocked-window click did not surface the modal (active=%v)", got)
	}
	wins := d.windowManager.Windows()
	if len(wins) == 0 || wins[len(wins)-1] != modal {
		t.Error("modal was not raised to the top of the z-order")
	}
}
