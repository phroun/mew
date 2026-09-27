package editor

import (
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
	"github.com/phroun/pawscript"
)

// Caret motion: by character, line, word, page and buffer, to a line number, and
// around the cursor ring, with the ideal column that vertical movement keeps.

// moveCursor moves the cursor by delta amounts.
func (e *Editor) moveCursor(dx, dy int) {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	if dy != 0 {
		// Establish the ideal column from the source line before moving.
		e.ensureIdealColumn(w)

		newLine := w.CursorPos().Line + dy
		if newLine < 0 {
			newLine = 0
		}
		maxLine := w.Buffer.GetLineCount() - 1
		if newLine > maxLine {
			newLine = maxLine
		}
		w.SetCursorLine(newLine)

		// Apply ghost cursor logic after vertical movement
		e.afterVerticalMovement(w)
	}

	if dx != 0 {
		newRune := w.CursorPos().Rune + dx
		lineLen := e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line)

		if newRune < 0 {
			// Move to end of prior line
			if w.CursorPos().Line > 0 {
				w.SetCursorLine(w.CursorPos().Line - 1)
				w.SetCursorRune(e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line))
			} else {
				newRune = 0
			}
		} else if newRune > lineLen {
			// Move to start of next line
			if w.CursorPos().Line < w.Buffer.GetLineCount()-1 {
				w.SetCursorLine(w.CursorPos().Line + 1)
				w.SetCursorRune(0)
			}
		} else {
			w.SetCursorRune(newRune)
		}

		// Update ideal column after horizontal movement
		e.afterHorizontalMovement(w)
	}

	// A horizontal move locks in the column and follows it; a bare vertical move
	// only follows vertically, leaving the horizontal view (and the ghost column)
	// where it is until a horizontal/locking action.
	if dx != 0 {
		e.ensureCursorVisible(w)
	} else {
		e.ensureCursorVisibleVertical(w)
	}
}

// ensureIdealColumn establishes the ideal visual column from the cursor's
// current position if it has not been set yet. It must be called BEFORE the
// cursor's Line is changed for a vertical move, so the ideal is computed from
// the source line (computing it after the move would pair the destination
// line's content with the source rune index and mis-handle tabs).
func (e *Editor) ensureIdealColumn(w *viewport.Viewport) {
	if w == nil || w.Buffer == nil {
		return
	}
	if w.IdealVisualColumn != 0 || w.CursorPos().Rune == 0 {
		return
	}
	tabSize := e.tabSize(w)
	line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
	w.IdealVisualColumn = e.storedIdealColumn(w, line, w.CursorPos().Rune, tabSize)
}

// idealDenominator is how many NORMAL screen columns one column of a
// document line's own space covers: 2 on a double-width (DECDWL heading)
// line, 1 everywhere else.
func (e *Editor) idealDenominator(w *viewport.Viewport, docLine int) int {
	if w == nil || w.Buffer == nil || docLine < 0 || docLine >= w.Buffer.GetLineCount() {
		return 1
	}
	if _, dw := e.lineDisplaySpans(w, docLine); dw {
		return 2
	}
	return 1
}

// storedIdealColumn is the sticky column to REMEMBER for the caret at runePos
// on line: the caret's column in the line as DISPLAYED, converted to normal
// screen columns.
//
// Display space, because that is where the caret is painted — a browse-mode
// heading's "======" markers and a link's target text take no columns of their
// own, so measuring the raw line would remember a column the caret never sat
// at. Normal screen columns, because a double-width line's space is half as
// dense (see idealDenominator); applyIdealColumn undoes both.
func (e *Editor) storedIdealColumn(w *viewport.Viewport, line string, runePos, tabSize int) int {
	dispLine, dispRune := e.displayCaretLine(w, line, runePos)
	return e.idealColumn(w, dispLine, dispRune, tabSize) *
		e.idealDenominator(w, w.CursorPos().Line)
}

// applyIdealColumn converts the stored ideal into the caret line's own display
// column space — the space every column computation in afterVerticalMovement
// works in, and the space the renderer paints the ghost cursor in.
func (e *Editor) applyIdealColumn(w *viewport.Viewport) int {
	return w.IdealVisualColumn / e.idealDenominator(w, w.CursorPos().Line)
}

