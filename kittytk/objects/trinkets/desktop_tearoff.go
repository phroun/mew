package trinkets

import (
	"math"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/platform"
)

// Tear-off choreography (G4 granting, desktop side). On multi-surface
// platforms a desktop window dragged past the surface edge undocks
// into its own borderless OS window - chrome intact, so it looks the
// same torn or docked - and re-docks when the pointer crosses back
// over the desktop mid-drag.
//
// Two drags exist. While the tearing gesture is live the DESKTOP
// window still owns the pointer capture, so the desktop keeps
// receiving motion and drives the torn surface itself (tornDrag). A
// later drag started on the torn window's own title bar is driven by
// its TearOffHost, which asks the desktop via the redock callback
// whether the pointer has come home.

// tornDrag is the desktop-driven phase of a tear-off: the gesture
// that tore the window is still holding the button.
type tornDrag struct {
	host *window.TearOffHost
	surf platform.Surface
	offX core.Unit // grab offset within the window, units
	offY core.Unit
}

// setupTearOff arms the window manager's modal-surfacing policy (every
// platform) and, when the platform can host more than one surface, its
// tear-off handler.
func (d *Desktop) setupTearOff(p platform.Platform, surf platform.Surface) {
	// Modal-surfacing works in-surface too, so it is wired on every platform
	// (the TUI is single-surface): clicking a modally-blocked window surfaces
	// the modal blocking it (raising or, for a torn one, OS-restoring it), even
	// across applications; the wallpaper dim and wallpaper-click apply only when
	// the desktop itself is blocked (a system modal, or a modal owned by the app
	// currently on the menu bar).
	d.windowManager.SetOnBlockedClick(d.surfaceBlockingModal)
	d.windowManager.SetActiveAppIDFunc(func() core.ObjectID {
		d.mu.RLock()
		a := d.activeApp
		d.mu.RUnlock()
		if a != nil {
			return a.ObjectID()
		}
		return 0
	})
	d.windowManager.SetOnWallpaperClick(d.surfaceActiveAppModal)

	// The tear-off handler needs multiple native surfaces and a global pointer.
	ms, ok := p.(platform.MultiSurfacePlatform)
	if !ok || !ms.SupportsMultipleSurfaces() {
		return
	}
	if _, ok := surf.(platform.NativeSurface); !ok {
		return
	}
	if _, ok := p.(platform.GlobalPointerPlatform); !ok {
		return
	}
	d.windowManager.SetTearOffHandler(d.tearOffWindow)
}

// deviceScale is the desktop surface's device zoom (integer pixels per
// unit at the base font). For unit<->pixel geometry that must track
// font_size use pxPerUnit / unitToPx / pxToUnit instead.
func (d *Desktop) deviceScale() int {
	d.mu.RLock()
	backend := d.backend
	d.mu.RUnlock()
	if ds, ok := backend.(core.DeviceScaler); ok {
		if s := ds.Scale(); s > 0 {
			return s
		}
	}
	return 1
}

// pxPerUnit is the desktop surface's fractional pixels-per-unit (device
// zoom times the font_size ratio). Torn surfaces are sized/placed with
// it so they match the pixel size the window content actually paints at.
func (d *Desktop) pxPerUnit() float64 {
	d.mu.RLock()
	backend := d.backend
	d.mu.RUnlock()
	if m, ok := backend.(core.UnitPixelMapper); ok {
		if ppu := m.PxPerUnit(); ppu > 0 {
			return ppu
		}
	}
	return float64(d.deviceScale())
}

// unitToPx / pxToUnit convert between unit lengths and device pixels at
// the desktop surface's font_size-aware scale.
func (d *Desktop) unitToPx(u core.Unit) int {
	return int(math.Round(float64(u) * d.pxPerUnit()))
}

func (d *Desktop) pxToUnit(px int) core.Unit {
	ppu := d.pxPerUnit()
	if ppu <= 0 {
		ppu = 1
	}
	return core.Unit(math.Round(float64(px) / ppu))
}

