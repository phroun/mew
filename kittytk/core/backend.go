// Package core provides fundamental types for KittyTK.
package core

import (
	"image"
	"math"

	"github.com/phroun/kittytk/style"
)

// RenderBackend abstracts the rendering target.
// Implementations exist for text terminals, and could be added for
// graphics (SDL, OpenGL, Canvas, WebGL, etc.).
type RenderBackend interface {
	// Lifecycle
	Init() error
	Shutdown()

	// Size returns the current size in abstract units.
	Size() UnitSize

	// CellMetrics returns the metrics for this backend.
	// For TUI, this defines how units map to character cells.
	// For GUI, this might be 1:1 with pixels or scaled.
	Metrics() CellMetrics

	// BeginFrame starts a new frame for rendering.
	BeginFrame()

	// EndFrame completes the frame and presents it.
	EndFrame()

	// Clear fills the entire surface with a style.
	Clear(s style.CellStyle)

	// SetClip sets the clipping rectangle. All drawing operations
	// will be clipped to this region. Pass empty rect to disable clipping.
	SetClip(clip UnitRect)

	// Drawing primitives (all coordinates in abstract units)

	// DrawCell draws a single character at the given position.
	DrawCell(x, y Unit, ch rune, s style.CellStyle)

	// DrawText draws a string starting at the given position using the given font.
	// If font is nil, uses DefaultFont().
	// Returns the width consumed in units.
	DrawText(x, y Unit, text string, s style.CellStyle, font *Font) Unit

	// DrawTextAligned draws text aligned within a box using the given font.
	// If font is nil, uses DefaultFont().
	DrawTextAligned(bounds UnitRect, text string, hAlign, vAlign Alignment, s style.CellStyle, font *Font)

	// FillRect fills a rectangle with a character and style.
	FillRect(r UnitRect, ch rune, s style.CellStyle)

	// DrawRect draws just the border of a rectangle.
	DrawRect(r UnitRect, border style.BorderStyle, s style.CellStyle)

	// DrawHLine draws a horizontal line using border style.
	DrawHLine(x, y, width Unit, ch rune, s style.CellStyle)

	// DrawVLine draws a vertical line using border style.
	DrawVLine(x, y, height Unit, ch rune, s style.CellStyle)

	// DrawBox draws a box with optional title.
	DrawBox(r UnitRect, border style.BorderStyle, title string, s style.CellStyle)

	// Input handling

	// PollEvent returns the next input event, or nil if none available.
	// This is non-blocking.
	PollEvent() Event

	// WaitEvent blocks until an event is available.
	WaitEvent() Event

	// SetCursorVisible shows or hides the cursor.
	SetCursorVisible(visible bool)

	// SetCursorPosition positions the cursor (for text input feedback).
	SetCursorPosition(x, y Unit)

	// SetCursorStyle selects the cursor's DECSCUSR shape (0 the terminal's
	// own default, 1/2 blinking/steady block, 3/4 underline, 5/6 bar).
	// Backends without a real cursor ignore it.
	SetCursorStyle(style int)

	// Capabilities

	// SupportsColor returns whether the backend supports color.
	SupportsColor() bool

	// SupportsMouse returns whether the backend supports mouse input.
	SupportsMouse() bool

	// SupportsUnicode returns whether the backend supports Unicode.
	SupportsUnicode() bool

	// ColorDepth returns the number of colors supported (2, 16, 256, or 16777216 for true color).
	ColorDepth() int

	// Clipboard operations

	// GetClipboard returns the current clipboard contents.
	GetClipboard() string

	// SetClipboard sets the clipboard contents.
	SetClipboard(text string)

	// System

	// Beep produces an audible alert.
	Beep()
}

// AsyncClipboardReader is an optional RenderBackend capability for surfaces
// whose clipboard read is asynchronous - a terminal answering an OSC 52 query
// may prompt the user for permission or otherwise take an unbounded time. The
// desktop uses it to drive a "waiting for clipboard" affordance instead of
// blocking the event loop. Backends whose read is instant (SDL) omit it, and
// callers use the synchronous GetClipboard.
type AsyncClipboardReader interface {
	// RequestClipboardRead asks the host/terminal for its clipboard. It returns
	// false when an async read isn't available or applicable right now (the
	// caller should fall back to GetClipboard); when true, the handler set via
	// SetClipboardReadHandler will be invoked with the reply if/when it arrives
	// (it may never arrive - the caller decides how long to wait).
	RequestClipboardRead() bool

	// SetClipboardReadHandler registers the single callback invoked (possibly
	// on another goroutine) when a clipboard response arrives.
	SetClipboardReadHandler(func(text string))
}

// SmoothPositioner is an optional RenderBackend capability: true
// when the surface can place window chrome at arbitrary unit
// positions (pixel surfaces). Cell-only surfaces (terminals) omit it
// - their painting quantizes to the cell grid, so drag/resize must
// snap to keep hit-testing and pixels aligned.
type SmoothPositioner interface {
	SmoothPositioning() bool
}

// SmoothPositioningProvider is the trinket-side carrier of the same
// capability: window-manager hosts stamp it onto the windows they
// manage, and nested window hosts (MDI panes) discover it by walking
// their ancestry with FindSmoothPositioning.
type SmoothPositioningProvider interface {
	SmoothWindowPositioning() bool
}

// RoundedRectDrawer is an optional RenderBackend capability: pixel
// surfaces paint a filled, stroked rounded rectangle in a single
// pass - the fill in the style's background color, the stroke in its
// foreground, with the stroke weight taken from the border style
// (2 device pixels for double, 1 for single). Window frames use this
// as their entire graphical surface; cell surfaces omit it and
// frames fall back to box-drawing runes.
//
// StrokeRoundedRect paints only the stroke, leaving the interior
// untouched: window frames re-stroke over their content, because
// graphical content extends to the window edge (only the titlebar
// reserves a full row - the hairline border shares its boundary
// pixels with the content beneath it).
type RoundedRectDrawer interface {
	DrawRoundedRect(r UnitRect, radius Unit, border style.BorderStyle, s style.CellStyle)
	StrokeRoundedRect(r UnitRect, radius Unit, border style.BorderStyle, s style.CellStyle)
}

// RoundedRectWeightStroker is an optional RenderBackend capability: stroke
// a rounded rectangle with an explicit device-pixel weight instead of the
// fixed border-style weight. Used for the thin inner line of a
// single-border (active-but-not-focused) window frame, whose weight tracks
// the tabbed control's tab stroke rather than the frame border.
type RoundedRectWeightStroker interface {
	StrokeRoundedRectWeight(r UnitRect, radius Unit, strokePx int, s style.CellStyle)
}

// TranslucentPixelFiller is an optional RenderBackend capability: fill a
// device-pixel rectangle with a color at partial opacity, blended over
// the existing pixels and respecting the clip (including a rounded clip
// region). The resize-edge hover highlight uses it. Cell surfaces omit it.
type TranslucentPixelFiller interface {
	FillRectPxAlpha(xPx, yPx, wPx, hPx int, r, g, b uint8, alpha float64)
}

// ArcWedgeDrawer is an optional RenderBackend capability: fill the
// part of a rect lying outside the quarter ellipse inscribed in it
// and centered on the chosen corner - antialiased - painting the fill
// in the style's background and an optional stroke of the given
// weight along the arc in its foreground (0 = no stroke). The tab
// strip's silhouette corners use this; cell surfaces omit it and
// callers fall back to scanline fills.
type ArcWedgeDrawer interface {
	DrawArcWedge(r UnitRect, centerRight, centerBottom bool, strokeW Unit, offXPx, offYPx int, s style.CellStyle)
}

