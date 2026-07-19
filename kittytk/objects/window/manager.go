// Package window provides windowing support for KittyTK.
package window

import (
	"sync"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Resize edge constants (can be combined for corners)
const (
	ResizeEdgeNone   = 0
	ResizeEdgeLeft   = 1 << 0
	ResizeEdgeRight  = 1 << 1
	ResizeEdgeTop    = 1 << 2
	ResizeEdgeBottom = 1 << 3
)

// DockProvider is implemented by desktops that have a dock for minimized windows.
type DockProvider interface {
	// DockEntryCount returns the number of entries in the dock.
	DockEntryCount() int
	// IsDockFocused returns true if the dock currently has focus.
	IsDockFocused() bool
	// FocusDock sets focus to the dock.
	FocusDock()
	// UnfocusDock removes focus from the dock.
	UnfocusDock()
}

// WindowManager manages all windows in the application.
// It handles z-ordering, focus, modal windows, and window positioning.
//
// Scope (G4): the WindowManager composites windows WITHIN ONE
// surface - its "screen" bounds are that surface's bounds, set by
// the desktop from Surface.Size. Windows granted native mode live
// outside its jurisdiction entirely (one window per surface, hosted
// by SurfaceHost with OS-provided chrome); which mode a window gets
// is host policy consulting Window.NativeRequested.
type WindowManager struct {
	mu sync.RWMutex

	// All windows in z-order (back to front)
	windows []*Window

	// Active/focused window
	activeWindow *Window

	// Previously active window (remembered when menu bar activates)
	previousActiveWindow *Window

	// Topmost window under the pointer on the last plain (no-button) move,
	// so its hover states can be cleared when the pointer moves off it.
	lastHoverWindow *Window

	// Modal window stacks, three tiers. modalStack is the system-level stack
	// (system modals such as the auth prompt block every in-surface window).
	// appModalStacks are per-application (keyed by the window's AppID): an app
	// modal blocks only that application's windows. windowModalStacks are
	// per-owner-window (keyed by the resolved owner): a window modal blocks its
	// owner window and that window's group. A modal joins exactly one tier by
	// its owner/appID (see registerModalLocked).
	modalStack        []*Window
	appModalStacks    map[core.ObjectID][]*Window
	windowModalStacks map[*Window][]*Window
	// modalObserved tracks modals for which a close observer (unregistering
	// the modal from its stack) has been installed, so re-docking a torn modal
	// - which re-runs AddWindow - does not add a duplicate observer.
	modalObserved map[*Window]bool

	// Desktop/root trinket (what's behind all windows)
	desktop core.Trinket

	// Screen bounds
	screenBounds core.UnitRect

	// Theme
	theme *style.Theme

	// Tear-off policy (G4 granting): when a drag crosses the surface
	// edge, the host may lift the window out into its own OS surface.
	// Returning true means the window left this manager.
	tearOff func(win *Window, event core.MouseMoveEvent, offsetX, offsetY core.Unit) bool

	// Drag state
	dragging    *Window
	dragStartX  core.Unit
	dragStartY  core.Unit
	dragOffsetX core.Unit
	dragOffsetY core.Unit
	// dragNeedsButton marks a drag armed programmatically (BeginDrag,
	// re-dock): its press happened in another surface, so its release
	// can be lost there too. Such a drag ends the moment motion
	// arrives without the button held. Press-armed drags keep the
	// historical behavior (terminal backends don't always report
	// button state on motion).
	dragNeedsButton bool
	// dragIsTearHandle marks a drag begun on the %/# tear handle: only
	// such a drag may tear the window off (or re-dock it); a plain
	// title drag just moves it in-surface. dragMoved tracks whether the
	// pointer left the press point, so a handle press released in place
	// is a click (toggles detach) rather than a drag.
	dragIsTearHandle bool
	dragMoved        bool

	// Resize state
	resizing       *Window
	resizeEdge     int
	resizeStartX   core.Unit
	resizeStartY   core.Unit
	resizeOriginal core.UnitRect

	// Double-click detection
	lastClickTime   time.Time
	lastClickX      core.Unit
	lastClickY      core.Unit
	lastClickWindow *Window

	// Focus-without-raise: track pressed window for conditional raise on release
	pressedWindow *Window

	// Callbacks
	onWindowAdded     func(*Window)
	onWindowRemoved   func(*Window)
	onActiveChanged   func(*Window)
	onRepaintNeeded   func()
	onWindowMinimized func(*Window) // Called when a window is minimized
	onWindowRestored  func(*Window) // Called when a window is restored
	// onBlockedClick fires when a modally-blocked window is clicked, so the
	// desktop can surface (raise/restore, incl. OS-restore of a torn one) the
	// modal blocking it - a convenience that also works across applications.
	onBlockedClick func(*Window)
	// activeAppID returns the ObjectID of the application whose menu bar is
	// currently showing on the desktop (0 = none / the desktop itself). The
	// wallpaper dim and wallpaper-click surface apply only when a system modal
	// is up or THAT app is the one blocked, so a modal in a background app
	// neither shades the wallpaper nor is raised by clicking it.
	activeAppID func() core.ObjectID
	// onWallpaperClick fires when the desktop background is clicked, so the
	// desktop can surface the active app's modal (if any).
	onWallpaperClick func()

	// Popup overlays (painted on top of everything)
	popups []*PopupOverlay

	// Cycle order for M-Tab: tracks activation order of windows and dock.
	// Items are *Window or nil (nil represents the dock).
	cycleOrder []interface{}

	// cycling is true while an M-Tab/M-S-Tab run is in progress. During a run
	// the MRU cycleOrder is left frozen (activation does not promote the
	// selected item to the front) so stepping walks a stable list in both
	// directions. The MRU is committed once, when the run ends (endCycleSession).
	cycling bool

	// lastCycleAt marks when the last cycle step ran, for the idle lock-in.
	lastCycleAt time.Time

	// modifierReleaseTracked is set on surfaces that deliver key releases
	// (the graphical/SDL backend): there the run is committed the moment all
	// modifiers go up (NotifyModifiersReleased), so the idle lock-in timer is
	// disabled. The TUI can't see the modifier release and relies on the timer.
	modifierReleaseTracked bool

	// Smooth positioning: when the surface's backend supports sub-cell
	// placement (pixel surfaces), drag and resize track the pointer at
	// unit granularity instead of snapping to cell boundaries.
	smoothPositioning bool

	// resizeGrip narrows the resize-handle zones on graphical frames
	// to the outer sliver of each edge (units; 0 = classic cell-wide
	// zones), so trinkets at a window's edge remain clickable.
	resizeGrip core.Unit
}

// PopupOverlay represents a popup that should be painted on top of all windows.
type PopupOverlay struct {
	// Unique identifier for the popup
	ID string
	// Bounds in screen coordinates
	Bounds core.UnitRect
	// Paint function to render the popup
	Paint func(p *core.Painter)
	// HandleMousePress function to handle clicks (returns true if handled)
	HandleMousePress func(event core.MousePressEvent) bool
	// HandleMouseMove function to handle mouse movement (returns true if handled)
	HandleMouseMove func(event core.MouseMoveEvent) bool
	// HandleMouseRelease function to handle mouse release (returns true if handled)
	HandleMouseRelease func(event core.MouseReleaseEvent) bool
	// HandleMouseWheel function to handle wheel scrolling (returns true if handled)
	HandleMouseWheel func(event core.MouseWheelEvent) bool
	// OnDismiss is called when the manager force-clears the popup (a
	// press outside every popup) without routing the press to it - the
	// owner's chance to reset its open-state. Not called on an
	// explicit UnregisterPopup.
	OnDismiss func()
}

// NewWindowManager creates a new window manager.
func NewWindowManager() *WindowManager {
	return &WindowManager{
		theme: style.DefaultTheme(),
	}
}

// SetSmoothPositioning controls whether drag and resize snap to cell
// boundaries. Pixel-capable surfaces enable smooth positioning; cell-grid
// surfaces leave it off so windows always land on whole cells.
func (m *WindowManager) SetSmoothPositioning(smooth bool) {
	m.mu.Lock()
	m.smoothPositioning = smooth
	windows := make([]*Window, len(m.windows))
	copy(windows, m.windows)
	m.mu.Unlock()
	for _, win := range windows {
		win.SetSmoothPositioning(smooth)
	}
}

// SetResizeGrip sets the resize-handle thickness in units for
// graphical frames. Zero restores the cell-frame behavior (the whole
// border row/column is the grip - it IS the frame there).
func (m *WindowManager) SetResizeGrip(grip core.Unit) {
	m.mu.Lock()
	m.resizeGrip = grip
	m.mu.Unlock()
}

// SmoothPositioning reports whether drag and resize track the pointer at
// unit granularity rather than snapping to cell boundaries.
// SetTearOffHandler installs the host's tear-off policy: called
// during a title drag when the pointer leaves the surface. A nil
// handler (the default) keeps every drag in-surface.
func (m *WindowManager) SetTearOffHandler(h func(win *Window, event core.MouseMoveEvent, offsetX, offsetY core.Unit) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tearOff = h
}

// BeginDrag arms the title-bar drag state programmatically, as if the
// user had pressed on the titlebar with the given grab offset. The
// re-dock choreography uses it so a window dropped back onto the
// desktop keeps following the held pointer.
func (m *WindowManager) BeginDrag(win *Window, offsetX, offsetY core.Unit) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dragging = win
	m.dragOffsetX = offsetX
	m.dragOffsetY = offsetY
	m.dragNeedsButton = true
}

func (m *WindowManager) SmoothPositioning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.smoothPositioning
}

// abs returns the absolute value of an integer.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ResizeEdgeAt returns the resize edge bits for point (x, y) against a
// window occupying `bounds` (all in the same coordinate space). `grip` is
// the effective grab-zone thickness in units - the graphical grip sliver
// plus any frame border; when it is 0 the cell-frame defaults from
// `metrics` apply (a full cell on the sides and bottom) and the top edge
// is NOT grabbable (the top row is the titlebar, used for dragging). The
// bottom and (when grabbable) top edges widen at the corners so diagonal
// resize is easy to hit.
//
// This is the single source of resize-edge geometry: the desktop
// WindowManager and the embedded MDIPane both call it, so desktop and MDI
// windows detect identical edges and corners.
func ResizeEdgeAt(bounds core.UnitRect, x, y core.Unit, metrics core.CellMetrics, grip core.Unit) int {
	edgeThreshold := metrics.CellWidth
	cornerThreshold := metrics.CellWidth * 2
	bottomBand := metrics.CellHeight
	if grip > 0 {
		edgeThreshold = grip
		cornerThreshold = grip * 2
		bottomBand = grip
	}

	edge := ResizeEdgeNone

	atBottom := y >= bounds.Y+bounds.Height-bottomBand && y < bounds.Y+bounds.Height
	atTop := grip > 0 && y >= bounds.Y && y < bounds.Y+grip

	if x >= bounds.X && x < bounds.X+edgeThreshold {
		edge |= ResizeEdgeLeft
	} else if x >= bounds.X+bounds.Width-edgeThreshold && x < bounds.X+bounds.Width {
		edge |= ResizeEdgeRight
	}

	if atBottom {
		edge |= ResizeEdgeBottom
		if x >= bounds.X && x < bounds.X+cornerThreshold {
			edge |= ResizeEdgeLeft
		} else if x >= bounds.X+bounds.Width-cornerThreshold && x < bounds.X+bounds.Width {
			edge |= ResizeEdgeRight
		}
	}

	if atTop {
		edge |= ResizeEdgeTop
		if x >= bounds.X && x < bounds.X+cornerThreshold {
			edge |= ResizeEdgeLeft
		} else if x >= bounds.X+bounds.Width-cornerThreshold && x < bounds.X+bounds.Width {
			edge |= ResizeEdgeRight
		}
	}

	return edge
}

// EffectiveResizeGrip is the grab-zone thickness in units for `win`: the
// base grip sliver plus the frame border (a thicker border makes a
// proportionally thicker grip that also overlaps a little into content).
// Shared so WindowManager and MDIPane compute the same grip.
func EffectiveResizeGrip(win *Window, baseGrip core.Unit) core.Unit {
	return baseGrip + core.FindFrameBorderUnits(win)
}

