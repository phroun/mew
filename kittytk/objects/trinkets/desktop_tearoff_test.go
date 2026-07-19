package trinkets

import (
	"testing"
	"time"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/platform"
)

// msSurface is a fake native surface: an OS window's worth of state.
type msSurface struct {
	size        core.UnitSize
	handler     platform.SurfaceHandler
	x, y        int
	closed      bool
	invalidated bool
	opacity     float64
	minimized   bool
	raised      bool
	primary     bool // the loop-owning surface; like SDL, refuses Close
	bordered    bool // OS title bar present (solo strips it, ExitSolo restores)
	opts        platform.SurfaceOptions
}

// SetBordered implements platform.BorderToggler.
func (s *msSurface) SetBordered(b bool) { s.bordered = b }

func (s *msSurface) Size() core.UnitSize                  { return s.size }
func (s *msSurface) Metrics() core.CellMetrics            { return core.DefaultCellMetrics() }
func (s *msSurface) SetHandler(h platform.SurfaceHandler) { s.handler = h }
func (s *msSurface) Invalidate(core.UnitRect)             { s.invalidated = true }
func (s *msSurface) SetCursorVisible(bool)                {}
func (s *msSurface) SetCursorPosition(x, y core.Unit)     {}
func (s *msSurface) ScreenPositionPx() (int, int)         { return s.x, s.y }
func (s *msSurface) SetScreenPositionPx(x, y int)         { s.x, s.y = x, y }
func (s *msSurface) ScreenSizePx() (int, int)             { return int(s.size.Width), int(s.size.Height) }

// Close mimics the real platform: the primary (loop-owning) surface
// refuses to close - solo mode reshapes it instead of destroying it.
func (s *msSurface) Close() {
	if s.primary {
		return
	}
	s.closed = true
}
func (s *msSurface) SetOpacity(o float64)             { s.opacity = o }
func (s *msSurface) Raise()                           { s.raised = true }
func (s *msSurface) Minimized() bool                  { return s.minimized }
func (s *msSurface) Minimize()                        { s.minimized = true }
func (s *msSurface) WorkAreaPx() (int, int, int, int) { return 0, 0, 1600, 1000 }

// SetScreenSizePx mimics the real platform: the size change reports
// back through Resized (scale 1: pixels are units).
func (s *msSurface) SetScreenSizePx(w, h int) {
	s.size = core.UnitSize{Width: core.Unit(w), Height: core.Unit(h)}
	if s.handler != nil {
		s.handler.Resized(s.size)
	}
}

// msPlatform is a fake multi-surface platform: the desktop surface
// plus any torn-off windows, with a scriptable global pointer.
type msPlatform struct {
	surfaces   []*msSurface
	script     func()
	afters     []func() // PostAfter callbacks, fired by the script
	gx, gy     int
	quitCalled bool
}

func (p *msPlatform) Run(init func(platform.Platform)) int {
	init(p)
	if p.script != nil {
		p.script()
	}
	return 0
}
func (p *msPlatform) Post(fn func())                       { fn() }
func (p *msPlatform) PostAfter(_ time.Duration, fn func()) { p.afters = append(p.afters, fn) }
func (p *msPlatform) Quit(int)                             { p.quitCalled = true }
func (p *msPlatform) Clipboard() string                    { return "" }
func (p *msPlatform) SetClipboard(string)                  {}
func (p *msPlatform) Beep()                                {}
func (p *msPlatform) SupportsMultipleSurfaces() bool       { return true }
func (p *msPlatform) GlobalPointerPx() (int, int)          { return p.gx, p.gy }
func (p *msPlatform) CreateSurface(o platform.SurfaceOptions) (platform.Surface, error) {
	s := &msSurface{opts: o, x: o.XPx, y: o.YPx, opacity: 1, bordered: !o.Borderless}
	if len(p.surfaces) == 0 {
		// The desktop window: 800x480 units at 50,60 px, scale 1. It owns
		// the event loop, so like SDL's main window it refuses to close.
		s.size = core.UnitSize{Width: 800, Height: 480}
		s.x, s.y = 50, 60
		s.primary = true
		s.bordered = true
	} else {
		s.size = core.UnitSize{Width: core.Unit(o.WidthPx), Height: core.Unit(o.HeightPx)}
	}
	p.surfaces = append(p.surfaces, s)
	return s, nil
}