// ImageDrawer is an optional RenderBackend capability: composite a
// raster image onto the surface. The image is in DEVICE pixels
// (callers render at the surface's scale); alpha is honored
// (Porter-Duff over). DrawImage anchors at a unit position;
// DrawImagePx anchors at a device pixel for sub-unit placement
// (sprite fine positioning, animation offsets). The carrier for
// PurfecTerm's sprites and custom glyphs, and any trinket with image
// content.
type ImageDrawer interface {
	DrawImage(x, y Unit, img image.Image)
	DrawImagePx(xPx, yPx int, img image.Image)
}

// CellPixelSizer is an optional RenderBackend capability reporting how many
// DEVICE PIXELS one cell of this surface covers — which a cell surface knows
// only if it asked, and only a terminal host has anyone to ask.
//
// A graphical surface derives its cell from its own font and never needs this.
// A TUI host does: it draws into somebody else's terminal, whose cell size is
// that terminal's business, and it finds out by querying (CSI 16 t). The number
// matters beyond its own layout, because a program hosted INSIDE such a surface
// asks the same question of us and can do nothing about pictures without an
// answer — no image can be sized, positioned, or scaled without it.
//
// Zero means the question has not been answered, which is the honest state
// before the reply arrives and on a terminal that never sends one.
type CellPixelSizer interface {
	CellPixelSize() (w, h int)
}

// MotionTracker is an optional RenderBackend capability: a surface that has to
// ASK to be told about pointer motion.
//
// A graphical host is given every mouse move whether it wants one or not. A
// terminal host is given only what it asked its own terminal for, and asking
// for motion is a separate mode (?1003) from asking for clicks — so a trinket
// that needs to follow the pointer without a button held has to say so, every
// frame it still needs it. Hover is the obvious case; a hosted program that
// turned on its own motion tracking is the one that made this necessary,
// because the events it is waiting for do not otherwise exist.
//
// Requests are per FRAME and not sticky: a surface stops asking and the mode
// goes away, which keeps a busy wire quiet when nothing is watching.
type MotionTracker interface {
	RequestMotionTracking()
}

// MaskTintDrawer is an optional RenderBackend capability: composite a
// color-independent coverage mask (only its alpha is read) tinted with a solid
// color. Lets a caller cache one grayscale glyph per shape and recolor it per
// draw, so color-varying content doesn't re-rasterize a glyph per color.
type MaskTintDrawer interface {
	DrawImageMaskTintPx(xPx, yPx int, mask *image.RGBA, r, g, b uint8)
}

// DeviceScaler is an optional RenderBackend capability reporting the
// device zoom: how many device pixels one unit covers at the base font
// size (the raster backend's integer scale). Chrome that wants a
// physical hairline weight uses it; geometry that must track font_size
// uses UnitPixelMapper instead.
type DeviceScaler interface {
	Scale() int
}

// DisplayDensityReporter is an optional RenderBackend capability reporting the
// PHYSICAL display's content scale — 2 on a HiDPI panel, 1 on an ordinary one.
//
// This is emphatically not DeviceScaler. That one is how much this application
// magnifies itself, a preference; this one is a fact about the screen, and the
// two are independent (a user may well ask for 1 on a HiDPI panel, which is
// exactly the case that tells them apart).
//
// It matters because a SEPARATE process rendering pictures for us — a browser
// in a terminal pane — reads the display's density from the window system
// itself and sizes its content to it, entirely outside our sight. Nothing in
// the terminal protocols carries that number in either direction, so the only
// way to end up agreeing with such a child is to learn the same fact it did.
// Deriving it from our own magnification instead looks right exactly when the
// two happen to be equal.
type DisplayDensityReporter interface {
	DisplayDensity() float64
}

// UnitPixelMapper is an optional RenderBackend capability exposing the
// backend's true (font_size-aware, possibly fractional and cell-snapped)
// unit-to-device-pixel mapping, so the Painter's device-pixel helpers
// place sub-unit fills exactly where the backend's own geometry lands.
// Without it the Painter falls back to integer unit*DeviceScale.
type UnitPixelMapper interface {
	// PxPerUnit is the unsnapped device pixels per unit (for lengths).
	PxPerUnit() float64
	// UnitToPxX / UnitToPxY are the cell-snapped conversions of a unit
	// position on each axis (for anchors).
	UnitToPxX(Unit) int
	UnitToPxY(Unit) int
}

// UnitPixelUnmapper is the inverse of UnitToPxX/Y: it converts a device
// pixel extent back to whole units on the SAME hardened cell pitch,
// rounding to nearest so the round-trip is exact. Geometry that OWNS a
// surface's pixel size (a torn window sized to UnitToPxX(W)) reads it back
// through this so the unit size never drifts on re-sizing. The raster
// backend implements it; callers fall back to round(px / PxPerUnit) when a
// backend does not.
type UnitPixelUnmapper interface {
	PxToUnitX(int) Unit
	PxToUnitY(int) Unit
}

// GraphicalModer is the D1 mode query: a backend reports true when
// it paints pixels rather than character cells. Trinkets branch their
// rendering on Painter.Graphical() - e.g. label-type text passes
// style.ColorTransparent backgrounds only on graphical targets,
// where glyphs can blend over existing pixels.
type GraphicalModer interface {
	GraphicalMode() bool
}

// SurfaceClearer is an optional RenderBackend capability: reset pixels
// to fully transparent, WITHIN THE CLIP. A compositing host uses it
// before painting a layer meant to sit over something else — the
// desktop's chrome layer clears this way so the GPU-tiled wallpaper
// underneath shows through everywhere the chrome does not paint.
//
// Honoring the clip is the whole contract. A frame repainting only its
// damaged region gets a clipped painter, and a clear that ignored that
// would erase the chrome outside the region and then not repaint it —
// the menu bar and status bar flickering out, with the wallpaper showing
// through where they had been.
//
// Cell surfaces have no alpha and omit it.
type SurfaceClearer interface {
	ClearTransparent()
}

// ImageTiler is an optional RenderBackend capability: lay an image
// across a rect as a WallpaperLayout describes — sized by its mode and
// scale, anchored by its alignment, repeated along the axes it tiles.
// It is the CPU counterpart of the compositor's repeat-sampled wallpaper
// quad, for the software renderer and for any host that does not take
// the wallpaper as a layer of its own.
type ImageTiler interface {
	TileImagePx(r UnitRect, tile *image.RGBA, layout WallpaperLayout)
}

// PatternFiller is an optional RenderBackend capability: tile an 8x8
// two-color bitmap pattern across a rect (classic MacOS desktop
// style). Each pattern bit covers chunkPx x chunkPx device pixels
// (set = foreground, clear = background); the pattern is anchored at
// the surface origin so it does not swim as rects move. Cell
// surfaces omit it and callers fall back to rune fills.
type PatternFiller interface {
	FillPattern(r UnitRect, pattern [8]uint8, chunkPx int, s style.CellStyle)
}

// RoundedClipper is an optional RenderBackend capability: an
// additional clip constraint shaped as a rounded rectangle,
// composing with the rectangular SetClip (a pixel paints only if it
// passes both). A zero rect clears it. Window frames confine their
// edge-to-edge content with this so nothing paints past the rounded
// corners.
type RoundedClipper interface {
	SetRoundedClip(r UnitRect, radius Unit)
}

