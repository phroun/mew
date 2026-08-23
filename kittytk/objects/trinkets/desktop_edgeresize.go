package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/platform"
)

// The desktop's OWN edges resize the desktop's OS window, under the same
// rules its child windows follow.
//
// Until now the host window offered only the OS's native resize border,
// which is thinner than the grab zone every window INSIDE the desktop gets —
// so the easiest window to miss was the outermost one. The outer sliver of
// the desktop surface now behaves like any window edge: the grab rule is
// ResizeHitGrip with the frame border the surface actually carries — the
// themed frame's reserved border, or zero where the OS chrome sits outside
// the client area — the hover affordance is the same translucent band at
// ResizeOverlayGrip width, and the corners reach as far as the affordance.
//
// The press is applied the way TearOffHost applies one — global pointer
// deltas onto the OS window's pixel geometry through platform.NativeSurface
// — because the desktop IS an OS window here, and the size change reports
// back through Resized like any other host resize.
//
// The zones engage only where they can act: a graphical surface that is a
// NativeSurface, on a platform with a global pointer, and not while the OS
// window is maximized or fullscreen (the OS holds its geometry there). In
// solo mode the primary surface is driven by a TearOffHost handler, whose
// edges already work, so this path never sees those events.

// hostEdgeState is the desktop-edge resize gesture and its hover affordance.
// Guarded by Desktop.mu: events arrive on the platform loop but the bands
// paint from wherever the renderer runs.
type hostEdgeState struct {
	hover  int // edge bits under the pointer (bands + cursor)
	active bool
	edges  int

	startGX, startGY int // global pointer at press, device px
	startX, startY   int // OS window origin at press, device px
	startW, startH   int // OS window size at press, device px
}

// hostResizeParts returns what the desktop-edge gesture needs, or ok=false
// where the feature cannot run at all.
func (d *Desktop) hostResizeParts() (platform.NativeSurface, platform.GlobalPointerPlatform, bool) {
	d.mu.RLock()
	graphical := d.graphicalFrames
	frame := d.desktopFrameLocked()
	surf := d.surface
	plat := d.platform
	d.mu.RUnlock()
	// desktop_frame=native: the OS chrome is the whole resize story, and the
	// desktop's own zones stand down entirely.
	if !graphical || surf == nil || frame == DesktopFrameNative {
		return nil, nil, false
	}
	native, ok := surf.(platform.NativeSurface)
	if !ok {
		return nil, nil, false
	}
	gp, ok := plat.(platform.GlobalPointerPlatform)
	if !ok {
		return nil, nil, false
	}
	if z, ok := surf.(platform.NativeZoomReporter); ok && z.NativeZoomed() {
		return nil, nil, false
	}
	return native, gp, true
}

// applyHostMinimumSize tells the OS the smallest this window may become —
// the same floor our own gestures clamp to (window.MinHostCols/Rows), so
// a resize we do not drive (a native title bar's edges in the
// native/native_titlebar frame modes, the window manager's own keyboard
// resize or tiling) cannot pull the desktop — or, in solo mode, the app
// filling this same surface — down to nothing.
//
// Called when the surface is created and again whenever the cell metrics
// or zoom change, since the floor is measured in cells.
func (d *Desktop) applyHostMinimumSize() {
	d.mu.RLock()
	surf := d.surface
	d.mu.RUnlock()
	ms, ok := surf.(platform.NativeMinimumSizer)
	if !ok {
		return
	}
	w, h := window.MinHostSizePx(d.EffectiveCellMetrics(), d.pxPerUnit())
	ms.SetMinimumSizePx(w, h)
}

// paintableSurfacePx rounds a requested surface size DOWN to a size the
// surface can actually paint: the largest pixel extent that is exactly
// where some whole unit count lands on the snapped grid.
//
// A drag hands us an arbitrary pixel count, and the reported extent floors
// (it must never point past the true edge), so an odd size leaves a last
// column or row outside every unit — nothing paints it, the frame's
// outermost stroke is clipped against it, and that edge reads thinner
// than the other three. Asking for the paintable size below instead costs
// at most a pixel of window and keeps the border one thickness all the
// way round. Only whole-window SIZES go through this.
func (d *Desktop) paintableSurfacePx(px int, vertical bool) int {
	toUnit, toPx := d.HardPxToUnitX, d.HardUnitToPxX
	if vertical {
		toUnit, toPx = d.HardPxToUnitY, d.HardUnitToPxY
	}
	u := toUnit(px)
	// The unmapper rounds to nearest, so step down until the unit extent
	// fits, then take the pixels that extent actually paints.
	for u > 0 && toPx(u) > px {
		u--
	}
	if u <= 0 {
		return px
	}
	if fit := toPx(u); fit > 0 {
		return fit
	}
	return px
}

