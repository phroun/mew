package trinkets

import (
	"github.com/phroun/khatool"
	"sort"

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

	// head is empty room kept at the run's LEFT end, and the run starts after
	// it. A line whose reading ENDS on the left -- a right-to-left word at the
	// end of the text -- has its final caret position out there, past the
	// leftmost letter, and with nothing reserved it would stand off the edge of
	// the field and not be drawn at all. The room is left empty: it is not a
	// place in the text, so it shows as the field's own ground.
	head   core.Unit
	headPx int

	// marks are the direction-marker slots, present only when the run was laid
	// out with them (see SetShowBidiControls).
	marks []fieldMark

	// pieces are the stretches of the shaped run that are drawn apart from one
	// another, present when markers were pushed in between them. Each carries
	// how far right of where the shaper put it the piece now sits.
	//
	// A shaper orders and joins a whole line at once, and a marker is not part
	// of that line. So the line is shaped once, whole, and then TRANSLATED
	// piece by piece: every glyph is the raster the shaper made, in the place
	// the shaper's ordering put it, moved along to leave room. Re-shaping each
	// fragment as its own string instead would re-resolve the neutrals inside
	// it, which is how a bracket in a right-to-left region comes to face the
	// wrong way when it is measured alone.
	pieces []fieldPiece
}

// fieldPiece is one stretch of the shaped run in its final place.
type fieldPiece struct {
	lo, hi     core.Unit
	loPx, hiPx int
	shift      core.Unit
	shiftPx    int
}

// fieldMark is one stretch of the run that is not the author's own glyph:
// where it sits, and what stands there. Two things qualify -- a direction
// marker the field added to show where the reading turns, and a substitute
// standing in for a character that must not be drawn as itself.
//
// They share a colour because they are the same kind of thing to a reader:
// the field telling them about the text rather than showing it.
type fieldMark struct {
	x     core.Unit
	w     core.Unit
	xPx   int
	wPx   int
	text  string
	inRun bool // the run already carries this glyph; recolour it rather than draw it
}

