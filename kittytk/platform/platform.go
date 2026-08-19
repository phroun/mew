// Package platform defines the inverted execution model (G2/G3, plan
// decision D21): a Platform owns the main loop and calls back into
// KittyTK on its OS-locked main thread; a Surface is one render target
// with damage-driven repaints and its own input stream.
//
// Threading contract (D21): every callback - events, frames, posted
// functions - runs on the platform's main thread. Platform.Post is
// the ONLY door in from other threads (socket readers, workers,
// timers living elsewhere). Handlers must not block; long work
// belongs on goroutines that Post their results back.
package platform

import (
	"image"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/phroun/kittytk/core"
)

// Platform owns the main loop, surface creation, and the services
// that exist per-process rather than per-window.
type Platform interface {
	// Run locks the calling goroutine to its OS thread, starts the
	// loop, and blocks until Quit. init runs on the platform thread
	// before the first event. Returns the exit code passed to Quit.
	Run(init func(Platform)) int

	// Post schedules fn on the platform thread. Safe from any
	// goroutine; the only cross-thread entry point (D21).
	Post(fn func())

	// PostAfter schedules fn on the platform thread no earlier than
	// d from now. Safe from any goroutine.
	PostAfter(d time.Duration, fn func())

	// Quit ends Run with the given exit code. Safe from any
	// goroutine.
	Quit(code int)

	// CreateSurface creates a render target. The TUI platform has
	// exactly one (the terminal); native platforms create windows.
	CreateSurface(opts SurfaceOptions) (Surface, error)

	// Clipboard access.
	Clipboard() string
	SetClipboard(text string)

	// Beep produces an audible alert.
	Beep()
}

// SurfaceOptions parameterizes surface creation. The TUI platform
// ignores everything but Title; multi-surface platforms honor the
// pixel geometry (zero size = platform default) and the Borderless
// flag (no OS chrome - the window draws its own, as torn-off desktop
// windows do).
type SurfaceOptions struct {
	Title             string
	XPx, YPx          int
	WidthPx, HeightPx int
	Borderless        bool
	// CornerRadiusPx rounds a borderless surface's corners by shaping
	// the OS window (platforms that can't shape ignore it). Torn-off
	// desktop windows pass their frame radius so the corners outside
	// the drawn roundrect aren't opaque.
	CornerRadiusPx int
}

// MultiSurfacePlatform is an optional Platform capability: more than
// one surface may exist at a time (each an OS window). Hosts gate
// tear-off choreography on it.
type MultiSurfacePlatform interface {
	SupportsMultipleSurfaces() bool
}

// GlobalPointerPlatform is an optional Platform capability: the
// pointer position in screen pixels, for drag choreography that
// crosses surfaces.
type GlobalPointerPlatform interface {
	GlobalPointerPx() (x, y int)
}

// NativeSurface is an optional Surface capability on platforms whose
// surfaces are OS windows: screen-pixel geometry and lifetime.
type NativeSurface interface {
	// ScreenPositionPx returns the surface origin in screen pixels.
	ScreenPositionPx() (x, y int)
	// SetScreenPositionPx moves the surface.
	SetScreenPositionPx(x, y int)
	// ScreenSizePx returns the OS window's current size in screen pixels.
	// This is the authoritative pixel size; deriving it from the surface's
	// unit size and back would drift at fractional pixels-per-unit (the
	// unit size snaps to whole cells).
	ScreenSizePx() (w, h int)
	// SetScreenSizePx resizes the surface's OS window; the size
	// change reports back through SurfaceHandler.Resized.
	SetScreenSizePx(w, h int)
	// WorkAreaPx returns the usable bounds (menu bar and taskbar
	// excluded) of the display the surface currently occupies.
	WorkAreaPx() (x, y, w, h int)
	// Minimize miniaturizes the OS window (macOS: to the Dock, like
	// any document window; the platform makes borderless windows
	// miniaturizable at creation). Restore is the OS's affair - the
	// surface keeps painting when it comes back.
	Minimize()
	// Minimized reports whether the OS window is minimized or hidden
	// (its screen rectangle is a phantom - re-dock hit tests must
	// ignore it).
	Minimized() bool
	// SetOpacity sets whole-window opacity (0 invisible, 1 opaque).
	// Re-dock ghosting turns the old window invisible while its live
	// mouse session finishes, instead of destroying it mid-gesture.
	SetOpacity(opacity float64)
	// Raise brings the surface to the front of the window stack and
	// gives it input focus. Used when tearing a window off so it lands
	// on top of any child surfaces that detach with it.
	Raise()
	// Close destroys the surface and its OS window.
	Close()
}

