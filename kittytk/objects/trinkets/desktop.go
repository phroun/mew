// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/platform"
	"github.com/phroun/kittytk/style"
)

// ApplicationProvider is the interface that applications must implement
// to integrate with Desktop. This allows multiple applications to run
// in the same desktop environment.
type ApplicationProvider interface {
	// Name returns the application name.
	Name() string

	// ObjectID returns the application's stable protocol identity, from the
	// same space as windows and trinkets - so a running app can be referred
	// to (and set) over the protocol. See Application.ObjectID.
	ObjectID() core.ObjectID

	// MenuName returns the title of the app's own menu as shown on its
	// detached main window's menu bar (defaulting to "≡"). It is not used
	// on the desktop bar, where the app menu carries the app name.
	MenuName() string

	// MultiWindow reports whether the app manages more than one primary
	// window. A multi-window app automatically gets a system-managed Window
	// menu (list of its windows, Tile/Cascade); a single-window app does not.
	// See Application.SetMultiWindow for the window-creation contract.
	MultiWindow() bool

	// ContextOnly, on a graphical surface, suppresses the automatic Edit
	// menu and its standard Cut/Copy/Paste/Select All items (the app relies
	// on context menus instead). It is ignored in the text/TUI version, where
	// the Edit menu is always present. See Application.SetContextOnly.
	ContextOnly() bool

	// Windows returns all windows owned by this application.
	Windows() []*window.Window

	// MainWindow returns the application's main window, or nil if it has
	// none. When the main window is detached (torn off), it hosts the
	// app's own menu bar and the desktop shows only the reduced bar.
	MainWindow() *window.Window

	// AddWindow adds a window to this application.
	AddWindow(w *window.Window)

	// RemoveWindow removes a window from this application.
	RemoveWindow(w *window.Window)

	// MenuBarContent returns the menu bar content for this application.
	// Called when the application becomes active.
	MenuBarContent() []*Menu

	// StatusBarContent returns the status bar content for this application.
	StatusBarContent() []StatusSection

	// OnActivate is called when this application becomes the active one.
	OnActivate()

	// OnDeactivate is called when this application is no longer active.
	OnDeactivate()

	// SetDesktop sets the desktop that owns this application.
	// Called by Desktop.AddApplication().
	SetDesktop(desktop core.Trinket)

	// PassNextKeyToTrinket returns whether pass-next-key mode is active for this app.
	PassNextKeyToTrinket() bool

	// ActivatePassNextKeyToTrinket activates pass-next-key mode for this app.
	ActivatePassNextKeyToTrinket()

	// ClearPassNextKeyToTrinket clears pass-next-key mode for this app.
	ClearPassNextKeyToTrinket()
}

// Desktop represents the application desktop (background behind windows).
// It can optionally display a menu bar at the top (Mac-style) and a
// status bar at the bottom. Desktop can serve as the top-level object
// managing multiple applications.
type Desktop struct {
	core.TrinketBase

	// graphicalFrames reports whether the backend paints rounded
	// window frames (core.RoundedRectDrawer); windows discover it via
	// core.FindGraphicalFrames to pick their client-area contract.
	graphicalFrames bool

	// resizeGrip is the graphical resize-handle thickness in units
	// (0 on cell frames, where the whole border cell is the grip).
	resizeGrip core.Unit

	// Graphical wallpaper (classic MacOS style): an 8x8 two-color
	// bitmap, each bit rendered as wallpaperChunkPx x wallpaperChunkPx
	// device pixels. Tune via SetWallpaperPattern/SetWallpaperChunk.
	wallpaperPattern [8]uint8
	wallpaperChunkPx int

	// Menu bar at the top (Mac-style)
	menuBar *MenuBar

	// System menu (always present, upper-left)
	systemMenu *Menu

	// soleAppChromeSuppression, when enabled (SetSoleAppChromeSuppression), lets
	// the sole-single-window-app condition hide the desktop chrome (Ψ menu, menu
	// bar, status bar). Opt-in per host: a TUI host turns it on so a lone
	// fullscreen app fills the screen; the graphical host leaves it off (solo
	// mode already handles fullscreen there). Off by default - so the standalone
	// hosts keep their normal chrome.
	soleAppChromeSuppression bool

	// hideMenuBarSoleApp, when set (SetHideMenuBarForSoleApp), extends the
	// suppression to the menu bar too (see menuBarShown) - an experimental toggle
	// for a fully chrome-free single-app desktop. Off by default (the menu bar
	// always shows).
	hideMenuBarSoleApp bool

	// onApplicationsChanged, when set (SetOnApplicationsChanged), fires whenever
	// an application is added to or removed from the desktop, so a host can react
	// to the app set changing - e.g. a single-app host upgrading itself to
	// multi-window once a peer app joins the server.
	onApplicationsChanged func()

	// solo: a single application owns the whole display. Its main window
	// replaces the desktop entirely - no system (Psi) menu, no dock, no
	// wallpaper - and the host quits when the last window closes.
	solo bool

	// Status bar at the bottom
	statusBar *StatusBar

	// Dock row for minimized windows (above status bar)
	dockRow *DockRow

	// Background pattern
	bgChar rune

	// Whether child windows include a virtual "blur" focus item that allows
	// keyboard users to exit the window and focus the menu bar. Default: true.
	keyboardBlurChildren bool

	// Content area (shown behind windows but below menu/status)
	content core.Trinket

	// Multi-application support
	mu           sync.RWMutex
	applications []ApplicationProvider
	activeApp    ApplicationProvider

	// tornFocusOwner is the torn-off window that most recently took
	// focus, if it still holds it. While set, the desktop's own surface
	// regaining focus must NOT re-light an in-surface window (that would
	// steal focus from the detached window, which still owns the menu
	// bar line). Cleared when an in-surface window is actually activated.
	tornFocusOwner *window.Window

	// Backend for rendering (optional - used when Desktop.Run() is called)
	backend core.RenderBackend

	// Window manager (optional - used when Desktop.Run() is called)
	windowManager *window.WindowManager

	// Focus manager
	focusManager *core.GlobalFocusManager

	// Accessibility manager
	accessibilityManager *core.AccessibilityManager

	// Theme
	theme *style.Theme

	// Font (default font for all windows/trinkets)
	font *core.Font

	// Inverted-loop residence (G3/D21): the platform whose main
	// thread runs us, and our one surface on it.
	platform platform.Platform
	surface  platform.Surface

	// Live tear-off drag driven from the desktop's event stream (the
	// desktop window owns the capture until the button is released).
	tornDrag *tornDrag

	// Every window currently torn off into its own surface: the
	// repaint tick drives their animation alongside the desktop's.
	tornHosts []*window.TearOffHost

	// soloPrimaryHost is the tear-off host on the desktop's own OS
	// surface in solo mode (the one window that can't just be closed,
	// because that surface owns the event loop). When its window closes,
	// a remaining window is promoted onto the primary surface.
	soloPrimaryHost *window.TearOffHost

	// soloHosting is true while a window is being lifted onto the primary
	// surface. The lift removes the window from the manager, which fires
	// the removed-hook; this flag stops that internal removal from being
	// mistaken for the last window closing (a spurious quit).
	soloHosting bool

	// Running state
	running atomic.Bool

	// needsFrame is set by the core repaint hook (Update()) between ticks; the
	// periodic tick only invalidates the surface when it is set, so an idle
	// desktop stops repainting. idleTicks counts ticks since the last real
	// repaint to drive a slow heartbeat (a safety net for any animation that
	// changes without calling Update()).
	needsFrame atomic.Bool
	idleTicks  int

	// Bounded damage accumulated between ticks (main surface only): when set
	// and no full repaint was requested, the tick invalidates just this
	// rectangle so the surface repaints and re-uploads only that region.
	damageMu   sync.Mutex
	hasDamage  bool
	damageRect core.UnitRect

	// Quit channel
	quitChan chan struct{}

	// Update request channel
	updateChan chan struct{}

	// Timer events
	timers     []*DesktopTimer
	timerMutex sync.Mutex

	// Passive status-bar notifications (see NotifyPassive): a transient
	// message that overlays the status bar's normal content for a couple of
	// seconds, then reverts. notifyBase holds the content to restore;
	// notifyGen invalidates the timer of a superseded notice.
	notifyMu     sync.Mutex
	notifyActive bool
	notifyBase   []StatusSection
	notifyTimer  *DesktopTimer
	notifyGen    int

	// clipWait tracks an in-progress asynchronous clipboard read (the terminal
	// may prompt the user for OSC 52 permission). At most one runs at a time.
	// All access is on the UI thread (ReadClipboardAsync, timer callbacks, and
	// the read handler's Post), so no lock is needed.
	clipWait *clipboardWait

	// Callbacks
	onStartup  func()
	onShutdown func()

	// Event filters
	eventFilters []func(core.Event) bool

	// Command registry for desktop-level (system menu) commands,
	// keyed by stable command ID - the D2 dispatch seam.
	commands *core.CommandRegistry

	// Exit code
	exitCode int
}

// DesktopTimer represents a scheduled timer callback.
type DesktopTimer struct {
	ID       int
	Interval time.Duration
	Repeat   bool
	Callback func()
	nextFire time.Time
	stopped  bool
}

// Stop stops the timer.
func (t *DesktopTimer) Stop() {
	if t != nil {
		t.stopped = true
	}
}

// NewDesktop creates a new desktop trinket.
func NewDesktop() *Desktop {
	d := &Desktop{
		bgChar:               '▓', // Default pattern (three-quarter shade block)
		dockRow:              NewDockRow(),
		quitChan:             make(chan struct{}),
		updateChan:           make(chan struct{}, 100),
		theme:                style.DefaultTheme(),
		keyboardBlurChildren: true, // Default to enabling keyboard blur
		// Classic MacOS-style wallpaper: 50% checkerboard dither,
		// each pattern bit 2x2 device pixels.
		wallpaperPattern: [8]uint8{0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55},
		wallpaperChunkPx: 2,
	}
	d.TrinketBase = *core.NewTrinketBase()
	d.Init(d)
	d.SetFocusPolicy(core.NoFocus)
	d.dockRow.SetParent(d)

	// Desktop-level command registry (system menu dispatch)
	d.commands = core.NewCommandRegistry()

	// Create system menu and bind it to the command registry
	d.systemMenu = d.createSystemMenu()
	d.systemMenu.BindCommands(d.commands)

	// Create menu bar (always present in Desktop)
	d.menuBar = NewMenuBar()
	d.menuBar.SetParent(d)
	d.menuBar.AddMenu(d.systemMenu)
	// The menu bar represents the active app; when that app (or the desktop)
	// is modally blocked, the bar is disabled and must not highlight items.
	d.menuBar.SetModalBlockedChecker(func() bool {
		return d.windowManager != nil && d.windowManager.WallpaperModalActive()
	})

	// Create status bar (always present in Desktop)
	d.statusBar = NewStatusBar()
	d.statusBar.SetParent(d)

	// Wire up Tab navigation between dock and menu bar
	d.dockRow.SetOnFocusMenuBar(func() {
		d.UnfocusDock()
		d.menuBar.HandleKeyPress(core.KeyPressEvent{Key: "F10"})
	})
	d.menuBar.SetOnFocusDock(func() {
		if !d.dockRow.IsEmpty() {
			d.FocusDock()
		}
	})

	return d
}

// createSystemMenu creates the always-present system menu (ψ).
func (d *Desktop) createSystemMenu() *Menu {
	menu := NewMenu("Ψ")
	menu.AddItem(NewMenuItem("&About Desktop").SetOnTriggered(func() {
		d.showAboutDesktop()
	}))
	menu.AddItem(NewSeparator())
	menu.AddItem(NewMenuItem("Desktop &Accessories").SetEnabled(false)) // Placeholder
	menu.AddItem(NewSeparator())

	// Exit Desktop - uses ActionExitDesktop keybinding
	exitItem := NewMenuItem("E&xit Desktop")
	if keys := core.DefaultKeyBindings.Keys(core.ActionExitDesktop); len(keys) > 0 {
		exitItem.SetShortcut(core.NewShortcut(keys[0]))
	}
	exitItem.SetOnTriggered(func() {
		d.ExitDesktop()
	})
	menu.AddItem(exitItem)

	return menu
}

// aboutDesktopText is the body of the About KittyTK dialog: the recursive
// name, a one-line description, the version, and the copyright. The name and
// version come from the core package's single source of truth.
func aboutDesktopText() string {
	return fmt.Sprintf("%s — %s\n\n"+
		"One user interface toolkit for graphics, text, and speech.\n\n"+
		"Version %s\n\n"+
		"© 2026 Jeffrey R. Day. All rights reserved.",
		core.Name, core.Tagline, core.FullVersion())
}

// showAboutDesktop opens the About KittyTK dialog - the About entry in the
// system (Ψ) menu - as a modal message box on the desktop.
func (d *Desktop) showAboutDesktop() {
	mb := NewMessageBox("About KittyTK", aboutDesktopText(), ButtonOK)
	mb.SetIcon(IconInformation)
	wm := d.WindowManager()
	if wm == nil {
		return
	}
	wm.AddWindow(&mb.Window)
	// Now parented to the desktop, the window knows its real (graphical vs
	// cell) chrome: re-measure so the content holds the text and OK button,
	// then center the dialog in the desktop's client area.
	mb.ResizeToFitContent()
	area := wm.ClientArea()
	b := mb.Bounds()
	x := area.X + (area.Width-b.Width)/2
	y := area.Y + (area.Height-b.Height)/2
	// Snap the centered origin to the cell grid on the text/cell path (a
	// half-cell position breaks TUI rendering); graphical smooth positioning
	// keeps the exact center.
	if !wm.SmoothPositioning() {
		metrics := d.EffectiveCellMetrics()
		x = metrics.RoundDownToCellX(x)
		y = metrics.RoundDownToCellY(y)
	}
	mb.SetBounds(core.UnitRect{X: x, Y: y, Width: b.Width, Height: b.Height})
}

// SetBackend sets the render backend and initializes related components.
func (d *Desktop) SetBackend(backend core.RenderBackend) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.backend = backend

	// Async clipboard read (OSC 52 terminal query): route the reply onto the UI
	// thread so ReadClipboardAsync can resolve its pending paste / dismiss the
	// waiting modal.
	if reader, ok := backend.(core.AsyncClipboardReader); ok {
		reader.SetClipboardReadHandler(func(text string) {
			d.Post(func() { d.onClipboardResponse(text) })
		})
	}

	// The desktop roots the grid-metrics inheritance chain: seed its
	// override from the backend so every trinket inherits the display
	// service's default unless a container overrides it.
	rootMetrics := backend.Metrics()
	d.TrinketBase.SetCellMetrics(&rootMetrics)

	// Measurement comes from the render target (G1): a pixel backend
	// answers from its shaping engine so measurement matches the
	// proportional render; the text-based system keeps the built-in
	// cell arithmetic (nil restores it).
	if tm, ok := backend.(core.TextMeasurer); ok {
		core.SetTextMeasurer(tm)
	} else {
		core.SetTextMeasurer(nil)
	}

	// Frame mode: when the backend paints rounded window frames, the
	// client-area contract changes (content extends to the window
	// edges; only the titlebar reserves a full row). Windows discover
	// this through the desktop via core.FindGraphicalFrames.
	_, d.graphicalFrames = backend.(core.RoundedRectDrawer)

	// Resize grip: on graphical frames only the outer sliver of a
	// window edge resizes - a quarter of a layout column, scaled by
	// the device scale so the physical grab target grows with the
	// zoom, and never thinner than 4 device pixels - so edge trinkets
	// stay clickable.
	d.resizeGrip = 0
	if d.graphicalFrames {
		scale := 1
		if ds, ok := backend.(core.DeviceScaler); ok && ds.Scale() > 0 {
			scale = ds.Scale()
		}
		grip := rootMetrics.CellWidth / 4 * core.Unit(scale)
		if minUnits := core.Unit((4 + scale - 1) / scale); grip < minUnits {
			grip = minUnits
		}
		d.resizeGrip = grip
	}

	d.windowManager = window.NewWindowManager()
	d.windowManager.SetResizeGrip(d.resizeGrip)
	if sp, ok := backend.(core.SmoothPositioner); ok && sp.SmoothPositioning() {
		// Pixel surfaces place windows at unit granularity; cell-grid
		// surfaces keep the default snap-to-cell behavior.
		d.windowManager.SetSmoothPositioning(true)
	}
	// Graphical (SDL) surfaces deliver key releases, so the window-cycle run
	// commits its MRU order the instant all modifiers rise. The TUI can't see
	// that and falls back to the idle lock-in timer.
	if d.graphicalFrames {
		d.windowManager.SetModifierReleaseTracked(true)
	}
	d.windowManager.SetOnRepaintNeeded(func() {
		d.RequestUpdate()
	})
	d.windowManager.SetOnActiveChanged(func(win *window.Window) {
		d.windowFocusChanged(win)
	})
	d.focusManager = core.NewGlobalFocusManager()
	d.accessibilityManager = core.NewAccessibilityManager()
	d.focusManager.SetAccessibilityManager(d.accessibilityManager)

	// Connect each window's FocusManager to the AccessibilityManager
	d.windowManager.SetOnWindowAdded(func(win *window.Window) {
		if fm := win.FocusManager(); fm != nil {
			fm.SetAccessibilityManager(d.accessibilityManager)
		}
		// A tearable window's handle activation (click/keyboard)
		// detaches it into its own surface at its current position.
		if win.IsTearable() {
			win.SetOnTearRequest(func() { d.tearOffInPlace(win) })
		}
		// In solo mode there is no desktop surface: every window lives on
		// its own surface, so a newly added one is torn off immediately
		// (deferred, since we are mid-add). The solo main window is torn
		// by EnterSoloMode itself, before solo is set, so it is not caught
		// here.
		if d.IsSolo() {
			d.Post(func() { d.soloAdoptWindow(win) })
		}
	})

	d.windowManager.SetDesktop(d)

	// Wire up dock row integration
	if d.dockRow != nil {
		d.windowManager.SetOnWindowMinimized(func(win *window.Window) {
			entry := &DockEntry{
				Title:    win.Title(),
				WindowID: win.ObjectID(),
				OnClick: func() {
					d.windowManager.RestoreWindow(win)
				},
			}
			d.dockRow.AddEntry(entry)
		})

		d.windowManager.SetOnWindowRestored(func(win *window.Window) {
			d.dockRow.RemoveEntryByID(win.ObjectID())
		})
	}

	// Wire up menu bar integration
	if d.menuBar != nil {
		d.menuBar.SetOnMenuOpen(func() {
			// While a torn-off window owns focus the desktop bar is the
			// desktop's own menu bar; opening/closing it must not deactivate
			// or re-activate an in-surface window, which would steal focus
			// from the torn window.
			d.mu.RLock()
			torn := d.tornFocusOwner != nil
			d.mu.RUnlock()
			if torn {
				return
			}
			d.windowManager.DeactivateActiveWindow()
		})
		d.menuBar.SetOnMenuDismiss(func() {
			d.mu.RLock()
			torn := d.tornFocusOwner != nil
			d.mu.RUnlock()
			if torn {
				return
			}
			d.windowManager.RestorePreviousActiveWindow()
		})
	}
}

