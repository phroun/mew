// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"sync"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// MDIPane is a container trinket that manages multiple floating windows (MDI children).
// Unlike Desktop's WindowManager which is screen-level, MDIPane is a regular trinket
// that can be embedded in any container (Window, TabTrinket, Splitter, Panel, etc.).
//
// MDIPane provides:
// - Background content area (for trinkets behind the floating windows)
// - Z-ordered floating window management
// - Window dragging, resizing, minimize/maximize/restore
// - Keyboard and mouse focus management
// - Tile and cascade window arrangement
type MDIPane struct {
	core.TrinketBase
	mu sync.RWMutex

	// Background content trinket (shown behind windows)
	content core.Trinket

	// Layout manager for content
	layout core.LayoutManager

	// Background pattern character
	bgChar rune

	// Whether to draw the pattern background (true) or solid background like ScrollArea (false)
	// Defaults to true when no content, automatically set to false when content is added
	drawPattern    bool
	drawPatternSet bool // tracks if drawPattern was explicitly set

	// Whether child windows include a virtual "blur" focus item that allows
	// keyboard users to exit the window and return to MDIPane. Default: true.
	keyboardBlurChildren bool

	// Floating windows in z-order (back to front)
	windows []*window.Window

	// cycleOrder is the M-Tab sequence, kept separate from the visual z-order
	// (windows) so cycling can raise a child to see it without committing the
	// sequence. Most-recently-committed child is at the end. Committed only on a
	// genuine child interaction or the idle lock-in - not by parent Next/Prev
	// buttons or by stepping the run.
	cycleOrder []*window.Window

	// cycling is true while a NextWindow/PrevWindow run is in progress (the
	// cycleOrder stays frozen); lastCycleAt drives the idle lock-in.
	cycling     bool
	lastCycleAt time.Time

	// Active/focused window
	activeWindow *window.Window

	// Modal window stack
	modalStack []*window.Window

	// Drag state
	dragging    *window.Window
	dragStartX  core.Unit
	dragStartY  core.Unit
	dragOffsetX core.Unit
	dragOffsetY core.Unit

	// Resize state
	resizing       *window.Window
	resizeEdge     int
	resizeStartX   core.Unit
	resizeStartY   core.Unit
	resizeOriginal core.UnitRect

	// Double-click detection
	lastClickTime   time.Time
	lastClickX      core.Unit
	lastClickY      core.Unit
	lastClickWindow *window.Window

	// Focus-without-raise: track pressed window for conditional raise on release
	pressedWindow *window.Window

	// Topmost window under the pointer on the last plain (no-button) move,
	// so its hover states can be cleared when the pointer moves to another
	// window. Lets inactive windows highlight their hoverable widgets.
	lastHoverWindow *window.Window

	// Callbacks
	onWindowAdded         func(*window.Window)
	onWindowRemoved       func(*window.Window)
	onActiveWindowChanged func(*window.Window)
	onWindowMinimized     func(*window.Window)
	onWindowRestored      func(*window.Window)
}

// NewMDIPane creates a new MDI pane trinket.
func NewMDIPane() *MDIPane {
	m := &MDIPane{
		bgChar:               '░',  // Light shade for MDI background
		drawPattern:          true, // Default to pattern when no content
		keyboardBlurChildren: true, // Default to enabling keyboard blur
	}
	m.TrinketBase = *core.NewTrinketBase()
	m.Init(m)
	m.SetFocusPolicy(core.StrongFocus)
	return m
}

// DrawPattern returns whether the MDI pane draws a pattern background.
func (m *MDIPane) DrawPattern() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.drawPattern
}

// SetDrawPattern sets whether the MDI pane draws a pattern background.
// When true, draws a pattern character with DesktopFill colors.
// When false, draws a solid background like ScrollArea.
func (m *MDIPane) SetDrawPattern(drawPattern bool) {
	m.mu.Lock()
	m.drawPattern = drawPattern
	m.drawPatternSet = true
	m.mu.Unlock()
	m.Update()
}

// KeyboardBlurChildren returns whether child windows include a virtual "blur"
// focus item that allows keyboard users to exit the window.
func (m *MDIPane) KeyboardBlurChildren() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.keyboardBlurChildren
}

// SetKeyboardBlurChildren sets whether child windows include a virtual "blur"
// focus item that allows keyboard users to exit the window and return to MDIPane.
func (m *MDIPane) SetKeyboardBlurChildren(enabled bool) {
	m.mu.Lock()
	m.keyboardBlurChildren = enabled
	m.mu.Unlock()
}

// PerformKeyboardBlur implements core.KeyboardBlurChildrenProvider.
// It deactivates the current active window, same as clicking on the MDI pane background.
func (m *MDIPane) PerformKeyboardBlur() {
	m.DeactivateActiveWindow()
}

// IsWindowPassive implements core.PassiveWindowProvider.
// A window is passive when:
// 1. It's active but contains an MDIPane with an active descendant window, OR
// 2. An ancestor Desktop has menu bar focused (no active window at desktop level)
func (m *MDIPane) IsWindowPassive(win core.Trinket) bool {
	// Only the active window can be passive
	m.mu.RLock()
	activeWin := m.activeWindow
	m.mu.RUnlock()

	if activeWin == nil || activeWin != win {
		return false
	}

	// Check if an ancestor Desktop has menu bar focused (no active window)
	if hasAncestorMenuBarFocused(m) {
		return true
	}

	// Check if this window contains an MDIPane with an active window
	return hasActiveDescendantWindow(win)
}

// hasAncestorMenuBarFocused walks up the trinket tree to check if an ancestor
// Desktop has the menu bar focused (indicated by no active window in the window manager).
func hasAncestorMenuBarFocused(w core.Trinket) bool {
	current := w.Parent()
	for current != nil {
		// Check if this is a Desktop with no active window (menu bar has focus)
		if desktop, ok := current.(*Desktop); ok {
			if wm := desktop.WindowManager(); wm != nil {
				if wm.ActiveWindow() == nil && wm.PreviousActiveWindow() != nil {
					return true
				}
			}
		}
		current = current.Parent()
	}
	return false
}

// hasActiveDescendantWindow recursively checks if a trinket contains an MDIPane
// with an active window (indicating focus is in a nested MDI child).
func hasActiveDescendantWindow(w core.Trinket) bool {
	container, ok := w.(core.Container)
	if !ok {
		return false
	}

	for _, child := range container.Children() {
		// Check if child is an MDIPane with an active window
		if mdi, ok := child.(*MDIPane); ok {
			if mdi.ActiveWindow() != nil {
				return true
			}
		}
		// Recursively check children
		if hasActiveDescendantWindow(child) {
			return true
		}
	}
	return false
}

