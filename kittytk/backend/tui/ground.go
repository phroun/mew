package tui

import (
	"github.com/phroun/khatool"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// What a cell wears divides in two. Some of it is INK -- the glyph's own
// colour, its weight, its slant -- and travels with the glyph wherever a
// terminal decides to put it. The rest is painted at the CELL: the background,
// reverse video, an underline, a strike. A terminal that reorders what it is
// sent and miscounts while doing it puts those in the wrong place, so on a row
// where that has started they are dropped rather than left to land somewhere
// they do not belong. Dropping makes a picture less informative; leaving one to
// drift makes it wrong.
//
// Measured rather than assumed: a background over six pointed letters arrives
// on one of them, an underline arrives under one of them, and reverse video
// splits -- its inverted foreground rides with the glyph while its inverted
// background drifts off, so the text goes dark on dark and vanishes.
//
// A blank cell is the one case that loses nothing by the drop: with no glyph of
// its own to carry, it can say what its background was by DRAWING it. Black is
// the exception, being the ground a terminal already shows -- there is nothing
// there to draw, though it still goes, since a black fill landing on a coloured
// cell would black that cell out.
//
// The shade rather than a full block: fonts draw the full block as often as a
// rectangular bullet as a tile, so a run of them can come out beaded. mew uses
// a quarter for the selection and three quarters for its gutter; this is what
// anything with no treatment of its own wears.
const fallbackBlank = '▒'

// cellPainted is everything the terminal draws at the cell rather than on the
// glyph, and so everything a miscounting host puts in the wrong place.
const cellPainted = style.StyleReverse | style.StyleUnderline |
	style.StyleStrikethrough

// dropGround returns the style with everything painted at the cell removed and
// everything belonging to the glyph kept.
func dropGround(s style.CellStyle) style.CellStyle {
	s.Bg = style.ColorDefault
	s.Attrs &^= cellPainted
	return s
}

// groundAsInk returns the style a blank cell wears to draw its own background
// instead of being given one, and whether there was anything to draw.
func groundAsInk(s style.CellStyle) (style.CellStyle, bool) {
	if s.Bg == style.ColorDefault || s.Bg == style.ColorTransparent ||
		s.Bg == style.ColorBlack {
		return dropGround(s), false
	}
	ink := dropGround(s)
	ink.Fg = s.Bg
	return ink, true
}

// emitSlot is one base cell of a row, with its column and how many it takes --
// the currency the flip emission works in.
type emitSlot struct {
	x    int
	cell Cell
	w    int
}

// driftStart is the emission slot where a miscounting host stops placing a
// background where it was written: the first slot of the first right-to-left
// run still carrying a mark once folding has had its way. -1 when the row has
// none.
//
// A run is a stretch of slots the plan turned over, which is what mirror marks.
// The whole run is affected, not just the marked cell, so the answer is the
// run's first slot rather than the mark's.
func driftStart(slots []emitSlot, order []int, mirror []bool) int {
	folding := core.RtlMarkFolds()
	for k := 0; k < len(order); {
		if !mirror[k] {
			k++
			continue
		}
		end := k
		for end+1 < len(order) && mirror[end+1] {
			end++
		}
		for j := k; j <= end; j++ {
			c := slots[order[j]].cell
			runes := append([]rune{c.Char}, []rune(c.Combining)...)
			if khatool.HasZeroWidthAfterFold(runes, folding, core.ZeroWidth) {
				return k
			}
		}
		k = end + 1
	}
	return -1
}