// SetWallpaperPattern sets the graphical wallpaper's 8x8 two-color
// bitmap (row-major, bit 7 leftmost; set bits use the desktop fill
// foreground, clear bits its background).
func (d *Desktop) SetWallpaperPattern(pattern [8]uint8) {
	d.mu.Lock()
	d.wallpaperPattern = pattern
	d.mu.Unlock()
	d.RequestUpdate()
}

// SetWallpaperChunk sets how many device pixels each wallpaper
// pattern bit covers (the pattern's "pixel size"; minimum 1).
func (d *Desktop) SetWallpaperChunk(px int) {
	if px < 1 {
		px = 1
	}
	d.mu.Lock()
	d.wallpaperChunkPx = px
	d.mu.Unlock()
	d.RequestUpdate()
}

// GraphicalResizeGrip implements core.ResizeGripProvider: the
// resize-handle thickness for graphical frames (0 on cell frames).
func (d *Desktop) GraphicalResizeGrip() core.Unit {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.resizeGrip
}

// GraphicalWindowFrames implements core.GraphicalFrameProvider: true
// when the backend paints rounded window frames, which switches the
// window client-area contract to edge-to-edge below the titlebar.
func (d *Desktop) GraphicalWindowFrames() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.graphicalFrames
}

// WindowFrameBorderUnits implements core.FrameBorderProvider: the frame
// border width (device pixels) converted to this surface's units, so
// windows reserve it outside their content area. 0 on cell surfaces
// (there the border already occupies a full cell). Rounded up so the
// content always clears the drawn stroke.
func (d *Desktop) WindowFrameBorderUnits() core.Unit {
	d.mu.RLock()
	graphical := d.graphicalFrames
	d.mu.RUnlock()
	if !graphical {
		return 0
	}
	ppu := d.pxPerUnit()
	if ppu <= 0 {
		ppu = 1
	}
	return core.Unit(math.Ceil(float64(core.WindowFrameBorderPx()) / ppu))
}

// Backend returns the render backend.
func (d *Desktop) Backend() core.RenderBackend {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.backend
}

// WindowManager returns the window manager.
func (d *Desktop) WindowManager() *window.WindowManager {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.windowManager
}

// FocusManager returns the focus manager.
func (d *Desktop) FocusManager() *core.GlobalFocusManager {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.focusManager
}

// AccessibilityManager returns the accessibility manager.
func (d *Desktop) AccessibilityManager() *core.AccessibilityManager {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.accessibilityManager
}

// Theme returns the current theme.
func (d *Desktop) Theme() *style.Theme {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.theme
}

// SetTheme sets the current theme.
func (d *Desktop) SetTheme(theme *style.Theme) {
	d.mu.Lock()
	d.theme = theme
	d.mu.Unlock()
	d.RequestUpdate()
}

// Font returns the desktop's default font, or nil if using the system default.
func (d *Desktop) Font() *core.Font {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.font
}

// SetFont sets the desktop's default font.
// This font is inherited by all windows and trinkets unless overridden.
// Set to nil to use the system default (Monday 12pt).
func (d *Desktop) SetFont(font *core.Font) {
	d.mu.Lock()
	d.font = font
	apps := make([]ApplicationProvider, len(d.applications))
	copy(apps, d.applications)
	d.mu.Unlock()

	// Recalculate layout for all windows since font affects trinket sizes
	for _, app := range apps {
		for _, w := range app.Windows() {
			w.Layout()
		}
	}
	d.RequestUpdate()
}

// EffectiveFont returns the font to use for the desktop.
// Returns the set font, or the system default if none is set.
func (d *Desktop) EffectiveFont() *core.Font {
	d.mu.RLock()
	f := d.font
	d.mu.RUnlock()
	if f != nil {
		return f
	}
	return core.DefaultFont()
}

// AddApplication registers an application with the desktop.
func (d *Desktop) AddApplication(app ApplicationProvider) {
	d.mu.Lock()
	d.applications = append(d.applications, app)

	// If this is the first app, make it active
	shouldActivate := d.activeApp == nil
	if shouldActivate {
		d.activeApp = app
	}
	wm := d.windowManager
	d.mu.Unlock()

	// Set this desktop as the application's desktop
	app.SetDesktop(d)

	// Add any existing windows from the app to the WindowManager.
	// This handles the case where windows were added to the app before
	// it was registered with the desktop.
	if wm != nil {
		for _, win := range app.Windows() {
			wm.AddWindow(win)
		}
	}

	if shouldActivate {
		app.OnActivate()
		d.updateMenuBarContent()
		d.updateStatusBarContent()
	}

	d.fireApplicationsChanged()
}

// RemoveApplication unregisters an application from the desktop.
func (d *Desktop) RemoveApplication(app ApplicationProvider) {
	d.mu.Lock()

	for i, a := range d.applications {
		if a == app {
			d.applications = append(d.applications[:i], d.applications[i+1:]...)
			break
		}
	}

	// If this was the active app, switch to another or none
	wasActive := d.activeApp == app
	var newActiveApp ApplicationProvider
	if wasActive {
		if len(d.applications) > 0 {
			d.activeApp = d.applications[0]
			newActiveApp = d.activeApp
		} else {
			d.activeApp = nil
		}
	}
	d.mu.Unlock()

	if wasActive {
		app.OnDeactivate()
		if newActiveApp != nil {
			newActiveApp.OnActivate()
		}
		d.updateMenuBarContent()
		d.updateStatusBarContent()
	}

	d.fireApplicationsChanged()
}

// SetApplication sets a single application (for backward compatibility).
func (d *Desktop) SetApplication(app ApplicationProvider) {
	d.mu.Lock()
	// Clear existing applications
	for _, a := range d.applications {
		a.OnDeactivate()
	}
	d.applications = nil
	d.activeApp = nil
	d.mu.Unlock()

	d.AddApplication(app)
}

// ActiveApplication returns the currently active application.
func (d *Desktop) ActiveApplication() ApplicationProvider {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.activeApp
}

// SetOnApplicationsChanged registers a callback fired whenever the set of
// applications changes (added or removed). A host uses it to adapt to the
// presence of other apps; it runs on the platform thread, after the app set and
// active application have been updated.
func (d *Desktop) SetOnApplicationsChanged(fn func()) {
	d.mu.Lock()
	d.onApplicationsChanged = fn
	d.mu.Unlock()
}

// fireApplicationsChanged invokes the applications-changed callback (if any)
// without holding d.mu, so the callback may read Applications() and mutate app
// state freely.
func (d *Desktop) fireApplicationsChanged() {
	d.mu.RLock()
	fn := d.onApplicationsChanged
	d.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// Applications returns all registered applications.
func (d *Desktop) Applications() []ApplicationProvider {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]ApplicationProvider, len(d.applications))
	copy(result, d.applications)
	return result
}

// findApplicationForWindow finds which application owns a window.
func (d *Desktop) findApplicationForWindow(w *window.Window) ApplicationProvider {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, app := range d.applications {
		for _, win := range app.Windows() {
			if win == w {
				return app
			}
		}
	}
	return nil
}

// quasiActivateExclusive makes owner quasi-active (lit, single/heavy
// border) and returns every OTHER top-level window - torn or in-surface -
// to the inactive style, so only one top-level window is lit while the
// desktop menu bar holds focus. MDI children live inside their parent
// window (not top-level), so they follow their parent's lineage separately.
func (d *Desktop) quasiActivateExclusive(owner *window.Window) {
	d.mu.RLock()
	hosts := append([]*window.TearOffHost(nil), d.tornHosts...)
	wm := d.windowManager
	d.mu.RUnlock()

	clear := func(w *window.Window) {
		if w == nil || w == owner {
			return
		}
		// SetActive short-circuits when already inactive, so clear the quasi
		// flag explicitly - a window stuck quasi has isActive == false.
		w.SetActive(false)
		w.SetQuasiActive(false)
	}
	for _, th := range hosts {
		clear(th.Window())
	}
	if wm != nil {
		for _, w := range wm.Windows() {
			clear(w)
		}
	}
	owner.SetQuasiActive(true)
}

// windowFocusChanged is called when window focus changes.
func (d *Desktop) windowFocusChanged(w *window.Window) {
	// When w is nil, it means the window was deactivated (e.g., menu bar took focus).
	// In this case, we should NOT change the active app or update menus - the user
	// is still interacting with the same app through its menu bar.
	if w == nil {
		return
	}

	owner := d.findApplicationForWindow(w)

	d.mu.Lock()
	if owner != d.activeApp {
		if d.activeApp != nil {
			d.activeApp.OnDeactivate()
		}
		d.activeApp = owner
		if d.activeApp != nil {
			d.activeApp.OnActivate()
		}
	}
	// Remember when a detached window owns focus, so the desktop's own
	// surface regaining focus won't re-light an in-surface window over
	// it. Activating an in-surface window clears the reference - and any
	// quasi-active torn window it named must go fully inactive, since the
	// desktop now has a real active window rather than merely holding
	// focus on the torn window's behalf.
	prevTorn := d.tornFocusOwner
	if w.IsDetached() {
		d.tornFocusOwner = w
	} else {
		d.tornFocusOwner = nil
	}
	d.mu.Unlock()

	if prevTorn != nil && prevTorn != w && !w.IsDetached() {
		prevTorn.SetActive(false)
	}

	d.updateMenuBarContent()
	d.updateStatusBarContent()
}

// IsSolo reports whether the desktop is running a single application as
// the whole display (see docs/solo-app-plan.md).
func (d *Desktop) IsSolo() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.solo
}

// EnterSoloMode makes win the whole display. The desktop's own OS window
// is reshaped into the app's window: its border is stripped (the app's
// chrome is the only title bar) and win is hosted on that surface via a
// tear-off host, so win fills it and keyboard/edge resize maps onto the OS
// window. No second window is created and none is closed (the SDL platform
// refuses to close its main window); the desktop lives on as a windowless
// coordinator and quits when the last window closes. Idempotent.
func (d *Desktop) EnterSoloMode(win *window.Window) {
	d.mu.Lock()
	first := !d.solo
	d.solo = true
	wm := d.windowManager
	d.mu.Unlock()

	// Quit when the last window closes (peers, no privileged root). On a
	// native platform every solo window is hosted on its own surface, so
	// its close flows through dropTornHost -> soloRebalance; but on a
	// single-surface platform (a terminal, or headless polling) windows
	// stay docked, and this hook is the only signal that the last one
	// left. The soloHosting guard keeps the internal lift (which removes a
	// window from the manager to host it) from tripping a spurious quit.
	if first && wm != nil {
		wm.SetOnWindowRemoved(func(*window.Window) {
			d.Post(func() { d.soloRebalance(false) })
		})
	}
	if win != nil {
		d.soloHostOnPrimary(win)
	}
}

// RaiseToFront brings the desktop's primary OS surface to the front of the
// window stack (and focuses it). Two host uses: at startup, so the window comes
// forward even from a session-detached launch that the window manager won't
// focus; and after ExitSoloMode, so the revealed desktop ends up on top (that
// call raises the surface too, but then re-homes the solo window above it, so it
// needs raising once more). No-op on a single-surface backend (the TUI) that
// can't reorder OS windows.
func (d *Desktop) RaiseToFront() {
	d.mu.RLock()
	surf := d.surface
	d.mu.RUnlock()
	if ns, ok := surf.(platform.NativeSurface); ok {
		ns.Raise()
	}
}

// ExitSoloMode is the inverse of EnterSoloMode: it gives the primary
// surface back to the desktop (re-bordered, wallpaper/dock/menu drawn
// again) and re-homes the window that filled it as an ordinary tearable
// torn-off window at the same screen rectangle - so it floats over the
// freshly revealed desktop with its redock handle and can be dragged in to
// dock. Any client can request this over the protocol (the `spawndesktop`
// verb); it is a no-op when not in solo mode or when the platform can't
// host surfaces. Runs on the platform thread.
func (d *Desktop) ExitSoloMode() {
	d.mu.RLock()
	solo := d.solo
	host := d.soloPrimaryHost
	surf := d.surface
	wm := d.windowManager
	d.mu.RUnlock()
	if !solo || host == nil || surf == nil || wm == nil {
		return
	}
	win := host.Window()
	if win == nil {
		return
	}

	// Give the primary surface back to the desktop: re-border it and point
	// its handler at the desktop again so it paints its own chrome.
	if bt, ok := surf.(platform.BorderToggler); ok {
		bt.SetBordered(true)
	}
	surf.SetHandler(&desktopSurfaceHandler{d: d})

	// Retire the solo host without closing the primary surface (it lives on
	// as the desktop's surface).
	host.SetOnClosed(nil)
	d.mu.Lock()
	d.solo = false
	d.soloPrimaryHost = nil
	for i, th := range d.tornHosts {
		if th == host {
			d.tornHosts = append(d.tornHosts[:i], d.tornHosts[i+1:]...)
			break
		}
	}
	d.mu.Unlock()

	// The desktop reclaims its bounds and rebuilds its bar content.
	size := surf.Size()
	wm.SetScreenBounds(core.UnitRect{Width: size.Width, Height: size.Height})
	d.updateMenuBarContent()
	d.updateStatusBarContent()

	// Bring the revealed desktop to the front - otherwise its surface keeps
	// the z-order it had while hidden behind the solo window, so it can
	// surface behind unrelated app windows and the change goes unnoticed.
	// The re-homed window is raised above it next (createTornHost raises a
	// main window), so the desktop and its app come forward together.
	if ns, ok := surf.(platform.NativeSurface); ok {
		ns.Raise()
	}

	// Re-home the app window as a tearable torn-off window at the same
	// screen rectangle it occupied while solo (the primary surface's rect).
	// The desktop origin is now that surface, so tearing at desktop unit
	// (0,0) with the surface's size lands it exactly where it was.
	win.SetDetached(false) // createTornHost re-detaches and re-wires it
	win.SetTearable(true)  // its redock handle returns; it can dock now
	win.SetBounds(core.UnitRect{Width: size.Width, Height: size.Height})
	d.createTornHost(win, 0, 0)

	d.invalidateSurface()
}

// EnterSoloFromDesktop makes a detached window solo again: it picks a
// torn-off window (preferring an app's main window) and hosts it filling
// the primary surface borderless, dismissing the desktop. This is the
// inverse of ExitSoloMode - "promote a detached app" - so any client can
// toggle the root back to solo over the protocol (the `gosolo` verb). A
// no-op when already solo or when no detached window exists. The primary
// surface adopts the promoted window's screen rectangle, so the app stays
// exactly where it was floating rather than snapping to where the desktop
// sat. Runs on the platform thread.
func (d *Desktop) EnterSoloFromDesktop() {
	d.mu.RLock()
	solo := d.solo
	wm := d.windowManager
	hosts := append([]*window.TearOffHost(nil), d.tornHosts...)
	d.mu.RUnlock()
	if solo || wm == nil {
		return
	}
	if h := pickPromotable(hosts); h != nil {
		// Same quit-on-last wiring as a fresh solo entry (idempotent if it
		// was installed before): the removed-hook drives the docked case.
		wm.SetOnWindowRemoved(func(*window.Window) {
			d.Post(func() { d.soloRebalance(false) })
		})
		d.mu.Lock()
		d.solo = true
		d.mu.Unlock()
		// Discard the window's own surface and host it on the primary,
		// moving the primary to where the window was (reposition=true) so
		// the app keeps its on-screen position instead of jumping to the
		// desktop's spot.
		d.promoteToPrimary(h, true)
		return
	}
	// No torn window: lift a docked application window straight onto the
	// primary surface. EnterSoloMode removes it from the manager and hosts
	// it, so it fills the display as the new solo window.
	if win := pickDockedMain(wm.Windows()); win != nil {
		d.EnterSoloMode(win)
	}
}

