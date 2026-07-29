//go:build mew

package trinkets

import (
	"fmt"
	"math"
	"sync"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
	mew "github.com/phroun/mew"
)

// Graphical editor scrollbars.
//
// mew draws its own bar as cell glyphs ('░' track, '█' thumb) on a text
// terminal, which is right there and wrong here: on a pixel surface the thumb
// would be stuck to whole rows, jumping a full line at a time under a pointer
// that moves by pixels. So on a graphical target the host takes over. mew
// still RESERVES the column — the layout is identical either way — but paints
// nothing into it and stops hit-testing it (WithScrollbarRegions), publishing
// the geometry and scroll state instead; everything below draws and drives the
// bar in the same pixel space, and with the same visual language, as the
// hosted terminals' bars.
//
// The thumb is continuous: its length is a real fraction of the track, and
// during a drag it follows the pointer pixel for pixel. Only the RESULT
// quantizes — the top line mew is asked to park at is always a whole number,
// because a text editor has no meaning for two-thirds of a line.

// gfxEditorLane is the bar's thickness in units, matching the hosted
// terminals' lane so every bar on screen is the same width.
const gfxEditorLane = gfxScrollbarLane

// editorBar is one published mew scrollbar plus the host-side drag state that
// belongs to it.
type editorBar struct {
	region mew.ScrollbarRegion
	// thumbPx is the thumb's live top edge in render pixels while a drag is in
	// flight. Between drags the thumb is derived from the region's scroll
	// state; during one it follows the pointer, so it moves smoothly even
	// though the line it settles on is whole.
	thumbPx  float64
	dragging bool
	grabPx   float64 // where in the thumb the pointer grabbed it
	hover    bool
}

// editorBars holds the published bars, keyed by viewport ID.
type editorBars struct {
	mu   sync.Mutex
	bars map[string]*editorBar
	// order preserves mew's publication order so painting is deterministic.
	order []string
}

func (b *editorBars) set(regions []mew.ScrollbarRegion) (changed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bars == nil {
		b.bars = map[string]*editorBar{}
	}
	seen := make(map[string]bool, len(regions))
	b.order = b.order[:0]
	for _, r := range regions {
		seen[r.ViewportID] = true
		b.order = append(b.order, r.ViewportID)
		if cur, ok := b.bars[r.ViewportID]; ok {
			if cur.region != r {
				cur.region = r
				changed = true
			}
			continue
		}
		b.bars[r.ViewportID] = &editorBar{region: r}
		changed = true
	}
	for id := range b.bars {
		if !seen[id] {
			delete(b.bars, id)
			changed = true
		}
	}
	return changed
}

// each calls fn for every bar in publication order, under the lock.
func (b *editorBars) each(fn func(*editorBar)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, id := range b.order {
		if bar := b.bars[id]; bar != nil {
			fn(bar)
		}
	}
}

// barRect is a bar's track in render pixels, and the pixels-per-unit it was
// computed at.
type barRect struct {
	x, y, w, h float64
}

func (r barRect) contains(px, py float64) bool {
	return px >= r.x && px < r.x+r.w && py >= r.y && py < r.y+r.h
}

// trackPx converts a bar's published cell geometry into render pixels. The
// lane is the same thickness as a hosted terminal's, anchored on the cell
// column mew reserved — so it lands exactly where the reserved column is,
// whichever side of the window it sits on.
func (e *Editor) trackPx(r mew.ScrollbarRegion) (barRect, float64, bool) {
	cw, chh := e.cellDims()
	if cw <= 0 || chh <= 0 || r.TrackH <= 0 {
		return barRect{}, 0, false
	}
	ppu := e.gfx.ppu
	if ppu <= 0 {
		ppu = 1
	}
	lane := float64(gfxEditorLane) * ppu
	// The reserved column in pixels, then the lane centred in it: a cell is
	// usually wider than the lane, and the bar should sit against the window
	// edge the column is on rather than float mid-cell.
	colPx := float64(r.Col-1) * float64(cw) * ppu
	cellPx := float64(cw) * ppu
	x := colPx + cellPx - lane // flush to the column's outer edge
	if x < colPx {
		x = colPx
	}
	return barRect{
		x: x,
		y: float64(r.Row-1) * float64(chh) * ppu,
		w: lane,
		h: float64(r.TrackH) * float64(chh) * ppu,
	}, ppu, true
}

