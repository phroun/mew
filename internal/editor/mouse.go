package editor

import (
	"strings"
	"time"

	"github.com/phroun/mew/internal/render"
	"github.com/phroun/mew/internal/window"
)

// Mouse input (TUI). The key layer (direct-key-handler) decodes SGR/X10
// mouse reports into pseudo-keys — "Mouse@x,y" (position, emitted before its
// action), "MouseLeftPress"/"MouseLeftRelease"/"MouseScrollUp"/... and drags
// as "MouseLeftDrag@x,y" — once the terminal is asked to report the mouse at
// all (see EnableMouseReporting; purfecterm answers the same DECSET trio by
// routing mouse to the app instead of local selection).
//
// mew's semantics — MODAL-SAFE: only the FOCUSED window processes mouse
// actions. A click in any other window is ignored outright (no focus steal),
// so a modal prompt keeps the whole keyboard-and-mouse stage to itself.
//   - A left press in the focused window's content area sets the caret to
//     the clicked cell (tab-, bidi-, double-width- and button-substitution-
//     aware).
//   - In browse mode, a press ON a link button CAPTURES it: the button
//     shows the pressed style while the pointer is over it, reverts to its
//     focused style when dragged off (the capture holds), and re-presses
//     when dragged back on. Releasing on the captured button follows the
//     link exactly as keyboard navigation would; releasing anywhere else
//     abandons the click.
//   - The scroll wheel scrolls the focused window (when under the pointer).
//   - With all-motion tracking delivered (the graphical build), the link or
//     button under the pointer takes a hover style.

// pressedLink identifies a link by position (window identity, document
// line, span start) — used for the mouse CAPTURE (the button a press
// grabbed, held until release) and for hover. Ephemeral press-to-release /
// motion-to-motion state: identity by position is fine at this lifetime.
type pressedLink struct {
	active bool
	winID  string
	line   int
	start  int
}

// dragSelState tracks a drag block-selection in progress. A plain left press
// in the focused window's content area arms it (begun=false): the first drag
// onto a DIFFERENT cell places _block_begin at the press origin, and every
// drag onto a new cell re-places _block_end (the caret follows). A
// shift+click arms it pre-begun (begun=true): _block_begin is already placed
// at the ORIGINAL caret position and only _block_end follows the drag.
// Press-to-release lifetime, focused window only.
//
// shifted records the gesture's origin for the buffer's mouse-block flag: a
// PLAIN drag makes a transient selection (mouseBlock on — a later plain
// click dissolves it), while a shift+click gesture — including its
// continuing drag — is the mouse user's DELIBERATE, persistent selection
// (mouseBlock off, like a keyboard-made block).
type dragSelState struct {
	active     bool
	begun      bool
	shifted    bool
	winID      string
	originLine int
	originRune int
	lastLine   int
	lastRune   int
}

// handleMouseKey consumes mouse pseudo-keys from the key stream. Reports
// true when the key was a mouse event (handled or deliberately ignored), so
// the caller skips keymap dispatch.
func (e *Editor) handleMouseKey(key string) bool {
	// Strip modifier prefixes, remembering SHIFT (a shift+click extends the
	// block from the caret — see mousePress) separately from EVERY OTHER
	// modifier (meta/alt, ctrl, super/hyper): any of those on a left-click
	// stands in for a right-click, because which modified clicks a terminal
	// actually lets through varies wildly.
	base := key
	shift, mod := false, false
	for {
		switch {
		case strings.HasPrefix(base, "S-"):
			shift = true
			base = base[2:]
			continue
		case strings.HasPrefix(base, "M-"), strings.HasPrefix(base, "C-"), strings.HasPrefix(base, "H-"):
			mod = true
			base = base[2:]
			continue
		}
		break
	}
	if !strings.HasPrefix(base, "Mouse") {
		return false
	}

	e.renderMu.Lock()
	defer e.renderMu.Unlock()

	switch {
	case strings.HasPrefix(base, "Mouse@"):
		if x, y, ok := parseMouseAt(base[len("Mouse@"):]); ok {
			e.mouseX, e.mouseY = x, y
		}
	case base == "MouseLeftPress":
		// Any modifier beyond shift on a left-click is a RIGHT-click
		// alternative (some terminals never deliver a real right button —
		// or reserve alt-click for themselves; ctrl/super+click covers
		// those).
		switch {
		case mod:
			e.mouseRightPress(e.mouseX, e.mouseY)
		case e.modebarNavPressAt(e.mouseX, e.mouseY):
			// Consumed by a modebar nav-history button (capture started).
		default:
			e.mousePress(e.mouseX, e.mouseY, shift)
		}
	case strings.HasPrefix(base, "MouseLeftDrag@"):
		if x, y, ok := parseMouseAt(base[len("MouseLeftDrag@"):]); ok {
			e.mouseX, e.mouseY = x, y
			if e.modebarNavCapture != 0 {
				e.modebarNavDrag(x, y)
			} else {
				e.mouseDrag(x, y)
			}
		}
	case base == "MouseLeftRelease", base == "MouseRelease":
		if e.modebarNavCapture != 0 {
			e.modebarNavRelease(e.mouseX, e.mouseY)
		} else {
			e.mouseRelease(e.mouseX, e.mouseY)
		}
	case base == "MouseRightPress":
		e.mouseRightPress(e.mouseX, e.mouseY)
	case strings.HasPrefix(base, "MouseDrag@"):
		// Plain motion, no button (all-motion tracking): hover.
		if x, y, ok := parseMouseAt(base[len("MouseDrag@"):]); ok {
			e.mouseX, e.mouseY = x, y
			e.mouseHoverAt(x, y)
			e.modebarNavHoverAt(x, y)
		}
	case base == "MouseScrollUp":
		e.hScrollReset() // a vertical tick re-arms the sideways barrier
		e.mouseScroll(e.mouseX, e.mouseY, -3)
	case base == "MouseScrollDown":
		e.hScrollReset()
		e.mouseScroll(e.mouseX, e.mouseY, +3)
	case base == "MouseScrollLeft":
		e.mouseScrollHoriz(e.mouseY, -1)
	case base == "MouseScrollRight":
		e.mouseScrollHoriz(e.mouseY, +1)
	}
	// Every other Mouse* event (middle button, right release/drags) is
	// swallowed so it never leaks into keymap dispatch.
	return true
}

