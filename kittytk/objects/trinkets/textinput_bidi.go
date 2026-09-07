package trinkets

import (
	"github.com/phroun/khatool"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/text"
)

// Where a field's run went.
//
// A caret, a selection and an input method's clause all ask a line the same
// thing: which part of the DRAWN run is this stretch of the text? On a line
// that reads one way the answer is a prefix measurement -- the width of the
// text before the position -- and a field can ask for it that way for as long
// as its content stays in one direction.
//
// It is the wrong question otherwise. The first letter of a Hebrew word is
// drawn at that word's RIGHT edge, so the width of the text before it names the
// far end of the word, and a caret placed there sits where the word ENDS while
// claiming to be where it begins.
//
// So the field asks this instead. Every logical rune has a BOX -- the cluster
// it was drawn in -- and every question above is a set of boxes.
type fieldGeometry struct {
	// draw is the run to hand the painter, in the order it will be stamped.
	// Empty means "the text itself": a pixel target orders and shapes whole
	// paragraphs on its own, and handing it a run already turned over would
	// turn it back.
	draw string

	// lo and hi are, per LOGICAL rune, the edges of the box it was drawn in.
	// Runes share a box where a ligature carries them together, and a mark
	// shares the box of the base it composes onto.
	lo, hi []core.Unit

	// rtl is, per logical rune, whether it sits in a right-to-left run. It is
	// what says which edge of a box the caret leaves by.
	rtl []bool

	// total is the whole run's width.
	total core.Unit

	// marks are the direction-marker slots, present only when the run was laid
	// out with them (see SetShowBidiControls).
	marks []fieldMark
}

// fieldMark is one direction marker on the run: where it sits, the glyph it
// shows, and whether it stands for a control the AUTHOR typed rather than one
// the field added to show a turn.
type fieldMark struct {
	x     core.Unit
	w     core.Unit
	glyph rune
	typed bool
}

// boxOf is the box logical rune i was drawn in, and whether the run has one.
func (g *fieldGeometry) boxOf(i int) (lo, hi core.Unit, ok bool) {
	if g == nil || i < 0 || i >= len(g.lo) {
		return 0, 0, false
	}
	return g.lo[i], g.hi[i], true
}

// caretBox is where the caret goes for a logical position, following mew's
// rule: the caret COVERS the box of the rune it precedes, in either base
// direction and for a rune of either direction. It is a block, not a boundary,
// which is what lets it be exact where a boundary would be ambiguous -- at a
// direction change the same insertion point stands at two different edges, and
// a block sits on the character instead of choosing between them.
//
// Past the end of the text there is no rune to cover, so the caret takes the
// width of one blank at the run's READING end: the left edge for a run that
// ends right-to-left, the right edge otherwise.
func (g *fieldGeometry) caretBox(p int, blank core.Unit) (lo, hi core.Unit) {
	if g == nil || len(g.lo) == 0 {
		return 0, blank
	}
	if p < 0 {
		p = 0
	}
	if p < len(g.lo) {
		// A mark has no box of its own -- it shares its base's -- so a caret
		// standing on one covers the whole cluster rather than a sliver of it.
		return g.lo[p], g.hi[p]
	}
	last := len(g.lo) - 1
	if g.rtl[last] {
		lo = g.lo[last] - blank
		if lo < 0 {
			lo = 0
		}
		return lo, g.lo[last]
	}
	return g.hi[last], g.hi[last] + blank
}

// spans is the stretches of the drawn run that logical runes [from, to) were
// drawn in, left to right.
//
// A logical range is not one stretch. Select a word that spans a direction
// change and the characters chosen sit in two places on the line with text
// between them that was not selected -- which is what the reader sees, and
// what a highlight drawn as a single rectangle would lie about.
func (g *fieldGeometry) spans(from, to int) [][2]core.Unit {
	if g == nil || to <= from {
		return nil
	}
	if from < 0 {
		from = 0
	}
	if to > len(g.lo) {
		to = len(g.lo)
	}
	var out [][2]core.Unit
	for i := from; i < to; i++ {
		lo, hi := g.lo[i], g.hi[i]
		if hi <= lo {
			continue // a rune sharing the box of the one before it
		}
		if n := len(out); n > 0 && lo <= out[n-1][1] && hi >= out[n-1][0] {
			if lo < out[n-1][0] {
				out[n-1][0] = lo
			}
			if hi > out[n-1][1] {
				out[n-1][1] = hi
			}
			continue
		}
		out = append(out, [2]core.Unit{lo, hi})
	}
	// Boxes come in logical order, which on a turned-over run is not the order
	// they sit in, so a second pass joins what is now adjacent.
	sortSpans(out)
	var joined [][2]core.Unit
	for _, s := range out {
		if n := len(joined); n > 0 && s[0] <= joined[n-1][1] {
			if s[1] > joined[n-1][1] {
				joined[n-1][1] = s[1]
			}
			continue
		}
		joined = append(joined, s)
	}
	return joined
}

func sortSpans(s [][2]core.Unit) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j][0] < s[j-1][0]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// runGeometry is where the field's run went, worked out the way the target
// that will draw it works: from the shaper on a pixel target, from the cell
// rule on a cell one.
func (t *TextInput) runGeometry(runes []rune, font *core.Font, graphical, marked bool) *fieldGeometry {
	if graphical {
		return t.shapedGeometry(runes, font)
	}
	return t.cellGeometry(runes, marked)
}

