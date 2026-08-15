package trinkets

// Pictures on a CELL surface.
//
// A graphical host rasterises the child's images itself (purfecterm_gfx.go). A
// terminal host cannot: it owns no pixels. What it can do is hand them onward,
// because the terminal IT is drawing into may speak a graphics protocol — and
// the TUI backend does exactly that, emitting kitty or sixel after each frame's
// text diff. This file is the half that feeds it.
//
// The whole thing turns on one number. A child sizes, positions and scales
// every picture in pixels-per-cell, and asks for it with CSI 16 t; nothing else
// in the terminal protocols carries geometry. On a graphical surface that
// number comes from the font. Here it can only come from the outer terminal,
// which the backend already asked (it divides ?1016 mouse coordinates by the
// same answer). Unasked, the child is told a cell is 0x0 pixels, and a program
// drawing pictures does the only sensible thing with that: nothing at all.

import (
	"image"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/purfecterm"
)

// tuiImgKey identifies the pixels a placement resolves to. Everything
// imageForBlitGfx reads to build them is in here, so two placements with equal
// keys produce identical output.
type tuiImgKey struct {
	src            *purfecterm.SixelImage
	sx, sy, sw, sh int
	destW, destH   int
}

// tuiImgEntry is one cached, privately-owned rendering.
type tuiImgEntry struct {
	key tuiImgKey
	img *image.RGBA
}

// tuiImgKeyFor reads a placement's identity without building anything.
func tuiImgKeyFor(im *purfecterm.PlacedImage) tuiImgKey {
	sx, sy, sw, sh := im.SourceRect()
	dw, dh := im.DestSize()
	return tuiImgKey{src: im.Image, sx: sx, sy: sy, sw: sw, sh: sh, destW: dw, destH: dh}
}

// cloneRGBA copies an image into storage of its own.
func cloneRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Rect)
	copy(dst.Pix, src.Pix)
	dst.Stride = src.Stride
	return dst
}

// pushCellPixelSizeTUI gives the hosted child the outer terminal's cell size,
// and records the pane's extent in those same pixels for the PTY winsize.
//
// The graphical twin (pushCellPixelSizeGfx) derives the cell from the font and
// the display's density. There is no equivalent freedom here: an image will be
// handed to the outer terminal untouched, so it has to be authored in that
// terminal's pixels, which means the cell we advertise must be that terminal's
// cell exactly. No scaling, no oversampling — the outer terminal is the one
// dealing with the display, and it has already made those decisions.
//
// Silent until the query is answered, rather than advertising a guess: a cell
// size is arrived at by asking, and the reply comes back asynchronously (see
// core.CellPixelSizer). The resize that carries it to the child follows on its
// own, because the pane's pixel extent changes when this lands even though its
// grid does not.
func (t *PurfecTerm) pushCellPixelSizeTUI(p *core.Painter) {
	if t.terminal == nil {
		return
	}
	cw, ch := p.CellPixelSize()
	if cw <= 0 || ch <= 0 {
		return
	}
	cols, rows := t.terminal.GetSize()
	if cols <= 0 || rows <= 0 {
		return
	}

	buf := t.terminal.Buffer()
	buf.SetCellPixelSize(cw, ch)
	// One number, as everywhere else: whatever a cell is said to measure is
	// what a ?1016 pointer coordinate is divided by.
	buf.SetPointerPixelUnit(cw, ch)

	// The pane in pixels. Unlike the graphical case this IS the product of
	// cells and the cell size, because a cell surface has no sub-cell extent
	// to lose — the pane is a whole number of cells by construction.
	moved := t.gfx.advCW != cw || t.gfx.advCH != ch
	t.gfx.advCW, t.gfx.advCH = cw, ch
	t.gfx.contentWpx, t.gfx.contentHpx = float64(cols*cw), float64(rows*ch)

	// The cell size arrives after the first resize — it takes a round trip to
	// the outer terminal — so the grid the child was given is already right
	// while its pixel window is still zero. Nothing else will correct that:
	// the grid has not changed, and a resize keyed on cells alone is silent.
	if moved {
		t.emitResize(cols, rows)
	}
}

