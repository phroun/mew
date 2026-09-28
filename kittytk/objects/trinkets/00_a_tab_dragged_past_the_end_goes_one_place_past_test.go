package trinkets

// A tab dragged past either end of the tabs in view goes to the place just past
// them, and no further: past that, the strip's own scrolling is how to reach.
// It is what lets a strip showing one tab, or part of one, still be reordered.

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// longStrip is eight tabs with the third current, on a strip cols cells wide,
// scrolled to show the current tab and painted.
func longStrip(t *testing.T, pos TabPosition, dir core.Direction, cols, rows int) *TabTrinket {
	t.Helper()
	core.SetTextMeasurer(nil)
	form := NewPanel()
	form.SetDirection(dir)
	tt := NewTabTrinket()
	tt.SetTabPosition(pos)
	tt.SetMovable(true)
	form.AddChild(tt)
	for _, n := range []string{"A", "Bravo", "C", "Delta", "E", "F", "G", "H"} {
		tt.AddTab(n, NewPanel())
	}
	tt.SetCurrentIndex(2)
	m := tt.EffectiveCellMetrics()
	tt.SetBounds(core.UnitRect{Width: core.Unit(cols) * m.UnitsPerCellWidth, Height: core.Unit(rows) * m.UnitsPerCellHeight})
	if tt.onSide() {
		tt.vertEnsureVisible(2)
	} else {
		tt.ensureTabFullyVisible(2)
	}
	paintCloseGridW(t, tt, cols)
	return tt
}

// dragger presses on the current tab and carries it, painting after every
// move as the display would.
type dragger struct {
	t      *testing.T
	tt     *TabTrinket
	y      core.Unit
	cols   int
	mirror bool
}

func (d *dragger) screen(runX core.Unit) core.Unit {
	if d.mirror {
		return d.tt.Bounds().Width - runX - 1
	}
	return runX
}

func (d *dragger) press(runX core.Unit) {
	d.t.Helper()
	d.tt.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: d.screen(runX), Y: d.y})
	if d.tt.dragTab == nil {
		d.t.Fatal("the press picked up nothing")
	}
	paintCloseGridW(d.t, d.tt, d.cols)
}

func (d *dragger) to(runX core.Unit) {
	d.tt.HandleMouseMove(core.MouseMoveEvent{X: d.screen(runX), Y: d.y, Buttons: 1})
	paintCloseGridW(d.t, d.tt, d.cols)
}

func (d *dragger) span(owner int) stripSpan {
	for _, sp := range d.tt.stripSpans {
		if sp.owner == owner {
			return sp
		}
	}
	d.t.Fatalf("no span for tab %d", owner)
	return stripSpan{}
}

func TestATabDraggedPastTheEndGoesOnePlacePast(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		// Sixteen cells: the overflow mark, C whole, Delta cut short, and the
		// scroll buttons.
		tt := longStrip(t, TabsTop, dir, 16, 4)
		first, last, _, hi, _ := tt.tabsInView()
		if first != 2 || last != 3 {
			t.Fatalf("%v: tabs %d..%d in view, want C and Delta", dir, first, last)
		}
		var moves [][2]int
		tt.SetOnTabMoved(func(from, to int) { moves = append(moves, [2]int{from, to}) })
		d := &dragger{t: t, tt: tt, y: 1, cols: 16, mirror: core.ChromeMirrored(tt)}
		cw := tt.EffectiveCellMetrics().UnitsPerCellWidth
		d.press(d.span(2).x + cw/2)

		// Onto the scroll buttons: C goes to the place just past the last in
		// view, out of view, and E closes up behind it. From the first unit
		// past the run: the scroll buttons start there.
		d.to(hi)
		if got := tabOrder(tt); got != "A Bravo Delta E C F G H" {
			t.Errorf("%v: past the trailing end: %q", dir, got)
		}
		// And no further, however far the pointer goes.
		for x := hi; x < 40*cw; x += cw {
			d.to(x)
		}
		if got := tabOrder(tt); got != "A Bravo Delta E C F G H" {
			t.Errorf("%v: further past the trailing end: %q", dir, got)
		}

		// Past the leading end -- the overflow mark, then off the strip -- it
		// goes just before the first tab in view.
		first, _, lo, _, _ := tt.tabsInView()
		d.to(lo - 1)
		if got, want := tt.TabText(first-1), "C"; got != want {
			t.Errorf("%v: past the leading end, %q stands before the first tab in view, want %q (order %q)",
				dir, got, want, tabOrder(tt))
		}
		order := tabOrder(tt)
		for x := lo - 1; x > -20*cw; x -= cw {
			d.to(x)
		}
		if got := tabOrder(tt); got != order {
			t.Errorf("%v: further past the leading end: %q, want %q", dir, got, order)
		}

		// Put down out of view, the strip comes to it, and the move is told
		// once.
		at := tt.CurrentIndex()
		tt.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton, X: d.screen(0), Y: 1})
		if tt.tabScrollOffset > at {
			t.Errorf("%v: put down at %d with the strip scrolled to %d", dir, at, tt.tabScrollOffset)
		}
		if len(moves) != 1 || moves[0] != [2]int{2, at} {
			t.Errorf("%v: told %v, want one move from 2 to %d", dir, moves, at)
		}
	}
}