// afterVerticalMovement applies ghost cursor logic after up/down movement.
// It tries to position cursor at the ideal visual column, showing a ghost
// cursor if the line is shorter than the ideal. Callers must establish the
// ideal column (via ensureIdealColumn) before changing the cursor's line.
func (e *Editor) afterVerticalMovement(w *viewport.Viewport) {
	if w == nil || w.Buffer == nil {
		return
	}

	// Normal vertical movement is not link nav: drop any vertical nav ideal
	// (navVert saves/restores it around its own paging fallback).
	w.NavIdealSet = false

	tabSize := e.tabSize(w)

	// Get line content (without trailing newline), as DISPLAYED: every column
	// below — the reach checks, the landing position, and the ghost's own
	// column, which the renderer paints in this same space — is a display
	// column. place() maps a landing back to the document rune it came from.
	raw := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
	line, _ := e.displayCaretLine(w, raw, w.CursorPos().Rune)
	lineLen := len([]rune(line))
	place := func(dispRune int) {
		w.SetCursorRune(e.displayCaretDoc(w, raw, dispRune))
	}

	// The stored ideal is in NORMAL screen columns; this line's own column
	// space is half as dense when it paints double-width, so divide in.
	ideal := e.applyIdealColumn(w)

	// direction=rtl: the view is right-anchored, so the ideal is a READING
	// column (distance back from the reading start). Placement and the ghost
	// mirror the LTR logic below, but in reading space so screen X is held.
	if e.winRTL(w) {
		idealReading := ideal
		vw := e.lineVisualWidth(w, line, tabSize)
		if lineLen == 0 {
			// Empty line: the caret can only sit at the reading start; a
			// non-trivial ideal reads as "past the end", so ghost it.
			place(0)
			w.HasGhostCursor = idealReading > 0
			if w.HasGhostCursor {
				w.GhostCursorVisualColumn = idealReading
			} else {
				w.GhostCursorVisualColumn = 0
			}
			return
		}
		// The furthest-back reachable reading column belongs to the line's
		// LEFTMOST caret position. That is end-of-line only when the line
		// ENDS in RTL text; a right-anchored line ending in an LTR fragment
		// has its EOL caret at the reading-START side (reading 0), and its
		// furthest-back caret inside the line — measuring EOL here read every
		// such line as "too short" and ghosted columns it actually reaches.
		farCol, farPos := e.leftmostCaret(w, line, tabSize)
		if idealReading > vw-farCol {
			// Line does not reach that far back — ghost past its reading
			// end, with the real caret parked at the reachable position
			// nearest the ghost.
			place(farPos)
			w.HasGhostCursor = true
			w.GhostCursorVisualColumn = idealReading
			return
		}
		// Map the reading column to a left-based visual column and land on the
		// covering rune. A mismatch (short line / inside a wide cell) ghosts.
		result := e.visualColumnToRuneWithActual(w, line, vw-idealReading, tabSize)
		place(result.Rune)
		if vw-result.ActualColumn != idealReading {
			w.HasGhostCursor = true
			w.GhostCursorVisualColumn = idealReading
		} else {
			w.HasGhostCursor = false
			w.GhostCursorVisualColumn = 0
		}
		return
	}

	// Calculate the maximum visual column for this line (end of line position)
	maxVisualColumn := e.runeToVisualColumn(w, line, lineLen, tabSize)

	if maxVisualColumn < ideal {
		// Line is shorter than ideal visual column - show ghost cursor at end
		place(lineLen)
		w.HasGhostCursor = true
		w.GhostCursorVisualColumn = ideal
	} else {
		// Line is long enough - position at the rune that corresponds to ideal visual column
		result := e.visualColumnToRuneWithActual(w, line, ideal, tabSize)
		place(result.Rune)

		// Check if we landed inside a wide character (like a tab)
		// If the actual column differs from ideal, we're inside a tab stop
		if result.ActualColumn != ideal {
			w.HasGhostCursor = true
			w.GhostCursorVisualColumn = ideal
		} else {
			w.HasGhostCursor = false
			w.GhostCursorVisualColumn = 0
		}
	}
}

// syncCursorAfterUndoRedo moves the editor cursor to the post-undo position
// garland slid the viewport's caret to, clamps it, and refreshes derived state.
func (e *Editor) syncCursorAfterUndoRedo(w *viewport.Viewport) {
	if w == nil || w.Buffer == nil || w.Caret == nil {
		return
	}
	e.clampCursorToBuffer(w)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.RequestRender()
}

// afterHorizontalMovement clears ghost cursor and updates ideal column.
func (e *Editor) afterHorizontalMovement(w *viewport.Viewport) {
	if w == nil {
		return
	}

	// Clear ghost cursor
	w.HasGhostCursor = false
	w.GhostCursorVisualColumn = 0
	w.NavIdealSet = false // a horizontal move re-anchors any vertical nav ideal

	// Update ideal column to current visual position (reading column in RTL).
	if w.Buffer != nil {
		tabSize := e.tabSize(w)
		line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
		w.IdealVisualColumn = e.storedIdealColumn(w, line, w.CursorPos().Rune, tabSize)
	} else {
		w.IdealVisualColumn = w.CursorPos().Rune
	}
}

