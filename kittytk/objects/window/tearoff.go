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
	// minimizeKeys resolves the one key the HOST takes before the window
	// sees it: miniaturizing is the OS window's business, not the toolkit
	// window's, so it cannot go through win's own context.
	minimizeKeys core.TrinketKeys
	// ppu reports the LIVE device pixels-per-unit (font_size-aware, may be
	// fractional). A getter, not a captured value: the host font zoom can
	// change the ratio at any time, and a snapshot from tear-off time made
	// every later px<->unit conversion — the title-drag grab anchor first
	// among them — land on the wrong pixel.
	ppu    func() float64
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
	// grabPxX/Y is the SAME grab point in device pixels, captured at press
	// time as (global pointer - window origin). The unit pair above is what
	// the desktop needs to re-dock (it thinks in window units), but driving
	// the OS window from it makes the window jolt: the pointer arrives in
	// pixels, is divided into units by the surface, and px() multiplies it
	// back, so the sub-unit part of the grab is lost and reappears as an
	// offset of up to one pixels-per-unit. At the default zoom ppu is 2 and
	// it hides; zoomed in it is 3 or more and the window visibly jumps on
	// the first motion. beginResize already anchors in true pixels for the
	// same reason — this is the drag half of that.
	grabPxX     int
	grabPxY     int
	grabPxValid bool
	// dragIsHandle marks a drag begun on the '#' tear handle: only such
	// a drag re-docks (over the desktop); a plain title drag just moves
	// the OS window. dragMoved distinguishes a handle CLICK (re-dock in
	// place) from a handle DRAG.
	dragIsHandle bool
	dragMoved    bool

	// Edge-resize drag: the OS window resizes with the pointer.
	resizing    bool
	resizeEdges int // resizeLeft | resizeRight | resizeBottom

	// graphicalFrames says the surface paints rounded window frames, which
	// is what decides between the graphical grab rule and the classic
	// cell-wide zones. A detached window's parent chain does not reach the
	// desktop, so core.FindGraphicalFrames cannot answer here and the desktop
	// pushes it in instead.
	graphicalFrames bool
	startGX         int // global pointer at resize start, px
	startGY         int
	startX          int // OS window rect at resize start, px
	startY          int
	startW          int
	startH          int

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

	// popupEpoch moves whenever a popup is registered or unregistered.
	// Popups are not in the window's trinket subtree, so nothing else
	// would tell a host caching this surface's pixels that one appeared.
	popupEpoch uint64

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
	// cursors.
	setCursor func(core.CursorShape)

	// geom converts unit sizes to device pixels on the HARDENED cell pitch —
	// the same grid the window frame paints on — so the OS surface is sized
	// and read back exactly as wide/tall as the frame draws
	// (geometry-cells-units-pixels.md). nil falls back to the raw px()/ppu
	// ratio, which coincides at the base zoom; the desktop wires the real one
	// via SetTornGeometry.
	geom TornGeometry
}