// shiftAtPx is how far the piece holding final device-pixel position x was
// moved from where the shaper put it -- which is what a redraw clipped out of
// the whole run has to be stamped at to land back on the same glyphs.
//
// A run nothing was pushed into is one piece moved by the room reserved at its
// left end, which is a shift like any other.
func (g *fieldGeometry) shiftAtPx(x int) int {
	if g == nil {
		return 0
	}
	for i := range g.pieces {
		if x >= g.pieces[i].loPx && x < g.pieces[i].hiPx {
			return g.pieces[i].shiftPx
		}
	}
	return g.headPx
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

// readsToTheLeft reports whether the run's reading ENDS at its left edge,
// which is what puts the last caret position out past the leftmost letter.
func readsToTheLeft(rtl []bool) bool {
	return len(rtl) > 0 && rtl[len(rtl)-1]
}

// secondaryCaretAt is the character the caret's OTHER reading belongs to, and
// which side of it that reading sits on.
//
// At a direction change one insertion point stands in two places on the line:
// what is typed next lands at whichever end matches its own direction, and a
// single caret would name one of them and hide the other. The primary follows
// the character the caret precedes; this one rests one blank past the character
// BEFORE it, in that character's own direction -- the same "one past, in its own
// direction" form the end of the line takes.
//
// A direction control the author typed marks the turn itself, so a boundary
// either side of one has no second reading to show.
func (g *fieldGeometry) secondaryCaretAt(runes []rune, p int) (q int, leftOf, ok bool) {
	if g == nil || p <= 0 || p >= len(g.lo) || p >= len(runes) {
		return 0, false, false
	}
	if g.rtl[p] == g.rtl[p-1] {
		return 0, false, false
	}
	if khatool.IsDirectionControl(runes[p]) || khatool.IsDirectionControl(runes[p-1]) {
		return 0, false, false
	}
	// A mark has no box of its own, so step back to the base carrying it: the
	// caret goes past the CLUSTER, not past a sliver inside it.
	q = p - 1
	for q > 0 && g.hi[q] <= g.lo[q] {
		q--
	}
	return q, g.rtl[q], true
}

// outermostIn is the caret position sitting furthest toward one end of the span
// [lo, hi), and whether the span holds one at all.
//
// Fully inside, both edges: a position whose box is half off the end of the room
// is one the field would have to SCROLL to show, and the whole point of asking
// is to find how far the caret can go without scrolling anything.
func (g *fieldGeometry) outermostIn(lo, hi, blank core.Unit, towardLeft bool) (int, bool) {
	if g == nil {
		return 0, false
	}
	best, found := 0, false
	var bestX core.Unit
	for p := 0; p <= len(g.lo); p++ {
		a, b := g.caretBox(p, blank)
		if a < lo || b > hi {
			continue
		}
		x := a
		if !towardLeft {
			x = b
		}
		if !found || (towardLeft && x < bestX) || (!towardLeft && x > bestX) {
			best, bestX, found = p, x, true
		}
	}
	return best, found
}

// nextVisual is the caret position one step further along the LINE from p --
// the nearest box beyond p's own, left or right.
//
// Visual, not logical. A step to the left is a step to the left on a line that
// turns over too, where the character to the left of a Hebrew letter is the one
// AFTER it in the text.
func (g *fieldGeometry) nextVisual(p int, blank core.Unit, towardLeft bool) (int, bool) {
	if g == nil {
		return 0, false
	}
	fromLo, fromHi := g.caretBox(p, blank)
	best, found := 0, false
	var bestX core.Unit
	for q := 0; q <= len(g.lo); q++ {
		if q == p {
			continue
		}
		a, b := g.caretBox(q, blank)
		if towardLeft {
			if a >= fromLo {
				continue
			}
			if !found || a > bestX {
				best, bestX, found = q, a, true
			}
			continue
		}
		if b <= fromHi {
			continue
		}
		if !found || b < bestX {
			best, bestX, found = q, b, true
		}
	}
	return best, found
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
		return t.shapedGeometry(runes, font, marked, ppu)
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
func (t *TextInput) shapedGeometry(runes []rune, font *core.Font, marked bool, ppu float64) *fieldGeometry {
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
	if l.RTLAt(span[len(runes)-1][0]) {
		g.head = t.blankWidth()
		if ppu > 0 {
			if bl := e.ShapeRun(font, " "); bl != nil && len(bl.Lines) > 0 {
				g.headPx = bl.Lines[0].AdvancePx(ppu)
			}
		}
		g.total += g.head
	}
	// Everything below is laid out in the SHAPER's own coordinates, and the
	// head is applied at the end -- as a shift, which is what it is. The run is
	// stamped at that shift too, so the boxes and the glyphs move together.
	if substituted {
		g.draw = string(shaped)
	}
	if ppu > 0 {
		g.loPx, g.hiPx, g.havePx = make([]int, len(runes)), make([]int, len(runes)), true
	}
	for i := range runes {
		// The box of the FIRST shaped rune the logical one became, widened to
		// take in the rest: a substitute is several characters of notation
		// standing for one, and the field treats it as one.
		a, b, ok := l.BoxOf(span[i][0])
		for j := span[i][0] + 1; ok && j < span[i][1]; j++ {
			if c, d, ok2 := l.BoxOf(j); ok2 {
				if c < a {
					a = c
				}
				if d > b {
					b = d
				}
			}
		}
		g.rtl[i] = l.RTLAt(span[i][0])
		g.lo[i], g.hi[i] = a, b
		if g.havePx {
			c, d, _ := l.BoxOfPx(span[i][0], ppu)
			for j := span[i][0] + 1; j < span[i][1]; j++ {
				if e, f, ok2 := l.BoxOfPx(j, ppu); ok2 {
					if e < c {
						c = e
					}
					if f > d {
						d = f
					}
				}
			}
			g.loPx[i], g.hiPx[i] = c, d
		}
		if substituted && span[i][1] > span[i][0]+1 {
			m := fieldMark{x: a, w: b - a, text: string(shaped[span[i][0]:span[i][1]]), inRun: true}
			if g.havePx {
				m.xPx, m.wPx = g.loPx[i], g.hiPx[i]-g.loPx[i]
			}
			g.marks = append(g.marks, m)
		}
	}
	if marked {
		t.markShapedRun(g, e, font, l, span, ppu)
		return g
	}
	// Nothing was pushed in between, so the whole run moves as one.
	if g.head != 0 {
		for i := range g.lo {
			g.lo[i], g.hi[i] = g.lo[i]+g.head, g.hi[i]+g.head
			if g.havePx {
				g.loPx[i], g.hiPx[i] = g.loPx[i]+g.headPx, g.hiPx[i]+g.headPx
			}
		}
		for i := range g.marks {
			g.marks[i].x += g.head
			g.marks[i].xPx += g.headPx
		}
	}
	return g
}

// markShapedRun spreads the shaped line apart to make room for the direction
// markers, and records where they go.
//
// The line is already laid out; what this adds is the notation. A FRAGMENT is a
// maximal stretch of one direction, and every fragment gets two marks: the
// arrow it reads AWAY from at its reading start, and a bar at its reading end.
// So a right-to-left fragment carries "<" on its right and "|" on its left, and
// a left-to-right one the mirror of that.
//
// One fragment is left bare -- the first one in READING order, when it reads
// the way the line as a whole does. The line's own direction already says which
// way that piece is read, and an arrow at the very place a reader starts would
// be telling them what they can see.
//
// Nothing is re-shaped. Each fragment keeps the glyphs and the order the shaper
// gave the whole line and is simply moved along, so the marks cost room and
// change nothing else about the picture.
func (t *TextInput) markShapedRun(g *fieldGeometry, e *text.Engine, font *core.Font,
	l *text.Line, span [][2]int, ppu float64) {
	frags := shapedFragments(l, ppu)
	if len(frags) == 0 {
		return
	}
	// The first fragment in reading order is the one holding the line's first
	// rune, and the line's base direction is that fragment's: everything ahead
	// of the first strong character is neutral, and neutrals take the base.
	bare, last := 0, 0
	for i := range frags {
		if frags[i].start < frags[bare].start {
			bare = i
		}
		if frags[i].start > frags[last].start {
			last = i
		}
	}
	// The line's own direction is that first fragment's: everything ahead of
	// the first strong character is neutral, and neutrals take the base.
	baseRTL := frags[bare].rtl
	// The closing bar says the reading stops here and the eye jumps. At the
	// LINE's own end there is nowhere to jump to, so a last fragment reading
	// the way the line does closes without one.
	closes := func(i int) bool { return i != bare && (i != last || frags[i].rtl != baseRTL) }

	width := func(glyph rune) (core.Unit, int) {
		sp := e.ShapeRun(font, string(glyph))
		if sp == nil || len(sp.Lines) == 0 {
			return 0, 0
		}
		w := sp.Lines[0].Width
		if ppu > 0 {
			return w, sp.Lines[0].AdvancePx(ppu)
		}
		return w, 0
	}

	// Walk the fragments left to right, laying each one down after whatever
	// mark stands at its left edge and before whatever stands at its right.
	shift := make([]core.Unit, len(frags))
	shiftPx := make([]int, len(frags))
	x, xPx := g.head, g.headPx
	put := func(glyph rune) {
		w, wPx := width(glyph)
		g.marks = append(g.marks, fieldMark{x: x, w: w, xPx: xPx, wPx: wPx, text: string(glyph)})
		x, xPx = x+w, xPx+wPx
	}
	for i := range frags {
		f := &frags[i]
		// A fragment reads away from one edge and stops at the other: a
		// left-to-right one begins at its left, a right-to-left one at its
		// right. So the arrow and the bar swap ends with the fragment.
		if f.rtl {
			if closes(i) {
				put(markerEndGlyph)
			}
		} else if i != bare {
			put(markerLTRGlyph)
		}
		shift[i], shiftPx[i] = x-f.lo, xPx-f.loPx
		g.pieces = append(g.pieces, fieldPiece{
			lo: x, hi: x + (f.hi - f.lo), shift: shift[i],
			loPx: xPx, hiPx: xPx + (f.hiPx - f.loPx), shiftPx: shiftPx[i],
		})
		x, xPx = x+(f.hi-f.lo), xPx+(f.hiPx-f.loPx)
		if f.rtl {
			if i != bare {
				put(markerRTLGlyph)
			}
		} else if closes(i) {
			put(markerEndGlyph)
		}
	}
	g.total = x

	// Everything already measured moves with the piece it sits in.
	for i := range g.lo {
		for j := range frags {
			if span[i][0] >= frags[j].start && span[i][0] < frags[j].end {
				g.lo[i], g.hi[i] = g.lo[i]+shift[j], g.hi[i]+shift[j]
				if g.havePx {
					g.loPx[i], g.hiPx[i] = g.loPx[i]+shiftPx[j], g.hiPx[i]+shiftPx[j]
				}
				break
			}
		}
	}
	for i := range g.marks {
		if !g.marks[i].inRun {
			continue
		}
		s, sPx := core.Unit(0), 0
		for j := range frags {
			if g.marks[i].x >= frags[j].lo && g.marks[i].x < frags[j].hi {
				s, sPx = shift[j], shiftPx[j]
				break
			}
		}
		g.marks[i].x, g.marks[i].xPx = g.marks[i].x+s, g.marks[i].xPx+sPx
	}
}

// shapedFragment is a maximal stretch of one direction in a shaped line: the
// logical runes it covers, and where it sits.
type shapedFragment struct {
	start, end int
	rtl        bool
	lo, hi     core.Unit
	loPx, hiPx int
}

// edgesPx is a run's left and right edges in device pixels, taken from the run's
// OWN pen so the span covers that run, all of it, and nothing beside it.
func edgesPx(r *text.Run, ppu float64) (lo, hi int) {
	return r.OriginPx(ppu), r.EndPx(ppu)
}

// shapedFragments merges the shaper's runs into the stretches a reader sees as
// one piece. A shaper splits a run wherever the FACE changes as well as
// wherever the direction does, and a change of face is not a change of reading.
func shapedFragments(l *text.Line, ppu float64) []shapedFragment {
	var out []shapedFragment
	for i := range l.Runs {
		r := &l.Runs[i]
		if n := len(out); n > 0 && out[n-1].rtl == r.RTL && out[n-1].hi == r.X {
			f := &out[n-1]
			f.hi = r.X + r.Width
			if _, hiPx := edgesPx(r, ppu); hiPx > f.hiPx {
				f.hiPx = hiPx
			}
			if r.Runes.Start < f.start {
				f.start = r.Runes.Start
			}
			if r.Runes.End > f.end {
				f.end = r.Runes.End
			}
			continue
		}
		f := shapedFragment{
			start: r.Runes.Start, end: r.Runes.End, rtl: r.RTL,
			lo: r.X, hi: r.X + r.Width,
		}
		f.loPx, f.hiPx = edgesPx(r, ppu)
		out = append(out, f)
	}
	return out
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
		// No bar at the line's own reading end: the field reserves its own room
		// for the caret's last position (see head), and past that room is the
		// field's own ground, which says the line ended more plainly than a
		// mark could.
		lay = khatool.OrderMarkedWith(runes, baseRTL, core.CellRides, khatool.Marks{})
	} else {
		lay = khatool.Order(runes, baseRTL, core.CellRides)
	}
	g := &fieldGeometry{
		lo:  make([]core.Unit, len(runes)),
		hi:  make([]core.Unit, len(runes)),
		rtl: make([]bool, len(runes)),
	}
	if lay != nil && readsToTheLeft(lay.RTL) {
		g.head = cw
	}
	x := g.head
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
	x, at := g.head, core.Unit(-1)
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

// The direction markers: which way a fragment of the line begins reading, and
// where its reading stops.
const (
	markerLTRGlyph = '>'
	markerRTLGlyph = '<'
	markerEndGlyph = '|'
)

// markerGlyph is what a marker slot shows: the direction a fragment begins
// reading in, or the end of one.
func markerGlyph(slot int) rune {
	switch slot {
	case khatool.MarkerLTR:
		return markerLTRGlyph
	case khatool.MarkerRTL:
		return markerRTLGlyph
	}
	return markerEndGlyph
}

// controlGlyph is what an explicit direction control shows under the markers:
// the same two arrows, because it means the same thing the field's own marker
// would have meant there.
func controlGlyph(rtl bool) rune {
	if rtl {
		return markerRTLGlyph
	}
	return markerLTRGlyph
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

// markWidth is the room one more-marker takes: one character cell, whatever
// face the field is set in.
//
// Not the glyph's own width. A proportional face's triangle is most of an em
// wide, which is a large bite out of a short field and a different bite in
// every face -- and the arrow is chrome about the field's edge, not a character
// of the text, so it takes the field's own unit rather than the font's.
func (t *TextInput) markWidth() core.Unit {
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

// fillStretch is a range of logical runes the selection paints in one style.
type fillStretch struct {
	from, to int
	giveUp   bool
}

// fillStretches splits a logical range into the stretches that wear one style.
// Where giveUp says a fill cannot be placed, that stretch wears what rides a
// glyph instead, and the stretches on either side of it keep the ordinary bar.
// A nil giveUp is one stretch wearing the bar, which is every field on a
// terminal that places what it is sent where it was put.
func fillStretches(giveUp []bool, from, to int) []fillStretch {
	if from < 0 || to <= from {
		return nil
	}
	at := func(i int) bool { return i < len(giveUp) && giveUp[i] }
	var out []fillStretch
	for i := from; i < to; {
		j := i + 1
		for j < to && at(j) == at(i) {
			j++
		}
		out = append(out, fillStretch{from: i, to: j, giveUp: at(i)})
		i = j
	}
	return out
}

// visualOrder is the logical runes in the order they are drawn, left to right.
// It is what tells a caller reasoning about the emitted stream which rune comes
// AFTER which -- a question the rune order itself answers wrongly the moment a
// run turns over.
//
// Runes sharing a box (a mark riding its base) keep their logical order within
// it, so a cluster stays together.
func (g *fieldGeometry) visualOrder() []int {
	if g == nil || len(g.lo) == 0 {
		return nil
	}
	order := make([]int, len(g.lo))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return g.lo[order[a]] < g.lo[order[b]]
	})
	return order
}
