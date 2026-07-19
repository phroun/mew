package window

import (
	"math"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/platform"
)

// TearOffHost runs one desktop window as the entire content of its
// own OS surface with the KittyTK chrome intact - the torn-off half of
// G4's granting. The surface is borderless: the window's own title
// bar stays the drag handle, but here a title drag moves the OS
// window itself (via the platform's global pointer), and the host's
// redock callback lets the desktop reclaim the window when the
// pointer crosses back over it mid-drag.
type TearOffHost struct {
	win    *Window
	surf   platform.Surface
	native platform.NativeSurface
	ppu    float64 // device pixels per unit (font_size-aware, may be fractional)
	global func() (int, int)

	// onRedock runs during a title drag with the pointer at the given
	// global pixel position and the grab point in window units.
	// Returning true means the desktop took the window back; the host
	// must go quiet (its surface is closed by the callback).
	onRedock func(globalX, globalY int, grabX, grabY core.Unit) bool

	// onFocus fires when the torn surface gains or loses OS focus, so the
	// desktop can point its menu bar at this window's app when it becomes
	// focused (the window still borrows the desktop's menu bar line).
	onFocus func(focused bool)

	// modalBlocked reports whether this torn window is suppressed by an
	// application- or window-level modal of its app (across surfaces).
	// onBlockedPress fires on a press while blocked so the desktop can
	// surface the blocking modal - OS-restoring it if it is minimized -
	// mirroring the in-surface "click a blocked window to reach the modal".
	modalBlocked   func() bool
	onBlockedPress func()

	savedFlags WindowFlags

	dragging bool
	grabX    core.Unit
	grabY    core.Unit
	// dragIsHandle marks a drag begun on the '#' tear handle: only such
	// a drag re-docks (over the desktop); a plain title drag just moves
	// the OS window. dragMoved distinguishes a handle CLICK (re-dock in
	// place) from a handle DRAG.
	dragIsHandle bool
	dragMoved    bool

	// Edge-resize drag: the OS window resizes with the pointer.
	resizing    bool
	resizeEdges int // resizeLeft | resizeRight | resizeBottom

	// resizeGrip is the edge thickness (units) that starts a resize.
	// Defaults to tearResizeGrip; the desktop overrides it to match its
	// own in-surface grip so torn edges are the same width as docked
	// ones (and don't overlap edge trinkets like scrollbars).
	resizeGrip core.Unit
	startGX    int // global pointer at resize start, px
	startGY    int
	startX     int // OS window rect at resize start, px
	startY     int
	startW     int
	startH     int

	// Zoom (the maximize button while torn): fill the display's work
	// area, second press restores the saved rect.
	zoomed    bool
	zoomSaved [4]int // x, y, w, h in px
	// dragRestored latches after a title drag un-zooms the window, so
	// the same drag can't snap-zoom right back until the pointer has
	// clearly left the top strip.
	dragRestored bool

	// Double-click tracking for the title bar (zoom toggle), matching
	// the in-surface manager's maximize double-click.
	lastClickAt time.Time
	lastClickX  core.Unit
	lastClickY  core.Unit

	// Popup overlays (combobox dropdowns, context menus) opened by
	// trinkets inside the torn window: they belong to THIS surface.
	popups []*PopupOverlay

	// Clipboard bridge for trinkets that have no desktop in their
	// ancestry while torn (the desktop wires the platform clipboard).
	clipGet func() string
	clipSet func(string)

	// onClosed runs when the hosted window closes itself (the [x]
	// button): the desktop disposes of the surface. Without it the
	// closed window would keep showing in its orphaned OS window.
	onClosed func()

	// Ghost mode: the desktop has re-adopted the window mid-drag, but
	// THIS window still owns the OS mouse session (the press happened
	// here). The surface goes invisible instead of being destroyed,
	// and the rest of the gesture relays to the desktop; the release
	// finishes it and the desktop then closes the surface. Destroying
	// the session's window mid-gesture loses the release and wedges
	// the platform's button state.
	ghost       bool
	onGhostMove func(gx, gy int)
	onGhostEnd  func()

	// setCursor applies a system mouse cursor (wired by the desktop from
	// the platform's CursorController). nil when the platform can't set
	// cursors. lastCursor avoids redundant applications.
	setCursor  func(core.CursorShape)
	lastCursor core.CursorShape
}