// pickDockedMain chooses a docked application window to make solo,
// preferring one that requested to be its app's main window.
func pickDockedMain(wins []*window.Window) *window.Window {
	for _, w := range wins {
		if w.MainRequested() {
			return w
		}
	}
	if len(wins) > 0 {
		return wins[0]
	}
	return nil
}

// ExitDesktop handles the system menu's "Exit Desktop" command. If any
// application window remains, the desktop is dismissed and that app takes
// over the whole display as a solo app (promote a detached app again);
// otherwise nothing is left to run, so the desktop process quits. A no-op
// distinction only matters off solo - a solo app has no desktop to exit.
func (d *Desktop) ExitDesktop() {
	d.mu.RLock()
	solo := d.solo
	wm := d.windowManager
	tornCount := len(d.tornHosts)
	d.mu.RUnlock()
	docked := 0
	if wm != nil {
		docked = len(wm.Windows())
	}
	if !solo && tornCount+docked > 0 {
		d.EnterSoloFromDesktop()
		return
	}
	d.Quit()
}

// screenRect is a surface's OS-window geometry in screen pixels.
type screenRect struct{ x, y, w, h int }

// soloHostOnPrimary reshapes the desktop's own OS window into the app's
// window (see EnterSoloMode). Returns without effect when the platform
// can't host it (headless); solo mode is still recorded.
func (d *Desktop) soloHostOnPrimary(win *window.Window) {
	d.soloHostOnPrimaryAt(win, nil)
}

// soloHostOnPrimaryAt is soloHostOnPrimary with an optional target
// geometry: when a promoted peer takes over the primary surface, the
// surface repositions and resizes to where that peer's window was, so the
// primary surface "takes on the personality" of the promoted window
// including its screen placement.
func (d *Desktop) soloHostOnPrimaryAt(win *window.Window, target *screenRect) {
	d.mu.RLock()
	plat := d.platform
	surf := d.surface
	wm := d.windowManager
	d.mu.RUnlock()
	if plat == nil || surf == nil || wm == nil {
		return
	}
	native, ok := surf.(platform.NativeSurface)
	if !ok {
		return
	}
	gp, ok := plat.(platform.GlobalPointerPlatform)
	if !ok {
		return
	}

	// Strip the OS title bar - the app's own chrome is the only one.
	if bt, ok := native.(platform.BorderToggler); ok {
		bt.SetBordered(false)
	}

	// Adopt the promoted window's screen placement.
	if target != nil {
		if target.w > 0 && target.h > 0 {
			native.SetScreenSizePx(target.w, target.h)
		}
		native.SetScreenPositionPx(target.x, target.y)
	}

	// Lift the window out of the manager (and any dock entry). The removed
	// hook must not read this internal removal as the last window closing.
	d.mu.Lock()
	d.soloHosting = true
	d.mu.Unlock()
	wm.RemoveWindow(win)
	d.mu.Lock()
	d.soloHosting = false
	d.mu.Unlock()
	if win.IsMinimized() {
		win.Restore()
	}
	if d.dockRow != nil {
		d.dockRow.RemoveEntryByID(win.ObjectID())
	}

	// Host it on the primary surface. No redock: there is no desktop to
	// dock back to.
	var host *window.TearOffHost
	host = window.NewTearOffHost(win, surf, d.pxPerUnit(), gp.GlobalPointerPx,
		func(int, int, core.Unit, core.Unit) bool { return false })
	host.SetOnClosed(func() { d.dropTornHost(host) })
	host.SetClipboardAccess(d.Clipboard, d.SetClipboard)
	if cc, ok := plat.(platform.CursorController); ok {
		host.SetCursorSetter(cc.SetCursor)
	}
	host.SetResizeGrip(d.resizeGrip)
	host.SetOnFocus(func(focused bool) {
		if focused {
			d.windowFocusChanged(win)
			d.invalidateSurface()
		}
	})

	// The solo primary surface is a torn host too, so - exactly as createTornHost
	// wires for a regular torn window - it must consult the modal stack: an
	// application- or window-level modal of this window's own app blocks it (the
	// host dims it and swallows input), and a press while blocked surfaces the
	// blocking modal (OS-restoring it if minimized, raising it to the top).
	// Without this the solo editor keeps taking input while a modal of its app is
	// up - the modal shows but doesn't actually block.
	host.SetModalChecker(
		func() bool { return d.windowManager.IsTornWindowBlocked(win) },
		func() { d.surfaceBlockingModal(win) })

	win.SetDetached(true)
	win.SetTearable(false) // no tear/redock handle in solo
	d.attachMainWindowChrome(win)
	if mb, ok := win.WindowMenuBar().(*MenuBar); ok {
		mb.SetScrollTimerStarter(func(interval time.Duration, cb func()) interface{ Stop() } {
			return d.StartRepeatingTimer(interval, cb)
		})
		mb.SetRequestUpdate(host.Invalidate)
	}

	d.mu.Lock()
	d.tornHosts = append(d.tornHosts, host)
	d.soloPrimaryHost = host
	d.mu.Unlock()

	host.Invalidate()
}

// soloAdoptWindow tears a newly added window onto its own surface (a peer
// of the solo main window). A no-op if the window is already detached or
// solo mode has since ended. Peers keep no tear handle (nothing to dock
// back to) and are not zoomed - only the main window fills the display.
func (d *Desktop) soloAdoptWindow(win *window.Window) {
	if win == nil || !d.IsSolo() || win.IsDetached() {
		return
	}
	// Only genuinely managed windows tear off (a closed one has left).
	if !d.managesWindow(win) {
		return
	}
	win.SetTearable(true)
	d.tearOffInPlace(win)
	win.SetTearable(false)
}

// managesWindow reports whether win is currently in the window manager.
func (d *Desktop) managesWindow(win *window.Window) bool {
	d.mu.RLock()
	wm := d.windowManager
	d.mu.RUnlock()
	if wm == nil {
		return false
	}
	for _, w := range wm.Windows() {
		if w == win {
			return true
		}
	}
	return false
}

// dockVisible reports whether the minimized-window dock occupies space.
func (d *Desktop) dockVisible() bool {
	return d.dockRow != nil && !d.dockRow.IsEmpty()
}

// soloRebalance runs after a solo window closes: if no windows remain the
// host quits; if the window that closed was the one on the primary
// surface (which the platform won't let us close), a remaining window is
// promoted onto that surface so it always hosts something.
func (d *Desktop) soloRebalance(primaryClosed bool) {
	d.mu.RLock()
	solo := d.solo
	hosting := d.soloHosting
	wm := d.windowManager
	hosts := append([]*window.TearOffHost(nil), d.tornHosts...)
	havePrimary := d.soloPrimaryHost != nil
	d.mu.RUnlock()
	if !solo || wm == nil || hosting {
		// Mid-lift: a window was just removed to be hosted, not closed.
		return
	}
	if len(hosts) == 0 && len(wm.Windows()) == 0 {
		d.Quit()
		return
	}
	if primaryClosed && !havePrimary {
		if h := pickPromotable(hosts); h != nil {
			d.promoteToPrimary(h, true)
		}
	}
}

// pickPromotable chooses which window takes over the primary surface,
// preferring an application's own main window.
func pickPromotable(hosts []*window.TearOffHost) *window.TearOffHost {
	for _, h := range hosts {
		if h.Window() != nil && h.Window().MainRequested() {
			return h
		}
	}
	if len(hosts) > 0 {
		return hosts[0]
	}
	return nil
}

// promoteToPrimary moves peer's window off its own surface onto the
// desktop's primary surface, so that (un-closeable) surface always hosts
// a live window while any window remains. When reposition is true the
// primary surface adopts the peer's screen placement (used when the
// primary's own window closed and a replacement takes over its spot);
// when false the primary keeps its current geometry (used when a detached
// window is promoted to fill the display anew).
func (d *Desktop) promoteToPrimary(peer *window.TearOffHost, reposition bool) {
	win := peer.Window()
	if win == nil {
		return
	}
	d.mu.Lock()
	for i, h := range d.tornHosts {
		if h == peer {
			d.tornHosts = append(d.tornHosts[:i], d.tornHosts[i+1:]...)
			break
		}
	}
	d.mu.Unlock()
	// Capture the peer's screen placement (if adopting it) then discard the
	// peer's own surface without closing the window.
	var target *screenRect
	peer.SetOnClosed(nil)
	psurf := peer.Surface()
	if s, ok := psurf.(platform.NativeSurface); ok {
		if reposition {
			x, y := s.ScreenPositionPx()
			sz := psurf.Size() // units; screen pixels track font_size
			target = &screenRect{
				x: x, y: y,
				w: d.unitToPx(sz.Width),
				h: d.unitToPx(sz.Height),
			}
		}
		s.Close()
	}
	// Re-host the window on the primary surface (sets soloPrimaryHost).
	d.soloHostOnPrimaryAt(win, target)
}

// soleForegroundApp reports whether exactly one non-context-only application is
// present. When true, that app owns the whole menu bar, so the system (Ψ) menu
// is suppressed as redundant furniture. A context-only app (a windowless host
// context, e.g. the graphical service's own app) does not count, so an empty
// desktop still reads as "no foreground app" and keeps Ψ.
//
// This reads d.applications WITHOUT taking d.mu: it is consulted from the layout,
// paint, and hit-test paths, some of which already run under d.mu, so locking
// here would deadlock. The application set is only mutated on the platform
// thread (AddApplication/RemoveApplication run pre-Run or via Post), the same
// thread that lays out and paints, so the read needs no lock.
func (d *Desktop) soleForegroundApp() bool {
	n := 0
	for _, a := range d.applications {
		if a != nil && !a.ContextOnly() {
			n++
		}
	}
	return n == 1
}

// suppressSoleAppChrome reports whether the desktop's own chrome (Ψ menu, menu
// bar, status bar) should be hidden because a single self-contained app owns the
// desktop. This is opt-in per host (SetSoleAppChromeSuppression): a TUI host
// enables it so a lone fullscreen app (mew) fills the screen; the graphical host
// leaves it off, since solo mode already handles the fullscreen case there. It
// applies only while exactly one non-context app is present AND that (active)
// app is single-window; once the sole app declares itself multi-window (e.g. mew
// on show_desktop), the chrome returns.
func (d *Desktop) suppressSoleAppChrome() bool {
	if !d.soleAppChromeSuppression || !d.soleForegroundApp() {
		return false
	}
	if a := d.activeApp; a != nil && a.MultiWindow() {
		return false
	}
	return true
}

// SetSoleAppChromeSuppression enables the sole-app chrome suppression (Ψ menu,
// status bar, and - with SetHideMenuBarForSoleApp - the menu bar). Off by
// default; a TUI host enables it, the graphical host does not.
func (d *Desktop) SetSoleAppChromeSuppression(enabled bool) {
	d.soleAppChromeSuppression = enabled
	d.Update()
}

// statusBarShown reports whether the desktop's own status bar should occupy the
// bottom row and paint. It is suppressed when a single self-contained app owns
// the desktop (see suppressSoleAppChrome): that app provides its own status
// surface (e.g. mew's modebar), so the desktop's is redundant and its row is
// reclaimed for content.
func (d *Desktop) statusBarShown() bool {
	return d.statusBar != nil && !d.suppressSoleAppChrome()
}

// menuBarShown reports whether the desktop menu bar should occupy the top row
// and paint. It is present unless the experimental hideMenuBarSoleApp toggle is
// on AND the sole-app chrome is being suppressed, in which case it too is
// hidden (its row reclaimed) for a fully chrome-free single-app desktop.
func (d *Desktop) menuBarShown() bool {
	return d.menuBar != nil && !(d.hideMenuBarSoleApp && d.suppressSoleAppChrome())
}

// SetHideMenuBarForSoleApp toggles whether the desktop menu bar is also hidden
// while the sole-app chrome is suppressed (see menuBarShown). Experimental:
// hiding it removes the only pointer route to the app's menus, so it is off by
// default.
func (d *Desktop) SetHideMenuBarForSoleApp(hide bool) {
	d.hideMenuBarSoleApp = hide
	d.Update()
}

// updateMenuBarContent updates the menu bar with the active app's menus.
// The first menu after the system menu automatically gets standard app items
// (Hide, Hide Others, Show All, Quit) appended.
func (d *Desktop) updateMenuBarContent() {
	if d.menuBar == nil {
		return
	}

	d.mu.RLock()
	activeApp := d.activeApp
	d.mu.RUnlock()

	// Clear existing menus
	d.menuBar.Clear()

	// Reduced bar: while the active app's main window is detached it
	// carries the app's own menus on its own surface, so the desktop
	// shows only the real system Psi menu, an app-named menu with just
	// the merged Hide section, a Tile/Cascade Window menu, and the
	// calendar (drawn by the bar itself).
	if activeApp != nil {
		if main := activeApp.MainWindow(); main != nil && main.IsDetached() {
			if d.systemMenu != nil {
				d.menuBar.AddMenu(d.systemMenu)
			}
			d.menuBar.AddMenu(d.buildAppHideMenu(activeApp.Name()))
			d.menuBar.AddMenu(d.buildWindowTileCascadeMenu())
			return
		}
	}

	// Full bar: system menu first - unless the sole-app chrome is suppressed
	// (a bundled single-app host like mew on the TUI), in which case Ψ is
	// dropped as redundant furniture and that app's own menus are all that
	// shows. An empty desktop, a multi-window app, or a graphical host all keep
	// Ψ.
	if d.systemMenu != nil && !d.suppressSoleAppChrome() {
		d.menuBar.AddMenu(d.systemMenu)
	}

	// Add the active app's menus in the canonical order: app, file, edit,
	// select, format, view, custom..., window, help. Well-known tags place
	// the app/window/help menus and drive the system Edit menu; everything
	// else follows its declared order.
	if activeApp != nil {
		appName := activeApp.Name()
		b := bucketMenus(activeApp.MenuBarContent())

		// A windowless, menuless, single-window application - the empty
		// desktop host at boot, before any client app connects - contributes
		// no menu at all, so only the Psi menu shows. Any real app gets the
		// mandatory app/edit menus even when it declared neither.
		if b.declaredAny() || len(activeApp.Windows()) > 0 || activeApp.MultiWindow() {
			// App menu (mandatory), leading, with the Hide/Quit sections merged
			// in. Synthesized when the app declared no app-tagged menu.
			if b.app != nil {
				d.menuBar.AddMenu(d.createAppMenuWithStandardItems(b.app, appName))
			} else {
				d.menuBar.AddMenu(d.createStandardAppMenu(appName))
			}

			d.appendAppBody(d.menuBar.AddMenu, activeApp, b)

			// System-managed Window menu for multi-window apps (enforced, and
			// always the final menu before Help). A single-window app that
			// still tagged a Window menu gets it shown as-is.
			if activeApp.MultiWindow() {
				d.menuBar.AddMenu(d.buildDesktopWindowMenu(b.window, activeApp))
			} else if b.window != nil {
				d.menuBar.AddMenu(b.window)
			}
			if b.help != nil {
				d.menuBar.AddMenu(b.help)
			}
		}
	}
}

// createAppMenuWithStandardItems creates a copy of the given menu with standard
// app items (Hide, Hide Others, Show All, Quit) appended.
func (d *Desktop) createAppMenuWithStandardItems(original *Menu, appName string) *Menu {
	// Create a new menu with the same title
	merged := NewMenu(original.Title())

	// Copy all items from the original menu
	for _, item := range original.Items() {
		merged.AddItem(item)
	}

	// Add standard app items
	d.appendStandardAppItems(merged, appName)

	return merged
}

// createStandardAppMenu creates a standard app menu when the app provides none.
// The menu is named after the app with the first letter as the accelerator.
func (d *Desktop) createStandardAppMenu(appName string) *Menu {
	// Create menu with app name, first letter as accelerator
	menuTitle := "&" + appName
	appMenu := NewMenu(menuTitle)

	// Add standard app items
	d.appendStandardAppItems(appMenu, appName)

	return appMenu
}

// appendStandardAppItems adds the standard app menu items to the given menu.
// Items added: separator, Hide [App], Hide Others, Show All, separator, Quit [App]
func (d *Desktop) appendStandardAppItems(menu *Menu, appName string) {
	d.appendHideSection(menu, appName, true)
	d.appendQuitSection(menu, appName)
}

// appendHideSection adds the merged system items - Hide [App], Hide
// Others, Show All. When leadingSeparator is true a separator first
// offsets them from the menu's own items (as in the desktop's app
// menu); the standalone Psi menu passes false.
func (d *Desktop) appendHideSection(menu *Menu, appName string, leadingSeparator bool) {
	if leadingSeparator {
		menu.AddSeparator()
	}

	hideItem := NewMenuItem("&Hide " + appName)
	if keys := core.DefaultKeyBindings.Keys(core.ActionAppHide); len(keys) > 0 {
		hideItem.SetShortcut(core.NewShortcut(keys[0]))
	}
	hideItem.SetOnTriggered(func() { d.hideActiveApp() })
	menu.AddItem(hideItem)

	hideOthersItem := NewMenuItem("Hide &Others")
	if keys := core.DefaultKeyBindings.Keys(core.ActionAppHideOthers); len(keys) > 0 {
		hideOthersItem.SetShortcut(core.NewShortcut(keys[0]))
	}
	hideOthersItem.SetOnTriggered(func() { d.hideOtherApps() })
	menu.AddItem(hideOthersItem)

	showAllItem := NewMenuItem("&Show All")
	if keys := core.DefaultKeyBindings.Keys(core.ActionAppShowAll); len(keys) > 0 {
		showAllItem.SetShortcut(core.NewShortcut(keys[0]))
	}
	showAllItem.SetOnTriggered(func() { d.showAllApps() })
	menu.AddItem(showAllItem)
}

