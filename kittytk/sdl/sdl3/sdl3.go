//go:build sdl

// Package sdl3 is KittyTK's SDL layer: the slice of SDL3 the graphical
// host uses, with the awkward parts handled once.
//
// It exists because several SDL3 facts are worth confining to one
// place rather than spreading through the platform layer:
//
//   - The binding is purego, so libSDL3 is opened at RUN time (see
//     Init) rather than linked, and has to be found in prefixes the
//     dynamic loader does not search by default.
//   - Some binding entry points are declared but panic; the ones the
//     host needs are bound directly (gapfill.go).
//   - SDL3 delivers a flat event union with distinct types per window
//     event; PollEvent translates that into typed Go values the host
//     can switch on.
//   - Window creation lost its position arguments, native handles moved
//     to properties, and the renderer is chosen by driver name.
//
// Names here are SDL3's where SDL3 has one. Where this package invents
// something — the event kinds especially — the name is deliberately
// unlike an SDL constant, so it cannot be mistaken for one.
package sdl3

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	csdl "github.com/Zyko0/go-sdl3/sdl"
	"github.com/ebitengine/purego"
)

// --- init / lifecycle ---

const (
	INIT_VIDEO  = csdl.INIT_VIDEO
	INIT_EVENTS = csdl.INIT_EVENTS
)

// Init loads libSDL3 and initializes the requested subsystems. The
// binding is purego-based: nothing is linked at build time, so the
// library has to be opened before any SDL call — otherwise the first
// one dereferences an unregistered function pointer. Doing it here
// keeps that requirement from leaking into the platform layer.
func Init(flags csdl.InitFlags) error {
	if err := loadLibrary(); err != nil {
		return err
	}
	return csdl.Init(flags)
}

var libLoaded bool

// loadLibrary opens libSDL3, trying the plain library name first and
// then the usual install prefixes.
//
// The bare name alone is not enough: dyld searches /usr/lib and the
// dyld cache, but Homebrew installs to /opt/homebrew (Apple Silicon)
// or /usr/local (Intel), and neither is on the default search path. A
// correctly installed SDL3 would otherwise fail to load with a
// confusing "no such file". KITTYTK_SDL3 overrides the search outright
// for an SDL built somewhere else.
func loadLibrary() error {
	if libLoaded {
		return nil
	}

	// An embedded build carries its own SDL3 and never searches.
	if embeddedSDL {
		if err := loadEmbedded(); err != nil {
			return err
		}
		libLoaded = true
		return nil
	}

	candidates := libraryCandidates()

	var firstErr error
	for _, path := range candidates {
		if path == "" {
			continue
		}
		if err := csdl.LoadLibrary(path); err == nil {
			// Record EXACTLY what loaded. gapfill must reopen this same file,
			// not re-search: csdl.Path() hands back a bare "libSDL3.dylib", and
			// resolving that afresh can land on a different SDL3 than this one
			// (a system copy beside a bundled one), whereupon both load and
			// SDL's Obj-C classes register twice — the Metal layer lookup then
			// fails. An explicit path is refcounted, so gapfill's reopen is the
			// same handle.
			loadedSDLPath = path
			libLoaded = true
			return nil
		} else if firstErr == nil {
			firstErr = err
		}
	}
	return fmt.Errorf("sdl3: could not load libSDL3 (tried %v): %w"+
		"\n\tinstall it (macOS: brew install sdl3) or set KITTYTK_SDL3 to its path",
		candidates, firstErr)
}

// loadedSDLPath is the exact candidate loadLibrary succeeded with, so gapfill
// can bind its extra symbols against the very same file (see gapfill.go). Empty
// for an embedded build (loadEmbedded owns the handle) and before the first
// load.
var loadedSDLPath string

