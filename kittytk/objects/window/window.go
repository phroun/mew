// Package window provides windowing support for KittyTK.
package window

import (
	"sync"
	"sync/atomic"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// WindowState represents the current state of a window.
type WindowState int

const (
	WindowStateNormal WindowState = iota
	WindowStateMaximized
	WindowStateMinimized
)

// WindowFlags control window behavior and appearance.
type WindowFlags int

const (
	WindowFlagNone                 WindowFlags = 0
	WindowFlagFrameless            WindowFlags = 1 << iota // No window frame
	WindowFlagNoTitle                                      // No title bar
	WindowFlagNoResize                                     // Cannot be resized
	WindowFlagNoMove                                       // Cannot be moved
	WindowFlagNoClose                                      // No close button
	WindowFlagNoMinimize                                   // No minimize button
	WindowFlagNoMaximize                                   // No maximize button
	WindowFlagStaysOnTop                                   // Always on top
	WindowFlagTearable                                     // Shows the %/# tear-off handle; window may detach
	WindowFlagNoTitleWhenMaximized                         // No title bar (and no frame) WHILE maximized; normal chrome when restored
)

// windowCornerRadius is the corner radius of the graphical window frame's
// single rounded-rect surface, in SCREEN units: the rounded-rect painter
// transforms the rectangle but passes the radius straight through, so this
// is stated in the surface's own denomination and never re-expressed into a
// window's. Kept below the frame's one-cell inset (8 units) so titlebar
// buttons and content never overlap the curve; cell surfaces ignore it.
const windowCornerRadius core.Unit = 6

// FrameCornerRadius reports the graphical frame's corner radius in screen
// units, for hosts that shape OS windows around torn-off frames.
func FrameCornerRadius() core.Unit { return windowCornerRadius }

// TitleButton identifies a titlebar button.
type TitleButton int

const (
	TitleButtonNone     TitleButton = iota
	TitleButtonClose                // [x] button
	TitleButtonMinimize             // [.] button
	TitleButtonMaximize             // [^] or [o] button
	TitleButtonTear                 // [%] docked / [#] detached handle
)

// TitleFocus identifies which title bar element has keyboard focus.
type TitleFocus int

const (
	TitleFocusNone     TitleFocus = iota // No title bar element focused
	TitleFocusTitle                      // Title text focused (for moving)
	TitleFocusClose                      // Close button focused
	TitleFocusMinimize                   // Minimize button focused
	TitleFocusMaximize                   // Maximize button focused
	TitleFocusTear                       // Tear-off handle focused (between [^] and title)
	TitleFocusBlur                       // Blur item focused (exit window)
)

// Window represents a floating window with frame, title bar, and content area.
// Windows support maximization, minimization, MDI-style child windows,
// and optional Mac-like menu integration.
type Window struct {
	core.TrinketBase
	core.TrinketKeys
	mu sync.RWMutex

	// Window properties
	title string
	flags WindowFlags
	state WindowState

	// windowType classifies the window's role (main, normal, mdichild,
	// dialog, modal, toolpalette). owner is the resolved non-overlay window a
	// dialog/modal/toolpalette floats above (nil = application-level). appID
	// is the owning application's ObjectID (0 = a system window). See
	// window_type.go.
	windowType WindowType
	owner      *Window
	appID      core.ObjectID
	// ownerRequestID is the wire object id an owner= property asked for; the
	// display layer resolves it to owner at adoption time.
	ownerRequestID uint64

	// G4 dual mode: the app's request for a native OS window,
	// honored when the platform can create surfaces.
	nativeRequested bool

	// smoothPositioning is stamped by the hosting window manager
	// from the surface capability (core.SmoothPositioner): pixel
	// surfaces drag/resize at unit granularity, cell surfaces snap.
	// Nested hosts (MDI panes) inherit it via FindSmoothPositioning.
	smoothPositioning bool

	// Position before maximization (for restore)
	normalBounds core.UnitRect

	// stateBeforeMinimize is the state the window was in when it was minimized
	// (Normal or Maximized), so Restore returns it to the right one — a
	// maximized window minimized to the dock comes back maximized, not normal.
	stateBeforeMinimize WindowState

	// Content
	content core.Trinket
	layout  core.LayoutManager

	// Focus management
	focusManager *core.FocusManager

	// Child windows (MDI support)
	parent   *Window
	children []*Window

	// Window chrome
	borderStyle style.BorderStyle
	titleStyle  style.CellStyle
	frameStyle  style.CellStyle

	// Font (nil = inherit from desktop/MDI pane)
	font *core.Font

	// Sizing
	minWidth  core.Unit
	minHeight core.Unit
	maxWidth  core.Unit
	maxHeight core.Unit

	// Callbacks
	onClose       func() bool // Return false to prevent close
	onResize      func(width, height core.Unit)
	onMove        func(x, y core.Unit)
	onActivate    func(active bool)
	onStateChange func(state WindowState)

	// detached is true while the window lives in its own torn-off
	// surface; the tear handle then shows '#' and re-docks on click.
	detached bool

	// mainRequested marks (via the wire `main` property) that this
	// window should become its application's main window when adopted -
	// so its menu/status chrome detaches with it on tear-off.
	mainRequested bool

	// tearHighlight is set while the tear handle is pressed or dragged
	// so the frame draws its black tear-off halo (see TearIndicatorActive).
	tearHighlight bool

	// menuDropdownComposited is set by a host that draws the menu bar's
	// open dropdown as its own compositor layer; paintChrome then leaves
	// it out of the window's surface. See MenuDropdownLayer.
	menuDropdownComposited bool

	// repaintRev counts repaint requests from anywhere in this window's
	// subtree (see core.SubtreeRepaintTracker). The GPU compositor
	// caches a texture per window and compares this against the value it
	// last painted, so a window nobody has touched costs no repaint, no
	// pixel conversion and no upload. Atomic, not under mu: Update()
	// bumps it from whatever goroutine changed something, and it must
	// never be in the way of a lock the caller already holds.
	repaintRev atomic.Uint64

	// resizeBandRects are window-local rectangles (one per lit resize edge,
	// two for a corner) that the frame fills with the translucent cue. Cue
	// only: what responds to a press is the resize edge, computed fresh from
	// ResizeHitGrip, which never reads these. The host decides when to set
	// them -- on a hover, and during a drag as well.
	resizeBandRects []core.UnitRect

	// resizeBandEdges is the same highlight expressed as the EDGE MASK
	// instead, with the band thickness that sizes it. Preferred, because the
	// bands are derived from the window's bounds and those change under a
	// live resize: computing rectangles up front bakes in whatever size the
	// window had when the gesture began, and a window that then grows leaves
	// its bands stranded mid-frame. The paint resolves the mask against the
	// bounds it is actually painting, which cannot be stale.
	resizeBandEdges     int
	resizeBandThickness EdgeThickness

	// Detached main-window chrome, set by the desktop when the window is
	// torn off: a menu bar between the title bar and content, and a
	// status bar along the bottom edge. Kept as generic core.Trinket so
	// the window package needn't import trinkets. Both only occupy space,
	// paint, and receive input while the window is detached; either may
	// be hidden.
	menuBar          core.Trinket
	statusBar        core.Trinket
	menuBarVisible   bool
	statusBarVisible bool
	// lastChromeHover is the chrome trinket (menu/status bar) that last
	// received a hover move, so it can be sent a clearing move when the
	// pointer leaves it and its hover doesn't stick.
	lastChromeHover core.Trinket

	// shortcutResolver, when set, gets first crack at a key event's
	// accelerator after the window's own menu bar. The desktop points a
	// torn-off child window's resolver at its detached main window's menu
	// bar, so the child services the app's shortcuts (Cut/Copy/Paste, ...)
	// despite carrying no chrome of its own.
	shortcutResolver func(core.KeyPressEvent) bool

	// passNextKeyRaw makes HandleKeyPress route the very next key straight
	// to the focused trinket, bypassing this window's own menu-bar shortcut
	// handling - the detached-window half of the app's "raw key input"
	// feature. onRawKeyDone fires once that key is consumed.
	passNextKeyRaw bool
	onRawKeyDone   func()

	// Request callbacks (for WindowManager integration)
	onMinimizeRequest     func()                   // Called when user clicks minimize button
	onMaximizeRequest     func()                   // Called when user clicks maximize button
	onTearRequest         func()                   // Called when the tear handle is activated (dock<->detach)
	onBoundsRequest       func(core.UnitRect) bool // Takes title-focus keyboard geometry whole (torn-off hosts)
	onCloseComplete       func()                   // Called when window is closed, to remove from manager
	onClosedObservers     []func()                 // Additional close observers (survive onCloseComplete reassignment)
	getConstrainingBounds func() core.UnitRect     // Returns the client area for movement constraints
	getDisplayBounds      func() core.UnitRect     // Returns where the container DRAWS this window (the corral)
	popupController       core.PopupController     // Popup controller for ComboBox etc.

	// Button press tracking
	pressedButton TitleButton // Currently pressed titlebar button

	// contentMousePressed records that a mouse press was routed to the
	// CONTENT and its release has not arrived yet: the content has captured
	// the gesture, so moves and the release keep flowing to it even when the
	// pointer crosses the window's own chrome (menu bar, status bar). Without
	// this, dragging a selection down onto the status bar froze the drag —
	// the chrome-first routing swallowed every further move.
	contentMousePressed bool
	buttonHovered       bool        // Whether mouse is still over the pressed button
	hoveredButton       TitleButton // Titlebar button under the pointer (plain hover)

	// Title bar keyboard focus
	titleFocus TitleFocus // Which title bar element has keyboard focus
	// keyContext is what this window's keyboard currently offers, rebuilt when
	// the UI state changes (see refreshKeyContext). Held per window so a
	// change here cannot stale another window's.
	keyContext *core.KeyContext
	// What the context above was built from, so a stale one is noticed at the
	// point of use rather than needing everything that could change it to
	// remember to say so.
	keyContextReg     *core.KeyRegistry
	keyContextRev     uint64
	keyContextState   core.UIState
	resizeEdges       int           // Which edges are being keyboard-resized (ResizeEdge* constants)
	resizeStartBounds core.UnitRect // Bounds when resize operation started (for Escape to revert)

	// Active state (set by WindowManager/MDIPane, separate from focus)
	isActive bool

	// quasiActive marks a torn-off window that has yielded OS focus to the
	// desktop but stays "quasi-active": its border remains lit (active
	// colors) yet single (heavy) instead of the focused double border,
	// mirroring an in-surface window that is active but not focused. A real
	// SetActive (either direction) clears it.
	quasiActive bool
}

// NewWindow creates a new window with the given title.
func NewWindow(title string) *Window {
	w := &Window{
		title:       title,
		state:       WindowStateNormal,
		borderStyle: style.BorderDouble,
		titleStyle:  style.DefaultStyle().WithFg(style.ColorWhite).WithBg(style.ColorBlue).Bold(),
		frameStyle:  style.DefaultStyle().WithFg(style.ColorBrightCyan).WithBg(style.ColorBlue),
		minWidth:    80, // 10 characters minimum
		minHeight:   48, // 3 lines minimum
		maxWidth:    1<<30 - 1,
		maxHeight:   1<<30 - 1,
	}
	w.TrinketBase = *core.NewTrinketBase()
	// A window's own vocabulary: the frame commands it always answers to, and
	// the geometry family that belongs to a FOCUSED TITLE BAR -- sixteen
	// bindings that exist in no other situation, which is why the title bar
	// is a UI state rather than a trinket. Nothing here reaches the content;
	// the focused trinket resolves its own keys against its own set.
	w.SetCommands(
		core.CmdWindowClose, core.CmdWindowMaximizeToggle, core.CmdAppMenu,
		core.CmdAppHelp, core.CmdAppQuit,
		core.CmdFocusNext, core.CmdFocusPrior,
		core.CmdTrinketActivate, core.CmdWindowCancelResize,
		core.CmdWindowMoveFineUp, core.CmdWindowMoveFineDown,
		core.CmdWindowMoveFineLeft, core.CmdWindowMoveFineRight,
		core.CmdWindowSizeFineUp, core.CmdWindowSizeFineDown,
		core.CmdWindowSizeFineLeft, core.CmdWindowSizeFineRight,
		core.CmdWindowMoveUp, core.CmdWindowMoveDown,
		core.CmdWindowMoveLeft, core.CmdWindowMoveRight,
		core.CmdWindowSizeUp, core.CmdWindowSizeDown,
		core.CmdWindowSizeLeft, core.CmdWindowSizeRight,
	)
	w.Init(w)
	w.SetFocusPolicy(core.StrongFocus)
	w.focusManager = core.NewFocusManager(nil)
	return w
}

// FocusManager returns the window's focus manager.
func (w *Window) FocusManager() *core.FocusManager {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.focusManager
}

// Title returns the window title.
func (w *Window) Title() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.title
}

// SetTitle sets the window title.
func (w *Window) SetTitle(title string) {
	w.mu.Lock()
	w.title = title
	w.mu.Unlock()
	w.Update()
}

// SetNativeRequested records the app's preference for a native OS
// window (G4 dual mode). It is a REQUEST, honored when the hosting
// platform can create surfaces (see SurfaceHost); single-surface
// platforms (the terminal) keep the window in-surface under the
// WindowManager. Matches the wire's `native` flag.
func (w *Window) SetNativeRequested(native bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.nativeRequested = native
}

// NativeRequested reports whether a native window was requested.
func (w *Window) NativeRequested() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.nativeRequested
}

// SetSmoothPositioning is stamped by the hosting manager from the
// surface capability.
func (w *Window) SetSmoothPositioning(smooth bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.smoothPositioning = smooth
}

// SmoothWindowPositioning implements core.SmoothPositioningProvider,
// letting trinkets inside this window (e.g. MDI panes) inherit the
// surface's positioning granularity.
func (w *Window) SmoothWindowPositioning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.smoothPositioning
}

// Flags returns the window flags.
func (w *Window) Flags() WindowFlags {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.flags
}

// SetFlags sets the window flags.
func (w *Window) SetFlags(flags WindowFlags) {
	w.mu.Lock()
	w.flags = flags
	w.mu.Unlock()
	w.Update()
}

// State returns the current window state.
func (w *Window) State() WindowState {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.state
}

// SetContent sets the window's content trinket.
func (w *Window) SetContent(trinket core.Trinket) {
	w.mu.Lock()
	w.content = trinket
	fm := w.focusManager
	if trinket != nil {
		trinket.SetParent(w)
	}
	w.mu.Unlock()

	// Update focus manager root and focus first non-furtive trinket
	if fm != nil {
		fm.SetRoot(trinket)
		fm.FocusFirstNonFurtive()
	}

	w.layoutContent()
	w.Update()
}

// Content returns the window's content trinket.
func (w *Window) Content() core.Trinket {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.content
}

// SetLayout sets the layout manager for the content area.
func (w *Window) SetLayout(layout core.LayoutManager) {
	w.mu.Lock()
	w.layout = layout
	w.mu.Unlock()
	w.layoutContent()
}

// Layout returns the layout manager.
func (w *Window) LayoutManager() core.LayoutManager {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.layout
}

// SetLayoutManager implements core.Container.
func (w *Window) SetLayoutManager(layout core.LayoutManager) {
	w.SetLayout(layout)
}

// Parent returns the parent window (for MDI).
func (w *Window) ParentWindow() *Window {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.parent
}

// SetParentWindow sets the parent window (for MDI).
func (w *Window) SetParentWindow(parent *Window) {
	w.mu.Lock()
	oldParent := w.parent
	w.parent = parent
	w.mu.Unlock()

	// Remove from old parent
	if oldParent != nil {
		oldParent.removeChildWindow(w)
	}

	// Add to new parent
	if parent != nil {
		parent.addChildWindow(w)
	}
}

// ChildWindows returns all child windows.
func (w *Window) ChildWindows() []*Window {
	w.mu.RLock()
	defer w.mu.RUnlock()
	result := make([]*Window, len(w.children))
	copy(result, w.children)
	return result
}

func (w *Window) addChildWindow(child *Window) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, c := range w.children {
		if c == child {
			return
		}
	}
	w.children = append(w.children, child)
}

func (w *Window) removeChildWindow(child *Window) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, c := range w.children {
		if c == child {
			w.children = append(w.children[:i], w.children[i+1:]...)
			return
		}
	}
}

// canMaximize reports whether the window may be maximized. Maximizing is
// a form of resize, so it is suppressed both by an explicit NoMaximize
// flag and by NoResize. Governs the maximize button (paint, hit-test,
// focus order, keyboard/mouse triggers), programmatic Maximize, and the
// window manager's drag-to-top snap.
func canMaximize(flags WindowFlags) bool {
	return flags&WindowFlagNoMaximize == 0 && flags&WindowFlagNoResize == 0
}

// hasTitleBar reports whether the window shows a title bar in the given state,
// and thus whether its title-bar hit regions are live: the caption buttons,
// drag-to-move/detach, and double-click-to-restore. A NoTitle or Frameless
// window never has one; a NoTitleWhenMaximized window has one only when NOT
// maximized (it drops all chrome while maximized and regains it when restored).
// When there is no title bar, those clicks must not be caught - the top row is
// ordinary content there.
func hasTitleBar(flags WindowFlags, state WindowState) bool {
	if flags&WindowFlagNoTitle != 0 || flags&WindowFlagFrameless != 0 {
		return false
	}
	if flags&WindowFlagNoTitleWhenMaximized != 0 && state == WindowStateMaximized {
		return false
	}
	return true
}

// Maximize maximizes the window.
func (w *Window) Maximize() {
	w.mu.Lock()
	if w.state == WindowStateMaximized {
		w.mu.Unlock()
		return
	}
	if !canMaximize(w.flags) {
		w.mu.Unlock()
		return
	}

	// Store current bounds for restore
	w.normalBounds = w.Bounds()
	w.state = WindowStateMaximized
	handler := w.onStateChange
	w.mu.Unlock()

	// Request the window manager to maximize us
	// (actual resize happens through SetBounds from manager)
	w.Update()

	if handler != nil {
		handler(WindowStateMaximized)
	}
}

// Minimize minimizes the window.
func (w *Window) Minimize() {
	w.mu.Lock()
	if w.state == WindowStateMinimized {
		w.mu.Unlock()
		return
	}
	if w.flags&WindowFlagNoMinimize != 0 {
		w.mu.Unlock()
		return
	}

	// Remember what to come back to. Only capture normalBounds from a NORMAL
	// window: a maximized window's Bounds() is the full maximized rect, and
	// normalBounds already holds its pre-maximize floating size — overwriting it
	// here would lose that (and, with the state, its maximized-ness).
	w.stateBeforeMinimize = w.state
	if w.state == WindowStateNormal {
		w.normalBounds = w.Bounds()
	}
	w.state = WindowStateMinimized
	handler := w.onStateChange
	w.mu.Unlock()

	w.Update()

	if handler != nil {
		handler(WindowStateMinimized)
	}
}