// hostEdgeAt is the desktop's own resize-edge answer for a surface-local
// point: the same geometry a child window's edges use, with the frame
// border the surface actually carries — the reserved themed border, or
// zero under an OS title bar — so the grab zone is the border plus a
// quarter column (floored at 3 device pixels) and the corners reach as
// far as the affordance bands, exactly the window rule.
func (d *Desktop) hostEdgeAt(x, y core.Unit) int {
	if _, _, ok := d.hostResizeParts(); !ok {
		return 0
	}
	b := d.Bounds()
	border := d.hostFrameInset()
	metrics := d.EffectiveCellMetrics()
	grip := window.ResizeHitGrip(true, metrics, d.pxPerUnit(), border)
	corner := window.ResizeOverlayGrip(true, metrics, border)
	return window.ResizeEdgeAt(core.UnitRect{Width: b.Width, Height: b.Height},
		x, y, metrics, grip, corner)
}

// hostResizeBegin arms a desktop-edge resize when the press lands in the
// desktop's own grab zone. The zone is the outermost thing on the surface —
// like the OS resize border it extends — so it is consulted before the
// window manager, and wins over anything corralled against the edge.
func (d *Desktop) hostResizeBegin(e core.MousePressEvent) bool {
	if e.Button != core.LeftButton {
		return false
	}
	edges := d.hostEdgeAt(e.X, e.Y)
	if edges == 0 {
		return false
	}
	native, gp, ok := d.hostResizeParts()
	if !ok {
		return false
	}
	d.mu.Lock()
	st := &d.hostEdge
	st.active, st.edges, st.hover = true, edges, edges
	st.startGX, st.startGY = gp.GlobalPointerPx()
	st.startX, st.startY = native.ScreenPositionPx()
	st.startW, st.startH = native.ScreenSizePx()
	d.mu.Unlock()
	d.applyHostCursor(window.ResizeCursorForEdge(edges))
	d.RequestUpdate()
	return true
}

// hostResizeMove applies the pointer delta to the armed edges, moving and
// resizing the OS window; the size change reports back through Resized.
// Reports false when no gesture is in progress.
func (d *Desktop) hostResizeMove(e core.MouseMoveEvent) bool {
	d.mu.RLock()
	active := d.hostEdge.active
	st := d.hostEdge
	d.mu.RUnlock()
	if !active {
		return false
	}
	if e.Buttons&core.LeftButton == 0 {
		// The release was missed (left the surface mid-gesture); end here.
		d.hostResizeFinish(e.X, e.Y)
		return true
	}
	native, gp, ok := d.hostResizeParts()
	if !ok {
		d.hostResizeFinish(e.X, e.Y)
		return true
	}
	gx, gy := gp.GlobalPointerPx()
	if gx != st.startGX || gy != st.startGY {
		// ACTUALLY resizing (not just a press in the zone): a hand-resized
		// window is an ordinary floating window again, not the zoom
		// rectangle, so the frame returns and the next Zoom starts fresh.
		d.hostZoomForget()
	}
	metrics := d.EffectiveCellMetrics()
	ppu := d.pxPerUnit()
	if ppu <= 0 {
		ppu = 1
	}
	// The same minimum every host surface enforces (window.MinHostCols/Rows).
	minW, minH := window.MinHostSizePx(metrics, ppu)
	x, y, w, h := applyHostResize(st.edges, st.startX, st.startY, st.startW, st.startH,
		gx-st.startGX, gy-st.startGY, minW, minH)
	// Round the dragged size DOWN to what the surface can paint, so no
	// half-addressable pixel is left to clip the frame's outer stroke.
	w = d.paintableSurfacePx(w, false)
	h = d.paintableSurfacePx(h, true)
	if st.edges&(window.ResizeEdgeLeft|window.ResizeEdgeTop) != 0 {
		native.SetScreenPositionPx(x, y)
	}
	native.SetScreenSizePx(w, h)
	d.applyHostCursor(window.ResizeCursorForEdge(st.edges))
	return true
}

// hostResizeEnd completes the gesture on release. Reports false when none
// was in progress.
func (d *Desktop) hostResizeEnd(e core.MouseReleaseEvent) bool {
	d.mu.RLock()
	active := d.hostEdge.active
	d.mu.RUnlock()
	if !active {
		return false
	}
	d.hostResizeFinish(e.X, e.Y)
	return true
}