// libraryCandidates lists where libSDL3 might live, most specific
// first. dyld and ld.so search neither Homebrew prefix by default, so
// a correctly installed SDL3 is invisible to a bare library name.
func libraryCandidates() []string {
	var c []string
	if custom := os.Getenv("KITTYTK_SDL3"); custom != "" {
		c = append(c, custom)
	}
	// A copy bundled inside a .app, next to the executable
	// (Contents/MacOS/<bin> -> Contents/Frameworks/libSDL3.dylib), on darwin.
	// It leads the list — ahead even of csdl.Path()'s bare "libSDL3.dylib" —
	// so a self-contained bundle loads ITS OWN copy: an explicit path is
	// unambiguous, where the bare name may resolve to a system SDL3 and, loaded
	// alongside the bundled one, register SDL's Obj-C classes twice (the Metal
	// layer lookup then fails, "implemented in both …"). A bare binary has no
	// such sibling; the path won't exist and the search falls through.
	if runtime.GOOS == "darwin" {
		if exe, err := os.Executable(); err == nil {
			c = append(c, filepath.Join(filepath.Dir(exe), "..", "Frameworks", "libSDL3.dylib"))
		}
	}
	c = append(c, csdl.Path())
	switch runtime.GOOS {
	case "darwin":
		c = append(c,
			"/opt/homebrew/lib/libSDL3.dylib", // Homebrew, Apple Silicon
			"/opt/homebrew/opt/sdl3/lib/libSDL3.dylib",
			"/usr/local/lib/libSDL3.dylib", // Homebrew, Intel
			"/opt/local/lib/libSDL3.dylib", // MacPorts
		)
	case "linux":
		c = append(c,
			"libSDL3.so",
			"/usr/local/lib/libSDL3.so.0",
			"/usr/lib/x86_64-linux-gnu/libSDL3.so.0",
			"/usr/lib/aarch64-linux-gnu/libSDL3.so.0",
		)
	}
	return c
}
func Quit()           { csdl.Quit() }
func Delay(ms uint32) { csdl.Delay(ms) }

// SetHint sets an SDL hint. Some hints (SDL_APP_NAME in particular) must be set
// BEFORE SDL_Init to take effect, so this cannot wait for Init to open the
// library: like every entry point in this purego binding, csdl.SetHint
// dereferences a function pointer that is nil until libSDL3 is loaded. Load it
// here first (idempotent — Init's own loadLibrary shares the libLoaded guard),
// so a pre-Init SetHint works instead of segfaulting on the unregistered call.
func SetHint(name, value string) error {
	if err := loadLibrary(); err != nil {
		return err
	}
	return csdl.SetHint(name, value)
}

// --- windows ---

// WINDOWPOS_CENTERED asks for a centered window. SDL3 keeps the same
// sentinel, but position is no longer a creation argument, so
// CreateWindow applies it after the window exists.
const WINDOWPOS_CENTERED = 0x2FFF0000

type WindowFlags = csdl.WindowFlags

const (
	WINDOW_SHOWN      WindowFlags = 0 // SDL3 shows by default; HIDDEN is the opt-out
	WINDOW_HIDDEN                 = csdl.WINDOW_HIDDEN
	WINDOW_RESIZABLE              = csdl.WINDOW_RESIZABLE
	WINDOW_BORDERLESS             = csdl.WINDOW_BORDERLESS
	WINDOW_MINIMIZED              = csdl.WINDOW_MINIMIZED
	WINDOW_MAXIMIZED              = csdl.WINDOW_MAXIMIZED
	WINDOW_METAL                  = csdl.WINDOW_METAL

	// WINDOW_TRANSPARENT is the SDL3-only flag this whole migration was
	// for: the window's framebuffer alpha composites with what is
	// behind it. It must be set at CREATION - it cannot be applied to
	// an existing window.
	WINDOW_TRANSPARENT = csdl.WINDOW_TRANSPARENT
)

// Window wraps an SDL3 window with the method names the platform layer
// already uses.
type Window struct {
	w *csdl.Window
}

// Raw exposes the underlying SDL3 window for code that needs the real
// thing (the macOS layer shim).
func (w *Window) Raw() *csdl.Window { return w.w }

// CreateWindow creates a window at a position, SDL2-style. SDL3 dropped
// the position arguments, so they are applied immediately after.
func CreateWindow(title string, x, y int32, width, height int, flags WindowFlags) (*Window, error) {
	sw, err := csdl.CreateWindow(title, width, height, flags)
	if err != nil {
		return nil, err
	}
	w := &Window{w: sw}
	if x != WINDOWPOS_CENTERED || y != WINDOWPOS_CENTERED {
		w.SetPosition(x, y)
	} else {
		w.SetPosition(WINDOWPOS_CENTERED, WINDOWPOS_CENTERED)
	}
	return w, nil
}