// Restore restores the window from maximized or minimized state. A window
// minimized while maximized comes back MAXIMIZED (not normal); the window
// manager re-applies the client-area bounds for that case (RestoreWindow), since
// the window itself doesn't know the client area. Un-minimizing to normal (or
// un-maximizing) restores the saved floating bounds here.
func (w *Window) Restore() {
	w.mu.Lock()
	if w.state == WindowStateNormal {
		w.mu.Unlock()
		return
	}

	restoreTo := WindowStateNormal
	if w.state == WindowStateMinimized && w.stateBeforeMinimize == WindowStateMaximized {
		restoreTo = WindowStateMaximized
	}
	bounds := w.normalBounds
	w.state = restoreTo
	w.stateBeforeMinimize = WindowStateNormal
	w.pressedButton = TitleButtonNone // Reset pressed button state
	handler := w.onStateChange
	w.mu.Unlock()

	// Only the un-maximize/normal case restores the floating bounds; a
	// return-to-maximized is resized to the client area by the manager.
	if restoreTo == WindowStateNormal {
		w.SetBounds(bounds)
	} else {
		// The return-to-maximized path sets no bounds, so nothing else
		// here announces that the state (and with it the title buttons)
		// changed.
		w.Update()
	}

	if handler != nil {
		handler(restoreTo)
	}
}

// RestoreInPlace clears a maximized state WITHOUT moving the window: its
// current rect becomes its normal (floating) bounds. Restore() snaps back to
// the saved pre-maximize rect, which is the wrong answer once the geometry has
// already moved on — a torn/solo host whose OS window was edge-resized while
// the window still believed it was maximized. Left maximized, such a window
// paints the maximized frame (title bar only, no border stroke) around an
// arbitrary rect and offers a restore button that would teleport it; adopting
// the rect as normal makes it honest again, so the full frame and the maximize
// button come back. No-op unless the window is maximized.
func (w *Window) RestoreInPlace() {
	w.mu.Lock()
	if w.state != WindowStateMaximized {
		w.mu.Unlock()
		return
	}
	w.state = WindowStateNormal
	w.stateBeforeMinimize = WindowStateNormal
	w.normalBounds = w.Bounds()
	handler := w.onStateChange
	w.mu.Unlock()

	w.Update()

	if handler != nil {
		handler(WindowStateNormal)
	}
}

// keyboardTopSnapMaximize maximizes an in-surface window through its
// maximize-request handler when it is already pressed against the top of
// its client area - the keyboard equivalent of dragging the titlebar up
// into the menu bar. It returns true if it consumed the gesture by
// maximizing, false if the window should just move.
//
// Torn-off windows (which manage their own OS geometry via a bounds
// delegate), windows with no client area to snap into, and windows that
// cannot be maximized all fall through to a normal move.
func (w *Window) keyboardTopSnapMaximize(bounds core.UnitRect) bool {
	w.mu.RLock()
	getBounds := w.getConstrainingBounds
	delegate := w.onBoundsRequest
	maxHandler := w.onMaximizeRequest
	flags := w.flags
	w.mu.RUnlock()

	if delegate != nil || getBounds == nil || maxHandler == nil {
		return false
	}
	if !canMaximize(flags) || w.IsMaximized() {
		return false
	}
	if bounds.Y <= getBounds().Y {
		maxHandler()
		return true
	}
	return false
}

// unmaximizeInPlace leaves the maximized state without changing the
// window's on-screen bounds: the current (full-screen) bounds become the
// floating size. Used by the keyboard resize path so shrinking a
// maximized window snaps it off maximized and then continues resizing
// from the large size, rather than jumping back to the pre-maximize size
// the way Restore does.
func (w *Window) unmaximizeInPlace() {
	w.mu.Lock()
	if w.state != WindowStateMaximized {
		w.mu.Unlock()
		return
	}
	w.state = WindowStateNormal
	w.normalBounds = w.Bounds()
	handler := w.onStateChange
	w.mu.Unlock()

	if handler != nil {
		handler(WindowStateNormal)
	}
}

// IsMaximized returns true if the window is maximized.
func (w *Window) IsMaximized() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.state == WindowStateMaximized
}

// IsMinimized returns true if the window is minimized.
func (w *Window) IsMinimized() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.state == WindowStateMinimized
}

// IsActive returns true if this window is the active window in its container
// (WindowManager or MDIPane). This is separate from focus - a window is active
// when it's selected, even if a child trinket has keyboard focus.
func (w *Window) IsActive() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.isActive
}

// IsQuasiActive reports whether the window is quasi-active: lit but drawn
// with a single (heavy) border because OS focus lives elsewhere (the
// desktop menu bar) while this torn-off window remains the owner.
func (w *Window) IsQuasiActive() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.quasiActive
}

// SetQuasiActive sets the quasi-active state. A subsequent SetActive (in
// either direction) clears it, so callers set it only after the window has
// gone inactive on its own OS surface.
func (w *Window) SetQuasiActive(q bool) {
	w.mu.Lock()
	if w.quasiActive == q {
		w.mu.Unlock()
		return
	}
	w.quasiActive = q
	w.mu.Unlock()
	w.Update()
}

// renderActive reports whether the window should paint with active
// (as opposed to inactive) colors: either genuinely active or quasi-active.
func (w *Window) renderActive() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.isActive || w.quasiActive
}

// nearestAncestorWindow returns the closest Window enclosing this one (its
// MDI parent, or that parent's parent, and so on), or nil for a top-level
// window that isn't nested inside another window.
func (w *Window) nearestAncestorWindow() *Window {
	for p := w.Parent(); p != nil; p = p.Parent() {
		if win, ok := p.(*Window); ok {
			return win
		}
	}
	return nil
}

// isLit reports whether the window paints with a lit border - active,
// quasi-active, or passive (menu-remembered) - AND its whole ancestor
// lineage is lit. A nested MDI child is only lit while every window above
// it is lit, so a dimmed parent dims its children.
func (w *Window) isLit() bool {
	lit := w.renderActive()
	if !lit {
		if parent := w.Parent(); parent != nil {
			if provider, ok := parent.(core.PassiveWindowProvider); ok {
				lit = provider.IsWindowPassive(w)
			}
		}
	}
	if !lit {
		return false
	}
	if aw := w.nearestAncestorWindow(); aw != nil {
		return aw.isLit()
	}
	return true
}

// SetActive sets the window's active state. This is called by WindowManager
// or MDIPane when the window becomes the active (selected) window.
func (w *Window) SetActive(active bool) {
	w.mu.Lock()
	if w.isActive == active {
		w.mu.Unlock()
		return
	}
	w.isActive = active
	w.quasiActive = false
	handler := w.onActivate
	title := w.title
	w.mu.Unlock()

	// Announce window activation for accessibility
	if active {
		if am := core.FindAccessibilityManager(w); am != nil {
			am.AnnouncePolite(title + ", window")
		}
	}

	if handler != nil {
		handler(active)
	}
	w.Update()
}

// Close attempts to close the window, and reports whether it actually did. It
// is an ATTEMPT throughout: this window's close handler may decline, and so
// may any of its children, since a child left open over a closed parent is
// not a window anyone can get back to.
//
// When something declines, the window that did so is brought back to the
// user's attention along with every window between here and it, innermost
// last so it lands on top. A refusal the user cannot see is a window that
// simply will not close for no visible reason.
func (w *Window) Close() bool {
	ok, blocker := w.attemptClose()
	if !ok {
		w.surfaceBlockingChain(blocker)
	}
	return ok
}

// attemptClose is Close without the surfacing, so a nested child refusal
// surfaces ONCE from the outermost Close rather than once per level (which
// would raise the parents last and bury the window actually asking). It
// reports the innermost window that declined.
func (w *Window) attemptClose() (bool, *Window) {
	w.mu.RLock()
	handler := w.onClose
	closeComplete := w.onCloseComplete
	observers := append([]func(){}, w.onClosedObservers...)
	title := w.title
	w.mu.RUnlock()

	if handler != nil && !handler() {
		return false, w
	}

	// Close child windows first. A child that declines cancels this close
	// too: its own dialog is the one asking, and answering "don't close"
	// there cannot mean the window behind it goes anyway. Children that
	// already agreed stay closed, exactly as a refused application quit
	// leaves the windows that agreed to it closed.
	for _, child := range w.ChildWindows() {
		if ok, blocker := child.attemptClose(); !ok {
			return false, blocker
		}
	}

	// Announce window closing for accessibility. After the children, so a
	// close that a child cancels is never announced as having happened.
	if am := core.FindAccessibilityManager(w); am != nil {
		am.AnnouncePolite(title + ", closed")
	}

	// Remove from parent
	if parent := w.ParentWindow(); parent != nil {
		parent.removeChildWindow(w)
	}

	w.Hide()

	// Notify manager to remove this window
	if closeComplete != nil {
		closeComplete()
	}
	// Notify any additional observers (e.g. the owning Application, whose
	// removal must survive the manager/tear-off reassigning onCloseComplete).
	for _, fn := range observers {
		fn()
	}

	return true, nil
}

// windowSurfacer is the desktop, which is the only thing that knows where a
// window actually lives -- docked, minimized to the dock, or torn onto its own
// OS surface, possibly minimized there too. Declared here rather than imported
// so the dependency stays one-way.
type windowSurfacer interface {
	SurfaceWindow(win *Window)
}

// surfaceBlockingChain brings the window that refused back into view, together
// with every window between this one and it. Outermost first so the innermost
// -- the one actually asking the user something -- ends up on top.
func (w *Window) surfaceBlockingChain(blocker *Window) {
	if blocker == nil {
		return
	}
	surfacer := w.findSurfacer()
	if surfacer == nil {
		return
	}
	// Walk up from the blocker to here, then surface in reverse.
	chain := []*Window{blocker}
	for p := blocker.ParentWindow(); p != nil; p = p.ParentWindow() {
		chain = append(chain, p)
		if p == w {
			break
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		surfacer.SurfaceWindow(chain[i])
	}
}

// findSurfacer walks up for the desktop.
func (w *Window) findSurfacer() windowSurfacer {
	var current any = w.Parent()
	for current != nil {
		if s, ok := current.(windowSurfacer); ok {
			return s
		}
		t, ok := current.(core.Trinket)
		if !ok {
			return nil
		}
		current = t.Parent()
	}
	return nil
}

// SetOnClose sets the close handler.
func (w *Window) SetOnClose(handler func() bool) {
	w.mu.Lock()
	w.onClose = handler
	w.mu.Unlock()
}

// SetOnResize sets the resize handler.
func (w *Window) SetOnResize(handler func(width, height core.Unit)) {
	w.mu.Lock()
	w.onResize = handler
	w.mu.Unlock()
}

// SetOnMove sets the move handler.
func (w *Window) SetOnMove(handler func(x, y core.Unit)) {
	w.mu.Lock()
	w.onMove = handler
	w.mu.Unlock()
}

// SetOnActivate sets the activation handler.
func (w *Window) SetOnActivate(handler func(active bool)) {
	w.mu.Lock()
	w.onActivate = handler
	w.mu.Unlock()
}

// SetOnMinimizeRequest sets the minimize request handler.
// Called when the user clicks the minimize button. The handler should
// call WindowManager.MinimizeWindow() to properly minimize the window.
func (w *Window) SetOnMinimizeRequest(handler func()) {
	w.mu.Lock()
	w.onMinimizeRequest = handler
	w.mu.Unlock()
}

// SetOnMaximizeRequest sets the maximize/restore request handler.
// Called when the user clicks the maximize button or double-clicks titlebar.
// The handler should call WindowManager.MaximizeWindow() or RestoreWindow().
func (w *Window) SetOnMaximizeRequest(handler func()) {
	w.mu.Lock()
	w.onMaximizeRequest = handler
	w.mu.Unlock()
}

// SetOnTearRequest sets the handler for the tear-off handle: fired
// when the %/# handle is activated by click or keyboard. The host
// detaches the window (retaining position/size) or re-docks it.
func (w *Window) SetOnTearRequest(handler func()) {
	w.mu.Lock()
	w.onTearRequest = handler
	w.mu.Unlock()
}

// SetTearable enables the tear-off handle on the title bar.
func (w *Window) SetTearable(tearable bool) {
	w.mu.Lock()
	if tearable {
		w.flags |= WindowFlagTearable
	} else {
		w.flags &^= WindowFlagTearable
	}
	w.mu.Unlock()
	w.Update()
}

// IsTearable reports whether the tear-off handle is shown.
func (w *Window) IsTearable() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.flags&WindowFlagTearable != 0
}

// SetMainRequested records that this window wants to be its
// application's main window (wire `main` property). The host reads it
// when adopting the window.
func (w *Window) SetMainRequested(v bool) {
	w.mu.Lock()
	w.mainRequested = v
	w.mu.Unlock()
}

// MainRequested reports whether the window asked to be the app's main
// window.
func (w *Window) MainRequested() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.mainRequested
}

// SetDetached marks whether the window currently lives in its own
// torn-off surface (the handle then shows '#' and re-docks on click).
func (w *Window) SetDetached(detached bool) {
	w.mu.Lock()
	w.detached = detached
	w.mu.Unlock()
	w.Update()
}

// IsDetached reports whether the window is currently torn off.
func (w *Window) IsDetached() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.detached
}

// SetWindowMenuBar installs (or clears) the window's own menu bar,
// shown between the title bar and content while the window is detached.
// The desktop supplies it as a generic trinket. Passing nil removes it.
func (w *Window) SetWindowMenuBar(mb core.Trinket) {
	w.mu.Lock()
	w.menuBar = mb
	w.menuBarVisible = mb != nil
	w.mu.Unlock()
	if mb != nil {
		mb.SetParent(w)
		// A solo or torn-off window carries its own bar, so it forms its own
		// accelerators, against its own context. The desktop's bar does the
		// same for the desktop; neither has to know about the other.
		w.refreshKeyContext()
		// Tab out of this bar into the window's own focus chain. The desktop's
		// bar hands Tab to the dock; a window's bar has no dock beside it, so
		// without this the key fell through to the focused trinket and a
		// full-screen one swallowed it (see focusOutOfMenuBar).
		if fo, ok := mb.(interface{ SetOnFocusOut(func(bool) bool) }); ok {
			fo.SetOnFocusOut(w.focusOutOfMenuBar)
		}
	}
	w.layoutContent()
	w.Update()
}

// focusOutOfMenuBar moves focus off this window's own menu bar: Shift+Tab
// (forward=false) back to the title bar, Tab forward to the first content
// trinket. It mirrors where Tab lands when it walks off either end of the
// content chain, so the bar sits between the title bar and the content in one
// continuous cycle. Reports whether focus moved; a window with nothing
// focusable to move to leaves the key alone rather than eating it.
func (w *Window) focusOutOfMenuBar(forward bool) bool {
	fm := w.FocusManager()
	if !forward {
		// Backward into the title bar - the blur item when it is enabled,
		// matching Shift+Tab off the front of the content chain.
		if w.hasKeyboardBlurEnabled() {
			w.SetTitleFocus(TitleFocusBlur)
		} else {
			w.SetTitleFocus(TitleFocusTitle)
		}
		if fm != nil {
			fm.ClearFocus()
		}
		w.Update()
		return true
	}
	if fm == nil {
		return false
	}
	w.SetTitleFocus(TitleFocusNone)
	if !fm.FocusFirst() {
		return false
	}
	w.Update()
	return true
}

// WindowMenuBar returns the window's own menu bar (the chrome a detached
// main window hosts), or nil. Used by the desktop to route a torn-off
// child window's shortcuts through its app's menu bar.
func (w *Window) WindowMenuBar() core.Trinket {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.menuBar
}

// applicationQuitter is the desktop. Declared here rather than imported so
// the dependency stays one-way: a window knows nothing about applications,
// and the desktop, which owns them, answers the question.
type applicationQuitter interface {
	QuitApplicationOwning(win *Window) bool
}

// quitOwningApplication walks up for the desktop and asks it to end the
// application this window belongs to. The walk works for a torn-off window
// too: tearing removes it from the manager but leaves it parented to the
// desktop, which is exactly the link needed here.
func (w *Window) quitOwningApplication() bool {
	var current any = w.Parent()
	for current != nil {
		if q, ok := current.(applicationQuitter); ok {
			return q.QuitApplicationOwning(w)
		}
		t, ok := current.(core.Trinket)
		if !ok {
			return false
		}
		current = t.Parent()
	}
	return false
}

// SetShortcutResolver installs a fallback accelerator handler, consulted
// in HandleKeyPress after the window's own menu bar. The desktop uses it
// to give a torn-off child window access to its app's shortcuts.
func (w *Window) SetShortcutResolver(fn func(core.KeyPressEvent) bool) {
	w.mu.Lock()
	w.shortcutResolver = fn
	w.mu.Unlock()
}

// WindowStatusBar returns the window's own status bar chrome, or nil.
func (w *Window) WindowStatusBar() core.Trinket {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.statusBar
}

// BeginRawKeyInput arms the window to pass its next key straight to the
// focused trinket, bypassing this window's menu-bar shortcut handling.
// onDone runs after that key is consumed, so the caller can restore any
// prompt it showed. This is the detached-window path for the app's "raw
// key input" feature; on a docked window the desktop handles it instead.
func (w *Window) BeginRawKeyInput(onDone func()) {
	w.mu.Lock()
	w.passNextKeyRaw = true
	w.onRawKeyDone = onDone
	w.mu.Unlock()
}

// RawKeyInputPending reports whether this window is armed to pass its next
// key straight to the focused trinket.
//
// The desktop needs to ask. Some keys never reach a window at all — the
// window manager sends F10 to the desktop for the menu bar before any window
// sees it — so a door that claims a key has to check this one-shot on the way
// past, or it takes the key the mode exists to deliver.
func (w *Window) RawKeyInputPending() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.passNextKeyRaw
}

// SetWindowStatusBar installs (or clears) the window's own status bar,
// shown along the bottom edge while the window is detached.
func (w *Window) SetWindowStatusBar(sb core.Trinket) {
	w.mu.Lock()
	w.statusBar = sb
	w.statusBarVisible = sb != nil
	w.mu.Unlock()
	if sb != nil {
		sb.SetParent(w)
	}
	w.layoutContent()
	w.Update()
}

// SetMenuBarVisible / SetStatusBarVisible toggle the chrome rows.
func (w *Window) SetMenuBarVisible(v bool) {
	w.mu.Lock()
	w.menuBarVisible = v
	w.mu.Unlock()
	w.layoutContent()
	w.Update()
}

func (w *Window) SetStatusBarVisible(v bool) {
	w.mu.Lock()
	w.statusBarVisible = v
	w.mu.Unlock()
	w.layoutContent()
	w.Update()
}

