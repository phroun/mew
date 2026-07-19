package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// Growing the list while scrolled down pulls the content back into the
// freed space (the scrollbar must not vanish leaving a stale blank) -
// the TreeView's resize re-clamp, for ListView.
func TestListResizeReclampsScroll(t *testing.T) {
	lv := NewListView()
	for i := 0; i < 30; i++ {
		lv.AddItem(NewListItem(fmtItem(i)))
	}
	lv.SetBounds(core.UnitRect{Width: 200, Height: 160})
	lv.scrollOffset = 30 - lv.visibleCount() // scrolled to the bottom
	if lv.scrollOffset <= 0 {
		t.Fatal("precondition: view smaller than the list")
	}
	lv.SetBounds(core.UnitRect{Width: 200, Height: 400})
	if want := 30 - lv.visibleCount(); lv.scrollOffset != want {
		t.Errorf("scrollOffset after grow = %d, want %d", lv.scrollOffset, want)
	}
	// Tall enough for everything: the offset snaps to 0.
	lv.SetBounds(core.UnitRect{Width: 200, Height: 640})
	if lv.scrollOffset != 0 {
		t.Errorf("scrollOffset with everything visible = %d, want 0", lv.scrollOffset)
	}
}

// Squeezing a view below its own chrome must never panic the paint
// path: a negative visible-row count reached make() ("makeslice: cap
// out of range"). Both views clamp to zero content rows instead.
func TestTinyBoundsPaintNoPanic(t *testing.T) {
	b, _ := raster.New(240, 64)
	core.SetTextMeasurer(b)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	tv := newColumnsTree(60, 10)
	tv.SetFitWidth(false) // header AND footer row reserved
	tv.SetBounds(core.UnitRect{Width: 200, Height: 8})
	if got := tv.visibleCount(); got != 0 {
		t.Fatalf("tree visibleCount = %d, want 0", got)
	}
	tv.Paint(core.NewPainter(b))

	lv := NewListView()
	lv.AddItem(NewListItem("x"))
	lv.SetBounds(core.UnitRect{Width: 200, Height: -16}) // layout squeeze
	if got := lv.visibleCount(); got != 0 {
		t.Fatalf("list visibleCount = %d, want 0", got)
	}
	lv.Paint(core.NewPainter(b))
}