// Resize edge bits. The top edge is the title bar (drag handle), so
// left/right/bottom/top resize - matching the in-surface manager. Top
// is grabbable because a torn window is always on a pixel surface.
const (
	resizeLeft = 1 << iota
	resizeRight
	resizeBottom
	resizeTop
)

// tearResizeGrip is the edge thickness (units) that starts a resize.
const tearResizeGrip core.Unit = 6

// NewTearOffHost attaches the window to its own surface. Unlike
// SurfaceHost no chrome is suppressed; maximize/minimize make no
// sense without a managing desktop and are masked until re-dock.
// Call on the platform thread.
func NewTearOffHost(win *Window, surf platform.Surface, ppu float64,
	global func() (int, int),
	onRedock func(globalX, globalY int, grabX, grabY core.Unit) bool) *TearOffHost {
	h := &TearOffHost{win: win, surf: surf, ppu: ppu, global: global, onRedock: onRedock, resizeGrip: tearResizeGrip}
	h.native, _ = surf.(platform.NativeSurface)
	if h.ppu <= 0 {
		h.ppu = 1
	}

	// Popups from the torn window's trinkets open on this surface.
	win.SetPopupController(h)
	if content := win.Content(); content != nil {
		stampPopupController(content, h)
	}

	h.savedFlags = win.Flags()
	// All three title buttons keep meaning while torn: minimize
	// miniaturizes the OS window (the Dock, on macOS), maximize zooms
	// to the display's work area, resize maps onto the OS window. On
	// surfaces that aren't native OS windows, minimize is masked.
	if h.native == nil {
		win.SetFlags(h.savedFlags | WindowFlagNoMinimize)
	}
	win.SetOnMinimizeRequest(func() {
		if h.native != nil {
			h.native.Minimize()
		}
	})
	win.SetOnMaximizeRequest(h.ToggleZoom)
	win.SetOnBoundsRequest(h.applyKeyboardBounds)
	win.SetOnCloseComplete(func() {
		if h.onClosed != nil {
			h.onClosed()
		}
	})

	size := surf.Size()
	win.SetBounds(core.UnitRect{Width: size.Width, Height: size.Height})
	win.Layout()
	win.SetActive(true)

	surf.SetHandler(h)
	surf.Invalidate(core.UnitRect{})
	return h
}

// Window returns the hosted window.
func (h *TearOffHost) Window() *Window { return h.win }

// Surface returns the hosted surface.
func (h *TearOffHost) Surface() platform.Surface { return h.surf }

// Invalidate requests a repaint of the hosted window. The desktop's
// repaint tick calls it so animation (blinking carets, indeterminate
// progress) keeps running in torn-off windows.
func (h *TearOffHost) Invalidate() {
	h.surf.Invalidate(core.UnitRect{})
}

// SavedFlags returns the window's flags from before the tear-off,
// for the desktop to restore on re-dock.
func (h *TearOffHost) SavedFlags() WindowFlags { return h.savedFlags }

// BeginDrag arms the OS-window drag as if the user had pressed the
// title bar at the given window-unit grab point. The tear-off
// choreography uses it so the gesture that tore the window continues
// seamlessly in the new surface.
func (h *TearOffHost) BeginDrag(grabX, grabY core.Unit) {
	h.dragging = true
	h.dragIsHandle = false
	h.dragMoved = false
	h.grabX, h.grabY = grabX, grabY
}

// Dragging reports whether a title drag is moving the OS window.
func (h *TearOffHost) Dragging() bool { return h.dragging }

// SetOnClosed installs the desktop's disposal for a torn window that
// closes itself.
func (h *TearOffHost) SetOnClosed(fn func()) { h.onClosed = fn }

// SetOnFocus installs a callback fired when the torn surface gains or
// loses OS focus, letting the desktop point its menu bar at this
// window's app when it becomes focused.
func (h *TearOffHost) SetOnFocus(fn func(focused bool)) { h.onFocus = fn }

// SetModalChecker wires the app/window modal state: blocked reports whether
// this torn window is currently suppressed by a modal, and onBlockedPress
// runs on a press while blocked (so the desktop can OS-restore a minimized
// blocking modal). Either may be nil.
func (h *TearOffHost) SetModalChecker(blocked func() bool, onBlockedPress func()) {
	h.modalBlocked = blocked
	h.onBlockedPress = onBlockedPress
}