// ApplyResize computes the new bounds for a window resized from `original`
// by dragging edge bits `edge` a delta of (deltaX, deltaY). It enforces a
// minimum size (3x2 cells), optionally snaps to cell boundaries
// (snapToCells - false on smooth/pixel surfaces), keeps the window's
// top/left within `clientArea` (the far edge absorbs the clamp when
// resizing from that side), and limits the height to the client area. It
// is the single resize-geometry rule shared by the desktop WindowManager
// drag-resize and the embedded MDIPane.
func ApplyResize(original core.UnitRect, edge int, deltaX, deltaY core.Unit, metrics core.CellMetrics, snapToCells bool, clientArea core.UnitRect) core.UnitRect {
	nb := original
	if edge&ResizeEdgeLeft != 0 {
		nb.X = original.X + deltaX
		nb.Width = original.Width - deltaX
	}
	if edge&ResizeEdgeRight != 0 {
		nb.Width = original.Width + deltaX
	}
	if edge&ResizeEdgeTop != 0 {
		nb.Y = original.Y + deltaY
		nb.Height = original.Height - deltaY
	}
	if edge&ResizeEdgeBottom != 0 {
		nb.Height = original.Height + deltaY
	}

	if snapToCells {
		nb = metrics.AlignRect(nb)
	}

	minWidth := metrics.CellWidth * 3
	minHeight := metrics.CellHeight * 2
	if nb.Width < minWidth {
		if edge&ResizeEdgeLeft != 0 {
			nb.X = original.X + original.Width - minWidth
		}
		nb.Width = minWidth
	}
	if nb.Height < minHeight {
		if edge&ResizeEdgeTop != 0 {
			nb.Y = original.Y + original.Height - minHeight
		}
		nb.Height = minHeight
	}

	// Keep the top/left within the client area; when resizing from that
	// side, the opposite (anchored) edge absorbs the clamp so it stays put.
	if nb.X < clientArea.X {
		if edge&ResizeEdgeLeft != 0 {
			nb.Width = original.X + original.Width - clientArea.X
		}
		nb.X = clientArea.X
	}
	if nb.Y < clientArea.Y {
		if edge&ResizeEdgeTop != 0 {
			nb.Height = original.Y + original.Height - clientArea.Y
		}
		nb.Y = clientArea.Y
	}
	if nb.Height > clientArea.Height {
		nb.Height = clientArea.Height
	}
	return nb
}

// detectResizeEdge determines which window edge(s) the mouse is near.
// Returns a combination of ResizeEdge constants.
func (m *WindowManager) detectResizeEdge(win *Window, x, y core.Unit) int {
	if win.Flags()&WindowFlagNoResize != 0 || win.IsMaximized() {
		return ResizeEdgeNone
	}
	m.mu.RLock()
	baseGrip := m.resizeGrip
	m.mu.RUnlock()
	return ResizeEdgeAt(win.Bounds(), x, y, core.DefaultCellMetrics(),
		EffectiveResizeGrip(win, baseGrip))
}

// resizeEdgeRects returns the window-local rectangles (one per set edge
// bit, two for a corner) covering the size-sensitive band(s) for the
// given resize edge, matching detectResizeEdge's thresholds. Used to
// highlight the edge under the pointer.
func (m *WindowManager) resizeEdgeRects(win *Window, edge int) []core.UnitRect {
	m.mu.RLock()
	baseGrip := m.resizeGrip
	m.mu.RUnlock()
	return ResizeEdgeRects(win, edge, EffectiveResizeGrip(win, baseGrip))
}

// ResizeEdgeRects returns the window-local rectangles to highlight for the
// given resize edge(s), sized to the effective grip (the border width plus
// the grip sliver, matching ResizeEdgeAt) so the band covers the whole
// outer border. Shared by the WindowManager and the MDIPane so both draw
// the same resize overlay.
func ResizeEdgeRects(win *Window, edge int, grip core.Unit) []core.UnitRect {
	b := win.Bounds()
	metrics := core.DefaultCellMetrics()
	edgeThreshold := metrics.CellWidth
	bottomBand := metrics.CellHeight
	if grip > 0 {
		edgeThreshold = grip
		bottomBand = grip
	}

	var rects []core.UnitRect
	if edge&ResizeEdgeLeft != 0 {
		rects = append(rects, core.UnitRect{Width: edgeThreshold, Height: b.Height})
	}
	if edge&ResizeEdgeRight != 0 {
		rects = append(rects, core.UnitRect{X: b.Width - edgeThreshold, Width: edgeThreshold, Height: b.Height})
	}
	if edge&ResizeEdgeTop != 0 {
		// The top band is grip-thick (top resize is graphical-only, where
		// bottomBand == grip).
		rects = append(rects, core.UnitRect{Width: b.Width, Height: bottomBand})
	}
	if edge&ResizeEdgeBottom != 0 {
		rects = append(rects, core.UnitRect{Y: b.Height - bottomBand, Width: b.Width, Height: bottomBand})
	}
	return rects
}

// clearWindowHover clears any lingering per-widget hover on the window we
// last forwarded a hover move to, so nothing stays highlighted while a
// window is dragged or resized.
func (m *WindowManager) clearWindowHover() {
	if m.lastHoverWindow != nil {
		m.lastHoverWindow.HandleMouseMove(core.MouseMoveEvent{X: -1, Y: -1})
		m.lastHoverWindow = nil
	}
}

// ClearHover drops any lingering per-widget hover highlight. Called when
// the pointer leaves the surface entirely so nothing stays highlighted.
func (m *WindowManager) ClearHover() {
	m.clearWindowHover()
}

// pointOverOverlay reports whether a desktop-coordinate point is covered by a
// registered popup (combobox dropdown, context menu) or the desktop's active
// menu-bar dropdown. These float above every window, so window chrome (resize
// cursors and edge highlights) and window content cursors must not show
// through them.
func (m *WindowManager) pointOverOverlay(x, y core.Unit) bool {
	pt := core.UnitPoint{X: x, Y: y}
	m.mu.RLock()
	popups := make([]*PopupOverlay, len(m.popups))
	copy(popups, m.popups)
	desktop := m.desktop
	m.mu.RUnlock()

	for _, p := range popups {
		if p.Bounds.Contains(pt) {
			return true
		}
	}
	if desktop != nil {
		if g, ok := desktop.(interface{ ActiveMenuBounds() core.UnitRect }); ok {
			if b := g.ActiveMenuBounds(); !b.IsEmpty() && b.Contains(pt) {
				return true
			}
		}
	}
	return false
}

// updateResizeHover highlights the size-sensitive edge(s) of the topmost
// window under the pointer, clearing the highlight on every other window.
// Called on mouse move when no drag or resize is in progress.
func (m *WindowManager) updateResizeHover(x, y core.Unit) {
	m.mu.RLock()
	windows := make([]*Window, len(m.windows))
	copy(windows, m.windows)
	m.mu.RUnlock()

	// A dropdown menu or popup floats above the windows: no edge highlight
	// shows through it.
	overOverlay := m.pointOverOverlay(x, y)

	var target *Window
	edge := ResizeEdgeNone
	for i := len(windows) - 1; i >= 0; i-- {
		win := windows[i]
		if !win.IsVisible() || win.IsMinimized() {
			continue
		}
		if win.Bounds().Contains(core.UnitPoint{X: x, Y: y}) {
			target = win
			// A modally-blocked window can't be resized, and a point covered
			// by a dropdown/popup belongs to that overlay: either way, no edge
			// highlight.
			if !overOverlay && !m.isModalBlocked(win) {
				edge = m.detectResizeEdge(win, x, y)
			}
			break
		}
	}

	changed := false
	for _, win := range windows {
		var rects []core.UnitRect
		if win == target && edge != ResizeEdgeNone {
			rects = m.resizeEdgeRects(win, edge)
		}
		if win.SetResizeHoverRects(rects) {
			changed = true
		}
	}
	if changed {
		m.RequestRepaint()
	}
}

// topWindowAt returns the topmost visible, non-minimized window whose
// bounds contain the point, or nil.
func (m *WindowManager) topWindowAt(x, y core.Unit) *Window {
	m.mu.RLock()
	windows := make([]*Window, len(m.windows))
	copy(windows, m.windows)
	m.mu.RUnlock()
	for i := len(windows) - 1; i >= 0; i-- {
		win := windows[i]
		if !win.IsVisible() || win.IsMinimized() {
			continue
		}
		if win.Bounds().Contains(core.UnitPoint{X: x, Y: y}) {
			return win
		}
	}
	return nil
}

