package editor

// The live capture rung's mirror engine.
//
// Where `raw` tees bytes and `lines` folds a transcript, `live` maintains a
// two-dimensional MIRROR of the hosted terminal's screen inside the buffer: the
// child's cursor moves, overtypes, wraps, scrolls and clears are replayed as
// edits so the buffer shows what the screen shows, in place. It consumes the
// seven structural events purfecterm emits for the rung (OnWrite, OnCursorMove,
// OnNewline, OnLineWrap, OnBackspace, OnScrollLineOff, OnClearScreen), relayed
// across the host seam to the CaptureSink and delegated here.
//
// The region is bounded by three ephemeral cursors, per the design:
//
//   - top ("start of terminal") sits at the start of terminal row 0's line.
//     Row y maps to document line top.line + y. When the screen scrolls, top
//     advances one line: the departed row stays ABOVE the window as history
//     (O(1), history-preserving — no line is deleted).
//   - caret ("terminal caret") is where writes land; it rides ordinary edits.
//   - bottom ("end of terminal") is the frontier. The region GROWS by inserting
//     a newline here; it is never pre-materialized, so a short-lived command
//     leaves no trailing blank rows.
//
// The engine stores no screen height: purfecterm already clamps coordinates to
// the live geometry and emits OnScrollLineOff itself, so the mirror simply
// follows the event stream — which makes it correct across a resize for free.
//
// Colour (full format only) rides inline as absolute, 0;-prefixed SGRs — never
// decorations (which would be per-document-unique and costly). Because SGR
// state persists across newlines in the linear document, a single-colour run
// emits exactly one SGR at its start and nothing after: the pen carried into
// each row is recorded in rowStartPen so a redundant SGR is suppressed. The
// begin/end correction for a caret that JUMPS back into already-coloured cells
// (emit the right pen, restore the old pen after the overtyped chunk, replace
// rather than stack an SGR it lands on) is the live rung's remaining refinement;
// this stage keeps colour correct for the streaming and sequential cases and
// leaves a mid-line recolour after a jump to that follow-up.

import (
	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/textwidth"
)

// liveMirror replays a hosted terminal's screen into a buffer region. It is
// driven entirely by the CaptureSink live events, which arrive on mew's main
// loop (synchronously inside a feed), so it needs no locking of its own.
type liveMirror struct {
	buf    *buffer.Buffer
	caret  *buffer.Cursor // "terminal caret": the write position
	top    *buffer.Cursor // "start of terminal": just before the EOL of the line above row 0
	bottom *buffer.Cursor // "end of terminal": the frontier boundary, below the last row
	peek   *buffer.Cursor // read-only scratch: never perturbs the write caret
	format captureFormat

	// curX, curY is the caret's believed cell position in the terminal. Every
	// event carries the post-move coordinates, so we trust them and reconcile
	// only when a write arrives somewhere the caret is not.
	curX, curY int

	// full-format colour state. curPen is the absolute SGR in effect in the
	// document at the caret ("" == default); rowStartPen[y] is the pen carried
	// into row y's column 0, so a pen unchanged across a newline emits nothing.
	curPen      string
	rowStartPen []string
}

// newLiveMirror builds the Method 4 workspace around the caret's launch point.
// caret is the session's ephemeral out-cursor (seeked to the launch point);
// editCaret is the viewport's editing caret, parked on "end of terminal" so the
// tail rides with the output (nil when there is none).
//
// If the caret is not at the start of a line, a newline first breaks the
// terminal onto its own line. Then a two-newline scaffold creates three lines:
// the line above row 0 (holding "start of terminal", just before its EOL), row 0
// itself ("terminal caret"), and the line below (holding "end of terminal").
// Every terminal mutation lands strictly ABOVE end of terminal, so garland
// carries end of terminal — and the editing caret on it — forward for free.
func newLiveMirror(b *buffer.Buffer, caret *buffer.Cursor, format captureFormat, editCaret *buffer.Caret) *liveMirror {
	line, roff := caret.GetPosition()
	if roff != 0 {
		caret.InsertString("\n", nil, false) // break onto a fresh line
		line, _ = caret.GetPosition()
	}
	caret.SeekLineRune(line, 0)
	caret.InsertString("\n\n", nil, false) // the region scaffold

	top := b.NewEphemeralCursor()
	top.SeekLineRune(line, 0)
	top.SeekLineEnd() // just before the EOL of the line above row 0
	caret.SeekLineRune(line+1, 0)
	bottom := b.NewEphemeralCursor()
	bottom.SeekLineRune(line+2, 0)
	if editCaret != nil {
		editCaret.Seek(line+2, 0) // park on end of terminal so it rides the tail
	}
	return &liveMirror{
		buf:         b,
		caret:       caret,
		top:         top,
		bottom:      bottom,
		peek:        b.NewEphemeralCursor(),
		format:      format,
		rowStartPen: []string{""},
	}
}