// CreateTransparentWindow creates a window whose framebuffer alpha
// composites — SDL3's replacement for SDL2's shaped windows, and the
// mechanism rounded corners now rely on.
func CreateTransparentWindow(title string, x, y int32, width, height int, flags WindowFlags) (*Window, error) {
	return CreateWindow(title, x, y, width, height, flags|WINDOW_TRANSPARENT)
}

func (w *Window) Destroy()               { w.w.Destroy() }
func (w *Window) SetTitle(title string)  { _ = w.w.SetTitle(title) }
func (w *Window) SetPosition(x, y int32) { _ = w.w.SetPosition(int32(x), int32(y)) }

func (w *Window) Position() (int32, int32) {
	x, y, err := w.w.Position()
	if err != nil {
		return 0, 0
	}
	return x, y
}

func (w *Window) Size() (int32, int32) {
	width, height, err := w.w.Size()
	if err != nil {
		return 0, 0
	}
	return width, height
}

func (w *Window) SetSize(width, height int32) { _ = w.w.SetSize(width, height) }

// SizeInPixels returns the client area in real device pixels (SDL screen
// coordinates are points on a HiDPI display; the framebuffer/swapchain is sized
// in pixels). Falls back to Size() if the pixel query fails.
func (w *Window) SizeInPixels() (int32, int32) {
	width, height, err := w.w.SizeInPixels()
	if err != nil {
		return w.Size()
	}
	return width, height
}

func (w *Window) ID() (uint32, error) {
	id, err := w.w.ID()
	return uint32(id), err
}

func (w *Window) Flags() WindowFlags         { return w.w.Flags() }
func (w *Window) Minimize()                  { _ = w.w.Minimize() }
func (w *Window) Restore()                   { _ = w.w.Restore() }
func (w *Window) Raise()                     { _ = w.w.Raise() }
func (w *Window) SetBordered(bordered bool)  { _ = w.w.SetBordered(bordered) }
func (w *Window) SetOpacity(o float32) error { return w.w.SetOpacity(o) }

// GetDisplayIndex reports the display this window is on. SDL3 returns a
// DisplayID rather than an index; callers only use it to look the
// display's bounds back up, so the ID serves the same purpose.
func (w *Window) Display() (int, error) {
	id := csdl.GetDisplayForWindow(w.w)
	return int(id), nil
}

// --- displays ---

type Rect = csdl.Rect

// GetDisplayUsableBounds returns the work area of a display (the screen
// minus the menu bar and Dock), keyed by the value GetDisplayIndex
// returned.
func GetDisplayUsableBounds(display int) (Rect, error) {
	r, err := csdl.DisplayID(display).UsableBounds()
	if err != nil {
		return Rect{}, err
	}
	return *r, nil
}

// --- surfaces (shape masks) ---

type Surface = csdl.Surface

const (
	PIXELFORMAT_ARGB8888 = csdl.PIXELFORMAT_ARGB8888
	PIXELFORMAT_ABGR8888 = csdl.PIXELFORMAT_ABGR8888
)

// CreateSurface allocates an off-screen surface — the host uses one as
// a window shape mask.
func CreateSurface(width, height int32, format csdl.PixelFormat) (*Surface, error) {
	return csdl.CreateSurface(int(width), int(height), format)
}

// FreeSurface releases a surface.
func FreeSurface(s *Surface) { s.Destroy() }

// SetShape applies an alpha mask to a transparent window. SDL3 requires
// the window to have been created with WINDOW_TRANSPARENT.
func (w *Window) SetShape(shape *Surface) error {
	return w.w.SetShape(shape)
}

// --- mouse / cursors ---

type Cursor = csdl.Cursor
type SystemCursor = csdl.SystemCursor

// Cursor shapes, in SDL3's spelling (SDL2 called these ARROW, IBEAM
// and SIZE*).
const (
	SYSTEM_CURSOR_DEFAULT     = csdl.SYSTEM_CURSOR_DEFAULT
	SYSTEM_CURSOR_TEXT        = csdl.SYSTEM_CURSOR_TEXT
	SYSTEM_CURSOR_EW_RESIZE   = csdl.SYSTEM_CURSOR_EW_RESIZE
	SYSTEM_CURSOR_NS_RESIZE   = csdl.SYSTEM_CURSOR_NS_RESIZE
	SYSTEM_CURSOR_NWSE_RESIZE = csdl.SYSTEM_CURSOR_NWSE_RESIZE
	SYSTEM_CURSOR_NESW_RESIZE = csdl.SYSTEM_CURSOR_NESW_RESIZE
)