// ResizeCursorForEdge maps a set of resize edges to the cursor shape that
// signals resizing them (H/V for a single edge, the two diagonals for
// corners, default for none). Shared by the desktop WindowManager and the
// embedded MDIPane so both show the same resize cursors.
func ResizeCursorForEdge(edge int) core.CursorShape {
	left := edge&ResizeEdgeLeft != 0
	right := edge&ResizeEdgeRight != 0
	top := edge&ResizeEdgeTop != 0
	bottom := edge&ResizeEdgeBottom != 0
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

// CursorAt resolves the mouse cursor for a desktop-coordinate point: a
// resize cursor when over a window's size-sensitive edge, otherwise the
// cursor requested by the trinket under the pointer (e.g. a text I-beam),
// or the default arrow.
func (m *WindowManager) CursorAt(x, y core.Unit) core.CursorShape {
	// A dropdown menu or popup floats above the windows: over it, no resize or
	// trinket cursor from a window underneath shows through - just the arrow.
	if m.pointOverOverlay(x, y) {
		return core.CursorDefault
	}
	win := m.topWindowAt(x, y)
	if win == nil {
		return core.CursorDefault
	}
	// A modally-blocked window is inert: no resize cursor on its edges and no
	// trinket cursor (text I-beam, terminal, etc.) from its interior.
	if m.isModalBlocked(win) {
		return core.CursorDefault
	}
	if s := ResizeCursorForEdge(m.detectResizeEdge(win, x, y)); s != core.CursorDefault {
		return s
	}
	b := win.Bounds()
	return win.CursorShapeAt(x-b.X, y-b.Y)
}

// ClearResizeHover removes the resize-edge highlight from every window.
// Called when the pointer leaves the surface, so no stale band lingers.
func (m *WindowManager) ClearResizeHover() {
	m.mu.RLock()
	windows := make([]*Window, len(m.windows))
	copy(windows, m.windows)
	m.mu.RUnlock()
	changed := false
	for _, win := range windows {
		if win.SetResizeHoverRects(nil) {
			changed = true
		}
	}
	if changed {
		m.RequestRepaint()
	}
}

// SetDesktop sets the desktop trinket (background behind windows).
func (m *WindowManager) SetDesktop(desktop core.Trinket) {
	m.mu.Lock()
	m.desktop = desktop
	bounds := m.screenBounds
	m.mu.Unlock()

	// Set the desktop bounds to the screen size
	if desktop != nil && !bounds.IsEmpty() {
		desktop.SetBounds(bounds)
	}
}

// Desktop returns the desktop trinket.
func (m *WindowManager) Desktop() core.Trinket {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.desktop
}

// SetScreenBounds sets the available screen area.
func (m *WindowManager) SetScreenBounds(bounds core.UnitRect) {
	m.mu.Lock()
	m.screenBounds = bounds
	desktop := m.desktop
	m.mu.Unlock()

	// Update desktop bounds
	if desktop != nil {
		desktop.SetBounds(bounds)
	}

	// Adjust maximized windows to client area
	clientArea := m.ClientArea()
	for _, win := range m.windows {
		if win.IsMaximized() {
			win.SetBounds(clientArea)
		}
	}
}

// ClientArea returns the area available for windows (excluding desktop chrome like menu bars).
// ClampToClientArea keeps a window within reach on re-dock or
// placement: title bar vertically inside the client area (below any
// menu bar, above any dock/status bar) and a couple of columns
// visible horizontally.
func (m *WindowManager) ClampToClientArea(bounds core.UnitRect) core.UnitRect {
	return clampWindowToClientArea(bounds, m.ClientArea(), m.ScreenCellMetrics())
}

// displayBounds returns where a window is drawn and hit-tested: its
// logical bounds corralled into the current client area. The corral is
// PROVISIONAL - never written back - so shrinking the desktop nudges an
// off-screen window into view, and growing it again lets the window
// re-spread to its original spot. A deliberate interaction commits the
// corral (see commitDisplayBounds). Maximized windows are exempt (they
// already track the client area).
func (m *WindowManager) displayBounds(win *Window) core.UnitRect {
	if win.IsMaximized() {
		return win.Bounds()
	}
	return clampWindowToClientArea(win.Bounds(), m.ClientArea(), m.ScreenCellMetrics())
}

func (m *WindowManager) ClientArea() core.UnitRect {
	m.mu.RLock()
	screen := m.screenBounds
	desktop := m.desktop
	m.mu.RUnlock()

	// If desktop has a ClientArea method, use it
	if da, ok := desktop.(interface{ ClientArea() core.UnitRect }); ok {
		return da.ClientArea()
	}

	return screen
}

// ScreenBounds returns the available screen area for popups.
// This returns the ClientArea (excluding desktop chrome like menu bars and dock)
// so popups are positioned within the visible window area.
func (m *WindowManager) ScreenBounds() core.UnitRect {
	return m.ClientArea()
}

// AddWindow adds a window to the manager.
func (m *WindowManager) AddWindow(win *Window) {
	m.mu.Lock()
	// Check if already added
	for _, w := range m.windows {
		if w == win {
			m.mu.Unlock()
			return
		}
	}
	m.windows = append(m.windows, win)
	// Add to cycle order (for M-Tab cycling)
	m.cycleOrder = append(m.cycleOrder, win)
	// A modal window joins the appropriate modal stack (window/app/system) so
	// it blocks input from the moment it is added. Registration is idempotent
	// (re-dock re-runs AddWindow).
	isModal := win.Type() == WindowTypeModal
	if isModal {
		m.registerModalLocked(win)
	}
	handler := m.onWindowAdded
	desktop := m.desktop
	smooth := m.smoothPositioning
	m.mu.Unlock()

	// Tie modal unregistration to the window's close (not to manager
	// membership) so a torn-off modal keeps blocking across surfaces.
	if isModal {
		m.ensureModalCloseObserver(win)
	}

	win.SetSmoothPositioning(smooth)

	// Set window's parent to desktop so trinkets can traverse up to find timer provider
	if desktop != nil {
		if container, ok := desktop.(core.Container); ok {
			win.SetParent(container)
			// Ancestry decides capability lookups (graphical frames,
			// smooth positioning, metrics): a window laid out before
			// joining the manager used cell-frame insets, so re-lay it
			// out under its real context.
			win.Layout()
		}
	}

	// Set up request callbacks so button clicks go through WindowManager
	win.SetOnMinimizeRequest(func() {
		m.MinimizeWindow(win)
	})
	win.SetOnMaximizeRequest(func() {
		if win.IsMaximized() {
			m.RestoreWindow(win)
		} else {
			m.MaximizeWindow(win)
		}
	})
	win.SetOnCloseComplete(func() {
		m.RemoveWindow(win)
	})
	win.SetGetConstrainingBounds(func() core.UnitRect {
		return m.ClientArea()
	})

	// Set popup controller on window and its content so trinkets can use overlays
	win.SetPopupController(m)
	if content := win.Content(); content != nil {
		m.setPopupControllerRecursive(content)
	}

	// Position if not explicitly set (X and Y both at default 0)
	bounds := win.Bounds()
	if bounds.X == 0 && bounds.Y == 0 {
		m.positionWindow(win)
	}

	// Activate
	m.ActivateWindow(win)

	if handler != nil {
		handler(win)
	}

	// A modal must stay on top: if an app drops a new window onto the desktop
	// while a modal is up, pull the modal back over it and refocus it. Adding
	// the modal itself (via ShowModal) is exempt - win is the top modal.
	m.RaiseTopModalOver(win)
}

// setPopupControllerRecursive sets this WindowManager as the popup controller
// for a trinket and all its descendants.
func (m *WindowManager) setPopupControllerRecursive(trinket core.Trinket) {
	stampPopupController(trinket, m)
}

// stampPopupController assigns the popup controller to a trinket and
// its whole subtree. The WindowManager stamps windows it manages; a
// TearOffHost stamps its torn window so popups (combobox dropdowns,
// context menus) open on the torn surface instead of the desktop's.
func stampPopupController(trinket core.Trinket, pc core.PopupController) {
	if setter, ok := trinket.(interface{ SetPopupController(core.PopupController) }); ok {
		setter.SetPopupController(pc)
	}
	// Prefer AllChildren over Children: a TabTrinket's Children() is only the
	// active tab, so a combobox on an inactive tab would keep a stale
	// controller (e.g. the desktop's, from when its tab was last active) and
	// open its popup on the wrong surface after the window is torn off.
	if ac, ok := trinket.(interface{ AllChildren() []core.Trinket }); ok {
		for _, child := range ac.AllChildren() {
			stampPopupController(child, pc)
		}
		return
	}
	if container, ok := trinket.(core.Container); ok {
		for _, child := range container.Children() {
			stampPopupController(child, pc)
		}
	}
}

// RemoveWindow removes a window from the manager.
func (m *WindowManager) RemoveWindow(win *Window) {
	m.mu.Lock()
	for i, w := range m.windows {
		if w == win {
			m.windows = append(m.windows[:i], m.windows[i+1:]...)
			break
		}
	}

	// Remove from cycle order
	for i, it := range m.cycleOrder {
		if it == win {
			m.cycleOrder = append(m.cycleOrder[:i], m.cycleOrder[i+1:]...)
			break
		}
	}

	// NOTE: modal-stack membership is NOT dropped here. Removing a window from
	// the manager also happens on tear-off, and a torn-off modal must keep
	// blocking its app/owner across surfaces. The close observer installed in
	// AddWindow unregisters the modal when it is actually closed.

	// Update active window
	wasActive := m.activeWindow == win
	var newActive *Window
	if wasActive {
		m.activeWindow = nil
		if len(m.windows) > 0 {
			newActive = m.windows[len(m.windows)-1]
			m.activeWindow = newActive
		}
	}

	handler := m.onWindowRemoved
	activeHandler := m.onActiveChanged
	m.mu.Unlock()

	// Deactivate the removed window
	if wasActive {
		win.SetActive(false)
	}

	// Activate the new active window
	if newActive != nil {
		newActive.SetActive(true)
		// Focus the window's first trinket if no trinket is focused
		if fm := newActive.FocusManager(); fm != nil {
			if fm.FocusedTrinket() == nil {
				fm.FocusFirst()
			}
		}
	}

	if handler != nil {
		handler(win)
	}
	if activeHandler != nil && wasActive {
		activeHandler(newActive)
	}
}

// Windows returns all windows in z-order.
func (m *WindowManager) Windows() []*Window {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Window, len(m.windows))
	copy(result, m.windows)
	return result
}

// ActiveWindow returns the currently active window.
func (m *WindowManager) ActiveWindow() *Window {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeWindow
}

// PreviousActiveWindow returns the window that was active before the menu bar was activated.
func (m *WindowManager) PreviousActiveWindow() *Window {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.previousActiveWindow
}

// ActivateWindow brings a window to the front and gives it focus. Any
// M-Tab cycle run in progress is ended first (committing its MRU order), since
// an explicit activation is a fresh, non-cycle interaction.
func (m *WindowManager) ActivateWindow(win *Window) {
	m.endCycleSession()
	m.activate(win, true)
}

// activate brings a window to the front and gives it focus. reorderCycle
// controls whether the window is promoted to the front of the MRU cycle order:
// true for a normal activation, false while stepping an M-Tab cycle run (which
// must not churn the list it is walking).
func (m *WindowManager) activate(win *Window, reorderCycle bool) {
	m.mu.Lock()
	// Nothing to do only if it is already the active window AND visually
	// active. A window can be m.activeWindow yet inactive - e.g. a torn
	// window took surface focus and this one was SetActive(false)'d - in which
	// case a click must re-activate it, not early-return.
	if win == m.activeWindow && win != nil && win.IsActive() {
		m.mu.Unlock()
		return
	}

	// A modally-blocked window can't be fronted or focused.
	if m.isModalBlockedLocked(win) {
		m.mu.Unlock()
		return
	}

	oldActive := m.activeWindow
	m.activeWindow = win

	// Move to top of z-order, forcing owned overlays (dialogs, modals, tool
	// palettes) to stay above their owner - and, for a tool palette, bringing
	// its whole owner group forward. Pure z-order: overlays are not focused.
	m.raiseWithOverlaysLocked(win)

	// Move to front of cycle order (for M-Tab cycling), unless a cycle run is
	// stepping through - then the MRU stays frozen until the run ends.
	if reorderCycle {
		m.bringToCycleFront(win)
	}

	handler := m.onActiveChanged
	desktop := m.desktop
	m.mu.Unlock()

	// Deactivate menu bar and dock when a window becomes active
	if desktop != nil {
		if deactivator, ok := desktop.(interface{ DeactivateMenuBar() }); ok {
			deactivator.DeactivateMenuBar()
		}
		if dockProvider, ok := desktop.(DockProvider); ok {
			dockProvider.UnfocusDock()
		}
	}

	// Update active states (SetActive handles the onActivate callback)
	if oldActive != nil && oldActive != win {
		oldActive.SetActive(false)
	}
	if win != nil {
		win.SetActive(true)
		// Focus the window's first trinket if no trinket is focused
		if fm := win.FocusManager(); fm != nil {
			if fm.FocusedTrinket() == nil {
				fm.FocusFirst()
			}
		}
	}

	if handler != nil {
		handler(win)
	}
}

// cycleCommitTimeout is the idle gap after which an unfinished window-cycle run
// locks its result into the MRU order on surfaces that can't observe the
// modifier going up (the TUI). It approximates "you released the Alt-Tab keys".
const cycleCommitTimeout = 2 * time.Second

// SetModifierReleaseTracked marks whether this surface delivers key-release
// events (the graphical/SDL backend does). When true, a cycle run is committed
// the moment all modifiers rise (NotifyModifiersReleased) and the idle lock-in
// timer is disabled; when false (the TUI) the idle timer is the fallback.
func (m *WindowManager) SetModifierReleaseTracked(tracked bool) {
	m.mu.Lock()
	m.modifierReleaseTracked = tracked
	m.mu.Unlock()
}

// NotifyModifiersReleased locks an in-progress window-cycle run into the MRU
// order when every modifier key has gone up - the desktop convention of
// committing the Alt-Tab order on release. Driven by the SDL backend's key
// releases; a no-op when no run is in progress.
func (m *WindowManager) NotifyModifiersReleased() {
	m.endCycleSession()
}

// endCycleSession ends an in-progress M-Tab cycle run, committing its result to
// the MRU order: the window (or dock) the run landed on is promoted to the
// front, so the next independent cycle starts from it. A no-op when no run is
// in progress. Called on any non-cycle interaction (a normal key, a click, an
// explicit activation).
func (m *WindowManager) endCycleSession() {
	m.mu.Lock()
	if !m.cycling {
		m.mu.Unlock()
		return
	}
	m.cycling = false
	active := m.activeWindow
	desktop := m.desktop
	m.mu.Unlock()

	// The run landed on the dock when no window is active and the dock holds
	// focus; otherwise it landed on the active window.
	dockFocused := false
	if active == nil && desktop != nil {
		if dp, ok := desktop.(DockProvider); ok {
			dockFocused = dp.IsDockFocused()
		}
	}

	m.mu.Lock()
	if active != nil {
		m.bringToCycleFront(active)
	} else if dockFocused {
		m.bringToCycleFront(nil)
	}
	m.mu.Unlock()
}

// DeactivateActiveWindow removes focus from the active window without closing it.
// This is used when the menu bar becomes active. The deactivated window is remembered
// so it can be restored when the menu bar is dismissed.
func (m *WindowManager) DeactivateActiveWindow() {
	m.mu.Lock()
	oldActive := m.activeWindow
	if oldActive == nil {
		m.mu.Unlock()
		return
	}

	m.previousActiveWindow = oldActive
	m.activeWindow = nil
	handler := m.onActiveChanged
	m.mu.Unlock()

	oldActive.SetActive(false)

	if handler != nil {
		handler(nil)
	}
}

// RestorePreviousActiveWindow activates the previously active window if one was remembered.
// This is used when the menu bar is dismissed via Escape.
func (m *WindowManager) RestorePreviousActiveWindow() {
	m.mu.Lock()
	prev := m.previousActiveWindow
	m.previousActiveWindow = nil
	m.mu.Unlock()

	if prev != nil {
		m.ActivateWindow(prev)
	}
}

// FocusWindow gives a window focus without raising it to the front.
// This is used for focus-follows-click behavior where the window only
// raises on mouse release within its bounds.
func (m *WindowManager) FocusWindow(win *Window) {
	m.endCycleSession()
	m.mu.Lock()
	// As in ActivateWindow: only skip if it is already active AND visually
	// active, so a click re-focuses a topmost-but-inactive window (one that
	// lost its active look when a torn window took surface focus).
	if win == m.activeWindow && win != nil && win.IsActive() {
		m.mu.Unlock()
		return
	}

	// A modally-blocked window can't be focused.
	if m.isModalBlockedLocked(win) {
		m.mu.Unlock()
		return
	}

	oldActive := m.activeWindow
	m.activeWindow = win
	// Note: bringToFront is NOT called here - window stays in current z-order

	handler := m.onActiveChanged
	m.mu.Unlock()

	// Update active states (SetActive handles the onActivate callback)
	if oldActive != nil && oldActive != win {
		oldActive.SetActive(false)
	}
	if win != nil {
		win.SetActive(true)
		// Focus the window's first trinket if no trinket is focused
		if fm := win.FocusManager(); fm != nil {
			if fm.FocusedTrinket() == nil {
				fm.FocusFirst()
			}
		}
	}

	if handler != nil {
		handler(win)
	}
}