// NativeZoomReporter is an optional NativeSurface capability: whether the
// OS window is currently maximized or fullscreen. Edge-resizing a filled
// window makes no sense — the OS holds its geometry — so grab zones that
// consult this stand down while it reports true.
type NativeZoomReporter interface {
	NativeZoomed() bool
}

// CursorController is an optional Platform capability: set the system
// mouse cursor shape for the application. Platforms that don't implement
// it keep the default arrow.
type CursorController interface {
	SetCursor(shape core.CursorShape)
}

// BorderToggler is an optional NativeSurface capability: toggle the OS
// window's title bar / border at runtime. Solo mode removes the border
// from the primary window so the app's own chrome is the only title bar.
type BorderToggler interface {
	SetBordered(bordered bool)
}

// NativeRestorer is an optional NativeSurface capability: programmatically
// un-minimize the OS window (the counterpart to Minimize, which the OS
// otherwise only reverses via the Dock/taskbar). Used by the desktop's
// "Show All" to bring torn-off windows back.
type NativeRestorer interface {
	Restore()
}

// NativeShapeSquarer is an optional NativeSurface capability: force a
// shaped (rounded) window's corners square, the way an OS maximize does.
// The desktop's own Zoom is a plain move+resize the OS does not recognize
// as a maximize, so it squares the corners itself while zoomed and
// re-rounds them on restore.
type NativeShapeSquarer interface {
	SetShapeSquared(squared bool)
}

// NativeMinimumSizer is an optional NativeSurface capability: tell the OS
// the smallest this window may become. Our own resize gestures clamp
// themselves, but a resize we do NOT drive — a native title bar's edges,
// the window manager's own keyboard resize or tiling — answers only to
// the OS, and without this it will happily pull a window down to nothing.
type NativeMinimumSizer interface {
	SetMinimumSizePx(w, h int)
}

// NativeRectSetter is an optional NativeSurface capability: move AND
// resize in one call, as one geometry change.
//
// It exists for un-zooming. Setting the position and the size separately
// is two changes the window manager may animate independently, and where
// the WM holds the window maximized it also has a stored "floating"
// rectangle of its own — which can be stale (the geometry from before a
// mode change, an era-old solo layout) — so a plain Restore animates to
// THAT and only then gets corrected by our writes, visibly landing wrong
// and snapping. An implementation must prime its restore target with the
// destination rectangle BEFORE releasing the maximized state, so the one
// animation the user sees ends where the window actually belongs.
type NativeRectSetter interface {
	SetScreenRectPx(x, y, w, h int)
}

// Surface is one render target: per-surface size, damage, input.
type Surface interface {
	// Size returns the surface size in units.
	Size() core.UnitSize

	// Metrics returns the surface's native cell metrics.
	Metrics() core.CellMetrics

	// SetHandler installs the callback receiver. Must be called
	// before events can be delivered.
	SetHandler(h SurfaceHandler)

	// Invalidate requests a repaint of rect (zero rect = the whole
	// surface). Safe from any goroutine; the frame callback runs
	// later on the platform thread. Damage coalesces.
	Invalidate(rect core.UnitRect)

	// Cursor control (text caret).
	SetCursorVisible(visible bool)
	SetCursorPosition(x, y core.Unit)
	// SetCursorStyle selects the caret's DECSCUSR shape (0 the platform's
	// own default, 1/2 blinking/steady block, 3/4 underline, 5/6 bar).
	SetCursorStyle(style int)
}

// TextInputAreaSetter is an optional Surface capability: report where
// text is being typed, so an input method can place its candidate window
// there. Distinct from the caret methods, which are about a caret the
// PLATFORM draws — a graphical surface draws none (trinkets paint their
// own) yet still has an input method to inform.
//
// visible false forgets the position, so the OS falls back to its own
// placement rather than anchoring on a stale one.
type TextInputAreaSetter interface {
	SetTextInputArea(x, y core.Unit, visible bool)
}

// TextInputEnabler is an optional Surface capability: turn text input on and
// off. Distinct from the area, which says only WHERE — an input method whose
// area is forgotten still opens, just at whatever corner the platform picks.
//
// Off is what stops it opening at all, and that is what belongs on a trinket
// that does not type: a button holding focus has nothing for an accent palette
// or a candidate list to compose for.
//
// Key events are unaffected. A press is named from the key, so a surface with
// text input off still receives every keystroke under its own name.
type TextInputEnabler interface {
	SetTextInputEnabled(on bool)
}

