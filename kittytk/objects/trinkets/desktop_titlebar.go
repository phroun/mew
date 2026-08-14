package trinkets

import (
	"math"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/platform"
	"github.com/phroun/kittytk/style"
)

// The themed desktop frame ([window] desktop_frame=themed, the default):
// the desktop's own OS window carries no OS chrome at all. The desktop
// paints a title bar of its own — in the toolkit's window-title style, one
// cell tall, above the menu bar — and handles moving itself the way a torn
// window does: global pointer deltas onto the OS window's pixel origin.
// Resizing is the desktop-edge machinery next door (desktop_edgeresize.go);
// its top sliver rides over the title row exactly as it does on a torn
// window's title bar. Minimize and Zoom are system (Ψ) menu items rather
// than title-bar buttons.
//
// The bar only appears where the whole story can be delivered: a graphical
// surface that is a platform.NativeSurface (so the drag can actually move
// the OS window), in themed mode, and not in solo mode (there the surface
// IS the app's window, which paints its own title bar via its tear-off
// host). A title bar that could not move its window would be an affordance
// that lies, so it is not painted at all in the other cases.
//
// The menu bar stays origin-local (it paints at Y=0 and hit-tests its own
// row internally), so the title row's shift is carried at the desktop
// boundary: the bar's Bounds get the real Y, painting goes through an
// offset painter, and the input paths translate event Y by the title
// height on the way in.

// hostMoveState is the themed title bar's drag-to-move gesture, plus the
// double-click memory (a double-click on the drag area zooms, the same
// convention the window manager applies to window title bars). Guarded by
// Desktop.mu, like hostEdgeState.
type hostMoveState struct {
	active           bool
	startGX, startGY int // global pointer at press, device px
	startX, startY   int // OS window origin at press, device px

	// clicks is the kit's double-click tracker (400ms, one cell, consume
	// on fire) — the same convention the window manager applies to window
	// title bars, from the same code.
	clicks window.DoubleClickTracker
}

// The title bar's controls, on the left like every title bar in the
// system: [x][.][^]. The same constants serve as the bar's keyboard focus
// states (none / a button / the title itself, which arrows then move and
// Shift+arrows grow and shrink, exactly like a window's focused title).
const (
	hostTitleButtonNone = iota
	hostTitleButtonClose
	hostTitleButtonMinimize
	hostTitleButtonZoom
	hostTitleFocusTitle
)

// hostZoomedNow reports whether the desktop's OS window is filling the
// screen zoomed right now — by its own Zoom or an OS maximize — which
// restyles the frame the way a maximized window is styled: flush to the
// edges, no border reserved, no frame stroke. Lock-free like
// themedFrameActive (it sits under the same layout paths).
func (d *Desktop) hostZoomedNow() bool {
	if d.hostZoom.zoomed {
		return true
	}
	if z, ok := d.surface.(platform.NativeZoomReporter); ok {
		return z.NativeZoomed()
	}
	return false
}

// hostFrameInset is the frame border the themed desktop RESERVES around
// its chrome and content, exactly as a window does ("the frame border
// rests OUTSIDE the content coordinate system... a thicker border shrinks
// the interior rather than overlapping it" — Window.contentBounds). Zero
// in every other mode, and zero while zoomed — a maximized window is
// flush to the edges with no border.
//
// Lock-free like themedFrameActive: it sits under layoutChildren, which
// SetBackend reaches while holding d.mu.
func (d *Desktop) hostFrameInset() core.Unit {
	if !d.themedFrameActive() || d.hostZoomedNow() {
		return 0
	}
	ppu := 1.0
	if m, ok := d.backend.(core.UnitPixelMapper); ok {
		if p := m.PxPerUnit(); p > 0 {
			ppu = p
		}
	}
	return core.Unit(math.Ceil(float64(core.ScaledWindowFrameBorderPx(ppu)) / ppu))
}

// hostChromeOffset is where the menu bar row begins: past the left border
// and below the border + title row. (0,0) in the other frame modes, so
// the pre-themed geometry is unchanged there.
func (d *Desktop) hostChromeOffset() (x, y core.Unit) {
	b := d.hostFrameInset()
	return b, b + d.TitleBarHeight()
}

