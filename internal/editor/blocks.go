package editor

import (
	"fmt"

	"strings"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
	"github.com/phroun/pawscript"
)

// setUserMark sets a user-defined mark at the given position. It rejects empty
// names and the reserved "_" internal-mark namespace.
func (e *Editor) setUserMark(w *viewport.Viewport, name string, line, runePos int) bool {
	if w == nil || w.Buffer == nil || name == "" {
		return false
	}
	if strings.HasPrefix(name, "_") {
		e.ShowWarning("Mark names starting with '_' are reserved")
		return false
	}
	if err := w.Buffer.SetMark(name, line, runePos); err != nil {
		e.ShowError("Failed to set mark: " + err.Error())
		return false
	}
	e.ShowNotification("Mark '" + name + "' set")
	return true
}

// gotoUserMark moves the caret to a named user mark, warning if it is unset.
// Shared by go_mark's direct-argument and prompted paths.
func (e *Editor) gotoUserMark(w *viewport.Viewport, name string) bool {
	if w == nil || w.Buffer == nil {
		return false
	}
	line, runePos, exists := w.Buffer.GetMark(name)
	if !exists {
		e.ShowWarning("Mark '" + name + "' not set")
		return false
	}
	w.SetCursorPos(viewport.Position{Line: line, Rune: runePos})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	w.TrackMove()
	return true
}

// setBlockMark sets an internal block-selection mark at the cursor position.
func (e *Editor) setBlockMark(markName, label string) bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	if err := w.Buffer.SetMark(markName, w.CursorPos().Line, w.CursorPos().Rune); err != nil {
		e.ShowError("Failed to set mark: " + err.Error())
		return false
	}
	// A keyboard-placed block mark makes the block deliberate: it survives
	// plain mouse clicks (unlike a transient mouse-drag selection).
	w.Buffer.SetMouseBlock(false)
	e.ShowNotification(label + " set")
	return true
}

// goBlockMark moves the cursor to an internal block-selection mark.
func (e *Editor) goBlockMark(markName, label string) bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	line, rune_, exists := w.Buffer.GetMark(markName)
	if !exists {
		e.ShowWarning(label + " not set")
		return false
	}
	w.SetCursorPos(viewport.Position{Line: line, Rune: rune_})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	return true
}

// copyBlock copies the marked block to the cursor position.
func (e *Editor) copyBlock() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return false
	}

	startLine, startRune, endLine, endRune, exists := w.Buffer.GetBlockRange()
	if !exists {
		e.ShowWarning("No block marked")
		return false
	}

	// Get block content
	content := e.getBlockContent(w.Buffer, startLine, startRune, endLine, endRune)
	if content == "" {
		return false
	}

	// Insert at cursor position
	w.Buffer.InsertText(w.CursorPos().Line, w.CursorPos().Rune, content)

	// Move cursor past inserted content
	insertedRunes := []rune(content)
	newlines := 0
	lastNewlineIdx := -1
	for i, r := range insertedRunes {
		if r == '\n' {
			newlines++
			lastNewlineIdx = i
		}
	}

	if newlines > 0 {
		w.SetCursorLine(w.CursorPos().Line + newlines)
		w.SetCursorRune(len(insertedRunes) - lastNewlineIdx - 1)
	} else {
		w.SetCursorRune(w.CursorPos().Rune + len(insertedRunes))
	}

	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.ShowNotification("Block copied")
	return true
}

// deleteBlock deletes the marked block.
func (e *Editor) deleteBlock() bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return false
	}

	startLine, startRune, endLine, endRune, exists := w.Buffer.GetBlockRange()
	if !exists {
		e.ShowWarning("No block marked")
		return false
	}

	// Delete KILLING into the ring (emacs kill-region): the removed text and
	// its in-range user marks become a kill entry, yankable anywhere. The
	// block markers themselves stay behind (kill filter) and are cleared just
	// below. Garland slides the viewport's own caret with the edit — a caret
	// after the block moves back, a caret inside it collapses to the deletion
	// point, a caret before it stays put — so no hand-computed adjustment.
	// The delete and the marker-clear are two mutations: group them so one undo
	// reverses the whole block delete (marks restored with the text).
	w.Buffer.BeginUserCommand("block_delete")
	defer w.Buffer.EndUserCommand()
	cap := w.Buffer.DeleteTextRangeForKill(startLine, startRune, endLine, endRune)
	e.killCapture(w, cap, true)

	// Clear block marks
	w.Buffer.ClearBlockMarks()

	e.clampCursorToBuffer(w)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.ShowNotification("Block deleted")
	return true
}