// chromeHeights returns the vertical space the menu bar (top) and status
// bar (bottom) reserve inside a detached window; zero for both when the
// window is docked or the chrome is absent/hidden.
func (w *Window) chromeHeights() (menuTop, statusBottom core.Unit) {
	w.mu.RLock()
	detached := w.detached
	mb, sb := w.menuBar, w.statusBar
	mbVis, sbVis := w.menuBarVisible, w.statusBarVisible
	w.mu.RUnlock()
	if !detached {
		return 0, 0
	}
	outer := w.frameCellMetrics()
	interior := core.FindEffectiveCellMetrics(w.Self())
	if mb != nil && mbVis {
		// A bar that states its own row (core.MenuRowProvider) answers in ITS
		// denomination -- the interior one it paints through, see paintChrome
		// -- and its row is whatever core.MenuScale left it, which is not
		// always a whole cell. The reservation is in the frame's currency, so
		// the two exchange. Reserving a cell for a shortened bar leaves a dead
		// strip below it that the bar does not answer for.
		menuTop = outer.UnitsPerCellHeight
		if rp, ok := mb.(core.MenuRowProvider); ok {
			if h := rp.MenuRowHeight(); h > 0 {
				menuTop = core.ExchangeY(h, interior, outer)
			}
		}
	}
	if sb != nil && sbVis {
		statusBottom = outer.UnitsPerCellHeight
	}
	return
}

// menuBarRect / statusBarRect return the chrome rows in window-local
// coordinates (empty when that chrome isn't shown). Derived from the
// content bounds, which already reserve the chrome space.
func (w *Window) menuBarRect() core.UnitRect {
	top, _ := w.chromeHeights()
	if top == 0 {
		return core.UnitRect{}
	}
	cb := w.contentBounds()
	return core.UnitRect{X: cb.X, Y: cb.Y - top, Width: cb.Width, Height: top}
}

func (w *Window) statusBarRect() core.UnitRect {
	_, bottom := w.chromeHeights()
	if bottom == 0 {
		return core.UnitRect{}
	}
	cb := w.contentBounds()
	return core.UnitRect{X: cb.X, Y: cb.Y + cb.Height, Width: cb.Width, Height: bottom}
}

// SetTearHighlight toggles the tear-off halo shown while the tear
// handle is being pressed or dragged. The window manager sets it on
// mousedown/drag of the '%'/'#' handle and clears it on release.
func (w *Window) SetTearHighlight(on bool) {
	w.mu.Lock()
	changed := w.tearHighlight != on
	w.tearHighlight = on
	w.mu.Unlock()
	if changed {
		w.Update()
	}
}

// TearIndicatorActive reports whether the tear-off halo should be
// drawn: the handle is pressed/dragged, or the tear button holds
// keyboard focus. Only tearable windows ever qualify.
func (w *Window) TearIndicatorActive() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.flags&WindowFlagTearable == 0 {
		return false
	}
	return w.tearHighlight || w.titleFocus == TitleFocusTear
}

// tearHaloMargin is how far the black tear-off halo extends beyond the
// window frame, in units - a thin outline reading as a ~2px stroke.
const tearHaloMargin core.Unit = 2

// PaintTearHalo draws the black tear-off halo behind the window. p is
// the parent (desktop) painter and bounds is the window's rect in that
// space; the manager calls it just before painting the window so the
// halo shows only as a thin black outline. It is intentionally left
// unclipped to the client area, so a maximized window bleeds the stroke
// over the menu bar (top) and status bar (bottom). No-op on cell
// surfaces (the affordance is graphical only).
func (w *Window) PaintTearHalo(p *core.Painter, bounds core.UnitRect) {
	m := tearHaloMargin
	halo := core.UnitRect{
		X:      bounds.X - m,
		Y:      bounds.Y - m,
		Width:  bounds.Width + 2*m,
		Height: bounds.Height + 2*m,
	}
	radius := windowCornerRadius + m
	if w.IsMaximized() {
		radius = 0 // Square frame -> square halo.
	}
	black := style.DefaultStyle().WithFg(style.ColorBlack).WithBg(style.ColorBlack)
	p.DrawRoundedRect(halo, radius, style.BorderHeavy, black)
}

// requestTear fires the tear-off handle's activation callback.
func (w *Window) requestTear() {
	w.mu.RLock()
	handler := w.onTearRequest
	w.mu.RUnlock()
	if handler != nil {
		handler()
	}
}

// SetOnCloseComplete sets the callback for when the window is fully closed.
// This is called by WindowManager to remove the window from its list.
func (w *Window) SetOnCloseComplete(handler func()) {
	w.mu.Lock()
	w.onCloseComplete = handler
	w.mu.Unlock()
}

// AddOnClosed registers an additional observer fired when the window is
// closed. Unlike SetOnCloseComplete (a single slot the manager and tear-off
// host reassign), observers accumulate and always run - the owning
// Application uses one to drop the window from its list no matter which
// surface the window was living on.
func (w *Window) AddOnClosed(fn func()) {
	if fn == nil {
		return
	}
	w.mu.Lock()
	w.onClosedObservers = append(w.onClosedObservers, fn)
	w.mu.Unlock()
}

// SetGetConstrainingBounds sets the callback to get the client area for movement constraints.
// This is called during keyboard window movement to constrain the window position.
func (w *Window) SetGetConstrainingBounds(handler func() core.UnitRect) {
	w.mu.Lock()
	w.getConstrainingBounds = handler
	w.mu.Unlock()
}

// SetGetDisplayBounds installs the container's answer to "where is this
// window actually drawn". A WindowManager or MDIPane sets it to its own
// provisional corral, so a window left off-screen by a container shrink
// reports the nudged-into-view rectangle without its logical bounds moving.
//
// It exists because the corral has to be readable from OUTSIDE the container
// that computes it. The software paint loop asks the container directly, but a
// GPU compositor is handed the windows themselves and positions each one as a
// layer of its own -- and with no way to ask, it positioned from Bounds() and
// drew windows clean off the edge of a shrunk desktop, while hit-testing (which
// does go through the container) still corralled them. Draw and hit disagreed.
//
// Nil means "no container", which is the honest answer for a torn-off window
// managing its own OS geometry: DisplayBounds is then just Bounds.
func (w *Window) SetGetDisplayBounds(handler func() core.UnitRect) {
	w.mu.Lock()
	w.getDisplayBounds = handler
	w.mu.Unlock()
}

// DisplayBounds is where this window is DRAWN and hit-tested: its logical
// bounds corralled into its container's client area. It is the same rectangle
// the container's own paint loop uses, because it is the container that
// answers -- there is one corral, not two implementations of one.
//
// Equal to Bounds when the window fits, when nothing installed a delegate, and
// while maximized (a maximized window already tracks the client area).
func (w *Window) DisplayBounds() core.UnitRect {
	w.mu.RLock()
	getBounds := w.getDisplayBounds
	w.mu.RUnlock()
	if getBounds == nil {
		return w.Bounds()
	}
	return getBounds()
}

// SetPopupController sets the popup controller for this window.
// This is called by WindowManager when the window is added.
func (w *Window) SetPopupController(pc core.PopupController) {
	w.mu.Lock()
	w.popupController = pc
	w.mu.Unlock()
}

// PopupController returns the popup controller for this window.
// This implements the interface needed by trinkets like ComboBox.
func (w *Window) PopupController() core.PopupController {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.popupController
}

// RegisterPopup implements core.PopupController by delegating to the stored controller.
func (w *Window) RegisterPopup(request *core.PopupRequest) {
	w.mu.RLock()
	pc := w.popupController
	w.mu.RUnlock()
	if pc != nil {
		pc.RegisterPopup(request)
	}
}

// UnregisterPopup implements core.PopupController by delegating to the stored controller.
func (w *Window) UnregisterPopup(id string) {
	w.mu.RLock()
	pc := w.popupController
	w.mu.RUnlock()
	if pc != nil {
		pc.UnregisterPopup(id)
	}
}

// MapToScreen implements core.PopupController by delegating to the stored controller.
func (w *Window) MapToScreen(trinket core.Trinket, local core.UnitPoint) core.UnitPoint {
	w.mu.RLock()
	pc := w.popupController
	w.mu.RUnlock()
	if pc != nil {
		return pc.MapToScreen(trinket, local)
	}
	return local
}

// SetBorderStyle sets the border style.
func (w *Window) SetBorderStyle(border style.BorderStyle) {
	w.mu.Lock()
	w.borderStyle = border
	w.mu.Unlock()
	w.Update()
}

// SetTitleStyle sets the title bar style.
func (w *Window) SetTitleStyle(s style.CellStyle) {
	w.mu.Lock()
	w.titleStyle = s
	w.mu.Unlock()
	w.Update()
}

// SetFrameStyle sets the frame style.
func (w *Window) SetFrameStyle(s style.CellStyle) {
	w.mu.Lock()
	w.frameStyle = s
	w.mu.Unlock()
	w.Update()
}

// Font returns the window's font, or nil if inheriting from desktop.
func (w *Window) Font() *core.Font {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.font
}

// SetFont sets the window's font.
// Set to nil to inherit from the desktop/MDI pane.
func (w *Window) SetFont(f *core.Font) {
	w.mu.Lock()
	w.font = f
	w.mu.Unlock()
	w.Layout() // Recalculate layout since font affects trinket sizes
	w.Update()
}

// EffectiveFont returns the font to use for this window and its contents.
func (w *Window) EffectiveFont() *core.Font {
	w.mu.RLock()
	if w.font != nil {
		f := w.font
		w.mu.RUnlock()
		return f
	}
	w.mu.RUnlock()

	// Check parent's effective font (walks up the chain through MDI pane, desktop, etc.)
	if parent := w.Parent(); parent != nil {
		if trinket, ok := parent.(core.Trinket); ok {
			return core.FindEffectiveFont(trinket)
		}
	}

	return core.DefaultFont()
}

// BackgroundColor returns the window's explicit background color, if set.
func (w *Window) BackgroundColor() *style.Color {
	return w.TrinketBase.BackgroundColor()
}

// SchemeBackgroundColor returns the window's scheme-derived background color.
// This is the color the window paints its content area with, based on its scheme.
func (w *Window) SchemeBackgroundColor() *style.Color {
	scheme := w.GetScheme()
	bgColor := scheme.GetWindowBG(w.renderActive())
	return &bgColor
}

// frameCellMetrics is the denomination the window's chrome (title bar,
// borders, buttons) is drawn and hit-tested in. The frame paints with
// the surface/container metrics (Painter.Metrics), and the window's
// bounds live in the container's coordinate space - NOT the window's own
// content denomination - so a per-window denomination override must not
// change chrome geometry (layout stays invariant under re-denomination).
// Falls back to the default when the window has no container yet.
func (w *Window) frameCellMetrics() core.CellMetrics {
	if p := w.Parent(); p != nil {
		return core.FindEffectiveCellMetrics(p)
	}
	return core.DefaultCellMetrics()
}

// titleBarMetrics resolves this window's title-bar kit metrics, in the
// FRAME denomination its chrome lays out in (the title bar sits above the
// content area and is never sized in the interior denomination).
func (w *Window) titleBarMetrics() TitleBarMetrics {
	return TitleBarMetricsFor(w.frameCellMetrics(), w.EffectiveFont(), core.FindGraphicalFrames(w))
}

// frameBorder is the reserved frame border in the FRAME denomination, one
// count per axis. The desktop reports the border in its own units; the two
// are the same number only for a window the desktop itself holds.
func (w *Window) frameBorder() (x, y core.Unit) {
	return core.FindFrameBorderUnitsIn(w, w.frameCellMetrics())
}

// contentBounds returns the bounds for the content area. When the window
// is detached and carries its own chrome, the menu bar (top) and status
// bar (bottom) rows are reserved out of it (see reserveChrome).
func (w *Window) contentBounds() core.UnitRect {
	bounds := w.Bounds()
	metrics := w.frameCellMetrics()

	w.mu.RLock()
	state := w.state
	flags := w.flags
	w.mu.RUnlock()

	var cb core.UnitRect
	switch {
	case state == WindowStateMaximized:
		// Maximized: flush to the edges with no side borders. The top title row
		// is reserved only when the window actually has a title bar in this state
		// - a NoTitle, Frameless, or (while maximized) NoTitleWhenMaximized window
		// fills the whole surface (being maximized is independent of having a
		// title bar or a frame).
		top := core.Unit(0)
		if hasTitleBar(flags, state) {
			// The (possibly scaled) title row height, from the kit —
			// UnitsPerCellHeight at scale 1.0 and on every cell surface.
			top = w.titleBarMetrics().RowH
		}
		cb = core.UnitRect{X: 0, Y: top, Width: bounds.Width, Height: bounds.Height - top}
	case flags&WindowFlagFrameless != 0:
		cb = core.UnitRect{Width: bounds.Width, Height: bounds.Height}
	case core.FindGraphicalFrames(w):
		// Graphical frames: the frame border rests OUTSIDE the content
		// coordinate system, reserving its own width on every edge, and the
		// titlebar sits inside the top border. So the top reserves the
		// border AND the titlebar row; the sides and bottom reserve just
		// the border. A thicker border shrinks the interior rather than
		// overlapping it.
		bx, by := w.frameBorder()
		top := by + w.titleBarMetrics().RowH
		if flags&WindowFlagNoTitle != 0 {
			top = by
		}
		cb = core.UnitRect{X: bx, Y: top, Width: bounds.Width - 2*bx, Height: bounds.Height - top - by}
	default:
		// Cell frames: the border occupies a full cell on every side.
		left, top, right, bottom := metrics.UnitsPerCellWidth, metrics.UnitsPerCellHeight, metrics.UnitsPerCellWidth, metrics.UnitsPerCellHeight
		cb = core.UnitRect{X: left, Y: top, Width: bounds.Width - left - right, Height: bounds.Height - top - bottom}
	}

	return clampClientArea(w.reserveChrome(cb))
}

// reserveChrome removes the detached window's menu bar (top) and status
// bar (bottom) rows from a content rect.
func (w *Window) reserveChrome(cb core.UnitRect) core.UnitRect {
	top, bottom := w.chromeHeights()
	if top == 0 && bottom == 0 {
		return cb
	}
	cb.Y += top
	cb.Height -= top + bottom
	if cb.Height < 0 {
		cb.Height = 0
	}
	return cb
}

// clampClientArea guarantees the client area is never empty: a window
// squeezed below its chrome still exposes a 1-unit sliver so content
// paints (clipped) instead of spilling unclipped.
func clampClientArea(r core.UnitRect) core.UnitRect {
	if r.Width < 1 {
		r.Width = 1
	}
	if r.Height < 1 {
		r.Height = 1
	}
	return r
}

// ClientAreaOffset returns the offset from the window's top-left corner
// to the client (content) area. This accounts for title bar and frame.
func (w *Window) ClientAreaOffset() core.UnitPoint {
	cb := w.contentBounds()
	return core.UnitPoint{X: cb.X, Y: cb.Y}
}

// ContentBounds returns the window-local rectangle available to the content
// trinket, inside the title bar and frame (and any detached chrome). Callers
// that size a window to fit its content use it to learn how much room the
// chrome takes: chrome = window bounds minus ContentBounds.
func (w *Window) ContentBounds() core.UnitRect {
	return w.contentBounds()
}

// ClientArea reports the space a dropdown from the window's own (detached)
// menu bar may occupy, expressed in that menu bar's local coordinate
// space (its origin sits at the menu bar's top-left, since the dropdown
// paints offset by menuBarRect). The menu reads Y+Height as the bottom
// limit, so it clamps to the window's surface and shows scroll bumpers
// instead of overflowing. Mirrors the desktop's ClientArea contract so
// the same menu-bar height logic works on a torn window.
func (w *Window) ClientArea() core.UnitRect {
	b := w.Bounds()
	mbr := w.menuBarRect()
	top := w.frameCellMetrics().UnitsPerCellHeight
	// Bottom edge of the surface in menu-bar-local coordinates.
	bottom := b.Height - mbr.Y
	if bottom < top {
		bottom = top
	}
	// Bounds and the chrome rect are in the window's OUTER currency; the bar
	// and its dropdown work in the interior one, which is what "that menu
	// bar's local coordinate space" above means. Handing the outer number
	// over unconverted let the dropdown divide a height in one currency by a
	// row height in another, so it decided how many items fit -- and whether
	// to scroll at all -- from a row count that was out by the ratio between
	// them.
	outer, interior := w.denominations()
	return core.UnitRect{
		Y:      core.ExchangeY(top, outer, interior),
		Height: core.ExchangeY(bottom-top, outer, interior),
	}
}

// denominations returns the grid-metrics currency of the window's own
// coordinate space (outer: the parent's, in which bounds and chrome
// live) and of its content area (interior: honoring a per-window
// override). Equal unless an override is set on this window.
func (w *Window) denominations() (outer, interior core.CellMetrics) {
	interior = w.EffectiveCellMetrics()
	if w.CellMetricsOverride() == nil {
		return interior, interior
	}
	return core.ParentCellMetrics(w.Self()), interior
}

// layoutContent lays out the content trinket.
func (w *Window) layoutContent() {
	w.mu.RLock()
	content := w.content
	layout := w.layout
	w.mu.RUnlock()

	if content == nil {
		return
	}

	contentRect := w.contentBounds()

	// Content bounds should be relative to the content area (0,0), not the window.
	// The window's Paint method handles the offset translation.
	// The content area is denominated in the window's interior currency:
	// the same physical area, re-expressed in interior units.
	outer, interior := w.denominations()
	localContentRect := core.UnitRect{
		X:      0,
		Y:      0,
		Width:  core.ExchangeX(contentRect.Width, outer, interior),
		Height: core.ExchangeY(contentRect.Height, outer, interior),
	}

	if layout != nil {
		layout.Layout(w, localContentRect)
	} else {
		content.SetBounds(localContentRect)
	}
}

// Children implements core.Container.
func (w *Window) Children() []core.Trinket {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.content == nil {
		return nil
	}
	return []core.Trinket{w.content}
}

// AddChild implements core.Container.
func (w *Window) AddChild(child core.Trinket) {
	w.SetContent(child)
}

// RemoveChild implements core.Container.
func (w *Window) RemoveChild(child core.Trinket) {
	w.mu.Lock()
	if w.content == child {
		w.content = nil
	}
	w.mu.Unlock()
}

// ChildAt implements core.Container.
func (w *Window) ChildAt(pos core.UnitPoint) core.Trinket {
	w.mu.RLock()
	content := w.content
	w.mu.RUnlock()

	if content == nil {
		return nil
	}

	contentRect := w.contentBounds()
	outer, interior := w.denominations()
	localPos := core.UnitPoint{
		X: core.ExchangeX(pos.X-contentRect.X, outer, interior),
		Y: core.ExchangeY(pos.Y-contentRect.Y, outer, interior),
	}

	if content.Bounds().Contains(localPos) {
		return content
	}
	return nil
}

// Layout implements core.Container.
func (w *Window) Layout() {
	w.layoutContent()

	// Force content to re-layout with fresh SizeHints.
	// This is important when parent chain changes (e.g., window added to MDIPane)
	// since EffectiveFont may now return a different font.
	w.mu.RLock()
	content := w.content
	w.mu.RUnlock()

	if content != nil {
		if container, ok := content.(core.Container); ok {
			container.Layout()
		}
	}
}

