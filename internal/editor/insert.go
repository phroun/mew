package editor

import (
	"strings"

	"github.com/phroun/mew/internal/viewport"
)

// replacePrior stands text in place of the n characters immediately before the
// caret, as ONE mutation.
//
// One matters. Picking an accent from the press-and-hold palette is a single
// user action, and the state between its halves — the letter gone, the accented
// one not yet arrived — is one nobody ever saw on screen. Caret.Overwrite is a
// single overwrite mutation in the buffer's history, so undo steps from the
// accented character straight back to the plain one.
//
// n is clamped to what is actually on this line before the caret, so the
// replacement can never cross a line boundary. It never has to: the character a
// palette replaces is the one its own key just typed.
func (e *Editor) replacePrior(n int, text string) bool {
	if e.contentLocked() {
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	pos := w.CursorPos()
	if n > pos.Rune {
		n = pos.Rune
	}
	if n == 0 {
		// Nothing to stand in for: an ordinary insert, and it keeps the
		// coalescing an ordinary insert has.
		e.insertText(text)
		e.trackEdit()
		e.editCoalesced = true
		return true
	}

	runes := []rune(strings.TrimRight(w.Buffer.GetLine(pos.Line), "\n\r"))
	if pos.Rune > len(runes) {
		return false
	}
	replaced := len(string(runes[pos.Rune-n : pos.Rune]))

	w.Buffer.BeginUserCommand("replace_prior")
	w.Caret.Seek(pos.Line, pos.Rune-n)
	w.Caret.Overwrite(int64(replaced), text)
	// Overwrite leaves the caret where it was told to start, so the caller
	// advances it — to the far side of what it just wrote.
	w.Caret.Seek(pos.Line, pos.Rune-n+len([]rune(text)))
	w.Buffer.EndUserCommand()

	e.trackEdit()
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	return true
}

// preeditCommit puts text where the standing composition stood, and ends it.
//
// The composition's own position is what is replaced. It was recorded when the
// composition opened, so it survives the caret moving on — which it does
// whenever a palette is dismissed by typing, the keystroke landing before the
// input method's commit catches up.
//
// Text the composition never covered is left exactly where it is, including
// that keystroke: the caret ends up after it, where the person typing put it.
func (e *Editor) preeditCommit(text string) bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil {
		return false
	}
	p := w.Preedit()
	line, from, anchored := w.PreeditAt()
	w.ClearPreedit()

	// A viewport running a child process has no document to place this in; the
	// text goes to the child, as replace_prior's does.
	if e.focusedPTY() != nil {
		if p.Covers > 0 {
			e.ptyEraseBefore(p.Covers)
		}
		return e.ptySendBytes([]byte(text))
	}
	if !anchored || p.Covers == 0 {
		// Nothing was stood over, so there is nothing to stand in for.
		return e.replacePrior(0, text)
	}

	pos := w.CursorPos()
	// Where the caret sits relative to the region, so it can be put back after
	// the text under it changes length. A caret inside the region lands at the
	// end of what replaced it; one after the region keeps its distance from it.
	trailing := 0
	if pos.Line == line && pos.Rune > from+p.Covers {
		trailing = pos.Rune - (from + p.Covers)
	}

	e.setCaret(w, line, from+p.Covers)
	if !e.replacePrior(p.Covers, text) {
		return false
	}
	after := w.CursorPos()
	e.setCaret(w, after.Line, after.Rune+trailing)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	return true
}

// insertBidiControl inserts the bidi control named by name, behaving exactly
// like the insert command (insert the text, track the edit). Reports false on
// an unknown name.
func (e *Editor) insertBidiControl(name string) bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	r, ok := bidiControlRune(name)
	if !ok {
		e.ShowWarning("Unknown control mark: " + name)
		return false
	}
	e.insertText(string(r))
	e.trackEdit()
	e.editCoalesced = true // a single-point edit: coalesce the undo run
	return true
}