// RaiseWindow brings a window to the front without changing focus, keeping
// the owner-group z-order invariant.
func (m *WindowManager) RaiseWindow(win *Window) {
	m.mu.Lock()
	m.raiseWithOverlaysLocked(win)
	m.mu.Unlock()
	m.RequestRepaint()
}

// bringToFront moves a window to the top of the z-order.
func (m *WindowManager) bringToFront(win *Window) {
	for i, w := range m.windows {
		if w == win {
			// Remove from current position
			m.windows = append(m.windows[:i], m.windows[i+1:]...)
			// Add to end (top)
			m.windows = append(m.windows, win)
			return
		}
	}
}

// ownedOverlaysLocked returns the dialog/modal/toolpalette windows owned by
// owner, in current z-order (back-to-front). m.mu held.
func (m *WindowManager) ownedOverlaysLocked(owner *Window) []*Window {
	if owner == nil {
		return nil
	}
	var out []*Window
	for _, w := range m.windows {
		if w.Type().IsOwnedOverlay() && w.Owner() == owner {
			out = append(out, w)
		}
	}
	return out
}

// raiseOverlaysAboveLocked moves owner's overlays to the top of the z-order,
// keeping their relative order, so they sit above owner (expected already near
// the top). m.mu held.
func (m *WindowManager) raiseOverlaysAboveLocked(owner *Window) {
	overlays := m.ownedOverlaysLocked(owner)
	if len(overlays) == 0 {
		return
	}
	set := make(map[*Window]bool, len(overlays))
	for _, o := range overlays {
		set[o] = true
	}
	kept := make([]*Window, 0, len(m.windows))
	for _, w := range m.windows {
		if !set[w] {
			kept = append(kept, w)
		}
	}
	m.windows = append(kept, overlays...)
}

// raiseWithOverlaysLocked brings win to the top of the z-order while keeping
// the owner-group invariant: an owned overlay always sits above its owner.
// This is pure z-order and never changes focus/active state. m.mu held.
//
//   - normal/main/mdichild: raise it, then float its own overlays on top.
//   - tool palette: raise its owner group (owner then its overlays in relative
//     order), then float the chosen palette to the very top - so focusing a
//     palette brings its whole group forward with the palette on top.
//   - dialog/modal: raise it alone; it must not drag other windows forward.
func (m *WindowManager) raiseWithOverlaysLocked(win *Window) {
	if win == nil {
		return
	}
	switch {
	case win.Type() == WindowTypeToolPalette:
		if owner := win.Owner(); owner != nil {
			m.bringToFront(owner)
			m.raiseOverlaysAboveLocked(owner) // owner's overlays (incl win) above it
			m.bringToFront(win)               // the chosen palette on the very top
			return
		}
		m.bringToFront(win) // application-level palette: raise itself
	case win.Type().IsOwnedOverlay():
		m.bringToFront(win) // dialog or modal: forward alone
	default:
		m.bringToFront(win)
		m.raiseOverlaysAboveLocked(win)
	}
}

// bringToCycleFront moves an item to the front (end) of the cycle order.
// item should be *Window or nil (nil represents the dock).
func (m *WindowManager) bringToCycleFront(item interface{}) {
	// Remove existing occurrence
	for i, it := range m.cycleOrder {
		if it == item {
			m.cycleOrder = append(m.cycleOrder[:i], m.cycleOrder[i+1:]...)
			break
		}
	}
	// Add to end (most recently activated)
	m.cycleOrder = append(m.cycleOrder, item)
}

// isChildOf checks if child is a descendant of parent.
func (m *WindowManager) isChildOf(child, parent *Window) bool {
	for p := child.ParentWindow(); p != nil; p = p.ParentWindow() {
		if p == parent {
			return true
		}
	}
	return false
}

// modalDimAlpha is the darkening applied over a window (and the wallpaper)
// suppressed by the modal stack.
const modalDimAlpha = 0.25

// isModalBlocked reports whether an in-surface window is suppressed by the
// modal stack: it is blocked when a modal sits above it - for a non-modal
// window that means any modal at all; for a window that is itself a modal it
// means a modal added after it. The top modal (and any descendant of it) is
// never blocked, and a detached (torn-off) window lives on its own surface and
// is never blocked here.
func (m *WindowManager) isModalBlocked(win *Window) bool {
	if win == nil || win.IsDetached() {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isModalBlockedLocked(win)
}

// registerModalLocked routes a modal window into the appropriate tier by its
// owner and application: a window modal (owner set) joins that owner's stack,
// an application modal (app set) joins that app's stack, and only a modal with
// NEITHER an owner NOR an application is a system modal (the desktop's own
// prompts - never an app's, and never a torn-off window). Application is
// checked before ownerlessness precisely so an app's modal is an application
// modal even when it (or its owner) is torn off, keeping it blocking across
// surfaces. m.mu held.
func (m *WindowManager) registerModalLocked(win *Window) {
	if contains(m.modalStack, win) {
		return
	}
	switch {
	case win.Owner() != nil:
		owner := win.Owner()
		if contains(m.windowModalStacks[owner], win) {
			return
		}
		if m.windowModalStacks == nil {
			m.windowModalStacks = map[*Window][]*Window{}
		}
		m.windowModalStacks[owner] = append(m.windowModalStacks[owner], win)
	case win.AppID() != 0:
		id := win.AppID()
		if contains(m.appModalStacks[id], win) {
			return
		}
		if m.appModalStacks == nil {
			m.appModalStacks = map[core.ObjectID][]*Window{}
		}
		m.appModalStacks[id] = append(m.appModalStacks[id], win)
	default:
		m.modalStack = append(m.modalStack, win)
	}
}

func contains(s []*Window, w *Window) bool {
	for _, x := range s {
		if x == w {
			return true
		}
	}
	return false
}

// ensureModalCloseObserver installs, once per modal, a close observer that
// unregisters the modal from its stack. Registration is thus tied to the
// window's lifetime, not to window-manager membership - so a torn-off modal
// stays on its stack (and keeps blocking its app/owner across surfaces) and is
// removed only when it is actually closed.
func (m *WindowManager) ensureModalCloseObserver(win *Window) {
	m.mu.Lock()
	if m.modalObserved == nil {
		m.modalObserved = map[*Window]bool{}
	}
	if m.modalObserved[win] {
		m.mu.Unlock()
		return
	}
	m.modalObserved[win] = true
	m.mu.Unlock()

	win.AddOnClosed(func() {
		m.mu.Lock()
		m.unregisterModalLocked(win)
		delete(m.modalObserved, win)
		m.mu.Unlock()
	})
}

// IsTornWindowBlocked reports whether a torn-off (native-surface) window is
// suppressed by an application- or window-level modal of its own app. System
// modals live in-surface and do not reach across to torn surfaces. The torn
// window need not be in this manager's window list - the check consults only
// the app/owner modal stacks, which survive tear-off.
func (m *WindowManager) IsTornWindowBlocked(win *Window) bool {
	if win == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if id := win.AppID(); id != 0 {
		if st := m.appModalStacks[id]; len(st) > 0 {
			if top := st[len(st)-1]; !m.modalExempt(win, top) {
				return true
			}
		}
	}
	for owner, st := range m.windowModalStacks {
		if len(st) == 0 {
			continue
		}
		top := st[len(st)-1]
		if m.windowInModalScope(win, owner) && !m.modalExempt(win, top) {
			return true
		}
	}
	return false
}

// TopAppModal returns the top modal on an application's modal stack, or nil.
// Used to surface (and OS-restore) a minimized app modal when a blocked
// window or the wallpaper is clicked.
func (m *WindowManager) TopAppModal(appID core.ObjectID) *Window {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if st := m.appModalStacks[appID]; len(st) > 0 {
		return st[len(st)-1]
	}
	return nil
}

// unregisterModalLocked removes win from whichever modal stack holds it,
// pruning empty per-key stacks. m.mu held.
func (m *WindowManager) unregisterModalLocked(win *Window) {
	rm := func(s []*Window) []*Window {
		for i, w := range s {
			if w == win {
				return append(s[:i], s[i+1:]...)
			}
		}
		return s
	}
	m.modalStack = rm(m.modalStack)
	if id := win.AppID(); id != 0 && m.appModalStacks != nil {
		if st, ok := m.appModalStacks[id]; ok {
			if st = rm(st); len(st) == 0 {
				delete(m.appModalStacks, id)
			} else {
				m.appModalStacks[id] = st
			}
		}
	}
	if o := win.Owner(); o != nil && m.windowModalStacks != nil {
		if st, ok := m.windowModalStacks[o]; ok {
			if st = rm(st); len(st) == 0 {
				delete(m.windowModalStacks, o)
			} else {
				m.windowModalStacks[o] = st
			}
		}
	}
}

// anyModalActiveLocked reports whether any modal (system, application, or
// window level) is currently up. m.mu held.
func (m *WindowManager) anyModalActiveLocked() bool {
	return len(m.modalStack) > 0 || len(m.appModalStacks) > 0 || len(m.windowModalStacks) > 0
}

// modalExempt reports whether win is exempt from the modal top: it is the
// modal itself, a child window of it, or an owned overlay of it.
func (m *WindowManager) modalExempt(win, top *Window) bool {
	return win == top || m.isChildOf(win, top) || win.Owner() == top
}

// windowInModalScope reports whether win is within a window-level modal's
// scope for owner: the owner itself, a descendant of it, or one of its owned
// overlays (so a window modal blocks its owner's whole group).
func (m *WindowManager) windowInModalScope(win, owner *Window) bool {
	return win == owner || m.isChildOf(win, owner) || win.Owner() == owner
}

// isModalBlockedLocked is isModalBlocked with m.mu already held. A window is
// blocked when a system modal is up (blocks everything), an application modal
// for its own app is up (blocks that app's windows), or a window modal on a
// window in its group is up - unless the window is the relevant top modal, a
// child of it, or one of its owned overlays.
func (m *WindowManager) isModalBlockedLocked(win *Window) bool {
	if win == nil {
		return false
	}
	// System-level modals block every in-surface window.
	if n := len(m.modalStack); n > 0 {
		if top := m.modalStack[n-1]; !m.modalExempt(win, top) {
			return true
		}
	}
	// Application-level modals block only this app's windows.
	if id := win.AppID(); id != 0 {
		if st := m.appModalStacks[id]; len(st) > 0 {
			if top := st[len(st)-1]; !m.modalExempt(win, top) {
				return true
			}
		}
	}
	// Window-level modals block their owner's group.
	for owner, st := range m.windowModalStacks {
		if len(st) == 0 {
			continue
		}
		top := st[len(st)-1]
		if m.windowInModalScope(win, owner) && !m.modalExempt(win, top) {
			return true
		}
	}
	return false
}

// beginBlockedTitleDrag starts a move drag when a modally-blocked window is
// pressed on its draggable title area (not a titlebar button, not a resize
// edge). Every other press on a blocked window is swallowed with no effect -
// no edge resize, no button, no content, and no raise.
func (m *WindowManager) beginBlockedTitleDrag(win *Window, event core.MousePressEvent, bounds core.UnitRect) {
	if !hasTitleBar(win.Flags(), win.State()) || win.Flags()&WindowFlagNoMove != 0 {
		return
	}
	if m.detectResizeEdge(win, event.X, event.Y) != ResizeEdgeNone {
		return // no edge resizing while blocked
	}
	metrics := core.DefaultCellMetrics()
	titleTop := core.FindFrameBorderUnits(win)
	if event.Y >= bounds.Y+titleTop+metrics.CellHeight {
		return // below the title row
	}
	if win.buttonAtPosition(event.X-bounds.X, event.Y-bounds.Y) != TitleButtonNone {
		return // no titlebar buttons while blocked
	}
	m.mu.Lock()
	m.dragging = win
	m.dragStartX = event.X
	m.dragStartY = event.Y
	m.dragOffsetX = event.X - bounds.X
	m.dragOffsetY = event.Y - bounds.Y
	m.dragNeedsButton = false
	m.dragIsTearHandle = false
	m.dragMoved = false
	m.pressedWindow = nil
	m.mu.Unlock()
}

// ShowModal shows a window as a SYSTEM modal - the desktop's own prompts (the
// authorization prompt), which block every in-surface window. It is reserved
// for the desktop itself: applications must not use it. An app's modal instead
// goes through the application (Application.AddWindow), which stamps its app id
// so the modal is an application modal - blocking that app across the desktop
// and its torn-off surfaces, and never leaking into the system stack.
func (m *WindowManager) ShowModal(win *Window) {
	win.SetType(WindowTypeModal)
	m.AddWindow(win)
}

// CloseModal closes the top modal window.
func (m *WindowManager) CloseModal() {
	m.mu.Lock()
	if len(m.modalStack) == 0 {
		m.mu.Unlock()
		return
	}
	win := m.modalStack[len(m.modalStack)-1]
	m.modalStack = m.modalStack[:len(m.modalStack)-1]
	m.mu.Unlock()

	win.Close()
}

// topModal returns the topmost window on the modal stack, or nil when no
// modal is active.
func (m *WindowManager) topModal() *Window {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if n := len(m.modalStack); n > 0 {
		return m.modalStack[n-1]
	}
	return nil
}

// RaiseTopModalOver returns the top modal to the front of the z-order with
// focus after another window was brought up - a window re-docking, or an app
// adding a desktop window - so a modal always stays above everything and
// keeps focus. It is a no-op when no modal is active, when the given window
// is the top modal itself or a descendant of it (a modal's own child dialog
// belongs above it), or when the modal is minimized (a click restores it).
func (m *WindowManager) RaiseTopModalOver(win *Window) {
	top := m.topModal()
	if top == nil || top == win || m.isChildOf(win, top) || top.IsMinimized() {
		return
	}
	m.ActivateWindow(top)
}

// restoreMinimizedTopModal restores the top modal when it is minimized, so a
// click on any modally-blocked window or on the wallpaper surfaces the modal
// the user must deal with instead of leaving them stuck behind it. Restoring
// removes it from the dock via the restore callback, exactly as clicking its
// dock item would. Returns true when it restored a modal.
func (m *WindowManager) restoreMinimizedTopModal() bool {
	top := m.topModal()
	if top == nil || !top.IsMinimized() {
		return false
	}
	m.RestoreWindow(top)
	return true
}

// MaximizeWindow maximizes a window to fill the client area. Windows that
// can't be maximized (NoMaximize, or NoResize since maximizing is a
// resize) are left untouched, so callers - double-click, drag-to-top
// snap - don't silently resize a fixed-size dialog.
func (m *WindowManager) MaximizeWindow(win *Window) {
	if !canMaximize(win.Flags()) {
		return
	}
	clientArea := m.ClientArea()
	win.Maximize()
	win.SetBounds(clientArea)
}

// MinimizeWindow minimizes a window.
func (m *WindowManager) MinimizeWindow(win *Window) {
	win.Minimize()

	// Notify via callback (for dock row integration)
	m.mu.RLock()
	handler := m.onWindowMinimized
	m.mu.RUnlock()

	if handler != nil {
		handler(win)
	}

	m.RequestRepaint()
}

// RestoreWindow restores a minimized window.
func (m *WindowManager) RestoreWindow(win *Window) {
	win.Restore()
	m.ActivateWindow(win)

	// Notify via callback (for dock row integration)
	m.mu.RLock()
	handler := m.onWindowRestored
	m.mu.RUnlock()

	if handler != nil {
		handler(win)
	}

	m.RequestRepaint()
}

// SetOnBlockedClick sets the callback invoked when a modally-blocked window is
// clicked, so the host can surface the modal blocking it.
func (m *WindowManager) SetOnBlockedClick(handler func(*Window)) {
	m.mu.Lock()
	m.onBlockedClick = handler
	m.mu.Unlock()
}

// SetActiveAppIDFunc sets the accessor for the active menu-bar application's
// ObjectID, used to scope the wallpaper dim/click to the app that is blocked.
func (m *WindowManager) SetActiveAppIDFunc(fn func() core.ObjectID) {
	m.mu.Lock()
	m.activeAppID = fn
	m.mu.Unlock()
}

// SetOnWallpaperClick sets the callback invoked when the desktop background is
// clicked, so the host can surface the active application's modal.
func (m *WindowManager) SetOnWallpaperClick(handler func()) {
	m.mu.Lock()
	m.onWallpaperClick = handler
	m.mu.Unlock()
}

// wallpaperModalActive reports whether the desktop wallpaper should be dimmed
// (and its click surface a modal): a system modal is up, or the active menu-bar
// application has an application modal. A modal owned by a background app does
// not shade the wallpaper.
func (m *WindowManager) wallpaperModalActive() bool {
	var appID core.ObjectID
	if m.activeAppID != nil {
		appID = m.activeAppID()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.modalStack) > 0 {
		return true
	}
	return appID != 0 && len(m.appModalStacks[appID]) > 0
}

// WallpaperModalActive is the exported form of wallpaperModalActive: it reports
// whether the desktop (and its menu bar, showing the active app) is currently
// blocked by a modal - a system modal, or a modal owned by the active menu-bar
// app. A background app's modal does not count.
func (m *WindowManager) WallpaperModalActive() bool {
	return m.wallpaperModalActive()
}

// SetOnWindowMinimized sets the callback for window minimization.
func (m *WindowManager) SetOnWindowMinimized(handler func(*Window)) {
	m.mu.Lock()
	m.onWindowMinimized = handler
	m.mu.Unlock()
}

// SetOnWindowRestored sets the callback for window restoration.
func (m *WindowManager) SetOnWindowRestored(handler func(*Window)) {
	m.mu.Lock()
	m.onWindowRestored = handler
	m.mu.Unlock()
}

// RegisterPopup implements core.PopupController.
// It registers a popup overlay to be painted on top of all windows.
func (m *WindowManager) RegisterPopup(request *core.PopupRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Remove any existing popup with the same ID
	for i, p := range m.popups {
		if p.ID == request.ID {
			m.popups = append(m.popups[:i], m.popups[i+1:]...)
			break
		}
	}
	// Convert core.PopupRequest to internal PopupOverlay
	overlay := &PopupOverlay{
		ID:                 request.ID,
		Bounds:             request.Bounds,
		Paint:              request.Paint,
		HandleMousePress:   request.HandleMousePress,
		HandleMouseMove:    request.HandleMouseMove,
		HandleMouseRelease: request.HandleMouseRelease,
		HandleMouseWheel:   request.HandleMouseWheel,
		OnDismiss:          request.OnDismiss,
	}
	m.popups = append(m.popups, overlay)
}

// UnregisterPopup removes a popup overlay by ID.
func (m *WindowManager) UnregisterPopup(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, p := range m.popups {
		if p.ID == id {
			m.popups = append(m.popups[:i], m.popups[i+1:]...)
			return
		}
	}
}

// HasPopups returns true if there are any registered popups.
func (m *WindowManager) HasPopups() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.popups) > 0
}

