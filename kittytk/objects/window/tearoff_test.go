package window

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/platform"
)

// nativeFakeSurface is an OS window's worth of fake: unit size, px
// position, and a size setter that reports back through Resized like
// the real platform (scale 1: pixels are units).
type nativeFakeSurface struct {
	size      core.UnitSize
	pxW, pxH  int // OS pixel size; when 0 it mirrors size (scale-1 fakes)
	handler   platform.SurfaceHandler
	x, y      int
	closed    bool
	opacity   float64
	minimized bool
	raised    bool
}

func (s *nativeFakeSurface) Size() core.UnitSize                  { return s.size }
func (s *nativeFakeSurface) Metrics() core.CellMetrics            { return core.DefaultCellMetrics() }
func (s *nativeFakeSurface) SetHandler(h platform.SurfaceHandler) { s.handler = h }
func (s *nativeFakeSurface) Invalidate(core.UnitRect)             {}
func (s *nativeFakeSurface) SetCursorVisible(bool)                {}
func (s *nativeFakeSurface) SetCursorPosition(x, y core.Unit)     {}
func (s *nativeFakeSurface) ScreenPositionPx() (int, int)         { return s.x, s.y }
func (s *nativeFakeSurface) SetScreenPositionPx(x, y int)         { s.x, s.y = x, y }
func (s *nativeFakeSurface) ScreenSizePx() (int, int) {
	if s.pxW != 0 || s.pxH != 0 {
		return s.pxW, s.pxH
	}
	return int(s.size.Width), int(s.size.Height)
}
func (s *nativeFakeSurface) WorkAreaPx() (int, int, int, int) { return 0, 30, 1600, 970 }
func (s *nativeFakeSurface) Close()                           { s.closed = true }
func (s *nativeFakeSurface) SetOpacity(o float64)             { s.opacity = o }
func (s *nativeFakeSurface) Raise()                           { s.raised = true }
func (s *nativeFakeSurface) Minimized() bool                  { return false }
func (s *nativeFakeSurface) Minimize()                        { s.minimized = true }

func (s *nativeFakeSurface) SetScreenSizePx(w, h int) {
	if s.pxW != 0 || s.pxH != 0 {
		s.pxW, s.pxH = w, h
	}
	s.size = core.UnitSize{Width: core.Unit(w), Height: core.Unit(h)}
	if s.handler != nil {
		s.handler.Resized(s.size)
	}
}

// Edge presses on a torn window resize its OS window with the
// pointer: the right and bottom edges grow it, the left edge moves
// the origin while pinning the right edge.
func TestTearOffHostEdgeResize(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}, x: 500, y: 300}
	gx, gy := 700, 380
	win := NewWindow("torn")
	h := NewTearOffHost(win, surf, 1, func() (int, int) { return gx, gy }, nil)

	// Right edge: press within the grip, drag 40 px right.
	h.Event(core.MousePressEvent{X: 197, Y: 50, Button: core.LeftButton})
	gx = 740
	h.Event(core.MouseMoveEvent{X: 237, Y: 50, Buttons: core.LeftButton})
	if surf.size.Width != 240 {
		t.Errorf("right-edge resize: width %d, want 240", surf.size.Width)
	}
	if b := win.Bounds(); b.Width != 240 {
		t.Errorf("window did not track the resized surface: %d", b.Width)
	}
	h.Event(core.MouseReleaseEvent{X: 237, Y: 50, Button: core.LeftButton})

	// Bottom edge: drag 30 px down.
	gx, gy = 700, 380
	h.Event(core.MousePressEvent{X: 100, Y: 97, Button: core.LeftButton})
	gy = 410
	h.Event(core.MouseMoveEvent{X: 100, Y: 127, Buttons: core.LeftButton})
	if surf.size.Height != 130 {
		t.Errorf("bottom-edge resize: height %d, want 130", surf.size.Height)
	}
	h.Event(core.MouseReleaseEvent{X: 100, Y: 127, Button: core.LeftButton})

	// Left edge: drag 20 px left - width grows, origin follows, the
	// right edge stays pinned.
	gx, gy = 700, 380
	rightEdge := surf.x + int(surf.size.Width)
	h.Event(core.MousePressEvent{X: 3, Y: 50, Button: core.LeftButton})
	gx = 680
	h.Event(core.MouseMoveEvent{X: -17, Y: 50, Buttons: core.LeftButton})
	if surf.size.Width != 260 {
		t.Errorf("left-edge resize: width %d, want 260", surf.size.Width)
	}
	if surf.x+int(surf.size.Width) != rightEdge {
		t.Errorf("left-edge resize moved the right edge: %d, want %d",
			surf.x+int(surf.size.Width), rightEdge)
	}
	h.Event(core.MouseReleaseEvent{X: -17, Y: 50, Button: core.LeftButton})

	// A press in the title row near the left edge drags, not resizes.
	h.Event(core.MousePressEvent{X: 120, Y: 8, Button: core.LeftButton})
	if h.resizing {
		t.Error("title-row press armed a resize")
	}
	if !h.Dragging() {
		t.Error("title-row press did not arm the drag")
	}
	h.Event(core.MouseReleaseEvent{X: 120, Y: 8, Button: core.LeftButton})
}

