// Package raster is the pixel implementation of KittyTK's rendering
// primitives (D23): the same core.RenderBackend interface the TUI
// speaks, drawn onto an RGBA framebuffer with real font glyphs and
// real lines. There is no glyph-grid emulation stage - DrawRect
// draws lines, not box runes; DrawText rasterizes a TTF; the whole
// desktop renders graphically through the existing trinket paint
// paths.
//
// Substrates (SDL first, per D23) present this framebuffer in a
// window and feed input back; the package itself is substrate-free
// and cgo-free, so it also serves headless rendering and tests.
//
// Units: a unit is 1/denomination of a cell, and a cell's PIXEL size is
// set by font_size (12pt = the historical 8x16-pixel cell at zoom 1).
// The root denomination stays the default 8x16 regardless of font_size;
// font_size instead scales pixels-per-unit, so a bigger font grows every
// unit-measured length in pixels without changing its cell count. The
// device zoom (scale) multiplies on top, so pixels-per-unit is
// scale * font_size/12 and may be fractional.
package raster

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sync"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
	"github.com/phroun/kittytk/text"
)

// Backend renders KittyTK drawing primitives into an RGBA image.
type Backend struct {
	img      *image.RGBA
	w, h     int              // pixels
	scale    int              // device zoom: pixels per unit at the 12pt base font
	fontSize int              // UI point size that sets the cell's pixel size (12 = base)
	metrics  core.CellMetrics // root cell denomination (units per cell)

	clip    core.UnitRect
	hasClip bool
	// Device-pixel bounds of clip, cached by SetClip so pointVisible does
	// not re-snap the clip through pxX/pxY (and snapAxis) for every pixel -
	// that per-pixel recompute dominated fill time.
	clipPxX0, clipPxY0, clipPxX1, clipPxY1 int

	// snapOX/Y and snapOPxX/Y anchor cell snapping at a window's origin
	// (see SetSnapOrigin): the origin maps to a fixed whole device pixel
	// and content snaps RELATIVE to it, so a window's interior is
	// pixel-identical wherever the window sits (no sub-cell jitter as it
	// moves). Default (0,0)/(0,0) = the historical global snapping.
	snapOX, snapOY     core.Unit
	snapOPxX, snapOPxY int

	// Rounded clip (core.RoundedClipper): an additional constraint on
	// top of the rectangular clip. Window frames confine their
	// edge-to-edge content with it.
	roundClip       core.UnitRect
	roundClipRadius core.Unit
	hasRoundClip    bool
	// Device-pixel bounds + radius of the rounded clip, cached by
	// SetRoundedClip (same reason as the rectangular clip above).
	roundPxX0, roundPxY0, roundPxX1, roundPxY1, roundPxRad int

	// pxClip is a transient device-pixel X clip [pxClipX0, pxClipX1),
	// applied only for the duration of a single clipped blit (selection
	// text drawn from the whole run and revealed only over the selected
	// columns). It is pixel-precise, unlike the cell-snapped unit clip.
	pxClipActive bool
	pxClipX0     int
	pxClipX1     int

	// clipboard holds the local clipboard for headless use; SDL and
	// other substrates may sync it with the system clipboard.
	clipboard string

	// sysClipGet/sysClipSet bridge to the host's system clipboard when the
	// substrate provides one (the SDL platform wires these to SDL2's
	// SDL_GetClipboardText/SDL_SetClipboardText, which cover macOS, Windows
	// and X11/Wayland). nil in headless use, where the local string is used.
	sysClipGet func() string
	sysClipSet func(string)
}

// SetSystemClipboard bridges this backend's clipboard to the host's system
// clipboard. The SDL host wires get/set to the platform's SDL2-backed
// clipboard, so Copy/Cut/Paste cross the process boundary to other apps.
// Passing nils reverts to the internal (headless) clipboard.
func (b *Backend) SetSystemClipboard(get func() string, set func(string)) {
	b.sysClipGet = get
	b.sysClipSet = set
}

// New creates a framebuffer backend of the given pixel size at scale 1
// (one unit = one pixel).
func New(widthPx, heightPx int) (*Backend, error) {
	return NewScaled(widthPx, heightPx, 1)
}

// NewScaled creates a framebuffer backend where each abstract unit
// covers scale x scale pixels. Glyphs are rasterized by the shared
// text engine at the scaled size (not upsampled), so they stay sharp
// at any scale.
func NewScaled(widthPx, heightPx, scale int) (*Backend, error) {
	if scale < 1 {
		scale = 1
	}
	b := &Backend{
		img:      image.NewRGBA(image.Rect(0, 0, widthPx, heightPx)),
		w:        widthPx,
		h:        heightPx,
		scale:    scale,
		fontSize: 12,
		metrics:  core.CellMetrics{CellWidth: 8, CellHeight: 16},
	}
	return b, nil
}

// termRGBA converts a palette color to an opaque framebuffer color.
func termRGBA(c style.TermRGB) color.RGBA {
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: 255}
}

// cellPx is the exact integer pixel size of one root cell along an axis:
// the denomination base (8 wide, 16 tall) scaled by the font_size ratio,
// then by the integer device zoom. Whole cells land on exact multiples
// of this, so cell-aligned geometry (grid lines, borders, the cell
// primitive) never falls on a fractional pixel.
//
// The ratio is rounded UP (ceil): a cell must fully contain its glyph
// line box, so it can never be shorter than the character it holds -
// otherwise descenders spill below the item's background fill.
func (b *Backend) cellPx(denom int) int {
	if denom < 1 {
		denom = 1
	}
	n := int(math.Ceil(float64(denom) * float64(b.fontSize) / 12))
	if n < 1 {
		n = 1
	}
	return n * b.scale
}

func (b *Backend) cellWPx() int { return b.cellPx(int(b.metrics.CellWidth)) }
func (b *Backend) cellHPx() int { return b.cellPx(int(b.metrics.CellHeight)) }

// snapAxis converts a unit coordinate/length to device pixels along one
// axis: whole denomination-cells map to exact cellPx multiples, the
// sub-cell remainder to a rounded fraction of a cell (D: cells are the
// alignment grid, so integer cells must stay pixel-exact).
func snapAxis(u core.Unit, denom, cellPx int) int {
	if denom < 1 {
		denom = 1
	}
	cells := int(u) / denom
	rem := int(u) % denom
	return cells*cellPx + int(math.Round(float64(rem)*float64(cellPx)/float64(denom)))
}

// pxX / pxY are the cell-snapped unit-to-pixel conversions for the two
// axes. Positions and cell-aligned rect edges go through these.
func (b *Backend) pxX(u core.Unit) int {
	return b.snapOPxX + snapAxis(u-b.snapOX, int(b.metrics.CellWidth), b.cellWPx())
}
func (b *Backend) pxY(u core.Unit) int {
	return b.snapOPxY + snapAxis(u-b.snapOY, int(b.metrics.CellHeight), b.cellHPx())
}

// SetSnapOrigin anchors cell snapping at unit (ux, uy): that point keeps
// its whole-pixel position and everything else snaps RELATIVE to it, so a
// window painted with its origin set here has a pixel-identical interior
// no matter where the window sits - eliminating the sub-cell jitter that
// absolute snapping produces at fractional pixels-per-unit. Returns the
// previous origin so the caller can restore it after the subtree paints.
// (0,0) restores the global default.
//
// The pixel anchor is computed from the raw (origin-0) mapping, so this is
// exact for a top-level window whose surface origin is 0; nested snap
// origins are not composed.
func (b *Backend) SetSnapOrigin(ux, uy core.Unit) (core.Unit, core.Unit) {
	prevX, prevY := b.snapOX, b.snapOY
	b.snapOX, b.snapOY = ux, uy
	b.snapOPxX = snapAxis(ux, int(b.metrics.CellWidth), b.cellWPx())
	b.snapOPxY = snapAxis(uy, int(b.metrics.CellHeight), b.cellHPx())
	return prevX, prevY
}

