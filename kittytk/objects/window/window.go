// Package window provides windowing support for KittyTK.
package window

import (
	"strings"
	"sync"

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
	WindowFlagNone       WindowFlags = 0
	WindowFlagFrameless  WindowFlags = 1 << iota // No window frame
	WindowFlagNoTitle                            // No title bar
	WindowFlagNoResize                           // Cannot be resized
	WindowFlagNoMove                             // Cannot be moved
	WindowFlagNoClose                            // No close button
	WindowFlagNoMinimize                         // No minimize button
	WindowFlagNoMaximize                         // No maximize button
	WindowFlagStaysOnTop                         // Always on top
	WindowFlagTearable                           // Shows the %/# tear-off handle; window may detach
)

// windowCornerRadius is the corner radius (in units) of the graphical
// window frame's single rounded-rect surface. Kept below the frame's
// one-cell inset (8 units) so titlebar buttons and content never
// overlap the curve; cell surfaces ignore it entirely.
const windowCornerRadius core.Unit = 6

// FrameCornerRadius reports the graphical frame's corner radius in
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

	// resizeHoverRects are window-local rectangles (one per hovered resize
	// edge, two for a corner) that the frame highlights while the pointer
	// is over a size-sensitive edge. Set by the window manager on hover.
	resizeHoverRects []core.UnitRect

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
	popupController       core.PopupController     // Popup controller for ComboBox etc.

	// Button press tracking
	pressedButton TitleButton // Currently pressed titlebar button
	buttonHovered bool        // Whether mouse is still over the pressed button
	hoveredButton TitleButton // Titlebar button under the pointer (plain hover)

	// Title bar keyboard focus
	titleFocus        TitleFocus    // Which title bar element has keyboard focus
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

// hasTitleBar reports whether the window shows a title bar, and thus whether its
// title-bar hit regions are live: the caption buttons, drag-to-move/detach, and
// double-click-to-restore. A NoTitle or Frameless window has none, so those
// clicks must not be caught (the top row is ordinary content there).
func hasTitleBar(flags WindowFlags) bool {
	return flags&WindowFlagNoTitle == 0 && flags&WindowFlagFrameless == 0
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

	w.normalBounds = w.Bounds()
	w.state = WindowStateMinimized
	handler := w.onStateChange
	w.mu.Unlock()

	w.Update()

	if handler != nil {
		handler(WindowStateMinimized)
	}
}

