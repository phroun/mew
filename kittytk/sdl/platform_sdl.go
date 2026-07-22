//go:build sdl

package sdl

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	sdl2 "github.com/veandco/go-sdl2/sdl"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/platform"
)

// Platform runs KittyTK over SDL2 windows: each surface is an OS
// window with its own raster backend, SDL presents and feeds input.
// All callbacks on the OS-locked main thread per D21.
type Platform struct {
	title    string
	appName  string // OS application name (macOS menu bar / task switcher); "" = SDL default
	wPx, hPx int
	scale    int              // device zoom: pixels per unit at 12pt; see SetScale
	fontSize int              // UI point size that sets the cell pixel size (0 = 12pt base)
	metrics  core.CellMetrics // root cell denomination for every surface (0 = raster default 8x16)

	mu     sync.Mutex
	posts  []func()
	timers []timerEntry

	quitting atomic.Bool
	exitCode atomic.Int32

	backend *raster.Backend // main window's framebuffer

	main *nativeWin
	wins map[uint32]*nativeWin // by SDL window ID, main included

	// System mouse cursors, created on demand and cached by shape.
	cursors   map[core.CursorShape]*sdl2.Cursor
	curCursor core.CursorShape
	cursorSet bool

	// FPS overlay in the OS title bar (kittytk-sdl [window] fps=true): count
	// presented frames and, once a second, rewrite the main window's title to
	// "<title> - N fps". main-thread only, so no locking.
	showFPS   bool
	fpsFrames int
	fpsSince  time.Time

	// vsync selects whether presents sync to the display refresh. On by
	// default; turning it off uncaps the burn loop (see SetShowFPS) so fps can
	// read the raw render throughput.
	vsync bool
}

// SetShowFPS enables the render frame-rate readout in the main window's OS
// title bar. Off by default. Call before Run.
func (p *Platform) SetShowFPS(on bool) { p.showFPS = on }

// SetVSync selects whether presents sync to the display refresh. On by
// default; call before Run/EnsureBackend. Off lets fps=true read uncapped
// throughput (and removes the refresh-rate cap generally).
func (p *Platform) SetVSync(on bool) { p.vsync = on }

// nativeWin bundles one OS window with its presentation chain.
type nativeWin struct {
	window   *sdl2.Window
	renderer *sdl2.Renderer
	texture  *sdl2.Texture
	backend  *raster.Backend
	surface  *sdlSurface
	id       uint32

	// shapeRadiusPx > 0 shapes the OS window with rounded corners
	// (borderless torn-off windows); reapplied on every resize.
	shapeRadiusPx int

	// transparent marks a window with real per-pixel alpha (macOS):
	// the framebuffer's alpha-0 corners composite through, so no
	// shape mask is needed and the frame's antialiasing survives.
	transparent bool
}

type timerEntry struct {
	due time.Time
	fn  func()
}

// New creates an SDL platform; the main window has the given pixel size.
func New(title string, widthPx, heightPx int) *Platform {
	return &Platform{title: title, wPx: widthPx, hPx: heightPx, scale: 1, vsync: true, wins: map[uint32]*nativeWin{}}
}

// SetAppName sets the OS application name - on macOS the name shown in the
// application (first) menu of the system menu bar and, where the OS uses it, the
// task switcher. Empty leaves SDL's default (the executable/process name). Call
// before Run.
func (p *Platform) SetAppName(name string) {
	p.appName = name
}

// macAboutHandler backs the macOS application-menu "About" item (see
// SetAboutHandler). Package-level because the Cocoa menu-action callback
// (kittytkAboutClicked) reaches it from C with no receiver.
var macAboutHandler func()

// SetAboutHandler wires the native macOS application menu's "About <app>" item to
// fn, replacing the standard Cocoa about panel; fn runs on the main (platform)
// thread when the item is chosen. No-op on other platforms and when fn is nil.
// Call before Run — Run installs it once the menu exists. fn should schedule its
// work via the platform/desktop post queue rather than touch UI state directly,
// since it fires from AppKit's menu-tracking loop.
func (p *Platform) SetAboutHandler(fn func()) {
	macAboutHandler = fn
}

// SetScale sets how many window pixels one abstract unit covers.
// The raster backend renders glyphs at the scaled size (crisp, not
// upsampled) and input coordinates are converted back to units. Call
// before Run/EnsureBackend. Stopgap until DPI-derived scaling lands.
func (p *Platform) SetScale(scale int) {
	if scale < 1 {
		scale = 1
	}
	p.scale = scale
}

// SetCellMetrics sets the root cell denomination applied to EVERY
// surface's backend - the main window (including after a resize, which
// recreates the framebuffer) and every torn-off/secondary window. Call
// before EnsureBackend/Run. A zero value keeps the raster default 8x16.
// font_size does NOT go through here (it scales the cell's pixel size,
// not the denomination); see SetFontSize.
func (p *Platform) SetCellMetrics(m core.CellMetrics) {
	p.metrics = m
}

// SetFontSize sets the UI point size that fixes the cell's pixel size on
// EVERY surface's backend (12pt = the base 8x16-pixel cell at zoom 1). It
// scales pixels-per-unit, so layout is unchanged in units and only the
// pixel size of every cell grows. Call before EnsureBackend/Run.
func (p *Platform) SetFontSize(size int) {
	p.fontSize = size
}

// applyMetrics re-seeds a freshly created backend with the platform's
// denomination and font_size so its geometry matches every other surface.
func (p *Platform) applyMetrics(b *raster.Backend) {
	if p.metrics.CellWidth > 0 && p.metrics.CellHeight > 0 {
		b.SetCellMetrics(p.metrics)
	}
	if p.fontSize > 0 {
		b.SetFontSize(p.fontSize)
	}
}