// appendQuitSection adds a separator and the app's Quit item. This is
// the part that stays on a detached main window's own menu bar.
func (d *Desktop) appendQuitSection(menu *Menu, appName string) {
	menu.AddSeparator()

	quitItem := NewMenuItem("&Quit " + appName)
	if keys := core.DefaultKeyBindings.Keys(core.ActionQuit); len(keys) > 0 {
		quitItem.SetShortcut(core.NewShortcut(keys[0]))
	}
	quitItem.SetOnTriggered(func() { d.quitActiveApp() })
	menu.AddItem(quitItem)
}

// createAppMenuWithQuitOnly copies a menu under the given title and
// appends only the Quit section (no Hide section, no offset separator) -
// the first-menu form for a detached main window's own menu bar. The
// title comes from the app's menu name so it reads "≡" (or a developer
// override) rather than the app-named desktop menu.
func (d *Desktop) createAppMenuWithQuitOnly(original *Menu, title, appName string) *Menu {
	merged := NewMenu(title)
	for _, item := range original.Items() {
		merged.AddItem(item)
	}
	d.appendQuitSection(merged, appName)
	return merged
}

// buildAppHideMenu builds the app-named menu shown on the desktop bar
// while the active app's main window is detached: it carries only the
// merged Hide section (Hide App, Hide Others, Show All). The real system
// Psi menu is shown separately, unchanged.
func (d *Desktop) buildAppHideMenu(appName string) *Menu {
	menu := NewMenu("&" + appName)
	d.appendHideSection(menu, appName, false)
	return menu
}

// applicationForMainWindow returns the application whose main window is
// win, or nil if win is not any app's main window.
func (d *Desktop) applicationForMainWindow(win *window.Window) ApplicationProvider {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, app := range d.applications {
		if app.MainWindow() == win {
			return app
		}
	}
	return nil
}

// buildDetachedMenuBar builds the menu bar a detached main window hosts
// itself: the app's menus (the first titled with the app's menu name -
// "≡" by default - and carrying only the Quit section, no Hide section,
// no Psi menu), and no calendar.
func (d *Desktop) buildDetachedMenuBar(app ApplicationProvider) *MenuBar {
	mb := NewMenuBar()
	mb.SetHideCalendar(true)

	appName := app.Name()
	menuName := app.MenuName()
	b := bucketMenus(app.MenuBarContent())

	// App menu (mandatory), leading, titled with the app's menu name and
	// carrying only the Quit section. Synthesized when the app declared no
	// app-tagged menu.
	if b.app != nil {
		mb.AddMenu(d.createAppMenuWithQuitOnly(b.app, menuName, appName))
	} else {
		m := NewMenu(menuName)
		d.appendQuitSection(m, appName)
		mb.AddMenu(m)
	}

	d.appendAppBody(mb.AddMenu, app, b)

	// A multi-window app's detached bar gets the system Window menu too
	// (listing the app's windows, Tile/Cascade its OS windows), always before
	// any Help menu.
	if app.MultiWindow() {
		mb.AddMenu(d.buildDetachedWindowMenu(b.window, app))
	} else if b.window != nil {
		mb.AddMenu(b.window)
	}
	if b.help != nil {
		mb.AddMenu(b.help)
	}
	return mb
}

// tornTileItem builds a Tile/Cascade item that arranges the app's torn-off
// OS windows across the desktop (see arrangeTornAppWindows).
func (d *Desktop) tornTileItem(app ApplicationProvider, cascade bool) *MenuItem {
	label := "&Tile"
	if cascade {
		label = "&Cascade"
	}
	it := NewMenuItem(label)
	it.SetOnTriggered(func() { d.arrangeTornAppWindows(app, cascade) })
	return it
}

// buildDetachedWindowMenu builds the system Window menu for a detached main
// window's own bar. It leads with Tile/Cascade (arranging the app's OS
// windows), then - each behind a separator - the app's own Window-menu items
// (if any), then the list of the app's windows. appWin (the app's own Window
// menu, or nil) customizes the title and contributes the middle items.
// Rebuilt on every open; the app's shared menu is left untouched.
func (d *Desktop) buildDetachedWindowMenu(appWin *Menu, app ApplicationProvider) *Menu {
	menu := NewMenu(windowMenuRawTitle(appWin))
	menu.SetWellKnownID(MenuIDWindow)
	var custom []*MenuItem
	if appWin != nil {
		custom = appWin.Items()
	}

	populate := func() {
		menu.Clear()
		menu.AddItem(d.tornTileItem(app, false))
		menu.AddItem(d.tornTileItem(app, true))

		if len(custom) > 0 {
			menu.AddSeparator()
			for _, it := range custom {
				menu.AddItem(it)
			}
		}

		wins := app.Windows()
		if len(wins) > 0 {
			menu.AddSeparator()
			for _, win := range wins {
				d.appendWindowItem(menu, win)
			}
		}
	}

	populate()
	menu.SetOnAboutToShow(populate)
	return menu
}

// windowMenuRawTitle returns the raw title (with accelerator markup) for the
// system Window menu: the app's own Window menu title customizes it, but a
// blank title falls back to the default "&Window".
func windowMenuRawTitle(appWin *Menu) string {
	if appWin != nil && appWin.RawTitle() != "" {
		return appWin.RawTitle()
	}
	return "&Window"
}

// appendWindowItem adds a menu item that raises win, checkmarking it when it
// is the current top-level window.
func (d *Desktop) appendWindowItem(menu *Menu, win *window.Window) {
	win2 := win
	item := NewMenuItem(win2.Title())
	if d.isCurrentTopLevel(win2) {
		item.SetCheckable(true).SetChecked(true)
	}
	item.SetOnTriggered(func() { d.activateWindowFromMenu(win2) })
	menu.AddItem(item)
}

// desktopTileItem builds a Tile/Cascade item that arranges the desktop's
// in-surface windows (the app is docked).
func (d *Desktop) desktopTileItem(cascade bool) *MenuItem {
	label := "&Tile"
	if cascade {
		label = "&Cascade"
	}
	it := NewMenuItem(label)
	it.SetOnTriggered(func() {
		wm := d.WindowManager()
		if wm == nil {
			return
		}
		if cascade {
			wm.CascadeWindows()
		} else {
			wm.TileWindows()
		}
	})
	return it
}

// buildDesktopWindowMenu builds the system Window menu for the desktop bar
// (a multi-window app's main menu showing in the SDL Desktop). It leads with
// Tile/Cascade (arranging in-surface windows), then - each behind a
// separator - the app's own Window-menu items (if it supplied any via a
// MenuIDWindow menu), then the app's own windows, then every other
// in-surface desktop window. appWin (the app's own Window menu, or nil)
// customizes the title and contributes the middle items. Rebuilt on every
// open; the app's shared menu is left untouched.
func (d *Desktop) buildDesktopWindowMenu(appWin *Menu, app ApplicationProvider) *Menu {
	menu := NewMenu(windowMenuRawTitle(appWin))
	menu.SetWellKnownID(MenuIDWindow)
	var custom []*MenuItem
	if appWin != nil {
		custom = appWin.Items()
	}

	populate := func() {
		menu.Clear()
		menu.AddItem(d.desktopTileItem(false))
		menu.AddItem(d.desktopTileItem(true))

		// The app's own Window-menu items sit between Tile/Cascade and the
		// window list, separated on either side.
		if len(custom) > 0 {
			menu.AddSeparator()
			for _, it := range custom {
				menu.AddItem(it)
			}
		}

		// The app's own windows (torn, minimized, or not). MDI children are
		// not registered with the app, so they are excluded.
		appWins := app.Windows()
		seen := make(map[*window.Window]bool, len(appWins))
		if len(appWins) > 0 {
			menu.AddSeparator()
			for _, win := range appWins {
				seen[win] = true
				d.appendWindowItem(menu, win)
			}
		}

		// Every other in-surface desktop window (belonging to other apps).
		var others []*window.Window
		if wm := d.WindowManager(); wm != nil {
			for _, w := range wm.Windows() {
				if !seen[w] {
					others = append(others, w)
				}
			}
		}
		if len(others) > 0 {
			menu.AddSeparator()
			for _, win := range others {
				d.appendWindowItem(menu, win)
			}
		}
	}

	populate()
	menu.SetOnAboutToShow(populate)
	return menu
}

// menuBuckets holds an app's declared menus sorted into their well-known
// roles. Detection is by well-known tag ONLY - there is no title-based
// fallback. Untagged menus (and menus carrying an unrecognized or duplicate
// tag) collect in custom, preserving their declared order.
type menuBuckets struct {
	app    *Menu
	file   *Menu
	edit   *Menu
	sel    *Menu
	format *Menu
	view   *Menu
	custom []*Menu
	window *Menu
	help   *Menu
}

// declaredAny reports whether the app contributed any menu at all.
func (b menuBuckets) declaredAny() bool {
	return b.app != nil || b.file != nil || b.edit != nil || b.sel != nil ||
		b.format != nil || b.view != nil || b.window != nil || b.help != nil ||
		len(b.custom) > 0
}

// bucketMenus sorts an app's declared menus into their well-known roles so
// the system can lay them out in the canonical order (app, file, edit,
// select, format, view, custom..., window, help). The first menu seen for a
// role wins; any later duplicate falls through to custom.
func bucketMenus(menus []*Menu) menuBuckets {
	var b menuBuckets
	for _, m := range menus {
		switch m.WellKnownID() {
		case MenuIDApp:
			if b.app == nil {
				b.app = m
				continue
			}
		case MenuIDFile:
			if b.file == nil {
				b.file = m
				continue
			}
		case MenuIDEdit:
			if b.edit == nil {
				b.edit = m
				continue
			}
		case MenuIDSelect:
			if b.sel == nil {
				b.sel = m
				continue
			}
		case MenuIDFormat:
			if b.format == nil {
				b.format = m
				continue
			}
		case MenuIDView:
			if b.view == nil {
				b.view = m
				continue
			}
		case MenuIDWindow:
			if b.window == nil {
				b.window = m
				continue
			}
		case MenuIDHelp:
			if b.help == nil {
				b.help = m
				continue
			}
		}
		b.custom = append(b.custom, m)
	}
	return b
}

// appendAppBody adds, via add, the middle of the canonical menu bar: the
// app's File menu, the system Edit menu (see systemEditMenu), then the app's
// Select/Format/View menus and any custom menus, all in canonical order. The
// leading app menu and the trailing Window/Help menus are placed by the
// caller (they differ between the docked and detached bars).
func (d *Desktop) appendAppBody(add func(*Menu), app ApplicationProvider, b menuBuckets) {
	if b.file != nil {
		add(b.file)
	}
	if em := d.systemEditMenu(app, b.edit); em != nil {
		add(em)
	}
	if b.sel != nil {
		add(b.sel)
	}
	if b.format != nil {
		add(b.format)
	}
	if b.view != nil {
		add(b.view)
	}
	for _, m := range b.custom {
		add(m)
	}
}

// editActor is the capability a focused trinket advertises to take part in
// the standard Edit menu. A focused trinket that implements all four methods
// is an active edit target: Copy, Paste, and Select All operate on it and
// show enabled. Cut is additionally gated by cutEnabler. A focused trinket
// that does not implement editActor leaves every standard Edit item disabled.
// New editable trinkets opt in simply by implementing this interface.
type editActor interface {
	Cut()
	Copy()
	Paste()
	SelectAll()
}

// cutEnabler lets an edit target report that Cut does not apply to it even
// though the other actions do - a terminal's scrollback can be copied but not
// cut. Absent this interface, an editActor's Cut is enabled.
type cutEnabler interface {
	CutEnabled() bool
}

// selectionReporter lets an edit target report whether it currently holds a
// selection, so the Edit menu can raise a passive "nothing selected" notice
// when Cut or Copy would otherwise be a silent no-op. An edit target that
// does not implement it is assumed to have a selection (no notice).
type selectionReporter interface {
	HasSelection() bool
}

// hasSelection reports whether ea currently has a selection, defaulting to
// true for targets that do not advertise their selection state.
func hasSelection(ea editActor) bool {
	if sr, ok := ea.(selectionReporter); ok {
		return sr.HasSelection()
	}
	return true
}

// editActorProvider lets a focused trinket DELEGATE the standard Edit
// actions to an inner editor it hosts. The TreeView's in-place row
// editor is the model case: the tree keeps the real focus and forwards
// input, so while the editor is up the tree advertises IT as the edit
// target - the Edit menu (and its enabled states) then operate on the
// cell editor exactly as they would on a plain TextInput.
type editActorProvider interface {
	editActorTarget() (editActor, bool)
}

// focusedEditActor returns the focused trinket as an editActor, if it is one.
// A provider that currently hosts an inner editor supersedes the trinket's
// own capabilities; a provider with no active editor falls through.
func (d *Desktop) focusedEditActor() (editActor, bool) {
	if fw := d.FocusedTrinket(); fw != nil {
		if p, ok := fw.(editActorProvider); ok {
			if ea, active := p.editActorTarget(); active {
				return ea, true
			}
		}
		if ea, ok := fw.(editActor); ok {
			return ea, true
		}
	}
	return nil, false
}

// appendStandardEditItems adds the system Edit items - Cut, Copy, Paste,
// separator, Select All - each wired to whatever trinket holds focus when it
// fires. It returns a closure that recomputes their enabled state from the
// currently focused trinket: Copy/Paste/Select All (and Cut) enable only when
// the focused trinket is an editActor, and Cut additionally disables when the
// target reports CutEnabled()==false. Callers wire the closure to the menu's
// OnAboutToShow so the state tracks focus (which rests on the previous active
// window while the menu is open).
func (d *Desktop) appendStandardEditItems(menu *Menu) func() {
	shortcut := func(it *MenuItem, action string) {
		if keys := core.DefaultKeyBindings.Keys(action); len(keys) > 0 {
			it.SetShortcut(core.NewShortcut(keys[0]))
		}
	}

	cut := NewMenuItem("Cu&t")
	shortcut(cut, core.ActionCut)
	cut.SetOnTriggered(func() {
		if ea, ok := d.focusedEditActor(); ok {
			if hasSelection(ea) {
				ea.Cut()
			} else {
				d.NotifyPassive("Nothing was selected to cut.")
			}
		}
	})
	menu.AddItem(cut)

	copyIt := NewMenuItem("&Copy")
	shortcut(copyIt, core.ActionCopy)
	copyIt.SetOnTriggered(func() {
		if ea, ok := d.focusedEditActor(); ok {
			if hasSelection(ea) {
				ea.Copy()
			} else {
				d.NotifyPassive("Nothing was selected to copy.")
			}
		}
	})
	menu.AddItem(copyIt)

	pasteIt := NewMenuItem("&Paste")
	shortcut(pasteIt, core.ActionPaste)
	pasteIt.SetOnTriggered(func() {
		if ea, ok := d.focusedEditActor(); ok {
			ea.Paste()
		}
	})
	menu.AddItem(pasteIt)

	menu.AddSeparator()

	selectAll := NewMenuItem("Select &All")
	shortcut(selectAll, core.ActionSelectAll)
	selectAll.SetOnTriggered(func() {
		if ea, ok := d.focusedEditActor(); ok {
			ea.SelectAll()
		}
	})
	menu.AddItem(selectAll)

	update := func() {
		ea, editable := d.focusedEditActor()
		copyIt.SetEnabled(editable)
		pasteIt.SetEnabled(editable)
		selectAll.SetEnabled(editable)

		cutOK := editable
		if editable {
			if ce, ok := ea.(cutEnabler); ok {
				cutOK = ce.CutEnabled()
			}
		}
		cut.SetEnabled(cutOK)
	}
	update()
	return update
}

// systemEditMenu builds the Edit menu the system supplies. On a text/TUI
// surface, and on a graphical surface unless the app opted into ContextOnly,
// it leads with the standard Cut/Copy/Paste/Select All items (wired to the
// focused trinket, enabled per its advertised capability). Any Edit menu the
// app declared (declared != nil) contributes its own items after those,
// behind a separator. The declared menu's title customizes the menu; a blank
// or absent declaration falls back to "&Edit".
//
// It returns nil only when there is nothing to show: a ContextOnly graphical
// app that declared no Edit menu of its own.
func (d *Desktop) systemEditMenu(app ApplicationProvider, declared *Menu) *Menu {
	auto := true
	if d.graphicalFrames && app != nil && app.ContextOnly() {
		auto = false
	}
	if !auto && declared == nil {
		return nil
	}

	title := "&Edit"
	if declared != nil && declared.RawTitle() != "" {
		title = declared.RawTitle()
	}
	menu := NewMenu(title)
	menu.SetWellKnownID(MenuIDEdit)

	var custom []*MenuItem
	if declared != nil {
		custom = declared.Items()
	}

	if auto {
		update := d.appendStandardEditItems(menu)
		if len(custom) > 0 {
			menu.AddSeparator()
			for _, it := range custom {
				menu.AddItem(it)
			}
		}
		menu.SetOnAboutToShow(update)
	} else {
		for _, it := range custom {
			menu.AddItem(it)
		}
	}
	return menu
}