// notifyPointerRegion publishes the rectangle where a graphical host should
// show the text I-beam (Config.PointerRegion): the FOCUSED window's editable
// content area — its cells, including the blank rows below the document that
// still follow click-to-EOF — in 1-based terminal cells. Everything outside it
// is the ordinary arrow: the gutter (left of the content), the modebar and
// other chrome (other windows), an unfocused pane, and — when a prompt holds
// focus — the document area (only the prompt's own field then yields the
// I-beam, a cue that input is awaited there).
//
// Pushed after each render, on the first computation and thereafter only when
// the rectangle changes (layout, focus, scroll) — NOT per mouse motion — so
// the host resolves per-pixel cursor queries locally. Runs under renderMu with
// the frame's geometry already set by the renderer.
func (e *Editor) notifyPointerRegion() {
	if e.Config.PointerRegion == nil {
		return
	}
	var rect [4]int // col, row, width, height (1-based cells; zero w/h = none)
	var arrows []PointerArrowSpan
	if w := e.pointerRegionWindow(); w != nil {
		rect = [4]int{w.ContentX + 1, w.ContentY + 1, w.ContentWidth, w.ContentHeight}
		arrows = e.pointerArrowSpans(w)
	}
	if !e.pointerRegionPushed || rect != e.pointerRegionSent || !arrowSpansEqual(arrows, e.pointerArrowsSent) {
		e.pointerRegionPushed = true
		e.pointerRegionSent = rect
		e.pointerArrowsSent = arrows
		e.Config.PointerRegion(rect[0], rect[1], rect[2], rect[3], arrows)
	}
}

// pointerArrowSpans returns the on-screen cell spans of the focused window's
// browse-mode link BUTTONS — the buttons that sit INSIDE the text region and
// so must show the arrow, not the I-beam. Empty unless the window is in browse
// mode over a linkable buffer. LTR only: an RTL page's right-anchored button
// columns are not mapped, so its buttons fall back to the I-beam (an exotic
// edge — RTL wiki browsing).
func (e *Editor) pointerArrowSpans(w *window.Window) []PointerArrowSpan {
	if w == nil || w.Buffer == nil || !w.BrowseActive || !w.ViewState.LinkBrowsing || e.winRTL(w) {
		return nil
	}
	tabSize := e.tabSize(w)
	top := w.ViewState.ViewOffsetY
	bottom := top + w.ContentHeight
	if n := w.Buffer.GetLineCount(); bottom > n {
		bottom = n
	}
	loCol := w.ContentX + 1
	hiCol := w.ContentX + w.ContentWidth + 1 // exclusive
	var arrows []PointerArrowSpan
	for docLine := top; docLine < bottom; docLine++ {
		screenRow := w.ContentY + 1 + (docLine - top)
		for _, s := range e.linkSpansOnLine(w, docLine) {
			c0 := e.displayVisualColumn(w, docLine, s.Start, tabSize)
			c1 := e.displayVisualColumn(w, docLine, s.End, tabSize)
			if c1 < c0 {
				c0, c1 = c1, c0
			}
			// Visual columns -> screen columns (LTR): the content origin plus
			// the visual column offset by the horizontal scroll. Clamp to the
			// window's visible content columns.
			col0 := w.ContentX + 1 + (c0 - w.ViewState.ViewOffsetX)
			col1 := w.ContentX + 1 + (c1 - w.ViewState.ViewOffsetX)
			if col0 < loCol {
				col0 = loCol
			}
			if col1 > hiCol {
				col1 = hiCol
			}
			if col1 > col0 {
				arrows = append(arrows, PointerArrowSpan{Row: screenRow, Col: col0, Width: col1 - col0})
			}
		}
	}
	return arrows
}