func containsWindow(wm *window.WindowManager, win *window.Window) bool {
	for _, w := range wm.Windows() {
		if w == win {
			return true
		}
	}
	return false
}

// The tear handle sits in a button-width slot after [x][.][^]; at
// DefaultCellMetrics (8px cells) that is local x in [80,104). Grab
// its center.
const tearHandleLocalX = core.Unit(88)

// A non-tearable window dragged by the title past the surface edge
// does NOT tear off - tear-off is opt-in.
func TestNonTearableWindowStaysDocked(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)
	win := window.NewWindow("plain")
	d.SetOnStartup(func() {
		d.WindowManager().AddWindow(win)
		win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 200, Height: 100})
		win.Layout()
	})
	plat := &msPlatform{}
	plat.script = func() {
		desk := plat.surfaces[0]
		send := func(ev core.Event) { desk.handler.Event(ev) }
		send(core.MousePressEvent{X: 220, Y: 108, Button: core.LeftButton})
		send(core.MouseMoveEvent{X: -30, Y: 150, Buttons: core.LeftButton})
		if len(plat.surfaces) != 1 {
			t.Errorf("non-tearable window tore off: %d surfaces", len(plat.surfaces))
		}
		if !containsWindow(d.WindowManager(), win) {
			t.Error("non-tearable window left the desktop")
		}
		d.QuitWithCode(0)
	}
	d.RunOn(plat)
}

// A plain title drag on a tearable window moves it in-surface but does
// NOT tear it off - only a drag by the handle tears.
func TestTearableTitleDragDoesNotTear(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)
	win := window.NewWindow("tearme")
	win.SetTearable(true)
	d.SetOnStartup(func() {
		d.WindowManager().AddWindow(win)
		win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 200, Height: 100})
		win.Layout()
	})
	plat := &msPlatform{}
	plat.script = func() {
		desk := plat.surfaces[0]
		send := func(ev core.Event) { desk.handler.Event(ev) }
		// Grab the title well right of the handle slot.
		send(core.MousePressEvent{X: 250, Y: 108, Button: core.LeftButton})
		send(core.MouseMoveEvent{X: -30, Y: 150, Buttons: core.LeftButton})
		if len(plat.surfaces) != 1 {
			t.Errorf("title drag tore the window off: %d surfaces", len(plat.surfaces))
		}
		d.QuitWithCode(0)
	}
	d.RunOn(plat)
}

// Dragging a tearable window BY its handle past the surface edge tears
// it off; the live gesture keeps driving the torn surface, and
// crossing back over the desktop re-docks it.
func TestTearByHandleDragAndLiveRedock(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)
	win := window.NewWindow("tearme")
	win.SetTearable(true)
	d.SetOnStartup(func() {
		d.WindowManager().AddWindow(win)
		win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 200, Height: 100})
		win.Layout()
	})
	plat := &msPlatform{}
	plat.script = func() {
		desk := plat.surfaces[0]
		wm := d.WindowManager()
		send := func(ev core.Event) { desk.handler.Event(ev) }

		// Press the handle (local 88 -> screen 188), then cross the
		// left edge to tear off.
		send(core.MousePressEvent{X: 188, Y: 108, Button: core.LeftButton})
		send(core.MouseMoveEvent{X: -30, Y: 150, Buttons: core.LeftButton})
		if len(plat.surfaces) != 2 {
			t.Fatalf("handle drag did not tear off: %d surfaces", len(plat.surfaces))
		}
		torn := plat.surfaces[1]
		if !torn.opts.Borderless {
			t.Error("torn surface not borderless")
		}
		if containsWindow(wm, win) {
			t.Error("window still docked after tear-off")
		}
		if !win.IsDetached() {
			t.Error("window not marked detached")
		}

		// Back over the desktop while held: re-dock (live tornDrag).
		plat.gx, plat.gy = 50+200, 60+150
		send(core.MouseMoveEvent{X: 200, Y: 150, Buttons: core.LeftButton})
		if !containsWindow(wm, win) {
			t.Fatal("live drag back did not re-dock")
		}
		if win.IsDetached() {
			t.Error("re-docked window still marked detached")
		}
		send(core.MouseReleaseEvent{X: 200, Y: 150, Button: core.LeftButton})
		d.QuitWithCode(0)
	}
	d.RunOn(plat)
}