// cursorToLineStart moves cursor to start of line.
func (e *Editor) cursorToLineStart() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil {
		return
	}
	w.SetCursorRune(0)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// cursorToLineEnd moves cursor to end of line.
func (e *Editor) cursorToLineEnd() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}
	lineLen := e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line)
	w.SetCursorRune(lineLen)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// cursorToBufferStart moves cursor to beginning of buffer (line 0, rune 0).
func (e *Editor) cursorToBufferStart() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// cursorToBufferEnd moves cursor to end of buffer (last line, last rune).
func (e *Editor) cursorToBufferEnd() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}
	lastLine := w.Buffer.GetLineCount() - 1
	if lastLine < 0 {
		lastLine = 0
	}
	w.SetCursorPos(viewport.Position{Line: lastLine, Rune: e.getEffectiveLineLen(w.Buffer, lastLine)})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// gotoLine moves cursor to a specific line number (1-based).
func (e *Editor) gotoLine(lineNum int) {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	// Convert from 1-based (user input) to 0-based (internal)
	targetLine := lineNum - 1

	// Clamp to valid range
	if targetLine < 0 {
		targetLine = 0
	}
	maxLine := w.Buffer.GetLineCount() - 1
	if targetLine > maxLine {
		targetLine = maxLine
	}

	w.SetCursorPos(viewport.Position{Line: targetLine, Rune: 0})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// scrollLineTop parks line lineNum (1-based) at the top of the target viewport's
// viewport without moving the caret — the scroll analogue of gotoLine.
func (e *Editor) scrollLineTop(lineNum int) {
	e.scrollViewTo(e.resolveTargetMain(), lineNum-1)
}

// cursorToTop moves cursor to start of buffer.
func (e *Editor) cursorToTop() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil {
		return
	}
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// cursorToBottom moves cursor to end of buffer.
func (e *Editor) cursorToBottom() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}
	w.SetCursorPos(viewport.Position{Line: w.Buffer.GetLineCount() - 1, Rune: e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line)})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// pageUp moves up by a page.
func (e *Editor) pageUp() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil {
		return
	}
	_, pageSize := e.pageSize(w)
	e.ensureIdealColumn(w)
	w.SetCursorLine(w.CursorPos().Line - pageSize) // setter clamps to 0
	e.afterVerticalMovement(w)

	// Scroll the viewport up by the same page so the caret keeps its relative
	// screen row, clamped to the top. Never scroll down here.
	w.RefreshViewTop()
	top := w.ViewState.ViewOffsetY - pageSize
	if top < 0 {
		top = 0
	}
	if top < w.ViewState.ViewOffsetY {
		w.SetViewTop(top)
	}
	e.ensureCursorVisibleVertical(w) // guarantee the caret is visible
}

// pageDown moves down by a page.
func (e *Editor) pageDown() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}
	viewHeight, pageSize := e.pageSize(w)
	e.ensureIdealColumn(w)
	w.SetCursorLine(w.CursorPos().Line + pageSize) // setter clamps to last line
	e.afterVerticalMovement(w)

	// Scroll the viewport down by the same page so the caret keeps its relative
	// screen row. Two caps: don't scroll so far that more than one blank line
	// shows past the end of the buffer (making the end obvious), and never
	// rewind the viewport upward — if it is already further down, leave it. The
	// blank-line cap uses the VIEW height, not the page distance.
	w.RefreshViewTop()
	oldTop := w.ViewState.ViewOffsetY
	// maxTop puts the last line on the second-to-last row, leaving exactly one
	// blank row below it. Negative when the buffer is shorter than the view, in
	// which case there is nothing to scroll.
	maxTop := w.Buffer.GetLineCount() - viewHeight + 1
	if maxTop < 0 {
		maxTop = 0
	}
	top := oldTop + pageSize
	if top > maxTop {
		top = maxTop
	}
	if top < oldTop {
		top = oldTop // never rewind upward
	}
	w.SetViewTop(top)
	e.ensureCursorVisibleVertical(w) // guarantee the caret is visible
}

// pageSize returns the viewport's view height and the configured page distance
// (evaluated against that height). The height falls back to a default when it
// is not yet known.
func (e *Editor) pageSize(w *viewport.Viewport) (viewHeight, page int) {
	viewHeight = w.ContentHeight
	if viewHeight < 1 {
		viewHeight = 20
	}
	return viewHeight, e.pageSizeSpec.eval(viewHeight)
}