// release drops the top/bottom/peek cursors. The caret is the session's
// out-cursor, released by the session teardown, not here.
func (m *liveMirror) release() {
	if m.top != nil {
		m.top.Release()
		m.top = nil
	}
	if m.bottom != nil {
		m.bottom.Release()
		m.bottom = nil
	}
	if m.peek != nil {
		m.peek.Release()
		m.peek = nil
	}
}

// rowLine returns the document line number of terminal row y: one past the line
// "start of terminal" sits on, then down y more.
func (m *liveMirror) rowLine(y int) int {
	sl, _ := m.top.GetPosition()
	return sl + 1 + y
}

// frontierRow is the highest materialized row index (0-based). Rows occupy the
// lines strictly between "start of terminal" (line S) and "end of terminal"
// (line E): S+1 .. E-1, so the highest index is E-S-2.
func (m *liveMirror) frontierRow() int {
	sl, _ := m.top.GetPosition()
	el, _ := m.bottom.GetPosition()
	return el - sl - 2
}

// grow appends one blank row at the bottom of the region by inserting a newline
// at the END of the last row — strictly BEFORE "end of terminal", so end of
// terminal (and the editing caret parked on it) ride forward and the new row
// lands inside the region. The scratch cursor does the insert so the write caret
// is untouched.
func (m *liveMirror) grow() {
	el, _ := m.bottom.GetPosition()
	m.peek.SeekLineRune(el-1, 0)
	m.peek.SeekLineEnd()
	m.peek.InsertString("\n", nil, false)
	m.rowStartPen = append(m.rowStartPen, "")
}

// growTo materializes rows up to and including row y.
func (m *liveMirror) growTo(y int) {
	for m.frontierRow() < y {
		m.grow()
	}
}

// setRowPen records the pen carried into row y's column 0, growing the array as
// needed. Out-of-range rows below 0 are ignored.
func (m *liveMirror) setRowPen(y int, pen string) {
	if y < 0 {
		return
	}
	for len(m.rowStartPen) <= y {
		m.rowStartPen = append(m.rowStartPen, "")
	}
	m.rowStartPen[y] = pen
}

// rowPen returns the pen carried into row y's column 0, or "" if unknown.
func (m *liveMirror) rowPen(y int) string {
	if y < 0 || y >= len(m.rowStartPen) {
		return ""
	}
	return m.rowStartPen[y]
}

// lineFromCaret reads the caret's current line and its rune offset within it,
// using the scratch cursor so the write caret is never moved. Returns the line
// text (which may include a trailing newline) and the caret's rune offset.
func (m *liveMirror) lineFromCaret() (text string, off int) {
	line, roff := m.caret.GetPosition()
	m.peek.SeekLineRune(line, 0)
	text, _ = m.peek.ReadLine()
	return text, roff
}

