package purfecterm

// Standard visual-column protocol surface over the logical grid.
//
// PurfecTerm's buffer is LOGICAL — one cell per character, with the visual
// width carried as a per-cell attribute (CellWidth). That model is the right
// substrate for the graphics system (sprites, splits, fractional widths), but
// the DEFAULT protocol a hosted application sees must be the standard
// terminal contract every wcwidth-based program assumes: a wide glyph
// occupies two columns, printing it advances the cursor two columns, and
// CUP/CHA/CUF/CUB/HT address VISUAL columns. Flex mode (DECSET 2027) remains
// the opt-in enhanced contract with logical addressing and fractional widths.
//
// This file supplies the boundary translation, so the parser hunks stay
// one-liners: each entry point translates visual->logical when flex mode is
// OFF and degrades to the raw logical behavior when it is ON. Two invariants
// make the default contract real:
//
//  1. WIDTH TRUTH: standard-mode writes store the character's East Asian
//     Width (1.0 or 2.0) in CellWidth, so rendering, wrapping, and
//     translation all agree. (Renderers should treat CellWidth > 0 as the
//     cell's width regardless of the FlexWidth flag.)
//
//  2. COLUMN GEOMETRY: standard-mode overwrites preserve the row's column
//     layout exactly as a hardware terminal would — writing a narrow char
//     over a wide cell leaves an orphaned space for the vacated column;
//     writing a wide char swallows the following column (padding a broken
//     wide neighbor with a space). Cells to the right of an edit NEVER move,
//     which is what makes minimal span updates from applications safe.

// standardCharWidth returns the wcwidth-compatible column width (1.0 or 2.0)
// used for cells written while flex mode is off: East Asian Wide/Fullwidth
// are 2.0; ambiguous-width characters follow the 2029/2030 setting (narrow
// unless AmbiguousWidthWide — deterministic, unlike flex auto-matching);
// everything else is 1.0. Combining marks never reach here (they attach to
// the previous cell before width assignment).
func (b *Buffer) standardCharWidth(ch rune) float64 {
	w := GetEastAsianWidth(ch)
	if w < 0 { // ambiguous
		if b.ambiguousWidthMode == AmbiguousWidthWide {
			return 2.0
		}
		return 1.0
	}
	if w >= 1.5 {
		return 2.0
	}
	return 1.0
}

// cellWidthAt returns the effective width of the cell at (row, x): its
// CellWidth when set, else 1.0 (also 1.0 beyond the stored line — padding is
// always narrow). Caller holds the lock.
func (b *Buffer) cellWidthAt(row, x int) float64 {
	if row < 0 || row >= len(b.screen) || x < 0 || x >= len(b.screen[row]) {
		return 1.0
	}
	if w := b.screen[row][x].CellWidth; w > 0 {
		return w
	}
	return 1.0
}

// visualToLogicalLocked maps a visual column to the logical cell index whose
// span covers it. A column landing on the trailing half of a wide cell clamps
// onto that cell (the cursor can never sit inside a glyph, matching
// hardware terminals). Columns past the stored line extend 1:1 (padding
// cells are narrow). DEC double-width lines address per doubled cell, like
// real terminals, so no line-attribute scaling applies here. Caller holds
// the lock.
func (b *Buffer) visualToLogicalLocked(row int, visualCol int) int {
	if visualCol <= 0 {
		return 0
	}
	target := float64(visualCol)
	acc := 0.0
	x := 0
	if row >= 0 && row < len(b.screen) {
		line := b.screen[row]
		for ; x < len(line); x++ {
			w := b.cellWidthAt(row, x)
			if acc+w > target {
				return x // target falls within this cell's span (clamp)
			}
			acc += w
			if acc == target {
				return x + 1
			}
		}
	}
	// Past the stored line: extend with narrow padding columns.
	return x + int(target-acc)
}

// logicalToVisualLocked returns the visual column where the logical cell at
// (row, x) begins. Caller holds the lock.
func (b *Buffer) logicalToVisualLocked(row, x int) int {
	acc := 0.0
	stored := 0
	if row >= 0 && row < len(b.screen) {
		line := b.screen[row]
		for i := 0; i < x && i < len(line); i++ {
			acc += b.cellWidthAt(row, i)
			stored++
		}
	}
	// Beyond the stored line, each logical step is one narrow column.
	return int(acc) + (x - stored)
}

// VisualColToLogical / LogicalToVisualCol are the exported forms, for
// renderers, mouse mapping, and tests.
func (b *Buffer) VisualColToLogical(row, visualCol int) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.visualToLogicalLocked(row, visualCol)
}