// MapToScreen implements core.PopupController.
func (m *WindowManager) MapToScreen(trinket core.Trinket, local core.UnitPoint) core.UnitPoint {
	return MapTrinketToScreen(trinket, local)
}

// MapTrinketToScreen converts local trinket coordinates to surface
// coordinates by walking the ancestry, exchanging denominations at
// each re-denominating container boundary. Pure ancestry - both the
// WindowManager and a TearOffHost use it.
func MapTrinketToScreen(trinket core.Trinket, local core.UnitPoint) core.UnitPoint {
	// Traverse up the trinket hierarchy to accumulate offsets.
	// Each trinket's Bounds().X/Y is its position within its parent,
	// denominated in the parent's currency. The accumulated point is
	// kept in the currency of the space it currently describes.
	result := local

	current := trinket
	for current != nil {
		parent := current.Parent()

		// Leaving a container that re-denominates its interior: the
		// accumulated point is in its interior currency; re-express it
		// in the outer currency its bounds live in. (Windows exchange
		// in the parent branch below, where the client-area offset must
		// be added in the outer currency - skip them here.)
		//
		// The ROOT (a container with no parent - the desktop, or a torn
		// host) is skipped: its denomination IS the screen currency, so
		// its coordinates are final. Exchanging there would rescale every
		// mapped point by root/DefaultCellMetrics - which is exactly what
		// broke once the desktop's own denomination stopped being 8x16
		// (font_size), sending popups to the wrong place.
		if _, isWin := current.(*Window); !isWin && current.Parent() != nil {
			if mp, ok := current.(core.CellMetricsProvider); ok {
				if ov := mp.CellMetricsOverride(); ov != nil {
					outer := core.ParentCellMetrics(current)
					result.X = core.ExchangeX(result.X, *ov, outer)
					result.Y = core.ExchangeY(result.Y, *ov, outer)
				}
			}
		}

		bounds := current.Bounds()
		result.X += bounds.X
		result.Y += bounds.Y

		if parent == nil {
			break
		}

		// Check if parent is a scroll container and adjust for scroll
		// offset. Unit-denominated scrollers (smooth surfaces) report
		// units directly; classic scrollers report cells of their own
		// denomination.
		if su, ok := parent.(core.ScrollOffsetUnitsProvider); ok {
			ox, oy := su.ScrollOffsetUnits()
			result.X -= ox
			result.Y -= oy
		} else if scroller, ok := parent.(core.ScrollOffsetProvider); ok {
			pm := core.DefaultCellMetrics()
			if pw, ok := parent.(core.Trinket); ok {
				pm = core.FindEffectiveCellMetrics(pw)
			}
			scrollX, scrollY := scroller.ScrollOffset()
			result.X -= core.Unit(scrollX) * pm.CellWidth
			result.Y -= core.Unit(scrollY) * pm.CellHeight
		}

		// Crossing a window's content boundary: content coordinates are
		// interior currency; exchange to the window's outer currency,
		// then add the client-area offset (outer currency).
		if win, ok := parent.(*Window); ok {
			outer, interior := win.denominations()
			result.X = core.ExchangeX(result.X, interior, outer)
			result.Y = core.ExchangeY(result.Y, interior, outer)
			offset := win.ClientAreaOffset()
			result.X += offset.X
			result.Y += offset.Y
		}

		if pw, ok := parent.(core.Trinket); ok {
			current = pw
		} else {
			break
		}
	}

	return result
}

// ScreenCellMetrics returns the grid metrics of the screen/desktop
// surface - the denomination popup overlays are composited in.
func (m *WindowManager) ScreenCellMetrics() core.CellMetrics {
	m.mu.RLock()
	desktop := m.desktop
	m.mu.RUnlock()
	if dw, ok := desktop.(core.Trinket); ok && dw != nil {
		return core.FindEffectiveCellMetrics(dw)
	}
	return core.DefaultCellMetrics()
}

// trinketIsInWindow checks if a trinket is contained within a window.
func (m *WindowManager) trinketIsInWindow(trinket core.Trinket, win *Window) bool {
	current := trinket
	for current != nil {
		if current == win.Content() {
			return true
		}
		parent := current.Parent()
		if parent == nil {
			break
		}
		if pw, ok := parent.(core.Trinket); ok {
			current = pw
		} else {
			break
		}
	}
	return false
}

// positionWindow positions a new window using cascading.
func (m *WindowManager) positionWindow(win *Window) {
	m.mu.RLock()
	numWindows := len(m.windows)
	m.mu.RUnlock()

	clientArea := m.ClientArea()
	metrics := core.DefaultCellMetrics()

	// Use the window's current size if set, otherwise use SizeHint
	bounds := win.Bounds()
	width := bounds.Width
	height := bounds.Height
	if width <= 0 || height <= 0 {
		hint := win.SizeHint()
		width = hint.Width
		height = hint.Height
	}

	// Cascade offset (numWindows-1 because the window was already added to the list)
	cascadeIndex := numWindows - 1
	if cascadeIndex < 0 {
		cascadeIndex = 0
	}
	offset := core.Unit(cascadeIndex) * metrics.CellWidth * 2

	x := clientArea.X + offset
	y := clientArea.Y + offset

	// Wrap if off screen
	if x+width > clientArea.X+clientArea.Width {
		x = clientArea.X
	}
	if y+height > clientArea.Y+clientArea.Height {
		y = clientArea.Y
	}

	win.SetBounds(core.UnitRect{
		X:      x,
		Y:      y,
		Width:  width,
		Height: height,
	})
}

