package editor

import (
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/phroun/mew/internal/bidi"
	"github.com/phroun/mew/internal/viewport"
	"github.com/phroun/pawscript"
)

// ensureCursorVisible scrolls the viewport so the cursor is visible both
// vertically and horizontally. Horizontal following "locks in" the column — it
// is used after horizontal movement, edits, and other actions, but NOT after
// bare vertical movement (see ensureCursorVisibleVertical), so a run of up/down
// with an active ghost cursor keeps the horizontal view (and any manual scroll)
// stable until a horizontal/locking action.
func (e *Editor) ensureCursorVisible(w *viewport.Viewport) {
	e.ensureCursorVisibleVertical(w)
	e.ensureCursorVisibleHorizontal(w)
}

// ensureCursorVisibleVertical scrolls the viewport vertically so the cursor's
// line is visible. It never changes the horizontal offset, so it does not
// disturb a manual horizontal scroll or force the ghost column on-screen.
//
// This is the COMMAND path: a command asking to show the caret is, by
// definition, done browsing a detached (free) scroll, so it RE-ENGAGES caret
// following — clearing any ScrollDetached parked by the mouse wheel or a
// scroll_* command. (The per-frame render follow uses renderFollowCaret, which
// instead respects a detached scroll.) Cursor-movement and edit leaf commands
// reach here through their own ensureCursorVisible / ...Vertical calls, so the
// re-engagement lives in their definitions, not in any command-name analysis.
func (e *Editor) ensureCursorVisibleVertical(w *viewport.Viewport) {
	w.ViewState.ScrollDetached = false
	e.clampViewToCaret(w)
	// Vertical movement does not scroll horizontally, but the caret may have
	// landed on (or left) a line whose end needs the phantom column, which is a
	// one-cell adjustment rather than a scroll.
	e.reconcilePhantomColumn(w)
}

// clampViewToCaret scrolls the viewport vertically the minimum needed to bring
// the caret's line on-screen, WITHOUT touching ScrollDetached. Both the command
// path (ensureCursorVisibleVertical, which first re-engages following) and the
// per-frame render follow (renderFollowCaret, which first honors a detached
// scroll) share it.
func (e *Editor) clampViewToCaret(w *viewport.Viewport) {
	if w.ContentHeight <= 0 {
		w.ContentHeight = 20 // Default
	}
	// Absorb any slide the viewport anchor took from edits (e.g. lines inserted
	// above the top by another viewport on the same buffer), then scroll only as
	// far as needed to keep the caret visible. Both writes go through
	// SetViewTop so the anchor stays in lockstep with the painting offset.
	w.RefreshViewTop()
	top := w.ViewState.ViewOffsetY
	if w.CursorPos().Line < top {
		w.SetViewTop(w.CursorPos().Line)
	} else if w.CursorPos().Line >= top+w.ContentHeight {
		w.SetViewTop(w.CursorPos().Line - w.ContentHeight + 1)
	}
}

// renderFollowCaret is the per-frame vertical caret follow. Unlike the command
// path it RESPECTS a free scroll: while the focused viewport is ScrollDetached
// (mouse wheel / scroll_* command), the viewport is left exactly where the user
// parked it — the caret may sit off-screen, marked by the cursorOffScreen
// indicator — until a cursor-movement or edit command re-engages following.
func (e *Editor) renderFollowCaret(w *viewport.Viewport) {
	if w.ViewState.ScrollDetached {
		return
	}
	e.clampViewToCaret(w)
}