// Clicking a tearable window's handle (no drag) detaches it in place;
// clicking the detached '#' handle re-docks it where it sits.
func TestHandleClickDetachAndRedock(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)
	win := window.NewWindow("tearme")
	win.SetTearable(true)
	d.SetOnStartup(func() {
		d.WindowManager().AddWindow(win)
		win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 200, Height: 100})
		win.Layout()
	})
	plat := &msPlatform{}
	plat.script = func() {
		desk := plat.surfaces[0]
		wm := d.WindowManager()
		send := func(ev core.Event) { desk.handler.Event(ev) }

		// Press and release the handle in place: detach.
		send(core.MousePressEvent{X: 188, Y: 108, Button: core.LeftButton})
		send(core.MouseReleaseEvent{X: 188, Y: 108, Button: core.LeftButton})
		if len(plat.surfaces) != 2 {
			t.Fatalf("handle click did not detach: %d surfaces", len(plat.surfaces))
		}
		torn := plat.surfaces[1]
		if !win.IsDetached() || containsWindow(wm, win) {
			t.Fatal("window not detached after handle click")
		}

		// Click the '#' handle in the torn window: re-dock in place.
		torn.handler.Event(core.MousePressEvent{X: 88, Y: 8, Button: core.LeftButton})
		torn.handler.Event(core.MouseReleaseEvent{X: 88, Y: 8, Button: core.LeftButton})
		if !containsWindow(wm, win) {
			t.Fatal("'#' click did not re-dock")
		}
		if win.IsDetached() {
			t.Error("re-docked window still detached")
		}
		d.QuitWithCode(0)
	}
	d.RunOn(plat)
}

// The platform may hand the tail of the tearing gesture (motion and
// release) to the torn window once it appears under the held pointer.
// The desktop must not treat its stale tear state as a live drag.
func TestMissedReleaseDoesNotStealTornWindow(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)
	win := window.NewWindow("tearme")
	win.SetTearable(true)
	d.SetOnStartup(func() {
		d.WindowManager().AddWindow(win)
		win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 200, Height: 100})
		win.Layout()
	})
	plat := &msPlatform{}
	plat.script = func() {
		desk := plat.surfaces[0]
		wm := d.WindowManager()
		send := func(ev core.Event) { desk.handler.Event(ev) }

		// Tear off by the handle; the gesture continues at the torn window.
		send(core.MousePressEvent{X: 188, Y: 108, Button: core.LeftButton})
		send(core.MouseMoveEvent{X: -30, Y: 150, Buttons: core.LeftButton})
		if len(plat.surfaces) != 2 {
			t.Fatalf("handle drag did not tear off: %d surfaces", len(plat.surfaces))
		}
		torn := plat.surfaces[1]

		// The release lands on the torn window - the desktop never sees it.
		torn.handler.Event(core.MouseReleaseEvent{X: 40, Y: 9, Button: core.LeftButton})

		// Hovering the desktop later (button up) must not re-dock.
		send(core.MouseMoveEvent{X: 400, Y: 300})
		send(core.MouseMoveEvent{X: 410, Y: 310})
		if torn.closed || containsWindow(wm, win) {
			t.Fatal("hover over the desktop stole the torn window back")
		}

		// The repaint tick still drives the torn surface.
		torn.invalidated = false
		if len(plat.afters) > 0 {
			plat.afters[len(plat.afters)-1]()
		}
		if !torn.invalidated {
			t.Error("repaint tick did not invalidate the torn surface")
		}
		d.QuitWithCode(0)
	}
	d.RunOn(plat)
}

// Closing a torn window with its [x] button disposes of its OS
// window immediately - it must not linger until a re-dock.
func TestClosingTornWindowDisposesSurface(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	win := window.NewWindow("tearme")
	win.SetTearable(true)
	d.SetOnStartup(func() {
		d.WindowManager().AddWindow(win)
		win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 200, Height: 100})
		win.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		desk := plat.surfaces[0]
		send := func(ev core.Event) { desk.handler.Event(ev) }

		// Tear off by the handle and release outside.
		send(core.MousePressEvent{X: 188, Y: 108, Button: core.LeftButton})
		send(core.MouseMoveEvent{X: -30, Y: 150, Buttons: core.LeftButton})
		send(core.MouseReleaseEvent{X: -30, Y: 150, Button: core.LeftButton})
		torn := plat.surfaces[1]

		// The window closes itself (the [x] button path).
		win.Close()
		if !torn.closed {
			t.Error("torn surface still open after the window closed")
		}

		// The repaint tick must not resurrect the dead host.
		if len(plat.afters) > 0 {
			plat.afters[len(plat.afters)-1]()
		}

		d.QuitWithCode(0)
	}

	d.RunOn(plat)
}

