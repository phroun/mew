package trinkets

import (
	"fmt"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// Dragging the splitter divider preserves the grab point within the
// band instead of snapping the divider's leading edge to the pointer.
func TestSplitterDragAnchorsToGrabOffset(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	sp := NewSplitter(core.Horizontal)
	sp.AddChild(NewPanel())
	sp.AddChild(NewPanel())
	win := window.NewWindow("host")
	win.SetContent(sp)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 400, Height: 200})
	win.Layout()

	before := sp.dividerBounds()
	// Grab 5 units into the 8-unit divider band and drag 100 right:
	// the divider's left edge must land exactly 100 further, not at
	// the pointer.
	sp.HandleMousePress(core.MousePressEvent{X: before.X + 5, Y: 10, Button: core.LeftButton})
	if !sp.IsDragging() {
		t.Fatal("press on divider did not start drag")
	}
	sp.HandleMouseMove(core.MouseMoveEvent{X: before.X + 5 + 100, Y: 10})
	after := sp.dividerBounds()
	if after.X != before.X+100 {
		t.Errorf("divider at %d after drag; want %d (grab offset preserved)", after.X, before.X+100)
	}
	sp.HandleMouseRelease(core.MouseReleaseEvent{X: before.X + 5 + 100, Y: 10, Button: core.LeftButton})
	if sp.IsDragging() {
		t.Error("release did not end divider drag")
	}
}

// A mouse release must reach the trinket that owns the drag even when a
// sibling earlier in the container's forwarding order also receives it.
// (ListView used to consume every release unconditionally, so a
// TreeView scrub in the second splitter pane never ended.)
func TestReleaseReachesSecondPaneAfterListView(t *testing.T) {
	sp := NewSplitter(core.Horizontal)
	lv := NewListView()
	for i := 0; i < 5; i++ {
		lv.AddItem(NewListItem(fmt.Sprintf("item %d", i)))
	}
	tv := NewTreeView()
	for i := 0; i < 5; i++ {
		tv.AddRootItem(NewTreeItem(fmt.Sprintf("node %d", i)))
	}
	sp.SetFirst(lv)
	sp.SetSecond(tv)
	sp.SetBounds(core.UnitRect{Width: 400, Height: 200})
	sp.Layout()

	_, second := sp.childBounds()
	sp.HandleMousePress(core.MousePressEvent{X: second.X + 10, Y: 8, Button: core.LeftButton})
	if !tv.isDragging {
		t.Fatal("press in second pane did not start treeview scrub")
	}
	if !sp.HandleMouseRelease(core.MouseReleaseEvent{X: second.X + 10, Y: 8, Button: core.LeftButton}) {
		t.Error("release not handled by splitter tree")
	}
	if tv.isDragging {
		t.Error("treeview scrub survived the release (a sibling starved the event)")
	}
	if lv.isDragging || lv.scrollbarDragging {
		t.Error("listview invented drag state from a release it did not own")
	}
}

// On pixel surfaces the ListView scrollbar thumb tracks the pointer at
// unit granularity while the scroll offset snaps to whole rows.
func TestListViewSmoothScrollbarDrag(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	lv := NewListView()
	for i := 0; i < 20; i++ {
		lv.AddItem(NewListItem(fmt.Sprintf("item %d", i)))
	}
	win := window.NewWindow("host")
	win.SetContent(lv)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 300, Height: 200})
	win.Layout()

	bounds := lv.Bounds()
	visible := lv.visibleCount()
	if visible <= 0 || len(lv.items) <= visible {
		t.Fatalf("harness: %d visible of %d items - no scrollbar", visible, len(lv.items))
	}
	trackU, thumbU, posU := lv.scrollbarUnits(visible)
	scrollable := trackU - thumbU
	if scrollable <= 0 {
		t.Fatal("harness: no scrollable track")
	}

	// Grab 4 units into the thumb.
	grabY := core.Unit(posU) + 4
	if !lv.HandleMousePress(core.MousePressEvent{X: bounds.Width - 2, Y: grabY, Button: core.LeftButton}) {
		t.Fatal("press on thumb not consumed")
	}
	if !lv.scrollbarDragging || !lv.smoothScrollbarDrag {
		t.Fatal("press on thumb did not start a smooth scrollbar drag")
	}
	lv.SetFocus() // moves are gated on focus

	// Drag 30 units down: thumb origin follows exactly; the offset
	// snaps to the nearest whole row.
	lv.HandleMouseMove(core.MouseMoveEvent{X: bounds.Width - 2, Y: grabY + 30})
	wantPos := posU + 30
	if wantPos > scrollable {
		wantPos = scrollable
	}
	if lv.scrollbarThumbPos != wantPos {
		t.Errorf("smooth thumb pos = %v, want %v", lv.scrollbarThumbPos, wantPos)
	}
	maxScroll := len(lv.items) - visible
	wantOffset := int(wantPos*float64(maxScroll)/scrollable + 0.5)
	if lv.scrollOffset != wantOffset {
		t.Errorf("scroll offset = %d, want snapped %d", lv.scrollOffset, wantOffset)
	}

	lv.HandleMouseRelease(core.MouseReleaseEvent{X: bounds.Width - 2, Y: grabY + 30, Button: core.LeftButton})
	if lv.scrollbarDragging || lv.smoothScrollbarDrag {
		t.Error("release did not end the scrollbar drag")
	}
}

