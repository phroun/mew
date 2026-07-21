package render

import (
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/phroun/mew/internal/bidi"
	"github.com/phroun/mew/internal/textwidth"
)

// backBuffer is an off-screen terminal grid. Every write the renderer would
// have sent straight to the terminal is instead painted into cur (the frame
// being composed); present() then diffs cur against disp (what the terminal
// currently shows) and emits only the escape sequences needed to reconcile the
// two. The renderer keeps calling MoveCursor/Write exactly as before, so all
// bidi/Arabic shaping, cursor math, and layout logic are untouched — they just
// paint into cells instead of stdout.
//
// Fidelity notes that make this safe to slot underneath the existing renderer:
//   - Styles are stored as the exact SGR bytes in effect (an accumulator reset
//     by any sequence carrying a "0" param), never re-parsed or re-ordered, so
//     the bytes emitted before a glyph match what the direct writer produced.
//   - Column advancement uses textwidth.Rune — the same width model the
//     renderer uses — so wide (2-col) glyphs and zero-width combining marks land
//     in exactly the columns the terminal would have advanced to. Combining
//     marks attach to the preceding cell's cluster; a wide glyph owns its base
//     cell plus a continuation cell.
//   - The bottom-right corner cell is never emitted (the renderer's "corner cut"
//     that avoids terminal scroll).
type backBuffer struct {
	w, h int

	cur  [][]bbCell // the frame currently being painted
	disp [][]bbCell // what the terminal is currently showing

	penX, penY int    // 0-based cursor position within cur
	penStyle   string // SGR bytes currently in effect ("" == terminal default)

	// Desired hardware-cursor visibility, set via Show/HideCursor; applied at
	// the end of present(). lastVisible/haveVisible track what the terminal was
	// last told so an unchanged state emits nothing.
	curVisible  bool
	lastVisible bool
	haveVisible bool

	// pendingClear forces present() to emit a full clear (\x1b[2J) and repaint
	// everything — set on resize and by ForceRedraw/screen_refresh.
	pendingClear bool

	// logicalCUP switches cursor addressing to LOGICAL columns for flex-width
	// terminals (purfecterm under DECSET 2027): their grid stores one cell per
	// character — a wide glyph occupies a single cell whose 2-column width is an
	// attribute — so a CUP column must count characters, not visual cells.
	// mew's grid is visual (wide = base + continuation), so the emitted column
	// is the count of non-continuation cells left of the target. Off (default)
	// emits visual columns, the classic terminal contract.
	logicalCUP bool

	// emitPen is the SGR currently in effect on the terminal during present():
	// the last style putStyle emitted. Same-style cells then skip re-emitting the
	// sequence, which both shrinks the byte stream and keeps adjacent glyphs
	// contiguous — so a terminal's Arabic shaping / grapheme joining is not broken
	// by an escape injected mid-run. The pen persists across cursor moves (CUP
	// does not change SGR), so it is tracked across the whole present() frame and
	// reset to emitPenUnknown at its top (the prior frame's trailing pen is not
	// reliably known).
	emitPen string

	// flipBidi re-emits each RTL run of a row in reverse (logical) order, with
	// mirrored glyphs restored, for host terminals that apply their own bidi
	// (macOS Terminal.app): they reorder the run back to the visual layout mew
	// intended. Off (default) emits visual order for terminals that do not
	// reorder (iTerm2, xterm, ...). See the flipBidiForHost option.
	flipBidi bool

	// sawRTL latches when any strong-RTL rune is painted — the trigger for the
	// one-time flipBidiForHost=auto terminal probe (RTL is the first point the
	// setting matters).
	sawRTL bool

	// rowWide[y] draws that row double-width (DEC DECDWL, ESC#6): the terminal
	// shows the row's left half at 2x and hides the right half, so the renderer
	// lays content into the left half only. dispRowWide tracks what the terminal
	// was last told, so the mode is (re)emitted only on change. A double-width
	// row is always fully re-emitted (no cell diff) so no mid-row cursor address
	// — which DECDWL would misplace — is ever used on it.
	rowWide     []bool
	dispRowWide []bool
	// rowWideFill is the SGR the erase-to-end uses on a double-width row, so the
	// cleared cells take the row's own background instead of whatever colour was
	// last emitted.
	rowWideFill []string
}

