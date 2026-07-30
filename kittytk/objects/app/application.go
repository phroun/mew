// Package app provides the main application framework for KittyTK.
package app

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/style"
)

// Application is the main entry point for a TUI application.
// It manages the event loop, windows, and global state.
// Application implements the trinkets.ApplicationProvider interface
// for integration with multi-application Desktop environments.
type Application struct {
	mu sync.RWMutex

	// objectID is this application's stable protocol identity, drawn from the
	// same ObjectID space as windows and trinkets. It makes a running app a
	// first-class object: something a client can refer to and - in time - set
	// application-wide properties on through the same protocol syntax used for
	// windows and trinkets.
	objectID core.ObjectID

	// wireNameAllowed reports whether this connection's trust is independent of
	// the app name (a local app, or an "Always for All Apps" client), so a
	// wire Set "name" may change it to anything. When false, a wire rename must
	// match the authorized (connect-time) name. In-process SetName is
	// unaffected. See SetWireNameChangeAllowed.
	wireNameAllowed bool

	// Backend for rendering
	backend core.RenderBackend

	// Window manager
	windowManager *window.WindowManager

	// Global focus manager
	focusManager *core.GlobalFocusManager

	// Accessibility manager
	accessibilityManager *core.AccessibilityManager

	// Shortcut map for global shortcuts
	shortcuts *core.ShortcutMap

	// Theme
	theme *style.Theme

	// Desktop trinket (behind all windows)
	desktop core.Trinket

	// Running state
	running atomic.Bool

	// Quit channel
	quitChan chan struct{}

	// Update request channel
	updateChan chan struct{}

	// Timer events
	timers     []*Timer
	timerMutex sync.Mutex

	// Callbacks
	onStartup    func()
	onShutdown   func()
	onIdle       func()
	onActivate   func()
	onDeactivate func()

	// Event filters (processed before trinkets)
	eventFilters []EventFilter

	// Application name
	name string

	// menuName is the title of the application's own menu on its
	// detached main window's menu bar. It is only used when the main
	// window is torn off an SDL desktop; on the desktop bar the app menu
	// carries the app name. Defaults to "≡".
	menuName string

	// Exit code
	exitCode int

	// Windows owned by this application (for ApplicationProvider interface)
	windows []*window.Window

	// mainWindow is the app's optional main window; when detached it
	// hosts the app's own menu bar and the desktop shows a reduced bar.
	mainWindow *window.Window

	// multiWindow declares that the app manages more than one primary
	// window. See SetMultiWindow.
	multiWindow bool

	// contextOnly suppresses the automatic graphical Edit menu. See
	// SetContextOnly.
	contextOnly bool

	// Menu bar content for this application
	menuBarContent []*trinkets.Menu

	// Command registry: menu (and future) handlers keyed by stable
	// command ID - the app-side half of the D2 dispatch seam.
	commands *core.CommandRegistry

	// Status bar content for this application
	statusBarContent []trinkets.StatusSection

	// Pass-next-key-to-trinket mode for this application
	passNextKeyToTrinket bool

	// Saved status bar content to restore after pass-next-key mode
	savedStatusBarContent []trinkets.StatusSection
}

// EventFilter is a function that can intercept events before they reach trinkets.
// Return true to consume the event (prevent further processing).
type EventFilter func(event core.Event) bool

// Timer represents a scheduled timer callback.
type Timer struct {
	ID       int
	Interval time.Duration
	Repeat   bool
	Callback func()
	nextFire time.Time
	stopped  bool
}

// New creates a new application instance.
// Applications are containers for windows, menus, and status bar content.
// Multiple applications can coexist on a single Desktop.
// The backend parameter is optional - pass nil if the Desktop owns the backend.
func New(backend core.RenderBackend) *Application {
	app := &Application{
		objectID:             core.NextObjectID(),
		quitChan:             make(chan struct{}),
		updateChan:           make(chan struct{}, 100),
		theme:                style.DefaultTheme(),
		accessibilityManager: core.NewAccessibilityManager(),
		commands:             core.NewCommandRegistry(),
	}

	if backend != nil {
		app.backend = backend
		app.windowManager = window.NewWindowManager()
		// Wire up repaint callback for window dragging/updates
		app.windowManager.SetOnRepaintNeeded(func() {
			app.RequestUpdate()
		})
		app.focusManager = core.NewGlobalFocusManager()
		app.shortcuts = core.DefaultShortcuts()

		// Connect accessibility to focus manager
		app.focusManager.SetAccessibilityManager(app.accessibilityManager)
	}

	return app
}