// The standalone ScrollBar trinket gets the same treatment: smooth
// thumb, values snapped to whole steps.
func TestScrollBarSmoothDragOnPixelSurfaces(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	sb := NewScrollBar(core.Vertical)
	sb.SetRange(0, 10)
	win := window.NewWindow("host")
	win.SetContent(sb)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 100, Height: 200})
	win.Layout()

	bounds := sb.Bounds()
	metrics := sb.EffectiveCellMetrics()
	trackU, thumbU, posU, ok := sb.thumbSpanUnits(bounds, metrics)
	if !ok {
		t.Fatal("harness: no thumb geometry")
	}
	scrollable := trackU - thumbU
	if scrollable <= 0 {
		t.Fatal("harness: no scrollable track")
	}

	grabY := core.Unit(posU) + 4
	if !sb.HandleMousePress(core.MousePressEvent{X: 2, Y: grabY, Button: core.LeftButton}) {
		t.Fatal("press on thumb not consumed")
	}
	if !sb.dragging || !sb.smoothDrag {
		t.Fatal("press on thumb did not start a smooth drag")
	}

	// Drag halfway down the scrollable range.
	delta := core.Unit(scrollable / 2)
	sb.HandleMouseMove(core.MouseMoveEvent{X: 2, Y: grabY + delta})
	wantPos := posU + float64(delta)
	if sb.smoothThumbPos != wantPos {
		t.Errorf("smooth thumb pos = %v, want %v", sb.smoothThumbPos, wantPos)
	}
	wantValue := int(wantPos*10/scrollable + 0.5)
	if sb.Value() != wantValue {
		t.Errorf("value = %d, want snapped %d", sb.Value(), wantValue)
	}

	sb.HandleMouseRelease(core.MouseReleaseEvent{X: 2, Y: grabY + delta, Button: core.LeftButton})
	if sb.dragging || sb.smoothDrag {
		t.Error("release did not end the drag")
	}
}

// PurfecTerm graphical scrollbars: the thumb follows the pointer
// smoothly with the grab point anchored, while the buffer's scroll
// offset snaps to whole lines.
func TestGfxScrollbarSmoothThumbSnappedContent(t *testing.T) {
	_, _, term, _ := gfxHarness(t)
	buf := term.Terminal().Buffer()
	for i := 0; i < 100; i++ {
		term.Feed([]byte("line\r\n"))
	}
	if buf.GetMaxScrollOffset() <= 0 {
		t.Skip("no scrollback accumulated")
	}

	bounds := term.Bounds()
	track, thumb, upper, page, _, ok := term.vScrollGeometry(bounds)
	if !ok {
		t.Fatal("no vertical scrollbar")
	}
	span := float64(track.Height - thumb.Height)
	if span <= 0 {
		t.Fatal("harness: no scrollable track")
	}

	// Grab 3 units into the thumb (it starts at the track bottom:
	// offset 0 = newest content), then drag 13 units up.
	pressY := thumb.Y + 3
	if !term.scrollbarPress(core.MousePressEvent{X: track.X + 1, Y: pressY, Button: core.LeftButton}) {
		t.Fatal("press in scrollbar lane not consumed")
	}
	term.scrollbarDragTo(track.X+1, pressY-13)

	wantPos := float64(thumb.Y - 13)
	if term.gfx.vThumbPos != wantPos {
		t.Errorf("smooth thumb pos = %v, want %v (grab offset preserved)", term.gfx.vThumbPos, wantPos)
	}
	// The painted thumb uses the smooth position mid-drag.
	_, thumbNow, _, _, _, _ := term.vScrollGeometry(bounds)
	if thumbNow.Y != core.Unit(wantPos+0.5) {
		t.Errorf("mid-drag thumb painted at %d, want smooth %d", thumbNow.Y, core.Unit(wantPos+0.5))
	}
	// The content offset snapped to the whole line nearest that
	// position.
	wantValue := int(wantPos*float64(upper-page)/span + 0.5)
	if got := buf.GetMaxScrollOffset() - buf.GetScrollOffset(); got != wantValue {
		t.Errorf("content value = %d, want snapped %d", got, wantValue)
	}

	term.HandleMouseRelease(core.MouseReleaseEvent{X: track.X + 1, Y: pressY - 13, Button: core.LeftButton})
	if term.gfx.vDragging {
		t.Error("release did not end the scrollbar drag")
	}
}