// hostForWindow returns the tear-off host currently hosting win, or nil.
func (d *Desktop) hostForWindow(win *window.Window) *window.TearOffHost {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, th := range d.tornHosts {
		if th.Window() == win {
			return th
		}
	}
	return nil
}

// arrangeTornAppWindows tiles or cascades the application's torn-off,
// non-MDI windows across the OS desktop, placing the main window as the
// upper-left window of the arrangement. MDI children live inside their
// parent window, so they are not arranged. Called from a detached window's
// own Window menu.
func (d *Desktop) arrangeTornAppWindows(app ApplicationProvider, cascade bool) {
	if app == nil {
		return
	}

	native := func(w *window.Window) platform.NativeSurface {
		h := d.hostForWindow(w)
		if h == nil {
			return nil
		}
		n, _ := h.Surface().(platform.NativeSurface)
		return n
	}

	// Ordered list: the main window first (it lands upper-left), then the
	// app's other torn, non-minimized windows.
	var wins []*window.Window
	seen := map[*window.Window]bool{}
	add := func(w *window.Window) {
		if w == nil || seen[w] {
			return
		}
		if n := native(w); n == nil || n.Minimized() {
			return
		}
		seen[w] = true
		wins = append(wins, w)
	}
	add(app.MainWindow())
	for _, w := range app.Windows() {
		add(w)
	}
	if len(wins) == 0 {
		return
	}

	wx, wy, ww, wh := native(wins[0]).WorkAreaPx()
	if ww <= 0 || wh <= 0 {
		return
	}

	// place positions and (optionally) resizes a window's OS surface,
	// honoring its constraints: a WindowFlagNoResize window keeps its own
	// size, a WindowFlagNoMove window keeps its own position.
	place := func(w *window.Window, n platform.NativeSurface, x, y, cw, ch int) {
		if w.Flags()&window.WindowFlagNoResize == 0 {
			n.SetScreenSizePx(cw, ch)
		}
		if w.Flags()&window.WindowFlagNoMove == 0 {
			n.SetScreenPositionPx(x, y)
		}
	}

	if cascade {
		// Uniform size, staggered from the top-left; the main window is least
		// offset so it sits at the upper-left, later windows stacked over it.
		step := wh / 16
		if step < 24 {
			step = 24
		}
		cw, ch := ww*2/3, wh*2/3
		for i, w := range wins {
			n := native(w)
			if n == nil {
				continue
			}
			ox, oy := wx+i*step, wy+i*step
			if ox+cw > wx+ww {
				ox = wx + ww - cw
			}
			if oy+ch > wy+wh {
				oy = wy + wh - ch
			}
			place(w, n, ox, oy, cw, ch)
			n.Raise()
		}
		return
	}

	// Grid tile via the shared balanced layout: rows fill from the bottom,
	// each row splits its own width, non-resizable windows are fit into
	// suitable cells, and the main window is pinned to the upper-left.
	main := app.MainWindow()
	items := make([]window.TileItem, len(wins))
	for i, w := range wins {
		pw, ph := native(w).ScreenSizePx()
		items[i] = window.TileItem{
			Resizable: w.Flags()&window.WindowFlagNoResize == 0,
			Size:      core.UnitSize{Width: core.Unit(pw), Height: core.Unit(ph)},
			First:     w == main,
		}
	}
	cells := window.TileLayout(
		core.UnitRect{X: core.Unit(wx), Y: core.Unit(wy), Width: core.Unit(ww), Height: core.Unit(wh)},
		items,
	)
	for i, w := range wins {
		n := native(w)
		if n == nil {
			continue
		}
		c := cells[i]
		place(w, n, int(c.X), int(c.Y), int(c.Width), int(c.Height))
	}
}

// buildDetachedStatusBar builds the status bar a detached main window
// hosts, seeded with the app's status sections.
func (d *Desktop) buildDetachedStatusBar(app ApplicationProvider) *StatusBar {
	sb := NewStatusBar()
	if sections := app.StatusBarContent(); len(sections) > 0 {
		sb.SetSections(sections)
	}
	return sb
}

// attachMainWindowChrome gives a detached main window its own menu bar
// and status bar; detachMainWindowChrome removes them on re-dock.
func (d *Desktop) attachMainWindowChrome(win *window.Window) {
	app := d.applicationForMainWindow(win)
	if app == nil {
		return
	}
	win.SetWindowMenuBar(d.buildDetachedMenuBar(app))
	win.SetWindowStatusBar(d.buildDetachedStatusBar(app))
}

func (d *Desktop) detachMainWindowChrome(win *window.Window) {
	win.SetWindowMenuBar(nil)
	win.SetWindowStatusBar(nil)
}

// isCurrentTopLevel reports whether win is the top-level window currently
// shown lit - either genuinely focused (double border) or thick/single
// (quasi-active or the desktop's menu-remembered passive window). The Window
// menu marks this one with a checkmark.
func (d *Desktop) isCurrentTopLevel(win *window.Window) bool {
	if win == nil {
		return false
	}
	return win.IsActive() || win.IsQuasiActive() || d.IsWindowPassive(win)
}

// tileableDesktopWindows returns the desktop's in-surface windows that
// Tile/Cascade would arrange: visible and not minimized.
func (d *Desktop) tileableDesktopWindows() []*window.Window {
	wm := d.WindowManager()
	if wm == nil {
		return nil
	}
	var out []*window.Window
	for _, w := range wm.Windows() {
		if w.IsVisible() && !w.IsMinimized() {
			out = append(out, w)
		}
	}
	return out
}

// activateWindowFromMenu raises a window chosen from a Window menu, from
// whichever surface it lives on, restoring it first if it was minimized.
// A torn-off window's own OS surface is raised; an in-surface desktop
// window is raised/activated within the desktop and the desktop's own OS
// window is brought to the front.
func (d *Desktop) activateWindowFromMenu(win *window.Window) {
	if win == nil {
		return
	}
	d.mu.RLock()
	hosts := make([]*window.TearOffHost, len(d.tornHosts))
	copy(hosts, d.tornHosts)
	surface := d.surface
	d.mu.RUnlock()

	// Torn-off window: raise its own OS surface (restore first if needed).
	for _, th := range hosts {
		if th.Window() == win {
			if win.IsMinimized() {
				win.Restore()
			}
			if n, ok := th.Surface().(platform.NativeSurface); ok {
				n.Raise()
			}
			return
		}
	}

	// In-surface desktop window: raise/restore within the desktop, then
	// bring the desktop's own OS window forward.
	if wm := d.WindowManager(); wm != nil {
		if win.IsMinimized() {
			wm.RestoreWindow(win)
		} else {
			wm.ActivateWindow(win)
		}
	}
	if n, ok := surface.(platform.NativeSurface); ok {
		n.Raise()
	}
}

// buildWindowTileCascadeMenu builds the reduced Window menu shown on the
// desktop bar while the main window is detached: Tile and Cascade (disabled
// when there is nothing to arrange), plus - when there are windows - a
// separator and a list of the desktop's windows that raises the chosen one.
// It repopulates on every open so the window list and the enabled state
// stay current.
func (d *Desktop) buildWindowTileCascadeMenu() *Menu {
	menu := NewMenu("&Window")

	populate := func() {
		menu.Clear()

		tileable := d.tileableDesktopWindows()

		tile := NewMenuItem("&Tile")
		tile.SetOnTriggered(func() {
			if wm := d.WindowManager(); wm != nil {
				wm.TileWindows()
			}
		})
		cascade := NewMenuItem("&Cascade")
		cascade.SetOnTriggered(func() {
			if wm := d.WindowManager(); wm != nil {
				wm.CascadeWindows()
			}
		})
		// Nothing to tile/cascade when there are no non-minimized windows.
		if len(tileable) == 0 {
			tile.SetEnabled(false)
			cascade.SetEnabled(false)
		}
		menu.AddItem(tile)
		menu.AddItem(cascade)

		if len(tileable) > 0 {
			menu.AddSeparator()
			for _, win := range tileable {
				win := win
				item := NewMenuItem(win.Title())
				if d.isCurrentTopLevel(win) {
					item.SetCheckable(true).SetChecked(true)
				}
				item.SetOnTriggered(func() { d.activateWindowFromMenu(win) })
				menu.AddItem(item)
			}
		}
	}

	populate()
	menu.SetOnAboutToShow(populate)
	return menu
}

// hideAppWindow minimizes one of an app's windows. A torn-off window is
// OS-minimized on its own surface (no SDL dock entry); an in-surface window
// goes to the desktop dock via the window manager.
func (d *Desktop) hideAppWindow(win *window.Window) {
	if win == nil {
		return
	}
	if h := d.hostForWindow(win); h != nil {
		if n, ok := h.Surface().(platform.NativeSurface); ok && !n.Minimized() {
			n.Minimize()
		}
		return
	}
	if d.windowManager != nil && win.IsVisible() && !win.IsMinimized() {
		d.windowManager.MinimizeWindow(win)
	}
}

// showAppWindow restores one of an app's windows. A torn-off window is
// un-minimized on its own OS surface; an in-surface window is restored from
// the desktop dock.
func (d *Desktop) showAppWindow(win *window.Window) {
	if win == nil {
		return
	}
	if h := d.hostForWindow(win); h != nil {
		if n, ok := h.Surface().(platform.NativeSurface); ok && n.Minimized() {
			if r, ok := h.Surface().(platform.NativeRestorer); ok {
				r.Restore()
			}
		}
		return
	}
	if d.windowManager != nil && win.IsMinimized() {
		d.windowManager.RestoreWindow(win)
	}
}

// hideActiveApp minimizes all windows of the active application.
func (d *Desktop) hideActiveApp() {
	d.mu.RLock()
	activeApp := d.activeApp
	d.mu.RUnlock()

	if activeApp == nil {
		return
	}
	for _, win := range activeApp.Windows() {
		d.hideAppWindow(win)
	}
}

// hideOtherApps minimizes all windows of applications other than the active one.
func (d *Desktop) hideOtherApps() {
	d.mu.RLock()
	activeApp := d.activeApp
	apps := make([]ApplicationProvider, len(d.applications))
	copy(apps, d.applications)
	d.mu.RUnlock()

	for _, app := range apps {
		if app != activeApp {
			for _, win := range app.Windows() {
				d.hideAppWindow(win)
			}
		}
	}
}

// showAllApps restores all minimized windows of all applications.
func (d *Desktop) showAllApps() {
	d.mu.RLock()
	apps := make([]ApplicationProvider, len(d.applications))
	copy(apps, d.applications)
	d.mu.RUnlock()

	for _, app := range apps {
		for _, win := range app.Windows() {
			d.showAppWindow(win)
		}
	}
}

// quitActiveApp closes all windows of the active application and removes it.
// If this was the last application with windows, the desktop automatically exits.
func (d *Desktop) quitActiveApp() {
	d.mu.RLock()
	activeApp := d.activeApp
	d.mu.RUnlock()

	if activeApp == nil {
		return
	}

	// Close all windows of this application
	for _, win := range activeApp.Windows() {
		if win != nil {
			win.Close()
		}
	}

	// Remove the application from the desktop
	d.RemoveApplication(activeApp)

	// Check if there are any remaining windows across all applications
	d.mu.RLock()
	hasWindows := false
	for _, app := range d.applications {
		if len(app.Windows()) > 0 {
			hasWindows = true
			break
		}
	}
	d.mu.RUnlock()

	// If no windows remain, exit the desktop
	if !hasWindows {
		d.Quit()
	}
}

// updateStatusBarContent updates the status bar with the active app's content.
func (d *Desktop) updateStatusBarContent() {
	if d.statusBar == nil {
		return
	}

	d.mu.RLock()
	activeApp := d.activeApp
	d.mu.RUnlock()

	if activeApp != nil {
		d.statusBar.SetSections(activeApp.StatusBarContent())
	} else {
		d.statusBar.SetSections(nil)
	}
}

// RefreshStatusBar refreshes the status bar from the active app's content.
// This is called by applications when their status bar content changes.
func (d *Desktop) RefreshStatusBar() {
	d.updateStatusBarContent()
}

// SetOnStartup sets the startup callback.
func (d *Desktop) SetOnStartup(handler func()) {
	d.mu.Lock()
	d.onStartup = handler
	d.mu.Unlock()
}

// SetOnShutdown sets the shutdown callback.
func (d *Desktop) SetOnShutdown(handler func()) {
	d.mu.Lock()
	d.onShutdown = handler
	d.mu.Unlock()
}

// FocusedTrinket returns the trinket with keyboard focus in the active
// window, or nil. Window focus lives in each window's own focus
// manager (the desktop's GlobalFocusManager tracks scopes, not the
// per-window focused trinket), so this reaches through the active
// window. Menu actions run after window focus is restored
// (Menu.onWillTrigger fires RestorePreviousActiveWindow before the
// action), so the active window is valid when a menu command asks.
func (d *Desktop) FocusedTrinket() core.Trinket {
	d.mu.RLock()
	wm := d.windowManager
	torn := d.tornFocusOwner
	d.mu.RUnlock()

	// A torn-off window keeps its own focus manager and is no longer in
	// the window manager's stack. When it currently owns focus, menu
	// actions (and global shortcuts) must target its focused trinket, not
	// whatever docked window the window manager still calls active.
	if torn != nil {
		if fw := torn.FocusManager().FocusedTrinket(); fw != nil {
			return resolveFocusedTrinket(fw)
		}
	}

	if wm == nil {
		return nil
	}
	win := wm.ActiveWindow()
	if win == nil {
		// While a menu is open the active window is deactivated
		// (its focus is remembered as the previous window); menu
		// about-to-show hooks and actions still target it.
		win = wm.PreviousActiveWindow()
	}
	if win != nil {
		return resolveFocusedTrinket(win.FocusManager().FocusedTrinket())
	}
	return nil
}

// mdiFocusHost is implemented by a trinket (an MDIPane) that hosts its own
// child windows: real keyboard focus can live inside its active child window,
// deeper than the enclosing window's focus manager reaches.
type mdiFocusHost interface {
	ActiveWindow() *window.Window
}

// resolveFocusedTrinket drills through MDI panes: a window's focus manager
// only reaches as far as the MDIPane trinket, but the actually-focused control
// (e.g. an input box) lives inside the pane's active child window. Follow the
// active child's own focus, recursively for nested MDI, so Edit-menu
// inspection sees the real focused trinket rather than the pane. Nil-safe.
func resolveFocusedTrinket(t core.Trinket) core.Trinket {
	for {
		host, ok := t.(mdiFocusHost)
		if !ok {
			return t
		}
		win := host.ActiveWindow()
		if win == nil {
			return t // no active child: the pane itself is the focus
		}
		fw := win.FocusManager().FocusedTrinket()
		if fw == nil {
			return t
		}
		t = fw
	}
}

// updateCursor resolves and applies the system mouse cursor for the
// pointer position: a resize cursor over a window's size-sensitive edge,
// a text I-beam over an editable control, otherwise the arrow. No-op on
// platforms without cursor control.
func (d *Desktop) updateCursor(x, y core.Unit) {
	d.mu.RLock()
	plat := d.platform
	wm := d.windowManager
	d.mu.RUnlock()
	if wm == nil {
		return
	}
	cc, ok := plat.(platform.CursorController)
	if !ok {
		return
	}
	cc.SetCursor(wm.CursorAt(x, y))
}

// ActivatePassNextKeyToTrinket activates pass-next-key-to-trinket mode for the active app.
// The next keypress will bypass all global shortcut handling and go directly
// to the focused trinket. This can be called from menu items or other UI elements.
func (d *Desktop) ActivatePassNextKeyToTrinket() {
	d.mu.RLock()
	activeApp := d.activeApp
	d.mu.RUnlock()
	if activeApp == nil {
		return
	}
	// When the app's main window is detached, the key stream and the
	// status bar both live on that window, not the desktop. Arm raw-key
	// mode there so the next key reaches the detached window's focused
	// trinket and the prompt shows on its own status bar.
	if main := activeApp.MainWindow(); main != nil && main.IsDetached() {
		d.activateRawKeyOnDetached(main)
		return
	}
	activeApp.ActivatePassNextKeyToTrinket()
}

// activateRawKeyOnDetached shows the raw-key prompt on the detached main
// window's own status bar and arms the window to pass its next key
// straight to the focused trinket, restoring the status bar afterwards.
func (d *Desktop) activateRawKeyOnDetached(main *window.Window) {
	sb, _ := main.WindowStatusBar().(*StatusBar)
	var saved []StatusSection
	if sb != nil {
		saved = sb.Sections()
		sb.SetSections([]StatusSection{
			{Text: "Raw Key Input: The next key pressed will be passed directly to the focused trinket."},
		})
		main.Update()
	}
	main.BeginRawKeyInput(func() {
		if sb != nil {
			sb.SetSections(saved)
			main.Update()
		}
	})
}

// AddEventFilter adds an event filter.
// Filters are called before normal event handling and can consume events.
// Return true to consume the event, false to let it propagate.
func (d *Desktop) AddEventFilter(filter func(core.Event) bool) {
	d.mu.Lock()
	d.eventFilters = append(d.eventFilters, filter)
	d.mu.Unlock()
}

// filterEvent runs the event through all filters.
// Returns true if the event was consumed.
func (d *Desktop) filterEvent(event core.Event) bool {
	d.mu.RLock()
	filters := d.eventFilters
	d.mu.RUnlock()

	for _, filter := range filters {
		if filter(event) {
			return true
		}
	}
	return false
}