// tearOffWindow implements the WindowManager tear-off policy: lift
// the window out into its own OS surface positioned so the grab
// point stays under the pointer, and keep driving the drag from the
// desktop (which still owns the capture).
func (d *Desktop) tearOffWindow(win *window.Window, e core.MouseMoveEvent, offX, offY core.Unit) bool {
	// Only tearable windows detach - dialogs and other plain windows
	// stay put.
	if !win.IsTearable() {
		return false
	}
	host := d.createTornHost(win, e.X-offX, e.Y-offY)
	if host == nil {
		return false
	}
	// Drag path: keep driving the gesture from the desktop's captured
	// pointer stream until release.
	host.BeginDrag(offX, offY)
	d.mu.Lock()
	d.tornDrag = &tornDrag{host: host, surf: host.Surface(), offX: offX, offY: offY}
	d.mu.Unlock()
	return true
}

// tearOffInPlace detaches a docked tearable window into its own
// surface at its current desktop position and size, without a drag -
// the tear-handle click/keyboard activation path.
func (d *Desktop) tearOffInPlace(win *window.Window) {
	if !win.IsTearable() || win.IsDetached() {
		return
	}
	// A maximized window restores to its unmaximized size as part of the
	// tear-off, so it lands on its own surface at its normal bounds rather
	// than filling one the size of the desktop's client area.
	if win.IsMaximized() {
		win.Restore()
	}
	b := win.Bounds()
	d.createTornHost(win, b.X, b.Y)
}