// Backend returns the main window's raster backend (valid after Run
// starts; used by embedders that must seed desktop metrics before
// RunOn).
func (p *Platform) Backend() *raster.Backend { return p.backend }

// EnsureBackend creates the main framebuffer early (before Run) so
// Desktop.SetBackend can seed metrics from it.
func (p *Platform) EnsureBackend() (*raster.Backend, error) {
	if p.backend == nil {
		b, err := raster.NewScaled(p.wPx, p.hPx, p.scale)
		if err != nil {
			return nil, err
		}
		p.applyMetrics(b)
		p.backend = b
	}
	return p.backend, nil
}

// Run implements platform.Platform.
func (p *Platform) Run(init func(platform.Platform)) int {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// The application name (macOS menu bar / task switcher) must be set before
	// SDL initializes video, when it builds the Cocoa application menu. SDL
	// otherwise falls back to the process name (here "mew-sdl").
	if p.appName != "" {
		_ = sdl2.SetHint("SDL_APP_NAME", p.appName)
	}

	if err := sdl2.Init(sdl2.INIT_VIDEO | sdl2.INIT_EVENTS); err != nil {
		return 1
	}
	defer sdl2.Quit()

	// Deliver the click that activates a background window to the app, so a
	// press on a non-focused SDL window (a torn-off window or the desktop
	// window, brought forward from another of ours or from another macOS
	// app) still hits the control under the pointer instead of only raising
	// the window. Off by default on macOS; read live at event time.
	_ = sdl2.SetHint("SDL_MOUSE_FOCUS_CLICKTHROUGH", "1")

	win, err := p.createWindow(p.title, sdl2.WINDOWPOS_CENTERED, sdl2.WINDOWPOS_CENTERED,
		p.wPx, p.hPx, sdl2.WINDOW_SHOWN|sdl2.WINDOW_RESIZABLE, 0)
	if err != nil {
		return 1
	}
	p.main = win
	p.backend = win.backend
	defer func() {
		for _, w := range p.wins {
			w.destroy()
		}
	}()

	// Retarget the macOS app menu's "About <app>" item now that SDL has built
	// the menu (during Init/window creation), if a handler was set. No-op off
	// macOS and when no handler was set.
	if macAboutHandler != nil {
		installAboutMenuHandler()
	}

	sdl2.StartTextInput()

	// Interactive resize runs a modal loop (macOS): PollEvent stalls
	// until release and SDL stretches the stale texture meanwhile. An
	// event WATCH fires from inside that loop, so the framebuffer can
	// re-lay out and present live at every size change.
	sdl2.AddEventWatchFunc(func(ev sdl2.Event, _ interface{}) bool {
		if e, ok := ev.(*sdl2.WindowEvent); ok && e.Event == sdl2.WINDOWEVENT_SIZE_CHANGED {
			// The main loop is frozen inside that modal loop, so its post-queue
			// drain is stalled too - posted work can't land, and a live terminal
			// (whose feed batches apply through Post) would sit on its last frame
			// until release while the chrome around it reflows. Drain the queue
			// on the main thread first (safe: the watch runs on the same, but
			// blocked, main thread, and feed-applies push no SDL events) so that
			// work lands, then resize. If the drain dirtied the surface at an
			// unchanged size, liveResize won't have presented it - so do.
			p.drainPosts()
			p.liveResize(e.WindowID, int(e.Data1), int(e.Data2))
			if w, ok := p.wins[e.WindowID]; ok && w.surface != nil && w.surface.dirty.Swap(false) {
				p.paintAndPresent(w, true)
			}
		}
		return true
	}, nil)

	if init != nil {
		init(p)
	}

	for !p.quitting.Load() {
		p.drainPosts()
		p.fireDueTimers()
		if p.quitting.Load() {
			break
		}

		delivered := p.pumpEvents()

		for _, w := range p.wins {
			s := w.surface
			if s == nil {
				continue
			}
			dirty := s.dirty.Swap(false)
			// fps=true runs a continuous repaint (burn) loop on the main
			// window so the reading reflects a steady render rate; otherwise
			// only dirty surfaces repaint (on-demand). The burn loop always
			// repaints in full.
			burn := p.showFPS && w == p.main
			if dirty || burn {
				p.paintAndPresent(w, burn)
			}
		}

		if p.showFPS {
			p.updateFPSTitle()
		}

		// The burn loop must not sleep - vsync in the present chain paces it.
		// On-demand mode idles at 5ms when nothing was delivered.
		if !delivered && !p.showFPS {
			sdl2.Delay(5)
		}
	}
	return int(p.exitCode.Load())
}