// Run starts the desktop under the inverted loop (G3/D21): it wraps
// the polled backend in a one-surface Platform and hands control
// over. Kept as the compatible entry point; RunOn is the general
// form. Returns the exit code when the desktop quits.
func (d *Desktop) Run() int {
	d.mu.RLock()
	backend := d.backend
	d.mu.RUnlock()
	if backend == nil {
		return 1
	}
	return d.RunOn(platform.NewPolling(backend))
}

// RunOn runs the desktop as the content of one surface of the given
// platform. All dispatch, layout, and paint happen on the platform's
// main thread (D21); Platform.Post is the only cross-thread door.
func (d *Desktop) RunOn(p platform.Platform) int {
	d.mu.Lock()
	d.platform = p
	onStartup := d.onStartup
	wm := d.windowManager
	d.mu.Unlock()

	code := p.Run(func(pf platform.Platform) {
		surface, err := pf.CreateSurface(platform.SurfaceOptions{})
		if err != nil {
			pf.Quit(1)
			return
		}
		d.mu.Lock()
		d.surface = surface
		d.mu.Unlock()
		surface.SetHandler(&desktopSurfaceHandler{d: d})
		d.setupTearOff(pf, surface)

		size := surface.Size()
		wm.SetScreenBounds(core.UnitRect{Width: size.Width, Height: size.Height})

		d.running.Store(true)
		// Wake the tick's repaint only when something actually changed.
		core.SetRepaintHook(func() { d.needsFrame.Store(true) })
		if onStartup != nil {
			onStartup()
		}
		surface.Invalidate(core.UnitRect{})
		d.scheduleTick(pf)
	})

	d.running.Store(false)
	core.SetRepaintHook(nil)

	d.mu.RLock()
	onShutdown := d.onShutdown
	d.mu.RUnlock()
	if onShutdown != nil {
		onShutdown()
	}
	return code
}

// tickHeartbeatTicks is how many 50ms ticks may pass with nothing requesting a
// repaint before one is forced anyway - a ~1s safety net so any animation that
// mutates without calling Update() still advances.
const tickHeartbeatTicks = 20

// InvalidateRect requests a partial repaint of a rectangle in main-surface
// coordinates - the tick repaints (and re-uploads) only that region unless a
// full repaint was also requested. A degenerate rect escalates to a full
// repaint. Only ever called with main-surface rects (see IsMainSurfaceController).
func (d *Desktop) InvalidateRect(r core.UnitRect) {
	if r.Width <= 0 || r.Height <= 0 {
		d.needsFrame.Store(true)
		return
	}
	d.damageMu.Lock()
	if d.hasDamage {
		d.damageRect = unionDesktopRect(d.damageRect, r)
	} else {
		d.damageRect = r
		d.hasDamage = true
	}
	d.damageMu.Unlock()
}

// IsMainSurfaceController reports whether pc is the desktop's own window
// manager - i.e. content it manages composites on the main surface. Partial
// (bounded) invalidation is restricted to this case; torn-off windows are
// separate surfaces and take a full invalidate instead.
func (d *Desktop) IsMainSurfaceController(pc core.PopupController) bool {
	if pc == nil {
		return false
	}
	d.mu.RLock()
	wm := d.windowManager
	d.mu.RUnlock()
	return pc == core.PopupController(wm)
}