// createTornHost lifts win out of the window manager into a new
// borderless OS surface whose top-left is at the given desktop-unit
// position. Returns nil when the platform can't host it. Shared by
// the drag and click detach paths.
func (d *Desktop) createTornHost(win *window.Window, deskUnitX, deskUnitY core.Unit) *window.TearOffHost {
	d.mu.RLock()
	plat := d.platform
	surf := d.surface
	wm := d.windowManager
	d.mu.RUnlock()
	if plat == nil || surf == nil {
		return nil
	}
	native, ok := surf.(platform.NativeSurface)
	if !ok {
		return nil
	}
	gp, ok := plat.(platform.GlobalPointerPlatform)
	if !ok {
		return nil
	}

	deskX, deskY := native.ScreenPositionPx()
	b := win.Bounds()
	newSurf, err := plat.CreateSurface(platform.SurfaceOptions{
		Title:          win.Title(),
		Borderless:     true,
		CornerRadiusPx: d.unitToPx(window.FrameCornerRadius()),
		XPx:            deskX + d.unitToPx(deskUnitX),
		YPx:            deskY + d.unitToPx(deskUnitY),
		WidthPx:        d.unitToPx(b.Width),
		HeightPx:       d.unitToPx(b.Height),
	})
	if err != nil {
		return nil
	}

	wm.RemoveWindow(win)

	// The window is leaving the desktop for its own surface, so it must
	// not linger in the desktop dock. A minimized window (e.g. a follower
	// that was docked when its main window tore off) also un-minimizes,
	// so it actually shows on its torn surface instead of staying hidden.
	if win.IsMinimized() {
		win.Restore()
	}
	if d.dockRow != nil {
		d.dockRow.RemoveEntryByID(win.ObjectID())
	}

	var host *window.TearOffHost
	// A detached window re-docks by dragging its '#' handle back over
	// the desktop, or by clicking it. The host only calls this during
	// a HANDLE drag - a plain title drag just moves the OS window.
	host = window.NewTearOffHost(win, newSurf, d.pxPerUnit(), gp.GlobalPointerPx,
		func(gx, gy int, grabX, grabY core.Unit) bool {
			return d.redockAt(host, gx, gy, grabX, grabY)
		})
	host.SetGhostRelay(
		func(gx, gy int) {
			ux, uy := d.globalToDesktopUnits(gx, gy)
			d.dispatchEvent(core.MouseMoveEvent{X: ux, Y: uy, Buttons: core.LeftButton})
			d.invalidateSurface()
		},
		func() {
			gx, gy := gp.GlobalPointerPx()
			ux, uy := d.globalToDesktopUnits(gx, gy)
			d.dispatchEvent(core.MouseReleaseEvent{X: ux, Y: uy, Button: core.LeftButton})
			if native, ok := newSurf.(platform.NativeSurface); ok {
				native.Close()
			}
			d.invalidateSurface()
		})
	host.SetOnClosed(func() { d.dropTornHost(host) })
	host.SetClipboardAccess(d.Clipboard, d.SetClipboard)

	// App/window modals block this torn window across surfaces just as they
	// block in-surface windows; a press while blocked surfaces (OS-restores)
	// the blocking modal, mirroring the in-surface dock restore.
	host.SetModalChecker(
		func() bool { return d.windowManager.IsTornWindowBlocked(win) },
		func() { d.surfaceBlockingModal(win) })

	// The torn surface drives the same system-cursor control as the
	// desktop, so resize/edge and text cursors work over it too.
	if cc, ok := plat.(platform.CursorController); ok {
		host.SetCursorSetter(cc.SetCursor)
	}

	// Match the desktop's in-surface resize-edge thickness so torn edges
	// are the same width as docked ones (and don't overlap edge trinkets
	// such as scrollbars).
	host.SetResizeGrip(d.resizeGrip)

	// A torn window still borrows the desktop's menu bar line: when its
	// surface gains focus, point the menu bar at this window's app so the
	// app's menus are actually reachable (they showed nowhere before).
	host.SetOnFocus(func(focused bool) {
		if focused {
			d.windowFocusChanged(win)
			d.invalidateSurface()
		}
	})

	// The window now reads as detached (handle shows '#'); clicking
	// the handle (or Cmd-style activation) re-docks it to the desktop.
	win.SetDetached(true)
	// A detached main window carries the app's own menu bar + status bar.
	d.attachMainWindowChrome(win)
	// The detached menu bar's parent is the window, which can't provide the
	// desktop timer system its dropdowns need for hover auto-scroll. Wire
	// the bar to the desktop's timers and this surface's repaint directly.
	if mb, ok := win.WindowMenuBar().(*MenuBar); ok {
		mb.SetScrollTimerStarter(func(interval time.Duration, cb func()) interface{ Stop() } {
			return d.StartRepeatingTimer(interval, cb)
		})
		mb.SetRequestUpdate(host.Invalidate)
	}
	win.SetOnTearRequest(func() { d.redockInPlace(host) })

	d.mu.Lock()
	d.tornHosts = append(d.tornHosts, host)
	d.mu.Unlock()

	// A main window drags its non-tearable children off the desktop with
	// it: dialogs and other windows that can't be torn off by hand live on
	// their own surfaces while the main window is detached, and re-dock
	// with it. Only fires for a genuine main window (a follower being torn
	// here is not any app's main window, so it does not recurse). The main
	// window then rises above those children and takes focus, so the tear
	// ends with it on top rather than buried under a large child surface.
	if app := d.applicationForMainWindow(win); app != nil {
		// b was captured before NewTearOffHost zeroed the window's origin,
		// so it still holds the main window's docked bounds - the reference
		// for keeping each child at the same relative position.
		d.tearOffFollowers(app, win, b)
		if n, ok := host.Surface().(platform.NativeSurface); ok {
			n.Raise()
		}
		d.windowFocusChanged(win)
	}
	return host
}

// tornHostForWindow returns the torn host currently hosting win, or nil.
func (d *Desktop) tornHostForWindow(win *window.Window) *window.TearOffHost {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, h := range d.tornHosts {
		if h.Window() == win {
			return h
		}
	}
	return nil
}

// surfaceBlockingModal is called when a press lands on a modally-blocked torn
// window: it surfaces whatever modal actually blocks that window - window-,
// application-, or system-level - OS-restoring a torn modal (or dock-restoring
// an in-surface one) if minimized, so the user is pulled to the modal they must
// resolve. Mirrors the in-surface pattern where clicking a blocked window (or
// the wallpaper) surfaces the blocking modal. Using TopModalBlocking (not just
// the app stack) means an owner-scoped modal - e.g. a dialog owned by the solo
// window - is surfaced too, not only application modals.
func (d *Desktop) surfaceBlockingModal(win *window.Window) {
	wm := d.windowManager
	if wm == nil {
		return
	}
	d.surfaceModal(wm.TopModalBlocking(win))
}

