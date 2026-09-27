package editor

import (
	"fmt"

	"strconv"
	"strings"
	"sync/atomic"

	"github.com/phroun/mew/internal/viewport"
	"github.com/phroun/pawscript"
)

// Insertion: typed text, overwrite, newlines with auto-indent, paste chunks,
// single runes and raw bytes, bidi controls, input-method preedit and commit,
// and replace_prior.

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

// registerInsertCommands registers the commands that put text into a buffer.
func (e *Editor) registerInsertCommands(ps *pawscript.PawScript) {
	// insert and insert_newline DISPATCH on what the focused viewport is.
	//
	// A viewport running a terminal session gets the input sent to the child
	// process, exactly as tinput would; anything else gets the buffer edit that
	// used to carry these names (now buffer_insert / buffer_insert_newline).
	//
	// This is why the keymaps need no pty variant. tab, return, and every
	// self-inserting key already end in `insert` or `insert_newline`, so typing
	// into a terminal viewport reaches the shell through the bindings that were
	// already there — and the same keys keep editing text everywhere else. One
	// name, two meanings, chosen by what is under the caret.
	ps.RegisterCommand("insert", func(ctx *pawscript.Context) pawscript.Result {
		if e.focusedPTY() != nil {
			if len(ctx.Args) > 0 {
				if sb, ok := ctx.Args[0].(pawscript.StoredBytes); ok {
					return pawscript.BoolStatus(e.ptySendBytes(sb.Data()))
				}
				return pawscript.BoolStatus(e.ptySendBytes([]byte(fmt.Sprintf("%v", ctx.Args[0]))))
			}
			return pawscript.BoolStatus(false)
		}
		return pawscript.BoolStatus(e.bufferInsertArgs(ctx.Args))
	})

	// replace_prior <n>, '<text>' stands text in place of the n characters
	// immediately before the caret. It exists for input methods, and macOS's
	// press-and-hold accent palette above all: that palette COMMITS the held
	// letter the moment the key goes down, so choosing an accent has to remove
	// a character that is already in the document.
	//
	// The host says how many, because only the host can know. It watched the
	// key commit the letter and watched an input method take the key over; mew
	// sees the finished text and nothing about what it stands for.
	//
	// n of 0 is an ordinary insert, which is what every composition that
	// appends rather than replaces sends — a CJK candidate, and every host that
	// cannot know a replacement count at all.
	ps.RegisterCommand("replace_prior", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 2 {
			return pawscript.BoolStatus(false)
		}
		n, err := strconv.Atoi(strings.TrimSpace(fmt.Sprintf("%v", ctx.Args[0])))
		if err != nil || n < 0 {
			return pawscript.BoolStatus(false)
		}
		text := fmt.Sprintf("%v", ctx.Args[1])
		if sb, ok := ctx.Args[1].(pawscript.StoredBytes); ok {
			text = string(sb.Data())
		}
		// A viewport running a child process has no document to replace in, so
		// the replacement is made the way a person would make it: erase what it
		// stands in for, then type the new text. The erase goes as the child's
		// own backspace, encoded by the terminal that knows what this child
		// negotiated (see ptyEraseBefore).
		//
		// It has to be sent, because nothing else will. The toolkit swallowed
		// the platform's Backspace on the way in — it belonged to the palette,
		// not to the user — so without this the accent lands after the letter
		// it was chosen to replace.
		//
		// A child with no translator still gets the text. Losing the accent
		// entirely would be worse than leaving the letter in front of it.
		if e.focusedPTY() != nil {
			e.ptyEraseBefore(n)
			return pawscript.BoolStatus(e.ptySendBytes([]byte(text)))
		}
		return pawscript.BoolStatus(e.replacePrior(n, text))
	})

	// preedit '<text>', <caret>, <covers>, <clauseStart>, <clauseLen> shows what
	// an input method is still composing: painted at the caret, not put in the
	// document. An empty text ends it.
	//
	// covers is how many committed characters before the caret the composition
	// stands OVER and hides. macOS's press-and-hold palette commits the held
	// letter before it opens, so without this the line shows the letter and the
	// accent chosen to replace it side by side for as long as the palette is
	// up. Nothing is deleted to hide it — ending the composition brings it
	// straight back, which is what dismissing a palette means.
	//
	// Not stored, because storing it would mean un-storing it on every update —
	// a Japanese input method rewrites the whole composition on each keystroke —
	// and every one of those round trips would go through the undo history.
	// It is synthesized into the line at paint time instead, the way a control
	// character is painted "^X" without the buffer holding two runes.
	//
	// caret is the input method's own cursor within the text, which is what
	// shows progress through a long composition. It defaults to the end.
	//
	// clauseStart and clauseLen mark the segment being CONVERTED, when the
	// input method distinguishes one. A Japanese composition is several
	// clauses and a candidate list changes only the selected one — "らなに"
	// converts to "羅なに" with the tail still in kana — so the clause is
	// painted apart from the rest, which is what tells the untouched remainder
	// from characters the composition failed to replace.
	ps.RegisterCommand("preedit", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil {
			return pawscript.BoolStatus(false)
		}
		// A viewport running a child process paints the child's grid, not a
		// document: there is no line to synthesize a composition into.
		if e.focusedPTY() != nil {
			return pawscript.BoolStatus(false)
		}
		text := ""
		if len(ctx.Args) > 0 {
			text = fmt.Sprintf("%v", ctx.Args[0])
		}
		runes := []rune(text)
		caret := len(runes)
		if len(ctx.Args) > 1 {
			if n, err := strconv.Atoi(strings.TrimSpace(fmt.Sprintf("%v", ctx.Args[1]))); err == nil {
				caret = n
			}
		}
		covers := 0
		if len(ctx.Args) > 2 {
			if n, err := strconv.Atoi(strings.TrimSpace(fmt.Sprintf("%v", ctx.Args[2]))); err == nil && n > 0 {
				covers = n
			}
		}
		clauseStart, clauseLen := 0, 0
		if len(ctx.Args) > 4 {
			s, err1 := strconv.Atoi(strings.TrimSpace(fmt.Sprintf("%v", ctx.Args[3])))
			n, err2 := strconv.Atoi(strings.TrimSpace(fmt.Sprintf("%v", ctx.Args[4])))
			if err1 == nil && err2 == nil {
				clauseStart, clauseLen = s, n
			}
		}
		// Empty text goes through too, rather than clearing here: whether it
		// ENDS the composition on its way to a commit or CANCELS it turns on
		// the extent it still names, and SetPreedit is where that is decided.
		w.SetPreedit(viewport.Preedit{
			Text: runes, Caret: caret, Covers: covers,
			ClauseStart: clauseStart, ClauseLen: clauseLen,
		})
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	// preedit_commit '<text>' takes a finished composition into the document,
	// in place of THE REGION THE COMPOSITION STOOD OVER.
	//
	// Anchored, not measured from the caret, which is the whole reason it is
	// its own command rather than a replace_prior. A composition is dismissed
	// by typing: macOS commits whatever was selected in its palette and the
	// keystroke that dismissed it lands first, so by the time this arrives the
	// caret has moved past a character the composition never covered.
	// Counting back from the caret replaced that character instead — the
	// accent ate it and the letter stayed, "oò" with the "." gone.
	//
	// With no composition standing it is an ordinary insert, which is what a
	// host that never opened one sends.
	ps.RegisterCommand("preedit_commit", func(ctx *pawscript.Context) pawscript.Result {
		text := ""
		if len(ctx.Args) > 0 {
			text = fmt.Sprintf("%v", ctx.Args[0])
		}
		return pawscript.BoolStatus(e.preeditCommit(text))
	})

	ps.RegisterCommand("insert_newline", func(ctx *pawscript.Context) pawscript.Result {
		if e.focusedPTY() != nil {
			// A shell wants CR for Enter, not LF: that is what a terminal
			// sends and what line discipline turns back into a newline.
			return pawscript.BoolStatus(e.ptySendBytes([]byte{'\r'}))
		}
		return pawscript.BoolStatus(e.bufferInsertNewline())
	})

	ps.RegisterCommand("buffer_insert", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.bufferInsertArgs(ctx.Args))
	})

	// buffer_insert_newline breaks the line like `buffer_insert '\n'`, then — when the
	// autoIndent option is on for the viewport — repeats the split line's
	// leading whitespace so the new line starts under its text. It is the tail
	// of the default Enter binding (nav_follow false|accept|insert_newline).
	// The plain name now DISPATCHES — see the pair registered above.
	ps.RegisterCommand("buffer_insert_newline", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.bufferInsertNewline())
	})

	// insert_bidi_control inserts a Unicode bidi control by short name (lrm,
	// rlm, alm, fsi, lri, rli, pdi) — otherwise behaving exactly like insert.
	// With no argument it prompts; "?" shows the legend and re-prompts.
	ps.RegisterCommand("insert_bidi_control", func(ctx *pawscript.Context) pawscript.Result {
		if arg, ok := argString(ctx, 0); ok {
			return pawscript.BoolStatus(e.insertBidiControl(arg))
		}
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		var ask func()
		ask = func() {
			e.PromptMgr.PromptForInput("Insert control mark [lrm/rlm/alm, fsi/lri/rli, pdi, ?]: ", "",
				func(accepted bool, _, input string) {
					defer e.RequestRender()
					if expired.Load() {
						e.ShowWarning("Prompt timed out")
						return
					}
					name := strings.ToLower(strings.TrimSpace(input))
					if !accepted || name == "" {
						ctx.ResumeToken(token, false)
						return
					}
					if name == "?" {
						e.ShowNotification("lrm=left-to-right, rlm=right-to-left, alm=arabic letter mark")
						e.ShowNotification("fsi=first strong isolate, lri=left-to-right isolate, rli=right-to-left-isolate, pdi=pop directional isolate")
						ask() // re-prompt with the same prompt
						return
					}
					if _, ok := bidiControlRune(name); !ok {
						e.ShowWarning("Unknown control mark: " + name)
						ask() // stay in the loop on an unrecognized name
						return
					}
					ctx.ResumeToken(token, e.insertBidiControl(name))
				}, "bidictl")
		}
		ask()
		return pawscript.TokenResult(token)
	})

	ps.RegisterCommand("insert_rune", func(ctx *pawscript.Context) pawscript.Result {
		if arg, ok := argString(ctx, 0); ok {
			r, ok := parseCodePoint(arg)
			if !ok {
				e.ShowWarning("Not a Unicode code point: " + arg)
				return pawscript.BoolStatus(false)
			}
			return pawscript.BoolStatus(e.insertRuneAt(r))
		}
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		var ask func()
		ask = func() {
			e.PromptMgr.PromptForInput("Insert rune by code point [U+xxxx hex, #NNN decimal, \\uXXXX]: ", "",
				func(accepted bool, _, input string) {
					defer e.RequestRender()
					if expired.Load() {
						e.ShowWarning("Prompt timed out")
						return
					}
					if !accepted || strings.TrimSpace(input) == "" {
						ctx.ResumeToken(token, false)
						return
					}
					r, ok := parseCodePoint(input)
					if !ok {
						e.ShowWarning("Not a Unicode code point: " + input)
						ask() // stay in the loop rather than eat the keystroke
						return
					}
					ctx.ResumeToken(token, e.insertRuneAt(r))
				}, "insrune")
		}
		ask()
		return pawscript.TokenResult(token)
	})

	// insert_raw_byte inserts bytes verbatim. A PawScript {bytes ...} value goes
	// in whole; otherwise a single byte 0..255, written whichever way the value is
	// already in your head - see parseByteSpec for the full set: ^[ or ESC for
	// the escape character, x1b, o33, b11011, #27. "?" in the prompt lists
	// them.
	ps.RegisterCommand("insert_raw_byte", func(ctx *pawscript.Context) pawscript.Result {
		// A {bytes ...} value is the language's own way to say this, so take it
		// first and take ALL of it: {bytes 0xDEADBEEF} inserts four bytes, not
		// one. Every other spelling below carries a single byte because it is a
		// way of NAMING one; this is a way of holding a sequence.
		if len(ctx.Args) > 0 {
			if sb, ok := ctx.Args[0].(pawscript.StoredBytes); ok {
				return pawscript.BoolStatus(e.insertRawBytesAt(sb.Data()))
			}
		}
		if arg, ok := argString(ctx, 0); ok {
			r, ok := parseByteSpec(arg)
			if !ok {
				e.ShowWarning("Not a byte value: " + arg)
				return pawscript.BoolStatus(false)
			}
			return pawscript.BoolStatus(e.insertRawByteAt(byte(r)))
		}
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		var ask func()
		ask = func() {
			e.PromptMgr.PromptForInput("Insert raw byte [x1b, o33, b11011, #27, ^[, \\e, ESC, ?]: ", "",
				func(accepted bool, _, input string) {
					defer e.RequestRender()
					if expired.Load() {
						e.ShowWarning("Prompt timed out")
						return
					}
					in := strings.TrimSpace(input)
					if !accepted || in == "" {
						ctx.ResumeToken(token, false)
						return
					}
					if in == "?" {
						e.ShowNotification("x1b/1b=hex (default), o33=octal, b11011=binary, #27=decimal")
						e.ShowNotification("^[=control (^@..^_, ^?=DEL), \\n \\r \\t \\e \\0 \\xNN \\NNN, or a name: BEL BS TAB LF CR ESC DEL")
						ask() // re-prompt, same as insert_bidi_control's "?"
						return
					}
					r, ok := parseByteSpec(in)
					if !ok {
						e.ShowWarning("Not a byte value: " + in)
						ask()
						return
					}
					ctx.ResumeToken(token, e.insertRawByteAt(byte(r)))
				}, "insbyte")
		}
		ask()
		return pawscript.TokenResult(token)
	})
}