// The desktop frame's three states, themed like a window's: its own chrome
// holding the keyboard is a focused window (double border), focus down in
// a child window is quasi-active (lit, heavy single border), and a blurred
// OS window is inactive.
const (
	hostFrameInactive = iota
	hostFrameQuasi
	hostFrameActive
)

// hostFrameState reads the desktop frame's current state: inactive when
// the OS window has no focus; active when the desktop's own chrome (title
// bar, menu bar, or dock) holds the keyboard — or when there is no child
// window to hold it; quasi-active while a child window does.
func (d *Desktop) hostFrameState() int {
	d.mu.RLock()
	unfocused := d.hostUnfocused
	titleFocused := d.hostTitleFocus != hostTitleButtonNone
	wm := d.windowManager
	d.mu.RUnlock()
	if unfocused {
		return hostFrameInactive
	}
	if titleFocused ||
		(d.menuBar != nil && d.menuBar.HasFocus()) ||
		(d.dockRow != nil && d.dockRow.HasFocus()) {
		return hostFrameActive
	}
	if wm != nil && wm.ActiveWindow() != nil {
		return hostFrameQuasi
	}
	return hostFrameActive
}

// SetTitle sets the text shown on the desktop's own themed title bar
// (typically the same [window] title the OS title bar would have shown).
// Inert in the other frame modes and on cell surfaces.
func (d *Desktop) SetTitle(title string) {
	d.mu.Lock()
	changed := d.hostTitle != title
	d.hostTitle = title
	d.mu.Unlock()
	if changed {
		d.RequestUpdate()
	}
}

// themedFrameActive reports whether the desktop is carrying its own title
// bar right now: themed mode, on a graphical surface the desktop can
// actually move (a platform.NativeSurface), and not in solo mode.
//
// Deliberately lock-free, like menuBarShown: it sits under layoutChildren
// and Paint, which are reached both with and without d.mu held (SetBackend
// seeds the root metrics — and so lays out — while holding the lock), so
// taking even a read lock here self-deadlocks. The fields it reads are set
// at startup and on rare mode transitions.
func (d *Desktop) themedFrameActive() bool {
	if !d.graphicalFrames || d.solo || d.desktopFrameLocked() != DesktopFrameThemed {
		return false
	}
	_, native := d.surface.(platform.NativeSurface)
	return native
}

// hostTitleMetrics resolves the themed bar's title-bar kit metrics — the
// same kit every window title bar measures with, so the desktop's bar
// scales (core.TitleBarScale) and lays out identically. Lock-free like
// themedFrameActive: TitleBarHeight sits under layout paths that run
// while d.mu is held (SetBackend seeds metrics inside the lock), so
// d.font is read directly rather than through EffectiveFont's lock.
func (d *Desktop) hostTitleMetrics() window.TitleBarMetrics {
	f := d.font
	if f == nil {
		f = core.DefaultFont()
	}
	return window.TitleBarMetricsFor(d.EffectiveCellMetrics(), f, d.graphicalFrames)
}

// TitleBarHeight is the height of the desktop's own themed title bar row:
// the kit's (possibly scaled) row when the themed frame is active, 0 in
// every other mode (the companion to MenuBarHeight, one row further out).
func (d *Desktop) TitleBarHeight() core.Unit {
	if !d.themedFrameActive() {
		return 0
	}
	return d.hostTitleMetrics().RowH
}

// wantsNativeBorder reports whether the desktop's primary surface should
// carry the OS chrome while the DESKTOP owns it. Solo mode strips the
// border regardless (the app's chrome is the only title bar there); this
// is what to restore to when solo ends.
func (d *Desktop) wantsNativeBorder() bool {
	d.mu.RLock()
	graphical := d.graphicalFrames
	frame := d.desktopFrameLocked()
	surf := d.surface
	d.mu.RUnlock()
	if !graphical || frame != DesktopFrameThemed {
		return true
	}
	// Without a native surface the themed frame cannot deliver its drag, so
	// the bar is not painted (themedFrameActive) and the chrome must stay.
	if _, ok := surf.(platform.NativeSurface); !ok {
		return true
	}
	return false
}