// shapedGeometry reads the boxes off the shaped line.
//
// It shapes with the SAME call the painter makes -- ShapeRun, whose base
// direction is the text's own first strong character -- because the geometry
// and the ink have to be the same layout. Shaping here with the field's
// declared direction instead would put the caret on a line the painter never
// drew.
func (t *TextInput) shapedGeometry(runes []rune, font *core.Font) *fieldGeometry {
	e := text.Shared()
	if e == nil {
		// A pixel target publishes its engine the first time it is asked to
		// measure. That has always happened by the time a field paints -- it
		// is laid out first -- but a geometry that quietly answered zero for
		// every rune would be a caret pinned to the left edge, so ask.
		t.MeasureText(" ")
		e = text.Shared()
	}
	if e == nil || len(runes) == 0 {
		return emptyGeometry(len(runes))
	}
	sp := e.ShapeRun(font, string(runes))
	if sp == nil || len(sp.Lines) == 0 {
		return emptyGeometry(len(runes))
	}
	l := &sp.Lines[0]
	g := &fieldGeometry{
		lo:    make([]core.Unit, len(runes)),
		hi:    make([]core.Unit, len(runes)),
		rtl:   make([]bool, len(runes)),
		total: l.Width,
	}
	for i := range runes {
		a, b := l.CaretX(i), l.CaretX(i+1)
		if a > b {
			a, b = b, a
			g.rtl[i] = true
		}
		g.lo[i], g.hi[i] = a, b
	}
	return g
}

// cellGeometry works the boxes out the way a cell target draws: the runes put
// in visual order and each given the cells it occupies.
func (t *TextInput) cellGeometry(runes []rune, marked bool) *fieldGeometry {
	cw := t.EffectiveCellMetrics().UnitsPerCellWidth
	dir := core.FindEffectiveDirection(t.Self())
	baseRTL := dir == core.DirRTL

	var lay *khatool.Layout
	if marked {
		lay = khatool.OrderMarked(runes, baseRTL, core.CellRides)
	} else {
		lay = khatool.Order(runes, baseRTL, core.CellRides)
	}
	g := &fieldGeometry{
		lo:  make([]core.Unit, len(runes)),
		hi:  make([]core.Unit, len(runes)),
		rtl: make([]bool, len(runes)),
	}
	if lay == nil {
		// Visual order is logical order: every rune takes its own cells, in
		// the order they were given.
		x := core.Unit(0)
		var b []rune
		for i, r := range runes {
			w := core.Unit(core.CellWidth(r)) * cw
			g.lo[i], g.hi[i] = x, x+w
			x += w
			b = append(b, r)
		}
		g.draw, g.total = string(b), x
		return g
	}

	x := core.Unit(0)
	var b []rune
	for _, slot := range lay.Perm {
		if slot < 0 {
			// A marker the field added to show where the direction turns.
			glyph := markerGlyph(slot)
			g.marks = append(g.marks, fieldMark{x: x, w: cw, glyph: glyph})
			b = append(b, glyph)
			x += cw
			continue
		}
		r := runes[slot]
		if lay.Glyph != nil {
			if gl := lay.Glyph[slot]; gl == khatool.LigatureAbsorbed {
				// The glyph before it is showing this rune too, so it shares
				// that box and takes none of its own.
				g.lo[slot], g.hi[slot] = x, x
				g.rtl[slot] = lay.RTL[slot]
				continue
			} else {
				r = gl
			}
		}
		if lay.RTL[slot] {
			r = khatool.Mirror(r)
		}
		w := core.Unit(core.CellWidth(r)) * cw
		if lay.Marked && khatool.IsDirectionControl(runes[slot]) {
			// Under the markers an explicit control is shown rather than
			// spent: it is the turn it stands for, in one cell of its own.
			w = cw
			r = controlGlyph(lay.RTL[slot])
			g.marks = append(g.marks, fieldMark{x: x, w: cw, glyph: r, typed: true})
		}
		g.lo[slot], g.hi[slot] = x, x+w
		g.rtl[slot] = lay.RTL[slot]
		b = append(b, r)
		x += w
	}
	// A rune the run gave no cell to shares the box of the cluster it rides,
	// which is the box of the base before it in the drawn order.
	for i := range runes {
		if g.hi[i] == g.lo[i] && i > 0 {
			g.lo[i], g.hi[i] = g.lo[i-1], g.hi[i-1]
		}
	}
	g.draw, g.total = string(b), x
	return g
}

func emptyGeometry(n int) *fieldGeometry {
	return &fieldGeometry{
		lo:  make([]core.Unit, n),
		hi:  make([]core.Unit, n),
		rtl: make([]bool, n),
	}
}

// markerGlyph is what a marker slot shows: the direction a fragment begins
// reading in, or the end of one.
func markerGlyph(slot int) rune {
	switch slot {
	case khatool.MarkerLTR:
		return '>'
	case khatool.MarkerRTL:
		return '<'
	}
	return '|'
}

// controlGlyph is what an explicit direction control shows under the markers:
// the same two arrows, because it means the same thing the field's own marker
// would have meant there.
func controlGlyph(rtl bool) rune {
	if rtl {
		return '<'
	}
	return '>'
}