// scrollViewHorizontal moves the view one step in a VISUAL direction: dir -1 is
// leftward on screen, +1 rightward, whichever way the text runs.
//
// ViewOffsetX counts columns scrolled past the NEAR edge, and which edge that
// is depends on direction: the left under ltr, the reading start on the right
// under rtl. So a visual direction maps to opposite signs in the two modes —
// under rtl, moving the view LEFT (further into the reading tail) RAISES the
// offset. It reports whether the view actually moved; already at the near edge,
// it cannot.
func (e *Editor) scrollViewHorizontal(w *viewport.Viewport, dir int) bool {
	if w == nil || dir == 0 {
		return false
	}
	step := 8 * dir
	if e.winRTL(w) {
		step = -step
	}
	off := w.ViewState.ViewOffsetX + step
	if off < 0 {
		off = 0
	}
	if off == w.ViewState.ViewOffsetX {
		return false
	}
	w.ViewState.ViewOffsetX = off
	e.RequestRender()
	return true
}

// scrollViewByLines parks the focused-content viewport delta lines away from
// its current top (negative = toward the start), detaching it from the caret
// like the mouse wheel: the caret stays put and the per-frame follow will not
// snap the view back until a cursor-movement or edit command re-engages it.
func (e *Editor) scrollViewByLines(w *viewport.Viewport, delta int) {
	if w == nil {
		return
	}
	w.RefreshViewTop() // base the relative move on the actually-painted top
	e.scrollViewTo(w, w.ViewState.ViewOffsetY+delta)
}

// scrollViewTo parks the viewport at an absolute top line, detaching it from
// caret-follow. Shared by the mouse wheel, the drag-autoscroll, mew's own
// scrollbar, every scroll_* command and a graphical host's scroll_viewport, so
// the limit below is the ONE place the bottom of the scroll range is decided.
//
// That limit leaves exactly one row past the end of the document on screen — the
// one the gutter marks "~" (MaxScrollTop): the text does not get to drift off the
// top leaving an empty window behind it, and more than one such row appears only
// for a document too short to scroll at all.
func (e *Editor) scrollViewTo(w *viewport.Viewport, top int) {
	if w == nil || w.Buffer == nil {
		return
	}
	if max := viewport.MaxScrollTop(w.ContentHeight, w.Buffer.GetLineCount()); top > max {
		top = max
	}
	if top < 0 {
		top = 0
	}
	w.ViewState.ScrollDetached = true
	w.SetViewTop(top)
	e.RequestRender()
}