// The maximize button on a torn window zooms it to the display's
// work area (option-zoom, not a fullscreen space); a second press
// restores the saved rect.
func TestTearOffHostZoom(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}, x: 500, y: 300}
	win := NewWindow("torn")
	h := NewTearOffHost(win, surf, 1, func() (int, int) { return 0, 0 }, nil)

	h.ToggleZoom()
	if surf.x != 0 || surf.y != 30 || surf.size.Width != 1600 || surf.size.Height != 970 {
		t.Errorf("zoom did not fill the work area: %d,%d %dx%d",
			surf.x, surf.y, surf.size.Width, surf.size.Height)
	}
	if !win.IsMaximized() {
		t.Error("window chrome not in maximized state while zoomed")
	}

	h.ToggleZoom()
	if surf.x != 500 || surf.y != 300 || surf.size.Width != 200 || surf.size.Height != 100 {
		t.Errorf("zoom did not restore the saved rect: %d,%d %dx%d",
			surf.x, surf.y, surf.size.Width, surf.size.Height)
	}
	if win.IsMaximized() {
		t.Error("window chrome still maximized after restore")
	}
	if b := win.Bounds(); b.Width != 200 || b.Height != 100 {
		t.Errorf("window did not track the restored surface: %dx%d", b.Width, b.Height)
	}
}

// Title-focus keyboard geometry (arrow moves, Shift-arrow resizes)
// maps onto the OS window while torn: the same keys that walk an
// in-surface window around the KittyTK desktop walk the torn window
// around the real one.
func TestTearOffHostKeyboardGeometry(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}, x: 500, y: 300}
	win := NewWindow("torn")
	h := NewTearOffHost(win, surf, 2, func() (int, int) { return 0, 0 }, nil)

	// Arrow move: -8 units at scale 2 = -16 px.
	if !h.applyKeyboardBounds(core.UnitRect{X: -8, Y: 0, Width: 200, Height: 100}) {
		t.Fatal("bounds delegate not taken")
	}
	if surf.x != 500-16 || surf.y != 300 {
		t.Errorf("keyboard move: window at %d,%d, want %d,%d", surf.x, surf.y, 500-16, 300)
	}

	// Shift-arrow resize: +16 units wide at scale 2 = +32 px.
	h.applyKeyboardBounds(core.UnitRect{X: 0, Y: 0, Width: 216, Height: 100})
	if surf.size.Width != 200*2+32 {
		t.Errorf("keyboard resize: width %d px, want %d", surf.size.Width, 200*2+32)
	}
}