// applyDesktopFrame applies the chosen frame mode to the freshly created
// primary surface: themed strips the OS chrome so the desktop's own title
// bar is the only one. The other modes keep the OS chrome as-is.
func (d *Desktop) applyDesktopFrame(surf platform.Surface) {
	if d.wantsNativeBorder() {
		return
	}
	if bt, ok := surf.(platform.BorderToggler); ok {
		bt.SetBordered(false)
	}
}

// hostTitleButtonAt returns the control under a surface-local point, or
// hostTitleButtonNone. The buttons sit on the left in button-width slots
// of three cells, like every title bar's controls — inset past the
// reserved frame border, inside which the title row sits.
func (d *Desktop) hostTitleButtonAt(x, y core.Unit) int {
	th := d.TitleBarHeight()
	if th == 0 {
		return hostTitleButtonNone
	}
	tm := d.hostTitleMetrics()
	b := d.hostFrameInset()
	// The controls start one cell in from the (border-inset) left edge,
	// exactly where a window's do ("Start after left border") — and flush
	// at the edge while zoomed, the maximized-frame convention.
	lead := tm.CellW
	if d.hostZoomedNow() {
		lead = 0
	}
	x, y = x-b-lead, y-b
	if y < 0 || y >= th || x < 0 {
		return hostTitleButtonNone
	}
	switch {
	case x < tm.ButtonW:
		return hostTitleButtonClose
	case x < tm.ButtonW*2:
		return hostTitleButtonMinimize
	case x < tm.ButtonW*3:
		return hostTitleButtonZoom
	}
	return hostTitleButtonNone
}

// hostTitleButtonTrigger runs a control's action: the close box exits the
// desktop (the same verb as the Ψ menu's Exit item), the others are the Ψ
// menu's Minimize and Zoom.
func (d *Desktop) hostTitleButtonTrigger(btn int) {
	switch btn {
	case hostTitleButtonClose:
		d.ExitDesktop()
	case hostTitleButtonMinimize:
		d.hostMinimize()
	case hostTitleButtonZoom:
		d.hostZoomToggle()
	}
}

// hostTitleHoverUpdate tracks which control the pointer is over, for the
// hover highlight. The desktop's own resize zones outrank the buttons —
// a press there resizes (hostResizeBegin runs first), so the two hover
// affordances must never light together: where the edge zone claims the
// point, no button reads as hovered.
func (d *Desktop) hostTitleHoverUpdate(x, y core.Unit) {
	btn := d.hostTitleButtonAt(x, y)
	if btn != hostTitleButtonNone && d.hostEdgeAt(x, y) != 0 {
		btn = hostTitleButtonNone
	}
	d.mu.Lock()
	changed := d.hostTitleHover != btn
	d.hostTitleHover = btn
	d.mu.Unlock()
	if changed {
		d.RequestUpdate()
	}
}

// hostTitleHoverClear drops the hover highlight and any armed press when
// the pointer leaves the surface.
func (d *Desktop) hostTitleHoverClear() {
	d.mu.Lock()
	changed := d.hostTitleHover != hostTitleButtonNone || d.hostTitlePressed != hostTitleButtonNone
	d.hostTitleHover = hostTitleButtonNone
	d.hostTitlePressed = hostTitleButtonNone
	d.mu.Unlock()
	if changed {
		d.RequestUpdate()
	}
}

