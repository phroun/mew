package core

import (
	"sync"
	"unicode"
)

// The cell-width rule: how many terminal cells one rune takes. A cell backend
// installs its own, so that what a layout MEASURES is what the emitter will
// ADVANCE. Two answers to that question means two pictures of one row, and
// every column after the disagreement is in the wrong place -- a caption
// centred against a width the emitter does not produce, a label truncated a
// cell early, a run that overflows what was reserved for it.
//
// It lives in core, rather than in the text engine, for the same reason the
// RTL-mark hint does: the TUI backend has no font engine -- glyphs are the
// outer terminal's concern -- and core is the one lightweight package every
// backend already imports.
//
// A pixel backend installs a text measurer instead (see SetTextMeasurer) and
// never consults this rule: it measures real glyph advances.
var (
	cellWidthMu sync.RWMutex
	cellWidthFn func(rune) int
)

// SetCellWidth installs the cell target's own rule. Pass nil to restore the
// built-in one. Called by the backend that will do the emitting; one render
// target per process.
func SetCellWidth(fn func(rune) int) {
	cellWidthMu.Lock()
	cellWidthFn = fn
	cellWidthMu.Unlock()
}

// CellWidth is how many cells r occupies: 0 for a mark that paints into the
// cell before it, 2 for a wide glyph, 1 for everything else.
func CellWidth(r rune) int {
	cellWidthMu.RLock()
	fn := cellWidthFn
	cellWidthMu.RUnlock()
	if fn != nil {
		return fn(r)
	}
	return builtinCellWidth(r)
}

// builtinCellWidth is the answer with no backend to ask: a non-spacing or
// enclosing mark paints into the cell before it and advances nothing, the
// ideograph blocks core knows about take two, everything else takes one.
//
// A real cell backend knows more than this -- its tables cover the wide runes
// outside those blocks, and the runes that take no cell without being marks at
// all -- which is why it installs its own.
func builtinCellWidth(r rune) int {
	if unicode.In(r, unicode.Mn, unicode.Me) {
		return 0
	}
	if isWideChar(r) {
		return 2
	}
	return 1
}
