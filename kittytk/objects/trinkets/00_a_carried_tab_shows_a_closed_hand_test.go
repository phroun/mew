package trinkets

// Over a tab a press would pick up, the pointer is an open hand; the press
// that picks it up closes it, and the closed hand stays on while the tab is
// carried, wherever the pointer goes, until the press comes up.

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

		hover := core.CursorDefault
		if movable {
			hover = core.CursorGrab
		}
		d.dispatchEvent(core.MouseMoveEvent{X: at.X, Y: at.Y})
		if cw.shape != hover {
			t.Fatalf("movable %v: hovering the current tab shows %v, want %v", movable, cw.shape, hover)
		}
		// The content under the strip is not a tab.
		c := window.MapTrinketToScreen(tt, core.UnitPoint{X: m.UnitsPerCellWidth, Y: 5 * m.UnitsPerCellHeight})
		d.dispatchEvent(core.MouseMoveEvent{X: c.X, Y: c.Y})
		if cw.shape != core.CursorDefault {
			t.Errorf("movable %v: hovering the content shows %v, want the arrow", movable, cw.shape)
		}
		d.dispatchEvent(core.MouseMoveEvent{X: at.X, Y: at.Y})
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
		tt.HandleMouseMove(core.MouseMoveEvent{X: 2 * m.UnitsPerCellWidth, Y: row})
		hover := core.CursorDefault
		if movable {
			hover = core.CursorGrab
		}
		if got := tt.CursorShape(); got != hover {
			t.Errorf("movable %v: hovering a side tab shows %v, want %v", movable, got, hover)
		}
		tt.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: 2 * m.UnitsPerCellWidth, Y: row})
		want := core.CursorDefault
		if movable {
			want = core.CursorGrabbing
		}
		if got := tt.CursorShape(); got != want {
			t.Errorf("movable %v: sweeping the side tabs shows %v, want %v", movable, got, want)
		}
		tt.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton, X: 2 * m.UnitsPerCellWidth, Y: row})
		if got := tt.CursorShape(); got != hover {
			t.Errorf("movable %v: after the release, still over the tab, it shows %v, want %v", movable, got, hover)
		}
	}
}

// The open hand promises exactly what a press does: on every cell of the tab
// bar, for strips along each edge, both directions, narrow enough to scroll
// and with close buttons, a disabled tab among them (cut short, too), the hand shows where a
// press would pick a tab up and nowhere else. Each press is made on a fresh
// strip, so no press is judged by what an earlier one did.
func TestTheOpenHandShowsWhereAPressPicksUp(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	type layout struct {
		pos        TabPosition
		dir        core.Direction
		cols, rows int
		closable   bool
		offset     int // where the strip is scrolled to; -1 leaves it
		disabled   int // the tab that is not enabled
	}
	var layouts []layout
	for _, pos := range []TabPosition{TabsTop, TabsBottom, TabsSide, TabsSideOpposite} {
		for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
			for _, closable := range []bool{false, true} {
				if pos == TabsSide || pos == TabsSideOpposite {
					for _, disabled := range []int{1, 3} {
						layouts = append(layouts, layout{pos, dir, 40, 3, closable, -1, disabled})
					}
					continue
				}
				// Scrolled so the current tab (the third) stands whole, cut
				// short at the end, or first and cut short on a strip too
				// narrow to show it whole.
				for _, off := range []int{-1, 0, 1} {
					for _, cols := range []int{40, 12, 6} {
						for _, disabled := range []int{1, 3} {
							layouts = append(layouts, layout{pos, dir, cols, 4, closable, off, disabled})
						}
					}
				}
			}
		}
	}
	build := func(l layout) *TabTrinket {
		tt := longStrip(t, l.pos, l.dir, l.cols, l.rows)
		tt.SetClosable(l.closable)
		tt.SetTabEnabled(l.disabled, false)
		if l.offset >= 0 {
			tt.tabScrollOffset = l.offset
		}
		paintCloseGridW(t, tt, l.cols)
		return tt
	}
	grabs, presses := 0, 0
	clippedFirst, clippedLater, clippedDisabled, wholeDisabled := 0, 0, 0, 0
	for _, l := range layouts {
		probe := build(l)
		for _, sp := range probe.stripSpans {
			if sp.owner >= 0 && !probe.tabs[sp.owner].Enabled {
				if sp.clipped {
					clippedDisabled++
				} else {
					wholeDisabled++
				}
			}
			if sp.owner >= 0 && sp.clipped {
				if sp.owner == probe.tabScrollOffset {
					clippedFirst++
				} else {
					clippedLater++
				}
			}
		}
		m := probe.EffectiveCellMetrics()
		b := probe.Bounds()
		// Every row of a side strip; across the top or bottom, the first and
		// last rows, which are the bar and a row of the content either way.
		var ys []core.Unit
		for y := m.UnitsPerCellHeight / 2; y < b.Height; y += m.UnitsPerCellHeight {
			if probe.onSide() || y < m.UnitsPerCellHeight || y >= b.Height-m.UnitsPerCellHeight {
				ys = append(ys, y)
			}
		}
		for _, y := range ys {
			for x := m.UnitsPerCellWidth / 2; x < b.Width; x += m.UnitsPerCellWidth {
				promised := probe.grabbableAt(x, y)
				tt := build(l)
				tt.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: x, Y: y})
				picked := tt.carryingTab()
				if promised != picked {
					t.Fatalf("%+v at (%d,%d): the hand promises %v, the press picks up %v", l, x, y, promised, picked)
				}
				if promised {
					grabs++
				} else {
					presses++
				}
			}
		}
	}
	if grabs == 0 || presses == 0 || clippedFirst == 0 || clippedLater == 0 || clippedDisabled == 0 || wholeDisabled == 0 {
		t.Fatalf("the sweep found %d grabbable cells and %d others, a tab cut short %d times first, %d later and %d disabled, and a disabled one whole %d times; it misses a case",
			grabs, presses, clippedFirst, clippedLater, clippedDisabled, wholeDisabled)
	}
}