// hostMoveBegin claims a press on the themed title bar. Consulted after
// hostResizeBegin (the resize sliver rides over the title row's outer
// edge, like a torn window's) and before the window manager. A press on a
// control arms that button — it acts on release over the same button,
// like a window's — and anywhere else starts the drag-to-move.
func (d *Desktop) hostMoveBegin(e core.MousePressEvent) bool {
	if e.Button != core.LeftButton {
		return false
	}
	th := d.TitleBarHeight()
	b := d.hostFrameInset()
	// The title row sits inside the top border; the border band itself
	// belongs to the resize zones (consulted before this).
	if th == 0 || e.Y < b || e.Y >= b+th {
		return false
	}
	// An open dropdown owns the next press anywhere on the surface (it is
	// how the menu closes); the bar must not swallow it.
	if d.menuBar != nil && d.menuBar.ActiveMenu() != nil {
		return false
	}
	if btn := d.hostTitleButtonAt(e.X, e.Y); btn != hostTitleButtonNone {
		d.mu.Lock()
		d.hostTitlePressed = btn
		d.hostTitleHover = btn
		d.mu.Unlock()
		d.RequestUpdate()
		return true
	}
	native, gp, ok := d.hostResizeParts()
	if !ok {
		return false
	}
	// A double-click on the drag area zooms — the kit's tracker, the same
	// convention (400ms, within a cell, consume on fire) the window
	// manager applies to window title bars.
	metrics := d.EffectiveCellMetrics()
	d.mu.Lock()
	st := &d.hostMove
	isDouble := st.clicks.Press(e.X, e.Y, metrics)
	d.mu.Unlock()
	if isDouble {
		d.hostZoomToggle()
		return true
	}

	gx, gy := gp.GlobalPointerPx()
	x, y := native.ScreenPositionPx()
	d.mu.Lock()
	st.active = true
	st.startGX, st.startGY = gx, gy
	st.startX, st.startY = x, y
	d.mu.Unlock()
	return true
}

// hostMoveMove owns the pointer stream while a title-bar gesture is in
// progress: an armed BUTTON tracks the pointer — the pressed visual
// releases when the pointer drifts off the button and re-arms when it
// drifts back, exactly a button's capture semantics — and an armed DRAG
// applies the delta to the OS window's origin. Reports false when
// neither is in progress.
func (d *Desktop) hostMoveMove(e core.MouseMoveEvent) bool {
	d.mu.RLock()
	pressed := d.hostTitlePressed
	st := d.hostMove
	d.mu.RUnlock()
	if pressed != hostTitleButtonNone {
		if e.Buttons&core.LeftButton == 0 {
			// The release was missed (left the surface mid-press): disarm
			// without firing.
			d.mu.Lock()
			d.hostTitlePressed = hostTitleButtonNone
			d.mu.Unlock()
			d.RequestUpdate()
		}
		// Hover keeps tracking under the held button, so the paint's
		// "pressed AND still over it" test stays truthful.
		d.hostTitleHoverUpdate(e.X, e.Y)
		return true
	}
	if !st.active {
		return false
	}
	if e.Buttons&core.LeftButton == 0 {
		// The release was missed (left the surface mid-gesture); end here.
		d.hostMoveFinish()
		return true
	}
	native, gp, ok := d.hostResizeParts()
	if !ok {
		d.hostMoveFinish()
		return true
	}
	gx, gy := gp.GlobalPointerPx()
	if gx != st.startGX || gy != st.startGY {
		// ACTUALLY dragging (not just a press): a moved window is an
		// ordinary floating window again, not the zoom rectangle.
		d.hostZoomForget()
	}
	native.SetScreenPositionPx(st.startX+gx-st.startGX, st.startY+gy-st.startGY)
	return true
}

// hostMoveEnd completes the title-bar gesture on release: an armed button
// fires if the pointer is still over it (a slide-away cancels, like a
// window's), a drag disarms. Reports false when neither was in progress.
func (d *Desktop) hostMoveEnd(e core.MouseReleaseEvent) bool {
	d.mu.Lock()
	pressed := d.hostTitlePressed
	d.hostTitlePressed = hostTitleButtonNone
	active := d.hostMove.active
	d.mu.Unlock()
	if pressed != hostTitleButtonNone {
		d.RequestUpdate()
		if d.hostTitleButtonAt(e.X, e.Y) == pressed {
			d.hostTitleButtonTrigger(pressed)
		}
		return true
	}
	if !active {
		return false
	}
	d.hostMoveFinish()
	return true
}

// hostMoveFinish disarms the drag.
func (d *Desktop) hostMoveFinish() {
	d.mu.Lock()
	d.hostMove.active = false
	d.mu.Unlock()
}

// hostZoomState is the Ψ menu's Zoom toggle: the OS window rectangle to
// restore when zooming back down. Guarded by Desktop.mu.
type hostZoomState struct {
	zoomed                     bool
	prevX, prevY, prevW, prevH int // device px, saved at zoom-up
}