// TileWindows arranges windows in a tiled layout.
func (m *WindowManager) TileWindows() {
	m.mu.RLock()
	windows := make([]*Window, 0)
	for _, w := range m.windows {
		if w.IsVisible() && !w.IsMinimized() {
			windows = append(windows, w)
		}
	}
	m.mu.RUnlock()

	if len(windows) == 0 {
		return
	}

	clientArea := m.ClientArea()

	items := make([]TileItem, len(windows))
	for i, w := range windows {
		items[i] = TileItem{
			Resizable: w.Flags()&WindowFlagNoResize == 0,
			Size:      core.UnitSize{Width: w.Bounds().Width, Height: w.Bounds().Height},
		}
	}
	cells := TileLayout(clientArea, items)

	// TileLayout divides the area proportionally, so its cell boundaries land
	// at arbitrary unit positions. A cell-quantized surface (the TUI) can only
	// render windows on the cell grid, so snap each cell there; shared edges
	// round the same way and stay flush. Smooth (pixel) surfaces keep the exact
	// proportional layout.
	if !m.SmoothPositioning() {
		metrics := core.DefaultCellMetrics()
		for i := range cells {
			cells[i] = snapRectToCells(metrics, cells[i])
		}
	}

	for i, win := range windows {
		win.Restore()
		PlaceInCell(win, cells[i], items[i].Resizable)
	}
}

// snapRectToCells snaps a rectangle's edges down to the cell grid. Each edge is
// rounded with the same rule, so two cells that shared a boundary still meet
// exactly (no seam or overlap) after snapping.
func snapRectToCells(m core.CellMetrics, r core.UnitRect) core.UnitRect {
	left := m.RoundDownToCellX(r.X)
	top := m.RoundDownToCellY(r.Y)
	right := m.RoundDownToCellX(r.X + r.Width)
	bot := m.RoundDownToCellY(r.Y + r.Height)
	return core.UnitRect{X: left, Y: top, Width: right - left, Height: bot - top}
}

// PlaceInCell moves win into cell: a resizable window fills it, a
// non-resizable window keeps its own size at the cell's top-left.
func PlaceInCell(win *Window, cell core.UnitRect, resizable bool) {
	if resizable {
		win.SetBounds(cell)
		return
	}
	b := win.Bounds()
	b.X, b.Y = cell.X, cell.Y
	win.SetBounds(b)
}

// CascadeWindows arranges windows in a cascade.
func (m *WindowManager) CascadeWindows() {
	m.mu.RLock()
	windows := make([]*Window, 0)
	for _, w := range m.windows {
		if w.IsVisible() && !w.IsMinimized() {
			windows = append(windows, w)
		}
	}
	m.mu.RUnlock()

	if len(windows) == 0 {
		return
	}

	clientArea := m.ClientArea()
	metrics := core.DefaultCellMetrics()
	// The cascade step includes the frame border, so each window's whole
	// top chrome (border + titlebar) clears the one beneath it.
	border := core.FindFrameBorderUnits(windows[0])
	offset := metrics.CellWidth*2 + border

	// Standard size for cascaded windows - align to cell boundaries
	width := metrics.RoundDownToCellX(clientArea.Width * 3 / 4)
	height := metrics.RoundDownToCellY(clientArea.Height * 3 / 4)

	for i, win := range windows {
		// Leave any maximized/minimized state before positioning, so the
		// cascade bounds stick (Restore would otherwise overwrite them).
		win.Restore()

		x := clientArea.X + core.Unit(i)*offset
		y := clientArea.Y + core.Unit(i)*offset

		// A window that can't be resized is only repositioned, keeping its
		// own size; only resizable windows adopt the standard cascade size.
		w, h := width, height
		if win.Flags()&WindowFlagNoResize != 0 {
			b := win.Bounds()
			w, h = b.Width, b.Height
		}

		// Wrap if off screen
		if x+w > clientArea.X+clientArea.Width {
			x = clientArea.X
		}
		if y+h > clientArea.Y+clientArea.Height {
			y = clientArea.Y
		}

		win.SetBounds(core.UnitRect{
			X:      x,
			Y:      y,
			Width:  w,
			Height: h,
		})
	}
}

// HandleMousePress processes mouse events for windows.
func (m *WindowManager) HandleMousePress(event core.MousePressEvent) bool {
	m.mu.Lock()
	if m.dragging != nil && m.dragNeedsButton {
		// A press while an armed-without-press drag is live means its
		// release was lost: disarm and process the press normally.
		m.dragging = nil
		m.dragNeedsButton = false
	}
	m.mu.Unlock()

	m.mu.RLock()
	windows := m.windows
	desktop := m.desktop
	popups := m.popups
	m.mu.RUnlock()

	// Check popups first (highest z-order)
	for i := len(popups) - 1; i >= 0; i-- {
		popup := popups[i]
		if popup.Bounds.Contains(core.UnitPoint{X: event.X, Y: event.Y}) {
			if popup.HandleMousePress != nil {
				return popup.HandleMousePress(event)
			}
			return true // Consume click even if no handler
		}
	}

	// If there are popups but click was outside them, close all popups
	if len(popups) > 0 {
		m.mu.Lock()
		m.popups = nil
		m.mu.Unlock()
		// Tell each owner its popup is gone (outside the lock - the
		// callback may re-enter the controller), or it will keep
		// routing input to an overlay that no longer exists.
		for _, p := range popups {
			if p.OnDismiss != nil {
				p.OnDismiss()
			}
		}
		m.RequestRepaint()
		// Don't consume the click - let it propagate to close the underlying popup source
	}

	// Check if click is within an active menu dropdown (rendered on top of windows)
	// Menu dropdowns have higher z-order than windows, so check them first
	if desktop != nil {
		if menuBoundsGetter, ok := desktop.(interface {
			ActiveMenuBounds() core.UnitRect
		}); ok {
			menuBounds := menuBoundsGetter.ActiveMenuBounds()
			if !menuBounds.IsEmpty() && menuBounds.Contains(core.UnitPoint{X: event.X, Y: event.Y}) {
				// Click is on the menu dropdown - pass to desktop for menu handling
				return desktop.HandleMousePress(event)
			}
		}
	}

	// Check if click is in the dock area (dock has higher z-order than windows)
	if desktop != nil {
		if dockBoundsGetter, ok := desktop.(interface {
			DockBounds() core.UnitRect
		}); ok {
			dockBounds := dockBoundsGetter.DockBounds()
			if !dockBounds.IsEmpty() && dockBounds.Contains(core.UnitPoint{X: event.X, Y: event.Y}) {
				// Click is on the dock - pass to desktop for dock handling
				return desktop.HandleMousePress(event)
			}
		}
	}

	// Check if click is in the status bar area (status bar has higher z-order than windows)
	if desktop != nil {
		if statusBarBoundsGetter, ok := desktop.(interface {
			StatusBarBounds() core.UnitRect
		}); ok {
			statusBarBounds := statusBarBoundsGetter.StatusBarBounds()
			if !statusBarBounds.IsEmpty() && statusBarBounds.Contains(core.UnitPoint{X: event.X, Y: event.Y}) {
				// Click is on the status bar - pass to desktop for status bar handling
				return desktop.HandleMousePress(event)
			}
		}
	}

	// Check windows from top to bottom
	for i := len(windows) - 1; i >= 0; i-- {
		win := windows[i]
		if !win.IsVisible() || win.IsMinimized() {
			continue
		}

		bounds := m.displayBounds(win)
		if bounds.Contains(core.UnitPoint{X: event.X, Y: event.Y}) {
			// Deliberate interaction: commit the provisional corral so
			// this window's displayed position becomes its real one and
			// all downstream geometry (resize edges, drag offsets) agrees.
			win.SetBounds(bounds)

			// Close any active menu before processing window click
			if desktop != nil {
				if menuCloser, ok := desktop.(interface {
					CloseActiveMenu()
				}); ok {
					menuCloser.CloseActiveMenu()
				}
			}

			// Modally blocked: swallow the press, allowing only a title-bar
			// drag to move the window out of the way. If a system modal is
			// minimized, a click here restores it; otherwise surface the
			// application/window modal blocking this window (raise it, or
			// restore it from the dock) as a convenience - especially when
			// clicking another app's window that its own modal is blocking.
			if m.isModalBlocked(win) {
				if m.restoreMinimizedTopModal() {
					return true
				}
				if m.onBlockedClick != nil {
					m.onBlockedClick(win)
				}
				m.beginBlockedTitleDrag(win, event, bounds)
				return true
			}

			// Check for resize edge first - resize operations raise immediately
			resizeEdge := m.detectResizeEdge(win, event.X, event.Y)
			if resizeEdge != ResizeEdgeNone {
				// Activate (focus + raise) for resize
				m.ActivateWindow(win)
				// Start resize
				m.mu.Lock()
				m.resizing = win
				m.resizeEdge = resizeEdge
				m.resizeStartX = event.X
				m.resizeStartY = event.Y
				m.resizeOriginal = bounds
				m.pressedWindow = nil // Clear pressed window for resize
				m.mu.Unlock()
				return true
			}

			// Check for title bar interaction - titlebar operations raise
			// immediately. The titlebar sits below the top frame border, so
			// the drag region covers the border AND the titlebar row.
			metrics := core.DefaultCellMetrics()
			titleTop := core.FindFrameBorderUnits(win)
			if event.Y < bounds.Y+titleTop+metrics.CellHeight &&
				hasTitleBar(win.Flags(), win.State()) {

				// Activate (focus + raise) for titlebar interaction
				m.ActivateWindow(win)

				// The tear handle is draggable AND clickable: grab it to
				// begin a tear-capable drag; a release in place is a click
				// that toggles detach/dock.
				if win.Flags()&WindowFlagTearable != 0 &&
					win.buttonAtPosition(event.X-bounds.X, event.Y-bounds.Y) == TitleButtonTear {
					m.mu.Lock()
					m.dragging = win
					m.dragStartX = event.X
					m.dragStartY = event.Y
					m.dragOffsetX = event.X - bounds.X
					m.dragOffsetY = event.Y - bounds.Y
					m.dragIsTearHandle = true
					m.dragMoved = false
					m.dragNeedsButton = false
					m.pressedWindow = nil
					m.mu.Unlock()
					win.SetTearHighlight(true) // Show the tear-off halo while grabbed.
					return true
				}

				// First, let the window handle button clicks (close, minimize, maximize)
				// Pass the event to the window - if it handles a button click, don't drag
				localEvent := event
				localEvent.X -= bounds.X
				localEvent.Y -= bounds.Y
				if win.HandleMousePress(localEvent) {
					// Window handled it (button click) - update click tracking but don't drag
					m.mu.Lock()
					m.lastClickTime = time.Now()
					m.lastClickX = event.X
					m.lastClickY = event.Y
					m.lastClickWindow = win
					m.pressedWindow = nil
					m.mu.Unlock()
					return true
				}

				// Check for double-click on titlebar (for maximize/restore)
				now := time.Now()
				m.mu.Lock()
				isDoubleClick := m.lastClickWindow == win &&
					now.Sub(m.lastClickTime) < 400*time.Millisecond &&
					abs(int(event.X-m.lastClickX)) < int(metrics.CellWidth) &&
					abs(int(event.Y-m.lastClickY)) < int(metrics.CellHeight)

				// Update last click info
				m.lastClickTime = now
				m.lastClickX = event.X
				m.lastClickY = event.Y
				m.lastClickWindow = win
				m.mu.Unlock()

				if isDoubleClick && win.Flags()&WindowFlagNoMaximize == 0 {
					if win.IsMaximized() {
						win.Restore()
					} else {
						m.MaximizeWindow(win)
					}
					// Clear double-click state so next click starts fresh
					m.mu.Lock()
					m.lastClickWindow = nil
					m.pressedWindow = nil
					m.mu.Unlock()
					return true
				}

				// Start drag (if movable)
				if win.Flags()&WindowFlagNoMove == 0 {
					m.mu.Lock()
					m.dragging = win
					m.dragStartX = event.X
					m.dragStartY = event.Y
					m.dragOffsetX = event.X - bounds.X
					m.dragOffsetY = event.Y - bounds.Y
					m.dragNeedsButton = false
					m.dragIsTearHandle = false
					m.dragMoved = false
					m.pressedWindow = nil // Clear pressed window for drag
					m.mu.Unlock()
				}
				return true
			}

			// Content area click: focus without raise (raise on release within bounds)
			m.FocusWindow(win)
			m.mu.Lock()
			m.pressedWindow = win
			m.mu.Unlock()

			// Pass to window
			localEvent := event
			localEvent.X -= bounds.X
			localEvent.Y -= bounds.Y
			return win.HandleMousePress(localEvent)
		}
	}

	// Wallpaper (desktop background) click: only when the desktop itself is
	// blocked - a system modal, or a modal owned by the active menu-bar app -
	// surface it (restore a minimized system modal; raise/restore the active
	// app's modal). A background app's modal is left alone: that app isn't the
	// one showing, so clicking the wallpaper must not raise it.
	if m.wallpaperModalActive() {
		if m.restoreMinimizedTopModal() {
			return true
		}
		if m.onWallpaperClick != nil {
			m.onWallpaperClick()
			return true
		}
	}

	// Check desktop (already read above, but re-read in case it changed)
	if desktop != nil {
		return desktop.HandleMousePress(event)
	}

	return false
}