// bbCell is one terminal cell. A width-2 glyph occupies a base cell (width 2)
// followed by a continuation cell (cont true, width 0); present() emits the
// base and skips the continuation. A blank cell has nil runes (rendered as a
// space) and the default style.
type bbCell struct {
	runes []rune // primary rune + any zero-width combining marks; nil == blank
	style string // exact SGR bytes for this cell ("" == default)
	width int8   // 1 or 2 for a base cell; 0 for a continuation; -1 == sentinel
	cont  bool   // right half of a wide glyph
}

const defaultStyleSeq = "\x1b[0m"

// emitPenUnknown is the sentinel emitPen is reset to at the top of each
// present(): no real style string equals it, so the first cell always emits its
// SGR (the prior frame's trailing pen is not reliably known).
const emitPenUnknown = "\x00"

// putStyle emits style's SGR bytes only when they differ from the pen already in
// effect on the terminal (emitPen), collapsing runs of same-styled cells. The
// pen persists across cursor moves — CUP does not change SGR — so this is
// tracked across the whole present() frame. An empty style is the terminal
// default.
func (b *backBuffer) putStyle(sb *strings.Builder, style string) {
	if style == "" {
		style = defaultStyleSeq
	}
	if style == b.emitPen {
		return
	}
	sb.WriteString(style)
	b.emitPen = style
}

func newBackBuffer(w, h int) *backBuffer {
	b := &backBuffer{curVisible: true}
	b.reshape(w, h)
	// The first frame paints over the existing terminal (disp is all-sentinel so
	// every cell is drawn) without a screen clear — matching the original
	// renderer's startup, which never cleared. Resize and screen_refresh still
	// clear via reshape/forceRedraw.
	b.pendingClear = false
	return b
}

// reshape (re)allocates the grids for a new size and forces a full repaint. The
// display grid is filled with sentinel cells so every real cell differs and is
// emitted on the next present().
func (b *backBuffer) reshape(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	b.w, b.h = w, h
	b.cur = make([][]bbCell, h)
	b.disp = make([][]bbCell, h)
	b.rowWide = make([]bool, h)
	b.dispRowWide = make([]bool, h)
	b.rowWideFill = make([]string, h)
	for y := 0; y < h; y++ {
		b.cur[y] = make([]bbCell, w)
		b.disp[y] = make([]bbCell, w)
		for x := 0; x < w; x++ {
			b.cur[y][x] = bbCell{width: 1}
			b.disp[y][x] = bbCell{width: -1} // sentinel: never equal to a real cell
		}
	}
	b.pendingClear = true
}

// begin starts a fresh frame: the cursor and pen reset, and every cell of cur
// is cleared to a blank so regions the renderer does not touch this frame are
// correctly erased by the diff.
func (b *backBuffer) begin() {
	b.penX, b.penY = 0, 0
	b.penStyle = ""
	for y := 0; y < b.h; y++ {
		row := b.cur[y]
		for x := 0; x < b.w; x++ {
			row[x] = bbCell{width: 1}
		}
		if y < len(b.rowWide) {
			b.rowWide[y] = false
		}
	}
}

// setRowWide marks the pen's current row double-width for this frame, with the
// SGR its erase-to-end should use (the row's own background). The renderer
// calls it after painting a double-width line; the content must already be
// confined to the left half of the row.
func (b *backBuffer) setRowWide(fill string) {
	if b.penY >= 0 && b.penY < len(b.rowWide) {
		b.rowWide[b.penY] = true
		b.rowWideFill[b.penY] = fill
	}
}

// forceRedraw makes the next present() clear the screen and repaint every cell.
func (b *backBuffer) forceRedraw() {
	b.pendingClear = true
	for y := 0; y < b.h; y++ {
		for x := 0; x < b.w; x++ {
			b.disp[y][x] = bbCell{width: -1}
		}
		b.dispRowWide[y] = false // re-emit the line mode next present
	}
}

// emitWideRow writes one double-width row in full: DECDWL, an erase-to-end (so
// a terminal that mis-handles the right half shows no junk), then the row's
// left-half cells (the only half DECDWL displays). No mid-row cursor
// addressing is used. disp is synced so the normal diff skips this row.
func (b *backBuffer) emitWideRow(sb *strings.Builder, y int) {
	writeCUP(sb, y+1, 1)
	sb.WriteString("\x1b#6") // DECDWL: double-width line
	// Set the row's background BEFORE erasing, so the cleared cells take it
	// rather than whatever colour was last emitted.
	b.putStyle(sb, b.rowWideFill[y])
	sb.WriteString("\x1b[0K") // erase to end of line
	half := b.w / 2
	for x := 0; x < half; x++ {
		nc := b.cur[y][x]
		if nc.cont {
			b.disp[y][x] = nc
			continue
		}
		b.putStyle(sb, nc.style)
		sb.WriteString(runesOf(nc))
		b.disp[y][x] = nc
		if nc.width == 2 && x+1 < b.w {
			b.disp[y][x+1] = b.cur[y][x+1]
		}
	}
	for x := half; x < b.w; x++ {
		b.disp[y][x] = b.cur[y][x] // synced (blank, off-screen right half)
	}
	b.dispRowWide[y] = true
}