// NewSecondary creates a new independent application instance.
// Unlike New(), this creates a fresh Application that is NOT the singleton.
// Secondary applications are used for multi-app desktops where each app
// has its own windows, menus, and status bar content.
// Secondary apps share the desktop's WindowManager and don't have their own event loop.
func NewSecondary() *Application {
	return &Application{
		objectID:   core.NextObjectID(),
		quitChan:   make(chan struct{}),
		updateChan: make(chan struct{}, 100),
		theme:      style.DefaultTheme(),
		commands:   core.NewCommandRegistry(),
	}
}

// ObjectID returns the application's stable protocol identity, drawn from the
// same space as Window.ObjectID and TrinketBase.ObjectID. It lets a client
// refer to a running application - and is the hook for setting
// application-wide properties over the protocol the way windows and trinkets
// already accept them.
func (app *Application) ObjectID() core.ObjectID {
	return app.objectID
}

// SetName sets the application name.
func (app *Application) SetName(name string) {
	app.mu.Lock()
	app.name = name
	app.mu.Unlock()
}

// Name returns the application name.
func (app *Application) Name() string {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.name
}

// SetMenuName sets the title of the application's own menu as it appears
// on the detached main window's menu bar (e.g. "&File" or "&Menu"). It is
// ignored while the app is docked in an SDL desktop. An empty value falls
// back to the default "≡".
func (app *Application) SetMenuName(name string) {
	app.mu.Lock()
	app.menuName = name
	app.mu.Unlock()
}

// MenuName returns the detached-menu title, defaulting to "≡" when unset.
func (app *Application) MenuName() string {
	app.mu.RLock()
	defer app.mu.RUnlock()
	if app.menuName == "" {
		return "≡"
	}
	return app.menuName
}

// Backend returns the render backend.
func (app *Application) Backend() core.RenderBackend {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.backend
}

// WindowManager returns the window manager.
func (app *Application) WindowManager() *window.WindowManager {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.windowManager
}

// FocusManager returns the global focus manager.
func (app *Application) FocusManager() *core.GlobalFocusManager {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.focusManager
}

// AccessibilityManager returns the accessibility manager.
func (app *Application) AccessibilityManager() *core.AccessibilityManager {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.accessibilityManager
}

// Shortcuts returns the global shortcut map.
func (app *Application) Shortcuts() *core.ShortcutMap {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.shortcuts
}

// Theme returns the current theme.
func (app *Application) Theme() *style.Theme {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.theme
}

// SetTheme sets the current theme.
func (app *Application) SetTheme(theme *style.Theme) {
	app.mu.Lock()
	app.theme = theme
	app.mu.Unlock()
	app.RequestUpdate()
}

// SetDesktop sets the desktop trinket.
func (app *Application) SetDesktop(desktop core.Trinket) {
	app.mu.Lock()
	app.desktop = desktop
	wm := app.windowManager
	app.mu.Unlock()

	if wm != nil {
		wm.SetDesktop(desktop)

		// Wire up dock row integration if desktop is a *trinkets.Desktop
		if d, ok := desktop.(*trinkets.Desktop); ok {
			dockRow := d.DockRow()
			if dockRow != nil {
				// When a window is minimized, add it to the dock row
				wm.SetOnWindowMinimized(func(win *window.Window) {
					entry := &trinkets.DockEntry{
						Title:    win.Title(),
						WindowID: win.ObjectID(),
						OnClick: func() {
							wm.RestoreWindow(win)
						},
					}
					dockRow.AddEntry(entry)
				})

				// When a window is restored, remove it from the dock row
				wm.SetOnWindowRestored(func(win *window.Window) {
					dockRow.RemoveEntryByID(win.ObjectID())
				})
			}

			// Wire up menu bar to deactivate windows when a menu opens
			if menuBar := d.MenuBar(); menuBar != nil {
				menuBar.SetOnMenuOpen(func() {
					wm.DeactivateActiveWindow()
				})
				// Wire up menu bar dismiss to restore previous window
				menuBar.SetOnMenuDismiss(func() {
					wm.RestorePreviousActiveWindow()
				})
			}
		}
	}
}