// addHostWindowMenuItems puts Minimize and Zoom on the system (Ψ) menu,
// just above Exit Desktop. With no OS title bar there are no title-bar
// buttons to carry them, and the themed frame deliberately paints none —
// the system menu is where the desktop's own window verbs live. Called
// from RunOn once the surface exists, only when the themed frame is
// actually active (so the items never appear where they could not act).
func (d *Desktop) addHostWindowMenuItems() {
	if !d.themedFrameActive() || d.systemMenu == nil {
		return
	}
	minItem := NewMenuItem("&Minimize").SetOnTriggered(func() {
		d.hostMinimize()
	})
	zoomItem := NewMenuItem("&Zoom").SetOnTriggered(func() {
		d.hostZoomToggle()
	})
	// Insert [sep, Minimize, Zoom] immediately above the Exit item, so
	// Exit stays last however the menu grows.
	items := d.systemMenu.Items()
	at := len(items)
	for i, it := range items {
		if it != nil && it.Command == core.CmdDesktopExit {
			at = i
			break
		}
	}
	d.systemMenu.InsertItem(at, NewSeparator())
	d.systemMenu.InsertItem(at+1, minItem)
	d.systemMenu.InsertItem(at+2, zoomItem)
}

// hostMinimize miniaturizes the desktop's OS window (the Ψ menu item).
func (d *Desktop) hostMinimize() {
	d.mu.RLock()
	surf := d.surface
	d.mu.RUnlock()
	if native, ok := surf.(platform.NativeSurface); ok {
		native.Minimize()
	}
}

// hostZoomToggle is Zoom (the Ψ menu item and the [^] button): fill the
// display's work area, or put the window back where it was before the
// last zoom. It is the themed frame's stand-in for OS maximize — a plain
// move+resize, so the window stays freely resizable while zoomed.
//
// Deliberately NOT routed through hostResizeParts: that gate stands the
// resize zones down while the OS reports the window zoomed, and some
// window managers mark a window that exactly fills the work area as
// maximized — which made the second Zoom a silent no-op, unable to
// restore. Getting OUT of that state is exactly this verb's job, so it
// reaches for the surface directly, and asks the OS to square the window
// first (SDL's Restore releases a maximize) where the OS took it over.
func (d *Desktop) hostZoomToggle() {
	d.mu.RLock()
	surf := d.surface
	d.mu.RUnlock()
	native, ok := surf.(platform.NativeSurface)
	if !ok {
		return
	}
	osZoomed := false
	if z, ok := surf.(platform.NativeZoomReporter); ok {
		osZoomed = z.NativeZoomed()
	}
	d.mu.Lock()
	st := d.hostZoom
	d.mu.Unlock()

	if st.zoomed {
		// A geometry write is ignored while the OS holds the window
		// maximized; release that first, then restore our remembered rect.
		if osZoomed {
			if r, ok := surf.(platform.NativeRestorer); ok {
				r.Restore()
			}
		}
		d.mu.Lock()
		d.hostZoom.zoomed = false
		d.mu.Unlock()
		// Floating again: the corners round back before the frame redraws.
		if sq, ok := surf.(platform.NativeShapeSquarer); ok {
			sq.SetShapeSquared(false)
		}
		native.SetScreenPositionPx(st.prevX, st.prevY)
		native.SetScreenSizePx(st.prevW, st.prevH)
		d.RequestUpdate() // the zoom button's icon and the frame flip back
		return
	}
	if osZoomed {
		// The OS zoomed it without us (no remembered rect): Zoom means
		// "square it", and the OS holds the pre-maximize geometry.
		if r, ok := surf.(platform.NativeRestorer); ok {
			r.Restore()
			d.RequestUpdate()
			return
		}
	}
	x, y := native.ScreenPositionPx()
	w, h := native.ScreenSizePx()
	ax, ay, aw, ah := native.WorkAreaPx()
	if aw <= 0 || ah <= 0 {
		return
	}
	d.mu.Lock()
	d.hostZoom = hostZoomState{zoomed: true, prevX: x, prevY: y, prevW: w, prevH: h}
	d.mu.Unlock()
	// A screen-filling window keeps the maximized convention: square
	// corners, no shadow, no frame (hostFrameInset and paintHostFrame
	// consult the zoom state). The OS won't call this a maximize — it is
	// a plain move+resize — so the shape is squared by hand.
	if sq, ok := surf.(platform.NativeShapeSquarer); ok {
		sq.SetShapeSquared(true)
	}
	native.SetScreenPositionPx(ax, ay)
	native.SetScreenSizePx(aw, ah)
	d.RequestUpdate()
}