// pxPerUnit is the unsnapped device pixels covered by one unit, equal on
// both axes (cellPx/denomination = font_size/12 * zoom). Proportional
// text glyphs and decorative lengths (radii, arc strokes) - which are
// not cell-aligned - use this instead of the snapped conversions.
func (b *Backend) pxPerUnit() float64 {
	return float64(b.scale) * float64(b.fontSize) / 12
}

// pxLen rounds an axis-agnostic length (radius, stroke) to device pixels.
func (b *Backend) pxLen(u core.Unit) int {
	return int(math.Round(float64(u) * b.pxPerUnit()))
}

// Scale reports the device zoom (pixels per unit at the 12pt base font).
// Chrome that wants a physical device-pixel weight (hairlines, grab
// targets) uses this; geometry uses px()/PxPerUnit() so it also tracks
// font_size.
func (b *Backend) Scale() int { return b.scale }

// PxPerUnit exposes the fractional pixels-per-unit (see pxPerUnit) so
// the painter's device-pixel helpers place sub-unit fills where the
// backend's own geometry lands, at any font_size. Implements
// core.UnitPixelMapper together with UnitToPxX/UnitToPxY.
func (b *Backend) PxPerUnit() float64 { return b.pxPerUnit() }

// UnitToPxX / UnitToPxY expose the cell-snapped axis conversions so the
// painter can anchor device-pixel fills on the same grid the backend
// paints (core.UnitPixelMapper).
func (b *Backend) UnitToPxX(u core.Unit) int { return b.pxX(u) }
func (b *Backend) UnitToPxY(u core.Unit) int { return b.pxY(u) }

// SetFontSize sets the UI point size that fixes the cell's pixel size
// (12 = the base 8x16-pixel cell at zoom 1). It scales pixels-per-unit,
// not the denomination, so layout is unchanged in units and only the
// pixel size of every cell grows.
func (b *Backend) SetFontSize(size int) {
	if size < 1 {
		size = 12
	}
	b.fontSize = size
}

// Image exposes the framebuffer (substrates blit it; tests read it).
func (b *Backend) Image() *image.RGBA { return b.img }

// DevicePxRect maps a unit rectangle to device-pixel bounds on the
// framebuffer, clamped to it - used for partial texture uploads when only a
// damaged region was repainted. Returns ok=false if the rect is empty.
func (b *Backend) DevicePxRect(r core.UnitRect) (x0, y0, x1, y1 int, ok bool) {
	x0, y0 = b.pxX(r.X), b.pxY(r.Y)
	x1, y1 = b.pxX(r.X+r.Width), b.pxY(r.Y+r.Height)
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > b.w {
		x1 = b.w
	}
	if y1 > b.h {
		y1 = b.h
	}
	return x0, y0, x1, y1, x1 > x0 && y1 > y0
}

// WritePNG saves the framebuffer.
func (b *Backend) WritePNG(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, b.img)
}

// --- RenderBackend: lifecycle & geometry ---

func (b *Backend) Init() error { return nil }
func (b *Backend) Shutdown()   {}

// Metrics: the root cell denomination (default 8x16 per D23/D8'), the
// units-per-cell subdivision. font_size does NOT change this - it scales
// the cell's pixel size (see pxX/pxY) - but SetCellMetrics can re-seed a
// non-default root denomination.
func (b *Backend) Metrics() core.CellMetrics {
	return b.metrics
}

// SetCellMetrics re-seeds the root cell denomination the desktop
// inherits. font_size does NOT change this (it scales the cell's pixel
// size, not its subdivision count); callers use it only to choose a
// non-default root denomination. Call before Desktop.SetBackend so the
// whole trinket tree picks it up.
func (b *Backend) SetCellMetrics(m core.CellMetrics) {
	if m.CellWidth < 1 {
		m.CellWidth = 1
	}
	if m.CellHeight < 1 {
		m.CellHeight = 1
	}
	b.metrics = m
}

// unSnapAxisFloor inverts snapAxis to a unit extent that fits WITHIN px:
// the largest unit count whose cell-snapped placement (pxX/pxY) stays at
// or below px. It floors the sub-cell remainder (rather than rounding to
// nearest, as hit-testing's pxToUnitAxis does) so a reported surface size
// never maps back to a pixel beyond the true edge - which would push
// right/bottom-aligned content off and clip it.
func unSnapAxisFloor(px, denom, cellPx int) int {
	if cellPx < 1 {
		cellPx = 1
	}
	cells := px / cellPx
	rem := px - cells*cellPx
	return cells*denom + rem*denom/cellPx
}

// Size reports the surface extent in units. It inverts the SAME
// cell-snapped mapping content is placed with (pxX/pxY), NOT the unsnapped
// pixels-per-unit: at fractional cell sizes those differ, and using the
// unsnapped ratio here made the surface look wider in units than snapped
// placement could fit, so right/bottom-aligned content (e.g. the menu-bar
// clock) landed past the true edge and clipped.
func (b *Backend) Size() core.UnitSize {
	return core.UnitSize{
		Width:  core.Unit(unSnapAxisFloor(b.w, int(b.metrics.CellWidth), b.cellWPx())),
		Height: core.Unit(unSnapAxisFloor(b.h, int(b.metrics.CellHeight), b.cellHPx())),
	}
}

func (b *Backend) BeginFrame() {}
func (b *Backend) EndFrame()   {}

func (b *Backend) SetClip(clip core.UnitRect) {
	b.clip = clip
	// An empty clip clips EVERYTHING (a window squeezed until its
	// client area vanishes must not spill its content); "unclipped"
	// is only ever the state before the first SetClip.
	b.hasClip = true
	// Snap the clip to device pixels once here (see clipPxX0) rather than
	// per pixel in pointVisible.
	b.clipPxX0, b.clipPxY0 = b.pxX(clip.X), b.pxY(clip.Y)
	b.clipPxX1, b.clipPxY1 = b.pxX(clip.X+clip.Width), b.pxY(clip.Y+clip.Height)
}

// SetRoundedClip implements core.RoundedClipper: a zero rect clears
// the constraint.
func (b *Backend) SetRoundedClip(r core.UnitRect, radius core.Unit) {
	b.roundClip = r
	b.roundClipRadius = radius
	b.hasRoundClip = !r.IsEmpty()
	if !b.hasRoundClip {
		return
	}
	b.roundPxX0, b.roundPxY0 = b.pxX(r.X), b.pxY(r.Y)
	b.roundPxX1, b.roundPxY1 = b.pxX(r.X+r.Width), b.pxY(r.Y+r.Height)
	rad := b.pxLen(radius)
	if m := min(b.roundPxX1-b.roundPxX0, b.roundPxY1-b.roundPxY0) / 2; rad > m {
		rad = m
	}
	b.roundPxRad = rad
}

// clipRejects reports whether the device rectangle [x0,x1) x [y0,y1) lies
// entirely outside the framebuffer and every active clip - so a draw confined
// to it writes nothing and can be skipped wholesale. A cheap bounding-box test
// (the rounded clip is treated as its bounding rect) that lets clipped paints
// skip fully off-clip glyph/image composites instead of testing every pixel.
func (b *Backend) clipRejects(x0, y0, x1, y1 int) bool {
	if x1 <= 0 || y1 <= 0 || x0 >= b.w || y0 >= b.h {
		return true
	}
	if b.pxClipActive && (x1 <= b.pxClipX0 || x0 >= b.pxClipX1) {
		return true
	}
	if b.hasClip && (x1 <= b.clipPxX0 || y1 <= b.clipPxY0 || x0 >= b.clipPxX1 || y0 >= b.clipPxY1) {
		return true
	}
	if b.hasRoundClip && (x1 <= b.roundPxX0 || y1 <= b.roundPxY0 || x0 >= b.roundPxX1 || y0 >= b.roundPxY1) {
		return true
	}
	return false
}

