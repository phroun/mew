package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// hoverScrollBar builds an overflowing menu bar on a pixel surface and paints
// it once, since the hover highlight is a pointer affordance the bar only
// tracks once it knows it is on a surface with a pointer.
func hoverScrollBar(t *testing.T) (*MenuBar, *raster.Backend) {
	t.Helper()
	b, err := raster.NewScaled(400, 200, 1)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)

	d := NewDesktop()
	d.SetBackend(b)
	d.SetBounds(core.UnitRect{Width: 400, Height: 200})
	bar := NewMenuBar()
	d.AddChild(bar)
	for _, title := range []string{"File", "Edit", "View", "Insert", "Format", "Tools", "Window", "Help"} {
		bar.AddMenu(NewMenu(title))
	}
	bar.SetBounds(core.UnitRect{Width: 400, Height: bar.menuMetrics().RowH})
	b.Clear(style.DefaultStyle())
	bar.Paint(core.NewPainter(b))
	if !bar.menusNeedScrolling() {
		t.Fatal("precondition: the bar should overflow")
	}
	return bar, b
}

// Scrolling the bar answers the pointer again: the highlight follows the title
// that is under the pointer NOW, not the one that was there before the run
// moved.
//
// Hover is the answer to "which title is under the pointer", and a scroll
// changes that answer with no pointer movement and so no event to prompt a
// fresh one. Clicking the clipped title at the end of the run scrolls it into
// view, which slides a DIFFERENT title under the very pointer that clicked --
// and the highlight stayed behind on the one that had been there.
func TestMenuBarHoverFollowsAScroll(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	bar, _ := hoverScrollBar(t)

	// The last unit of the run, which is the title clipped by the right end.
	x := bar.menusRightLimit() - 1
	bar.HandleMouseMove(core.MouseMoveEvent{X: x, Y: 0})
	before := bar.hoverIndex
	if before < 0 {
		t.Fatalf("precondition: x=%d should be over a title, hover is %d", x, before)
	}

	// Clicking it opens it, and opening it scrolls it into view.
	if !bar.HandleMousePress(core.MousePressEvent{X: x, Y: 0, Button: core.LeftButton}) {
		t.Fatal("press on the clipped title not consumed")
	}
	if bar.scrollOffset == 0 {
		t.Fatal("precondition: opening the clipped title should have scrolled the bar")
	}

	want := bar.menuItemAt(x, 0)
	if want == before {
		t.Fatalf("precondition: the scroll should have put a different title under x=%d", x)
	}
	if bar.hoverIndex != want {
		t.Errorf("after scrolling, x=%d is over menu %d (%q) but the highlight is on %d",
			x, want, bar.menus[want].title, bar.hoverIndex)
	}
}

// Every other way the run moves, on the same terms: the wheel, a menu scrolled
// into view by something the hand did elsewhere, and the bar re-fitting itself
// when its window is resized under a pointer that stayed put.
func TestMenuBarHoverFollowsEveryScroll(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	for _, c := range []struct {
		name string
		move func(*MenuBar, *raster.Backend)
	}{
		{"the wheel", func(m *MenuBar, _ *raster.Backend) {
			m.HandleMouseWheel(core.MouseWheelEvent{DeltaY: 1})
			m.HandleMouseWheel(core.MouseWheelEvent{DeltaY: 1})
		}},
		{"a menu scrolled into view", func(m *MenuBar, _ *raster.Backend) {
			m.OpenMenu(len(m.menus) - 1)
		}},
		{"the bar widened under the pointer", func(m *MenuBar, b *raster.Backend) {
			// Scrolled to the end, then given room for everything: the next
			// paint re-fits the run and the offset falls back.
			for i := 0; i < 6; i++ {
				m.HandleMouseWheel(core.MouseWheelEvent{DeltaY: 1})
			}
			if m.scrollOffset == 0 {
				t.Fatal("precondition: the wheel should have scrolled the bar")
			}
			m.SetBounds(core.UnitRect{Width: 4000, Height: m.menuMetrics().RowH})
			m.Paint(core.NewPainter(b))
			if m.scrollOffset != 0 {
				t.Fatalf("precondition: a bar with room for every title should sit at offset 0, not %d",
					m.scrollOffset)
			}
		}},
	} {
		bar, b := hoverScrollBar(t)
		x := bar.menusRightLimit() - 1
		bar.HandleMouseMove(core.MouseMoveEvent{X: x, Y: 0})
		if bar.hoverIndex < 0 {
			t.Fatalf("%s: precondition -- x=%d should be over a title", c.name, x)
		}

		c.move(bar, b)
		if want := bar.menuItemAt(x, 0); bar.hoverIndex != want {
			t.Errorf("%s: x=%d is over menu %d, the highlight is on %d",
				c.name, x, want, bar.hoverIndex)
		}
	}
}

// A press is a pointer position too, so a bar that has seen no movement at all
// still answers for where the pointer is once the press scrolls it. Pressing
// [>] puts the pointer on [>], so what is highlighted is the button and no
// title -- however far the run moved out from under it.
func TestMenuBarHoverFollowsAScrollWithNoPriorMove(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	bar, _ := hoverScrollBar(t)

	buttonWidth := bar.scrollButtonWidth()
	rightButton := bar.Bounds().Width - bar.dateTimeWidth() - buttonWidth + buttonWidth/2
	bar.HandleMousePress(core.MousePressEvent{X: rightButton, Y: 0, Button: core.LeftButton})

	// The pointer is over [>], which is not a title.
	if bar.hoverIndex != -1 {
		t.Errorf("the pointer is over [>], but the highlight is on menu %d", bar.hoverIndex)
	}
	if bar.hoverScrollBtn != 1 {
		t.Errorf("the pointer is over [>], but hoverScrollBtn = %d", bar.hoverScrollBtn)
	}
}

// A blocked bar highlights nothing whatever is under it, and a scroll does not
// hand it a highlight the pointer could not have given it.
func TestMenuBarScrollLeavesABlockedBarUnhighlighted(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	bar, _ := hoverScrollBar(t)
	x := bar.menusRightLimit() - 1
	bar.HandleMouseMove(core.MouseMoveEvent{X: x, Y: 0})
	bar.SetModalBlockedChecker(func() bool { return true })
	bar.HandleMouseMove(core.MouseMoveEvent{X: x, Y: 0})
	if bar.hoverIndex != -1 {
		t.Fatalf("precondition: a blocked bar should hover nothing, got %d", bar.hoverIndex)
	}

	bar.HandleMouseWheel(core.MouseWheelEvent{DeltaY: 1})
	if bar.scrollOffset == 0 {
		t.Fatal("precondition: the wheel should have scrolled the bar")
	}
	if bar.hoverIndex != -1 {
		t.Errorf("a blocked bar highlighted menu %d after a scroll", bar.hoverIndex)
	}
}