// moveTo positions the pen (1-based, matching the terminal's CUP coordinates).
func (b *backBuffer) moveTo(x, y int) {
	b.penX = x - 1
	b.penY = y - 1
	if b.penX < 0 {
		b.penX = 0
	}
	if b.penY < 0 {
		b.penY = 0
	}
}

// writeString paints a run of terminal output into cur: SGR escapes update the
// pen style, everything else is decoded rune-by-rune into cells.
func (b *backBuffer) writeString(s string) {
	i := 0
	for i < len(s) {
		if s[i] == 0x1b {
			if i+1 < len(s) && s[i+1] == '[' {
				k := i + 2
				for k < len(s) {
					ch := s[k]
					if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
						break
					}
					k++
				}
				if k < len(s) {
					if s[k] == 'm' {
						b.applySGR(s[i : k+1])
					}
					// Non-SGR CSI sequences never reach this buffer (all cursor
					// motion and clears go through dedicated methods); ignore any
					// for grid purposes.
					i = k + 1
					continue
				}
				return // malformed trailing escape
			}
			i++ // lone ESC / non-CSI: skip
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			break
		}
		i += size
		b.putRune(r)
	}
}

// applySGR folds an "\x1b[...m" sequence into the pen-style accumulator. A
// sequence carrying a "0" reset param replaces the accumulator; anything else
// appends, so a color followed by e.g. "\x1b[7m" (reverse) keeps both — exactly
// the bytes the direct writer would have sent.
func (b *backBuffer) applySGR(seq string) {
	params := seq[2 : len(seq)-1] // between '[' and 'm'
	reset := params == ""         // ESC[m == ESC[0m
	if !reset {
		for _, p := range strings.Split(params, ";") {
			if p == "0" {
				reset = true
				break
			}
		}
	}
	if reset {
		b.penStyle = seq
	} else {
		b.penStyle += seq
	}
}

// putRune paints one decoded rune at the pen, advancing by its display width.
// Zero-width runes attach to the preceding cell's cluster; wide runes claim a
// continuation cell.
func (b *backBuffer) putRune(r rune) {
	if b.penY < 0 || b.penY >= b.h {
		return
	}
	w := textwidth.Rune(r)
	if w <= 0 {
		b.attachCombining(r)
		return
	}
	if !b.sawRTL && bidi.IsStrongRTL(r) {
		b.sawRTL = true
	}
	if b.penX < 0 {
		b.penX = 0
	}
	if b.penX >= b.w {
		return
	}
	row := b.cur[b.penY]
	x := b.penX

	// Blank any wide glyph this write straddles, so no orphaned half survives.
	// A wide glyph occupies a base cell (width 2) plus a continuation cell; if a
	// write lands on only one of the two, the other cell must be cleared or it
	// stays on screen as a "dead" copy — present() would otherwise skip a stray
	// continuation (thinking its base covers it) or leave a widowed base drawn.
	//   - Left edge: if x is the continuation of a glyph headed at x-1, blank x-1.
	//   - Right edge: if the last cell we occupy is a wide base, blank its
	//     continuation just past our span.
	if x > 0 && row[x].cont {
		row[x-1] = bbCell{width: 1}
	}
	last := x + w - 1
	if last >= b.w {
		last = b.w - 1
	}
	if last+1 < b.w && row[last].width == 2 {
		row[last+1] = bbCell{width: 1}
	}

	row[x] = bbCell{runes: []rune{r}, style: b.penStyle, width: int8(w)}
	if w == 2 && x+1 < b.w {
		row[x+1] = bbCell{style: b.penStyle, width: 0, cont: true}
	}
	b.penX += w
}