// pointVisible applies every active clip constraint to a device
// pixel: framebuffer bounds, the rectangular clip, and the rounded
// clip's rect and corner arcs (hard-edged, pixel centers).
func (b *Backend) pointVisible(x, y int) bool {
	if x < 0 || y < 0 || x >= b.w || y >= b.h {
		return false
	}
	if b.pxClipActive && (x < b.pxClipX0 || x >= b.pxClipX1) {
		return false
	}
	if b.hasClip {
		if x < b.clipPxX0 || y < b.clipPxY0 || x >= b.clipPxX1 || y >= b.clipPxY1 {
			return false
		}
	}
	if b.hasRoundClip {
		rx0, ry0, rx1, ry1 := b.roundPxX0, b.roundPxY0, b.roundPxX1, b.roundPxY1
		if x < rx0 || y < ry0 || x >= rx1 || y >= ry1 {
			return false
		}
		rad := b.roundPxRad
		if rad > 0 {
			// Inside a corner box: require the pixel center within
			// the corner arc.
			cx, cy := 0.0, 0.0
			inCorner := false
			switch {
			case x < rx0+rad && y < ry0+rad:
				cx, cy, inCorner = float64(rx0+rad), float64(ry0+rad), true
			case x >= rx1-rad && y < ry0+rad:
				cx, cy, inCorner = float64(rx1-rad), float64(ry0+rad), true
			case x < rx0+rad && y >= ry1-rad:
				cx, cy, inCorner = float64(rx0+rad), float64(ry1-rad), true
			case x >= rx1-rad && y >= ry1-rad:
				cx, cy, inCorner = float64(rx1-rad), float64(ry1-rad), true
			}
			if inCorner {
				dx := float64(x) + 0.5 - cx
				dy := float64(y) + 0.5 - cy
				if dx*dx+dy*dy > float64(rad)*float64(rad) {
					return false
				}
			}
		}
	}
	return true
}

// --- Color resolution ---

// rgba resolves a style color to a framebuffer color, reading the 16
// standard colors and the default fg/bg from the active theme palette
// each time so a theme switch shows on the next paint (no cached copy
// to invalidate). True colors (>=256) carry their own RGB.
func (b *Backend) rgba(c style.Color, isFg bool) color.RGBA {
	switch {
	case c == style.ColorDefault:
		return b.defaultColor(isFg)
	case c >= 0 && c < 16:
		return termRGBA(style.ActiveTermANSIColor(int(c)))
	case c >= 256:
		v := uint32(c - 256)
		return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
	default:
		return b.defaultColor(isFg)
	}
}

// defaultColor returns the active theme's default foreground or
// background (used for style.ColorDefault and unknown indices).
func (b *Backend) defaultColor(isFg bool) color.RGBA {
	if isFg {
		return termRGBA(style.ActiveTermPalette.Foreground)
	}
	return termRGBA(style.ActiveTermPalette.Background)
}

func (b *Backend) styleColors(s style.CellStyle) (fg, bg color.RGBA) {
	fg = b.rgba(s.Fg, true)
	bg = b.rgba(s.Bg, false)
	if s.Attrs&style.StyleReverse != 0 {
		fg, bg = bg, fg
	}
	return fg, bg
}

// --- Pixel helpers (clip-aware) ---

func (b *Backend) fillPx(x0, y0, x1, y1 int, c color.RGBA) {
	if b.hasClip {
		if x0 < b.clipPxX0 {
			x0 = b.clipPxX0
		}
		if y0 < b.clipPxY0 {
			y0 = b.clipPxY0
		}
		if x1 > b.clipPxX1 {
			x1 = b.clipPxX1
		}
		if y1 > b.clipPxY1 {
			y1 = b.clipPxY1
		}
	}
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > b.w {
		x1 = b.w
	}
	if y1 > b.h {
		y1 = b.h
	}
	if x1 <= x0 || y1 <= y0 {
		return
	}
	if b.hasRoundClip {
		// The rounded clip only carves the four corners; clamp X to its
		// rect once, then per-pixel-test just the corner-band rows and fill
		// the interior rows straight.
		if x0 < b.roundPxX0 {
			x0 = b.roundPxX0
		}
		if x1 > b.roundPxX1 {
			x1 = b.roundPxX1
		}
		rad := b.roundPxRad
		for y := y0; y < y1; y++ {
			if y < b.roundPxY0 || y >= b.roundPxY1 {
				continue
			}
			if rad > 0 && (y < b.roundPxY0+rad || y >= b.roundPxY1-rad) {
				for x := x0; x < x1; x++ {
					if b.pointVisible(x, y) {
						b.img.SetRGBA(x, y, c)
					}
				}
			} else {
				b.fillRowPx(y, x0, x1, c)
			}
		}
		return
	}
	for y := y0; y < y1; y++ {
		b.fillRowPx(y, x0, x1, c)
	}
}

// fillRowPx paints a solid horizontal run [x0, x1) at row y straight into
// the framebuffer (no per-pixel bounds check - the caller has clamped),
// growing the run by copy-doubling. The caller guarantees the span is in
// bounds and passes every active clip.
func (b *Backend) fillRowPx(y, x0, x1 int, c color.RGBA) {
	if x1 <= x0 {
		return
	}
	pix := b.img.Pix
	o := b.img.PixOffset(x0, y)
	end := o + (x1-x0)*4
	pix[o], pix[o+1], pix[o+2], pix[o+3] = c.R, c.G, c.B, c.A
	for filled := o + 4; filled < end; {
		filled += copy(pix[filled:end], pix[o:filled])
	}
}

// blendRectPx blends `over` at the given alpha (0..1) over the existing
// pixels in the rectangle, respecting the clip and rounded clip - the
// translucent counterpart of fillPx.
func (b *Backend) blendRectPx(x0, y0, x1, y1 int, over color.RGBA, alpha float64) {
	if b.hasClip {
		cx0, cy0 := b.pxX(b.clip.X), b.pxY(b.clip.Y)
		cx1, cy1 := b.pxX(b.clip.X+b.clip.Width), b.pxY(b.clip.Y+b.clip.Height)
		if x0 < cx0 {
			x0 = cx0
		}
		if y0 < cy0 {
			y0 = cy0
		}
		if x1 > cx1 {
			x1 = cx1
		}
		if y1 > cy1 {
			y1 = cy1
		}
	}
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > b.w {
		x1 = b.w
	}
	if y1 > b.h {
		y1 = b.h
	}
	if x1 <= x0 || y1 <= y0 {
		return
	}

	// Fixed-point coverage in [0,256]: mix = (over*a + under*(256-a)) >> 8,
	// matching blend()'s float result within a unit. The alpha channel mixes
	// with 255 as `over` (opaque ground stays opaque; a transparent corner
	// keeps its smooth rim), same as blend(). Direct Pix indexing and integer
	// math replace per-pixel RGBAAt/SetRGBA + float - this is the hot path for
	// the scroll-area edge fades and the modal dim.
	a := int(alpha*256 + 0.5)
	if a <= 0 {
		return
	}
	if a > 256 {
		a = 256
	}
	inv := uint32(256 - a)
	oR := uint32(over.R) * uint32(a)
	oG := uint32(over.G) * uint32(a)
	oB := uint32(over.B) * uint32(a)
	oA := uint32(255) * uint32(a)
	pix := b.img.Pix
	for y := y0; y < y1; y++ {
		o := b.img.PixOffset(x0, y)
		for x := x0; x < x1; x++ {
			if b.hasRoundClip && !b.pointVisible(x, y) {
				o += 4
				continue
			}
			pix[o] = uint8((oR + uint32(pix[o])*inv) >> 8)
			pix[o+1] = uint8((oG + uint32(pix[o+1])*inv) >> 8)
			pix[o+2] = uint8((oB + uint32(pix[o+2])*inv) >> 8)
			pix[o+3] = uint8((oA + uint32(pix[o+3])*inv) >> 8)
			o += 4
		}
	}
}

