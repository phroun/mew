package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// InvalidateRect accumulates the union of bounded regions, and a degenerate
// rect escalates to a full repaint (needsFrame).
func TestDesktopInvalidateRectAccumulates(t *testing.T) {
	d := NewDesktop()
	d.InvalidateRect(core.UnitRect{X: 10, Y: 10, Width: 20, Height: 20})
	d.InvalidateRect(core.UnitRect{X: 40, Y: 5, Width: 10, Height: 10})

	d.damageMu.Lock()
	got := d.damageRect
	has := d.hasDamage
	d.damageMu.Unlock()
	want := core.UnitRect{X: 10, Y: 5, Width: 40, Height: 25} // union of the two
	if !has || got != want {
		t.Errorf("damage = %+v (has=%v), want %+v", got, has, want)
	}

	// A degenerate rect can't be a partial region: escalate to a full repaint.
	d2 := NewDesktop()
	d2.InvalidateRect(core.UnitRect{})
	if !d2.needsFrame.Load() {
		t.Error("degenerate InvalidateRect did not escalate to a full repaint")
	}
	d2.damageMu.Lock()
	has2 := d2.hasDamage
	d2.damageMu.Unlock()
	if has2 {
		t.Error("degenerate InvalidateRect should not set bounded damage")
	}
}

// Partial damage is restricted to the main-surface controller; anything else
// (nil, a foreign controller) is rejected so the caller repaints in full.
func TestDesktopIsMainSurfaceController(t *testing.T) {
	d := NewDesktop()
	if d.IsMainSurfaceController(nil) {
		t.Error("nil controller must not be treated as the main surface")
	}
	if d.IsMainSurfaceController(stubPopupController{}) {
		t.Error("a foreign controller must not be treated as the main surface")
	}
}