// A lost release cannot leave an armed drag inverted: motion without
// the button disarms it (no hover-drag), and a fresh press processes
// normally instead of resuming the dead gesture.
func TestTearOffHostLostReleaseDisarms(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}, x: 500, y: 300}
	gx, gy := 700, 380
	win := NewWindow("torn")
	h := NewTearOffHost(win, surf, 1, func() (int, int) { return gx, gy }, nil)

	// Armed (as the tear-off arms it), but the release was lost:
	// hover motion must not move the window.
	h.BeginDrag(120, 8)
	gx, gy = 900, 500
	h.Event(core.MouseMoveEvent{X: 40, Y: 9}) // no buttons held
	if surf.x != 500 || surf.y != 300 {
		t.Errorf("hover moved the window: %d,%d", surf.x, surf.y)
	}
	if h.Dragging() {
		t.Error("hover did not disarm the stale drag")
	}

	// Same for a stale armed drag hit by a fresh press: it must not
	// resume; the press processes normally (here: content press).
	h.BeginDrag(120, 8)
	h.Event(core.MousePressEvent{X: 100, Y: 50, Button: core.LeftButton})
	h.Event(core.MouseMoveEvent{X: 110, Y: 60, Buttons: core.LeftButton})
	if surf.x != 500 || surf.y != 300 {
		t.Errorf("stale drag resumed on content press: %d,%d", surf.x, surf.y)
	}
	h.Event(core.MouseReleaseEvent{X: 110, Y: 60, Button: core.LeftButton})
}

// Double-clicking the torn window's title bar toggles the work-area
// zoom, exactly as it toggles maximize in-surface.
func TestTearOffHostTitleDoubleClickZooms(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}, x: 500, y: 300}
	win := NewWindow("torn")
	h := NewTearOffHost(win, surf, 1, func() (int, int) { return 0, 0 }, nil)

	h.Event(core.MousePressEvent{X: 120, Y: 8, Button: core.LeftButton})
	h.Event(core.MouseReleaseEvent{X: 120, Y: 8, Button: core.LeftButton})
	h.Event(core.MousePressEvent{X: 121, Y: 8, Button: core.LeftButton})
	h.Event(core.MouseReleaseEvent{X: 121, Y: 8, Button: core.LeftButton})
	if !win.IsMaximized() || surf.size.Width != 1600 {
		t.Fatalf("double-click did not zoom: maximized=%v width=%d", win.IsMaximized(), surf.size.Width)
	}

	h.Event(core.MousePressEvent{X: 120, Y: 8, Button: core.LeftButton})
	h.Event(core.MouseReleaseEvent{X: 120, Y: 8, Button: core.LeftButton})
	h.Event(core.MousePressEvent{X: 121, Y: 8, Button: core.LeftButton})
	h.Event(core.MouseReleaseEvent{X: 121, Y: 8, Button: core.LeftButton})
	if win.IsMaximized() || surf.size.Width != 200 {
		t.Fatalf("second double-click did not restore: maximized=%v width=%d", win.IsMaximized(), surf.size.Width)
	}
}

// A BeginDrag-armed manager drag (re-dock hand-off) whose release was
// lost ends on the first button-less motion instead of dragging the
// window around on hover.
func TestManagerArmedDragEndsWithoutButton(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 800, Height: 480})
	win := NewWindow("docked")
	m.AddWindow(win)
	win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 200, Height: 100})

	m.BeginDrag(win, 120, 8)
	// Held motion follows.
	m.HandleMouseMove(core.MouseMoveEvent{X: 240, Y: 120, Buttons: core.LeftButton})
	if b := win.Bounds(); b.X != 120 || b.Y != 112 {
		t.Fatalf("armed drag with button did not follow: %d,%d", b.X, b.Y)
	}
	// Button-less motion ends it; further held motion must not drag.
	m.HandleMouseMove(core.MouseMoveEvent{X: 300, Y: 200})
	m.HandleMouseMove(core.MouseMoveEvent{X: 400, Y: 300, Buttons: core.LeftButton})
	if b := win.Bounds(); b.X != 120 || b.Y != 112 {
		t.Errorf("stale armed drag kept following: %d,%d", b.X, b.Y)
	}
}

// A torn window's frame leaves the framebuffer's corners at alpha 0
// (and its antialiased rim at partial alpha), so a transparent OS
// window composites rounded corners instead of opaque black ones.
func TestTornFrameLeavesCornersTransparent(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(400, 200)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(px)

	win := NewWindow("torn")
	win.SetBounds(core.UnitRect{Width: 400, Height: 200})
	win.Layout()
	win.Paint(core.NewPainter(px))

	img := px.Image()
	for _, c := range [][2]int{{0, 0}, {399, 0}, {0, 199}, {399, 199}} {
		if _, _, _, a := img.At(c[0], c[1]).RGBA(); a != 0 {
			t.Errorf("corner %d,%d not transparent: alpha %d", c[0], c[1], a)
		}
	}
	if _, _, _, a := img.At(200, 100).RGBA(); a != 0xffff {
		t.Errorf("interior not opaque: alpha %d", a)
	}
}