// hostZoomForget drops the zoom memory and re-rounds the corners: a manual
// edge-resize or title-bar drag makes the window an ordinary floating
// window again (its geometry no longer IS the zoom rectangle), so the
// frame comes back and the next Zoom starts fresh instead of "restoring".
func (d *Desktop) hostZoomForget() {
	d.mu.Lock()
	was := d.hostZoom.zoomed
	d.hostZoom.zoomed = false
	surf := d.surface
	d.mu.Unlock()
	if !was {
		return
	}
	if sq, ok := surf.(platform.NativeShapeSquarer); ok {
		sq.SetShapeSquared(false)
	}
	d.RequestUpdate()
}

// hostTitleFocused reports whether the themed title bar holds the keyboard.
func (d *Desktop) hostTitleFocused() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.hostTitleFocus != hostTitleButtonNone
}

// setHostTitleFocus moves the title bar's keyboard focus, announcing the
// landed-on control the way a window's title bar does.
func (d *Desktop) setHostTitleFocus(focus int) {
	d.mu.Lock()
	old := d.hostTitleFocus
	d.hostTitleFocus = focus
	zoomed := d.hostZoom.zoomed
	d.mu.Unlock()
	if focus == old {
		return
	}
	if focus != hostTitleButtonNone {
		if am := core.FindAccessibilityManager(d); am != nil {
			var name string
			switch focus {
			case hostTitleButtonClose:
				name = "exit desktop button"
			case hostTitleButtonMinimize:
				name = "minimize button"
			case hostTitleButtonZoom:
				if zoomed {
					name = "restore button"
				} else {
					name = "zoom button"
				}
			case hostTitleFocusTitle:
				d.mu.RLock()
				title := d.hostTitle
				d.mu.RUnlock()
				name = title + ", title bar"
			}
			if name != "" {
				am.AnnouncePolite(name)
			}
		}
	}
	d.RequestUpdate()
}

// enterHostTitleFocus lands the keyboard on the title bar from the chrome
// ring: on the first element coming forward (Tab off the dock's end), the
// last — the title itself — coming backward (Shift+Tab off the menu bar).
// Declines — reporting false so the ring can fall through to its next
// stop — when there is no themed title bar to land on.
func (d *Desktop) enterHostTitleFocus(forward bool) bool {
	if !d.themedFrameActive() {
		return false
	}
	// The bar being left keeps its trinket focus unless told otherwise (the
	// dock path clears its own; the menu bar's is cleared here, without the
	// dismiss notification that would restore a window's focus).
	if d.menuBar != nil {
		d.menuBar.CloseMenuWithoutRestore()
	}
	if forward {
		d.setHostTitleFocus(hostTitleButtonClose)
	} else {
		d.setHostTitleFocus(hostTitleFocusTitle)
	}
	return true
}