// rebuildPageSizeSpec re-derives the paging spec after any of the three page
// options changes.
func (e *Editor) rebuildPageSizeSpec() {
	e.pageSizeSpec = buildPageSizeSpec(e.Config.PageSizeOptimal, e.Config.PageOverlapMinimum, e.Config.PageSizeStep)
}

// trackEdit records a caret-area edit on the focused viewport's cursor ring, run
// after an editing command has completed. A no-op when there is no ring (e.g. a
// prompt viewport). See Viewport.TrackEdit. It also shifts the kill-chain flag:
// lastEditKill becomes true only when this edit was a kill capture, so a
// non-kill edit (typing, paste) between deletes breaks the accumulation.
func (e *Editor) trackEdit() {
	if w := e.ViewportManager.GetFocusedViewport(); w != nil {
		w.TrackEdit()
		// An edit re-engages caret following (edits also call ensureCursorVisible,
		// but not every path does; make the re-engage unconditional here).
		w.ViewState.ScrollDetached = false
		// The mew lock is lazy: the first edit that leaves the buffer modified
		// claims the deferred lock (matching garland's emacs locks). A no-op once
		// held, or for a buffer that was never opened through the lock path. Runs
		// before checkEditLock so any foreign lock it records is ready to prompt.
		if w.Buffer != nil && w.Buffer.IsModified() {
			e.ensureDeferredMewLock(w.Buffer)
		}
	}
	e.lastEditKill = e.pendingKill
	e.pendingKill = false
	e.checkEditLock()
}

// trackMove records a deliberate caret movement on the focused viewport's cursor
// ring, run after a movement command has completed. See Viewport.TrackMove.
func (e *Editor) trackMove() {
	if w := e.ViewportManager.GetFocusedViewport(); w != nil {
		w.TrackMove()
	}
}

// cursorRingGo walks the caret one step through the cursor ring — forward
// (newer) when next is true, backward (older) otherwise — and brings it into
// view. Returns false, leaving the caret put, when there is nowhere further to
// go in that direction.
func (e *Editor) cursorRingGo(next bool) bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	var pos int64
	var ok bool
	if next {
		pos, ok = w.CursorRingNext()
	} else {
		pos, ok = w.CursorRingPrior()
	}
	if !ok {
		return false
	}
	w.SeekCaretByte(pos)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	return true
}

// setCaret parks the caret at a line and rune position.
func (e *Editor) setCaret(w *viewport.Viewport, line, runePos int) {
	w.SetCursorPos(viewport.Position{Line: line, Rune: runePos})
}

// clampCursorToBuffer ensures the cursor position is within valid buffer bounds.
func (e *Editor) clampCursorToBuffer(w *viewport.Viewport) {
	if w == nil || w.Buffer == nil {
		return
	}

	lineCount := w.Buffer.GetLineCount()

	// Clamp line
	if w.CursorPos().Line < 0 {
		w.SetCursorLine(0)
	}
	if w.CursorPos().Line >= lineCount {
		w.SetCursorLine(lineCount - 1)
	}

	// Clamp rune within line (using effective length without trailing newline)
	lineLen := e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line)
	if w.CursorPos().Rune < 0 {
		w.SetCursorRune(0)
	}
	if w.CursorPos().Rune > lineLen {
		w.SetCursorRune(lineLen)
	}
}

// getEffectiveLineLen returns the length of a line without trailing newline/CR.
// Garland's GetLine includes the line terminator, but for cursor positioning
// we need the length without it.
func (e *Editor) getEffectiveLineLen(buf *buffer.Buffer, lineNum int) int {
	line := buf.GetLine(lineNum)
	line = strings.TrimRight(line, "\n\r")
	return len([]rune(line))
}

// isWhitespace returns true if the rune is whitespace.
func isWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

// isWordRune returns true if the rune is part of a word.
func isWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// moveToNextWord moves cursor to the next word.
func (e *Editor) moveToNextWord() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	line := w.Buffer.GetLine(w.CursorPos().Line)
	runes := []rune(line)
	runePos := w.CursorPos().Rune

	// Skip current word if we're in one
	for runePos < len(runes) && isWordRune(runes[runePos]) {
		runePos++
	}

	// Skip non-word runes
	for runePos < len(runes) && !isWordRune(runes[runePos]) {
		runePos++
	}

	// If we reached end of line and not the last line, go to next line
	if runePos >= len(runes) && w.CursorPos().Line < w.Buffer.GetLineCount()-1 {
		w.SetCursorLine(w.CursorPos().Line + 1)
		w.SetCursorRune(0)
	} else {
		w.SetCursorRune(runePos)
	}

	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// moveToPriorWord moves cursor to the prior word.
