package editor

import (
	"strings"

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

// handleMouseKey consumes mouse pseudo-keys from the key stream. Reports
// true when the key was a mouse event (handled or deliberately ignored), so
// the caller skips keymap dispatch.
func (e *Editor) handleMouseKey(key string) bool {
	// Strip modifier prefixes: a modified click acts like a plain one (for
	// now), and recognizing the base name is what matters.
	base := key
	for {
		switch {
		case strings.HasPrefix(base, "S-"), strings.HasPrefix(base, "M-"), strings.HasPrefix(base, "C-"):
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
		e.mousePress(e.mouseX, e.mouseY)
	case strings.HasPrefix(base, "MouseLeftDrag@"):
		if x, y, ok := parseMouseAt(base[len("MouseLeftDrag@"):]); ok {
			e.mouseX, e.mouseY = x, y
			e.mouseDrag(x, y)
		}
	case base == "MouseLeftRelease", base == "MouseRelease":
		e.mouseRelease(e.mouseX, e.mouseY)
	case strings.HasPrefix(base, "MouseDrag@"):
		// Plain motion, no button (all-motion tracking): hover.
		if x, y, ok := parseMouseAt(base[len("MouseDrag@"):]); ok {
			e.mouseX, e.mouseY = x, y
			e.mouseHoverAt(x, y)
		}
	case base == "MouseScrollUp":
		e.mouseScroll(e.mouseX, e.mouseY, -3)
	case base == "MouseScrollDown":
		e.mouseScroll(e.mouseX, e.mouseY, +3)
	}
	// Every mouse event may change the pointer affordance (over a button /
	// captured): push the change to the host, once per transition.
	e.notifyPointerShape()

	// Every other Mouse* event (middle/right buttons, their drags) is
	// swallowed so it never leaks into keymap dispatch.
	return true
}

// notifyPointerShape tells the host (via Config.PointerShape) whenever the
// pointer's affordance changes: true while the pointer is over a link button
// or a button is captured — a graphical host shows the arrow pointer — and
// false for ordinary text (the I-beam). Pushed only on transitions.
func (e *Editor) notifyPointerShape() {
	if e.Config.PointerShape == nil {
		return
	}
	over := e.mouseHovered.active || e.mousePressed.active
	if over != e.pointerOverSent {
		e.pointerOverSent = over
		e.Config.PointerShape(over)
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
		if best == nil || (best.Type != window.MainBuffer && w.Type == window.MainBuffer) {
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
func (e *Editor) mousePress(x, y int) {
	w, docLine, runePos, ok := e.mouseHit(x, y)
	if !ok || e.WindowManager.GetFocusedWindow() != w {
		return
	}
	w.SetCursorPos(window.Position{Line: docLine, Rune: runePos})
	e.afterHorizontalMovement(w)
	e.updateBrowseState()
	if span := e.focusedLinkButton(w); span != nil {
		e.mousePressed = pressedLink{active: true, winID: w.ID, line: docLine, start: span.Start}
		e.mouseOnCaptured = true
	}
	e.RequestRender()
}

// mouseDrag: the captured button tracks the pointer — pressed style while
// over it, its ordinary (focused) style while dragged off, re-pressed when
// dragged back on. The capture itself holds until release.
func (e *Editor) mouseDrag(x, y int) {
	if !e.mousePressed.active {
		return
	}
	if on := e.hitOnPressedButton(x, y); on != e.mouseOnCaptured {
		e.mouseOnCaptured = on
		e.RequestRender()
	}
}

// mouseRelease: releasing ON the captured button follows the link, exactly
// as keyboard navigation would; releasing anywhere else abandons the click.
// Either way the capture ends.
func (e *Editor) mouseRelease(x, y int) {
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

// mouseScroll scrolls the window under the pointer by delta lines — only
// when it is the focused window (modal safety, as with every mouse action).
func (e *Editor) mouseScroll(x, y int, delta int) {
	w := e.windowAtRow(y)
	if w == nil || w.Buffer == nil || e.WindowManager.GetFocusedWindow() != w {
		return
	}
	top := w.ViewState.ViewOffsetY + delta
	if max := w.Buffer.GetLineCount() - 1; top > max {
		top = max
	}
	if top < 0 {
		top = 0
	}
	w.SetViewTop(top)
	e.RequestRender()
}