// handleHostTitleKey is the title bar's keyboard when it holds focus:
// Tab/Shift+Tab walk close → minimize → zoom → title and step off the
// ends of the chrome ring (forward to the menu bar, backward to the
// dock), activate runs the focused control, cancel drops the focus — and
// with the TITLE focused, arrows move the OS window and the size
// commands grow and shrink it, exactly like a window's focused title.
func (d *Desktop) handleHostTitleKey(event core.KeyPressEvent) bool {
	d.mu.RLock()
	focus := d.hostTitleFocus
	d.mu.RUnlock()
	if focus == hostTitleButtonNone {
		return false
	}
	cmd := d.KeyCommand(event.Key)
	switch cmd {
	case core.CmdFocusNext:
		switch focus {
		case hostTitleButtonClose:
			d.setHostTitleFocus(hostTitleButtonMinimize)
		case hostTitleButtonMinimize:
			d.setHostTitleFocus(hostTitleButtonZoom)
		case hostTitleButtonZoom:
			d.setHostTitleFocus(hostTitleFocusTitle)
		default:
			// Off the forward end: the menu bar is next in the ring.
			d.setHostTitleFocus(hostTitleButtonNone)
			if d.menuBar != nil {
				d.menuBar.ToggleMenuFocus()
			}
		}
		return true
	case core.CmdFocusPrior:
		switch focus {
		case hostTitleFocusTitle:
			d.setHostTitleFocus(hostTitleButtonZoom)
		case hostTitleButtonZoom:
			d.setHostTitleFocus(hostTitleButtonMinimize)
		case hostTitleButtonMinimize:
			d.setHostTitleFocus(hostTitleButtonClose)
		default:
			// Off the backward end: the dock is previous in the ring (the
			// menu bar where there is no dock).
			d.setHostTitleFocus(hostTitleButtonNone)
			if d.dockVisible() {
				d.FocusDock()
			} else if d.menuBar != nil {
				d.menuBar.ToggleMenuFocus()
			}
		}
		return true
	case core.CmdTrinketActivate:
		d.hostTitleButtonTrigger(focus)
		return true
	case core.CmdTrinketCancel:
		d.setHostTitleFocus(hostTitleButtonNone)
		return true
	}
	if focus == hostTitleFocusTitle {
		return d.handleHostTitleGeometry(cmd)
	}
	return false
}

// handleHostTitleGeometry is the focused title's move/grow/shrink: the
// same command vocabulary a window's focused title answers — plain
// arrows move (fine = a cell, coarse = the window system's 10-column /
// 4-row step), the size commands resize — applied to the OS window's
// pixel geometry, with the same minimum the pointer gestures enforce.
func (d *Desktop) handleHostTitleGeometry(cmd string) bool {
	// The kit's decode — the same vocabulary a window's focused title
	// answers, so the two cannot drift — then the standard steps.
	metrics := d.EffectiveCellMetrics()
	dir, resize, coarse, ok := window.DecodeTitleGeometry(cmd)
	if !ok {
		return false
	}
	dx, dy := window.TitleGeometryDelta(dir, coarse, metrics)
	d.mu.RLock()
	surf := d.surface
	d.mu.RUnlock()
	native, ok := surf.(platform.NativeSurface)
	if !ok {
		return true // focused title still owns the key
	}
	ppu := d.pxPerUnit()
	if ppu <= 0 {
		ppu = 1
	}
	pdx := int(math.Round(float64(dx) * ppu))
	pdy := int(math.Round(float64(dy) * ppu))
	if resize {
		w, h := native.ScreenSizePx()
		minW := int(math.Round(float64(metrics.CellWidth*12) * ppu))
		minH := int(math.Round(float64(metrics.CellHeight*4) * ppu))
		w += pdx
		h += pdy
		if w < minW {
			w = minW
		}
		if h < minH {
			h = minH
		}
		native.SetScreenSizePx(w, h)
	} else {
		x, y := native.ScreenPositionPx()
		native.SetScreenPositionPx(x+pdx, y+pdy)
	}
	return true
}