// ensureCursorVisibleHorizontal scrolls the viewport horizontally so the cursor
// (or its ghost column) is visible. ViewOffsetX is a VISUAL-column offset (the
// renderer slices and positions by visual column), so decisions use the cursor's
// visual column, not its rune index — otherwise tabs and control chars (visual
// width > 1) let the cursor drift off the edge.
func (e *Editor) ensureCursorVisibleHorizontal(w *viewport.Viewport) {
	if w.ContentWidth <= 0 {
		w.ContentWidth = 80 // Default
	}
	targetCol := w.CursorPos().Rune
	vw := -1 // total visual width, needed for reading-space scroll under rtl
	if w.Buffer != nil {
		tabSize := e.tabSize(w)
		line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
		// Browse-mode buttons: visibility runs in DISPLAY space — the line as
		// painted, with the caret mapped onto it (a caret inside a button
		// parks on the button). Identity when the line has no buttons.
		line, curRune := e.displayCaretLine(w, line, w.CursorPos().Rune)
		targetCol = e.caretVisualColumn(w, line, curRune, tabSize)
		if e.winRTL(w) {
			// Content only: the terminator's marker cells paint into the left
			// padding under rtl (see prepareLineForDisplay), so counting them
			// here would put the reading-space scroll a cell out of step with
			// the caret column, which never counts them either.
			vw = e.lineVisualWidth(w, line, tabSize)
		}
		if targetCol < 0 && !e.winRTL(w) && !e.caretWantsPhantom(w) {
			targetCol = 0
		}
	}
	// When a ghost cursor is active the user's intended column is the ghost's
	// column (past the line's end), so keep that visible instead. Under RTL
	// the ghost column is already a READING column (see afterVerticalMovement),
	// so it feeds the reading-space branch below directly.
	ghostReadingRTL := false
	if w.HasGhostCursor {
		targetCol = w.GhostCursorVisualColumn
		ghostReadingRTL = e.winRTL(w)
	}

	// A double-width (heading) caret line shows half as many columns, and the
	// renderer reads the horizontal scroll at one cell per two positions. So the
	// visibility test runs in that halved display space, and the resulting
	// offset is scaled back to the stored (normal-column) ViewOffsetX.
	dw := false
	if w.Buffer != nil {
		_, dw = e.lineDisplaySpans(w, w.CursorPos().Line)
	}
	effWidth := w.ContentWidth
	dispOff := w.ViewState.ViewOffsetX
	if dw {
		effWidth /= 2
		if effWidth < 1 {
			effWidth = 1
		}
		dispOff /= 2
	}
	// The PHANTOM COLUMN: a caret whose insertion point lies one cell past the
	// line's leading edge — the reading end of an RTL fragment starting an LTR
	// line (one cell LEFT of visual column 0), or an insertion point right of
	// an LTR run at an RTL line's reading start (one cell RIGHT of it). Neither
	// column exists in the content, so the follow may scroll ONE further than
	// the content itself allows: ViewOffsetX -1, a state where the first
	// visible column is 0 instead of 1 and the renderer shows a plain space
	// there for the caret to occupy (see prepareLineForDisplay / rtlView).
	// Guarded on the caret's logical rune being positive — there must be
	// content the caret is after — per the shape of the problem: rune positive
	// but visual column at the leading edge. Under direction=rtl a mirrored
	// line-number gutter already gives that caret a cell to dip into
	// (positionCursor), so the phantom only steps in without one.
	phantom := false

	setOff := func(v int) {
		if v < 0 {
			if phantom && v == -1 {
				// The one legal negative offset: the phantom column.
			} else {
				v = 0
			}
		}
		if dw {
			v *= 2
		}
		w.ViewState.ViewOffsetX = v
	}

	// direction=rtl: the view is right-anchored, so visibility is decided in
	// READING columns — the caret's distance back from the line's reading
	// start (its rightmost visual cell). ViewOffsetX counts reading columns
	// scrolled past, matching the renderer's right-anchored viewport.
	if e.winRTL(w) && vw >= 0 {
		reading := vw - targetCol
		if ghostReadingRTL {
			reading = targetCol // already a reading column
		}
		if reading < 0 {
			reading = 0
		}
		if !w.HasGhostCursor && e.caretWantsPhantom(w) {
			phantom = true
			reading = -1
		}
		if reading < dispOff {
			setOff(reading)
		} else if reading > dispOff+effWidth-1 {
			setOff(reading - effWidth + 1)
		}
		return
	}

	if !w.HasGhostCursor && e.caretWantsPhantom(w) {
		phantom = true // targetCol stays -1: the window checks scroll to it
	}
	if targetCol < dispOff {
		setOff(targetCol)
	} else if targetCol >= dispOff+effWidth {
		setOff(targetCol - effWidth + 1)
	}
}