// When the desktop's own OS window loses focus, its active window's
// chrome dims; re-focusing lights the same window back up.
func TestDesktopBlurDimsActiveWindow(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	win := window.NewWindow("focused")
	d.SetOnStartup(func() {
		d.WindowManager().AddWindow(win)
		win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 200, Height: 100})
		win.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		desk := plat.surfaces[0]
		if !win.IsActive() {
			t.Fatal("window not active after AddWindow")
		}
		desk.handler.Event(core.FocusEvent{Focused: false})
		if win.IsActive() {
			t.Error("active window still lit while the desktop is blurred")
		}
		desk.handler.Event(core.FocusEvent{Focused: true})
		if !win.IsActive() {
			t.Error("active window not re-lit when the desktop re-focused")
		}
		d.QuitWithCode(0)
	}

	d.RunOn(plat)
}

// Tearing off an app's main window carries its non-tearable children onto
// their own surfaces, preserving each child's z-order and its position
// relative to the main window, then raises the main window above those
// children and gives it focus so the tear ends with it on top.
func TestMainWindowTearOffCascadeArrangeRaise(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("main")
	main.SetTearable(true)
	childBack := window.NewWindow("back")   // non-tearable, behind
	childFront := window.NewWindow("front") // non-tearable, in front

	app := &mockApp{name: "App", main: main, windows: []*window.Window{main, childBack, childFront}}
	d.AddApplication(app)

	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		main.Layout()
		wm.AddWindow(childBack)
		childBack.SetBounds(core.UnitRect{X: 10, Y: 10, Width: 100, Height: 80})
		childBack.Layout()
		wm.AddWindow(childFront)
		childFront.SetBounds(core.UnitRect{X: 20, Y: 20, Width: 100, Height: 80})
		childFront.Layout()
		// Deterministic z-order: main (back), childBack, childFront (front).
		wm.ActivateWindow(main)
		wm.ActivateWindow(childBack)
		wm.ActivateWindow(childFront)
	})

	plat := &msPlatform{}
	plat.script = func() {
		d.tearOffInPlace(main)

		if len(plat.surfaces) != 4 {
			t.Fatalf("want 4 surfaces (desktop + main + 2 children), got %d", len(plat.surfaces))
		}
		mainSurf := plat.surfaces[1]
		// Children tear off in z-order (back to front), so surfaces are
		// created back-first: surfaces[2]=childBack, surfaces[3]=childFront.
		backSurf := plat.surfaces[2]
		frontSurf := plat.surfaces[3]

		if !mainSurf.raised {
			t.Error("main window surface was not raised above its children")
		}
		if !main.IsDetached() || !childBack.IsDetached() || !childFront.IsDetached() {
			t.Error("main window and its non-tearable children should all be detached")
		}

		// Desktop origin (50,60) px, scale 1. Main torn in place at unit
		// (100,100). Each child keeps its docked position (offset from main
		// preserved): back at unit (10,10) -> px (60,70); front at unit
		// (20,20) -> px (70,80).
		if backSurf.x != 60 || backSurf.y != 70 {
			t.Errorf("back child not at its original position: (%d,%d), want (60,70)", backSurf.x, backSurf.y)
		}
		if frontSurf.x != 70 || frontSurf.y != 80 {
			t.Errorf("front child not at its original position: (%d,%d), want (70,80)", frontSurf.x, frontSurf.y)
		}

		if d.tornFocusOwner != main {
			t.Error("torn main window did not take focus")
		}
		d.QuitWithCode(0)
	}

	d.RunOn(plat)
}