// TextInputFrame is what a finished frame knows about where text is going.
//
// Two sources, deliberately kept apart. WHERE comes from paint, because that is
// the only place it is known: the insertion point sits at an accumulated pixel
// advance through a line of glyphs, under this font at this zoom, and nothing
// outside the paint pass can reproduce it. WHETHER comes from focus, because
// paint cannot answer it — a frame that drew no caret may only have been
// clipped to a damaged region, or caught the cursor in the dark half of a
// blink.
//
// Conflating them is what made a blinking cursor look like text input going
// away and coming back twice a second.
type TextInputFrame struct {
	// Caret is what the frame reported, if anything: position, and whether a
	// platform-drawn caret was asked for.
	Caret core.TextCaret

	// Sink is what the frame's owner could determine about focus. Unknown is
	// a real answer and means "do not act".
	Sink core.TextSinkState

	// Complete is whether the frame painted the whole surface, and so whether
	// its silence is evidence. See core.Painter.Complete.
	Complete bool
}

// ApplyTextCaret pushes a finished frame's text-input state to a surface.
//
// Every surface handler that finishes a frame calls this — the native
// one-window-per-surface host and the desktop compositing many windows into one
// surface alike — so the caret behaves the same in both modes.
//
// The rule for withdrawing is the point of the whole type. An insertion point
// is taken back only on evidence: focus is on something that does not type, or
// a COMPLETE frame painted and none of it reported one (the caret is scrolled
// out of view, or hidden). A partial frame that reported nothing has said
// nothing, and what stands, stands.
func ApplyTextCaret(s Surface, f TextInputFrame) {
	if s == nil {
		return
	}
	// Text input itself follows focus, and only focus: whether text is going
	// anywhere is not something a frame can answer, and Unknown leaves it as
	// it was rather than guessing. This is what keeps an input method from
	// opening over a trinket that does not type.
	if enabler, ok := s.(TextInputEnabler); ok {
		switch f.Sink {
		case core.TextSinkPresent:
			enabler.SetTextInputEnabled(true)
		case core.TextSinkAbsent:
			enabler.SetTextInputEnabled(false)
		}
	}
	// The insertion point goes first and separately: a trinket that
	// paints its own caret reports one without asking for a drawn caret
	// at all, so this must not hang off Visible.
	if setter, ok := s.(TextInputAreaSetter); ok {
		switch {
		case f.Sink == core.TextSinkAbsent:
			setter.SetTextInputArea(0, 0, false)
		case f.Caret.Requested():
			setter.SetTextInputArea(f.Caret.X, f.Caret.Y, true)
		case f.Complete:
			setter.SetTextInputArea(0, 0, false)
		}
	}
	if !f.Caret.Visible {
		// Same evidence, same rule: a partial frame that drew no caret is
		// not a surface being told to stop drawing one.
		if f.Complete || f.Sink == core.TextSinkAbsent {
			s.SetCursorVisible(false)
		}
		return
	}
	s.SetCursorStyle(f.Caret.Style)
	s.SetCursorPosition(f.Caret.X, f.Caret.Y)
	s.SetCursorVisible(true)
}

// TextSinkReporter is an optional SurfaceHandler capability: answer whether the
// trinket holding focus types. The platform asks a handler this when it is the
// one finishing the frame (the compositing path), since focus lives in the
// handler's tree and the platform cannot see it.
type TextSinkReporter interface {
	FocusedTextSink() core.TextSinkState
}

// SurfaceHandler receives a surface's callbacks, always on the
// platform thread.
type SurfaceHandler interface {
	// Frame paints the surface. v1 contract: paint everything; the
	// damage region becomes a parameter when a substrate can use it.
	Frame(p *core.Painter)

	// Event delivers one input event (key, mouse, quit request).
	Event(ev core.Event) bool

	// Resized reports a new surface size (already applied).
	Resized(size core.UnitSize)
}

// ChildWindowList wraps a list of child windows for compositor enumeration.
// Uses a concrete type to avoid []interface{} syntax issues across modules.
type ChildWindowList struct {
	Windows      []interface{}
	Popups       []interface{} // Popup overlays to render on top of windows
	MenuDropdown interface{}   // Active menu dropdown (nil if none)

	// ClientArea is the region window content may occupy, in surface
	// units — the surface minus menu bar, status bar, and dock. The
	// compositor clips window layers to it so desktop chrome overlays
	// windows exactly as the software path's client-area clip does.
	// A zero rect means no clipping.
	ClientArea core.UnitRect

	// BaseRevision changes whenever anything the BASE layer paints
	// changes — wallpaper, menu bar, status bar, dock, desktop content —
	// and holds still while only the windows above it move or redraw. A
	// compositor caching the base layer's texture repaints it only when
	// this moves. HasBaseRevision is false for a provider that does not
	// report one, which makes the base repaint every frame as before.
	BaseRevision    uint64
	HasBaseRevision bool

	// Wallpaper is the desktop's background tile, for a host that can
	// repeat a texture across the surface. Nil means the base layer
	// painted its own background and there is nothing to draw under it.
	Wallpaper *WallpaperLayer
}

