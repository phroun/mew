package editor

// OS-clipboard commands (os_copy / os_cut / os_paste / os_select_all): the
// bridge between mew's block selection and the HOST's system clipboard, wired
// through Config.ClipboardWrite / ClipboardRead by an embedding host (the
// KittyTK editor trinket routes its Edit menu and right-click context menu
// here via a HostPort). This channel is deliberately SEPARATE from mew's kill
// ring: os_cut removes the block with a plain (non-kill) delete, so the system
// clipboard and the kill ring never interfere with each other.

import (
	"strings"

	"github.com/phroun/mew/internal/window"
)

// osClipboardText returns the marked block's text from the focused window,
// or ok=false (with a warning shown) when there is no buffer or no block.
func (e *Editor) osClipboardText() (w *window.Window, text string, ok bool) {
	w = e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return nil, "", false
	}
	startLine, startRune, endLine, endRune, exists := w.Buffer.GetBlockRange()
	if !exists {
		e.ShowWarning("No block marked")
		return nil, "", false
	}
	return w, e.getBlockContent(w.Buffer, startLine, startRune, endLine, endRune), true
}

// osCopy places the marked block's text on the host clipboard.
func (e *Editor) osCopy() bool {
	if e.Config.ClipboardWrite == nil {
		e.ShowWarning("No host clipboard")
		return false
	}
	_, text, ok := e.osClipboardText()
	if !ok {
		return false
	}
	e.Config.ClipboardWrite(text)
	e.ShowNotification("Copied block to clipboard")
	return true
}

// osCut places the marked block's text on the host clipboard and removes the
// block — with a plain delete, NOT a kill: the removed text must not enter
// the kill ring, so the system clipboard and the ring stay independent. The
// delete and the marker-clear group into one undo step.
func (e *Editor) osCut() bool {
	if e.Config.ClipboardWrite == nil {
		e.ShowWarning("No host clipboard")
		return false
	}
	if e.contentLocked() {
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w, text, ok := e.osClipboardText()
	if !ok {
		return false
	}
	e.Config.ClipboardWrite(text)

	startLine, startRune, endLine, endRune, _ := w.Buffer.GetBlockRange()
	w.Buffer.BeginUserCommand("os_cut")
	w.Buffer.DeleteTextRange(startLine, startRune, endLine, endRune)
	w.Buffer.ClearBlockMarks()
	w.Buffer.EndUserCommand()

	w.TrackEdit()
	e.lastEditKill = false // a plain delete, not a kill: no accumulation chain

	e.clampCursorToBuffer(w)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.ShowNotification("Cut block to clipboard")
	return true
}

// osPaste asks the host for its clipboard and applies it via osPasteText.
// The read may resolve asynchronously on a host thread, so the delivery is
// marshaled back onto the editor main loop through PostAction.
func (e *Editor) osPaste() bool {
	if e.Config.ClipboardRead == nil {
		e.ShowWarning("No host clipboard")
		return false
	}
	e.Config.ClipboardRead(func(text string) {
		if !e.PostAction(func() { e.osPasteText(text) }) {
			// No postable input source (shouldn't happen in a hosted
			// session): the delivery is dropped rather than run unsafely
			// off-thread.
			return
		}
	})
	return true
}

// osPasteText applies resolved clipboard text: when a block is marked AND the
// caret sits within it (or on either edge), the text REPLACES the block —
// identical semantics to block_from_file, block left marked around the pasted
// text. Otherwise the text inserts at the caret as one undo revision —
// identical semantics to buffer_insert_file. Line endings normalize like any
// paste.
func (e *Editor) osPasteText(text string) bool {
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil {
		return false
	}
	text = normalizeLineEndings(text)
	if text == "" {
		e.ShowNotification("Clipboard is empty")
		return false
	}

	if w.Buffer.HasBlockMarks() && e.caretWithinBlock(w) {
		if !e.replaceBlockText(text, "os_paste") {
			return false
		}
		e.ShowNotification("Block replaced from clipboard")
		return true
	}

	// No engaged block: insert at the caret, one undo revision (the
	// buffer_insert_file shape).
	if e.contentLocked() {
		return false
	}
	w.Buffer.BeginUserCommand("os_paste")
	e.insertText(text)
	w.Buffer.EndUserCommand()
	w.TrackEdit()
	e.lastEditKill = false // an insert, not a kill: breaks delete accumulation
	return true
}

// osSelectAll marks the whole buffer as the block — mew's analog of a system
// Select All — without moving the caret.
func (e *Editor) osSelectAll() bool {
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil {
		return false
	}
	lastLine := w.Buffer.GetLineCount() - 1
	if lastLine < 0 {
		lastLine = 0
	}
	endRune := len([]rune(strings.TrimRight(w.Buffer.GetLine(lastLine), "\n\r")))
	w.Buffer.SetMark("_block_begin", 0, 0)
	w.Buffer.SetMark("_block_end", lastLine, endRune)
	e.ShowNotification("Selected all")
	e.RequestRender()
	return true
}