// arrowSpansEqual reports whether two exclusion-span slices are element-equal.
func arrowSpansEqual(a, b []PointerArrowSpan) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// pointerRegionWindow returns the window whose editable text drives the I-beam
// region. It is the focused window ONLY when that window is actually on screen
// — VISIBLE, holding a buffer, and laid out this frame (non-zero content
// geometry). A focused window that is invisible or not laid out (a background
// or stacked window that never rendered) has stale or zero geometry that would
// blank or misplace the region — the I-beam then never lands over the visible
// text — so we fall back to the visible document instead. A visible prompt is a
// legitimate focus target (its own field shows the I-beam); only genuinely
// off-screen focus falls through to the document.
func (e *Editor) pointerRegionWindow() *window.Window {
	onScreen := func(w *window.Window) bool {
		return w != nil && w.Visible && w.Buffer != nil &&
			w.ContentWidth > 0 && w.ContentHeight > 0
	}
	if w := e.WindowManager.GetFocusedWindow(); onScreen(w) {
		return w
	}
	if m := e.WindowManager.GetLastMainWindow(); onScreen(m) {
		return m
	}
	return nil
}

// promptHasPriority reports whether a modal prompt currently holds focus, so
// mouse interactions on the document/chrome (the modebar nav buttons, their
// hover styling) stand down while input is awaited at the prompt.
func (e *Editor) promptHasPriority() bool {
	w := e.WindowManager.GetFocusedWindow()
	return w != nil && w.Type == window.PromptWindow
}

// notifyEditState tells the host (via Config.EditState) whenever the FOCUSED
// window's read-only state changes — a host greys out its mutating
// affordances (the Edit menu's Cut) while a read-only buffer holds focus.
// Pushed once at the first render, then only on transitions. Called from
// performRender, which runs after every state-changing event.
func (e *Editor) notifyEditState() {
	if e.Config.EditState == nil {
		return
	}
	ro := false
	if w := e.WindowManager.GetFocusedWindow(); w != nil {
		ro = w.ViewState.ReadOnly
	}
	if !e.readOnlyPushed || ro != e.readOnlySent {
		e.readOnlyPushed = true
		e.readOnlySent = ro
		e.Config.EditState(ro)
	}
}

// notifyHelpState tells the host (via Config.HelpState) whether the built-in
// help window is open, once at the first render and thereafter on transitions,
// so a host keeps a "Quick Help" menu checkmark in sync as help_toggle (or a
// close) opens and closes it. Called from performRender.
func (e *Editor) notifyHelpState() {
	if e.Config.HelpState == nil {
		return
	}
	open := e.quickHelpWindowOpen()
	if !e.helpStatePushed || open != e.helpStateSent {
		e.helpStatePushed = true
		e.helpStateSent = open
		e.Config.HelpState(open)
	}
}

// parseMouseAt parses the "x,y" tail of a mouse position (1-based terminal
// coordinates).
func parseMouseAt(s string) (x, y int, ok bool) {
	comma := strings.IndexByte(s, ',')
	if comma <= 0 {
		return 0, 0, false
	}
	toInt := func(t string) (int, bool) {
		n := 0
		if t == "" {
			return 0, false
		}
		for _, c := range t {
			if c < '0' || c > '9' {
				return 0, false
			}
			n = n*10 + int(c-'0')
		}
		return n, true
	}
	x, okX := toInt(s[:comma])
	y, okY := toInt(s[comma+1:])
	return x, y, okX && okY
}

// mouseHit resolves 1-based screen coordinates to a window and document
// position. ok is false outside every window's content area. The column math
// mirrors the painter: gutter/margins, horizontal scroll, double-width rows
// (half-width gutter, two columns per cell), bidi visual order, tabs, and
// browse-mode display substitution all resolve back to a document rune;
// clicking button chrome parks inside the button's source span.
func (e *Editor) mouseHit(x, y int) (w *window.Window, docLine, runePos int, ok bool) {
	w = e.windowAtRow(y)
	if w == nil || w.Buffer == nil {
		return nil, 0, 0, false
	}
	docLine = w.ViewState.ViewOffsetY + (y - 1 - w.ContentY)
	if docLine < 0 || docLine >= w.Buffer.GetLineCount() {
		return nil, 0, 0, false
	}

	raw := strings.TrimRight(w.Buffer.GetLine(docLine), "\n\r")
	spans, dw := e.lineDisplaySpans(w, docLine)
	dispLine := raw
	var dispToDoc []int
	if len(spans) > 0 || dw {
		dispLine, dispToDoc = render.SubstituteDisplay(raw, spans, dw)
	}

	// Geometry in the row's cell space. ContentX already includes the full
	// gutter (LTR); a double-width row shows half as many gutter cells and
	// each content cell spans two physical columns.
	lineNumWidth := 0
	if w.ViewState.ShowLineNumbers {
		lineNumWidth = w.LineNumWidth
	}
	base := w.ContentX + 1 // first content column, 1-based
	cells := w.ContentWidth
	viewOff := w.ViewState.ViewOffsetX
	cell := x - base
	if dw {
		if !e.winRTL(w) {
			base = w.ContentX - lineNumWidth + lineNumWidth/2 + 1
		}
		cell = (x - base) / 2
		cells /= 2
		viewOff /= 2
	}
	if cell < 0 || (cells > 0 && cell >= cells) {
		return nil, 0, 0, false
	}

	target := cell + viewOff
	if e.winRTL(w) {
		// Right-anchored view: visible visual columns are [vw-off-width,
		// vw-off), with left padding when the line is narrower.
		vw := e.lineVisualWidth(w, dispLine, e.tabSize(w))
		eff := vw - w.ViewState.ViewOffsetX - cells
		pad := 0
		if eff < 0 {
			pad = -eff
			eff = 0
		}
		target = cell - pad + eff
		if target < 0 {
			target = 0
		}
	}

	idx := e.runeAtVisualColumn(w, dispLine, target)
	if dispToDoc != nil {
		if idx >= len(dispToDoc) {
			// Past the end of the display line: the caret goes to the END of
			// the document line — never inside a trailing button's span, so
			// a click in the void after a button places the caret instead of
			// following.
			idx = len([]rune(raw))
		} else {
			idx = displayToDoc(dispToDoc, idx)
		}
	}
	return w, docLine, idx, true
}