// Restore restores the window from maximized or minimized state.
func (w *Window) Restore() {
	w.mu.Lock()
	if w.state == WindowStateNormal {
		w.mu.Unlock()
		return
	}

	bounds := w.normalBounds
	w.state = WindowStateNormal
	w.pressedButton = TitleButtonNone // Reset pressed button state
	handler := w.onStateChange
	w.mu.Unlock()

	w.SetBounds(bounds)

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

// Close attempts to close the window.
func (w *Window) Close() bool {
	w.mu.RLock()
	handler := w.onClose
	closeComplete := w.onCloseComplete
	observers := append([]func(){}, w.onClosedObservers...)
	title := w.title
	w.mu.RUnlock()

	if handler != nil && !handler() {
		return false
	}

	// Announce window closing for accessibility
	if am := core.FindAccessibilityManager(w); am != nil {
		am.AnnouncePolite(title + ", closed")
	}

	// Close child windows first
	for _, child := range w.ChildWindows() {
		child.Close()
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

	return true
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
	}
	w.layoutContent()
	w.Update()
}

// WindowMenuBar returns the window's own menu bar (the chrome a detached
// main window hosts), or nil. Used by the desktop to route a torn-off
// child window's shortcuts through its app's menu bar.
func (w *Window) WindowMenuBar() core.Trinket {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.menuBar
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
	metrics := w.frameCellMetrics()
	if mb != nil && mbVis {
		menuTop = metrics.CellHeight
	}
	if sb != nil && sbVis {
		statusBottom = metrics.CellHeight
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
		// is reserved only when the window actually has a title bar - a NoTitle
		// or Frameless maximized window fills the whole surface (being maximized
		// is independent of having a title bar or a frame).
		top := core.Unit(0)
		if flags&WindowFlagNoTitle == 0 && flags&WindowFlagFrameless == 0 {
			top = metrics.CellHeight
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
		b := core.FindFrameBorderUnits(w)
		top := b + metrics.CellHeight
		if flags&WindowFlagNoTitle != 0 {
			top = b
		}
		cb = core.UnitRect{X: b, Y: top, Width: bounds.Width - 2*b, Height: bounds.Height - top - b}
	default:
		// Cell frames: the border occupies a full cell on every side.
		left, top, right, bottom := metrics.CellWidth, metrics.CellHeight, metrics.CellWidth, metrics.CellHeight
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
	top := w.frameCellMetrics().CellHeight
	// Bottom edge of the surface in menu-bar-local coordinates.
	bottom := b.Height - mbr.Y
	if bottom < top {
		bottom = top
	}
	return core.UnitRect{Y: top, Height: bottom - top}
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
		// window has one; a NoTitle or Frameless maximized window has no frame
		// at all (no title, no border) - being maximized no longer implies a
		// title bar, and Frameless means no frame in any state.
		if flags&WindowFlagNoTitle == 0 && flags&WindowFlagFrameless == 0 {
			w.paintMaximizedFrame(p, bounds, metrics, title, titleStyle, frameStyle, frameBorder)
		}
	} else if flags&WindowFlagFrameless == 0 {
		// Normal frame
		w.paintNormalFrame(p, bounds, metrics, title, titleStyle, frameStyle, frameBorder, flags)
	}

	// Paint content (in the window's interior denomination)
	outer, interior := w.denominations()
	localBounds := core.UnitRect{Width: bounds.Width, Height: bounds.Height}
	graphicalFrame := state != WindowStateMaximized && flags&WindowFlagFrameless == 0 &&
		core.FindGraphicalFrames(w)
	if content != nil {
		contentBounds := w.contentBounds()
		contentBase := p
		if graphicalFrame {
			// Edge-to-edge content stays inside the frame's rounded
			// outline (bottom corners in particular).
			contentBase = p.WithRoundedClipRegion(localBounds, windowCornerRadius)
		}
		contentPainter := contentBase.WithOffset(contentBounds.X, contentBounds.Y).
			WithClip(core.UnitRect{Width: contentBounds.Width, Height: contentBounds.Height}).
			WithDenomination(outer, interior)
		content.Paint(contentPainter)
	}
	if graphicalFrame {
		// Content reaches the window edges, so the hairline border is
		// re-stroked over it - the frame stays visible on all sides.
		frameStyle := w.GetScheme().GetWindowBorder(focused || isPassive)
		if frameBorder == style.BorderHeavy {
			// Single border: the outer band disappears into the window
			// background, then a thin inner line in the active border color
			// sits just inside it.
			bg := w.GetScheme().GetWindowBG(w.renderActive())
			p.StrokeRoundedRect(localBounds, windowCornerRadius, frameBorder, frameStyle.WithFg(bg))
			w.paintSingleBorderInner(p, localBounds)
		} else {
			p.StrokeRoundedRect(localBounds, windowCornerRadius, frameBorder, frameStyle)
		}
	}

	// Paint child windows (within the content area, clipped)
	if len(w.ChildWindows()) > 0 {
		contentBounds := w.contentBounds()
		// Create a painter clipped to the content area
		contentPainter := p.WithOffset(contentBounds.X, contentBounds.Y).
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
	w.paintChrome(p, outer, interior)

	// Resize-edge hover highlight: translucent white bands along the
	// size-sensitive edge(s) under the pointer, clipped to the frame's
	// rounded corners.
	w.paintResizeHover(p, localBounds)
}

// resizeHoverAlpha is the opacity of the resize-edge hover highlight.
const resizeHoverAlpha = 0.25

// SetResizeHoverRects sets the window-local rectangles highlighted while
// the pointer hovers a resize edge (empty clears the highlight). Returns
// true when the set changed, so the caller can repaint only on change.
func (w *Window) SetResizeHoverRects(rects []core.UnitRect) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if sameRects(w.resizeHoverRects, rects) {
		return false
	}
	w.resizeHoverRects = rects
	return true
}

// ResizeHoverRects returns the window-local resize-edge highlight
// rectangles currently set (nil when the overlay is off).
func (w *Window) ResizeHoverRects() []core.UnitRect {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.resizeHoverRects
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
			if cur != trinket || !isContainer {
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

// paintResizeHover fills the hovered resize edges with a translucent white
// band, clipped to the window's rounded corner radius. No-op on cell
// surfaces (FillRectPixelsAlpha returns false there).
func (w *Window) paintResizeHover(p *core.Painter, localBounds core.UnitRect) {
	w.mu.RLock()
	rects := w.resizeHoverRects
	w.mu.RUnlock()
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
			255, 255, 255, resizeHoverAlpha)
	}
}

// paintChrome paints the detached window's menu bar and status bar in
// their reserved rows, and the menu bar's dropdown on top of content.
func (w *Window) paintChrome(p *core.Painter, outer, interior core.CellMetrics) {
	w.mu.RLock()
	mb, sb := w.menuBar, w.statusBar
	w.mu.RUnlock()

	if r := w.menuBarRect(); mb != nil && !r.IsEmpty() {
		mb.SetBounds(core.UnitRect{Width: r.Width, Height: r.Height})
		mp := p.WithOffset(r.X, r.Y).
			WithClip(core.UnitRect{Width: r.Width, Height: r.Height}).
			WithDenomination(outer, interior)
		mb.Paint(mp)
	}
	if r := w.statusBarRect(); sb != nil && !r.IsEmpty() {
		sb.SetBounds(core.UnitRect{Width: r.Width, Height: r.Height})
		sp := p.WithOffset(r.X, r.Y).
			WithClip(core.UnitRect{Width: r.Width, Height: r.Height}).
			WithDenomination(outer, interior)
		sb.Paint(sp)
	}
	// The menu bar's dropdown paints last, unclipped, so it overlays the
	// window content below the bar.
	if r := w.menuBarRect(); mb != nil && !r.IsEmpty() {
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

// paintFocusedTitleDecoration draws the keyboard-focused title as
// "< title >" centered in innerWidth over a highlight foundation. It
// shapes the whole thing as ONE run rather than cell brackets abutting a
// proportional title: at a fractional font size the two rates diverge, so
// placing the closing bracket at the title's re-snapped unit end left it
// drifting right of where the glyphs actually finish. A single run ends
// the bracket exactly on the title. On a cell surface each character still
// occupies its own cell, so the classic look is unchanged.
func (w *Window) paintFocusedTitleDecoration(p *core.Painter, innerWidth core.Unit, title string, s style.CellStyle, font *core.Font, cellHeight core.Unit) {
	decorated := "< " + title + " >"
	totalWidth := font.MeasureText(decorated)
	startX := (innerWidth - totalWidth) / 2
	if startX < 0 {
		startX = 0
	}
	p.FillRect(core.UnitRect{X: startX, Width: totalWidth, Height: cellHeight}, ' ', s)
	p.DrawText(startX, 0, decorated, s, font)
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

	font := w.EffectiveFont()

	// Fill title bar background
	titleRect := core.UnitRect{
		X:      0,
		Y:      0,
		Width:  bounds.Width,
		Height: metrics.CellHeight,
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

	// Draw window controls on the LEFT: [x][.][^] or [x][.][o]
	// These are decorative buttons - use cell-based sizing (3 cells each)
	buttonWidth := metrics.CellWidth * 3
	controlX := core.Unit(0)
	if flags&WindowFlagNoClose == 0 {
		isFocused := titleFocus == TitleFocusClose
		isPressed := pressedButton == TitleButtonClose && buttonHovered
		isHovered := hoveredButton == TitleButtonClose && !isPressed && p.Graphical()
		btnStyle := scheme.GetTitleBarButtonState(buttonActive, isFocused, isHovered, isPressed)
		p.DrawCell(controlX, 0, '[', btnStyle)
		p.DrawCell(controlX+metrics.CellWidth, 0, 'x', btnStyle)
		p.DrawCell(controlX+metrics.CellWidth*2, 0, ']', btnStyle)
		controlX += buttonWidth
	}
	if flags&WindowFlagNoMinimize == 0 {
		isFocused := titleFocus == TitleFocusMinimize
		isPressed := pressedButton == TitleButtonMinimize && buttonHovered
		isHovered := hoveredButton == TitleButtonMinimize && !isPressed && p.Graphical()
		btnStyle := scheme.GetTitleBarButtonState(buttonActive, isFocused, isHovered, isPressed)
		p.DrawCell(controlX, 0, '[', btnStyle)
		p.DrawCell(controlX+metrics.CellWidth, 0, '.', btnStyle)
		p.DrawCell(controlX+metrics.CellWidth*2, 0, ']', btnStyle)
		controlX += buttonWidth
	}
	if canMaximize(flags) {
		isFocused := titleFocus == TitleFocusMaximize
		isPressed := pressedButton == TitleButtonMaximize && buttonHovered
		isHovered := hoveredButton == TitleButtonMaximize && !isPressed && p.Graphical()
		btnStyle := scheme.GetTitleBarButtonState(buttonActive, isFocused, isHovered, isPressed)
		var icon rune
		if state == WindowStateMaximized {
			icon = 'o' // Restore icon
		} else {
			icon = '^' // Maximize icon
		}
		p.DrawCell(controlX, 0, '[', btnStyle)
		p.DrawCell(controlX+metrics.CellWidth, 0, icon, btnStyle)
		p.DrawCell(controlX+metrics.CellWidth*2, 0, ']', btnStyle)
		controlX += buttonWidth
	}

	// Tear-off handle floats immediately left of the (centered) title, but
	// is omitted while the title itself is focused - the '< >' brackets
	// stand in for it - so it isn't shoved aside; it returns on the next
	// Tab / Shift+Tab focus change.
	if titleFocus != TitleFocusTitle {
		tearTitleW := font.MeasureText(title)
		controlX = w.paintTearHandle(p, scheme, titleStyle, metrics, controlX, bounds.Width, tearTitleW, buttonActive, titleFocus)
	}

	// Draw title text centered, with angle brackets and cyan bg if title has keyboard focus
	if titleFocus == TitleFocusTitle {
		// Title has focus - draw with decorative angle brackets, as one run.
		titleDisplayStyle := scheme.GetTitleBarButton(focused, true, false)
		w.paintFocusedTitleDecoration(p, titleRect.Width, title, titleDisplayStyle, font, metrics.CellHeight)
	} else {
		rightLimit := bounds.Width
		if titleFocus == TitleFocusBlur {
			rightLimit = bounds.Width - buttonWidth
		}
		w.paintTitleText(p, title, titleStyle, font, metrics, controlX, rightLimit, bounds.Width)
	}

	// Draw blur button on far right when blur item is focused
	// This is a decorative button - use cell-based sizing (3 cells)
	if titleFocus == TitleFocusBlur {
		blurBtnStyle := scheme.GetTitleBarButton(focused, true, false) // Focused button style
		blurX := bounds.Width - buttonWidth                            // Position at far right
		p.DrawCell(blurX, 0, '[', blurBtnStyle)
		p.DrawCell(blurX+metrics.CellWidth, 0, '~', blurBtnStyle)
		p.DrawCell(blurX+metrics.CellWidth*2, 0, ']', blurBtnStyle)
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
func (w *Window) paintSingleBorderInner(p *core.Painter, localBounds core.UnitRect) {
	b := core.FindFrameBorderUnits(w)
	inner := core.UnitRect{
		X:      b,
		Y:      b,
		Width:  localBounds.Width - 2*b,
		Height: localBounds.Height - 2*b,
	}
	radius := windowCornerRadius - b
	if radius < 0 {
		radius = 0
	}
	th := p.UnitsToPx(1) // match the tabbed control's tab-stroke weight
	if th < 1 {
		th = 1
	}
	p.StrokeRoundedRectWeight(inner, radius, th, w.GetScheme().GetWindowBorder(true))
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
		p.DrawCell(localBounds.Width-metrics.CellWidth, 0, topRight, activeFrameStyle)
		p.DrawCell(0, localBounds.Height-metrics.CellHeight, bottomLeft, activeFrameStyle)
		p.DrawCell(localBounds.Width-metrics.CellWidth, localBounds.Height-metrics.CellHeight, bottomRight, activeFrameStyle)

		// Draw top edge - first and last chars adjacent to corners in active color, rest dashed
		for x := metrics.CellWidth; x < localBounds.Width-metrics.CellWidth; x += metrics.CellWidth {
			if x == metrics.CellWidth || x == localBounds.Width-2*metrics.CellWidth {
				// Adjacent to corner - use active style with normal horizontal line
				p.DrawCell(x, 0, horizLine, activeFrameStyle)
			} else {
				p.DrawCell(x, 0, horizDash, blurFrameStyle)
			}
		}

		// Draw bottom edge - first and last chars adjacent to corners in active color, rest dashed
		for x := metrics.CellWidth; x < localBounds.Width-metrics.CellWidth; x += metrics.CellWidth {
			if x == metrics.CellWidth || x == localBounds.Width-2*metrics.CellWidth {
				// Adjacent to corner - use active style with normal horizontal line
				p.DrawCell(x, localBounds.Height-metrics.CellHeight, horizLine, activeFrameStyle)
			} else {
				p.DrawCell(x, localBounds.Height-metrics.CellHeight, horizDash, blurFrameStyle)
			}
		}

		// Draw left edge - all dashed
		for y := metrics.CellHeight; y < localBounds.Height-metrics.CellHeight; y += metrics.CellHeight {
			p.DrawCell(0, y, vertDash, blurFrameStyle)
		}

		// Draw right edge - all dashed
		for y := metrics.CellHeight; y < localBounds.Height-metrics.CellHeight; y += metrics.CellHeight {
			p.DrawCell(localBounds.Width-metrics.CellWidth, y, vertDash, blurFrameStyle)
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
		b := core.FindFrameBorderUnits(w)
		titleRect := core.UnitRect{Width: localBounds.Width, Height: b + metrics.CellHeight}
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
	font := w.EffectiveFont()

	// Draw title if enabled
	if flags&WindowFlagNoTitle == 0 {
		// The titlebar chrome (title text + buttons) sits INSIDE the frame
		// border: shift it in by the border and use the inner width. On cell
		// surfaces the border reservation is 0, so this is a no-op.
		b := core.FindFrameBorderUnits(w)
		tp := p
		innerW := bounds.Width
		if b > 0 {
			tp = p.WithOffset(b, b)
			innerW = bounds.Width - 2*b
		}
		// Draw window controls on the LEFT: [x][.][^] or [x][.][o]
		// These are decorative buttons - use cell-based sizing (3 cells each)
		buttonWidth := metrics.CellWidth * 3
		controlX := metrics.CellWidth // Start after left border
		if flags&WindowFlagNoClose == 0 {
			isFocused := titleFocus == TitleFocusClose
			isPressed := pressedButton == TitleButtonClose && buttonHovered
			isHovered := hoveredButton == TitleButtonClose && !isPressed && p.Graphical()
			btnStyle := scheme.GetTitleBarButtonState(buttonFocused, isFocused, isHovered, isPressed)
			tp.DrawCell(controlX, 0, '[', btnStyle)
			tp.DrawCell(controlX+metrics.CellWidth, 0, 'x', btnStyle)
			tp.DrawCell(controlX+metrics.CellWidth*2, 0, ']', btnStyle)
			controlX += buttonWidth
		}
		if flags&WindowFlagNoMinimize == 0 {
			isFocused := titleFocus == TitleFocusMinimize
			isPressed := pressedButton == TitleButtonMinimize && buttonHovered
			isHovered := hoveredButton == TitleButtonMinimize && !isPressed && p.Graphical()
			btnStyle := scheme.GetTitleBarButtonState(buttonFocused, isFocused, isHovered, isPressed)
			tp.DrawCell(controlX, 0, '[', btnStyle)
			tp.DrawCell(controlX+metrics.CellWidth, 0, '.', btnStyle)
			tp.DrawCell(controlX+metrics.CellWidth*2, 0, ']', btnStyle)
			controlX += buttonWidth
		}
		if canMaximize(flags) {
			isFocused := titleFocus == TitleFocusMaximize
			isPressed := pressedButton == TitleButtonMaximize && buttonHovered
			isHovered := hoveredButton == TitleButtonMaximize && !isPressed && p.Graphical()
			btnStyle := scheme.GetTitleBarButtonState(buttonFocused, isFocused, isHovered, isPressed)
			var icon rune
			if state == WindowStateMaximized {
				icon = 'o' // Restore icon
			} else {
				icon = '^' // Maximize icon
			}
			tp.DrawCell(controlX, 0, '[', btnStyle)
			tp.DrawCell(controlX+metrics.CellWidth, 0, icon, btnStyle)
			tp.DrawCell(controlX+metrics.CellWidth*2, 0, ']', btnStyle)
			controlX += buttonWidth
		}

		// Calculate title area (centered on top border)
		titleRect := core.UnitRect{
			X:      0,
			Y:      0,
			Width:  innerW,
			Height: metrics.CellHeight,
		}

		// Tear-off handle floats immediately left of the (centered) title,
		// but is omitted while the title itself is focused - the '< >'
		// brackets stand in for it - so it isn't shoved aside; it returns on
		// the next Tab / Shift+Tab focus change.
		if titleFocus != TitleFocusTitle {
			tearTitleW := font.MeasureText(title)
			// On the graphical path a blur-focused bar reads fully inactive, so
			// the tear/redock handle and the space around it take the inactive
			// title colors too (matching a real inactive window frame).
			tearStyle := titleStyle
			tearActive := buttonFocused
			if rounded && titleFocus == TitleFocusBlur {
				tearStyle = scheme.GetWindowTitle(false)
				tearActive = false
			}
			controlX = w.paintTearHandle(tp, scheme, tearStyle, metrics, controlX, innerW, tearTitleW, tearActive, titleFocus)
		}

		// Draw title text centered, with angle brackets and cyan bg if title has keyboard focus
		if titleFocus == TitleFocusTitle {
			// Title has focus - draw with decorative angle brackets, as one run.
			titleDisplayStyle := scheme.GetTitleBarButton(focused, true, false)
			w.paintFocusedTitleDecoration(tp, titleRect.Width, title, titleDisplayStyle, font, metrics.CellHeight)
		} else {
			// Normal title or blur focused
			titleDisplayStyle := titleStyle
			if titleFocus == TitleFocusBlur {
				// Blur item focused - use inactive title style for the title text
				titleDisplayStyle = scheme.GetWindowTitle(false)
			}
			rightLimit := innerW - metrics.CellWidth
			if titleFocus == TitleFocusBlur {
				rightLimit = innerW - metrics.CellWidth - buttonWidth
			}
			w.paintTitleText(tp, title, titleDisplayStyle, font, metrics, controlX, rightLimit, innerW)
		}

		// Draw blur button on far right when blur item is focused
		// This is a decorative button - use cell-based sizing (3 cells)
		if titleFocus == TitleFocusBlur {
			blurBtnStyle := scheme.GetTitleBarButton(true, true, false) // Focused button style
			blurX := innerW - metrics.CellWidth - buttonWidth           // Position before right border
			tp.DrawCell(blurX, 0, '[', blurBtnStyle)
			tp.DrawCell(blurX+metrics.CellWidth, 0, '~', blurBtnStyle)
			tp.DrawCell(blurX+metrics.CellWidth*2, 0, ']', blurBtnStyle)
		}
	}

	// Single-border (active, not focused): the outer band is window-background
	// colored (drawn above), so add the thin inner line in the active border
	// color. Drawn last - after the titlebar icons and title - so it slightly
	// overlays them. The after-content re-stroke re-adds it over edge-to-edge
	// content.
	if rounded && border == style.BorderHeavy {
		w.paintSingleBorderInner(p, localBounds)
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
func ellipsizeToWidth(s string, avail core.Unit, font *core.Font) string {
	const ell = "..."
	if font.MeasureText(s) <= avail {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		if font.MeasureText(string(runes)+ell) <= avail {
			return string(runes) + ell
		}
	}
	return ""
}

// paintTitleText draws the (unfocused) titlebar title. Centered when
// a centered title fits between the left buttons and the right limit
// (the blur button when shown, else the right edge); otherwise its
// left edge sits just past the buttons and the text ellipsizes so
// the "..." butts against the right limit - the right side keeps no
// mirrored reserve. A span of zero or less clips the title entirely.
func (w *Window) paintTitleText(p *core.Painter, title string, ts style.CellStyle, font *core.Font, metrics core.CellMetrics, leftUsed, rightLimit, barWidth core.Unit) {
	leftEdge := leftUsed + metrics.CellWidth
	avail := rightLimit - leftEdge
	if avail <= 0 || title == "" {
		return
	}
	display := title
	titleW := font.MeasureText(display)
	if titleW > avail {
		display = ellipsizeToWidth(title, avail, font)
		if display == "" {
			return
		}
		titleW = font.MeasureText(display)
	}
	x := (barWidth - titleW) / 2
	if x < leftEdge {
		x = leftEdge
	}
	if x+titleW > rightLimit {
		x = rightLimit - titleW
	}
	p.DrawText(x, 0, display, ts, font)
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
// and returns the leftUsed value paintTitleText expects (its +CellWidth
// gap lands the title just past the handle slot). The glyph carries the
// button foreground over the title-bar background; when the handle is
// the focused title element it draws [%]/[#] in the focused-button
// style like the other buttons. Not tearable: controlsRight is returned
// unchanged (title keeps its normal gap past the controls).
func (w *Window) paintTearHandle(p *core.Painter, scheme *style.Scheme, titleStyle style.CellStyle, metrics core.CellMetrics, controlsRight, barWidth, titleW core.Unit, windowActive bool, titleFocus TitleFocus) core.Unit {
	w.mu.RLock()
	tearable := w.flags&WindowFlagTearable != 0
	detached := w.detached
	w.mu.RUnlock()
	if tearable == false || !hasTitleBar(w.flags) {
		return controlsRight
	}
	buttonWidth := metrics.TextWidth(3)
	handleX := tearHandleSlotX(barWidth, controlsRight, titleW, buttonWidth)
	glyph := '%'
	if detached {
		glyph = '#'
	}
	if titleFocus == TitleFocusTear {
		st := scheme.GetTitleBarButton(windowActive, true, false)
		p.DrawCell(handleX, 0, '[', st)
		p.DrawCell(handleX+metrics.CellWidth, 0, glyph, st)
		p.DrawCell(handleX+metrics.CellWidth*2, 0, ']', st)
	} else {
		btn := scheme.GetTitleBarButton(windowActive, false, false)
		st := titleStyle.WithFg(btn.Fg)
		// Fill the slot's flanking cells with the title-bar background so
		// the frame's top-border stroke does not peek through the gaps
		// on either side of the floating glyph.
		p.DrawCell(handleX, 0, ' ', titleStyle)
		p.DrawCell(handleX+metrics.CellWidth, 0, glyph, st)
		p.DrawCell(handleX+metrics.CellWidth*2, 0, ' ', titleStyle)
	}
	// The title butts against the right edge of the handle slot; the
	// -CellWidth cancels paintTitleText's +CellWidth gap so a centered
	// title lands exactly one slot right of the handle.
	return handleX + buttonWidth - metrics.CellWidth
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

	metrics := w.frameCellMetrics()

	// The titlebar chrome sits inside the frame border (maximized has no
	// side border); shift the hit-test into the same inner coordinate
	// system paintNormalFrame draws it in.
	inset := core.Unit(0)
	if state != WindowStateMaximized {
		inset = core.FindFrameBorderUnits(w)
	}
	x -= inset
	y -= inset

	// Must be in titlebar
	if !hasTitleBar(flags) || y < 0 || y >= metrics.CellHeight {
		return TitleButtonNone
	}

	// Control buttons are on the left
	controlX := metrics.CellWidth // Start after left border (for normal frame)
	if state == WindowStateMaximized {
		controlX = 0 // No border in maximized state
	}

	buttonWidth := metrics.TextWidth(3)

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
	// centered title, so hit-test the same slot paintTearHandle draws. The
	// handle is hidden while the title is focused, so it isn't hittable then.
	if flags&WindowFlagTearable != 0 && hasTitleBar(flags) && titleFocus != TitleFocusTitle {
		titleW := w.EffectiveFont().MeasureText(title)
		// Inner width: the paint centers within the border-inset titlebar.
		handleX := tearHandleSlotX(w.Bounds().Width-2*inset, controlX, titleW, buttonWidth)
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
	if focus == TitleFocusNone {
		w.resizeEdges = ResizeEdgeNone // Clear resize state when leaving title bar
	}
	title := w.title
	w.mu.Unlock()

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
func (w *Window) hasKeyboardBlurEnabled() bool {
	parent := w.Parent()
	if parent == nil {
		return false
	}
	if provider, ok := parent.(core.KeyboardBlurChildrenProvider); ok {
		return provider.KeyboardBlurChildren()
	}
	return false
}

// performKeyboardBlur calls the parent's PerformKeyboardBlur if available.
func (w *Window) performKeyboardBlur() {
	parent := w.Parent()
	if parent == nil {
		return
	}
	if provider, ok := parent.(core.KeyboardBlurChildrenProvider); ok {
		provider.PerformKeyboardBlur()
	}
}

// handleTitleBarKey handles keyboard input when title bar has focus.
func (w *Window) handleTitleBarKey(event core.KeyPressEvent) bool {
	w.mu.RLock()
	titleFocus := w.titleFocus
	resizeEdges := w.resizeEdges
	flags := w.flags
	w.mu.RUnlock()

	metrics := w.frameCellMetrics()

	// Handle navigation between title bar elements
	switch event.Key {
	case "Tab":
		// Check if Shift is held - use same logic as S-Tab case
		if event.Modifiers&core.ShiftModifier != 0 {
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
		}
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

	case "S-Tab", "Shift-Tab":
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

	case "Escape":
		// Exit title bar focus, return to content
		w.SetTitleFocus(TitleFocusNone)
		w.mu.Lock()
		w.resizeEdges = ResizeEdgeNone
		w.mu.Unlock()
		if fm := w.FocusManager(); fm != nil {
			fm.FocusFirst()
		}
		return true

	case "Enter", " ", "Space":
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
		hasShift := event.Modifiers&core.ShiftModifier != 0
		hasCtrl := event.Modifiers&core.ControlModifier != 0
		hasMeta := event.Modifiers&core.MetaModifier != 0
		hasAlt := event.Modifiers&core.AltModifier != 0

		// Determine movement multiplier based on modifiers
		// Alt/Meta/Ctrl increases horizontal by 10 chars, vertical by 4 lines
		horizStep := metrics.CellWidth
		vertStep := metrics.CellHeight
		if hasMeta || hasAlt || hasCtrl {
			horizStep = metrics.CellWidth * 10
			vertStep = metrics.CellHeight * 4
		}

		// Normalize key names. Modifier prefixes can arrive in any order
		// and in combination - the SDL backend emits arrows as e.g.
		// "M-S-Left" (Alt+Shift), so stripping only the single leading
		// prefix would leave "S-Left" and lose the resize. Peel every
		// recognized prefix, whatever the order.
		key := event.Key
		for {
			switch {
			case strings.HasPrefix(key, "S-"):
				hasShift = true
				key = key[2:]
			case strings.HasPrefix(key, "M-"), strings.HasPrefix(key, "A-"):
				hasMeta = true
				hasAlt = true
				key = key[2:]
			case strings.HasPrefix(key, "C-"):
				hasCtrl = true
				key = key[2:]
			case strings.HasPrefix(key, "s-"):
				hasMeta = true
				key = key[2:]
			default:
			}
			if !strings.HasPrefix(key, "S-") && !strings.HasPrefix(key, "M-") &&
				!strings.HasPrefix(key, "A-") && !strings.HasPrefix(key, "C-") &&
				!strings.HasPrefix(key, "s-") {
				break
			}
		}
		// Any large-step modifier (Alt/Meta/Ctrl) makes moves and resizes
		// chunky.
		if hasMeta || hasAlt || hasCtrl {
			horizStep = metrics.CellWidth * 10
			vertStep = metrics.CellHeight * 4
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

		case "Enter", "Return", "KPEnter":
			// Confirm resize - clear edges so next Shift+arrow starts fresh
			// Also update resizeStartBounds to current bounds
			w.mu.Lock()
			if w.resizeEdges != ResizeEdgeNone {
				w.resizeEdges = ResizeEdgeNone
				w.resizeStartBounds = w.Bounds()
			}
			w.mu.Unlock()
			return true

		case "Escape", "Esc":
			// Cancel resize - revert to bounds from when resize started
			w.mu.Lock()
			if w.resizeEdges != ResizeEdgeNone {
				startBounds := w.resizeStartBounds
				w.resizeEdges = ResizeEdgeNone
				w.mu.Unlock()
				w.requestKeyboardBounds(startBounds, false)
			} else {
				w.mu.Unlock()
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
	minVisible := metrics.CellWidth * minVisibleColumns

	if bounds.Y < clientArea.Y {
		bounds.Y = clientArea.Y
	}
	maxY := clientArea.Y + clientArea.Height - metrics.CellHeight
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
	if mb != nil {
		menuActive := mb.HasFocus() || event.Key == "F10"
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
		if sc, ok := mb.(interface {
			HandleShortcut(core.KeyPressEvent) bool
		}); ok && sc.HandleShortcut(event) {
			return true
		}
	}
	if shortcutResolver != nil && shortcutResolver(event) {
		return true
	}

	// If title bar has focus, handle title bar keys
	if titleFocus != TitleFocusNone {
		if w.handleTitleBarKey(event) {
			return true
		}
	}

	// Check if this is a Tab or Shift+Tab event
	isShiftTab := event.Key == "S-Tab" || event.Key == "Shift-Tab" ||
		(event.Key == "Tab" && event.Modifiers&core.ShiftModifier != 0)
	isTab := event.Key == "Tab" && event.Modifiers&core.ShiftModifier == 0

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
	switch event.Key {
	case "M-F4": // Alt+F4 - Close
		w.Close()
		return true
	case "M-F10": // Alt+F10 - Maximize/Restore
		if w.IsMaximized() {
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

	metrics := w.frameCellMetrics()

	// The titlebar chrome sits inside the frame border (offset down by the
	// border), so the titlebar band runs [0, border+CellHeight) in window-
	// local coordinates - not [0, CellHeight). Missing the border here would
	// leave the bottom of the visible titlebar (and the bottoms of the
	// titlebar buttons) routed to content. Maximized has no side border.
	titleBand := metrics.CellHeight
	if state != WindowStateMaximized {
		titleBand += core.FindFrameBorderUnits(w)
	}

	// Check for title bar clicks
	if hasTitleBar(flags) && event.Y < titleBand {
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
		le := event
		le.X -= r.X
		le.Y -= r.Y
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
	w.mu.RUnlock()

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
			le := event
			le.X -= r.X
			le.Y -= r.Y
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
	w.mu.RUnlock()

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
			le := event
			le.X -= r.X
			le.Y -= r.Y
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
		width += metrics.CellWidth * 2   // Left and right borders
		height += metrics.CellHeight * 2 // Top and bottom borders
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
				le := event
				le.X -= r.X
				le.Y -= r.Y
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