// surfaceActiveAppModal surfaces the modal of the application whose menu bar is
// currently showing (a wallpaper click). A background app's modal is not
// touched: only the app the user is looking at can be surfaced this way.
func (d *Desktop) surfaceActiveAppModal() {
	d.mu.RLock()
	active := d.activeApp
	d.mu.RUnlock()
	if active != nil {
		d.surfaceAppModal(active.ObjectID())
	}
}

// surfaceAppModal raises (or restores, incl. OS-restore of a torn one) the top
// modal of the given application, so the user is pulled to the modal blocking
// that app. Used by the wallpaper-click path (surfaceActiveAppModal).
func (d *Desktop) surfaceAppModal(appID core.ObjectID) {
	if wm := d.windowManager; wm != nil {
		d.surfaceModal(wm.TopAppModal(appID))
	}
}

// surfaceModal pulls a specific modal window to the front: if it is torn onto
// its own OS surface, un-minimize it (OS-restore) and raise that surface back
// over the window the user just clicked; otherwise (in-surface) restore it from
// the dock if minimized, else raise and activate it so the user lands on it
// ready to interact. Nil is a no-op.
func (d *Desktop) surfaceModal(modal *window.Window) {
	wm := d.windowManager
	if wm == nil || modal == nil {
		return
	}
	if h := d.tornHostForWindow(modal); h != nil {
		surf := h.Surface()
		// Restore before raising if the modal is minimized at EITHER level: the
		// OS window (Minimized(), set when the user minimizes via the OS window
		// controls - the app-level flag below doesn't capture that) or the
		// window's own minimized flag. Raise alone won't un-minimize an
		// OS-minimized window, so it would otherwise stay hidden.
		osMinimized := false
		if n, ok := surf.(platform.NativeSurface); ok {
			osMinimized = n.Minimized()
		}
		if osMinimized || modal.IsMinimized() {
			if r, ok := surf.(platform.NativeRestorer); ok {
				r.Restore()
			}
			if modal.IsMinimized() {
				modal.Restore()
			}
		}
		if n, ok := surf.(platform.NativeSurface); ok {
			n.Raise()
		}
		return
	}
	if modal.IsMinimized() {
		wm.RestoreWindow(modal)
	} else {
		wm.ActivateWindow(modal)
	}
}

// tearOffFollowers tears every non-tearable child of app (other than the
// main window itself) onto its own surface. Children are torn in the
// window manager's z-order (back to front) so their new surfaces stack in
// the same order they had on the desktop, and each keeps the position it
// held relative to the main window (mainDocked, the main window's bounds
// before it was torn) - the tear preserves the layout as it was. Called
// when the app's main window is torn off.
func (d *Desktop) tearOffFollowers(app ApplicationProvider, main *window.Window, mainDocked core.UnitRect) {
	d.mu.RLock()
	wm := d.windowManager
	d.mu.RUnlock()
	if wm == nil {
		return
	}
	for _, w := range wm.Windows() {
		if w == main || w.IsTearable() || w.IsDetached() || !appOwnsWindow(app, w) {
			continue
		}
		x, y := d.positionRelativeToMain(main, mainDocked, w)
		d.createTornHost(w, x, y)
		d.setChildShortcutResolver(w, main)
	}
}

// mainTornUnitOrigin returns the top-left of the main window's torn-off
// surface in desktop units, and whether it could be located.
func (d *Desktop) mainTornUnitOrigin(main *window.Window) (core.Unit, core.Unit, bool) {
	d.mu.RLock()
	surf := d.surface
	hosts := make([]*window.TearOffHost, len(d.tornHosts))
	copy(hosts, d.tornHosts)
	d.mu.RUnlock()

	deskNative, ok := surf.(platform.NativeSurface)
	if !ok {
		return 0, 0, false
	}
	var mainNative platform.NativeSurface
	for _, h := range hosts {
		if h.Window() == main {
			if n, ok := h.Surface().(platform.NativeSurface); ok {
				mainNative = n
			}
			break
		}
	}
	if mainNative == nil {
		return 0, 0, false
	}
	deskX, deskY := deskNative.ScreenPositionPx()
	mx, my := mainNative.ScreenPositionPx()
	return d.pxToUnit(mx - deskX), d.pxToUnit(my - deskY), true
}