// windowAtRow finds the visible window whose CONTENT area covers the 1-based
// screen row (the renderer maintains ContentY/ContentHeight per frame). The
// FOCUSED window wins outright when it covers the row: only the current main
// window is laid out each frame, so a background main window's stale
// geometry can cover the same rows — and every mouse action is
// focused-gated anyway, so the focused window is the only correct answer
// wherever it covers.
func (e *Editor) windowAtRow(y int) *window.Window {
	row := y - 1 // ContentY is 0-based
	covers := func(w *window.Window) bool {
		return w != nil && w.Visible && w.Buffer != nil &&
			row >= w.ContentY && row < w.ContentY+w.ContentHeight
	}
	if fw := e.WindowManager.GetFocusedWindow(); covers(fw) {
		return fw
	}
	var best *window.Window
	for _, w := range e.WindowManager.AllWindows() {
		if !covers(w) {
			continue
		}
		// Prefer main buffers when areas would overlap (stale geometry
		// on hidden windows).
		if best == nil || (!best.FocusEligible() && w.FocusEligible()) {
			best = w
		}
	}
	return best
}

// runeAtVisualColumn is the inverse of the caret-column math: the logical
// index of the rune whose visual cell run covers the target column, or the
// line length when the target lies past the end.
func (e *Editor) runeAtVisualColumn(w *window.Window, line string, target int) int {
	runes := []rune(line)
	tabSize := e.tabSize(w)
	layout := e.layoutFor(w, runes)
	if layout == nil {
		col := 0
		for i, r := range runes {
			wd := e.getRuneVisualWidth(r, col, tabSize)
			if target < col+wd {
				return i
			}
			col += wd
		}
		return len(runes)
	}
	cols, total := e.bidiColumns(runes, layout, e.lineMarkSet(w, runes), tabSize)
	if target >= total {
		return len(runes)
	}
	for i := range runes {
		wd := e.slotWidth(layout, runes, i, cols[i], tabSize)
		if wd > 0 && target >= cols[i] && target < cols[i]+wd {
			return i
		}
	}
	return len(runes)
}

// displayToDoc maps a display index to a document rune through DispToDoc:
// chrome cells (-1, button caps/shadow/isolates) park INSIDE the button's
// source span, so a click on any part of a button focuses it.
func displayToDoc(dispToDoc []int, idx int) int {
	if len(dispToDoc) == 0 {
		return idx
	}
	if idx >= len(dispToDoc) {
		// Past end of display: one past the last mapped doc rune.
		for i := len(dispToDoc) - 1; i >= 0; i-- {
			if dispToDoc[i] >= 0 {
				return dispToDoc[i] + 1
			}
		}
		return 0
	}
	if d := dispToDoc[idx]; d >= 0 {
		return d
	}
	for i := idx - 1; i >= 0; i-- {
		if dispToDoc[i] >= 0 {
			return dispToDoc[i] + 1 // just after the doc rune left of the chrome
		}
	}
	for i := idx + 1; i < len(dispToDoc); i++ {
		if dispToDoc[i] >= 0 {
			if d := dispToDoc[i]; d > 0 {
				return d - 1 // just inside the span whose chrome starts the line
			}
			return 0
		}
	}
	return 0
}

