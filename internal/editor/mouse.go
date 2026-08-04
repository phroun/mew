package editor

import (
	"sort"
	"strings"
	"time"

	"github.com/phroun/mew/internal/bidi"
	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/render"
	"github.com/phroun/mew/internal/textwidth"
	"github.com/phroun/mew/internal/viewport"
)

// Mouse input (TUI). The key layer (direct-key-handler) decodes SGR/X10
// mouse reports into pseudo-keys — "Mouse@x,y" (position, emitted before its
// action), "MouseLeftPress"/"MouseLeftRelease"/"MouseScrollUp"/... and drags
// as "MouseLeftDrag@x,y" — once the terminal is asked to report the mouse at
// all (see EnableMouseReporting; purfecterm answers the same DECSET trio by
// routing mouse to the app instead of local selection).
//
// mew's semantics — MODAL-SAFE: only the FOCUSED viewport processes mouse
// actions. A click in any other viewport is ignored outright (no focus steal),
// so a modal prompt keeps the whole keyboard-and-mouse stage to itself.
//   - A left press in the focused viewport's content area sets the caret to
//     the clicked cell (tab-, bidi-, double-width- and button-substitution-
//     aware).
//   - In browse mode, a press ON a link button CAPTURES it: the button
//     shows the pressed style while the pointer is over it, reverts to its
//     focused style when dragged off (the capture holds), and re-presses
//     when dragged back on. Releasing on the captured button follows the
//     link exactly as keyboard navigation would; releasing anywhere else
//     abandons the click.
//   - The scroll wheel scrolls the focused viewport (when under the pointer).
//   - With all-motion tracking delivered (the graphical build), the link or
//     button under the pointer takes a hover style.

// pressedLink identifies a link by position (viewport identity, document
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
// in the focused viewport's content area arms it (begun=false): the first drag
// onto a DIFFERENT cell places _block_begin at the press origin, and every
// drag onto a new cell re-places _block_end (the caret follows). A
// shift+click arms it pre-begun (begun=true): _block_begin is already placed
// at the ORIGINAL caret position and only _block_end follows the drag.
// Press-to-release lifetime, focused viewport only.
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

// scrollbarDragState is a viewport-scrollbar gesture held from press to
// release: which viewport's bar is captured (by ID), and where within the
// thumb the pointer grabbed it (a track press centers the thumb on the
// pointer instead).
type scrollbarDragState struct {
	active  bool
	winID   string
	grabOff int
}

// scrollbarPressAt starts a scrollbar gesture when the press lands in a
// viewport's reserved scrollbar column (ScrollbarX, laid out by the
// renderer). A press on the thumb anchors the grab where the pointer sits in
// it; a press on the track jumps the thumb center to the pointer and the drag
// continues from that anchor — the same gesture the terminal scrollbars
// implement. The thumb moves cell by cell; only the top line quantizes, and
// the viewport is left scroll-detached exactly as the wheel leaves it (no
// snap-back, no sticky bottom). Like the wheel, it works on any on-screen
// viewport without stealing focus. Returns true when consumed.
func (e *Editor) scrollbarPressAt(x, y int) bool {
	if e.hostDrawsScrollbars() {
		return false // the host owns the bar, in pixels
	}
	w := e.viewportAt(x, y)
	if w == nil || w.Buffer == nil || w.ScrollbarX < 0 || x-1 != w.ScrollbarX {
		return false
	}
	r := y - 1 - w.ContentY
	if r < 0 || r >= w.ContentHeight {
		return false
	}
	// A press on the given-up corner row (past the track) still belongs to
	// the bar's column: treat it as the bottom of the track.
	if r >= w.ScrollbarTrackH {
		r = w.ScrollbarTrackH - 1
	}
	pos, size := viewport.ScrollbarThumb(w.ScrollbarTrackH, w.ContentHeight, w.Buffer.GetLineCount(), w.ViewState.ViewOffsetY)
	if size <= 0 {
		// A document that fits has no thumb and nowhere to scroll to. The press
		// is still CONSUMED — the column belongs to the bar, and a click there
		// must not fall through and start selecting text in it — it simply does
		// nothing, and starts no drag.
		return true
	}
	if r >= pos && r < pos+size {
		e.sbDrag.grabOff = r - pos
	} else {
		e.sbDrag.grabOff = size / 2
	}
	e.sbDrag.active = true
	e.sbDrag.winID = w.ID
	e.scrollbarDragTo(w, r)
	return true
}

// scrollbarDrag continues a captured scrollbar gesture: the pointer's row —
// wherever it is now, on or off the bar — moves the thumb and the view
// follows. Returns false when no gesture is captured.
func (e *Editor) scrollbarDrag(y int) bool {
	if !e.sbDrag.active {
		return false
	}
	w := e.ViewportManager.GetViewport(e.sbDrag.winID)
	if w == nil || w.Buffer == nil {
		e.sbDrag.active = false
		return true
	}
	e.scrollbarDragTo(w, y-1-w.ContentY)
	return true
}

// scrollbarRelease ends a captured scrollbar gesture; false when none is.
func (e *Editor) scrollbarRelease() bool {
	if !e.sbDrag.active {
		return false
	}
	e.sbDrag.active = false
	return true
}

