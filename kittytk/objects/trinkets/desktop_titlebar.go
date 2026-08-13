package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/platform"
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

// hostMoveBegin arms the drag when a press lands on the themed title bar.
// Consulted after hostResizeBegin (the resize sliver rides over the title
// row's outer edge, like a torn window's) and before the window manager.
func (d *Desktop) hostMoveBegin(e core.MousePressEvent) bool {
	if e.Button != core.LeftButton {
		return false
	}
	th := d.TitleBarHeight()
	if th == 0 || e.Y < 0 || e.Y >= th {
		return false
	}
	// An open dropdown owns the next press anywhere on the surface (it is
	// how the menu closes); the drag must not swallow it.
	if d.menuBar != nil && d.menuBar.ActiveMenu() != nil {
		return false
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

// hostMoveEnd completes the drag on release. Reports false when none was
// in progress.
func (d *Desktop) hostMoveEnd(core.MouseReleaseEvent) bool {
	d.mu.RLock()
	active := d.hostMove.active
	d.mu.RUnlock()
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

// hostZoomToggle is the Ψ menu's Zoom: fill the display's work area, or
// put the window back where it was before the last zoom. It is the themed
// frame's stand-in for OS maximize — a plain move+resize, so the window
// stays freely resizable and the desktop's own edge zones stay live (the
// zones stand down only for a true OS maximize, which the OS owns).
func (d *Desktop) hostZoomToggle() {
	native, _, ok := d.hostResizeParts()
	if !ok {
		return
	}
	d.mu.Lock()
	st := d.hostZoom
	d.mu.Unlock()
	if st.zoomed {
		native.SetScreenPositionPx(st.prevX, st.prevY)
		native.SetScreenSizePx(st.prevW, st.prevH)
		d.mu.Lock()
		d.hostZoom.zoomed = false
		d.mu.Unlock()
		return
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
}

// paintHostTitleBar draws the themed title bar: the toolkit's window-title
// band across the top of the surface, dimmed when the OS window is not
// focused, with the title text centered. No-op unless the themed frame is
// active.
func (d *Desktop) paintHostTitleBar(p *core.Painter, bounds core.UnitRect) {
	th := d.TitleBarHeight()
	if th == 0 {
		return
	}
	d.mu.RLock()
	title := d.hostTitle
	focused := !d.hostUnfocused
	d.mu.RUnlock()

	titleStyle := d.GetScheme().GetWindowTitle(focused)
	bar := core.UnitRect{Width: bounds.Width, Height: th}
	p.FillRect(bar, ' ', titleStyle)
	if title == "" {
		return
	}
	font := d.EffectiveFont()
	margin := d.EffectiveCellMetrics().CellWidth
	x := (bounds.Width - font.MeasureText(title)) / 2
	if x < margin {
		// Wider than the bar: pinned to the left margin, clipped at the bar.
		x = margin
	}
	p.WithClip(bar).DrawText(x, 0, title, titleStyle, font)
}