// TornGeometry is the hardened cell-pitch unit<->pixel conversion a torn
// host uses to size and place its OS surface so it lines up with the frame
// paint. The desktop supplies it (its backend's cell-snapped mapping); a
// host without one falls back to the raw pixels-per-unit ratio, which
// coincides at the base zoom. See geometry-cells-units-pixels.md.
type TornGeometry interface {
	HardUnitToPxX(core.Unit) int
	HardUnitToPxY(core.Unit) int
	HardPxToUnitX(int) core.Unit
	HardPxToUnitY(int) core.Unit
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

// NewTearOffHost attaches the window to its own surface. Unlike
// SurfaceHost no chrome is suppressed; maximize/minimize make no
// sense without a managing desktop and are masked until re-dock.
// Call on the platform thread. ppu reports the live pixels-per-unit
// (the desktop's pxPerUnit); nil means an unscaled 1:1 surface.
func NewTearOffHost(win *Window, surf platform.Surface, ppu func() float64,
	global func() (int, int),
	onRedock func(globalX, globalY int, grabX, grabY core.Unit) bool) *TearOffHost {
	h := &TearOffHost{win: win, surf: surf, ppu: ppu, global: global, onRedock: onRedock, graphicalFrames: true}
	h.native, _ = surf.(platform.NativeSurface)
	// The OS-side floor, matching what resizeMove clamps to: a resize we
	// do not drive (the window manager's own keyboard resize or tiling)
	// answers only to the OS.
	if ms, ok := surf.(platform.NativeMinimumSizer); ok {
		metrics := core.DefaultCellMetrics()
		ms.SetMinimumSizePx(h.pxHardX(metrics.UnitsPerCellWidth*MinHostCols),
			h.pxHardY(metrics.UnitsPerCellHeight*MinHostRows))
	}
	h.minimizeKeys.SetCommands(core.CmdAppMinimize)
	h.minimizeKeys.SetKeyOwner(win) // the torn window's own keymap, if it has one

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

// FocusedTextSink implements platform.TextSinkReporter: whether the trinket
// holding focus in the torn-off window types. See platform.TextInputFrame.
func (h *TearOffHost) FocusedTextSink() core.TextSinkState {
	if h.win == nil {
		return core.TextSinkUnknown
	}
	return core.FocusedTextSink(h.win.FocusManager())
}

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
// BeginDrag is PRESCRIPTIVE: the window will be placed so the pointer sits
// at this unit offset, whatever the geometry is now — the tear-off
// choreography re-anchors the just-torn window under the cursor with it, and
// the offset re-reads the live pixels-per-unit so a mid-drag font zoom keeps
// the grab proportionally placed. For a press that lands on THIS window's own
// title bar use beginDragAt, which pins the exact pixels under the pointer.
func (h *TearOffHost) BeginDrag(grabX, grabY core.Unit) {
	h.dragging = true
	h.dragIsHandle = false
	h.dragMoved = false
	h.grabX, h.grabY = grabX, grabY
	h.grabPxValid = false
}

// beginDragAt is DESCRIPTIVE: the pointer is already at (x, y) — a real
// press on this window's title bar — so the anchor is captured in device
// pixels from the actual pointer and window positions, exactly. The unit
// pair still records the grab for re-docking; it is just not what drives
// the OS window, because the platform's px->unit division already dropped
// the sub-unit part and multiplying it back jolts the window by up to one
// pixels-per-unit on the first motion.
func (h *TearOffHost) beginDragAt(x, y core.Unit) {
	h.BeginDrag(x, y)
	h.captureGrabPx()
}

// captureGrabPx records the grab point in device pixels: where the pointer is
// now, less where the window is now. Exact by construction — no unit round
// trip — so the point under the pointer stays under it. Silently leaves the
// anchor invalid when the OS window is not placed yet (the tear-off
// choreography arms a drag mid-flight); grabPx then falls back to the unit
// conversion, which is what this code did before.
func (h *TearOffHost) captureGrabPx() {
	h.grabPxValid = false
	if h.global == nil || h.native == nil {
		return
	}
	gx, gy := h.global()
	wx, wy := h.native.ScreenPositionPx()
	h.grabPxX, h.grabPxY = gx-wx, gy-wy
	h.grabPxValid = true
}

// grabPx is the grab offset to position the OS window by.
func (h *TearOffHost) grabPx() (int, int) {
	if h.grabPxValid {
		return h.grabPxX, h.grabPxY
	}
	return h.px(h.grabX), h.px(h.grabY)
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

// SetGraphicalFrames tells a torn host whether its surface paints graphical
// window frames, which decides its resize-edge geometry. The desktop pushes
// its own answer in, because a detached window has no parent chain reaching
// back to ask.
func (h *TearOffHost) SetGraphicalFrames(g bool) { h.graphicalFrames = g }

// GraphicalFrames reports what the host was told about its surface.
func (h *TearOffHost) GraphicalFrames() bool { return h.graphicalFrames }

// applyCursor sets the system cursor, skipping redundant applications.
func (h *TearOffHost) applyCursor(shape core.CursorShape) {
	if h.setCursor == nil {
		return
	}
	// Re-assert on every hover, even when the shape is unchanged: the platform
	// must re-set the OS cursor on each mouse-move (macOS resets it to the arrow
	// otherwise), so a same-shape short-circuit here would starve it and the
	// cursor would flip back to the arrow as the pointer moves over the torn
	// window. The platform's SetCursor is the idempotent, cheap re-set.
	h.setCursor(shape)
}

// edgeAt returns the resize-edge bitmask for a window-local point, or 0
// when the point starts no resize - mirroring beginResize (no resize in
// the title row, on a non-resizable or zoomed window).
// effectiveGrip is the AFFORDANCE thickness, per axis: the whole painted
// frame border plus half a cell beyond it, matching docked windows
// (ResizeOverlayGrip).
// What a press actually grabs is narrower and border-inclusive — see
// ResizeHitGrip, used by edgeAt. FindFrameBorderUnits needs the desktop in the
// parent chain, which a detached window lacks, so derive the border straight
// from the live pixels-per-unit exactly as the desktop's
// WindowFrameBorderUnits does (ceil(scaled border px / ppu)).
func (h *TearOffHost) effectiveGrip() ResizeGrip {
	border := h.frameBorderUnits()
	return ResizeOverlayGrip(h.graphicalFrames, core.DefaultCellMetrics(), border, border)
}

// frameBorderUnits is the painted frame-border thickness in units, derived from
// the live pixels-per-unit exactly as the desktop's WindowFrameBorderUnits does
// (ceil(scaled border px / ppu), the zoom-scaled border law (a)). A detached
// window has no desktop in its parent chain
// for FindFrameBorderUnits, so it is computed here. It offsets both the resize
// grip and the title-bar zone so torn windows match docked ones under a wide
// border_width.
// pxPerUnit is this torn surface's live pixels-per-unit, or 1 when the host
// has no reporter (a bare host in a test). Device-pixel geometry converts
// through this, never through the integer device scale: the two agree only at
// font size 12.
func (h *TearOffHost) pxPerUnit() float64 {
	if h.ppu != nil {
		if v := h.ppu(); v > 0 {
			return v
		}
	}
	return 1
}

func (h *TearOffHost) frameBorderUnits() core.Unit {
	ppu := h.pxPerUnit()
	b := core.ScaledWindowFrameBorderPx(ppu)
	if b <= 0 {
		return 0
	}
	return core.Unit(math.Ceil(float64(b) / ppu))
}

func (h *TearOffHost) edgeAt(x, y core.Unit) int {
	if h.win.Flags()&WindowFlagNoResize != 0 || h.zoomed {
		return 0
	}
	b := h.win.Bounds()
	// The HIT zone follows the grab rule; effectiveGrip stays the affordance
	// overlay's, which must not move. A detached window has no desktop in its
	// parent chain, so the border comes from its own live pixels-per-unit.
	metrics := core.DefaultCellMetrics()
	border := h.frameBorderUnits()
	grip := ResizeHitGrip(h.graphicalFrames, metrics, h.pxPerUnit(), border, border)
	corner := ResizeOverlayGrip(h.graphicalFrames, metrics, border, border)
	edges := 0

	// Corners reach further in than the side zones do: a diagonal target only
	// as wide as a side zone is one nobody can hit. Same rule the docked path
	// applies in ResizeEdgeAt.
	if corner.X > grip.X || corner.Y > grip.Y {
		nearL, nearR := x < corner.X, x >= b.Width-corner.X
		nearT, nearB := y < corner.Y, y >= b.Height-corner.Y
		if nearL && nearR {
			nearL, nearR = 2*x < b.Width, 2*x >= b.Width
		}
		if nearT && nearB {
			nearT, nearB = 2*y < b.Height, 2*y >= b.Height
		}
		if (nearL || nearR) && (nearT || nearB) {
			if nearL {
				edges |= resizeLeft
			} else {
				edges |= resizeRight
			}
			if nearT {
				edges |= resizeTop
			} else {
				edges |= resizeBottom
			}
			return edges
		}
	}

	// On a window small enough (or a border wide enough) that opposite grips
	// overlap, a pointer sits in BOTH the left and right zone, or BOTH the top
	// and bottom. Rather than letting one side always win, the pointer's half
	// decides: past the 50% line the far edge (right / bottom) takes it, before
	// it the near edge (left / top) does — so both handles stay reachable.
	leftZone := x < grip.X
	rightZone := x >= b.Width-grip.X
	if leftZone && rightZone {
		if 2*x >= b.Width {
			leftZone = false
		} else {
			rightZone = false
		}
	}
	if leftZone {
		edges |= resizeLeft
	}
	if rightZone {
		edges |= resizeRight
	}

	topZone := y < grip.Y
	bottomZone := y >= b.Height-grip.Y
	if topZone && bottomZone {
		if 2*y >= b.Height {
			topZone = false
		} else {
			bottomZone = false
		}
	}
	if topZone {
		edges |= resizeTop
	} else if bottomZone {
		edges |= resizeBottom
	} else if y < core.DefaultCellMetrics().UnitsPerCellHeight {
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
func tornEdgeRects(b core.UnitRect, edges int, grip ResizeGrip) []core.UnitRect {
	var rects []core.UnitRect
	if edges&resizeLeft != 0 {
		rects = append(rects, core.UnitRect{Width: grip.X, Height: b.Height})
	}
	if edges&resizeRight != 0 {
		rects = append(rects, core.UnitRect{X: b.Width - grip.X, Width: grip.X, Height: b.Height})
	}
	if edges&resizeBottom != 0 {
		rects = append(rects, core.UnitRect{Y: b.Height - grip.Y, Width: b.Width, Height: grip.Y})
	}
	if edges&resizeTop != 0 {
		rects = append(rects, core.UnitRect{Width: b.Width, Height: grip.Y})
	}
	return rects
}

// refreshResizeHover re-arms the resize-edge highlight while a resize is in
// flight (the hover path that normally sets it is skipped then). It publishes
// the armed EDGES, not rectangles: the window's new size arrives back from
// the OS asynchronously, so anything measured here would be a frame behind,
// and the paint resolves the mask against the bounds it actually has.
func (h *TearOffHost) refreshResizeHover() {
	if !h.resizing || h.resizeEdges == 0 {
		return
	}
	h.win.SetResizeHoverEdges(h.resizeEdges, h.effectiveGrip())
}

// updateHoverAndCursor refreshes the resize-edge highlight and the system
// cursor for a plain (non-drag, non-resize) hover over the torn window.
func (h *TearOffHost) updateHoverAndCursor(x, y core.Unit) {
	// A popup (combobox dropdown, context menu) composited on the torn surface
	// floats above the content: over it, no trinket cursor from underneath
	// shows through — just the arrow. Mirrors the desktop's CursorAt rule, so
	// a torn-off window never shows an I-beam THROUGH an open menu.
	for _, p := range h.popups {
		b := p.Bounds
		if x >= b.X && y >= b.Y && x < b.X+b.Width && y < b.Y+b.Height {
			h.win.SetResizeHoverEdges(0, ResizeGrip{})
			h.applyCursor(core.CursorDefault)
			return
		}
	}
	// The open menu-bar dropdown is a SEPARATE compositor layer, not one of
	// h.popups, so it needs its own check — otherwise the I-beam shows through
	// where the dropdown overlaps the editor's text (the docked window gets this
	// for free from CursorAt's ActiveMenuBounds test).
	if b, _, _, ok := h.win.MenuDropdownLayer(); ok &&
		x >= b.X && y >= b.Y && x < b.X+b.Width && y < b.Y+b.Height {
		h.win.SetResizeHoverEdges(0, ResizeGrip{})
		h.applyCursor(core.CursorDefault)
		return
	}
	edges := h.edgeAt(x, y)
	if edges != 0 {
		h.win.SetResizeHoverEdges(edges, h.effectiveGrip())
		h.applyCursor(tornCursorForEdge(edges))
		return
	}
	h.win.SetResizeHoverEdges(0, ResizeGrip{})
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
		ID:     request.ID,
		Bounds: request.Bounds,
		// The opening control's rect travels with the popup so the two
		// cast one drop shadow when composited (a combo box and its list
		// read as one piece).
		Anchor:             request.Anchor,
		Paint:              request.Paint,
		HandleMousePress:   request.HandleMousePress,
		HandleMouseMove:    request.HandleMouseMove,
		HandleMouseRelease: request.HandleMouseRelease,
		HandleMouseWheel:   request.HandleMouseWheel,
		OnDismiss:          request.OnDismiss,
	})
	h.popupEpoch++
	h.surf.Invalidate(core.UnitRect{})
}

// UnregisterPopup implements core.PopupController.
func (h *TearOffHost) UnregisterPopup(id string) {
	for i, p := range h.popups {
		if p.ID == id {
			h.popups = append(h.popups[:i], h.popups[i+1:]...)
			h.popupEpoch++
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

// Frame implements platform.SurfaceHandler. Like SurfaceHost, the frame's
// platform text-caret request is applied after painting — and popups paint
// last, so a menu open over the window's content owns the caret.
func (h *TearOffHost) Frame(p *core.Painter) {
	if h.ghost {
		// The window lives on the desktop again; this surface only
		// survives (invisibly) to finish its mouse session.
		return
	}
	p.ResetTextCaretRequest()
	defer func() {
		platform.ApplyTextCaret(h.Surface(), platform.TextInputFrame{
			Caret:    p.TextCaretRequest(),
			Sink:     h.FocusedTextSink(),
			Complete: p.Complete(),
		})
	}()
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

// FrameBase implements platform.BaseLayerPainter: the torn window and
// its chrome, with the overlays GetChildWindows handed to the compositor
// left out. The menu bar itself stays on this surface — only its open
// dropdown lifts onto a layer, so it can carry a drop shadow.
func (h *TearOffHost) FrameBase(p *core.Painter) {
	if h.ghost {
		return
	}
	h.win.SetMenuDropdownComposited(true)
	defer h.win.SetMenuDropdownComposited(false)

	// The caret is not applied here: the popups this host handed to the
	// compositor paint on layers above, and one of them may claim it.
	// The host gathers every layer's request and applies the winner.
	p.ResetTextCaretRequest()
	h.win.Paint(p)
	if h.isModalBlocked() {
		b := h.win.Bounds()
		h.win.PaintModalDim(p, core.UnitRect{Width: b.Width, Height: b.Height})
	}
}

// RepaintRevision implements platform.RepaintRevisionProvider: what the
// torn window would paint is its own subtree plus its popups.
//
// Dragging a torn window is why this exists. The move arrives as input,
// Event invalidates the surface after every input event, and the host
// would otherwise repaint the entire window and re-upload its pixels for
// each mouse move — to produce the picture already on screen. The OS is
// moving the window; its contents did not change.
func (h *TearOffHost) RepaintRevision() uint64 {
	rev := h.win.SubtreeRepaintRevision()
	// Popups live on the host, not in the window's trinket subtree, so
	// opening or closing one moves nothing above. Their CONTENT changes
	// do bump the window (the trinket that changed is inside it).
	return rev*31 + h.popupEpoch
}

// GetChildWindows implements platform.WindowProvider so a torn-off
// window composites exactly as the desktop does: its own surface at the
// bottom, then its open menu dropdown, then its popups — each over a
// drop shadow of its own.
//
// It returns nil, keeping the plain single-surface present, whenever
// there is nothing to lift onto a layer. A torn window with no menu open
// and no popup gains nothing from the compositor, and the narrower the
// path switches, the less there is to go wrong.
func (h *TearOffHost) GetChildWindows() *platform.ChildWindowList {
	if h.ghost {
		return nil
	}

	popups := make([]interface{}, 0, len(h.popups))
	for _, popup := range h.popups {
		if popup.Paint != nil {
			popups = append(popups, popup)
		}
	}

	var menuDropdown interface{}
	if bounds, anchor, paint, ok := h.win.MenuDropdownLayer(); ok {
		menuDropdown = &struct {
			Bounds core.UnitRect
			Anchor core.UnitRect
			Paint  func(*core.Painter)
		}{Bounds: bounds, Anchor: anchor, Paint: paint}
	}

	if len(popups) == 0 && menuDropdown == nil {
		return nil
	}

	// No ClientArea: the window IS the surface, so nothing clips it.
	return &platform.ChildWindowList{
		Popups:       popups,
		MenuDropdown: menuDropdown,
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
				h.beginDragAt(e.X, e.Y)
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
		// Cmd+M miniaturizes, like any macOS document window. Resolved
		// through the keymap rather than matched against a spelling, so the
		// binding is what decides -- and so it is the same binding a docked
		// window minimizes on.
		if h.native != nil && h.minimizeKeys.KeyCommand(e.Key) == core.CmdAppMinimize {
			h.native.Minimize()
			handled = true
			break
		}
		handled = h.win.HandleKeyPress(e)
	case core.KeyReleaseEvent:
		handled = h.win.HandleKeyRelease(e)
	case core.TextEditingEvent:
		handled = h.win.HandleTextEditing(e)
	case core.TextCommitEvent:
		handled = h.win.HandleTextCommit(e)
	case core.TextEraseEvent:
		handled = h.win.HandleTextErase(e)
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
			h.beginDragAt(e.X, e.Y)
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
				e.X-h.lastClickX < metrics.UnitsPerCellWidth && h.lastClickX-e.X < metrics.UnitsPerCellWidth &&
				e.Y-h.lastClickY < metrics.UnitsPerCellHeight && h.lastClickY-e.Y < metrics.UnitsPerCellHeight {
				h.lastClickAt = time.Time{}
				h.ToggleZoom()
			} else {
				h.lastClickAt = now
				h.lastClickX, h.lastClickY = e.X, e.Y
				h.beginDragAt(e.X, e.Y)
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
			// The highlight bands are window-LOCAL and sized from the
			// window's bounds, so a live resize invalidates them: the right
			// band sits at Width-grip and the bottom one at Height-grip, both
			// of which just moved. Nothing else recomputes them mid-gesture —
			// the hover path that built them is skipped while resizing — so
			// they must be re-derived here or they hang where the drag
			// started. A single edge hides this when its own band happens not
			// to move (the left and top bands are anchored at 0), which is why
			// dragging a CORNER, where at least one band always moves, is
			// where it shows.
			h.refreshResizeHover()
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
			h.win.SetResizeHoverEdges(0, ResizeGrip{})
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
			h.win.SetResizeHoverEdges(0, ResizeGrip{})
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
		_, gpy := h.grabPx()
		if gy-gpy >= way {
			if ww > 0 {
				h.grabX = core.Unit(float64(h.grabX) * float64(h.zoomSaved[2]) / float64(ww))
			}
			h.zoomed = false
			h.dragRestored = true
			h.win.Restore()
			h.native.SetScreenSizePx(h.zoomSaved[2], h.zoomSaved[3])
			// The window just changed size under the pointer, so the pixel
			// anchor it was captured against is gone: re-derive it from the
			// re-proportioned unit grab, which is the best answer available
			// once the geometry it referred to no longer exists.
			h.grabPxX, h.grabPxY = h.px(h.grabX), h.px(h.grabY)
			h.grabPxValid = true
			h.native.SetScreenPositionPx(gx-h.grabPxX, gy-h.grabPxY)
		}
		return true
	}
	if h.dragRestored && gy >= way+h.px(core.DefaultCellMetrics().UnitsPerCellHeight) {
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
	gpx, gpy := h.grabPx()
	h.native.SetScreenPositionPx(gx-gpx, gy-gpy)
	return true
}

// maximizedFillSlackPx is how far under the work area a window may sit and
// still count as filling it — absorbing the rounding of a units/points/pixels
// round-trip, so only a real shrink reads as one.
const maximizedFillSlackPx = 4

// healMaximizedDivergence un-maximizes a window whose surface no longer fills
// the display's work area, adopting the size it actually has.
//
// The solo primary window is an OS-RESIZABLE window (created that way with a
// title bar, its border stripped at runtime), so the window manager serves its
// edges itself and a drag on one never reaches edgeAt — the check that keeps a
// zoomed torn window (borderless from birth) from being resized at all. The
// window therefore kept WindowStateMaximized at whatever size the OS gave it,
// and a maximized-state window paints the maximized frame: title bar only, no
// border stroke, a restore button that would teleport it, and no repaint of the
// surface beyond the content. Reconciling here covers every route in, since
// each one lands in Resized — the OS edge drag, a window-manager snap, or any
// programmatic size that isn't the zoom itself.
func (h *TearOffHost) healMaximizedDivergence() {
	if h.native == nil || !h.win.IsMaximized() {
		return
	}
	pw, ph := h.native.ScreenSizePx()
	_, _, ww, wh := h.native.WorkAreaPx()
	if pw <= 0 || ph <= 0 || ww <= 0 || wh <= 0 {
		return
	}
	if pw >= ww-maximizedFillSlackPx && ph >= wh-maximizedFillSlackPx {
		return // still filling the work area: genuinely maximized
	}
	h.zoomed = false
	h.win.RestoreInPlace()
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
	// A window still flagged maximized while this host is NOT zoomed is out of
	// sync: something maximized it without going through ToggleZoom, so the OS
	// surface never filled the work area and the edges stayed live (h.zoomed,
	// checked above, is what suppresses them on a properly zoomed window).
	// Resizing from there would leave a maximized-state window at an arbitrary
	// rect — no border stroke, a restore button that teleports, and surface
	// beyond the content left unpainted. Adopt the current rect as its normal
	// bounds so it resizes as the ordinary window it now is.
	h.win.RestoreInPlace()
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
// The ratio is re-read on every call — see the ppu field.
func (h *TearOffHost) px(u core.Unit) int {
	ppu := 1.0
	if h.ppu != nil {
		if v := h.ppu(); v > 0 {
			ppu = v
		}
	}
	return int(math.Round(float64(u) * ppu))
}

// SetTornGeometry wires the hardened cell-pitch conversions (the desktop's
// backend mapping) so the OS surface is sized/read on the frame's own grid.
func (h *TearOffHost) SetTornGeometry(g TornGeometry) { h.geom = g }

// pxHardX / pxHardY convert a unit LENGTH to device pixels on the hardened
// cell pitch (the frame's paint grid); size geometry uses these so the OS
// surface matches what the frame draws. They fall back to the raw px()
// ratio when no TornGeometry is wired (it coincides at the base zoom).
func (h *TearOffHost) pxHardX(u core.Unit) int {
	if h.geom != nil {
		return h.geom.HardUnitToPxX(u)
	}
	return h.px(u)
}

func (h *TearOffHost) pxHardY(u core.Unit) int {
	if h.geom != nil {
		return h.geom.HardUnitToPxY(u)
	}
	return h.px(u)
}

// unitHardX / unitHardY invert pxHardX/pxHardY: they read a device-pixel
// extent back to whole units on the hardened cell pitch, rounding to
// nearest so a surface sized to pxHardX(W) reports exactly W (no drift).
// Falls back to round(px / ppu) with no TornGeometry.
func (h *TearOffHost) unitHardX(px int) core.Unit {
	if h.geom != nil {
		return h.geom.HardPxToUnitX(px)
	}
	return h.unitFromPxRaw(px)
}

func (h *TearOffHost) unitHardY(px int) core.Unit {
	if h.geom != nil {
		return h.geom.HardPxToUnitY(px)
	}
	return h.unitFromPxRaw(px)
}

// unitFromPxRaw is the raw-ratio px->unit fallback used when no hardened
// TornGeometry is wired.
func (h *TearOffHost) unitFromPxRaw(px int) core.Unit {
	ppu := 1.0
	if h.ppu != nil {
		if v := h.ppu(); v > 0 {
			ppu = v
		}
	}
	return core.Unit(math.Round(float64(px) / ppu))
}

// paintablePxX / paintablePxY round a DRAGGED surface size down to an
// extent the surface can actually paint: the largest pixel size that is
// exactly where some whole unit count lands on the hardened cell pitch.
//
// A drag hands the window an arbitrary pixel count. The reported extent
// floors (it must never point past the true edge), so an odd size leaves
// a last column or row outside every unit — nothing paints it, the
// frame's outermost stroke is clipped against it, and that edge reads
// thinner than the other three. It costs at most a pixel of window. The
// desktop's own edge gesture rounds the same way; a torn window (and a
// solo one, hosted here on the primary surface) is the other half of it.
//
// Only whole-window SIZES come here. Positions, deltas, and the sizes we
// derive from our own unit bounds keep the geometry they had — those
// already land on the grid by construction.
func (h *TearOffHost) paintablePxX(px int) int {
	u := h.paintableUnitsX(px)
	if u <= 0 {
		return px
	}
	if fit := h.pxHardX(u); fit > 0 {
		return fit
	}
	return px
}

func (h *TearOffHost) paintablePxY(px int) int {
	u := h.paintableUnitsY(px)
	if u <= 0 {
		return px
	}
	if fit := h.pxHardY(u); fit > 0 {
		return fit
	}
	return px
}

// paintableUnitsX / paintableUnitsY read a device-pixel extent back to the
// largest whole unit count the surface can actually PAINT inside it: the
// nearest-unit answer, stepped down while the extent it paints overruns the
// pixels really there.
//
// Rounding to nearest alone answers one unit too MANY for an extent that
// falls between units — at 2 device px per unit a 101px surface reads as 51
// units, which is 102px of paint — and the frame then strokes its outer edge
// against a column the surface does not have, so that edge reads a pixel
// thin. Flooring the raw ratio instead sheds up to a unit every cycle and
// drifts. Stepping down from nearest does neither: a surface sized to
// pxHardX(W) still reads back as exactly W, because pxHardX(W) never
// overruns itself and the loop does not run.
//
// A size WE asked for is already paintable (resizeMove rounds it), so this
// matters for the sizes we do not choose: the OS's own configure events on
// the primary surface, a compositor's adjustment, a work-area zoom.
func (h *TearOffHost) paintableUnitsX(px int) core.Unit {
	u := h.unitHardX(px)
	for u > 0 && h.pxHardX(u) > px {
		u--
	}
	return u
}

func (h *TearOffHost) paintableUnitsY(px int) core.Unit {
	u := h.unitHardY(px)
	for u > 0 && h.pxHardY(u) > px {
		u--
	}
	return u
}

func (h *TearOffHost) resizeMove() bool {
	gx, gy := h.global()
	dx, dy := gx-h.startGX, gy-h.startGY
	metrics := core.DefaultCellMetrics()
	// The shared host minimum, on the hardened cell pitch this host sizes by.
	minW := h.pxHardX(metrics.UnitsPerCellWidth * MinHostCols)
	minH := h.pxHardY(metrics.UnitsPerCellHeight * MinHostRows)

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
	// Round the dragged size DOWN to what this surface can paint, so no
	// half-addressable pixel is left to clip the frame's outer stroke.
	// Left/top edges absorb the trim so the opposite edge stays put.
	if pw := h.paintablePxX(w); pw != w {
		if h.resizeEdges&resizeLeft != 0 {
			x += w - pw
		}
		w = pw
	}
	if ph := h.paintablePxY(ht); ph != ht {
		if h.resizeEdges&resizeTop != 0 {
			y += ht - ph
		}
		ht = ph
	}
	if h.resizeEdges&(resizeLeft|resizeTop) != 0 {
		h.native.SetScreenPositionPx(x, y)
	}
	h.native.SetScreenSizePx(w, ht)
	// The OS resize reports back through Resized, which updates the window bounds
	// AND the resize-edge highlights (computing them here from the pre-resize
	// bounds would just set them stale). Keep the resize cursor for the gesture.
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

// KeepPixelSizeOnFontZoom implements platform.PixelAnchoredOnFontZoom: a
// ZOOMED torn window fills its display's work area, so a live font zoom must
// leave its pixel size alone (the unit grid re-derives, like the main
// window) — re-sizing it to preserve units would pull it away from the
// display edges it is snapped to.
func (h *TearOffHost) KeepPixelSizeOnFontZoom() bool { return h.zoomed }

// zoomToWorkArea saves the current rect and fills the display's work
// area.
func (h *TearOffHost) zoomToWorkArea() {
	wx, wy, ww, wh := h.native.WorkAreaPx()
	if ww <= 0 || wh <= 0 {
		return
	}
	x, y := h.native.ScreenPositionPx()
	// Save the ACTUAL device-pixel size to restore, not a units->px
	// reconversion: the surface is already sized on the hardened pitch, so
	// reconverting would round-trip through the ratio and could restore a
	// hair off. ScreenSizePx is the rect to put back — floored to a
	// paintable extent, since the OS may have left the window between units
	// and restoring that verbatim would restore a thin edge with it. The
	// floor is identity on a size already on the grid, so this is still the
	// exact rect wherever it matters.
	pw, ph := h.native.ScreenSizePx()
	h.zoomSaved = [4]int{x, y, h.paintablePxX(pw), h.paintablePxY(ph)}
	h.zoomed = true
	h.win.Maximize()
	h.native.SetScreenPositionPx(wx, wy)
	// The work-area size itself is NOT rounded: a maximized window draws no
	// rounded frame (window.go's graphicalFrame excludes WindowStateMaximized,
	// as hostFrameInset does for the desktop), so there is no outer stroke to
	// protect here — and shrinking it would leave the screen edge uncovered.
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
	// Everything converts on the hardened cell pitch (the frame's grid).
	// Position is applied as a DELTA off the OS window's current screen px
	// (its screen origin isn't the desktop-relative b.X); size is set to the
	// TARGET width/height ABSOLUTELY, so it lands exactly where the frame
	// paints with no current-px round-trip.
	dx := h.pxHardX(b.X - cur.X)
	dy := h.pxHardY(b.Y - cur.Y)
	dw := b.Width - cur.Width
	dh := b.Height - cur.Height
	if dx != 0 || dy != 0 {
		x, y := h.native.ScreenPositionPx()
		h.native.SetScreenPositionPx(x+dx, y+dy)
	}
	if (dw != 0 || dh != 0) && h.win.Flags()&WindowFlagNoResize == 0 {
		h.native.SetScreenSizePx(h.pxHardX(b.Width), h.pxHardY(b.Height))
	}
	return true
}

// inTitleBar reports whether the point sits in the window's title
// row (the drag handle), matching the WindowManager's notion: the
// top cell row, excluding nothing else - button clicks were already
// offered to the window and declined.
func (h *TearOffHost) inTitleBar(x, y core.Unit) bool {
	b := h.win.Bounds()
	// The title bar is painted BELOW the top frame border, so its zone runs to
	// frameBorder + UnitsPerCellHeight — matching the WindowManager (titleTop +
	// UnitsPerCellHeight). Without the border term a wide border_width left only a thin
	// draggable/double-click strip. The top resize grip (checked before this)
	// owns the overlap at the very top.
	th := core.DefaultCellMetrics().UnitsPerCellHeight + h.frameBorderUnits()
	return x >= 0 && x < b.Width && y >= 0 && y < th
}

// Resized implements platform.SurfaceHandler: the window tracks the
// surface.
func (h *TearOffHost) Resized(size core.UnitSize) {
	// Derive the window's unit size from the actual device-pixel size on the
	// HARDENED cell pitch — the same grid the frame paints on — so the bounds
	// round-trip is exact: a surface sized to pxHardX(W) reads back as exactly
	// W. This is what keeps a torn window from drifting on undock/zoom (the
	// backend's cell-snapped Size() FLOORS, shedding up to a unit each cycle
	// so it accumulated) without going the other way and sizing bounds off the
	// raw ratio (which the frame does NOT paint on, so the frame's right/bottom
	// edge fell outside the surface — the "lost right edge"). With no hardened
	// geometry wired it falls back to the platform-reported size.
	if h.native != nil {
		if pw, ph := h.native.ScreenSizePx(); pw > 0 && ph > 0 {
			// The largest extent that FITS, not the nearest one: a size the
			// OS chose (an app-switch configure, a corner drag the compositor
			// adjusted, a work-area zoom) can fall between units, and rounding
			// it up leaves the frame stroking an edge column the surface does
			// not have. See paintableUnitsX.
			if w, ht := h.paintableUnitsX(pw), h.paintableUnitsY(ph); w > 0 && ht > 0 {
				size = core.UnitSize{Width: w, Height: ht}
			}
		}
	}
	h.win.SetBounds(core.UnitRect{Width: size.Width, Height: size.Height})
	h.healMaximizedDivergence()
	h.win.Layout()
	// While an edge-resize is in progress, keep the resize-edge highlight rects
	// in step with the new size. resizeMove only asks the OS to resize; the new
	// bounds land HERE (asynchronously), so this is the one place they can be
	// recomputed accurately — otherwise they stay at the pre-resize position
	// until the next hover recomputes them.
	if h.resizing {
		h.win.SetResizeHoverEdges(h.resizeEdges, h.effectiveGrip())
	}
	h.surf.Invalidate(core.UnitRect{})
}
