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

	// loPx and hiPx are the same edges in device pixels, filled when the
	// caller had a painter to ask. A unit is a layout granularity: rounding a
	// position inside a run to one leans the caret into the glyph beside it.
	loPx, hiPx []int
	havePx     bool

	// total is the whole run's width.
	total core.Unit

	// marks are the direction-marker slots, present only when the run was laid
	// out with them (see SetShowBidiControls).
	marks []fieldMark
}

// fieldMark is one stretch of the run that is not the author's own glyph:
// where it sits, and what stands there. Two things qualify -- a direction
// marker the field added to show where the reading turns, and a substitute
// standing in for a character that must not be drawn as itself.
//
// They share a colour because they are the same kind of thing to a reader:
// the field telling them about the text rather than showing it.
type fieldMark struct {
	x    core.Unit
	w    core.Unit
	text string
}

// substituteFor is what the field draws in place of a rune it must not hand
// to a renderer as itself, and whether there is one.
//
// Two kinds. A CONTROL character has no glyph and, worse, a terminal decoding
// one acts on it -- the C1 range holds the introducers that make a terminal
// swallow the rest of the line -- so it shows as its caret or hex form. An
// ill-formed combining MARK has no base to compose onto, and a shaper that
// rejects the pairing falls back to a spacing glyph that advances a cell
// nobody budgeted, so it shows on a dotted circle that supplies the base.
//
// Either way what comes back is measured and treated as ONE thing: the caret
// covers the whole of it, an arrow walks past all of it at once, and a
// selection takes it whole. It stands for a single character, and reading it
// as several would be reading the field's own notation as text.
func substituteFor(runes []rune, i int) (string, bool) {
	r := runes[i]
	if khatool.IsControl(r) {
		return khatool.Substitute(r), true
	}
	if khatool.IsMark(r) && khatool.DefectiveMark(khatool.PrevBase(runes, i), r) {
		return khatool.MarkForm(r), true
	}
	return "", false
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

// spansPx is spans in device pixels. Where the geometry kept no pixel record
// -- a cell target, whose cells are whole units -- the caller's scale converts
// the unit answer, which there is the same answer.
func (g *fieldGeometry) spansPx(from, to int, scale func(core.Unit) int) [][2]int {
	if g == nil {
		return nil
	}
	if !g.havePx {
		var out [][2]int
		for _, sp := range g.spans(from, to) {
			out = append(out, [2]int{scale(sp[0]), scale(sp[1])})
		}
		return out
	}
	if from < 0 {
		from = 0
	}
	if to > len(g.loPx) {
		to = len(g.loPx)
	}
	var out [][2]int
	for i := from; i < to; i++ {
		if lo, hi := g.loPx[i], g.hiPx[i]; hi > lo {
			out = append(out, [2]int{lo, hi})
		}
	}
	sortSpansPx(out)
	var joined [][2]int
	for _, sp := range out {
		if n := len(joined); n > 0 && sp[0] <= joined[n-1][1] {
			if sp[1] > joined[n-1][1] {
				joined[n-1][1] = sp[1]
			}
			continue
		}
		joined = append(joined, sp)
	}
	return joined
}

// caretBoxPx is caretBox in device pixels.
func (g *fieldGeometry) caretBoxPx(p, blankPx int, scale func(core.Unit) int) (lo, hi int) {
	if g == nil || !g.havePx || len(g.loPx) == 0 {
		u0, u1 := g.caretBox(p, 0)
		lo, hi = scale(u0), scale(u1)
		if hi <= lo {
			hi = lo + blankPx
		}
		return lo, hi
	}
	if p < 0 {
		p = 0
	}
	if p < len(g.loPx) {
		return g.loPx[p], g.hiPx[p]
	}
	last := len(g.loPx) - 1
	if g.rtl[last] {
		if lo = g.loPx[last] - blankPx; lo < 0 {
			lo = 0
		}
		return lo, g.loPx[last]
	}
	return g.hiPx[last], g.hiPx[last] + blankPx
}

func sortSpansPx(s [][2]int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j][0] < s[j-1][0]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
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
// ppu is the painter's pixels per unit, or zero when the caller has no painter
// and wants only the unit answers -- the scroll clamp, which runs from a key
// press.
func (t *TextInput) runGeometry(runes []rune, font *core.Font, graphical, marked bool, ppu float64) *fieldGeometry {
	if graphical {
		return t.shapedGeometry(runes, font, ppu)
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
func (t *TextInput) shapedGeometry(runes []rune, font *core.Font, ppu float64) *fieldGeometry {
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
	// The run the shaper sees is the run that will be drawn, so the substitutes
	// go in FIRST and are shaped, measured and ordered as the text they are.
	// span says which stretch of it each logical rune became.
	shaped, span, substituted := substituteRun(runes)
	sp := e.ShapeRun(font, string(shaped))
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
	if substituted {
		g.draw = string(shaped)
	}
	if ppu > 0 {
		g.loPx, g.hiPx, g.havePx = make([]int, len(runes)), make([]int, len(runes)), true
	}
	for i := range runes {
		a, b := l.CaretX(span[i][0]), l.CaretX(span[i][1])
		rtl := a > b
		if rtl {
			a, b = b, a
		}
		g.rtl[i] = rtl
		g.lo[i], g.hi[i] = a, b
		if g.havePx {
			c, d := l.CaretXPx(span[i][0], ppu), l.CaretXPx(span[i][1], ppu)
			if c > d {
				c, d = d, c
			}
			g.loPx[i], g.hiPx[i] = c, d
		}
		if substituted && span[i][1] > span[i][0]+1 {
			g.marks = append(g.marks, fieldMark{x: a, w: b - a,
				text: string(shaped[span[i][0]:span[i][1]])})
		}
	}
	return g
}

// substituteRun is the text with every character that must not be drawn as
// itself replaced by what stands in for it, the stretch of that text each
// logical rune became, and whether anything was replaced at all.
func substituteRun(runes []rune) (shaped []rune, span [][2]int, substituted bool) {
	span = make([][2]int, len(runes))
	shaped = make([]rune, 0, len(runes))
	for i := range runes {
		start := len(shaped)
		if sub, ok := substituteFor(runes, i); ok {
			shaped = append(shaped, []rune(sub)...)
			substituted = true
		} else {
			shaped = append(shaped, runes[i])
		}
		span[i] = [2]int{start, len(shaped)}
	}
	return shaped, span, substituted
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
	x := core.Unit(0)
	var b []rune

	// put lays one logical rune down at x and moves on. A substitute takes the
	// cells its own text needs; everything else takes the cells its glyph does.
	put := func(slot int, glyph rune, rtl bool) {
		if sub, ok := substituteFor(runes, slot); ok {
			w := core.Unit(len([]rune(sub))) * cw
			g.marks = append(g.marks, fieldMark{x: x, w: w, text: sub})
			g.lo[slot], g.hi[slot], g.rtl[slot] = x, x+w, rtl
			b = append(b, []rune(sub)...)
			x += w
			return
		}
		w := core.Unit(core.CellWidth(glyph)) * cw
		g.lo[slot], g.hi[slot], g.rtl[slot] = x, x+w, rtl
		b = append(b, glyph)
		x += w
	}

	if lay == nil {
		// Visual order is logical order: every rune takes its own cells, in
		// the order they were given.
		for i, r := range runes {
			put(i, r, false)
		}
		g.draw, g.total = string(b), x
		return g
	}

	for _, slot := range lay.Perm {
		if slot < 0 {
			// A marker the field added to show where the direction turns.
			glyph := markerGlyph(slot)
			g.marks = append(g.marks, fieldMark{x: x, w: cw, text: string(glyph)})
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
		if lay.Marked && khatool.IsDirectionControl(runes[slot]) {
			// Under the markers an explicit control is shown rather than
			// spent: it is the turn it stands for, in one cell of its own.
			glyph := controlGlyph(lay.RTL[slot])
			g.marks = append(g.marks, fieldMark{x: x, w: cw, text: string(glyph)})
			g.lo[slot], g.hi[slot] = x, x+cw
			g.rtl[slot] = lay.RTL[slot]
			b = append(b, glyph)
			x += cw
			continue
		}
		put(slot, r, lay.RTL[slot])
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

// cellSlice is the part of the drawn run lying in [from, to), and where that
// part starts. Whole cells only: a cell target cannot show half of one.
func (g *fieldGeometry) cellSlice(from, to, cw core.Unit) (string, core.Unit) {
	if g == nil || cw <= 0 {
		return "", 0
	}
	x, at := core.Unit(0), core.Unit(-1)
	var out []rune
	for _, r := range g.draw {
		w := core.Unit(core.CellWidth(r)) * cw
		if x >= from && x+w <= to {
			if at < 0 {
				at = x
			}
			out = append(out, r)
		}
		x += w
	}
	if at < 0 {
		at = from
	}
	return string(out), at
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

// The more-markers: a single glyph pinned to each end of the field saying
// there is text past it that way. They are drawn INSIDE the field's span and
// the run gives up that much room, which is how a field has always shown it --
// the arrows are chrome, and chrome that overlapped the text it is describing
// would be reporting on itself.
const (
	moreLeftGlyph  = '◀'
	moreRightGlyph = '▶'
)

// markWidth is the room one more-marker takes.
func (t *TextInput) markWidth() core.Unit {
	if w := t.MeasureText(string(moreRightGlyph)); w > 0 {
		return w
	}
	return t.EffectiveCellMetrics().UnitsPerCellWidth
}

// defaultShowAhead is how far a field looks ahead when nothing sets it: two
// characters' worth, which is enough to see what is being typed toward without
// spending much of a short field on room the caret never reaches.
const defaultShowAhead = 2

// showAhead is how much of the run the field keeps visible PAST the caret,
// counted in blanks. It is a margin, not a boundary: the caret never reaches
// the edge, so there is always a little of what is being typed toward.
//
// Both directions trigger on the same margin, but scrolling BACK aims further:
// it puts the caret a couple of characters inside the margin rather than on it,
// so stepping forward again does not scroll the run the other way at once,
// which reads as the text shivering under the caret.
func (t *TextInput) showAheadUnits(blank core.Unit) (ahead, back core.Unit) {
	n := t.showAhead
	if n < 0 {
		n = 0
	}
	return core.Unit(n) * blank, core.Unit(n+2) * blank
}

// window settles where the run sits behind the field: how far into the run the
// field's own left edge falls, and which more-markers show.
//
// The caret cannot be lost in it. The clamp keeps the caret's whole box inside
// the room BETWEEN the markers, so a caret at either end of a scrolled run
// stands beside a marker and never under one. A marker appearing takes room,
// which can want a further scroll, so the answer is settled twice -- and only
// twice, because a marker that has appeared cannot appear again.
//
// quantum, where it is not zero, is the width the field's own left edge is
// allowed to land on multiples of. A cell target passes its cell width: every
// box there is a whole number of cells, so rounding the ROOM down to whole
// cells leaves every position in the reckoning cell-aligned, and the edge falls
// between two characters rather than through one. A pixel target passes zero
// and the run slides smoothly under the field.
func (t *TextInput) window(g *fieldGeometry, caret int, width, blank, from, quantum core.Unit) (scroll, usable core.Unit, left, right bool) {
	if g == nil || width <= 0 {
		return 0, width, false, false
	}
	scroll = from
	ahead, back := t.showAheadUnits(blank)
	mark := t.markWidth()
	for pass := 0; pass < 2; pass++ {
		usable = width
		if left {
			usable -= mark
		}
		if right {
			usable -= mark
		}
		if usable <= 0 {
			usable = width
		}
		if quantum > 0 {
			usable -= usable % quantum
			if usable <= 0 {
				usable = quantum
			}
		}
		// One direction or the other, never both: a step that pushed the run
		// along must not be pulled back by the margin behind the caret, which
		// in a field only a few characters wide is the margin it just left.
		lo, hi := g.caretBox(caret, blank)
		switch {
		case hi+ahead > scroll+usable:
			scroll = hi + ahead - usable
		case lo-ahead < scroll:
			scroll = lo - back
		}
		// The caret past the end of the text stands beyond the run's own width,
		// and it is part of what has to fit.
		extent := g.total
		if hi > extent {
			extent = hi
		}
		if scroll > extent-usable {
			scroll = extent - usable
		}
		if scroll < 0 {
			scroll = 0
		}
		left, right = scroll > 0, scroll+usable < g.total
	}
	return scroll, usable, left, right
}