// Desktop returns the desktop trinket.
func (app *Application) Desktop() core.Trinket {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.desktop
}

// SetOnStartup sets the startup callback.
func (app *Application) SetOnStartup(handler func()) {
	app.mu.Lock()
	app.onStartup = handler
	app.mu.Unlock()
}

// SetOnShutdown sets the shutdown callback.
func (app *Application) SetOnShutdown(handler func()) {
	app.mu.Lock()
	app.onShutdown = handler
	app.mu.Unlock()
}

// SetOnIdle sets the idle callback (called when no events are pending).
func (app *Application) SetOnIdle(handler func()) {
	app.mu.Lock()
	app.onIdle = handler
	app.mu.Unlock()
}

// AddEventFilter adds an event filter.
func (app *Application) AddEventFilter(filter EventFilter) {
	app.mu.Lock()
	app.eventFilters = append(app.eventFilters, filter)
	app.mu.Unlock()
}

// Run starts the application event loop.
// Returns the exit code when the application quits.
func (app *Application) Run() int {
	app.mu.Lock()
	backend := app.backend
	desktop := app.desktop
	onStartup := app.onStartup
	onShutdown := app.onShutdown
	app.mu.Unlock()

	if backend == nil {
		return 1
	}

	// If we have a Desktop, delegate to it
	if desktop != nil {
		if d, ok := desktop.(*trinkets.Desktop); ok {
			// Pass backend to Desktop
			d.SetBackend(backend)

			// Wire up Application callbacks through Desktop
			d.SetOnStartup(func() {
				// Update screen bounds for window manager
				app.mu.Lock()
				wm := app.windowManager
				app.mu.Unlock()
				size := backend.Size()
				wm.SetScreenBounds(core.UnitRect{Width: size.Width, Height: size.Height})

				// Mark Application as running
				app.running.Store(true)

				// Call Application's startup handler
				if onStartup != nil {
					onStartup()
				}
			})

			d.SetOnShutdown(func() {
				app.running.Store(false)
				if onShutdown != nil {
					onShutdown()
				}
			})

			// Run via Desktop
			return d.Run()
		}
	}

	// Fallback: no Desktop, run directly (legacy mode)
	// DEPRECATED: Application-centric event loop is legacy code.
	// Applications should use Desktop.Run() instead.
	// This code path will be removed in a future version.
	panic("DEPRECATED: Application.Run() fallback mode is no longer supported. " +
		"Applications must be associated with a Desktop and use Desktop.Run(). " +
		"This legacy code path has been removed to simplify the architecture.")

	/* Legacy code preserved for reference during refactoring:
	if err := backend.Init(); err != nil {
		return 1
	}
	defer backend.Shutdown()

	app.mu.Lock()
	wm := app.windowManager
	app.mu.Unlock()

	size := backend.Size()
	wm.SetScreenBounds(core.UnitRect{Width: size.Width, Height: size.Height})

	app.running.Store(true)
	defer app.running.Store(false)

	if onStartup != nil {
		onStartup()
	}

	app.eventLoop()

	app.mu.RLock()
	exitCode := app.exitCode
	app.mu.RUnlock()

	if onShutdown != nil {
		onShutdown()
	}

	return exitCode
	*/

	// Unreachable - panic above ensures we never get here
	return 1
}

// eventLoop is the main event processing loop.
// DEPRECATED: This is legacy code from Application-centric mode.
// The Desktop now owns the event loop.
func (app *Application) eventLoop() {
	panic("DEPRECATED: Application.eventLoop() is no longer supported. Use Desktop.Run() instead.")
	/* Legacy code:
	for app.running.Load() {
		app.processTimers()
		app.processEvents()
		app.render()
	}
	*/
}

