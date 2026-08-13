package trinkets

import (
	"math"
	"time"

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
// ResizeHitGrip with a border of zero (the desktop paints no frame of its
// own; the OS chrome is outside the client area), the hover affordance is
// the same translucent band at ResizeOverlayGrip width, and the corners
// reach as far as the affordance.
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

	// outsideTimer polls the global pointer while it sits in the OS's own
	// resize strip just OUTSIDE the client area, where no surface events
	// arrive, so the affordance stays lit across the whole combined edge
	// instead of going dark at the client boundary.
	outsideTimer *DesktopTimer
}

// osResizeMarginPx is how far outside the client area still reads as the
// OS's own resize strip. The OS's true grab width is not queryable from
// here and varies by platform (Windows ~8 device px, GNOME ~10); this is a
// display hint for the affordance, not a hit zone — the OS answers the
// actual press out there — so approximately right is right.
const osResizeMarginPx = 10

// hostResizeParts returns what the desktop-edge gesture needs, or ok=false
// where the feature cannot run at all.
func (d *Desktop) hostResizeParts() (platform.NativeSurface, platform.GlobalPointerPlatform, bool) {
	d.mu.RLock()
	graphical := d.graphicalFrames
	surf := d.surface
	plat := d.platform
	d.mu.RUnlock()
	if !graphical || surf == nil {
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

// hostEdgeAt is the desktop's own resize-edge answer for a surface-local
// point: the same geometry a child window's edges use, with a border of
// zero, so the zone is a bare quarter column (or 3 device pixels) and the
// corners reach as far as the affordance bands.
func (d *Desktop) hostEdgeAt(x, y core.Unit) int {
	if _, _, ok := d.hostResizeParts(); !ok {
		return 0
	}
	b := d.Bounds()
	metrics := d.EffectiveCellMetrics()
	grip := window.ResizeHitGrip(true, metrics, d.pxPerUnit(), 0)
	corner := window.ResizeOverlayGrip(true, metrics, 0)
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
	metrics := d.EffectiveCellMetrics()
	ppu := d.pxPerUnit()
	if ppu <= 0 {
		ppu = 1
	}
	// Same minimum a torn window enforces: 12 columns by 4 rows.
	minW := int(math.Round(float64(metrics.CellWidth*12) * ppu))
	minH := int(math.Round(float64(metrics.CellHeight*4) * ppu))
	x, y, w, h := applyHostResize(st.edges, st.startX, st.startY, st.startW, st.startH,
		gx-st.startGX, gy-st.startGY, minW, minH)
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
	// A surface event means the pointer is back inside: the outside poll's
	// job is over.
	d.hostOutsideStop()
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

// hostPointerLeft handles the pointer leaving the surface: if it stepped
// off across an edge into the OS's own resize strip, keep that edge's band
// lit and start the outside poll; anywhere else, the affordance clears as
// it always did. Detecting the strip at all takes the global pointer —
// events stop at the client boundary, which is exactly the problem.
func (d *Desktop) hostPointerLeft() {
	edges := 0
	if _, _, ok := d.hostResizeParts(); ok {
		edges = d.hostOutsideEdges()
	}
	if edges == 0 {
		d.hostHoverClear()
		return
	}
	d.mu.Lock()
	changed := d.hostEdge.hover != edges
	d.hostEdge.hover = edges
	timer := d.hostEdge.outsideTimer
	d.mu.Unlock()
	if changed {
		d.RequestUpdate()
	}
	if timer == nil {
		t := d.StartRepeatingTimer(50*time.Millisecond, d.hostOutsidePoll)
		d.mu.Lock()
		d.hostEdge.outsideTimer = t
		d.mu.Unlock()
	}
}

// hostOutsidePoll re-derives the outside-strip hover between surface events.
// It retires itself the moment the pointer is back inside (a move event owns
// hover from there) or has wandered past the strip.
func (d *Desktop) hostOutsidePoll() {
	edges := 0
	if _, _, ok := d.hostResizeParts(); ok {
		edges = d.hostOutsideEdges()
	}
	d.mu.RLock()
	prev := d.hostEdge.hover
	d.mu.RUnlock()
	if edges != prev {
		d.mu.Lock()
		d.hostEdge.hover = edges
		d.mu.Unlock()
		d.RequestUpdate()
	}
	if edges == 0 {
		d.hostOutsideStop()
	}
}

// hostOutsideStop retires the outside poll.
func (d *Desktop) hostOutsideStop() {
	d.mu.Lock()
	t := d.hostEdge.outsideTimer
	d.hostEdge.outsideTimer = nil
	d.mu.Unlock()
	if t != nil {
		d.StopTimer(t)
	}
}

// hostOutsideEdges reads the global pointer against the OS window's screen
// rectangle: edge bits when it sits in the resize strip just outside the
// client area, zero when it is inside (surface events own that) or beyond
// the strip.
//
// The TOP strip is deliberately absent: on a decorated window the area just
// above the client is the TITLE BAR — a move, not a resize — and the
// borderless case (solo) never reaches this path at all. The sides run the
// window's full height, and the bottom corners reach as far as the
// affordance, matching the inside rule.
func (d *Desktop) hostOutsideEdges() int {
	native, gp, ok := d.hostResizeParts()
	if !ok {
		return 0
	}
	gx, gy := gp.GlobalPointerPx()
	x, y := native.ScreenPositionPx()
	w, h := native.ScreenSizePx()
	if w <= 0 || h <= 0 {
		return 0
	}
	ppu := d.pxPerUnit()
	if ppu <= 0 {
		ppu = 1
	}
	cornerPx := int(math.Round(float64(window.ResizeOverlayGrip(true, d.EffectiveCellMetrics(), 0)) * ppu))

	const m = osResizeMarginPx
	if gx < x-m || gx >= x+w+m || gy < y || gy >= y+h+m {
		return 0 // beyond the strip (or above the client: the title bar's)
	}
	if gx >= x && gx < x+w && gy >= y && gy < y+h {
		return 0 // back inside: surface events own hover here
	}

	edges := 0
	if gx < x {
		edges |= window.ResizeEdgeLeft
	} else if gx >= x+w {
		edges |= window.ResizeEdgeRight
	}
	if gy >= y+h {
		edges |= window.ResizeEdgeBottom
	}
	// Corner reach, matching the inside rule: a side near the bottom takes
	// the bottom too, and the bottom strip near a side takes that side.
	if edges&(window.ResizeEdgeLeft|window.ResizeEdgeRight) != 0 &&
		edges&window.ResizeEdgeBottom == 0 && gy >= y+h-cornerPx {
		edges |= window.ResizeEdgeBottom
	}
	if edges == window.ResizeEdgeBottom {
		if gx < x+cornerPx {
			edges |= window.ResizeEdgeLeft
		} else if gx >= x+w-cornerPx {
			edges |= window.ResizeEdgeRight
		}
	}
	return edges
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
	band := window.ResizeOverlayGrip(true, d.EffectiveCellMetrics(), 0)
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