func blend(over, under color.RGBA, alpha float64) color.RGBA {
	mix := func(a, b uint8) uint8 {
		return uint8(float64(a)*alpha + float64(b)*(1-alpha))
	}
	// Alpha mixes like the channels (over is always painted at 255):
	// opaque ground stays opaque - the historical result - while
	// antialiased edges over an untouched (alpha-0) framebuffer carry
	// partial alpha, so transparent torn-window corners composite
	// with smooth rims instead of dark fringes.
	return color.RGBA{mix(over.R, under.R), mix(over.G, under.G), mix(over.B, under.B), mix(255, under.A)}
}

// Clear fills the entire surface.
func (b *Backend) Clear(s style.CellStyle) {
	_, bg := b.styleColors(s)
	saved, savedHas := b.clip, b.hasClip
	savedRound := b.hasRoundClip
	b.hasClip, b.hasRoundClip = false, false
	b.fillPx(0, 0, b.w, b.h, bg)
	b.clip, b.hasClip, b.hasRoundClip = saved, savedHas, savedRound
}

// --- Text ---

// engine is the shared text engine (D5/D6): one per process, shared
// by every raster backend (SDL recreates backends on resize; the
// engine and its font caches survive). The same engine answers
// core.TextMeasurer, so measurement and painting cannot disagree.
var (
	engineOnce sync.Once
	engineInst *text.Engine
)

func engine() *text.Engine {
	engineOnce.Do(func() {
		engineInst = text.NewEngine()
		// Well-known OS faces join the TAIL of the fallback chain:
		// the embedded faces stay authoritative, system fonts only
		// catch runes nothing embedded covers.
		engineInst.LoadSystemFallbacks()
		// macOS's UI font, addressable by name so native-mode menu
		// shortcuts render in Apple's own typeface (no-op off macOS).
		engineInst.LoadMacMenuFont()
	})
	return engineInst
}

// Engine exposes the shared text engine (e.g. to register fonts).
func (b *Backend) Engine() *text.Engine { return engine() }

// MeasureText implements core.TextMeasurer: the graphical target's
// measurement is the shaper's, matching the proportional render
// exactly. (The text-based system keeps its own cell-arithmetic
// answer; this never applies there.)
func (b *Backend) MeasureText(f *core.Font, s string) core.Unit {
	return engine().Measure(f, s)
}

// LineHeight implements core.TextMeasurer: Size * 4/3 units by the
// engine's denomination (12pt = 16 units = one default cell row).
func (b *Backend) LineHeight(f *core.Font) core.Unit {
	return engine().LineHeight(f)
}

// cellAdvance is the terminal-region advance for one rune: one cell of
// the backend's current denomination (two for wide characters). Used
// only by the cell primitives (DrawCell, glyph tiling), never by
// DrawText. At the default 8x16 denomination this is 8 (16 wide).
func (b *Backend) cellAdvance(ch rune) core.Unit {
	adv := b.metrics.CellWidth
	if isWide(ch) {
		adv += b.metrics.CellWidth
	}
	return adv
}

// cellFontSize is the cell-face point size whose line box fills a cell
// of the given height. LineHeight = Size*4/3, so Size = height*3/4; at
// the default 16-unit cell this is 12pt (the historical cell face).
func cellFontSize(cellHeight core.Unit) int {
	s := int(cellHeight) * 3 / 4
	if s < 1 {
		s = 1
	}
	return s
}

func isWide(ch rune) bool {
	return (ch >= 0x4E00 && ch <= 0x9FFF) || (ch >= 0x3400 && ch <= 0x4DBF) ||
		(ch >= 0xAC00 && ch <= 0xD7AF) || (ch >= 0x3040 && ch <= 0x30FF) ||
		(ch >= 0xFF00 && ch <= 0xFFEF)
}

// drawRune paints one glyph inside its advance box: background fill,
// then the glyph centered horizontally, baseline-aligned.
// cellFont is the face for the CELL primitive (DrawCell): the
// monospace cell font at the size whose line box equals one cell
// row. Shaping through the engine gives chrome glyphs (checkmarks,
// markers, box drawing) the same per-rune fallback chain as text.
var cellFont = &core.Font{Name: "Monday", Size: 12}

func (b *Backend) drawRune(x, y core.Unit, ch rune, adv, cellH core.Unit, fg, bg color.RGBA, underline, transparentBg bool) {
	// Cell-snap the box so adjacent cells tile pixel-exact at any
	// font_size: origin and far edges each convert through pxX/pxY.
	xPx, yPx := b.pxX(x), b.pxY(y)
	advPx := b.pxX(x+adv) - xPx
	hPx := b.pxY(y+cellH) - yPx
	if !transparentBg {
		b.fillPx(xPx, yPx, xPx+advPx, yPx+hPx, bg)
	}

	if ch != ' ' && ch != 0 {
		// The cell face is sized to the cell so chrome glyphs (checkmarks,
		// markers, brackets, box drawing) grow with the host font_size
		// instead of staying at the historical 12pt.
		f := &core.Font{Name: cellFont.Name, Size: cellFontSize(cellH)}
		ti := b.cachedTextImage(f, string(ch), fg, color.RGBA{}, false, false)
		pad := (advPx - ti.img.Rect.Dx()) / 2
		if pad < 0 {
			pad = 0
		}
		b.compositeRGBA(xPx+pad, yPx, ti.img)
	}
	if underline {
		b.fillPx(xPx, b.pxY(y+cellH-2), xPx+advPx, b.pxY(y+cellH-1), fg)
	}
}

// DrawCell is the CELL primitive: terminal-style regions (D23
// carve-out - PurfecTerm and friends) paint through it, so its
// advance is exactly one cell of the backend's denomination regardless
// of any font. This is not a second text path: UI text goes through
// DrawText's shaped path below.
func (b *Backend) DrawCell(x, y core.Unit, ch rune, s style.CellStyle) {
	fg, bg := b.styleColors(s)
	b.drawRune(x, y, ch, b.cellAdvance(ch), b.metrics.CellHeight, fg, bg,
		s.Attrs&style.StyleUnderline != 0, s.Bg == style.ColorTransparent)
}

// textImage is one cached rendered string at the backend's scale,
// ready to blit. Opaque images include the background; transparent
// ones (style.ColorTransparent background) carry alpha and are
// composited over the framebuffer.
type textImage struct {
	img    *image.RGBA
	width  core.Unit
	opaque bool
}

// textKey identifies a cached render. It is an all-comparable struct
// (color.RGBA is comparable), so it maps directly with no per-call key
// string to build - the string-concat key this replaced allocated on every
// DrawText.
type textKey struct {
	name      string
	text      string
	fg, bg    color.RGBA
	style     int
	size      int
	scale     int
	fontSize  int
	underline bool
	opaque    bool
}

// textImageCache caches rendered strings across frames (and across
// backends - SDL recreates the backend on resize; scale is in the
// key). Same two-generation scheme as the engine's shape cache.
// Without it every frame re-rasterizes every glyph outline of every
// visible string.
var textImageCache = struct {
	sync.Mutex
	epoch     uint64
	cur, prev map[textKey]textImage
}{cur: map[textKey]textImage{}, prev: map[textKey]textImage{}}

const textImageCacheMax = 512

func textImageKey(f *core.Font, s string, fg, bg color.RGBA, underline, opaque bool, scale, fontSize int) textKey {
	if f == nil {
		f = core.DefaultFont()
	}
	return textKey{
		name: f.Name, text: s, fg: fg, bg: bg,
		style: int(f.Style), size: f.Size,
		scale: scale, fontSize: fontSize,
		underline: underline, opaque: opaque,
	}
}