// mousePress: set the caret to the clicked cell and — on a link button in
// browse mode — arm the pressed style. ONLY the focused window processes
// presses: a click anywhere else is ignored (no focus steal), preserving the
// modal prompt system.
//
// A SHIFT+click extends instead: the block is marked from the caret's
// CURRENT position — a document position, which may be scrolled out of view —
// to the clicked cell, and the caret moves to the click. A drag continuing
// from the shift+click keeps moving only the block's end.
//
// A plain press also ARMS drag selection: if the pointer then drags to a new
// cell before release, the block marks from the press origin (see mouseDrag /
// dragSelUpdate). A press that captured a link button does not — its drag
// tracks the button.
func (e *Editor) mousePress(x, y int, shift bool) {
	w, docLine, runePos, ok := e.mouseHit(x, y)
	if !ok {
		// A press on the BLANK AREA below the document's last line (still
		// inside the window's content area) means the END of the document:
		// click below the doc and drag upward to select its tail.
		w, docLine, runePos, ok = e.mouseHitBelowText(x, y)
	}
	if !ok || e.WindowManager.GetFocusedWindow() != w {
		return
	}

	if shift {
		// Extend from the caret's current document position to the click.
		// A shift+click is the mouse user's DELIBERATE selection: the
		// mouse-block flag goes OFF, so this block persists through later
		// plain clicks exactly like a keyboard-made one.
		origin := w.CursorPos()
		w.Buffer.SetMark("_block_begin", origin.Line, origin.Rune)
		w.Buffer.SetMark("_block_end", docLine, runePos)
		w.Buffer.SetMouseBlock(false)
		e.dragSel = dragSelState{
			active: true, begun: true, shifted: true, winID: w.ID,
			originLine: origin.Line, originRune: origin.Rune,
			lastLine: docLine, lastRune: runePos,
		}
		w.SetCursorPos(window.Position{Line: docLine, Rune: runePos})
		e.afterHorizontalMovement(w)
		w.ViewState.ScrollDetached = false
		e.updateBrowseState()
		e.RequestRender()
		return
	}

	// A plain click dissolves a MOUSE-made block (a transient drag
	// selection); a keyboard-made or shift+click-made block survives.
	if w.Buffer.MouseBlock() {
		w.Buffer.ClearBlockMarks() // clears the flag with the marks
	}

	w.SetCursorPos(window.Position{Line: docLine, Rune: runePos})
	e.afterHorizontalMovement(w)
	// A click is a cursor movement: re-engage caret following, cancelling any
	// free scroll left by the wheel so the view tracks the caret again.
	w.ViewState.ScrollDetached = false
	e.updateBrowseState()
	if span := e.focusedLinkButton(w); span != nil {
		e.mousePressed = pressedLink{active: true, winID: w.ID, line: docLine, start: span.Start}
		e.mouseOnCaptured = true
	} else {
		// Arm drag selection from this press origin; it only takes effect
		// when the drag reaches a different cell (dragSelUpdate).
		e.dragSel = dragSelState{
			active: true, winID: w.ID,
			originLine: docLine, originRune: runePos,
			lastLine: docLine, lastRune: runePos,
		}
	}
	e.RequestRender()
}

// mouseRightPress: a right-click within the EDITING AREA of the focused
// window asks the host to pop its context menu at the clicked cell
// (Config.ShowContextMenu). The gate is mouseHit + the focused-window rule —
// exactly the left-click caret path's routing — so the modebar, gutters,
// column ruler, and title/message rows never pop the menu, and neither does
// any unfocused window (modal safety, like every mouse action). The caret
// does NOT move: a right-click inspects, it doesn't relocate — moving it
// would silently change what a subsequent paste targets (caret-in-block).
func (e *Editor) mouseRightPress(x, y int) {
	if e.Config.ShowContextMenu == nil {
		return
	}
	w, _, _, ok := e.mouseHit(x, y)
	if !ok {
		// The blank area below the document's last line counts as the
		// editing area too — same as a left press there.
		w, _, _, ok = e.mouseHitBelowText(x, y)
	}
	if !ok || e.WindowManager.GetFocusedWindow() != w {
		return
	}
	e.Config.ShowContextMenu(x, y)
}

// mouseDrag: with a captured link button, the button tracks the pointer —
// pressed style while over it, its ordinary (focused) style while dragged
// off, re-pressed when dragged back on (the capture holds until release).
// Otherwise an armed drag selection extends the block (dragSelUpdate).
func (e *Editor) mouseDrag(x, y int) {
	if e.mousePressed.active {
		if on := e.hitOnPressedButton(x, y); on != e.mouseOnCaptured {
			e.mouseOnCaptured = on
			e.RequestRender()
		}
		return
	}
	if e.dragSel.active {
		e.dragSelUpdate(x, y)
	}
}

