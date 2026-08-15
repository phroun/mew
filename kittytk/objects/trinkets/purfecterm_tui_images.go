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
	"github.com/phroun/kittytk/core"
	"github.com/phroun/purfecterm"
)

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
	for _, band := range [][]*purfecterm.PlacedImage{below, above} {
		for _, im := range band {
			img := t.imageForBlitGfx(im)
			if img == nil {
				continue
			}
			// The same cell arithmetic as the graphical anchor, stopping at
			// the cell rather than going on to pixels.
			col := im.Col - horizOffset
			row := im.Row + scrollOffset
			if col < 0 || row < 0 {
				continue // scrolled off the top or left; the outer terminal
			} // has no way to clip a picture, so it must not be sent
			x, y := metrics.CellToUnitsX(col), metrics.CellToUnitsY(row)
			if x >= bounds.Width || y >= bounds.Height {
				continue
			}
			p.DrawImage(x, y, img)
		}
	}
}
