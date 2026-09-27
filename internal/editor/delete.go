package editor

import (
	"strings"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
	"github.com/phroun/pawscript"
)

// Deletion: characters on either side of the caret, words, lines and parts of
// lines, and trimming whitespace from a line's ends.

// deleteWouldRemoveNewline reports whether a delete at the caret would remove a
// line terminator (join two lines) rather than an in-line rune. forward is the
// del_char_next direction; !forward is del_char_prior (backspace). It reads only
// the caret POSITION and line lengths — never moving the editing caret — so it
// cannot disturb the undo coalescing run. In-line deletes never target a
// newline; only the line-boundary branches of deleteCharBefore/deleteCharAt do,
// which this mirrors exactly.
func (e *Editor) deleteWouldRemoveNewline(w *viewport.Viewport, forward bool) bool {
	if w == nil || w.Buffer == nil {
		return false
	}
	pos := w.CursorPos()
	if forward {
		lineLen := e.getEffectiveLineLen(w.Buffer, pos.Line)
		return pos.Rune >= lineLen && pos.Line < w.Buffer.GetLineCount()-1
	}
	return pos.Rune == 0 && pos.Line > 0
}

// deleteCharBefore deletes the character before the cursor.
func (e *Editor) deleteCharBefore() {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	if w.CursorPos().Rune > 0 {
		// Delete the rune before the caret through the viewport's caret cursor,
		// which moves back with the deletion. Backspace kills prepend.
		w.Caret.Seek(w.CursorPos().Line, w.CursorPos().Rune)
		e.killCapture(w, w.Caret.DeleteBackwardCaptured(1), false)
	} else if w.CursorPos().Line > 0 {
		// Join with the prior line by deleting the terminator that ends it.
		// Position the caret at the end of the prior line's content and
		// delete the terminator runes forward: garland joins the lines and
		// slides every decoration and cursor across the seam. Cursor-relative
		// (a fresh seek, not a captured byte offset).
		priorRaw := w.Buffer.GetLine(w.CursorPos().Line - 1)
		priorLen := len([]rune(strings.TrimRight(priorRaw, "\n\r")))
		termRunes := len([]rune(priorRaw)) - priorLen // 1 for "\n", 2 for "\r\n"
		w.Caret.Seek(w.CursorPos().Line-1, priorLen)
		e.killCapture(w, w.Caret.DeleteForwardCaptured(termRunes), false)
	}

	// Clear ghost cursor and update ideal column after editing
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w) // edit locks in the horizontal view
}

// deleteCharAt deletes the character at the cursor.
func (e *Editor) deleteCharAt() {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	lineLen := e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line)
	if w.CursorPos().Rune < lineLen {
		// Delete the rune under the caret (forward delete keeps the caret put).
		// Forward kills append.
		w.Caret.Seek(w.CursorPos().Line, w.CursorPos().Rune)
		e.killCapture(w, w.Caret.DeleteForwardCaptured(1), true)
	} else if w.CursorPos().Line < w.Buffer.GetLineCount()-1 {
		// Join with the next line by deleting this line's terminator. The caret
		// is already at end-of-content (rune == lineLen); delete the terminator
		// runes forward so garland joins the lines and slides everything across.
		curRaw := w.Buffer.GetLine(w.CursorPos().Line)
		termRunes := len([]rune(curRaw)) - lineLen // 1 for "\n", 2 for "\r\n"
		w.Caret.Seek(w.CursorPos().Line, lineLen)
		e.killCapture(w, w.Caret.DeleteForwardCaptured(termRunes), true)
	}

	// Clear ghost cursor and update ideal column after editing
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w) // edit locks in the horizontal view
}

// deleteLine deletes the current line.
func (e *Editor) deleteLine() {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	e.killCapture(w, w.Buffer.DeleteLineCaptured(w.CursorPos().Line), true)
	if w.CursorPos().Line >= w.Buffer.GetLineCount() {
		w.SetCursorLine(w.Buffer.GetLineCount() - 1)
	}
	if w.CursorPos().Line < 0 {
		w.SetCursorLine(0)
	}
	w.SetCursorRune(0)

	// Clear ghost cursor and update ideal column after editing
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w) // edit locks in the horizontal view
}