// unionDesktopRect returns the smallest rectangle covering both.
func unionDesktopRect(a, b core.UnitRect) core.UnitRect {
	x0, y0 := a.X, a.Y
	if b.X < x0 {
		x0 = b.X
	}
	if b.Y < y0 {
		y0 = b.Y
	}
	x1, y1 := a.X+a.Width, a.Y+a.Height
	if b.X+b.Width > x1 {
		x1 = b.X + b.Width
	}
	if b.Y+b.Height > y1 {
		y1 = b.Y + b.Height
	}
	return core.UnitRect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// scheduleTick keeps desktop timers firing. It repaints only when an Update()
// arrived since the last tick (via the core repaint hook), with a slow
// heartbeat fallback, so an idle desktop no longer burns a full frame every
// tick. Self-reposting through the platform (D21: PostAfter is the timer
// primitive).
func (d *Desktop) scheduleTick(p platform.Platform) {
	p.PostAfter(50*time.Millisecond, func() {
		if !d.running.Load() {
			return
		}
		d.ProcessTimers()
		// Flush a held navigation announcement once the user has paused.
		if am := d.AccessibilityManager(); am != nil {
			am.ProcessPending()
		}
		d.mu.RLock()
		s := d.surface
		torn := append([]*window.TearOffHost(nil), d.tornHosts...)
		d.mu.RUnlock()

		// Only repaint when something asked for one since the last tick: a full
		// repaint (needsFrame, from a plain Update()) or bounded damage (a
		// partial region, e.g. a blinking caret). A ~1s heartbeat is the safety
		// net for anything that animates without signalling. An idle desktop
		// otherwise stops repainting instead of burning a full frame per tick.
		full := d.needsFrame.Swap(false)
		d.damageMu.Lock()
		hasDmg := d.hasDamage
		dmg := d.damageRect
		d.hasDamage = false
		d.damageRect = core.UnitRect{}
		d.damageMu.Unlock()

		d.idleTicks++
		if !full && !hasDmg && d.idleTicks >= tickHeartbeatTicks {
			full = true
		}
		switch {
		case full:
			d.idleTicks = 0
			if s != nil {
				s.Invalidate(core.UnitRect{})
			}
			for _, h := range torn {
				h.Invalidate()
			}
		case hasDmg:
			// Bounded damage only ever targets the main surface; torn surfaces
			// take a full invalidate via the full path above.
			d.idleTicks = 0
			if s != nil {
				s.Invalidate(dmg)
			}
		}
		d.scheduleTick(p)
	})
}

// desktopSurfaceHandler adapts the Desktop to platform.SurfaceHandler.
type desktopSurfaceHandler struct {
	d *Desktop
}

// Frame paints the whole desktop (v1 full-frame contract).
func (h *desktopSurfaceHandler) Frame(painter *core.Painter) {
	d := h.d
	d.mu.RLock()
	wm := d.windowManager
	theme := d.theme
	s := d.surface
	d.mu.RUnlock()

	size := s.Size()
	painter.Clear(core.UnitRect{Width: size.Width, Height: size.Height}, theme.Normal)
	wm.Paint(painter)
}

// Resized reports the terminal/window size change.
func (h *desktopSurfaceHandler) Resized(size core.UnitSize) {
	d := h.d
	d.mu.RLock()
	wm := d.windowManager
	s := d.surface
	d.mu.RUnlock()
	wm.SetScreenBounds(core.UnitRect{Width: size.Width, Height: size.Height})
	if s != nil {
		s.Invalidate(core.UnitRect{})
	}
}

// Event dispatches one input event, then requests a frame (parity
// with the historical render-after-events loop).
func (h *desktopSurfaceHandler) Event(ev core.Event) bool {
	d := h.d
	handled := d.dispatchEvent(ev)
	d.mu.RLock()
	s := d.surface
	d.mu.RUnlock()
	if s != nil {
		s.Invalidate(core.UnitRect{})
	}
	return handled
}

// dispatchEvent routes one input event through the desktop: pass-key
// mode, event filters, shortcuts, focus, window manager. Runs on the
// platform thread (delivered by the surface handler).
func (d *Desktop) dispatchEvent(event core.Event) bool {
	d.mu.RLock()
	wm := d.windowManager
	fm := d.focusManager
	d.mu.RUnlock()

	// A live tear-off drag owns the pointer stream: the desktop keeps
	// the capture while the torn window follows the pointer.
	if d.handleTornDrag(event) {
		return true
	}

	// Check pass-next-key-to-trinket mode FIRST, before any event filters.
	// This ensures the key goes directly to the trinket without any interception.
	if keyEvent, isKey := event.(core.KeyPressEvent); isKey {
		d.mu.RLock()
		activeApp := d.activeApp
		d.mu.RUnlock()
		if activeApp != nil && activeApp.PassNextKeyToTrinket() {
			activeApp.ClearPassNextKeyToTrinket()
			// Skip ALL shortcut handling - send key directly to the active window's
			// focused trinket, bypassing WindowManager's menu accelerator interception
			if wm != nil {
				if activeWin := wm.ActiveWindow(); activeWin != nil {
					activeWin.HandleKeyPress(keyEvent)
				}
			}
			return true
		}
	}

	// Run through event filters
	if d.filterEvent(event) {
		return true
	}

	// Handle event based on type
	switch e := event.(type) {
	case core.QuitEvent:
		d.QuitWithCode(0)
		return true

	case core.KeyPressEvent:
		// Pass-next-key mode is handled above, before event filters.
		// Check global shortcuts first
		if d.handleShortcut(e) {
			return true
		}
		// Try focus manager
		if fm != nil && fm.HandleKeyPress(e) {
			return true
		}
		// Pass to window manager
		return wm.HandleKeyPress(e)

	case core.KeyReleaseEvent:
		// When every modifier has gone up, lock in an in-progress window-cycle
		// run's MRU order (the Alt-Tab "commit on release"). Only the graphical
		// backend delivers releases; the WM ignores this on the TUI.
		if e.Modifiers == 0 && wm != nil {
			wm.NotifyModifiersReleased()
		}
		return false

	case core.FocusEvent:
		// The desktop's OS window gained or lost focus: its active
		// window's chrome follows, the same way a torn-off window's
		// chrome follows its own OS window. WM state is untouched -
		// re-focusing lights the same window back up.
		//
		// Exception: while a detached window owns focus (and the menu bar
		// line), regaining focus must NOT re-light an in-surface window -
		// that would silently steal focus from the detached window. Only
		// an actual click on an in-surface window changes the focus.
		d.mu.RLock()
		owner := d.tornFocusOwner
		d.mu.RUnlock()
		tornOwns := owner != nil
		if aw := wm.ActiveWindow(); aw != nil {
			if e.Focused && tornOwns {
				// Don't relight; leave the detached window as the owner.
			} else {
				aw.SetActive(e.Focused)
			}
		}
		if e.Focused && owner != nil {
			// The torn window whose focus "lives" on the desktop menu bar
			// stays quasi-active while the desktop holds OS focus, mirroring
			// the in-surface blur: you can pick menu items that apply to it.
			// Its OS surface dimmed it on FOCUS_LOST, so re-light it here -
			// but as quasi-active (lit, single/heavy border) rather than a
			// full double-bordered focus, since focus really is on the
			// desktop, not the torn window. Exclusive: any other top-level
			// window still carrying a lit/heavy style goes inactive.
			d.quasiActivateExclusive(owner)
		}
		return true

	case core.MousePressEvent:
		return wm.HandleMousePress(e)

	case core.MouseMoveEvent:
		core.WheelPointerMoved()
		handled := wm.HandleMouseMove(e)
		// The resize cursor over a window edge is a hover affordance. While a
		// button is held, a drag begun elsewhere is in progress: leave the
		// cursor as the gesture set it (a real resize keeps its cursor because
		// it was set on the pre-press hover) rather than flipping to a resize
		// shape as the pointer crosses an edge.
		if e.Buttons == 0 {
			d.updateCursor(e.X, e.Y)
		}
		return handled

	case core.MouseLeaveEvent:
		// Pointer left the desktop surface: drop any resize-edge highlight,
		// clear per-widget hover in windows and the desktop chrome, and reset
		// the cursor to the arrow.
		wm.ClearResizeHover()
		wm.ClearHover()
		if d.menuBar != nil {
			d.menuBar.HandleMouseMove(core.MouseMoveEvent{X: -1, Y: -1})
		}
		if d.dockRow != nil {
			d.dockRow.HandleMouseMove(core.MouseMoveEvent{X: -1, Y: -1})
		}
		if cc, ok := d.platform.(platform.CursorController); ok {
			cc.SetCursor(core.CursorDefault)
		}
		return true

	case core.MouseReleaseEvent:
		return wm.HandleMouseRelease(e)

	case core.MouseWheelEvent:
		// Stamp the screen position once; translations preserve it.
		e.ScreenX, e.ScreenY = e.X, e.Y
		// An active gesture stays latched to its claimant.
		if core.DeliverLatchedWheel(e) {
			return true
		}
		// Menu wheel handling before anything beneath: an open dropdown
		// scrolls its items; a wheel/two-finger pan over the (closed)
		// bar pans an overflowing bar horizontally. Both paths live in
		// MenuBar.HandleMouseWheel; it declines (false) when neither
		// applies, so we fall through to the windows below.
		if d.menuBar != nil {
			overBar := d.menuBar.Bounds().Contains(core.UnitPoint{X: e.X, Y: e.Y})
			if d.menuBar.ActiveMenu() != nil || overBar {
				if d.menuBar.HandleMouseWheel(e) {
					return true
				}
			}
		}
		return wm.HandleMouseWheel(e)
	}
	return false
}

// handleShortcut checks if a key event matches a global shortcut.
// This is called BEFORE the focus manager, so these shortcuts work even
// when an EditBox or other input trinket has focus.
//
// All shortcuts are now handled through menu items - this method delegates
// to handleMenuShortcut which checks both the system menu and app menus.
func (d *Desktop) handleShortcut(event core.KeyPressEvent) bool {
	// Check global menu shortcuts (system menu + active app's menus)
	return d.handleMenuShortcut(event)
}

// handleMenuShortcut checks if a key event matches any menu item shortcut.
// This allows menu shortcuts to work globally even when menus are closed.
// Checks both the system menu and the active application's menus.
func (d *Desktop) handleMenuShortcut(event core.KeyPressEvent) bool {
	// Check system menu first (contains Exit Desktop, etc.)
	if d.systemMenu != nil {
		if d.checkMenuItemShortcuts(d.systemMenu, event) {
			return true
		}
	}

	d.mu.RLock()
	activeApp := d.activeApp
	d.mu.RUnlock()

	if activeApp == nil {
		return false
	}

	// Check all menus from the active app (includes standard items like Quit, Hide, etc.)
	for _, menu := range activeApp.MenuBarContent() {
		if d.checkMenuItemShortcuts(menu, event) {
			return true
		}
	}

	// Also check the merged menu (which includes standard app items added by Desktop)
	// This is needed because MenuBarContent() returns the app's original menus,
	// but appendStandardAppItems adds Quit, Hide, etc. to a merged copy
	if d.menuBar != nil {
		for _, menu := range d.menuBar.Menus() {
			// Skip system menu (already checked above)
			if menu == d.systemMenu {
				continue
			}
			if d.checkMenuItemShortcuts(menu, event) {
				return true
			}
		}
	}

	return false
}

// checkMenuItemShortcuts recursively checks menu items for matching shortcuts.
func (d *Desktop) checkMenuItemShortcuts(menu *Menu, event core.KeyPressEvent) bool {
	if menu == nil {
		return false
	}

	for _, item := range menu.Items() {
		if item == nil || item.Separator || !item.Enabled {
			continue
		}

		// Check if this item's shortcut matches. Trigger routes
		// through the command registry (dispatch by stable ID), and
		// keeps checkable-toggle semantics consistent with clicking.
		if item.Shortcut != "" && item.Shortcut.Matches(event) {
			item.Trigger()
			return true
		}

		// Recursively check submenus
		if item.SubMenu != nil {
			if d.checkMenuItemShortcuts(item.SubMenu, event) {
				return true
			}
		}
	}
	return false
}

// ProcessTimers checks and fires due timers.
// This is called by the Application's event loop to process desktop timers.
func (d *Desktop) ProcessTimers() {
	d.timerMutex.Lock()
	now := time.Now()
	var toFire []*DesktopTimer
	var remaining []*DesktopTimer

	for _, timer := range d.timers {
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

	d.timers = remaining
	d.timerMutex.Unlock()

	// Fire timers outside lock
	for _, timer := range toFire {
		if timer.Callback != nil {
			timer.Callback()
		}
	}
}

// Post schedules fn on the platform (UI) thread - the D21
// cross-thread door, exposed for embedders like the display-service
// transport whose socket readers must hand work to the UI. Before
// the desktop runs (no platform yet) fn runs inline; use only from
// wiring code in that window.
func (d *Desktop) Post(fn func()) {
	d.mu.RLock()
	p := d.platform
	d.mu.RUnlock()
	if p != nil {
		p.Post(fn)
		return
	}
	fn()
}

// RequestUpdate requests a screen update (damage-driven: invalidates
// the surface; the platform schedules the frame).
func (d *Desktop) RequestUpdate() {
	d.mu.RLock()
	s := d.surface
	d.mu.RUnlock()
	if s != nil {
		s.Invalidate(core.UnitRect{})
	}
	select {
	case d.updateChan <- struct{}{}:
	default:
		// Channel full, update already pending
	}
}

// Quit requests the desktop to quit.
func (d *Desktop) Quit() {
	d.QuitWithCode(0)
}

// QuitWithCode requests the desktop to quit with an exit code.
func (d *Desktop) QuitWithCode(code int) {
	d.mu.Lock()
	d.exitCode = code
	p := d.platform
	d.mu.Unlock()
	d.running.Store(false)
	if p != nil {
		p.Quit(code)
	}
	select {
	case <-d.quitChan:
		// Already closed
	default:
		close(d.quitChan)
	}
}

// IsRunning returns whether the desktop is running.
func (d *Desktop) IsRunning() bool {
	return d.running.Load()
}

// StartTimer starts a single-shot timer.
func (d *Desktop) StartTimer(interval time.Duration, callback func()) *DesktopTimer {
	return d.startTimerInternal(interval, false, callback)
}

// StartRepeatingTimer starts a repeating timer.
func (d *Desktop) StartRepeatingTimer(interval time.Duration, callback func()) *DesktopTimer {
	return d.startTimerInternal(interval, true, callback)
}

func (d *Desktop) startTimerInternal(interval time.Duration, repeat bool, callback func()) *DesktopTimer {
	d.timerMutex.Lock()
	defer d.timerMutex.Unlock()

	timer := &DesktopTimer{
		ID:       len(d.timers) + 1,
		Interval: interval,
		Repeat:   repeat,
		Callback: callback,
		nextFire: time.Now().Add(interval),
	}
	d.timers = append(d.timers, timer)
	return timer
}

// StopTimer stops a timer.
func (d *Desktop) StopTimer(timer *DesktopTimer) {
	if timer != nil {
		timer.stopped = true
	}
}

// passiveNotificationDuration is how long a passive status-bar notification
// stays up before the bar reverts to its normal content.
const passiveNotificationDuration = 2 * time.Second

// NotifyPassive flashes a transient message across the status bar for a
// couple of seconds, then reverts to whatever the bar was showing. It is the
// general channel for passive notifications: the newest one shows
// immediately and restarts the timer, so a rapid series collapses to the
// latest message plus one countdown. The message is also routed through the
// accessibility announce system, so when the user has enabled speech it is
// read aloud.
func (d *Desktop) NotifyPassive(message string) {
	sb := d.StatusBar()
	if sb == nil {
		return
	}

	d.notifyMu.Lock()
	if !d.notifyActive {
		// First notice of a run: remember the normal content to restore.
		// Copy the slice so the upcoming SetText (which mutates section 0
		// in place) doesn't disturb the saved baseline.
		d.notifyBase = append([]StatusSection(nil), sb.Sections()...)
		d.notifyActive = true
	}
	d.notifyGen++
	gen := d.notifyGen
	if d.notifyTimer != nil {
		d.StopTimer(d.notifyTimer)
		d.notifyTimer = nil
	}
	d.notifyMu.Unlock()

	// Speak it (when speech is enabled) via the accessibility channel. Done
	// before SetText so any visual-announcement echo can't clobber the clean
	// notification text we set next.
	if am := d.AccessibilityManager(); am != nil {
		am.AnnounceAssertive(message)
	}

	sb.SetText(message)

	timer := d.StartTimer(passiveNotificationDuration, func() {
		d.clearPassiveNotification(gen)
	})
	d.notifyMu.Lock()
	d.notifyTimer = timer
	d.notifyMu.Unlock()
}

// clearPassiveNotification restores the status bar's normal content once a
// passive notice expires, unless a newer notice (a higher generation) has
// taken over in the meantime.
func (d *Desktop) clearPassiveNotification(gen int) {
	d.notifyMu.Lock()
	if gen != d.notifyGen {
		d.notifyMu.Unlock()
		return
	}
	base := d.notifyBase
	d.notifyActive = false
	d.notifyBase = nil
	d.notifyTimer = nil
	d.notifyMu.Unlock()

	if sb := d.StatusBar(); sb != nil {
		sb.SetSections(base)
	}
}

// ScreenSize returns the current screen size in units.
func (d *Desktop) ScreenSize() core.UnitSize {
	d.mu.RLock()
	backend := d.backend
	d.mu.RUnlock()

	if backend != nil {
		return backend.Size()
	}
	return core.UnitSize{}
}

// Clipboard returns the clipboard contents.
func (d *Desktop) Clipboard() string {
	d.mu.RLock()
	backend := d.backend
	d.mu.RUnlock()

	if backend != nil {
		return backend.GetClipboard()
	}
	return ""
}

// SetClipboard sets the clipboard contents.
func (d *Desktop) SetClipboard(text string) {
	d.mu.RLock()
	backend := d.backend
	d.mu.RUnlock()

	if backend != nil {
		backend.SetClipboard(text)
	}
}

// clipboardGraceDelay is how long an async clipboard read waits for the
// terminal to answer before putting up the "waiting for clipboard" modal. A
// terminal that answers instantly (no permission prompt) resolves within this
// window, so no modal flashes; a terminal that prompts the user does not, so
// the modal appears to explain the wait.
const clipboardGraceDelay = 300 * time.Millisecond

// clipboardWait is one in-progress asynchronous clipboard read.
type clipboardWait struct {
	onResult func(string) // delivered exactly once
	internal string       // fallback (host-internal clipboard) if none arrives
	resolved bool
	timer    *DesktopTimer
	modal    *MessageBox
	closing  bool // the modal's own Cancel is closing it; don't double-close
}

// ReadClipboardAsync resolves the clipboard, calling onResult exactly once on
// the UI thread. When the backend reads synchronously (SDL, or read-back off)
// it resolves immediately with GetClipboard. When the backend reads
// asynchronously (a terminal OSC 52 query that may prompt the user), it fires
// the query, and if no reply lands within clipboardGraceDelay it raises a
// "Waiting for Clipboard" system modal with a Cancel button: a reply closes the
// modal and delivers it; Cancel delivers the internal clipboard instead.
func (d *Desktop) ReadClipboardAsync(onResult func(string)) {
	d.mu.RLock()
	backend := d.backend
	d.mu.RUnlock()

	internal := ""
	if backend != nil {
		internal = backend.GetClipboard()
	}

	reader, ok := backend.(core.AsyncClipboardReader)
	if !ok || d.clipWait != nil || !reader.RequestClipboardRead() {
		// Synchronous / unsupported / already waiting: resolve now.
		onResult(internal)
		return
	}

	w := &clipboardWait{onResult: onResult, internal: internal}
	d.clipWait = w
	w.timer = d.StartTimer(clipboardGraceDelay, func() { d.clipboardGraceElapsed(w) })
}

// clipboardGraceElapsed fires when a still-pending read has waited past the
// grace period: put up the waiting modal (unless already resolved).
func (d *Desktop) clipboardGraceElapsed(w *clipboardWait) {
	if w.resolved || d.clipWait != w {
		return
	}
	w.timer = nil

	mb := NewMessageBox("Waiting for Clipboard",
		"Waiting for the terminal to provide the clipboard.\nCancel to paste the last copied item instead.",
		ButtonCancel)
	mb.SetIcon(IconInformation)
	mb.SetOnFinished(func(DialogResult) {
		// Cancel (or Escape): the modal is closing itself, so resolve with the
		// internal clipboard without closing it again.
		w.closing = true
		d.resolveClipboard(w, w.internal)
	})
	w.modal = mb

	if wm := d.WindowManager(); wm != nil {
		wm.AddWindow(&mb.Window)
		mb.ResizeToFitContent()
		area := wm.ClientArea()
		b := mb.Bounds()
		x := area.X + (area.Width-b.Width)/2
		y := area.Y + (area.Height-b.Height)/2
		if !wm.SmoothPositioning() {
			metrics := d.EffectiveCellMetrics()
			x = metrics.RoundDownToCellX(x)
			y = metrics.RoundDownToCellY(y)
		}
		mb.SetBounds(core.UnitRect{X: x, Y: y, Width: b.Width, Height: b.Height})
	}
}

// onClipboardResponse handles a clipboard reply arriving from the backend
// (already marshalled onto the UI thread). It resolves the pending read; a
// late/stray reply with no pending read is ignored.
func (d *Desktop) onClipboardResponse(text string) {
	if w := d.clipWait; w != nil {
		d.resolveClipboard(w, text)
	}
}

// resolveClipboard completes a clipboard read exactly once: it stops the grace
// timer, dismisses the waiting modal (unless the modal is closing itself), and
// delivers text to the waiter.
func (d *Desktop) resolveClipboard(w *clipboardWait, text string) {
	if w.resolved {
		return
	}
	w.resolved = true
	if w.timer != nil {
		d.StopTimer(w.timer)
		w.timer = nil
	}
	if d.clipWait == w {
		d.clipWait = nil
	}
	if w.modal != nil && !w.closing {
		w.modal.Close()
	}
	w.onResult(text)
	d.RequestUpdate()
}

// Beep produces an audible alert.
func (d *Desktop) Beep() {
	d.mu.RLock()
	backend := d.backend
	d.mu.RUnlock()

	if backend != nil {
		backend.Beep()
	}
}

// Children returns all child trinkets.
func (d *Desktop) Children() []core.Trinket {
	var children []core.Trinket
	if d.menuBarShown() {
		children = append(children, d.menuBar)
	}
	if d.content != nil {
		children = append(children, d.content)
	}
	if d.dockVisible() {
		children = append(children, d.dockRow)
	}
	if d.statusBarShown() {
		children = append(children, d.statusBar)
	}
	return children
}

// AddChild adds a child trinket (sets it as content).
func (d *Desktop) AddChild(child core.Trinket) {
	d.SetContent(child)
}

// RemoveChild removes a child trinket.
func (d *Desktop) RemoveChild(child core.Trinket) {
	if d.content == child {
		d.content = nil
	}
}

// ChildAt returns the child at the given position.
func (d *Desktop) ChildAt(pos core.UnitPoint) core.Trinket {
	metrics := d.EffectiveCellMetrics()
	bounds := d.Bounds()

	// Check menu bar
	if d.menuBarShown() && pos.Y < metrics.CellHeight {
		return d.menuBar
	}

	// Check status bar
	if d.statusBarShown() && pos.Y >= bounds.Height-metrics.CellHeight {
		return d.statusBar
	}

	// Check content
	if d.content != nil {
		clientArea := d.ClientArea()
		if pos.Y >= clientArea.Y && pos.Y < clientArea.Y+clientArea.Height {
			return d.content
		}
	}

	return nil
}

// Layout arranges children within the desktop.
func (d *Desktop) Layout() {
	d.layoutChildren()
}

// LayoutManager returns nil (desktop uses custom layout).
func (d *Desktop) LayoutManager() core.LayoutManager {
	return nil
}

// SetLayoutManager does nothing (desktop uses custom layout).
func (d *Desktop) SetLayoutManager(lm core.LayoutManager) {
	// Desktop uses custom layout, ignores layout manager
}

// SetMenuBar sets the menu bar (displayed at the top of the screen).
// The system menu (ψ) is automatically prepended to the menu bar.
func (d *Desktop) SetMenuBar(menuBar *MenuBar) {
	d.menuBar = menuBar
	if menuBar != nil {
		menuBar.SetParent(d)
		// Prepend system menu if we have one
		if d.systemMenu != nil {
			menuBar.InsertMenu(0, d.systemMenu)
		}
	}
	d.Update()
}

// MenuBar returns the menu bar.
func (d *Desktop) MenuBar() *MenuBar {
	return d.menuBar
}

// ActiveMenuBarContentChanged rebuilds and repaints the desktop menu bar
// when the app whose menus just changed is the active one. An app can
// update its menus at any time - including a freshly built window whose
// `menubar` statement is adopted after the window itself activated (which
// rebuilt the bar while the app still had no menus) - and the change
// appears immediately instead of only after the next focus switch.
func (d *Desktop) ActiveMenuBarContentChanged(who ApplicationProvider) {
	d.mu.RLock()
	active := d.activeApp
	d.mu.RUnlock()
	if who != active {
		return
	}
	d.updateMenuBarContent()
	d.RequestUpdate()
}

// CloseActiveMenu closes any active dropdown menu.
func (d *Desktop) CloseActiveMenu() {
	if d.menuBar != nil && d.menuBar.ActiveMenu() != nil {
		d.menuBar.CloseMenu()
	}
}

// ActiveMenuBounds returns the bounds of the active dropdown menu.
// Returns an empty rect if no menu is open.
func (d *Desktop) ActiveMenuBounds() core.UnitRect {
	if d.menuBar == nil {
		return core.UnitRect{}
	}
	return d.menuBar.ActiveMenuBounds()
}

// IsMenuBarActive returns true if the menu bar should capture keyboard events.
// This is true when a menu is open, or when the menu bar is focused AND
// actively showing accelerators (not just technically holding focus).
func (d *Desktop) IsMenuBarActive() bool {
	if !d.menuBarShown() {
		return false
	}
	// Menu open always captures
	if d.menuBar.ActiveMenu() != nil {
		return true
	}
	// Menu bar focused with accelerators active (F10 pressed, awaiting key)
	return d.menuBar.HasFocus() && d.menuBar.AcceleratorsActive()
}

// DeactivateMenuBar closes any open menu and unfocuses the menu bar.
// Uses CloseMenuWithoutRestore because when a window becomes active,
// we want that window to stay in front, not restore the previous window.
func (d *Desktop) DeactivateMenuBar() {
	if d.menuBar != nil {
		d.menuBar.CloseMenuWithoutRestore()
	}
}

// SetStatusBar sets the status bar (displayed at the bottom).
func (d *Desktop) SetStatusBar(statusBar *StatusBar) {
	d.statusBar = statusBar
	if statusBar != nil {
		statusBar.SetParent(d)
	}
	d.Update()
}

// StatusBar returns the status bar.
func (d *Desktop) StatusBar() *StatusBar {
	return d.statusBar
}

// SetBackgroundChar sets the background pattern character.
func (d *Desktop) SetBackgroundChar(ch rune) {
	d.bgChar = ch
	d.Update()
}

// BackgroundChar returns the background pattern character.
func (d *Desktop) BackgroundChar() rune {
	return d.bgChar
}

// KeyboardBlurChildren returns whether child windows include a virtual "blur"
// focus item that allows keyboard users to exit the window.
func (d *Desktop) KeyboardBlurChildren() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.keyboardBlurChildren
}

// SetKeyboardBlurChildren sets whether child windows include a virtual "blur"
// focus item that allows keyboard users to exit the window and focus the menu bar.
func (d *Desktop) SetKeyboardBlurChildren(enabled bool) {
	d.mu.Lock()
	d.keyboardBlurChildren = enabled
	d.mu.Unlock()
}

// PerformKeyboardBlur implements core.KeyboardBlurChildrenProvider.
// It performs the F10 action (focus the menu bar), same as pressing F10.
func (d *Desktop) PerformKeyboardBlur() {
	// Deactivate the active window first (so it visually shows as inactive)
	if d.windowManager != nil {
		d.windowManager.DeactivateActiveWindow()
	}
	// Then activate the menu bar
	if d.menuBar != nil {
		d.menuBar.HandleKeyPress(core.KeyPressEvent{Key: "F10"})
	}
}

// IsWindowPassive implements core.PassiveWindowProvider.
// A window is passive when:
// 1. It's the remembered previous window while the menu bar has focus, OR
// 2. It's active but contains an MDIPane with an active descendant window
func (d *Desktop) IsWindowPassive(win core.Trinket) bool {
	if d.windowManager == nil {
		return false
	}

	// A detached window lives on its own OS surface: its lit/heavy state is
	// driven by that surface's focus (SetActive/SetQuasiActive), not by the
	// desktop's menu-bar/previous-window bookkeeping - otherwise it would read
	// as the desktop's remembered previous window and always paint passive
	// (single/heavy) even while focused. It DOES still go passive when focus
	// lives in one of its own MDI children, so the torn parent shows thick
	// while the active child shows the double focused border.
	if w, ok := win.(*window.Window); ok && w.IsDetached() {
		return hasActiveDescendantWindow(win)
	}

	activeWin := d.windowManager.ActiveWindow()
	previousWin := d.windowManager.PreviousActiveWindow()

	// Case 1: Menu bar has focus, this is the remembered previous window
	if activeWin == nil && previousWin != nil && previousWin == win {
		return true
	}

	// Case 2: Window is active but contains an MDIPane with an active window
	if activeWin == win {
		return hasActiveDescendantWindow(win)
	}

	return false
}

// SetContent sets the content trinket (shown behind windows).
func (d *Desktop) SetContent(content core.Trinket) {
	d.content = content
	if content != nil {
		content.SetParent(d)
	}
	d.Update()
}

// Content returns the content trinket.
func (d *Desktop) Content() core.Trinket {
	return d.content
}

// SetBounds sets the desktop bounds (typically the full screen).
func (d *Desktop) SetBounds(bounds core.UnitRect) {
	d.TrinketBase.SetBounds(bounds)
	d.layoutChildren()
}

// layoutChildren updates the bounds of menu bar, status bar, dock row, and content.
func (d *Desktop) layoutChildren() {
	bounds := d.Bounds()
	metrics := d.EffectiveCellMetrics()

	// Menu bar at top (skipped when suppressed for a single-app desktop; its
	// row is reclaimed by the content area, see ClientArea/menuBarShown).
	if d.menuBarShown() {
		d.menuBar.SetBounds(core.UnitRect{
			X:      0,
			Y:      0,
			Width:  bounds.Width,
			Height: metrics.CellHeight,
		})
	}

	// Calculate dock row position and size (above status bar)
	dockHeight := core.Unit(0)
	if d.dockVisible() {
		// First set width so RowCount works correctly
		d.dockRow.SetBounds(core.UnitRect{
			X:     0,
			Y:     0,
			Width: bounds.Width,
		})
		dockHeight = d.dockRow.RequiredHeight()
	}

	// Status bar at bottom (skipped when suppressed for a single-app desktop;
	// its row is reclaimed by the content area, see ClientArea/statusBarShown).
	if d.statusBarShown() {
		d.statusBar.SetBounds(core.UnitRect{
			X:      0,
			Y:      bounds.Height - metrics.CellHeight,
			Width:  bounds.Width,
			Height: metrics.CellHeight,
		})
	}

	// Dock row above status bar
	if d.dockVisible() {
		dockY := bounds.Height - metrics.CellHeight - dockHeight
		if !d.statusBarShown() {
			dockY = bounds.Height - dockHeight
		}
		d.dockRow.SetBounds(core.UnitRect{
			X:      0,
			Y:      dockY,
			Width:  bounds.Width,
			Height: dockHeight,
		})
	}

	// Content in the middle
	if d.content != nil {
		clientArea := d.ClientArea()
		d.content.SetBounds(core.UnitRect{
			X:      0,
			Y:      0,
			Width:  clientArea.Width,
			Height: clientArea.Height,
		})
	}
}

// ClientArea returns the area available for windows (excluding menu/status/dock bars).
func (d *Desktop) ClientArea() core.UnitRect {
	bounds := d.Bounds()
	metrics := d.EffectiveCellMetrics()

	top := core.Unit(0)
	bottom := bounds.Height

	if d.menuBarShown() {
		top = metrics.CellHeight
	}
	if d.statusBarShown() {
		bottom -= metrics.CellHeight
	}
	// Account for dock row height (when not empty)
	if d.dockVisible() {
		// Need to calculate height based on current width
		d.dockRow.SetBounds(core.UnitRect{Width: bounds.Width})
		bottom -= d.dockRow.RequiredHeight()
	}

	return core.UnitRect{
		X:      0,
		Y:      top,
		Width:  bounds.Width,
		Height: bottom - top,
	}
}

// MenuBarHeight returns the height of the menu bar area (0 when there is no menu
// bar or it is suppressed for a single-app desktop).
func (d *Desktop) MenuBarHeight() core.Unit {
	if !d.menuBarShown() {
		return 0
	}
	return d.EffectiveCellMetrics().CellHeight
}

// StatusBarHeight returns the height of the status bar area (0 when there is no
// status bar or it is suppressed for a single-app desktop).
func (d *Desktop) StatusBarHeight() core.Unit {
	if !d.statusBarShown() {
		return 0
	}
	return d.EffectiveCellMetrics().CellHeight
}

// StatusBarBounds returns the bounds of the status bar area (empty rect when
// there is no status bar or it is suppressed for a single-app desktop).
func (d *Desktop) StatusBarBounds() core.UnitRect {
	if !d.statusBarShown() {
		return core.UnitRect{}
	}
	bounds := d.Bounds()
	metrics := d.EffectiveCellMetrics()
	return core.UnitRect{
		X:      0,
		Y:      bounds.Height - metrics.CellHeight,
		Width:  bounds.Width,
		Height: metrics.CellHeight,
	}
}

// DockRow returns the dock row trinket.
func (d *Desktop) DockRow() *DockRow {
	return d.dockRow
}

// DockRowHeight returns the height of the dock row area (0 if empty).
func (d *Desktop) DockRowHeight() core.Unit {
	if d.dockRow == nil || d.dockRow.IsEmpty() {
		return 0
	}
	return d.dockRow.RequiredHeight()
}

// DockBounds returns the bounds of the dock row area (empty rect if no dock or empty).
func (d *Desktop) DockBounds() core.UnitRect {
	if d.dockRow == nil || d.dockRow.IsEmpty() {
		return core.UnitRect{}
	}
	bounds := d.Bounds()
	metrics := d.EffectiveCellMetrics()
	dockHeight := d.dockRow.RequiredHeight()
	dockY := bounds.Height - dockHeight
	if d.statusBar != nil {
		dockY = bounds.Height - metrics.CellHeight - dockHeight
	}
	return core.UnitRect{
		X:      0,
		Y:      dockY,
		Width:  bounds.Width,
		Height: dockHeight,
	}
}

// DockEntryCount returns the number of entries in the dock.
func (d *Desktop) DockEntryCount() int {
	if d.dockRow == nil {
		return 0
	}
	return d.dockRow.EntryCount()
}

// IsDockFocused returns true if the dock currently has focus.
func (d *Desktop) IsDockFocused() bool {
	if d.dockRow == nil {
		return false
	}
	return d.dockRow.HasFocus()
}

// FocusDock sets focus to the dock.
func (d *Desktop) FocusDock() {
	if d.dockVisible() {
		d.dockRow.SetFocus()
	}
}

// UnfocusDock removes focus from the dock.
func (d *Desktop) UnfocusDock() {
	if d.dockRow != nil {
		d.dockRow.ClearFocus()
	}
}

// SizeHint returns the preferred size.
func (d *Desktop) SizeHint() core.UnitSize {
	// Desktop fills available space
	return d.Bounds().Size()
}

// Paint renders the desktop.
func (d *Desktop) Paint(p *core.Painter) {
	bounds := d.Bounds()
	scheme := d.GetScheme()
	metrics := d.EffectiveCellMetrics()

	// Draw background pattern. Graphical targets tile the classic
	// 8x8 two-color bitmap wallpaper (chunked to WallpaperChunkPx);
	// cell targets keep the rune fill.
	bgStyle := scheme.GetDesktopFill()
	if !p.FillPattern(core.UnitRect{Width: bounds.Width, Height: bounds.Height},
		d.wallpaperPattern, d.wallpaperChunkPx, bgStyle) {
		for y := core.Unit(0); y < bounds.Height; y += metrics.CellHeight {
			for x := core.Unit(0); x < bounds.Width; x += metrics.CellWidth {
				p.DrawCell(x, y, d.bgChar, bgStyle)
			}
		}
	}

	// Draw content if any
	if d.content != nil {
		clientArea := d.ClientArea()
		contentPainter := p.WithOffset(clientArea.X, clientArea.Y).
			WithClip(core.UnitRect{Width: clientArea.Width, Height: clientArea.Height})
		d.content.Paint(contentPainter)
	}

	// Draw menu bar at top (skipped when suppressed for a single-app desktop)
	if d.menuBarShown() {
		// Set menu bar bounds
		d.menuBar.SetBounds(core.UnitRect{
			X:      0,
			Y:      0,
			Width:  bounds.Width,
			Height: metrics.CellHeight,
		})
		d.menuBar.Paint(p)
	}

	// Draw dock row above status bar (if not empty)
	if d.dockVisible() {
		dockHeight := d.dockRow.RequiredHeight()
		dockY := bounds.Height - metrics.CellHeight - dockHeight
		if d.statusBar == nil {
			dockY = bounds.Height - dockHeight
		}
		d.dockRow.SetBounds(core.UnitRect{
			X:      0,
			Y:      dockY,
			Width:  bounds.Width,
			Height: dockHeight,
		})
		dockPainter := p.WithOffset(0, dockY)
		d.dockRow.Paint(dockPainter)
	}

	// Draw status bar at bottom
	if d.statusBar != nil {
		y := bounds.Height - metrics.CellHeight
		d.statusBar.SetBounds(core.UnitRect{
			X:      0,
			Y:      y,
			Width:  bounds.Width,
			Height: metrics.CellHeight,
		})
		statusPainter := p.WithOffset(0, y)
		d.statusBar.Paint(statusPainter)
	}
}

// HandleKeyPress handles keyboard input.
func (d *Desktop) HandleKeyPress(event core.KeyPressEvent) bool {
	// Check if menu bar wants to handle keys
	if d.menuBar != nil {
		// F10 toggles menu bar focus
		if event.Key == "F10" {
			// Deactivate the active window when invoking menu bar
			if d.windowManager != nil && !d.menuBar.HasFocus() {
				d.windowManager.DeactivateActiveWindow()
			}
			d.menuBar.HandleKeyPress(event)
			return true
		}
		// Alt+letter (M-<letter>) for menu shortcuts
		if strings.HasPrefix(event.Key, "M-") && len(event.Key) == 3 {
			if d.menuBar.HandleKeyPress(event) {
				return true
			}
		}
		// If menu bar is active (menu open, or focused with accelerators showing), forward keys
		if d.menuBar.ActiveMenu() != nil || (d.menuBar.HasFocus() && d.menuBar.AcceleratorsActive()) {
			handled := d.menuBar.HandleKeyPress(event)
			// If menu bar didn't handle Escape and has focus (no menu open),
			// unfocus the menu bar
			if !handled && event.Key == "Escape" && d.menuBar.HasFocus() {
				d.menuBar.CloseMenuAndUnfocus()
				return true
			}
			return handled
		}
	}

	// If dock has focus, forward keys to it
	if d.dockRow != nil && d.dockRow.HasFocus() {
		return d.dockRow.HandleKeyPress(event)
	}

	// Forward to content
	if d.content != nil {
		return d.content.HandleKeyPress(event)
	}

	return false
}

// PaintMenuDropdown paints the active menu dropdown (call after windows for z-order).
func (d *Desktop) PaintMenuDropdown(p *core.Painter) {
	if d.menuBar != nil {
		d.menuBar.PaintDropdown(p)
	}
}

// HandleMousePress handles mouse clicks.
func (d *Desktop) HandleMousePress(event core.MousePressEvent) bool {
	metrics := d.EffectiveCellMetrics()
	bounds := d.Bounds()

	// Helper to cancel drag state on a trinket
	cancelDrag := func(w core.Trinket) {
		if w == nil {
			return
		}
		if handler, ok := w.(interface {
			HandleMouseRelease(core.MouseReleaseEvent) bool
		}); ok {
			handler.HandleMouseRelease(core.MouseReleaseEvent{Button: event.Button})
		}
	}

	// Check menu bar first - either in menu bar area or when menu is open
	if d.menuBar != nil {
		if event.Y < metrics.CellHeight || d.menuBar.ActiveMenu() != nil {
			// Cancel drags on other children
			cancelDrag(d.statusBar)
			cancelDrag(d.dockRow)
			cancelDrag(d.content)
			return d.menuBar.HandleMousePress(event)
		}
	}

	// Check status bar
	if d.statusBar != nil {
		statusY := bounds.Height - metrics.CellHeight
		if event.Y >= statusY {
			// Cancel drags on other children
			cancelDrag(d.menuBar)
			cancelDrag(d.dockRow)
			cancelDrag(d.content)
			localEvent := event
			localEvent.Y -= statusY
			return d.statusBar.HandleMousePress(localEvent)
		}
	}

	// Check dock row (above status bar)
	if d.dockVisible() {
		dockHeight := d.dockRow.RequiredHeight()
		dockY := bounds.Height - metrics.CellHeight - dockHeight
		if d.statusBar == nil {
			dockY = bounds.Height - dockHeight
		}
		if event.Y >= dockY && event.Y < dockY+dockHeight {
			// Cancel drags on other children
			cancelDrag(d.menuBar)
			cancelDrag(d.statusBar)
			cancelDrag(d.content)
			localEvent := event
			localEvent.Y -= dockY
			return d.dockRow.HandleMousePress(localEvent)
		}
	}

	// Check content
	if d.content != nil {
		clientArea := d.ClientArea()
		if event.Y >= clientArea.Y && event.Y < clientArea.Y+clientArea.Height {
			// Cancel drags on other children
			cancelDrag(d.menuBar)
			cancelDrag(d.statusBar)
			cancelDrag(d.dockRow)
			localEvent := event
			localEvent.X -= clientArea.X
			localEvent.Y -= clientArea.Y
			return d.content.HandleMousePress(localEvent)
		}
	}

	// Click was on blank desktop area - cancel all drags
	cancelDrag(d.menuBar)
	cancelDrag(d.statusBar)
	cancelDrag(d.dockRow)
	cancelDrag(d.content)
	return false
}

// HandleMouseMove handles mouse movement.
func (d *Desktop) HandleMouseMove(event core.MouseMoveEvent) bool {
	// Forward to menu bar for drag navigation
	if d.menuBar != nil {
		if d.menuBar.HandleMouseMove(event) {
			return true
		}
	}

	// Forward to the dock for hover highlighting. The dock self-clears when
	// the pointer isn't over an entry, so this can run unconditionally; the
	// Y offset matches the press path.
	if d.dockVisible() {
		bounds := d.Bounds()
		metrics := d.EffectiveCellMetrics()
		dockHeight := d.dockRow.RequiredHeight()
		dockY := bounds.Height - metrics.CellHeight - dockHeight
		if d.statusBar == nil {
			dockY = bounds.Height - dockHeight
		}
		localEvent := event
		localEvent.Y -= dockY
		d.dockRow.HandleMouseMove(localEvent)
	}
	return false
}

// HandleMouseRelease handles mouse release.
func (d *Desktop) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	// Forward to menu bar for drag release
	if d.menuBar != nil {
		if d.menuBar.HandleMouseRelease(event) {
			return true
		}
	}
	return false
}