// thumbSpan is the thumb's length and top edge within the track, in pixels.
// Both are continuous: the length is the visible fraction of the document and
// the position its scroll fraction, with only a minimum length imposed so the
// thumb stays grabbable in a very long file.
func thumbSpan(track barRect, r mew.ScrollbarRegion, ppu float64) (pos, length float64) {
	if r.LineCount <= 0 || r.Page <= 0 {
		return 0, track.h
	}
	if r.LineCount <= r.Page {
		return 0, track.h // the whole document fits
	}
	length = track.h * float64(r.Page) / float64(r.LineCount)
	if min := 8 * ppu; length < min {
		length = min
	}
	if length > track.h {
		length = track.h
	}
	span := track.h - length
	maxTop := float64(r.LineCount - r.Page)
	if maxTop > 0 && span > 0 {
		f := float64(r.Top) / maxTop
		if f < 0 {
			f = 0
		}
		if f > 1 {
			f = 1
		}
		pos = span * f
	}
	return pos, length
}

// topLineForThumb inverts thumbSpan: the whole line the document should sit at
// for a thumb whose top edge is at pos. This is where the smooth gesture meets
// the discrete document — the pointer moved by pixels, the view moves by
// lines, and the rounding happens exactly once, here.
func topLineForThumb(pos float64, track barRect, r mew.ScrollbarRegion, ppu float64) int {
	if r.LineCount <= r.Page || r.Page <= 0 {
		return 0
	}
	_, length := thumbSpan(track, r, ppu)
	span := track.h - length
	if span <= 0 {
		return 0
	}
	if pos < 0 {
		pos = 0
	}
	if pos > span {
		pos = span
	}
	maxTop := r.LineCount - r.Page
	return int(math.Round(pos / span * float64(maxTop)))
}

// paintEditorScrollbars draws every published bar, in the hosted terminals'
// visual language: a translucent wash for the track and a solid thumb that
// lights up on hover.
func (e *Editor) paintEditorScrollbars(p *core.Painter) {
	if !core.FindGraphicalFrames(e) {
		return // a cell surface: mew paints its own bar there
	}
	thumbStyle := style.DefaultStyle().WithBg(style.RGB(168, 168, 168))
	hs := e.GetScheme().GetHoveredScrollbarThumb()
	hoverThumb := hs.WithBg(hs.Fg)

	e.bars.each(func(bar *editorBar) {
		track, ppu, ok := e.trackPx(bar.region)
		if !ok {
			return
		}
		pos, length := thumbSpan(track, bar.region, ppu)
		if bar.dragging {
			pos = bar.thumbPx // mid-drag the thumb leads, the view follows
			if limit := track.h - length; pos > limit {
				pos = limit
			}
			if pos < 0 {
				pos = 0
			}
		}
		fill := func(x, y, w, h float64, s style.CellStyle) {
			x0, y0 := int(math.Round(x)), int(math.Round(y))
			x1, y1 := int(math.Round(x+w)), int(math.Round(y+h))
			if x1 > x0 && y1 > y0 {
				p.FillRectPixels(0, 0, x0, y0, x1-x0, y1-y0, s)
			}
		}
		// Track: a translucent grey wash, as the hosted terminals use, so the
		// text behind it stays legible.
		x0, y0 := int(math.Round(track.x)), int(math.Round(track.y))
		x1, y1 := int(math.Round(track.x+track.w)), int(math.Round(track.y+track.h))
		if x1 > x0 && y1 > y0 {
			if !p.FillRectPixelsAlpha(0, 0, x0, y0, x1-x0, y1-y0, 128, 128, 128, 0.30) {
				fill(track.x, track.y, track.w, track.h,
					style.DefaultStyle().WithBg(style.RGB(128, 128, 128)))
			}
		}
		ts := thumbStyle
		if bar.hover {
			ts = hoverThumb
		}
		fill(track.x, track.y+pos, track.w, length, ts)
	})
}