// dragSelUpdate extends the drag block-selection to the position under the
// pointer. The first drag onto a position that differs from the press origin
// places _block_begin at the origin (a click that never leaves its cell
// marks nothing); after that, every NEW position re-places _block_end there
// and the caret follows.
//
// The drag is CAPTURED: while the button is held, positions outside the
// content area still resolve instead of being ignored (dragSelResolve) — the
// gutter/line-number side means the START of that row's line, so selecting
// exactly to line beginnings just means dragging into the gutter, no
// precision required; rows above/below the content clamp to the nearest
// text row, and columns past the far edge clamp to the last visible cell.
func (e *Editor) dragSelUpdate(x, y int) {
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil || w.ID != e.dragSel.winID {
		return
	}
	e.dragScrollTrack(w, x, y)
	docLine, runePos, ok := e.dragSelResolve(w, x, y)
	if !ok {
		return
	}
	if !e.dragSel.begun {
		if docLine == e.dragSel.originLine && runePos == e.dragSel.originRune {
			return // still on the press cell: no selection yet
		}
		w.Buffer.SetMark("_block_begin", e.dragSel.originLine, e.dragSel.originRune)
		e.dragSel.begun = true
	}
	if docLine == e.dragSel.lastLine && runePos == e.dragSel.lastRune {
		return // same cell as the last update
	}
	w.Buffer.SetMark("_block_end", docLine, runePos)
	// A plain drag marks a TRANSIENT mouse block (a later plain click
	// dissolves it); a drag continuing a shift+click keeps that gesture's
	// deliberate, persistent nature.
	w.Buffer.SetMouseBlock(!e.dragSel.shifted)
	e.dragSel.lastLine, e.dragSel.lastRune = docLine, runePos
	w.SetCursorPos(window.Position{Line: docLine, Rune: runePos})
	e.afterHorizontalMovement(w)
	// While an edge autoscroll is engaged the ticker OWNS the viewport (a free
	// scroll, ScrollDetached). ensureCursorVisible re-attaches caret-following
	// and clamps the view back to the caret, which fights the free scroll and
	// stalls the autoscroll — so skip it while overshoot is nonzero. The caret
	// sits at the clamped edge, which the autoscrolled view keeps visible
	// anyway; a drag INSIDE the content (no overshoot) follows as usual.
	if e.dragScroll.vert == 0 && e.dragScroll.horiz == 0 {
		e.ensureCursorVisible(w)
	}
	e.RequestRender()
}

// mouseHitBelowText resolves a click that mouseHit rejected when it landed
// on the blank rows BELOW the document's last line, inside a window's
// content area (content columns only — the gutter stays inert, as on text
// rows). It answers the END of the document (last line, end of line), so a
// click below the doc parks the caret at EOF — and a drag upward from there
// selects the document's tail.
func (e *Editor) mouseHitBelowText(x, y int) (w *window.Window, docLine, runePos int, ok bool) {
	w = e.windowAtRow(y)
	if w == nil || w.Buffer == nil {
		return nil, 0, 0, false
	}
	lineCount := w.Buffer.GetLineCount()
	row := w.ViewState.ViewOffsetY + (y - 1 - w.ContentY)
	if row < lineCount || lineCount < 1 {
		// Not a below-the-text row: this was some other rejection (gutter,
		// margin) — stay inert.
		return nil, 0, 0, false
	}
	if x < w.ContentX+1 || x > w.ContentX+w.ContentWidth {
		return nil, 0, 0, false
	}
	docLine = lineCount - 1
	runePos = len([]rune(strings.TrimRight(w.Buffer.GetLine(docLine), "\n\r")))
	return w, docLine, runePos, true
}

// dragSelResolve resolves a held-drag pointer position to a document
// position in w, CLAMPING instead of rejecting (the drag owns the pointer):
//
//   - rows above/below the window's text clamp to the nearest row that
//     holds a visible line;
//   - the gutter side (line numbers — left in LTR, right in RTL) resolves
//     to rune 0 of the row's line, so a drag into the gutter pins the
//     selection to the line's beginning;
//   - the far side clamps to the last content column (the rightmost —
//     in RTL leftmost — visible cell, or the line end when the line ends
//     inside the view).
//
// ok is false only when the window shows no text at all or the clamped
// position still fails to resolve (a double-width edge case).
func (e *Editor) dragSelResolve(w *window.Window, x, y int) (docLine, runePos int, ok bool) {
	lineCount := w.Buffer.GetLineCount()
	visText := lineCount - w.ViewState.ViewOffsetY
	if visText > w.ContentHeight {
		visText = w.ContentHeight
	}
	if visText < 1 {
		return 0, 0, false
	}

	// Dragged below the document's LAST line (its row is visible and the
	// pointer sits below it): the selection reaches the END of the document
	// (EOF), not just the horizontal position on the last row. When the last
	// line is scrolled off the bottom the pointer can't be below its row, so
	// this doesn't fire and the autoscroll below carries the drag onward.
	lastLine := lineCount - 1
	lastLineRow := w.ContentY + 1 + (lastLine - w.ViewState.ViewOffsetY)
	if y > lastLineRow {
		endRune := len([]rune(strings.TrimRight(w.Buffer.GetLine(lastLine), "\n\r")))
		return lastLine, endRune, true
	}

	top := w.ContentY + 1 // 1-based first content row
	bottom := w.ContentY + visText
	if y < top {
		y = top
	}
	if y > bottom {
		y = bottom
	}
	docLine = w.ViewState.ViewOffsetY + (y - 1 - w.ContentY)
	if docLine < 0 {
		docLine = 0
	}
	if docLine >= lineCount {
		docLine = lineCount - 1
	}

	first := w.ContentX + 1 // 1-based first content column
	last := w.ContentX + w.ContentWidth
	if (!e.winRTL(w) && x < first) || (e.winRTL(w) && x > last) {
		// Over the gutter: the START of this row's line.
		return docLine, 0, true
	}
	if x < first {
		x = first
	}
	if x > last {
		x = last
	}
	if hw, hl, hr, hok := e.mouseHit(x, y); hok && hw == w {
		return hl, hr, true
	}
	return 0, 0, false
}