// moveBlock moves the marked block to the cursor position.
func (e *Editor) moveBlock() bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return false
	}

	startLine, startRune, endLine, endRune, exists := w.Buffer.GetBlockRange()
	if !exists {
		e.ShowWarning("No block marked")
		return false
	}

	// Check if cursor is inside the block
	if (w.CursorPos().Line > startLine || (w.CursorPos().Line == startLine && w.CursorPos().Rune >= startRune)) &&
		(w.CursorPos().Line < endLine || (w.CursorPos().Line == endLine && w.CursorPos().Rune <= endRune)) {
		e.ShowWarning("Cannot move block to position within block")
		return false
	}

	// Delete the block CAPTURING its text and marks — the in-range user marks
	// plus the block markers themselves (same filtering decision as the kill
	// capture, block markers included because a move cannot duplicate them).
	// Garland slides the viewport's caret to the correct insertion point (moved
	// back if the caret was after the block, unchanged if before — a caret
	// inside was rejected above). Re-inserting the capture at the caret places
	// every mark at its offset in the moved text, so the block stays marked at
	// its destination; insertBefore=false keeps the caret at the start of the
	// inserted text.
	// The delete and the re-insert are two mutations that must undo as one step
	// (a half-undone move would lose or duplicate the block), so group them.
	w.Buffer.BeginUserCommand("block_move")
	defer w.Buffer.EndUserCommand()
	cap := w.Buffer.DeleteTextRangeForMove(startLine, startRune, endLine, endRune)
	if cap.Empty() {
		return false
	}
	ins := w.CursorPos()
	w.Buffer.InsertCaptured(ins.Line, ins.Rune, cap)

	e.clampCursorToBuffer(w)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.ShowNotification("Block moved")
	return true
}

// indentBlock indents all lines in the marked block.
func (e *Editor) indentBlock() bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return false
	}

	if !w.Buffer.HasBlockMarks() {
		e.ShowWarning("No block marked")
		return false
	}

	indentString := strings.Repeat(" ", e.tabSize(w))

	// The block is delimited by the _block_begin/_block_end decorations;
	// IndentBlock walks a garland cursor between them, anchored to positions
	// garland maintains rather than captured line numbers. The viewport's caret
	// slides with the inserted indent on its own — no read-back needed.
	w.Buffer.IndentBlock("_block_begin", "_block_end", indentString)

	e.clampCursorToBuffer(w)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.ShowNotification("Block indented")
	return true
}

// unindentBlock removes leading whitespace from all lines in the marked block.
func (e *Editor) unindentBlock() bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return false
	}

	if !w.Buffer.HasBlockMarks() {
		e.ShowWarning("No block marked")
		return false
	}

	// Decoration-anchored cursor walk (see indentBlock); the viewport's caret
	// slides left with the deleted indent on its own.
	w.Buffer.UnindentBlock("_block_begin", "_block_end", e.tabSize(w))

	e.clampCursorToBuffer(w)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.ShowNotification("Block unindented")
	return true
}

// getBlockContent extracts text from the marked block. Backed by garland's
// byte-range read so terminators are reproduced exactly (one per line), not
// doubled by line-by-line reconstruction.
func (e *Editor) getBlockContent(buf *buffer.Buffer, startLine, startRune, endLine, endRune int) string {
	return buf.GetTextRange(startLine, startRune, endLine, endRune)
}

// writeBlock writes the marked block's text to a prompted-for file.
func (e *Editor) writeBlock() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	startLine, startRune, endLine, endRune, exists := w.Buffer.GetBlockRange()
	if !exists {
		e.ShowWarning("No block marked")
		return false
	}
	content := e.getBlockContent(w.Buffer, startLine, startRune, endLine, endRune)
	if content == "" {
		e.ShowWarning("Block is empty")
		return false
	}

	e.PromptMgr.PromptForFilename("Write block to", "", func(accepted bool, _, filename string) {
		if !accepted || filename == "" {
			e.RequestRender()
			return
		}
		write := func() {
			if err := e.FS.WriteFile(filename, []byte(content)); err != nil {
				e.ShowError("Failed to write block: " + err.Error())
			} else {
				e.ShowNotification("Block written: " + filename)
			}
			e.RequestRender()
		}
		// A block write is never a whole-buffer save, so overwriting ANY
		// existing file (the buffer's own source included) gets a prompt.
		if e.fileExists(filename) {
			e.PromptMgr.PromptForConfirmation(fmt.Sprintf("13: OVERWRITE EXISTING FILE %s?", filename), false, func(accepted, confirmed bool) {
				if accepted && confirmed {
					write()
				} else {
					e.ShowNotification("Block write cancelled")
					e.RequestRender()
				}
			})
			return
		}
		write()
	})
	return true
}