// Horizontal scrollbar lanes are one column width tall on pixel
// surfaces - the same thickness as vertical lanes - and a full row
// on cell surfaces.
func TestHorizontalScrollBarLaneThinOnPixelSurfaces(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	host := func(d *Desktop) *ScrollArea {
		sa := NewScrollArea()
		win := window.NewWindow("host")
		win.SetContent(sa)
		d.WindowManager().AddWindow(win)
		win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 300, Height: 200})
		win.Layout()
		// Wide, short content: horizontal bar only.
		sa.contentWidth = 10000
		sa.contentHeight = 10
		return sa
	}

	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)
	sa := host(d)
	if lane := sa.Bounds().Height - sa.viewportBounds().Height; lane != 8 {
		t.Errorf("pixel surface horizontal lane = %d units, want 8 (one column)", lane)
	}

	dc := NewDesktop()
	dc.SetBackend(&nullBackend{})
	sac := host(dc)
	if lane := sac.Bounds().Height - sac.viewportBounds().Height; lane != 16 {
		t.Errorf("cell surface horizontal lane = %d units, want 16 (one row)", lane)
	}
}

// tallStub is a bare trinket with a fixed, tall size hint.
type tallStub struct{ core.TrinketBase }

func (t *tallStub) SizeHint() core.UnitSize { return core.UnitSize{Width: 100, Height: 2000} }

func newTallStub() *tallStub {
	s := &tallStub{}
	s.TrinketBase = *core.NewTrinketBase()
	s.Init(s)
	return s
}

// ScrollArea content scrolls at unit granularity on pixel surfaces:
// the scrollbars are unit-denominated, so offsets need not land on
// cell boundaries. (Content that wants quantized output - the
// terminal - snaps itself; the scroll area does not impose it.)
func TestScrollAreaSmoothContentOffsets(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	sa := NewScrollArea()
	sa.SetContent(newTallStub()) // tall content: vertical scrolling only
	win := window.NewWindow("host")
	win.SetContent(sa)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 300, Height: 200})
	win.Layout()
	sa.updateScrollBars()

	viewport := sa.viewportBounds()
	if got, want := sa.vScrollBar.Maximum(), int(2000-viewport.Height); got != want {
		t.Errorf("smooth vertical range max = %d, want %d (unit-denominated)", got, want)
	}
	sa.SetScrollY(13) // not a multiple of any cell dimension
	_, offY := sa.scrollOffsetUnits()
	if offY != 13 {
		t.Errorf("content offset = %d units, want 13 (unquantized)", offY)
	}

	// Cell surface: ranges stay cell-denominated and offsets are
	// whole rows.
	dc := NewDesktop()
	dc.SetBackend(&nullBackend{})
	sac := NewScrollArea()
	sac.SetContent(newTallStub())
	winc := window.NewWindow("host")
	winc.SetContent(sac)
	dc.WindowManager().AddWindow(winc)
	winc.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 300, Height: 200})
	winc.Layout()
	sac.updateScrollBars()
	sac.SetScrollY(3)
	_, offYC := sac.scrollOffsetUnits()
	if offYC != 3*16 {
		t.Errorf("cell surface offset = %d units, want 48 (three rows)", offYC)
	}
}

// tallPanel is a Panel with a fixed tall size hint.
type tallPanel struct{ Panel }

func (t *tallPanel) SizeHint() core.UnitSize { return core.UnitSize{Width: 100, Height: 2000} }

// Mid-gesture, a child scrolling under the pointer must NOT steal
// the wheel: the latched handler is the scroll area's self-scroll,
// which never re-routes to content.
func TestWheelGestureStaysOnScrollArea(t *testing.T) {
	t.Cleanup(func() {
		core.SetTextMeasurer(nil)
		core.ResetWheelGesture()
	})
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	// Tall panel content holding a list further down the panel.
	panel := &tallPanel{Panel: *NewPanel()}
	panel.Init(panel)
	lv := NewListView()
	for i := 0; i < 40; i++ {
		lv.AddItem(NewListItem(fmt.Sprintf("item %d", i)))
	}
	panel.AddChild(lv)
	sa := NewScrollArea()
	sa.SetContent(panel)
	win := window.NewWindow("host")
	win.SetContent(sa)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 300, Height: 200})
	win.Layout()
	sa.updateScrollBars()
	panel.SetBounds(core.UnitRect{Width: 100, Height: 2000})
	lv.SetBounds(core.UnitRect{X: 0, Y: 300, Width: 100, Height: 320})

	// Gesture starts over plain panel area (Y=100 local; list is at 300+).
	ev := core.MouseWheelEvent{X: 50, Y: 100, ScreenX: 50, ScreenY: 100, DeltaY: 1}
	if !sa.HandleMouseWheel(ev) {
		t.Fatal("scroll area did not consume the first wheel event")
	}
	firstScroll := sa.scrollY
	if firstScroll == 0 {
		t.Fatal("scroll area did not scroll")
	}

	// Content scrolled: the list may now sit under the pointer. The
	// latched delivery must keep scrolling the AREA, not the list.
	lvBefore := lv.scrollOffset
	next := core.MouseWheelEvent{ScreenX: 50, ScreenY: 100, DeltaY: 1}
	if !core.DeliverLatchedWheel(next) {
		t.Fatal("gesture not latched to the scroll area")
	}
	if lv.scrollOffset != lvBefore {
		t.Error("child list stole the latched wheel event")
	}
	if sa.scrollY <= firstScroll {
		t.Errorf("scroll area did not keep scrolling (was %d, now %d)", firstScroll, sa.scrollY)
	}
}
