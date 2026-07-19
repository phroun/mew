package trinkets

import (
	"fmt"
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// A tall menu opened from a detached window's own menu bar clamps to the
// window surface (maxVisible set, so scroll bumpers appear) instead of
// overflowing off the bottom.
func TestDetachedMenuClampsToWindowHeight(t *testing.T) {
	w := window.NewWindow("w")
	w.SetDetached(true)

	mb := NewMenuBar()
	menu := NewMenu("Big")
	for i := 0; i < 50; i++ {
		menu.AddItem(NewMenuItem(fmt.Sprintf("Item %d", i)))
	}
	mb.AddMenu(menu)

	// Chrome install points the menu bar's parent at the window.
	w.SetWindowMenuBar(mb)
	w.SetBounds(core.UnitRect{Width: 400, Height: 200})
	w.Layout()

	mb.OpenMenu(0)

	if !menu.needsScrolling() {
		t.Errorf("menu did not clamp to the window height (maxVisible=%d, items=%d)",
			menu.maxVisible, len(menu.items))
	}
}

type stubTimer struct{}

func (stubTimer) Stop() {}

// When a detached window's menu bar has the fallback scroll-timer wiring
// (set by the desktop at tear-off), its opened dropdown inherits it, so
// hover auto-scroll works even though the parent Window can't provide a
// timer.
func TestDetachedMenuInheritsScrollTimer(t *testing.T) {
	w := window.NewWindow("w")
	w.SetDetached(true)

	mb := NewMenuBar()
	menu := NewMenu("Big")
	for i := 0; i < 50; i++ {
		menu.AddItem(NewMenuItem(fmt.Sprintf("Item %d", i)))
	}
	mb.AddMenu(menu)
	w.SetWindowMenuBar(mb)
	w.SetBounds(core.UnitRect{Width: 400, Height: 200})
	w.Layout()

	mb.SetScrollTimerStarter(func(_ time.Duration, _ func()) interface{ Stop() } { return stubTimer{} })
	mb.SetRequestUpdate(func() {})

	mb.OpenMenu(0)

	if menu.scrollTimerStarter == nil {
		t.Error("opened dropdown did not inherit the bar's scroll-timer starter")
	}
	if menu.requestUpdate == nil {
		t.Error("opened dropdown did not inherit the bar's update requester")
	}
}