func CreateSystemCursor(id SystemCursor) (*Cursor, error) { return csdl.CreateSystemCursor(id) }
func SetCursor(c *Cursor) error                           { return csdl.SetCursor(c) }
func CaptureMouse(enabled bool) error                     { return csdl.CaptureMouse(enabled) }

const (
	BUTTON_LEFT   = 1
	BUTTON_MIDDLE = 2
	BUTTON_RIGHT  = 3

	// ButtonLeftMask is the held-buttons bit for the left button.
	ButtonLeftMask = 1 << 0
)

// GetGlobalMouseState reports the pointer in desktop coordinates. SDL3
// returns floats; the platform layer works in whole pixels.
func GetGlobalMouseState() (int32, int32, uint32) {
	state, x, y := csdl.GetGlobalMouseState()
	return int32(x), int32(y), uint32(state)
}

// GetMouseState reports the pointer relative to the focused window.
func GetMouseState() (int32, int32, uint32) {
	state, x, y := csdl.GetMouseState()
	return int32(x), int32(y), uint32(state)
}

// --- keyboard ---

type Keycode = csdl.Keycode

// Keysym keeps SDL2's shape: SDL3 puts the key and modifiers directly
// on the event, so this is assembled when the event is translated.
type Keysym struct {
	Sym Keycode
	Mod uint16
}

const (
	KMOD_LSHIFT = uint16(csdl.KMOD_LSHIFT)
	KMOD_RSHIFT = uint16(csdl.KMOD_RSHIFT)
	KMOD_SHIFT  = uint16(csdl.KMOD_SHIFT)
	KMOD_LCTRL  = uint16(csdl.KMOD_LCTRL)
	KMOD_RCTRL  = uint16(csdl.KMOD_RCTRL)
	KMOD_CTRL   = uint16(csdl.KMOD_CTRL)
	KMOD_LALT   = uint16(csdl.KMOD_LALT)
	KMOD_RALT   = uint16(csdl.KMOD_RALT)
	KMOD_ALT    = uint16(csdl.KMOD_ALT)
	KMOD_LGUI   = uint16(csdl.KMOD_LGUI)
	KMOD_RGUI   = uint16(csdl.KMOD_RGUI)
	KMOD_GUI    = uint16(csdl.KMOD_GUI)
	KMOD_MODE   = uint16(csdl.KMOD_MODE) // AltGr / ISO_Level3_Shift (the Glyph modifier)
)

func GetModState() uint16 { return uint16(csdl.GetModState()) }

const (
	K_RETURN    = csdl.K_RETURN
	K_ESCAPE    = csdl.K_ESCAPE
	K_BACKSPACE = csdl.K_BACKSPACE
	K_TAB       = csdl.K_TAB
	K_DELETE    = csdl.K_DELETE
	K_INSERT    = csdl.K_INSERT
	K_HOME      = csdl.K_HOME
	K_END       = csdl.K_END
	K_PAGEUP    = csdl.K_PAGEUP
	K_PAGEDOWN  = csdl.K_PAGEDOWN
	K_UP        = csdl.K_UP
	K_DOWN      = csdl.K_DOWN
	K_LEFT      = csdl.K_LEFT
	K_RIGHT     = csdl.K_RIGHT
	K_EQUALS    = csdl.K_EQUALS
	K_PLUS      = csdl.K_PLUS
	K_MINUS     = csdl.K_MINUS
	K_0         = csdl.K_0
	K_a         = csdl.K_A
	K_r         = csdl.K_R
	K_KP_0      = csdl.K_KP_0
	K_KP_PLUS   = csdl.K_KP_PLUS
	K_KP_MINUS  = csdl.K_KP_MINUS
	K_KP_ENTER  = csdl.K_KP_ENTER
	K_F1        = csdl.K_F1
	K_F2        = csdl.K_F2
	K_F3        = csdl.K_F3
	K_F4        = csdl.K_F4
	K_F5        = csdl.K_F5
	K_F6        = csdl.K_F6
	K_F7        = csdl.K_F7
	K_F8        = csdl.K_F8
	K_F9        = csdl.K_F9
	K_F10       = csdl.K_F10
	K_F11       = csdl.K_F11
	K_F12       = csdl.K_F12
)