// attachCombining appends a zero-width mark to the cluster of the cell just left
// of the pen, stepping back over a wide glyph's continuation cell to reach its
// base.
func (b *backBuffer) attachCombining(r rune) {
	if b.penY < 0 || b.penY >= b.h {
		return
	}
	row := b.cur[b.penY]
	x := b.penX - 1
	if x >= 0 && x < b.w && row[x].cont {
		x--
	}
	if x >= 0 && x < b.w && len(row[x].runes) > 0 {
		row[x].runes = append(row[x].runes, r)
	}
}

// present reconciles the terminal (disp) with the painted frame (cur), writing
// the minimal escape sequences to out and leaving disp equal to cur.
//
// The diff granularity depends on the emission mode. The default emits minimal
// CELL SPANS — stream-order terminals place each addressed cell exactly, so
// nothing more is needed. With flipBidi, the granularity becomes a ROW: the
// flip is a run-level transform (an RTL run re-emitted in logical order for the
// terminal's own bidi to lay out), which is only well-defined over a whole
// row's runs — a single-cell write would interleave visual-order bytes into a
// line the terminal stores logically. An unchanged row costs nothing in either
// mode.
func (b *backBuffer) present(out io.Writer) {
	var sb strings.Builder
	b.emitPen = emitPenUnknown // the terminal's trailing pen from last frame is unknown

	if b.pendingClear {
		sb.WriteString("\x1b[2J\x1b[H")
		b.pendingClear = false
	}

	// Double-width rows are emitted fully here (no cell diff, so no mid-row
	// cursor addressing lands on a DECDWL line), then the normal diff below
	// skips them (their disp is synced). A row that stopped being double-width
	// is reset to single-width and handed back to the diff to repaint.
	for y := 0; y < b.h; y++ {
		if b.rowWide[y] {
			b.emitWideRow(&sb, y)
		} else if b.dispRowWide[y] {
			writeCUP(&sb, y+1, 1)
			sb.WriteString("\x1b#5") // DECSWL: back to single-width
			b.dispRowWide[y] = false
			for x := 0; x < b.w; x++ {
				b.disp[y][x] = bbCell{width: -1} // force the diff to repaint it
			}
		}
	}

	// Emission granularity. flipBidi needs whole rows (the run-level flip).
	// Under logicalCUP, presentSpans itself escalates to a whole row exactly
	// when that row's width profile changed (see rowProfileChanged) — minimal
	// spans whenever they are safe, the full rewrite only when necessary.
	if b.flipBidi {
		b.presentRows(&sb)
	} else {
		b.presentSpans(&sb)
	}

	// Paint the bottom-right corner cell's background. It can never hold a glyph
	// (printing there scrolls the terminal), so instead of leaving a stale notch
	// we move onto it and emit EL (\x1b[K), which fills from the cursor to the
	// end of the line with the current SGR background WITHOUT writing a character
	// — colouring the corner to match its neighbour. The colour is the corner
	// cell's own painted style when it has one (e.g. an RTL gutter's trailing
	// cell), else the cell to its left (e.g. the last LTR content cell).
	if b.w >= 1 && b.h >= 1 {
		corner := b.cur[b.h-1][b.w-1]
		style := corner.style
		if style == "" && b.w >= 2 {
			style = b.cur[b.h-1][b.w-2].style
		}
		fill := bbCell{style: style, width: 1}
		if !cellsEqual(fill, b.disp[b.h-1][b.w-1]) {
			writeCUP(&sb, b.h, b.logicalColFor(b.h-1, b.w-1)+1)
			b.putStyle(&sb, style)
			sb.WriteString("\x1b[K")
			b.disp[b.h-1][b.w-1] = fill
		}
	}

	// Place the hardware cursor where the renderer left the pen, then apply
	// visibility only when it changed. The pen is a visual column; a
	// flex-width terminal is addressed by its logical column instead.
	writeCUP(&sb, b.penY+1, b.logicalColFor(b.penY, b.penX)+1)
	if !b.haveVisible || b.curVisible != b.lastVisible {
		if b.curVisible {
			sb.WriteString("\x1b[?25h")
		} else {
			sb.WriteString("\x1b[?25l")
		}
		b.lastVisible = b.curVisible
		b.haveVisible = true
	}

	io.WriteString(out, sb.String())
}

// presentSpans emits minimal cell-span diffs: a cursor move to each changed
// span, then only its changed cells. The default mode for stream-order
// terminals, which place every addressed cell exactly.
// rowProfileChanged reports whether a row's WIDTH PROFILE — which cells are
// wide-glyph continuations — differs between the new frame and what the
// terminal shows. On a logical-grid terminal that means the row's logical
// cell structure changed: a mid-row span write would then consume a different
// number of logical cells than it replaces, silently shifting everything
// preserved to its right. Such a row must be rewritten whole.
func (b *backBuffer) rowProfileChanged(y int) bool {
	for x := 0; x < b.w; x++ {
		if b.cur[y][x].cont != b.disp[y][x].cont {
			return true
		}
	}
	return false
}