// caretWantsPhantom reports whether the caret has earned the phantom column:
// its insertion point lies one cell PAST the line's leading edge while its
// logical rune is positive — there is content it stands after. Under an LTR
// base that is visual column -1 (the reading end of an RTL run starting the
// line); under RTL it is one cell past the content's rightmost cell, and the
// caret must additionally take an LTR run's direction — a caret on an RTL rune
// at the reading start marks the cell the NEXT character will occupy and keeps
// its clamp (the same split the gutter dip and the bar nudge use, via
// bidi.RTLAt).
//
// The RTL measure is the CONTENT's visual width, deliberately excluding the
// terminator marker cells showInvisibles appends: the caret's own column never
// counts them, so measuring against the marker-inflated width read as "one cell
// in from the edge" and silently withheld the phantom whenever invisibles were
// on.
//
// This is pure CARET geometry: a ghost column does not enter into it. Vertical
// movement sets a ghost even when the caret lands exactly at a line's end (the
// remembered column and the caret coincide there), so excluding ghosts here
// withheld the phantom from every arrival by arrow key. Where the ghost really
// matters is the horizontal FOLLOW, whose scroll target is the ghost rather
// than the caret; that is gated separately.
func (e *Editor) caretWantsPhantom(w *viewport.Viewport) bool {
	if w == nil || w.Buffer == nil {
		return false
	}
	// An insertion point past the leading edge needs content it stands AFTER,
	// so rune 0 cannot want the phantom for that reason. The bar case below is
	// the exception: it is about a bar needing the cell beside the character it
	// sits ON, which is exactly the line's first rune.
	afterContent := w.CursorPos().Rune > 0
	line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
	dispLine, curRune := e.displayCaretLine(w, line, w.CursorPos().Rune)
	tabSize := e.tabSize(w)
	col := e.caretVisualColumn(w, dispLine, curRune, tabSize)
	if !e.winRTL(w) {
		return afterContent && col == -1
	}
	vwContent := e.lineVisualWidth(w, dispLine, tabSize)
	caretRTL := bidi.RTLAt([]rune(dispLine), curRune, true)
	if col == vwContent {
		return afterContent && !caretRTL
	}
	// One more RTL case, for the BAR shapes alone. A bar draws on the LEFT edge
	// of the cell it is addressed to, so on the reading start's RTL character
	// its insertion point — that character's right edge — is one cell further
	// out. A mirrored gutter supplies that cell (positionCursor dips into it);
	// with line numbers off there is nothing past the content, and the bar
	// clamps back onto the character, landing exactly where the caret one rune
	// along would. The phantom gives it a cell of its own.
	if caretRTL && col == vwContent-1 && !w.LineNumbersVisible() {
		if shape := e.cursorStyleFor(w); shape == 5 || shape == 6 {
			return true
		}
	}
	return false
}

// reconcilePhantomColumn TAKES the phantom column for the caret as it now
// stands, WITHOUT otherwise disturbing the horizontal scroll — it only ever
// moves from 0 to -1, never touching a scrolled view.
//
// It does not release: this is caret VISIBILITY, not a snap. Whether a caret
// "wants" the phantom flips as the text around it changes — a trailing space is
// a neutral that resolves to the base direction, so typing one at the end of an
// LTR run in an RTL line reclassifies the caret for exactly one keystroke — and
// releasing on that would jerk the whole line back and forth by a column as you
// type. The phantom is given up only when some action genuinely requires a
// different scroll, which the follow's range checks then compute.
//
// Vertical movement deliberately leaves the horizontal offset alone (see
// ensureCursorVisibleVertical), so arriving at a line whose end sits at the
// leading edge would otherwise leave the caret pinned on the character it
// stands after — or, under rtl with a gutter, dipped onto a line-number digit —
// until some later command happened to run the full horizontal follow.
func (e *Editor) reconcilePhantomColumn(w *viewport.Viewport) {
	if w == nil {
		return
	}
	phantomOff := -1
	if w.Buffer != nil {
		if _, dw := e.lineDisplaySpans(w, w.CursorPos().Line); dw {
			phantomOff = -2 // a doubled row stores twice the display offset
		}
	}
	if w.ViewState.ViewOffsetX == 0 && e.caretWantsPhantom(w) {
		w.ViewState.ViewOffsetX = phantomOff
	}
}