// StartTextInput enables text events. SDL3 scopes it to a window, and
// leaves it OFF until asked — unlike SDL2, whose global
// SDL_StartTextInput() was on by default. Every window needs its own
// call or it receives key events but no typed text.
func StartTextInput(w *Window) error { return w.w.StartTextInput() }

// TextInputActive reports whether text events are enabled for a window.
// Exists so a test can prove every window got StartTextInput, rather
// than the absence showing up as "typing does nothing" in one window.
func TextInputActive(w *Window) bool { return w.w.TextInputActive() }

// SetTextInputArea tells the OS where the caret is, in WINDOW pixels, so
// an input method can put its candidate window under the text being
// typed instead of at some default corner. cursorPx is the caret's
// offset from the rect's left edge.
//
// This is what places the CJK candidate list, the macOS press-and-hold
// accent picker, and the emoji picker. SDL2 had the global
// SDL_SetTextInputRect; SDL3 scopes it to a window and adds the cursor
// offset, the same move it made with StartTextInput.
func SetTextInputArea(w *Window, xPx, yPx, wPx, hPx, cursorPx int) error {
	r := csdl.Rect{X: int32(xPx), Y: int32(yPx), W: int32(wPx), H: int32(hPx)}
	return w.w.SetTextInputArea(&r, int32(cursorPx))
}

// ClearTextInputArea forgets the caret position, for a surface with no
// caret to report. An input method then falls back to its own placement
// rather than anchoring on a stale rectangle.
func ClearTextInputArea(w *Window) error { return w.w.SetTextInputArea(nil, 0) }

// --- clipboard ---

func SetClipboardText(text string) error { return csdl.SetClipboardText(text) }
func GetClipboardText() (string, error)  { return csdl.GetClipboardText() }

// --- events ---
//
// SDL3 replaced SDL2's WINDOWEVENT-with-a-subtype scheme with distinct
// event types. The platform layer still switches on SDL2's shapes, so
// PollEvent translates: a resized/focus/leave event becomes a
// WindowEvent carrying the matching SDL2 subtype.

// Event kinds. These are this package's own values, NOT SDL constants:
// SDL3 encodes them in its event type, which PollEvent has already
// consumed by the time a caller sees a typed event. They are spelled
// unlike SDL's names on purpose — an invented constant wearing SDL's
// spelling is how clicks were once silently dropped, by being compared
// against SDL's raw event number.
const (
	KeyDown = iota + 1
	KeyUp
	MouseDown
	MouseUp
)

// Window event kinds, likewise this package's own.
const (
	WindowResized = iota + 1
	WindowFocusGained
	WindowFocusLost
	WindowMouseLeave
)

// Event is any translated SDL event.
type Event interface{ isEvent() }

type QuitEvent struct{}

type WindowEvent struct {
	WindowID     uint32
	Event        uint8
	Data1, Data2 int32
}

type KeyboardEvent struct {
	Type     uint32
	WindowID uint32
	Keysym   Keysym
}

type TextInputEvent struct {
	WindowID uint32
	text     string
}

func (e *TextInputEvent) GetText() string { return e.text }

// TextEditingEvent is one update to an input method's in-flight
// composition (SDL_EVENT_TEXT_EDITING): text that is being typed but has
// not been committed. The whole composition arrives every time, never a
// delta, and empty text ends it.
//
// Start/Length are the input method's selection within the text, or -1
// when it reports none; core.PreeditFrom is where they are interpreted.
type TextEditingEvent struct {
	WindowID uint32
	text     string
	Start    int32
	Length   int32
}

func (e *TextEditingEvent) GetText() string { return e.text }

type MouseButtonEvent struct {
	Type     uint32
	WindowID uint32
	Button   uint8
	State    uint8
	X, Y     int32
}

type MouseMotionEvent struct {
	WindowID uint32
	X, Y     int32
	State    uint32
}

type MouseWheelEvent struct {
	WindowID uint32
	X, Y     int32
	PreciseX float32
	PreciseY float32
}

func (*QuitEvent) isEvent()        {}
func (*WindowEvent) isEvent()      {}
func (*KeyboardEvent) isEvent()    {}
func (*TextInputEvent) isEvent()   {}
func (*TextEditingEvent) isEvent() {}
func (*MouseButtonEvent) isEvent() {}
func (*MouseMotionEvent) isEvent() {}
func (*MouseWheelEvent) isEvent()  {}

// PollEvent returns the next translated event, or nil when the queue is
// empty.
func PollEvent() Event {
	var ev csdl.Event
	if !csdl.PollEvent(&ev) {
		return nil
	}
	return translate(&ev)
}

