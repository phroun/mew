package sdl

import (
	"image"
	"math"
	"os"
	"time"

	"github.com/phroun/kittytk/core"
)

// This file is intentionally free of build tags: it holds the pure math
// the GPU compositor rests on, so tests exercise it on any platform
// without SDL or a GPU.

// windowNDC maps a child window's unit bounds within a surface of the
// given unit size to the normalized-device-coordinate quad the blit
// shader draws: x/y is the quad's bottom-left corner, w/h its extent.
// NDC spans [-1,1] on both axes with +y up, while unit bounds have +y
// down from the top-left, hence the flip.
func windowNDC(bounds core.UnitRect, surfaceSize core.UnitSize) (x, y, w, h float32) {
	if surfaceSize.Width <= 0 || surfaceSize.Height <= 0 {
		return 0, 0, 0, 0
	}
	x = (float32(bounds.X)/float32(surfaceSize.Width))*2.0 - 1.0
	top := 1.0 - (float32(bounds.Y)/float32(surfaceSize.Height))*2.0
	w = (float32(bounds.Width) / float32(surfaceSize.Width)) * 2.0
	h = (float32(bounds.Height) / float32(surfaceSize.Height)) * 2.0
	y = top - h
	return x, y, w, h
}

// outsetBounds grows a rect by the stroke offset on every side — the
// overlay texture is padded so outer strokes drawn just outside the
// nominal bounds still land on it.
func outsetBounds(bounds core.UnitRect, offset core.Unit) core.UnitRect {
	return core.UnitRect{
		X:      bounds.X - offset,
		Y:      bounds.Y - offset,
		Width:  bounds.Width + offset*2,
		Height: bounds.Height + offset*2,
	}
}

// overlayTexturePx sizes an overlay layer's texture: the bounds mapped
// at the surface's pixels-per-unit, plus the stroke padding converted at
// the SAME density on every side. Texture pixels and the outset
// on-screen quad must describe the same physical size — padding the
// texture by raw pixels while outsetting the quad by units stretched
// the texture (distorted glyphs) and pushed the painted stroke off the
// texture's right/bottom edge at any scale above 1.
func overlayTexturePx(bounds core.UnitRect, ppuW, ppuH float64, pad core.Unit) (w, h, padPxW, padPxH int) {
	padPxW = int(math.Round(float64(pad) * ppuW))
	padPxH = int(math.Round(float64(pad) * ppuH))
	w = int(math.Round(float64(bounds.Width)*ppuW)) + padPxW*2
	h = int(math.Round(float64(bounds.Height)*ppuH)) + padPxH*2
	return w, h, padPxW, padPxH
}

// scissorPx maps a clip region in surface units to a framebuffer scissor
// rectangle in pixels, clamped to the surface. ok is false for an empty
// region (draw nothing) — callers treat a zero input rect as "no
// clipping" BEFORE calling this.
func scissorPx(area core.UnitRect, ppuW, ppuH float64, maxW, maxH int) (x, y, w, h int, ok bool) {
	x0 := int(math.Round(float64(area.X) * ppuW))
	y0 := int(math.Round(float64(area.Y) * ppuH))
	x1 := int(math.Round(float64(area.X+area.Width) * ppuW))
	y1 := int(math.Round(float64(area.Y+area.Height) * ppuH))
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > maxW {
		x1 = maxW
	}
	if y1 > maxH {
		y1 = maxH
	}
	if x1 <= x0 || y1 <= y0 {
		return 0, 0, 0, 0, false
	}
	return x0, y0, x1 - x0, y1 - y0, true
}

// shadowSpec is one drop-shadow style in the form the compositor works
// in: the same lengths as core.DropShadowStyle, with alpha narrowed to
// the float32 the shader's uniform block carries.
type shadowSpec struct {
	offsetX core.Unit // cast down-right
	offsetY core.Unit
	blur    core.Unit // falloff distance around the caster
	radius  core.Unit // caster corner rounding
	alpha   float32   // peak opacity
}

// The styles themselves live in core, because shadows reach the screen
// two ways: composited here for the layers the compositor owns, and
// painted into a surface (core.Painter.DropShadow) for anything nested
// inside one, such as an MDI child in its parent window's texture. One
// definition, so the two cannot drift apart in look.
func toShadowSpec(s core.DropShadowStyle) shadowSpec {
	return shadowSpec{
		offsetX: s.OffsetX,
		offsetY: s.OffsetY,
		blur:    s.Blur,
		radius:  s.Radius,
		alpha:   float32(s.Alpha),
	}
}