// processEvents handles pending events.
// DEPRECATED: This is legacy code from Application-centric mode.
// The Desktop now owns event processing.
func (app *Application) processEvents() {
	app.mu.RLock()
	backend := app.backend
	wm := app.windowManager
	fm := app.focusManager
	onIdle := app.onIdle
	app.mu.RUnlock()

	// Process all pending events
	for {
		event := backend.PollEvent()
		if event == nil {
			// No more events
			if onIdle != nil {
				onIdle()
			}

			// Wait for next event or update request
			select {
			case <-app.quitChan:
				return
			case <-app.updateChan:
				return
			default:
				// Wait briefly for events
				event = app.waitEventWithTimeout(50 * time.Millisecond)
				if event == nil {
					return
				}
			}
		}

		// Run through event filters
		if app.filterEvent(event) {
			continue
		}

		// Handle event based on type
		switch e := event.(type) {
		case core.ResizeEvent:
			wm.SetScreenBounds(core.UnitRect{Width: e.Width, Height: e.Height})

		case core.QuitEvent:
			app.running.Store(false)
			return

		case core.KeyPressEvent:
			// Check global shortcuts first
			if app.handleShortcut(e) {
				continue
			}
			// Try focus manager
			if fm.HandleKeyPress(e) {
				continue
			}
			// Pass to window manager
			wm.HandleKeyPress(e)

		case core.MousePressEvent:
			wm.HandleMousePress(e)

		case core.MouseMoveEvent:
			wm.HandleMouseMove(e)

		case core.MouseReleaseEvent:
			wm.HandleMouseRelease(e)
		}
	}
}