// WallpaperLayer is one wallpaper tile, repeated from the surface origin
// across the whole surface, underneath every other layer.
//
// The tile's size is its own business: repeating happens in the GPU's
// sampler, so a 16x16 pattern and a 512x512 photograph cost the same one
// quad. Only the size of the upload differs, and Revision means that
// happens once rather than every frame.
type WallpaperLayer struct {
	Tile *image.RGBA

	// Revision changes whenever Tile's pixels would. A host holding an
	// uploaded copy re-uploads only when it moves. It does NOT cover
	// Layout — that changes where the same pixels land, not what they
	// are, so a host re-lays the tile without re-uploading it.
	Revision uint64

	// Layout is how the tile covers the surface: sized by its mode and
	// scale, anchored by its alignment, repeated along the axes it tiles.
	Layout core.WallpaperLayout
}

// WindowProvider is an optional Surface Handler interface that allows
// the platform to enumerate UI child windows for GPU compositing.
// Desktop implements this to expose its window manager's windows.
type WindowProvider interface {
	// GetChildWindows returns the list of child windows that should be
	// composited together. Returns nil if no child windows exist or
	// compositor should not be used.
	GetChildWindows() *ChildWindowList
}

// RepaintRevisionProvider is an optional SurfaceHandler capability: a
// counter that changes whenever the handler would paint something
// different. A host holding the last frame's pixels — every graphical
// host does, in a texture — repaints only when it moves.
//
// It exists for surfaces that DRAG. Moving an OS window changes nothing
// about what the surface contains, but the move arrives as input, and a
// handler that invalidates after input (most do, as a parity contract
// with the terminal) asks for a full repaint of a picture identical to
// the one already on screen. Reporting a revision lets the host notice.
//
// Only equality between two readings is meaningful, never the value.
// A handler that does not implement this repaints every frame, which is
// what every handler did before it existed.
type RepaintRevisionProvider interface {
	RepaintRevision() uint64
}

// BaseLayerPainter is an optional SurfaceHandler refinement for GPU
// compositing. FrameBase paints only the surface's base layer (desktop
// chrome: background, menu bar, dock, status bar) and leaves child
// windows, menus, and popups to the compositor's separate texture
// layers. Frame keeps its v1 contract — paint EVERYTHING — so a
// non-compositing present (software renderer, or a GPU present outside
// the compositor) always produces a complete picture.
type BaseLayerPainter interface {
	FrameBase(p *core.Painter)
}

// PixelAnchoredOnFontZoom is an optional SurfaceHandler refinement consulted
// by a live host font zoom. A graphical platform re-applies its font size to
// every open window: the main window keeps its PIXEL size (the unit grid
// re-derives, as in a resize) while secondary windows normally keep their
// UNIT size and re-size in pixels. A handler reporting true opts its surface
// into the pixel-anchored treatment — a maximized torn-off window fills its
// display's work area, and re-sizing it to preserve units would pull it away
// from the edges it is snapped to.
type PixelAnchoredOnFontZoom interface {
	KeepPixelSizeOnFontZoom() bool
}

// --- Polling platform: any core.RenderBackend as a one-surface
// Platform. This is the TUI reimplemented under the inverted loop
// (G3); it is also the parity bridge - the backend keeps doing what
// it did, control flow is what changed.

// pollInterval mirrors the historical loop cadence: idle latency for
// posts/timers, and the poll granularity for terminal input.
const pollInterval = 10 * time.Millisecond

// PollingPlatform adapts a polled RenderBackend to the Platform
// contract.
type PollingPlatform struct {
	backend core.RenderBackend

	mu     sync.Mutex
	posts  []func()
	timers []timerEntry

	surface *pollingSurface

	quitting atomic.Bool
	exitCode atomic.Int32
}

type timerEntry struct {
	due time.Time
	fn  func()
}

// NewPolling wraps a RenderBackend in the inverted-loop contract.
func NewPolling(backend core.RenderBackend) *PollingPlatform {
	return &PollingPlatform{backend: backend}
}

