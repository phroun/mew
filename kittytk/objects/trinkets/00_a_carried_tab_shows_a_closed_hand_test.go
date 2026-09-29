package trinkets

// A press that picks up a tab on a movable strip shows the closed hand, and it
// stays on while the tab is carried, wherever the pointer goes, until the
// press comes up.

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/platform"
)

// cursorWatch is a platform that records the cursor it was last given.
type cursorWatch struct {
	platform.Platform
	shape core.CursorShape
}

func (c *cursorWatch) SetCursor(s core.CursorShape) { c.shape = s }

func TestACarriedTabShowsAClosedHand(t *testing.T) {
	for _, movable := range []bool{true, false} {
		core.SetTextMeasurer(nil)
		d := NewDesktop()
		d.SetBackend(&nullBackend{})
		d.WindowManager().SetScreenBounds(core.UnitRect{Width: 640, Height: 384})
		tt := NewTabTrinket()
		tt.SetMovable(movable)
		for _, n := range []string{"Alpha", "Bravo", "Charlie"} {
			tt.AddTab(n, NewPanel())
		}
		win := window.NewWindow("tabs")
		win.SetContent(tt)
		d.WindowManager().AddWindow(win)
		d.WindowManager().ActivateWindow(win)
		win.SetBounds(core.UnitRect{X: 16, Y: 32, Width: 400, Height: 200})
		win.Layout()
		paintCloseGridW(t, tt, 50)
		cw := &cursorWatch{shape: -1}
		d.platform = cw

		var sp stripSpan
		for _, s := range tt.stripSpans {
			if s.owner == tt.CurrentIndex() {
				sp = s
			}
		}
		m := tt.EffectiveCellMetrics()
		at := window.MapTrinketToScreen(tt, core.UnitPoint{X: sp.x + m.UnitsPerCellWidth, Y: m.UnitsPerCellHeight / 2})

		d.dispatchEvent(core.MouseMoveEvent{X: at.X, Y: at.Y})
		if cw.shape != core.CursorDefault {
			t.Fatalf("movable %v: hovering the tab shows %v, want the arrow", movable, cw.shape)
		}
		d.dispatchEvent(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
		want := core.CursorDefault
		if movable {
			want = core.CursorGrabbing
		}
		if cw.shape != want {
			t.Fatalf("movable %v: pressing the tab shows %v, want %v", movable, cw.shape, want)
		}
		// Carried off the strip, over the content: still the hand.
		d.dispatchEvent(core.MouseMoveEvent{X: at.X, Y: at.Y + 5*m.UnitsPerCellHeight, Buttons: core.LeftButton})
		if cw.shape != want {
			t.Errorf("movable %v: carried over the content it shows %v, want %v", movable, cw.shape, want)
		}
		d.dispatchEvent(core.MouseReleaseEvent{X: at.X, Y: at.Y + 5*m.UnitsPerCellHeight, Button: core.LeftButton})
		if cw.shape != core.CursorDefault {
			t.Errorf("movable %v: after the release it shows %v, want the arrow", movable, cw.shape)
		}
	}
}

// A side strip carries its tab by sweeping the rows: the hand shows while a
// movable one is swept, and a strip that cannot move its tabs only selects.
func TestASweptSideTabShowsAClosedHandOnlyWhenItMoves(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	for _, movable := range []bool{true, false} {
		tt := longStrip(t, TabsSide, core.DirLTR, 40, 2)
		tt.SetMovable(movable)
		m := tt.EffectiveCellMetrics()
		row := core.Unit(2-tt.vertScrollOffset)*m.UnitsPerCellHeight + m.UnitsPerCellHeight/2
		tt.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: 2 * m.UnitsPerCellWidth, Y: row})
		want := core.CursorDefault
		if movable {
			want = core.CursorGrabbing
		}
		if got := tt.CursorShape(); got != want {
			t.Errorf("movable %v: sweeping the side tabs shows %v, want %v", movable, got, want)
		}
		tt.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton, X: 2 * m.UnitsPerCellWidth, Y: row})
		if got := tt.CursorShape(); got != core.CursorDefault {
			t.Errorf("movable %v: after the release it shows %v", movable, got)
		}
	}
}