// GraphicalFrameProvider is the trinket-side carrier of the frame
// mode: the desktop reports true when its backend paints rounded
// window frames, and windows discover it by walking their ancestry
// with FindGraphicalFrames. It governs the client-area contract: on
// graphical frames the content area extends to the window's left,
// right, and bottom edges (only the titlebar reserves a full row);
// on cell frames the border occupies a full cell on every side.
type GraphicalFrameProvider interface {
	GraphicalWindowFrames() bool
}

// FindGraphicalFrames walks up the trinket tree for a
// GraphicalFrameProvider. Default (no provider found): false - the
// cell-frame client area, the only always-safe answer.
func FindGraphicalFrames(w Trinket) bool {
	for current := Trinket(w); current != nil; {
		if p, ok := current.(GraphicalFrameProvider); ok {
			return p.GraphicalWindowFrames()
		}
		parent := current.Parent()
		if parent == nil {
			return false
		}
		current = parent
	}
	return false
}

// windowFrameBorderPx is the configured graphical window-frame border
// width in device pixels (0 = the built-in default). Set by the host from
// the ini's border_width. It is read both by the raster backend (to
// stroke the frame) and, converted to units, by the window layout (to
// reserve the border outside the content coordinate system).
var windowFrameBorderPx int

// SetWindowFrameBorderPx sets the graphical window-frame border width in
// device pixels; 0 (or negative) restores the default.
func SetWindowFrameBorderPx(px int) {
	if px < 0 {
		px = 0
	}
	windowFrameBorderPx = px
}

// WindowFrameBorderPx returns the configured frame border width at the
// BASE zoom (pixels-per-unit == 1, i.e. font 12 / scale 1) in device
// pixels - the configured value, or the built-in default (2) when unset.
// This is the thickness before zoom scaling; consumers that paint or
// reserve the border use ScaledWindowFrameBorderPx to apply the zoom.
func WindowFrameBorderPx() int {
	if windowFrameBorderPx > 0 {
		return windowFrameBorderPx
	}
	return defaultWindowFrameBorderPx
}

// ScaledWindowFrameBorderPx is the frame border's effective device-pixel
// thickness at the given pixels-per-unit: the base-zoom width scaled by
// zoom, per the geometry model's border law (a) —
//
//	border_px = round(border_width × pixels-per-unit)
//
// (geometry-cells-units-pixels.md). A fixed pixel count would look
// proportionally thinner as the font zooms in; scaling keeps the border a
// constant fraction of the content. The single desktop ppu makes this one
// value physically uniform on every window, hardened once per zoom. Never
// below 1px so the stroke is always visible.
func ScaledWindowFrameBorderPx(ppu float64) int {
	if ppu <= 0 {
		ppu = 1
	}
	n := int(math.Round(float64(WindowFrameBorderPx()) * ppu))
	if n < 1 {
		n = 1
	}
	return n
}

// defaultWindowFrameBorderPx is the built-in frame stroke weight.
const defaultWindowFrameBorderPx = 2

// titleBarScale scales every GRAPHICAL title bar's height and content:
// 1.0 is the classic full-cell row; 0.9 renders the bar at 90% of it —
// ceiled to a full device pixel — with the fonts and controls scaled to
// match. Cell (terminal) surfaces cannot subdivide a character cell and
// always render at 1.0 regardless. Read by the title-bar kit
// (objects/window/titlebar.go), which is the only place bars measure.
var titleBarScale = 1.0

// SetTitleBarScale sets the graphical title-bar scale; values at or below
// zero restore 1.0.
func SetTitleBarScale(s float64) {
	if s <= 0 {
		s = 1
	}
	titleBarScale = s
}

// TitleBarScale returns the current graphical title-bar scale.
func TitleBarScale() float64 { return titleBarScale }

// menuScale scales every GRAPHICAL menu's rows and their contents: the menu
// bar, the dropdowns it opens, and context menus. 1.0 is the classic
// full-cell row; 0.9 renders the rows at 90% of it with the fonts and the
// cell-based gutters and pads scaled to match. Cell (terminal) surfaces
// cannot subdivide a character cell and always render at 1.0 regardless.
// Read by the menu kit (objects/trinkets/menu_metrics.go), which is the only
// place menus measure.
var menuScale = 1.0

// SetMenuScale sets the graphical menu scale; values at or below zero
// restore 1.0.
func SetMenuScale(s float64) {
	if s <= 0 {
		s = 1
	}
	menuScale = s
}

// MenuScale returns the current graphical menu scale.
func MenuScale() float64 { return menuScale }

// MenuRowProvider is the optional capability a chrome bar has when it can
// state its own row height: the menu kit's row at the current MenuScale,
// counted in the bar's OWN denomination. A window reserves that much for it
// rather than assuming a whole cell, so a shortened bar leaves no dead strip
// below it that nothing answers for. A bar that cannot say keeps the cell.
type MenuRowProvider interface {
	MenuRowHeight() Unit
}

// FrameBorderProvider is the trinket-side carrier of the graphical
// window-frame border reservation: the desktop reports how many units the
// frame border occupies (the device-pixel width converted at its
// pixels-per-unit), 0 on cell surfaces. Windows reserve it out of their
// content area so the border rests OUTSIDE the interior coordinate system
// (a thicker border shrinks the interior / needs a bigger window).
type FrameBorderProvider interface {
	WindowFrameBorderUnits() Unit
}

// FindFrameBorderUnits walks up the trinket tree for a
// FrameBorderProvider. Default (no provider found): 0 - no reserved
// border, the cell-frame / safe answer.
func FindFrameBorderUnits(w Trinket) Unit {
	for current := Trinket(w); current != nil; {
		if p, ok := current.(FrameBorderProvider); ok {
			return p.WindowFrameBorderUnits()
		}
		parent := current.Parent()
		if parent == nil {
			return 0
		}
		current = parent
	}
	return 0
}

// FindFrameBorderUnitsIn is FindFrameBorderUnits stated in m's
// denomination. The provider answers in ITS OWN units -- device pixels
// divided by its surface's pixels-per-unit -- and a window whose frame
// counts in another denomination spends a different number for the same
// physical thickness. A top-level window's frame denomination IS the
// desktop's, so the two agree there; an MDI child's is its pane's, which
// follows whatever the host window's content was re-expressed to.
//
// Both axes come back because a unit is square only where the cell is: a
// 16x16 denomination over an 8x16 desktop spends 4 units on the same
// border across and 2 down.
func FindFrameBorderUnitsIn(w Trinket, m CellMetrics) (x, y Unit) {
	for current := Trinket(w); current != nil; {
		if p, ok := current.(FrameBorderProvider); ok {
			b := p.WindowFrameBorderUnits()
			from := FindEffectiveCellMetrics(current)
			return ExchangeX(b, from, m), ExchangeY(b, from, m)
		}
		parent := current.Parent()
		if parent == nil {
			return 0, 0
		}
		current = parent
	}
	return 0, 0
}

// PxPerUnitProvider is the trinket-side carrier of the surface's
// pixels-per-unit, so geometry expressed in DEVICE PIXELS (a minimum grab
// width, say) can be converted honestly rather than assumed equal to the
// integer device scale. The desktop reports its surface's ppu; a cell
// surface has none and the walk falls back to 1.
type PxPerUnitProvider interface {
	SurfacePxPerUnit() float64
}