// SetContent sets the background content trinket.
// This trinket is displayed behind all floating windows.
// When content is added and drawPattern hasn't been explicitly set,
// drawPattern is automatically set to false for a solid background.
func (m *MDIPane) SetContent(content core.Trinket) {
	m.mu.Lock()
	hadContent := m.content != nil
	m.content = content
	if content != nil {
		content.SetParent(m)
		// Auto-switch to solid background when first content is added
		// (unless drawPattern was explicitly set)
		if !hadContent && !m.drawPatternSet {
			m.drawPattern = false
		}
	}
	m.mu.Unlock()
	m.layoutContent()
	m.Update()
}

// Content returns the background content trinket.
func (m *MDIPane) Content() core.Trinket {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.content
}

// SetLayout sets the layout manager for the content area.
func (m *MDIPane) SetLayout(layout core.LayoutManager) {
	m.mu.Lock()
	m.layout = layout
	m.mu.Unlock()
	m.layoutContent()
}

// SetBackgroundChar sets the background pattern character.
func (m *MDIPane) SetBackgroundChar(ch rune) {
	m.mu.Lock()
	m.bgChar = ch
	m.mu.Unlock()
	m.Update()
}

// BackgroundChar returns the background pattern character.
func (m *MDIPane) BackgroundChar() rune {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.bgChar
}

// AddWindow adds a floating window to the MDI pane.
func (m *MDIPane) AddWindow(win *window.Window) {
	m.mu.Lock()
	// Check if already added
	for _, w := range m.windows {
		if w == win {
			m.mu.Unlock()
			return
		}
	}
	m.windows = append(m.windows, win)
	m.cycleOrder = append(m.cycleOrder, win)
	handler := m.onWindowAdded
	m.mu.Unlock()

	// Set window's parent to this pane
	win.SetParent(m)

	// MDI children are not stamped by the WindowManager, so carry
	// the surface's smooth-positioning capability onto them here -
	// trinkets inside (splitters, future draggables) discover it via
	// FindSmoothPositioning, which stops at the first window-like
	// provider it meets.
	win.SetSmoothPositioning(core.FindSmoothPositioning(m.Self()))

	// Trigger layout now that parent is set (font inheritance works)
	win.Layout()

	// Set up request callbacks
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
	win.SetGetConstrainingBounds(func() core.UnitRect {
		return m.ClientArea()
	})
	win.SetOnCloseComplete(func() {
		m.RemoveWindow(win)
	})

	// Position if not explicitly set
	bounds := win.Bounds()
	if bounds.X == 0 && bounds.Y == 0 {
		m.positionWindow(win)
	}

	// Activate
	m.ActivateWindow(win)

	if handler != nil {
		handler(win)
	}
}

// RemoveWindow removes a window from the MDI pane.
func (m *MDIPane) RemoveWindow(win *window.Window) {
	m.mu.Lock()
	for i, w := range m.windows {
		if w == win {
			m.windows = append(m.windows[:i], m.windows[i+1:]...)
			break
		}
	}

	// Remove from the cycle sequence too
	for i, w := range m.cycleOrder {
		if w == win {
			m.cycleOrder = append(m.cycleOrder[:i], m.cycleOrder[i+1:]...)
			break
		}
	}

	// Remove from modal stack
	for i, w := range m.modalStack {
		if w == win {
			m.modalStack = append(m.modalStack[:i], m.modalStack[i+1:]...)
			break
		}
	}

	// Update active window
	wasActive := m.activeWindow == win
	var newActive *window.Window
	if wasActive {
		m.activeWindow = nil
		if len(m.windows) > 0 {
			newActive = m.windows[len(m.windows)-1]
			m.activeWindow = newActive
		}
	}

	removedHandler := m.onWindowRemoved
	activeHandler := m.onActiveWindowChanged
	m.mu.Unlock()

	// Update active states and focus
	if wasActive {
		win.SetActive(false)
		if newActive != nil {
			newActive.SetActive(true)
			// Focus the new active window's first trinket
			if fm := newActive.FocusManager(); fm != nil {
				if fm.FocusedTrinket() == nil {
					fm.FocusFirst()
				}
			}
		}
		// MDIPane keeps focus so keyboard events come here
		m.SetFocus()
	}

	if removedHandler != nil {
		removedHandler(win)
	}
	if activeHandler != nil && wasActive {
		activeHandler(newActive)
	}

	m.Update()
}

// Windows returns all windows in z-order.
func (m *MDIPane) Windows() []*window.Window {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*window.Window, len(m.windows))
	copy(result, m.windows)
	return result
}

// ActiveWindow returns the currently active window.
func (m *MDIPane) ActiveWindow() *window.Window {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeWindow
}

// SetActiveWindow activates a specific window.
func (m *MDIPane) SetActiveWindow(win *window.Window) {
	m.ActivateWindow(win)
}

// mdiCycleCommitTimeout is the idle gap after which an in-progress MDI cycle
// run locks its landing spot into the sequence. MDI cycling can be driven by
// the parent's own Next/Prev buttons (no modifier to release), so the timer is
// the commit mechanism here rather than modifier release.
const mdiCycleCommitTimeout = 2 * time.Second

// bringToCycleFront moves win to the end (most-recent) of the cycle sequence.
func (m *MDIPane) bringToCycleFront(win *window.Window) {
	for i, w := range m.cycleOrder {
		if w == win {
			m.cycleOrder = append(m.cycleOrder[:i], m.cycleOrder[i+1:]...)
			m.cycleOrder = append(m.cycleOrder, win)
			return
		}
	}
	m.cycleOrder = append(m.cycleOrder, win)
}

// endCycleSession ends an in-progress cycle run, committing the child it landed
// on to the front of the sequence. A no-op when no run is in progress. Fired by
// a genuine child interaction (a key the child handles, or a click) or the idle
// lock-in - never by stepping the run or the parent's Next/Prev buttons.
func (m *MDIPane) endCycleSession() {
	m.mu.Lock()
	if !m.cycling {
		m.mu.Unlock()
		return
	}
	m.cycling = false
	if active := m.activeWindow; active != nil {
		m.bringToCycleFront(active)
	}
	m.mu.Unlock()
}

// ActivateWindow brings a window to the front and makes it the active window.
// When a window becomes active, MDIPane takes focus so that all keyboard
// events (including Tab) are routed through MDIPane to the active window.
func (m *MDIPane) ActivateWindow(win *window.Window) {
	m.endCycleSession()
	m.activate(win, true)
}