// HandleMouseMove processes mouse movement for dragging.
func (m *WindowManager) HandleMouseMove(event core.MouseMoveEvent) bool {
	m.mu.Lock()
	dragging := m.dragging
	offsetX := m.dragOffsetX
	offsetY := m.dragOffsetY
	resizing := m.resizing
	resizeEdge := m.resizeEdge
	resizeStartX := m.resizeStartX
	resizeStartY := m.resizeStartY
	resizeOriginal := m.resizeOriginal
	popups := m.popups
	m.mu.Unlock()

	// While a window is being dragged or resized, nothing should show a
	// hover highlight: clear whatever was hovered when the gesture began.
	if dragging != nil || resizing != nil {
		m.clearWindowHover()
	}

	// Check popups first (highest z-order) - only when not window dragging/resizing
	if dragging == nil && resizing == nil {
		for i := len(popups) - 1; i >= 0; i-- {
			popup := popups[i]
			if popup.HandleMouseMove != nil {
				if popup.HandleMouseMove(event) {
					return true
				}
			}
		}
	}

	// Handle resize
	if resizing != nil {
		newBounds := ApplyResize(resizeOriginal, resizeEdge,
			event.X-resizeStartX, event.Y-resizeStartY,
			core.DefaultCellMetrics(), !m.SmoothPositioning(), m.ClientArea())

		resizing.SetBounds(newBounds)
		// Keep the edge highlight on the edge being dragged, tracking the
		// window's new size instead of leaving it stale at the start bounds.
		resizing.SetResizeHoverRects(m.resizeEdgeRects(resizing, resizeEdge))
		m.RequestRepaint()
		return true
	}

	// Handle drag
	if dragging != nil {
		m.mu.RLock()
		needsButton := m.dragNeedsButton
		m.mu.RUnlock()
		if needsButton && event.Buttons&core.LeftButton == 0 {
			// The release was lost in another surface (re-dock
			// hand-off): the gesture is over, stop following hovers.
			m.mu.Lock()
			if m.dragging == dragging {
				m.dragging = nil
				m.dragNeedsButton = false
			}
			m.mu.Unlock()
			dragging.SetTearHighlight(false)
			return true
		}

		// Any motion during a drag marks it moved (a handle press that
		// never moves is a click, not a drag).
		m.mu.Lock()
		if m.dragging == dragging {
			m.dragMoved = true
		}
		isTearHandle := m.dragIsTearHandle
		m.mu.Unlock()

		// Tear-off: past the surface edge, the host may lift the window
		// out into its own OS surface (G4 granting) - but ONLY when the
		// drag was begun on the tear handle. A plain title drag just
		// moves the window in-surface.
		m.mu.RLock()
		tear := m.tearOff
		screen := m.screenBounds
		m.mu.RUnlock()
		if tear != nil && isTearHandle && !dragging.IsMaximized() &&
			(event.X < screen.X || event.Y < screen.Y ||
				event.X >= screen.X+screen.Width || event.Y >= screen.Y+screen.Height) {
			if tear(dragging, event, offsetX, offsetY) {
				m.mu.Lock()
				if m.dragging == dragging {
					m.dragging = nil
				}
				m.mu.Unlock()
				dragging.SetTearHighlight(false)
				return true
			}
		}

		// Track if we just restored from maximized (to avoid immediate re-maximize)
		justRestored := false

		// Constrain to client area (below menu bar, above status bar)
		clientArea := m.ClientArea()
		metrics := core.DefaultCellMetrics()

		// If window is maximized, only restore if dragging DOWN (below menu bar)
		// Dragging left/right while in menu bar area keeps window maximized
		if dragging.IsMaximized() {
			// Calculate where the window would be positioned
			newY := event.Y - offsetY

			// Only restore if dragging below the menu bar
			if newY >= clientArea.Y {
				// Get the normalized bounds before restore
				oldBounds := dragging.Bounds()

				// Restore the window
				dragging.Restore()
				justRestored = true
				newBounds := dragging.Bounds()

				// Force layout recalculation for the restored window state
				// This ensures content bounds are recalculated for normal mode (with borders)
				dragging.Layout()

				// Recalculate offset so the cursor stays proportionally positioned
				// on the titlebar (e.g., if you grabbed the middle, keep it middle)
				proportion := float64(offsetX) / float64(oldBounds.Width)
				offsetX = core.Unit(proportion * float64(newBounds.Width))

				// Update stored offset
				m.mu.Lock()
				m.dragOffsetX = offsetX
				m.mu.Unlock()
			} else {
				// Still in menu bar area - keep maximized, don't process further
				return true
			}
		}

		// Move window
		newX := event.X - offsetX
		newY := event.Y - offsetY

		bounds := dragging.Bounds()
		bounds.X = newX
		bounds.Y = newY

		// Snap-maximize only when the POINTER itself enters the menu-bar strip
		// above the client area - not merely when the window's top edge is
		// pushed up there (which fired too eagerly, since the grab offset lifts
		// the edge well before the cursor arrives). Skipped for a tear-handle
		// drag (that gesture tears off, not maximize) and for windows that
		// can't be maximized (fixed-size dialogs), which fall through to the
		// normal clamped move.
		if !isTearHandle && event.Y < clientArea.Y && canMaximize(dragging.Flags()) && !justRestored {
			if !dragging.IsMaximized() {
				m.MaximizeWindow(dragging)
				m.RequestRepaint()
			}
			return true
		}

		// Keep the window retrievable: title bar vertically within the
		// client area, at least a couple of columns visible horizontally
		// on each side (dragging it down to a few pixels made it
		// impossible to grab back).
		bounds = clampWindowToClientArea(bounds, clientArea, metrics)

		// Limit height to client area height (windows can be wider but not taller)
		if bounds.Height > clientArea.Height {
			bounds.Height = clientArea.Height
		}

		// Align position to cell boundaries (important after restore from
		// maximized); pixel surfaces drag smoothly at unit granularity
		if !m.SmoothPositioning() {
			bounds = metrics.AlignRect(bounds)
		}

		dragging.SetBounds(bounds)

		// Request repaint to show the window at its new position
		m.RequestRepaint()

		return true
	}

	// Not dragging or resizing in this manager. A held button means a gesture
	// began elsewhere (a menu scrub, a selection drag) and is passing through:
	// the resize-edge highlight is a hover affordance, so suppress it and drop
	// any lingering band. A plain move (no button) previews the edge.
	if event.Buttons == 0 {
		m.updateResizeHover(event.X, event.Y)
	} else {
		m.ClearResizeHover()
	}

	// Forward to desktop first (for menu bar drag navigation)
	m.mu.RLock()
	desktop := m.desktop
	active := m.activeWindow
	windows := make([]*Window, len(m.windows))
	copy(windows, m.windows)
	m.mu.RUnlock()

	if desktop != nil {
		if handler, ok := desktop.(interface {
			HandleMouseMove(core.MouseMoveEvent) bool
		}); ok {
			if handler.HandleMouseMove(event) {
				return true
			}
		}
	}

	// A held button means a drag/selection: keep routing to the active
	// window (drag capture). A plain move is hover: route it to the topmost
	// window under the pointer so its widgets highlight even when it isn't
	// active, clearing the window we last hovered when the pointer leaves.
	if event.Buttons != 0 {
		if active != nil && !active.IsMinimized() {
			bounds := m.displayBounds(active)
			localEvent := event
			localEvent.X -= bounds.X
			localEvent.Y -= bounds.Y
			if active.HandleMouseMove(localEvent) {
				m.RequestRepaint()
				return true
			}
		}
		return false
	}

	pos := core.UnitPoint{X: event.X, Y: event.Y}
	var hoverTarget *Window
	for i := len(windows) - 1; i >= 0; i-- {
		win := windows[i]
		// Use displayBounds (the on-screen, corral-clamped rectangle) so
		// detection matches the offset used to forward the local move.
		if win.IsVisible() && !win.IsMinimized() && m.displayBounds(win).Contains(pos) {
			hoverTarget = win
			break
		}
	}
	if m.lastHoverWindow != nil && m.lastHoverWindow != hoverTarget {
		m.lastHoverWindow.HandleMouseMove(core.MouseMoveEvent{X: -1, Y: -1})
	}
	m.lastHoverWindow = hoverTarget
	if hoverTarget != nil {
		bounds := m.displayBounds(hoverTarget)
		localEvent := event
		localEvent.X -= bounds.X
		localEvent.Y -= bounds.Y
		// A modally-blocked window shows no hover at all: clear any stuck
		// highlight and consume so it can't leak to windows below.
		if m.isModalBlocked(hoverTarget) {
			hoverTarget.HandleMouseMove(core.MouseMoveEvent{X: -1, Y: -1})
			m.RequestRepaint()
			return true
		}
		// Over a resize edge a press resizes, so no widget under the
		// pointer would fire: send an out-of-bounds move to clear all hover
		// in the window (titlebar buttons and edge-adjacent content).
		if m.detectResizeEdge(hoverTarget, event.X, event.Y) != ResizeEdgeNone {
			localEvent.X, localEvent.Y = -1, -1
		}
		hoverTarget.HandleMouseMove(localEvent)
		// The pointer is over this window: consume the move so the hover
		// can't leak to windows stacked underneath.
		m.RequestRepaint()
		return true
	}

	return false
}

// HandleMouseRelease processes mouse button release.
func (m *WindowManager) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	m.mu.Lock()
	dragging := m.dragging
	resizing := m.resizing
	pressedWin := m.pressedWindow
	popups := m.popups
	tearHandleClick := m.dragIsTearHandle && !m.dragMoved
	m.dragging = nil
	m.resizing = nil
	m.resizeEdge = ResizeEdgeNone
	m.pressedWindow = nil
	m.dragIsTearHandle = false
	m.dragMoved = false
	m.mu.Unlock()

	if dragging != nil || resizing != nil {
		// The tear-off halo only shows while the handle is grabbed.
		if dragging != nil {
			dragging.SetTearHighlight(false)
		}
		// A tear-handle press released in place is a click: toggle the
		// window between docked and detached (retaining position/size).
		if dragging != nil && tearHandleClick {
			dragging.requestTear()
		}
		return true
	}

	// Check popups first (highest z-order)
	for i := len(popups) - 1; i >= 0; i-- {
		popup := popups[i]
		if popup.HandleMouseRelease != nil {
			if popup.HandleMouseRelease(event) {
				return true
			}
		}
	}

	// Check if we should raise the pressed window (focus-without-raise behavior)
	// Only raise if release is over a non-occluded part of the window
	if pressedWin != nil && !pressedWin.IsMinimized() {
		bounds := m.displayBounds(pressedWin)
		releasePoint := core.UnitPoint{X: event.X, Y: event.Y}
		if bounds.Contains(releasePoint) {
			// Check that no other window is on top at this position
			m.mu.RLock()
			windows := m.windows
			m.mu.RUnlock()

			topmostAtPoint := (*Window)(nil)
			for i := len(windows) - 1; i >= 0; i-- {
				win := windows[i]
				if win.IsVisible() && !win.IsMinimized() && m.displayBounds(win).Contains(releasePoint) {
					topmostAtPoint = win
					break
				}
			}

			// Only raise if the pressed window is the topmost at the release point
			if topmostAtPoint == pressedWin {
				m.RaiseWindow(pressedWin)
			}
		}
	}

	// Forward to desktop first (for menu bar drag release)
	m.mu.RLock()
	desktop := m.desktop
	active := m.activeWindow
	m.mu.RUnlock()

	if desktop != nil {
		if handler, ok := desktop.(interface {
			HandleMouseRelease(core.MouseReleaseEvent) bool
		}); ok {
			if handler.HandleMouseRelease(event) {
				return true
			}
		}
	}

	// Forward to active window (for splitter/trinket release, but not if minimized)
	if active != nil && !active.IsMinimized() {
		bounds := m.displayBounds(active)
		localEvent := event
		localEvent.X -= bounds.X
		localEvent.Y -= bounds.Y
		if active.HandleMouseRelease(localEvent) {
			// Request repaint since trinket state may have changed
			m.RequestRepaint()
			return true
		}
	}

	return false
}

