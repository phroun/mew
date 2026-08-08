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
	caret  *buffer.Cursor // write position ("terminal caret")
	top    *buffer.Cursor // row 0 line start ("start of terminal")
	bottom *buffer.Cursor // frontier line ("end of terminal")
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

// newLiveMirror builds a mirror whose row 0 is the caret's current line. caret
// is the session's ephemeral out-cursor (already seeked to the launch point);
// top and bottom are freshly created around it and released with the mirror.
func newLiveMirror(b *buffer.Buffer, caret *buffer.Cursor, format captureFormat) *liveMirror {
	line, _ := caret.GetPosition()
	top := b.NewEphemeralCursor()
	top.SeekLineRune(line, 0)
	bottom := b.NewEphemeralCursor()
	bottom.SeekLineRune(line, 0)
	bottom.SeekLineEnd()
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

// frontierRow is the highest materialized row index (0-based): bottom sits on
// its line, top on row 0's, so the count of materialized rows is their line
// delta plus one.
func (m *liveMirror) frontierRow() int {
	tl, _ := m.top.GetPosition()
	bl, _ := m.bottom.GetPosition()
	return bl - tl
}

// growTo materializes rows up to and including row y by inserting blank lines at
// the frontier. Newly grown rows start at the default pen; a caller that grows
// by carrying a pen across a newline fixes rowStartPen[y] afterwards.
func (m *liveMirror) growTo(y int) {
	for m.frontierRow() < y {
		m.bottom.SeekLineEnd()
		m.bottom.InsertString("\n", nil, false)
		m.rowStartPen = append(m.rowStartPen, "")
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
	tl, _ := m.top.GetPosition()
	line := tl + y
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
		m.curPen = m.rowPen(y)
	}
}

// Write folds an OnWrite run into the mirror at (x, y) as a SINGLE garland
// mutation — the caret is left pinned on the frontier so a stream of adjacent
// runs coalesces into one undo step, exactly as the raw rung's chunk inserts do.
// A pen change (full format) is prepended to the same chunk as an absolute SGR.
func (m *liveMirror) Write(x, y int, text, sgr string) {
	if x != m.curX || y != m.curY {
		m.seekTo(x, y)
	}
	payload := text
	if m.format == captureFull && sgr != m.curPen {
		// One absolute SGR rides at the head of the chunk. At the frontier there
		// is nothing to the right to restore; the jump-back recolour (restore
		// after the chunk) is the 2b refinement.
		payload = sgr + text
		m.curPen = sgr
		if x == 0 {
			m.setRowPen(y, sgr)
		}
	}
	w := runsWidth(text)
	del := m.spanBytes(w) // bytes of the existing cells this run overtypes (0 at EOL)
	if del == 0 {
		m.caret.InsertString(payload, nil, false) // append/insert; caret advances past it
	} else {
		// OverwriteBytes replaces the del bytes and, when payload is longer,
		// grows in place — so a run that overtypes and then spills past the old
		// EOL is still one op. The caret does not move, so advance it past what we
		// wrote to stay on the frontier.
		m.caret.Overwrite(del, payload)
		m.caret.SeekRelativeRunes(len([]rune(payload)))
	}
	m.curX += w
}

// spanBytes returns the byte length of the existing cells at the caret spanning
// w visible columns — what an overtype of width w must replace — reading through
// the scratch cursor. Inline SGR runs in the span count toward the bytes (they
// are replaced) but not the width. It stops at end of line, returning fewer
// (0 at EOL): the caller's OverwriteBytes then grows past it.
func (m *liveMirror) spanBytes(w int) int {
	text, off := m.lineFromCaret()
	rs := []rune(text)
	if off > len(rs) {
		off = len(rs)
	}
	rs = rs[off:]
	vis, bytes, i := 0, 0, 0
	for i < len(rs) && vis < w {
		if rs[i] == '\n' {
			break
		}
		if rs[i] == 0x1b {
			if n := sgrRunLen(string(rs[i:])); n > 0 {
				bytes += len(string(rs[i : i+n]))
				i += n
				continue
			}
		}
		bytes += len(string(rs[i]))
		vis += textwidth.Rune(rs[i])
		i++
	}
	return bytes
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

// ScrollLineOff slides the window down n rows: top advances so the departed rows
// become history above, and the per-row pen array drops them. The frontier is
// regrown by the newline that follows the scroll.
func (m *liveMirror) ScrollLineOff(n int) {
	for i := 0; i < n; i++ {
		m.top.SeekLineEnd()
		m.top.SeekRelativeRunes(1)
		if len(m.rowStartPen) > 1 {
			m.rowStartPen = m.rowStartPen[1:]
		}
		if m.curY > 0 {
			m.curY--
		}
	}
}

// ClearScreen preserves the current screen as history and starts a fresh region
// below it (OnClearScreen), consistent with the scroll model: nothing is
// deleted. A new blank row 0 is opened past the frontier and the region resets
// onto it.
func (m *liveMirror) ClearScreen() {
	m.bottom.SeekLineEnd()
	m.bottom.InsertString("\n", nil, false)
	line, _ := m.bottom.GetPosition()
	m.top.SeekLineRune(line, 0)
	m.bottom.SeekLineRune(line, 0)
	m.bottom.SeekLineEnd()
	m.caret.SeekLineRune(line, 0)
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