// createWindow builds one OS window with its presentation chain.
// shapeRadiusPx > 0 creates a shapeable window and rounds its corners.
func (p *Platform) createWindow(title string, x, y int32, wPx, hPx int, flags uint32, shapeRadiusPx int) (*nativeWin, error) {
	w := &nativeWin{shapeRadiusPx: shapeRadiusPx}
	var err error
	if shapeRadiusPx > 0 && !platformPerPixelAlpha {
		// Shaped windows must be born shaped. Position is applied
		// after creation (SDL's shaped-window position args are
		// unreliable). Fall back to a plain window if shaping is
		// unavailable on this video driver.
		w.window, err = sdl2.CreateShapedWindow(title, 0, 0, uint32(wPx), uint32(hPx), flags)
		if err == nil {
			w.window.SetPosition(x, y)
		} else {
			w.shapeRadiusPx = 0
		}
	}
	if w.window == nil {
		w.window, err = sdl2.CreateWindow(title, x, y, int32(wPx), int32(hPx), flags)
	}
	if err != nil {
		return nil, err
	}
	rendererFlags := uint32(sdl2.RENDERER_ACCELERATED)
	if p.vsync {
		rendererFlags |= sdl2.RENDERER_PRESENTVSYNC
	}
	w.renderer, err = sdl2.CreateRenderer(w.window, -1, rendererFlags)
	if err != nil {
		w.renderer, err = sdl2.CreateRenderer(w.window, -1, 0)
		if err != nil {
			w.window.Destroy()
			return nil, err
		}
	}
	if err := p.sizeFramebuffer(w, wPx, hPx); err != nil {
		w.renderer.Destroy()
		w.window.Destroy()
		return nil, err
	}
	// Rounded corners, best mechanism first: per-pixel window alpha
	// (macOS - antialiased, plain borderless window), else the binary
	// shape mask on the shaped window created above.
	if w.shapeRadiusPx > 0 && platformPerPixelAlpha && makeWindowTransparent(w.window) {
		w.transparent = true
		w.shapeRadiusPx = 0
	}
	w.applyShape()
	w.id, _ = w.window.GetID()
	p.wins[w.id] = w
	return w, nil
}

// destroy tears down one window's chain.
func (w *nativeWin) destroy() {
	if w.texture != nil {
		w.texture.Destroy()
		w.texture = nil
	}
	if w.renderer != nil {
		w.renderer.Destroy()
		w.renderer = nil
	}
	if w.window != nil {
		w.window.Destroy()
		w.window = nil
	}
}

// sizeFramebuffer sizes one window's raster backend and streaming
// texture.
func (p *Platform) sizeFramebuffer(w *nativeWin, wPx, hPx int) error {
	b, err := raster.NewScaled(wPx, hPx, p.scale)
	if err != nil {
		return err
	}
	p.applyMetrics(b)
	w.backend = b
	if w == p.main || p.main == nil {
		p.backend = b
		p.wPx, p.hPx = wPx, hPx
	}

	if w.texture != nil {
		w.texture.Destroy()
	}
	// Go's image.RGBA stores bytes R,G,B,A; on little-endian that is
	// SDL's ABGR8888 packed format.
	w.texture, err = w.renderer.CreateTexture(
		sdl2.PIXELFORMAT_ABGR8888, sdl2.TEXTUREACCESS_STREAMING,
		int32(wPx), int32(hPx))
	return err
}

// paintAndPresent runs the handler frame into the window's raster
// backend and blits it.
func (p *Platform) paintAndPresent(w *nativeWin, forceFull bool) {
	s := w.surface
	if s == nil || s.handler == nil || w.texture == nil {
		return
	}
	full, dmg := s.takeDamage()
	if forceFull {
		full = true
	}
	if !full && (dmg.Width <= 0 || dmg.Height <= 0) {
		// Dirty with no bounded region recorded: repaint everything.
		full = true
	}

	w.backend.BeginFrame()
	if full {
		s.handler.Frame(core.NewPainter(w.backend))
	} else {
		// Clip the whole tree to the damaged region: the persistent
		// framebuffer keeps everything outside it, off-clip draws are rejected,
		// and only this rectangle is re-uploaded below.
		s.handler.Frame(core.NewPainter(w.backend).WithClip(dmg))
	}
	w.backend.EndFrame()

	img := w.backend.Image()
	if x0, y0, x1, y1, ok := damageDevicePx(w.backend, full, dmg); ok {
		off := img.PixOffset(x0, y0)
		_ = w.texture.Update(
			&sdl2.Rect{X: int32(x0), Y: int32(y0), W: int32(x1 - x0), H: int32(y1 - y0)},
			unsafe.Pointer(&img.Pix[off]), img.Stride)
	} else {
		_ = w.texture.Update(nil, unsafe.Pointer(&img.Pix[0]), img.Stride)
	}
	if w.transparent {
		// Alpha-0 clear so unpainted pixels (the frame's corner
		// cutouts) stay transparent through the composite.
		_ = w.renderer.SetDrawColor(0, 0, 0, 0)
	}
	_ = w.renderer.Clear()
	_ = w.renderer.Copy(w.texture, nil, nil)
	w.renderer.Present()

	if p.showFPS && w == p.main {
		p.fpsFrames++
	}
}

// damageDevicePx returns the device-pixel sub-rectangle to re-upload for a
// bounded repaint; ok=false means upload the whole texture (full repaint or a
// degenerate region).
func damageDevicePx(b *raster.Backend, full bool, dmg core.UnitRect) (x0, y0, x1, y1 int, ok bool) {
	if full {
		return 0, 0, 0, 0, false
	}
	return b.DevicePxRect(dmg)
}

// updateFPSTitle rewrites the main window's OS title with the measured frame
// rate about once a second. fps=true drives a continuous repaint of the main
// window, so this reads the sustained render rate (vsync-paced - typically the
// monitor refresh) rather than the sporadic on-demand present rate.
func (p *Platform) updateFPSTitle() {
	now := time.Now()
	if p.fpsSince.IsZero() {
		p.fpsSince = now
		return
	}
	elapsed := now.Sub(p.fpsSince)
	if elapsed < time.Second {
		return
	}
	fps := int(float64(p.fpsFrames)/elapsed.Seconds() + 0.5)
	if p.main != nil && p.main.window != nil {
		p.main.window.SetTitle(fmt.Sprintf("%s - %d fps", p.title, fps))
	}
	p.fpsFrames = 0
	p.fpsSince = now
}