// positionRelativeToMain returns the desktop-unit top-left that keeps
// child at the same offset from the main window it had while docked.
// mainDocked is the main window's bounds captured before it was torn (its
// live bounds now read as origin-zero on its own surface). Falls back to
// the child's own bounds when the main surface is unknown.
func (d *Desktop) positionRelativeToMain(main *window.Window, mainDocked core.UnitRect, child *window.Window) (core.Unit, core.Unit) {
	cb := child.Bounds()
	ox, oy, ok := d.mainTornUnitOrigin(main)
	if !ok {
		return cb.X, cb.Y
	}
	return ox + (cb.X - mainDocked.X), oy + (cb.Y - mainDocked.Y)
}

// centerOverMain returns the desktop-unit top-left at which a child of
// size childW x childH is centered over the app's torn-off main window.
// Used only for freshly created child windows. Falls back to the supplied
// rectangle's origin when the main window's torn surface can't be located.
func (d *Desktop) centerOverMain(main *window.Window, childW, childH core.Unit, fallback core.UnitRect) (core.Unit, core.Unit) {
	ox, oy, ok := d.mainTornUnitOrigin(main)
	if !ok {
		return fallback.X, fallback.Y
	}
	mb := main.Bounds()
	return ox + (mb.Width-childW)/2, oy + (mb.Height-childH)/2
}

// redockFollowers re-docks every torn non-tearable child of app back onto
// the desktop at its current on-screen position. Called when the app's
// main window re-docks.
func (d *Desktop) redockFollowers(app ApplicationProvider, main *window.Window) {
	d.mu.RLock()
	hosts := make([]*window.TearOffHost, len(d.tornHosts))
	copy(hosts, d.tornHosts)
	d.mu.RUnlock()
	for _, h := range hosts {
		w := h.Window()
		if w == main || w.IsTearable() || !appOwnsWindow(app, w) {
			continue
		}
		d.redockInPlace(h)
	}
}

// appOwnsWindow reports whether win is one of app's windows.
func appOwnsWindow(app ApplicationProvider, win *window.Window) bool {
	for _, w := range app.Windows() {
		if w == win {
			return true
		}
	}
	return false
}

// SyncAddedWindowDetachState tears a freshly added window off immediately
// when its app's main window is already detached and the new window is a
// non-tearable child - so a dialog spawned by a torn-off main window
// appears torn off too, rather than docked back on the desktop. Called by
// Application.AddWindow after the window joins the window manager.
func (d *Desktop) SyncAddedWindowDetachState(win *window.Window) {
	if win == nil || win.IsTearable() || win.IsDetached() {
		return
	}
	app := d.applicationForWindow(win)
	if app == nil {
		return
	}
	main := app.MainWindow()
	if main == nil || main == win || !main.IsDetached() {
		return
	}
	b := win.Bounds()
	x, y := d.centerOverMain(main, b.Width, b.Height, b)
	d.createTornHost(win, x, y)
	d.setChildShortcutResolver(win, main)
}

// setChildShortcutResolver lets a torn-off child window service its app's
// keyboard shortcuts through the same menu bar its detached main window
// hosts, so Cut/Copy/Paste (etc.) work when the child has focus even
// though the child carries no chrome of its own. Read lazily at event
// time, so a later chrome change (or re-dock) is reflected.
func (d *Desktop) setChildShortcutResolver(child, main *window.Window) {
	child.SetShortcutResolver(func(ev core.KeyPressEvent) bool {
		mb := main.WindowMenuBar()
		if mb == nil {
			return false
		}
		if sc, ok := mb.(interface {
			HandleShortcut(core.KeyPressEvent) bool
		}); ok {
			return sc.HandleShortcut(ev)
		}
		return false
	})
}