// waitEventWithTimeout waits for an event with a timeout.
func (app *Application) waitEventWithTimeout(timeout time.Duration) core.Event {
	app.mu.RLock()
	backend := app.backend
	app.mu.RUnlock()

	// Simple polling with timeout
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		event := backend.PollEvent()
		if event != nil {
			return event
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// filterEvent runs an event through the filter chain.
func (app *Application) filterEvent(event core.Event) bool {
	app.mu.RLock()
	filters := app.eventFilters
	app.mu.RUnlock()

	for _, filter := range filters {
		if filter(event) {
			return true
		}
	}
	return false
}

// handleShortcut checks if a key event matches a global shortcut.
func (app *Application) handleShortcut(event core.KeyPressEvent) bool {
	// Handle common shortcuts directly using key handler format
	switch event.Key {
	case "^Q": // Ctrl+Q - Quit
		app.Quit()
		return true
	case "^W": // Ctrl+W - Close window
		app.mu.RLock()
		wm := app.windowManager
		app.mu.RUnlock()
		if wm != nil {
			if active := wm.ActiveWindow(); active != nil {
				active.Close()
				return true
			}
		}
	}

	// Check registered shortcuts using key handler format directly
	app.mu.RLock()
	shortcuts := app.shortcuts
	app.mu.RUnlock()

	if shortcuts == nil {
		return false
	}

	// Direct lookup - key handler format is the source of truth
	actionID := shortcuts.FindActionByKey(event.Key)
	if actionID != "" {
		// TODO: Trigger the action through action registry
		return true
	}

	return false
}

// render redraws the screen.
// DEPRECATED: This is legacy code from Application-centric mode.
// The Desktop now owns rendering.
func (app *Application) render() {
	app.mu.RLock()
	backend := app.backend
	wm := app.windowManager
	theme := app.theme
	app.mu.RUnlock()

	backend.BeginFrame()

	// Clear with theme background
	backend.Clear(theme.Normal)

	// Create painter
	painter := core.NewPainter(backend)

	// Paint window manager (includes desktop and windows)
	wm.Paint(painter)

	backend.EndFrame()
}

// processTimers checks and fires due timers.
// DEPRECATED: This is legacy code from Application-centric mode.
// The Desktop now owns timer processing via Desktop.ProcessTimers().
func (app *Application) processTimers() {
	app.timerMutex.Lock()
	now := time.Now()
	var toFire []*Timer
	var remaining []*Timer

	for _, timer := range app.timers {
		if timer.stopped {
			continue
		}

		if now.After(timer.nextFire) || now.Equal(timer.nextFire) {
			toFire = append(toFire, timer)
			if timer.Repeat {
				timer.nextFire = now.Add(timer.Interval)
				remaining = append(remaining, timer)
			}
		} else {
			remaining = append(remaining, timer)
		}
	}

	app.timers = remaining
	app.timerMutex.Unlock()

	// Fire timers outside lock
	for _, timer := range toFire {
		if timer.Callback != nil {
			timer.Callback()
		}
	}

	// Also process Desktop timers if we have a Desktop
	app.mu.RLock()
	desktop := app.desktop
	app.mu.RUnlock()
	if desktop != nil {
		if d, ok := desktop.(*trinkets.Desktop); ok {
			d.ProcessTimers()
		}
	}
}

// RequestUpdate requests a screen update.
func (app *Application) RequestUpdate() {
	select {
	case app.updateChan <- struct{}{}:
	default:
		// Channel full, update already pending
	}
}

// Quit requests the application to quit.
func (app *Application) Quit() {
	app.QuitWithCode(0)
}

// QuitWithCode requests the application to quit with an exit code.
func (app *Application) QuitWithCode(code int) {
	app.mu.Lock()
	app.exitCode = code
	app.mu.Unlock()
	app.running.Store(false)
	close(app.quitChan)
}

// IsRunning returns whether the application is running.
func (app *Application) IsRunning() bool {
	return app.running.Load()
}

// StartTimer starts a single-shot timer.
func (app *Application) StartTimer(interval time.Duration, callback func()) *Timer {
	return app.startTimerInternal(interval, false, callback)
}

// StartRepeatingTimer starts a repeating timer.
func (app *Application) StartRepeatingTimer(interval time.Duration, callback func()) *Timer {
	return app.startTimerInternal(interval, true, callback)
}

func (app *Application) startTimerInternal(interval time.Duration, repeat bool, callback func()) *Timer {
	app.timerMutex.Lock()
	defer app.timerMutex.Unlock()

	timer := &Timer{
		ID:       len(app.timers) + 1,
		Interval: interval,
		Repeat:   repeat,
		Callback: callback,
		nextFire: time.Now().Add(interval),
	}
	app.timers = append(app.timers, timer)
	return timer
}

// StopTimer stops a timer.
func (app *Application) StopTimer(timer *Timer) {
	if timer != nil {
		timer.stopped = true
	}
}

// ProcessEvents processes pending events without blocking.
// Useful for modal dialogs or long-running operations.
func (app *Application) ProcessEvents() {
	app.processEvents()
}

// ProcessEventsAndRender processes events and renders.
func (app *Application) ProcessEventsAndRender() {
	app.processEvents()
	app.render()
}

// Alert shows a simple message to the user.
// This is a convenience method for simple notifications.
func (app *Application) Alert(title, message string) {
	app.mu.RLock()
	am := app.accessibilityManager
	app.mu.RUnlock()

	if am != nil {
		am.AnnounceAlert(message)
	}
	// TODO: Show alert dialog when dialogs are implemented
}

// Beep produces an audible alert.
func (app *Application) Beep() {
	app.mu.RLock()
	backend := app.backend
	app.mu.RUnlock()

	if backend != nil {
		backend.Beep()
	}
}

// Clipboard returns the clipboard contents.
func (app *Application) Clipboard() string {
	app.mu.RLock()
	backend := app.backend
	app.mu.RUnlock()

	if backend != nil {
		return backend.GetClipboard()
	}
	return ""
}

// SetClipboard sets the clipboard contents.
func (app *Application) SetClipboard(text string) {
	app.mu.RLock()
	backend := app.backend
	app.mu.RUnlock()

	if backend != nil {
		backend.SetClipboard(text)
	}
}

// ScreenSize returns the current screen size in units.
func (app *Application) ScreenSize() core.UnitSize {
	app.mu.RLock()
	backend := app.backend
	app.mu.RUnlock()

	if backend != nil {
		return backend.Size()
	}
	return core.UnitSize{}
}

// Compile-time check that Application implements trinkets.ApplicationProvider
var _ trinkets.ApplicationProvider = (*Application)(nil)

// --- ApplicationProvider interface implementation ---

// Windows returns all windows owned by this application.
func (app *Application) Windows() []*window.Window {
	app.mu.RLock()
	defer app.mu.RUnlock()
	result := make([]*window.Window, len(app.windows))
	copy(result, app.windows)
	return result
}

// AddWindow adds a window to this application.
func (app *Application) AddWindow(w *window.Window) {
	app.mu.Lock()
	app.windows = append(app.windows, w)
	desktop := app.desktop
	app.mu.Unlock()

	// Stamp the owning application so the manager can scope
	// application-level modal blocking to this app's windows.
	w.SetAppID(app.ObjectID())

	// Closing the window must drop it from this app's list too. The manager
	// and tear-off host each reassign the single onCloseComplete slot for
	// their own removal, so use an accumulating observer that survives -
	// otherwise a dismissed dialog would linger in Windows() (and the Window
	// menu) forever. forgetWindow only touches the app's slice; the manager
	// and host already forget the window through their own close paths.
	w.AddOnClosed(func() { app.forgetWindow(w) })

	// Also add to Desktop's WindowManager if we have one
	if desktop != nil {
		if d, ok := desktop.(*trinkets.Desktop); ok {
			if wm := d.WindowManager(); wm != nil {
				wm.AddWindow(w)
			}
			// If this app's main window is already torn off, a new
			// non-tearable child (e.g. a dialog) is torn off too, so it
			// appears with its detached parent rather than docked here.
			d.SyncAddedWindowDetachState(w)
		}
	}
}

// forgetWindow removes w from the application's window list only (the
// manager and tear-off host handle their own removal on close).
func (app *Application) forgetWindow(w *window.Window) {
	app.mu.Lock()
	for i, win := range app.windows {
		if win == w {
			app.windows = append(app.windows[:i], app.windows[i+1:]...)
			break
		}
	}
	app.mu.Unlock()
}

// RemoveWindow removes a window from this application.
func (app *Application) RemoveWindow(w *window.Window) {
	app.mu.Lock()
	for i, win := range app.windows {
		if win == w {
			app.windows = append(app.windows[:i], app.windows[i+1:]...)
			break
		}
	}
	desktop := app.desktop
	app.mu.Unlock()

	// Also remove from Desktop's WindowManager if we have one
	if desktop != nil {
		if d, ok := desktop.(*trinkets.Desktop); ok {
			if wm := d.WindowManager(); wm != nil {
				wm.RemoveWindow(w)
			}
		}
	}
}

// MainWindow returns the application's main window, or nil if unset.
func (app *Application) MainWindow() *window.Window {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.mainWindow
}

// MultiWindow reports whether the app is declared multi-window.
func (app *Application) MultiWindow() bool {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.multiWindow
}

// SetMultiWindow declares whether the application manages more than one
// primary window.
//
// When true, the system gives the app a Window menu automatically - listing
// its windows with Tile/Cascade - on both the desktop bar and its detached
// main window's bar, so the app developer never has to add one by hand. The
// app may still contribute its own Window-menu items by tagging a menu with
// MenuIDWindow; those items are merged between the system's Tile/Cascade and
// its window list.
//
// When false (the default), the app is single-window: it should create only
// its first/main window plus transient dialog or tool-palette style windows
// (tool palettes are not yet implemented). It receives no Window menu. This
// contract is documented rather than hard-enforced for now, since the
// window classification it depends on does not fully exist yet.
func (app *Application) SetMultiWindow(multi bool) {
	app.mu.Lock()
	app.multiWindow = multi
	app.mu.Unlock()
}

// ContextOnly reports whether the app opts out of the automatic graphical
// Edit menu.
func (app *Application) ContextOnly() bool {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.contextOnly
}

// SetContextOnly, when true, suppresses the automatic Edit menu on graphical
// surfaces: the standard Cut/Copy/Paste/Select All items are not injected,
// and no Edit menu is created unless the app declares one itself (in which
// case only the app's own items appear). It is ignored in the text/TUI
// version, where the Edit menu is always present with the standard items.
func (app *Application) SetContextOnly(contextOnly bool) {
	app.mu.Lock()
	app.contextOnly = contextOnly
	app.mu.Unlock()
}

// SetMainWindow marks a window as the application's main window. When
// that window is torn off it carries the app's own menu bar, and the
// desktop shows only the reduced (Psi/Window/calendar) bar.
func (app *Application) SetMainWindow(w *window.Window) {
	app.mu.Lock()
	app.mainWindow = w
	app.mu.Unlock()
}

// MenuBarContent returns the menu bar content for this application.
func (app *Application) MenuBarContent() []*trinkets.Menu {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.menuBarContent
}

// SetMenuBarContent sets the menu bar content for this application and
// binds the menus' handlers into the app's command registry, so all
// menu activation dispatches by stable command ID (the D2 seam).
func (app *Application) SetMenuBarContent(menus []*trinkets.Menu) {
	app.mu.Lock()
	app.menuBarContent = menus
	commands := app.commands
	app.mu.Unlock()

	if commands != nil {
		for _, menu := range menus {
			menu.BindCommands(commands)
		}
	}

	// Tell the desktop so it can rebuild its visible bar if this app is
	// active - menus can change at any time (including a window's menubar
	// adopted just after the window activated), and the change must show
	// without waiting for a focus switch.
	app.mu.RLock()
	desktop := app.desktop
	app.mu.RUnlock()
	if r, ok := desktop.(interface {
		ActiveMenuBarContentChanged(trinkets.ApplicationProvider)
	}); ok {
		r.ActiveMenuBarContentChanged(app)
	}
}

// Commands returns the application's command registry: handlers keyed
// by stable command ID. Menu bar content is bound automatically;
// additional commands may be registered directly.
func (app *Application) Commands() *core.CommandRegistry {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.commands
}

// StatusBarContent returns the status bar content for this application.
func (app *Application) StatusBarContent() []trinkets.StatusSection {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.statusBarContent
}

// SetStatusBarContent sets the status bar content for this application.
func (app *Application) SetStatusBarContent(sections []trinkets.StatusSection) {
	app.mu.Lock()
	defer app.mu.Unlock()
	app.statusBarContent = sections
}

// OnActivate is called when this application becomes the active one.
func (app *Application) OnActivate() {
	app.mu.RLock()
	handler := app.onActivate
	app.mu.RUnlock()
	if handler != nil {
		handler()
	}
}

// OnDeactivate is called when this application is no longer active.
func (app *Application) OnDeactivate() {
	app.mu.RLock()
	handler := app.onDeactivate
	app.mu.RUnlock()
	if handler != nil {
		handler()
	}
}

// SetOnActivate sets the callback for when this application becomes active.
func (app *Application) SetOnActivate(handler func()) {
	app.mu.Lock()
	app.onActivate = handler
	app.mu.Unlock()
}

// SetOnDeactivate sets the callback for when this application becomes inactive.
func (app *Application) SetOnDeactivate(handler func()) {
	app.mu.Lock()
	app.onDeactivate = handler
	app.mu.Unlock()
}

// PassNextKeyToTrinket returns whether pass-next-key mode is active.
func (app *Application) PassNextKeyToTrinket() bool {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.passNextKeyToTrinket
}

// ActivatePassNextKeyToTrinket activates pass-next-key-to-trinket mode.
// The next keypress will bypass all global shortcut handling and go directly
// to the focused trinket. The status bar shows a message while active.
func (app *Application) ActivatePassNextKeyToTrinket() {
	app.mu.Lock()
	if app.passNextKeyToTrinket {
		app.mu.Unlock()
		return // Already active
	}
	app.passNextKeyToTrinket = true
	// Save current status bar content
	app.savedStatusBarContent = app.statusBarContent
	// Show pass-next-key message
	app.statusBarContent = []trinkets.StatusSection{
		{Text: "Raw Key Input: The next key pressed will be passed directly to the focused trinket."},
	}
	desktop := app.desktop
	app.mu.Unlock()

	// Refresh desktop status bar
	if desktop != nil {
		if d, ok := desktop.(*trinkets.Desktop); ok {
			d.RefreshStatusBar()
		}
	}
}

// ClearPassNextKeyToTrinket clears pass-next-key-to-trinket mode.
func (app *Application) ClearPassNextKeyToTrinket() {
	app.mu.Lock()
	if !app.passNextKeyToTrinket {
		app.mu.Unlock()
		return // Not active
	}
	app.passNextKeyToTrinket = false
	// Restore saved status bar content
	app.statusBarContent = app.savedStatusBarContent
	app.savedStatusBarContent = nil
	desktop := app.desktop
	app.mu.Unlock()

	// Refresh desktop status bar
	if desktop != nil {
		if d, ok := desktop.(*trinkets.Desktop); ok {
			d.RefreshStatusBar()
		}
	}
}