// insertRuneAt inserts a single scalar, sharing insertBidiControl's edit
// shape: refused on locked content, one coalesced undo step.
func (e *Editor) insertRuneAt(r rune) bool {
	if e.contentLocked() {
		return false
	}
	e.insertText(string(r))
	e.trackEdit()
	e.editCoalesced = true
	return true
}

// insertRawByteAt inserts ONE byte, exactly the byte asked for. 0x80..0xFF go
// in as that single byte and NOT as the Latin-1 scalar of the same number,
// which would be two bytes of UTF-8 and a different file on disk.
//
// This does mean a line can hold a byte sequence that is not valid UTF-8, and
// the editor's rune-indexed columns will read it as whatever Go's decoder makes
// of it. That is the deal: a command called "insert raw byte" that quietly
// inserted a different byte would be worthless for the job it exists to do -
// patching a binary, embedding a control code, fixing an encoding by hand. The
// consequences belong to the user who asked for the byte.
func (e *Editor) insertRawByteAt(b byte) bool { return e.insertRawBytesAt([]byte{b}) }

// insertRawBytesAt inserts a byte SEQUENCE verbatim, for a PawScript
// {bytes ...} value. Garland stores bytes (insertStringAt is []byte(data) with
// no re-encoding), so what reaches the buffer is exactly what was asked for.
func (e *Editor) insertRawBytesAt(b []byte) bool {
	if e.contentLocked() || len(b) == 0 {
		return false
	}
	e.insertText(string(b))
	e.trackEdit()
	e.editCoalesced = true
	return true
}

// insertText inserts text at the cursor position.
func (e *Editor) insertText(text string) { e.insertTextMode(text, true) }

// insertTextMode is insertText with control over whether overwrite mode applies.
// honorOverwrite=false forces a true insert, for synthetic text that must never
// consume what is already there — an auto-indent's whitespace, which would
// otherwise overwrite the very characters the line break just moved down.
func (e *Editor) insertTextMode(text string, honorOverwrite bool) {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	// Ensure cursor is within valid bounds before insertion
	e.clampCursorToBuffer(w)

	if honorOverwrite && w.ViewState.OverwriteMode {
		// Overwrite mode: typing replaces the character under the caret.
		e.overwriteText(w, text)
	} else {
		// Insert through the viewport's own caret cursor, then read the caret back:
		// garland advances it past the inserted text (splitting on embedded
		// newlines internally), so there is no manual line/rune arithmetic.
		w.Caret.Seek(w.CursorPos().Line, w.CursorPos().Rune)
		w.Caret.Insert(text)
	}

	// Clear ghost cursor and update ideal column after typing
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.RequestRender()
}