func (b *backBuffer) presentSpans(sb *strings.Builder) {
	termRow, termCol := -1, -1 // where the terminal cursor sits (unknown)
	for y := 0; y < b.h; y++ {
		// Flex-terminal escalation: minimal spans are safe only while the
		// row's logical structure is stable. When the width profile changed,
		// rewrite the row whole and truncate its logical remainder with EL
		// (the old row may have held more logical cells than the new content
		// writes; leftovers would resurface as stale fragments).
		if b.logicalCUP && b.rowProfileChanged(y) {
			writeCUP(sb, y+1, 1)
			b.emitRow(sb, y)
			sb.WriteString("\x1b[0K")
			if y == b.h-1 && b.w >= 1 {
				// The EL cleared the never-written corner cell: force the
				// corner-fill pass below to repaint it.
				b.disp[y][b.w-1] = bbCell{width: -1}
			}
			termRow, termCol = -1, -1
			continue
		}
		x := 0
		for x < b.w {
			if b.isCornerCut(y, x) {
				break
			}
			if cellsEqual(b.cur[y][x], b.disp[y][x]) {
				x++
				continue
			}
			// Position the cursor at the start of this changed span.
			if termRow != y || termCol != x {
				writeCUP(sb, y+1, b.logicalColFor(y, x)+1)
				termRow, termCol = y, x
			}
			for x < b.w && !b.isCornerCut(y, x) && !cellsEqual(b.cur[y][x], b.disp[y][x]) {
				nc := b.cur[y][x]
				if nc.cont {
					// A lone changed continuation implies its base changed too
					// and was already emitted; just sync and move on.
					b.disp[y][x] = nc
					x++
					termCol = -1
					break
				}
				// Emit the style only when it changes from the pen already in
				// effect (putStyle), so a same-styled run stays a contiguous glyph
				// stream — no SGR is injected between adjacent letters, which the
				// terminal needs for Arabic shaping / grapheme joining.
				b.putStyle(sb, nc.style)
				sb.WriteString(runesOf(nc))
				b.disp[y][x] = nc
				wd := int(nc.width)
				if wd < 1 {
					wd = 1
				}
				if wd == 2 && x+1 < b.w {
					b.disp[y][x+1] = b.cur[y][x+1]
				}
				x += wd
				termRow, termCol = y, x
			}
		}
	}
}

// presentRows emits whole changed rows (flip mode: the run-level bidi flip
// needs the full row); unchanged rows cost nothing.
func (b *backBuffer) presentRows(sb *strings.Builder) {
	for y := 0; y < b.h; y++ {
		changed := false
		for x := 0; x < b.w; x++ {
			if b.isCornerCut(y, x) {
				break
			}
			if !cellsEqual(b.cur[y][x], b.disp[y][x]) {
				changed = true
				break
			}
		}
		if !changed {
			continue
		}
		writeCUP(sb, y+1, 1)
		b.emitRow(sb, y)
		if b.logicalCUP {
			// Truncate the logical-grid row's remainder: the old row may have
			// held MORE logical cells than we just wrote (each wide glyph in
			// the new content is one cell there), and any leftover would
			// resurface as stale fragments past the new content.
			sb.WriteString("\x1b[0K")
			if y == b.h-1 && b.w >= 1 {
				// The EL cleared the never-written corner cell: force the
				// corner-fill pass to repaint it.
				b.disp[y][b.w-1] = bbCell{width: -1}
			}
		}
	}
}