// liveResize re-sizes one window's framebuffer, re-lays out its
// handler, and presents immediately. Idempotent: a size the
// framebuffer already has is a no-op, so the event-watch call (live,
// inside the modal resize loop) and the queued WindowEvent don't do
// the work twice.
func (p *Platform) liveResize(id uint32, wPx, hPx int) {
	w, ok := p.wins[id]
	if !ok || wPx <= 0 || hPx <= 0 {
		return
	}
	if img := w.backend.Image(); img != nil &&
		img.Bounds().Dx() == wPx && img.Bounds().Dy() == hPx {
		return
	}
	if err := p.sizeFramebuffer(w, wPx, hPx); err != nil {
		return
	}
	w.applyShape()
	if s := w.surface; s != nil && s.handler != nil {
		s.handler.Resized(w.backend.Size())
		p.paintAndPresent(w, true) // a resize repaints the whole surface
		s.dirty.Store(false)
	}
}

// surfaceFor routes an event's window ID to its surface.
func (p *Platform) surfaceFor(id uint32) *sdlSurface {
	if w, ok := p.wins[id]; ok {
		return w.surface
	}
	return nil
}

// pumpEvents drains SDL's queue into the per-window surface handlers.
func (p *Platform) pumpEvents() bool {
	delivered := false
	for {
		ev := sdl2.PollEvent()
		if ev == nil {
			return delivered
		}
		delivered = true
		switch e := ev.(type) {
		case *sdl2.QuitEvent:
			if s := p.mainSurface(); s != nil && s.handler != nil {
				s.handler.Event(core.QuitEvent{})
			}
		case *sdl2.WindowEvent:
			s := p.surfaceFor(e.WindowID)
			if s == nil || s.handler == nil {
				continue
			}
			switch e.Event {
			case sdl2.WINDOWEVENT_SIZE_CHANGED:
				// The event watch usually handled this live; this is
				// the no-op-if-current backstop.
				p.liveResize(e.WindowID, int(e.Data1), int(e.Data2))
			case sdl2.WINDOWEVENT_FOCUS_GAINED:
				s.handler.Event(core.FocusEvent{Focused: true})
				s.Invalidate(core.UnitRect{})
			case sdl2.WINDOWEVENT_FOCUS_LOST:
				s.handler.Event(core.FocusEvent{Focused: false})
				s.Invalidate(core.UnitRect{})
			case sdl2.WINDOWEVENT_LEAVE:
				// Pointer left the window: clear hover-only affordances.
				s.handler.Event(core.MouseLeaveEvent{})
				s.Invalidate(core.UnitRect{})
			}
		case *sdl2.TextInputEvent:
			s := p.surfaceFor(e.WindowID)
			if s == nil || s.handler == nil {
				continue
			}
			text := e.GetText()
			for _, ch := range text {
				// On macOS the Option key composes text (Option+a -> "å"),
				// so meta shortcuts arrive here as accented characters rather
				// than as KEYDOWN modifier combos. Decode them back to their
				// "M-key" notation - matching the TUI backend - and dispatch
				// as a key event instead of typing the composed character.
				if runtime.GOOS == "darwin" {
					if decoded, ok := decodeMacOSOptionChar(ch); ok {
						mods, name := core.ParseKeyModifiers(decoded)
						t := ""
						if len(name) == 1 && name[0] >= 32 && name[0] < 127 {
							t = name
						}
						s.handler.Event(core.KeyPressEvent{Key: decoded, Modifiers: mods, Text: t})
						continue
					}
				}
				s.handler.Event(core.KeyPressEvent{
					Key:  string(ch),
					Text: string(ch),
				})
			}
		case *sdl2.KeyboardEvent:
			s := p.surfaceFor(e.WindowID)
			if s == nil || s.handler == nil {
				continue
			}
			if e.Type == sdl2.KEYDOWN {
				if key := translateKey(e.Keysym); key != "" {
					mods, name := core.ParseKeyModifiers(key)
					text := ""
					if len(name) == 1 && name[0] >= 32 && name[0] < 127 {
						text = name
					}
					s.handler.Event(core.KeyPressEvent{Key: key, Modifiers: mods, Text: text})
				}
			} else if e.Type == sdl2.KEYUP {
				// Report releases with the modifier state AFTER this key rose
				// (SDL's live keymap), so the desktop can commit a window-cycle
				// run once every modifier is up. Emitted even for a bare
				// modifier key (translateKey == "") so that release is seen.
				s.handler.Event(core.KeyReleaseEvent{
					Key:       translateKey(e.Keysym),
					Modifiers: currentKeyModifiers(),
				})
			}
		case *sdl2.MouseButtonEvent:
			s := p.surfaceFor(e.WindowID)
			if s == nil || s.handler == nil {
				continue
			}
			btn := mapButton(e.Button)
			x, y := p.toUnits(e.X, e.Y)
			mods := currentKeyModifiers()
			if e.Type == sdl2.MOUSEBUTTONDOWN {
				// Capture so a drag keeps reporting past the window
				// edge (coordinates go negative/out of bounds) - the
				// tear-off choreography depends on it.
				_ = sdl2.CaptureMouse(true)
				s.handler.Event(core.MousePressEvent{X: x, Y: y, Button: btn, Modifiers: mods})
			} else {
				_ = sdl2.CaptureMouse(false)
				s.handler.Event(core.MouseReleaseEvent{X: x, Y: y, Button: btn, Modifiers: mods})
			}
		case *sdl2.MouseMotionEvent:
			s := p.surfaceFor(e.WindowID)
			if s == nil || s.handler == nil {
				continue
			}
			var held core.MouseButton
			if e.State&sdl2.ButtonLMask() != 0 {
				held = core.LeftButton
			}
			x, y := p.toUnits(e.X, e.Y)
			s.handler.Event(core.MouseMoveEvent{X: x, Y: y, Buttons: held, Modifiers: currentKeyModifiers()})
		case *sdl2.MouseWheelEvent:
			s := p.surfaceFor(e.WindowID)
			if s == nil || s.handler == nil {
				continue
			}
			mx, my, _ := sdl2.GetMouseState()
			x, y := p.toUnits(mx, my)
			s.handler.Event(core.MouseWheelEvent{
				X: x, Y: y,
				// Toolkit convention: negative DeltaY = scroll up
				// (matches the TUI backend); SDL reports the inverse.
				DeltaX: int(e.X), DeltaY: -int(e.Y),
				PreciseX:  float64(e.PreciseX),
				PreciseY:  -float64(e.PreciseY),
				Modifiers: currentKeyModifiers(),
			})
		}
	}
}