// scrollbarDragTo scrolls w so its thumb's top cell sits at track row r minus
// the gesture's grab offset (ScrollbarTopForThumb clamps to the track).
func (e *Editor) scrollbarDragTo(w *viewport.Viewport, r int) {
	top := viewport.ScrollbarTopForThumb(r-e.sbDrag.grabOff, w.ScrollbarTrackH, w.ContentHeight, w.Buffer.GetLineCount())
	e.scrollViewTo(w, top)
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
	// The individual modifiers are kept alongside mod because a child process
	// running in a terminal viewport wants the event as it happened (see
	// ptyMouseKey); mod is mew's own collapsed reading of the same thing.
	base := key
	shift, alt, ctrl, mod := false, false, false, false
	for {
		switch {
		case strings.HasPrefix(base, "S-"):
			shift = true
			base = base[2:]
			continue
		case strings.HasPrefix(base, "M-"):
			alt, mod = true, true
			base = base[2:]
			continue
		case strings.HasPrefix(base, "C-"):
			ctrl, mod = true, true
			base = base[2:]
			continue
		case strings.HasPrefix(base, "H-"):
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

	// Position rides on the "@" forms and is remembered for the action keys
	// that follow it. Hoisted out of the switch so every path — mew's own and
	// the terminal's — reads one already-current position.
	atOK := true
	if i := strings.IndexByte(base, '@'); i >= 0 {
		x, y, ok := parseMouseAt(base[i+1:])
		atOK = ok
		if ok {
			// Under SGR-Pixels (?1016) the report is in PIXELS: convert to a
			// cell and keep the sub-cell offset for nearest-edge caret
			// placement. Otherwise x,y are already cells and there is no
			// sub-cell information.
			if e.pixelMouseIsActive() {
				e.mouseX, e.mouseY, e.mouseSubX = e.pixelToCell(x, y)
			} else {
				e.mouseX, e.mouseY, e.mouseSubX = x, y, -1
			}
		}
	}

	// A terminal running in the focused viewport gets the mouse first, when
	// its application asked for it.
	if atOK && e.ptyMouseKey(base, shift, alt, ctrl, e.mouseX, e.mouseY) {
		return true
	}

	switch {
	case strings.HasPrefix(base, "Mouse@"):
		// Position only; already recorded above.
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
		case e.scrollbarPressAt(e.mouseX, e.mouseY):
			// Consumed by a viewport scrollbar (capture started).
		default:
			e.mousePress(e.mouseX, e.mouseY, shift)
		}
	case strings.HasPrefix(base, "MouseLeftDrag@"):
		if atOK {
			switch {
			case e.modebarNavCapture != 0:
				e.modebarNavDrag(e.mouseX, e.mouseY)
			case e.scrollbarDrag(e.mouseY):
				// Captured scrollbar gesture followed the pointer.
			default:
				e.mouseDrag(e.mouseX, e.mouseY)
			}
		}
	case base == "MouseLeftRelease", base == "MouseRelease":
		switch {
		case e.modebarNavCapture != 0:
			e.modebarNavRelease(e.mouseX, e.mouseY)
		case e.scrollbarRelease():
			// Scrollbar gesture ended; the view stays where the drag left it.
		default:
			e.mouseRelease(e.mouseX, e.mouseY)
		}
	case base == "MouseRightPress":
		e.mouseRightPress(e.mouseX, e.mouseY)
	case strings.HasPrefix(base, "MouseDrag@"):
		// Plain motion, no button (all-motion tracking): hover. The position was
		// already parsed AND pixel-converted at the top (e.mouseX/e.mouseY);
		// reuse it rather than re-parsing the raw report, which is in PIXELS
		// under SGR-Pixels (?1016) and would put hover far off the grid.
		if atOK {
			e.mouseHoverAt(e.mouseX, e.mouseY)
			e.modebarNavHoverAt(e.mouseX, e.mouseY)
		}
	case base == "MouseScrollUp":
		e.hScrollReset() // a vertical tick re-arms the sideways barrier
		e.mouseScroll(e.mouseX, e.mouseY, -3)
	case base == "MouseScrollDown":
		e.hScrollReset()
		e.mouseScroll(e.mouseX, e.mouseY, +3)
	case base == "MouseScrollLeft":
		e.mouseScrollHoriz(e.mouseX, e.mouseY, -1)
	case base == "MouseScrollRight":
		e.mouseScrollHoriz(e.mouseX, e.mouseY, +1)
	}
	// Every other Mouse* event (middle button, right release/drags) is
	// swallowed so it never leaks into keymap dispatch.
	return true
}

// notifyPointerRegion publishes the rectangle where a graphical host should
// show the text I-beam (Config.PointerRegion): the FOCUSED viewport's editable
// content area — its cells, including the blank rows below the document that
// still follow click-to-EOF — in 1-based terminal cells. Everything outside it
// is the ordinary arrow: the gutter (left of the content), the modebar and
// other chrome (other viewports), an unfocused pane, and — when a prompt holds
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
	if w := e.pointerRegionViewport(); w != nil {
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

// hostDrawsScrollbars reports whether a graphical host has taken over drawing
// the editor scrollbars (Config.ScrollbarRegions set). mew then reserves the
// column and publishes the geometry, but paints nothing into it and leaves the
// hit-testing to the host, which owns the pointer in pixel space.
func (e *Editor) hostDrawsScrollbars() bool { return e.Config.ScrollbarRegions != nil }

// notifyScrollbarRegions publishes every visible editor scrollbar to a host
// that draws them (see Config.ScrollbarRegions). Pushed after each render and
// only when the set changes — a bar appearing, moving, resizing, or its view
// scrolling — so a host repaints without polling. Runs under renderMu with the
// frame's geometry already set by the renderer.
func (e *Editor) notifyScrollbarRegions() {
	if e.Config.ScrollbarRegions == nil {
		return
	}
	var regions []ScrollbarRegion
	for _, w := range e.ViewportManager.AllViewports() {
		if w.ScrollbarX < 0 || w.ScrollbarTrackH <= 0 || w.Buffer == nil {
			continue
		}
		if !e.viewportOnScreen(w) {
			continue
		}
		regions = append(regions, ScrollbarRegion{
			ViewportID: w.ID,
			Col:        w.ScrollbarX + 1,
			Row:        w.ContentY + 1,
			TrackH:     w.ScrollbarTrackH,
			Top:        w.ViewState.ViewOffsetY,
			Page:       w.ContentHeight,
			LineCount:  w.Buffer.GetLineCount(),
		})
	}
	sort.Slice(regions, func(i, j int) bool { return regions[i].ViewportID < regions[j].ViewportID })
	if e.scrollbarRegionsPushed && scrollbarRegionsEqual(regions, e.scrollbarRegionsSent) {
		return
	}
	e.scrollbarRegionsPushed = true
	e.scrollbarRegionsSent = regions
	e.Config.ScrollbarRegions(regions)
}

// scrollbarRegionsEqual reports whether two published sets are element-equal.
func scrollbarRegionsEqual(a, b []ScrollbarRegion) bool {
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

// pointerArrowSpans returns the on-screen cell spans of the focused viewport's
// browse-mode link BUTTONS — the buttons that sit INSIDE the text region and
// so must show the arrow, not the I-beam. Empty unless the viewport is in browse
// mode over a linkable buffer. LTR only: an RTL page's right-anchored button
// columns are not mapped, so its buttons fall back to the I-beam (an exotic
// edge — RTL wiki browsing).
func (e *Editor) pointerArrowSpans(w *viewport.Viewport) []PointerArrowSpan {
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
			// viewport's visible content columns.
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

// pointerRegionViewport returns the viewport whose editable text drives the I-beam
// region. It is the focused viewport ONLY when that viewport is actually on screen
// — VISIBLE, holding a buffer, and laid out this frame (non-zero content
// geometry). A focused viewport that is invisible or not laid out (a background
// or stacked viewport that never rendered) has stale or zero geometry that would
// blank or misplace the region — the I-beam then never lands over the visible
// text — so we fall back to the visible document instead. A visible prompt is a
// legitimate focus target (its own field shows the I-beam); only genuinely
// off-screen focus falls through to the document.
func (e *Editor) pointerRegionViewport() *viewport.Viewport {
	onScreen := func(w *viewport.Viewport) bool {
		return w != nil && w.Visible && w.Buffer != nil &&
			w.ContentWidth > 0 && w.ContentHeight > 0
	}
	if w := e.ViewportManager.GetFocusedViewport(); onScreen(w) {
		return w
	}
	if m := e.ViewportManager.GetLastMainViewport(); onScreen(m) {
		return m
	}
	return nil
}

// promptHasPriority reports whether a modal prompt currently holds focus, so
// mouse interactions on the document/chrome (the modebar nav buttons, their
// hover styling) stand down while input is awaited at the prompt.
func (e *Editor) promptHasPriority() bool {
	w := e.ViewportManager.GetFocusedViewport()
	return w != nil && w.Type == viewport.PromptViewport
}

// notifyEditState tells the host (via Config.EditState) whenever the FOCUSED
// viewport's read-only state changes — a host greys out its mutating
// affordances (the Edit menu's Cut) while a read-only buffer holds focus.
// Pushed once at the first render, then only on transitions. Called from
// performRender, which runs after every state-changing event.
func (e *Editor) notifyEditState() {
	if e.Config.EditState == nil {
		return
	}
	ro := false
	if w := e.ViewportManager.GetFocusedViewport(); w != nil {
		ro = w.ViewState.ReadOnly
	}
	if !e.readOnlyPushed || ro != e.readOnlySent {
		e.readOnlyPushed = true
		e.readOnlySent = ro
		e.Config.EditState(ro)
	}
}

// notifyHelpState tells the host (via Config.HelpState) whether the built-in
// help viewport is open, once at the first render and thereafter on transitions,
// so a host keeps a "Quick Help" menu checkmark in sync as help_toggle (or a
// close) opens and closes it. Called from performRender.
func (e *Editor) notifyHelpState() {
	if e.Config.HelpState == nil {
		return
	}
	open := e.quickHelpViewportOpen()
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

// mouseHit resolves 1-based screen coordinates to a viewport and document
// position. ok is false outside every viewport's content area. The column math
// mirrors the painter: gutter/margins, horizontal scroll, double-width rows
// (half-width gutter, two columns per cell), bidi visual order, tabs, and
// browse-mode display substitution all resolve back to a document rune;
// clicking button chrome parks inside the button's source span.
// physicalToCell maps a 1-based PHYSICAL screen column to the 1-based cell it
// falls in on a double-width (DECDWL) row, where every cell spans two physical
// columns: cell c covers columns 2c-1 and 2c. The inverse of the renderer's
// cellToPhysical.
func physicalToCell(x int) int { return (x + 1) / 2 }

func (e *Editor) mouseHit(x, y int) (w *viewport.Viewport, docLine, runePos, caretRune int, ok bool) {
	w = e.viewportAt(x, y)
	if w == nil || w.Buffer == nil {
		return nil, 0, 0, 0, false
	}
	docLine = w.ViewState.ViewOffsetY + (y - 1 - w.ContentY)
	if docLine < 0 || docLine >= w.Buffer.GetLineCount() {
		return nil, 0, 0, 0, false
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
	base := w.ContentX + 1 // first content CELL, 1-based
	cells := w.ContentWidth
	viewOff := w.ViewState.ViewOffsetX
	cell := x - base
	if dw {
		// A doubled row is addressed in CELLS: each one spans two physical
		// columns, and the row shows half as many gutter and content cells
		// (matching caretRowGeom, which places the caret in the same space).
		// x arrives as a PHYSICAL column, so map it into cell space FIRST —
		// subtracting a cell-space base from a physical column and halving the
		// difference charges the gutter's cells at physical width and lands
		// the click a cell right of the pointer, further right the wider the
		// gutter.
		if !e.winRTL(w) {
			base = w.ContentX - lineNumWidth + lineNumWidth/2 + 1
		}
		cells /= 2
		viewOff /= 2
		cell = physicalToCell(x) - base
	}
	if cell < 0 || (cells > 0 && cell >= cells) {
		return nil, 0, 0, 0, false
	}

	target := cell + viewOff
	if e.winRTL(w) {
		// Right-anchored view: visible visual columns are [vw-off-width,
		// vw-off), with left padding when the line is narrower.
		// The offset here is the row's own (halved on a doubled row, like
		// cells) — mixing the raw offset with halved cells walked the hit
		// further off with every column scrolled.
		vw := e.lineVisualWidth(w, dispLine, e.tabSize(w))
		eff := vw - viewOff - cells
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

	// mapDisp turns a DISPLAY-rune index (over dispLine) into a document rune,
	// through the button/substitution mapping (past the end → end of line, never
	// inside a trailing button's span).
	mapDisp := func(i int) int {
		if dispToDoc != nil {
			if i >= len(dispToDoc) {
				return len([]rune(raw))
			}
			return displayToDoc(dispToDoc, i)
		}
		return i
	}
	// resolve maps a visual column to a document rune.
	resolve := func(vt int) int {
		return mapDisp(e.runeAtVisualColumn(w, dispLine, vt))
	}
	idx := resolve(target)
	// subX is the pointer's sub-cell fraction (permille, -1 = none). On a DOUBLED
	// row each cell spans two physical columns, but a pixel report's fraction is
	// within ONE physical column, so fold the clicked column's parity in to
	// recover the fraction across the whole double-width cell — otherwise both
	// halves of the cell would read as a full 0..1 sweep and the split point
	// would be unpredictable. (x is the physical column: (x+1)%2 is 0 for a
	// cell's left column, 1 for its right; physicalToCell pairs them.)
	subX := e.mouseSubX
	if dw && subX >= 0 {
		subX = (((x+1)%2)*1000 + subX) / 2
	}
	// caretRune is where a CARET should land: the containing rune normally, but
	// the NEAREST reading edge of the clicked character for an insert-mode click.
	// The decision is made across the character's FULL visual span, not one cell:
	// a glyph wider than a cell (a wide CJK letter, a tab, a DEC-doubled cell)
	// splits by which of its cells was clicked, so its trailing cell(s) advance
	// the caret even on a terminal with no sub-cell (pixel) reporting; a
	// single-cell glyph splits only when a pixel report supplies the sub-cell
	// half. Which edge ADVANCES flips with the character's direction — LTR's
	// trailing edge is on the right, RTL's on the left — and the advance skips
	// the cluster's combining marks so niqqud/harakat never split a caret.
	// Overwrite mode keeps idx (a click selects the character to type over).
	caretRune = idx
	// A click on a synthetic bidi direction marker (the "<" / ">" / "|" glyphs
	// shown under showBidi) resolves to a specific caret boundary that is
	// direction- and half-aware — not a character to overwrite — so it runs
	// ahead of, and instead of, the nearest-edge logic below.
	if mc, ok := e.markerCaretAt(w, dispLine, target, subX); ok {
		return w, docLine, idx, mapDisp(mc), true
	}
	rr := []rune(raw)
	if w != nil && !w.ViewState.OverwriteMode && idx < len(rr) {
		// The clicked character's visual span [c0, cEnd]: widen left and right
		// while the same DISPLAY rune covers the column (a multi-cell glyph maps
		// every one of its cells to one index). Capped so a pathological layout
		// can never spin.
		di := e.runeAtVisualColumn(w, dispLine, target)
		c0, cEnd := target, target
		for c0 > 0 && cEnd-c0 < 64 && e.runeAtVisualColumn(w, dispLine, c0-1) == di {
			c0--
		}
		for cEnd-c0 < 64 && e.runeAtVisualColumn(w, dispLine, cEnd+1) == di {
			cEnd++
		}
		wd := cEnd - c0 + 1
		// Only meaningful with a sub-cell half (pixel report) or a multi-cell
		// span; a lone cell in cell-resolution mode keeps the classic
		// before-the-character landing.
		if subX >= 0 || wd > 1 {
			frac := 0.0
			if subX >= 0 {
				frac = float64(subX) / 1000
			}
			vf := (float64(target-c0) + frac) / float64(wd) // 0..1 across the glyph
			rtl := e.winRTL(w)
			if layout := e.layoutFor(w, []rune(dispLine)); layout != nil && di >= 0 && di < len(layout.RTL) {
				rtl = layout.RTL[di]
			}
			// The screen-RIGHT half is the trailing (after) side for an LTR
			// glyph and the leading (before) side for an RTL one, so the advance
			// condition inverts with direction.
			if (vf >= 0.5) != rtl {
				n := idx + 1
				for n < len(rr) && textwidth.IsMark(rr[n]) {
					n++
				}
				caretRune = n
			}
		}
	}
	return w, docLine, idx, caretRune, true
}

// viewportOnScreen reports whether w was laid out by the most recent frame —
// truly visible, with current geometry. A background main viewport keeps its
// Visible flag and stale geometry from an earlier frame; the layout-epoch
// stamp is what tells the two apart.
func (e *Editor) viewportOnScreen(w *viewport.Viewport) bool {
	return w != nil && w.Visible && w.Buffer != nil &&
		e.Renderer != nil && w.LayoutEpoch == e.Renderer.LayoutEpoch()
}

// viewportAt finds the on-screen viewport covering the 1-based screen column x
// and row y: vertically its CONTENT rows (ContentY/ContentHeight), horizontally
// its whole paint frame ([FrameX, FrameX+FrameWidth), the full tile including
// gutter and scrollbar; FrameWidth 0 means full width from FrameX, as for docked
// chrome). The column test is what distinguishes side-by-side tiles — without it,
// two tiles sharing the same rows both matched. Only viewports laid out by the
// CURRENT frame qualify — a background main viewport's stale geometry can cover
// the same cells and must not win. The focused viewport wins as a tiebreak when
// areas overlap.
func (e *Editor) viewportAt(x, y int) *viewport.Viewport {
	row := y - 1 // ContentY is 0-based
	col := x - 1 // FrameX is 0-based
	covers := func(w *viewport.Viewport) bool {
		if !e.viewportOnScreen(w) ||
			!(row >= w.ContentY && row < w.ContentY+w.ContentHeight) {
			return false
		}
		width := w.FrameWidth
		if width <= 0 {
			width = e.Renderer.Width - w.FrameX
		}
		return col >= w.FrameX && col < w.FrameX+width
	}
	if fw := e.ViewportManager.GetFocusedViewport(); covers(fw) {
		return fw
	}
	var best *viewport.Viewport
	for _, w := range e.ViewportManager.AllViewports() {
		if !covers(w) {
			continue
		}
		if best == nil || (!best.FocusEligible() && w.FocusEligible()) {
			best = w
		}
	}
	return best
}

// runeAtVisualColumn is the inverse of the caret-column math: the logical
// index of the rune whose visual cell run covers the target column, or the
// line length when the target lies past the end.
func (e *Editor) runeAtVisualColumn(w *viewport.Viewport, line string, target int) int {
	runes := []rune(line)
	tabSize := e.tabSize(w)
	layout := e.layoutFor(w, runes)
	if layout == nil {
		col := 0
		for i := range runes {
			wd := e.runeWidthAt(runes, i, col, tabSize)
			if target < col+wd {
				return i
			}
			col += wd
		}
		return len(runes)
	}
	// Walk the visual order (mirroring bidiColumns' column accounting) so a click
	// on a synthetic slot — a bidi direction marker (the < / > arrow shown under
	// showBidi) — resolves too, instead of matching no rune and falling through
	// to end-of-line. A marker maps to its run's own adjacent real cell: a START
	// marker (MarkerRTL/LTR) precedes its run, so the NEXT real cell in visual
	// order; an END marker follows its run, so the PREVIOUS one.
	marked := e.lineMarkSet(w, runes)
	col := 0
	for pi, li := range layout.Perm {
		if li >= 0 && marked[li] {
			col++ // the showMarks "*" cell before the rune
		}
		wd := e.slotWidth(layout, runes, li, col, tabSize)
		if wd > 0 && target >= col && target < col+wd {
			if li >= 0 {
				return li
			}
			if li == bidi.MarkerEnd {
				for j := pi - 1; j >= 0; j-- {
					if layout.Perm[j] >= 0 {
						return layout.Perm[j]
					}
				}
			} else {
				for j := pi + 1; j < len(layout.Perm); j++ {
					if layout.Perm[j] >= 0 {
						return layout.Perm[j]
					}
				}
			}
			return len(runes)
		}
		col += wd
	}
	return len(runes)
}

// markerCaretAt resolves a click that landed on a synthetic bidi direction
// marker — the one-column "<" (MarkerRTL), ">" (MarkerLTR) or "|" (MarkerEnd)
// glyphs drawn under showBidi — to a DISPLAY-rune caret position. Returns
// ok=false when the target column is a real cell, so the caller keeps its
// normal cell logic. line is the display line; subX is the pointer's sub-cell
// horizontal permille (<0 when unknown, treated as the left half).
//
// The marker's caret lands on a run BOUNDARY, chosen by the marker's meaning:
//   - "<" points at the RTL cell to its LEFT (that run's leading edge); the
//     caret goes BEFORE that character — its own logical index.
//   - ">" points at the LTR cell to its RIGHT (that run's leading edge); the
//     caret goes BEFORE that character — its own logical index.
//   - "|" is a run's reading END, shared by the two cells it sits between; the
//     caret takes the "after" side of the NEARER neighbor: the left half lands
//     on the RIGHT edge of the cell visually to the left, the right half on the
//     LEFT edge of the cell visually to the right. Whether an edge is a
//     logical-before or logical-after boundary flips with that cell's own
//     direction (RTL reads right-to-left, so its right edge is the before side).
func (e *Editor) markerCaretAt(w *viewport.Viewport, line string, target, subX int) (caret int, ok bool) {
	runes := []rune(line)
	layout := e.layoutFor(w, runes)
	if layout == nil {
		return 0, false
	}
	tabSize := e.tabSize(w)
	marked := e.lineMarkSet(w, runes)

	// A base is a width-bearing cell; zero-width combining marks and the
	// absorbed half of a ligature share their base's column and are stepped over
	// so a marker's neighbor is the cell that actually occupies the column.
	isBase := func(li int) bool {
		return li >= 0 && e.slotWidth(layout, runes, li, 0, tabSize) > 0
	}
	prevBase := func(pi int) int {
		for j := pi - 1; j >= 0; j-- {
			if isBase(layout.Perm[j]) {
				return layout.Perm[j]
			}
		}
		return -1
	}
	nextBase := func(pi int) int {
		for j := pi + 1; j < len(layout.Perm); j++ {
			if isBase(layout.Perm[j]) {
				return layout.Perm[j]
			}
		}
		return -1
	}
	// afterCluster is the caret position just past a base and its trailing
	// combining marks / absorbed ligature half (all logically after the base).
	afterCluster := func(i int) int {
		n := i + 1
		for n < len(runes) {
			if textwidth.IsMark(runes[n]) ||
				(layout.Glyph != nil && n < len(layout.Glyph) && layout.Glyph[n] == bidi.LigatureAbsorbed) {
				n++
				continue
			}
			break
		}
		return n
	}
	rtl := func(i int) bool {
		return i >= 0 && i < len(layout.RTL) && layout.RTL[i]
	}
	// rightEdge/leftEdge convert a base's SPATIAL edge to a caret boundary: for
	// an LTR cell the right edge is its logical-after side and the left edge its
	// logical-before side; an RTL cell reads the other way.
	rightEdge := func(i int) int {
		if i < 0 {
			return 0 // no cell to the left → line start
		}
		if rtl(i) {
			return i
		}
		return afterCluster(i)
	}
	leftEdge := func(i int) int {
		if i < 0 {
			return len(runes) // no cell to the right → line end
		}
		if rtl(i) {
			return afterCluster(i)
		}
		return i
	}

	col := 0
	for pi, li := range layout.Perm {
		if li >= 0 && marked[li] {
			col++ // the showMarks "*" cell before the rune
		}
		wd := e.slotWidth(layout, runes, li, col, tabSize)
		if wd > 0 && target >= col && target < col+wd {
			if li >= 0 {
				return 0, false // a real cell — not a marker
			}
			switch li {
			case bidi.MarkerRTL: // "<": before the RTL cell to its left
				if p := prevBase(pi); p >= 0 {
					return p, true
				}
			case bidi.MarkerLTR: // ">": before the LTR cell to its right
				if n := nextBase(pi); n >= 0 {
					return n, true
				}
			case bidi.MarkerEnd: // "|": the "after" side of the nearer neighbor
				if subX < 500 {
					return rightEdge(prevBase(pi)), true
				}
				return leftEdge(nextBase(pi)), true
			}
			return 0, false
		}
		col += wd
	}
	return 0, false
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
// browse mode — arm the pressed style. ONLY the focused viewport processes
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
	e.endDragTxn() // a lost release: never carry a stale gesture transaction
	w, docLine, runePos, caretRune, ok := e.mouseHit(x, y)
	if !ok {
		// A press on the BLANK AREA below the document's last line (still
		// inside the viewport's content area) means the END of the document:
		// click below the doc and drag upward to select its tail.
		w, docLine, runePos, ok = e.mouseHitBelowText(x, y)
		caretRune = runePos // no sub-cell in the void below the text
	}
	if !ok {
		return
	}
	if e.ViewportManager.GetFocusedViewport() != w {
		// An UNFOCUSED viewport takes a press in exactly two cases — and only
		// while no modal prompt holds focus (clicks elsewhere stay inert then).
		if e.promptHasPriority() || !e.viewportOnScreen(w) {
			return
		}
		// A link button does NOT steal focus: pressing one in a truly visible
		// viewport captures it for the press/release follow, same as in the
		// focused viewport, leaving focus (and this viewport's caret) alone.
		if span := e.linkButtonAt(w, docLine, runePos); span != nil {
			e.mousePressed = pressedLink{active: true, winID: w.ID, line: docLine, start: span.Start}
			e.mouseOnCaptured = true
			e.RequestRender()
			return
		}
		// A NON-link press focuses the viewport exactly the way the focus
		// switcher (^B N) would land on it — cycle-stop eligibility and the
		// newest-prompt resolution included. The click only switches focus;
		// it does not also place the caret.
		if e.ViewportManager.FocusViewportAsCycle(w) {
			e.announceFocusedViewport()
			e.RequestRender()
		}
		return
	}

	if shift {
		// Extend from the caret's current document position to the click.
		// A shift+click is the mouse user's DELIBERATE selection: the
		// mouse-block flag goes OFF, so this block persists through later
		// plain clicks exactly like a keyboard-made one.
		e.beginDragTxn(w.Buffer)
		origin := w.CursorPos()
		w.Buffer.SetMark("_block_begin", origin.Line, origin.Rune)
		w.Buffer.SetMark("_block_end", docLine, caretRune)
		w.Buffer.SetMouseBlock(false)
		e.dragSel = dragSelState{
			active: true, begun: true, shifted: true, winID: w.ID,
			originLine: origin.Line, originRune: origin.Rune,
			lastLine: docLine, lastRune: caretRune,
		}
		w.SetCursorPos(viewport.Position{Line: docLine, Rune: caretRune})
		e.afterHorizontalMovement(w)
		w.ViewState.ScrollDetached = false
		e.RequestRender()
		return
	}

	// A plain click dissolves a MOUSE-made block (a transient drag
	// selection); a keyboard-made or shift+click-made block survives.
	if w.Buffer.MouseBlock() {
		w.Buffer.ClearBlockMarks() // clears the flag with the marks
	}

	w.SetCursorPos(viewport.Position{Line: docLine, Rune: caretRune})
	e.afterHorizontalMovement(w)
	// A click is a cursor movement: re-engage caret following, cancelling any
	// free scroll left by the wheel so the view tracks the caret again.
	w.ViewState.ScrollDetached = false
	if span := e.focusedLinkButton(w); span != nil {
		e.mousePressed = pressedLink{active: true, winID: w.ID, line: docLine, start: span.Start}
		e.mouseOnCaptured = true
	} else {
		// Arm drag selection from this press origin; it only takes effect
		// when the drag reaches a different cell (dragSelUpdate).
		e.dragSel = dragSelState{
			active: true, winID: w.ID,
			originLine: docLine, originRune: caretRune,
			lastLine: docLine, lastRune: caretRune,
		}
	}
	e.RequestRender()
}

// mouseRightPress: a right-click within the EDITING AREA of the focused
// viewport asks the host to pop its context menu at the clicked cell
// (Config.ShowContextMenu). The gate is mouseHit + the focused-viewport rule —
// exactly the left-click caret path's routing — so the modebar, gutters,
// column ruler, and title/message rows never pop the menu, and neither does
// any unfocused viewport (modal safety, like every mouse action). The caret
// does NOT move: a right-click inspects, it doesn't relocate — moving it
// would silently change what a subsequent paste targets (caret-in-block).
func (e *Editor) mouseRightPress(x, y int) {
	if e.Config.ShowContextMenu == nil {
		return
	}
	w, _, _, _, ok := e.mouseHit(x, y)
	if !ok {
		// The blank area below the document's last line counts as the
		// editing area too — same as a left press there.
		w, _, _, ok = e.mouseHitBelowText(x, y)
	}
	if !ok || e.ViewportManager.GetFocusedViewport() != w {
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
	w := e.ViewportManager.GetFocusedViewport()
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
		// The drag becomes a selection here: open the gesture's transaction so
		// every mark movement until release lands in ONE undo step.
		e.beginDragTxn(w.Buffer)
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
	w.SetCursorPos(viewport.Position{Line: docLine, Rune: runePos})
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
// on the blank rows BELOW the document's last line, inside a viewport's
// content area (content columns only — the gutter stays inert, as on text
// rows). It answers the END of the document (last line, end of line), so a
// click below the doc parks the caret at EOF — and a drag upward from there
// selects the document's tail.
func (e *Editor) mouseHitBelowText(x, y int) (w *viewport.Viewport, docLine, runePos int, ok bool) {
	w = e.viewportAt(x, y)
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
//   - rows above/below the viewport's text clamp to the nearest row that
//     holds a visible line;
//   - the gutter side (line numbers — left in LTR, right in RTL) resolves
//     to rune 0 of the row's line, so a drag into the gutter pins the
//     selection to the line's beginning;
//   - the far side clamps to the last content column (the rightmost —
//     in RTL leftmost — visible cell, or the line end when the line ends
//     inside the view).
//
// ok is false only when the viewport shows no text at all or the clamped
// position still fails to resolve (a double-width edge case).
func (e *Editor) dragSelResolve(w *viewport.Viewport, x, y int) (docLine, runePos int, ok bool) {
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

	// The mirror at the top: dragged above the document's FIRST line — its row
	// is visible and the pointer sits above it — the selection reaches the
	// START of the document (BOF), not just the horizontal position on that
	// row. A pointer pinned on the grid's first row counts as "above" when the
	// document's first line is already there, since no host can report a row
	// that does not exist (the same allowance dragScrollTrack makes to engage
	// the scroll). While line 0 is still scrolled off the top neither test
	// passes, so the autoscroll carries the drag upward until it is.
	firstLineRow := w.ContentY + 1 - w.ViewState.ViewOffsetY
	if y < firstLineRow || (y <= 1 && firstLineRow <= 1) {
		return 0, 0, true
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
	if hw, hl, _, hcr, hok := e.mouseHit(x, y); hok && hw == w {
		return hl, hcr, true // drag endpoint snaps to the nearest edge too
	}
	return 0, 0, false
}

// Drag-edge autoscroll: while a drag selection holds the pointer beyond the
// viewport's top/bottom (or parked on the far column), the view scrolls and
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
//
// The same pointer-cannot-leave rule applies VERTICALLY when the viewport's
// content touches a grid edge: a host may never deliver drag coordinates
// beyond its grid (an SDL surface, a torn-off window — motion clips at the
// window edge), so when the content's last row IS the grid's last row,
// parking ON that row is the down-edge gesture (and row 1, when content
// starts there, the up-edge gesture). The engagement delay still guards
// against accidental edge grazes.
func (e *Editor) dragScrollTrack(w *viewport.Viewport, x, y int) {
	top := w.ContentY + 1
	bottom := w.ContentY + w.ContentHeight
	vert := 0
	if y < top {
		vert = y - top
	} else if y > bottom {
		vert = y - bottom
	} else if gridBottom := e.Renderer.Height; y >= gridBottom && bottom >= gridBottom {
		vert = 1 // pinned on the grid's last row, no rows beyond to report
	} else if y <= 1 && top <= 1 {
		vert = -1 // pinned on the grid's first row
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
		// A NEW gesture engaging autoscroll self-heals the tick throttle: if a
		// previous gesture's posted tick was ever lost (leaving the pending
		// flag stuck true), autoscroll would otherwise stay dead for the whole
		// session. Clearing here at worst lets one stale in-flight tick pair
		// with a fresh one — a single extra scroll step, harmless.
		e.dragScrollPending.Store(false)
		ds.stop = make(chan struct{})
		go e.dragScrollLoop(ds.stop)
	}
}

// beginDragTxn opens (once) the user-command transaction that makes a whole
// drag selection ONE undo step: garland coalesces every mark movement inside
// it into a single revision. The buffer is pinned so a mid-gesture swap
// cannot orphan the open transaction. The transaction itself opens lazily in
// garland on the first mutation, so a drag that never marks costs nothing.
func (e *Editor) beginDragTxn(buf *buffer.Buffer) {
	if e.dragTxnBuf != nil || buf == nil {
		return
	}
	e.dragTxnBuf = buf
	buf.BeginUserCommand("drag_select")
}

// endDragTxn closes the drag-selection transaction, if one is open. Called on
// release (the gesture's natural end) and defensively on a fresh press (a
// release the terminal never delivered).
func (e *Editor) endDragTxn() {
	if e.dragTxnBuf == nil {
		return
	}
	e.dragTxnBuf.EndUserCommand()
	e.dragTxnBuf = nil
}

// dragScrollStop ends the autoscroll ticker (mouse release). The pending-tick
// throttle resets with the gesture: a tick still in flight runs as a no-op
// (dragSel is already inactive), and a tick that was LOST can no longer pin
// the flag — the next gesture starts clean either way.
func (e *Editor) dragScrollStop() {
	if e.dragScroll.stop != nil {
		close(e.dragScroll.stop)
	}
	e.dragScroll = dragScrollState{}
	e.dragScrollPending.Store(false)
}

// dragScrollLoop is the ticker goroutine: it posts dragScrollTick onto the
// editor main loop at the scroll cadence, one tick in flight at a time
// (a tick that cannot be consumed — a torn-down session — just parks).
// A watchdog guards the one-in-flight throttle: a posted tick that somehow
// never runs (its Do event lost between host layers) would pin the pending
// flag and silence autoscroll; after ~half a second of consecutive skipped
// ticks the flag is force-cleared and posting resumes. A tick genuinely still
// in flight that late just pairs with the next one — one extra scroll step.
func (e *Editor) dragScrollLoop(stop chan struct{}) {
	t := time.NewTicker(dragScrollInterval)
	defer t.Stop()
	skipped := 0
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if !e.dragScrollPending.CompareAndSwap(false, true) {
				if skipped++; skipped >= 8 {
					e.dragScrollPending.Store(false)
					skipped = 0
				}
				continue
			}
			skipped = 0
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
	w := e.ViewportManager.GetFocusedViewport()
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
			// Scroll toward the edge the pointer is past, named visually:
			// dragScrollTrack arms on the right edge under ltr and the left
			// under rtl, and scroll_left/scroll_right are visual commands, so
			// the two agree without any sign juggling here.
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
	e.endDragTxn()
	if !e.mousePressed.active {
		return
	}
	pressed := e.mousePressed
	onButton := e.hitOnPressedButton(x, y)
	e.mousePressed = pressedLink{}
	e.mouseOnCaptured = false
	if onButton {
		// Follow in the viewport the press CAPTURED — which may be an
		// unfocused (but visible) one; the follow neither needs nor takes
		// focus there.
		if w := e.ViewportManager.GetViewport(pressed.winID); w != nil {
			for _, s := range e.linkSpansOnLine(w, pressed.line) {
				if s.Start == pressed.start {
					sp := s
					e.followLinkSpan(w, &sp)
					break
				}
			}
		}
	}
	e.RequestRender()
}

// mouseHoverAt tracks the link under the pointer (plain motion, no button).
// Hover follows the same modal rule as every mouse action — only the focused
// viewport's links light up — and repaints only when the hovered identity
// actually changes.
func (e *Editor) mouseHoverAt(x, y int) {
	nh := pressedLink{}
	if w, docLine, runePos, _, ok := e.mouseHit(x, y); ok && w.ViewState.LinkBrowsing {
		focused := e.ViewportManager.GetFocusedViewport() == w
		// The focused viewport hovers links in either mode; an UNFOCUSED but
		// truly visible viewport hovers only its browse-mode BUTTONS — the
		// ones a click now follows — and never while a modal prompt is up.
		hoverable := focused ||
			(!e.promptHasPriority() && e.viewportOnScreen(w) && w.BrowseActive)
		if hoverable {
			for _, s := range e.linkSpansOnLine(w, docLine) {
				if s.Start <= runePos && runePos < s.End {
					nh = pressedLink{active: true, winID: w.ID, line: docLine, start: s.Start}
					break
				}
			}
		}
	}
	if nh != e.mouseHovered {
		e.mouseHovered = nh
		e.RequestRender()
	}
}

// hitOnPressedButton reports whether the coordinates land on the very button
// the press CAPTURED (same viewport, same line, same span).
func (e *Editor) hitOnPressedButton(x, y int) bool {
	w, docLine, runePos, _, ok := e.mouseHit(x, y)
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

// wheelTarget resolves the viewport a wheel event over row y may scroll: the
// on-screen viewport under the pointer. The focused viewport always
// qualifies; an UNFOCUSED one qualifies too — the wheel scrolls what the
// pointer is over, without moving focus — except while a modal prompt holds
// focus (then, as with every mouse action, only the prompt itself responds).
func (e *Editor) wheelTarget(x, y int) *viewport.Viewport {
	w := e.viewportAt(x, y)
	if w == nil || w.Buffer == nil {
		return nil
	}
	if e.ViewportManager.GetFocusedViewport() == w {
		return w
	}
	if e.promptHasPriority() || !e.viewportOnScreen(w) {
		return nil
	}
	return w
}

// mouseScrollHoriz scrolls the viewport under the pointer sideways by one
// 8-column step (the same step and clamping as the scroll_left/scroll_right
// commands), but only once the barrier is cleared. dir is -1 for left, +1 for
// right. A direction reversal restarts the barrier.
func (e *Editor) mouseScrollHoriz(x, y, dir int) {
	w := e.wheelTarget(x, y)
	if w == nil {
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
	// The wheel is a VISUAL gesture, so it goes through the same visual
	// mapping the scroll_left/scroll_right commands use: tilting left moves the
	// view left whichever way the text runs.
	if dir < 0 {
		e.scrollViewHorizontal(w, -1)
	} else {
		e.scrollViewHorizontal(w, +1)
	}
}

// mouseScroll scrolls the viewport under the pointer by delta lines — the
// focused one, or any truly visible viewport the pointer is over (the wheel
// follows the pointer, not the focus; a modal prompt still blocks it).
func (e *Editor) mouseScroll(x, y int, delta int) {
	w := e.wheelTarget(x, y)
	if w == nil {
		return
	}
	// Free scroll: park the viewport delta lines away and leave the caret where
	// it is (detaching from caret-follow until a cursor/edit command re-engages).
	e.scrollViewByLines(w, delta)
}