// seekTo positions the caret at cell (x, y): the document line for row y, at
// visual column x, padding a short line with spaces so the column exists. It
// materializes intervening rows if y is beyond the frontier. Reads go through
// the scratch cursor; only the pad insert and the final seek touch the caret.
func (m *liveMirror) seekTo(x, y int) {
	if y < 0 {
		y = 0
	}
	m.growTo(y)
	line := m.rowLine(y)
	m.peek.SeekLineRune(line, 0)
	text, _ := m.peek.ReadLine()
	runeOff, pad := visualColToRune(text, x)
	if pad > 0 {
		// Extend the line to reach column x. Padding is plain (default pen); it
		// stands in for cells the child never wrote.
		end := visibleRuneCount(text)
		m.caret.SeekLineRune(line, end)
		m.caret.InsertString(spaces(pad), nil, false)
		m.caret.SeekLineRune(line, end+pad)
	} else {
		m.caret.SeekLineRune(line, runeOff)
	}
	m.curX, m.curY = x, y
	if m.format == captureFull {
		// The ambient pen at the LANDING column, resolved from the row's inline
		// SGRs — so a write that jumps into coloured content knows what pen is
		// already in effect there and whether it must correct it.
		m.curPen = m.penAt(y, x)
	}
}

// Write folds an OnWrite run into the mirror at (x, y) as a SINGLE garland
// mutation — the caret is left pinned on the frontier so a stream of adjacent
// runs coalesces into one undo step, exactly as the raw rung's chunk inserts do.
//
// In full format the run carries a begin/end colour correction so a jump back
// into already-coloured cells stays exact: an absolute SGR at the head sets the
// run's pen (replacing an adjacent wrong SGR rather than stacking), and — when
// the run overtypes cells that have differently-coloured content to their right
// — a restore SGR at the tail returns the trailing cells to their own pen. All
// of it rides in one overwrite.
func (m *liveMirror) Write(x, y int, text, sgr string) {
	if x != m.curX || y != m.curY {
		m.seekTo(x, y)
	}
	w := runsWidth(text)

	if m.format != captureFull {
		// No colour: geometry only, one op.
		line, off := m.lineFromCaret()
		midBytes, _, _, _ := spanForward([]rune(line), off, w)
		if midBytes == 0 {
			m.caret.InsertString(text, nil, false)
		} else {
			m.caret.Overwrite(midBytes, text)
			m.caret.SeekRelativeRunes(len([]rune(text)))
		}
		m.curX += w
		return
	}

	line, off := m.lineFromCaret()
	rs := []rune(line)
	if off > len(rs) {
		off = len(rs)
	}
	writePen := normalizePen(sgr)

	midBytes, endRune, boundarySGR, _ := spanForward(rs, off, w)
	hasTrailing := hasVisibleAfter(rs, endRune)

	// Begin correction: make the run's pen take effect. Emit only on a real
	// change; when an SGR sits immediately before the caret, replace it (extend
	// the overwrite back over it) rather than stacking a second one.
	begin := ""
	startOff, delLead := off, 0
	if writePen != m.curPen {
		begin = sgr
		if begin == "" {
			begin = sgrReset // reverting to default needs an explicit reset
		}
		if p := sgrEndingAt(rs, off); p > 0 {
			startOff = off - p
			delLead = len(string(rs[startOff:off]))
		}
	}

	// End correction: if the overtyped cells have differently-coloured content
	// to their right, restore that pen after the run. Replace an SGR sitting at
	// the boundary rather than stacking.
	end, delBoundary := "", 0
	if hasTrailing {
		if restore := m.penAt(y, x+w); restore != writePen {
			end = restore
			if end == "" {
				end = sgrReset
			}
			if boundarySGR > 0 {
				delBoundary = len(string(rs[endRune : endRune+boundarySGR]))
			}
		}
	}

	del := delLead + midBytes + delBoundary
	payload := begin + text + end
	if del == 0 {
		// Frontier append (end is empty here): the insert advances the caret past
		// begin+text, leaving it on the frontier.
		m.caret.InsertString(payload, nil, false)
	} else {
		if startOff != off {
			m.caret.SeekRelativeRunes(startOff - off) // back over the replaced leading SGR
		}
		m.caret.Overwrite(del, payload)
		// Land the caret right after begin+text — before any restore SGR — so an
		// adjacent continuation writes at the correct column.
		m.caret.SeekRelativeRunes(len([]rune(begin)) + len([]rune(text)))
	}

	m.curPen = writePen
	if x == 0 {
		m.setRowPen(y, writePen)
	}
	m.curX += w
}

