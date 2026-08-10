//go:build sdl

package sdl

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	gputypes "github.com/gogpu/gputypes"
	wgpu "github.com/gogpu/wgpu" // Native, zero-cgo WebGPU dependency
	_ "github.com/gogpu/wgpu/hal/allbackends"
	sdl3 "github.com/phroun/kittytk/sdl/sdl3"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/platform"
)

// Platform runs KittyTK over SDL3 windows with pluggable renderer backend.
// All callbacks execute on the OS-locked main thread.
type Platform struct {
	title    string
	appName  string // OS application name (macOS menu bar / task switcher); "" = SDL default
	wPx, hPx int
	scale    int              // device zoom: pixels per unit at 12pt; see SetScale
	fontSize int              // UI point size that sets the cell pixel size (0 = 12pt base)
	metrics  core.CellMetrics // root cell denomination for every surface (0 = raster default 8x16)

	// defaultFontSize is the CONFIGURED font size (the SetFontSize value)
	defaultFontSize int

	mu     sync.Mutex
	posts  []func()
	timers []timerEntry

	quitting atomic.Bool
	exitCode atomic.Int32

	backend *raster.Backend // main window's framebuffer
	seed    *raster.Backend

	// Rendering backend (software or WebGPU)
	renderer Renderer

	// WebGPU-specific fields (only used when renderer is WebGPURenderer)
	// These will eventually be moved entirely into WebGPURenderer
	gpuInstance                 *wgpu.Instance
	gpuAdapter                  *wgpu.Adapter
	gpuDevice                   *wgpu.Device
	gpuQueue                    *wgpu.Queue
	blitPipeline                *wgpu.RenderPipeline
	blitSampler                 *wgpu.Sampler
	blitLayout                  *wgpu.BindGroupLayout
	blitUniformBuffer           *wgpu.Buffer
	blitUniformLayout           *wgpu.BindGroupLayout
	blitUniformBindGroup        *wgpu.BindGroup
	cubePipeline                *wgpu.RenderPipeline
	cubeVertexBuffer            *wgpu.Buffer
	cubeIndexBuffer             *wgpu.Buffer
	cubeUniformBuffer           *wgpu.Buffer
	cubeUniformLayout           *wgpu.BindGroupLayout
	cubeUniformBindGroup        *wgpu.BindGroup
	cubeLayout                  *wgpu.BindGroupLayout
	rotationStartTime           time.Time
	rotationEnabled             atomic.Bool
	rotationActivationTime      time.Time
	rotationDeactivationTime    time.Time
	rotationAngleAtDeactivation float64
	// rotationGate, when set, must return true for the R-key rotation easter
	// egg to fire. The desktop wires it to "the About box is focused", so the
	// effect is reachable only from that dialog and R stays an ordinary key
	// everywhere else. Nil means never (no host opted in).
	rotationGate func() bool

	main *nativeWin
	wins map[uint32]*nativeWin // by SDL window ID, main included

	// System mouse cursors, created on demand and cached by shape.
	cursors   map[core.CursorShape]*sdl3.Cursor
	cursorSet bool

	// FPS overlay in the OS title bar
	showFPS   bool
	fpsFrames int
	fpsSince  time.Time

	// vsync selects whether presents sync to the display refresh.
	vsync bool
}

// SetShowFPS enables the render frame-rate readout in the main window's OS title bar.
func (p *Platform) SetShowFPS(on bool) { p.showFPS = on }

// SetVSync selects whether presents sync to the display refresh.
func (p *Platform) SetVSync(on bool) { p.vsync = on }

// nativeWin bundles one OS window with its GoGPU hardware presentation chain.
type nativeWin struct {
	window *sdl3.Window

	// WebGPU rendering (when using webgpu renderer)
	gpuSurface   *wgpu.Surface
	config       *wgpu.SurfaceConfiguration
	uiTexture    *wgpu.Texture     // VRAM container matching your framebuffer dimensions
	depthTexture *wgpu.Texture     // Depth buffer for 3D rendering
	depthView    *wgpu.TextureView // View for depth texture

	// Software rendering (when using software renderer)
	renderer *sdl3.Renderer
	texture  *sdl3.Texture

	// Common fields
	backend  *raster.Backend
	uiBuffer *wgpu.Buffer // Staging buffer (WebGPU only, unused now)
	surface  *sdlSurface
	id       uint32

	// shapeRadiusPx > 0 shapes the OS window with rounded corners
	shapeRadiusPx int

	// wantRadiusPx is the corner radius this window should have when it is
	// floating (borderless and not maximized), in device pixels at the
	// current font size. It is the source of truth that survives maximize
	// (which squares the corners) and font zoom (which rescales the radius);
	// applyWindowShape reads it to (re)apply the shape. 0 means the window is
	// never rounded (a plain bordered window).
	wantRadiusPx int

	// appliedShapePx is the radius applyWindowShape last applied (-1 before its
	// first call), so an ordinary resize — which changes neither the radius nor
	// the maximized/borderless state — doesn't needlessly re-round the window
	// and thrash the layer. Zoom (radius changes) and maximize/restore (radius
	// flips to/from 0) still re-apply.
	appliedShapePx int

	// transparent marks a window with real per-pixel alpha (macOS)
	transparent bool

	// layerRadiusPx > 0 asks Core Animation to keep this window's layer
	// non-opaque (and rounds it, where that has any effect);
	// re-applied per present because surface configuration can reset
	// layer state.
	layerRadiusPx int

	// cornerRadiusPx > 0 rounds the window by clearing the corner
	// pixels of every painted frame — the mechanism that actually
	// shapes the window, independent of renderer and platform.
	cornerRadiusPx int

	// painted tracks what the backend's pixels currently show, so a
	// frame can tell "nothing about this surface changed" from "repaint
	// it". pixelsDirty says those pixels have not reached the GPU yet.
	//
	// This is what makes dragging a torn-off window cheap. The move
	// arrives as input, the handler invalidates after every input event,
	// and without this the whole window repainted and re-uploaded per
	// mouse move to produce the picture already on screen.
	// frameCaret is the platform-caret request the COMPOSITOR gathered
	// this frame. Child windows and overlays paint into textures of
	// their own, so their requests never reach the painter the base
	// layer applies; the compositor collects them and the platform
	// applies the winner to the surface. frameCaretSet distinguishes
	// "no layer asked for it" (hide) from "the compositor did not run".
	frameCaret    core.TextCaret
	frameCaretSet bool

	// baseCaret is the caret request from the BASE layer's own paint —
	// desktop chrome, or a torn window's frame. The compositor seeds the
	// frame's caret from it so chrome can hold the caret when no layer
	// above asks for it.
	baseCaret core.TextCaret

	painted        paintSignature
	paintedAt      time.Time
	paintedBackend *raster.Backend
	pixelsDirty    bool
	paintedValid   bool
}

type timerEntry struct {
	due time.Time
	fn  func()
}

// New creates an SDL + WebGPU composite platform.
// New creates an SDL platform with the specified renderer backend.
// rendererType should be "software" or "webgpu".
func New(title string, widthPx, heightPx int, rendererType string) (*Platform, error) {
	// Create renderer
	renderer, err := NewRenderer(rendererType, true) // vsync default true
	if err != nil {
		return nil, fmt.Errorf("failed to create %s renderer: %w", rendererType, err)
	}

	return &Platform{
		title:             title,
		wPx:               widthPx,
		hPx:               heightPx,
		scale:             1,
		vsync:             true,
		renderer:          renderer,
		wins:              map[uint32]*nativeWin{},
		cursors:           map[core.CursorShape]*sdl3.Cursor{},
		rotationStartTime: time.Now(),
	}, nil
}

func (p *Platform) SetAppName(name string) {
	p.appName = name
}

var macAboutHandler func()

func (p *Platform) SetAboutHandler(fn func()) {
	// This retargets the macOS application-menu About item; it just shows the
	// host's About dialog. (It used to also start the rotation easter egg, but
	// that fired on the system menu item rather than the desktop's own About
	// box - the egg is now the R key gated to that box, see the key handler.)
	macAboutHandler = fn
}

// SetRotationTriggerGate sets the predicate the R-key rotation easter egg is
// gated on: R only toggles rotation while this returns true. The desktop wires
// it to "the About box is focused" so the effect can't be triggered from
// ordinary typing. A nil gate (the default) disables the egg entirely.
func (p *Platform) SetRotationTriggerGate(fn func() bool) {
	p.rotationGate = fn
}

func (p *Platform) SetScale(scale int) {
	if scale < 1 {
		scale = 1
	}
	p.scale = scale
}

func (p *Platform) SetCellMetrics(m core.CellMetrics) {
	p.metrics = m
}

func (p *Platform) SetFontSize(size int) {
	if size > 0 {
		size = clampFontPt(size)
	}
	p.fontSize = size
	p.defaultFontSize = size
}

func (p *Platform) applyMetrics(b *raster.Backend) {
	if p.metrics.CellWidth > 0 && p.metrics.CellHeight > 0 {
		b.SetCellMetrics(p.metrics)
	}
	if p.fontSize > 0 {
		b.SetFontSize(p.fontSize)
	}
}

func (p *Platform) Backend() *raster.Backend { return p.backend }

func (p *Platform) EnsureBackend() (*raster.Backend, error) {
	if p.backend == nil {
		b, err := raster.NewScaled(p.wPx, p.hPx, p.scale)
		if err != nil {
			return nil, err
		}
		p.applyMetrics(b)
		p.backend = b
		p.seed = b
	}
	return p.backend, nil
}