// doFind searches for text in the current buffer.
// deleteWord deletes from cursor to end of current word.
func (e *Editor) deleteWord() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	line := w.Buffer.GetLine(w.CursorPos().Line)
	runes := []rune(line)
	startPos := w.CursorPos().Rune

	if startPos >= len(runes) {
		return
	}

	// Find end of word (skip non-whitespace, then skip whitespace)
	endPos := startPos
	// Skip non-whitespace
	for endPos < len(runes) && !isWhitespace(runes[endPos]) {
		endPos++
	}
	// Skip trailing whitespace
	for endPos < len(runes) && isWhitespace(runes[endPos]) {
		endPos++
	}

	if endPos > startPos {
		w.Buffer.DeleteText(w.CursorPos().Line, startPos, endPos-startPos)
	}
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w) // edit locks in the horizontal view
}

// deleteToWordStart deletes from cursor to beginning of word.
func (e *Editor) deleteToWordStart() {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	line := w.Buffer.GetLine(w.CursorPos().Line)
	line = strings.TrimRight(line, "\n\r")
	runes := []rune(line)

	// Nothing to delete if at start or line is empty
	if len(runes) == 0 || w.CursorPos().Rune <= 0 {
		return
	}

	endPos := w.CursorPos().Rune
	if endPos > len(runes) {
		endPos = len(runes)
	}
	runePos := endPos - 1

	// Skip non-word runes backwards (with bounds check)
	for runePos >= 0 && runePos < len(runes) && !isWordRune(runes[runePos]) {
		runePos--
	}

	// Find beginning of word (with bounds check)
	for runePos >= 0 && runePos < len(runes) && isWordRune(runes[runePos]) {
		runePos--
	}

	startPos := runePos + 1
	if startPos < endPos {
		e.killCapture(w, w.Buffer.DeleteTextCaptured(w.CursorPos().Line, startPos, endPos-startPos), false)
		w.SetCursorRune(startPos)
		// Clear ghost cursor and update ideal column after editing
		e.afterHorizontalMovement(w)
	}
}

// deleteToWordEnd deletes from cursor to end of word.
func (e *Editor) deleteToWordEnd() {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	line := w.Buffer.GetLine(w.CursorPos().Line)
	line = strings.TrimRight(line, "\n\r")
	runes := []rune(line)

	// Nothing to delete if line is empty
	if len(runes) == 0 {
		return
	}

	startPos := w.CursorPos().Rune
	if startPos >= len(runes) {
		return
	}
	runePos := startPos

	// Skip current word if we're in one
	for runePos < len(runes) && isWordRune(runes[runePos]) {
		runePos++
	}

	// Skip trailing non-word runes (whitespace etc)
	for runePos < len(runes) && !isWordRune(runes[runePos]) {
		runePos++
	}

	if runePos > startPos {
		e.killCapture(w, w.Buffer.DeleteTextCaptured(w.CursorPos().Line, startPos, runePos-startPos), true)
		// Clear ghost cursor and update ideal column after editing
		e.afterHorizontalMovement(w)
	}
}

// deleteToLineStart deletes from cursor to beginning of line.
func (e *Editor) deleteToLineStart() {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	if w.CursorPos().Rune > 0 {
		e.killCapture(w, w.Buffer.DeleteTextCaptured(w.CursorPos().Line, 0, w.CursorPos().Rune), false)
		w.SetCursorRune(0)
		// Clear ghost cursor and update ideal column after editing
		e.afterHorizontalMovement(w)
		e.ensureCursorVisible(w) // edit locks in the horizontal view
	}
}

// deleteToLineEnd deletes from cursor to end of line.
func (e *Editor) deleteToLineEnd() {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	lineLen := e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line)

	if w.CursorPos().Rune < lineLen {
		e.killCapture(w, w.Buffer.DeleteTextCaptured(w.CursorPos().Line, w.CursorPos().Rune, lineLen-w.CursorPos().Rune), true)
		// Clear ghost cursor and update ideal column after editing
		e.afterHorizontalMovement(w)
		e.ensureCursorVisible(w) // edit locks in the horizontal view
	}
}

// trimLineStart removes leading whitespace (spaces and tabs) from the
// current line, keeping the cursor on the same character where possible.
// Reports whether anything was removed.
func (e *Editor) trimLineStart() bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}

	line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
	runes := []rune(line)
	n := 0
	for n < len(runes) && (runes[n] == ' ' || runes[n] == '\t') {
		n++
	}
	if n == 0 {
		return false
	}

	// Garland slides the viewport caret with the deletion (back by n if it was
	// past the indent, collapsing to 0 if it was within it) — no manual adjust.
	w.Buffer.DeleteText(w.CursorPos().Line, 0, n)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	return true
}