// isModalBlocked reports whether this torn window is currently modal-blocked.
func (h *TearOffHost) isModalBlocked() bool {
	return h.modalBlocked != nil && h.modalBlocked()
}

// IsModalBlocked reports whether this torn window is currently suppressed by a
// modal. Exported so hosts (and tests) can confirm a torn host consults the
// modal stack; nil checker (never wired) always reads false.
func (h *TearOffHost) IsModalBlocked() bool { return h.isModalBlocked() }

// blockedTitleDragStart reports whether a press at (x,y) on a modally-blocked
// torn window may begin a title-bar move: on the draggable title area, not on
// a titlebar button (or the tear handle), and not on a resize edge. This is
// the one interaction a blocked window still allows, so it can be moved aside.
func (h *TearOffHost) blockedTitleDragStart(x, y core.Unit) bool {
	if h.win.Flags()&(WindowFlagNoTitle|WindowFlagNoMove) != 0 {
		return false
	}
	if h.edgeAt(x, y) != 0 {
		return false
	}
	if h.win.buttonAtPosition(x, y) != TitleButtonNone {
		return false
	}
	return h.inTitleBar(x, y)
}

// SetCursorSetter wires the platform's system-cursor control so the torn
// surface can update the mouse cursor as the pointer moves over its edges
// and controls, matching the desktop.
func (h *TearOffHost) SetCursorSetter(fn func(core.CursorShape)) { h.setCursor = fn }

// SetResizeGrip overrides the resize-edge thickness (units) so a torn
// window's edges match the desktop's in-surface grip rather than the
// built-in default. Values <= 0 are ignored.
func (h *TearOffHost) SetResizeGrip(g core.Unit) {
	if g > 0 {
		h.resizeGrip = g
	}
}

// ResizeGrip returns the resize-edge thickness (units) in effect.
func (h *TearOffHost) ResizeGrip() core.Unit { return h.resizeGrip }

// applyCursor sets the system cursor, skipping redundant applications.
func (h *TearOffHost) applyCursor(shape core.CursorShape) {
	if h.setCursor == nil || shape == h.lastCursor {
		return
	}
	h.lastCursor = shape
	h.setCursor(shape)
}

// edgeAt returns the resize-edge bitmask for a window-local point, or 0
// when the point starts no resize - mirroring beginResize (no resize in
// the title row, on a non-resizable or zoomed window).
func (h *TearOffHost) edgeAt(x, y core.Unit) int {
	if h.win.Flags()&WindowFlagNoResize != 0 || h.zoomed {
		return 0
	}
	b := h.win.Bounds()
	edges := 0
	if x < h.resizeGrip {
		edges |= resizeLeft
	}
	if x >= b.Width-h.resizeGrip {
		edges |= resizeRight
	}
	if y < h.resizeGrip {
		edges |= resizeTop
	} else if y >= b.Height-h.resizeGrip {
		edges |= resizeBottom
	} else if y < core.DefaultCellMetrics().CellHeight {
		// Title row below the top grip: drag, not resize.
		return 0
	}
	return edges
}

// tornCursorForEdge maps a torn-window resize-edge bitmask to its
// directional cursor.
func tornCursorForEdge(edges int) core.CursorShape {
	left := edges&resizeLeft != 0
	right := edges&resizeRight != 0
	top := edges&resizeTop != 0
	bottom := edges&resizeBottom != 0
	switch {
	case (left && top) || (right && bottom):
		return core.CursorResizeNWSE // top-left / bottom-right diagonal
	case (right && top) || (left && bottom):
		return core.CursorResizeNESW // top-right / bottom-left diagonal
	case left || right:
		return core.CursorResizeH
	case top || bottom:
		return core.CursorResizeV
	default:
		return core.CursorDefault
	}
}

// tornEdgeRects returns the window-local highlight bands for the given
// resize edges (one per edge, two for a corner), each the width of the
// resize grip.
func tornEdgeRects(b core.UnitRect, edges int, grip core.Unit) []core.UnitRect {
	var rects []core.UnitRect
	if edges&resizeLeft != 0 {
		rects = append(rects, core.UnitRect{Width: grip, Height: b.Height})
	}
	if edges&resizeRight != 0 {
		rects = append(rects, core.UnitRect{X: b.Width - grip, Width: grip, Height: b.Height})
	}
	if edges&resizeBottom != 0 {
		rects = append(rects, core.UnitRect{Y: b.Height - grip, Width: b.Width, Height: grip})
	}
	if edges&resizeTop != 0 {
		rects = append(rects, core.UnitRect{Width: b.Width, Height: grip})
	}
	return rects
}