// The dynamic size is bounded to 4..100pt.
const (
	minFontPt = 4
	maxFontPt = 100
)

// alphaPresentTest (KITTYTK_ALPHA_TEST=1) is a diagnostic: transparent
// (per-pixel alpha) windows present a bare alpha-0 clear with no
// content. A torn-off window that still shows as a black rectangle
// proves the compositing chain below the renderer (CAMetalLayer / SDL
// content view / NSWindow) is discarding alpha; a window that vanishes
// entirely proves the chain honors alpha and any remaining opacity
// comes from painted content.
var alphaPresentTest = os.Getenv("KITTYTK_ALPHA_TEST") != ""

// roundedCornerMechanism selects how a rounded borderless window gets
// its corners cut, via KITTYTK_WINDOW_SHAPE:
//
//	"layer"    Core Animation clips the Metal layer itself with
//	           cornerRadius + masksToBounds. The corner pixels are
//	           never composited, so this does not depend on the
//	           framebuffer's alpha surviving the swapchain - and CA
//	           antialiases the curve. Requires a Metal layer, so it
//	           applies to the WebGPU renderer only.
//	"shape"    SDL's shaped-window alpha mask. SDL masks the window's
//	           CONTENT VIEW, which is where SDL's OWN renderer draws -
//	           so this is the mechanism for the software renderer. A
//	           Metal-rendered window draws through a separate subview
//	           layer on top of that view, which the mask cannot clip.
//	"perpixel" the Cocoa route alone: a non-opaque NSWindow
//	           compositing the framebuffer's alpha channel, with no
//	           geometric clipping. Kept for experimentation; it has
//	           never produced transparent corners here.
//
// The default follows the renderer: "layer" when a GPU device gives us
// a Metal layer, else "shape". Off macOS there is no per-pixel window
// alpha and no Metal layer, so the shaped window is the only mechanism.
func (p *Platform) roundedCornerMechanism() string {
	switch os.Getenv("KITTYTK_WINDOW_SHAPE") {
	case "perpixel":
		return "perpixel"
	case "shape":
		return "shape"
	case "layer":
		return "layer"
	}
	if platformPerPixelAlpha {
		// macOS: the window/layer alpha arrangement. SDL's shaped-window
		// API is gone in SDL3 (SetShape reports NONSHAPEABLE), so it is
		// not a fallback here for either renderer.
		return "layer"
	}
	return "shape"
}

// clampFontPt bounds a point size to the dynamic zoom range.
func clampFontPt(size int) int {
	if size < minFontPt {
		return minFontPt
	}
	if size > maxFontPt {
		return maxFontPt
	}
	return size
}

// Run implements platform.Platform.
func (p *Platform) Run(init func(platform.Platform)) int {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if p.appName != "" {
		_ = sdl3.SetHint("SDL_APP_NAME", p.appName)
	}

	if err := sdl3.Init(sdl3.INIT_VIDEO | sdl3.INIT_EVENTS); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: SDL init failed: %v\n", err)
		return 1
	}
	defer sdl3.Quit()

	_ = sdl3.SetHint("SDL_MOUSE_FOCUS_CLICKTHROUGH", "1")

	// Initialize the renderer (WebGPU setup or software renderer setup)
	if err := p.renderer.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to initialize renderer: %v\n", err)
		return 1
	}
	defer p.renderer.Shutdown()

	// For WebGPU renderer, expose GPU objects to Platform temporarily
	// TODO: Remove this once full extraction is complete
	p.exposeWebGPUObjects()

	// 2. Create Master System UI Window Viewport Surface. It is created
	// transparent-capable and RESIZABLE with a border; solo mode strips the
	// border at runtime (SetBordered), and that is when it actually gets
	// shaped and shadowed — a bordered window cannot be shaped, and its OS
	// title bar owns the corners. The radius here only marks it shapeable and
	// requests the transparent surface (which must be asked for at creation).
	win, err := p.createWindow(p.title, sdl3.WINDOWPOS_CENTERED, sdl3.WINDOWPOS_CENTERED,
		p.wPx, p.hPx, sdl3.WINDOW_SHOWN|sdl3.WINDOW_RESIZABLE, p.shapeRadiusPx())
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to create window: %v\n", err)
		return 1
	}
	p.main = win
	p.backend = win.backend
	defer func() {
		for _, w := range p.wins {
			w.destroy()
		}
	}()

	if macAboutHandler != nil {
		installAboutMenuHandler()
	}

	// (Text input is started per window in createWindow — SDL3 scopes it
	// to a window rather than the process.)

	// Event watch hooks handle continuous redraw requests during active modal resize loops
	sdl3.AddEventWatchFunc(func(ev sdl3.Event, _ interface{}) bool {
		if e, ok := ev.(*sdl3.WindowEvent); ok && e.Event == sdl3.WindowResized {
			p.drainPosts()
			if !p.liveResize(e.WindowID, int(e.Data1), int(e.Data2)) {
				// Same-size event: liveResize didn't present, so keep the
				// modal loop fed with a fresh frame ourselves.
				if w, ok := p.wins[e.WindowID]; ok && w.window != nil {
					p.presentWindow(w, true)
				}
			}
		}
		return true
	}, nil)

	if init != nil {
		init(p)
	}

	// Main Display Server Loop Execution Pipeline
	for !p.quitting.Load() {
		p.drainPosts()
		p.fireDueTimers()
		if p.quitting.Load() {
			break
		}

		delivered := p.pumpEvents()
		p.reassertCursor()

		// Check if any windows need rendering
		anyDirty := false
		for _, w := range p.wins {
			s := w.surface
			if s == nil {
				continue
			}
			if s.dirty.Load() || (p.showFPS && w == p.main) {
				anyDirty = true
				break
			}
		}

		if anyDirty {
			for _, w := range p.wins {
				s := w.surface
				if s == nil {
					continue
				}
				dirty := s.dirty.Swap(false)
				burn := p.showFPS && w == p.main
				if dirty || burn {
					p.presentWindow(w, burn)
				}
			}
		}

		if p.showFPS {
			p.updateFPSTitle()
		}

		// Always add a small delay to prevent event loop starvation
		// Even when continuously rendering (rotation effects), we need to process events
		if !delivered {
			sdl3.Delay(5)
		} else {
			// Yield briefly even when events are flowing to prevent UI freeze
			sdl3.Delay(1)
		}
	}
	return int(p.exitCode.Load())
}