// promptBlockFromFile gates block_from_file: there must be a marked block and
// the caret must lie within it (or on either edge). Only when that holds does
// it prompt for the file to stream in over the block's contents. The caret gate
// is the same inclusive span block_move refuses to move a block into — demanded
// here rather than forbidden. Prompts are modal (focus is on the prompt viewport),
// so the block and caret cannot shift before the callback fires; blockFromFile
// re-reads the range defensively anyway.
func (e *Editor) promptBlockFromFile() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return false
	}
	if !w.Buffer.HasBlockMarks() {
		e.ShowWarning("No block marked")
		return false
	}
	if !e.caretWithinBlock(w) {
		e.ShowWarning("Caret must be within the block")
		return false
	}

	e.PromptMgr.PromptForFilename("Stream file into block", "", func(accepted bool, _, filename string) {
		if accepted && filename != "" {
			e.blockFromFile(filename)
		}
		e.RequestRender()
	})
	return true
}

// caretWithinBlock reports whether the focused viewport's caret lies within the
// marked block or on either of its edges — the inclusive span the block
// commands that TARGET the block (block_from_file, os_paste's replace mode)
// demand. False when no block is marked.
func (e *Editor) caretWithinBlock(w *viewport.Viewport) bool {
	startLine, startRune, endLine, endRune, exists := w.Buffer.GetBlockRange()
	if !exists {
		return false
	}
	pos := w.CursorPos()
	return (pos.Line > startLine || (pos.Line == startLine && pos.Rune >= startRune)) &&
		(pos.Line < endLine || (pos.Line == endLine && pos.Rune <= endRune))
}

// blockFromFile replaces the marked block's contents with the contents of
// filename, streamed in at the block's location (see replaceBlockText for the
// replace semantics). The caller (promptBlockFromFile) has already verified
// there is a block and the caret is within it.
func (e *Editor) blockFromFile(filename string) bool {
	data, err := e.FS.ReadFile(filename)
	if err != nil {
		e.ShowError("Failed to read file: " + err.Error())
		return false
	}
	if !e.replaceBlockText(normalizeLineEndings(string(data)), "block_from_file") {
		return false
	}
	e.ShowNotification("Block replaced from " + filename)
	return true
}

// normalizeLineEndings converts CRLF and lone CR to '\n', the same
// normalization paste and buffer_insert_file apply.
func normalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// replaceBlockText replaces the marked block's contents with text (already
// newline-normalized) and leaves the newly inserted text marked as the block,
// so it is immediately ready for another block op. The delete, insert, and
// re-mark are grouped into one user command (cmdName) so a single undo
// reverses the whole replace.
func (e *Editor) replaceBlockText(text, cmdName string) bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	startLine, startRune, endLine, endRune, exists := w.Buffer.GetBlockRange()
	if !exists {
		e.ShowWarning("No block marked")
		return false
	}

	// Delete the old block content, place the text at its location, and mark
	// the inserted text as the new block. All one undo step. The old markers
	// collapse to the deletion point on delete; we clear them and re-set both to
	// absolute positions around the inserted text, so the block surrounds the
	// stream regardless of insert gravity. insertText advances the viewport caret
	// to the end of what it inserts, which is the block's new end.
	w.Buffer.BeginUserCommand(cmdName)
	w.Buffer.DeleteTextRange(startLine, startRune, endLine, endRune)
	w.Buffer.ClearBlockMarks()
	w.SetCursorPos(viewport.Position{Line: startLine, Rune: startRune})
	if text != "" {
		e.insertText(text)
	}
	endPos := w.CursorPos()
	w.Buffer.SetMark("_block_begin", startLine, startRune)
	w.Buffer.SetMark("_block_end", endPos.Line, endPos.Rune)
	w.Buffer.EndUserCommand()

	w.TrackEdit()
	e.lastEditKill = false // an insert, not a kill: breaks delete accumulation

	e.clampCursorToBuffer(w)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	return true
}

