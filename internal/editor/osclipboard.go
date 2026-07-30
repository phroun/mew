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

	"github.com/phroun/mew/internal/viewport"
)

// osClipboardText returns the marked block's text from the focused viewport,
// or ok=false (with a warning shown) when there is no buffer or no block.
func (e *Editor) osClipboardText() (w *viewport.Viewport, text string, ok bool) {
	w = e.ViewportManager.GetFocusedViewport()
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
		// The buffer's owning viewport is read-only, or a link button is
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
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	text = normalizeLineEndings(text)
	if text == "" {
		e.ShowNotification("Clipboard is empty")
		return false
	}

	if w.Buffer.HasBlockMarks() && e.caretWithinBlock(w) {
		// A host-layer paste (Edit menu, context menu) REPLACES the block but
		// does not change its provenance: the replace path clears the
		// mouse-block flag with the old marks, so restore whatever it was —
		// a transient drag selection pasted into stays transient, a
		// deliberate block stays deliberate.
		wasMouseBlock := w.Buffer.MouseBlock()
		if !e.replaceBlockText(text, "os_paste") {
			return false
		}
		w.Buffer.SetMouseBlock(wasMouseBlock)
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

// osExchange swaps the marked block with the clipboard: the block's text goes
// out, the clipboard's text comes in to replace it. It cannot be composed from
// os_copy + os_paste — after the copy the clipboard holds the block, so the
// paste would only put it back — so the outgoing text has to be captured
// before the incoming text lands.
//
// The clipboard READ is asynchronous (a host may resolve it on a UI thread),
// so the block is captured in the delivery callback, not here: by the time the
// text arrives the buffer may have moved on, and re-reading is what keeps the
// swap consistent with what the user can see. A block that has since been
// unmarked cancels the exchange rather than pasting into nowhere.
func (e *Editor) osExchange() bool {
	if e.Config.ClipboardRead == nil || e.Config.ClipboardWrite == nil {
		e.ShowWarning("No host clipboard")
		return false
	}
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	// Fail fast and loudly while the user is still looking at the menu, rather
	// than silently doing nothing once the clipboard resolves.
	if _, _, ok := e.osClipboardText(); !ok {
		return false
	}
	e.Config.ClipboardRead(func(incoming string) {
		e.PostAction(func() { e.osExchangeApply(incoming) })
	})
	return true
}

// osExchangeApply completes an exchange once the clipboard text has arrived:
// re-read the block (the buffer may have moved on since the read was issued),
// swap it for incoming, and publish the outgoing text. Split out from
// osExchange so the swap is reachable without a running main loop.
func (e *Editor) osExchangeApply(incoming string) bool {
	w, outgoing, ok := e.osClipboardText()
	if !ok || e.contentLocked() {
		return false
	}
	if !e.caretWithinBlock(w) {
		// osPasteText would insert at the caret instead of replacing the
		// block, which is not an exchange. Say so rather than half-do it.
		e.ShowWarning("Cursor is outside the block")
		return false
	}
	// Replace FIRST, publish second: a failed replace must not leave the
	// clipboard already clobbered with text the user never got back.
	if !e.osPasteText(incoming) {
		return false
	}
	e.Config.ClipboardWrite(outgoing)
	e.ShowNotification("Exchanged block with clipboard")
	return true
}

// osSelectAll marks the whole buffer as the block — mew's analog of a system
// Select All — without moving the caret.
func (e *Editor) osSelectAll() bool {
	w := e.ViewportManager.GetFocusedViewport()
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