// Dragging a zoomed torn window's title restores it (grab point kept
// proportional), and dragging the pointer above the work area's top
// snap-zooms it again - in-surface maximize-drag parity.
func TestTearOffHostZoomDragRestoreAndSnap(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}, x: 500, y: 300}
	gx, gy := 0, 0
	win := NewWindow("torn")
	h := NewTearOffHost(win, surf, 1, func() (int, int) { return gx, gy }, nil)

	h.ToggleZoom() // 0,30 1600x970

	// Grab the zoomed title at its center and drag down: restore,
	// with the grab re-proportioned onto the restored width.
	gx, gy = 800, 200
	h.Event(core.MousePressEvent{X: 800, Y: 8, Button: core.LeftButton})
	h.Event(core.MouseMoveEvent{X: 800, Y: 170, Buttons: core.LeftButton})
	if win.IsMaximized() || surf.size.Width != 200 {
		t.Fatalf("drag did not restore the zoomed window: maximized=%v width=%d",
			win.IsMaximized(), surf.size.Width)
	}
	// Grab was at the title's center: it stays centered (800/1600*200 = 100).
	if surf.x != 800-100 || surf.y != 200-8 {
		t.Errorf("restored window at %d,%d; want %d,%d", surf.x, surf.y, 800-100, 200-8)
	}

	// Motion below the top strip re-arms the snap latch...
	gx, gy = 800, 220
	h.Event(core.MouseMoveEvent{X: 100, Y: 190, Buttons: core.LeftButton})
	// ...then dragging up past the work area's top snap-zooms.
	gx, gy = 800, 20 // above way=30
	h.Event(core.MouseMoveEvent{X: 100, Y: 5, Buttons: core.LeftButton})
	if !win.IsMaximized() || surf.size.Width != 1600 || surf.y != 30 {
		t.Errorf("snap-zoom did not fire: maximized=%v %dx%d at %d,%d",
			win.IsMaximized(), surf.size.Width, surf.size.Height, surf.x, surf.y)
	}
	h.Event(core.MouseReleaseEvent{X: 100, Y: 5, Button: core.LeftButton})
}

// The minimize button on a torn window miniaturizes its OS window
// (the Dock, on macOS) instead of being masked.
func TestTearOffHostMinimizeButton(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}, x: 500, y: 300}
	win := NewWindow("torn")
	h := NewTearOffHost(win, surf, 1, func() (int, int) { return 0, 0 }, nil)

	if win.Flags()&WindowFlagNoMinimize != 0 {
		t.Fatal("minimize masked on a native torn window")
	}
	// The [.] button sits after [x]: cells 4-6 of the title row.
	h.Event(core.MousePressEvent{X: 40, Y: 8, Button: core.LeftButton})
	h.Event(core.MouseReleaseEvent{X: 40, Y: 8, Button: core.LeftButton})
	if !surf.minimized {
		t.Error("minimize button did not miniaturize the OS window")
	}
}

// A torn window's chrome follows its OS window's focus, and Cmd+M
// (key "s-m") miniaturizes it like any macOS document window.
func TestTearOffHostFocusAndCmdM(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}, x: 500, y: 300}
	win := NewWindow("torn")
	h := NewTearOffHost(win, surf, 1, func() (int, int) { return 0, 0 }, nil)

	h.Event(core.FocusEvent{Focused: false})
	if win.IsActive() {
		t.Error("torn window still active after its OS window blurred")
	}
	h.Event(core.FocusEvent{Focused: true})
	if !win.IsActive() {
		t.Error("torn window not active after its OS window focused")
	}

	mods, _ := core.ParseKeyModifiers("s-m")
	h.Event(core.KeyPressEvent{Key: "s-m", Modifiers: mods})
	if !surf.minimized {
		t.Error("Cmd+M did not miniaturize the OS window")
	}
}

