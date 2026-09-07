package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// chooserTree has optional columns, so the header carries the [=] button, and
// a popup controller to catch the menu it opens.
// The tree stands well inside the screen, so a menu that hangs off the wrong
// corner has room to be wrong rather than being clamped back into place.
func chooserTree(t *testing.T, dir core.Direction) (*TreeView, *offsetPopupController) {
	t.Helper()
	tv := newColumnsTree(40, 10)
	tv.SetDirection(dir)
	tv.SetShowHeader(true)
	for _, c := range tv.Columns() {
		c.Optional = true
	}
	host := &offsetPopupController{dx: 200, dy: 100}
	parent := NewPanel()
	parent.SetPopupController(host)
	tv.SetParent(parent)
	tv.SetBounds(core.UnitRect{Width: 40 * 8, Height: 10 * 16})
	return tv, host
}

// The column chooser stands over the scrollbar's lane, which is at the
// trailing edge -- so it moves to the other corner of the header with it.
func TestTheChooserStandsOverTheLane(t *testing.T) {
	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		tv, _ := chooserTree(t, dir)
		r, ok := tv.chooserButtonRect()
		if !ok {
			t.Fatalf("%v: a header with optional columns drew no chooser", dir)
		}
		lane := tv.laneX()
		if r.X > lane || lane >= r.X+r.Width {
			t.Errorf("%v: the chooser at [%d,%d) is not over the lane at %d",
				dir, r.X, r.X+r.Width, lane)
		}
		// The header takes a press there, and it opens the chooser.
		if !tv.handleMultiPress(core.MousePressEvent{
			Button: core.LeftButton, X: r.X + r.Width/2, Y: 0,
		}) || !tv.chooserOpen {
			t.Errorf("%v: a press on the chooser at %d did not open it", dir, r.X+r.Width/2)
		}
		tv.closeColumnChooser()
	}
}

// The menu hangs from the button's leading corner, opening across the header
// instead of off the edge the button stands against.
func TestTheChooserMenuOpensAcrossTheHeader(t *testing.T) {
	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		tv, host := chooserTree(t, dir)
		btn, _ := tv.chooserButtonRect()
		tv.openColumnChooser(false)
		if host.popup == nil {
			t.Fatalf("%v: opening the chooser registered no menu", dir)
		}
		m := host.popup.Bounds
		screen := host.ScreenBounds()
		btnX := btn.X + host.dx // the button, where the popup layer sees it
		if core.ChromeMirrored(tv) {
			if m.X != btnX {
				t.Errorf("%v: the menu opens at %d, want the button's near corner %d",
					dir, m.X, btnX)
			}
		} else if got, want := m.X+m.Width, btnX+btn.Width; got != want {
			t.Errorf("%v: the menu ends at %d, want the button's far corner %d", dir, got, want)
		}
		if m.X < screen.X || m.X+m.Width > screen.X+screen.Width {
			t.Errorf("%v: the menu opens off the screen at [%d,%d)", dir, m.X, m.X+m.Width)
		}
		tv.closeColumnChooser()
	}
}