// translate maps one SDL3 event onto the SDL2-shaped value the platform
// layer switches on. Unhandled event types return nil, which the caller
// treats as "nothing for me" rather than "queue empty" — PollEvent's
// contract is preserved because the platform loop drains until nil and
// SDL3 delivers many more event types than SDL2 did.
func translate(ev *csdl.Event) Event {
	switch ev.Type {
	case csdl.EVENT_QUIT:
		return &QuitEvent{}

	case csdl.EVENT_WINDOW_RESIZED, csdl.EVENT_WINDOW_PIXEL_SIZE_CHANGED:
		w := ev.WindowEvent()
		return &WindowEvent{
			WindowID: uint32(w.WindowID),
			Event:    WindowResized,
			Data1:    w.Data1,
			Data2:    w.Data2,
		}
	case csdl.EVENT_WINDOW_FOCUS_GAINED:
		w := ev.WindowEvent()
		return &WindowEvent{WindowID: uint32(w.WindowID), Event: WindowFocusGained}
	case csdl.EVENT_WINDOW_FOCUS_LOST:
		w := ev.WindowEvent()
		return &WindowEvent{WindowID: uint32(w.WindowID), Event: WindowFocusLost}
	case csdl.EVENT_WINDOW_MOUSE_LEAVE:
		w := ev.WindowEvent()
		return &WindowEvent{WindowID: uint32(w.WindowID), Event: WindowMouseLeave}

	case csdl.EVENT_KEY_DOWN, csdl.EVENT_KEY_UP:
		k := ev.KeyboardEvent()
		typ := uint32(KeyDown)
		if ev.Type == csdl.EVENT_KEY_UP {
			typ = KeyUp
		}
		return &KeyboardEvent{
			Type:     typ,
			WindowID: uint32(k.WindowID),
			Keysym:   Keysym{Sym: k.Key, Mod: uint16(k.Mod)},
		}

	case csdl.EVENT_TEXT_INPUT:
		t := ev.TextInputEvent()
		return &TextInputEvent{WindowID: uint32(t.WindowID), text: t.Text}

	case csdl.EVENT_TEXT_EDITING:
		t := ev.TextEditingEvent()
		return &TextEditingEvent{
			WindowID: uint32(t.WindowID),
			text:     t.Text,
			Start:    t.Start,
			Length:   t.Length,
		}

	case csdl.EVENT_MOUSE_BUTTON_DOWN, csdl.EVENT_MOUSE_BUTTON_UP:
		m := ev.MouseButtonEvent()
		typ, state := uint32(MouseUp), uint8(0)
		if ev.Type == csdl.EVENT_MOUSE_BUTTON_DOWN {
			typ, state = MouseDown, 1
		}
		return &MouseButtonEvent{
			Type:     typ,
			WindowID: uint32(m.WindowID),
			Button:   m.Button,
			State:    state,
			X:        int32(m.X),
			Y:        int32(m.Y),
		}

	case csdl.EVENT_MOUSE_MOTION:
		m := ev.MouseMotionEvent()
		return &MouseMotionEvent{
			WindowID: uint32(m.WindowID),
			X:        int32(m.X),
			Y:        int32(m.Y),
			State:    uint32(m.State),
		}

	case csdl.EVENT_MOUSE_WHEEL:
		m := ev.MouseWheelEvent()
		// SDL3 split what SDL2 called X/Y: its X/Y are FRACTIONAL
		// amounts (SDL2's PreciseX/Y), and the whole-tick counts SDL2
		// put in X/Y are now IntegerX/IntegerY. Truncating the float
		// instead would round every sub-tick trackpad scroll to zero.
		return &MouseWheelEvent{
			WindowID: uint32(m.WindowID),
			X:        m.IntegerX,
			Y:        m.IntegerY,
			PreciseX: m.X,
			PreciseY: m.Y,
		}
	}
	return nil
}

// AddEventWatchFunc installs a callback run as events arrive, which is
// how the host keeps painting during macOS's modal resize loop.
func AddEventWatchFunc(fn func(Event, interface{}) bool, userdata interface{}) {
	// SDL3's event filter is a raw C function pointer, so the Go
	// callback has to be trampolined. purego.NewCallback allocates a
	// permanent trampoline, which suits a watch installed once for the
	// process's lifetime.
	cb := purego.NewCallback(func(_ uintptr, ev *csdl.Event) uintptr {
		if translated := translate(ev); translated != nil {
			fn(translated, userdata)
		}
		return 1
	})
	_ = csdl.AddEventWatch(csdl.EventFilter(cb))
}

