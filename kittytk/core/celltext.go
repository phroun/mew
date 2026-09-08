package core

import "github.com/phroun/khatool"

// CellRun turns a run of text into the cells to stamp on a CELL target: its
// glyphs in the order they are drawn, left to right.
//
// A cell target draws one cell at a time and has no shaper of its own, so the
// two things a Hebrew or Arabic run needs are done before it is handed over --
// the runes put in visual order, and the Arabic ones exchanged for the
// presentation forms that stand in for joining. A bracket inside a
// right-to-left run becomes its mirror image for the same reason: nothing
// downstream will do it.
//
// dir is the direction the run is READ in, which is the trinket's own. A run
// still reorders inside a left-to-right one: a Hebrew word in an English menu
// is an RTL run in an LTR line, and that is the case this exists for.
// DirInherit means no direction was resolved, and the run is left alone.
//
// On a PIXEL target the run comes back unchanged -- there the text engine
// orders and shapes whole paragraphs itself, and doing it twice would undo it.
//
// Prepare LAST. What is measured, trimmed, sliced or hit-tested is the LOGICAL
// text; this is the step just before drawing. And the run that is drawn is the
// run to measure: a ligature takes one cell where its two letters took two, so
// measuring the text this was made from would count a cell that never appears.
func CellRun(text string, dir Direction) string {
	run, _ := CellRunMapped(text, dir)
	return string(run)
}

// CellRunMapped is CellRun with the cell each rune of the original landed in:
// where[i] is the index in run of the cell logical rune i is drawn in, or -1
// for a rune that took no cell of its own -- the second letter of a ligature,
// which the glyph before it is already showing.
//
// It is what a caller needs to style ONE character of a run it did not order
// itself, or to put a caret on one: an accelerator's underline, a selection, a
// cursor. Splitting the ORIGINAL text and preparing the pieces separately does
// not answer the same question -- the pieces of a run that reads right to left
// are not the pieces of the text that made it, and each piece would be ordered
// and shaped on its own, three runs that never join, in the wrong order.
func CellRunMapped(text string, dir Direction) (run []rune, where []int) {
	runes := []rune(text)
	identity := func() ([]rune, []int) {
		where := make([]int, len(runes))
		for i := range where {
			where[i] = i
		}
		return runes, where
	}
	if text == "" || dir == DirInherit || HasTextMeasurer() {
		return identity()
	}
	lay := khatool.Order(runes, dir == DirRTL, CellRides)
	if lay == nil {
		return identity() // visual order is logical order, and nothing to shape
	}
	// Giving up the marks that ride a right-to-left letter, for a display that
	// wants its fill back (see RtlCombining). One answer per rune, so the
	// ordering worked out above still indexes it.
	var drawn []rune
	if !RtlCombining() {
		drawn = khatool.FoldRidingMarks(runes, RtlMarkFolds(), func(r rune) bool {
			return CellWidth(r) == 0
		})
	}
	out := make([]rune, 0, len(lay.Perm))
	where = make([]int, len(runes))
	for i := range where {
		where[i] = -1
	}
	for _, i := range lay.Perm {
		r := runes[i]
		if drawn != nil {
			if drawn[i] == khatool.MarkDropped {
				continue // a mark this display has given up
			}
			r = drawn[i]
		}
		if lay.Glyph != nil {
			g := lay.Glyph[i]
			if g == khatool.LigatureAbsorbed {
				continue // the pair before it took one cell for both
			}
			if r == runes[i] {
				r = g // shaping applies where folding did not
			}
		}
		if lay.RTL[i] {
			r = khatool.Mirror(r)
		}
		where[i] = len(out)
		out = append(out, r)
	}
	return out, where
}

// CellRides is the cluster rule for a cell target, and it asks the same
// question the emitter answers when it advances: a rune the target gives no
// cell to is drawn INTO the cell before it, so it travels with that cell when
// a run turns over. See CellWidth.
//
// It is exported because a caller that orders a run ITSELF -- to know where
// each rune of it landed, which is what a caret and a selection need -- has to
// cluster by the same rule this package does, or its idea of the run and the
// run drawn are two different pictures.
func CellRides(runes []rune, i int) bool {
	if i < 0 || i >= len(runes) {
		return false
	}
	r := runes[i]
	if r == '\t' || khatool.IsControl(r) {
		return false
	}
	if CellWidth(r) != 0 {
		return false
	}
	return !khatool.DefectiveMark(khatool.PrevBase(runes, i), r)
}
