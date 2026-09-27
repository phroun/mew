package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// scrollIntoViewBar builds an overflowing menu bar of the given width on a
// pixel surface, so the bar carries its frame indent -- which is the term the
// scroll-into-view arithmetic used to be missing.
func scrollIntoViewBar(t *testing.T, width core.Unit) *MenuBar {
	t.Helper()
	b, err := raster.NewScaled(int(width), 200, 1)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)

	d := NewDesktop()
	d.SetBackend(b)
	d.SetBounds(core.UnitRect{Width: width, Height: 200})
	bar := NewMenuBar()
	d.AddChild(bar)
	for _, title := range []string{"File", "Edit", "View", "Insert", "Format", "Tools", "Window", "Help"} {
		bar.AddMenu(NewMenu(title))
	}
	bar.SetBounds(core.UnitRect{Width: width, Height: bar.menuMetrics().RowH})
	return bar
}

// titleFits reports whether a title lies wholly within the run at the bar's
// current scroll offset, asked in the terms the bar paints and hit-tests with.
func titleFits(m *MenuBar, index int) bool {
	return m.calculateMenuX(index)+m.menuTitleWidth(m.menus[index].title) <= m.menusRightLimit()
}

// Scrolling a menu into view puts it where the bar can actually draw and hit
// it, at every bar width.
//
// The run starts at the frame indent and ends at the scroll buttons' left
// edge. Measuring the fit from zero instead granted it the indent's worth of
// room that is not there, so at the widths where the boundary fell inside that
// margin the bar stopped scrolling with the title still clipped -- and a
// keystroke that named a menu off the right of the bar brought it only nearly
// into view.
func TestEnsureMenuVisibleAgreesWithThePaintedRun(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	for width := core.Unit(300); width <= 340; width++ {
		bar := scrollIntoViewBar(t, width)
		if !bar.menusNeedScrolling() {
			continue
		}
		for i := range bar.menus {
			// The best any offset can do is to make this title the first
			// one shown; if it fits there it must fit once scrolled to.
			bar.scrollOffset = i
			if !titleFits(bar, i) {
				continue // too wide for the run at any offset
			}

			bar.scrollOffset = 0
			bar.ensureMenuVisible(i)
			if !titleFits(bar, i) {
				t.Errorf("width %d: after scrolling menu %d (%q) into view at offset %d it still runs from %d to %d, past the run's %d",
					width, i, bar.menus[i].title, bar.scrollOffset,
					bar.calculateMenuX(i),
					bar.calculateMenuX(i)+bar.menuTitleWidth(bar.menus[i].title),
					bar.menusRightLimit())
			}
		}
	}
}

// And it gives up no more of the left than it has to: one menu further would
// have shown the title just as whole.
func TestEnsureMenuVisibleScrollsTheLeastItCan(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	for width := core.Unit(300); width <= 340; width++ {
		bar := scrollIntoViewBar(t, width)
		if !bar.menusNeedScrolling() {
			continue
		}
		for i := range bar.menus {
			bar.scrollOffset = 0
			bar.ensureMenuVisible(i)
			got := bar.scrollOffset
			if got == 0 {
				continue
			}
			bar.scrollOffset = got - 1
			if titleFits(bar, i) {
				t.Errorf("width %d: menu %d (%q) scrolled to offset %d, but offset %d already showed it whole",
					width, i, bar.menus[i].title, got, got-1)
			}
		}
	}
}

// A menu to the LEFT of the run becomes the first one shown, which is the
// least scrolling that can reach it.
func TestEnsureMenuVisibleScrollsBackLeft(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	bar := scrollIntoViewBar(t, 320)
	if !bar.menusNeedScrolling() {
		t.Fatal("precondition: bar should overflow")
	}
	bar.scrollOffset = 4
	bar.ensureMenuVisible(1)
	if bar.scrollOffset != 1 {
		t.Errorf("scrolling back to menu 1 landed at offset %d, want 1", bar.scrollOffset)
	}
}
