package raster

import (
	"image"
	"image/color"
	"sync"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// DEC double-width / double-height lines (DECDWL ESC#6, DECDHL ESC#3/#4) on a
// pixel surface.
//
// A terminal backend gets this for free — it emits the DEC escape and the real
// terminal scales the row. A pixel surface has to draw the scaling itself, and
// without this file it fell back to core.Painter's literal path: the glyph at
// its ORDINARY size with a filler space beside it. Columns and hit-testing
// were right, but a heading looked like normal text loosely spaced rather than
// a genuinely bigger line.
//
// One rasterization serves all three modes. The glyph is rendered at DOUBLE
// the cell face's point size, giving an image twice as wide and twice as tall,
// and each mode samples it differently:
//
//	DECDWL  ('6') — twice as wide, ordinary height: box-filter the 2x image
//	                down by 2 vertically, keeping full horizontal detail.
//	DECDHL  ('3') — the top half of the 2x image, unscaled.
//	        ('4') — the bottom half, unscaled: the two rows together show one
//	                glyph at twice the size.
//
// Rendering large and sampling down beats stretching a cell-sized bitmap: the
// horizontal detail is real outline detail at the final resolution rather than
// interpolated pixels, which is what makes a doubled heading look drawn rather
// than magnified.

// DEC line-mode selectors, as they appear in the ESC # <mode> sequence and in
// core.Painter.DrawCellDWL's mode argument.
const (
	dwlModeDouble byte = '6' // DECDWL: double width, single height
	dwlModeTop    byte = '3' // DECDHL top half
	dwlModeBottom byte = '4' // DECDHL bottom half
)

// dwlKey identifies one scaled glyph image. Like textKey it is all-comparable,
// so it maps with no per-call allocation.
type dwlKey struct {
	ch        rune
	combining string
	fg        color.RGBA
	mode      byte
	cellH     int
	boxPx     int // target box width: a squeezed glyph depends on it
	scale     int
	fontSize  int
}

// dwlCache holds the scaled glyphs. Scaling is far more expensive than a plain
// rasterization (a 2x render plus a resample), and a doubled heading redraws
// every frame it is on screen, so the result is cached exactly like the
// ordinary text images are. Generation-swapped rather than evicted per entry,
// matching textImageCache.
var dwlCache = struct {
	sync.Mutex
	cur, prev map[dwlKey]*image.RGBA
	epoch     uint64
}{
	cur:  map[dwlKey]*image.RGBA{},
	prev: map[dwlKey]*image.RGBA{},
}

const dwlCacheMax = 512

// DrawCellDWL implements core.DWLCellDrawer: one logical cell of a DEC
// double-width or double-height line, occupying twice its ordinary width on
// screen. Returns the columns consumed.
//
// cellWidth is purfecterm's flex-width attribute (Cell.CellWidth: 0.5, 1.0,
// 1.5, 2.0), authoritative when present — the same precedence the GTK and Qt
// renderers give it via cellVisualWidth. Zero falls back to the rune's own
// East Asian width, so a caller with no flex-width model still gets a wide
// glyph its four columns.
func (b *Backend) DrawCellDWL(x, y core.Unit, ch rune, combining string, s style.CellStyle, mode byte, cellWidth float64) int {
	fg, bg := b.styleColors(s)
	cellH := b.metrics.CellHeight

	visual := cellWidth
	if visual <= 0 {
		visual = 1
		if isWide(ch) {
			visual = 2
		}
	}
	// The box is the cell's visual width, doubled — cellVisualWidth * charWidth
	// * 2.0, exactly as the GTK/Qt paths compute it.
	adv := core.Unit(float64(b.metrics.CellWidth) * visual * 2)
	cols := int(visual*2 + 0.5)
	if cols < 1 {
		cols = 1
	}

	// Cell-snap the box exactly as drawRune does, so a doubled row tiles
	// pixel-exact against its neighbours at any font_size.
	xPx, yPx := b.pxX(x), b.pxY(y)
	advPx := b.pxX(x+adv) - xPx
	hPx := b.pxY(y+cellH) - yPx
	if s.Bg != style.ColorTransparent {
		b.fillPx(xPx, yPx, xPx+advPx, yPx+hPx, bg)
	}

	if ch != ' ' && ch != 0 {
		if img := b.dwlGlyph(ch, combining, fg, mode, cellH, advPx); img != nil {
			pad := (advPx - img.Rect.Dx()) / 2
			if pad < 0 {
				pad = 0
			}
			b.compositeRGBA(xPx+pad, yPx, img)
		}
	}
	if s.Attrs&style.StyleUnderline != 0 {
		b.fillPx(xPx, b.pxY(y+cellH-2), xPx+advPx, b.pxY(y+cellH-1), fg)
	}
	return cols
}

// dwlGlyph returns the scaled glyph image for one mode, rasterizing and
// caching on first use. nil when the glyph rasterizes empty.
func (b *Backend) dwlGlyph(ch rune, combining string, fg color.RGBA, mode byte, cellH core.Unit, boxPx int) *image.RGBA {
	key := dwlKey{
		ch: ch, combining: combining, fg: fg, mode: mode,
		cellH: int(cellH), boxPx: boxPx, scale: b.scale, fontSize: b.fontSize,
	}

	dwlCache.Lock()
	defer dwlCache.Unlock()
	if e := engine().Epoch(); e != dwlCache.epoch {
		// Font set changed: shaped output may differ - flush, as the text
		// image cache does.
		dwlCache.epoch = e
		dwlCache.cur = map[dwlKey]*image.RGBA{}
		dwlCache.prev = map[dwlKey]*image.RGBA{}
	}
	img, ok := dwlCache.cur[key]
	if !ok {
		if img, ok = dwlCache.prev[key]; ok {
			dwlCache.cur[key] = img // keep the working set warm
		}
	}
	if !ok {
		img = b.renderDWLGlyph(ch, combining, fg, mode, cellH, boxPx)
		if len(dwlCache.cur) >= dwlCacheMax {
			dwlCache.prev = dwlCache.cur
			dwlCache.cur = map[dwlKey]*image.RGBA{}
		}
		dwlCache.cur[key] = img
	}
	return img
}

// renderDWLGlyph rasterizes the glyph at double the cell face's size and
// samples the half the mode asks for. The 2x image is transparent (alpha
// only): DrawCellDWL has already filled the background across both cells, so
// compositing preserves whatever sits beneath a transparent-background cell.
func (b *Backend) renderDWLGlyph(ch rune, combining string, fg color.RGBA, mode byte, cellH core.Unit, boxPx int) *image.RGBA {
	f := &core.Font{Name: cellFont.Name, Size: cellFontSize(cellH) * 2}
	ti := b.cachedTextImage(f, string(ch)+combining, fg, color.RGBA{}, false, false)
	if ti.img == nil {
		return nil
	}
	src := ti.img
	w, h := src.Rect.Dx(), src.Rect.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}

	// The height of ONE cell in device pixels — the height every mode's
	// result must have, since each covers a single row.
	rowPx := b.pxY(cellH) - b.pxY(0)
	if rowPx < 1 {
		rowPx = 1
	}

	var out *image.RGBA
	switch mode {
	case dwlModeTop, dwlModeBottom:
		// Double height: take the matching half of the 2x raster at its own
		// resolution. The halves meet exactly, so the two rows join seamlessly.
		y0 := 0
		if mode == dwlModeBottom {
			y0 = h / 2
		}
		out = cropRows(src, y0, y0+rowPx)
	default:
		// DECDWL: full width, single height — average pairs of rows so the
		// glyph keeps its 2x horizontal detail at ordinary height.
		out = squashRows(src, rowPx)
	}

	// A glyph wider than its box is squeezed to fit rather than allowed to
	// spill into the neighbouring cell — the same rule the GTK and Qt
	// renderers apply (textScaleX *= targetCellWidth / actualWidth). Narrow
	// glyphs are left alone and centred by the caller.
	if out != nil && boxPx > 0 && out.Rect.Dx() > boxPx {
		out = squashCols(out, boxPx)
	}
	return out
}