// cachedTextImage returns the cached render of one string (see
// textImageCache), rasterizing on a miss.
func (b *Backend) cachedTextImage(f *core.Font, s string, fg, bg color.RGBA, underline, opaque bool) textImage {
	key := textImageKey(f, s, fg, bg, underline, opaque, b.scale, b.fontSize)
	textImageCache.Lock()
	defer textImageCache.Unlock()
	if e := engine().Epoch(); e != textImageCache.epoch {
		// Font set changed: shaped output may differ - flush.
		textImageCache.epoch = e
		textImageCache.cur = map[textKey]textImage{}
		textImageCache.prev = map[textKey]textImage{}
	}
	ti, ok := textImageCache.cur[key]
	if !ok {
		if ti, ok = textImageCache.prev[key]; ok {
			textImageCache.cur[key] = ti // keep the working set warm
		}
	}
	if !ok {
		ti = b.renderTextImage(f, s, fg, bg, underline, opaque)
		if len(textImageCache.cur) >= textImageCacheMax {
			textImageCache.prev = textImageCache.cur
			textImageCache.cur = map[textKey]textImage{}
		}
		textImageCache.cur[key] = ti
	}
	return ti
}

// renderTextImage rasterizes one string into a fresh image: opaque
// (background filled) or transparent (alpha only, for compositing).
func (b *Backend) renderTextImage(f *core.Font, s string, fg, bg color.RGBA, underline, opaque bool) textImage {
	sp := engine().ShapeRun(f, s)
	w := sp.Width()
	h := engine().LineHeight(f)
	// Proportional text is not cell-aligned: size and rasterize it at the
	// unsnapped fractional pixels-per-unit.
	img := image.NewRGBA(image.Rect(0, 0, b.pxLen(w), b.pxLen(h)))
	if opaque {
		for i := range img.Pix {
			switch i % 4 {
			case 0:
				img.Pix[i] = bg.R
			case 1:
				img.Pix[i] = bg.G
			case 2:
				img.Pix[i] = bg.B
			case 3:
				img.Pix[i] = 255
			}
		}
	}
	text.Render(img, sp, 0, 0, b.pxPerUnit(), fg)
	if underline && len(sp.Lines) > 0 {
		uy := b.pxLen(sp.Lines[0].Baseline) + b.scale
		for py := uy; py < uy+b.scale && py < img.Rect.Max.Y; py++ {
			for px := 0; px < img.Rect.Max.X; px++ {
				img.SetRGBA(px, py, fg)
			}
		}
	}
	return textImage{img: img, width: w, opaque: opaque}
}

// compositeRGBA alpha-blends a transparent text image over the
// framebuffer (source pixels are alpha-premultiplied, as image.RGBA
// defines), honoring every active clip.
func (b *Backend) compositeRGBA(xPx, yPx int, src *image.RGBA) {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	if sw <= 0 || sh <= 0 {
		return
	}
	if b.clipRejects(xPx, yPx, xPx+sw, yPx+sh) {
		return
	}
	dst := b.img

	// When no clip is active (the common case for text), clamp the destination
	// rectangle to the framebuffer once so the inner loop needs no per-pixel
	// visibility test - it walks the Pix slices directly. Any clip falls back
	// to the per-pixel pointVisible test, but still via direct indexing.
	noClip := !b.pxClipActive && !b.hasClip && !b.hasRoundClip
	col0, row0, col1, row1 := 0, 0, sw, sh
	if noClip {
		if xPx < 0 {
			col0 = -xPx
		}
		if yPx < 0 {
			row0 = -yPx
		}
		if xPx+col1 > b.w {
			col1 = b.w - xPx
		}
		if yPx+row1 > b.h {
			row1 = b.h - yPx
		}
		if col1 <= col0 || row1 <= row0 {
			return
		}
	}

	sp, dp := src.Pix, dst.Pix
	for row := row0; row < row1; row++ {
		so := src.PixOffset(src.Rect.Min.X+col0, src.Rect.Min.Y+row)
		do := dst.PixOffset(xPx+col0, yPx+row)
		for col := col0; col < col1; col++ {
			sa := sp[so+3]
			if sa == 0 {
				so += 4
				do += 4
				continue
			}
			if !noClip && !b.pointVisible(xPx+col, yPx+row) {
				so += 4
				do += 4
				continue
			}
			if sa == 255 {
				dp[do], dp[do+1], dp[do+2], dp[do+3] = sp[so], sp[so+1], sp[so+2], 255
			} else {
				inv := uint32(255 - sa)
				dp[do] = uint8(uint32(sp[so]) + uint32(dp[do])*inv/255)
				dp[do+1] = uint8(uint32(sp[so+1]) + uint32(dp[do+1])*inv/255)
				dp[do+2] = uint8(uint32(sp[so+2]) + uint32(dp[do+2])*inv/255)
				dp[do+3] = 255
			}
			so += 4
			do += 4
		}
	}
}

// compositeMaskTintPx composites a color-independent coverage mask (only the
// mask's alpha channel is read) tinted with (tr,tg,tb) over the framebuffer at
// (xPx,yPx): out = tint*cov + dst*(255-cov). This is the caller-tinted twin of
// compositeRGBA - the terminal caches one grayscale glyph mask per shape and
// tints it per cell, so color-varying content (e.g. a fire animation) stops
// thrashing the per-color glyph cache.
func (b *Backend) compositeMaskTintPx(xPx, yPx int, mask *image.RGBA, tr, tg, tb uint8) {
	sw, sh := mask.Rect.Dx(), mask.Rect.Dy()
	if sw <= 0 || sh <= 0 {
		return
	}
	if b.clipRejects(xPx, yPx, xPx+sw, yPx+sh) {
		return
	}
	dst := b.img
	noClip := !b.pxClipActive && !b.hasClip && !b.hasRoundClip
	col0, row0, col1, row1 := 0, 0, sw, sh
	if noClip {
		if xPx < 0 {
			col0 = -xPx
		}
		if yPx < 0 {
			row0 = -yPx
		}
		if xPx+col1 > b.w {
			col1 = b.w - xPx
		}
		if yPx+row1 > b.h {
			row1 = b.h - yPx
		}
		if col1 <= col0 || row1 <= row0 {
			return
		}
	}
	sp, dp := mask.Pix, dst.Pix
	for row := row0; row < row1; row++ {
		so := mask.PixOffset(mask.Rect.Min.X+col0, mask.Rect.Min.Y+row)
		do := dst.PixOffset(xPx+col0, yPx+row)
		for col := col0; col < col1; col++ {
			cov := uint32(sp[so+3])
			if cov == 0 {
				so += 4
				do += 4
				continue
			}
			if !noClip && !b.pointVisible(xPx+col, yPx+row) {
				so += 4
				do += 4
				continue
			}
			if cov == 255 {
				dp[do], dp[do+1], dp[do+2], dp[do+3] = tr, tg, tb, 255
			} else {
				inv := 255 - cov
				dp[do] = uint8((uint32(tr)*cov + uint32(dp[do])*inv) / 255)
				dp[do+1] = uint8((uint32(tg)*cov + uint32(dp[do+1])*inv) / 255)
				dp[do+2] = uint8((uint32(tb)*cov + uint32(dp[do+2])*inv) / 255)
				dp[do+3] = 255
			}
			so += 4
			do += 4
		}
	}
}

// DrawImageMaskTintPx composites a coverage mask tinted with (r,g,bl) at the
// device-pixel anchor (see compositeMaskTintPx). Implements core.MaskTintDrawer.
func (b *Backend) DrawImageMaskTintPx(xPx, yPx int, mask *image.RGBA, r, g, bl uint8) {
	b.compositeMaskTintPx(xPx, yPx, mask, r, g, bl)
}