// createWindow builds one OS window with its presentation chain.
func (p *Platform) createWindow(title string, x, y int32, wPx, hPx int, flags sdl3.WindowFlags, shapeRadiusPx int) (*nativeWin, error) {
	// wantRadiusPx is the radius this window keeps as its source of truth; the
	// live shapeRadiusPx tracks what is currently applied (0 while bordered or
	// maximized). A window born borderless is shaped here; the main window is
	// born bordered and gets shaped by SetBordered once solo mode strips it.
	w := &nativeWin{shapeRadiusPx: shapeRadiusPx, wantRadiusPx: shapeRadiusPx, appliedShapePx: -1}
	bornBorderless := flags&sdl3.WINDOW_BORDERLESS != 0
	var err error

	if shapeRadiusPx > 0 && (!platformPerPixelAlpha || p.roundedCornerMechanism() == "shape") {
		// SDL's own shaped window: created shaped, masked in applyShape.
		// SDL3: a window whose framebuffer alpha composites — the real
		// per-pixel-alpha replacement for a masked shaped window, and
		// unlike a shaped window it must be requested at creation.
		w.window, err = sdl3.CreateTransparentWindow(title, x, y, wPx, hPx, flags)
		if err != nil {
			w.shapeRadiusPx = 0
		}
	}

	if w.window == nil {
		winFlags := flags
		if p.gpuDevice != nil && runtime.GOOS == "darwin" {
			winFlags |= sdl3.WINDOW_METAL
		}
		// A rounded window needs its alpha to composite even when it is
		// not created through the transparent path above.
		if shapeRadiusPx > 0 {
			winFlags |= sdl3.WINDOW_TRANSPARENT
		}
		w.window, err = sdl3.CreateWindow(title, x, y, wPx, hPx, winFlags)
	}

	if err != nil {
		return nil, err
	}

	// The WebGPU presentation chain binds directly to the native window.
	// The software renderer presents through SDL textures instead
	// (Renderer.CreateWindowRenderer below) and skips all of this.
	if p.gpuDevice != nil {
		// 1. Native Surface Integration: Map the raw SDL window handle directly to WebGPU
		// macOS hands over the CAMetalLayer; X11/Windows hand over display/window handles.
		displayHandle, windowHandle, err := nativeSurfaceHandles(w.window)
		if err != nil {
			w.window.Destroy()
			return nil, err
		}

		w.gpuSurface, err = p.gpuInstance.CreateSurface(displayHandle, windowHandle)
		if err != nil || w.gpuSurface == nil {
			w.window.Destroy()
			return nil, fmt.Errorf("failed to bind WebGPU hardware surface to window context: %w", err)
		}

		// The standard format supported natively by both macOS Metal and Windows 11
		surfaceFormat := wgpu.TextureFormatBGRA8Unorm

		presentMode := wgpu.PresentModeFifo
		if !p.vsync {
			presentMode = wgpu.PresentModeImmediate
		}

		// A shaped window on a per-pixel-alpha platform (macOS) will be
		// made transparent below; its surface must publish the alpha
		// channel or the rounded corners composite as opaque black.
		alphaMode := gputypes.CompositeAlphaModeOpaque
		if w.shapeRadiusPx > 0 && platformPerPixelAlpha {
			alphaMode = gputypes.CompositeAlphaModePremultiplied
		}

		w.config = &wgpu.SurfaceConfiguration{
			Format:      surfaceFormat,
			Usage:       wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageCopyDst,
			AlphaMode:   alphaMode,
			Width:       uint32(wPx),
			Height:      uint32(hPx),
			PresentMode: presentMode,
		}

		err = w.gpuSurface.Configure(p.gpuDevice, w.config)
		if err != nil {
			w.gpuSurface.Release()
			w.window.Destroy()
			return nil, fmt.Errorf("failed to configure surface: %w", err)
		}

		// 2. Initialize offscreen VRAM texture buffers for software framebuffer blitting
		// Use BGRA format to match the surface format for direct copying
		w.uiTexture, err = p.gpuDevice.CreateTexture(&wgpu.TextureDescriptor{
			Size:          wgpu.Extent3D{Width: uint32(wPx), Height: uint32(hPx), DepthOrArrayLayers: 1},
			MipLevelCount: 1,
			SampleCount:   1,
			Dimension:     wgpu.TextureDimension2D,
			Format:        wgpu.TextureFormatBGRA8Unorm,
			Usage:         wgpu.TextureUsageCopySrc | wgpu.TextureUsageCopyDst | wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding,
		})
		if err != nil {
			w.gpuSurface.Release()
			w.window.Destroy()
			return nil, err
		}

		// Size staging buffers to transfer raw pixel arrays from the raster framework to VRAM
		paddedBytesPerRow := (wPx * 4) // RGBA format pixel scaling
		w.uiBuffer, err = p.gpuDevice.CreateBuffer(&wgpu.BufferDescriptor{
			Size:  uint64(paddedBytesPerRow * hPx),
			Usage: wgpu.BufferUsageCopySrc | wgpu.BufferUsageMapWrite,
		})
	}

	if err := p.sizeFramebuffer(w, wPx, hPx); err != nil {
		if w.uiTexture != nil {
			w.uiTexture.Release()
		}
		if w.gpuSurface != nil {
			w.gpuSurface.Release()
		}
		w.window.Destroy()
		return nil, err
	}

	if shapeRadiusPx > 0 && os.Getenv("KITTYTK_ALPHA_DEBUG") != "" {
		fmt.Fprintf(os.Stderr,
			"kittytk-alpha: rounding request: radius=%dpx mechanism=%q perPixelAlpha=%v shapedWindow=%v\n",
			shapeRadiusPx, p.roundedCornerMechanism(), platformPerPixelAlpha, w.shapeRadiusPx > 0)
	}

	if bornBorderless && w.shapeRadiusPx > 0 && platformPerPixelAlpha {
		switch p.roundedCornerMechanism() {
		case "layer":
			// Core Animation clips the Metal layer itself. The window
			// must still be non-opaque for the clipped-away corners to
			// show what is behind them.
			transparent := makeWindowTransparent(w.window)
			rounded := roundWindowLayer(w.window, w.shapeRadiusPx)
			if os.Getenv("KITTYTK_ALPHA_DEBUG") != "" {
				fmt.Fprintf(os.Stderr,
					"kittytk-alpha: layer rounding: transparent=%v layerFound=%v\n",
					transparent, rounded)
			}
			if transparent {
				w.transparent = true
				w.cornerRadiusPx = w.shapeRadiusPx
				if rounded {
					w.layerRadiusPx = w.shapeRadiusPx
				}
				w.shapeRadiusPx = 0 // the frame's own pixels carry the shape
			}
		case "perpixel":
			// The drawn frame's own alpha cuts the corners, with no
			// Core Animation involvement at all.
			if makeWindowTransparent(w.window) {
				w.transparent = true
				w.cornerRadiusPx = w.shapeRadiusPx
				w.shapeRadiusPx = 0
			}
		}
	}
	w.id, _ = w.window.ID()
	p.wins[w.id] = w

	// Create renderer resources for this window (compositor texture)
	if err := p.renderer.CreateWindowRenderer(w, wPx, hPx); err != nil {
		// Non-fatal for software renderer, but log it
		fmt.Fprintf(os.Stderr, "WARNING: Failed to create window renderer resources: %v\n", err)
	}

	// Shape AFTER the renderer exists, matching the order in the
	// known-good SDL shaped-window sequence (create shaped window,
	// create renderer, then SetShape).
	w.applyShape()

	// A window born borderless and shaped gets its OS drop shadow now (the
	// main window, born bordered, gets its shadow later via SetBordered). The
	// shadow follows the shape and is click-through.
	if bornBorderless && w.wantRadiusPx > 0 {
		setWindowShadow(w.window, true)
	}

	// Text input is PER WINDOW in SDL3, and off until asked for. SDL2's
	// SDL_StartTextInput() was global and on by default, so the port
	// carried a single call for the main window — and every window made
	// afterwards, every torn-off window, silently received no
	// SDL_EVENT_TEXT_INPUT at all. Key events are a separate stream that
	// is always on, which is why Tab and the arrows kept working and
	// only typing was lost.
	if err := sdl3.StartTextInput(w.window); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: text input unavailable for window %d: %v\n", w.id, err)
	}

	return w, nil
}

// destroy tears down one window's hardware presentation chain.
func (w *nativeWin) destroy() {
	// Note: We can't call p.renderer.DestroyWindowRenderer here because we don't have access to Platform
	// The renderer cleanup will happen when Platform shuts down

	if w.texture != nil {
		w.texture.Destroy()
		w.texture = nil
	}
	if w.renderer != nil {
		w.renderer.Destroy()
		w.renderer = nil
	}
	if w.uiTexture != nil {
		w.uiTexture.Release()
		w.uiTexture = nil
	}
	if w.uiBuffer != nil {
		w.uiBuffer.Release()
		w.uiBuffer = nil
	}
	if w.gpuSurface != nil {
		w.gpuSurface.Release()
		w.gpuSurface = nil
	}
	if w.window != nil {
		w.window.Destroy()
		w.window = nil
	}
}