// paintHostTitleBar draws the themed title bar: the toolkit's window-title
// band across the top of the surface — active colors unless the OS window
// has blurred, like any window's — with the controls on the left and the
// title text centered. No-op unless the themed frame is active.
func (d *Desktop) paintHostTitleBar(p *core.Painter, bounds core.UnitRect) {
	th := d.TitleBarHeight()
	if th == 0 {
		return
	}
	d.mu.RLock()
	title := d.hostTitle
	focus := d.hostTitleFocus
	hover := d.hostTitleHover
	pressed := d.hostTitlePressed
	zoomed := d.hostZoom.zoomed
	d.mu.RUnlock()

	state := d.hostFrameState()
	scheme := d.GetScheme()
	tm := d.hostTitleMetrics()
	// Quasi-active uses the ACTIVE title colors, like a window's; only a
	// blurred OS window dims.
	lit := state != hostFrameInactive
	titleStyle := scheme.GetWindowTitle(lit)
	// The title row sits INSIDE the reserved frame border, like a
	// window's ("the titlebar sits inside the top border"); the border
	// band around it is painted by the frame stroke.
	b := d.hostFrameInset()
	barWidth := bounds.Width - 2*b
	tp := p.WithOffset(b, b).WithClip(core.UnitRect{Width: barWidth, Height: th})
	tp.FillRect(core.UnitRect{Width: barWidth, Height: th}, ' ', titleStyle)

	// The controls, on the left like every title bar: [x][.][^] — exit
	// desktop, minimize, zoom (a restore icon while zoomed) — each through
	// its own kit function (deliberately distinct per button), starting one
	// cell in from the (border-inset) edge exactly where a window's do, and
	// flush at the edge while zoomed like a maximized frame's.
	controlX := tm.CellW
	if d.hostZoomedNow() {
		controlX = 0
	}
	btnStyle := func(btn int) style.CellStyle {
		isPressed := pressed == btn && hover == btn
		isHovered := hover == btn && !isPressed && p.Graphical()
		return scheme.GetTitleBarButtonState(lit, focus == btn, isHovered, isPressed)
	}
	window.PaintCloseButton(tp, tm, controlX, btnStyle(hostTitleButtonClose))
	controlX += tm.ButtonW
	window.PaintMinimizeButton(tp, tm, controlX, btnStyle(hostTitleButtonMinimize))
	controlX += tm.ButtonW
	window.PaintZoomButton(tp, tm, controlX, zoomed, btnStyle(hostTitleButtonZoom))
	controlX += tm.ButtonW

	if title == "" {
		return
	}
	if focus == hostTitleFocusTitle {
		// The focused title wears the angle brackets — the REAL decoration
		// every window title uses (one shaped run over a highlight
		// foundation), not an approximation of it.
		window.PaintFocusedTitleDecoration(tp, tm, barWidth, title, scheme.GetTitleBarButton(lit, true, false))
		return
	}
	// Centered when it fits between the controls and the right edge;
	// otherwise pinned just past the controls and ellipsized against it,
	// the same layout every window title gets.
	window.PaintTitleBarText(tp, tm, title, titleStyle, controlX, barWidth, barWidth)
}

// paintHostFrame strokes the genuine window border around the themed
// desktop surface, themed by state exactly as a window's frame is: double
// in the active border color while the desktop's own chrome holds the
// keyboard, the quasi-active heavy treatment (the outer band vanishes
// into the window background with a thin active line just inside) while a
// child window does, and the inactive color when the OS window blurs.
// No-op unless the themed frame is active.
func (d *Desktop) paintHostFrame(p *core.Painter, bounds core.UnitRect) {
	// A zoomed window paints no frame at all — maximized style is flush
	// edges with only the title row (see Window.paintMaximizedFrame).
	if !d.themedFrameActive() || d.hostZoomedNow() {
		return
	}
	scheme := d.GetScheme()
	local := core.UnitRect{Width: bounds.Width, Height: bounds.Height}
	radius := window.FrameCornerRadius()
	switch d.hostFrameState() {
	case hostFrameActive:
		p.StrokeRoundedRect(local, radius, style.BorderDouble, scheme.GetWindowBorder(true))
	case hostFrameQuasi:
		bg := scheme.GetWindowBG(true)
		p.StrokeRoundedRect(local, radius, style.BorderHeavy, scheme.GetWindowBorder(true).WithFg(bg))
		d.paintHostFrameInner(p, local)
	default:
		p.StrokeRoundedRect(local, radius, style.BorderDouble, scheme.GetWindowBorder(false))
	}
}

// paintHostFrameInner is the thin inner line of the quasi-active frame,
// one tab-stroke weight just inside the (vanished) outer band — the same
// recipe as Window.paintSingleBorderInner.
func (d *Desktop) paintHostFrameInner(p *core.Painter, local core.UnitRect) {
	b := d.WindowFrameBorderUnits()
	inner := core.UnitRect{X: b, Y: b, Width: local.Width - 2*b, Height: local.Height - 2*b}
	radius := window.FrameCornerRadius() - b
	if radius < 0 {
		radius = 0
	}
	weight := p.UnitsToPx(1)
	if weight < 1 {
		weight = 1
	}
	p.StrokeRoundedRectWeight(inner, radius, weight, d.GetScheme().GetWindowBorder(true))
}
