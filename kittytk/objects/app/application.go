// Package app provides the main application framework for KittyTK.
package app

import (
	"sync"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/style"
)

// Application is the display's record of one running application: its
// windows, menus, status bar and commands. The desktop runs the event loop
// and owns the timers; an application runs neither. Application implements
// the trinkets.ApplicationProvider interface for integration with
// multi-application Desktop environments.
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

	// Theme
	theme *style.Theme

	// Desktop trinket (behind all windows)
	desktop core.Trinket

	// Callbacks
	onActivate   func()
	onDeactivate func()

	// Application name
	name string

	// menuName is the title of the application's own menu on its
	// detached main window's menu bar. It is only used when the main
	// window is torn off an SDL desktop; on the desktop bar the app menu
	// carries the app name. Defaults to "≡".
	menuName string

	// Windows owned by this application (for ApplicationProvider interface)
	windows []*window.Window

	// mainWindow is the app's optional main window; when detached it
	// hosts the app's own menu bar and the desktop shows a reduced bar.
	mainWindow *window.Window

	// multiWindow declares that the app manages more than one primary
	// window. See SetMultiWindow.
	multiWindow bool

	// showConnections asks for the desktop's Connections item on this app's
	// own menu. See SetShowConnections.
	showConnections bool

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

// New creates a new application instance: a container for windows, menus
// and status bar content. Multiple applications can coexist on a single
// Desktop, which owns the backend, the window manager and the focus for all
// of them.
func New() *Application {
	return &Application{
		objectID: core.NextObjectID(),
		theme:    style.DefaultTheme(),
		commands: core.NewCommandRegistry(),
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

// Theme returns the current theme.
func (app *Application) Theme() *style.Theme {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.theme
}

// SetDesktop sets the desktop trinket.
func (app *Application) SetDesktop(desktop core.Trinket) {
	app.mu.Lock()
	app.desktop = desktop
	app.mu.Unlock()
}

// Desktop returns the desktop trinket.
func (app *Application) Desktop() core.Trinket {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.desktop
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

// ShowConnections reports whether the app asked for the desktop's Connections
// item on its own menu.
func (app *Application) ShowConnections() bool {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.showConnections
}

// SetShowConnections asks for the desktop's Connections item on this app's own
// menu, directly above Quit.
//
// The desktop's own menu carries the item whatever any app says, so this is
// for an app that expects to be the only thing on the screen -- where that
// menu may not be shown at all -- and wants the user able to see who has been
// let in regardless. The window it opens is the desktop's: the app cannot
// read it, populate it, or be told it was opened.
func (app *Application) SetShowConnections(show bool) {
	app.mu.Lock()
	app.showConnections = show
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