// Paint renders the window.
func (w *Window) Paint(p *core.Painter) {
	w.mu.RLock()
	flags := w.flags
	state := w.state
	title := w.title
	border := w.borderStyle
	content := w.content
	isActive := w.isActive
	quasiActive := w.quasiActive
	w.mu.RUnlock()

	bounds := w.Bounds()
	metrics := p.Metrics()
	scheme := w.GetScheme()

	// Window appears focused if it's the active window in its container.
	// For MDI children (parent is MDIPane with StrongFocus): also require parent to have focus,
	// so MDI windows only appear focused when their tab is active.
	// For top-level windows (parent is Desktop with NoFocus): don't check parent focus.
	focused := isActive
	if focused {
		if parent := w.Parent(); parent != nil {
			policy := parent.FocusPolicy()
			if policy == core.StrongFocus || policy == core.TabFocus {
				// MDI-style container: check if parent has focus OR this window has internal focus.
				// When clicking on a trinket inside the window, focus goes to that trinket (not parent).
				if !parent.HasFocus() {
					windowHasInternalFocus := false
					if fm := w.FocusManager(); fm != nil {
						if focusedTrinket := fm.FocusedTrinket(); focusedTrinket != nil {
							windowHasInternalFocus = focusedTrinket.HasFocus()
						}
					}
					focused = windowHasInternalFocus
				}
			}
		}
	}

	// Check for passive state: window is remembered by menu bar while no
	// window is active, OR the window is a quasi-active torn window (lit but
	// single-bordered because OS focus lives on the desktop menu bar). Both
	// render with active colors and a heavy (single) border.
	isPassive := quasiActive
	if parent := w.Parent(); parent != nil {
		if provider, ok := parent.(core.PassiveWindowProvider); ok {
			if provider.IsWindowPassive(w) {
				isPassive = true
			}
		}
	}

	// An MDI child only lights up while its ancestor window lineage is lit.
	// If a containing window has gone inactive (another top-level window took
	// focus), the child follows it to the inactive style regardless of its
	// own internal focus. Top-level windows have no ancestor and are exempt.
	if aw := w.nearestAncestorWindow(); aw != nil && !aw.isLit() {
		focused = false
		isPassive = false
	}

	// Get styles from scheme based on focus state
	// Passive windows use active colors (same as focused)
	titleStyle := scheme.GetWindowTitle(focused || isPassive)
	frameStyle := scheme.GetWindowBorder(focused || isPassive)

	// Passive windows use heavy (thick single-line) border instead of double
	frameBorder := border
	if isPassive {
		frameBorder = style.BorderHeavy
	}

	// Draw frame based on state
	if state == WindowStateMaximized {
		// Maximized: no side borders. Draw the top title bar only when the
		// window has one in this state; a NoTitle, Frameless, or (while
		// maximized) NoTitleWhenMaximized window has no frame at all (no title,
		// no border).
		if hasTitleBar(flags, state) {
			w.paintMaximizedFrame(p, bounds, metrics, title, titleStyle, frameStyle, frameBorder)
		}
	} else if flags&WindowFlagFrameless == 0 {
		// Normal frame (a restored NoTitleWhenMaximized window lands here and
		// regains its full title bar and border).
		w.paintNormalFrame(p, bounds, metrics, title, titleStyle, frameStyle, frameBorder, flags)
	}

	// Paint content (in the window's interior denomination)
	outer, interior := w.denominations()
	localBounds := core.UnitRect{Width: bounds.Width, Height: bounds.Height}
	graphicalFrame := state != WindowStateMaximized && flags&WindowFlagFrameless == 0 &&
		core.FindGraphicalFrames(w)
	// ONE rounded clip region for everything the window draws inside its
	// own outline — content, MDI children, and its own chrome. Each of
	// those reaches the window's edges, and a square rectangle at a
	// rounded corner spills into the curve: the status bar squared off
	// the bottom corners and ate the frame there, while the content
	// (which had this clip already) did not. Sharing one region rather
	// than building three is also the cheap way to do it — the mask is
	// set up once per window paint.
	inside := p
	if graphicalFrame {
		inside = p.WithRoundedClipRegion(localBounds, windowCornerRadius)
	}

	if content != nil {
		contentBounds := w.contentBounds()
		contentPainter := inside.WithOffset(contentBounds.X, contentBounds.Y).
			WithClip(core.UnitRect{Width: contentBounds.Width, Height: contentBounds.Height}).
			WithDenomination(outer, interior)
		content.Paint(contentPainter)
	}

	// Paint child windows (within the content area, clipped)
	if len(w.ChildWindows()) > 0 {
		contentBounds := w.contentBounds()
		// Create a painter clipped to the content area
		contentPainter := inside.WithOffset(contentBounds.X, contentBounds.Y).
			WithClip(core.UnitRect{Width: contentBounds.Width, Height: contentBounds.Height}).
			WithDenomination(outer, interior)

		for _, child := range w.ChildWindows() {
			if child.IsVisible() && !child.IsMinimized() {
				childBounds := child.Bounds()
				childPainter := contentPainter.WithOffset(childBounds.X, childBounds.Y)
				child.Paint(childPainter)
			}
		}
	}

	// Detached-window chrome: the menu bar (between title and content)
	// and status bar (bottom edge), then the menu bar's dropdown on top.
	w.paintChrome(inside, outer, interior)

	// The frame goes on LAST, over everything inside it — the same order
	// the desktop paints its own chrome and then its frame, which is why
	// that one's corners survive. Content and chrome reach the window's
	// edges, so the border is re-stroked here rather than before them.
	if graphicalFrame {
		frameStyle := w.GetScheme().GetWindowBorder(focused || isPassive)
		if frameBorder == style.BorderHeavy {
			// Single border: the outer band disappears into the window
			// background, then a thin inner line in the active border color
			// sits just inside it.
			bg := w.GetScheme().GetWindowBG(w.renderActive())
			p.StrokeRoundedRect(localBounds, windowCornerRadius, frameBorder, frameStyle.WithFg(bg))
			w.paintSingleBorderInner(p, localBounds, w.GetScheme().GetWindowBorder(true))
		} else {
			p.StrokeRoundedRect(localBounds, windowCornerRadius, frameBorder, frameStyle)
			// Every border type closes on the same inner line, so the
			// innermost band is never left to whatever happens to be under
			// it. Only the SINGLE border makes that line a statement of its
			// own (its outer band is painted out, so this is the frame the
			// eye reads); the others draw it in their own border color, as
			// the inner edge of the stroke they just laid down.
			w.paintSingleBorderInner(p, localBounds, frameStyle)
		}
	}

	// Resize-edge cue: translucent white bands along the size-sensitive
	// edge(s) the host has lit, clipped to the frame's rounded corners.
	w.paintResizeBands(p, localBounds)
}

// ResizeBandAlpha is the opacity of the resize-edge cue, shared with the
// desktop's own edge bands so the two read as one system.
const ResizeBandAlpha = 0.25

// SetResizeBandRects sets the window-local rectangles the cue fills
// (empty clears it). Returns
// true when the set changed, so the caller can repaint only on change.
//
// Prefer SetResizeBandEdges: rectangles fixed here go stale the moment the
// window resizes under the gesture that is drawing them.
func (w *Window) SetResizeBandRects(rects []core.UnitRect) bool {
	w.mu.Lock()
	if sameRects(w.resizeBandRects, rects) {
		w.mu.Unlock()
		return false
	}
	w.resizeBandRects = rects
	w.mu.Unlock()

	// This changes what the window paints, and unlike most such setters
	// it reports the change to its caller instead of announcing it —
	// callers pair it with the manager's RequestRepaint, which says "a
	// frame is needed" without saying whose pixels went stale. The GPU
	// compositor caches a texture per window and needs to be told.
	w.NoteSubtreeRepaint()
	return true
}

// SetResizeBandEdges sets the highlight as an edge MASK plus the band
// thickness, to be resolved against the window's bounds at paint time. Zero
// edges clears it. Returns true when the state changed.
//
// This is the form that survives a live resize: the OS reports a new size
// back asynchronously, so any rectangle computed while the pointer moves is
// built from the PREVIOUS bounds — which is how a growing window ends up with
// its bands stranded in the middle of the frame.
func (w *Window) SetResizeBandEdges(edges int, band EdgeThickness) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.resizeBandEdges == edges && w.resizeBandThickness == band {
		return false
	}
	w.resizeBandEdges, w.resizeBandThickness = edges, band
	return true
}

// ResizeBandRects returns the window-local resize-edge highlight
// rectangles currently set (nil when the overlay is off).
func (w *Window) ResizeBandRects() []core.UnitRect {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.resizeBandRects
}

func sameRects(a, b []core.UnitRect) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// CursorShapeAt returns the mouse cursor requested by the trinket under
// the given window-local point (e.g. a text field's I-beam), or the
// default arrow when the point is outside the content area or over a
// trinket with no preference.
func (w *Window) CursorShapeAt(localX, localY core.Unit) core.CursorShape {
	w.mu.RLock()
	content := w.content
	w.mu.RUnlock()
	if content == nil {
		return core.CursorDefault
	}
	cb := w.contentBounds()
	if localX < cb.X || localY < cb.Y || localX >= cb.X+cb.Width || localY >= cb.Y+cb.Height {
		// Title bar, borders, or detached chrome: ordinary arrow.
		return core.CursorDefault
	}
	outer, interior := w.denominations()
	cx := core.ExchangeX(localX-cb.X, outer, interior)
	cy := core.ExchangeY(localY-cb.Y, outer, interior)
	return cursorShapeAtTrinket(content, core.UnitPoint{X: cx, Y: cy})
}

// cursorShapeAtTrinket descends to the deepest trinket containing pos and
// returns its requested cursor, or the default when none applies. The
// per-container coordinate transform must match the mouse-event descent
// (each container's HandleMouseMove), or the cursor region drifts from
// where clicks land - notably a scroll container positions its content
// offset by the scroll amount, which the event path adds and this must
// too, otherwise the I-beam region slides as the view scrolls.
func cursorShapeAtTrinket(trinket core.Trinket, pos core.UnitPoint) core.CursorShape {
	cur := trinket
	p := pos
	for {
		// A container that routes events through a transform the generic
		// descent can't reproduce (a nested window's chrome + denomination,
		// an MDI pane's window placement) answers for its whole subtree.
		// Skip the entry CONTAINER so a window delegating into its own content
		// does not recurse forever; a leaf entry (e.g. a terminal that wants a
		// different cursor over its scrollbar) is safe to consult directly.
		if cs, ok := cur.(core.CursorShaper); ok {
			_, isContainer := cur.(core.Container)
			// A container that declares it answers for its whole subtree
			// (core.CursorShapesSubtree) never re-enters this descent, so the
			// entry-skip — which exists only to stop a delegating window
			// recursing forever — must not silence it.
			_, ownsSubtree := cur.(core.CursorShapesSubtree)
			if cur != trinket || !isContainer || ownsSubtree {
				return cs.CursorShapeAt(p.X, p.Y)
			}
		}
		c, ok := cur.(core.Container)
		if !ok {
			break
		}
		child := c.ChildAt(p)
		if child == nil || child == cur {
			break
		}
		if sp, ok := cur.(core.ScrollOffsetUnitsProvider); ok {
			// Mirror ScrollArea.HandleMouseMove: content coordinate is the
			// viewport coordinate plus the scroll offset (content sits at
			// the scroll origin, not at its Bounds()).
			ox, oy := sp.ScrollOffsetUnits()
			p = core.UnitPoint{X: p.X + ox, Y: p.Y + oy}
		} else {
			cb := child.Bounds()
			p = core.UnitPoint{X: p.X - cb.X, Y: p.Y - cb.Y}
		}
		cur = child
	}
	if cp, ok := cur.(core.CursorProvider); ok {
		return cp.CursorShape()
	}
	return core.CursorDefault
}

// PaintModalDim darkens the whole window - content, titlebar, and border -
// with a translucent black fill, clipped to the frame's rounded corners (a
// plain rectangle when maximized or frameless). Called by the window manager
// for a window suppressed by the modal stack. Graphical path only: on cell
// surfaces FillRectPixelsAlpha no-ops.
func (w *Window) PaintModalDim(p *core.Painter, localBounds core.UnitRect) {
	rp := p
	if !w.IsMaximized() && w.Flags()&WindowFlagFrameless == 0 {
		rp = p.WithRoundedClipRegion(localBounds, windowCornerRadius)
	}
	rp.FillRectPixelsAlpha(0, 0, 0, 0,
		p.UnitSpanPxX(0, localBounds.Width), p.UnitSpanPxY(0, localBounds.Height),
		0, 0, 0, modalDimAlpha)
}

// paintResizeBands fills the lit resize edges with a translucent white
// band, clipped to the window's rounded corner radius. No-op on cell
// surfaces (FillRectPixelsAlpha returns false there).
func (w *Window) paintResizeBands(p *core.Painter, localBounds core.UnitRect) {
	rects := w.resizeBands(localBounds)
	if len(rects) == 0 {
		return
	}
	rp := p.WithRoundedClipRegion(localBounds, windowCornerRadius)
	for _, r := range rects {
		// Size the fill by the cell-snapped SPAN between the rect's edges,
		// not round(width*ppu): the fill is anchored at the snapped pixel of
		// (r.X, r.Y), so a raw width can leave the far end short of the
		// snapped opposite edge. UnitSpanPxX/Y snap both ends to the grid
		// the geometry paints on, so the band reaches exactly the far edge.
		rp.FillRectPixelsAlpha(r.X, r.Y, 0, 0,
			p.UnitSpanPxX(r.X, r.X+r.Width), p.UnitSpanPxY(r.Y, r.Y+r.Height),
			255, 255, 255, ResizeBandAlpha)
	}
}

// NoteSubtreeRepaint implements core.SubtreeRepaintTracker: something
// in this window (or the window itself) asked to be repainted.
func (w *Window) NoteSubtreeRepaint() { w.repaintRev.Add(1) }

// SubtreeRepaintRevision implements core.SubtreeRepaintTracker. Only
// equality between two reads means anything; the value does not.
func (w *Window) SubtreeRepaintRevision() uint64 { return w.repaintRev.Load() }

// SetMenuDropdownComposited tells the window that its host draws the
// menu bar's open dropdown as a compositor layer of its own (so it can
// carry a drop shadow). paintChrome then leaves the dropdown out of the
// window's own surface instead of painting it twice.
func (w *Window) SetMenuDropdownComposited(on bool) {
	w.mu.Lock()
	w.menuDropdownComposited = on
	w.mu.Unlock()
}

// menuBarDropdown returns the menu bar together with the exchange from
// its interior denomination to window-local units, or ok=false when
// there is no bar or no open menu.
type menuBarDropdown interface {
	PaintDropdown(*core.Painter)
	ActiveMenuBounds() core.UnitRect
	ActiveMenuTitleBounds() core.UnitRect
}

// MenuDropdownLayer returns the open menu bar dropdown as a compositor
// layer: its bounds and the bounds of the title it drops from, both in
// WINDOW-local units, plus a paint function that draws it through a
// painter at the window's origin. ok is false when nothing is open.
//
// A host that draws this layer itself must also call
// SetMenuDropdownComposited, or the dropdown paints twice.
func (w *Window) MenuDropdownLayer() (bounds, anchor core.UnitRect, paint func(*core.Painter), ok bool) {
	w.mu.RLock()
	mb := w.menuBar
	w.mu.RUnlock()

	dd, isDropdown := mb.(menuBarDropdown)
	barRect := w.menuBarRect()
	if !isDropdown || barRect.IsEmpty() {
		return core.UnitRect{}, core.UnitRect{}, nil, false
	}
	menuBounds := dd.ActiveMenuBounds()
	if menuBounds.IsEmpty() {
		return core.UnitRect{}, core.UnitRect{}, nil, false
	}

	// The bar paints in the window's INTERIOR denomination, offset to the
	// bar's row; the compositor works in window-local units, so exchange
	// back out and translate.
	outer, interior := w.denominations()
	toLocal := func(r core.UnitRect) core.UnitRect {
		if r.IsEmpty() {
			return core.UnitRect{}
		}
		return core.UnitRect{
			X:      barRect.X + core.ExchangeX(r.X, interior, outer),
			Y:      barRect.Y + core.ExchangeY(r.Y, interior, outer),
			Width:  core.ExchangeX(r.Width, interior, outer),
			Height: core.ExchangeY(r.Height, interior, outer),
		}
	}

	paint = func(p *core.Painter) {
		dd.PaintDropdown(p.WithOffset(barRect.X, barRect.Y).WithDenomination(outer, interior))
	}
	return toLocal(menuBounds), toLocal(dd.ActiveMenuTitleBounds()), paint, true
}

// resizeBands is the highlight resolved for a given window-local
// bounds. An edge MASK is resolved here, against the bounds the caller is
// actually painting — that is the whole point of storing a mask: a rectangle
// fixed earlier carries the size the window had when the gesture began, and a
// live resize is precisely when that stops being true. Explicit rectangles
// (the older setter) are returned as they were given.
func (w *Window) resizeBands(localBounds core.UnitRect) []core.UnitRect {
	w.mu.RLock()
	rects := w.resizeBandRects
	edges, band := w.resizeBandEdges, w.resizeBandThickness
	w.mu.RUnlock()
	if edges != 0 {
		return tornEdgeRects(localBounds, edges, band)
	}
	return rects
}

// inInterior restates a chrome rect's size in the interior denomination.
//
// The rect comes from the window's own geometry, which is in outer units,
// and the bar that receives it paints through WithDenomination(outer,
// interior) -- so handing the size over unconverted gives the bar a width
// in one currency and a painter in another. A bar told it was 400 units
// wide at an interior denomination of 16 against an outer 8 painted its
// background across half the window.
//
// Only the size crosses: the position is applied by WithOffset in outer
// units, before the denomination changes.
func inInterior(r core.UnitRect, outer, interior core.CellMetrics) core.UnitRect {
	return core.UnitRect{
		Width:  core.ExchangeX(r.Width, outer, interior),
		Height: core.ExchangeY(r.Height, outer, interior),
	}
}

// chromeLocal converts a window-local mouse position into the coordinates
// a chrome trinket in rect r actually works in: past the rect's origin,
// then out of the outer denomination and into the interior one.
//
// It is the mirror of what paintChrome does -- WithOffset in outer units,
// then WithDenomination -- and both halves are needed. Subtracting the
// origin alone hands the bar a position in the window's currency while its
// own geometry is in the interior's, so a click lands on whichever item
// happens to sit at the same NUMBER in the wrong currency. The content path
// has always exchanged; the chrome path did not.
func chromeLocal(x, y core.Unit, r core.UnitRect, outer, interior core.CellMetrics) (core.Unit, core.Unit) {
	return core.ExchangeX(x-r.X, outer, interior),
		core.ExchangeY(y-r.Y, outer, interior)
}