// The pointer coming back over a tab lets the next trip past an end move the
// tab again -- to the place past the tabs in view then.
func TestComingBackOverATabLetsItGoPastAgain(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	tt := longStrip(t, TabsTop, core.DirLTR, 16, 4)
	d := &dragger{t: t, tt: tt, y: 1, cols: 16}
	cw := tt.EffectiveCellMetrics().UnitsPerCellWidth
	d.press(d.span(2).x + cw/2)
	_, _, lo, hi, _ := tt.tabsInView()
	d.to(hi + 1) // C just past the tabs in view, out of view
	d.to(lo + 1) // back over a tab in view
	if tt.dragPast != 0 {
		t.Errorf("back over a tab, the drag still holds that it went past (%d)", tt.dragPast)
	}
	d.to(lo - 1) // out past the leading end
	first, _, _, _, _ := tt.tabsInView()
	if got := tt.TabText(first - 1); got != "C" {
		t.Errorf("past the leading end on the second trip, %q stands before the tabs in view: %q", got, tabOrder(tt))
	}
}

// Twelve cells show the current tab only in part: there is no room to bring it
// further into view, so the press picks it up, and dragging past either end
// still reorders.
func TestATabShownOnlyInPartCanStillBeMoved(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	tt := longStrip(t, TabsTop, core.DirLTR, 12, 4)
	d := &dragger{t: t, tt: tt, y: 1, cols: 12}
	sp := d.span(2)
	if !sp.clipped {
		t.Fatal("the current tab is shown whole; the strip is not narrow enough for this test")
	}
	d.press(sp.x + 1)
	_, _, lo, hi, _ := tt.tabsInView()
	d.to(hi + 1)
	if got := tabOrder(tt); got != "A Bravo Delta C E F G H" {
		t.Errorf("past the trailing end: %q", got)
	}
	tt.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})

	tt = longStrip(t, TabsTop, core.DirLTR, 12, 4)
	d = &dragger{t: t, tt: tt, y: 1, cols: 12}
	d.press(d.span(2).x + 1)
	d.to(lo - 1)
	if got := tabOrder(tt); got != "A C Bravo Delta E F G H" {
		t.Errorf("past the leading end: %q", got)
	}
}

// Down a side strip the same: above the rows in view the tab goes to the row
// just before them, below them to the row just after, and no further.
func TestASideTabDraggedPastTheRowsGoesOneRowPast(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	tt := longStrip(t, TabsSide, core.DirLTR, 40, 2)
	m := tt.EffectiveCellMetrics()
	x := 2 * m.UnitsPerCellWidth
	off := tt.vertScrollOffset
	row := core.Unit(2-off)*m.UnitsPerCellHeight + m.UnitsPerCellHeight/2
	tt.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: x, Y: row})
	off = tt.vertScrollOffset

	for _, y := range []core.Unit{3 * m.UnitsPerCellHeight, 9 * m.UnitsPerCellHeight, 30 * m.UnitsPerCellHeight} {
		tt.HandleMouseMove(core.MouseMoveEvent{X: x, Y: y, Buttons: 1})
		if got, want := tt.CurrentIndex(), off+2; got != want {
			t.Errorf("below the rows at y=%d the tab stands at %d, want %d (%q)", y, got, want, tabOrder(tt))
		}
	}
	// Back over a row in view before letting go, it comes back into view.
	tt.HandleMouseMove(core.MouseMoveEvent{X: x, Y: m.UnitsPerCellHeight / 2, Buttons: 1})
	if got := tt.CurrentIndex(); got != off {
		t.Errorf("back over the first row the tab stands at %d, want %d (%q)", got, off, tabOrder(tt))
	}
	for _, y := range []core.Unit{-1, -5 * m.UnitsPerCellHeight} {
		tt.HandleMouseMove(core.MouseMoveEvent{X: x, Y: y, Buttons: 1})
		if got, want := tt.CurrentIndex(), off-1; got != want {
			t.Errorf("above the rows at y=%d the tab stands at %d, want %d (%q)", y, got, want, tabOrder(tt))
		}
	}
	tt.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})
	if cur := tt.CurrentIndex(); cur < tt.vertScrollOffset || cur >= tt.vertScrollOffset+2 {
		t.Errorf("put down at %d with rows %d..%d in view", cur, tt.vertScrollOffset, tt.vertScrollOffset+1)
	}
}

// A tab carried out of view is not lost: bringing the pointer back over a tab
// in view, before letting go, brings it back to that tab's place.
func TestATabCarriedOutOfViewComesBack(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		for _, trailing := range []bool{true, false} {
			tt := longStrip(t, TabsTop, dir, 16, 4)
			d := &dragger{t: t, tt: tt, y: 1, cols: 16, mirror: core.ChromeMirrored(tt)}
			cw := tt.EffectiveCellMetrics().UnitsPerCellWidth
			d.press(d.span(2).x + cw/2)
			first, last, lo, hi, _ := tt.tabsInView()
			if trailing {
				d.to(hi)
			} else {
				d.to(lo - 1)
			}
			first, last, _, _, _ = tt.tabsInView()
			if cur := tt.CurrentIndex(); cur >= first && cur <= last {
				t.Fatalf("%v trailing=%v: the tab is still in view at %d", dir, trailing, cur)
			}
			// Back over the tab at the end it went out of.
			back := d.span(last)
			if !trailing {
				back = d.span(first)
			}
			d.to(back.x + 1)
			first, last, _, _, _ = tt.tabsInView()
			if cur := tt.CurrentIndex(); cur < first || cur > last {
				t.Errorf("%v trailing=%v: back over a tab in view, the tab stands at %d with %d..%d in view (%q)",
					dir, trailing, cur, first, last, tabOrder(tt))
			}
		}
	}
}