func (e *Editor) moveToPriorWord() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	// If at beginning of line and not first line, go to end of prior line
	if w.CursorPos().Rune == 0 && w.CursorPos().Line > 0 {
		w.SetCursorLine(w.CursorPos().Line - 1)
		w.SetCursorRune(e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line))
		e.afterHorizontalMovement(w)
		e.ensureCursorVisible(w)
		return
	}

	line := w.Buffer.GetLine(w.CursorPos().Line)
	runes := []rune(line)
	runePos := w.CursorPos().Rune - 1

	// Skip non-word runes backwards
	for runePos >= 0 && !isWordRune(runes[runePos]) {
		runePos--
	}

	// Find beginning of word
	for runePos >= 0 && isWordRune(runes[runePos]) {
		runePos--
	}

	// Position at start of word
	w.SetCursorRune(runePos + 1)

	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// registerMotionCommands registers the caret-motion (go_*) commands.
func (e *Editor) registerMotionCommands(ps *pawscript.PawScript) {
	// Movement commands (using TypeScript naming convention)
	ps.RegisterCommand("go_char_prior", func(ctx *pawscript.Context) pawscript.Result {
		e.moveCursor(-1, 0)
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_char_next", func(ctx *pawscript.Context) pawscript.Result {
		e.moveCursor(1, 0)
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_line_prior", func(ctx *pawscript.Context) pawscript.Result {
		e.moveCursor(0, -1)
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_line_next", func(ctx *pawscript.Context) pawscript.Result {
		e.moveCursor(0, 1)
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_line_beg", func(ctx *pawscript.Context) pawscript.Result {
		e.cursorToLineStart()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_line_end", func(ctx *pawscript.Context) pawscript.Result {
		e.cursorToLineEnd()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_buffer_beg", func(ctx *pawscript.Context) pawscript.Result {
		e.cursorToBufferStart()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_buffer_end", func(ctx *pawscript.Context) pawscript.Result {
		e.cursorToBufferEnd()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_page_prior", func(ctx *pawscript.Context) pawscript.Result {
		e.pageUp()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_page_next", func(ctx *pawscript.Context) pawscript.Result {
		e.pageDown()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_word_prior", func(ctx *pawscript.Context) pawscript.Result {
		e.moveToPriorWord()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_word_next", func(ctx *pawscript.Context) pawscript.Result {
		e.moveToNextWord()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	// go_pos_prior / go_pos_next walk the caret backward and forward through the
	// cursor ring — the trail of recent edit positions. They do not themselves
	// count as deliberate movements (they leave hasMoved untouched), so a run of
	// them stays a single navigation session until an edit or a real move.
	ps.RegisterCommand("go_pos_prior", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.cursorRingGo(false))
	})

	ps.RegisterCommand("go_pos_next", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.cursorRingGo(true))
	})

	// Go to line command. go_line [n] goes directly; without an argument it
	// prompts, with history reachable by arrow but never defaulted (the
	// prompt starts blank). An invalid entry warns "Invalid line number" and
	// the command resolves false — the prompt suspends the calling command
	// sequence on an async token and resumes it with the outcome, so a
	// script can chain an alternative with the | else operator. Cancelling
	// or accepting a blank entry also resolves false, without the warning.
	ps.RegisterCommand("go_line", func(ctx *pawscript.Context) pawscript.Result {
		goLine := func(input string) bool {
			n, err := strconv.Atoi(strings.TrimSpace(input))
			if err != nil || n < 1 {
				e.ShowWarning("Invalid line number")
				return false
			}
			e.gotoLine(n)
			return true
		}
		if arg, ok := argString(ctx, 0); ok {
			return pawscript.BoolStatus(goLine(arg))
		}
		// PawScript force-cleans suspension tokens after the promptTimeout
		// option (seconds, 0 = never), dropping the suspended sequence; the
		// cleanup callback records that. A prompt answered after its token
		// expired defaults to FAILURE: warn and perform nothing, rather
		// than half-succeeding (jumping) with the command's chain already
		// dead.
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		e.PromptMgr.PromptForInput("Go to line: ", "", func(accepted bool, _, input string) {
			defer e.RequestRender()
			if expired.Load() {
				e.ShowWarning("Prompt timed out")
				return
			}
			if !accepted || strings.TrimSpace(input) == "" {
				ctx.ResumeToken(token, false)
				return
			}
			ctx.ResumeToken(token, goLine(input))
		}, "goline")
		return pawscript.TokenResult(token)
	})
}
