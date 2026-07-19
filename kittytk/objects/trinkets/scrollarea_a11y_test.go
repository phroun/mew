package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

type sizedTrinket struct {
	core.TrinketBase
	hint core.UnitSize
}

func (w *sizedTrinket) SizeHint() core.UnitSize { return w.hint }

func newSized(w, h core.Unit) *sizedTrinket {
	s := &sizedTrinket{hint: core.UnitSize{Width: w, Height: h}}
	s.TrinketBase = *core.NewTrinketBase()
	return s
}

// A focused scroll area announces the thumb position of each visible
// scrollbar as a rounded percentage; 0% at start, 100% at end.
func TestScrollAreaAnnouncesScrollPercentages(t *testing.T) {
	sa := NewScrollArea()
	sa.SetContent(newSized(2000, 2000))
	sa.SetBounds(core.UnitRect{Width: 200, Height: 200})
	sa.updateScrollBars()

	if got := sa.AccessibleInfo().Name; got != "scroll area, horizontal 0 percent, vertical 0 percent" {
		t.Errorf("at origin: %q", got)
	}

	sa.SetScrollX(sa.hScrollBar.Maximum())
	sa.SetScrollY(sa.vScrollBar.Maximum())
	if got := sa.AccessibleInfo().Name; got != "scroll area, horizontal 100 percent, vertical 100 percent" {
		t.Errorf("at end: %q", got)
	}
}

// Only the direction whose scrollbar is actually visible is announced.
func TestScrollAreaAnnouncesOnlyVisibleDirections(t *testing.T) {
	sa := NewScrollArea()
	sa.SetContent(newSized(2000, 8)) // wide, not tall
	sa.SetBounds(core.UnitRect{Width: 200, Height: 200})
	sa.updateScrollBars()

	got := sa.AccessibleInfo().Name
	if !strings.HasPrefix(got, "scroll area, horizontal ") {
		t.Errorf("wide-only announcement = %q, want a horizontal-only phrase", got)
	}
	if strings.Contains(got, "vertical") {
		t.Errorf("wide-only announcement should omit vertical: %q", got)
	}
}

// A scroll area with no overflow announces just "scroll area".
func TestScrollAreaNoScrollbars(t *testing.T) {
	sa := NewScrollArea()
	sa.SetContent(newSized(10, 10))
	sa.SetBounds(core.UnitRect{Width: 200, Height: 200})
	sa.updateScrollBars()

	if got := sa.AccessibleInfo().Name; got != "scroll area" {
		t.Errorf("no-overflow announcement = %q, want \"scroll area\"", got)
	}
}