func (p *Platform) mainSurface() *sdlSurface {
	if p.main != nil {
		return p.main.surface
	}
	return nil
}

// rootDenomination is the platform's root cell denomination (units per
// cell), matching what every surface's backend reports: the configured
// override or the default 8x16.
func (p *Platform) rootDenomination() (int, int) {
	w, h := int(p.metrics.CellWidth), int(p.metrics.CellHeight)
	if w < 1 {
		w = 8
	}
	if h < 1 {
		h = 16
	}
	return w, h
}

// cellPx is the exact integer pixel size of one root cell along an axis -
// the same value the raster backend paints with (denomination base scaled
// by font_size, ceil'd so a cell contains its glyph, then the integer
// device zoom). Must match raster.Backend.cellPx or hit-testing drifts.
func (p *Platform) cellPx(denom int) int {
	fs := p.fontSize
	if fs < 1 {
		fs = 12
	}
	n := (denom*fs + 11) / 12 // ceil(denom * fontSize/12)
	if n < 1 {
		n = 1
	}
	return n * p.scale
}

// pxToUnitAxis inverts the backend's cell-snapped forward mapping on one
// axis: whole cells map back from exact cellPx multiples, the sub-cell
// remainder from its rounded fraction. Floors toward negative infinity so
// captured-drag coordinates left/above the window stay strictly negative.
func pxToUnitAxis(px, denom, cellPx int) int {
	if cellPx < 1 {
		cellPx = 1
	}
	cells := px / cellPx
	rem := px - cells*cellPx
	if rem < 0 { // floor division for negative coordinates
		cells--
		rem += cellPx
	}
	sub := (rem*denom + cellPx/2) / cellPx // round(rem * denom/cellPx)
	return cells*denom + sub
}

// toUnits converts window-pixel mouse coordinates to abstract units,
// inverting the backend's font_size-aware, cell-snapped pixel mapping so
// hit-testing lands on the same grid the UI paints on at any font_size.
func (p *Platform) toUnits(x, y int32) (core.Unit, core.Unit) {
	denomW, denomH := p.rootDenomination()
	ux := pxToUnitAxis(int(x), denomW, p.cellPx(denomW))
	uy := pxToUnitAxis(int(y), denomH, p.cellPx(denomH))
	return core.Unit(ux), core.Unit(uy)
}

// currentKeyModifiers translates SDL's live modifier state for mouse
// events (Shift+click bypasses terminal mouse reporting, Shift+wheel
// scrolls horizontally).
func currentKeyModifiers() core.KeyModifiers {
	var mods core.KeyModifiers
	state := sdl2.GetModState()
	if state&sdl2.KMOD_SHIFT != 0 {
		mods |= core.ShiftModifier
	}
	if state&sdl2.KMOD_CTRL != 0 {
		mods |= core.ControlModifier
	}
	if state&sdl2.KMOD_ALT != 0 {
		mods |= core.AltModifier
	}
	if state&sdl2.KMOD_GUI != 0 {
		mods |= core.MetaModifier
	}
	return mods
}

func mapButton(b uint8) core.MouseButton {
	switch b {
	case sdl2.BUTTON_LEFT:
		return core.LeftButton
	case sdl2.BUTTON_MIDDLE:
		return core.MiddleButton
	case sdl2.BUTTON_RIGHT:
		return core.RightButton
	}
	return core.NoButton
}

// specialKeys maps SDL keycodes to D3 key names (spellings match
// core/keybindings.go).
var specialKeys = map[sdl2.Keycode]string{
	sdl2.K_RETURN:    "Enter",
	sdl2.K_KP_ENTER:  "Enter",
	sdl2.K_TAB:       "Tab",
	sdl2.K_ESCAPE:    "Escape",
	sdl2.K_BACKSPACE: "Backspace",
	sdl2.K_DELETE:    "Delete",
	sdl2.K_INSERT:    "Insert",
	sdl2.K_HOME:      "Home",
	sdl2.K_END:       "End",
	sdl2.K_PAGEUP:    "PageUp",
	sdl2.K_PAGEDOWN:  "PageDown",
	sdl2.K_UP:        "Up",
	sdl2.K_DOWN:      "Down",
	sdl2.K_LEFT:      "Left",
	sdl2.K_RIGHT:     "Right",
	sdl2.K_F1:        "F1",
	sdl2.K_F2:        "F2",
	sdl2.K_F3:        "F3",
	sdl2.K_F4:        "F4",
	sdl2.K_F5:        "F5",
	sdl2.K_F6:        "F6",
	sdl2.K_F7:        "F7",
	sdl2.K_F8:        "F8",
	sdl2.K_F9:        "F9",
	sdl2.K_F10:       "F10",
	sdl2.K_F11:       "F11",
	sdl2.K_F12:       "F12",
}

