package editor

import (
	"strings"

	"github.com/phroun/mew/internal/bidi"
	"github.com/phroun/mew/internal/textwidth"
	"github.com/phroun/mew/internal/viewport"
	"github.com/phroun/pawscript"
)

// Converting between rune positions and screen columns: tab stops, wide and
// zero-width runes, marked (substituted) runes, and bidi layout, including
// where the caret is drawn on a right-to-left line.

// tabSize returns the effective tab size for a viewport. Per-viewport settings
// govern the viewport, so cursor math must use this — the viewport's own
// ViewState.TabSize — rather than the global e.Config.TabSize default.
func (e *Editor) tabSize(w *viewport.Viewport) int {
	if w != nil && w.ViewState.TabSize > 0 {
		return w.ViewState.TabSize
	}
	if e.Config.TabSize > 0 {
		return e.Config.TabSize
	}
	return 4
}

// runeToVisualColumn converts a rune position to a visual column position.
// This accounts for tabs (variable width) and control characters (2 chars wide).
// Translated from TypeScript CoordinateUtils.documentRuneToColumn
//
// lineMarkSet is the set of positions on the viewport's caret line that get a
// showMarks "*" cell, mirroring what prepareLineForDisplay draws: one before the
// cell of each marked rune, plus — only when invisibles are shown, so the
// terminator slot exists to host it — a mark sitting at end of line. It returns
// nil when showMarks is off or the line has no drawable marks. Every visual-
// column walk (plain and bidi, forward and inverse) consumes this one set, so a
// mark inserts its cell in the same place on all of them; there is no flat
// offset (a mark before a tab steals a column the tab would otherwise fill, so
// the shift is not additive). Marks are keyed on the caret's line, the only line
// these coordinate helpers are asked about.
func (e *Editor) lineMarkSet(w *viewport.Viewport, runes []rune) map[int]bool {
	if w == nil || !w.ViewState.MarksVisible() || w.Buffer == nil {
		return nil
	}
	// Mark cells are suppressed on a substituted caret line — the doc positions
	// the marks name have no cells of their own there. Mirrors the renderer's
	// lineMarkSet gate.
	if spans, dw := e.lineDisplaySpans(w, w.CursorPos().Line); len(spans) > 0 || dw {
		return nil
	}
	raw := w.Buffer.MarksOnLine(w.CursorPos().Line, w.ViewState.MarksShowInternal())
	if len(raw) == 0 {
		return nil
	}
	// A mark at end of line has no rune to precede. On a plain line the renderer
	// just appends a trailing "*" cell, so it always shows; on a bidi line
	// "after the content" is ambiguous under reordering, so there it still rides
	// the terminator slot and only shows when invisibles are on.
	eolDrawn := w.ViewState.ShowInvisibles || e.layoutFor(w, runes) == nil
	m := make(map[int]bool, len(raw))
	for _, p := range raw {
		if p < 0 || p > len(runes) {
			continue
		}
		if p == len(runes) && !eolDrawn {
			continue
		}
		m[p] = true
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// markedLine reports the plain (non-bidi) showMarks case, where the visual walk
// is a simple left-to-right scan: showMarks on, the caret line non-bidi, and it
// has drawable marks. It returns the line's runes and the mark-cell set; ok is
// false (callers take the base path) for bidi lines, marks off, or no marks.
// Bidi lines are exact too, but through bidiColumns / the bidi inverse walks,
// which consume lineMarkSet directly.
func (e *Editor) markedLine(w *viewport.Viewport, line string) (runes []rune, marked map[int]bool, ok bool) {
	if w == nil || !w.ViewState.MarksVisible() || w.Buffer == nil {
		return nil, nil, false
	}
	runes = []rune(line)
	if e.layoutFor(w, runes) != nil {
		return nil, nil, false
	}
	marked = e.lineMarkSet(w, runes)
	if marked == nil {
		return nil, nil, false
	}
	return runes, marked, true
}

// runeToVisualColumnMarked is the plain forward walk with showMarks cells: it
// emits a "*" cell before each marked rune, then the rune, resolving tab widths
// at the SHIFTED column exactly as prepareLineForDisplay paints them. It returns
// the visual column of rune runePos's own cell (past its leading "*"). It is the
// forward twin of visualColumnToRuneMarked and mirrors the renderer's slot loop,
// so a mark before a tab shrinks (or grows) that tab identically on both sides.
func (e *Editor) runeToVisualColumnMarked(runes []rune, marked map[int]bool, runePos, tabSize int) int {
	if runePos < 0 {
		runePos = 0
	}
	col := 0
	for i := 0; i < len(runes); i++ {
		if marked[i] { // "*" cell before rune i
			col++
		}
		if i == runePos {
			return col
		}
		col += e.runeWidthAt(runes, i, col, tabSize)
	}
	// runePos at/after end of line: include a trailing mark so the column agrees
	// with the inverse walk (visualColumnToRuneMarked).
	if marked[len(runes)] {
		col++
	}
	return col
}

// runeToVisualColumn is the display column of a rune, including the showMarks
// "*" cells so it stays in step with what the renderer draws.
func (e *Editor) runeToVisualColumn(w *viewport.Viewport, line string, runePos int, tabSize int) int {
	if runes, marked, ok := e.markedLine(w, line); ok {
		return e.runeToVisualColumnMarked(runes, marked, runePos, tabSize)
	}
	return e.runeToVisualColumnBase(w, line, runePos, tabSize)
}

func (e *Editor) runeToVisualColumnBase(w *viewport.Viewport, line string, runePos int, tabSize int) int {
	runes := []rune(line)

	// Bidirectional line: the rune's visual column is where its cell is
	// painted in visual order, which can be anywhere on the line. bidiColumns
	// inserts the showMarks "*" cells in visual order, so cols/total are exact.
	if layout := e.layoutFor(w, runes); layout != nil {
		cols, total := e.bidiColumns(runes, layout, e.lineMarkSet(w, runes), tabSize)
		if runePos >= len(runes) {
			return total
		}
		if runePos < 0 {
			runePos = 0
		}
		return cols[runePos]
	}

	if runePos <= 0 {
		return 0
	}
	maxRune := runePos
	if maxRune > len(runes) {
		maxRune = len(runes)
	}

	column := 0
	for i := 0; i < maxRune; i++ {
		runeWidth := e.runeWidthAt(runes, i, column, tabSize)
		column += runeWidth
	}

	return column
}

// baseRTL reports whether the configured base direction is right-to-left.
func (e *Editor) baseRTL() bool { return e.Config.Direction == "rtl" }

// layoutFor computes a line's visual layout for a viewport: with the viewport's
// showBidi enabled it includes direction-marker slots (bidi.ComputeMarked),
// whose cell widths every visual-column computation must account for.
func (e *Editor) layoutFor(w *viewport.Viewport, runes []rune) *bidi.Layout {
	if w != nil && w.ViewState.ShowBidi {
		return bidi.ComputeMarked(runes, e.winRTL(w))
	}
	return bidi.Compute(runes, e.winRTL(w))
}

// slotWidth is the visual width of one layout slot: marker slots are one
// column; explicit direction controls are one column under a marked layout
// (they render as their own marker) and zero otherwise; everything else uses
// the ordinary rune width.
func (e *Editor) slotWidth(layout *bidi.Layout, runes []rune, entry, col, tabSize int) int {
	if entry < 0 {
		return 1
	}
	// The absorbed half of a lam-alef ligature shares the first half's cell.
	if layout != nil && layout.Glyph != nil && layout.Glyph[entry] == bidi.LigatureAbsorbed {
		return 0
	}
	r := runes[entry]
	if layout != nil && layout.Marked && bidi.IsDirectionControl(r) {
		return 1
	}
	// Base-aware, so an ill-formed mark measures the SPACING substitute painted
	// for it rather than the zero cells a well-formed mark takes. Without that
	// the caret walk would step over it as though it rode the preceding cell,
	// and the columns this feeds would disagree with the paint.
	return e.runeWidthAt(runes, entry, col, tabSize)
}

// winRTL is the EFFECTIVE direction for a viewport: its ViewState.Direction
// override when set (prompt viewports are pinned "ltr"), else the base option.
func (e *Editor) winRTL(w *viewport.Viewport) bool {
	if w != nil {
		switch w.ViewState.Direction {
		case "ltr":
			return false
		case "rtl":
			return true
		}
	}
	return e.baseRTL()
}

// caretVisualColumn is the column where the CARET is drawn for a logical
// position — biased by the direction of the character at the caret. In LTR
// context the caret sits on (the left edge of) the rune it precedes; in RTL
// context "before rune i" is visually at the rune's RIGHT edge, so the caret
// parks one cell to the right of the rune's cell. At end of line the boundary
// follows the last rune's direction (one cell LEFT of an RTL line's leftmost
// cell — possibly -1, which the right-alignment pad absorbs).
func (e *Editor) caretVisualColumn(w *viewport.Viewport, line string, runePos, tabSize int) int {
	// Non-bidi: the caret sits on the rune's own cell, so its column is exactly
	// the marks-inclusive rune column from the inline walk (which resolves tab
	// widths at the shifted column). Bidi is exact through caretVisualColumnBase,
	// whose cols/total include the "*" cells. Mirrors the renderer's wrapper.
	if runes, marked, ok := e.markedLine(w, line); ok {
		return e.runeToVisualColumnMarked(runes, marked, runePos, tabSize)
	}
	return e.caretVisualColumnBase(w, line, runePos, tabSize)
}

func (e *Editor) caretVisualColumnBase(w *viewport.Viewport, line string, runePos, tabSize int) int {
	runes := []rune(line)
	layout := e.layoutFor(w, runes)
	if layout == nil {
		return e.runeToVisualColumnBase(w, line, runePos, tabSize)
	}
	cols, total := e.bidiColumns(runes, layout, e.lineMarkSet(w, runes), tabSize)
	rtlBase := e.winRTL(w)

	// A zero-width combining mark shares its base character's cell, so the
	// END-OF-LINE boundary follows the side the BASE character dictates: step
	// back over zero-width runes to the cluster base. (Marked direction
	// controls are one column wide and stop the walk.)
	clusterBase := func(i int) int {
		for i > 0 && e.slotWidth(layout, runes, i, cols[i], tabSize) == 0 {
			i--
		}
		return i
	}

	// A caret PARTWAY THROUGH a cluster — between a base and its combining
	// mark, or between two marks — is placed as though it followed the whole
	// cluster. Those positions have no cell of their own (the marks ride the
	// base), so the caret has to borrow one, and the cluster's trailing edge is
	// the honest choice: the caret is logically past the base, and parking it at
	// the LEADING edge would draw it on the far side of a character it has
	// already gone by. In an RTL run that is the visible difference between the
	// caret sitting before the letter and after it — which is why this only ever
	// showed up in right-to-left text: the left-to-right walk is a prefix sum,
	// and a prefix sum already lands past the cluster.
	if runePos < 0 {
		runePos = 0
	}
	startPos := runePos
	for runePos < len(runes) && e.slotWidth(layout, runes, runePos, cols[runePos], tabSize) == 0 {
		runePos++
	}

	// The caret sits where the next character OF THE CARET'S OWN DIRECTION
	// (the direction the rtl command / modebar logo reports — the direction
	// of the rune at the caret) would land. This is independent of showBidi:
	// the markers only add cells (shifting cols), not change the rule.
	if runePos >= len(runes) {
		last := clusterBase(len(runes) - 1)
		if layout.RTL[last] {
			// One past the last character, leftward (its direction).
			return cols[last] - 1
		}
		if rtlBase {
			// Right-anchored line ending in LTR text: one cell right of the
			// last character — NOT the line's right edge (that is the gutter).
			return cols[last] + e.slotWidth(layout, runes, last, cols[last], tabSize)
		}
		return total
	}
	// When the caret advanced ACROSS a cluster (it skipped that cluster's marks)
	// whose base is RTL, it followed the cluster to its reading-trailing edge,
	// which in an RTL run is one cell LEFT of the base. Return that directly. For
	// an RTL rune following the cluster this equals its column, but where a
	// left-to-right run follows, that run's column is bidi-teleported to the far
	// end of the line — the caret must stay by the cluster, not jump there.
	if runePos > startPos {
		if base := clusterBase(startPos); layout.RTL[base] {
			return cols[base] - 1
		}
	}
	// The caret covers the cell of the rune it precedes — in either base
	// direction, for both LTR and RTL runes. A block cursor sits ON that
	// character, and an RTL rune's cell is painted at cols[runePos], so a
	// caret inside an RTL fragment stays on the rune rather than parking one
	// cell to its right.
	return cols[runePos]
}

// leftmostCaret returns the smallest visual column any caret position on the
// line can occupy, and the position (rune index; len for end-of-line) that
// occupies it. Under direction=rtl this is the line's furthest-back READING
// position — vw minus the returned column is the largest reading column the
// line reaches. On a plain left-to-right layout that caret is position 0 at
// column 0; on bidi lines it can sit anywhere, including one cell LEFT of the
// content when the line ends in RTL text (the EOL boundary's own cell).
func (e *Editor) leftmostCaret(w *viewport.Viewport, line string, tabSize int) (col, pos int) {
	runes := []rune(line)
	layout := e.layoutFor(w, runes)
	col, pos = e.caretVisualColumn(w, line, len(runes), tabSize), len(runes)
	if layout == nil {
		// Monotonic columns: position 0 is the leftmost caret.
		if c := e.caretVisualColumn(w, line, 0, tabSize); c < col {
			return c, 0
		}
		return col, pos
	}
	cols, _ := e.bidiColumns(runes, layout, e.lineMarkSet(w, runes), tabSize)
	for i, c := range cols {
		// A zero-width slot (combining mark, absorbed ligature half) shares
		// its base character's cell; the caret rests there via the base.
		if e.slotWidth(layout, runes, i, c, tabSize) == 0 {
			continue
		}
		if c < col {
			col, pos = c, i
		}
	}
	return col, pos
}

// lineVisualWidth is the total visual width of a line (tab widths resolved
// in visual order, matching the renderer).
func (e *Editor) lineVisualWidth(w *viewport.Viewport, line string, tabSize int) int {
	runes := []rune(line)
	marked := e.lineMarkSet(w, runes)
	layout := e.layoutFor(w, runes)
	if layout == nil {
		vw := 0
		for i := range runes {
			if marked[i] {
				vw++
			}
			vw += e.runeWidthAt(runes, i, vw, tabSize)
		}
		if marked[len(runes)] {
			vw++
		}
		return vw
	}
	_, total := e.bidiColumns(runes, layout, marked, tabSize)
	return total
}

// idealColumn returns the direction-appropriate "sticky" column used to hold
// the caret at a stable SCREEN position across vertical moves. In LTR it is
// the left-based visual column (the view is left-anchored, so screen X tracks
// the visual column directly). In RTL the view is right-anchored, so screen X
// tracks the READING column — the caret's distance back from the line's
// reading start (its rightmost visual cell) — which stays put on screen as
// lines of different widths right-align beneath it.
func (e *Editor) idealColumn(w *viewport.Viewport, line string, runePos, tabSize int) int {
	if e.winRTL(w) {
		return e.lineVisualWidth(w, line, tabSize) - e.caretVisualColumn(w, line, runePos, tabSize)
	}
	return e.runeToVisualColumn(w, line, runePos, tabSize)
}

// bidiColumns walks a line's visual order accumulating cell columns: cols
// maps each LOGICAL rune index to the visual column its cell starts at, and
// total is the line's full visual width. Tab widths resolve at their VISUAL
// column, like the renderer paints them. When marked is non-nil a showMarks "*"
// cell is inserted in visual order just before each marked rune's cell (and a
// trailing one for an end-of-line mark), so cols/total are exact for bidi lines
// with marks — mirroring the "*" insertion in prepareLineForDisplay.
func (e *Editor) bidiColumns(runes []rune, layout *bidi.Layout, marked map[int]bool, tabSize int) (cols []int, total int) {
	cols = make([]int, len(runes))
	col := 0
	for _, li := range layout.Perm {
		if li >= 0 && marked[li] {
			col++
		}
		if li >= 0 {
			cols[li] = col
		}
		col += e.slotWidth(layout, runes, li, col, tabSize)
	}
	if marked[len(runes)] {
		col++
	}
	return cols, col
}

// visualColumnToRune converts a visual column position to a rune position.
// This is the inverse of runeToVisualColumn.
// Translated from TypeScript CoordinateUtils.columnToDocumentRune
func (e *Editor) visualColumnToRune(w *viewport.Viewport, line string, targetColumn int, tabSize int) int {
	// showMarks (non-bidi): account for the inserted "*" cells via the marked
	// walk, so a click maps to the right rune.
	if runes, marked, ok := e.markedLine(w, line); ok {
		return e.visualColumnToRuneMarked(runes, marked, targetColumn, tabSize).Rune
	}

	runes := []rune(line)

	// Bidirectional line: find the visual cell covering the target column and
	// return its LOGICAL rune index (past the end returns len). A showMarks "*"
	// cell precedes each marked rune in visual order; a click on it selects that
	// rune, keeping this the exact inverse of bidiColumns.
	if layout := e.layoutFor(w, runes); layout != nil {
		marked := e.lineMarkSet(w, runes)
		col := 0
		for v, li := range layout.Perm {
			if li >= 0 && marked[li] {
				if targetColumn < col+1 {
					return li
				}
				col++
			}
			cw := e.slotWidth(layout, runes, li, col, tabSize)
			if cw > 0 && targetColumn < col+cw {
				if li >= 0 {
					return li
				}
				// A begin-marker cell maps to its fragment's leading rune:
				// the next real slot for an entering-LTR marker, the
				// previous for entering-RTL. An end marker ("|") maps to the
				// position just past the fragment's reading-last rune: one
				// past the prior real slot for an LTR fragment (the "|"
				// follows the content), one past the next real slot for an
				// RTL fragment (the "|" precedes the reversed content, whose
				// leftmost cell is the fragment's logically last rune).
				switch li {
				case bidi.MarkerLTR:
					for k := v + 1; k < len(layout.Perm); k++ {
						if layout.Perm[k] >= 0 {
							return layout.Perm[k]
						}
					}
				case bidi.MarkerEnd:
					for k := v - 1; k >= 0; k-- {
						if layout.Perm[k] >= 0 && !layout.RTL[layout.Perm[k]] {
							return layout.Perm[k] + 1
						}
						if layout.Perm[k] >= 0 {
							break
						}
					}
					for k := v + 1; k < len(layout.Perm); k++ {
						if layout.Perm[k] >= 0 {
							return layout.Perm[k] + 1
						}
					}
				default:
					for k := v - 1; k >= 0; k-- {
						if layout.Perm[k] >= 0 {
							return layout.Perm[k]
						}
					}
				}
				return len(runes)
			}
			col += cw
		}
		return len(runes)
	}

	if targetColumn <= 0 {
		return 0
	}

	column := 0

	for i := 0; i < len(runes); i++ {
		runeWidth := e.runeWidthAt(runes, i, column, tabSize)

		// If adding this rune would go past the target, return current position
		if column+runeWidth > targetColumn {
			return i
		}

		column += runeWidth

		// If we've reached or passed the target, return next position
		if column >= targetColumn {
			return i + 1
		}
	}

	return len(runes)
}

// visualColumnToRuneResult holds the result of visualColumnToRuneWithActual.
type visualColumnToRuneResult struct {
	Rune         int // The rune position
	ActualColumn int // The actual visual column at that rune position
}

// visualColumnToRuneWithActual converts a visual column to a rune position,
// also returning the actual visual column at that position.
// This is useful for detecting when the target falls within a wide character like a tab.
// visualColumnToRuneWithActual maps a display column back to a rune. With
// showMarks on (and a non-bidi line with marks), it walks the visual cells
// including the inserted "*" cells so a click on/after a mark lands on the right
// rune and the reported column is in the same marks-inclusive space as the
// forward math. Bidi lines fall back to the base mapping (marks are rare there).
func (e *Editor) visualColumnToRuneWithActual(w *viewport.Viewport, line string, targetColumn int, tabSize int) visualColumnToRuneResult {
	if runes, marked, ok := e.markedLine(w, line); ok {
		return e.visualColumnToRuneMarked(runes, marked, targetColumn, tabSize)
	}
	return e.visualColumnToRuneWithActualBase(w, line, targetColumn, tabSize)
}

// visualColumnToRuneMarked is the plain (non-bidi) inverse walk with showMarks
// cells: it emits, in order, the "*" cell at each marked rune position then the
// rune, and returns the rune whose cell (or leading "*") covers targetColumn.
// ActualColumn is reported marks-inclusive.
func (e *Editor) visualColumnToRuneMarked(runes []rune, marked map[int]bool, targetColumn, tabSize int) visualColumnToRuneResult {
	col := 0
	for i := 0; i <= len(runes); i++ {
		if marked[i] { // one "*" cell before rune i (or at end of line)
			if targetColumn <= col {
				return visualColumnToRuneResult{Rune: i, ActualColumn: col}
			}
			col++
		}
		if i == len(runes) {
			break
		}
		rw := e.runeWidthAt(runes, i, col, tabSize)
		if targetColumn < col+rw {
			return visualColumnToRuneResult{Rune: i, ActualColumn: col}
		}
		col += rw
	}
	return visualColumnToRuneResult{Rune: len(runes), ActualColumn: col}
}

func (e *Editor) visualColumnToRuneWithActualBase(w *viewport.Viewport, line string, targetColumn int, tabSize int) visualColumnToRuneResult {
	runes := []rune(line)

	// Bidirectional line: the cell covering the target column, by visual walk.
	// A showMarks "*" cell precedes each marked rune (a click on it selects that
	// rune), so this stays the exact inverse of bidiColumns.
	if layout := e.layoutFor(w, runes); layout != nil {
		marked := e.lineMarkSet(w, runes)
		col := 0
		for _, li := range layout.Perm {
			if li >= 0 && marked[li] {
				if targetColumn < col+1 {
					return visualColumnToRuneResult{Rune: li, ActualColumn: col}
				}
				col++
			}
			cw := e.slotWidth(layout, runes, li, col, tabSize)
			if li >= 0 && cw > 0 && targetColumn < col+cw {
				return visualColumnToRuneResult{Rune: li, ActualColumn: col}
			}
			col += cw
		}
		return visualColumnToRuneResult{Rune: len(runes), ActualColumn: col}
	}

	if targetColumn <= 0 {
		return visualColumnToRuneResult{Rune: 0, ActualColumn: 0}
	}

	column := 0

	for i := 0; i < len(runes); i++ {
		runeWidth := e.runeWidthAt(runes, i, column, tabSize)

		// If adding this rune would go past the target, return current position
		if column+runeWidth > targetColumn {
			return visualColumnToRuneResult{Rune: i, ActualColumn: column}
		}

		column += runeWidth

		// If we've reached or passed the target, return next position
		if column >= targetColumn {
			return visualColumnToRuneResult{Rune: i + 1, ActualColumn: column}
		}
	}

	return visualColumnToRuneResult{Rune: len(runes), ActualColumn: column}
}

// getRuneVisualWidth returns the visual width of a rune at a given visual
// column. Tabs have variable width depending on position, control chars are
// 2 wide (^X display); other runes measure by their terminal cell width —
// 0 for combining/zero-width characters, 2 for wide (CJK, emoji).
func (e *Editor) getRuneVisualWidth(r rune, currentColumn int, tabSize int) int {
	if r == '\t' {
		// Tab width to next tab stop
		return tabSize - (currentColumn % tabSize)
	} else if textwidth.IsControl(r) {
		// Control characters displayed as ^X / hex — C0, DEL, and the C1 set
		// U+0080..U+009F (see textwidth.IsControl).
		return len(runeHexSubstitute(r))
	}
	return textwidth.Rune(r)
}

// runeWidthAt measures runes[i] with its cluster base in hand, so an
// ill-formed combining mark (see textwidth.DefectiveMark) measures as the
// hex substitute the renderer paints for it rather than the zero cells a
// well-formed mark takes. Every caret/column walk goes through here, so the
// editor's math and the screen agree on where a defective mark's cells are.
func (e *Editor) runeWidthAt(runes []rune, i, currentColumn, tabSize int) int {
	r := runes[i]
	if textwidth.IsMark(r) {
		var base rune
		for j := i - 1; j >= 0; j-- {
			if !textwidth.IsMark(runes[j]) {
				base = runes[j]
				break
			}
		}
		if textwidth.DefectiveMark(base, r) {
			if textwidth.AnchorMark(base, r) {
				return 1 // dotted circle carries the mark in one cell
			}
			return len(runeHexSubstitute(r))
		}
	}
	return e.getRuneVisualWidth(r, currentColumn, tabSize)
}

// registerDirectionCommands registers rtl.
func (e *Editor) registerDirectionCommands(ps *pawscript.PawScript) {
	// rtl reports whether the caret currently sits inside a right-to-left
	// segment of its line (resolved under the configured base direction).
	ps.RegisterCommand("rtl", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
		return pawscript.BoolStatus(bidi.RTLAt([]rune(line), w.CursorPos().Rune, e.winRTL(w)))
	})
}