// emitRow writes one full row's cells into sb (left to right) and syncs disp.
// With flipBidi, each RTL run's cells are emitted in reverse (logical) order
// with mirrored glyphs restored — the host terminal's own bidi reorders the
// run back to the visual layout the grid holds.
func (b *backBuffer) emitRow(sb *strings.Builder, y int) {
	// Collect the row's visual cells (base cells only; a stray continuation
	// with no base paints as a blank).
	type vc struct{ cell bbCell }
	cells := make([]vc, 0, b.w)
	for x := 0; x < b.w; {
		if b.isCornerCut(y, x) {
			break
		}
		nc := b.cur[y][x]
		if nc.cont {
			nc = bbCell{width: 1}
		}
		cells = append(cells, vc{cell: nc})
		b.disp[y][x] = b.cur[y][x]
		wd := int(nc.width)
		if wd < 1 {
			wd = 1
		}
		if wd == 2 && x+1 < b.w {
			b.disp[y][x+1] = b.cur[y][x+1]
		}
		x += wd
	}

	emit := func(c bbCell, mirror bool) {
		// Style emission depends on the mode. flipBidi exists for Terminal.app,
		// whose own bidi engine re-processes each parsed line: coalesced
		// run-level SGR does not survive that reordering (colors vanish), while
		// per-glyph attributes do — and since it shapes from parsed characters,
		// per-glyph SGR cannot break its Arabic joining. Everywhere else
		// (logicalCUP whole-row escalation) coalesce via the pen as usual.
		if b.flipBidi {
			style := c.style
			if style == "" {
				style = defaultStyleSeq
			}
			sb.WriteString(style)
			b.emitPen = style
		} else {
			b.putStyle(sb, c.style)
		}
		if len(c.runes) == 0 {
			sb.WriteByte(' ')
			return
		}
		if mirror {
			sb.WriteRune(bidi.Mirror(c.runes[0]))
		} else {
			sb.WriteRune(c.runes[0])
		}
		for _, m := range c.runes[1:] {
			sb.WriteRune(m)
		}
	}

	if !b.flipBidi {
		for _, c := range cells {
			emit(c.cell, false)
		}
		return
	}

	// Flip mode: find maximal RTL runs — segments bounded by strong-RTL cells
	// whose interior holds no strong-LTR content — and emit each run's cells in
	// reverse order with mirrored glyphs restored (Mirror is an involution).
	isRTL := func(c bbCell) bool { return len(c.runes) > 0 && bidi.IsStrongRTL(c.runes[0]) }
	isStrongLTR := func(c bbCell) bool {
		if len(c.runes) == 0 {
			return false
		}
		r := c.runes[0]
		return !bidi.IsStrongRTL(r) && (unicode.IsLetter(r) || unicode.IsDigit(r))
	}
	for i := 0; i < len(cells); {
		if !isRTL(cells[i].cell) {
			emit(cells[i].cell, false)
			i++
			continue
		}
		// Extend the run: through further RTL cells, absorbing interior
		// neutral cells only when another RTL cell follows before any strong
		// LTR content.
		end := i
		for j := i + 1; j < len(cells); j++ {
			if isRTL(cells[j].cell) {
				end = j
				continue
			}
			if isStrongLTR(cells[j].cell) {
				break
			}
		}
		for j := end; j >= i; j-- {
			emit(cells[j].cell, true)
		}
		i = end + 1
	}
}

// isCornerCut reports the bottom-right cell, which is never written to avoid
// terminals scrolling when the last column of the last row is filled.
func (b *backBuffer) isCornerCut(y, x int) bool {
	return y == b.h-1 && x == b.w-1
}

// logicalColFor maps a 0-based visual grid column to the 0-based column to
// address in CUP: identity normally; under logicalCUP, the count of
// non-continuation cells to its left on the row (each wide glyph is ONE cell
// in a flex-width terminal's logical grid). It reads cur, which is valid for
// every present-path CUP: spans emit left-to-right within a row, so cells left
// of the target are already reconciled (or were unchanged).
func (b *backBuffer) logicalColFor(y, x int) int {
	if !b.logicalCUP || y < 0 || y >= b.h {
		return x
	}
	col := 0
	row := b.cur[y]
	for i := 0; i < x && i < b.w; i++ {
		if !row[i].cont {
			col++
		}
	}
	return col
}

func writeCUP(sb *strings.Builder, row, col int) {
	sb.WriteString("\x1b[")
	sb.WriteString(strconv.Itoa(row))
	sb.WriteByte(';')
	sb.WriteString(strconv.Itoa(col))
	sb.WriteByte('H')
}

// runesOf renders a cell's content: a blank cell is a single space.
func runesOf(c bbCell) string {
	if len(c.runes) == 0 {
		return " "
	}
	return string(c.runes)
}

func cellsEqual(a, b bbCell) bool {
	if a.width != b.width || a.cont != b.cont || a.style != b.style {
		return false
	}
	if len(a.runes) != len(b.runes) {
		return false
	}
	for i := range a.runes {
		if a.runes[i] != b.runes[i] {
			return false
		}
	}
	return true
}