// --- renderer / textures ---
//
// SDL3 reworked this API: the renderer is created by driver NAME rather
// than by flags (vsync is a separate call), textures blit through
// RenderTexture with float rects, and "accelerated vs software" is a
// driver choice rather than a flag bit.

type Renderer struct {
	r *csdl.Renderer
}

type Texture struct {
	t *csdl.Texture
	w int32
	h int32
	f csdl.PixelFormat
}

const TEXTUREACCESS_STREAMING = csdl.TEXTUREACCESS_STREAMING

// CreateRenderer makes a renderer for the window. SDL3 has no flag
// word: the driver is chosen by name (empty picks the best available)
// and vsync is a separate call, so these are plain options rather than
// SDL2's flag bits.
func CreateRenderer(w *Window, driver string, vsync bool) (*Renderer, error) {
	r, err := w.w.CreateRenderer(driver)
	if err != nil {
		return nil, err
	}
	if vsync {
		_ = r.SetVSync(1)
	}
	return &Renderer{r: r}, nil
}

func (r *Renderer) Destroy() { r.r.Destroy() }

func (r *Renderer) CreateTexture(format csdl.PixelFormat, access csdl.TextureAccess, w, h int32) (*Texture, error) {
	t, err := r.r.CreateTexture(format, access, int(w), int(h))
	if err != nil {
		return nil, err
	}
	return &Texture{t: t, w: w, h: h, f: format}, nil
}

func (r *Renderer) SetDrawColor(red, g, b, a uint8) error {
	return r.r.SetDrawColor(red, g, b, a)
}

func (r *Renderer) Clear() error   { return r.r.Clear() }
func (r *Renderer) Present() error { return r.r.Present() }

// Copy blits a whole texture over the whole render target — the only
// form the host uses. SDL3's rects are floats.
func (r *Renderer) Copy(t *Texture, src, dst *Rect) error {
	return r.r.RenderTexture(t.t, nil, nil)
}

func (t *Texture) Destroy() { t.t.Destroy() }

// Update uploads pixels into a streaming texture. The host passes the
// whole surface, so the rect is always nil.
func (t *Texture) Update(rect *Rect, pixels []byte, pitch int) error {
	return t.t.Update(nil, pixels, int32(pitch))
}

// Format reports the pixel format the texture was created with. SDL3
// dropped SDL2's combined query, so it is remembered at creation.
func (t *Texture) Format() uint32 { return uint32(t.f) }

// --- native handles ---
//
// SDL3 replaced SDL_GetWindowWMInfo with typed window properties.

// MetalLayer returns the CAMetalLayer for a window, creating the
// backing Metal view on first use (macOS/iOS only; nil elsewhere).
func (w *Window) MetalLayer() unsafe.Pointer {
	view := metalCreateView(uintptr(unsafe.Pointer(w.w)))
	if view == 0 {
		return nil
	}
	return metalGetLayer(view)
}

// CocoaWindow returns the NSWindow handle for a window, or 0 off
// macOS. It stays a uintptr: the value is an opaque Objective-C object
// the Cocoa shim passes straight back to the runtime, and turning it
// into an unsafe.Pointer here would claim it is Go-visible memory.
func (w *Window) CocoaWindow() uintptr {
	props, err := w.w.Properties()
	if err != nil {
		return 0
	}
	return pointerProperty(uint32(props), "SDL.window.cocoa.window")
}

// X11Handles returns the X11 Display* and Window id, or (0,0) when the
// window is not an X11 window.
func (w *Window) X11Handles() (uintptr, uintptr) {
	props, err := w.w.Properties()
	if err != nil {
		return 0, 0
	}
	display := pointerProperty(uint32(props), "SDL.window.x11.display")
	window := uintptr(props.NumberProperty("SDL.window.x11.window", 0))
	return display, window
}

// Win32HWND returns the HWND for a window, or 0 off Windows.
func (w *Window) Win32HWND() uintptr {
	props, err := w.w.Properties()
	if err != nil {
		return 0
	}
	return pointerProperty(uint32(props), "SDL.window.win32.hwnd")
}