// windowShadowSpec is the soft, larger shadow under desktop windows;
// overlayShadowSpec the tighter one under menus, popups, and combo
// lists.
var (
	windowShadowSpec  = toShadowSpec(core.WindowDropShadow)
	overlayShadowSpec = toShadowSpec(core.OverlayDropShadow)
)

// unionRect returns the smallest rect containing both. An empty rect is
// the identity.
func unionRect(a, b core.UnitRect) core.UnitRect {
	if a.IsEmpty() {
		return b
	}
	if b.IsEmpty() {
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

// shadowQuadBounds returns the on-screen quad a shadow needs: the caster
// (unioned with its anchor when present) shifted by the spec's offset
// and outset by the blur so the falloff has room on every side.
func shadowQuadBounds(caster, anchor core.UnitRect, spec shadowSpec) core.UnitRect {
	shape := unionRect(caster, anchor)
	shape.X += spec.offsetX
	shape.Y += spec.offsetY
	return outsetBounds(shape, spec.blur)
}

// rectPxIn maps a unit rect to pixel min/max coordinates relative to a
// quad's origin — the space the shadow shader's distance field works in.
func rectPxIn(quad, r core.UnitRect, ppuW, ppuH float64) (minX, minY, maxX, maxY float32) {
	minX = float32(float64(r.X-quad.X) * ppuW)
	minY = float32(float64(r.Y-quad.Y) * ppuH)
	maxX = float32(float64(r.X+r.Width-quad.X) * ppuW)
	maxY = float32(float64(r.Y+r.Height-quad.Y) * ppuH)
	return minX, minY, maxX, maxY
}

// punchRoundedCorners clears the pixels outside a rounded rectangle
// covering the whole image, with antialiased coverage on the curve.
//
// This is how a rounded window actually gets its shape: the pixels we
// present carry it. CAMetalLayer ignores cornerRadius/masksToBounds for
// its drawable (the drawable goes to the window server rather than
// through the layer's mask), and SDL's shaped-window API is gone in
// SDL3 — but a transparent corner in the framebuffer works on every
// renderer and every platform.
//
// image.RGBA is alpha-premultiplied, so every channel scales by the
// coverage: a cleared pixel is (0,0,0,0), which composites as nothing
// rather than as an additive black fringe.
func punchRoundedCorners(img *image.RGBA, radiusPx int) {
	if img == nil || radiusPx <= 0 {
		return
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	r := radiusPx
	if max := w / 2; r > max {
		r = max
	}
	if max := h / 2; r > max {
		r = max
	}
	if r <= 0 {
		return
	}

	rf := float64(r)
	for j := 0; j < r; j++ {
		for i := 0; i < r; i++ {
			// Distance from the corner circle's center to this pixel's
			// center; coverage ramps across the last pixel of the edge.
			dx := rf - float64(i) - 0.5
			dy := rf - float64(j) - 0.5
			d := math.Sqrt(dx*dx + dy*dy)
			cov := rf - d + 0.5
			if cov >= 1 {
				continue // fully inside
			}
			if cov < 0 {
				cov = 0
			}
			scale := func(x, y int) {
				o := img.PixOffset(b.Min.X+x, b.Min.Y+y)
				img.Pix[o+0] = uint8(float64(img.Pix[o+0])*cov + 0.5)
				img.Pix[o+1] = uint8(float64(img.Pix[o+1])*cov + 0.5)
				img.Pix[o+2] = uint8(float64(img.Pix[o+2])*cov + 0.5)
				img.Pix[o+3] = uint8(float64(img.Pix[o+3])*cov + 0.5)
			}
			scale(i, j)
			scale(w-1-i, j)
			scale(i, h-1-j)
			scale(w-1-i, h-1-j)
		}
	}
}

// paintSignature is everything about a child window that changes the
// pixels the compositor has cached in its texture. Position is NOT in
// it: a window's placement lives in its uniform buffer, which is
// rewritten every frame, so dragging a window costs no repaint at all.
type paintSignature struct {
	// revision is the window's subtree repaint counter. hasRevision is
	// false for a window that does not report one — an unknown window
	// type, or one whose tree predates the tracker — and forces a
	// repaint every frame, which is the old behaviour.
	revision    uint64
	hasRevision bool

	fontSize int
	metrics  core.CellMetrics
	widthPx  int
	heightPx int
}

// compositorAlwaysRepaint restores the unconditional repaint under
// KITTYTK_COMPOSITOR_REPAINT=always. A surface showing stale content is
// the failure mode every cache here can have, and one run with this set
// says in a second whether a cache is the cause. It lives with the pure
// math because both present paths honour it — the compositor's layers
// and the plain single-surface present alike.
var compositorAlwaysRepaint = os.Getenv("KITTYTK_COMPOSITOR_REPAINT") == "always"

// compositorHeartbeat is how long a cached window texture may go without
// a repaint, and compositorHeartbeatSpread how far past that a given
// window's turn may fall.
//
// The heartbeat exists because this cache changes a failure mode: before
// it, code that altered a window's look without signalling was masked by
// the desktop's own ~1s heartbeat repainting everything anyway, so the
// bug showed as at most a moment's lag. Cached, the same miss would
// freeze that window's pixels for good. Repainting on this interval puts
// the old bound back, at one repaint per window per second instead of
// twenty.
const (
	compositorHeartbeat       = time.Second
	compositorHeartbeatSpread = 500 * time.Millisecond
)

// heartbeatInterval staggers the forced repaints. Every window is first
// painted in the same frame, so a shared interval would keep the whole
// desk in lockstep: one ordinary frame in twenty doing a full repaint,
// conversion and upload for EVERY window at once — exactly the stutter
// the cache exists to remove. Spreading the deadline over an extra half
// second turns that spike into a trickle.
//
// The phase is derived from the window's id, which comes from a pointer,
// so its low bits are mostly alignment zeros — hence the mix before
// taking the high ones.
func heartbeatInterval(windowID uint32) time.Duration {
	const knuth = 2654435761
	phase := time.Duration((windowID * knuth) >> 24) // 0..255
	return compositorHeartbeat + compositorHeartbeatSpread*phase/256
}

// needsRepaint reports whether a cached window texture is stale. `last`
// is the signature at the last paint; `now` the current one. forceDirty
// covers what the signature cannot see — a freshly created or resized
// surface, whose texture holds nothing yet. sinceLastPaint and heartbeat
// drive the staggered forced repaint above.
//
// Anything unrecognized repaints: a false negative here shows stale
// pixels on screen, while a false positive only costs the work we used
// to do unconditionally.
func needsRepaint(last, now paintSignature, sinceLastPaint, heartbeat time.Duration, forceDirty, forceAlways bool) bool {
	if forceAlways || forceDirty {
		return true
	}
	if !now.hasRevision || !last.hasRevision {
		return true
	}
	if sinceLastPaint >= heartbeat {
		return true
	}
	return now != last
}

// caretInSurface shifts a caret request made inside a compositor layer
// into surface coordinates. A layer paints into its own texture at that
// texture's origin, so a trinket deep inside it asks for the caret in
// the layer's local space; the OS needs it relative to the window.
//
// A frame that asked for nothing keeps no position: a stale one would
// anchor an input method's candidate window at a caret that is not
// there.
func caretInSurface(caret core.TextCaret, layer core.UnitRect) core.TextCaret {
	if !caret.Requested() {
		return core.TextCaret{}
	}
	caret.X += layer.X
	caret.Y += layer.Y
	return caret
}

// gpuRowAlignment is WebGPU's required bytes-per-row alignment for
// texture uploads.
const gpuRowAlignment = 256

// bgraPixels converts an RGBA image to BGRA with rows padded to the GPU
// upload alignment, returning the pixel data and its bytes-per-row.
func bgraPixels(img *image.RGBA) (data []byte, bytesPerRow uint32) {
	bounds := img.Bounds()
	width := uint32(bounds.Dx())
	height := uint32(bounds.Dy())

	bytesPerRow = ((width*4 + gpuRowAlignment - 1) / gpuRowAlignment) * gpuRowAlignment
	data = make([]byte, bytesPerRow*height)

	for y := uint32(0); y < height; y++ {
		srcOffset := y * uint32(img.Stride)
		dstOffset := y * bytesPerRow
		for x := uint32(0); x < width; x++ {
			srcIdx := srcOffset + x*4
			dstIdx := dstOffset + x*4
			data[dstIdx+0] = img.Pix[srcIdx+2] // B
			data[dstIdx+1] = img.Pix[srcIdx+1] // G
			data[dstIdx+2] = img.Pix[srcIdx+0] // R
			data[dstIdx+3] = img.Pix[srcIdx+3] // A
		}
	}
	return data, bytesPerRow
}