// Popups from a torn window's trinkets register on the host surface,
// paint there, and route their mouse events there - not on the
// desktop.
func TestTearOffHostOwnsPopups(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 300, Height: 200}, x: 0, y: 0}
	win := NewWindow("torn")
	h := NewTearOffHost(win, surf, 1, func() (int, int) { return 0, 0 }, nil)

	// The host is a PopupController and bridges the clipboard.
	var pc core.PopupController = h
	h.SetClipboardAccess(func() string { return "cb" }, func(string) {})
	if h.Clipboard() != "cb" {
		t.Error("clipboard bridge not wired")
	}

	pressed := false
	pc.RegisterPopup(&core.PopupRequest{
		ID:     "menu",
		Bounds: core.UnitRect{X: 50, Y: 40, Width: 80, Height: 60},
		HandleMousePress: func(core.MousePressEvent) bool {
			pressed = true
			return true
		},
	})

	// A press inside the popup routes to it (not to the window).
	if !h.Event(core.MousePressEvent{X: 60, Y: 50, Button: core.LeftButton}) {
		t.Error("press inside popup not handled")
	}
	if !pressed {
		t.Error("popup did not receive the press")
	}

	// A press outside closes the popup without consuming; a second
	// press then reaches the window normally.
	h.Event(core.MousePressEvent{X: 5, Y: 5, Button: core.LeftButton})
	pressed = false
	h.Event(core.MousePressEvent{X: 60, Y: 50, Button: core.LeftButton})
	if pressed {
		t.Error("popup still active after an outside press closed it")
	}
}

// A modally-blocked torn window swallows all input: a press surfaces the
// blocking modal (onBlockedPress) and is consumed, move/release/key are
// consumed, and focus events still pass through. When unblocked, input flows
// again and onBlockedPress no longer fires.
func TestTornHostModalBlockedSuppressesInput(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}}
	win := NewWindow("torn")
	h := NewTearOffHost(win, surf, 1, func() (int, int) { return 0, 0 }, nil)

	blocked := true
	presses := 0
	h.SetModalChecker(func() bool { return blocked }, func() { presses++ })

	if !h.Event(core.MousePressEvent{X: 50, Y: 50, Button: core.LeftButton}) {
		t.Error("a blocked press should be consumed")
	}
	if presses != 1 {
		t.Errorf("onBlockedPress fired %d times, want 1", presses)
	}
	if !h.Event(core.KeyPressEvent{Key: "a"}) {
		t.Error("a blocked key should be consumed")
	}
	// Focus still updates while blocked (chrome must stay correct).
	h.Event(core.FocusEvent{Focused: true})
	if !win.IsActive() {
		t.Error("a focus event should pass through while blocked")
	}

	// Unblocked: onBlockedPress must not fire on further presses.
	blocked = false
	h.Event(core.MousePressEvent{X: 50, Y: 50, Button: core.LeftButton})
	if presses != 1 {
		t.Errorf("onBlockedPress fired while unblocked (count=%d)", presses)
	}
}

// The one interaction a blocked torn window still allows: dragging it by the
// title bar to move it aside.
func TestTornHostModalBlockedAllowsTitleDrag(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}}
	win := NewWindow("torn")
	h := NewTearOffHost(win, surf, 1, func() (int, int) { return 0, 0 }, nil)
	h.SetModalChecker(func() bool { return true }, nil)

	// Title-bar press (below the 6-unit top grip, above the content, mid-width
	// so it is not on a titlebar button) starts a move drag.
	h.Event(core.MousePressEvent{X: 100, Y: 8, Button: core.LeftButton})
	if !h.Dragging() {
		t.Error("a title-bar press on a blocked torn window should start a move drag")
	}

	// A content press does not start a drag.
	h2 := NewTearOffHost(NewWindow("t2"), surf, 1, func() (int, int) { return 0, 0 }, nil)
	h2.SetModalChecker(func() bool { return true }, nil)
	h2.Event(core.MousePressEvent{X: 100, Y: 60, Button: core.LeftButton})
	if h2.Dragging() {
		t.Error("a content press on a blocked torn window must not start a drag")
	}
}