// updateHoverAndCursor refreshes the resize-edge highlight and the system
// cursor for a plain (non-drag, non-resize) hover over the torn window.
func (h *TearOffHost) updateHoverAndCursor(x, y core.Unit) {
	edges := h.edgeAt(x, y)
	if edges != 0 {
		h.win.SetResizeHoverRects(tornEdgeRects(h.win.Bounds(), edges, h.resizeGrip))
		h.applyCursor(tornCursorForEdge(edges))
		return
	}
	h.win.SetResizeHoverRects(nil)
	h.applyCursor(h.win.CursorShapeAt(x, y))
}

// SetClipboardAccess bridges the platform clipboard to trinkets in the
// torn window (their ancestry has no desktop to ask).
func (h *TearOffHost) SetClipboardAccess(get func() string, set func(string)) {
	h.clipGet = get
	h.clipSet = set
}

// Clipboard exposes the bridge (trinkets discover it through their
// popup controller).
func (h *TearOffHost) Clipboard() string {
	if h.clipGet == nil {
		return ""
	}
	return h.clipGet()
}

// SetClipboard exposes the bridge.
func (h *TearOffHost) SetClipboard(s string) {
	if h.clipSet != nil {
		h.clipSet(s)
	}
}

// --- core.PopupController: popups composite on the torn surface ---

// RegisterPopup implements core.PopupController.
func (h *TearOffHost) RegisterPopup(request *core.PopupRequest) {
	h.UnregisterPopup(request.ID)
	h.popups = append(h.popups, &PopupOverlay{
		ID:                 request.ID,
		Bounds:             request.Bounds,
		Paint:              request.Paint,
		HandleMousePress:   request.HandleMousePress,
		HandleMouseMove:    request.HandleMouseMove,
		HandleMouseRelease: request.HandleMouseRelease,
		HandleMouseWheel:   request.HandleMouseWheel,
		OnDismiss:          request.OnDismiss,
	})
	h.surf.Invalidate(core.UnitRect{})
}

// UnregisterPopup implements core.PopupController.
func (h *TearOffHost) UnregisterPopup(id string) {
	for i, p := range h.popups {
		if p.ID == id {
			h.popups = append(h.popups[:i], h.popups[i+1:]...)
			h.surf.Invalidate(core.UnitRect{})
			return
		}
	}
}

// MapToScreen implements core.PopupController: the torn window fills
// its surface at the origin, so ancestry coordinates ARE surface
// coordinates.
func (h *TearOffHost) MapToScreen(trinket core.Trinket, local core.UnitPoint) core.UnitPoint {
	return MapTrinketToScreen(trinket, local)
}

// ScreenBounds implements core.PopupController.
func (h *TearOffHost) ScreenBounds() core.UnitRect {
	size := h.surf.Size()
	return core.UnitRect{Width: size.Width, Height: size.Height}
}

// popupsHandleMouse offers a mouse event to the popups (topmost
// first), mirroring the WindowManager's routing: a press outside
// every popup closes them all and does NOT consume the event.
func (h *TearOffHost) popupsHandleMouse(ev core.Event) (handled bool) {
	if len(h.popups) == 0 {
		return false
	}
	switch e := ev.(type) {
	case core.MousePressEvent:
		for i := len(h.popups) - 1; i >= 0; i-- {
			popup := h.popups[i]
			if popup.Bounds.Contains(core.UnitPoint{X: e.X, Y: e.Y}) {
				if popup.HandleMousePress != nil {
					return popup.HandleMousePress(e)
				}
				return true
			}
		}
		cleared := h.popups
		h.popups = nil
		// Same contract as the WindowManager: the owner must learn its
		// popup is gone or it keeps swallowing keys for a dead overlay.
		for _, p := range cleared {
			if p.OnDismiss != nil {
				p.OnDismiss()
			}
		}
		h.surf.Invalidate(core.UnitRect{})
		return false
	case core.MouseMoveEvent:
		for i := len(h.popups) - 1; i >= 0; i-- {
			if fn := h.popups[i].HandleMouseMove; fn != nil && fn(e) {
				return true
			}
		}
	case core.MouseReleaseEvent:
		for i := len(h.popups) - 1; i >= 0; i-- {
			if fn := h.popups[i].HandleMouseRelease; fn != nil && fn(e) {
				return true
			}
		}
	case core.MouseWheelEvent:
		for i := len(h.popups) - 1; i >= 0; i-- {
			popup := h.popups[i]
			if popup.Bounds.Contains(core.UnitPoint{X: e.X, Y: e.Y}) && popup.HandleMouseWheel != nil {
				return popup.HandleMouseWheel(e)
			}
		}
	}
	return false
}