// hostResizeFinish disarms the gesture and re-derives the hover state from
// wherever the pointer ended up.
func (d *Desktop) hostResizeFinish(x, y core.Unit) {
	d.mu.Lock()
	d.hostEdge.active = false
	d.hostEdge.edges = 0
	d.mu.Unlock()
	d.hostHoverUpdate(x, y)
}

// applyHostResize is the drag arithmetic alone: the armed edges, the press
// anchors, the pointer delta, and the minimum size, to the OS window's new
// pixel rectangle. Left/top resizes move the origin so the opposite edge
// stays pinned, and the minimum is absorbed by the moving edge.
func applyHostResize(edges, startX, startY, startW, startH, dx, dy, minW, minH int) (x, y, w, h int) {
	x, y, w, h = startX, startY, startW, startH
	if edges&window.ResizeEdgeLeft != 0 {
		w -= dx
		if w < minW {
			dx -= minW - w
			w = minW
		}
		x += dx
	}
	if edges&window.ResizeEdgeRight != 0 {
		w += dx
		if w < minW {
			w = minW
		}
	}
	if edges&window.ResizeEdgeTop != 0 {
		h -= dy
		if h < minH {
			dy -= minH - h
			h = minH
		}
		y += dy
	}
	if edges&window.ResizeEdgeBottom != 0 {
		h += dy
		if h < minH {
			h = minH
		}
	}
	return x, y, w, h
}

// hostHoverUpdate re-derives the hovered desktop edge for a plain pointer
// move (no gesture in progress), lighting or clearing the affordance bands.
func (d *Desktop) hostHoverUpdate(x, y core.Unit) {
	d.mu.RLock()
	active := d.hostEdge.active
	prev := d.hostEdge.hover
	d.mu.RUnlock()
	if active {
		return
	}
	edges := d.hostEdgeAt(x, y)
	if edges == prev {
		return
	}
	d.mu.Lock()
	d.hostEdge.hover = edges
	d.mu.Unlock()
	d.RequestUpdate()
}

// hostHoverClear drops the affordance when the pointer leaves the surface.
func (d *Desktop) hostHoverClear() {
	d.mu.Lock()
	changed := d.hostEdge.hover != 0
	d.hostEdge.hover = 0
	d.mu.Unlock()
	if changed {
		d.RequestUpdate()
	}
}

// hostHoverEdges is what the cursor and the bands show right now.
func (d *Desktop) hostHoverEdges() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.hostEdge.hover
}

// applyHostCursor sets the system cursor for the desktop-edge gesture.
func (d *Desktop) applyHostCursor(shape core.CursorShape) {
	d.mu.RLock()
	plat := d.platform
	d.mu.RUnlock()
	if cc, ok := plat.(platform.CursorController); ok {
		cc.SetCursor(shape)
	}
}

// The affordance ends exactly where our authority does: at the client-area
// boundary. The OS's own resize strip lies beyond it, its true width is not
// queryable, and the pointer out there may in fact be over ANOTHER window —
// so a band lit past the edge would be a promise this program cannot keep,
// and a click on it could go somewhere else entirely. Better a dark strip
// that resizes than a lit one that lies.

// paintHostEdgeHover draws the desktop's own resize affordance: the same
// translucent bands a window edge shows, along the hovered edges of the
// surface itself. Painted last in the desktop's own pass, so the bands lie
// over the chrome; in compositor mode child windows still composite above
// the base layer, so a window corralled hard against the edge can cover
// part of a band — the grab beneath it still works.
func (d *Desktop) paintHostEdgeHover(p *core.Painter, bounds core.UnitRect) {
	edges := d.hostHoverEdges()
	if edges == 0 {
		return
	}
	band := window.ResizeOverlayGrip(true, d.EffectiveCellMetrics(), d.hostFrameInset())
	var rects []core.UnitRect
	if edges&window.ResizeEdgeLeft != 0 {
		rects = append(rects, core.UnitRect{Width: band, Height: bounds.Height})
	}
	if edges&window.ResizeEdgeRight != 0 {
		rects = append(rects, core.UnitRect{X: bounds.Width - band, Width: band, Height: bounds.Height})
	}
	if edges&window.ResizeEdgeTop != 0 {
		rects = append(rects, core.UnitRect{Width: bounds.Width, Height: band})
	}
	if edges&window.ResizeEdgeBottom != 0 {
		rects = append(rects, core.UnitRect{Y: bounds.Height - band, Width: bounds.Width, Height: band})
	}
	for _, r := range rects {
		p.FillRectPixelsAlpha(r.X, r.Y, 0, 0,
			p.UnitSpanPxX(r.X, r.X+r.Width), p.UnitSpanPxY(r.Y, r.Y+r.Height),
			255, 255, 255, window.ResizeHoverAlpha)
	}
}