// FindPxPerUnit walks up from a trinket to the nearest surface that reports
// pixels-per-unit, returning 1 when nothing does. ppu is font_size aware
// (fontSize/12 x deviceScale), which is exactly why a device-pixel quantity
// must be divided by IT and not by the device scale: the two agree only at
// font size 12.
func FindPxPerUnit(w Trinket) float64 {
	for current := Trinket(w); current != nil; {
		if p, ok := current.(PxPerUnitProvider); ok {
			if ppu := p.SurfacePxPerUnit(); ppu > 0 {
				return ppu
			}
			return 1
		}
		parent := current.Parent()
		if parent == nil {
			return 1
		}
		current = parent
	}
	return 1
}

// SnapOriginSetter is an optional RenderBackend capability: anchor cell
// snapping at a unit origin so content snaps relative to it (a window's
// interior stays pixel-identical wherever the window sits). Cell surfaces
// omit it, so setting an origin there is a no-op.
type SnapOriginSetter interface {
	// SetSnapOrigin anchors snapping at (ux, uy) and returns the previous
	// origin for restore. (0,0) is the global default.
	SetSnapOrigin(ux, uy Unit) (Unit, Unit)
}

// CaretDrawer is an optional RenderBackend capability: pixel surfaces
// draw the text-insertion caret as a thin vertical bar sitting at the
// left edge of the glyph box at (x, y) - where the next character
// would be output. Callers pass the same style they would use for a
// block cursor; the backend renders the bar in the color that block
// would appear (the style's background). Cell surfaces omit the
// capability and trinkets fall back to their cell-idiom caret
// (reverse-video block).
type CaretDrawer interface {
	DrawCaret(x, y, height Unit, s style.CellStyle)
}

// PixelRectFiller is an optional RenderBackend capability: fill a
// rectangle whose anchor is given in units but whose position and size
// are refined in device pixels, for hairline separators and 1-pixel
// gutter strokes that a whole-unit FillRect is too coarse to express.
// The style's background color fills the rect. Cell surfaces omit it.
type PixelRectFiller interface {
	FillRectPx(xPx, yPx, wPx, hPx int, s style.CellStyle)
}

// TextPixelDrawer is an optional RenderBackend capability: draw a string
// with its top-left at a device pixel (not a unit that re-snaps to the cell
// grid), returning the advance in device pixels. Proportional glyphs
// rasterize at the unsnapped pixels-per-unit; laying successive segments and
// the caret out by this pixel advance - instead of re-snapping each unit
// position through the cell rate - keeps them exactly on the glyphs at a
// fractional font size, where the two rates diverge. Cell surfaces omit it.
type TextPixelDrawer interface {
	DrawTextPx(xPx, yPx int, s string, st style.CellStyle, f *Font) int
}

// TextPixelMeasurer is an optional RenderBackend capability: the advance of a
// string in device pixels, measured the way DrawTextPx paints it.
//
// MeasureText answers in whole units, which is the denomination trinkets are
// laid out in and the wrong one for finding a position INSIDE a run. The
// glyphs rasterize at the unsnapped pixels-per-unit, so a caret or a clip edge
// placed by measuring a prefix in units and scaling afterwards rounds twice
// and drifts off the glyphs it belongs between - visibly where a rune's
// advance is a fraction of a unit, as a space's is beside CJK text. Cell
// surfaces omit this: there is nothing finer than a cell to place there.
type TextPixelMeasurer interface {
	MeasureTextPx(s string, f *Font) int
}

// ClippedTextPixelDrawer is an optional RenderBackend capability: like
// DrawTextPx, but only the device-pixel columns in [clipX0, clipX1) are
// painted. It lets a caller draw one shaped run and reveal only part of it
// with pixel precision (the selection re-colors its text this way, from the
// same run as the base text, so the glyphs never move). Cell surfaces omit it.
type ClippedTextPixelDrawer interface {
	DrawTextPxClipped(xPx, yPx int, s string, st style.CellStyle, f *Font, clipX0, clipX1 int) int
}

// FindSmoothPositioning walks up the trinket tree for a
// SmoothPositioningProvider. Default (no provider found): false -
// snap to cells, the only always-safe answer.
func FindSmoothPositioning(w Trinket) bool {
	for current := Trinket(w); current != nil; {
		if p, ok := current.(SmoothPositioningProvider); ok {
			return p.SmoothWindowPositioning()
		}
		parent := current.Parent()
		if parent == nil {
			return false
		}
		current = parent
	}
	return false
}

// Event is the base interface for all input events.
type Event interface {
	isEvent()
}

// KeyPressEvent represents a key press.
type KeyPressEvent struct {
	Key       string       // Key name from direct-key-handler
	Modifiers KeyModifiers // Active modifiers
	Text      string       // Printable text if any

	// Repeat marks a press the keyboard generated because the key is being
	// HELD, rather than struck again.
	//
	// It is a press either way, and every consumer that only wants to know a
	// key happened can ignore this and be right. It is here for the ones that
	// cannot: a browser in a hosted terminal reports a repeat as a keydown with
	// its repeat flag set, and without this it has no way to tell a held key
	// from a drummed one. Both backends produced repeats and neither said so —
	// the TUI trimmed the protocol's marker off and SDL never read its own
	// repeat bit — so a hosted guest was told the key was struck ten times.
	Repeat bool
}

func (KeyPressEvent) isEvent() {}

// KeyReleaseEvent represents a key release.
// Note: Not all terminals support key release events.
type KeyReleaseEvent struct {
	Key       string       // Key name
	Modifiers KeyModifiers // Active modifiers
}

func (KeyReleaseEvent) isEvent() {}

// MousePressEvent represents a mouse button press.
type MousePressEvent struct {
	X, Y      Unit         // Position in units
	Button    MouseButton  // Which button
	Modifiers KeyModifiers // Active keyboard modifiers
}

func (MousePressEvent) isEvent() {}

// MouseReleaseEvent represents a mouse button release.
type MouseReleaseEvent struct {
	X, Y      Unit
	Button    MouseButton
	Modifiers KeyModifiers
}

func (MouseReleaseEvent) isEvent() {}

// MouseMoveEvent represents mouse movement.
type MouseMoveEvent struct {
	X, Y      Unit
	Buttons   MouseButton  // Buttons currently held
	Modifiers KeyModifiers // Active keyboard modifiers
}

func (MouseMoveEvent) isEvent() {}

// MouseWheelEvent represents mouse wheel scrolling.
type MouseWheelEvent struct {
	X, Y   Unit
	DeltaX int // Horizontal scroll
	DeltaY int // Vertical scroll (positive = up)
	// Precise deltas (trackpad two-finger pan); zero when the source
	// only reports whole notches. Sign convention matches DeltaX/Y.
	PreciseX, PreciseY float64
	// Screen-space position, stamped once at the top of routing and
	// preserved through coordinate translation (wheel-gesture latch).
	ScreenX, ScreenY Unit
	Modifiers        KeyModifiers // Active keyboard modifiers
}

func (MouseWheelEvent) isEvent() {}

// ResizeEvent indicates the terminal/window was resized.
type ResizeEvent struct {
	Width, Height Unit // New size in units
	Cols, Rows    int  // New size in cells (for TUI)
}

func (ResizeEvent) isEvent() {}

// FocusEvent indicates focus gained or lost.
type FocusEvent struct {
	Focused bool
}

func (FocusEvent) isEvent() {}

// MouseLeaveEvent signals that the pointer left the surface entirely, so
// hover-only affordances (resize-edge highlights, hover cursors) can be
// cleared - there is no move event when the pointer exits.
type MouseLeaveEvent struct{}

func (MouseLeaveEvent) isEvent() {}