// SetGhostRelay installs the desktop's continuation for a gesture
// that outlives its window: move relays motion (global px), end
// finishes the drag and disposes of the ghost surface.
func (h *TearOffHost) SetGhostRelay(move func(gx, gy int), end func()) {
	h.onGhostMove = move
	h.onGhostEnd = end
}

// finishGhost ends the relayed gesture.
func (h *TearOffHost) finishGhost() {
	h.ghost = false
	h.dragging = false
	if h.onGhostEnd != nil {
		h.onGhostEnd()
	}
}

// EndDrag disarms the drag and its restore latch. The desktop calls it when the gesture's
// end shows up on its side of the split event stream (release, or a
// move with the button no longer held) - without it a later drag
// inside the torn window's content would move the OS window.
func (h *TearOffHost) EndDrag() {
	h.dragging = false
	h.dragRestored = false
}

// Frame implements platform.SurfaceHandler.
func (h *TearOffHost) Frame(p *core.Painter) {
	if h.ghost {
		// The window lives on the desktop again; this surface only
		// survives (invisibly) to finish its mouse session.
		return
	}
	h.win.Paint(p)
	// A modally-blocked torn window is darkened, mirroring an in-surface
	// window suppressed by a modal.
	if h.isModalBlocked() {
		b := h.win.Bounds()
		h.win.PaintModalDim(p, core.UnitRect{Width: b.Width, Height: b.Height})
	}
	for _, popup := range h.popups {
		if popup.Paint != nil {
			popup.Paint(p)
		}
	}
}