// cropRows copies rows [y0, y1) of src into a fresh image, clamping to src and
// padding with transparency when the requested band runs past the raster.
func cropRows(src *image.RGBA, y0, y1 int) *image.RGBA {
	h := y1 - y0
	if h <= 0 {
		return nil
	}
	w := src.Rect.Dx()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		sy := y0 + y
		if sy < 0 || sy >= src.Rect.Dy() {
			continue // outside the raster: leave the row transparent
		}
		si := src.PixOffset(src.Rect.Min.X, src.Rect.Min.Y+sy)
		di := dst.PixOffset(0, y)
		copy(dst.Pix[di:di+w*4], src.Pix[si:si+w*4])
	}
	return dst
}

// squashRows box-filters src down to exactly dstH rows, averaging each output
// row over the source rows it covers. Premultiplied-alpha safe: the engine's
// rasters carry the foreground colour scaled by coverage, so a straight
// per-channel mean is the correct resample.
func squashRows(src *image.RGBA, dstH int) *image.RGBA {
	w, srcH := src.Rect.Dx(), src.Rect.Dy()
	if w <= 0 || srcH <= 0 || dstH <= 0 {
		return nil
	}
	if srcH == dstH {
		return cropRows(src, 0, dstH)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, dstH))
	for y := 0; y < dstH; y++ {
		// The source band this output row covers, at least one row wide.
		y0 := y * srcH / dstH
		y1 := (y + 1) * srcH / dstH
		if y1 <= y0 {
			y1 = y0 + 1
		}
		if y1 > srcH {
			y1 = srcH
		}
		n := y1 - y0
		di := dst.PixOffset(0, y)
		for x := 0; x < w; x++ {
			var r, g, bl, a int
			for sy := y0; sy < y1; sy++ {
				si := src.PixOffset(src.Rect.Min.X+x, src.Rect.Min.Y+sy)
				r += int(src.Pix[si])
				g += int(src.Pix[si+1])
				bl += int(src.Pix[si+2])
				a += int(src.Pix[si+3])
			}
			o := di + x*4
			dst.Pix[o] = uint8(r / n)
			dst.Pix[o+1] = uint8(g / n)
			dst.Pix[o+2] = uint8(bl / n)
			dst.Pix[o+3] = uint8(a / n)
		}
	}
	return dst
}