// blitRGBA copies a rendered image to (xPx, yPx), honoring every
// active clip. Fully visible blits take a per-row copy.
func (b *Backend) blitRGBA(xPx, yPx int, src *image.RGBA) {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	if sw <= 0 || sh <= 0 {
		return
	}
	if b.clipRejects(xPx, yPx, xPx+sw, yPx+sh) {
		return
	}
	fullyVisible := !b.hasRoundClip && !b.pxClipActive &&
		b.pointVisible(xPx, yPx) && b.pointVisible(xPx+sw-1, yPx) &&
		b.pointVisible(xPx, yPx+sh-1) && b.pointVisible(xPx+sw-1, yPx+sh-1)
	if fullyVisible {
		for row := 0; row < sh; row++ {
			so := src.PixOffset(0, row)
			do := b.img.PixOffset(xPx, yPx+row)
			copy(b.img.Pix[do:do+sw*4], src.Pix[so:so+sw*4])
		}
		return
	}
	for row := 0; row < sh; row++ {
		for col := 0; col < sw; col++ {
			if b.pointVisible(xPx+col, yPx+row) {
				b.img.SetRGBA(xPx+col, yPx+row, src.RGBAAt(col, row))
			}
		}
	}
}

// DrawText is the graphical renderer's one text path: fully shaped,
// proportional (D6). Choosing a monospaced family yields the
// cell-gridded look; there is no separate grid-quantized path here.
// Measurement (core.TextMeasurer, answered by this backend) uses the
// same engine, so the returned advance always equals the painted one.
// Rendered strings are cached: a repeated frame blits pixels instead
// of re-shaping and re-rasterizing.
func (b *Backend) DrawText(x, y core.Unit, s string, st style.CellStyle, f *core.Font) core.Unit {
	if s == "" {
		return 0
	}
	fg, bg := b.styleColors(st)
	underline := st.Attrs&style.StyleUnderline != 0
	opaque := st.Bg != style.ColorTransparent

	ti := b.cachedTextImage(f, s, fg, bg, underline, opaque)

	if ti.opaque {
		b.blitRGBA(b.pxX(x), b.pxY(y), ti.img)
	} else {
		b.compositeRGBA(b.pxX(x), b.pxY(y), ti.img)
	}
	return ti.width
}

// DrawTextPx implements core.TextPixelDrawer: draw a string with its
// top-left at an absolute device pixel and return the advance in device
// pixels. The image is the same cached raster DrawText blits, so its pixel
// width equals pxLen(Measure(s)) - accumulating that advance places the
// next segment (or a caret) exactly on the following glyph, at any font
// size, without re-snapping through the cell rate.
func (b *Backend) DrawTextPx(xPx, yPx int, s string, st style.CellStyle, f *core.Font) int {
	if s == "" {
		return 0
	}
	fg, bg := b.styleColors(st)
	underline := st.Attrs&style.StyleUnderline != 0
	opaque := st.Bg != style.ColorTransparent

	ti := b.cachedTextImage(f, s, fg, bg, underline, opaque)
	if ti.opaque {
		b.blitRGBA(xPx, yPx, ti.img)
	} else {
		b.compositeRGBA(xPx, yPx, ti.img)
	}
	return ti.img.Bounds().Dx()
}

// DrawTextPxClipped implements core.ClippedTextPixelDrawer: draw a string
// at an absolute device pixel but reveal only the columns in [clipX0,
// clipX1). The selection colors its text this way - drawing the WHOLE
// stable run and clipping to the selected columns - so the selected glyphs
// are the exact same rasters as the base run (never re-shaped or re-placed)
// and don't jitter as the selection grows; only the clip edge moves.
func (b *Backend) DrawTextPxClipped(xPx, yPx int, s string, st style.CellStyle, f *core.Font, clipX0, clipX1 int) int {
	if s == "" || clipX1 <= clipX0 {
		return 0
	}
	fg, bg := b.styleColors(st)
	underline := st.Attrs&style.StyleUnderline != 0
	opaque := st.Bg != style.ColorTransparent
	ti := b.cachedTextImage(f, s, fg, bg, underline, opaque)

	b.pxClipActive, b.pxClipX0, b.pxClipX1 = true, clipX0, clipX1
	if ti.opaque {
		b.blitRGBA(xPx, yPx, ti.img)
	} else {
		b.compositeRGBA(xPx, yPx, ti.img)
	}
	b.pxClipActive = false
	return ti.img.Bounds().Dx()
}

func (b *Backend) DrawTextAligned(bounds core.UnitRect, s string, hAlign, vAlign core.Alignment, st style.CellStyle, f *core.Font) {
	w := engine().Measure(f, s)
	h := engine().LineHeight(f)
	x := bounds.X
	switch hAlign {
	case core.AlignCenter:
		x += (bounds.Width - w) / 2
	case core.AlignRight:
		x += bounds.Width - w
	}
	y := bounds.Y
	switch vAlign {
	case core.AlignMiddle:
		y += (bounds.Height - h) / 2
	case core.AlignBottom:
		y += bounds.Height - h
	}
	b.DrawText(x, y, s, st, f)
}

// --- Fills & lines: REAL pixels, not box runes (D23) ---

// shadeAlpha maps the classic shade runes to fg-over-bg blends.
var shadeAlpha = map[rune]float64{
	'░': 0.25, '▒': 0.5, '▓': 0.75, '█': 1.0,
}

func (b *Backend) FillRect(r core.UnitRect, ch rune, s style.CellStyle) {
	fg, bg := b.styleColors(s)
	transparent := s.Bg == style.ColorTransparent
	switch {
	case ch == ' ' || ch == 0:
		if transparent {
			return // nothing to paint: the background shows through
		}
		b.fillPx(b.pxX(r.X), b.pxY(r.Y), b.pxX(r.X+r.Width), b.pxY(r.Y+r.Height), bg)
	default:
		if a, ok := shadeAlpha[ch]; ok {
			if transparent {
				// Blend the shade's foreground over what is beneath.
				for y := b.pxY(r.Y); y < b.pxY(r.Y+r.Height); y++ {
					for x := b.pxX(r.X); x < b.pxX(r.X+r.Width); x++ {
						b.blendPx(x, y, fg, a)
					}
				}
				return
			}
			b.fillPx(b.pxX(r.X), b.pxY(r.Y), b.pxX(r.X+r.Width), b.pxY(r.Y+r.Height), blend(fg, bg, a))
			return
		}
		// Arbitrary fill character: tile the glyph one cell at a time.
		cw, chH := b.metrics.CellWidth, b.metrics.CellHeight
		for y := r.Y; y < r.Y+r.Height; y += chH {
			for x := r.X; x < r.X+r.Width; x += cw {
				b.drawRune(x, y, ch, cw, chH, fg, bg, false, s.Bg == style.ColorTransparent)
			}
		}
	}
}

// borderWeight classifies a BorderStyle into line rendering.
func borderWeight(bs style.BorderStyle) (thickness int, double bool) {
	switch bs {
	case style.BorderDouble:
		return 1, true
	case style.BorderHeavy:
		return 2, false
	default:
		return 1, false
	}
}

// DrawRect draws a real rectangle outline, centered in the border
// cell band so it aligns with TUI-era layout (borders occupy one
// 8x16 cell ring).
func (b *Backend) DrawRect(r core.UnitRect, bs style.BorderStyle, s style.CellStyle) {
	fg, _ := b.styleColors(s)
	th, dbl := borderWeight(bs)
	th *= b.scale

	// The border sits centered in the cell ring: 4 units in horizontally
	// (half the 8-wide cell), 8 units in vertically (half the 16-tall
	// cell). Cell-snap so it lands on the same grid as the fills it rings.
	left := b.pxX(r.X + 4)
	right := b.pxX(r.X + r.Width - 4)
	top := b.pxY(r.Y + 8)
	bottom := b.pxY(r.Y + r.Height - 8)

	stroke := func(x0, y0, x1, y1 int) {
		b.fillPx(x0, y0, x1, y1, fg)
	}
	rect := func(l, t, rr, bt int) {
		stroke(l, t, rr, t+th)   // top
		stroke(l, bt-th, rr, bt) // bottom
		stroke(l, t, l+th, bt)   // left
		stroke(rr-th, t, rr, bt) // right
	}
	rect(left, top, right, bottom)
	if dbl {
		d := b.pxLen(3)
		rect(left-d, top-d, right+d, bottom+d)
	}
}