// applicationForWindow returns the application that owns win, or nil.
func (d *Desktop) applicationForWindow(win *window.Window) ApplicationProvider {
	d.mu.RLock()
	apps := make([]ApplicationProvider, len(d.applications))
	copy(apps, d.applications)
	d.mu.RUnlock()
	for _, app := range apps {
		if appOwnsWindow(app, win) {
			return app
		}
	}
	return nil
}

// redockInPlace re-docks a torn window to the desktop at its current
// on-screen position, retaining its size - the '#' handle click path.
func (d *Desktop) redockInPlace(host *window.TearOffHost) {
	d.mu.RLock()
	surf := d.surface
	d.mu.RUnlock()
	deskNative, ok := surf.(platform.NativeSurface)
	if !ok {
		return
	}
	tornNative, ok := host.Surface().(platform.NativeSurface)
	if !ok {
		return
	}
	deskX, deskY := deskNative.ScreenPositionPx()
	tx, ty := tornNative.ScreenPositionPx()
	ux := d.pxToUnit(tx - deskX)
	uy := d.pxToUnit(ty - deskY)
	d.adoptTornWindow(host, ux, uy, false)
}

// handleTornDrag continues a live tear gesture from the desktop's
// event stream (the desktop window still owns the capture). Returns
// false when no tear drag is active so normal dispatch proceeds.
func (d *Desktop) handleTornDrag(event core.Event) bool {
	d.mu.RLock()
	td := d.tornDrag
	surf := d.surface
	d.mu.RUnlock()
	if td == nil {
		return false
	}

	switch e := event.(type) {
	case core.MousePressEvent:
		// A fresh press means the tearing gesture ended somewhere we
		// couldn't see (the torn window took the release). Stale
		// state: the window stays torn, the press proceeds normally.
		d.clearTornDrag(td)
		return false

	case core.MouseMoveEvent:
		if e.Buttons&core.LeftButton == 0 {
			// Button no longer held: the release went to the torn
			// window. The gesture is over; do NOT re-dock on a mere
			// hover.
			d.clearTornDrag(td)
			return false
		}
		// Position from the GLOBAL pointer, not the event: when OS
		// mouse capture is lost mid-gesture (window churn can drop
		// it), SDL clamps window-relative motion to the window rect,
		// which would fence the torn window into a small range around
		// the desktop. The events remain the ticks; the global
		// pointer is the truth.
		ux, uy := e.X, e.Y
		gx, gy := 0, 0
		haveGlobal := false
		d.mu.RLock()
		plat := d.platform
		d.mu.RUnlock()
		if gp, ok := plat.(platform.GlobalPointerPlatform); ok {
			gx, gy = gp.GlobalPointerPx()
			ux, uy = d.globalToDesktopUnits(gx, gy)
			haveGlobal = true
		}
		size := surf.Size()
		if ux >= 0 && uy >= 0 && ux < size.Width && uy < size.Height {
			// Pointer came home: re-dock and hand the drag straight
			// back to the window manager.
			d.clearTornDrag(td)
			// The desktop owns this gesture's mouse session, so the
			// torn surface can be destroyed immediately.
			d.adoptTornWindow(td.host, ux-td.offX, uy-td.offY, false)
			d.windowManager.BeginDrag(td.host.Window(), td.offX, td.offY)
			return true
		}
		if native, ok := td.surf.(platform.NativeSurface); ok {
			if haveGlobal {
				native.SetScreenPositionPx(gx-d.unitToPx(td.offX), gy-d.unitToPx(td.offY))
			} else if deskNative, ok := surf.(platform.NativeSurface); ok {
				deskX, deskY := deskNative.ScreenPositionPx()
				native.SetScreenPositionPx(
					deskX+d.unitToPx(e.X-td.offX),
					deskY+d.unitToPx(e.Y-td.offY))
			}
		}
		return true

	case core.MouseReleaseEvent:
		// Dropped outside: the window stays torn off; its host owns
		// any further drags.
		_ = e
		d.clearTornDrag(td)
		return true
	}
	return false
}

// clearTornDrag ends the desktop-driven tear phase and disarms the
// host's mirror of the same gesture.
func (d *Desktop) clearTornDrag(td *tornDrag) {
	d.mu.Lock()
	if d.tornDrag == td {
		d.tornDrag = nil
	}
	d.mu.Unlock()
	td.host.EndDrag()
}

