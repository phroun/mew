package window

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/platform"
)

// A torn-off window composites like the desktop does: its own surface at
// the bottom, then its open menu dropdown, then its popups — each on its
// own layer so the compositor can lay a drop shadow under it. Before
// this, TearOffHost implemented neither compositor interface, so nothing
// on a torn-off window ever got a shadow: not its menus, not its context
// menus, not its combo box lists.
func TestTearOffHostIsACompositorHost(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}}
	h := NewTearOffHost(NewWindow("torn"), surf, ppu1, func() (int, int) { return 0, 0 }, nil)

	var _ platform.WindowProvider = h
	var _ platform.BaseLayerPainter = h
}

// With nothing to lift onto a layer, the host declines the compositor
// and keeps the plain single-surface present: a torn window with no menu
// open and no popup gains nothing from compositing.
func TestTearOffHostDeclinesCompositorWhenNothingOverlays(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}}
	h := NewTearOffHost(NewWindow("torn"), surf, ppu1, func() (int, int) { return 0, 0 }, nil)

	if list := h.GetChildWindows(); list != nil {
		t.Errorf("GetChildWindows = %+v, want nil with no popups and no open menu", list)
	}
}

// A registered popup becomes a compositor layer, and it carries the
// opening control's rect with it: the anchor is what makes a combo box
// and its dropped list cast one cohesive shadow. TearOffHost used to
// drop the Anchor on the floor when converting the request.
func TestTearOffHostPopupBecomesLayerWithAnchor(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}}
	h := NewTearOffHost(NewWindow("torn"), surf, ppu1, func() (int, int) { return 0, 0 }, nil)

	anchor := core.UnitRect{X: 10, Y: 20, Width: 40, Height: 8}
	bounds := core.UnitRect{X: 10, Y: 28, Width: 40, Height: 32}
	h.RegisterPopup(&core.PopupRequest{
		ID:     "combo",
		Bounds: bounds,
		Anchor: anchor,
		Paint:  func(*core.Painter) {},
	})

	list := h.GetChildWindows()
	if list == nil {
		t.Fatal("GetChildWindows = nil with a popup registered")
	}
	if len(list.Popups) != 1 {
		t.Fatalf("got %d popup layers, want 1", len(list.Popups))
	}
	popup, ok := list.Popups[0].(*PopupOverlay)
	if !ok {
		t.Fatalf("popup layer is %T, want *PopupOverlay", list.Popups[0])
	}
	if popup.Bounds != bounds {
		t.Errorf("popup bounds = %+v, want %+v", popup.Bounds, bounds)
	}
	if popup.Anchor != anchor {
		t.Errorf("popup anchor = %+v, want %+v — the opening control must join the popup's shadow",
			popup.Anchor, anchor)
	}
}

// The compositor draws popups on their own layers, so FrameBase must
// leave them out — painting them into the surface too would double them
// and put the base copy underneath the layer's own drop shadow. Frame
// keeps painting everything, for the non-compositing present.
func TestTearOffHostFrameBaseOmitsPopups(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(200, 100)
	if err != nil {
		t.Fatalf("raster.New: %v", err)
	}
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}}
	win := NewWindow("torn")
	win.SetBounds(core.UnitRect{Width: 200, Height: 100})
	win.Layout()
	h := NewTearOffHost(win, surf, ppu1, func() (int, int) { return 0, 0 }, nil)

	painted := 0
	h.RegisterPopup(&core.PopupRequest{
		ID:     "menu",
		Bounds: core.UnitRect{X: 10, Y: 20, Width: 40, Height: 32},
		Paint:  func(*core.Painter) { painted++ },
	})

	h.Frame(core.NewPainter(px))
	if painted != 1 {
		t.Errorf("Frame painted the popup %d times, want 1 (the complete-scene contract)", painted)
	}

	painted = 0
	h.FrameBase(core.NewPainter(px))
	if painted != 0 {
		t.Errorf("FrameBase painted the popup %d times, want 0 (the compositor layers it)", painted)
	}
}

// fakeMenuBar is a menu bar with one open dropdown, in its own interior
// denomination. Only the three methods MenuDropdownLayer needs are real.
type fakeMenuBar struct {
	core.TrinketBase
	dropdown, title core.UnitRect
	painted         int
}

func newFakeMenuBar(dropdown, title core.UnitRect) *fakeMenuBar {
	mb := &fakeMenuBar{dropdown: dropdown, title: title}
	mb.TrinketBase = *core.NewTrinketBase()
	return mb
}

func (m *fakeMenuBar) PaintDropdown(*core.Painter)          { m.painted++ }
func (m *fakeMenuBar) ActiveMenuBounds() core.UnitRect      { return m.dropdown }
func (m *fakeMenuBar) ActiveMenuTitleBounds() core.UnitRect { return m.title }