// strokePx maps a border style to a stroke weight in device pixels:
// double/heavy is the window-frame border, whose width is the configured
// (or default) value from core.WindowFrameBorderPx; single stays a 1px
// hairline. Rounded strokes are used only for window frames, so honoring
// the configured frame width here is safe.
func strokePx(bs style.BorderStyle) int {
	if bs == style.BorderDouble || bs == style.BorderHeavy {
		return core.WindowFrameBorderPx()
	}
	return 1
}

// inClip reports whether a device pixel is inside every active clip.
func (b *Backend) inClip(x, y int) bool {
	return b.pointVisible(x, y)
}

// blendPx composites c over the existing pixel with coverage a.
func (b *Backend) blendPx(x, y int, c color.RGBA, a float64) {
	if a <= 0 || !b.inClip(x, y) {
		return
	}
	if a >= 1 {
		b.img.SetRGBA(x, y, c)
		return
	}
	b.img.SetRGBA(x, y, blend(c, b.img.RGBAAt(x, y), a))
}

// DrawArcWedge implements core.ArcWedgeDrawer: per-pixel coverage
// against the quarter ellipse inscribed in r and centered on the
// chosen corner, so silhouette curves come out antialiased. The part
// of r outside the arc fills with the style's background; a stroke of
// the given weight follows the arc in its foreground (0 = no stroke).
func (b *Backend) DrawArcWedge(r core.UnitRect, centerRight, centerBottom bool, strokeW core.Unit, offXPx, offYPx int, s style.CellStyle) {
	fg, bg := b.styleColors(s)
	// offXPx/offYPx rigidly translate the snapped wedge by an exact device-pixel
	// amount (both box edges and the ellipse centre move together, so the shape
	// is unchanged - only its position shifts).
	x0, y0 := b.pxX(r.X)+offXPx, b.pxY(r.Y)+offYPx
	x1, y1 := b.pxX(r.X+r.Width)+offXPx, b.pxY(r.Y+r.Height)+offYPx
	w, h := x1-x0, y1-y0
	if w <= 0 || h <= 0 {
		return
	}
	rx, ry := float64(w), float64(h)
	cx, cy := float64(x0), float64(y0)
	if centerRight {
		cx = float64(x1)
	}
	if centerBottom {
		cy = float64(y1)
	}
	th := float64(b.pxLen(strokeW))
	clampCov := func(v float64) float64 {
		if v < 0 {
			return 0
		}
		if v > 1 {
			return 1
		}
		return v
	}
	for py := y0; py < y1; py++ {
		for px := x0; px < x1; px++ {
			nx := (float64(px) + 0.5 - cx) / rx
			ny := (float64(py) + 0.5 - cy) / ry
			nd := math.Hypot(nx, ny)
			// Signed pixel distance outside the arc: the normalized
			// distance converted back through the local radial
			// gradient (exactly d - r for a circle).
			pd := -math.Min(rx, ry)
			if nd > 0 {
				pd = (nd - 1) * nd / math.Hypot(nx/rx, ny/ry)
			}
			cov := clampCov(pd + th + 0.5) // stroke band + wedge fill
			fillCov := clampCov(pd + 0.5)  // wedge fill only
			if th <= 0 {
				cov = fillCov
			}
			if cov <= 0 {
				continue
			}
			src := bg
			if fillCov < cov {
				src = blend(bg, fg, fillCov/cov)
			}
			b.blendPx(px, py, src, cov)
		}
	}
}

// DrawRoundedRect implements core.RoundedRectDrawer: one pass paints
// the fill (style background) and the stroke (style foreground, at
// strokePx weight) with anti-aliased corners. Window frames on pixel
// surfaces are exactly one of these.
func (b *Backend) DrawRoundedRect(r core.UnitRect, radius core.Unit, bs style.BorderStyle, s style.CellStyle) {
	b.roundedRect(r, radius, bs, s, true)
}

// StrokeRoundedRect implements the stroke-only variant: the interior
// is left untouched. Window frames re-stroke over their edge-to-edge
// content with this.
func (b *Backend) StrokeRoundedRect(r core.UnitRect, radius core.Unit, bs style.BorderStyle, s style.CellStyle) {
	b.roundedRect(r, radius, bs, s, false)
}

// StrokeRoundedRectWeight implements core.RoundedRectWeightStroker: a
// stroke-only rounded rectangle at an explicit device-pixel weight, used
// for the thin inner line of a single-border window frame.
func (b *Backend) StrokeRoundedRectWeight(r core.UnitRect, radius core.Unit, strokePx int, s style.CellStyle) {
	if strokePx < 1 {
		strokePx = 1
	}
	b.roundedRectTh(r, radius, strokePx, s, false)
}

func (b *Backend) roundedRect(r core.UnitRect, radius core.Unit, bs style.BorderStyle, s style.CellStyle, fill bool) {
	b.roundedRectTh(r, radius, strokePx(bs), s, fill)
}

func (b *Backend) roundedRectTh(r core.UnitRect, radius core.Unit, th int, s style.CellStyle, fill bool) {
	fg, bg := b.styleColors(s)
	x0, y0 := b.pxX(r.X), b.pxY(r.Y)
	x1, y1 := b.pxX(r.X+r.Width), b.pxY(r.Y+r.Height)
	w, h := x1-x0, y1-y0
	if w <= 0 || h <= 0 {
		return
	}
	rad := b.pxLen(radius)
	if m := min(w, h) / 2; rad > m {
		rad = m
	}

	if rad <= 0 {
		if fill {
			b.fillPx(x0+th, y0+th, x1-th, y1-th, bg)
		}
		b.fillPx(x0, y0, x1, y0+th, fg)
		b.fillPx(x0, y1-th, x1, y1, fg)
		b.fillPx(x0, y0, x0+th, y1, fg)
		b.fillPx(x1-th, y0, x1, y1, fg)
		return
	}

	// Straight bands (everything outside the four corner boxes).
	b.fillPx(x0+rad, y0, x1-rad, y0+th, fg) // top stroke
	b.fillPx(x0+rad, y1-th, x1-rad, y1, fg) // bottom stroke
	b.fillPx(x0, y0+rad, x0+th, y1-rad, fg) // left stroke
	b.fillPx(x1-th, y0+rad, x1, y1-rad, fg) // right stroke
	if fill {
		b.fillPx(x0+th, y0+rad, x1-th, y1-rad, bg)  // center
		b.fillPx(x0+rad, y0+th, x1-rad, y0+rad, bg) // top band
		b.fillPx(x0+rad, y1-rad, x1-rad, y1-th, bg) // bottom band
	}

	// Corners: per-pixel coverage against the corner circle, blended
	// for smooth edges. cx/cy is the circle center in continuous
	// pixel coordinates; sx/sy the corner box origin.
	corners := [4]struct {
		cx, cy float64
		sx, sy int
	}{
		{float64(x0 + rad), float64(y0 + rad), x0, y0},             // top-left
		{float64(x1 - rad), float64(y0 + rad), x1 - rad, y0},       // top-right
		{float64(x0 + rad), float64(y1 - rad), x0, y1 - rad},       // bottom-left
		{float64(x1 - rad), float64(y1 - rad), x1 - rad, y1 - rad}, // bottom-right
	}
	clampCov := func(v float64) float64 {
		if v < 0 {
			return 0
		}
		if v > 1 {
			return 1
		}
		return v
	}
	for _, c := range corners {
		for py := c.sy; py < c.sy+rad; py++ {
			for px := c.sx; px < c.sx+rad; px++ {
				d := math.Hypot(float64(px)+0.5-c.cx, float64(py)+0.5-c.cy)
				outer := clampCov(float64(rad) - d + 0.5)
				if outer <= 0 {
					continue
				}
				inner := clampCov(float64(rad-th) - d + 0.5)
				if !fill {
					// Stroke only: paint just the band between the
					// inner and outer boundaries.
					if cov := outer - inner; cov > 0 {
						b.blendPx(px, py, fg, cov)
					}
					continue
				}
				// Source color: fill inside the inner boundary, stroke
				// in the band between inner and outer.
				src := fg
				if inner >= outer {
					src = bg
				} else if inner > 0 {
					src = blend(bg, fg, inner/outer)
				}
				b.blendPx(px, py, src, outer)
			}
		}
	}
}