// Run implements Platform.
func (p *PollingPlatform) Run(init func(Platform)) int {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := p.backend.Init(); err != nil {
		return 1
	}
	defer p.backend.Shutdown()

	if init != nil {
		init(p)
	}

	for !p.quitting.Load() {
		p.drainPosts()
		p.fireDueTimers()
		if p.quitting.Load() {
			break
		}

		delivered := p.deliverEvents()

		if s := p.surface; s != nil && s.takeDirty() {
			s.paint()
		}

		if !delivered {
			// Idle: wait briefly for input, keeping posts/timers at
			// pollInterval latency.
			time.Sleep(pollInterval)
		}
	}
	return int(p.exitCode.Load())
}

// deliverEvents drains the backend queue into the surface handler.
func (p *PollingPlatform) deliverEvents() bool {
	s := p.surface
	delivered := false
	for {
		ev := p.backend.PollEvent()
		if ev == nil {
			return delivered
		}
		delivered = true
		if s == nil || s.handler == nil {
			continue
		}
		if resize, ok := ev.(core.ResizeEvent); ok {
			s.handler.Resized(core.UnitSize{Width: resize.Width, Height: resize.Height})
			continue
		}
		s.handler.Event(ev)
	}
}

func (p *PollingPlatform) drainPosts() {
	for {
		p.mu.Lock()
		if len(p.posts) == 0 {
			p.mu.Unlock()
			return
		}
		fns := p.posts
		p.posts = nil
		p.mu.Unlock()
		for _, fn := range fns {
			fn()
		}
	}
}

func (p *PollingPlatform) fireDueTimers() {
	now := time.Now()
	p.mu.Lock()
	var due []func()
	var rest []timerEntry
	for _, t := range p.timers {
		if !t.due.After(now) {
			due = append(due, t.fn)
		} else {
			rest = append(rest, t)
		}
	}
	p.timers = rest
	p.mu.Unlock()
	for _, fn := range due {
		fn()
	}
}

// Post implements Platform.
func (p *PollingPlatform) Post(fn func()) {
	p.mu.Lock()
	p.posts = append(p.posts, fn)
	p.mu.Unlock()
}

// PostAfter implements Platform.
func (p *PollingPlatform) PostAfter(d time.Duration, fn func()) {
	p.mu.Lock()
	p.timers = append(p.timers, timerEntry{due: time.Now().Add(d), fn: fn})
	p.mu.Unlock()
}

// Quit implements Platform.
func (p *PollingPlatform) Quit(code int) {
	p.exitCode.Store(int32(code))
	p.quitting.Store(true)
}

// CreateSurface implements Platform: the terminal is the one surface.
func (p *PollingPlatform) CreateSurface(opts SurfaceOptions) (Surface, error) {
	if p.surface != nil {
		return nil, errSecondSurface
	}
	p.surface = &pollingSurface{platform: p}
	return p.surface, nil
}

// Clipboard implements Platform.
func (p *PollingPlatform) Clipboard() string { return p.backend.GetClipboard() }

// SetClipboard implements Platform.
func (p *PollingPlatform) SetClipboard(text string) { p.backend.SetClipboard(text) }

// Beep implements Platform.
func (p *PollingPlatform) Beep() { p.backend.Beep() }

type surfaceError string

func (e surfaceError) Error() string { return string(e) }

const errSecondSurface = surfaceError("polling platform has exactly one surface (the terminal)")

// pollingSurface is the terminal as a Surface.
type pollingSurface struct {
	platform *PollingPlatform
	handler  SurfaceHandler
	dirty    atomic.Bool
}

func (s *pollingSurface) Size() core.UnitSize         { return s.platform.backend.Size() }
func (s *pollingSurface) Metrics() core.CellMetrics   { return s.platform.backend.Metrics() }
func (s *pollingSurface) SetHandler(h SurfaceHandler) { s.handler = h }
func (s *pollingSurface) SetCursorVisible(v bool)     { s.platform.backend.SetCursorVisible(v) }
func (s *pollingSurface) SetCursorStyle(style int)    { s.platform.backend.SetCursorStyle(style) }
func (s *pollingSurface) SetCursorPosition(x, y core.Unit) {
	s.platform.backend.SetCursorPosition(x, y)
}

// Invalidate implements Surface: damage coalesces into "repaint on
// the next loop pass" (v1 full-frame contract).
func (s *pollingSurface) Invalidate(core.UnitRect) { s.dirty.Store(true) }

func (s *pollingSurface) takeDirty() bool { return s.dirty.Swap(false) }

// paint runs one frame on the platform thread.
func (s *pollingSurface) paint() {
	if s.handler == nil {
		return
	}
	b := s.platform.backend
	b.BeginFrame()
	s.handler.Frame(core.NewPainter(b))
	b.EndFrame()
}