func (b *Buffer) LogicalToVisualCol(row, x int) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.logicalToVisualLocked(row, x)
}

// SetCursorVisual is the parser's CUP/CHA entry point: x is a VISUAL column
// under the standard contract, a raw logical cell index under flex mode.
func (b *Buffer) SetCursorVisual(x, y int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.flexWidthMode {
		x = b.visualToLogicalLocked(y, x)
	}
	b.setCursorInternal(x, y)
}

// MoveCursorForwardVisual / MoveCursorBackwardVisual step the cursor by n
// COLUMNS under the standard contract (a wide cell is crossed in one logical
// step but costs two columns), by n cells under flex mode.
func (b *Buffer) MoveCursorForwardVisual(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.setHorizMoveDir(1, false)
	if b.flexWidthMode {
		b.cursorX += n
	} else {
		v := b.logicalToVisualLocked(b.cursorY, b.cursorX)
		b.cursorX = b.visualToLogicalLocked(b.cursorY, v+n)
	}
	if max := b.EffectiveCols() - 1; b.cursorX > max {
		b.cursorX = max
	}
	b.markDirty()
}

func (b *Buffer) MoveCursorBackwardVisual(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.setHorizMoveDir(-1, false)
	if b.flexWidthMode {
		b.cursorX -= n
	} else {
		v := b.logicalToVisualLocked(b.cursorY, b.cursorX)
		b.cursorX = b.visualToLogicalLocked(b.cursorY, v-n)
	}
	if b.cursorX < 0 {
		b.cursorX = 0
	}
	b.markDirty()
}

// TabVisual advances to the next 8-column tab stop measured in VISUAL columns
// under the standard contract (so tabs align across wide content), in logical
// cells under flex mode (the historical behavior).
func (b *Buffer) TabVisual() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.setHorizMoveDir(1, false)
	if b.flexWidthMode {
		b.cursorX = ((b.cursorX / 8) + 1) * 8
	} else {
		v := b.logicalToVisualLocked(b.cursorY, b.cursorX)
		b.cursorX = b.visualToLogicalLocked(b.cursorY, ((v/8)+1)*8)
	}
	if max := b.EffectiveCols() - 1; b.cursorX >= max {
		b.cursorX = max
	}
	b.markDirty()
}

// standardOverwriteFixup preserves the row's COLUMN GEOMETRY when the cell at
// (row, x) is about to be overwritten by a character of width newW, exactly
// as a hardware terminal does. Call it (standard mode only) in
// writeCharInternal after ensureLineLength and before the cell store:
//
//   - narrow over a wide cell: the vacated trailing column becomes an
//     orphaned space cell inserted after x;
//   - wide over narrow cells: the following cell is swallowed (removed); if
//     that neighbor was itself wide, its own trailing column survives as an
//     orphaned space instead of shifting the row.
//
// Everything to the right of the edit keeps its column, which is what makes
// span-granular updates from applications safe. Widths are 1.0/2.0 in
// standard mode; fractional flex cells encountered on a mixed row are
// consumed column-by-column on the same principle.
func (b *Buffer) standardOverwriteFixup(row, x int, newW float64) {
	if row < 0 || row >= len(b.screen) || x < 0 || x >= len(b.screen[row]) {
		return
	}
	line := b.screen[row]
	oldW := b.cellWidthAt(row, x)
	blank := line[x] // template keeps colors/attrs of the vacated area
	blank.Char = ' '
	blank.Combining = ""
	blank.CellWidth = 1.0

	switch {
	case newW < oldW:
		// Narrow over wide: insert orphan space(s) after x for the vacated
		// column(s).
		pads := int(oldW - newW)
		if pads < 1 {
			pads = 1
		}
		ins := make([]Cell, pads)
		for i := range ins {
			ins[i] = blank
		}
		line = append(line[:x+1], append(ins, line[x+1:]...)...)
		b.screen[row] = line

	case newW > oldW:
		// Wide over narrow: swallow following cells until the extra columns
		// are covered; a partially-swallowed wide neighbor leaves its own
		// orphan space.
		need := newW - oldW
		for need > 0 && x+1 < len(line) {
			nw := b.cellWidthAt(row, x+1)
			if nw <= need {
				line = append(line[:x+1], line[x+2:]...)
				need -= nw
			} else {
				orphan := line[x+1]
				orphan.Char = ' '
				orphan.Combining = ""
				orphan.CellWidth = nw - need
				if orphan.CellWidth < 1.0 {
					orphan.CellWidth = 1.0
				}
				line[x+1] = orphan
				need = 0
			}
		}
		b.screen[row] = line
	}
}