func (b *Backend) DrawHLine(x, y, width core.Unit, ch rune, s style.CellStyle) {
	fg, _ := b.styleColors(s)
	top := b.pxY(y + 8) // centered in the 16-tall cell band
	b.fillPx(b.pxX(x), top, b.pxX(x+width), top+b.scale, fg)
}

func (b *Backend) DrawVLine(x, y, height core.Unit, ch rune, s style.CellStyle) {
	fg, _ := b.styleColors(s)
	left := b.pxX(x + 4) // centered in the 8-wide cell band
	b.fillPx(left, b.pxY(y), left+b.scale, b.pxY(y+height), fg)
}

func (b *Backend) DrawBox(r core.UnitRect, bs style.BorderStyle, title string, s style.CellStyle) {
	b.DrawRect(r, bs, s)
	if title != "" {
		b.DrawText(r.X+16, r.Y, " "+title+" ", s, nil)
	}
}

// --- Input & misc: the substrate's job; headless stubs here ---

// SmoothPositioning implements core.SmoothPositioner: pixel surfaces
// place window chrome at any unit position.
func (b *Backend) SmoothPositioning() bool { return true }

// GraphicalMode implements core.GraphicalModer (the D1 mode query):
// this backend paints pixels.
func (b *Backend) GraphicalMode() bool { return true }

// DrawImage implements core.ImageDrawer: composite a device-pixel
// image (alpha honored) at a unit position, respecting every active
// clip.
func (b *Backend) DrawImage(x, y core.Unit, img image.Image) {
	b.DrawImagePx(b.pxX(x), b.pxY(y), img)
}

// DrawImagePx implements core.ImageDrawer's device-pixel anchor for
// sub-unit placement (sprite fine positioning, animation offsets).
func (b *Backend) DrawImagePx(xPx, yPx int, img image.Image) {
	if rgba, ok := img.(*image.RGBA); ok {
		b.compositeRGBA(xPx, yPx, rgba)
		return
	}
	// Generic path for other image types.
	bnds := img.Bounds()
	for row := 0; row < bnds.Dy(); row++ {
		for col := 0; col < bnds.Dx(); col++ {
			dx, dy := xPx+col, yPx+row
			if !b.pointVisible(dx, dy) {
				continue
			}
			r, g, bl, a := img.At(bnds.Min.X+col, bnds.Min.Y+row).RGBA()
			if a == 0 {
				continue
			}
			if a == 0xffff {
				b.img.SetRGBA(dx, dy, color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), 255})
				continue
			}
			d := b.img.RGBAAt(dx, dy)
			inv := 0xffff - a
			b.img.SetRGBA(dx, dy, color.RGBA{
				R: uint8((r + uint32(d.R)*0x101*inv/0xffff) >> 8),
				G: uint8((g + uint32(d.G)*0x101*inv/0xffff) >> 8),
				B: uint8((bl + uint32(d.B)*0x101*inv/0xffff) >> 8),
				A: 255,
			})
		}
	}
}

// FillPattern implements core.PatternFiller: an 8x8 two-color bitmap
// tiled across the rect, each pattern bit chunked to chunkPx x
// chunkPx device pixels, anchored at the surface origin.
func (b *Backend) FillPattern(r core.UnitRect, pattern [8]uint8, chunkPx int, s style.CellStyle) {
	if chunkPx < 1 {
		chunkPx = 1
	}
	fg, bg := b.styleColors(s)
	x0, y0 := b.pxX(r.X), b.pxY(r.Y)
	x1, y1 := b.pxX(r.X+r.Width), b.pxY(r.Y+r.Height)

	// Walk whole pattern blocks (origin-anchored), filling each with
	// its color; fillPx clips each block to the rect and clips.
	for by := y0 - mod(y0, chunkPx); by < y1; by += chunkPx {
		row := pattern[(by/chunkPx)%8]
		for bx := x0 - mod(x0, chunkPx); bx < x1; bx += chunkPx {
			c := bg
			if row&(0x80>>uint((bx/chunkPx)%8)) != 0 {
				c = fg
			}
			cx0, cy0, cx1, cy1 := bx, by, bx+chunkPx, by+chunkPx
			if cx0 < x0 {
				cx0 = x0
			}
			if cy0 < y0 {
				cy0 = y0
			}
			if cx1 > x1 {
				cx1 = x1
			}
			if cy1 > y1 {
				cy1 = y1
			}
			b.fillPx(cx0, cy0, cx1, cy1, c)
		}
	}
}

func mod(a, m int) int {
	r := a % m
	if r < 0 {
		r += m
	}
	return r
}

// DrawCaret implements core.CaretDrawer: a one-unit-wide vertical bar
// at the left edge of the glyph box - where the next character would
// start. Drawn in the color a block cursor of style s would show
// (its background), so trinkets pass their block-cursor style
// unchanged.
func (b *Backend) DrawCaret(x, y, height core.Unit, s style.CellStyle) {
	_, bar := b.styleColors(s)
	left := b.pxX(x)
	b.fillPx(left, b.pxY(y), left+b.scale, b.pxY(y+height), bar)
}

// FillRectPx implements core.PixelRectFiller: a device-pixel rectangle
// (already in framebuffer coordinates) filled with the style background.
func (b *Backend) FillRectPx(xPx, yPx, wPx, hPx int, s style.CellStyle) {
	_, bg := b.styleColors(s)
	b.fillPx(xPx, yPx, xPx+wPx, yPx+hPx, bg)
}

// FillRectPxAlpha blends a device-pixel rectangle with the given RGB at
// alpha (0..1) over existing pixels, respecting the clip. Implements
// core.TranslucentPixelFiller.
func (b *Backend) FillRectPxAlpha(xPx, yPx, wPx, hPx int, r, g, bl uint8, alpha float64) {
	b.blendRectPx(xPx, yPx, xPx+wPx, yPx+hPx, color.RGBA{R: r, G: g, B: bl, A: 255}, alpha)
}

func (b *Backend) PollEvent() core.Event                  { return nil }
func (b *Backend) WaitEvent() core.Event                  { return nil }
func (b *Backend) SetCursorVisible(bool)                  {}
func (b *Backend) SetCursorPosition(core.Unit, core.Unit) {}
func (b *Backend) SupportsColor() bool                    { return true }
func (b *Backend) SupportsMouse() bool                    { return true }
func (b *Backend) SupportsUnicode() bool                  { return true }
func (b *Backend) ColorDepth() int                        { return 1 << 24 }
func (b *Backend) GetClipboard() string {
	if b.sysClipGet != nil {
		return b.sysClipGet()
	}
	return b.clipboard
}
func (b *Backend) SetClipboard(s string) {
	b.clipboard = s // keep a local copy as a fallback / for headless
	if b.sysClipSet != nil {
		b.sysClipSet(s)
	}
}
func (b *Backend) Beep() {}