// sizeFramebuffer sizes one window's raster backend and streaming WebGPU textures.
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

	// The fresh backend starts zero-filled, so the surface's damage
	// tracking must be reset: without this the handler repaints only what
	// it thinks changed, and the untouched area presents as black.
	if w.surface != nil {
		w.surface.Invalidate(core.UnitRect{}) // Empty rect = invalidate all
	}

	if w.gpuSurface == nil {
		// Software renderer: re-size the SDL streaming texture instead of
		// the WebGPU chain.
		return p.renderer.ResizeWindowRenderer(w, wPx, hPx)
	}

	// Clean up old WebGPU texture if this is a resize event
	if w.uiTexture != nil {
		w.uiTexture.Release()
	}
	if w.depthTexture != nil {
		w.depthTexture.Release()
	}
	if w.depthView != nil {
		w.depthView.Release()
	}

	// 1. Re-configure the physical Window Surface Swapchain size limits
	w.config.Width = uint32(wPx)
	w.config.Height = uint32(hPx)
	w.gpuSurface.Configure(p.gpuDevice, w.config)

	// 2. Re-allocate the intermediate GPU backing texture layout
	w.uiTexture, err = p.gpuDevice.CreateTexture(&wgpu.TextureDescriptor{
		Size:          wgpu.Extent3D{Width: uint32(wPx), Height: uint32(hPx), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormatBGRA8Unorm,
		Usage:         wgpu.TextureUsageCopySrc | wgpu.TextureUsageCopyDst | wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding,
	})
	if err != nil {
		return err
	}

	// 3. Create depth texture for 3D rendering
	w.depthTexture, err = p.gpuDevice.CreateTexture(&wgpu.TextureDescriptor{
		Size:          wgpu.Extent3D{Width: uint32(wPx), Height: uint32(hPx), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormatDepth24Plus,
		Usage:         wgpu.TextureUsageRenderAttachment,
	})
	if err != nil {
		return err
	}

	w.depthView, err = p.gpuDevice.CreateTextureView(w.depthTexture, nil)
	if err != nil {
		return err
	}

	// Note: We'll create a transient view for each frame since textures don't have permanent views
	// The bind group will be created per-frame with the texture

	return nil
}

// createMVPMatrix creates a model-view-projection matrix for 3D cube rendering
func createMVPMatrix(aspectRatio float32, rotationAngle float32, scale float32, floatY float32) [16]float32 {
	// Simple rotation matrices
	sinY := float32(math.Sin(float64(rotationAngle)))
	cosY := float32(math.Cos(float64(rotationAngle)))
	sinX := float32(math.Sin(float64(rotationAngle * 0.7)))
	cosX := float32(math.Cos(float64(rotationAngle * 0.7)))

	// Use passed-in scale (will be eased from 0 to 1.5)
	translateZ := float32(0.5) // Move it forward so it's in front of clip plane

	return [16]float32{
		// Column 0 (X axis after transform)
		scale * cosY / aspectRatio, scale * sinX * sinY / aspectRatio, scale * cosX * sinY / aspectRatio, 0,
		// Column 1 (Y axis after transform)
		0, scale * cosX, -scale * sinX, 0,
		// Column 2 (Z axis after transform)
		-scale * sinY / aspectRatio, scale * sinX * cosY / aspectRatio, scale * cosX * cosY / aspectRatio, 0,
		// Column 3 (translation with floating Y motion)
		0, floatY, translateZ, 1,
	}
}

// paintBackend runs the handler frame into the window's raster backend,
// honoring the surface's damage region unless forceFull demands a
// complete repaint. baseOnly selects the handler's chrome-only base
// layer (BaseLayerPainter) when the compositor renders child windows,
// menus, and popups on their own layers.
func (p *Platform) paintBackend(w *nativeWin, forceFull, baseOnly bool) {
	s := w.surface
	if s == nil || s.handler == nil || w.backend == nil {
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

	frame := s.handler.Frame
	if baseOnly {
		if base, ok := s.handler.(platform.BaseLayerPainter); ok {
			frame = base.FrameBase
		}
	}

	w.backend.BeginFrame()
	painter := core.NewPainter(w.backend)
	painter.ResetTextCaretRequest()
	if full {
		frame(painter)
	} else {
		// Clip the whole tree to the damaged region
		frame(painter.WithClip(dmg))
	}
	w.backend.EndFrame()

	// Keep the base layer's own caret request. When compositing, the
	// handler must NOT apply it — layers painted above may claim the
	// caret instead, and the platform applies the single winner after
	// the compositor has seen them all.
	if baseOnly {
		w.baseCaret = painter.TextCaretRequest()
	}

	// A rounded window carries its shape in its own pixels: clear the
	// corners so they composite as nothing. Must run after EVERY frame,
	// including damage-clipped ones, since the handler repaints over
	// them whenever the damage reaches a corner.
	if w.cornerRadiusPx > 0 {
		punchRoundedCorners(w.backend.Image(), w.cornerRadiusPx)
	}
}

// presentWindow is the ONE way a window reaches the screen: it paints the
// backend and presents through the active renderer, compositing child
// windows when the renderer and the surface handler both support it.
// Every present path — main loop, live resize, font zoom — funnels here so
// resize frames cannot diverge from steady-state frames.
func (p *Platform) presentWindow(w *nativeWin, forceFull bool) {
	s := w.surface
	if s == nil || s.handler == nil {
		return
	}

	if p.renderer.SupportsFeature(FeatureCompositing) {
		if provider, ok := s.handler.(platform.WindowProvider); ok {
			if childWindowList := provider.GetChildWindows(); childWindowList != nil {
				w.frameCaretSet = false
				err := p.renderer.RenderFrameWithChildWindows(w, childWindowList, p.scale, func(win *nativeWin) {
					p.paintBackend(win, forceFull, true)
				})
				if err == nil {
					// The compositor gathered the frame's caret from the
					// layers; the surface is the platform's to talk to.
					if w.frameCaretSet {
						platform.ApplyTextCaret(s, w.frameCaret)
					}
					p.scheduleAnimationFrame(s)
					return
				}
				fmt.Fprintf(os.Stderr, "Child window compositor error on window %d: %v\n", w.id, err)
				// Fall through to the plain present so the frame still lands.
			}
		}
	}

	p.paintAndPresent(w, forceFull)
}

// scheduleAnimationFrame keeps frames coming while the rotation demo is
// running (or easing out) — the animation must not stall waiting for
// input events.
func (p *Platform) scheduleAnimationFrame(s *sdlSurface) {
	if s == nil {
		return
	}
	animating := p.rotationEnabled.Load()
	if !animating {
		animating = time.Since(p.rotationDeactivationTime).Seconds() < 0.5
	}
	if animating {
		s.Invalidate(core.UnitRect{})
	}
}

// frameDebug reports slow presents under KITTYTK_FRAME_DEBUG. It used to
// print unconditionally to stdout, which on a genuinely slow frame added
// a synchronous write to the terminal to the cost of the frame.
var frameDebug = os.Getenv("KITTYTK_FRAME_DEBUG") != ""

// surfaceNeedsRepaint reports whether a window's backend pixels are
// stale. A handler that reports no repaint revision repaints every
// frame, which is what every handler did before revisions existed.
//
// The backend's identity is part of it: a resize or font zoom replaces
// the backend with a fresh zero-filled one, and a check that compared
// only sizes could keep serving a black surface when the new one
// happened to match the old one's dimensions.
func (p *Platform) surfaceNeedsRepaint(w *nativeWin, forceFull bool) bool {
	if forceFull || compositorAlwaysRepaint || w.backend == nil {
		return true
	}
	s := w.surface
	if s == nil || s.handler == nil {
		return true
	}
	provider, ok := s.handler.(platform.RepaintRevisionProvider)
	if !ok {
		return true
	}

	sig := paintSignature{
		revision:    provider.RepaintRevision(),
		hasRevision: true,
		fontSize:    w.backend.FontSize(),
		metrics:     w.backend.Metrics(),
	}
	if img := w.backend.Image(); img != nil {
		sig.widthPx, sig.heightPx = img.Bounds().Dx(), img.Bounds().Dy()
	}

	now := time.Now()
	stale := !w.paintedValid || w.paintedBackend != w.backend ||
		needsRepaint(w.painted, sig, now.Sub(w.paintedAt), heartbeatInterval(w.id), false, false)
	if !stale {
		return false
	}
	w.painted = sig
	w.paintedAt = now
	w.paintedBackend = w.backend
	w.paintedValid = true
	return true
}

// paintAndPresent runs the handler frame into the window's raster backend and
// presents it as a single surface (no child window compositing).
func (p *Platform) paintAndPresent(w *nativeWin, forceFull bool) {
	frameStart := time.Now()

	s := w.surface
	if s == nil || s.handler == nil || w.backend == nil {
		return
	}

	// Repaint only when the surface would look different. The renderer
	// still presents every frame from the pixels it already holds, so
	// the present cadence is unchanged and nothing can go stale on an
	// expose — only the CPU paint, the pixel conversion and the upload
	// are skipped.
	repainted := p.surfaceNeedsRepaint(w, forceFull)
	if repainted {
		p.paintBackend(w, forceFull, false)
		w.pixelsDirty = true
	}

	// Nothing changed, so present nothing: the window still shows the
	// frame it last presented. Worth skipping because a present WAITS
	// for vsync, and the desktop's tick invalidates every torn-off host
	// whenever anything at all wants a repaint — so an idle torn window
	// was paying a refresh-rate stall per tick to redisplay a picture
	// identical to the one on screen.
	//
	// The rotation demo is the exception: it animates through the
	// uniform buffer rather than the pixels, so its frames have nothing
	// dirty and must present anyway.
	animating := p.rotationEnabled.Load() ||
		time.Since(p.rotationDeactivationTime).Seconds() < 0.5
	if !repainted && !w.pixelsDirty && !forceFull && !animating {
		p.scheduleAnimationFrame(s)
		return
	}

	// The GPU blit, the effects, and the rounded-corner punch-out all
	// live in the renderer, so there is exactly ONE implementation of
	// them — this path used to carry a second, drifting copy.
	if err := p.renderer.Present(w, w.backend); err != nil {
		fmt.Fprintf(os.Stderr, "Present error on window %d: %v\n", w.id, err)
	}

	// No frame-rate floor here. This used to sleep out the remainder of
	// 16ms "to prevent event starvation", which the main loop already
	// guards with its own Delay every iteration — and it slept on the
	// PLATFORM THREAD, so it stalled event handling and every other
	// window along with this one.
	//
	// It was also asymmetric: the compositing present returns before
	// reaching this function, so the desktop never paid it and only
	// torn-off windows did. That is most of why they felt slower to
	// move and update. Caching the repaint made it worse, not better —
	// with the paint skipped there was nothing left to fill the budget,
	// so it slept nearly the whole 16ms every frame.
	if frameDebug {
		if d := time.Since(frameStart); d > 30*time.Millisecond {
			fmt.Fprintf(os.Stderr, "kittytk-frame: window %d took %v\n", w.id, d)
		}
	}

	// Continuous rotation requires continuous repaints
	p.scheduleAnimationFrame(s)

	if p.showFPS && w == p.main {
		p.fpsFrames++
	}
}

// damageDevicePx returns the device-pixel sub-rectangle to re-upload for a bounded repaint.
func damageDevicePx(b *raster.Backend, full bool, dmg core.UnitRect) (x0, y0, x1, y1 int, ok bool) {
	if full {
		return 0, 0, 0, 0, false
	}
	return b.DevicePxRect(dmg)
}

// updateFPSTitle rewrites the main window's OS title with the measured frame rate.
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

// liveResize re-sizes one window's framebuffer, re-lays out its handler, and
// presents immediately. It reports whether it presented a frame, so the
// caller knows if a same-size event still needs a present of its own.
func (p *Platform) liveResize(id uint32, wPx, hPx int) bool {
	w, ok := p.wins[id]
	if !ok || wPx <= 0 || hPx <= 0 {
		return false
	}
	if img := w.backend.Image(); img != nil &&
		img.Bounds().Dx() == wPx && img.Bounds().Dy() == hPx {
		return false
	}
	if err := p.sizeFramebuffer(w, wPx, hPx); err != nil {
		return false
	}
	// Reshape on every size change: a maximize (which sets WINDOW_MAXIMIZED)
	// squares the corners and drops the shadow, a restore rounds them back.
	// applyWindowShape reads the live flags, so the transition needs no
	// separate maximize/restore event hook.
	p.applyWindowShape(w)
	s := w.surface
	if s == nil || s.handler == nil {
		return false
	}
	s.handler.Resized(w.backend.Size())
	p.presentWindow(w, true)
	s.dirty.Store(false)
	return true
}

// zoomChordActive reports whether the modifiers held are the platform's
// font-zoom chord. On Windows that is Ctrl+Shift — Windows keyboards seldom have
// a Command/Super key, and plain Ctrl+/- is commonly an app's own binding — so
// zoom is Ctrl+Shift with "-", "=" and "0". Everywhere else it is the
// Command/Meta (GUI) key, Shift free (Cmd++ is Cmd+Shift+= on common layouts).
func zoomChordActive(mod uint16) bool {
	return zoomChordActiveFor(mod, runtime.GOOS == "windows")
}

// zoomChordActiveFor is zoomChordActive with the platform decision passed in, so
// both branches are testable on any host.
func zoomChordActiveFor(mod uint16, windows bool) bool {
	if windows {
		return mod&sdl3.KMOD_CTRL != 0 && mod&sdl3.KMOD_SHIFT != 0 &&
			mod&(sdl3.KMOD_GUI|sdl3.KMOD_ALT) == 0
	}
	return mod&sdl3.KMOD_GUI != 0 && mod&(sdl3.KMOD_CTRL|sdl3.KMOD_ALT) == 0
}

// zoomTarget resolves a font-zoom chord (see zoomChordActive) to the font size
// it asks for: "+"/"=" (same key) steps up a point, "-" steps down, "0" returns
// to the configured default; the keypad's +/-/0 count too. ok is false for any
// other key or when the chord's modifiers are not held.
func zoomTarget(sym sdl3.Keysym, cur, def int) (int, bool) {
	if !zoomChordActive(sym.Mod) {
		return 0, false
	}
	switch sym.Sym {
	case sdl3.K_EQUALS, sdl3.K_PLUS, sdl3.K_KP_PLUS:
		return clampFontPt(cur + 1), true
	case sdl3.K_MINUS, sdl3.K_KP_MINUS:
		return clampFontPt(cur - 1), true
	case sdl3.K_0, sdl3.K_KP_0:
		return clampFontPt(def), true
	}
	return 0, false
}

// fontZoomKey consumes a KEYDOWN when it is one of the zoom chords, applying
// the resulting size live; the key never reaches the surface handler.
func (p *Platform) fontZoomKey(sym sdl3.Keysym) bool {
	cur, def := p.fontSize, p.defaultFontSize
	if cur < 1 {
		cur = 12 // the raster base when no size was ever configured
	}
	if def < 1 {
		def = 12
	}
	size, ok := zoomTarget(sym, cur, def)
	if !ok {
		return false
	}
	p.applyFontSize(size)
	return true
}

// applyFontSize changes the live font_size on every open window at once.
// When w.window.SetSize executes, it triggers an OS window resize event.
// Because Part 2 wires the watch function to catch this size shift, our refactored
// WebGPU swapchain code adapts dynamically with zero structural friction.
func (p *Platform) applyFontSize(size int) {
	cur := p.fontSize
	if cur < 1 {
		cur = 12
	}
	if size == cur {
		return
	}
	p.fontSize = size
	if p.seed != nil {
		p.seed.SetFontSize(size)
	}
	for _, w := range p.wins {
		if w.backend == nil {
			continue
		}
		keepPx := w == p.main
		if s := w.surface; !keepPx && s != nil && s.handler != nil {
			if a, ok := s.handler.(platform.PixelAnchoredOnFontZoom); ok {
				keepPx = a.KeepPixelSizeOnFontZoom()
			}
		}
		if keepPx {
			w.backend.SetFontSize(size)
			// The corner radius is font-scaled, so it changes on zoom even
			// though this window keeps its pixel size — re-shape it.
			p.applyWindowShape(w)
			if s := w.surface; s != nil && s.handler != nil {
				s.handler.Resized(w.backend.Size())
				p.presentWindow(w, true)
				s.dirty.Store(false)
			}
			continue
		}
		// Snapshot the surface's unit size on the hardened cell pitch by
		// ROUNDING the current pixels, not flooring (backend.Size()): this
		// window keeps its UNIT size across the zoom and is re-sized to the
		// new font's pixels, so a floor here would shed up to a unit every
		// zoom and the window would creep smaller. Round-trips exactly.
		units := w.backend.SizeRounded()
		w.backend.SetFontSize(size)
		wPx := w.backend.UnitToPxX(units.Width)
		hPx := w.backend.UnitToPxY(units.Height)
		if w.window != nil {
			w.window.SetSize(int32(wPx), int32(hPx))
		}
		if err := p.sizeFramebuffer(w, wPx, hPx); err != nil {
			continue
		}
		// Recompute the font-scaled radius and re-shape (also re-squares a
		// maximized window and refreshes its shadow).
		p.applyWindowShape(w)
		if s := w.surface; s != nil && s.handler != nil {
			s.handler.Resized(w.backend.Size())
			p.presentWindow(w, true)
			s.dirty.Store(false)
		}
	}
}

// surfaceFor routes an event's window ID to its surface mapping wrapper.
func (p *Platform) surfaceFor(id uint32) *sdlSurface {
	if w, ok := p.wins[id]; ok {
		return w.surface
	}
	return nil
}

// pumpEvents drains SDL's hardware queue into the per-window surface handlers.
// It maps system pixel dimensions to abstract core toolkit units dynamically.
func (p *Platform) pumpEvents() bool {
	delivered := false
	for {
		ev := sdl3.PollEvent()
		if ev == nil {
			return delivered
		}
		delivered = true
		switch e := ev.(type) {
		case *sdl3.QuitEvent:
			if s := p.mainSurface(); s != nil && s.handler != nil {
				s.handler.Event(core.QuitEvent{})
			}
		case *sdl3.WindowEvent:
			s := p.surfaceFor(e.WindowID)
			if s == nil || s.handler == nil {
				continue
			}
			switch e.Event {
			case sdl3.WindowResized:
				// Handled automatically via our event watch hook in Part 2.
				// This acts as a reliable, idempotent backstop fallback.
				p.liveResize(e.WindowID, int(e.Data1), int(e.Data2))
			case sdl3.WindowFocusGained:
				s.handler.Event(core.FocusEvent{Focused: true})
				s.Invalidate(core.UnitRect{})
			case sdl3.WindowFocusLost:
				s.handler.Event(core.FocusEvent{Focused: false})
				s.Invalidate(core.UnitRect{})
			case sdl3.WindowMouseLeave:
				// Pointer left the active boundary box: clear hover affordances.
				s.handler.Event(core.MouseLeaveEvent{})
				s.Invalidate(core.UnitRect{})
			}
		case *sdl3.TextInputEvent:
			s := p.surfaceFor(e.WindowID)
			if s == nil || s.handler == nil {
				continue
			}
			text := e.GetText()
			for _, ch := range text {
				// On macOS, handle native Option key shortcuts by mapping them
				// back into clear "M-key" syntax to ensure uniformity across environments.
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
		case *sdl3.TextEditingEvent:
			// The input method's in-flight composition. It goes to the
			// focused trinket UNTRANSLATED - no key names, no shortcut
			// gauntlet: these characters are not keys, they are a
			// picture of what the IME currently holds, replaced whole on
			// every update and ended by an empty one.
			s := p.surfaceFor(e.WindowID)
			if s == nil || s.handler == nil {
				continue
			}
			s.handler.Event(core.TextEditingEvent{
				Text:   e.GetText(),
				Start:  int(e.Start),
				Length: int(e.Length),
			})
		case *sdl3.KeyboardEvent:
			s := p.surfaceFor(e.WindowID)
			if s == nil || s.handler == nil {
				continue
			}
			if e.Type == sdl3.KeyDown {
				// Check for rotation trigger (R key) - toggles on/off.
				// Only supported by renderers with rotation capability
				// (WebGPU); works in plain-present AND compositor modes.
				// Gated: it fires only while the rotationGate says so (the
				// desktop points it at "the About box is focused"), so R stays
				// an ordinary key in the editor and everywhere else.
				if e.Keysym.Sym == sdl3.K_r && p.renderer.SupportsFeature(FeatureRotation) &&
					p.rotationGate != nil && p.rotationGate() {
					enabled := !p.rotationEnabled.Load()
					p.rotationEnabled.Store(enabled)

					// The Platform keeps its own copy of the animation clock
					// for mouse-coordinate rotation compensation.
					if enabled {
						p.rotationActivationTime = time.Now()
						p.rotationStartTime = time.Now()
					} else {
						// Store current angle for smooth ease-out
						elapsed := time.Since(p.rotationStartTime).Seconds()
						timeSinceActivation := time.Since(p.rotationActivationTime).Seconds()
						if timeSinceActivation > 1.0 { // After ease-in completes
							easeOutCubic := func(t float64) float64 {
								t = math.Min(t, 1.0)
								return 1.0 - math.Pow(1.0-t, 3.0)
							}
							rotationProgress := timeSinceActivation / 1.0
							rotationEased := easeOutCubic(rotationProgress)
							p.rotationAngleAtDeactivation = elapsed * 0.1 * rotationEased
						}
						p.rotationDeactivationTime = time.Now()
					}

					p.renderer.SetRotationEnabled(enabled)

					// The animation needs frames even while input is idle.
					s.Invalidate(core.UnitRect{})
					continue // Consumed by the easter egg; don't also type "r".
				}

				if p.fontZoomKey(e.Keysym) {
					continue // Consumed by host zoom controller, skip dispatching
				}
				if key := translateKey(e.Keysym); key != "" {
					mods, name := core.ParseKeyModifiers(key)
					text := ""
					if len(name) == 1 && name[0] >= 32 && name[0] < 127 {
						text = name
					}
					s.handler.Event(core.KeyPressEvent{Key: key, Modifiers: mods, Text: text})
				}
			} else if e.Type == sdl3.KeyUp {
				// Report release actions back to tracking vectors using the modifier
				// states parsed immediately AFTER the key release event completes.
				s.handler.Event(core.KeyReleaseEvent{
					Key:       translateKey(e.Keysym),
					Modifiers: currentKeyModifiers(),
				})
			}
		case *sdl3.MouseButtonEvent:
			s := p.surfaceFor(e.WindowID)
			if s == nil || s.handler == nil {
				continue
			}
			btn := mapButton(e.Button)
			x, y := p.toUnits(e.X, e.Y, e.WindowID)
			mods := currentKeyModifiers()
			if e.Type == sdl3.MouseDown {
				// Enable pointer capturing so dragging actions extend beyond window borders
				// to allow continuous, lag-free native widget tear-out gestures.
				_ = sdl3.CaptureMouse(true)
				s.handler.Event(core.MousePressEvent{X: x, Y: y, Button: btn, Modifiers: mods})
			} else {
				_ = sdl3.CaptureMouse(false)
				s.handler.Event(core.MouseReleaseEvent{X: x, Y: y, Button: btn, Modifiers: mods})
			}
		case *sdl3.MouseMotionEvent:
			s := p.surfaceFor(e.WindowID)
			if s == nil || s.handler == nil {
				continue
			}
			var held core.MouseButton
			if e.State&sdl3.ButtonLeftMask != 0 {
				held = core.LeftButton
			}
			x, y := p.toUnits(e.X, e.Y, e.WindowID)
			s.handler.Event(core.MouseMoveEvent{X: x, Y: y, Buttons: held, Modifiers: currentKeyModifiers()})
		case *sdl3.MouseWheelEvent:
			s := p.surfaceFor(e.WindowID)
			if s == nil || s.handler == nil {
				continue
			}
			mx, my, _ := sdl3.GetMouseState()
			x, y := p.toUnits(mx, my, e.WindowID)
			s.handler.Event(core.MouseWheelEvent{
				X: x, Y: y,
				// Invert raw SDL wheel vectors to match standard toolkit scroll conventions
				DeltaX: int(e.X), DeltaY: -int(e.Y),
				PreciseX:  float64(e.PreciseX),
				PreciseY:  -float64(e.PreciseY),
				Modifiers: currentKeyModifiers(),
			})
		}
	}
}

// mainSurface retrieves the abstract surface binding associated with the primary workspace window.
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

// windowShapeRadiusUnits is the rounded window-frame corner radius in cell
// units. It matches objects/window.FrameCornerRadius() (6), kept as a local
// constant so the platform package need not import objects/window.
const windowShapeRadiusUnits = 6

// shapeRadiusPx is the window corner radius in device pixels at the current
// font size, so a shaped window's corners track font zoom instead of being
// frozen at the size they had when the window was created.
//
// It uses the backend's FRACTIONAL pixels-per-unit (scale * fontSize/12), the
// same mapping the window frame's round-rect is DRAWN with — not the ceil'd,
// cell-snapped cellPx. At odd font sizes cellPx overshoots by up to a device
// pixel, which made the layer/mask clip miss the drawn round-rect (the corner
// "cut off" at certain zooms). Rounding the exact unit length lands the clip on
// the frame.
func (p *Platform) shapeRadiusPx() int {
	fs := p.fontSize
	if fs < 1 {
		fs = 12
	}
	return int(math.Round(float64(windowShapeRadiusUnits) * float64(p.scale) * float64(fs) / 12))
}

// applyWindowShape (re)applies a window's rounded shape and OS drop shadow to
// match its current state. It is the ONE place that decides a shaped window's
// live geometry, so creation, SetBordered, font zoom, resize and
// maximize/restore all funnel through it and can never disagree.
//
// A window is rounded only when it is floating: borderless (a bordered window
// cannot be shaped, and its OS title bar draws the corners) AND not maximized
// (a maximized window fills its display, so it is squared with no shadow, the
// same convention native windows follow). The radius is recomputed from the
// live font size every call, which is what keeps it correct across zoom.
func (p *Platform) applyWindowShape(w *nativeWin) {
	if w == nil || w.window == nil || w.wantRadiusPx <= 0 {
		return // a plain window that is never rounded
	}
	flags := w.window.Flags()
	round := flags&sdl3.WINDOW_BORDERLESS != 0 && flags&sdl3.WINDOW_MAXIMIZED == 0
	r := 0
	if round {
		r = p.shapeRadiusPx()
		// Never over-round: a radius past half the smaller side eats the whole
		// corner (a small window zoomed way out lost its corners entirely). The
		// SDL shaped-mask path clamps too, but the macOS layer/punch path did
		// not — clamp here so every mechanism agrees. Sizes are device pixels.
		if wPx, hPx := w.window.SizeInPixels(); wPx > 0 && hPx > 0 {
			m := int(wPx)
			if int(hPx) < m {
				m = int(hPx)
			}
			if r > m/2 {
				r = m / 2
			}
		}
	}
	// Skip when nothing changed: an ordinary resize keeps the same radius and
	// state, so re-rounding then only thrashes the window's layer (and drifted
	// the corners on macOS). Zoom and maximize/restore change r and fall through.
	if r == w.appliedShapePx {
		return
	}
	w.appliedShapePx = r
	if platformPerPixelAlpha {
		// macOS: Core Animation clips the layer, and the framebuffer punch
		// (cornerRadiusPx, consumed every present) cuts the corner pixels. The
		// window must be non-opaque for the clipped corners to show through;
		// the main window defers this to here (its first borderless shaping).
		if r > 0 && !w.transparent {
			if makeWindowTransparent(w.window) {
				w.transparent = true
			}
		}
		w.cornerRadiusPx = r
		roundWindowLayer(w.window, r) // r == 0 squares the layer
	} else {
		// Windows / X11: SDL shaped window. shapeRadiusPx drives applyShape,
		// which clears the shape (square) at 0.
		w.shapeRadiusPx = r
		w.applyShape()
	}
	// The OS drop shadow follows the shape and is click-through; drop it when
	// squared/maximized so a full-screen window casts none.
	setWindowShadow(w.window, r > 0)
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
func (p *Platform) toUnits(x, y int32, windowID uint32) (core.Unit, core.Unit) {
	// Check if rotation effects are active (either easing in or easing out)
	isActive := p.rotationEnabled.Load()
	isEasingOut := false

	if !isActive {
		// Check if we're still in ease-out phase
		timeSinceDeactivation := time.Since(p.rotationDeactivationTime).Seconds()
		if timeSinceDeactivation < 0.5 {
			isEasingOut = true
			isActive = true // Treat as active for transformation purposes
		}
	}

	// Only apply rotation/scaling if enabled or easing out
	if !isActive {
		// Normal path - no transformation
		denomW, denomH := p.rootDenomination()
		ux := pxToUnitAxis(int(x), denomW, p.cellPx(denomW))
		uy := pxToUnitAxis(int(y), denomH, p.cellPx(denomH))
		return core.Unit(ux), core.Unit(uy)
	}

	// Get window for rotation pivot (needed for display rotation compensation)
	win, ok := p.wins[windowID]
	if !ok || win.window == nil {
		denomW, denomH := p.rootDenomination()
		ux := pxToUnitAxis(int(x), denomW, p.cellPx(denomW))
		uy := pxToUnitAxis(int(y), denomH, p.cellPx(denomH))
		return core.Unit(ux), core.Unit(uy)
	}

	w, h := win.window.Size()
	centerX := float64(w) / 2.0
	centerY := float64(h) / 2.0

	// Translate to center
	fx := float64(x) - centerX
	fy := float64(y) - centerY

	easeOutCubic := func(t float64) float64 {
		t = math.Min(t, 1.0)
		return 1.0 - math.Pow(1.0-t, 3.0)
	}

	easeInOutCubic := func(t float64) float64 {
		if t < 0.5 {
			return 4.0 * t * t * t
		}
		return 1.0 - math.Pow(-2.0*t+2.0, 3.0)/2.0
	}

	var currentScale float64
	var angle float64

	if isEasingOut {
		// Easing out - continue forward with speedup
		timeSinceDeactivation := time.Since(p.rotationDeactivationTime).Seconds()
		scaleProgress := timeSinceDeactivation / 0.5
		scaleEased := easeOutCubic(scaleProgress)
		currentScale = 2.0 - scaleEased*1.0 // 2.0 -> 1.0

		// Match shader: continue forward with accelerated catch-up, capped at target
		currentAngle := p.rotationAngleAtDeactivation
		twoPi := 2.0 * math.Pi
		normalizedAngle := math.Mod(currentAngle, twoPi)
		if normalizedAngle < 0 {
			normalizedAngle += twoPi
		}
		angleRemaining := twoPi - normalizedAngle

		normalRotation := timeSinceDeactivation * 0.1
		catchUpProgress := math.Min(timeSinceDeactivation/0.5, 1.0)
		catchUpEased := easeInOutCubic(catchUpProgress)
		catchUpRotation := angleRemaining * catchUpEased

		targetAngle := currentAngle + angleRemaining
		currentRotatedAngle := currentAngle + normalRotation + catchUpRotation
		if currentRotatedAngle > targetAngle {
			currentRotatedAngle = targetAngle
		}

		angle = -currentRotatedAngle // Negative to match shader
	} else {
		// Easing in / active
		timeSinceActivation := time.Since(p.rotationActivationTime).Seconds()
		scaleProgress := timeSinceActivation / 0.5
		scaleEased := easeOutCubic(scaleProgress)
		currentScale = 1.0 + scaleEased*1.0 // 1.0 -> 2.0

		rotationProgress := timeSinceActivation / 1.0
		rotationEased := easeOutCubic(rotationProgress)
		elapsed := time.Since(p.rotationStartTime).Seconds()
		angle = -(elapsed * 0.1 * rotationEased) // Negative to match shader
	}

	// Scale by current scale to match the content scale
	fx *= currentScale
	fy *= currentScale

	// Rotate
	cosA := math.Cos(angle)
	sinA := math.Sin(angle)

	rotatedX := fx*cosA - fy*sinA
	rotatedY := fx*sinA + fy*cosA

	// Translate back
	finalX := rotatedX + centerX
	finalY := rotatedY + centerY

	denomW, denomH := p.rootDenomination()
	ux := pxToUnitAxis(int(finalX), denomW, p.cellPx(denomW))
	uy := pxToUnitAxis(int(finalY), denomH, p.cellPx(denomH))
	return core.Unit(ux), core.Unit(uy)
}

// currentKeyModifiers translates SDL's live modifier state for mouse
// events (Shift+click bypasses terminal mouse reporting, Shift+wheel
// scrolls horizontally).
func currentKeyModifiers() core.KeyModifiers {
	var mods core.KeyModifiers
	state := sdl3.GetModState()
	if state&sdl3.KMOD_SHIFT != 0 {
		mods |= core.ShiftModifier
	}
	if state&sdl3.KMOD_CTRL != 0 {
		mods |= core.ControlModifier
	}
	if state&sdl3.KMOD_ALT != 0 {
		mods |= core.AltModifier
	}
	if state&sdl3.KMOD_GUI != 0 {
		mods |= core.MetaModifier
	}
	return mods
}

func mapButton(b uint8) core.MouseButton {
	switch b {
	case sdl3.BUTTON_LEFT:
		return core.LeftButton
	case sdl3.BUTTON_MIDDLE:
		return core.MiddleButton
	case sdl3.BUTTON_RIGHT:
		return core.RightButton
	}
	return core.NoButton
}

// specialKeys maps SDL keycodes to D3 key names (spellings match
// core/keybindings.go).
var specialKeys = map[sdl3.Keycode]string{
	sdl3.K_RETURN:    "Enter",
	sdl3.K_KP_ENTER:  "Enter",
	sdl3.K_TAB:       "Tab",
	sdl3.K_ESCAPE:    "Escape",
	sdl3.K_BACKSPACE: "Backspace",
	sdl3.K_DELETE:    "Delete",
	sdl3.K_INSERT:    "Insert",
	sdl3.K_HOME:      "Home",
	sdl3.K_END:       "End",
	sdl3.K_PAGEUP:    "PageUp",
	sdl3.K_PAGEDOWN:  "PageDown",
	sdl3.K_UP:        "Up",
	sdl3.K_DOWN:      "Down",
	sdl3.K_LEFT:      "Left",
	sdl3.K_RIGHT:     "Right",
	sdl3.K_F1:        "F1",
	sdl3.K_F2:        "F2",
	sdl3.K_F3:        "F3",
	sdl3.K_F4:        "F4",
	sdl3.K_F5:        "F5",
	sdl3.K_F6:        "F6",
	sdl3.K_F7:        "F7",
	sdl3.K_F8:        "F8",
	sdl3.K_F9:        "F9",
	sdl3.K_F10:       "F10",
	sdl3.K_F11:       "F11",
	sdl3.K_F12:       "F12",
}

// translateKey produces the D3 key string for a KEYDOWN, or "" when
// the TextInput path owns it (plain printable characters).
//
// Hyper has no native SDL modifier, so mew synthesizes it from a doubled
// side modifier: holding BOTH the left and right Ctrl (or both Alt) keys
// promotes the chord to Hyper. The doubled modifier is consumed by the
// promotion; any single-side modifier still held keeps its normal role, so
//
//	LCtrl+RCtrl+X        -> H-X       (both Ctrl -> Hyper)
//	LAlt+RAlt+X          -> H-X       (both Alt  -> Hyper)
//	LAlt+RAlt+Ctrl+X     -> H-^X      (Hyper + a single Ctrl)
//	LCtrl+RCtrl+Alt+X    -> H-M-x     (Hyper + a single Alt)
//
// AltGr reports as a single (right) Alt, so it never trips the both-Alt
// promotion. Shift is deliberately left out — it is a text-producing
// modifier, so a doubled Shift would hijack ordinary capital letters.
func translateKey(sym sdl3.Keysym) string {
	bothCtrl := sym.Mod&sdl3.KMOD_LCTRL != 0 && sym.Mod&sdl3.KMOD_RCTRL != 0
	bothAlt := sym.Mod&sdl3.KMOD_LALT != 0 && sym.Mod&sdl3.KMOD_RALT != 0
	hyper := bothCtrl || bothAlt

	ctrl := sym.Mod&sdl3.KMOD_CTRL != 0
	alt := sym.Mod&sdl3.KMOD_ALT != 0
	shift := sym.Mod&sdl3.KMOD_SHIFT != 0
	gui := sym.Mod&sdl3.KMOD_GUI != 0

	if hyper {
		// The doubled modifier is spent on the Hyper promotion; a
		// single-side Ctrl or Alt still contributes its normal role.
		if bothCtrl {
			ctrl = false
		}
		if bothAlt {
			alt = false
		}
	}

	base := encodeKey(sym, ctrl, alt, shift, gui)

	if !hyper {
		return base
	}

	if base == "" {
		// The residual modifiers alone would defer to TextInput (a plain
		// or shifted printable). Hyper is a real chord, so synthesize the
		// bare key token here instead of dropping the keystroke.
		if base = bareKey(sym, shift); base == "" {
			return ""
		}
	}
	return "H-" + base
}

// bareKey returns the unmodified key token for a keysym: a special-key name,
// or the printable character (upper-cased when Shift is held, so the caseful
// hyphenated-modifier convention — H-a unshifted, H-A shifted — holds). It is
// used only to give a Hyper chord a key to attach to when the residual
// modifiers would otherwise have deferred the keystroke to TextInput.
func bareKey(sym sdl3.Keysym, shift bool) string {
	if name, ok := specialKeys[sym.Sym]; ok {
		return name
	}
	if sym.Sym >= 32 && sym.Sym < 127 {
		ch := rune(sym.Sym)
		if shift && ch >= 'a' && ch <= 'z' {
			return string(ch - 'a' + 'A')
		}
		return string(ch)
	}
	return ""
}

// encodeKey maps a keysym plus its effective modifier set to a D3 key string,
// or "" when the TextInput path owns it (plain printable characters). The
// modifier booleans are passed in rather than read from sym.Mod so translateKey
// can strip the modifiers it has already spent on a Hyper promotion.
func encodeKey(sym sdl3.Keysym, ctrl, alt, shift, gui bool) string {
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
	x, y, _ := sdl3.GetGlobalMouseState()
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
		x, y = sdl3.WINDOWPOS_CENTERED, sdl3.WINDOWPOS_CENTERED
	}
	flags := sdl3.WINDOW_SHOWN
	if opts.Borderless {
		flags |= sdl3.WINDOW_BORDERLESS
	} else {
		flags |= sdl3.WINDOW_RESIZABLE
	}
	radius := 0
	if opts.Borderless {
		radius = opts.CornerRadiusPx
	}

	// Prevent secondary torn-out windows from stealing active focus mid-drag sessions
	_ = sdl3.SetHint("SDL_WINDOW_NO_ACTIVATION_WHEN_SHOWN", "1")

	// Spawns a native window and sets up its independent WebGPU surface swapchain contexts
	w, err := p.createWindow(opts.Title, x, y, wPx, hPx, flags, radius)
	if err != nil {
		return nil, err
	}
	w.surface = &sdlSurface{platform: p, win: w}
	if opts.Borderless {
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
	if _, _, state := sdl3.GetGlobalMouseState(); state&sdl3.ButtonLeftMask != 0 {
		_ = sdl3.CaptureMouse(true)
	}
}

// Clipboard implements platform.Platform.
func (p *Platform) Clipboard() string {
	s, _ := sdl3.GetClipboardText()
	return s
}

// SetClipboard implements platform.Platform.
func (p *Platform) SetClipboard(text string) { _ = sdl3.SetClipboardText(text) }

// Beep implements platform.Platform.
func (p *Platform) Beep() {}

// SetCursor implements platform.CursorController: set the application's
// system mouse cursor. System cursors are created on demand and cached;
// redundant sets (same shape) are skipped.
func (p *Platform) SetCursor(shape core.CursorShape) {
	if p.cursors == nil {
		p.cursors = map[core.CursorShape]*sdl3.Cursor{}
	}
	cur, ok := p.cursors[shape]
	if !ok {
		// SDL3 reports creation failure; a nil cursor is cached so the
		// lookup is not retried every frame.
		cur, _ = sdl3.CreateSystemCursor(systemCursorID(shape))
		p.cursors[shape] = cur
	}
	if cur == nil {
		return
	}
	_ = sdl3.SetCursor(cur)
	p.cursorSet = true
}

func (p *Platform) reassertCursor() {
	if p.cursorSet {
		sdl3.SetCursor(nil)
	}
}

// systemCursorID maps a core cursor shape to its SDL system cursor.
func systemCursorID(shape core.CursorShape) sdl3.SystemCursor {
	switch shape {
	case core.CursorText:
		return sdl3.SYSTEM_CURSOR_TEXT
	case core.CursorResizeH:
		return sdl3.SYSTEM_CURSOR_EW_RESIZE
	case core.CursorResizeV:
		return sdl3.SYSTEM_CURSOR_NS_RESIZE
	case core.CursorResizeNWSE:
		return sdl3.SYSTEM_CURSOR_NWSE_RESIZE
	case core.CursorResizeNESW:
		return sdl3.SYSTEM_CURSOR_NESW_RESIZE
	default:
		return sdl3.SYSTEM_CURSOR_DEFAULT
	}
}

// sdlSurface is one SDL window mapped onto a hardware GoGPU/WebGPU presentation layout.
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

	// caretVisible/caretX/caretY are the last caret this surface reported
	// to the OS, so an unchanged caret costs no SDL call. There is no
	// OS-drawn caret on a graphical surface — trinkets paint their own —
	// so the position exists purely to place an input method's candidate
	// window. See SetCursorPosition.
	caretVisible bool
	caretX       core.Unit
	caretY       core.Unit
}

func (s *sdlSurface) Size() core.UnitSize {
	return s.win.backend.Size()
}
func (s *sdlSurface) Metrics() core.CellMetrics {
	return s.win.backend.Metrics()
}
func (s *sdlSurface) SetHandler(h platform.SurfaceHandler) { s.handler = h }

// Invalidate marks the surface dirty and accumulates damage regions for optimized sub-texture streaming.
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

// The caret methods are no-ops: a graphical surface has no OS-drawn
// caret to show, move or shape — trinkets paint their own. Where text is
// being typed is a different question, answered by SetTextInputArea.
func (s *sdlSurface) SetCursorVisible(bool)            {}
func (s *sdlSurface) SetCursorPosition(x, y core.Unit) {}
func (s *sdlSurface) SetCursorStyle(int)               {}

// SetTextInputArea implements platform.TextInputAreaSetter: it tells the
// OS where text is being typed, which is what anchors an input method's
// candidate window — the CJK candidate list, macOS's press-and-hold
// accent picker, the emoji picker. With no area set they appear at
// whatever corner the OS last used.
//
// visible false forgets the position, so the OS falls back to its own
// placement rather than anchoring on a stale rectangle.
func (s *sdlSurface) SetTextInputArea(x, y core.Unit, visible bool) {
	if s.closed || s.win == nil || s.win.window == nil {
		return
	}
	if s.caretVisible == visible && (!visible || (s.caretX == x && s.caretY == y)) {
		return // unchanged: no need to tell the OS again
	}
	s.caretVisible, s.caretX, s.caretY = visible, x, y

	if !visible {
		if err := sdl3.ClearTextInputArea(s.win.window); err != nil && imeDebug {
			fmt.Fprintf(os.Stderr, "kittytk-ime: clear failed: %v\n", err)
		}
		return
	}

	b := s.win.backend
	if b == nil {
		return
	}
	m := b.Metrics()
	x0, y0 := b.UnitToPxX(x), b.UnitToPxY(y)
	wPx := b.UnitToPxX(x+m.CellWidth) - x0
	hPx := b.UnitToPxY(y+m.CellHeight) - y0
	if wPx <= 0 {
		wPx = 1
	}
	if hPx <= 0 {
		hPx = 1
	}
	err := sdl3.SetTextInputArea(s.win.window, x0, y0, wPx, hPx, 0)
	if imeDebug {
		fmt.Fprintf(os.Stderr,
			"kittytk-ime: window %d area unit=(%v,%v) px=(%d,%d %dx%d) active=%v err=%v\n",
			s.win.id, x, y, x0, y0, wPx, hPx, sdl3.TextInputActive(s.win.window), err)
	}
}

// imeDebug reports every text-input-area update under KITTYTK_IME_DEBUG.
// An input method that ignores the area and a host that never sets one
// look identical on screen — both put the candidate window in a corner —
// so the only way to tell them apart is to watch the calls.
var imeDebug = os.Getenv("KITTYTK_IME_DEBUG") != ""

// ScreenPositionPx implements platform.NativeSurface.
func (s *sdlSurface) ScreenPositionPx() (int, int) {
	if s.closed || s.win.window == nil {
		return 0, 0
	}
	x, y := s.win.window.Position()
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
	// Shaping follows the border: a borderless window rounds + gains its OS
	// shadow, a re-bordered one squares and drops both (the OS chrome takes
	// over). This is how the main window becomes rounded — it is born bordered
	// and only shapeable once solo mode strips its border.
	s.platform.applyWindowShape(s.win)
}

// ScreenSizePx implements platform.NativeSurface: the OS window's current
// pixel size, straight from SDL (no unit round-trip that would drift at
// fractional pixels-per-unit).
func (s *sdlSurface) ScreenSizePx() (int, int) {
	if s.closed || s.win.window == nil {
		return 0, 0
	}
	w, h := s.win.window.Size()
	return int(w), int(h)
}

// SetScreenSizePx implements platform.NativeSurface: the size change normally
// reports back through the WINDOW_RESIZED path (framebuffer recreate, shape
// reapply, handler.Resized).
//
// Two hazards, both seen only on the solo primary window (created RESIZABLE with
// a title bar, its border stripped at runtime) and not on a torn window
// (borderless from birth): a shrink back from zoom-to-fill could leave the GPU
// swapchain at the maximized size, so the restored content painted into its
// top-left corner and the stale maximized frame stayed on screen until a manual
// edge-drag. First, some window managers flag a filled window MAXIMIZED, and
// SetWindowSize is a no-op on a maximized window — clear it first. Second, a
// programmatic resize did not always deliver a WINDOW_RESIZED for this window,
// so drive the framebuffer reconfigure here from the window's ACTUAL pixel size
// (read back after the resize, so HiDPI points-vs-pixels can't skew it). Both
// are idempotent: liveResize no-ops when the backend already matches, and a real
// resize event that does arrive later costs nothing.
func (s *sdlSurface) SetScreenSizePx(w, h int) {
	if s.closed || s.win.window == nil || w <= 0 || h <= 0 {
		return
	}
	if s.win.window.Flags()&sdl3.WINDOW_MAXIMIZED != 0 {
		s.win.window.Restore()
	}
	s.win.window.SetSize(int32(w), int32(h))
	pxW, pxH := s.win.window.SizeInPixels()
	if pxW > 0 && pxH > 0 {
		s.platform.liveResize(s.win.id, int(pxW), int(pxH))
	}
}

// WorkAreaPx implements platform.NativeSurface: the usable bounds of
// the display the window occupies (the macOS option-zoom target).
func (s *sdlSurface) WorkAreaPx() (int, int, int, int) {
	if s.closed || s.win.window == nil {
		return 0, 0, 0, 0
	}
	idx, err := s.win.window.Display()
	if err != nil {
		idx = 0
	}
	r, err := sdl3.GetDisplayUsableBounds(idx)
	if err != nil {
		return 0, 0, 0, 0
	}
	return int(r.X), int(r.Y), int(r.W), int(r.H)
}

// applyShape rounds the OS window's corners with a binary alpha mask
// so the pixels outside the drawn roundrect frame are not opaque black.
func (w *nativeWin) applyShape() {
	if w.window == nil {
		return
	}
	// applyShape is the SDL shaped-window mechanism (a binary alpha mask), used
	// only where there is no per-pixel window alpha (Windows/X11). macOS rounds
	// through the Core Animation layer instead and must NEVER be handed to
	// SetShape — doing so reshaped torn-off windows off their intended size
	// (a ~20px shrink after a zoom). On macOS this is a no-op, as it was before.
	if platformPerPixelAlpha {
		return
	}
	// A bordered window cannot be shaped (SetShape returns NONSHAPEABLE, and the
	// OS title bar owns the corners). The main window is created bordered and
	// only becomes shapeable once solo mode strips its border, at which point
	// SetBordered re-runs the shape.
	if w.window.Flags()&sdl3.WINDOW_BORDERLESS == 0 {
		return
	}
	if w.shapeRadiusPx <= 0 {
		// Squared (maximized, or a shapeable window asked to go square): clear
		// any shape so the corners are not rounded. Harmless if none was set.
		if w.wantRadiusPx > 0 {
			_ = w.window.SetShape(nil)
		}
		return
	}
	wPx, hPx := w.window.Size()
	if wPx <= 0 || hPx <= 0 {
		return
	}
	mask, err := sdl3.CreateSurface(wPx, hPx, sdl3.PIXELFORMAT_ARGB8888)
	if err != nil {
		return
	}
	defer sdl3.FreeSurface(mask)
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
	// BinarizeAlpha with a mid cutoff, matching the known-good SDL
	// shaped-window configuration (the mask is fully opaque inside and
	// fully clear outside, so the exact cutoff only needs to sit
	// between them).
	// A non-zero result is one of SDL's shape errors (most likely
	// NONSHAPEABLE_WINDOW: the window was not created shaped).
	if err := w.window.SetShape(mask); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: SetShape failed (window not transparent?): %v\n", err)
	}
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
	flags := s.win.window.Flags()
	return flags&sdl3.WINDOW_MINIMIZED != 0 || flags&sdl3.WINDOW_HIDDEN != 0
}

// SetOpacity implements platform.NativeSurface.
func (s *sdlSurface) SetOpacity(opacity float64) {
	if s.closed || s.win.window == nil {
		return
	}
	_ = s.win.window.SetOpacity(float32(opacity))
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
// It marshals the destruction onto the main thread via Post to avoid data races.
func (s *sdlSurface) Close() {
	if s.win == s.platform.main {
		return // Never destroy the primary manager shell layout
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
		s.win.destroy() // Calls our updated WebGPU FFI surface teardown macro smoothly
		reassertCapture()
	})
}