// editorBarAt finds the bar whose track contains a local point, with the point
// converted to render pixels.
func (e *Editor) editorBarAt(x, y core.Unit) (*editorBar, barRect, float64, float64, float64, bool) {
	px, py := e.gfxPointerPx(x, y)
	var (
		found *editorBar
		rect  barRect
		ppu   float64
	)
	e.bars.each(func(bar *editorBar) {
		if found != nil {
			return
		}
		track, k, ok := e.trackPx(bar.region)
		if ok && track.contains(px, py) {
			found, rect, ppu = bar, track, k
		}
	})
	if found == nil {
		return nil, barRect{}, 0, 0, 0, false
	}
	return found, rect, ppu, px, py, true
}

// scrollbarPressAt starts a bar drag when the press lands on a track. A press
// ON the thumb anchors the grab where it was taken; a press on the track jumps
// the thumb's centre to the pointer and drags on from there — the same gesture
// the hosted terminals implement.
func (e *Editor) scrollbarPressAt(x, y core.Unit) bool {
	if !core.FindGraphicalFrames(e) {
		return false
	}
	bar, track, ppu, _, py, ok := e.editorBarAt(x, y)
	if !ok {
		return false
	}
	pos, length := thumbSpan(track, bar.region, ppu)
	local := py - track.y
	e.bars.mu.Lock()
	if local >= pos && local < pos+length {
		bar.grabPx = local - pos
	} else {
		bar.grabPx = length / 2
	}
	bar.dragging = true
	bar.thumbPx = local - bar.grabPx
	e.bars.mu.Unlock()
	e.scrollbarDragTo(bar, track, ppu, py)
	return true
}

// scrollbarDragMove continues a captured drag. Returns false when none is.
func (e *Editor) scrollbarDragMove(x, y core.Unit) bool {
	var (
		active *editorBar
		track  barRect
		ppu    float64
	)
	e.bars.each(func(bar *editorBar) {
		if bar.dragging && active == nil {
			if t, k, ok := e.trackPx(bar.region); ok {
				active, track, ppu = bar, t, k
			}
		}
	})
	if active == nil {
		return false
	}
	_, py := e.gfxPointerPx(x, y)
	e.scrollbarDragTo(active, track, ppu, py)
	return true
}

// scrollbarDragRelease ends a captured drag; false when none is.
func (e *Editor) scrollbarDragRelease() bool {
	released := false
	e.bars.mu.Lock()
	for _, bar := range e.bars.bars {
		if bar.dragging {
			bar.dragging = false
			released = true
		}
	}
	e.bars.mu.Unlock()
	return released
}

// scrollbarDragTo moves the thumb to the pointer and asks mew for the line
// that lands under it. The thumb is placed in pixels; the line is whole.
func (e *Editor) scrollbarDragTo(bar *editorBar, track barRect, ppu, py float64) {
	e.bars.mu.Lock()
	pos := py - track.y - bar.grabPx
	_, length := thumbSpan(track, bar.region, ppu)
	if limit := track.h - length; pos > limit {
		pos = limit
	}
	if pos < 0 {
		pos = 0
	}
	bar.thumbPx = pos
	region := bar.region
	e.bars.mu.Unlock()

	top := topLineForThumb(pos, track, region, ppu)
	if top != region.Top && e.port != nil {
		e.port.Execute(fmt.Sprintf("scroll_viewport %q, %q", region.ViewportID, fmt.Sprint(top)))
	}
	e.Update()
}

// updateScrollbarHover lights the thumb under the pointer, repainting only on
// change. A drag in flight keeps its own bar lit wherever the pointer goes.
func (e *Editor) updateScrollbarHover(x, y core.Unit) {
	hovered, _, _, _, _, _ := e.editorBarAt(x, y)
	changed := false
	e.bars.mu.Lock()
	for _, bar := range e.bars.bars {
		want := bar == hovered || bar.dragging
		if bar.hover != want {
			bar.hover = want
			changed = true
		}
	}
	e.bars.mu.Unlock()
	if changed {
		e.Update()
	}
}

// overEditorScrollbar reports whether a local point lies on a published bar —
// the cursor path asks, so the pointer shows the arrow over chrome.
func (e *Editor) overEditorScrollbar(x, y core.Unit) bool {
	if !core.FindGraphicalFrames(e) {
		return false
	}
	_, _, _, _, _, ok := e.editorBarAt(x, y)
	return ok
}