// translateKey produces the D3 key string for a KEYDOWN, or "" when
// the TextInput path owns it (plain printable characters).
func translateKey(sym sdl2.Keysym) string {
	ctrl := sym.Mod&sdl2.KMOD_CTRL != 0
	alt := sym.Mod&sdl2.KMOD_ALT != 0
	shift := sym.Mod&sdl2.KMOD_SHIFT != 0
	gui := sym.Mod&sdl2.KMOD_GUI != 0

	if name, ok := specialKeys[sym.Sym]; ok {
		prefix := ""
		if alt {
			prefix += "M-"
		}
		if ctrl {
			prefix += "C-"
		}
		if shift {
			prefix += "S-"
		}
		if gui {
			prefix += "s-"
		}
		return prefix + name
	}

	// Letters and printable symbols.
	if sym.Sym >= 32 && sym.Sym < 127 {
		ch := rune(sym.Sym)
		isLetter := ch >= 'a' && ch <= 'z'

		// Control-punctuation combinations that produce C0 control
		// bytes on a terminal keep their caret spellings so key
		// strings match the TUI backend (byte 0x1C = "^\\", etc.).
		// SDL keycodes are unshifted, hence the shifted trio for the
		// US-layout ^, _, and @ positions.
		if ctrl {
			name := ""
			switch {
			case ch == '\\':
				name = "^\\"
			case ch == ']':
				name = "^]"
			case ch == '[':
				name = "Escape"
			case ch == ' ':
				name = "^@"
			case shift && ch == '6':
				name = "^^"
			case shift && ch == '-':
				name = "^_"
			case ch == '/':
				name = "^_" // Ctrl+/ collapses onto ^_ (byte 0x1F), the terminal
				// convention (xterm), so Ctrl+/ reaches a terminal app instead of
				// being dropped. Without this the unshifted-punctuation path names
				// it "C-/", which purfecterm's key encoder has no byte for.
			case shift && ch == '2':
				name = "^@"
			}
			if name != "" {
				if alt {
					return "M-" + name
				}
				return name
			}
		}

		switch {
		case ctrl && isLetter && !shift:
			base := "^" + string(ch-'a'+'A')
			if alt {
				return "M-" + base
			}
			return base
		case ctrl:
			prefix := ""
			if alt {
				prefix += "M-"
			}
			prefix += "C-"
			if shift {
				prefix += "S-"
			}
			return prefix + string(ch)
		case alt:
			// On macOS a bare Option+printable composes a character that
			// SDL also delivers via TextInput, where we decode it back to
			// M-key (see the TextInputEvent handler). Defer to that path so
			// the shortcut fires exactly once; elsewhere Alt is a plain Meta
			// modifier and TextInput carries nothing, so emit M-key here.
			if runtime.GOOS == "darwin" {
				return ""
			}
			return "M-" + string(ch)
		case gui:
			// Command-modified printables never arrive via TextInput;
			// "s-" is the toolkit's Meta/Cmd prefix.
			prefix := ""
			if ctrl {
				prefix += "C-"
			}
			if shift {
				prefix += "S-"
			}
			return prefix + "s-" + string(ch)
		default:
			// Plain (possibly shifted) printable: TextInput delivers it.
			return ""
		}
	}
	return ""
}