// insertNewline is the insert_newline command — the default Enter binding's
// last resort, after nav_follow and accept. It breaks the line exactly as
// `insert '\n'` does and, when the autoIndent option is on for the viewport,
// follows the break with the indent autoIndentPrefix computes, so the new line
// starts under the text of the one it split.
//
// The whole thing is ONE insert (the break and the whitespace together), so it
// is one undo step and one coalescible edit — except in overwrite mode, where
// the break goes through the overwrite path (which appends it, as overwriting a
// line break must) and the indent is force-inserted after it: overwriting with
// the indent would consume the characters the break just moved down.
func (e *Editor) insertNewline() bool {
	if e.contentLocked() {
		// Read-only viewport, or a focused link button: reject at the source,
		// the same way insertText would, before any of the below runs.
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	indent := e.autoIndentPrefix(w)
	if indent == "" {
		e.insertText("\n")
		return true
	}
	if w.ViewState.OverwriteMode {
		e.insertText("\n")
		e.insertTextMode(indent, false)
		return true
	}
	e.insertTextMode("\n"+indent, false)
	return true
}

// autoIndentPrefix returns the whitespace a new line should open with when the
// autoIndent option is on for w: the leading whitespace of the line the caret
// is about to split, replicated exactly — tabs stay tabs, spaces stay spaces,
// and a mixed indent survives as it was written.
//
// The run is truncated at the caret, which is what keeps Enter from pushing
// text rightward when it lands inside or before the indent: at column 0 of an
// indented line nothing is replicated (the line moves down whole, keeping its
// own indent), and from within the indent only the part already above the
// caret repeats, so the split halves still line up under the original.
func (e *Editor) autoIndentPrefix(w *viewport.Viewport) string {
	if w == nil || w.Buffer == nil || !w.ViewState.AutoIndent {
		return ""
	}
	pos := w.CursorPos()
	if pos.Line < 0 || pos.Line >= w.Buffer.GetLineCount() {
		return ""
	}
	line := []rune(strings.TrimRight(w.Buffer.GetLine(pos.Line), "\n\r"))
	n := 0
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	if pos.Rune < n {
		n = pos.Rune
	}
	if n <= 0 {
		return ""
	}
	return string(line[:n])
}

// overwriteText types text in overwrite mode: each rune replaces the character
// under the caret via garland's overwrite mutation, which coalesces a run of
// overwrites into one undo step (like typing and deleting do). At (or crossing)
// end of line — and for a newline, which splits the line — it switches to a
// plain insert so the text appends; garland lets that appending insert continue
// the overwrite run, so overtype-then-append stays a single undo step. The
// overwritten character is discarded (not sent to the kill ring), matching how
// typing over a selection works.
func (e *Editor) overwriteText(w *viewport.Viewport, text string) {
	for _, r := range text {
		pos := w.CursorPos()
		if r == '\n' || pos.Rune >= e.getEffectiveLineLen(w.Buffer, pos.Line) {
			// End of line reached (or a line break): append via insert.
			w.Caret.Seek(pos.Line, pos.Rune)
			w.Caret.Insert(string(r))
			continue
		}
		// Replace the rune under the caret, then advance past what we wrote so a
		// continuing overwrite (or the appending insert at EOL) lands at the
		// run's end.
		byteLen := e.runeByteLenAt(w, pos.Line, pos.Rune)
		w.Caret.Seek(pos.Line, pos.Rune)
		w.Caret.Overwrite(int64(byteLen), string(r))
		w.Caret.Seek(pos.Line, pos.Rune+1)
	}
}

// runeByteLenAt returns the UTF-8 byte length of the rune at (line, rune), or 0
// if the position is at or past end of line (no rune stands there).
func (e *Editor) runeByteLenAt(w *viewport.Viewport, line, rune_ int) int {
	content := strings.TrimRight(w.Buffer.GetLine(line), "\n\r")
	runes := []rune(content)
	if rune_ < 0 || rune_ >= len(runes) {
		return 0
	}
	return len(string(runes[rune_]))
}

// insertPasteChunk inserts a single chunk of paste content.
// Called by the main loop as chunks arrive from the keyboard handler.
func (e *Editor) insertPasteChunk(content []byte) {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}
	// Bracketed paste arrives from the main loop, not executeCommand, but it is
	// still a content mutation: gate it at its own source.
	if e.contentLocked() {
		return
	}

	text := string(content)
	if text == "" {
		return
	}

	// Normalize line endings: \r\n -> \n, standalone \r -> \n
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	// Ensure cursor is within valid bounds before insertion
	e.clampCursorToBuffer(w)

	// Insert through the viewport's caret cursor and read it back.
	w.Caret.Seek(w.CursorPos().Line, w.CursorPos().Rune)
	w.Caret.Insert(text)

	// Record the paste in the cursor ring. Multi-chunk pastes call this per
	// chunk, but TrackEdit collapses them: the first chunk may push the prior
	// edit point, and subsequent chunks find hasMoved already cleared, so a
	// paste yields at most one ring entry. A paste is not a kill, so it breaks
	// any delete accumulation in progress.
	w.TrackEdit()
	e.lastEditKill = false
}