// activate brings a window to the front (visual z-order, always) and makes it
// active. reorderCycle controls whether it is also promoted in the M-Tab
// sequence: true for a normal activation, false while stepping a cycle run
// (which raises the child to see it but must not commit the sequence).
func (m *MDIPane) activate(win *window.Window, reorderCycle bool) {
	m.mu.Lock()
	if win == m.activeWindow {
		m.mu.Unlock()
		return
	}

	// Check if blocked by modal
	if len(m.modalStack) > 0 {
		topModal := m.modalStack[len(m.modalStack)-1]
		if win != topModal && !m.isChildOf(win, topModal) {
			m.mu.Unlock()
			return
		}
	}

	oldActive := m.activeWindow
	m.activeWindow = win

	// Move to top of z-order (visual raise - always, so a cycled-to child is
	// visible). Promote in the M-Tab sequence only on a real activation; a
	// cycle step leaves the sequence frozen until the run commits.
	m.bringToFront(win)
	if reorderCycle {
		m.bringToCycleFront(win)
	}

	handler := m.onActiveWindowChanged
	m.mu.Unlock()

	// Update active states
	if oldActive != nil {
		oldActive.SetActive(false)
	}
	if win != nil {
		win.SetActive(true)

		// Only take focus and initialize child window focus if MDIPane
		// already has focus. This prevents stealing focus during initial
		// setup when windows are added but the MDI tab isn't active yet.
		if m.HasFocus() {
			// Focus the window's first trinket if no trinket is focused.
			// Use FocusFirstWithoutScroll since ActivateWindow is typically
			// called from mouse handlers where visibility is already proven.
			if fm := win.FocusManager(); fm != nil {
				if fm.FocusedTrinket() == nil {
					fm.FocusFirstWithoutScroll()
				}
			}
		}
	}

	if handler != nil {
		handler(win)
	}

	m.Update()
}

// FocusWindow gives a window focus without raising it to the front.
// This is used for focus-follows-click behavior where the window only
// raises on mouse release within its bounds.
func (m *MDIPane) FocusWindow(win *window.Window) {
	m.endCycleSession()
	m.mu.Lock()
	if win == m.activeWindow {
		m.mu.Unlock()
		return
	}

	// Check if blocked by modal
	if len(m.modalStack) > 0 {
		topModal := m.modalStack[len(m.modalStack)-1]
		if win != topModal && !m.isChildOf(win, topModal) {
			m.mu.Unlock()
			return
		}
	}

	oldActive := m.activeWindow
	m.activeWindow = win
	// Note: bringToFront is NOT called here - window stays in current z-order

	handler := m.onActiveWindowChanged
	m.mu.Unlock()

	// Update active states
	if oldActive != nil {
		oldActive.SetActive(false)
	}
	if win != nil {
		win.SetActive(true)
		// Focus the window's first trinket if no trinket is focused.
		// Use FocusFirstWithoutScroll since this is called from mouse handlers
		// and visibility is already proven by the click.
		if fm := win.FocusManager(); fm != nil {
			if fm.FocusedTrinket() == nil {
				fm.FocusFirstWithoutScroll()
			}
		}
	}

	if handler != nil {
		handler(win)
	}

	m.Update()
}

// RaiseWindow brings a window to the front without changing focus.
func (m *MDIPane) RaiseWindow(win *window.Window) {
	m.mu.Lock()
	m.bringToFront(win)
	m.mu.Unlock()
	m.Update()
}

// NextWindow steps the M-Tab sequence forward.
func (m *MDIPane) NextWindow() { m.cycle(true) }

// PrevWindow steps the M-Tab sequence backward.
func (m *MDIPane) PrevWindow() { m.cycle(false) }

// cycle steps the window-cycle run one place in the given direction. It walks
// the LIVE cycle sequence (so an added/removed child is handled automatically),
// locates the current selection by identity, and raises the target to see it
// WITHOUT committing the sequence - stepping mutates neither the sequence nor
// (asymmetrically) the list it is walking, so forward and backward are
// symmetric. The landing spot is committed later, by endCycleSession.
func (m *MDIPane) cycle(forward bool) {
	now := time.Now()

	m.mu.Lock()
	wasCycling := m.cycling
	gap := now.Sub(m.lastCycleAt)
	m.mu.Unlock()

	// Idle lock-in: a step long after the previous one is a fresh gesture, so
	// lock the prior run's landing spot into the sequence first. MDI has no
	// modifier-release to key off (the parent's Next/Prev buttons carry none),
	// so the timer is the commit mechanism.
	if wasCycling && gap > mdiCycleCommitTimeout {
		m.endCycleSession()
	}

	m.mu.Lock()
	seq := make([]*window.Window, len(m.cycleOrder))
	copy(seq, m.cycleOrder)
	active := m.activeWindow
	m.lastCycleAt = now
	m.cycling = true
	m.mu.Unlock()

	if len(seq) <= 1 {
		return
	}

	currentIdx := -1
	for i, w := range seq {
		if w == active {
			currentIdx = i
			break
		}
	}
	if currentIdx < 0 {
		currentIdx = len(seq) - 1
	}

	// cycleOrder holds the most-recently used child at the end, so (matching
	// the OS convention) forward steps toward the most recent - one press is
	// the most-recently-used other child - and backward heads toward the least
	// recent first.
	var nextIdx int
	if forward {
		nextIdx = (currentIdx - 1 + len(seq)) % len(seq)
	} else {
		nextIdx = (currentIdx + 1) % len(seq)
	}
	// Raise the target (visible) but leave the sequence frozen for the run.
	m.activate(seq[nextIdx], false)
}

// MaximizeWindow maximizes a window to fill the MDI pane.
func (m *MDIPane) MaximizeWindow(win *window.Window) {
	clientArea := m.ClientArea()
	win.Maximize()
	win.SetBounds(clientArea)
	m.Update()
}

// MinimizeWindow minimizes a window.
func (m *MDIPane) MinimizeWindow(win *window.Window) {
	win.Minimize()

	// Minimizing the active child hands focus back to the parent, exactly as
	// pressing its blur item would - the minimized window can't hold focus.
	m.mu.RLock()
	wasActive := m.activeWindow == win
	handler := m.onWindowMinimized
	m.mu.RUnlock()

	if wasActive {
		m.DeactivateActiveWindow()
	}

	if handler != nil {
		handler(win)
	}

	m.Update()
}

// RestoreWindow restores a minimized or maximized window.
func (m *MDIPane) RestoreWindow(win *window.Window) {
	win.Restore()
	m.ActivateWindow(win)

	// Notify via callback
	m.mu.RLock()
	handler := m.onWindowRestored
	m.mu.RUnlock()

	if handler != nil {
		handler(win)
	}

	m.Update()
}

// TileWindows arranges windows in a tiled layout.
func (m *MDIPane) TileWindows() {
	m.mu.RLock()
	var visibleWindows []*window.Window
	for _, w := range m.windows {
		if w.IsVisible() && !w.IsMinimized() {
			visibleWindows = append(visibleWindows, w)
		}
	}
	m.mu.RUnlock()

	if len(visibleWindows) == 0 {
		return
	}

	clientArea := m.ClientArea()

	items := make([]window.TileItem, len(visibleWindows))
	for i, w := range visibleWindows {
		items[i] = window.TileItem{
			Resizable: w.Flags()&window.WindowFlagNoResize == 0,
			Size:      core.UnitSize{Width: w.Bounds().Width, Height: w.Bounds().Height},
		}
	}
	cells := window.TileLayout(clientArea, items)

	for i, win := range visibleWindows {
		win.Restore()
		window.PlaceInCell(win, cells[i], items[i].Resizable)
	}

	m.Update()
}