// Drag-edge autoscroll: while a drag selection holds the pointer beyond the
// window's top/bottom (or parked on the far column), the view scrolls and
// the selection keeps extending — after a short delay so an ordinary drag
// that clips an edge never scrolls by accident, at a speed taken from how
// far past the edge the pointer sits. Vertical scrolling uses the shared
// free-scroll (scrollViewByLines); horizontal stays LOCK-STEPPED to the
// scroll_left/scroll_right commands, so its step and clamping are exactly
// the keyboard's. The main loop is event-driven (a held-still pointer emits
// nothing), so a ticker goroutine drives the repeats, marshaling each tick
// through PostAction; dragScrollPending keeps at most one tick in flight.
const (
	dragScrollDelay    = 350 * time.Millisecond
	dragScrollInterval = 70 * time.Millisecond
	dragScrollMaxLines = 8
	dragScrollMaxReps  = 3 // horizontal command invocations per tick, at most
)

// dragScrollState is the overshoot the ticker acts on, updated by every
// drag motion (dragScrollTrack) and consumed by dragScrollTick.
type dragScrollState struct {
	vert  int           // rows past the top (negative) / bottom (positive); 0 = none
	horiz int           // columns at/past the FAR side (gutter side never scrolls: it pins to line start)
	since time.Time     // when overshoot last became nonzero (the delay gate)
	stop  chan struct{} // closes to end the ticker goroutine; nil when not running
}

// dragScrollTrack derives the current overshoot from a drag position.
// Vertical engages strictly beyond the content rows; horizontal engages AT
// the far column too (the pointer cannot leave the terminal grid sideways,
// so parking on the last column is the far-edge gesture). The gutter side
// never scrolls — dragSelResolve pins it to the line start instead.
func (e *Editor) dragScrollTrack(w *window.Window, x, y int) {
	top := w.ContentY + 1
	bottom := w.ContentY + w.ContentHeight
	vert := 0
	if y < top {
		vert = y - top
	} else if y > bottom {
		vert = y - bottom
	}

	first := w.ContentX + 1
	last := w.ContentX + w.ContentWidth
	horiz := 0
	if !e.winRTL(w) && x >= last {
		horiz = x - last + 1
	} else if e.winRTL(w) && x <= first {
		horiz = first - x + 1
	}

	ds := &e.dragScroll
	had := ds.vert != 0 || ds.horiz != 0
	ds.vert, ds.horiz = vert, horiz
	if vert == 0 && horiz == 0 {
		return
	}
	if !had {
		ds.since = time.Now()
	}
	if ds.stop == nil {
		ds.stop = make(chan struct{})
		go e.dragScrollLoop(ds.stop)
	}
}

// dragScrollStop ends the autoscroll ticker (mouse release).
func (e *Editor) dragScrollStop() {
	if e.dragScroll.stop != nil {
		close(e.dragScroll.stop)
	}
	e.dragScroll = dragScrollState{}
}

// dragScrollLoop is the ticker goroutine: it posts dragScrollTick onto the
// editor main loop at the scroll cadence, one tick in flight at a time
// (a tick that cannot be consumed — a torn-down session — just parks).
func (e *Editor) dragScrollLoop(stop chan struct{}) {
	t := time.NewTicker(dragScrollInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if e.dragScrollPending.CompareAndSwap(false, true) {
				posted := e.PostAction(func() {
					e.dragScrollPending.Store(false)
					e.dragScrollTick()
				})
				if !posted {
					e.dragScrollPending.Store(false)
					return
				}
			}
		}
	}
}

// dragScrollTick runs on the main loop: after the delay gate, scroll by the
// overshoot-scaled step and re-extend the selection to the pointer under
// the moved viewport.
func (e *Editor) dragScrollTick() {
	ds := &e.dragScroll
	if !e.dragSel.active || (ds.vert == 0 && ds.horiz == 0) {
		return
	}
	if time.Since(ds.since) < dragScrollDelay {
		return
	}
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil || w.ID != e.dragSel.winID {
		return
	}

	if ds.vert != 0 {
		lines := ds.vert
		if lines > dragScrollMaxLines {
			lines = dragScrollMaxLines
		}
		if lines < -dragScrollMaxLines {
			lines = -dragScrollMaxLines
		}
		e.scrollViewByLines(w, lines)
	}

	if ds.horiz != 0 {
		// Scroll toward the far side only while the drag row's line actually
		// continues past the view (scroll_right never clamps on its own, and
		// running past the text would strand the view in blank space).
		raw := strings.TrimRight(w.Buffer.GetLine(e.dragSel.lastLine), "\n\r")
		if e.lineVisualWidth(w, raw, e.tabSize(w)) > w.ViewState.ViewOffsetX+w.ContentWidth {
			cmd := "scroll_right"
			if e.winRTL(w) {
				cmd = "scroll_left"
			}
			reps := 1 + (ds.horiz-1)/8
			if reps > dragScrollMaxReps {
				reps = dragScrollMaxReps
			}
			for i := 0; i < reps; i++ {
				e.executeCommand(cmd)
			}
		}
	}

	// The viewport moved: the same pointer position now resolves further
	// into the document — extend the selection to it.
	e.dragSelUpdate(e.mouseX, e.mouseY)
}