// A torn-off window's resize-edge grip matches the desktop's in-surface
// grip, not the built-in default - so torn edges are the same thickness
// as docked ones and don't overlap edge trinkets such as scrollbars.
func TestTornWindowResizeGripMatchesDesktop(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	win := window.NewWindow("w")
	win.SetTearable(true)
	d.SetOnStartup(func() {
		d.WindowManager().AddWindow(win)
		win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		win.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		if d.resizeGrip <= 0 {
			t.Fatalf("desktop has no graphical resize grip (%d); test needs one", d.resizeGrip)
		}
		d.tearOffInPlace(win)
		if len(d.tornHosts) == 0 {
			t.Fatal("window did not tear off")
		}
		if got := d.tornHosts[0].ResizeGrip(); got != d.resizeGrip {
			t.Errorf("torn window grip = %d, want the desktop grip %d", got, d.resizeGrip)
		}
		d.QuitWithCode(0)
	}
	d.RunOn(plat)
}

// Solo mode reshapes the desktop's own OS window into the app's window:
// no second surface is created, the main window is hosted on the primary
// surface and fills it, and the host quits when the last window closes.
func TestSoloModeHostsMainOnPrimarySurface(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("Solo")
	app := &mockApp{name: "Solo", main: main, windows: []*window.Window{main}}
	d.AddApplication(app)

	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		main.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		d.EnterSoloMode(main)

		if !d.IsSolo() {
			t.Error("desktop is not in solo mode")
		}
		if !main.IsDetached() {
			t.Error("solo main window is not hosted (detached)")
		}
		if len(plat.surfaces) != 1 {
			t.Fatalf("want 1 surface (the reshaped primary), got %d", len(plat.surfaces))
		}
		// The window fills the primary surface (no separate window).
		if b, s := main.Bounds(), plat.surfaces[0].size; b.Width != s.Width || b.Height != s.Height {
			t.Errorf("solo window %dx%d does not fill the surface %dx%d",
				b.Width, b.Height, s.Width, s.Height)
		}

		// Closing the last window quits the host.
		main.Close()
		if !plat.quitCalled {
			t.Error("host did not quit after its last window closed")
		}

		d.QuitWithCode(0)
	}

	d.RunOn(plat)
}

// A modal owned by the solo window must block the solo primary surface. That
// surface is a torn host, so it has to consult the modal stack exactly as a
// regular torn host does - a regression guard for the solo host omitting
// SetModalChecker, which left the editor taking input while its own modal was up
// (the modal displayed but did not actually block).
func TestSoloPrimaryHostBlockedByOwnedModal(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("Solo")
	app := &mockApp{name: "Solo", main: main, windows: []*window.Window{main}}
	d.AddApplication(app)

	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		main.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		d.EnterSoloMode(main)

		host := d.soloPrimaryHost
		if host == nil {
			t.Fatal("no solo primary host after EnterSoloMode")
		}
		if host.IsModalBlocked() {
			t.Fatal("solo host reports blocked before any modal is up")
		}

		// A window-level modal owned by the solo window.
		modal := window.NewWindow("Gate")
		modal.SetType(window.WindowTypeModal)
		modal.SetOwner(main)
		d.WindowManager().AddWindow(modal)

		if !host.IsModalBlocked() {
			t.Error("solo primary host is not blocked by a modal owned by its window")
		}

		// The blocked-press surfacing must find the window-scoped modal (not just
		// application modals) so a click on the blocked editor pulls it forward.
		if got := d.WindowManager().TopModalBlocking(main); got != modal {
			t.Errorf("TopModalBlocking = %v, want the owning modal %v", got, modal)
		}

		// Closing the modal releases the block.
		modal.Close()
		if host.IsModalBlocked() {
			t.Error("solo primary host still blocked after its modal closed")
		}

		d.QuitWithCode(0)
	}

	d.RunOn(plat)
}