// paintChrome paints the detached window's menu bar and status bar in
// their reserved rows, and the menu bar's dropdown on top of content.
func (w *Window) paintChrome(p *core.Painter, outer, interior core.CellMetrics) {
	w.mu.RLock()
	mb, sb := w.menuBar, w.statusBar
	dropdownComposited := w.menuDropdownComposited
	w.mu.RUnlock()

	if r := w.menuBarRect(); mb != nil && !r.IsEmpty() {
		mb.SetBounds(inInterior(r, outer, interior))
		mp := p.WithOffset(r.X, r.Y).
			WithClip(core.UnitRect{Width: r.Width, Height: r.Height}).
			WithDenomination(outer, interior)
		mb.Paint(mp)
	}
	if r := w.statusBarRect(); sb != nil && !r.IsEmpty() {
		sb.SetBounds(inInterior(r, outer, interior))
		sp := p.WithOffset(r.X, r.Y).
			WithClip(core.UnitRect{Width: r.Width, Height: r.Height}).
			WithDenomination(outer, interior)
		sb.Paint(sp)
	}
	// The menu bar's dropdown paints last, unclipped, so it overlays the
	// window content below the bar — unless the host lifted it onto a
	// compositor layer of its own (see MenuDropdownLayer).
	if r := w.menuBarRect(); mb != nil && !r.IsEmpty() && !dropdownComposited {
		if dp, ok := mb.(interface{ PaintDropdown(*core.Painter) }); ok {
			dp.PaintDropdown(p.WithOffset(r.X, r.Y).WithDenomination(outer, interior))
		}
	}
}

// chromeMouseTarget returns the chrome trinket that should receive a
// mouse event at window-local (x, y), its rect, and true when the chrome
// owns the event. An open menu owns all mouse input; otherwise the menu
// bar / status bar own their own rows.
func (w *Window) chromeMouseTarget(x, y core.Unit) (core.Trinket, core.UnitRect, bool) {
	w.mu.RLock()
	mb, sb := w.menuBar, w.statusBar
	w.mu.RUnlock()

	if mb != nil {
		if o, ok := mb.(interface{ IsMenuOpen() bool }); ok && o.IsMenuOpen() {
			return mb, w.menuBarRect(), true
		}
		if r := w.menuBarRect(); !r.IsEmpty() && r.Contains(core.UnitPoint{X: x, Y: y}) {
			return mb, r, true
		}
	}
	if sb != nil {
		if r := w.statusBarRect(); !r.IsEmpty() && r.Contains(core.UnitPoint{X: x, Y: y}) {
			return sb, r, true
		}
	}
	return nil, core.UnitRect{}, false
}

// TitleControlsInsetProvider is an optional container capability: how far
// the container's OWN title-bar controls sit from the origin of the area
// it hands its children. A maximized child has no border of its own to
// indent by, so its controls would start flush at that origin and sit one
// cell left of the host's — visibly out of line on a themed desktop or an
// unmaximized parent window. Asking the host closes the gap, and composes
// through nesting: a host whose own controls are flush (it is itself
// maximized or zoomed, or it draws no title bar at all) answers 0, and
// the child stays flush too, which is already aligned.
//
// Graphical frames only — see maximizedControlInset.
type TitleControlsInsetProvider interface {
	TitleControlsInset() core.Unit
}

// TitleControlsInset implements TitleControlsInsetProvider for a window
// hosting MDI children: the offset from the content area it gives them to
// where its own controls are drawn.
func (w *Window) TitleControlsInset() core.Unit {
	if !core.FindGraphicalFrames(w) || !hasTitleBar(w.Flags(), w.State()) {
		return 0
	}
	if w.State() == WindowStateMaximized {
		// Flush to its own content origin, plus whatever ITS host asked
		// for — so the alignment carries down a nested stack.
		return w.maximizedControlInset()
	}
	// The normal frame draws its controls one cell in from the
	// border-inset origin, and the content area starts at that same
	// border: one cell apart.
	return w.titleBarMetrics().CellW
}

// maximizedControlInset is where a MAXIMIZED window starts its title-bar
// controls: flush at its own origin, unless its host draws controls
// further in (a themed desktop, an unmaximized parent window), in which
// case it matches them. Always 0 on a cell surface — a terminal's chrome
// has no border to align across, and the TUI stays exactly as it was.
func (w *Window) maximizedControlInset() core.Unit {
	if !core.FindGraphicalFrames(w) {
		return 0
	}
	if host, ok := w.Parent().(TitleControlsInsetProvider); ok && host != nil {
		return host.TitleControlsInset()
	}
	return 0
}

// TitleControlsInsetForTest exposes maximizedControlInset — where this
// window's maximized chrome starts its controls — so a host's alignment
// can be asserted from the package that owns the host.
func (w *Window) TitleControlsInsetForTest() core.Unit {
	return w.maximizedControlInset()
}

// paintMaximizedFrame draws the title bar only (no side borders).
func (w *Window) paintMaximizedFrame(p *core.Painter, bounds core.UnitRect, metrics core.CellMetrics,
	title string, titleStyle, frameStyle style.CellStyle, border style.BorderStyle) {

	w.mu.RLock()
	flags := w.flags
	state := w.state
	pressedButton := w.pressedButton
	buttonHovered := w.buttonHovered
	hoveredButton := w.hoveredButton
	titleFocus := w.titleFocus
	w.mu.RUnlock()

	// The title-bar kit: the (possibly scaled) row, cells and font every
	// title bar in the system measures and paints with.
	tm := w.titleBarMetrics()

	// Fill title bar background
	titleRect := core.UnitRect{
		X:      0,
		Y:      0,
		Width:  bounds.Width,
		Height: tm.RowH,
	}
	p.FillRect(titleRect, ' ', titleStyle)

	scheme := w.GetScheme()
	// Derive visual focus: active AND (parent has focus OR window has internal focus)
	focused := w.IsActive()
	if focused {
		if parent := w.Parent(); parent != nil {
			policy := parent.FocusPolicy()
			if policy == core.StrongFocus || policy == core.TabFocus {
				if !parent.HasFocus() {
					windowHasInternalFocus := false
					if fm := w.FocusManager(); fm != nil {
						if focusedTrinket := fm.FocusedTrinket(); focusedTrinket != nil {
							windowHasInternalFocus = focusedTrinket.HasFocus()
						}
					}
					focused = windowHasInternalFocus
				}
			}
		}
	}

	// The heavy (single) state is lit even though it isn't "focused": a
	// maximized window in that state has no border to carry the distinction,
	// so its icons must paint in the active style.
	buttonActive := focused || border == style.BorderHeavy

	// Draw window controls on the LEFT: [x][.][^] or [x][.][o] — each
	// through its own kit function (deliberately distinct per button; only
	// the three-cell mechanics are shared).
	//
	// Flush at the origin, except where the host's own controls sit
	// further in (a themed desktop, an unmaximized parent window): then
	// they line up with those instead of one cell to their left. 0 on a
	// cell surface, so the TUI is unchanged (see maximizedControlInset).
	buttonWidth := tm.ButtonW
	controlX := w.maximizedControlInset()
	if flags&WindowFlagNoClose == 0 {
		isFocused := titleFocus == TitleFocusClose
		isPressed := pressedButton == TitleButtonClose && buttonHovered
		isHovered := hoveredButton == TitleButtonClose && !isPressed && p.Graphical()
		btnStyle := scheme.GetTitleBarButtonState(buttonActive, isFocused, isHovered, isPressed)
		PaintCloseButton(p, tm, controlX, btnStyle)
		controlX += buttonWidth
	}
	if flags&WindowFlagNoMinimize == 0 {
		isFocused := titleFocus == TitleFocusMinimize
		isPressed := pressedButton == TitleButtonMinimize && buttonHovered
		isHovered := hoveredButton == TitleButtonMinimize && !isPressed && p.Graphical()
		btnStyle := scheme.GetTitleBarButtonState(buttonActive, isFocused, isHovered, isPressed)
		PaintMinimizeButton(p, tm, controlX, btnStyle)
		controlX += buttonWidth
	}
	if canMaximize(flags) {
		isFocused := titleFocus == TitleFocusMaximize
		isPressed := pressedButton == TitleButtonMaximize && buttonHovered
		isHovered := hoveredButton == TitleButtonMaximize && !isPressed && p.Graphical()
		btnStyle := scheme.GetTitleBarButtonState(buttonActive, isFocused, isHovered, isPressed)
		PaintZoomButton(p, tm, controlX, state == WindowStateMaximized, btnStyle)
		controlX += buttonWidth
	}

	// Tear-off handle floats immediately left of the (centered) title, but
	// is omitted while the title itself is focused - the '< >' brackets
	// stand in for it - so it isn't shoved aside; it returns on the next
	// Tab / Shift+Tab focus change.
	if titleFocus != TitleFocusTitle {
		tearTitleW := tm.TitleWidth(title)
		controlX = w.paintTearHandle(p, scheme, titleStyle, tm, controlX, bounds.Width, tearTitleW, buttonActive, titleFocus)
	}

	// Draw title text centered, with angle brackets and cyan bg if title has keyboard focus
	if titleFocus == TitleFocusTitle {
		// Title has focus - draw with decorative angle brackets, as one run.
		titleDisplayStyle := scheme.GetTitleBarButton(focused, true, false)
		PaintFocusedTitleDecoration(p, tm, titleRect.Width, title, titleDisplayStyle)
	} else {
		rightLimit := bounds.Width
		if titleFocus == TitleFocusBlur {
			rightLimit = bounds.Width - buttonWidth
		}
		PaintTitleBarText(p, tm, title, titleStyle, controlX, rightLimit, bounds.Width)
	}

	// Draw blur button on far right when blur item is focused
	if titleFocus == TitleFocusBlur {
		blurBtnStyle := scheme.GetTitleBarButton(focused, true, false) // Focused button style
		PaintBlurButton(p, tm, bounds.Width-buttonWidth, blurBtnStyle)
	}

	// Fill content area with background (same as normal frame).
	// Honor active/inactive window background from the scheme.
	contentBounds := w.contentBounds()
	p.FillRect(contentBounds, ' ', scheme.GetNormal(w.renderActive()))
}

// paintSingleBorderInner draws the thin inner line of a single-border
// (active-but-not-focused) graphical frame. The outer frame band is painted
// in the window background (reading as no border), so this hairline in the
// active border color - one tab-stroke weight thick - sits just inside it.
// No-op on cell surfaces.
func (w *Window) paintSingleBorderInner(p *core.Painter, localBounds core.UnitRect, st style.CellStyle) {
	bx, by := w.frameBorder()
	inner := core.UnitRect{
		X:      bx,
		Y:      by,
		Width:  localBounds.Width - 2*bx,
		Height: localBounds.Height - 2*by,
	}
	// The radius is screen-space (StrokeRoundedRectWeight transforms the
	// rect and passes the radius through), so the border it steps in by is
	// the provider's own count -- not the frame-denominated bx above, which
	// insets a rectangle the painter will transform.
	radius := windowCornerRadius - core.FindFrameBorderUnits(w)
	if radius < 0 {
		radius = 0
	}
	th := p.UnitsToPx(1) // match the tabbed control's tab-stroke weight
	if th < 1 {
		th = 1
	}
	p.StrokeRoundedRectWeight(inner, radius, th, st)
}

// paintNormalFrame draws the full window frame with borders.
func (w *Window) paintNormalFrame(p *core.Painter, bounds core.UnitRect, metrics core.CellMetrics,
	title string, titleStyle, frameStyle style.CellStyle, border style.BorderStyle, flags WindowFlags) {

	w.mu.RLock()
	state := w.state
	pressedButton := w.pressedButton
	buttonHovered := w.buttonHovered
	hoveredButton := w.hoveredButton
	titleFocus := w.titleFocus
	w.mu.RUnlock()

	// Draw border at local (0,0) - painter is already offset to window position
	localBounds := core.UnitRect{Width: bounds.Width, Height: bounds.Height}

	// Graphical path (D1): the window's entire surface is ONE rounded
	// rectangle - filled with the window background, stroked with the
	// border color (2 device px for double, 1 for single). Title,
	// buttons, and content then draw over it as usual. Cell surfaces
	// return false and take the box-drawing path below.
	// Honor the active/inactive window background from the scheme so the
	// interior distinguishes active (blue) from inactive (black).
	windowBG := w.GetScheme().GetWindowBG(w.renderActive())
	roundedStyle := frameStyle.WithBg(windowBG)
	// Single-border state (active but not focused - MDI child/menu bar holds
	// focus) is drawn with BorderHeavy. On the graphical path its outer frame
	// band is painted in the window background so it reads as no border; a
	// thin inner line in the active border color is drawn on top afterward
	// (see paintSingleBorderInner).
	if border == style.BorderHeavy {
		roundedStyle = roundedStyle.WithFg(windowBG)
	}
	rounded := p.DrawRoundedRect(localBounds, windowCornerRadius, border, roundedStyle)
	if rounded {
		// Frame painted; fall through to title/buttons/content.
	} else if titleFocus == TitleFocusBlur {
		// When blur item is focused, draw dashed frame with inactive title
		// color but keep corners, horizontally adjacent chars, and buttons
		// in active color
		scheme := w.GetScheme()
		blurFrameStyle := scheme.GetWindowTitle(false)   // Inactive title color for dashed lines
		activeFrameStyle := scheme.GetWindowBorder(true) // Active color for corners

		// Dashed line characters
		horizDash := '┄' // U+2504 BOX DRAWINGS LIGHT TRIPLE DASH HORIZONTAL
		vertDash := '┆'  // U+2506 BOX DRAWINGS LIGHT TRIPLE DASH VERTICAL

		// Double corners (in active color)
		topLeft := '╔'
		topRight := '╗'
		bottomLeft := '╚'
		bottomRight := '╝'

		// Get border character for horizontally adjacent positions
		horizLine := border.Horizontal

		// Draw corners in active color
		p.DrawCell(0, 0, topLeft, activeFrameStyle)
		p.DrawCell(localBounds.Width-metrics.UnitsPerCellWidth, 0, topRight, activeFrameStyle)
		p.DrawCell(0, localBounds.Height-metrics.UnitsPerCellHeight, bottomLeft, activeFrameStyle)
		p.DrawCell(localBounds.Width-metrics.UnitsPerCellWidth, localBounds.Height-metrics.UnitsPerCellHeight, bottomRight, activeFrameStyle)

		// Draw top edge - first and last chars adjacent to corners in active color, rest dashed
		for x := metrics.UnitsPerCellWidth; x < localBounds.Width-metrics.UnitsPerCellWidth; x += metrics.UnitsPerCellWidth {
			if x == metrics.UnitsPerCellWidth || x == localBounds.Width-2*metrics.UnitsPerCellWidth {
				// Adjacent to corner - use active style with normal horizontal line
				p.DrawCell(x, 0, horizLine, activeFrameStyle)
			} else {
				p.DrawCell(x, 0, horizDash, blurFrameStyle)
			}
		}

		// Draw bottom edge - first and last chars adjacent to corners in active color, rest dashed
		for x := metrics.UnitsPerCellWidth; x < localBounds.Width-metrics.UnitsPerCellWidth; x += metrics.UnitsPerCellWidth {
			if x == metrics.UnitsPerCellWidth || x == localBounds.Width-2*metrics.UnitsPerCellWidth {
				// Adjacent to corner - use active style with normal horizontal line
				p.DrawCell(x, localBounds.Height-metrics.UnitsPerCellHeight, horizLine, activeFrameStyle)
			} else {
				p.DrawCell(x, localBounds.Height-metrics.UnitsPerCellHeight, horizDash, blurFrameStyle)
			}
		}

		// Draw left edge - all dashed
		for y := metrics.UnitsPerCellHeight; y < localBounds.Height-metrics.UnitsPerCellHeight; y += metrics.UnitsPerCellHeight {
			p.DrawCell(0, y, vertDash, blurFrameStyle)
		}

		// Draw right edge - all dashed
		for y := metrics.UnitsPerCellHeight; y < localBounds.Height-metrics.UnitsPerCellHeight; y += metrics.UnitsPerCellHeight {
			p.DrawCell(localBounds.Width-metrics.UnitsPerCellWidth, y, vertDash, blurFrameStyle)
		}
	} else {
		p.DrawRect(localBounds, border, frameStyle)
	}

	// Graphical path only: the rounded fill painted the whole surface with
	// the window background, so the title bar's non-text areas would show
	// that background and read as gaps. Paint the entire title strip with
	// the title style so the bar reads as one solid color, then re-stroke
	// the border over it (clipped to the rounded outline so the top corners
	// stay round). The cell path keeps the border color in those areas.
	if rounded && flags&WindowFlagNoTitle == 0 {
		_, by := w.frameBorder()
		titleRect := core.UnitRect{Width: localBounds.Width, Height: by + w.titleBarMetrics().RowH}
		fillStyle := titleStyle
		if titleFocus == TitleFocusBlur {
			// Blur item focused: the whole bar reads inactive on the graphical
			// path (only the blur button stays highlighted), matching the cell
			// path's dimmed dashed frame.
			fillStyle = w.GetScheme().GetWindowTitle(false)
		}
		clip := p.WithRoundedClipRegion(localBounds, windowCornerRadius)
		clip.FillRect(titleRect, ' ', fillStyle)
		p.StrokeRoundedRect(localBounds, windowCornerRadius, border, roundedStyle)
	}

	scheme := w.GetScheme()
	// Derive visual focus: active AND (parent has focus OR window has internal focus)
	// When blur item is focused, buttons stay in active color but title bar text uses inactive
	focused := w.IsActive()
	if focused {
		if parent := w.Parent(); parent != nil {
			policy := parent.FocusPolicy()
			if policy == core.StrongFocus || policy == core.TabFocus {
				if !parent.HasFocus() {
					windowHasInternalFocus := false
					if fm := w.FocusManager(); fm != nil {
						if focusedTrinket := fm.FocusedTrinket(); focusedTrinket != nil {
							windowHasInternalFocus = focusedTrinket.HasFocus()
						}
					}
					focused = windowHasInternalFocus
				}
			}
		}
	}
	// A nested MDI child dims with its ancestor lineage (see Paint): if a
	// containing window is inactive, the child's chrome is inactive too.
	if aw := w.nearestAncestorWindow(); aw != nil && !aw.isLit() {
		focused = false
	}

	// For button styling, use active appearance even when blur is focused -
	// except on the graphical path, where a blur-focused bar renders fully
	// inactive (the other buttons dim; only the blur button stays lit). The
	// single-border (heavy) state is lit, so its icons paint active too.
	buttonFocused := focused || titleFocus == TitleFocusBlur || border == style.BorderHeavy
	if rounded && titleFocus == TitleFocusBlur {
		buttonFocused = false
	}
	// The title-bar kit: the (possibly scaled) row, cells and font every
	// title bar in the system measures and paints with.
	tm := w.titleBarMetrics()

	// Draw title if enabled
	if flags&WindowFlagNoTitle == 0 {
		// The titlebar chrome (title text + buttons) sits INSIDE the frame
		// border: shift it in by the border and use the inner width. On cell
		// surfaces the border reservation is 0, so this is a no-op.
		bx, by := w.frameBorder()
		tp := p
		innerW := bounds.Width
		if bx > 0 || by > 0 {
			tp = p.WithOffset(bx, by)
			innerW = bounds.Width - 2*bx
		}
		// Draw window controls on the LEFT: [x][.][^] or [x][.][o] — each
		// through its own kit function (deliberately distinct per button;
		// only the three-cell mechanics are shared).
		buttonWidth := tm.ButtonW
		controlX := tm.CellW // Start after left border
		if flags&WindowFlagNoClose == 0 {
			isFocused := titleFocus == TitleFocusClose
			isPressed := pressedButton == TitleButtonClose && buttonHovered
			isHovered := hoveredButton == TitleButtonClose && !isPressed && p.Graphical()
			btnStyle := scheme.GetTitleBarButtonState(buttonFocused, isFocused, isHovered, isPressed)
			PaintCloseButton(tp, tm, controlX, btnStyle)
			controlX += buttonWidth
		}
		if flags&WindowFlagNoMinimize == 0 {
			isFocused := titleFocus == TitleFocusMinimize
			isPressed := pressedButton == TitleButtonMinimize && buttonHovered
			isHovered := hoveredButton == TitleButtonMinimize && !isPressed && p.Graphical()
			btnStyle := scheme.GetTitleBarButtonState(buttonFocused, isFocused, isHovered, isPressed)
			PaintMinimizeButton(tp, tm, controlX, btnStyle)
			controlX += buttonWidth
		}
		if canMaximize(flags) {
			isFocused := titleFocus == TitleFocusMaximize
			isPressed := pressedButton == TitleButtonMaximize && buttonHovered
			isHovered := hoveredButton == TitleButtonMaximize && !isPressed && p.Graphical()
			btnStyle := scheme.GetTitleBarButtonState(buttonFocused, isFocused, isHovered, isPressed)
			PaintZoomButton(tp, tm, controlX, state == WindowStateMaximized, btnStyle)
			controlX += buttonWidth
		}

		// Calculate title area (centered on top border)
		titleRect := core.UnitRect{
			X:      0,
			Y:      0,
			Width:  innerW,
			Height: tm.RowH,
		}

		// Tear-off handle floats immediately left of the (centered) title,
		// but is omitted while the title itself is focused - the '< >'
		// brackets stand in for it - so it isn't shoved aside; it returns on
		// the next Tab / Shift+Tab focus change.
		if titleFocus != TitleFocusTitle {
			tearTitleW := tm.TitleWidth(title)
			// On the graphical path a blur-focused bar reads fully inactive, so
			// the tear/redock handle and the space around it take the inactive
			// title colors too (matching a real inactive window frame).
			tearStyle := titleStyle
			tearActive := buttonFocused
			if rounded && titleFocus == TitleFocusBlur {
				tearStyle = scheme.GetWindowTitle(false)
				tearActive = false
			}
			controlX = w.paintTearHandle(tp, scheme, tearStyle, tm, controlX, innerW, tearTitleW, tearActive, titleFocus)
		}

		// Draw title text centered, with angle brackets and cyan bg if title has keyboard focus
		if titleFocus == TitleFocusTitle {
			// Title has focus - draw with decorative angle brackets, as one run.
			titleDisplayStyle := scheme.GetTitleBarButton(focused, true, false)
			PaintFocusedTitleDecoration(tp, tm, titleRect.Width, title, titleDisplayStyle)
		} else {
			// Normal title or blur focused
			titleDisplayStyle := titleStyle
			if titleFocus == TitleFocusBlur {
				// Blur item focused - use inactive title style for the title text
				titleDisplayStyle = scheme.GetWindowTitle(false)
			}
			rightLimit := innerW - tm.CellW
			if titleFocus == TitleFocusBlur {
				rightLimit = innerW - tm.CellW - buttonWidth
			}
			PaintTitleBarText(tp, tm, title, titleDisplayStyle, controlX, rightLimit, innerW)
		}

		// Draw blur button on far right when blur item is focused
		if titleFocus == TitleFocusBlur {
			blurBtnStyle := scheme.GetTitleBarButton(true, true, false) // Focused button style
			PaintBlurButton(tp, tm, innerW-tm.CellW-buttonWidth, blurBtnStyle)
		}
	}

	// Single-border (active, not focused): the outer band is window-background
	// colored (drawn above), so add the thin inner line in the active border
	// color. Drawn last - after the titlebar icons and title - so it slightly
	// overlays them. The after-content re-stroke re-adds it over edge-to-edge
	// content.
	if rounded && border == style.BorderHeavy {
		w.paintSingleBorderInner(p, localBounds, w.GetScheme().GetWindowBorder(true))
	}

	// Fill content area with background. Skipped when the rounded
	// frame painted: the whole window surface (corners included) is
	// already filled, and a square fill here would put background
	// pixels back outside the bottom corner arcs.
	if !rounded {
		contentBounds := w.contentBounds()
		p.FillRect(contentBounds, ' ', w.GetScheme().GetNormal(w.renderActive()))
	}
}