// HandleKeyPress processes keyboard events.
func (m *WindowManager) HandleKeyPress(event core.KeyPressEvent) bool {
	m.mu.RLock()
	active := m.activeWindow
	desktop := m.desktop
	m.mu.RUnlock()

	// Any key other than the cycle keys ends an in-progress M-Tab run,
	// Global shortcuts
	// Uses direct-key-handler naming: M- = Alt, C- = Ctrl, S- = Shift
	switch event.Key {
	case "M-Tab", "C-Tab":
		m.CycleWindows(true)
		return true
	case "M-S-Tab", "C-S-Tab":
		m.CycleWindows(false)
		return true
	case "F10":
		// F10 always goes to desktop for menu bar toggle
		if desktop != nil {
			return desktop.HandleKeyPress(event)
		}
	}

	// Alt+letter (M-<letter>) always goes to desktop first for menu accelerators
	if len(event.Key) == 3 && event.Key[0] == 'M' && event.Key[1] == '-' {
		letter := event.Key[2]
		if letter >= 'a' && letter <= 'z' {
			if desktop != nil {
				if desktop.HandleKeyPress(event) {
					return true
				}
			}
		}
	}

	// Check if desktop's menu bar is active (has focus or has open menu)
	// If so, send keys to desktop first to prevent window from intercepting
	if desktop != nil {
		if menuActive, ok := desktop.(interface{ IsMenuBarActive() bool }); ok && menuActive.IsMenuBarActive() {
			if desktop.HandleKeyPress(event) {
				return true
			}
		}
	}

	// Pass to active window first (but not if minimized, and not if a modal
	// blocks it - keys belong to the modal on top).
	if active != nil && !active.IsMinimized() && !m.isModalBlocked(active) {
		if active.HandleKeyPress(event) {
			// A key the window itself acts on is a genuine window interaction:
			// commit the cycle run's MRU order. Keys that fall through to the
			// menu bar (below) are not window interactions and do not commit.
			m.endCycleSession()
			return true
		}
	}

	// Forward unhandled keys to desktop for menu bar and other desktop shortcuts
	if desktop != nil {
		return desktop.HandleKeyPress(event)
	}

	return false
}

// CycleWindows cycles through windows and the dock (if it has entries).
// Uses activation order: most recently activated item is at the end.
// The dock participates in this order like a window (nil in cycleOrder).
func (m *WindowManager) CycleWindows(forward bool) {
	now := time.Now()

	m.mu.Lock()
	wasCycling := m.cycling
	gap := now.Sub(m.lastCycleAt)
	idleLockIn := !m.modifierReleaseTracked
	m.mu.Unlock()

	// Idle lock-in (TUI only, where we can't observe the modifier going up): a
	// step more than cycleCommitTimeout after the previous one is a new
	// gesture, so lock the prior run's landing spot into the MRU first. On the
	// SDL side NotifyModifiersReleased does this the instant all modifiers rise.
	if idleLockIn && wasCycling && gap > cycleCommitTimeout {
		m.endCycleSession()
	}

	m.mu.Lock()
	desktop := m.desktop
	// Read the LIVE cycle order (not a snapshot): stepping locates the current
	// selection by identity every press, so a window added or removed mid-run
	// is picked up automatically. The run only freezes the MRU *reordering*,
	// not the list's membership.
	cycleOrder := make([]interface{}, len(m.cycleOrder))
	copy(cycleOrder, m.cycleOrder)
	activeWindow := m.activeWindow
	// Mark the run in progress: activation during it must not promote the
	// selection to the front (that churn is what broke backward cycling). The
	// MRU commit is deferred to endCycleSession.
	m.lastCycleAt = now
	m.cycling = true
	m.mu.Unlock()

	// Check if dock is available and has entries
	var dockProvider DockProvider
	hasDock := false
	isDockFocused := false
	if desktop != nil {
		if dp, ok := desktop.(DockProvider); ok {
			dockProvider = dp
			hasDock = dp.DockEntryCount() > 0
			isDockFocused = dp.IsDockFocused()
		}
	}

	// Build effective cycle list: non-minimized windows + dock (if has entries)
	// Filter cycleOrder to only include valid items
	var effectiveCycle []interface{}
	for _, item := range cycleOrder {
		if item == nil {
			// Dock - include only if it has entries
			if hasDock {
				effectiveCycle = append(effectiveCycle, nil)
			}
		} else if win, ok := item.(*Window); ok {
			// Window - include only if not minimized
			if !win.IsMinimized() {
				effectiveCycle = append(effectiveCycle, win)
			}
		}
	}

	// Add dock to cycle if it has entries but isn't in the order yet
	if hasDock {
		hasDockInCycle := false
		for _, item := range effectiveCycle {
			if item == nil {
				hasDockInCycle = true
				break
			}
		}
		if !hasDockInCycle {
			effectiveCycle = append(effectiveCycle, nil)
		}
	}

	// Nothing to cycle to
	if len(effectiveCycle) == 0 {
		return
	}

	// Find current position in cycle
	currentIdx := -1
	if isDockFocused {
		for i, item := range effectiveCycle {
			if item == nil {
				currentIdx = i
				break
			}
		}
	} else {
		for i, item := range effectiveCycle {
			if item == activeWindow {
				currentIdx = i
				break
			}
		}
	}

	// Default to end if not found
	if currentIdx < 0 {
		currentIdx = len(effectiveCycle) - 1
	}

	// Calculate next index with wrapping. cycleOrder holds the most-recently
	// used item at the end, so, matching the OS convention: forward (Alt-Tab)
	// steps toward the most recent - one press lands on the most-recently-used
	// *other* window (index-1), two presses the second-most-recent, and so on;
	// backward (Shift-Alt-Tab) heads the other way, reaching the least-recently
	// used first (index+1, wrapping to the front of the list).
	var nextIdx int
	if forward {
		nextIdx = (currentIdx - 1 + len(effectiveCycle)) % len(effectiveCycle)
	} else {
		nextIdx = (currentIdx + 1) % len(effectiveCycle)
	}

	// Activate the target. During a run the MRU is left frozen (no
	// bringToCycleFront); endCycleSession commits the final landing spot.
	nextItem := effectiveCycle[nextIdx]
	if nextItem == nil {
		// Moving to dock - deactivate current window first
		if activeWindow != nil {
			activeWindow.SetActive(false)
		}
		m.mu.Lock()
		m.activeWindow = nil
		m.mu.Unlock()
		if dockProvider != nil {
			dockProvider.FocusDock()
		}
		m.RequestRepaint()
	} else if win, ok := nextItem.(*Window); ok {
		// Moving to a window
		if isDockFocused && dockProvider != nil {
			dockProvider.UnfocusDock()
		}
		m.activate(win, false)
	}
}

// Paint renders all windows.
func (m *WindowManager) Paint(p *core.Painter) {
	m.mu.RLock()
	desktop := m.desktop
	windows := m.windows
	m.mu.RUnlock()

	// Paint desktop
	if desktop != nil {
		desktop.Paint(p)
	}

	// Get client area to clip windows properly (avoid covering status bar)
	clientArea := m.ClientArea()

	// Darken the wallpaper only when the desktop itself is blocked: a system
	// modal is up, or the active menu-bar app has an application modal. A modal
	// in a background app blocks that app's own windows (each dims itself) but
	// leaves the wallpaper untouched. Graphical path only.
	if m.wallpaperModalActive() {
		p.FillRectPixelsAlpha(clientArea.X, clientArea.Y, 0, 0,
			p.UnitSpanPxX(clientArea.X, clientArea.X+clientArea.Width),
			p.UnitSpanPxY(clientArea.Y, clientArea.Y+clientArea.Height),
			0, 0, 0, modalDimAlpha)
	}

	// Paint windows from bottom to top, clipped to client area
	for _, win := range windows {
		if win.IsVisible() && !win.IsMinimized() {
			// Draw at the provisional (corralled) position so windows
			// left off-screen by a desktop shrink are nudged into view.
			bounds := m.displayBounds(win)

			// Calculate visible portion within client area
			visibleBounds := bounds.Intersection(clientArea)
			if visibleBounds.IsEmpty() {
				continue
			}

			// Tear-off affordance: a black halo just larger than the
			// window, drawn in desktop space (not clipped to the client
			// area) so a maximized window bleeds it over the menu and
			// status bars. Painted before the window so only the ring
			// beyond the frame shows.
			if win.TearIndicatorActive() {
				win.PaintTearHalo(p, bounds)
			}

			// Offset into window's local coordinates
			localClipX := visibleBounds.X - bounds.X
			localClipY := visibleBounds.Y - bounds.Y
			localClip := core.UnitRect{
				X:      localClipX,
				Y:      localClipY,
				Width:  visibleBounds.Width,
				Height: visibleBounds.Height,
			}

			windowPainter := p.WithOffset(bounds.X, bounds.Y).
				WithClip(localClip)
			// Anchor cell snapping at the window's origin so its interior is
			// pixel-stable as it moves (no sub-cell jitter at fractional
			// pixels-per-unit); restore the previous origin after.
			psx, psy := windowPainter.SetSnapOrigin(bounds.X, bounds.Y)
			win.Paint(windowPainter)
			// A window suppressed by a modal is darkened in place (over its
			// content, titlebar, and border), clipped to the frame shape.
			if m.isModalBlocked(win) {
				win.PaintModalDim(windowPainter, core.UnitRect{Width: bounds.Width, Height: bounds.Height})
			}
			windowPainter.SetSnapOrigin(psx, psy)
		}
	}

	// Paint menu dropdown on top of windows (if any is open)
	if desktop != nil {
		if dd, ok := desktop.(interface{ PaintMenuDropdown(*core.Painter) }); ok {
			dd.PaintMenuDropdown(p)
		}
	}

	// Paint registered popups on top of everything
	m.mu.RLock()
	popups := m.popups
	m.mu.RUnlock()
	for _, popup := range popups {
		if popup.Paint != nil {
			popup.Paint(p)
		}
	}
}

// SetOnWindowAdded sets the window added callback.
func (m *WindowManager) SetOnWindowAdded(handler func(*Window)) {
	m.mu.Lock()
	m.onWindowAdded = handler
	m.mu.Unlock()
}

// SetOnWindowRemoved sets the window removed callback.
func (m *WindowManager) SetOnWindowRemoved(handler func(*Window)) {
	m.mu.Lock()
	m.onWindowRemoved = handler
	m.mu.Unlock()
}

// SetOnActiveChanged sets the active window changed callback.
func (m *WindowManager) SetOnActiveChanged(handler func(*Window)) {
	m.mu.Lock()
	m.onActiveChanged = handler
	m.mu.Unlock()
}

// SetOnRepaintNeeded sets the repaint needed callback.
func (m *WindowManager) SetOnRepaintNeeded(handler func()) {
	m.mu.Lock()
	m.onRepaintNeeded = handler
	m.mu.Unlock()
}

// RequestRepaint requests a repaint from the application.
func (m *WindowManager) RequestRepaint() {
	m.mu.RLock()
	handler := m.onRepaintNeeded
	m.mu.RUnlock()
	if handler != nil {
		handler()
	}
}

// HandleMouseWheel routes a wheel event to the topmost visible
// window under the pointer (position routing; gesture latching
// happens above, in the desktop).
func (m *WindowManager) HandleMouseWheel(event core.MouseWheelEvent) bool {
	m.mu.RLock()
	windows := make([]*Window, len(m.windows))
	copy(windows, m.windows)
	m.mu.RUnlock()

	pos := core.UnitPoint{X: event.X, Y: event.Y}

	// Popup overlays float above everything.
	m.mu.RLock()
	popups := make([]*PopupOverlay, len(m.popups))
	copy(popups, m.popups)
	m.mu.RUnlock()
	for i := len(popups) - 1; i >= 0; i-- {
		popup := popups[i]
		if popup.HandleMouseWheel != nil && popup.Bounds.Contains(pos) {
			if popup.HandleMouseWheel(event) {
				m.RequestRepaint()
				return true
			}
		}
	}

	for i := len(windows) - 1; i >= 0; i-- {
		win := windows[i]
		b := m.displayBounds(win)
		if !win.IsVisible() || win.IsMinimized() || !b.Contains(pos) {
			continue
		}
		if m.isModalBlocked(win) {
			return true // blocked: consume, no scroll
		}
		local := event
		local.X -= b.X
		local.Y -= b.Y
		if win.HandleMouseWheel(local) {
			m.RequestRepaint()
			return true
		}
		return false // topmost window under the pointer owns the point
	}
	return false
}
