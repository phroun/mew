package editor

import (
	"strings"

	"github.com/phroun/mew/internal/viewport"
)

// Single-keystroke confirmations. Every yes/no confirmation prompt arms the
// after-key pseudo-binding with the confirm_key command, which classifies what
// the keystroke did to the prompt buffer purely from its LENGTH and the caret
// position — no key identity needed:
//
//   - grew by exactly one rune, same line count: a plain key landed. That key
//     IS the response (y, n, ...): settle the prompt with the caret line.
//   - grew by exactly one rune, one more line: Enter. The chosen option is
//     the line ABOVE the caret — at the end of the buffer that is the (blank)
//     input line, so the default answer applies; on a blank line in the middle
//     of the option list it is the option the user arrowed to and entered on.
//   - grew by more than one rune, or shrank: a paste, a deletion, or anything
//     else that is not an answer — treated as a cancel, never an affirmative.
//   - unchanged: caret movement (arrowing through the options) — keep waiting.

// confirmKeyBase records a confirmation prompt's opening buffer size, the
// reference the classifier measures each keystroke against.
type confirmKeyBase struct {
	runes int
	lines int
}

// armConfirmKeys installs the single-keystroke classifier on the just-created
// confirmation prompt (the focused viewport). Called by the PromptForConfirmation
// variants immediately after the prompt viewport is created.
func (pm *PromptManager) armConfirmKeys() {
	e := pm.editor
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Type != viewport.PromptViewport || w.Buffer == nil {
		return
	}
	w.AfterKey = "confirm_key"
	// The classifier reads a cancel from the buffer SHRINKING, and a backspace at
	// the start of the option list shrinks by joining lines — a newline delete.
	// Prompt viewports protect newlines by default, which would block that and
	// leave the buffer unchanged (read as "keep waiting"). This prompt is a
	// keystroke mechanism, not editable text, so it opts out.
	w.ViewState.ProtectNewlines = false
	if e.confirmKey == nil {
		e.confirmKey = make(map[string]confirmKeyBase)
	}
	e.confirmKey[w.ID] = confirmKeyBase{
		runes: len([]rune(w.Buffer.GetContent())),
		lines: w.Buffer.GetLineCount(),
	}
}

// confirmKeyStroke is the confirm_key command: classify the keystroke that
// just resolved against the armed baseline and settle the prompt when it
// amounts to an answer (or to a non-answer that cancels).
func (e *Editor) confirmKeyStroke() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Type != viewport.PromptViewport || w.Buffer == nil {
		return false
	}
	base, armed := e.confirmKey[w.ID]
	if !armed {
		return false
	}

	delta := len([]rune(w.Buffer.GetContent())) - base.runes
	switch {
	case delta == 0:
		// Movement only (arrowing through the options): keep waiting.
		return true
	case delta == 1:
		if w.Buffer.GetLineCount() > base.lines {
			// Enter: the answer is the line the caret was on when it was
			// pressed. The newline split that line in two — the halves are the
			// line above the caret and the caret line — so joining them
			// reconstructs it wherever in the line the enter landed. At the end
			// of the buffer both halves are blank (the input line), which the
			// parser resolves to the default; on an arrowed-to option the join
			// is that option's text.
			caretLine := w.CursorPos().Line
			prev := ""
			if caretLine > 0 {
				prev = strings.TrimRight(w.Buffer.GetLine(caretLine-1), "\n\r")
			}
			cur := strings.TrimRight(w.Buffer.GetLine(caretLine), "\n\r")
			e.settleConfirmPrompt(w, true, strings.TrimSpace(prev+cur))
			return true
		}
		// The key itself is the response.
		answer := strings.TrimSpace(strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r"))
		e.settleConfirmPrompt(w, true, answer)
		return true
	default:
		// Too long or too short: not an answer — same as a cancel/timeout.
		e.settleConfirmPrompt(w, false, "")
		return true
	}
}

// settleConfirmPrompt completes an armed confirmation exactly the way the
// accept/cancel commands would: viewport removed first (so focus returns to
// the main buffer before any callback output), then the prompt callback runs
// with the classified answer.
func (e *Editor) settleConfirmPrompt(w *viewport.Viewport, accepted bool, answer string) {
	delete(e.confirmKey, w.ID)
	promptCallback := w.PromptCallback
	legacyCallback := w.Callback
	bufferContent := ""
	if accepted && w.Buffer != nil && w.Buffer.GetLineCount() > 0 {
		bufferContent = strings.TrimRight(w.Buffer.GetLine(0), "\n\r")
	}
	e.ViewportManager.RemoveViewport(w.ID)
	switch {
	case promptCallback != nil && accepted:
		promptCallback(true, bufferContent, answer)
	case promptCallback != nil:
		promptCallback(false, "", "")
	case legacyCallback != nil && accepted:
		legacyCallback(answer, true)
	case legacyCallback != nil:
		legacyCallback("", false)
	}
	e.RequestRender()
}