// Event implements platform.SurfaceHandler: surface coordinates ARE
// window coordinates. A title-bar press the window doesn't consume
// starts an OS-window drag, mirroring the WindowManager's in-surface
// title drag.
func (h *TearOffHost) Event(ev core.Event) bool {
	// A modally-blocked torn window ignores input, with one exception: it may
	// be dragged by its title bar to move it out of the way (mirroring the
	// in-surface rule). Any press also surfaces the blocking modal - raising
	// it back on top (and OS-restoring it if minimized). Focus/leave events
	// still pass so chrome and hover stay sane.
	if !h.ghost && h.isModalBlocked() {
		switch e := ev.(type) {
		case core.MousePressEvent:
			if h.onBlockedPress != nil {
				h.onBlockedPress()
			}
			if e.Button == core.LeftButton && h.blockedTitleDragStart(e.X, e.Y) {
				h.BeginDrag(e.X, e.Y)
			}
			return true
		case core.MouseMoveEvent:
			if h.dragging {
				break // let the title-bar move continue
			}
			return true
		case core.MouseReleaseEvent:
			if h.dragging {
				break // let the title-bar move finish
			}
			return true
		case core.MouseWheelEvent, core.KeyPressEvent, core.KeyReleaseEvent:
			return true
		}
	}

	var handled bool
	switch e := ev.(type) {
	case core.FocusEvent:
		// The torn window's chrome follows its OS window's focus,
		// exactly as it would follow activation in the desktop.
		h.win.SetActive(e.Focused)
		if h.onFocus != nil {
			h.onFocus(e.Focused)
		}
		handled = true
	case core.KeyPressEvent:
		if h.native != nil && (e.Key == "s-m" ||
			(e.Modifiers&core.MetaModifier != 0 && e.Key == "m")) {
			// Cmd+M miniaturizes, like any macOS document window.
			h.native.Minimize()
			handled = true
			break
		}
		handled = h.win.HandleKeyPress(e)
	case core.KeyReleaseEvent:
		handled = h.win.HandleKeyRelease(e)
	case core.MousePressEvent:
		if !h.ghost && h.popupsHandleMouse(e) {
			handled = true
			break
		}
		if h.ghost {
			// A press reaching a ghost means its release was lost:
			// finish the relay and swallow the stray press.
			h.finishGhost()
			handled = true
			break
		}
		// A press while a drag/resize is still armed means the
		// gesture's release was lost in the split event stream:
		// disarm and process the press normally.
		h.dragging = false
		h.resizing = false
		if e.Button == core.LeftButton && h.beginResize(e.X, e.Y) {
			handled = true
			break
		}
		// The '#' handle is host-managed: a drag re-docks over the
		// desktop, a click re-docks in place. Grab it before the window
		// tracks it as a button.
		if e.Button == core.LeftButton && h.win.buttonAtPosition(e.X, e.Y) == TitleButtonTear {
			h.BeginDrag(e.X, e.Y)
			h.dragIsHandle = true
			handled = true
			break
		}
		handled = h.win.HandleMousePress(e)
		if !handled && e.Button == core.LeftButton && h.inTitleBar(e.X, e.Y) {
			// Double-click on the title bar toggles the zoom, exactly
			// as it toggles maximize in-surface.
			metrics := core.DefaultCellMetrics()
			now := time.Now()
			if now.Sub(h.lastClickAt) < 400*time.Millisecond &&
				e.X-h.lastClickX < metrics.CellWidth && h.lastClickX-e.X < metrics.CellWidth &&
				e.Y-h.lastClickY < metrics.CellHeight && h.lastClickY-e.Y < metrics.CellHeight {
				h.lastClickAt = time.Time{}
				h.ToggleZoom()
			} else {
				h.lastClickAt = now
				h.lastClickX, h.lastClickY = e.X, e.Y
				h.BeginDrag(e.X, e.Y)
			}
			handled = true
		}
	case core.MouseMoveEvent:
		if !h.ghost && !h.resizing && !h.dragging && h.popupsHandleMouse(e) {
			handled = true
			break
		}
		if h.ghost {
			if e.Buttons&core.LeftButton == 0 {
				h.finishGhost()
			} else if h.global != nil && h.onGhostMove != nil {
				gx, gy := h.global()
				h.onGhostMove(gx, gy)
			}
			handled = true
		} else if (h.resizing || h.dragging) && e.Buttons&core.LeftButton == 0 {
			// Button no longer held: the release happened where we
			// couldn't see it. The gesture is over - do not move the
			// window on a mere hover.
			h.resizing = false
			h.dragging = false
			handled = h.win.HandleMouseMove(e)
		} else if h.resizing {
			handled = h.resizeMove()
		} else if h.dragging {
			handled = h.dragMove()
		} else if e.Buttons == 0 {
			// Plain hover. Over a resize edge a press would resize, not click
			// a control under the pointer, so clear all control hover (titlebar
			// buttons and edge-adjacent content) and show only the edge
			// highlight + resize cursor - matching the in-surface desktop.
			if h.edgeAt(e.X, e.Y) != 0 {
				h.win.HandleMouseMove(core.MouseMoveEvent{X: -1, Y: -1})
				handled = true
			} else {
				handled = h.win.HandleMouseMove(e)
			}
			h.updateHoverAndCursor(e.X, e.Y)
		} else {
			// A button is held (a drag begun elsewhere passing over the frame):
			// forward it and drop any lingering edge band.
			handled = h.win.HandleMouseMove(e)
			h.win.SetResizeHoverRects(nil)
		}
	case core.MouseReleaseEvent:
		if !h.ghost && !h.resizing && !h.dragging && h.popupsHandleMouse(e) {
			handled = true
			break
		}
		if h.ghost {
			h.finishGhost()
			handled = true
		} else if h.resizing || h.dragging {
			handleClick := h.dragging && h.dragIsHandle && !h.dragMoved
			h.resizing = false
			h.dragging = false
			h.dragRestored = false
			if handleClick {
				// Click on the '#' handle: re-dock in place.
				h.win.requestTear()
			}
			handled = true
		} else {
			handled = h.win.HandleMouseRelease(e)
		}
	case core.MouseWheelEvent:
		if h.popupsHandleMouse(e) {
			handled = true
			break
		}
		handled = h.win.HandleMouseWheel(e)
	case core.MouseLeaveEvent:
		// Pointer left the torn surface: drop the resize-edge highlight and
		// reset the cursor. A live resize/drag keeps driving from the global
		// pointer, so leave its highlight alone.
		if !h.resizing && !h.dragging {
			h.win.SetResizeHoverRects(nil)
			h.win.HandleMouseMove(core.MouseMoveEvent{X: -1, Y: -1})
			h.applyCursor(core.CursorDefault)
		}
		handled = true
	}
	// Parity contract: repaint after input until trinkets migrate to
	// precise invalidation.
	h.surf.Invalidate(core.UnitRect{})
	return handled
}