// registerBlockCommands registers the mark, block and kill-ring commands.
func (e *Editor) registerBlockCommands(ps *pawscript.PawScript) {
	// Mark commands
	ps.RegisterCommand("set_mark", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		// With an explicit name, set it directly.
		if len(ctx.Args) > 0 {
			return pawscript.BoolStatus(e.setUserMark(w, fmt.Sprintf("%v", ctx.Args[0]), w.CursorPos().Line, w.CursorPos().Rune))
		}
		// No name given (e.g. "esc esc") - prompt for the mark identifier.
		// The position is captured as a garland decoration, not as absolute
		// coordinates: anything that edits the buffer while the prompt is up
		// (a second viewport, an async script) slides the pending mark along
		// with the text, so the mark lands where the caret's TEXT is, not
		// where its line number used to be.
		const pendingMark = "_pending_set_mark"
		w.Buffer.SetMark(pendingMark, w.CursorPos().Line, w.CursorPos().Rune)
		e.PromptForInput("Set mark (0-9): ", "", func(input string, accepted bool) {
			line, rune_, exists := w.Buffer.GetMark(pendingMark)
			w.Buffer.ClearMark(pendingMark)
			if accepted && exists {
				name := strings.TrimSpace(input)
				w.Buffer.BeginUserCommand("set_mark")
				e.setUserMark(w, name, line, rune_)
				w.Buffer.EndUserCommand()
			}
			e.RequestRender()
		})
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_mark", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		// With an explicit name, jump directly.
		if len(ctx.Args) > 0 {
			return pawscript.BoolStatus(e.gotoUserMark(w, fmt.Sprintf("%v", ctx.Args[0])))
		}
		// No name given (e.g. "esc esc") - prompt for the mark identifier, then
		// jump to it (mirroring set_mark's no-argument prompt).
		e.PromptForInput("Go to mark (0-9): ", "", func(input string, accepted bool) {
			if accepted {
				e.gotoUserMark(w, strings.TrimSpace(input))
			}
			e.RequestRender()
		})
		return pawscript.BoolStatus(true)
	})

	// Block-selection mark commands. These encapsulate the internal
	// _block_begin/_block_end marks so keybindings never name them directly
	// (keeping the "_" internal-mark namespace out of user-facing config).
	ps.RegisterCommand("set_block_begin", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.setBlockMark("_block_begin", "Block begin"))
	})
	ps.RegisterCommand("set_block_end", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.setBlockMark("_block_end", "Block end"))
	})
	ps.RegisterCommand("go_block_begin", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.goBlockMark("_block_begin", "Block begin"))
	})
	ps.RegisterCommand("go_block_end", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.goBlockMark("_block_end", "Block end"))
	})

	// Block commands (TypeScript uses set_mark '_block_begin' / '_block_end')
	ps.RegisterCommand("block_copy", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.copyBlock()
		e.trackEdit()
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("block_delete", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.deleteBlock()
		if ok {
			e.trackEdit() // consumes the kill flag so accumulation chains work
		}
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("block_move", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.moveBlock()
		e.trackEdit()
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("block_write", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.writeBlock())
	})

	// Kill ring (emacs-style). Deletes accumulate into kill entries as they
	// run (see killCapture); these commands read the ring back.
	ps.RegisterCommand("block_copy_kill", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.blockCopyKill())
	})

	ps.RegisterCommand("kill_ring_yank", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.killRingYank()
		if ok {
			e.trackEdit() // an insert-style edit: cursor ring + breaks kill chain
		}
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("kill_ring_pop", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.killRingPop()
		if ok {
			e.trackEdit()
		}
		return pawscript.BoolStatus(ok)
	})

	// kill_ring_append arms the next kill to accumulate into the most recent
	// kill entry even if it would otherwise start a new one (append-next-kill).
	ps.RegisterCommand("kill_ring_append", func(ctx *pawscript.Context) pawscript.Result {
		e.killAppendNext = true
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("block_indent", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.indentBlock())
	})

	ps.RegisterCommand("block_unindent", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.unindentBlock())
	})

	// block_from_file streams a prompted-for file over the marked block: it
	// replaces the block's contents with the file's, but only when a block is
	// marked AND the caret sits within it (or on either edge). The gate is
	// enforced up front (promptBlockFromFile) so the filename is never even
	// asked for on an invalid target; the replace itself (blockFromFile) is
	// wrapped like the other block mutations — one grouped undo, block left
	// marked around the streamed-in text.
	ps.RegisterCommand("block_from_file", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.promptBlockFromFile())
	})
}