// mouseRelease: releasing ON the captured button follows the link, exactly
// as keyboard navigation would; releasing anywhere else abandons the click.
// Either way the capture — and any armed drag selection, with its edge
// autoscroll — ends (a block the drag marked stays marked).
func (e *Editor) mouseRelease(x, y int) {
	e.dragSel = dragSelState{}
	e.dragScrollStop()
	if !e.mousePressed.active {
		return
	}
	onButton := e.hitOnPressedButton(x, y)
	e.mousePressed = pressedLink{}
	e.mouseOnCaptured = false
	if onButton {
		e.navFollow()
	}
	e.RequestRender()
}

// mouseHoverAt tracks the link under the pointer (plain motion, no button).
// Hover follows the same modal rule as every mouse action — only the focused
// window's links light up — and repaints only when the hovered identity
// actually changes.
func (e *Editor) mouseHoverAt(x, y int) {
	nh := pressedLink{}
	if w, docLine, runePos, ok := e.mouseHit(x, y); ok &&
		e.WindowManager.GetFocusedWindow() == w && w.ViewState.LinkBrowsing {
		for _, s := range e.linkSpansOnLine(w, docLine) {
			if s.Start <= runePos && runePos < s.End {
				nh = pressedLink{active: true, winID: w.ID, line: docLine, start: s.Start}
				break
			}
		}
	}
	if nh != e.mouseHovered {
		e.mouseHovered = nh
		e.RequestRender()
	}
}

// hitOnPressedButton reports whether the coordinates land on the very button
// the press CAPTURED (same window, same line, same span).
func (e *Editor) hitOnPressedButton(x, y int) bool {
	w, docLine, runePos, ok := e.mouseHit(x, y)
	if !ok || w.ID != e.mousePressed.winID || docLine != e.mousePressed.line {
		return false
	}
	for _, s := range e.linkSpansOnLine(w, docLine) {
		if s.Start == e.mousePressed.start {
			return s.Start <= runePos && runePos < s.End
		}
	}
	return false
}

// hScrollBarrier is how many horizontal wheel ticks must accumulate in one
// direction before a sideways scroll engages — a deliberately higher bar than
// the vertical wheel (which acts on the first tick), so incidental left/right
// motion during a normal up/down scroll does not drift the view sideways.
const hScrollBarrier = 3

// hScrollReset re-arms the horizontal barrier: the next sideways gesture must
// clear hScrollBarrier ticks again. Called on any vertical wheel tick.
func (e *Editor) hScrollReset() {
	e.hScrollAccum = 0
	e.hScrollEngaged = false
	e.hScrollDir = 0
}

// mouseScrollHoriz scrolls the focused window under the pointer sideways by one
// step (via the registered scroll_left/scroll_right command, so the step and
// clamping match the keyboard), but only once the barrier is cleared. dir is
// -1 for left, +1 for right. A direction reversal restarts the barrier.
func (e *Editor) mouseScrollHoriz(y, dir int) {
	w := e.windowAtRow(y)
	if w == nil || w.Buffer == nil || e.WindowManager.GetFocusedWindow() != w {
		return
	}
	if e.hScrollDir != dir { // first tick, or reversed: re-arm
		e.hScrollAccum = 0
		e.hScrollEngaged = false
		e.hScrollDir = dir
	}
	if !e.hScrollEngaged {
		e.hScrollAccum++
		if e.hScrollAccum < hScrollBarrier {
			return // not enough sideways movement yet
		}
		e.hScrollEngaged = true
	}
	if dir < 0 {
		e.executeCommand("scroll_left")
	} else {
		e.executeCommand("scroll_right")
	}
}

// mouseScroll scrolls the window under the pointer by delta lines — only
// when it is the focused window (modal safety, as with every mouse action).
func (e *Editor) mouseScroll(x, y int, delta int) {
	w := e.windowAtRow(y)
	if w == nil || w.Buffer == nil || e.WindowManager.GetFocusedWindow() != w {
		return
	}
	// Free scroll: park the viewport delta lines away and leave the caret where
	// it is (detaching from caret-follow until a cursor/edit command re-engages).
	e.scrollViewByLines(w, delta)
}