// Closing the window on the primary surface (which owns the loop and
// can't be destroyed) promotes a remaining peer onto that surface: the
// primary surface takes on the peer's window and repositions/resizes to
// where the peer was, and only closing the truly last window quits.
func TestSoloModePrimaryCloserPromotesPeer(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("Main")
	peer := window.NewWindow("Peer")
	app := &mockApp{name: "Solo", main: main, windows: []*window.Window{main}}
	d.AddApplication(app)

	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		main.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		wm := d.WindowManager()
		d.EnterSoloMode(main)

		// A window opened in solo mode tears onto its own peer surface
		// (the window manager's added-hook routes it to soloAdoptWindow).
		app.windows = append(app.windows, peer)
		peer.SetBounds(core.UnitRect{X: 40, Y: 30, Width: 220, Height: 160})
		wm.AddWindow(peer)
		peer.Layout()

		if len(plat.surfaces) != 2 {
			t.Fatalf("want 2 surfaces (primary + peer), got %d", len(plat.surfaces))
		}
		primary := plat.surfaces[0]
		peerSurf := plat.surfaces[1]
		peerX, peerY := peerSurf.x, peerSurf.y
		peerW, peerH := peerSurf.size.Width, peerSurf.size.Height

		// Close the window on the primary surface. The primary can't be
		// destroyed, so the peer is promoted onto it.
		main.Close()

		if plat.quitCalled {
			t.Fatal("host quit while a peer window remained")
		}
		if primary.closed {
			t.Fatal("primary surface was destroyed instead of reshaped")
		}
		if !peerSurf.closed {
			t.Error("promoted peer's own surface was not discarded")
		}
		if !peer.IsDetached() {
			t.Error("promoted peer is not hosted (detached)")
		}
		// The peer fills the primary surface...
		if b := peer.Bounds(); b.Width != primary.size.Width || b.Height != primary.size.Height {
			t.Errorf("promoted peer %dx%d does not fill primary %dx%d",
				b.Width, b.Height, primary.size.Width, primary.size.Height)
		}
		// ...which took on the peer's screen placement (position and size).
		if primary.x != peerX || primary.y != peerY {
			t.Errorf("primary not repositioned to peer origin: got (%d,%d), want (%d,%d)",
				primary.x, primary.y, peerX, peerY)
		}
		if primary.size.Width != peerW || primary.size.Height != peerH {
			t.Errorf("primary not resized to peer size: got %dx%d, want %dx%d",
				primary.size.Width, primary.size.Height, peerW, peerH)
		}

		// Now closing the last window (on the primary) quits the host.
		peer.Close()
		if !plat.quitCalled {
			t.Error("host did not quit after the last window closed")
		}
		d.QuitWithCode(0)
	}

	d.RunOn(plat)
}

// ExitSoloMode reveals a desktop from a running solo app: the primary
// surface is re-bordered and reclaimed by the desktop, and the solo window
// becomes an ordinary tearable torn-off window on its own surface at the
// same screen rectangle (so it can be dragged in to dock).
func TestExitSoloModeRevealsDesktop(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("Solo")
	app := &mockApp{name: "Solo", main: main, windows: []*window.Window{main}}
	d.AddApplication(app)

	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		main.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		d.EnterSoloMode(main)
		primary := plat.surfaces[0]
		if primary.bordered {
			t.Fatal("primary surface still bordered in solo mode")
		}

		d.ExitSoloMode()

		if d.IsSolo() {
			t.Error("still in solo mode after ExitSoloMode")
		}
		if !primary.bordered {
			t.Error("primary surface was not re-bordered for the desktop")
		}
		if primary.closed {
			t.Fatal("primary surface was destroyed instead of reclaimed")
		}
		if !primary.raised {
			t.Error("revealed desktop surface was not brought to the front")
		}
		if len(plat.surfaces) != 2 {
			t.Fatalf("want 2 surfaces (desktop + torn app window), got %d", len(plat.surfaces))
		}
		if !main.IsDetached() {
			t.Error("app window is not a torn-off window after exit")
		}
		if !main.IsTearable() {
			t.Error("app window did not regain its tearable/redock handle")
		}
		// The torn window lands at the primary's rectangle (same location).
		torn := plat.surfaces[1]
		if torn.x != primary.x || torn.y != primary.y {
			t.Errorf("torn window at (%d,%d), want the primary's (%d,%d)",
				torn.x, torn.y, primary.x, primary.y)
		}
		if torn.size != primary.size {
			t.Errorf("torn window size %v, want the primary's %v", torn.size, primary.size)
		}
		d.QuitWithCode(0)
	}

	d.RunOn(plat)
}

