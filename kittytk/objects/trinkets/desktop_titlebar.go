package trinkets

import (
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

// hostMoveState is the themed title bar's drag-to-move gesture. Guarded by
// Desktop.mu, like hostEdgeState.
type hostMoveState struct {
	active           bool
	startGX, startGY int // global pointer at press, device px
	startX, startY   int // OS window origin at press, device px
}

// The title bar's controls, on the left like every title bar in the
// system: [x][.][^]. The same constants serve as the bar's keyboard focus
// states (none / one of the buttons).
const (
	hostTitleButtonNone = iota
	hostTitleButtonClose
	hostTitleButtonMinimize
	hostTitleButtonZoom
)

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

// TitleBarHeight is the height of the desktop's own themed title bar row:
// one cell when the themed frame is active, 0 in every other mode (the
// companion to MenuBarHeight, one row further out).
func (d *Desktop) TitleBarHeight() core.Unit {
	if !d.themedFrameActive() {
		return 0
	}
	return d.EffectiveCellMetrics().CellHeight
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
// of three cells, like every title bar's controls.
func (d *Desktop) hostTitleButtonAt(x, y core.Unit) int {
	th := d.TitleBarHeight()
	if th == 0 || y < 0 || y >= th || x < 0 {
		return hostTitleButtonNone
	}
	bw := d.EffectiveCellMetrics().CellWidth * 3
	switch {
	case x < bw:
		return hostTitleButtonClose
	case x < bw*2:
		return hostTitleButtonMinimize
	case x < bw*3:
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
// hover highlight.
func (d *Desktop) hostTitleHoverUpdate(x, y core.Unit) {
	btn := d.hostTitleButtonAt(x, y)
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
	if th == 0 || e.Y < 0 || e.Y >= th {
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
	gx, gy := gp.GlobalPointerPx()
	x, y := native.ScreenPositionPx()
	d.mu.Lock()
	st := &d.hostMove
	st.active = true
	st.startGX, st.startGY = gx, gy
	st.startX, st.startY = x, y
	d.mu.Unlock()
	return true
}

// hostMoveMove applies the pointer delta to the OS window's origin while
// the drag is armed. Reports false when no gesture is in progress.
func (d *Desktop) hostMoveMove(e core.MouseMoveEvent) bool {
	d.mu.RLock()
	st := d.hostMove
	d.mu.RUnlock()
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
		native.SetScreenPositionPx(st.prevX, st.prevY)
		native.SetScreenSizePx(st.prevW, st.prevH)
		d.mu.Lock()
		d.hostZoom.zoomed = false
		d.mu.Unlock()
		d.RequestUpdate() // the zoom button's icon flips back
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
	native.SetScreenPositionPx(ax, ay)
	native.SetScreenSizePx(aw, ah)
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
			}
			if name != "" {
				am.AnnouncePolite(name)
			}
		}
	}
	d.RequestUpdate()
}

// enterHostTitleFocus lands the keyboard on the title bar from the chrome
// ring: on the first control coming forward (Tab off the dock's end), the
// last coming backward (Shift+Tab off the menu bar). Declines — reporting
// false so the ring can fall through to its next stop — when there is no
// themed title bar to land on.
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
		d.setHostTitleFocus(hostTitleButtonZoom)
	}
	return true
}

// handleHostTitleKey is the title bar's keyboard when it holds focus:
// Tab/Shift+Tab walk the controls and step off the ends of the chrome
// ring (forward to the menu bar, backward to the dock), activate runs the
// focused control, cancel drops the focus.
func (d *Desktop) handleHostTitleKey(event core.KeyPressEvent) bool {
	d.mu.RLock()
	focus := d.hostTitleFocus
	d.mu.RUnlock()
	if focus == hostTitleButtonNone {
		return false
	}
	switch d.KeyCommand(event.Key) {
	case core.CmdFocusNext:
		switch focus {
		case hostTitleButtonClose:
			d.setHostTitleFocus(hostTitleButtonMinimize)
		case hostTitleButtonMinimize:
			d.setHostTitleFocus(hostTitleButtonZoom)
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
	return false
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
	metrics := d.EffectiveCellMetrics()
	// Quasi-active uses the ACTIVE title colors, like a window's; only a
	// blurred OS window dims.
	lit := state != hostFrameInactive
	titleStyle := scheme.GetWindowTitle(lit)
	bar := core.UnitRect{Width: bounds.Width, Height: th}
	p.FillRect(bar, ' ', titleStyle)

	// The controls, on the left like every title bar: [x][.][^] — exit
	// desktop, minimize, zoom (a restore icon while zoomed).
	controlX := core.Unit(0)
	for _, c := range []struct {
		btn  int
		icon rune
	}{
		{hostTitleButtonClose, 'x'},
		{hostTitleButtonMinimize, '.'},
		{hostTitleButtonZoom, '^'},
	} {
		icon := c.icon
		if c.btn == hostTitleButtonZoom && zoomed {
			icon = 'o'
		}
		isPressed := pressed == c.btn && hover == c.btn
		isHovered := hover == c.btn && !isPressed && p.Graphical()
		st := scheme.GetTitleBarButtonState(lit, focus == c.btn, isHovered, isPressed)
		p.DrawCell(controlX, 0, '[', st)
		p.DrawCell(controlX+metrics.CellWidth, 0, icon, st)
		p.DrawCell(controlX+metrics.CellWidth*2, 0, ']', st)
		controlX += metrics.CellWidth * 3
	}

	if title == "" {
		return
	}
	font := d.EffectiveFont()
	x := (bounds.Width - font.MeasureText(title)) / 2
	if x < controlX+metrics.CellWidth {
		// Squeezed by the controls: pinned just past them, clipped at the bar.
		x = controlX + metrics.CellWidth
	}
	p.WithClip(bar).DrawText(x, 0, title, titleStyle, font)
}

// paintHostFrame strokes the genuine window border around the themed
// desktop surface, themed by state exactly as a window's frame is: double
// in the active border color while the desktop's own chrome holds the
// keyboard, the quasi-active heavy treatment (the outer band vanishes
// into the window background with a thin active line just inside) while a
// child window does, and the inactive color when the OS window blurs.
// No-op unless the themed frame is active.
func (d *Desktop) paintHostFrame(p *core.Painter, bounds core.UnitRect) {
	if !d.themedFrameActive() {
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