// QuitEvent indicates the user requested to quit.
type QuitEvent struct{}

func (QuitEvent) isEvent() {}

// PasteEvent contains pasted text. It is DECODED text, not wire bytes: a
// backend that receives a bracketed paste from its outer terminal strips the
// \x1b[200~ … \x1b[201~ framing and delivers the body here as one event. What a
// paste MEANS is then the focused trinket's call — a terminal surface
// re-brackets it for its own child (per that child's paste mode), a text field
// inserts it — which is why the framing does not travel on this event.
type PasteEvent struct {
	Text string
}

func (PasteEvent) isEvent() {}

// PasteHandler is implemented by any trinket that can receive pasted text
// directly: a terminal surface that re-brackets it for its child, a text field
// that inserts it at the caret. A PasteEvent is routed to the focused trinket
// the same way an input method's composition is (see FocusManager.HandlePaste);
// a focused trinket that does not implement PasteHandler simply does not
// receive pastes, and the event is dropped rather than reinterpreted.
type PasteHandler interface {
	// HandlePaste receives pasted text and reports whether it was consumed.
	HandlePaste(PasteEvent) bool
}

// Painter provides drawing operations with automatic coordinate translation.
// Trinkets receive a Painter configured with their local coordinate system.
type Painter struct {
	backend   RenderBackend
	transform Transform
	clip      UnitRect
	metrics   CellMetrics

	// partial marks a frame painted for only part of the surface — the tree
	// clipped to a damaged region rather than drawn whole. Derived painters
	// copy it, so it reaches whoever finishes the frame.
	//
	// It is here for what a frame's SILENCE is worth. A complete frame that
	// reported no insertion point has said there is none; a partial one has
	// said nothing at all, and the difference is the whole of Complete().
	partial bool

	// Rounded clip region (screen coordinates; zero rect = none): an
	// additional constraint beyond the rectangular clip, honored by
	// backends implementing RoundedClipper. Window frames set it so
	// edge-to-edge content cannot paint past the frame's rounded
	// corners.
	roundClip       UnitRect
	roundClipRadius Unit

	// offXPx/offYPx is a device-pixel RESIDUAL added to every pixel-precise
	// anchor this painter resolves (see deviceAnchor). The unit transform
	// snaps to the backend's cell grid, which is right for chrome and wrong
	// for content laid out at a different pitch: a subtree whose interior
	// steps by its own font's cell would be anchored on the host's grid and
	// drift from the text around it by a fraction of a cell. The residual is
	// how such a subtree says "and then this many pixels", exactly as the
	// drawing primitives already take offXPx per call.
	//
	// It deliberately does NOT affect the unit transform: clipping, hit
	// testing and whole-unit drawing are unchanged, and a span
	// (UnitSpanPxX/Y) is a difference of two anchors, so it cancels.
	offXPx, offYPx int

	// caret is the frame's platform text-caret request slot, shared by every
	// painter derived from this one (see textcaret.go).
	caret *caretSink
}

// NewPainter creates a painter for a backend.
func NewPainter(backend RenderBackend) *Painter {
	size := backend.Size()
	return &Painter{
		backend:   backend,
		transform: IdentityTransform(),
		clip:      UnitRect{Width: size.Width, Height: size.Height},
		metrics:   backend.Metrics(),
		caret:     &caretSink{},
	}
}

// Metrics returns the cell metrics.
func (p *Painter) Metrics() CellMetrics {
	return p.metrics
}

// WithTransform returns a new Painter with an additional transform
// applied. The new transform maps into the current local space: local
// coordinates pass through t first, then the existing transform. (With
// translations only the order is immaterial; once scales are involved
// it is not.)
func (p *Painter) WithTransform(t Transform) *Painter {
	np := *p
	np.transform = t.Compose(p.transform)
	return &np
}

// WithDenomination returns a Painter whose local coordinates are
// denominated in `child` metrics, given the current space is
// denominated in `parent` metrics. Used when descending into a
// container that carries a grid-metrics override: the same number of
// rows/columns, re-expressed, so re-denomination is visually invariant.
// Identity when the denominations match.
func (p *Painter) WithDenomination(parent, child CellMetrics) *Painter {
	if parent == child || child.UnitsPerCellWidth <= 0 || child.UnitsPerCellHeight <= 0 {
		return p
	}
	return p.WithTransform(Transform{
		ScaleX: float64(parent.UnitsPerCellWidth) / float64(child.UnitsPerCellWidth),
		ScaleY: float64(parent.UnitsPerCellHeight) / float64(child.UnitsPerCellHeight),
	})
}

// WithOffset returns a new Painter offset by the given amount.
func (p *Painter) WithOffset(dx, dy Unit) *Painter {
	return p.WithTransform(NewTranslation(dx, dy))
}

// WithClip returns a new Painter with clipping applied.
// The clip rect is intersected with any existing clip.
func (p *Painter) WithClip(clip UnitRect) *Painter {
	// Transform clip to screen coordinates
	screenClip := p.transform.ApplyRect(clip)
	// Intersect with existing clip
	np := *p
	np.clip = p.clip.Intersection(screenClip)
	return &np
}

// WithRoundedClipRegion returns a Painter whose drawing is
// additionally confined to a rounded rectangle (in current local
// coordinates). It composes with the rectangular clip chain: a pixel
// paints only if it passes both. Backends without RoundedClipper
// ignore it (cell surfaces have no rounded geometry to protect).
func (p *Painter) WithRoundedClipRegion(r UnitRect, radius Unit) *Painter {
	np := *p
	np.roundClip = p.transform.ApplyRect(r)
	np.roundClipRadius = radius
	return &np
}

// Clip returns the current clip rectangle in local coordinates.
func (p *Painter) Clip() UnitRect {
	inv := p.transform.Inverse()
	return inv.ApplyRect(p.clip)
}

// applyClip sets the backend clip to our current clip.
func (p *Painter) applyClip() {
	p.backend.SetClip(p.clip)
	if rc, ok := p.backend.(RoundedClipper); ok {
		rc.SetRoundedClip(p.roundClip, p.roundClipRadius)
	}
}

// toScreen transforms local coordinates to screen coordinates.
func (p *Painter) toScreen(x, y Unit) (Unit, Unit) {
	pt := p.transform.Apply(UnitPoint{X: x, Y: y})
	return pt.X, pt.Y
}

// DrawCell draws a single character.
func (p *Painter) DrawCell(x, y Unit, ch rune, s style.CellStyle) {
	sx, sy := p.toScreen(x, y)
	p.applyClip()
	p.backend.DrawCell(sx, sy, ch, s)
}

// DWLCellDrawer is an optional backend capability: draw one logical cell of a
// DEC double-width/double-height line as a visual-column group (the carrier
// glyph plus filler cells, twice the glyph's width). Cell backends that can
// emit real DECDWL rows implement it (the TUI backend); mode is the DEC line
// selector ('6' DECDWL, '3'/'4' DECDHL halves). Returns columns consumed.
type DWLCellDrawer interface {
	DrawCellDWL(x, y Unit, ch rune, combining string, s style.CellStyle, mode byte, cellWidth float64) int
}

