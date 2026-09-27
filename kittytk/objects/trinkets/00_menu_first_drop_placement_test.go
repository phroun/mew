package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// A menu is placed by its SIZE, and a menu that fills or relabels itself in
// its about-to-show handler decides its size there.
//
// The handler fired inside Show, which every opener calls AFTER measuring, so
// each placement was made against the contents of the previous opening. The
// first drop of a menu near the surface's right edge was placed as if it were
// still whatever width it had been built at, and the second drop was right
// only because the handler had by then left the right items behind -- which
// is exactly the shape of the report: wrong the first time, fine after.
func TestMenuBarPlacesAMenuByWhatItWillShow(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.NewScaled(640, 400, 1)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)

	build := func() (*MenuBar, *Menu) {
		d := NewDesktop()
		d.SetBackend(b)
		// Narrow, so the last menu's title sits where a wide dropdown runs
		// past the right edge and a narrow one does not -- which is the
		// decision being tested.
		d.SetBounds(core.UnitRect{Width: 240, Height: 400})
		bar := NewMenuBar()
		d.AddChild(bar)
		bar.SetBounds(core.UnitRect{Width: 240, Height: 16})

		// Enough menus that the last one's title sits near the right edge.
		for _, title := range []string{"File", "Edit", "View", "History"} {
			bar.AddMenu(NewMenu(title))
		}
		last := bar.MenuAt(len(bar.Menus()) - 1)
		// Built empty and filled on the way up, the way a menu that tracks
		// live state is. Its width is not knowable until the handler runs.
		last.SetOnAboutToShow(func() {
			if len(last.Items()) > 0 {
				return
			}
			last.AddItem(NewMenuItem("Prior History Item, and then some more"))
			last.AddItem(NewMenuItem("Next History Item, and then some more"))
		})
		return bar, last
	}

	// What the bar SHOULD do: where it settles once the menu's contents are
	// no longer news to it. Taken from a SECOND opening on a bar of its own,
	// so the reference does not come through the path under test.
	barRef, menuRef := build()
	barRef.OpenMenu(len(barRef.Menus()) - 1)
	barRef.CloseMenu()
	barRef.OpenMenu(len(barRef.Menus()) - 1)
	want := menuRef.DropdownBounds().X
	barRef.CloseMenu()

	// The first drop of a fresh bar has to agree with it.
	bar, menu := build()
	bar.OpenMenu(len(bar.Menus()) - 1)
	first := menu.DropdownBounds().X
	if first != want {
		t.Errorf("first drop placed at X=%d; a populated menu belongs at X=%d", first, want)
	}

	// And the second drop must not move it.
	bar.CloseMenu()
	bar.OpenMenu(len(bar.Menus()) - 1)
	if second := menu.DropdownBounds().X; second != first {
		t.Errorf("second drop moved the menu from X=%d to X=%d", first, second)
	}
}