// CascadeWindows arranges windows in a cascade.
func (m *MDIPane) CascadeWindows() {
	m.mu.RLock()
	var visibleWindows []*window.Window
	for _, w := range m.windows {
		if w.IsVisible() && !w.IsMinimized() {
			visibleWindows = append(visibleWindows, w)
		}
	}
	m.mu.RUnlock()

	if len(visibleWindows) == 0 {
		return
	}

	clientArea := m.ClientArea()
	metrics := m.EffectiveCellMetrics()
	// The cascade step includes the frame border, so each window's whole
	// top chrome (border + titlebar) clears the one beneath it.
	border := core.FindFrameBorderUnits(visibleWindows[0])
	offset := metrics.CellWidth*2 + border

	// Standard size for cascaded windows
	width := metrics.RoundDownToCellX(clientArea.Width * 3 / 4)
	height := metrics.RoundDownToCellY(clientArea.Height * 3 / 4)

	for i, win := range visibleWindows {
		// Leave any maximized/minimized state before positioning, so the
		// cascade bounds stick (Restore would otherwise overwrite them).
		win.Restore()

		x := clientArea.X + core.Unit(i)*offset
		y := clientArea.Y + core.Unit(i)*offset

		// A window that can't be resized is only repositioned, keeping its
		// own size; only resizable windows adopt the standard cascade size.
		w, h := width, height
		if win.Flags()&window.WindowFlagNoResize != 0 {
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

	m.Update()
}

// ShowModal shows a window as modal.
func (m *MDIPane) ShowModal(win *window.Window) {
	win.SetType(window.WindowTypeModal)
	m.mu.Lock()
	m.modalStack = append(m.modalStack, win)
	m.mu.Unlock()
	m.AddWindow(win)
}

// CloseModal closes the top modal window.
func (m *MDIPane) CloseModal() {
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

// SetOnWindowAdded sets the callback for when a window is added.
func (m *MDIPane) SetOnWindowAdded(handler func(*window.Window)) {
	m.mu.Lock()
	m.onWindowAdded = handler
	m.mu.Unlock()
}

// SetOnWindowRemoved sets the callback for when a window is removed.
func (m *MDIPane) SetOnWindowRemoved(handler func(*window.Window)) {
	m.mu.Lock()
	m.onWindowRemoved = handler
	m.mu.Unlock()
}

// SetOnActiveWindowChanged sets the callback for when the active window changes.
func (m *MDIPane) SetOnActiveWindowChanged(handler func(*window.Window)) {
	m.mu.Lock()
	m.onActiveWindowChanged = handler
	m.mu.Unlock()
}

// SetOnWindowMinimized sets the callback for when a window is minimized.
func (m *MDIPane) SetOnWindowMinimized(handler func(*window.Window)) {
	m.mu.Lock()
	m.onWindowMinimized = handler
	m.mu.Unlock()
}

// SetOnWindowRestored sets the callback for when a window is restored.
func (m *MDIPane) SetOnWindowRestored(handler func(*window.Window)) {
	m.mu.Lock()
	m.onWindowRestored = handler
	m.mu.Unlock()
}

// DeactivateActiveWindow deactivates the current active window (if any).
// This is called when the user clicks on the MDIPane's content area,
// which transfers focus away from the active MDI child.
func (m *MDIPane) DeactivateActiveWindow() {
	m.mu.Lock()
	oldActive := m.activeWindow
	if oldActive == nil {
		m.mu.Unlock()
		return
	}
	m.activeWindow = nil
	handler := m.onActiveWindowChanged
	m.mu.Unlock()

	oldActive.SetActive(false)

	if handler != nil {
		handler(nil)
	}

	m.Update()
}

// displayBounds returns where a child window is drawn and hit-tested:
// its logical bounds corralled into the current client area. The corral
// is PROVISIONAL - never written back - so shrinking the pane nudges an
// off-screen window into view, and growing it again lets the window
// re-spread to its original spot. A deliberate interaction commits it
// (see commitDisplayBounds). Maximized windows are exempt (they already
// track the client area).
func (m *MDIPane) displayBounds(win *window.Window) core.UnitRect {
	if win.IsMaximized() {
		return win.Bounds()
	}
	return window.ClampWindowToClientArea(win.Bounds(), m.ClientArea(), m.EffectiveCellMetrics())
}

// ClientArea returns the area available for windows.
func (m *MDIPane) ClientArea() core.UnitRect {
	bounds := m.Bounds()
	outer, interior := m.denominations()
	return core.UnitRect{
		X:      0,
		Y:      0,
		Width:  core.ExchangeX(bounds.Width, outer, interior),
		Height: core.ExchangeY(bounds.Height, outer, interior),
	}
}

// denominations returns the grid-metrics currency of the pane's own
// coordinate space (outer: the parent's, in which Bounds lives) and
// of its interior, where child-window geometry lives (honoring a
// per-pane override). Equal unless an override is set on this pane.
func (m *MDIPane) denominations() (outer, interior core.CellMetrics) {
	interior = m.EffectiveCellMetrics()
	if m.CellMetricsOverride() == nil {
		return interior, interior
	}
	return core.ParentCellMetrics(m.Self()), interior
}

// toInterior converts a point from the pane's outer currency into its
// interior currency (where window bounds and content live).
func (m *MDIPane) toInterior(x, y core.Unit) (core.Unit, core.Unit) {
	outer, interior := m.denominations()
	return core.ExchangeX(x, outer, interior), core.ExchangeY(y, outer, interior)
}

// bringToFront moves a window to the top of the z-order.
func (m *MDIPane) bringToFront(win *window.Window) {
	for i, w := range m.windows {
		if w == win {
			m.windows = append(m.windows[:i], m.windows[i+1:]...)
			m.windows = append(m.windows, win)
			return
		}
	}
}

// isChildOf checks if child is a descendant of parent.
func (m *MDIPane) isChildOf(child, parent *window.Window) bool {
	for p := child.ParentWindow(); p != nil; p = p.ParentWindow() {
		if p == parent {
			return true
		}
	}
	return false
}

// positionWindow positions a new window using cascading.
func (m *MDIPane) positionWindow(win *window.Window) {
	m.mu.RLock()
	numWindows := len(m.windows)
	m.mu.RUnlock()

	clientArea := m.ClientArea()
	metrics := m.EffectiveCellMetrics()

	// Use the window's current size if set, otherwise use SizeHint
	bounds := win.Bounds()
	width := bounds.Width
	height := bounds.Height
	if width <= 0 || height <= 0 {
		hint := win.SizeHint()
		width = hint.Width
		height = hint.Height
	}

	// Cascade offset
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

// layoutContent updates the content trinket bounds.
func (m *MDIPane) layoutContent() {
	m.mu.RLock()
	content := m.content
	layout := m.layout
	m.mu.RUnlock()

	if content == nil {
		return
	}

	clientArea := m.ClientArea()

	if layout != nil {
		layout.Layout(m, clientArea)
	} else {
		content.SetBounds(clientArea)
	}
}

// detectResizeEdge determines which window edge(s) the interior point
// (x, y) is near, delegating to the shared window.ResizeEdgeAt so MDI
// children detect exactly the same edges and corners (top edge and top
// corners included) as desktop windows.
func (m *MDIPane) detectResizeEdge(win *window.Window, x, y core.Unit) int {
	if win.Flags()&window.WindowFlagNoResize != 0 || win.IsMaximized() {
		return window.ResizeEdgeNone
	}
	// Use the displayed (provisional-corralled) bounds so hover detection
	// agrees with what the user sees and with the press path (which commits
	// these bounds before detecting).
	grip := m.resizeGripFor(win)
	return window.ResizeEdgeAt(m.displayBounds(win), x, y, m.EffectiveCellMetrics(), grip)
}

// resizeGripFor is the effective resize-grip thickness for a child window
// (the surface's grip capability, discovered by ancestry).
func (m *MDIPane) resizeGripFor(win *window.Window) core.Unit {
	return window.EffectiveResizeGrip(win, core.FindResizeGrip(m.Self()))
}

// setResizeHover shows the translucent white overlay along the given resize
// edge(s) of win (window-local rects), the same highlight desktop windows
// get from the WindowManager. edge == ResizeEdgeNone clears it.
func (m *MDIPane) setResizeHover(win *window.Window, edge int) {
	if edge == window.ResizeEdgeNone {
		win.SetResizeHoverRects(nil)
		return
	}
	win.SetResizeHoverRects(window.ResizeEdgeRects(win, edge, m.resizeGripFor(win)))
}

// clearWindowHover clears any lingering per-widget hover on the window we
// last forwarded a hover move to (titlebar buttons and content), so nothing
// stays highlighted while a window is dragged or resized.
func (m *MDIPane) clearWindowHover() {
	if m.lastHoverWindow != nil {
		m.lastHoverWindow.HandleMouseMove(core.MouseMoveEvent{X: -1, Y: -1})
		m.lastHoverWindow = nil
	}
}

// updateResizeHover highlights the size-sensitive edge under the pointer on
// the topmost child window and clears it on the others - the MDI equivalent
// of WindowManager.updateResizeHover, called on plain moves (no drag). It
// repaints only when a highlight actually changed.
func (m *MDIPane) updateResizeHover(x, y core.Unit) {
	m.mu.RLock()
	windows := m.windows
	m.mu.RUnlock()
	pos := core.UnitPoint{X: x, Y: y}
	topmost := -1
	for i := len(windows) - 1; i >= 0; i-- {
		win := windows[i]
		if win.IsVisible() && !win.IsMinimized() && m.displayBounds(win).Contains(pos) {
			topmost = i
			break
		}
	}
	changed := false
	for i, win := range windows {
		var rects []core.UnitRect
		if i == topmost {
			if edge := m.detectResizeEdge(win, x, y); edge != window.ResizeEdgeNone {
				rects = window.ResizeEdgeRects(win, edge, m.resizeGripFor(win))
			}
		}
		if win.SetResizeHoverRects(rects) {
			changed = true
		}
	}
	if changed {
		m.Update()
	}
}

// SetBounds sets the MDI pane bounds and triggers layout.
func (m *MDIPane) SetBounds(bounds core.UnitRect) {
	m.TrinketBase.SetBounds(bounds)
	m.layoutContent()

	// Adjust maximized windows
	clientArea := m.ClientArea()
	for _, win := range m.windows {
		if win.IsMaximized() {
			win.SetBounds(clientArea)
		}
	}
}

// SetFont sets the font and propagates layout to all child windows.
func (m *MDIPane) SetFont(f *core.Font) {
	m.TrinketBase.SetFont(f)

	// Propagate layout to all child windows since font affects trinket sizing
	m.mu.RLock()
	windows := m.windows
	content := m.content
	m.mu.RUnlock()

	for _, win := range windows {
		win.Layout()
	}
	if content != nil {
		if container, ok := content.(core.Container); ok {
			container.Layout()
		}
	}
	m.Update()
}

// Children returns all child trinkets.
func (m *MDIPane) Children() []core.Trinket {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var children []core.Trinket
	if m.content != nil {
		children = append(children, m.content)
	}
	for _, win := range m.windows {
		children = append(children, win)
	}
	return children
}

// AddChild adds a child trinket.
func (m *MDIPane) AddChild(child core.Trinket) {
	if win, ok := child.(*window.Window); ok {
		m.AddWindow(win)
	} else {
		m.SetContent(child)
	}
}

// RemoveChild removes a child trinket.
func (m *MDIPane) RemoveChild(child core.Trinket) {
	if win, ok := child.(*window.Window); ok {
		m.RemoveWindow(win)
	} else if m.content == child {
		m.content = nil
	}
}

// ChildAt returns the child at the given position (in the pane's
// outer currency; window geometry lives in the interior).
func (m *MDIPane) ChildAt(pos core.UnitPoint) core.Trinket {
	pos.X, pos.Y = m.toInterior(pos.X, pos.Y)

	m.mu.RLock()
	windows := m.windows
	content := m.content
	m.mu.RUnlock()

	// Check windows from top to bottom
	for i := len(windows) - 1; i >= 0; i-- {
		win := windows[i]
		if win.IsVisible() && !win.IsMinimized() && m.displayBounds(win).Contains(pos) {
			return win
		}
	}

	// Check content
	if content != nil && content.Bounds().Contains(pos) {
		return content
	}

	return nil
}

// CursorShapeAt implements core.CursorShaper: resolve the mouse cursor
// through the SAME coordinate transform the MDI pane routes events with
// (outer -> interior, then the topmost child window's displayBounds), so
// a text field's I-beam inside an MDI child lands where clicks do. The
// generic ChildAt+Bounds cursor descent can't reproduce this - it would
// subtract the window's raw Bounds off the un-toInterior'd point.
func (m *MDIPane) CursorShapeAt(localX, localY core.Unit) core.CursorShape {
	ix, iy := m.toInterior(localX, localY)
	pos := core.UnitPoint{X: ix, Y: iy}

	m.mu.RLock()
	windows := m.windows
	content := m.content
	m.mu.RUnlock()

	for i := len(windows) - 1; i >= 0; i-- {
		win := windows[i]
		if !win.IsVisible() || win.IsMinimized() {
			continue
		}
		b := m.displayBounds(win)
		if b.Contains(pos) {
			// A size-sensitive edge wins (matches WindowManager.CursorAt),
			// so MDI children show resize cursors on every edge and corner;
			// otherwise defer to the trinket under the pointer (e.g. an
			// I-beam over a text field).
			if s := window.ResizeCursorForEdge(m.detectResizeEdge(win, pos.X, pos.Y)); s != core.CursorDefault {
				return s
			}
			return win.CursorShapeAt(pos.X-b.X, pos.Y-b.Y)
		}
	}
	if content != nil && content.Bounds().Contains(pos) {
		if cp, ok := content.(core.CursorProvider); ok {
			return cp.CursorShape()
		}
	}
	return core.CursorDefault
}

// Layout arranges children within the MDI pane.
// This also triggers layout on all child windows since font changes
// propagate via Layout calls from parent containers.
func (m *MDIPane) Layout() {
	m.layoutContent()

	// Propagate layout to all child windows
	m.mu.RLock()
	windows := m.windows
	m.mu.RUnlock()

	for _, win := range windows {
		win.Layout()
	}
}

// LayoutManager returns the layout manager.
func (m *MDIPane) LayoutManager() core.LayoutManager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.layout
}

// SetLayoutManager sets the layout manager.
func (m *MDIPane) SetLayoutManager(lm core.LayoutManager) {
	m.SetLayout(lm)
}

// SizeHint returns the preferred size.
// If minimum size is set (via SetMinimumSize), returns that as the hint.
// This allows the MDIPane to report a fixed size when embedded in a ScrollArea.
// Otherwise returns the current bounds size.
func (m *MDIPane) SizeHint() core.UnitSize {
	minSize := m.MinimumSize()
	if minSize.Width > 0 || minSize.Height > 0 {
		return minSize
	}
	return m.Bounds().Size()
}

// HandleFocusIn is called when MDIPane gains focus.
// This ensures the active window's focus is properly initialized,
// which is important when the MDIPane first becomes visible.
func (m *MDIPane) HandleFocusIn() {
	m.mu.RLock()
	active := m.activeWindow
	m.mu.RUnlock()

	// Ensure active window has a focused trinket
	if active != nil && !active.IsMinimized() {
		if fm := active.FocusManager(); fm != nil {
			if fm.FocusedTrinket() == nil {
				fm.FocusFirst()
			}
		}
	}

	m.Update()
}

// CollectFocusChain implements core.FocusChainProvider.
// When an MDI child window is active, MDIPane acts as a focus boundary -
// Tab navigation stays within the MDIPane and is forwarded to the active window.
// When no MDI child is active, focus can move through the content trinkets.
func (m *MDIPane) CollectFocusChain(collector func(core.Trinket)) {
	m.mu.RLock()
	activeWindow := m.activeWindow
	content := m.content
	m.mu.RUnlock()

	// When an MDI child is active, MDIPane is the only focusable item
	// in the chain. This makes MDIPane a focus trap - Tab events come to
	// MDIPane which forwards them to the active window.
	if activeWindow != nil && !activeWindow.IsMinimized() {
		collector(m)
		return
	}

	// No active MDI child - include MDIPane and its content in the chain
	collector(m)
	if content != nil {
		collector(content)
	}
}

// Paint renders the MDI pane.
func (m *MDIPane) Paint(p *core.Painter) {
	scheme := m.GetScheme()
	metrics := m.EffectiveCellMetrics()

	m.mu.RLock()
	bgChar := m.bgChar
	drawPattern := m.drawPattern
	content := m.content
	windows := m.windows
	m.mu.RUnlock()

	// Everything inside the pane lives in the interior denomination:
	// content bounds, window geometry, the background cell grid. One
	// exchange at the boundary, then all math is interior.
	outer, interior := m.denominations()
	ip := p.WithDenomination(outer, interior)
	clientArea := m.ClientArea() // interior currency

	// Draw background
	if drawPattern {
		// Draw pattern background (like Desktop)
		bgStyle := scheme.GetDesktopFill()
		for y := core.Unit(0); y < clientArea.Height; y += metrics.CellHeight {
			for x := core.Unit(0); x < clientArea.Width; x += metrics.CellWidth {
				ip.DrawCell(x, y, bgChar, bgStyle)
			}
		}
	} else {
		// Draw solid background (like ScrollArea)
		inheritedBg := m.EffectiveBackgroundColor()
		bgStyle := scheme.GetNormal(true).WithBg(inheritedBg)
		ip.FillRect(core.UnitRect{Width: clientArea.Width, Height: clientArea.Height}, ' ', bgStyle)
	}

	// Draw content if any
	if content != nil {
		content.Paint(ip)
	}

	// Paint windows from bottom to top
	for _, win := range windows {
		if win.IsVisible() && !win.IsMinimized() {
			// Draw at the provisional (corralled) position so windows
			// left off-screen by a pane shrink are nudged into view.
			winBounds := m.displayBounds(win)

			// Calculate visible portion within client area
			visibleBounds := winBounds.Intersection(clientArea)
			if visibleBounds.IsEmpty() {
				continue
			}

			// Offset into window's local coordinates
			localClipX := visibleBounds.X - winBounds.X
			localClipY := visibleBounds.Y - winBounds.Y
			localClip := core.UnitRect{
				X:      localClipX,
				Y:      localClipY,
				Width:  visibleBounds.Width,
				Height: visibleBounds.Height,
			}

			windowPainter := ip.WithOffset(winBounds.X, winBounds.Y).
				WithClip(localClip)
			win.Paint(windowPainter)
		}
	}
}

// HandleKeyPress handles keyboard input.
// When an MDI child window is active, MDIPane forwards ALL keyboard events
// to that window, including Tab and Shift+Tab. This ensures focus stays
// within the active window until the user clicks elsewhere or closes it.
func (m *MDIPane) HandleKeyPress(event core.KeyPressEvent) bool {
	m.mu.RLock()
	active := m.activeWindow
	m.mu.RUnlock()

	// Check for MDI-specific shortcuts (window switching)
	switch event.Key {
	case "M-Tab", "C-Tab":
		m.NextWindow()
		return true
	case "M-S-Tab", "C-S-Tab":
		m.PrevWindow()
		return true
	}

	// Forward ALL key events to the active window.
	// This includes Tab and Shift+Tab which the window's FocusManager handles
	// for internal focus navigation.
	if active != nil && !active.IsMinimized() {
		// Ensure the window has a focused trinket before processing key events.
		// This is critical for proper Tab/Shift+Tab behavior - if no trinket is
		// focused, the first key press should establish focus.
		// BUT: don't do this if the title bar has focus (e.g., during window move/resize).
		if fm := active.FocusManager(); fm != nil {
			if fm.FocusedTrinket() == nil && !active.HasTitleFocus() {
				fm.FocusFirst()
			}
		}

		if active.HandleKeyPress(event) {
			// A key the child itself acts on is a genuine child interaction:
			// commit the cycle run's sequence. The M-Tab/M-S-Tab steps above
			// return before here, so stepping never commits.
			m.endCycleSession()
			return true
		}
	}

	return false
}

// HandleMousePress handles mouse clicks.
func (m *MDIPane) HandleMousePress(event core.MousePressEvent) bool {
	// Coordinates arrive in the pane's outer currency; window geometry
	// and all drag state live in the interior. Exchange once at entry.
	event.X, event.Y = m.toInterior(event.X, event.Y)

	// Any click inside the MDIPane should give it focus, so keyboard events
	// (including Tab) route through MDIPane to the active child window.
	// Use SetFocusWithoutScroll since mouse clicks prove visibility - no need to scroll.
	m.SetFocusWithoutScroll()

	m.mu.RLock()
	windows := m.windows
	content := m.content
	m.mu.RUnlock()

	metrics := m.EffectiveCellMetrics()

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

			// Check for resize edge first - resize operations raise immediately
			resizeEdge := m.detectResizeEdge(win, event.X, event.Y)
			if resizeEdge != window.ResizeEdgeNone {
				m.ActivateWindow(win)
				m.mu.Lock()
				m.resizing = win
				m.resizeEdge = resizeEdge
				m.resizeStartX = event.X
				m.resizeStartY = event.Y
				m.resizeOriginal = bounds
				m.pressedWindow = nil // Clear pressed window for resize
				m.mu.Unlock()
				m.setResizeHover(win, resizeEdge) // white overlay on the grabbed edge
				return true
			}

			// Check for title bar interaction - titlebar operations raise
			// immediately. On a graphical frame the titlebar sits INSIDE the
			// top border (client-area math in window.go), so the draggable
			// row runs from the border down a full cell; without the border
			// offset the region ended a border-thickness short of the
			// titlebar's bottom (the top border above it is a resize edge,
			// handled first). Cell frames draw the title on the top row, so
			// no offset.
			titleBottom := metrics.CellHeight
			if core.FindGraphicalFrames(win) {
				titleBottom += core.FindFrameBorderUnits(win)
			}
			if event.Y < bounds.Y+titleBottom &&
				win.Flags()&window.WindowFlagNoTitle == 0 {

				m.ActivateWindow(win)

				// Let window handle button clicks
				localEvent := event
				localEvent.X -= bounds.X
				localEvent.Y -= bounds.Y
				if win.HandleMousePress(localEvent) {
					// Update click tracking
					m.mu.Lock()
					m.lastClickTime = time.Now()
					m.lastClickX = event.X
					m.lastClickY = event.Y
					m.lastClickWindow = win
					m.pressedWindow = nil
					m.mu.Unlock()
					return true
				}

				// Check for double-click
				now := time.Now()
				m.mu.Lock()
				isDoubleClick := m.lastClickWindow == win &&
					now.Sub(m.lastClickTime) < 400*time.Millisecond &&
					abs(int(event.X-m.lastClickX)) < int(metrics.CellWidth) &&
					abs(int(event.Y-m.lastClickY)) < int(metrics.CellHeight)
				m.lastClickTime = now
				m.lastClickX = event.X
				m.lastClickY = event.Y
				m.lastClickWindow = win
				m.mu.Unlock()

				if isDoubleClick && win.Flags()&window.WindowFlagNoMaximize == 0 {
					if win.IsMaximized() {
						win.Restore()
					} else {
						m.MaximizeWindow(win)
					}
					m.mu.Lock()
					m.lastClickWindow = nil
					m.pressedWindow = nil
					m.mu.Unlock()
					return true
				}

				// Start drag
				if win.Flags()&window.WindowFlagNoMove == 0 {
					m.mu.Lock()
					m.dragging = win
					m.dragStartX = event.X
					m.dragStartY = event.Y
					m.dragOffsetX = event.X - bounds.X
					m.dragOffsetY = event.Y - bounds.Y
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

	// Clicking on the background or content deactivates the active MDI child
	m.DeactivateActiveWindow()

	// Forward to content
	if content != nil {
		return content.HandleMousePress(event)
	}

	return false
}

// HandleMouseMove handles mouse movement.
func (m *MDIPane) HandleMouseMove(event core.MouseMoveEvent) bool {
	// Outer -> interior at the boundary (see HandleMousePress).
	event.X, event.Y = m.toInterior(event.X, event.Y)

	m.mu.Lock()
	dragging := m.dragging
	offsetX := m.dragOffsetX
	offsetY := m.dragOffsetY
	resizing := m.resizing
	resizeEdge := m.resizeEdge
	resizeStartX := m.resizeStartX
	resizeStartY := m.resizeStartY
	resizeOriginal := m.resizeOriginal
	m.mu.Unlock()

	// While a window is being dragged or resized, nothing should show a
	// hover highlight: clear whatever was hovered when the gesture began so
	// it doesn't linger under the moving window.
	if resizing != nil || dragging != nil {
		m.clearWindowHover()
	}

	// Handle resize
	if resizing != nil {
		newBounds := window.ApplyResize(resizeOriginal, resizeEdge,
			event.X-resizeStartX, event.Y-resizeStartY,
			m.EffectiveCellMetrics(), !core.FindSmoothPositioning(m.Self()), m.ClientArea())

		resizing.SetBounds(newBounds)
		m.setResizeHover(resizing, resizeEdge) // overlay follows the new bounds
		m.Update()
		return true
	}

	// Handle drag
	if dragging != nil {
		justRestored := false
		clientArea := m.ClientArea()
		metrics := m.EffectiveCellMetrics()

		// Handle restore from maximized
		if dragging.IsMaximized() {
			newY := event.Y - offsetY
			if newY >= clientArea.Y {
				oldBounds := dragging.Bounds()
				dragging.Restore()
				justRestored = true
				newBounds := dragging.Bounds()
				dragging.Layout()

				proportion := float64(offsetX) / float64(oldBounds.Width)
				offsetX = core.Unit(proportion * float64(newBounds.Width))

				m.mu.Lock()
				m.dragOffsetX = offsetX
				m.mu.Unlock()
			} else {
				return true
			}
		}

		newX := event.X - offsetX
		newY := event.Y - offsetY

		bounds := dragging.Bounds()
		bounds.X = newX
		bounds.Y = newY

		// Maximize gesture: only when the POINTER itself moves above the pane's
		// top (into/past the pane edge), not merely when the window's top edge
		// is lifted there by the grab offset - which fired too eagerly.
		if event.Y < clientArea.Y && dragging.Flags()&window.WindowFlagNoMaximize == 0 && !justRestored {
			if !dragging.IsMaximized() {
				m.MaximizeWindow(dragging)
			}
			return true
		}

		// Keep the window retrievable within the pane: title bar
		// vertically inside the client area, at least a couple of
		// columns visible horizontally on each side - the same corral
		// the desktop enforces for its windows.
		bounds = window.ClampWindowToClientArea(bounds, clientArea, metrics)

		// Snap to cell boundaries unless the surface supports smooth
		// (unit-granular) positioning, as pixel surfaces do
		if !core.FindSmoothPositioning(m.Self()) {
			bounds = metrics.AlignRect(bounds)
		}
		dragging.SetBounds(bounds)
		m.Update()
		return true
	}

	// No drag or resize in progress: keep the resize-edge hover overlay in
	// sync with the pointer (like desktop windows). A held button means a
	// gesture began elsewhere and is passing through - the edge highlight is a
	// hover affordance, so suppress it and clear any lingering band.
	if event.Buttons == 0 {
		m.updateResizeHover(event.X, event.Y)
	} else {
		// Off-surface point clears every window's edge overlay.
		m.updateResizeHover(-1, -1)
	}

	m.mu.RLock()
	active := m.activeWindow
	content := m.content
	windows := m.windows
	m.mu.RUnlock()

	// A held button means a drag/selection in progress: keep routing to the
	// active window (drag capture) so the gesture doesn't jump windows.
	// A plain move is hover: route it to the topmost window under the
	// pointer so its widgets (titlebar buttons, splitters, ...) highlight
	// even when it isn't the active window, and clear the window we last
	// hovered when the pointer leaves it.
	if event.Buttons != 0 {
		if active != nil && !active.IsMinimized() {
			bounds := m.displayBounds(active)
			localEvent := event
			localEvent.X -= bounds.X
			localEvent.Y -= bounds.Y
			if active.HandleMouseMove(localEvent) {
				m.Update()
				return true
			}
		}
	} else {
		pos := core.UnitPoint{X: event.X, Y: event.Y}
		var hoverTarget *window.Window
		for i := len(windows) - 1; i >= 0; i-- {
			win := windows[i]
			if win.IsVisible() && !win.IsMinimized() && m.displayBounds(win).Contains(pos) {
				hoverTarget = win
				break
			}
		}
		if m.lastHoverWindow != nil && m.lastHoverWindow != hoverTarget {
			// Send an out-of-bounds move so the previously-hovered window
			// clears its widgets' hover states.
			m.lastHoverWindow.HandleMouseMove(core.MouseMoveEvent{X: -1, Y: -1})
		}
		m.lastHoverWindow = hoverTarget
		if hoverTarget != nil {
			bounds := m.displayBounds(hoverTarget)
			localEvent := event
			localEvent.X -= bounds.X
			localEvent.Y -= bounds.Y
			// Over a resize edge a press resizes, so no widget under the
			// pointer would fire: send an out-of-bounds move to clear all
			// hover in the window (titlebar buttons and edge-adjacent
			// content like a scrollbar tucked against the frame).
			if m.detectResizeEdge(hoverTarget, event.X, event.Y) != window.ResizeEdgeNone {
				localEvent.X, localEvent.Y = -1, -1
			}
			hoverTarget.HandleMouseMove(localEvent)
			// The pointer is over this window: consume the move so the
			// hover can't leak to windows stacked underneath or the
			// background content. First send the background content an
			// out-of-bounds move so any item it had hovered (a parent-window
			// control) doesn't stay stuck now that the pointer is over the
			// child.
			if content != nil {
				if handler, ok := content.(interface {
					HandleMouseMove(core.MouseMoveEvent) bool
				}); ok {
					handler.HandleMouseMove(core.MouseMoveEvent{X: -1, Y: -1})
				}
			}
			m.Update()
			return true
		}
	}

	// Forward to content (for hover states on buttons, etc.)
	if content != nil {
		if handler, ok := content.(interface {
			HandleMouseMove(core.MouseMoveEvent) bool
		}); ok {
			if handler.HandleMouseMove(event) {
				m.Update()
				return true
			}
		}
	}

	return false
}

// HandleMouseWheel forwards a wheel event to the hosted window under
// the pointer (topmost first), else the background content.
func (m *MDIPane) HandleMouseWheel(event core.MouseWheelEvent) bool {
	event.X, event.Y = m.toInterior(event.X, event.Y)
	pos := core.UnitPoint{X: event.X, Y: event.Y}

	m.mu.RLock()
	windows := make([]*window.Window, len(m.windows))
	copy(windows, m.windows)
	content := m.content
	m.mu.RUnlock()

	for i := len(windows) - 1; i >= 0; i-- {
		win := windows[i]
		b := m.displayBounds(win)
		if !win.IsVisible() || win.IsMinimized() || !b.Contains(pos) {
			continue
		}
		local := event
		local.X -= b.X
		local.Y -= b.Y
		return win.HandleMouseWheel(local)
	}
	if content != nil {
		if handler, ok := content.(interface {
			HandleMouseWheel(core.MouseWheelEvent) bool
		}); ok {
			return handler.HandleMouseWheel(event)
		}
	}
	return false
}

// HandleMouseRelease handles mouse release.
func (m *MDIPane) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	// Outer -> interior at the boundary (see HandleMousePress).
	event.X, event.Y = m.toInterior(event.X, event.Y)

	m.mu.Lock()
	dragging := m.dragging
	resizing := m.resizing
	pressedWin := m.pressedWindow
	m.dragging = nil
	m.resizing = nil
	m.resizeEdge = window.ResizeEdgeNone
	m.pressedWindow = nil
	m.mu.Unlock()

	if resizing != nil {
		resizing.SetResizeHoverRects(nil) // drop the overlay when the resize ends
		m.Update()
	}
	if dragging != nil || resizing != nil {
		return true
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

			topmostAtPoint := (*window.Window)(nil)
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

	// Forward to active window
	m.mu.RLock()
	active := m.activeWindow
	content := m.content
	m.mu.RUnlock()

	if active != nil && !active.IsMinimized() {
		bounds := m.displayBounds(active)
		localEvent := event
		localEvent.X -= bounds.X
		localEvent.Y -= bounds.Y
		if active.HandleMouseRelease(localEvent) {
			m.Update()
			return true
		}
	}

	// Forward to content (for buttons and other trinkets in the MDI background)
	if content != nil {
		if handler, ok := content.(interface {
			HandleMouseRelease(core.MouseReleaseEvent) bool
		}); ok {
			if handler.HandleMouseRelease(event) {
				m.Update()
				return true
			}
		}
	}

	return false
}

// abs returns the absolute value of an integer.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Verify MDIPane implements Container and KeyboardBlurChildrenProvider
var _ core.Container = (*MDIPane)(nil)
var _ core.KeyboardBlurChildrenProvider = (*MDIPane)(nil)