// DrawCellDWL draws one logical cell of a DEC double-width line through the
// backend's DWL capability. Backends without it get a literal fallback — the
// glyph followed by a filler space (double-spaced, no DEC modes) — so content
// still lands in the right columns. Returns the columns consumed.
//
// cellWidth is the cell's VISUAL width in cell units — purfecterm's flex-width
// attribute (0.5, 1.0, 1.5, 2.0; see its Cell.FlexWidth/CellWidth), which the
// GTK and Qt renderers fold into their cell box as cellVisualWidth. Pass 0 or
// 1 for an ordinary cell.
func (p *Painter) DrawCellDWL(x, y Unit, ch rune, combining string, s style.CellStyle, mode byte, cellWidth float64) int {
	sx, sy := p.toScreen(x, y)
	p.applyClip()
	if d, ok := p.backend.(DWLCellDrawer); ok {
		return d.DrawCellDWL(sx, sy, ch, combining, s, mode, cellWidth)
	}
	p.backend.DrawCell(sx, sy, ch, s)
	p.backend.DrawCell(sx+p.metrics.CellToUnitsX(1), sy, ' ', s)
	return 2
}

// DrawRoundedRect paints a filled, stroked rounded rectangle when
// the backend supports it (see RoundedRectDrawer). Returns false on
// cell surfaces; the caller then falls back to its cell-idiom
// rendering (box-drawing runes).
func (p *Painter) DrawRoundedRect(r UnitRect, radius Unit, border style.BorderStyle, s style.CellStyle) bool {
	rd, ok := p.backend.(RoundedRectDrawer)
	if !ok {
		return false
	}
	screenRect := p.transform.ApplyRect(r)
	p.applyClip()
	rd.DrawRoundedRect(screenRect, radius, border, s)
	return true
}

// DrawArcWedge paints an antialiased quarter-arc wedge when the
// backend supports it (see ArcWedgeDrawer). strokeW is in screen
// units; offXPx/offYPx rigidly translate the whole wedge by an exact
// device-pixel amount AFTER cell snapping - for sub-cell nudges that
// must be exact regardless of position (e.g. shifting a foot arc by
// one line thickness so its stroke meets the shoulder's without a
// snapping-dependent jog). Returns false on cell surfaces; the caller
// then falls back to its scanline rendering.
func (p *Painter) DrawArcWedge(r UnitRect, centerRight, centerBottom bool, strokeW Unit, offXPx, offYPx int, s style.CellStyle) bool {
	ad, ok := p.backend.(ArcWedgeDrawer)
	if !ok {
		return false
	}
	screenRect := p.transform.ApplyRect(r)
	p.applyClip()
	ad.DrawArcWedge(screenRect, centerRight, centerBottom, strokeW, offXPx, offYPx, s)
	return true
}

// DrawImage composites a device-pixel image at a unit position when
// the backend supports it (see ImageDrawer). Returns false on cell
// surfaces.
func (p *Painter) DrawImage(x, y Unit, img image.Image) bool {
	id, ok := p.backend.(ImageDrawer)
	if !ok {
		return false
	}
	sx, sy := p.toScreen(x, y)
	p.applyClip()
	id.DrawImage(sx, sy, img)
	return true
}

// DrawImageOffset composites a device-pixel image anchored at a unit
// position plus a device-pixel nudge - for content that needs
// sub-unit placement (sprite fine positioning, wave animation).
// Returns false on cell surfaces.
func (p *Painter) DrawImageOffset(x, y Unit, offXPx, offYPx int, img image.Image) bool {
	id, ok := p.backend.(ImageDrawer)
	if !ok {
		return false
	}
	sx, sy := p.toScreen(x, y)
	ax, ay := p.deviceAnchor(sx, sy)
	p.applyClip()
	id.DrawImagePx(ax+offXPx, ay+offYPx, img)
	return true
}

// DrawImageMaskTintOffset composites a coverage mask (only its alpha is read)
// tinted with (r,g,b) at unit (x,y) plus a device-pixel offset - the recolor
// twin of DrawImageOffset for cached grayscale glyphs. Returns false on
// backends without MaskTintDrawer.
func (p *Painter) DrawImageMaskTintOffset(x, y Unit, offXPx, offYPx int, mask *image.RGBA, r, g, b uint8) bool {
	md, ok := p.backend.(MaskTintDrawer)
	if !ok {
		return false
	}
	sx, sy := p.toScreen(x, y)
	ax, ay := p.deviceAnchor(sx, sy)
	p.applyClip()
	md.DrawImageMaskTintPx(ax+offXPx, ay+offYPx, mask, r, g, b)
	return true
}

// MeasureTextPx is the advance of a string in device pixels, measured the way
// DrawTextOffset paints it. Returns 0, false on cell surfaces, where the
// caller falls back to measuring in whole units.
//
// Use it for anything that has to land INSIDE a run - a caret, a selection
// edge, a composition's clip - rather than measuring in units and scaling:
// see TextPixelMeasurer.
func (p *Painter) MeasureTextPx(text string, font *Font) (int, bool) {
	tm, ok := p.backend.(TextPixelMeasurer)
	if !ok {
		return 0, false
	}
	return tm.MeasureTextPx(text, font), true
}

// DrawTextOffset draws a string with its top-left at unit (x, y) plus a
// device-pixel offset, returning the advance in device pixels. Proportional
// glyphs rasterize at the unsnapped pixels-per-unit, so laying successive
// segments out by accumulating this pixel advance - instead of re-snapping
// each unit position through the cell rate - keeps them exactly on the
// glyphs at a fractional font size, where the two rates diverge. Returns 0,
// false on cell surfaces (the caller falls back to whole-unit DrawText).
func (p *Painter) DrawTextOffset(x, y Unit, offXPx, offYPx int, text string, s style.CellStyle, font *Font) (int, bool) {
	td, ok := p.backend.(TextPixelDrawer)
	if !ok {
		return 0, false
	}
	sx, sy := p.toScreen(x, y)
	ax, ay := p.deviceAnchor(sx, sy)
	p.applyClip()
	return td.DrawTextPx(ax+offXPx, ay+offYPx, text, s, font), true
}

// DrawTextOffsetClipped draws a string at unit (x, y) plus a device-pixel
// offset, but reveals only the columns in [clipX0Px, clipX1Px) - both given
// as device-pixel offsets from the same unit anchor. Draw the whole run at
// offXPx and clip to the wanted span to re-color a slice of it (the
// selection) without re-shaping or re-placing the glyphs, so they don't
// jitter as the span grows. Returns 0, false on cell surfaces.
func (p *Painter) DrawTextOffsetClipped(x, y Unit, offXPx, clipX0Px, clipX1Px int, text string, s style.CellStyle, font *Font) (int, bool) {
	td, ok := p.backend.(ClippedTextPixelDrawer)
	if !ok {
		return 0, false
	}
	sx, sy := p.toScreen(x, y)
	ax, ay := p.deviceAnchor(sx, sy)
	p.applyClip()
	return td.DrawTextPxClipped(ax+offXPx, ay, text, s, font, ax+clipX0Px, ax+clipX1Px), true
}

// FillRectPixels fills, in device pixels, a rectangle anchored at unit
// (x, y) plus a device-pixel offset: wPx x hPx device pixels in the
// style's background color. For hairline separators and 1-pixel gutter
// strokes that whole-unit FillRect can't express. Returns false on cell
// surfaces (the caller falls back to a cell-idiom line).
func (p *Painter) FillRectPixels(x, y Unit, offXPx, offYPx, wPx, hPx int, s style.CellStyle) bool {
	pf, ok := p.backend.(PixelRectFiller)
	if !ok {
		return false
	}
	sx, sy := p.toScreen(x, y)
	ax, ay := p.deviceAnchor(sx, sy)
	p.applyClip()
	pf.FillRectPx(ax+offXPx, ay+offYPx, wPx, hPx, s)
	return true
}