// trimLineEnd removes trailing whitespace (spaces and tabs) from the current
// line's content. The line terminator itself is never touched — the line
// ends where it did, just without trailing whitespace before the newline.
// Reports whether anything was removed.
func (e *Editor) trimLineEnd() bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}

	line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
	runes := []rune(line)
	end := len(runes)
	for end > 0 && (runes[end-1] == ' ' || runes[end-1] == '\t') {
		end--
	}
	if end == len(runes) {
		return false
	}

	// Garland slides the viewport caret with the deletion (a caret inside the
	// trimmed run collapses to its start) — no manual adjust.
	w.Buffer.DeleteText(w.CursorPos().Line, end, len(runes)-end)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	return true
}

// registerDeleteCommands registers the del_* and trim_* commands.
func (e *Editor) registerDeleteCommands(ps *pawscript.PawScript) {
	// Editing commands (using TypeScript naming convention)
	ps.RegisterCommand("del_char_prior", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		// A backspace over a read-only buffer declines (false, no edit) but —
		// unlike every other mutation — WITHOUT the "Buffer is read-only" toast: a
		// key as ordinary as backspace should not nag. Other commands still warn.
		if w != nil && e.viewportReadOnly(w) {
			return pawscript.BoolStatus(false)
		}
		// When deleteNewlineAsChar is off for this viewport, a backspace at the
		// start of a line declines rather than joining it with the line above.
		// Fail with false and no visible error; no edit, so the undo coalescing run
		// is untouched.
		if w != nil && w.ViewState.ProtectNewlines && e.deleteWouldRemoveNewline(w, false) {
			return pawscript.BoolStatus(false)
		}
		e.deleteCharBefore()
		e.trackEdit()
		e.editCoalesced = true // a single-point edit: coalesce the undo run
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_char_next", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		// A forward-delete over a read-only buffer declines silently too — no
		// "Buffer is read-only" toast (see del_char_prior).
		if w != nil && e.viewportReadOnly(w) {
			return pawscript.BoolStatus(false)
		}
		// Same guard forward: a forward-delete at end of line declines rather than
		// pulling the next line up. No edit → coalescing untouched.
		if w != nil && w.ViewState.ProtectNewlines && e.deleteWouldRemoveNewline(w, true) {
			return pawscript.BoolStatus(false)
		}
		e.deleteCharAt()
		e.trackEdit()
		e.editCoalesced = true // a single-point edit: coalesce the undo run
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_line", func(ctx *pawscript.Context) pawscript.Result {
		e.deleteLine()
		e.trackEdit()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_word_beg", func(ctx *pawscript.Context) pawscript.Result {
		e.deleteToWordStart()
		e.trackEdit()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_word_end", func(ctx *pawscript.Context) pawscript.Result {
		e.deleteToWordEnd()
		e.trackEdit()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_line_beg", func(ctx *pawscript.Context) pawscript.Result {
		e.deleteToLineStart()
		e.trackEdit()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_line_end", func(ctx *pawscript.Context) pawscript.Result {
		e.deleteToLineEnd()
		e.trackEdit()
		return pawscript.BoolStatus(true)
	})

	// Whitespace trimming, mirroring the del_line_* family. These trim the
	// current line's leading/trailing spaces and tabs; the line terminator
	// itself is never removed. Each reports true only when something was
	// actually trimmed, so scripts can chain alternatives with | and &.
	ps.RegisterCommand("trim_line_beg", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.trimLineStart()
		e.trackEdit()
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("trim_line_end", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.trimLineEnd()
		e.trackEdit()
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("trim_line", func(ctx *pawscript.Context) pawscript.Result {
		// Trims both ends: two separate deletes that must undo as one step.
		var buf *buffer.Buffer
		if w := e.ViewportManager.GetFocusedViewport(); w != nil {
			buf = w.Buffer
		}
		if buf != nil {
			buf.BeginUserCommand("trim_line")
			defer buf.EndUserCommand()
		}
		trimmedStart := e.trimLineStart()
		trimmedEnd := e.trimLineEnd()
		e.trackEdit()
		return pawscript.BoolStatus(trimmedStart || trimmedEnd)
	})
}