// ellipsizeToWidth trims s so that with a trailing ellipsis it fits
// within avail; empty when not even the ellipsis fits. The ellipsis
// is three periods, not the "\u2026" glyph, matching the tab strip -
// on cell surfaces it is three cells wide, and MeasureText adjusts
// the need-for-ellipsis math on both surfaces.
// EllipsizeToWidth is ellipsizeToWidth for callers outside the package:
// the desktop's themed title bar lays out like a window title and trims
// with the same ellipsis.
func EllipsizeToWidth(s string, avail core.Unit, font *core.Font, metrics core.CellMetrics) string {
	return ellipsizeToWidth(s, avail, font, metrics)
}

func ellipsizeToWidth(s string, avail core.Unit, font *core.Font, metrics core.CellMetrics) string {
	const ell = "..."
	if font.MeasureTextIn(s, metrics) <= avail {
		return s
	}
	runes := []rune(s)
	// Binary search for the longest prefix that still fits with the
	// ellipsis. Text is never NARROWER for having one more character in
	// it - the assumption ellipsizing rests on to begin with - so "fits"
	// is downward closed and this lands on the same prefix a scan from
	// the full string would, in log(n) measurements instead of n. Every
	// title bar in the system runs this on every paint, and a measurement
	// is a shaping lookup keyed on a freshly built string.
	lo, hi := 0, len(runes) // lo fits (as ""), hi does not
	for lo < hi-1 {
		mid := (lo + hi) / 2
		if font.MeasureTextIn(string(runes[:mid])+ell, metrics) <= avail {
			lo = mid
		} else {
			hi = mid
		}
	}
	if lo == 0 {
		// Not even one character plus the ellipsis: the ellipsis alone
		// only shows if it fits by itself.
		if font.MeasureTextIn(ell, metrics) <= avail {
			return ell
		}
		return ""
	}
	return string(runes[:lo]) + ell
}

// tearHandleSlotX returns the X of the tear handle's button-width slot.
// The handle floats immediately left of where the title would center in
// the bar, and only butts against the control buttons (controlsRight)
// when the centered title leaves no room to its left.
func tearHandleSlotX(barWidth, controlsRight, titleW, buttonWidth core.Unit) core.Unit {
	x := (barWidth-titleW)/2 - buttonWidth
	if x < controlsRight {
		x = controlsRight
	}
	return x
}

// paintTearHandle draws the tear-off handle (the %/# glyph) in a
// button-width slot floating immediately left of the (centered) title,
// and returns the leftUsed value paintTitleText expects (its +UnitsPerCellWidth
// gap lands the title just past the handle slot). The glyph carries the
// button foreground over the title-bar background; when the handle is
// the focused title element it draws [%]/[#] in the focused-button
// style like the other buttons. Not tearable: controlsRight is returned
// unchanged (title keeps its normal gap past the controls).
func (w *Window) paintTearHandle(p *core.Painter, scheme *style.Scheme, titleStyle style.CellStyle, tm TitleBarMetrics, controlsRight, barWidth, titleW core.Unit, windowActive bool, titleFocus TitleFocus) core.Unit {
	w.mu.RLock()
	tearable := w.flags&WindowFlagTearable != 0
	detached := w.detached
	w.mu.RUnlock()
	if tearable == false || !hasTitleBar(w.flags, w.State()) {
		return controlsRight
	}
	handleX := tearHandleSlotX(barWidth, controlsRight, titleW, tm.ButtonW)
	glyph := '%'
	if detached {
		glyph = '#'
	}
	focusSt := scheme.GetTitleBarButton(windowActive, true, false)
	glyphSt := titleStyle.WithFg(scheme.GetTitleBarButton(windowActive, false, false).Fg)
	PaintTearHandleSlot(p, tm, handleX, glyph, titleFocus == TitleFocusTear, focusSt, titleStyle, glyphSt)
	// The title butts against the right edge of the handle slot; the
	// -CellW cancels PaintTitleBarText's +CellW gap so a centered title
	// lands exactly one slot right of the handle.
	return handleX + tm.ButtonW - tm.CellW
}

// buttonAtPosition returns which titlebar button is at the given local coordinates.
// Returns TitleButtonNone if not on a button.
func (w *Window) buttonAtPosition(x, y core.Unit) TitleButton {
	w.mu.RLock()
	flags := w.flags
	state := w.state
	title := w.title
	titleFocus := w.titleFocus
	w.mu.RUnlock()

	tm := w.titleBarMetrics()

	// The titlebar chrome sits inside the frame border (maximized has no
	// side border); shift the hit-test into the same inner coordinate
	// system paintNormalFrame draws it in.
	insetX, insetY := core.Unit(0), core.Unit(0)
	if state != WindowStateMaximized {
		insetX, insetY = w.frameBorder()
	}
	x -= insetX
	y -= insetY

	// Must be in titlebar (the kit's possibly-scaled row)
	if !hasTitleBar(flags, state) || y < 0 || y >= tm.RowH {
		return TitleButtonNone
	}

	// Control buttons are on the left
	controlX := tm.CellW // Start after left border (for normal frame)
	if state == WindowStateMaximized {
		// No border in maximized state: flush, or aligned with the host's
		// own controls where those sit further in (matches the paint).
		controlX = w.maximizedControlInset()
	}

	buttonWidth := tm.ButtonW

	// Check close button [x]
	if flags&WindowFlagNoClose == 0 {
		if x >= controlX && x < controlX+buttonWidth {
			return TitleButtonClose
		}
		controlX += buttonWidth
	}

	// Check minimize button [.]
	if flags&WindowFlagNoMinimize == 0 {
		if x >= controlX && x < controlX+buttonWidth {
			return TitleButtonMinimize
		}
		controlX += buttonWidth
	}

	// Check maximize/restore button [^] or [o]
	if canMaximize(flags) {
		if x >= controlX && x < controlX+buttonWidth {
			return TitleButtonMaximize
		}
		controlX += buttonWidth
	}

	// Check tear-off handle [%]/[#]. It floats immediately left of the
	// centered title, so hit-test the same slot paintTearHandle draws (with
	// the kit's font, so a scaled bar's slot lands where its paint does).
	// The handle is hidden while the title is focused, so it isn't hittable
	// then.
	if flags&WindowFlagTearable != 0 && hasTitleBar(flags, state) && titleFocus != TitleFocusTitle {
		titleW := tm.TitleWidth(title)
		// Inner width: the paint centers within the border-inset titlebar.
		handleX := tearHandleSlotX(w.Bounds().Width-2*insetX, controlX, titleW, buttonWidth)
		if x >= handleX && x < handleX+buttonWidth {
			return TitleButtonTear
		}
	}

	return TitleButtonNone
}

// TitleFocus returns the current title bar focus.
func (w *Window) TitleFocus() TitleFocus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.titleFocus
}

// SetTitleFocus sets which title bar element has keyboard focus.
func (w *Window) SetTitleFocus(focus TitleFocus) {
	w.mu.Lock()
	oldFocus := w.titleFocus
	w.titleFocus = focus
	stateChanged := (oldFocus == TitleFocusTitle) != (focus == TitleFocusTitle)
	if focus == TitleFocusNone {
		w.resizeEdges = ResizeEdgeNone // Clear resize state when leaving title bar
	}
	title := w.title
	w.mu.Unlock()

	// Entering or leaving the title bar changes which commands exist, so the
	// context is rebuilt and the bar re-forms its accelerators against it.
	if stateChanged {
		w.refreshKeyContext()
	}

	// Announce titlebar element change for accessibility
	if focus != oldFocus && focus != TitleFocusNone {
		if am := core.FindAccessibilityManager(w); am != nil {
			var elementName string
			switch focus {
			case TitleFocusClose:
				elementName = "close button"
			case TitleFocusMinimize:
				elementName = "minimize button"
			case TitleFocusMaximize:
				if w.IsMaximized() {
					elementName = "restore button"
				} else {
					elementName = "maximize button"
				}
			case TitleFocusTear:
				// The handle reads '#' while torn (re-docks) and '%' while
				// docked (tears off); announce its current action.
				if w.IsDetached() {
					elementName = "dock torn window button"
				} else {
					elementName = "tear-away button"
				}
			case TitleFocusTitle:
				elementName = title + ", title bar"
			case TitleFocusBlur:
				elementName = "blur button"
			}
			if elementName != "" {
				am.AnnouncePolite(elementName)
			}
		}
	}

	w.Update()
}

// HasTitleFocus returns true if any title bar element has keyboard focus.
func (w *Window) HasTitleFocus() bool {
	return w.TitleFocus() != TitleFocusNone
}

// hasKeyboardBlurEnabled returns true if the parent container has keyboard blur enabled.
//
// A TORN window is asked of the desktop instead: blurring one leaves the OS
// window entirely (see performKeyboardBlur), so the control only makes sense
// when there is another window or a desktop to land on. A solo app's main
// window has neither, and an inert control in a title bar is worse than no
// control at all.
func (w *Window) hasKeyboardBlurEnabled() bool {
	if w.IsDetached() {
		if b := w.findDetachedBlurrer(); b != nil {
			return b.CanBlurDetachedWindow(w)
		}
		return false
	}
	parent := w.Parent()
	if parent == nil {
		return false
	}
	if provider, ok := parent.(core.KeyboardBlurChildrenProvider); ok {
		return provider.KeyboardBlurChildren()
	}
	return false
}

// detachedBlurrer is the desktop, which is the only thing that knows what
// sits ABOVE a torn window: the app it belongs to, and whether there is a
// desktop behind it at all. Declared here rather than imported so the
// dependency stays one-way (as windowSurfacer is).
type detachedBlurrer interface {
	// CanBlurDetachedWindow reports whether this window has anywhere to blur
	// TO. A solo app's main window does not: it fills the primary surface and
	// there is no desktop behind it, so the control is not offered rather than
	// offered and inert. It comes back by itself the moment a desktop appears,
	// because the answer is asked fresh on every layout.
	CanBlurDetachedWindow(win *Window) bool
	BlurDetachedWindow(win *Window)
}

// findDetachedBlurrer walks up for the desktop. A torn window keeps its
// parent pointer -- tearing off removes it from the manager's list, not
// from the trinket tree -- so the walk still arrives.
func (w *Window) findDetachedBlurrer() detachedBlurrer {
	var current any = w.Parent()
	for current != nil {
		if b, ok := current.(detachedBlurrer); ok {
			return b
		}
		t, ok := current.(core.Trinket)
		if !ok {
			return nil
		}
		current = t.Parent()
	}
	return nil
}

// performKeyboardBlur hands the keyboard back to whatever contains this
// window.
//
// Docked, that is the parent container: the desktop or MDI pane focuses its
// menu bar, and the window is still on screen right beside it. TORN, the
// containing surface holds this one window and nothing else, so the generic
// path would focus a menu bar on a surface the user is not even looking at
// while this window kept the OS focus. A torn window blurs up the OWNERSHIP
// chain instead -- to its app's main window, or, if it is that main window,
// to the desktop it was torn from -- which is where "out of this window"
// actually leads when the window is a whole OS window.
func (w *Window) performKeyboardBlur() {
	if w.IsDetached() {
		if b := w.findDetachedBlurrer(); b != nil {
			b.BlurDetachedWindow(w)
			return
		}
	}
	parent := w.Parent()
	if parent == nil {
		return
	}
	if provider, ok := parent.(core.KeyboardBlurChildrenProvider); ok {
		provider.PerformKeyboardBlur()
	}
}