// dragMove follows the global pointer: first the desktop gets a
// chance to reclaim the window (pointer back over the desktop
// surface), otherwise the OS window moves to keep the grab point
// under the pointer. In-surface parity for the zoom state: dragging
// a zoomed window down restores it (grab kept proportional), and
// dragging the pointer above the work area's top snap-zooms.
func (h *TearOffHost) dragMove() bool {
	if h.global == nil || h.native == nil {
		return true
	}
	gx, gy := h.global()
	h.dragMoved = true
	if h.dragIsHandle && h.onRedock != nil && h.onRedock(gx, gy, h.grabX, h.grabY) {
		// Handle drag over the desktop: the desktop took the window;
		// this surface stays (invisible) to relay the rest of its live
		// mouse session.
		h.ghost = true
		return true
	}
	_, way, ww, wh := h.native.WorkAreaPx()
	if h.zoomed {
		// A zoomed window doesn't slide; dragging its title below the
		// work area's top restores it, with the grab point staying
		// proportionally placed on the narrower title bar.
		if gy-h.px(h.grabY) >= way {
			if ww > 0 {
				h.grabX = core.Unit(float64(h.grabX) * float64(h.zoomSaved[2]) / float64(ww))
			}
			h.zoomed = false
			h.dragRestored = true
			h.win.Restore()
			h.native.SetScreenSizePx(h.zoomSaved[2], h.zoomSaved[3])
			h.native.SetScreenPositionPx(gx-h.px(h.grabX), gy-h.px(h.grabY))
		}
		return true
	}
	if h.dragRestored && gy >= way+h.px(core.DefaultCellMetrics().CellHeight) {
		// Pointer clearly below the top strip: re-arm the snap.
		h.dragRestored = false
	}
	if ww > 0 && wh > 0 && !h.dragRestored &&
		(gy < way || (way <= 0 && gy <= 0)) {
		// Into the strip above the work area (the macOS menu bar):
		// snap-zoom, exactly like dragging into the desktop's menu
		// bar maximizes in-surface. Keep dragging so the user can
		// pull back down to restore.
		h.zoomToWorkArea()
		return true
	}
	h.native.SetScreenPositionPx(gx-h.px(h.grabX), gy-h.px(h.grabY))
	return true
}

// beginResize arms an edge resize when the press lands within the
// grip distance of the left, right, or bottom edge (the top edge is
// the title bar). Returns false when the window is not resizable or
// the press is interior.
func (h *TearOffHost) beginResize(x, y core.Unit) bool {
	if h.native == nil || h.global == nil || h.zoomed ||
		h.win.Flags()&WindowFlagNoResize != 0 {
		return false
	}
	edges := h.edgeAt(x, y)
	if edges == 0 {
		// Interior press, or within the title row (drag, not resize).
		return false
	}
	h.resizing = true
	h.resizeEdges = edges
	h.startGX, h.startGY = h.global()
	h.startX, h.startY = h.native.ScreenPositionPx()
	// Anchor to the OS window's true pixel size. Deriving it from the
	// surface's unit size and back through px() would undershoot at a
	// fractional pixels-per-unit (the unit size snaps to whole cells at a
	// rate slightly above ppu), so the first resizeMove would jump the
	// window smaller by roughly the frame width.
	h.startW, h.startH = h.native.ScreenSizePx()
	return true
}

// resizeMove applies the pointer delta to the armed edges, moving and
// resizing the OS window; the size change reports back through
// Resized and the window re-lays out to the surface.
// px converts a unit length to device pixels for this surface, tracking
// font_size (the surface backend renders at the same pixels-per-unit).
func (h *TearOffHost) px(u core.Unit) int {
	return int(math.Round(float64(u) * h.ppu))
}