// spanForward walks rs from rune offset off across w visible columns, treating
// inline SGR runs as zero-width. It returns the byte length of the spanned cells
// (including any interleaved SGR bytes, which an overtype replaces), the rune
// offset just past them, the rune length of an SGR sitting exactly at that
// boundary (0 if none), and whether the line ran out before w columns (an append
// past EOL). It stops at end of line.
func spanForward(rs []rune, off, w int) (midBytes, endRune, boundarySGR int, atEOL bool) {
	vis, i := 0, off
	for i < len(rs) && vis < w {
		if rs[i] == '\n' {
			break
		}
		if rs[i] == 0x1b {
			if n := sgrRunLen(string(rs[i:])); n > 0 {
				midBytes += len(string(rs[i : i+n]))
				i += n
				continue
			}
		}
		midBytes += len(string(rs[i]))
		vis += textwidth.Rune(rs[i])
		i++
	}
	endRune = i
	atEOL = vis < w
	if i < len(rs) && rs[i] == 0x1b {
		if n := sgrRunLen(string(rs[i:])); n > 0 {
			boundarySGR = n
		}
	}
	return
}

// hasVisibleAfter reports whether any visible cell (non-SGR, before the newline)
// remains in rs at or after rune offset off.
func hasVisibleAfter(rs []rune, off int) bool {
	for i := off; i < len(rs); i++ {
		if rs[i] == '\n' {
			return false
		}
		if rs[i] == 0x1b {
			if n := sgrRunLen(string(rs[i:])); n > 0 {
				i += n - 1
				continue
			}
		}
		return true
	}
	return false
}

// sgrEndingAt returns the rune length of an SGR run that ends exactly at rune
// offset off (so it sits immediately before the caret), or 0 if none does.
func sgrEndingAt(rs []rune, off int) int {
	i := 0
	for i < off {
		if rs[i] == 0x1b {
			if n := sgrRunLen(string(rs[i:])); n > 0 {
				if i+n == off {
					return n
				}
				i += n
				continue
			}
		}
		i++
	}
	return 0
}

// penAt returns the absolute pen in effect at visual column col of row y,
// resolved by walking the row's inline SGRs from its recorded start pen. A reset
// resolves to the default ("").
func (m *liveMirror) penAt(y, col int) string {
	pen := m.rowPen(y)
	rs := []rune(m.readRow(y))
	vis, i := 0, 0
	for i < len(rs) && vis < col {
		if rs[i] == '\n' {
			break
		}
		if rs[i] == 0x1b {
			if n := sgrRunLen(string(rs[i:])); n > 0 {
				pen = normalizePen(string(rs[i : i+n]))
				i += n
				continue
			}
		}
		vis += textwidth.Rune(rs[i])
		i++
	}
	return pen
}

// readRow reads row y's document line text through the scratch cursor.
func (m *liveMirror) readRow(y int) string {
	m.peek.SeekLineRune(m.rowLine(y), 0)
	s, _ := m.peek.ReadLine()
	return s
}

// sgrReset is the explicit "back to default pen" sequence, emitted only when a
// correction reverts to the default (which otherwise carries no SGR).
const sgrReset = "\x1b[0m"

// normalizePen maps the reset forms to the default ("") so a scanned pen and an
// absent pen compare equal.
func normalizePen(sgr string) string {
	switch sgr {
	case "", sgrReset, "\x1b[m", "\x1b[0;m":
		return ""
	}
	return sgr
}

// Newline moves the caret to column 0 of the next row (OnNewline / OnLineWrap):
// it steps past the current line's EOL rather than inserting one, so the
// remainder of a line is never pushed down; at the frontier it grows a row. The
// pen carries across unchanged.
func (m *liveMirror) Newline(x, y int) {
	if m.curY >= m.frontierRow() {
		m.growTo(m.curY + 1)
	}
	m.caret.SeekLineEnd()
	m.caret.SeekRelativeRunes(1) // over the newline, to next line's column 0
	m.curY++
	m.curX = 0
	if m.format == captureFull {
		m.setRowPen(m.curY, m.curPen) // SGR persists across the newline
	}
	if x != 0 || y != m.curY {
		m.seekTo(x, y)
	}
}