// redockAt serves a TearOffHost handle drag: when the global pointer
// is over the desktop surface, reclaim the window there (retaining
// size), enforcing the reachability bounds. The torn surface stays
// alive as a ghost until its live mouse session finishes.
func (d *Desktop) redockAt(host *window.TearOffHost, gx, gy int, grabX, grabY core.Unit) bool {
	d.mu.RLock()
	surf := d.surface
	d.mu.RUnlock()
	native, ok := surf.(platform.NativeSurface)
	if !ok || native.Minimized() {
		return false
	}
	deskX, deskY := native.ScreenPositionPx()
	size := surf.Size()
	ux := d.pxToUnit(gx - deskX)
	uy := d.pxToUnit(gy - deskY)
	if ux < 0 || uy < 0 || ux >= size.Width || uy >= size.Height {
		return false
	}
	d.adoptTornWindow(host, ux-grabX, uy-grabY, true)
	d.windowManager.BeginDrag(host.Window(), grabX, grabY)
	return true
}

// dropTornHost disposes of a torn window's surface and forgets the
// host (the window closed itself while torn).
func (d *Desktop) dropTornHost(host *window.TearOffHost) {
	d.mu.Lock()
	if d.tornDrag != nil && d.tornDrag.host == host {
		d.tornDrag = nil
	}
	// A closing torn window can't keep owning focus/the menu bar line. Note
	// whether it DID own focus - if so, focus must be handed back to another
	// window below (the OS won't do it for us when the surface is destroyed).
	wasFocus := d.tornFocusOwner == host.Window()
	if wasFocus {
		d.tornFocusOwner = nil
	}
	for i, th := range d.tornHosts {
		if th == host {
			d.tornHosts = append(d.tornHosts[:i], d.tornHosts[i+1:]...)
			break
		}
	}
	wasPrimary := host == d.soloPrimaryHost
	if wasPrimary {
		d.soloPrimaryHost = nil
	}
	solo := d.solo
	d.mu.Unlock()
	closing := host.Window()
	if native, ok := host.Surface().(platform.NativeSurface); ok {
		native.Close()
	}
	d.invalidateSurface()

	// Hand focus back when the closing window held it. A torn window is not in
	// the window manager, so RemoveWindow's "activate the topmost remaining
	// window" never runs for it, and destroying the OS surface leaves no window
	// focused. Refocus the window this one floated over (its owner - a dialog
	// returns focus to what it covered), else the solo primary / top remaining
	// torn window. Skip when the primary itself closed (soloRebalance promotes a
	// peer instead). Posted so it runs after the surface is actually destroyed
	// (Close defers that to the main loop too).
	if wasFocus && !wasPrimary {
		d.Post(func() { d.refocusAfterTornClose(closing) })
	}

	// In solo mode: the primary surface can't be closed, so when its
	// window closes a remaining window is promoted onto it; when no
	// windows remain the host quits.
	if solo {
		d.Post(func() { d.soloRebalance(wasPrimary) })
	}
}

// refocusAfterTornClose gives OS focus (and desktop/app focus) back to the
// window a just-closed torn window floated over: its owner if it has one, else
// the solo primary window, else the top remaining torn window. It raises that
// window's OS surface - its own torn surface if it has one, otherwise the
// desktop's primary surface (a docked window, or the solo primary host) - and
// re-points desktop focus at it.
func (d *Desktop) refocusAfterTornClose(closing *window.Window) {
	d.mu.RLock()
	primary := d.soloPrimaryHost
	surf := d.surface
	hosts := append([]*window.TearOffHost(nil), d.tornHosts...)
	d.mu.RUnlock()

	target := closing.Owner()
	if target == nil {
		switch {
		case primary != nil:
			target = primary.Window()
		case len(hosts) > 0:
			target = hosts[len(hosts)-1].Window()
		}
	}
	if target == nil {
		return
	}

	raised := false
	for _, h := range hosts {
		if h.Window() == target {
			if ns, ok := h.Surface().(platform.NativeSurface); ok {
				ns.Raise()
				raised = true
			}
			break
		}
	}
	if !raised {
		if ns, ok := surf.(platform.NativeSurface); ok {
			ns.Raise()
		}
	}
	d.windowFocusChanged(target)
}