// StatusBar is a simple status bar trinket.
type StatusBar struct {
	core.TrinketBase

	// Status sections
	sections []StatusSection
}

// StatusTextSpan represents a span of text with optional style override.
type StatusTextSpan struct {
	Text  string
	Style *style.CellStyle // nil = use default status bar style
}

// StatusSection represents a section of the status bar.
type StatusSection struct {
	Text      string           // Plain text (used if Spans is empty)
	Spans     []StatusTextSpan // Styled text spans (takes precedence over Text)
	Width     int              // 0 = auto, -1 = stretch
	Alignment int              // 0 = left, 1 = center, 2 = right
}

// NewStatusBar creates a new status bar.
func NewStatusBar() *StatusBar {
	s := &StatusBar{}
	s.TrinketBase = *core.NewTrinketBase()
	s.Init(s)
	s.SetFocusPolicy(core.NoFocus)
	return s
}

// SetText sets the main status text.
func (s *StatusBar) SetText(text string) {
	if len(s.sections) == 0 {
		s.sections = []StatusSection{{Text: text, Width: -1}}
	} else {
		s.sections[0].Text = text
		s.sections[0].Spans = nil // Clear any styled spans
	}
	s.Update()
}

// SetStyledText sets the main status text with styled spans.
func (s *StatusBar) SetStyledText(spans []StatusTextSpan) {
	if len(s.sections) == 0 {
		s.sections = []StatusSection{{Spans: spans, Width: -1}}
	} else {
		s.sections[0].Spans = spans
		s.sections[0].Text = "" // Clear plain text
	}
	s.Update()
}

// Text returns the main status text.
func (s *StatusBar) Text() string {
	if len(s.sections) == 0 {
		return ""
	}
	return s.sections[0].Text
}

// AddSection adds a section to the status bar.
func (s *StatusBar) AddSection(section StatusSection) {
	s.sections = append(s.sections, section)
	s.Update()
}

// SetSections sets all sections.
func (s *StatusBar) SetSections(sections []StatusSection) {
	s.sections = sections
	s.Update()
}

// Sections returns all sections.
func (s *StatusBar) Sections() []StatusSection {
	return s.sections
}

// SizeHint returns the preferred size.
func (s *StatusBar) SizeHint() core.UnitSize {
	metrics := s.EffectiveCellMetrics()
	return core.UnitSize{
		Width:  0, // Will stretch to fill
		Height: metrics.CellHeight,
	}
}

// Paint renders the status bar.
func (s *StatusBar) Paint(p *core.Painter) {
	bounds := s.Bounds()
	scheme := s.GetScheme()
	metrics := s.EffectiveCellMetrics()
	font := s.EffectiveFont()

	statusBarStyle := scheme.GetStatusBar()

	// Draw background
	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', statusBarStyle)

	// Draw sections. The whole status bar renders in the proportional
	// font: section auto-width comes from proportional text measurement
	// (plus a one-cell margin on each side), and each section's text is
	// clipped to its slot so it can't spill into the next one.
	x := core.Unit(0)
	for _, section := range s.sections {
		// Calculate section width
		var sectionWidth core.Unit
		if section.Width == -1 {
			// Stretch to remaining space
			sectionWidth = bounds.Width - x
		} else if section.Width == 0 {
			// Auto width based on measured content
			var textW core.Unit
			if len(section.Spans) > 0 {
				for _, span := range section.Spans {
					textW += font.MeasureText(span.Text)
				}
			} else {
				textW = font.MeasureText(section.Text)
			}
			sectionWidth = textW + 2*metrics.CellWidth
		} else {
			sectionWidth = core.Unit(section.Width) * metrics.CellWidth
		}

		// Draw text - either from spans or plain text - clipped to the slot.
		textX := x + metrics.CellWidth
		slot := p.WithClip(core.UnitRect{X: x, Y: 0, Width: sectionWidth, Height: bounds.Height})

		if len(section.Spans) > 0 {
			// Draw styled spans, advancing each by the device-pixel width the
			// glyphs painted (drawTextSegments) so a run that switches style
			// mid-word stays glyph-tight at a fractional font size, where
			// re-snapping each span's unit start through the cell rate opens a
			// gap.
			segs := make([]textSegment, 0, len(section.Spans))
			for _, span := range section.Spans {
				spanStyle := statusBarStyle
				if span.Style != nil {
					spanStyle = *span.Style
				}
				segs = append(segs, textSegment{span.Text, spanStyle})
			}
			drawTextSegments(slot, textX, 0, font, segs...)
		} else {
			// Draw plain text
			slot.DrawText(textX, 0, section.Text, statusBarStyle, font)
		}

		x += sectionWidth
	}
}

// HandleMousePress handles mouse clicks.
func (s *StatusBar) HandleMousePress(event core.MousePressEvent) bool {
	// Status bar clicks could be used for section-specific actions
	return true
}

// Verify Desktop implements KeyboardBlurChildrenProvider
var _ core.KeyboardBlurChildrenProvider = (*Desktop)(nil)
