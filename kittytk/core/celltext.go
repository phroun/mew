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
	if text == "" || dir == DirInherit || HasTextMeasurer() {
		return text
	}
	runes := []rune(text)
	lay := khatool.Order(runes, dir == DirRTL, ridesCell)
	if lay == nil {
		return text // visual order is logical order, and nothing to shape
	}
	out := make([]rune, 0, len(lay.Perm))
	for _, i := range lay.Perm {
		r := runes[i]
		if lay.Glyph != nil {
			g := lay.Glyph[i]
			if g == khatool.LigatureAbsorbed {
				continue // the pair before it took one cell for both
			}
			r = g
		}
		if lay.RTL[i] {
			r = khatool.Mirror(r)
		}
		out = append(out, r)
	}
	return string(out)
}

// ridesCell is the cluster rule for a cell target, and it asks the same
// question the emitter answers when it advances: a rune the target gives no
// cell to is drawn INTO the cell before it, so it travels with that cell when
// a run turns over. See CellWidth.
func ridesCell(runes []rune, i int) bool {
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