// FillRectPixelsAlpha blends, in device pixels, the rectangle anchored at
// unit (x, y) plus a device-pixel offset (wPx x hPx device pixels) with
// the given RGB at alpha (0..1), over the existing pixels and respecting
// the clip - including any rounded clip region, so a fill along a window
// edge follows its corner curve. Returns false on backends that can't
// blend (e.g. cell surfaces).
func (p *Painter) FillRectPixelsAlpha(x, y Unit, offXPx, offYPx, wPx, hPx int, r, g, b uint8, alpha float64) bool {
	tf, ok := p.backend.(TranslucentPixelFiller)
	if !ok {
		return false
	}
	sx, sy := p.toScreen(x, y)
	ax, ay := p.deviceAnchor(sx, sy)
	p.applyClip()
	tf.FillRectPxAlpha(ax+offXPx, ay+offYPx, wPx, hPx, r, g, b, alpha)
	return true
}

// DeviceScale reports the device zoom: how many device pixels one unit
// covers at the base font size (1 on cell surfaces and unscaled pixel
// surfaces). It does NOT include the font_size cell scaling; use
// UnitsToPx / the device anchor helpers for font_size-aware conversions.
func (p *Painter) DeviceScale() int {
	if ds, ok := p.backend.(DeviceScaler); ok {
		if s := ds.Scale(); s > 0 {
			return s
		}
	}
	return 1
}

// DisplayDensity reports the physical display's content scale (see
// DisplayDensityReporter): 2 on a HiDPI panel, 1 on an ordinary one, and 1
// when the backend does not know — a backend that cannot see a screen (an
// off-screen raster, a terminal host) has no business claiming one.
//
// Use it only for agreeing with something OUTSIDE this process about density.
// Everything internal — geometry, hairlines, cell metrics — uses DeviceScale
// or PxPerUnitF, which describe how this application draws.
func (p *Painter) DisplayDensity() float64 {
	if d, ok := p.backend.(DisplayDensityReporter); ok {
		if s := d.DisplayDensity(); s > 0 {
			return s
		}
	}
	return 1
}

// CellPixelSize reports the device pixels one cell of this surface covers, or
// 0,0 when the backend does not know (see CellPixelSizer). A graphical surface
// answers 0,0 and should be asked its font metrics instead; this exists for the
// number a cell surface had to ask its host terminal for.
func (p *Painter) CellPixelSize() (w, h int) {
	if cs, ok := p.backend.(CellPixelSizer); ok {
		if cw, ch := cs.CellPixelSize(); cw > 0 && ch > 0 {
			return cw, ch
		}
	}
	return 0, 0
}

// RequestMotionTracking asks the surface to deliver pointer motion for the rest
// of this frame's lifetime (see MotionTracker). A backend that always has
// motion ignores it, so callers need not ask what kind of host they are on.
func (p *Painter) RequestMotionTracking() {
	if mt, ok := p.backend.(MotionTracker); ok {
		mt.RequestMotionTracking()
	}
}

// PxPerUnitF reports the fractional device pixels per unit, tracking
// font_size on backends that expose UnitPixelMapper (else the integer
// DeviceScale). Device-pixel render paths (e.g. the terminal cell
// raster) multiply unit dimensions by this so they grow with the cell.
func (p *Painter) PxPerUnitF() float64 {
	if m, ok := p.backend.(UnitPixelMapper); ok {
		if ppu := m.PxPerUnit(); ppu > 0 {
			return ppu
		}
	}
	return float64(p.DeviceScale())
}

// UnitsToPx converts a unit length to device pixels, tracking font_size
// on backends that expose UnitPixelMapper (else integer unit*scale). For
// device-pixel widths/heights that must grow with the cell size.
//
// For a span that borders cell-snapped geometry (a menu edge, a hairline
// that must reach a fill's edge) use UnitSpanPxX/Y instead: those snap
// both ends to the same grid the shapes paint on, so the device fill and
// the shape line up exactly (no seam).
func (p *Painter) UnitsToPx(u Unit) int {
	return int(math.Round(float64(u) * p.PxPerUnitF()))
}

// UnitSpanPxX is the device-pixel distance between two unit X positions,
// snapped to the same grid the backend paints on (see deviceAnchor), so a
// device-pixel fill anchored at fromX and this many pixels wide ends
// exactly where a cell-snapped shape at toX does.
func (p *Painter) UnitSpanPxX(fromX, toX Unit) int {
	sf, _ := p.toScreen(fromX, 0)
	st, _ := p.toScreen(toX, 0)
	af, _ := p.deviceAnchor(sf, 0)
	at, _ := p.deviceAnchor(st, 0)
	return at - af
}

// UnitSpanPxY is UnitSpanPxX for the Y axis.
func (p *Painter) UnitSpanPxY(fromY, toY Unit) int {
	_, sf := p.toScreen(0, fromY)
	_, st := p.toScreen(0, toY)
	_, af := p.deviceAnchor(0, sf)
	_, at := p.deviceAnchor(0, st)
	return at - af
}

// deviceAnchor maps a screen-unit position to its device-pixel anchor,
// matching the backend's own (cell-snapped, font_size-aware) geometry so
// device-pixel fills line up with painted edges.
func (p *Painter) deviceAnchor(sx, sy Unit) (int, int) {
	if m, ok := p.backend.(UnitPixelMapper); ok {
		return m.UnitToPxX(sx) + p.offXPx, m.UnitToPxY(sy) + p.offYPx
	}
	scale := p.DeviceScale()
	return int(sx)*scale + p.offXPx, int(sy)*scale + p.offYPx
}

// WithPixelOffset returns a painter whose pixel-precise anchors are shifted
// by (dxPx, dyPx) device pixels, ADDED to any residual already in force so
// nested subtrees compose. See Painter.offXPx for what this is for and what
// it deliberately leaves alone.
func (p *Painter) WithPixelOffset(dxPx, dyPx int) *Painter {
	np := *p
	np.offXPx += dxPx
	np.offYPx += dyPx
	return &np
}

// PixelOffset reports the residual in force, so a caller can measure its own
// contribution against it.
func (p *Painter) PixelOffset() (int, int) { return p.offXPx, p.offYPx }

// SetSnapOrigin anchors the backend's cell snapping at unit (ux, uy) when
// the backend supports it (graphical surfaces), returning the previous
// origin (both 0 on cell surfaces). A window paints its subtree with the
// origin set to its top-left, so its interior is pixel-stable as the
// window moves, then restores the previous origin. Because paints are
// synchronous, save the return and restore it right after the subtree.
func (p *Painter) SetSnapOrigin(ux, uy Unit) (Unit, Unit) {
	if s, ok := p.backend.(SnapOriginSetter); ok {
		return s.SetSnapOrigin(ux, uy)
	}
	return 0, 0
}

// Graphical reports whether the target paints pixels rather than
// character cells (the D1 mode query). Trinkets use it to select
// their graphical rendering material - cell targets get the cell
// idiom, always.
func (p *Painter) Graphical() bool {
	gm, ok := p.backend.(GraphicalModer)
	return ok && gm.GraphicalMode()
}

// FillPattern tiles an 8x8 two-color bitmap across the rect when the
// backend supports it (see PatternFiller). Returns false on cell
// surfaces; the caller then falls back to its rune fill.
// ClearTransparent resets the clipped region to fully transparent (see
// SurfaceClearer). Returns false where the surface has no alpha to clear.
func (p *Painter) ClearTransparent() bool {
	sc, ok := p.backend.(SurfaceClearer)
	if !ok {
		return false
	}
	p.applyClip()
	sc.ClearTransparent()
	return true
}