// EnterSoloFromDesktop is the inverse: after a desktop has been revealed,
// promoting the detached app makes it solo again - its surface is discarded
// and it fills the primary surface, which MOVES to where the app's torn
// window was so the app keeps its on-screen position instead of snapping to
// the desktop's spot.
func TestReSoloFromDesktop(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("Solo")
	app := &mockApp{name: "Solo", main: main, windows: []*window.Window{main}}
	d.AddApplication(app)

	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		main.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		d.EnterSoloMode(main)
		d.ExitSoloMode() // now a desktop with main as a torn window
		primary := plat.surfaces[0]
		tornSurf := plat.surfaces[1]

		// Simulate the user dragging the torn app window away from where the
		// desktop sits, then shrinking it.
		tornSurf.SetScreenPositionPx(300, 220)
		tornSurf.SetScreenSizePx(220, 160)

		d.EnterSoloFromDesktop()

		if !d.IsSolo() {
			t.Error("not back in solo mode after EnterSoloFromDesktop")
		}
		if !tornSurf.closed {
			t.Error("the promoted window's own surface was not discarded")
		}
		if primary.bordered {
			t.Error("primary surface was not re-stripped of its border for solo")
		}
		if !main.IsDetached() {
			t.Error("solo window is not hosted (detached)")
		}
		// The primary MOVED to where the app's torn window was (the app keeps
		// its position and size, not the desktop's).
		if primary.x != 300 || primary.y != 220 {
			t.Errorf("primary at (%d,%d); should adopt the app's position (300,220)", primary.x, primary.y)
		}
		if primary.size.Width != 220 || primary.size.Height != 160 {
			t.Errorf("primary size %v; should adopt the app's size 220x160", primary.size)
		}
		if b := main.Bounds(); b.Width != primary.size.Width || b.Height != primary.size.Height {
			t.Errorf("solo window %dx%d does not fill the primary %dx%d",
				b.Width, b.Height, primary.size.Width, primary.size.Height)
		}
		d.QuitWithCode(0)
	}

	d.RunOn(plat)
}

// A combobox on a tab that is NOT active while a window is torn off must
// still open its popup on the torn surface, not the desktop. TabTrinket only
// exposes its active tab as a child, so a combobox whose tab was active
// during an earlier stamp (the desktop's window manager) kept that stale
// controller; stamping every tab page fixes it.
func TestTornWindowTabComboboxPopupController(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("Solo")
	tabs := NewTabTrinket()
	comboT0 := NewComboBox()
	comboT1 := NewComboBox()
	p0 := NewPanel()
	p0.AddChild(comboT0)
	p1 := NewPanel()
	p1.AddChild(comboT1)
	tabs.AddTab("First", p0)  // active on add -> gets the wm controller
	tabs.AddTab("Second", p1) // inactive on add
	main.SetContent(tabs)
	app := &mockApp{name: "Solo", main: main, windows: []*window.Window{main}}
	d.AddApplication(app)

	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		main.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		// Switch to the second tab, so the tear-off re-stamp runs while the
		// first tab (comboT0) is inactive - the case that stranded it.
		tabs.SetCurrentIndex(1)

		d.EnterSoloMode(main)
		d.ExitSoloMode()
		tornHost := core.PopupController(d.tornHosts[len(d.tornHosts)-1])

		if got := comboT0.findPopupController(); got != tornHost {
			t.Errorf("inactive-tab combobox resolves popups to %v, want the torn host %v (would open on the desktop)", got, tornHost)
		}
		if got := comboT1.findPopupController(); got != tornHost {
			t.Errorf("active-tab combobox resolves popups to %v, want the torn host %v", got, tornHost)
		}
		d.QuitWithCode(0)
	}

	d.RunOn(plat)
}

// The system menu's "Exit Desktop" command promotes a remaining app back
// to solo rather than quitting: with a desktop revealed and a torn app on
// it, ExitDesktop re-solos that app; with nothing left it quits the host.
func TestExitDesktopReSolosOrQuits(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("Solo")
	app := &mockApp{name: "Solo", main: main, windows: []*window.Window{main}}
	d.AddApplication(app)

	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		main.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		d.EnterSoloMode(main)
		d.ExitSoloMode() // desktop revealed, main is a torn window on it

		// Exit Desktop with an app still present -> promote it back to solo.
		d.ExitDesktop()
		if !d.IsSolo() {
			t.Error("Exit Desktop did not re-solo the remaining app")
		}
		if plat.quitCalled {
			t.Error("Exit Desktop quit the host while an app remained")
		}

		// Back on a desktop with the app gone, Exit Desktop quits.
		d.ExitSoloMode()
		main.Close()
		d.ExitDesktop()
		if !plat.quitCalled {
			t.Error("Exit Desktop did not quit with no app windows left")
		}
		d.QuitWithCode(0)
	}

	d.RunOn(plat)
}