// globalToDesktopUnits converts a global pixel position to desktop
// surface units.
func (d *Desktop) globalToDesktopUnits(gx, gy int) (core.Unit, core.Unit) {
	d.mu.RLock()
	surf := d.surface
	d.mu.RUnlock()
	native, ok := surf.(platform.NativeSurface)
	if !ok {
		return 0, 0
	}
	deskX, deskY := native.ScreenPositionPx()
	return d.pxToUnit(gx - deskX), d.pxToUnit(gy - deskY)
}

// invalidateSurface requests a desktop repaint.
func (d *Desktop) invalidateSurface() {
	d.mu.RLock()
	surf := d.surface
	d.mu.RUnlock()
	if surf != nil {
		surf.Invalidate(core.UnitRect{})
	}
}

// adoptTornWindow puts the window back under the window manager at
// the given desktop-unit position. ghost keeps the torn surface
// alive but invisible (its mouse session must finish before it can
// be destroyed); otherwise it closes immediately.
func (d *Desktop) adoptTornWindow(host *window.TearOffHost, x, y core.Unit, ghost bool) {
	d.mu.Lock()
	if d.tornDrag != nil && d.tornDrag.host == host {
		d.tornDrag = nil
	}
	for i, th := range d.tornHosts {
		if th == host {
			d.tornHosts = append(d.tornHosts[:i], d.tornHosts[i+1:]...)
			break
		}
	}
	d.mu.Unlock()
	host.EndDrag()

	win := host.Window()
	b := win.Bounds()

	if native, ok := hostSurface(host).(platform.NativeSurface); ok {
		if ghost {
			native.SetOpacity(0)
		} else {
			native.Close()
		}
	}

	win.SetFlags(host.SavedFlags())
	win.SetOnBoundsRequest(nil)
	// Re-docked: the handle reads '%' again and its click re-tears.
	win.SetDetached(false)
	// Docked again: the app's menus return to the desktop bar, and a child
	// no longer borrows its app's menu bar for shortcuts.
	d.detachMainWindowChrome(win)
	win.SetShortcutResolver(nil)
	// Drop any torn-surface resize highlight; the desktop's own hover
	// tracking takes over once docked.
	win.SetResizeHoverRects(nil)
	win.SetOnTearRequest(func() { d.tearOffInPlace(win) })
	d.windowManager.AddWindow(win)
	if win.IsMaximized() {
		// A window that was maximized when torn off re-fills the client
		// area of the desktop it docks into (which may differ in size from
		// wherever it was torn), rather than keeping its torn surface size.
		d.windowManager.MaximizeWindow(win)
	} else {
		// Keep the re-docked window reachable: title bar within the client
		// area, a couple of columns visible horizontally.
		win.SetBounds(d.windowManager.ClampToClientArea(core.UnitRect{X: x, Y: y, Width: b.Width, Height: b.Height}))
	}
	win.Layout()
	d.windowManager.ActivateWindow(win)

	d.mu.RLock()
	surf := d.surface
	d.mu.RUnlock()
	if surf != nil {
		surf.Invalidate(core.UnitRect{})
	}

	// A re-docking main window brings its non-tearable children home with
	// it, preserving their arrangement and z-order; the main window is then
	// re-activated so it re-docks last, on top of its children. A follower
	// re-docking here is not any app's main window, so this does not recurse.
	if app := d.applicationForMainWindow(win); app != nil {
		d.redockFollowers(app, win)
		d.windowManager.ActivateWindow(win)
	}

	// The re-dock is fully settled now (including any followers that came home
	// with a main window): if a modal is up, bring it back over everything and
	// refocus it so a modal that was active before the dock stays in charge.
	d.windowManager.RaiseTopModalOver(win)
}

// hostSurface exposes the host's surface for teardown.
func hostSurface(h *window.TearOffHost) platform.Surface { return h.Surface() }