// registerScrollCommands registers the commands that move the view rather than the caret.
func (e *Editor) registerScrollCommands(ps *pawscript.PawScript) {
	// Scroll commands park the viewport WITHOUT moving the caret — the
	// programmatic analogue of the mouse wheel. Each detaches the view from
	// caret-follow (ScrollDetached) so the per-frame follow leaves it put until a
	// cursor-movement or edit command re-engages it. They mirror the go_* family
	// name-for-name (scroll_line ~ go_line, scroll_line_next ~ go_line_next, ...).
	ps.RegisterCommand("scroll_line_prior", func(ctx *pawscript.Context) pawscript.Result {
		e.scrollViewByLines(e.resolveTargetMain(), -1)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_line_next", func(ctx *pawscript.Context) pawscript.Result {
		e.scrollViewByLines(e.resolveTargetMain(), 1)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_page_prior", func(ctx *pawscript.Context) pawscript.Result {
		if w := e.resolveTargetMain(); w != nil {
			_, page := e.pageSize(w)
			e.scrollViewByLines(w, -page)
		}
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_page_next", func(ctx *pawscript.Context) pawscript.Result {
		if w := e.resolveTargetMain(); w != nil {
			_, page := e.pageSize(w)
			e.scrollViewByLines(w, page)
		}
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_buffer_beg", func(ctx *pawscript.Context) pawscript.Result {
		e.scrollViewTo(e.resolveTargetMain(), 0)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_buffer_end", func(ctx *pawscript.Context) pawscript.Result {
		if w := e.resolveTargetMain(); w != nil && w.Buffer != nil {
			viewHeight, _ := e.pageSize(w)
			// Park the last line on the bottom row (clamped when the buffer is
			// shorter than the view).
			e.scrollViewTo(w, w.Buffer.GetLineCount()-viewHeight)
		}
		return pawscript.BoolStatus(true)
	})

	// scroll_line parks a given 1-based line at the TOP of the view (the scroll
	// analogue of go_line); it takes a line-number argument or, lacking one,
	// prompts for it exactly as go_line does.
	// scroll_viewport <id> <line>: park a NAMED viewport at a 0-based top
	// line. The host's graphical scrollbar drives scrolling with this — it
	// owns the bar in pixel space, so it computes the line itself and sends a
	// whole number; mew never scrolls by a fraction of a line. Like the wheel
	// and mew's own bar, it leaves the view detached and steals no focus, so a
	// background pane can be scrolled without disturbing the caret.
	ps.RegisterCommand("scroll_viewport", func(ctx *pawscript.Context) pawscript.Result {
		id, ok := argString(ctx, 0)
		if !ok {
			return pawscript.BoolStatus(false)
		}
		lineArg, ok := argString(ctx, 1)
		if !ok {
			return pawscript.BoolStatus(false)
		}
		top, err := strconv.Atoi(strings.TrimSpace(lineArg))
		if err != nil {
			return pawscript.BoolStatus(false)
		}
		w := e.ViewportManager.GetViewport(strings.TrimSpace(id))
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		e.scrollViewTo(w, top)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_line", func(ctx *pawscript.Context) pawscript.Result {
		scrollLine := func(input string) bool {
			n, err := strconv.Atoi(strings.TrimSpace(input))
			if err != nil || n < 1 {
				e.ShowWarning("Invalid line number")
				return false
			}
			e.scrollLineTop(n)
			return true
		}
		if arg, ok := argString(ctx, 0); ok {
			return pawscript.BoolStatus(scrollLine(arg))
		}
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		e.PromptMgr.PromptForInput("Scroll to line: ", "", func(accepted bool, _, input string) {
			defer e.RequestRender()
			if expired.Load() {
				e.ShowWarning("Prompt timed out")
				return
			}
			if !accepted || strings.TrimSpace(input) == "" {
				ctx.ResumeToken(token, false)
				return
			}
			ctx.ResumeToken(token, scrollLine(input))
		}, "scrollline")
		return pawscript.TokenResult(token)
	})

	// Scroll commands
	// scroll_left / scroll_right are VISUAL, not reading-relative: unlike the
	// _prior/_next commands, which follow the text's own direction, these move
	// the view the way the words name whichever way the text runs. Under
	// direction=rtl that is the opposite sign on the stored offset — see
	// scrollViewHorizontal.
	ps.RegisterCommand("scroll_left", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.scrollViewHorizontal(e.ViewportManager.GetFocusedViewport(), -1))
	})

	ps.RegisterCommand("scroll_right", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.scrollViewHorizontal(e.ViewportManager.GetFocusedViewport(), +1))
	})
}