func (h *TearOffHost) resizeMove() bool {
	gx, gy := h.global()
	dx, dy := gx-h.startGX, gy-h.startGY
	metrics := core.DefaultCellMetrics()
	minW := h.px(metrics.CellWidth * 12)
	minH := h.px(metrics.CellHeight * 4)

	x, y, w, ht := h.startX, h.startY, h.startW, h.startH
	if h.resizeEdges&resizeLeft != 0 {
		w -= dx
		if w < minW {
			dx -= minW - w
			w = minW
		}
		x += dx
	}
	if h.resizeEdges&resizeRight != 0 {
		w += dx
		if w < minW {
			w = minW
		}
	}
	if h.resizeEdges&resizeBottom != 0 {
		ht += dy
		if ht < minH {
			ht = minH
		}
	}
	if h.resizeEdges&resizeTop != 0 {
		ht -= dy
		if ht < minH {
			dy -= minH - ht
			ht = minH
		}
		y += dy
	}
	if h.resizeEdges&(resizeLeft|resizeTop) != 0 {
		h.native.SetScreenPositionPx(x, y)
	}
	h.native.SetScreenSizePx(w, ht)
	// Track the highlight on the edge being dragged (the OS resize reports
	// back through Resized, updating the window bounds) and keep the resize
	// cursor for the gesture.
	h.win.SetResizeHoverRects(tornEdgeRects(h.win.Bounds(), h.resizeEdges, h.resizeGrip))
	h.applyCursor(tornCursorForEdge(h.resizeEdges))
	return true
}

// ToggleZoom fills the display's work area (the maximize button's
// meaning while torn - macOS option-zoom, not a fullscreen space);
// a second toggle restores the saved rect.
// ZoomToFill fills the display work area (idempotent, unlike ToggleZoom).
// Used by solo mode to make the torn window the whole display.
func (h *TearOffHost) ZoomToFill() {
	if h.native == nil || h.zoomed {
		return
	}
	h.zoomToWorkArea()
}

func (h *TearOffHost) ToggleZoom() {
	if h.native == nil {
		return
	}
	if h.zoomed {
		h.zoomed = false
		h.win.Restore()
		h.native.SetScreenPositionPx(h.zoomSaved[0], h.zoomSaved[1])
		h.native.SetScreenSizePx(h.zoomSaved[2], h.zoomSaved[3])
		return
	}
	h.zoomToWorkArea()
}

// zoomToWorkArea saves the current rect and fills the display's work
// area.
func (h *TearOffHost) zoomToWorkArea() {
	wx, wy, ww, wh := h.native.WorkAreaPx()
	if ww <= 0 || wh <= 0 {
		return
	}
	x, y := h.native.ScreenPositionPx()
	size := h.surf.Size()
	h.zoomSaved = [4]int{x, y, h.px(size.Width), h.px(size.Height)}
	h.zoomed = true
	h.win.Maximize()
	h.native.SetScreenPositionPx(wx, wy)
	h.native.SetScreenSizePx(ww, wh)
}

// applyKeyboardBounds maps a title-focus keyboard geometry change
// (arrow move, Shift-arrow resize, Escape revert) onto the OS
// window: position deltas move it across the real desktop, size
// deltas resize it, exactly as the same keys move an in-surface
// window around the KittyTK desktop.
func (h *TearOffHost) applyKeyboardBounds(b core.UnitRect) bool {
	if h.native == nil || h.zoomed {
		return h.zoomed // zoomed: swallow, geometry is the work area's
	}
	cur := h.win.Bounds()
	dx := h.px(b.X - cur.X)
	dy := h.px(b.Y - cur.Y)
	dw := h.px(b.Width - cur.Width)
	dh := h.px(b.Height - cur.Height)
	if dx != 0 || dy != 0 {
		x, y := h.native.ScreenPositionPx()
		h.native.SetScreenPositionPx(x+dx, y+dy)
	}
	if (dw != 0 || dh != 0) && h.win.Flags()&WindowFlagNoResize == 0 {
		size := h.surf.Size()
		h.native.SetScreenSizePx(h.px(size.Width)+dw, h.px(size.Height)+dh)
	}
	return true
}

// inTitleBar reports whether the point sits in the window's title
// row (the drag handle), matching the WindowManager's notion: the
// top cell row, excluding nothing else - button clicks were already
// offered to the window and declined.
func (h *TearOffHost) inTitleBar(x, y core.Unit) bool {
	b := h.win.Bounds()
	th := core.DefaultCellMetrics().CellHeight
	return x >= 0 && x < b.Width && y >= 0 && y < th
}

// Resized implements platform.SurfaceHandler: the window tracks the
// surface.
func (h *TearOffHost) Resized(size core.UnitSize) {
	h.win.SetBounds(core.UnitRect{Width: size.Width, Height: size.Height})
	h.win.Layout()
	h.surf.Invalidate(core.UnitRect{})
}