// A minimized follower (docked while its main window was on the desktop)
// leaves the dock when the main window tears off: it comes along on its
// own surface, so its dock entry must be removed and it un-minimized.
func TestTearOffRemovesMinimizedFollowerFromDock(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("main")
	main.SetTearable(true)
	child := window.NewWindow("child") // non-tearable follower

	app := &mockApp{name: "App", main: main, windows: []*window.Window{main, child}}
	d.AddApplication(app)

	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		main.Layout()
		wm.AddWindow(child)
		child.SetBounds(core.UnitRect{X: 20, Y: 20, Width: 100, Height: 80})
		child.Layout()
		wm.ActivateWindow(main)
		wm.ActivateWindow(child)
		// Minimize the follower - it drops into the desktop dock.
		wm.MinimizeWindow(child)
	})

	plat := &msPlatform{}
	plat.script = func() {
		if !dockHasEntry(d.DockRow(), child.ObjectID()) {
			t.Fatal("follower was not added to the dock on minimize")
		}

		d.tearOffInPlace(main)

		if dockHasEntry(d.DockRow(), child.ObjectID()) {
			t.Error("torn-off follower still has a lingering dock entry")
		}
		if child.IsMinimized() {
			t.Error("torn-off follower is still minimized (invisible on its surface)")
		}
		if !child.IsDetached() {
			t.Error("follower did not tear off with the main window")
		}
		d.QuitWithCode(0)
	}
	d.RunOn(plat)
}

func dockHasEntry(dock *DockRow, id core.ObjectID) bool {
	if dock == nil {
		return false
	}
	for _, e := range dock.Entries() {
		if e.WindowID == id {
			return true
		}
	}
	return false
}

// A window that was maximized when torn off re-fills the client area of
// the desktop it docks back into, rather than keeping its torn size.
func TestMaximizedWindowRedockRefillsClientArea(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	win := window.NewWindow("max")
	win.SetTearable(true)
	d.SetOnStartup(func() {
		d.WindowManager().AddWindow(win)
		win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 200, Height: 100})
		win.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		wm := d.WindowManager()
		wm.MaximizeWindow(win)
		if !win.IsMaximized() {
			t.Fatal("window did not maximize")
		}

		host := d.createTornHost(win, 0, 0)
		if host == nil {
			t.Fatal("tear-off host not created")
		}
		if !win.IsDetached() {
			t.Fatal("window not detached after tear-off")
		}

		d.redockInPlace(host)
		if win.IsDetached() {
			t.Error("window still detached after redock")
		}
		if !win.IsMaximized() {
			t.Error("window lost its maximized state on redock")
		}
		if got, want := win.Bounds(), wm.ClientArea(); got != want {
			t.Errorf("redocked maximized bounds = %v, want client area %v", got, want)
		}
		d.QuitWithCode(0)
	}
	d.RunOn(plat)
}

// Clicking the tear handle on a maximized window restores it to its
// unmaximized size as part of the tear-off, so it lands on its own
// surface at its normal bounds rather than the client-area size.
func TestTearOffRestoresMaximizedWindow(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	win := window.NewWindow("max")
	win.SetTearable(true)
	normal := core.UnitRect{X: 100, Y: 100, Width: 200, Height: 100}
	d.SetOnStartup(func() {
		d.WindowManager().AddWindow(win)
		win.SetBounds(normal)
		win.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		wm := d.WindowManager()
		wm.MaximizeWindow(win)
		if !win.IsMaximized() || win.Bounds() == normal {
			t.Fatal("window did not maximize to the client area")
		}

		d.tearOffInPlace(win)

		if !win.IsDetached() {
			t.Fatal("window not detached after tear-off")
		}
		if win.IsMaximized() {
			t.Error("torn window is still maximized; should have restored")
		}
		// The torn surface is sized to the restored (normal) bounds, not
		// the client area. Desktop origin is (50,60) px, scale 1.
		torn := plat.surfaces[1]
		if torn.opts.WidthPx != int(normal.Width) || torn.opts.HeightPx != int(normal.Height) {
			t.Errorf("torn surface size = %dx%d, want %dx%d (restored size)",
				torn.opts.WidthPx, torn.opts.HeightPx, normal.Width, normal.Height)
		}
		d.QuitWithCode(0)
	}
	d.RunOn(plat)
}