// A torn-off window's open menu dropdown lifts onto a layer of its own,
// positioned in WINDOW-local units (the bar's row plus the dropdown's
// offset within it) and carrying the title it drops from as its anchor,
// so title and menu cast one shadow. The window itself must then stop
// painting the dropdown into its own surface.
func TestTearOffHostMenuDropdownBecomesLayer(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(200, 100)
	if err != nil {
		t.Fatalf("raster.New: %v", err)
	}
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}}
	win := NewWindow("torn")
	mb := newFakeMenuBar(
		core.UnitRect{X: 4, Y: 16, Width: 60, Height: 40},
		core.UnitRect{X: 4, Y: 0, Width: 24, Height: 16})
	win.SetWindowMenuBar(mb)
	win.SetMenuBarVisible(true)
	win.SetDetached(true)
	win.SetBounds(core.UnitRect{Width: 200, Height: 100})
	win.Layout()
	h := NewTearOffHost(win, surf, ppu1, func() (int, int) { return 0, 0 }, nil)

	list := h.GetChildWindows()
	if list == nil || list.MenuDropdown == nil {
		t.Fatalf("GetChildWindows = %+v, want a menu dropdown layer", list)
	}
	layer, ok := list.MenuDropdown.(*struct {
		Bounds core.UnitRect
		Anchor core.UnitRect
		Paint  func(*core.Painter)
	})
	if !ok {
		t.Fatalf("menu dropdown layer is %T, want the compositor's bounds/anchor/paint struct", list.MenuDropdown)
	}

	// The layer's rects are the bar's own, translated into window-local
	// units by the bar's row origin — which is inset from the window's
	// left edge by the frame, so a layer positioned at the raw bar
	// coordinates would sit that far off.
	bar := win.menuBarRect()
	if bar.X == 0 {
		t.Fatal("menu bar row is flush with the window's left edge; " +
			"this test needs the frame inset to prove the translation happens")
	}
	if want := (core.UnitRect{X: bar.X + 4, Y: bar.Y + 16, Width: 60, Height: 40}); layer.Bounds != want {
		t.Errorf("dropdown layer bounds = %+v, want %+v", layer.Bounds, want)
	}
	if want := (core.UnitRect{X: bar.X + 4, Y: bar.Y, Width: 24, Height: 16}); layer.Anchor != want {
		t.Errorf("dropdown layer anchor = %+v, want %+v — the title must join the menu's shadow",
			layer.Anchor, want)
	}

	// FrameBase leaves the dropdown to the layer; the layer's own paint
	// is what draws it.
	mb.painted = 0
	h.FrameBase(core.NewPainter(px))
	if mb.painted != 0 {
		t.Errorf("FrameBase painted the dropdown %d times, want 0 (the compositor layers it)", mb.painted)
	}
	layer.Paint(core.NewPainter(px))
	if mb.painted != 1 {
		t.Errorf("layer paint drew the dropdown %d times, want 1", mb.painted)
	}

	// And the plain, non-compositing present still paints everything.
	mb.painted = 0
	h.Frame(core.NewPainter(px))
	if mb.painted != 1 {
		t.Errorf("Frame painted the dropdown %d times, want 1 (the complete-scene contract)", mb.painted)
	}
}

// A torn-off window reports a repaint revision, and dragging it does not
// move that revision. This is the whole reason the capability exists:
// the move arrives as input, Event invalidates the surface after EVERY
// input event as a parity contract, and the host would otherwise repaint
// the entire window and re-upload its pixels for each mouse move — to
// produce the picture already on screen. The OS is moving the window.
func TestTearOffHostDragDoesNotChangeRepaintRevision(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}, x: 500, y: 300}
	gx, gy := 700, 310
	win := NewWindow("torn")
	win.SetBounds(core.UnitRect{Width: 200, Height: 100})
	win.Layout()
	h := NewTearOffHost(win, surf, ppu1, func() (int, int) { return gx, gy }, nil)

	var _ platform.RepaintRevisionProvider = h

	// The gesture the desktop hands over on tear-off: a title grab, then
	// moves that reposition the OS window.
	h.BeginDrag(40, 8)
	startX := surf.x
	before := h.RepaintRevision()

	for i := 1; i <= 5; i++ {
		gx = 700 + i*10
		h.Event(core.MouseMoveEvent{X: 40, Y: 8, Buttons: core.LeftButton})
	}

	if surf.x == startX {
		t.Fatalf("the window did not move (x stayed %d); this test proves nothing", startX)
	}
	if got := h.RepaintRevision(); got != before {
		t.Errorf("repaint revision moved from %d to %d across a drag; "+
			"every mouse move would repaint and re-upload the whole window", before, got)
	}
}

// Opening a popup DOES move it: popups are not in the window's trinket
// subtree, so nothing else would tell a host caching this surface's
// pixels that one appeared.
func TestTearOffHostPopupMovesRepaintRevision(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}}
	h := NewTearOffHost(NewWindow("torn"), surf, ppu1, func() (int, int) { return 0, 0 }, nil)

	before := h.RepaintRevision()
	h.RegisterPopup(&core.PopupRequest{
		ID:     "menu",
		Bounds: core.UnitRect{X: 10, Y: 20, Width: 40, Height: 32},
		Paint:  func(*core.Painter) {},
	})
	opened := h.RepaintRevision()
	if opened == before {
		t.Fatal("opening a popup did not move the revision; it would never be drawn")
	}

	h.UnregisterPopup("menu")
	if h.RepaintRevision() == opened {
		t.Error("closing a popup did not move the revision; it would never be erased")
	}
}

// And a real content change inside the window moves it, or the window
// would freeze until the heartbeat.
func TestTearOffHostContentChangeMovesRepaintRevision(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}}
	win := NewWindow("torn")
	content := core.NewTrinketBase()
	content.Init(content)
	win.SetContent(content)
	h := NewTearOffHost(win, surf, ppu1, func() (int, int) { return 0, 0 }, nil)

	before := h.RepaintRevision()
	content.Update()
	if h.RepaintRevision() == before {
		t.Error("a change inside the torn window did not move its repaint revision")
	}
}