// handleTitleBarKey handles keyboard input when title bar has focus.
func (w *Window) handleTitleBarKey(event core.KeyPressEvent, cmd string) bool {
	w.mu.RLock()
	titleFocus := w.titleFocus
	resizeEdges := w.resizeEdges
	flags := w.flags
	w.mu.RUnlock()

	metrics := w.frameCellMetrics()

	// Handle navigation between title bar elements
	switch cmd {
	case core.CmdFocusNext:
		// Move to next title element or exit to content
		next := w.nextTitleFocus(titleFocus)
		if next == TitleFocusNone {
			// Exit title bar, focus first trinket in content
			w.SetTitleFocus(TitleFocusNone)
			if fm := w.FocusManager(); fm != nil {
				fm.FocusFirst()
			}
		} else {
			w.SetTitleFocus(next)
		}
		return true

	case core.CmdFocusPrior:
		// Move to previous title element, or loop to content's last trinket
		prev := w.prevTitleFocus(titleFocus)
		if prev == titleFocus {
			// At first title element, loop to content's last trinket
			w.SetTitleFocus(TitleFocusNone)
			if fm := w.FocusManager(); fm != nil {
				fm.FocusLast()
			}
		} else {
			w.SetTitleFocus(prev)
		}
		return true

	case core.CmdWindowCancelResize:
		// Exit title bar focus, return to content
		w.SetTitleFocus(TitleFocusNone)
		w.mu.Lock()
		w.resizeEdges = ResizeEdgeNone
		w.mu.Unlock()
		if fm := w.FocusManager(); fm != nil {
			fm.FocusFirst()
		}
		return true

	case core.CmdTrinketActivate:
		// Activate focused button or confirm resize
		switch titleFocus {
		case TitleFocusClose:
			if flags&WindowFlagNoClose == 0 {
				w.Close()
			}
		case TitleFocusMinimize:
			if flags&WindowFlagNoMinimize == 0 {
				w.mu.RLock()
				handler := w.onMinimizeRequest
				w.mu.RUnlock()
				if handler != nil {
					handler()
				}
			}
		case TitleFocusMaximize:
			if canMaximize(flags) {
				w.mu.RLock()
				handler := w.onMaximizeRequest
				w.mu.RUnlock()
				if handler != nil {
					handler()
				}
			}
		case TitleFocusTear:
			if flags&WindowFlagTearable != 0 {
				w.requestTear()
			}
		case TitleFocusTitle:
			// Confirm resize - clear edges so next Shift+arrow starts fresh
			w.mu.Lock()
			if w.resizeEdges != ResizeEdgeNone {
				w.resizeEdges = ResizeEdgeNone
				w.resizeStartBounds = w.Bounds()
			}
			w.mu.Unlock()
		case TitleFocusBlur:
			// Blur the window - return focus to parent container
			w.SetTitleFocus(TitleFocusNone)
			w.performKeyboardBlur()
		}
		return true
	}

	// Handle window movement and resizing when title has focus
	if titleFocus == TitleFocusTitle {
		bounds := w.Bounds()

		// The command says which direction, whether this moves or resizes,
		// and how big the step is. This replaces a loop that peeled modifier
		// prefixes off the key string in any order and then re-derived the
		// same three facts from the leftovers -- a keymap answers all of it
		// now, including for chords the peeler never knew about. The decode
		// is the kit's (DecodeTitleGeometry), shared with the desktop's own
		// title bar so the vocabulary cannot drift.
		key, resize, coarse, ok := DecodeTitleGeometry(cmd)
		if !ok {
			return false
		}

		// hasShift is what the bodies below call "this is a resize", which is
		// what the shifted arrow always meant.
		hasShift := resize
		horizStep := metrics.UnitsPerCellWidth
		vertStep := metrics.UnitsPerCellHeight
		if coarse {
			horizStep = metrics.UnitsPerCellWidth * 10
			vertStep = metrics.UnitsPerCellHeight * 4
		}

		// A non-resizable window ignores keyboard resize (Shift is the
		// resize modifier) but still allows plain-arrow moves.
		if hasShift && flags&WindowFlagNoResize != 0 {
			switch key {
			case "Left", "Right", "Up", "Down":
				return true
			}
		}

		// A maximized window is already at its maximum size, so keyboard
		// geometry from the titlebar snaps it off the maximized state:
		//   - a MOVE (plain arrow) restores to the pre-maximize size and
		//     then moves, like dragging the titlebar off the top;
		//   - a RESIZE (Shift+arrow) can only make it smaller, so the first
		//     key defaults to shrinking the edge OPPOSITE the arrow
		//     (Shift+Left pulls the right edge in) while un-maximizing in
		//     place, so the window keeps its full-screen size as the
		//     starting point and just narrows from there.
		if w.IsMaximized() {
			switch key {
			case "Left", "Right", "Up", "Down":
				if hasShift {
					var edge int
					switch key {
					case "Left":
						edge = ResizeEdgeRight
					case "Right":
						edge = ResizeEdgeLeft
					case "Up":
						edge = ResizeEdgeBottom
					case "Down":
						edge = ResizeEdgeTop
					}
					w.mu.Lock()
					w.resizeStartBounds = w.Bounds()
					w.resizeEdges = edge
					w.mu.Unlock()
					w.unmaximizeInPlace()
					bounds = w.Bounds()
					resizeEdges = edge
				} else {
					w.Restore()
					bounds = w.Bounds()
				}
			}
		}

		switch key {
		case "Left":
			if hasShift {
				// Start/continue resizing left edge
				if resizeEdges&ResizeEdgeLeft != 0 {
					// Continue left resize: expand left
					newBounds := bounds
					newBounds.X -= horizStep
					newBounds.Width += horizStep
					w.requestKeyboardBounds(newBounds, false)
				} else if resizeEdges&ResizeEdgeRight != 0 {
					// Continue right resize: shrink right edge
					newBounds := bounds
					newBounds.Width -= horizStep
					if newBounds.Width >= w.minWidth {
						w.requestKeyboardBounds(newBounds, false)
					}
				} else {
					// Start: expand left edge
					w.mu.Lock()
					if w.resizeEdges == ResizeEdgeNone {
						w.resizeStartBounds = bounds // Save for Escape to revert
					}
					w.resizeEdges = ResizeEdgeLeft
					w.mu.Unlock()
					newBounds := bounds
					newBounds.X -= horizStep
					newBounds.Width += horizStep
					w.requestKeyboardBounds(newBounds, false)
				}
			} else {
				// Move window left
				newBounds := bounds
				newBounds.X -= horizStep
				w.requestKeyboardBounds(newBounds, true)
			}
			return true

		case "Right":
			if hasShift {
				// Start/continue resizing right edge
				if resizeEdges&ResizeEdgeRight != 0 {
					// Continue right resize: expand right
					newBounds := bounds
					newBounds.Width += horizStep
					w.requestKeyboardBounds(newBounds, false)
				} else if resizeEdges&ResizeEdgeLeft != 0 {
					// Continue left resize: shrink left edge
					newBounds := bounds
					newBounds.X += horizStep
					newBounds.Width -= horizStep
					if newBounds.Width >= w.minWidth {
						w.requestKeyboardBounds(newBounds, false)
					}
				} else {
					// Start: expand right edge
					w.mu.Lock()
					if w.resizeEdges == ResizeEdgeNone {
						w.resizeStartBounds = bounds // Save for Escape to revert
					}
					w.resizeEdges = ResizeEdgeRight
					w.mu.Unlock()
					newBounds := bounds
					newBounds.Width += horizStep
					w.requestKeyboardBounds(newBounds, false)
				}
			} else {
				// Move window right
				newBounds := bounds
				newBounds.X += horizStep
				w.requestKeyboardBounds(newBounds, true)
			}
			return true

		case "Up":
			if hasShift {
				// Start/continue resizing top edge
				if resizeEdges&ResizeEdgeTop != 0 {
					// Continue top resize: expand top
					newBounds := bounds
					newBounds.Y -= vertStep
					newBounds.Height += vertStep
					w.requestKeyboardBounds(newBounds, false)
				} else if resizeEdges&ResizeEdgeBottom != 0 {
					// Continue bottom resize: shrink bottom edge
					newBounds := bounds
					newBounds.Height -= vertStep
					if newBounds.Height >= w.minHeight {
						w.requestKeyboardBounds(newBounds, false)
					}
				} else {
					// Start: expand top edge
					w.mu.Lock()
					if w.resizeEdges == ResizeEdgeNone {
						w.resizeStartBounds = bounds // Save for Escape to revert
					}
					w.resizeEdges |= ResizeEdgeTop
					w.mu.Unlock()
					newBounds := bounds
					newBounds.Y -= vertStep
					newBounds.Height += vertStep
					w.requestKeyboardBounds(newBounds, false)
				}
			} else {
				// Move window up - or, if it is already pressed against the
				// top of the client area, snap it maximized (the keyboard
				// equivalent of dragging the titlebar into the menu bar).
				if !w.keyboardTopSnapMaximize(bounds) {
					newBounds := bounds
					newBounds.Y -= vertStep
					w.requestKeyboardBounds(newBounds, true)
				}
			}
			return true

		case "Down":
			if hasShift {
				// Start/continue resizing bottom edge
				if resizeEdges&ResizeEdgeBottom != 0 {
					// Continue bottom resize: expand bottom
					newBounds := bounds
					newBounds.Height += vertStep
					w.requestKeyboardBounds(newBounds, false)
				} else if resizeEdges&ResizeEdgeTop != 0 {
					// Continue top resize: shrink top edge
					newBounds := bounds
					newBounds.Y += vertStep
					newBounds.Height -= vertStep
					if newBounds.Height >= w.minHeight {
						w.requestKeyboardBounds(newBounds, false)
					}
				} else {
					// Start: expand bottom edge
					w.mu.Lock()
					if w.resizeEdges == ResizeEdgeNone {
						w.resizeStartBounds = bounds // Save for Escape to revert
					}
					w.resizeEdges |= ResizeEdgeBottom
					w.mu.Unlock()
					newBounds := bounds
					newBounds.Height += vertStep
					w.requestKeyboardBounds(newBounds, false)
				}
			} else {
				// Move window down
				newBounds := bounds
				newBounds.Y += vertStep
				w.requestKeyboardBounds(newBounds, true)
			}
			return true

		}
	}

	return false
}

// SetOnBoundsRequest installs a delegate for title-focus keyboard
// geometry changes (arrow moves, Shift-arrow resizes, Escape
// reverts). A torn-off window's host maps the deltas onto its OS
// window; nil restores normal in-surface SetBounds handling.
func (w *Window) SetOnBoundsRequest(handler func(core.UnitRect) bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onBoundsRequest = handler
}

// requestKeyboardBounds applies a title-focus keyboard geometry
// change: the bounds delegate takes it whole when installed,
// otherwise it applies in-surface - constrained to the client area
// when the change is a pure move.
func (w *Window) requestKeyboardBounds(b core.UnitRect, isMove bool) {
	w.mu.RLock()
	delegate := w.onBoundsRequest
	w.mu.RUnlock()
	if delegate != nil && delegate(b) {
		return
	}
	if isMove {
		b = w.constrainBoundsForMovement(b)
	}
	w.SetBounds(b)
}

// constrainBoundsForMovement adjusts bounds to keep titlebar visible within client area.
// Horizontally: allows window to go almost off-screen (just 1 unit border visible)
// Vertically: titlebar must stay within client area
func (w *Window) constrainBoundsForMovement(newBounds core.UnitRect) core.UnitRect {
	w.mu.RLock()
	getBounds := w.getConstrainingBounds
	w.mu.RUnlock()

	if getBounds == nil {
		return newBounds
	}

	clientArea := getBounds()
	metrics := w.frameCellMetrics()

	// Title bar vertically within the client area, at least a couple of
	// columns visible horizontally on each side (shared with the mouse
	// drag and re-dock paths).
	newBounds = clampWindowToClientArea(newBounds, clientArea, metrics)

	// Limit height to client area height (windows can be wider but not taller)
	if newBounds.Height > clientArea.Height {
		newBounds.Height = clientArea.Height
	}

	return newBounds
}

// minVisibleColumns is how much of a window must stay within the
// client area horizontally so it can always be grabbed back.
const minVisibleColumns core.Unit = 2

// ClampWindowToClientArea is the exported form of the shared corral
// used by both the desktop WindowManager and embedded MDIPanes: it
// keeps a window retrievable within its container (title bar vertically
// inside the client area, at least a couple of columns visible on each
// side horizontally).
func ClampWindowToClientArea(bounds, clientArea core.UnitRect, metrics core.CellMetrics) core.UnitRect {
	return clampWindowToClientArea(bounds, clientArea, metrics)
}

// clampWindowToClientArea keeps a window retrievable: its title bar
// vertically within the client area (below any menu bar, above any
// dock/status bar - the client area already excludes them), and at
// least minVisibleColumns of width within it on each side.
func clampWindowToClientArea(bounds, clientArea core.UnitRect, metrics core.CellMetrics) core.UnitRect {
	minVisible := metrics.UnitsPerCellWidth * minVisibleColumns

	if bounds.Y < clientArea.Y {
		bounds.Y = clientArea.Y
	}
	maxY := clientArea.Y + clientArea.Height - metrics.UnitsPerCellHeight
	if bounds.Y > maxY {
		bounds.Y = maxY
	}

	minX := clientArea.X - bounds.Width + minVisible
	if bounds.X < minX {
		bounds.X = minX
	}
	maxX := clientArea.X + clientArea.Width - minVisible
	if bounds.X > maxX {
		bounds.X = maxX
	}
	return bounds
}

// nextTitleFocus returns the next title bar element after the given one.
func (w *Window) nextTitleFocus(current TitleFocus) TitleFocus {
	w.mu.RLock()
	flags := w.flags
	w.mu.RUnlock()

	// Order: Close -> Minimize -> Maximize -> Title -> Blur (if enabled) -> (exit to content)
	switch current {
	case TitleFocusClose:
		if flags&WindowFlagNoMinimize == 0 {
			return TitleFocusMinimize
		}
		fallthrough
	case TitleFocusMinimize:
		if canMaximize(flags) {
			return TitleFocusMaximize
		}
		fallthrough
	case TitleFocusMaximize:
		if flags&WindowFlagTearable != 0 {
			return TitleFocusTear
		}
		return TitleFocusTitle
	case TitleFocusTear:
		return TitleFocusTitle
	case TitleFocusTitle:
		// If keyboard blur is enabled, go to blur item next
		if w.hasKeyboardBlurEnabled() {
			return TitleFocusBlur
		}
		return TitleFocusNone // Exit to content
	case TitleFocusBlur:
		return TitleFocusNone // Exit to content
	}
	return TitleFocusNone
}

// prevTitleFocus returns the previous title bar element before the given one.
func (w *Window) prevTitleFocus(current TitleFocus) TitleFocus {
	w.mu.RLock()
	flags := w.flags
	w.mu.RUnlock()

	// Reverse order: Blur -> Title -> Maximize -> Minimize -> Close
	switch current {
	case TitleFocusBlur:
		return TitleFocusTitle
	case TitleFocusTitle:
		if flags&WindowFlagTearable != 0 {
			return TitleFocusTear
		}
		if canMaximize(flags) {
			return TitleFocusMaximize
		}
		fallthrough
	case TitleFocusTear:
		if canMaximize(flags) {
			return TitleFocusMaximize
		}
		fallthrough
	case TitleFocusMaximize:
		if flags&WindowFlagNoMinimize == 0 {
			return TitleFocusMinimize
		}
		fallthrough
	case TitleFocusMinimize:
		if flags&WindowFlagNoClose == 0 {
			return TitleFocusClose
		}
		fallthrough
	case TitleFocusClose:
		return TitleFocusClose // Stay at close, can't go back further
	}
	return TitleFocusClose
}

// HandleKeyPress handles keyboard input.
// HandleTextEditing forwards an input method's composition straight to
// the focused trinket. The window's own key policy - the menu bar, the
// shortcut resolver, title-bar focus, Alt+F4 - is deliberately skipped:
// none of it has anything to say about characters that are still being
// composed, and a composition containing "m" is not Cmd+M.
func (w *Window) HandleTextEditing(event core.TextEditingEvent) bool {
	w.mu.RLock()
	fm := w.focusManager
	w.mu.RUnlock()

	if fm == nil {
		return false
	}
	return fm.HandleTextEditing(event)
}

// HandleTextCommit forwards a finished composition straight to the focused
// trinket, skipping the window's own key policy for the same reason
// HandleTextEditing does: a commit is not a keystroke, so the menu bar and the
// shortcut resolver have nothing to say about it.
func (w *Window) HandleTextCommit(event core.TextCommitEvent) bool {
	w.mu.RLock()
	fm := w.focusManager
	w.mu.RUnlock()

	if fm == nil {
		return false
	}
	return fm.HandleTextCommit(event)
}

// HandleTextErase forwards an input method's erase straight to the focused
// trinket, skipping the window's own key policy for the same reason
// HandleTextCommit does: it is not a keystroke.
func (w *Window) HandleTextErase(event core.TextEraseEvent) bool {
	w.mu.RLock()
	fm := w.focusManager
	w.mu.RUnlock()

	if fm == nil {
		return false
	}
	return fm.HandleTextErase(event)
}

// HandlePaste forwards pasted text straight to the focused trinket, skipping
// the window's own key policy for the same reason HandleTextEditing does:
// none of the menu bar, shortcut resolver, or title-bar focus has anything to
// say about text the user is dropping into whatever they are typing in.
func (w *Window) HandlePaste(event core.PasteEvent) bool {
	w.mu.RLock()
	fm := w.focusManager
	w.mu.RUnlock()

	if fm == nil {
		return false
	}
	return fm.HandlePaste(event)
}

// HandleKeyRelease hands a key coming back up to whatever holds focus in this
// window. None of HandleKeyPress's chrome applies — title-bar focus, the menu
// bar, the shortcut resolver and pass-next-key all act on the press.
func (w *Window) HandleKeyRelease(event core.KeyReleaseEvent) bool {
	w.mu.RLock()
	fm := w.focusManager
	w.mu.RUnlock()

	if fm == nil {
		return false
	}
	return fm.HandleKeyRelease(event)
}

func (w *Window) HandleKeyPress(event core.KeyPressEvent) bool {
	w.mu.RLock()
	fm := w.focusManager
	titleFocus := w.titleFocus
	mb := w.menuBar
	shortcutResolver := w.shortcutResolver
	rawNext := w.passNextKeyRaw
	rawDone := w.onRawKeyDone
	w.mu.RUnlock()

	// Raw key input: this key goes straight to the focused trinket,
	// bypassing the window's own menu-bar shortcut handling, then the mode
	// clears and the caller restores its prompt.
	if rawNext {
		w.mu.Lock()
		w.passNextKeyRaw = false
		w.onRawKeyDone = nil
		w.mu.Unlock()
		if fm != nil {
			fm.HandleKeyPress(event)
		}
		if rawDone != nil {
			rawDone()
		}
		return true
	}

	// The detached window's own menu bar owns keyboard navigation while it
	// is focused (F10) or has a dropdown open, and F10 itself always goes
	// to the bar so it can toggle that focus - matching the desktop bar.
	// Resolved ONCE and used everywhere below: the menu key, the title bar's
	// whole geometry model, Tab either way, and the frame commands. Feeding
	// the sequence processor the same keystroke at each of those points would
	// advance a chord's prefix once per check.
	cmd := w.KeyCommand(event.Key)

	if mb != nil {
		// The help key goes straight to Help on this window's own bar; an app
		// with no Help menu falls through to the plain menu key below.
		if cmd == core.CmdAppHelp {
			if h, ok := mb.(interface{ OpenHelpMenu() bool }); ok && h.OpenHelpMenu() {
				return true
			}
			cmd = core.CmdAppMenu
		}
		menuActive := mb.HasFocus() || cmd == core.CmdAppMenu
		if o, ok := mb.(interface{ IsMenuOpen() bool }); ok && o.IsMenuOpen() {
			menuActive = true
		}
		if menuActive && mb.HandleKeyPress(event) {
			return true
		}
	}

	// The detached window's own menu bar services its app shortcuts
	// (Cut/Copy/Paste, Close Window, Quit, ...) globally - checked before
	// the focused trinket sees the key, matching the desktop bar while
	// docked. A detached main window carries its own bar (mb); a torn-off
	// child carries no chrome but borrows its app's bar via the resolver.
	if mb != nil {
		// An item that names a COMMAND is matched against the command this
		// window already resolved, above -- the key is fed to the context once
		// per keystroke, and asking again would advance a chord's prefix twice.
		if ac, ok := mb.(interface{ ActivateCommand(string) bool }); ok &&
			cmd != "" && ac.ActivateCommand(cmd) {
			return true
		}
		if sc, ok := mb.(interface {
			HandleShortcut(core.KeyPressEvent) bool
		}); ok && sc.HandleShortcut(event) {
			return true
		}
		// ...and its chord ACCELERATORS, which are not item shortcuts and do
		// not go through HandleShortcut. The bar publishes them into this
		// window's context, so this is the only place that reads them back --
		// above the focused trinket, exactly where the desktop resolves them
		// for a docked window. Without it a window carrying its own bar drew
		// its accelerators lit and then let the focused trinket eat the chord.
		if bar, ok := mb.(interface {
			ActivateAcceleratorSequence(string) bool
		}); ok {
			// The context knows the whole sequence, which is what carries a
			// multi-key chord: it holds the prefix between keystrokes and
			// reports the lot when it lands.
			if ctx := w.KeyContext(); ctx != nil &&
				ctx.Resolve(event.Key) == core.CommandAppAccelerator &&
				bar.ActivateAcceleratorSequence(ctx.MatchedSequence()) {
				return true
			}
			// ...and the bar itself for a single-key one, which needs no
			// prefix held and so does not need to have been published yet.
			// This is what makes the very first keystroke work, before
			// anything has painted and put the accelerators in the context.
			if bar.ActivateAcceleratorSequence(event.Key) {
				return true
			}
		}
	}
	if shortcutResolver != nil && shortcutResolver(event) {
		return true
	}

	// If title bar has focus, handle title bar keys
	if titleFocus != TitleFocusNone {
		if w.handleTitleBarKey(event, cmd) {
			return true
		}
	}

	// Check if this is a Tab or Shift+Tab event
	isShiftTab := cmd == core.CmdFocusPrior
	isTab := cmd == core.CmdFocusNext

	// For Tab/Shift+Tab, first give the focused trinket a chance to handle it.
	// This is critical for containers like MDIPane that manage their own Tab navigation.
	// If the focused trinket handles it, we're done.
	if (isTab || isShiftTab) && fm != nil {
		focused := fm.FocusedTrinket()
		if focused != nil && focused.HandleKeyPress(event) {
			return true
		}

		// Focused trinket didn't handle it.
		// For Shift+Tab at first trinket, enter title bar (blur item if enabled, otherwise title).
		if isShiftTab {
			chain := fm.FocusChain()
			for _, trinket := range chain {
				if trinket.IsVisible() && trinket.IsEnabled() {
					if trinket == focused {
						// At first trinket, enter blur item if enabled, otherwise title bar
						if w.hasKeyboardBlurEnabled() {
							w.SetTitleFocus(TitleFocusBlur)
						} else {
							w.SetTitleFocus(TitleFocusTitle)
						}
						fm.ClearFocus()
						return true
					}
					break // Not at first trinket
				}
			}
			// Not at first trinket, move to previous
			return fm.FocusPrevious()
		}

		// Regular Tab - check if at last trinket
		if isTab {
			chain := fm.FocusChain()
			// Find the last visible/enabled trinket
			var lastTrinket core.Trinket
			for _, trinket := range chain {
				if trinket.IsVisible() && trinket.IsEnabled() {
					lastTrinket = trinket
				}
			}
			if focused == lastTrinket && w.hasKeyboardBlurEnabled() {
				// At last trinket with blur enabled, go to blur item
				w.SetTitleFocus(TitleFocusBlur)
				fm.ClearFocus()
				return true
			}
			// Not at last trinket, or blur not enabled - move to next
			return fm.FocusNext()
		}
	}

	// For non-Tab keys, use focus manager
	if fm != nil {
		if fm.HandleKeyPress(event) {
			return true
		}
	}

	// Handle window-specific keys
	switch cmd {
	case core.CmdWindowClose:
		w.Close()
		return true
	case core.CmdAppQuit:
		// Ending the APPLICATION, not this window. The desktop is the only
		// thing that knows which app a window belongs to, so the window asks
		// it; a window with no desktop above it has no application to end and
		// lets the key fall through.
		return w.quitOwningApplication()
	case core.CmdWindowMaximizeToggle:
		// Through the maximize-request handler when one is set, exactly as the
		// titlebar button does: a torn/solo host maps maximize onto its OS
		// window (zoom to the work area), and calling Maximize() directly here
		// would flip the window's state while leaving the host un-zoomed and
		// the surface its old size — a half-maximized window that paints no
		// border and can still be edge-resized.
		w.mu.RLock()
		maxHandler := w.onMaximizeRequest
		w.mu.RUnlock()
		if maxHandler != nil {
			maxHandler()
		} else if w.IsMaximized() {
			w.Restore()
		} else {
			w.Maximize()
		}
		return true
	}

	return false
}