func (p *Platform) drainPosts() {
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

func (p *Platform) fireDueTimers() {
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

// Post implements platform.Platform.
func (p *Platform) Post(fn func()) {
	p.mu.Lock()
	p.posts = append(p.posts, fn)
	p.mu.Unlock()
}

// PostAfter implements platform.Platform.
func (p *Platform) PostAfter(d time.Duration, fn func()) {
	p.mu.Lock()
	p.timers = append(p.timers, timerEntry{due: time.Now().Add(d), fn: fn})
	p.mu.Unlock()
}

// Quit implements platform.Platform.
func (p *Platform) Quit(code int) {
	p.exitCode.Store(int32(code))
	p.quitting.Store(true)
}

// SupportsMultipleSurfaces implements platform.MultiSurfacePlatform.
func (p *Platform) SupportsMultipleSurfaces() bool { return true }

// GlobalPointerPx implements platform.GlobalPointerPlatform.
func (p *Platform) GlobalPointerPx() (int, int) {
	x, y, _ := sdl2.GetGlobalMouseState()
	return int(x), int(y)
}

// CreateSurface implements platform.Platform: the first surface binds
// the main window; each further call opens another OS window (G4
// granting - torn-off desktop windows, native-mode windows).
func (p *Platform) CreateSurface(opts platform.SurfaceOptions) (platform.Surface, error) {
	if p.main == nil {
		return nil, fmt.Errorf("sdl platform: not running")
	}
	if p.main.surface == nil {
		p.main.surface = &sdlSurface{platform: p, win: p.main}
		if opts.Title != "" {
			p.main.window.SetTitle(opts.Title)
		}
		return p.main.surface, nil
	}

	wPx, hPx := opts.WidthPx, opts.HeightPx
	if wPx <= 0 || hPx <= 0 {
		wPx, hPx = 640, 480
	}
	x, y := int32(opts.XPx), int32(opts.YPx)
	if opts.XPx == 0 && opts.YPx == 0 {
		x, y = sdl2.WINDOWPOS_CENTERED, sdl2.WINDOWPOS_CENTERED
	}
	flags := uint32(sdl2.WINDOW_SHOWN)
	if opts.Borderless {
		flags |= sdl2.WINDOW_BORDERLESS
	} else {
		flags |= sdl2.WINDOW_RESIZABLE
	}
	radius := 0
	if opts.Borderless {
		radius = opts.CornerRadiusPx
	}
	// Never activate extra windows on show: a torn-off window appears
	// under a HELD pointer, and stealing key status from the desktop
	// window kills its live mouse session (the drag dies and SDL's
	// button state wedges). Click-to-focus still works.
	_ = sdl2.SetHint("SDL_WINDOW_NO_ACTIVATION_WHEN_SHOWN", "1")
	w, err := p.createWindow(opts.Title, x, y, wPx, hPx, flags, radius)
	if err != nil {
		return nil, err
	}
	w.surface = &sdlSurface{platform: p, win: w}
	if opts.Borderless {
		// Borderless windows can't miniaturize without help (Cocoa
		// requires the miniaturizable style-mask bit).
		makeWindowMiniaturizable(w.window)
	}
	reassertCapture()
	return w.surface, nil
}

// reassertCapture re-enables mouse capture if a button is still held:
// SDL can silently drop capture when windows are created or destroyed
// mid-gesture, after which it CLAMPS motion coordinates to the window
// rect - the tear-off drag would fence itself in.
func reassertCapture() {
	if _, _, state := sdl2.GetGlobalMouseState(); state&sdl2.ButtonLMask() != 0 {
		_ = sdl2.CaptureMouse(true)
	}
}

// Clipboard implements platform.Platform.
func (p *Platform) Clipboard() string {
	s, _ := sdl2.GetClipboardText()
	return s
}

// SetClipboard implements platform.Platform.
func (p *Platform) SetClipboard(text string) { _ = sdl2.SetClipboardText(text) }

// Beep implements platform.Platform.
func (p *Platform) Beep() {}

// SetCursor implements platform.CursorController: set the application's
// system mouse cursor. System cursors are created on demand and cached;
// redundant sets (same shape) are skipped.
func (p *Platform) SetCursor(shape core.CursorShape) {
	if p.cursorSet && p.curCursor == shape {
		return
	}
	if p.cursors == nil {
		p.cursors = map[core.CursorShape]*sdl2.Cursor{}
	}
	cur, ok := p.cursors[shape]
	if !ok {
		cur = sdl2.CreateSystemCursor(systemCursorID(shape))
		p.cursors[shape] = cur
	}
	if cur == nil {
		return
	}
	sdl2.SetCursor(cur)
	p.curCursor = shape
	p.cursorSet = true
}

// systemCursorID maps a core cursor shape to its SDL system cursor.
func systemCursorID(shape core.CursorShape) sdl2.SystemCursor {
	switch shape {
	case core.CursorText:
		return sdl2.SYSTEM_CURSOR_IBEAM
	case core.CursorResizeH:
		return sdl2.SYSTEM_CURSOR_SIZEWE
	case core.CursorResizeV:
		return sdl2.SYSTEM_CURSOR_SIZENS
	case core.CursorResizeNWSE:
		return sdl2.SYSTEM_CURSOR_SIZENWSE
	case core.CursorResizeNESW:
		return sdl2.SYSTEM_CURSOR_SIZENESW
	default:
		return sdl2.SYSTEM_CURSOR_ARROW
	}
}

// sdlSurface is one SDL window as a platform.Surface.
type sdlSurface struct {
	platform *Platform
	win      *nativeWin
	handler  platform.SurfaceHandler
	dirty    atomic.Bool
	closed   bool

	// Damage accumulated since the last present: a full-surface flag (an empty
	// Invalidate, the default) or the union of bounded regions. A bounded frame
	// repaints (and re-uploads) only that rectangle.
	damageMu   sync.Mutex
	damageFull bool
	damageRect core.UnitRect
}

func (s *sdlSurface) Size() core.UnitSize {
	return s.win.backend.Size()
}
func (s *sdlSurface) Metrics() core.CellMetrics {
	return s.win.backend.Metrics()
}
func (s *sdlSurface) SetHandler(h platform.SurfaceHandler) { s.handler = h }

// Invalidate marks the surface dirty and accumulates damage: an empty rect
// (the common case) means the whole surface; a bounded rect unions into the
// pending region so a partial repaint touches only what changed.
func (s *sdlSurface) Invalidate(r core.UnitRect) {
	s.damageMu.Lock()
	if r.Width <= 0 || r.Height <= 0 {
		s.damageFull = true
	} else if !s.damageFull {
		s.damageRect = unionUnitRect(s.damageRect, r)
	}
	s.damageMu.Unlock()
	s.dirty.Store(true)
}

// takeDamage reads and clears the accumulated damage.
func (s *sdlSurface) takeDamage() (full bool, rect core.UnitRect) {
	s.damageMu.Lock()
	full, rect = s.damageFull, s.damageRect
	s.damageFull = false
	s.damageRect = core.UnitRect{}
	s.damageMu.Unlock()
	return full, rect
}

// unionUnitRect returns the smallest rectangle covering both; a degenerate
// operand is treated as the other.
func unionUnitRect(a, b core.UnitRect) core.UnitRect {
	if a.Width <= 0 || a.Height <= 0 {
		return b
	}
	if b.Width <= 0 || b.Height <= 0 {
		return a
	}
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
func (s *sdlSurface) SetCursorVisible(bool)            {}
func (s *sdlSurface) SetCursorPosition(x, y core.Unit) {}

// ScreenPositionPx implements platform.NativeSurface.
func (s *sdlSurface) ScreenPositionPx() (int, int) {
	if s.closed || s.win.window == nil {
		return 0, 0
	}
	x, y := s.win.window.GetPosition()
	return int(x), int(y)
}

// SetScreenPositionPx implements platform.NativeSurface.
func (s *sdlSurface) SetScreenPositionPx(x, y int) {
	if s.closed || s.win.window == nil {
		return
	}
	s.win.window.SetPosition(int32(x), int32(y))
}

// SetBordered implements platform.BorderToggler: toggle the OS title bar
// at runtime (solo mode strips the primary window's border so the app's
// own chrome is the only title bar).
func (s *sdlSurface) SetBordered(bordered bool) {
	if s.closed || s.win == nil || s.win.window == nil {
		return
	}
	s.win.window.SetBordered(bordered)
}

// ScreenSizePx implements platform.NativeSurface: the OS window's current
// pixel size, straight from SDL (no unit round-trip that would drift at
// fractional pixels-per-unit).
func (s *sdlSurface) ScreenSizePx() (int, int) {
	if s.closed || s.win.window == nil {
		return 0, 0
	}
	w, h := s.win.window.GetSize()
	return int(w), int(h)
}

// SetScreenSizePx implements platform.NativeSurface: the size change
// reports back through the WINDOWEVENT_SIZE_CHANGED path (framebuffer
// recreate, shape reapply, handler.Resized).
func (s *sdlSurface) SetScreenSizePx(w, h int) {
	if s.closed || s.win.window == nil || w <= 0 || h <= 0 {
		return
	}
	s.win.window.SetSize(int32(w), int32(h))
}

// WorkAreaPx implements platform.NativeSurface: the usable bounds of
// the display the window occupies (the macOS option-zoom target).
func (s *sdlSurface) WorkAreaPx() (int, int, int, int) {
	if s.closed || s.win.window == nil {
		return 0, 0, 0, 0
	}
	idx, err := s.win.window.GetDisplayIndex()
	if err != nil {
		idx = 0
	}
	r, err := sdl2.GetDisplayUsableBounds(idx)
	if err != nil {
		return 0, 0, 0, 0
	}
	return int(r.X), int(r.Y), int(r.W), int(r.H)
}

// applyShape rounds the OS window's corners with a binary alpha mask
// so the pixels outside the drawn roundrect frame are not opaque
// black. Best effort: video drivers without shape support just keep
// square corners.
func (w *nativeWin) applyShape() {
	if w.shapeRadiusPx <= 0 || w.window == nil {
		return
	}
	wPx, hPx := w.window.GetSize()
	if wPx <= 0 || hPx <= 0 {
		return
	}
	mask, err := sdl2.CreateRGBSurfaceWithFormat(0, wPx, hPx, 32, uint32(sdl2.PIXELFORMAT_ARGB8888))
	if err != nil {
		return
	}
	defer mask.Free()
	_ = mask.FillRect(nil, 0xffffffff)
	pix := mask.Pixels()
	pitch := int(mask.Pitch)
	r := w.shapeRadiusPx
	if m := int(min32(wPx, hPx)) / 2; r > m {
		r = m
	}
	rf := float64(r)
	clear := func(x, y int) {
		off := y*pitch + x*4
		pix[off], pix[off+1], pix[off+2], pix[off+3] = 0, 0, 0, 0
	}
	for j := 0; j < r; j++ {
		for i := 0; i < r; i++ {
			dx := rf - float64(i) - 0.5
			dy := rf - float64(j) - 0.5
			if dx*dx+dy*dy > rf*rf {
				clear(i, j)
				clear(int(wPx)-1-i, j)
				clear(i, int(hPx)-1-j)
				clear(int(wPx)-1-i, int(hPx)-1-j)
			}
		}
	}
	_ = w.window.SetShape(mask, sdl2.ShapeModeDefault{})
}

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

// Minimize implements platform.NativeSurface.
func (s *sdlSurface) Minimize() {
	if s.closed || s.win.window == nil {
		return
	}
	s.win.window.Minimize()
}

// Restore implements platform.NativeRestorer: un-minimizes (and unhides)
// the OS window so the desktop's "Show All" can bring torn windows back.
func (s *sdlSurface) Restore() {
	if s.closed || s.win.window == nil {
		return
	}
	s.win.window.Restore()
}

// Minimized implements platform.NativeSurface.
func (s *sdlSurface) Minimized() bool {
	if s.closed || s.win.window == nil {
		return true
	}
	flags := s.win.window.GetFlags()
	return flags&sdl2.WINDOW_MINIMIZED != 0 || flags&sdl2.WINDOW_HIDDEN != 0
}

// SetOpacity implements platform.NativeSurface.
func (s *sdlSurface) SetOpacity(opacity float64) {
	if s.closed || s.win.window == nil {
		return
	}
	_ = s.win.window.SetWindowOpacity(float32(opacity))
}

// Raise implements platform.NativeSurface: brings the OS window to the
// front and gives it input focus.
func (s *sdlSurface) Raise() {
	if s.closed || s.win.window == nil {
		return
	}
	s.win.window.Raise()
}

// Close implements platform.NativeSurface: destroys the OS window.
// The main window ignores it (quitting the app is Platform.Quit).
//
// The SDL window/renderer/texture teardown — and the wins map, which the event
// pump reads — are only safe on the platform's main loop thread (macOS requires
// SDL video calls there, and touching wins off the pump's thread is a data
// race). Close is reachable from other goroutines: a torn window closing from an
// app's own session goroutine (mew's commit handler), a timer-driven dialog
// dismissal, and so on. So marshal the whole teardown onto the main loop via
// Post rather than destroying inline. The s.closed guard (and re-checking the
// wins entry) makes it idempotent and safe against a reused window id.
func (s *sdlSurface) Close() {
	if s.win == s.platform.main {
		return // never destroy the loop-owning window here
	}
	p := s.platform
	p.Post(func() {
		if s.closed {
			return
		}
		s.closed = true
		s.handler = nil
		if cur, ok := p.wins[s.win.id]; ok && cur == s.win {
			delete(p.wins, s.win.id)
		}
		s.win.destroy()
		reassertCapture()
	})
}