// TileImage lays tile across r as the layout describes (see ImageTiler).
// Returns false on backends that cannot draw images, where the caller
// falls back to a pattern or cell fill.
func (p *Painter) TileImage(r UnitRect, tile *image.RGBA, layout WallpaperLayout) bool {
	it, ok := p.backend.(ImageTiler)
	if !ok || tile == nil || tile.Bounds().Empty() {
		return false
	}
	p.applyClip()
	it.TileImagePx(p.transform.ApplyRect(r), tile, layout)
	return true
}

func (p *Painter) FillPattern(r UnitRect, pattern [8]uint8, chunkPx int, s style.CellStyle) bool {
	pf, ok := p.backend.(PatternFiller)
	if !ok {
		return false
	}
	screenRect := p.transform.ApplyRect(r)
	p.applyClip()
	pf.FillPattern(screenRect, pattern, chunkPx, s)
	return true
}

// StrokeRoundedRect paints only the rounded rectangle's stroke,
// leaving the interior untouched, when the backend supports it.
// Returns false on cell surfaces.
func (p *Painter) StrokeRoundedRect(r UnitRect, radius Unit, border style.BorderStyle, s style.CellStyle) bool {
	rd, ok := p.backend.(RoundedRectDrawer)
	if !ok {
		return false
	}
	screenRect := p.transform.ApplyRect(r)
	p.applyClip()
	rd.StrokeRoundedRect(screenRect, radius, border, s)
	return true
}

// StrokeRoundedRectWeight paints only the rounded rectangle's stroke at an
// explicit device-pixel weight (see RoundedRectWeightStroker), leaving the
// interior untouched. Returns false on cell surfaces.
func (p *Painter) StrokeRoundedRectWeight(r UnitRect, radius Unit, strokePx int, s style.CellStyle) bool {
	rd, ok := p.backend.(RoundedRectWeightStroker)
	if !ok {
		return false
	}
	screenRect := p.transform.ApplyRect(r)
	p.applyClip()
	rd.StrokeRoundedRectWeight(screenRect, radius, strokePx, s)
	return true
}

// DrawCaret draws a text-insertion caret at the left edge of the
// glyph box at (x, y) when the backend supports bar carets (see
// CaretDrawer). Returns false on cell surfaces; the caller then
// falls back to its cell-idiom caret (reverse-video block).
func (p *Painter) DrawCaret(x, y, height Unit, s style.CellStyle) bool {
	cd, ok := p.backend.(CaretDrawer)
	if !ok {
		return false
	}
	sx, sy := p.toScreen(x, y)
	p.applyClip()
	cd.DrawCaret(sx, sy, height, s)
	return true
}

// ScreenHeightToLocal converts a screen-space height into local
// units under the painter's current transform. MeasureText is
// screen-space: glyph rasters ignore denomination scaling, so layout
// math inside re-denominated interiors must convert its answer before
// mixing it with local coordinates.
func (p *Painter) ScreenHeightToLocal(h Unit) Unit {
	r := p.transform.Inverse().ApplyRect(UnitRect{Height: h})
	return r.Height
}

// ScreenWidthToLocal is ScreenHeightToLocal for the X axis.
func (p *Painter) ScreenWidthToLocal(w Unit) Unit {
	r := p.transform.Inverse().ApplyRect(UnitRect{Width: w})
	return r.Width
}

// HairlineWidth is the thinnest local width that still paints, and
// HairlineHeight is the same down the page.
//
// One screen unit converted into local units is the physical thickness wanted,
// but the conversion answers in whole units and the count it lands on can span
// no device pixel at all: inside an interior at column_units=12 a local unit is
// two-thirds of a pixel, so a 1-unit fill paints NOTHING and the rule, the
// divider, the line is simply absent. Clamping the unit count at 1 does not
// reach it -- 1 was already the answer -- so the floor belongs on the pixels,
// which is what these put it on.
//
// A separator's rule and a splitter's divider are drawn with these.
func (p *Painter) HairlineWidth() Unit {
	return p.hairline(p.ScreenWidthToLocal(1), func(u Unit) int { return p.UnitSpanPxX(0, u) })
}

// HairlineHeight is HairlineWidth for the Y axis.
func (p *Painter) HairlineHeight() Unit {
	return p.hairline(p.ScreenHeightToLocal(1), func(u Unit) int { return p.UnitSpanPxY(0, u) })
}

// hairline grows a local thickness until it spans a device pixel.
//
// It counts up rather than scaling: the span is the backend's own snapped
// geometry, not a ratio to compute against, and the answer is a unit or two in
// every denomination a surface is likely to carry. The cap is there so a
// backend that reports no pixels for any span ends the loop rather than
// running it out.
func (p *Painter) hairline(u Unit, spanPx func(Unit) int) Unit {
	if u < 1 {
		u = 1
	}
	for i := 0; i < 64 && spanPx(u) < 1; i++ {
		u++
	}
	return u
}

// DrawText draws a string using the specified font.
// If font is nil, uses DefaultFont().
func (p *Painter) DrawText(x, y Unit, text string, s style.CellStyle, font *Font) Unit {
	sx, sy := p.toScreen(x, y)
	p.applyClip()
	return p.backend.DrawText(sx, sy, text, s, font)
}

// DrawTextAligned draws text aligned within a box using the specified font.
// If font is nil, uses DefaultFont().
func (p *Painter) DrawTextAligned(bounds UnitRect, text string, hAlign, vAlign Alignment, s style.CellStyle, font *Font) {
	screenBounds := p.transform.ApplyRect(bounds)
	p.applyClip()
	p.backend.DrawTextAligned(screenBounds, text, hAlign, vAlign, s, font)
}

// FillRect fills a rectangle.
func (p *Painter) FillRect(r UnitRect, ch rune, s style.CellStyle) {
	screenRect := p.transform.ApplyRect(r)
	p.applyClip()
	p.backend.FillRect(screenRect, ch, s)
}

// DrawRect draws a rectangle border.
func (p *Painter) DrawRect(r UnitRect, border style.BorderStyle, s style.CellStyle) {
	screenRect := p.transform.ApplyRect(r)
	p.applyClip()
	p.backend.DrawRect(screenRect, border, s)
}

// DrawHLine draws a horizontal line.
func (p *Painter) DrawHLine(x, y, width Unit, ch rune, s style.CellStyle) {
	sx, sy := p.toScreen(x, y)
	p.applyClip()
	p.backend.DrawHLine(sx, sy, width, ch, s)
}

// DrawVLine draws a vertical line.
func (p *Painter) DrawVLine(x, y, height Unit, ch rune, s style.CellStyle) {
	sx, sy := p.toScreen(x, y)
	p.applyClip()
	p.backend.DrawVLine(sx, sy, height, ch, s)
}

// DrawBox draws a box with optional title.
func (p *Painter) DrawBox(r UnitRect, border style.BorderStyle, title string, s style.CellStyle) {
	screenRect := p.transform.ApplyRect(r)
	p.applyClip()
	p.backend.DrawBox(screenRect, border, title, s)
}

// Clear fills a rectangle with space characters.
func (p *Painter) Clear(r UnitRect, s style.CellStyle) {
	p.FillRect(r, ' ', s)
}

// Size returns a size in units for the given cell dimensions.
func (p *Painter) Size(cols, rows int) UnitSize {
	return p.metrics.CellsToUnits(cols, rows)
}