// Backspace moves the caret left one cell (OnBackspace).
func (m *liveMirror) Backspace(x, y int) {
	if y == m.curY && x == m.curX-1 {
		m.caret.SeekRelativeRunes(-1)
		m.curX = x
		return
	}
	m.seekTo(x, y)
}

// CursorMove repositions the caret for an absolute move (OnCursorMove).
func (m *liveMirror) CursorMove(x, y int) {
	m.seekTo(x, y)
}

// ScrollLineOff slides the window down n rows: "start of terminal" advances to
// just before the next line's EOL, so each departed row stays above the window
// as history (nothing is deleted), and the per-row pen array drops them. The
// frontier is regrown by the newline that follows the scroll.
func (m *liveMirror) ScrollLineOff(n int) {
	for i := 0; i < n; i++ {
		m.top.SeekRelativeRunes(1) // over the current EOL, onto the next line
		m.top.SeekLineEnd()        // to just before that line's EOL
		if len(m.rowStartPen) > 1 {
			m.rowStartPen = m.rowStartPen[1:]
		}
		if m.curY > 0 {
			m.curY--
		}
	}
}

// ClearScreen preserves the current screen as history and starts a fresh single
// row below it (OnClearScreen), consistent with the scroll model: nothing is
// deleted. A blank row is grown at the bottom, then "start of terminal" advances
// so that blank row becomes the new row 0 and every prior row is history above.
func (m *liveMirror) ClearScreen() {
	m.grow() // a fresh blank row just above end of terminal
	el, _ := m.bottom.GetPosition()
	// The new blank row is line el-1; start of terminal moves to just before the
	// EOL of the line above it.
	m.top.SeekLineRune(el-2, 0)
	m.top.SeekLineEnd()
	m.caret.SeekLineRune(el-1, 0)
	m.curX, m.curY = 0, 0
	m.curPen = ""
	m.rowStartPen = []string{""}
}

// --- small pure helpers ---

// visualColToRune maps a target visual column to a rune offset within line,
// treating inline SGR (ESC [ … m) sequences as zero-width. It returns the rune
// offset of the cell at that column and, if the line's visible content is
// shorter than the column, the number of pad cells needed to reach it.
func visualColToRune(line string, targetVis int) (runeOff, pad int) {
	rs := []rune(line)
	vis, i := 0, 0
	for i < len(rs) {
		if rs[i] == 0x1b {
			n := sgrRunLen(string(rs[i:]))
			if n > 0 {
				i += n
				continue
			}
		}
		if vis >= targetVis {
			return i, 0
		}
		vis += textwidth.Rune(rs[i])
		i++
	}
	if vis < targetVis {
		return i, targetVis - vis
	}
	return i, 0
}

// runsWidth is the total visible width of text in cells.
func runsWidth(text string) int {
	w := 0
	for _, r := range text {
		w += textwidth.Rune(r)
	}
	return w
}

// visibleRuneCount returns the number of runes in line up to (not counting a
// trailing newline) — the rune index of end of line, used to append at EOL.
func visibleRuneCount(line string) int {
	rs := []rune(line)
	n := len(rs)
	if n > 0 && rs[n-1] == '\n' {
		n--
	}
	return n
}

// sgrRunLen returns the rune length of an SGR/CSI sequence at the start of s
// (ESC [ … final-byte), or 0 if s does not begin one.
func sgrRunLen(s string) int {
	rs := []rune(s)
	if len(rs) < 2 || rs[0] != 0x1b || rs[1] != '[' {
		return 0
	}
	for i := 2; i < len(rs); i++ {
		if rs[i] >= 0x40 && rs[i] <= 0x7e { // CSI final byte
			return i + 1
		}
	}
	return 0 // unterminated: treat as not-yet-a-sequence
}

// spaces returns a run of n spaces.
func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}