// squashCols box-filters src down to exactly dstW columns, the horizontal twin
// of squashRows: each output column averages the source columns it covers.
func squashCols(src *image.RGBA, dstW int) *image.RGBA {
	srcW, h := src.Rect.Dx(), src.Rect.Dy()
	if srcW <= 0 || h <= 0 || dstW <= 0 || srcW <= dstW {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, h))
	for x := 0; x < dstW; x++ {
		x0 := x * srcW / dstW
		x1 := (x + 1) * srcW / dstW
		if x1 <= x0 {
			x1 = x0 + 1
		}
		if x1 > srcW {
			x1 = srcW
		}
		n := x1 - x0
		for y := 0; y < h; y++ {
			var r, g, bl, a int
			for sx := x0; sx < x1; sx++ {
				si := src.PixOffset(src.Rect.Min.X+sx, src.Rect.Min.Y+y)
				r += int(src.Pix[si])
				g += int(src.Pix[si+1])
				bl += int(src.Pix[si+2])
				a += int(src.Pix[si+3])
			}
			o := dst.PixOffset(x, y)
			dst.Pix[o] = uint8(r / n)
			dst.Pix[o+1] = uint8(g / n)
			dst.Pix[o+2] = uint8(bl / n)
			dst.Pix[o+3] = uint8(a / n)
		}
	}
	return dst
}
