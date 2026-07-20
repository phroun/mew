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
// mew's semantics:
//   - A left press anywhere in a window's content area focuses the window
//     and sets the caret to the clicked cell (tab-, bidi-, double-width- and
//     button-substitution-aware).
//   - In browse mode, a press ON a link button shows the button in the
//     pressed style; dragging off the button cancels the click (the style
//     reverts and a later release does nothing); a release still on the same
//     button reverts the style and follows the link exactly as keyboard
//     navigation would.
//   - The scroll wheel scrolls the window under the pointer.

// pressedLink identifies the button held down by the mouse (window identity,
// document line, span start). Ephemeral press-to-release state: identity by
// position is fine at this lifetime.
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
	case base == "MouseScrollUp":
		e.mouseScroll(e.mouseX, e.mouseY, -3)
	case base == "MouseScrollDown":
		e.mouseScroll(e.mouseX, e.mouseY, +3)
	}
	// Every other Mouse* event (middle/right buttons, their drags) is
	// swallowed so it never leaks into keymap dispatch.
	return true
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
		idx = displayToDoc(dispToDoc, idx)
	}
	return w, docLine, idx, true
}

// windowAtRow finds the visible window whose CONTENT area covers the 1-based
// screen row (the renderer maintains ContentY/ContentHeight per frame).
func (e *Editor) windowAtRow(y int) *window.Window {
	row := y - 1 // ContentY is 0-based
	var best *window.Window
	for _, w := range e.WindowManager.AllWindows() {
		if !w.Visible || w.Buffer == nil {
			continue
		}
		if row >= w.ContentY && row < w.ContentY+w.ContentHeight {
			// Prefer main buffers when areas would overlap (stale geometry
			// on hidden windows).
			if best == nil || (best.Type != window.MainBuffer && w.Type == window.MainBuffer) {
				best = w
			}
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

// mousePress: focus the window, set the caret to the clicked cell, and — on
// a link button in browse mode — arm the pressed style.
func (e *Editor) mousePress(x, y int) {
	w, docLine, runePos, ok := e.mouseHit(x, y)
	if !ok {
		return
	}
	if e.WindowManager.GetFocusedWindow() != w {
		e.WindowManager.SetFocus(w.ID)
	}
	w.SetCursorPos(window.Position{Line: docLine, Rune: runePos})
	e.afterHorizontalMovement(w)
	e.updateBrowseState()
	if span := e.focusedLinkButton(w); span != nil {
		e.mousePressed = pressedLink{active: true, winID: w.ID, line: docLine, start: span.Start}
	}
	e.RequestRender()
}

// mouseDrag: dragging off the pressed button cancels the click for good (the
// style reverts; the release will do nothing).
func (e *Editor) mouseDrag(x, y int) {
	if !e.mousePressed.active {
		return
	}
	if !e.hitOnPressedButton(x, y) {
		e.mousePressed = pressedLink{}
		e.RequestRender()
	}
}

// mouseRelease: a release still on the pressed button reverts its style and
// follows the link, exactly as keyboard navigation would.
func (e *Editor) mouseRelease(x, y int) {
	if !e.mousePressed.active {
		return
	}
	onButton := e.hitOnPressedButton(x, y)
	e.mousePressed = pressedLink{}
	if onButton {
		e.navFollow()
	}
	e.RequestRender()
}

// hitOnPressedButton reports whether the coordinates land on the very button
// the press armed (same window, same line, same span).
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

// mouseScroll scrolls the window under the pointer by delta lines.
func (e *Editor) mouseScroll(x, y int, delta int) {
	w := e.windowAtRow(y)
	if w == nil || w.Buffer == nil {
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