// renderImagesTUI hands the child's placed images to the painter, which on a
// cell surface queues them for the outer terminal (core.ImageDrawer ->
// TUIBackend.DrawImage). A backend that cannot draw pictures ignores them, so
// this costs a walk of an empty list on every other host.
//
// Placement is by CELL, which is all the outer terminal offers: it positions a
// picture by moving the cursor. The anchor is therefore the cell the graphical
// path would have anchored to, with no sub-cell part.
//
// ORDER IS LOAD-BEARING: this runs AFTER the trinket has painted its own text.
// A cell surface resolves "what is on top" by paint order, and a picture is
// queued rather than composited, so it is taken to be covered by anything
// written to its cells afterwards. Queued before the cell loop, a terminal's
// own blank cells — the very ones the picture sits on — count as painted over
// it, and every cell of it is suppressed. The picture then never appears at
// all, which is a far worse failure than the spilling it was meant to fix.
//
// Z-ORDER IS NOT HONOURED, and cannot be. The graphical path straddles the cell
// loop with the two bands GetImagesByZ returns, so a negative-z image paints
// under the glyphs; here every picture is emitted after the whole frame's text,
// because the screen is written as one diff at the end and anything drawn
// before it would simply be overwritten. So the bands are drawn in z order
// among themselves and all of them land on top of the text.
func (t *PurfecTerm) renderImagesTUI(p *core.Painter, buf *purfecterm.Buffer, metrics core.CellMetrics, bounds core.UnitRect) {
	if buf == nil {
		return
	}
	below, above := buf.GetImagesByZ()
	if len(below) == 0 && len(above) == 0 {
		return
	}
	scrollOffset := buf.GetEffectiveScrollOffset()
	horizOffset := buf.GetHorizOffset()
	// A fresh slice, not prev[:0]: they would share one backing array and
	// each append would overwrite the entry it is about to be compared with.
	prev := t.tuiImgCache
	t.tuiImgCache = make([]tuiImgEntry, 0, len(prev)+len(below)+len(above))
	for _, band := range [][]*purfecterm.PlacedImage{below, above} {
		for _, im := range band {
			img := t.tuiImageFor(im, prev)
			if img == nil {
				continue
			}
			// The same cell arithmetic as the graphical anchor, stopping at
			// the cell rather than going on to pixels.
			col := im.Col - horizOffset
			row := im.Row + scrollOffset
			if col < 0 || row < 0 {
				// Scrolled partly off the top or left. Cut away the part that
				// is gone and anchor what remains at the edge — dropping the
				// whole picture instead, which this did, makes it disappear
				// outright the moment one corner leaves the view.
				cut := t.cropLeadingCells(p, img, col, row)
				if cut == nil {
					continue // entirely past the edge, or nothing to crop with
				}
				img, col, row = cut, max(col, 0), max(row, 0)
			}
			x, y := metrics.CellToUnitsX(col), metrics.CellToUnitsY(row)
			if x >= bounds.Width || y >= bounds.Height {
				continue
			}
			p.DrawImage(x, y, img)
		}
	}
}

// tuiImageFor returns this placement's pixels, in storage of their own, reusing
// the previous frame's copy when the placement resolves to the same thing.
//
// Both halves matter. imageForBlitGfx hands back a SHARED scratch buffer valid
// only until the next call — the graphical path composites immediately and is
// fine with that, but this one gives the image to a surface that emits it at
// the END of the frame, by which time a second picture would have overwritten
// the first and both would show the last one. So the result has to be copied.
//
// And the copy has to be the SAME copy while nothing changes. A frame is drawn
// on a heartbeat whether or not anything happened; a fresh copy each time is a
// pointer the surface has never seen, so it would re-transmit every picture
// down the pty forever for no change at all. Holding the pointer steady is what
// lets that comparison mean "unchanged".
func (t *PurfecTerm) tuiImageFor(im *purfecterm.PlacedImage, prev []tuiImgEntry) *image.RGBA {
	key := tuiImgKeyFor(im)
	for i := range prev {
		if prev[i].key == key {
			t.tuiImgCache = append(t.tuiImgCache, prev[i])
			return prev[i].img
		}
	}
	built := t.imageForBlitGfx(im)
	if built == nil {
		return nil
	}
	img := cloneRGBA(built)
	t.tuiImgCache = append(t.tuiImgCache, tuiImgEntry{key: key, img: img})
	return img
}

// cropLeadingCells removes the cells of an image that lie before the origin —
// the part scrolled off the top or left — and returns what is left. nil when
// nothing survives, or when the cell size is unknown and the cut cannot be
// measured.
//
// The outer terminal places a picture at a cell and draws all of it from
// there; it cannot be asked to start part way in. So the trimming has to
// happen here, on the pixels, before it is handed over.
func (t *PurfecTerm) cropLeadingCells(p *core.Painter, img *image.RGBA, col, row int) *image.RGBA {
	cw, ch := p.CellPixelSize()
	if cw <= 0 || ch <= 0 {
		return nil
	}
	b := img.Bounds()
	x0, y0 := b.Min.X, b.Min.Y
	if col < 0 {
		x0 += -col * cw
	}
	if row < 0 {
		y0 += -row * ch
	}
	r := image.Rect(x0, y0, b.Max.X, b.Max.Y).Intersect(b)
	if r.Empty() {
		return nil
	}
	return img.SubImage(r).(*image.RGBA)
}