// HandleMousePress handles mouse clicks.
func (w *Window) HandleMousePress(event core.MousePressEvent) bool {
	w.mu.RLock()
	content := w.content
	flags := w.flags
	state := w.state
	w.mu.RUnlock()

	// The titlebar chrome sits inside the frame border (offset down by the
	// border), so the titlebar band runs [0, border+RowH) in window-local
	// coordinates - not [0, RowH). Missing the border here would leave the
	// bottom of the visible titlebar (and the bottoms of the titlebar
	// buttons) routed to content. Maximized has no side border. RowH is the
	// kit's (possibly scaled) title row height — UnitsPerCellHeight at scale 1.0.
	titleBand := w.titleBarMetrics().RowH
	if state != WindowStateMaximized {
		_, by := w.frameBorder()
		titleBand += by
	}

	// Check for title bar clicks
	if hasTitleBar(flags, state) && event.Y < titleBand {
		// Check if clicking on a button
		button := w.buttonAtPosition(event.X, event.Y)
		if button != TitleButtonNone {
			// Start tracking button press - don't trigger yet
			w.mu.Lock()
			w.pressedButton = button
			w.buttonHovered = true
			w.mu.Unlock()
			w.Update()
			return true
		}

		// Title bar click outside buttons - return false to let WindowManager handle drag
		return false
	}

	// Detached-window chrome (menu bar / status bar) claims the click
	// before content, and an open menu claims all clicks.
	if target, r, owns := w.chromeMouseTarget(event.X, event.Y); owns {
		outer, interior := w.denominations()
		le := event
		le.X, le.Y = chromeLocal(event.X, event.Y, r, outer, interior)
		target.HandleMousePress(le)
		return true
	}

	// A click below the title bar moves keyboard focus into the
	// content: drop any title-bar keyboard focus (set by Tab/Shift+Tab)
	// so it stops intercepting keys and Tab resumes from the clicked
	// control rather than the title-bar element.
	if w.TitleFocus() != TitleFocusNone {
		w.SetTitleFocus(TitleFocusNone)
	}

	// Pass to content (converted into the interior denomination)
	if content != nil {
		contentBounds := w.contentBounds()
		outer, interior := w.denominations()
		localEvent := event
		localEvent.X = core.ExchangeX(event.X-contentBounds.X, outer, interior)
		localEvent.Y = core.ExchangeY(event.Y-contentBounds.Y, outer, interior)
		if content.HandleMousePress(localEvent) {
			// The content owns this gesture until release: moves and the
			// release route to it even over the window's own chrome.
			w.mu.Lock()
			w.contentMousePressed = true
			w.mu.Unlock()
			return true
		}
	}

	return true // Consume click
}

// HandleMouseMove handles mouse movement.
func (w *Window) HandleMouseMove(event core.MouseMoveEvent) bool {
	w.mu.RLock()
	content := w.content
	pressedButton := w.pressedButton
	contentCaptured := w.contentMousePressed
	w.mu.RUnlock()

	// A gesture the CONTENT captured (press landed there, release pending)
	// keeps receiving every move — even with the pointer over the window's
	// own chrome. Without this, dragging a selection down onto the status
	// bar froze the drag (and its edge autoscroll) at the last content row,
	// while dragging up past the title band kept working: the chrome-first
	// routing below only swallowed the downward path.
	if contentCaptured && content != nil {
		if handler, ok := content.(interface {
			HandleMouseMove(core.MouseMoveEvent) bool
		}); ok {
			contentBounds := w.contentBounds()
			outer, interior := w.denominations()
			localEvent := event
			localEvent.X = core.ExchangeX(event.X-contentBounds.X, outer, interior)
			localEvent.Y = core.ExchangeY(event.Y-contentBounds.Y, outer, interior)
			handler.HandleMouseMove(localEvent)
		}
		return true
	}

	// If tracking a button press, update hover state
	if pressedButton != TitleButtonNone {
		currentButton := w.buttonAtPosition(event.X, event.Y)
		newHovered := currentButton == pressedButton

		w.mu.Lock()
		if w.buttonHovered != newHovered {
			w.buttonHovered = newHovered
			w.mu.Unlock()
			w.Update()
		} else {
			w.mu.Unlock()
		}
		return true // Capture mouse while button is pressed
	}

	// Plain hover over a titlebar button (no press in progress). The
	// manager/pane forward plain moves to the topmost window under the
	// pointer (and send an out-of-bounds move when the pointer is over a
	// resize edge, so nothing here hovers where a press would resize), so
	// this runs for inactive windows too. Update() schedules the repaint;
	// don't consume the move so chrome/content under the pointer still gets
	// it.
	// Hover is a no-button affordance: while a button is held (a drag begun
	// elsewhere passing over the title bar) don't light a button; clear any
	// set before the button went down.
	newHovered := TitleButtonNone
	if event.Buttons == 0 {
		newHovered = w.buttonAtPosition(event.X, event.Y)
	}
	w.mu.Lock()
	hoverChanged := w.hoveredButton != newHovered
	if hoverChanged {
		w.hoveredButton = newHovered
	}
	w.mu.Unlock()
	if hoverChanged {
		w.Update()
	}

	// Chrome (open-menu drag-select / hover) before content.
	target, r, owns := w.chromeMouseTarget(event.X, event.Y)
	var chromeTarget core.Trinket
	if owns {
		chromeTarget = target
	}
	// When the pointer leaves a chrome trinket (e.g. the menu bar), send it
	// an out-of-bounds move so its hover doesn't stick - chromeMouseTarget
	// only forwards while the pointer is actually over the chrome.
	w.mu.Lock()
	prevChrome := w.lastChromeHover
	w.lastChromeHover = chromeTarget
	w.mu.Unlock()
	if prevChrome != nil && prevChrome != chromeTarget {
		if h, ok := prevChrome.(interface {
			HandleMouseMove(core.MouseMoveEvent) bool
		}); ok {
			h.HandleMouseMove(core.MouseMoveEvent{X: -1, Y: -1})
		}
	}
	if owns {
		if h, ok := target.(interface {
			HandleMouseMove(core.MouseMoveEvent) bool
		}); ok {
			outer, interior := w.denominations()
			le := event
			le.X, le.Y = chromeLocal(event.X, event.Y, r, outer, interior)
			h.HandleMouseMove(le)
		}
		return true
	}

	// Forward to content
	if content != nil {
		if handler, ok := content.(interface {
			HandleMouseMove(core.MouseMoveEvent) bool
		}); ok {
			contentBounds := w.contentBounds()
			outer, interior := w.denominations()
			localEvent := event
			localEvent.X = core.ExchangeX(event.X-contentBounds.X, outer, interior)
			localEvent.Y = core.ExchangeY(event.Y-contentBounds.Y, outer, interior)
			if handler.HandleMouseMove(localEvent) {
				return true
			}
		}
	}

	return false
}

// HandleMouseRelease handles mouse button release.
func (w *Window) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	w.mu.RLock()
	content := w.content
	pressedButton := w.pressedButton
	buttonHovered := w.buttonHovered
	minHandler := w.onMinimizeRequest
	maxHandler := w.onMaximizeRequest
	contentCaptured := w.contentMousePressed
	w.mu.RUnlock()

	// The release ends a content-captured gesture and belongs to the
	// content, wherever the pointer sits — the chrome must not intercept it.
	if contentCaptured {
		w.mu.Lock()
		w.contentMousePressed = false
		w.mu.Unlock()
		if content != nil {
			if h, ok := content.(interface {
				HandleMouseRelease(core.MouseReleaseEvent) bool
			}); ok {
				contentBounds := w.contentBounds()
				outer, interior := w.denominations()
				localEvent := event
				localEvent.X = core.ExchangeX(event.X-contentBounds.X, outer, interior)
				localEvent.Y = core.ExchangeY(event.Y-contentBounds.Y, outer, interior)
				h.HandleMouseRelease(localEvent)
			}
		}
		return true
	}

	// If tracking a button press, handle release
	if pressedButton != TitleButtonNone {
		// Clear pressed state
		w.mu.Lock()
		w.pressedButton = TitleButtonNone
		w.buttonHovered = false
		w.mu.Unlock()
		w.Update()

		// Only trigger action if mouse is still on the button
		if buttonHovered {
			switch pressedButton {
			case TitleButtonClose:
				w.Close()
			case TitleButtonMinimize:
				if minHandler != nil {
					minHandler()
				} else {
					w.Minimize()
				}
			case TitleButtonMaximize:
				if maxHandler != nil {
					maxHandler()
				} else if w.IsMaximized() {
					w.Restore()
				} else {
					w.Maximize()
				}
			case TitleButtonTear:
				// Click on the %/# handle: toggle detach/dock. In the
				// detached host this is the re-dock path.
				w.requestTear()
			}
		}
		return true
	}

	// Chrome (menu drag-select release) before content.
	if target, r, owns := w.chromeMouseTarget(event.X, event.Y); owns {
		if h, ok := target.(interface {
			HandleMouseRelease(core.MouseReleaseEvent) bool
		}); ok {
			outer, interior := w.denominations()
			le := event
			le.X, le.Y = chromeLocal(event.X, event.Y, r, outer, interior)
			h.HandleMouseRelease(le)
		}
		return true
	}

	// Forward to content
	if content != nil {
		if handler, ok := content.(interface {
			HandleMouseRelease(core.MouseReleaseEvent) bool
		}); ok {
			contentBounds := w.contentBounds()
			outer, interior := w.denominations()
			localEvent := event
			localEvent.X = core.ExchangeX(event.X-contentBounds.X, outer, interior)
			localEvent.Y = core.ExchangeY(event.Y-contentBounds.Y, outer, interior)
			if handler.HandleMouseRelease(localEvent) {
				return true
			}
		}
	}

	return false
}

// SetBounds sets the window bounds and triggers layout.
func (w *Window) SetBounds(bounds core.UnitRect) {
	oldSize := w.Bounds().Size()
	w.TrinketBase.SetBounds(bounds)
	newSize := bounds.Size()
	// Manually call our HandleResize since embedded SetBounds won't do it
	if oldSize != newSize {
		w.HandleResize(oldSize, newSize)
	}
}

// HandleResize is called when the window is resized.
func (w *Window) HandleResize(oldSize, newSize core.UnitSize) {
	w.layoutContent()

	w.mu.RLock()
	handler := w.onResize
	w.mu.RUnlock()

	if handler != nil {
		handler(newSize.Width, newSize.Height)
	}
}

// SizeHint returns the preferred size for the window.
func (w *Window) SizeHint() core.UnitSize {
	w.mu.RLock()
	content := w.content
	flags := w.flags
	w.mu.RUnlock()

	metrics := w.frameCellMetrics()

	var width, height core.Unit

	if content != nil {
		// Content hints are denominated in the interior currency;
		// convert to the window's own (outer) currency.
		outer, interior := w.denominations()
		hint := core.ExchangeSize(content.SizeHint(), interior, outer)
		width = hint.Width
		height = hint.Height
	}

	// Add frame
	if flags&WindowFlagFrameless == 0 {
		width += metrics.UnitsPerCellWidth * 2   // Left and right borders
		height += metrics.UnitsPerCellHeight * 2 // Top and bottom borders
	}

	// Ensure minimum size
	w.mu.RLock()
	if width < w.minWidth {
		width = w.minWidth
	}
	if height < w.minHeight {
		height = w.minHeight
	}
	w.mu.RUnlock()

	return core.UnitSize{Width: width, Height: height}
}

// verify Window implements Container
var _ core.Container = (*Window)(nil)

// HandleMouseWheel forwards a wheel event to the content (in the
// window's interior denomination).
func (w *Window) HandleMouseWheel(event core.MouseWheelEvent) bool {
	w.mu.RLock()
	content := w.content
	mb := w.menuBar
	w.mu.RUnlock()

	// A detached window's own menu bar claims wheel/pan over its row (to
	// scroll an overflowing bar), and an open dropdown claims it wherever
	// the pointer is - mirroring the desktop bar's behaviour.
	if mb != nil {
		if wh, ok := mb.(interface {
			HandleMouseWheel(core.MouseWheelEvent) bool
		}); ok {
			open := false
			if o, isOpen := mb.(interface{ IsMenuOpen() bool }); isOpen {
				open = o.IsMenuOpen()
			}
			if r := w.menuBarRect(); open || (!r.IsEmpty() && r.Contains(core.UnitPoint{X: event.X, Y: event.Y})) {
				outer, interior := w.denominations()
				le := event
				le.X, le.Y = chromeLocal(event.X, event.Y, r, outer, interior)
				if wh.HandleMouseWheel(le) {
					return true
				}
			}
		}
	}

	if content == nil {
		return false
	}
	handler, ok := content.(interface {
		HandleMouseWheel(core.MouseWheelEvent) bool
	})
	if !ok {
		return false
	}
	contentBounds := w.contentBounds()
	outer, interior := w.denominations()
	local := event
	local.X = core.ExchangeX(event.X-contentBounds.X, outer, interior)
	local.Y = core.ExchangeY(event.Y-contentBounds.Y, outer, interior)
	return handler.HandleMouseWheel(local)
}

// keyContextConsumer is a menu bar that forms accelerators. Declared
// structurally so a window need not import the trinket package to hand its bar
// a context.
type keyContextConsumer interface {
	SetAcceleratorChord(string)
	SetKeyContext(*core.KeyContext)
}

// windowUIState reports which situation this window's keyboard is in. A title
// bar is a MODE of the window rather than a trinket with focus, which is why
// the state and not the focus chain is what a context is keyed on.
func (w *Window) windowUIState() core.UIState {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.titleFocus == TitleFocusTitle {
		return core.StateTitleBarFocused
	}
	return core.StateNormal
}

// refreshKeyContext rebuilds this window's context for its current state and
// hands it to the window's own menu bar, if it has one.
//
// A window builds its own rather than sharing the desktop's, so a change here
// cannot stale anything over there: the commonest structural event of all --
// focus moving between windows -- costs no rebuild at all, because each
// window's context is already built and still valid.
func (w *Window) refreshKeyContext() {
	reg := w.FocusedKeyRegistry()
	state := w.windowUIState()
	ctx := reg.BuildStateContext(state)

	w.mu.Lock()
	w.keyContext = ctx
	w.keyContextReg = reg
	w.keyContextRev = reg.Revision()
	w.keyContextState = state
	mb := w.menuBar
	w.mu.Unlock()

	if c, ok := mb.(keyContextConsumer); ok {
		c.SetAcceleratorChord(core.AcceleratorChord())
		c.SetKeyContext(ctx)
	}
}

// FocusedKeyRegistry is the registry in force for whatever holds the focus in
// this window (core.KeyRegistryFocuser). A window resolves its own frame
// commands through it, so they stand down when the focus is inside a trinket
// that took the keyboard on its own terms.
func (w *Window) FocusedKeyRegistry() *core.KeyRegistry {
	return core.FindFocusedKeyRegistry(w)
}

// KeyContext returns the set of actions this window currently offers,
// rebuilding it first if the keymap behind it has moved on: the focus landing
// inside a trinket with its own registry changes which keymap answers here,
// and does so without changing any registry's revision.
func (w *Window) KeyContext() *core.KeyContext {
	w.mu.RLock()
	ctx, reg, rev, state := w.keyContext, w.keyContextReg, w.keyContextRev, w.keyContextState
	w.mu.RUnlock()

	if ctx != nil && reg == w.FocusedKeyRegistry() &&
		rev == reg.Revision() && state == w.windowUIState() {
		return ctx
	}
	w.refreshKeyContext()

	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.keyContext
}
