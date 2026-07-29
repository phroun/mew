// Package render provides terminal rendering for the editor.
package render

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/term"

	"github.com/phroun/mew/internal/bidi"
	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/textwidth"
	"github.com/phroun/mew/internal/viewport"
)

// selectionRange holds the normalized selection range for rendering.
type selectionRange struct {
	startLine int
	startRune int
	endLine   int
	endRune   int
	exists    bool
}

// CustomRendererFunc is a function that renders custom viewport content.
type CustomRendererFunc func(w *viewport.Viewport, screenWidth int) string

// ScreenRenderer handles terminal screen output.
type ScreenRenderer struct {
	Width  int
	Height int

	viewportManager *viewport.Manager
	layoutManager   *viewport.LayoutManager

	// layoutEpoch counts layout passes; each pass stamps the viewports it laid
	// out (Viewport.LayoutEpoch), giving mouse hit-testing a "truly on screen
	// this frame" test that stale geometry cannot satisfy.
	layoutEpoch uint64

	// Custom renderers for plugin viewports
	customRenderers map[string]CustomRendererFunc

	// rulerRenderer produces the column-ruler line drawn on the top row of any
	// viewport whose ViewState.ShowRuler is enabled (and that is taller than one
	// line).
	rulerRenderer CustomRendererFunc

	// syntaxColorizer returns per-rune SGR colors for a document line of a
	// viewport ("" entries and nil slices mean the normal text color). Set by
	// the editor when syntax highlighting is configured.
	syntaxColorizer func(w *viewport.Viewport, docLine int) []string

	// displayProvider supplies the per-line browse-mode display transform
	// (buttons, markup marker-hiding/restyle, double-width); nil — the default
	// — disables all substitution. See buttons.go.
	displayProvider DisplayProvider

	// caretHiddenFn reports whether the hardware caret should be hidden for a
	// viewport even though it is on screen — set by the editor to hide the caret
	// while it is inert inside a focused button. nil = never hide for this
	// reason.
	caretHiddenFn func(w *viewport.Viewport) bool

	// cursorStyleFn reports the DECSCUSR shape the focused viewport's mode calls
	// for — the insertCursor / overwriteCursor / navigationCursor options. Only
	// consulted on frames that show the cursor. nil leaves the shape alone.
	cursorStyleFn func(w *viewport.Viewport) int

	// peekLabelFn expands a peek-indicator label through the modebar's %CODE%
	// engine (so e.g. "[%SPU%]" resolves to the live stat_peek_up binding).
	// nil leaves the configured label verbatim.
	peekLabelFn func(raw string) string

	// Peek indicators state
	peekIndicators struct {
		StatPeekUp     bool
		StatPeekDown   bool
		PromptPeekUp   bool
		PromptPeekDown bool
	}

	// Terminal output and size query; virtualizable by hosts.
	out    io.Writer
	sizeFn func() (width, height int, err error)

	// Resize handling. resizeChan carries resize ticks from the native
	// watcher (SIGWINCH on unix), a host resize channel, or TriggerResize.
	resizeChan        chan struct{}
	onResizeFunc      func() // Callback called when terminal resizes
	watchNativeResize bool   // watch OS resize signals (real terminals only)
	nativeStop        func() // uninstalls the native watcher, if installed

	// Indicator glyphs/labels used to draw chrome (whitespace markers, gutter,
	// cursor indicators, peek tab labels).
	indicators config.Indicators

	// Layered color scheme; colors are resolved per viewport by class, buffer
	// type, and color name.
	colorScheme config.ColorScheme

	// baseRTL is the configured base text direction ([general] direction):
	// every line's bidi resolution starts from it.
	baseRTL bool

	// frame is the off-screen double buffer. MoveCursor/Write/ClearScreen/
	// Show/HideCursor paint into it; Render presents the diff to out. renderMu
	// serializes a frame (begin..present) against a concurrent resize reshape.
	frame    *backBuffer
	renderMu sync.Mutex

	// scrollbarSuppressed, when set, vetoes the scrollbar option for a
	// viewport the renderer cannot judge itself — the editor supplies it to
	// keep the bar off viewports whose buffer hosts a terminal session (the
	// terminal has its own scrollbar, and reserving a column would shrink
	// its grid).
	scrollbarSuppressed func(*viewport.Viewport) bool
}

// SetScrollbarSuppressor installs the editor's veto for the per-viewport
// scrollbar (see the scrollbarSuppressed field).
func (sr *ScreenRenderer) SetScrollbarSuppressor(fn func(*viewport.Viewport) bool) {
	sr.scrollbarSuppressed = fn
}

// showScrollbar reports whether this viewport reserves its outer column for
// the vertical scrollbar this frame: the scrollbar option is on, the viewport
// is ordinary buffer chrome (not a prompt, not custom-rendered), and the
// editor's suppressor (terminal sessions) does not veto it.
func (sr *ScreenRenderer) showScrollbar(w *viewport.Viewport) bool {
	if w == nil || !w.ViewState.Scrollbar || w.Buffer == nil ||
		w.CustomRenderer != "" || w.Type == viewport.PromptViewport {
		return false
	}
	if sr.scrollbarSuppressed != nil && sr.scrollbarSuppressed(w) {
		return false
	}
	return true
}

// SetIndicators sets the indicator glyphs used to draw editor chrome.
func (sr *ScreenRenderer) SetIndicators(ind config.Indicators) {
	sr.indicators = ind
}

// winRTL is the effective text direction for a viewport: its own
// ViewState.Direction override when set (prompt viewports are pinned "ltr"),
// else the editor-wide base direction.
func (sr *ScreenRenderer) winRTL(w *viewport.Viewport) bool {
	if w != nil {
		switch w.ViewState.Direction {
		case "ltr":
			return false
		case "rtl":
			return true
		}
	}
	return sr.baseRTL
}

// layoutFor computes a line's visual layout for a viewport, including
// direction-marker slots when the viewport's showBidi is enabled.
func (sr *ScreenRenderer) layoutFor(w *viewport.Viewport, runes []rune) *bidi.Layout {
	if w != nil && w.ViewState.ShowBidi {
		return bidi.ComputeMarked(runes, sr.winRTL(w))
	}
	return bidi.Compute(runes, sr.winRTL(w))
}

// slotWidth is the visual width of one layout slot: marker slots are one
// column; explicit direction controls are one column under a marked layout;
// everything else uses the ordinary rune width.
func (sr *ScreenRenderer) slotWidth(layout *bidi.Layout, runes []rune, entry, col int, w *viewport.Viewport) int {
	if entry < 0 {
		return 1
	}
	if layout != nil && layout.Glyph != nil && layout.Glyph[entry] == bidi.LigatureAbsorbed {
		return 0 // absorbed half of a lam-alef ligature
	}
	r := runes[entry]
	if layout != nil && layout.Marked && bidi.IsDirectionControl(r) {
		return 1
	}
	return sr.getRuneVisualWidth(r, col, w)
}

// physMargins maps a viewport's logical margins (Inner = reading-start side,
// Outer = opposite, like a book page) to physical left/right columns per the
// viewport's effective direction.
func (sr *ScreenRenderer) physMargins(w *viewport.Viewport) (left, right int) {
	if sr.winRTL(w) {
		return w.MarginOuter, w.MarginInner
	}
	return w.MarginInner, w.MarginOuter
}

// SetBaseRTL sets the base text direction used for bidi line layout.
func (sr *ScreenRenderer) SetBaseRTL(rtl bool) {
	sr.baseRTL = rtl
}

// SetColorScheme sets the layered color scheme used for all viewport rendering.
func (sr *ScreenRenderer) SetColorScheme(cs config.ColorScheme) {
	sr.colorScheme = cs
}

// col resolves a named color for a viewport, cascading viewport class ->
// buffer type -> global -> built-in defaults.
func (sr *ScreenRenderer) col(w *viewport.Viewport, name string) string {
	return sr.colorScheme.Resolve(w.Class, w.Type.Name(), name)
}

// NewScreenRenderer creates a new screen renderer targeting the real
// terminal (stdout); use SetTerminal to virtualize output and size.
func NewScreenRenderer(wm *viewport.Manager, lm *viewport.LayoutManager) *ScreenRenderer {
	sr := &ScreenRenderer{
		Width:             80,
		Height:            24,
		viewportManager:   wm,
		layoutManager:     lm,
		customRenderers:   make(map[string]CustomRendererFunc),
		resizeChan:        make(chan struct{}, 1),
		indicators:        config.DefaultIndicators(),
		colorScheme:       config.NewColorScheme(),
		out:               os.Stdout,
		sizeFn:            realTerminalSize,
		watchNativeResize: true,
	}

	// Get initial terminal size
	sr.updateSize()

	sr.frame = newBackBuffer(sr.Width, sr.Height)

	return sr
}

// realTerminalSize queries the real terminal's dimensions.
func realTerminalSize() (int, int, error) {
	return term.GetSize(int(os.Stdout.Fd()))
}

// SetTerminal virtualizes the renderer's terminal: output goes to out, size
// comes from sizeFn (nil keeps the real-terminal query), and native OS
// resize signals are only watched when watchNativeResize is true (hosts
// signal resizes via TriggerResize or a resize channel instead).
func (sr *ScreenRenderer) SetTerminal(out io.Writer, sizeFn func() (int, int, error), watchNativeResize bool) {
	if out != nil {
		sr.out = out
	}
	if sizeFn != nil {
		sr.sizeFn = sizeFn
	}
	sr.watchNativeResize = watchNativeResize
	sr.updateSize()
}

// TriggerResize signals a terminal size change manually: the renderer
// re-queries the size and re-renders. This is the virtualized stand-in for
// SIGWINCH; hosts call it (or feed a resize channel) when their terminal
// changes. Safe to call from any goroutine; coalesces bursts.
func (sr *ScreenRenderer) TriggerResize() {
	select {
	case sr.resizeChan <- struct{}{}:
	default: // a resize tick is already pending
	}
}

// RegisterCustomRenderer registers a custom renderer function for a viewport type.
func (sr *ScreenRenderer) RegisterCustomRenderer(name string, renderer CustomRendererFunc) {
	sr.customRenderers[name] = renderer
}

// SetRulerRenderer sets the function used to render the per-viewport column ruler.
func (sr *ScreenRenderer) SetRulerRenderer(renderer CustomRendererFunc) {
	sr.rulerRenderer = renderer
}

// SetSyntaxColorizer sets the per-line syntax-color source used as the base
// text color of content cells.
// LayoutEpoch reports the current layout pass counter. A viewport whose
// LayoutEpoch equals this value was laid out by the most recent frame — it is
// truly on screen, with current geometry.
func (sr *ScreenRenderer) LayoutEpoch() uint64 {
	return sr.layoutEpoch
}

func (sr *ScreenRenderer) SetSyntaxColorizer(colorizer func(w *viewport.Viewport, docLine int) []string) {
	sr.syntaxColorizer = colorizer
}

// SetPeekLabelResolver sets the function that expands a peek-indicator label
// through the modebar %CODE% engine (resolving codes like %SPU% to live key
// bindings). nil leaves labels verbatim.
func (sr *ScreenRenderer) SetPeekLabelResolver(fn func(raw string) string) {
	sr.peekLabelFn = fn
}

// SetFlipBidiForHost re-emits RTL runs in logical order for host terminals
// that apply their own bidi reordering (macOS Terminal.app); off emits mew's
// visual order for terminals that do not reorder (iTerm2, xterm). Forces a
// full repaint so the whole screen switches convention at once.
func (sr *ScreenRenderer) SetFlipBidiForHost(flip bool) {
	sr.renderMu.Lock()
	defer sr.renderMu.Unlock()
	if sr.frame.flipBidi != flip {
		sr.frame.flipBidi = flip
		sr.frame.forceRedraw()
	}
}

// SawRTLContent reports whether any presented frame has contained strong-RTL
// text — the trigger point for the one-time flipBidiForHost=auto probe.
func (sr *ScreenRenderer) SawRTLContent() bool {
	sr.renderMu.Lock()
	defer sr.renderMu.Unlock()
	return sr.frame.sawRTL
}

// EmitProbe writes a control/probe sequence directly to the terminal,
// bypassing the back buffer, and invalidates the given screen row (1-based) so
// any glyphs the probe painted there are repainted on the next frame. Used by
// the flipBidiForHost=auto detection.
func (sr *ScreenRenderer) EmitProbe(seq string, dirtyRow int) {
	sr.renderMu.Lock()
	defer sr.renderMu.Unlock()
	fmt.Fprint(sr.out, seq)
	y := dirtyRow - 1
	if y >= 0 && y < sr.frame.h {
		for x := 0; x < sr.frame.w; x++ {
			sr.frame.disp[y][x] = bbCell{width: -1}
		}
	}
}

// rulerActive reports whether a viewport should render a column ruler on its top
// line. A one-line viewport ignores the ruler option: the single content line
// takes precedence. Custom-rendered viewports draw their own content entirely.
func rulerActive(w *viewport.Viewport, height int) bool {
	return w.ViewState.ShowRuler && height > 1 && w.CustomRenderer == ""
}

// updateSize updates the screen dimensions from the terminal size source.
func (sr *ScreenRenderer) updateSize() {
	width, height, err := sr.sizeFn()
	if err == nil {
		sr.Width = width
		sr.Height = height
	} else {
		// Fallback to defaults
		sr.Width = 80
		sr.Height = 24
	}
}

// SetOnResize sets the callback function to call when terminal resizes.
func (sr *ScreenRenderer) SetOnResize(callback func()) {
	sr.onResizeFunc = callback
}

// Start begins resize monitoring.
func (sr *ScreenRenderer) Start() {
	if sr.watchNativeResize {
		sr.nativeStop = watchNativeResize(sr)
	}
	go func() {
		for range sr.resizeChan {
			sr.updateSize()
			if sr.onResizeFunc != nil {
				sr.onResizeFunc()
			}
		}
	}()
}

// Stop stops resize monitoring.
func (sr *ScreenRenderer) Stop() {
	if sr.nativeStop != nil {
		sr.nativeStop()
		sr.nativeStop = nil
	}
	close(sr.resizeChan)
}

// EnableMouseReporting asks the terminal for mouse tracking with SGR
// encoding: press/release, cell motion, and all-motion (?1003, for hover
// affordances — graphical hosts deliver it; plain terminals under ?1002-only
// outer stacks simply never send plain motion). purfecterm answers these by
// routing mouse reports to the hosted app instead of doing local selection.
// Written directly (a mode switch, not a frame).
func (sr *ScreenRenderer) EnableMouseReporting() {
	fmt.Fprint(sr.out, "\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1006h")
}

// SetLogicalColumns switches cursor addressing to logical columns, for
// flex-width terminal hosts (purfecterm under DECSET 2027) whose grid stores
// one cell per character: a CUP column there counts characters, not visual
// cells, so a visual column must be translated by the wide glyphs before it.
// Plain terminals keep the classic visual-column addressing (off, default).
func (sr *ScreenRenderer) SetLogicalColumns(enabled bool) {
	sr.renderMu.Lock()
	defer sr.renderMu.Unlock()
	sr.frame.logicalCUP = enabled
}

// EnableGraphemeWidth requests DEC private mode 2027 (grapheme-cluster width):
// the terminal measures a cell's width per grapheme cluster — East Asian wide
// glyphs, ZWJ emoji, combining sequences — the same model mew's own textwidth
// uses to lay out and place the caret. Emitting it aligns the terminal (mew's
// purfecterm, or any host that honors 2027) with those expectations; terminals
// that do not implement the mode ignore the unknown DECSET. Written directly, a
// mode switch, not a frame. Reset by Cleanup.
func (sr *ScreenRenderer) EnableGraphemeWidth() {
	fmt.Fprint(sr.out, "\x1b[?2027h")
}

// Cleanup restores terminal state. It writes directly to the terminal
// (bypassing the back buffer) because it runs at shutdown, after the last
// frame, when there is no present() to flush the buffer. Mouse reporting is
// turned back off so the terminal's own selection returns to the user, and the
// grapheme-width mode (2027) is reset for symmetry. The cursor shape is reset
// to the terminal's own default (DECSCUSR 0) so the shell that follows does not
// inherit whichever mode's cursor mew happened to leave selected.
func (sr *ScreenRenderer) Cleanup() {
	fmt.Fprintf(sr.out, "\x1b[?2027l\x1b[?1006l\x1b[?1003l\x1b[?1002l\x1b[?1000l\x1b[0 q\x1b[%d;%dH\x1b[?25h\x1b[0m", sr.Height, 1)
}

// ClearScreen forces a full clear-and-repaint on the next presented frame.
func (sr *ScreenRenderer) ClearScreen() {
	sr.ForceRedraw()
}

// ForceRedraw discards the renderer's knowledge of what the terminal shows so
// the next frame clears the screen and repaints every cell (the screen_refresh
// command, and recovery from external corruption of the display).
func (sr *ScreenRenderer) ForceRedraw() {
	sr.renderMu.Lock()
	defer sr.renderMu.Unlock()
	sr.frame.forceRedraw()
}

// MoveCursor positions the pen for subsequent Writes (1-indexed). It paints
// into the back buffer; nothing reaches the terminal until the frame is
// presented.
func (sr *ScreenRenderer) MoveCursor(x, y int) {
	sr.frame.moveTo(x, y)
}

// ShowCursor requests the hardware cursor be visible after the next present.
func (sr *ScreenRenderer) ShowCursor() {
	sr.frame.curVisible = true
}

// HideCursor requests the hardware cursor be hidden after the next present.
func (sr *ScreenRenderer) HideCursor() {
	sr.frame.curVisible = false
}

// SetCursorStyle selects the hardware cursor's DECSCUSR shape (0 default,
// 1/2 blinking/steady block, 3/4 underline, 5/6 bar) for the next present.
// Out-of-range values are ignored rather than passed to the terminal.
func (sr *ScreenRenderer) SetCursorStyle(style int) {
	if style < 0 || style > 6 {
		return
	}
	sr.frame.curStyle = style
}

// SetCursorStyleFn installs the callback that reports which DECSCUSR shape the
// focused viewport's current mode calls for (insert / overwrite / navigation).
// It is consulted only on frames that SHOW the cursor, so a shape is never
// selected for a caret the editor is deliberately hiding.
func (sr *ScreenRenderer) SetCursorStyleFn(fn func(w *viewport.Viewport) int) {
	sr.cursorStyleFn = fn
}

// ClearLine blanks the pen's row in the back buffer.
func (sr *ScreenRenderer) ClearLine() {
	if sr.frame.penY >= 0 && sr.frame.penY < sr.frame.h {
		row := sr.frame.cur[sr.frame.penY]
		for x := range row {
			row[x] = bbCell{width: 1}
		}
	}
}

// Write paints text at the pen position into the back buffer.
func (sr *ScreenRenderer) Write(text string) {
	sr.frame.writeString(text)
}

// Sync flushes the output when it is backed by a real file.
func (sr *ScreenRenderer) Sync() {
	if f, ok := sr.out.(*os.File); ok {
		f.Sync()
	}
}

// Render renders all visible viewports. It composes the whole frame into the
// off-screen back buffer, then presents the minimal diff to the terminal.
func (sr *ScreenRenderer) Render(layout viewport.Layout) {
	sr.renderMu.Lock()
	defer sr.renderMu.Unlock()

	// The buffer may have been reshaped by a resize since the last frame.
	if sr.frame.w != sr.Width || sr.frame.h != sr.Height {
		sr.frame.reshape(sr.Width, sr.Height)
	}
	sr.frame.begin()
	defer sr.frame.present(sr.out)
	sr.paintFrame(layout)
}

// CaptureFrame renders a full frame of the layout to an ANSI string — a forced,
// complete repaint against a blank display — without disturbing the live
// terminal or the incremental diff state. It powers debug_screen: the returned
// bytes, written to a file, reproduce the screen when cat'd to a terminal.
func (sr *ScreenRenderer) CaptureFrame(layout viewport.Layout) string {
	sr.renderMu.Lock()
	defer sr.renderMu.Unlock()

	// Paint into a throwaway buffer whose display starts blank, so present()
	// emits every cell (a full frame) rather than a diff against the live
	// terminal. A leading clear makes the file self-contained.
	saved := sr.frame
	tmp := newBackBuffer(sr.Width, sr.Height)
	tmp.flipBidi = saved.flipBidi
	tmp.pendingClear = true
	sr.frame = tmp
	tmp.begin()
	sr.paintFrame(layout)
	var sb strings.Builder
	tmp.present(&sb)
	sr.frame = saved
	return sb.String()
}

// paintFrame paints the whole layout into sr.frame — peek indicators, viewport
// groups, ghost/secondary/real cursors — and applies the hardware-cursor
// visibility. The caller must hold renderMu and have called sr.frame.begin();
// it presents the frame afterward. Shared by Render and CaptureFrame.
func (sr *ScreenRenderer) paintFrame(layout viewport.Layout) {
	// Save peek indicator state
	sr.peekIndicators.StatPeekUp = layout.NeedsStatPeekUp
	sr.peekIndicators.StatPeekDown = layout.NeedsStatPeekDown
	sr.peekIndicators.PromptPeekUp = layout.NeedsPromptPeekUp
	sr.peekIndicators.PromptPeekDown = layout.NeedsPromptPeekDown

	// Update viewport content properties
	sr.updateViewportContentProperties(layout)

	// Render all viewport groups
	sr.renderViewportGroup(layout.TopLayout)
	sr.renderViewportGroup(layout.MainLayout)
	sr.renderViewportGroup(layout.BottomLayout)

	// Render peek indicators
	sr.renderPeekIndicators(layout)

	// Render ghost cursor if present, then position real cursor
	hideCursor := false
	focusedViewport := sr.viewportManager.GetFocusedViewport()
	if focusedViewport != nil {
		viewportLayout := layout.FindViewportLayout(focusedViewport.ID)
		if viewportLayout != nil {
			// Render ghost cursor first (if present)
			if focusedViewport.HasGhostCursor {
				sr.renderGhostCursor(focusedViewport, viewportLayout)
			}
			// Secondary bidi cursor at an automatic direction boundary
			sr.renderSecondaryCursor(focusedViewport, viewportLayout)
			// Position the real cursor
			hideCursor = sr.positionCursor(focusedViewport, viewportLayout)
		}
		// The caret is inert inside a focused button: keep it positioned (so
		// its column tracks) but hide the hardware cursor.
		if sr.caretHiddenFn != nil && sr.caretHiddenFn(focusedViewport) {
			hideCursor = true
		}
	}

	if hideCursor {
		sr.HideCursor()
	} else {
		// Only a frame that actually shows the caret selects its shape, so the
		// mode's cursor style is never asserted for a caret being hidden.
		if sr.cursorStyleFn != nil && focusedViewport != nil {
			sr.SetCursorStyle(sr.cursorStyleFn(focusedViewport))
		}
		sr.ShowCursor()
	}
}

// updateViewportContentProperties updates calculated properties on viewports.
// Each laid-out viewport is stamped with the new layout epoch: geometry on a
// viewport whose stamp is older belongs to an earlier frame (a background main
// not shown now) and mouse hit-testing must ignore it.
func (sr *ScreenRenderer) updateViewportContentProperties(layout viewport.Layout) {
	allLayouts := append(append(layout.TopLayout, layout.MainLayout...), layout.BottomLayout...)
	sr.layoutEpoch++

	for _, wl := range allLayouts {
		w := wl.Viewport

		w.LayoutEpoch = sr.layoutEpoch
		w.ContentY = wl.Y
		w.ContentHeight = wl.Height

		// Adjust for the column ruler, which occupies the viewport's top line
		if rulerActive(w, wl.Height) {
			w.ContentY++
			w.ContentHeight--
		}

		// Adjust for message bars
		if w.MessageTopInner != "" || w.MessageTopCenter != "" || w.MessageTopOuter != "" {
			w.ContentY++
			w.ContentHeight--
		}
		if w.MessageBottomInner != "" || w.MessageBottomCenter != "" || w.MessageBottomOuter != "" {
			w.ContentHeight--
		}

		// Update line number width
		if w.ViewState.ShowLineNumbers && w.Buffer != nil {
			lineCount := w.Buffer.GetLineCount()
			if lineCount < 10 {
				lineCount = 10
			}
			w.LineNumWidth = len(fmt.Sprintf("%d", lineCount)) + 1
			// In browse mode a heading may render double-width. Round the gutter
			// up to an even width so a doubled row can show exactly half as many
			// gutter cells (each drawn 2x) and align its content with the normal
			// rows — otherwise an odd gutter leaves a one-column notch.
			if w.BrowseActive && w.LineNumWidth%2 != 0 {
				w.LineNumWidth++
			}
		}

		// Calculate content dimensions. ContentWidth is the width of the text
		// area, so it must exclude the line-number gutter (matching the
		// baseContentWidth used in renderContent); otherwise horizontal-scroll
		// decisions that key off ContentWidth let the cursor run off the edge.
		lineNumWidth := 0
		if w.ViewState.ShowLineNumbers {
			lineNumWidth = w.LineNumWidth
		}
		// The vertical scrollbar reserves the OUTER physical column — the
		// rightmost in LTR, the leftmost in RTL — outside the margins.
		w.ScrollbarX = -1
		w.ScrollbarTrackH = 0
		sbw := 0
		if sr.showScrollbar(w) {
			sbw = 1
			w.ScrollbarTrackH = w.ContentHeight
			if sr.winRTL(w) {
				w.ScrollbarX = 0
			} else {
				w.ScrollbarX = sr.Width - 1
				// The screen's bottom-right cell can never be written (it
				// scrolls the terminal), so an LTR bar reaching the bottom
				// screen row gives that cell up as its corner: the track is
				// one row shorter and the thumb bottoms out on the
				// second-to-last row instead of clipping into the corner.
				if w.ContentY+w.ContentHeight == sr.Height {
					w.ScrollbarTrackH--
				}
			}
		}
		marginL, _ := sr.physMargins(w)
		w.ContentWidth = sr.Width - w.MarginInner - lineNumWidth - w.MarginOuter - sbw
		if sr.winRTL(w) {
			// The gutter mirrors to the right side; content starts after the
			// scrollbar column and the physical left margin.
			w.ContentX = marginL + sbw
		} else {
			w.ContentX = marginL + lineNumWidth
		}
	}
}

// renderViewportGroup renders a group of viewports.
func (sr *ScreenRenderer) renderViewportGroup(layouts []viewport.ViewportLayout) {
	for _, wl := range layouts {
		sr.renderViewport(wl.Viewport, wl.Y+1, wl.Height)
	}
}

// renderViewport renders a single viewport.
func (sr *ScreenRenderer) renderViewport(w *viewport.Viewport, startY, height int) {
	messagesColor := sr.col(w, "messages")
	resetColor := sr.col(w, "reset")

	// Check for custom renderer
	if w.CustomRenderer != "" {
		if renderer, ok := sr.customRenderers[w.CustomRenderer]; ok {
			// Record the bar's screen geometry (it writes from column 1) so the
			// editor can hit-test clicks on it — e.g. the modebar's nav-history
			// buttons. A custom-rendered chrome viewport has no Buffer, so this
			// never affects content hit-testing (viewportAtRow requires a buffer).
			w.ContentX = 0
			w.ContentY = startY - 1
			w.ContentHeight = height
			sr.MoveCursor(1, startY)
			content := renderer(w, sr.Width)
			sr.Write(content)
			return
		}
	}

	// Render message bars and content
	y := startY
	remainingHeight := height

	// Column ruler on the viewport's top line, above everything else
	if rulerActive(w, height) && sr.rulerRenderer != nil {
		sr.MoveCursor(1, y)
		sr.Write(sr.rulerRenderer(w, sr.Width))
		y++
		remainingHeight--
	}

	// Top message bar: the Inner slot renders on the reading-start side
	// (left in LTR, right in RTL), Outer on the opposite edge.
	if w.MessageTopInner != "" || w.MessageTopCenter != "" || w.MessageTopOuter != "" {
		sr.MoveCursor(1, y)
		sr.Write(messagesColor)
		if sr.winRTL(w) {
			sr.renderMessageBar(w.MessageTopOuter, w.MessageTopCenter, w.MessageTopInner, sr.Width)
		} else {
			sr.renderMessageBar(w.MessageTopInner, w.MessageTopCenter, w.MessageTopOuter, sr.Width)
		}
		sr.Write(resetColor)
		y++
		remainingHeight--
	}

	// Main content area
	contentHeight := remainingHeight
	if w.MessageBottomInner != "" || w.MessageBottomCenter != "" || w.MessageBottomOuter != "" {
		contentHeight--
	}

	sr.renderContent(w, y, contentHeight)
	y += contentHeight

	// Bottom message bar, Inner/Outer mapped like the top bar.
	if w.MessageBottomInner != "" || w.MessageBottomCenter != "" || w.MessageBottomOuter != "" {
		sr.MoveCursor(1, y)
		sr.Write(messagesColor)
		if sr.winRTL(w) {
			sr.renderMessageBar(w.MessageBottomOuter, w.MessageBottomCenter, w.MessageBottomInner, sr.Width)
		} else {
			sr.renderMessageBar(w.MessageBottomInner, w.MessageBottomCenter, w.MessageBottomOuter, sr.Width)
		}
		sr.Write(resetColor)
	}
}

// renderMessageBar renders a message bar with left, center, and right content.
func (sr *ScreenRenderer) renderMessageBar(left, center, right string, width int) {
	sr.Write(composeMessageBar(left, center, right, width))
}

// composeMessageBar builds a message-bar line: left content, center content
// centered, right content right-aligned, padded to exactly width columns and
// ellipsized (never over-long, which would wrap onto the row below). ANSI
// escapes are measured as zero-width.
func composeMessageBar(left, center, right string, width int) string {
	// Calculate visible lengths (ANSI-aware)
	leftLen := calculateAnsiAwareLength(left)
	centerLen := calculateAnsiAwareLength(center)
	rightLen := calculateAnsiAwareLength(right)

	// Build the line
	line := left

	// Add spacing for center
	if center != "" {
		centerStart := (width - centerLen) / 2
		if centerStart > leftLen {
			for i := leftLen; i < centerStart; i++ {
				line += " "
			}
			line += center
		}
	}

	// Add spacing for right
	currentLen := calculateAnsiAwareLength(line)
	rightStart := width - rightLen
	if rightStart > currentLen {
		for i := currentLen; i < rightStart; i++ {
			line += " "
		}
		line += right
	}

	// Pad to full width
	currentLen = calculateAnsiAwareLength(line)
	for i := currentLen; i < width; i++ {
		line += " "
	}

	// Never emit more columns than the bar is wide. An over-long message would
	// wrap onto the row below in the terminal, corrupting (blanking) the
	// viewports beneath it — so ellipsize it to fit instead of omitting it.
	if calculateAnsiAwareLength(line) > width {
		line = truncateToWidth(line, width)
	}

	return line
}

// truncateToWidth limits s to at most maxCols display columns, appending a
// one-column ellipsis ("…") when it must cut. ANSI escape sequences are
// preserved (they occupy no columns) and are never split; wide runes count as
// their display width.
func truncateToWidth(s string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	if calculateAnsiAwareLength(s) <= maxCols {
		return s
	}
	limit := maxCols - 1 // reserve one column for the ellipsis
	var b strings.Builder
	cols := 0
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			b.WriteRune(r)
			continue
		}
		if inEscape {
			b.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		w := textwidth.Rune(r)
		if cols+w > limit {
			break
		}
		b.WriteRune(r)
		cols += w
	}
	b.WriteString("…")
	return b.String()
}

// renderContent renders the main content area of a viewport.
func (sr *ScreenRenderer) renderContent(w *viewport.Viewport, startY, height int) {
	textColor := sr.col(w, "text")
	messagesColor := sr.col(w, "messages")
	lineNumbersColor := sr.col(w, "lineNumbers")
	resetColor := sr.col(w, "reset")

	lineNumWidth := 0
	if w.ViewState.ShowLineNumbers {
		lineNumWidth = w.LineNumWidth
	}

	marginL, marginR := sr.physMargins(w)
	sbw := 0
	var sbPos, sbLen, sbTrackH int
	if sr.showScrollbar(w) {
		// One column narrower, and the thumb geometry is fixed for the whole
		// frame: the track is ScrollbarTrackH (the content rows, minus the
		// bottom-right corner when the layout gave it up — see
		// updateViewportContentProperties), the scroll range comes from the
		// full page. Shared with the mouse path via ScrollbarThumb.
		sbw = 1
		sbTrackH = w.ScrollbarTrackH
		if sbTrackH > height {
			sbTrackH = height
		}
		lineCount := 0
		if w.Buffer != nil {
			lineCount = w.Buffer.GetLineCount()
		}
		sbPos, sbLen = viewport.ScrollbarThumb(sbTrackH, height, lineCount, w.ViewState.ViewOffsetY)
	}
	scrollbarTrackColor := sr.col(w, "scrollbarTrack")
	scrollbarThumbColor := sr.col(w, "scrollbarThumb")
	baseContentWidth := sr.Width - w.MarginInner - lineNumWidth - w.MarginOuter - sbw

	// Get selection range once for all lines. A transient find/replace match
	// highlight takes precedence: while a match is being offered, it is shown
	// as the selection and the user's block is not, though the block marks
	// stay set underneath and repaint when the match highlight clears. The
	// MatchHighlight flag gates the extra mark lookup so ordinary rendering
	// pays nothing for it.
	var sel selectionRange
	if w.Buffer != nil {
		var startLine, startRune, endLine, endRune int
		var exists bool
		if w.MatchHighlight {
			startLine, startRune, endLine, endRune, exists = w.Buffer.GetMatchRange()
		} else {
			startLine, startRune, endLine, endRune, exists = w.Buffer.GetBlockRange()
		}
		sel = selectionRange{
			startLine: startLine,
			startRune: startRune,
			endLine:   endLine,
			endRune:   endRune,
			exists:    exists,
		}
	}

	for row := 0; row < height; row++ {
		screenY := startY + row
		sr.MoveCursor(1, screenY)

		// Corner cut: the bottom-right cell can never be written (it scrolls the
		// terminal), so reserve it on the bottom row — but only when the content
		// actually reaches that corner. Under direction=rtl with a line-number
		// gutter, the gutter sits on the right and absorbs the corner, so the
		// content stays full width (reducing it there would shift the whole
		// right-anchored bottom row left by one). The back buffer paints the
		// corner cell's background without landing a glyph in it.
		contentWidth := baseContentWidth
		if screenY == sr.Height && !(sr.winRTL(w) && w.ViewState.ShowLineNumbers) &&
			!(sbw > 0 && !sr.winRTL(w)) {
			// An LTR scrollbar occupies the corner column itself (its glyph is
			// simply skipped on that row, below), so the content stays full.
			contentWidth--
		}

		// The scrollbar's cell for this row: thumb within [sbPos, sbPos+sbLen),
		// track elsewhere, nothing past the track (the corner row a
		// bottom-of-screen LTR bar gave up). Under RTL the bar is the
		// leftmost column, emitted before everything else; under LTR it is
		// the rightmost, emitted last.
		sbGlyph, sbColor := "", ""
		if sbw > 0 && row < sbTrackH {
			if row >= sbPos && row < sbPos+sbLen {
				sbGlyph, sbColor = sr.indicators.ScrollbarThumb, scrollbarThumbColor
			} else {
				sbGlyph, sbColor = sr.indicators.ScrollbarTrack, scrollbarTrackColor
			}
		}
		if sbGlyph != "" && sr.winRTL(w) {
			sr.Write(sbColor)
			sr.Write(sbGlyph)
		}

		// Physical left margin. Row messages (prompt labels) belong to the
		// INNER margin, which is the left side only in LTR viewports.
		if marginL > 0 {
			sr.Write(messagesColor)
			if !sr.winRTL(w) && row < len(w.RowMessages) {
				msg := w.RowMessages[row]
				if len(msg) > marginL {
					msg = msg[:marginL]
				}
				sr.Write(msg)
				for i := len(msg); i < marginL; i++ {
					sr.Write(" ")
				}
			} else {
				for i := 0; i < marginL; i++ {
					sr.Write(" ")
				}
			}
		}

		rtl := sr.winRTL(w)
		docLine := w.ViewState.ViewOffsetY + row

		// Fetch the line content and its browse-mode display transform up front:
		// the gutter (below) needs to know whether this row is double-width.
		var disp *lineDisplay
		var dispSyn []string
		var lineContent, lineEnding string
		haveContent := w.Buffer != nil && docLine < w.Buffer.GetLineCount()
		if haveContent {
			rawLine := w.Buffer.GetLine(docLine)
			lineContent = strings.TrimRight(rawLine, "\n\r")
			lineEnding = rawLine[len(lineContent):] // "", "\n", "\r\n", ...
			disp, dispSyn = sr.displayFor(w, docLine, lineContent)
		}
		doubleWide := disp != nil && disp.DoubleWide

		// Line number gutter. Under RTL it mirrors to the RIGHT side (emitted
		// after the content below). A double-width row shows blank gutter cells
		// in the gutter colour instead of the number — a doubled number is
		// oversized and misaligned. The gutter width is rounded even in browse
		// mode, so half as many cells (each drawn 2x) match the normal gutter
		// exactly and still leave room for a marker.
		if w.ViewState.ShowLineNumbers && !rtl {
			sr.Write(lineNumbersColor)
			switch {
			case doubleWide:
				sr.Write(strings.Repeat(" ", lineNumWidth/2))
			case haveContent:
				sr.Write(fmt.Sprintf("%*d ", lineNumWidth-1, docLine+1))
			default:
				sr.Write(fmt.Sprintf("%*s ", lineNumWidth-1, sr.indicators.GutterEmpty))
			}
		}

		// Content
		if haveContent {
			// A double-width (heading) line: the terminal shows only the left
			// half at 2x, so lay the content into half the columns, interpret the
			// horizontal scroll at one cell per two scrolled positions, and mark
			// the row so present() emits the DECDWL mode.
			lineWidth := contentWidth
			voff := w.ViewState.ViewOffsetX
			if doubleWide {
				lineWidth = contentWidth / 2
				if lineWidth < 1 {
					lineWidth = 1
				}
				voff /= 2
			}
			displayLine := sr.prepareLineForDisplay(lineContent, lineEnding, lineWidth, voff, w, docLine, sel, disp, dispSyn)
			sr.Write(displayLine)
			if doubleWide {
				// The erase-to-end fills with the row's own text background.
				sr.frame.setRowWide(textColor)
			}
		} else {
			// Empty line - fill with background color
			sr.Write(textColor)
			for i := 0; i < contentWidth; i++ {
				sr.Write(" ")
			}
			sr.Write(resetColor)
		}

		// RTL: the mirrored line-number gutter, between content and right margin.
		if w.ViewState.ShowLineNumbers && rtl {
			sr.Write(lineNumbersColor)
			switch {
			case doubleWide:
				sr.Write(strings.Repeat(" ", lineNumWidth/2))
			case haveContent:
				sr.Write(fmt.Sprintf(" %-*d", lineNumWidth-1, docLine+1))
			default:
				sr.Write(fmt.Sprintf(" %-*s", lineNumWidth-1, sr.indicators.GutterEmpty))
			}
		}

		// Physical right margin — the INNER margin of an RTL viewport, where
		// its row messages (prompt labels) paint.
		if marginR > 0 {
			sr.Write(messagesColor)
			if sr.winRTL(w) && row < len(w.RowMessages) {
				msg := w.RowMessages[row]
				if len(msg) > marginR {
					msg = msg[:marginR]
				}
				sr.Write(msg)
				for i := len(msg); i < marginR; i++ {
					sr.Write(" ")
				}
			} else {
				for i := 0; i < marginR; i++ {
					sr.Write(" ")
				}
			}
		}

		// LTR scrollbar: the outermost (rightmost) physical column, past the
		// outer margin. On a double-width (DECDWL) row every cell renders two
		// columns wide, so the bar is placed by LOGICAL column — padded out to
		// the last whole doubled cell — and its glyph doubles with the row,
		// which is the desired look. A row past the track (the given-up
		// bottom-right corner) has no glyph and the corner stays blank.
		if sbGlyph != "" && !rtl {
			if doubleWide {
				lineWidth := contentWidth / 2
				if lineWidth < 1 {
					lineWidth = 1
				}
				written := marginL + lineNumWidth/2 + lineWidth + marginR
				if pad := sr.Width/2 - 1 - written; pad > 0 {
					sr.Write(textColor)
					sr.Write(strings.Repeat(" ", pad))
				}
			}
			sr.Write(sbColor)
			sr.Write(sbGlyph)
		}

		sr.Write(resetColor)
	}
}

// composeTabMarker builds the whitespace-marker representation of a tab that
// occupies `width` visual columns: VisibleTabEnd right-aligned (highest
// precedence), VisibleTabStart left-aligned (next), and VisibleTabFill's first
// rune repeated in between — e.g. ":--->|". When space is tight, end wins first
// (keeping its rightmost runes, so a 1-wide tab shows just "|"), then start,
// then fill. Returns exactly `width` runes, each one visual column wide.
func (sr *ScreenRenderer) composeTabMarker(width int) []rune {
	if width <= 0 {
		return nil
	}
	start := []rune(sr.indicators.VisibleTabStart)
	end := []rune(sr.indicators.VisibleTabEnd)
	fill := ' '
	if f := []rune(sr.indicators.VisibleTabFill); len(f) > 0 {
		fill = f[0]
	}

	// End: right-aligned, highest precedence — keep its rightmost runes.
	kEnd := len(end)
	if kEnd > width {
		kEnd = width
	}
	endShown := end[len(end)-kEnd:]

	// Start: left-aligned, next precedence — keep its leftmost runes.
	remaining := width - kEnd
	kStart := len(start)
	if kStart > remaining {
		kStart = remaining
	}
	startShown := start[:kStart]

	// Fill: whatever columns are left.
	fillCount := remaining - kStart

	out := make([]rune, 0, width)
	out = append(out, startShown...)
	for i := 0; i < fillCount; i++ {
		out = append(out, fill)
	}
	out = append(out, endShown...)
	return out
}

// prepareLineForDisplay prepares a line for display by handling UTF-8, control characters, tabs, etc.
// Translated directly from TypeScript viewport-renderer.js prepareLineForDisplay
func (sr *ScreenRenderer) prepareLineForDisplay(line, lineEnding string, width, viewOffsetX int, w *viewport.Viewport, docLine int, sel selectionRange, disp *lineDisplay, dispSyn []string) string {
	var displayLine strings.Builder

	// Link-as-button substitution: when disp is non-nil the walk below runs
	// over the substituted display runes. Selection is checked against the
	// original doc positions through disp.DispToDoc; chrome (button/shadow)
	// cells carry a forced color and take no selection tint or whitespace
	// markers. Syntax colors arrive display-aligned in dispSyn.
	if disp != nil {
		line = disp.Text
	}

	textColor := sr.col(w, "text")
	// Selection styling. Under flipBidiForHost a line carrying combining
	// marks cannot use a background/reverse selection bar — the host
	// terminal's bidi reorder miscounts the fill (codepoints vs cells) and
	// the bar drifts and half-vanishes on pointed RTL. Foreground + bold
	// ride each glyph intact, so those lines use the flip-safe selection
	// (see the selectionFlip colors). Mark-free lines (English; Arabic,
	// which mew pre-shapes to single presentation forms) keep the real bar.
	selName, selInvName := "selection", "selectionInvisibles"
	// The ride-safe selection is only needed when the marks are actually
	// EMITTED: with rtlCombining off they are suppressed below, so the line
	// carries codepoints == cells and the real bar works. (An LTR combining
	// mark under flip is a rarer case that keeps the bar here; rtlCombining
	// targets RTL scripts, whose marks are the ones this line strips.)
	if sr.frame.flipBidi && !w.ViewState.SuppressRTLCombining && lineHasZeroWidth(line) {
		selName, selInvName = "selectionFlip", "selectionInvisiblesFlip"
	}
	selectionColor := sr.col(w, selName)
	resetColor := sr.col(w, "reset")
	substitutesColor := sr.col(w, "special") // control char substitutes (^X / hex)
	truncatedColor := sr.col(w, "truncation")

	// Whitespace visualization. When enabled for this viewport, tabs and spaces
	// are drawn with visible marker glyphs in the invisibles color (or the
	// selection variant when the marker falls inside the selection).
	showInvisibles := w.ViewState.ShowInvisibles
	plainInvisiblesColor := sr.col(w, "invisibles")
	selectionInvisiblesColor := sr.col(w, selInvName)
	invisibleSpace := sr.indicators.VisibleSpace // marker for a space

	// showMarks: draw a "*" (in the "marks" color) at every mark / garland-
	// decoration position on this line, each in its own cell before the rune it
	// precedes. marksSet holds the marked rune positions; showMarks is cleared
	// when this line has none, so the per-slot check below stays cheap. On a
	// button-substituted line mark cells are suppressed: doc positions inside a
	// replaced span have no cells of their own (lineMarkSet gates identically).
	showMarks := w.ViewState.MarksVisible() && disp == nil
	var marksColor string
	var marksSet map[int]bool
	if showMarks && w.Buffer != nil {
		if cols := w.Buffer.MarksOnLine(docLine, w.ViewState.MarksShowInternal()); len(cols) > 0 {
			marksColor = sr.col(w, "marks")
			marksSet = make(map[int]bool, len(cols))
			for _, c := range cols {
				marksSet[c] = true
			}
		} else {
			showMarks = false
		}
	} else {
		showMarks = false
	}

	// Get Unicode runes. When showing invisibles, append the line's terminator
	// (stripped by the caller) so its \n / \r are drawn as end-of-line markers.
	runes := []rune(line)
	contentLen := len(runes)
	if showInvisibles && lineEnding != "" {
		runes = append(runes, []rune(lineEnding)...)
	}

	// Bidirectional layout: the walk below runs over VISUAL slots left to
	// right; logicalAt maps each slot to the logical rune it paints (identity
	// on pure-LTR lines, where layout is nil). rtlCell drives bracket mirroring.
	rtl := sr.winRTL(w)
	layout := sr.layoutFor(w, runes[:contentLen])
	termCount := len(runes) - contentLen
	contentSlots := contentLen
	if layout != nil {
		contentSlots = len(layout.Perm)
	}
	totalSlots := contentSlots + termCount
	// The terminator's marker cells belong at the line's END, and a line ends
	// wherever its direction runs out: the visual RIGHT under ltr, the visual
	// LEFT under rtl. Appending them to the slot order pins them to the right
	// in both, which under rtl put them at the line's reading START — reading
	// as though the terminator switched to LTR all by itself.
	//
	// Under rtl they take PADDING cells rather than content columns (see the
	// leftPad adjustment below), so the content — and every caret, ghost and
	// ruler position measured against it — lands exactly where it does with
	// invisibles off. rtlPadOffset omits them for the same reason.
	termFirst := rtl && termCount > 0
	logicalAt := func(slot int) int {
		if termFirst {
			if slot < termCount {
				return contentLen + slot
			}
			slot -= termCount
		}
		if layout == nil || slot >= contentSlots {
			if layout != nil {
				return contentLen + (slot - contentSlots)
			}
			return slot
		}
		return layout.Perm[slot]
	}
	rtlCell := func(li int) bool {
		return layout != nil && li >= 0 && li < len(layout.RTL) && layout.RTL[li]
	}

	// prevBase is the base character a combining mark at logical index li
	// would attach to: the nearest preceding rune that is not itself a mark
	// (a cluster may stack several marks on one base). 0 when the mark opens
	// the line and has nothing to anchor onto. Feeds textwidth.DefectiveMark.
	prevBase := func(li int) rune {
		for j := li - 1; j >= 0; j-- {
			if !textwidth.IsMark(runes[j]) {
				return runes[j]
			}
		}
		return 0
	}

	// Arabic cursive shaping lives on the layout (Layout.Glyph): each Arabic
	// letter is substituted with its contextual presentation form (computed
	// in LOGICAL order), and lam-alef pairs become one ligature glyph on the
	// lam with the alef absorbed — so joins survive our per-cell colouring and
	// RTL reversal. glyphFor returns the glyph to paint for a logical index;
	// LigatureAbsorbed means "no glyph, no cell".
	glyphFor := func(li int) rune {
		if layout != nil && layout.Glyph != nil && li >= 0 && li < len(layout.Glyph) {
			return layout.Glyph[li]
		}
		return runes[li]
	}

	// direction=rtl anchors the view to the RIGHT: the visible viewport in
	// visual columns is [vw-offset-width, vw-offset), so the line's reading
	// start (its rightmost visual cell) sits at the right edge and horizontal
	// scrolling advances through READING order — trimming the reading head
	// off the right as the tail is revealed on the left. When the viewport
	// extends past the line's left end the difference becomes left padding
	// (right alignment). Rewriting viewOffsetX to the viewport's left bound
	// lets the walk below run unchanged.
	// The phantom column (ViewOffsetX -1) opens one cell beyond the content's
	// leading edge for a caret that would otherwise sit off-screen. It is NOT
	// data, so it takes the plain text color wherever it is painted — never the
	// selection bar (see the padding at the end of this function).
	phantom := viewOffsetX < 0
	leftPad := 0
	if rtl {
		// CONTENT width only: the terminator markers ride in the padding, so
		// counting them here would shift the text a cell out of step with the
		// caret column, which never counts them either.
		vw := 0
		for slot := 0; slot < contentSlots; slot++ {
			li := slot
			if layout != nil {
				li = layout.Perm[slot]
			}
			// A marked slot draws its "*" cell before the rune (as in the loop
			// below), so the right-anchored width must count it too.
			if showMarks && li >= 0 && marksSet[li] {
				vw++
			}
			vw += sr.slotWidth(layout, runes, li, vw, w)
		}
		leftPad, viewOffsetX = rtlView(vw, viewOffsetX, width)
	}
	if termFirst {
		// Each terminator paints as a ONE-column marker (the two columns a raw
		// control character would take never apply here), plus a "*" cell for a
		// mark sitting at end of line.
		termCells := termCount
		for i := 0; showMarks && i < termCount; i++ {
			if marksSet[contentLen+i] {
				termCells++
			}
		}
		if leftPad >= termCells {
			leftPad -= termCells
		} else {
			// No padding to ride in: the line's visual end is off the left edge
			// (a full-width or horizontally scrolled line), and so are its
			// markers. Drop the slots rather than displace the text.
			termFirst = false
			totalSlots -= termCount
			termCount = 0
		}
	}

	// Build visual representation of the line with proper scrolling
	currentVisualColumn := 0
	documentRune := 0
	outputVisualColumn := 0
	// The phantom column (ViewOffsetX -1): the caret follow scrolled ONE past
	// the content's leading edge, because a caret's insertion point fell one
	// cell left of visual column 0 (the reading end of an RTL fragment
	// starting the line — see ensureCursorVisibleHorizontal). Paint a plain
	// space in the first cell for the caret to occupy and run the walk from a
	// zero offset, shifting the whole line right one. Under direction=rtl the
	// same offset flows through rtlView above instead, shortening the pad so
	// the phantom cell opens at the RIGHT edge.
	if viewOffsetX < 0 && !rtl {
		displayLine.WriteString(textColor + " ")
		outputVisualColumn++
		viewOffsetX = 0
	}
	if leftPad > 0 {
		// rtlView returns an unclamped pad (positioners need the true off-screen
		// distance); when the content is scrolled fully off the right, cap the
		// painted padding at the content width so we never overflow the row.
		paintPad := leftPad
		if paintPad > width {
			paintPad = width
		}
		displayLine.WriteString(textColor + strings.Repeat(" ", paintPad))
		outputVisualColumn = paintPad
	}
	// RTL: content trimmed off the LEFT edge is the line's reading tail
	// continuing — mark it per line (the mirror of LTR's right-edge marker).
	// The marker takes the first content cell; advancing the viewport start by
	// one keeps every remaining cell on its screen column.
	if rtl && viewOffsetX > 0 {
		displayLine.WriteString(truncatedColor + sr.indicators.TruncationLeft + textColor)
		outputVisualColumn++
		viewOffsetX++
	}
	// Byte offset in displayLine just before the last visible cell was written,
	// so the truncation indicator can drop that whole cell (including its color
	// escapes) cleanly rather than chopping the last rune mid-escape — and the
	// output column where that cell started, so replacing a wide cell with the
	// 1-column marker can pad the difference.
	lastCellStart := 0
	lastCellColumn := 0

	// markSlotDrawn is the documentRune (visual slot) whose leading mark "*" has
	// already been emitted, so re-entering the same slot after emitting it draws
	// the character rather than another "*".
	markSlotDrawn := -1

	// Helper to check if a rune position is within the selection. On a
	// substituted line the walk's positions are display indices: map back to
	// the doc rune the cell came from; chrome cells (-1) are never selected.
	// selCovers reports whether the selection covers doc rune p on this line.
	var selCovers func(p int) bool
	isSelected := func(runePos int) bool {
		if disp != nil {
			if runePos < 0 || runePos >= len(disp.DispToDoc) {
				return false
			}
			if d := disp.DispToDoc[runePos]; d < 0 {
				// A chrome cell (a button's caps, shadow, isolates) has no doc
				// rune of its own, so it answers for the span it belongs to:
				// selected once the selection reaches that span's doc range.
				// The button's cells are ALL chrome, so it colours as a unit —
				// there is no per-cell doc position to split it on.
				if runePos < len(disp.SelSpan) {
					r := disp.SelSpan[runePos]
					for p := r[0]; p < r[1]; p++ {
						if selCovers(p) {
							return true
						}
					}
				}
				return false
			} else {
				runePos = d
			}
		}
		return selCovers(runePos)
	}
	selCovers = func(runePos int) bool {
		if !sel.exists || runePos < 0 {
			return false
		}
		// Single line selection
		if docLine == sel.startLine && docLine == sel.endLine {
			return runePos >= sel.startRune && runePos < sel.endRune
		}
		// Multi-line selection
		if docLine == sel.startLine {
			return runePos >= sel.startRune
		}
		if docLine == sel.endLine {
			return runePos < sel.endRune
		}
		// Line is fully within selection
		return docLine > sel.startLine && docLine < sel.endLine
	}

	// Syntax highlighting: per-rune SGR colors for this document line (nil
	// when no grammar applies). Selection still wins over syntax color. On a
	// substituted line the display-aligned dispSyn replaces the doc-aligned
	// colorizer output.
	var synColors []string
	if disp != nil {
		synColors = dispSyn
	} else if sr.syntaxColorizer != nil {
		synColors = sr.syntaxColorizer(w, docLine)
	}

	// Get the appropriate base color for a rune position. A chrome cell's
	// forced color wins outright — buttons keep their look through selections.
	getBaseColor := func(runePos int) string {
		forced := ""
		if disp != nil && runePos >= 0 && runePos < len(disp.Forced) {
			forced = disp.Forced[runePos]
		}
		if isSelected(runePos) {
			// Selection outranks a forced colour: a selected heading shows the
			// selection bar rather than hiding it behind the heading colour.
			// A button says so explicitly instead — its own selected scheme,
			// per cell, so a partly-selected button splits at the boundary.
			if disp != nil && runePos >= 0 && runePos < len(disp.ForcedSel) &&
				disp.ForcedSel[runePos] != "" {
				return disp.ForcedSel[runePos]
			}
			return selectionColor
		}
		if forced != "" {
			return forced
		}
		if runePos >= 0 && runePos < len(synColors) && synColors[runePos] != "" {
			return synColors[runePos]
		}
		return textColor
	}

	// Get the invisibles-marker color for a rune position: selected whitespace
	// markers use the selectionInvisibles variant.
	getInvisiblesColor := func(runePos int) string {
		if isSelected(runePos) {
			return selectionInvisiblesColor
		}
		return plainInvisiblesColor
	}

	// isChrome reports whether a display cell belongs to a button (forced
	// color): chrome cells never take whitespace markers — a space inside a
	// button cap or title stays a plain space.
	isChrome := func(runePos int) bool {
		return disp != nil && runePos >= 0 && runePos < len(disp.Forced) && disp.Forced[runePos] != ""
	}

	// Process each visual slot of the line (logical order on plain lines).
	for documentRune < totalSlots && outputVisualColumn < width {
		logicalIdx := logicalAt(documentRune)

		// Direction-marker slot (showBidi): a one-column glyph at a fragment's
		// leading edge ("<" / ">") or its reading end ("|"), in the
		// control-character color.
		if logicalIdx < 0 {
			glyph := ">"
			switch logicalIdx {
			case bidi.MarkerRTL:
				glyph = "<"
			case bidi.MarkerEnd:
				glyph = "|"
			}
			if currentVisualColumn >= viewOffsetX && outputVisualColumn < width {
				lastCellStart = displayLine.Len()
				lastCellColumn = outputVisualColumn
				displayLine.WriteString(substitutesColor + glyph + textColor)
				outputVisualColumn++
			}
			currentVisualColumn++
			documentRune++
			continue
		}

		// showMarks: a mark at this logical position gets its own "*" cell,
		// emitted before the character it precedes. documentRune is NOT advanced
		// (this is an inserted cell, not a text slot); markSlotDrawn prevents a
		// second "*" when the loop re-enters the slot to draw the character.
		if showMarks && marksSet[logicalIdx] && markSlotDrawn != documentRune {
			if currentVisualColumn >= viewOffsetX && outputVisualColumn < width {
				lastCellStart = displayLine.Len()
				lastCellColumn = outputVisualColumn
				displayLine.WriteString(marksColor + "*" + textColor)
				outputVisualColumn++
			}
			currentVisualColumn++
			markSlotDrawn = documentRune
			continue
		}

		r := runes[logicalIdx]
		runeDisplay := ""
		runeVisualWidth := 0
		baseColor := getBaseColor(logicalIdx)
		invisiblesColor := getInvisiblesColor(logicalIdx)
		var tabMarker []rune // non-nil only for a tab when invisibles are shown

		// An explicit direction-control character under showBidi renders as
		// its own one-column marker (it IS the direction change).
		if layout != nil && layout.Marked && bidi.IsDirectionControl(r) {
			glyph := ">"
			if rtlCell(logicalIdx) {
				glyph = "<"
			}
			if currentVisualColumn >= viewOffsetX && outputVisualColumn < width {
				lastCellStart = displayLine.Len()
				lastCellColumn = outputVisualColumn
				displayLine.WriteString(substitutesColor + glyph + baseColor)
				outputVisualColumn++
			}
			currentVisualColumn++
			documentRune++
			continue
		}

		// Outside showBidi, a direction control is layout INPUT, never
		// output: mew's own bidi pass already consumed it and the emitted
		// stream is in visual order, so writing the raw control would invite
		// a bidi-aware terminal to reorder that stream a second time (and
		// even inert terminals may draw a stray glyph for a lone format
		// character). It occupies no cell and emits no bytes. This also keeps
		// the FSI/PDI isolates injected around browse-mode buttons out of the
		// terminal byte stream entirely.
		if bidi.IsDirectionControl(r) {
			documentRune++
			continue
		}

		// Calculate what this rune should look like and its visual width
		if r == '\t' {
			// Tab - calculate width to next tab stop
			tabWidth := sr.getTabWidth(currentVisualColumn, w)
			if showInvisibles {
				// Tab drawn as start/fill/end markers spanning the tab area.
				tabMarker = sr.composeTabMarker(tabWidth)
				runeDisplay = invisiblesColor + string(tabMarker) + baseColor
			} else {
				runeDisplay = strings.Repeat(" ", tabWidth)
			}
			runeVisualWidth = tabWidth
		} else if showInvisibles && r == ' ' && !isChrome(logicalIdx) {
			// Space shown as a marker glyph (never inside button chrome).
			runeDisplay = invisiblesColor + invisibleSpace + baseColor
			runeVisualWidth = 1
		} else if showInvisibles && r == '\n' {
			// Line feed marker (appended terminator).
			runeDisplay = invisiblesColor + sr.indicators.VisibleNewline + baseColor
			runeVisualWidth = 1
		} else if showInvisibles && r == '\r' {
			// Carriage return marker (appended terminator).
			runeDisplay = invisiblesColor + sr.indicators.VisibleReturn + baseColor
			runeVisualWidth = 1
		} else if textwidth.IsControl(r) {
			// Control characters as ^X / hex format — the C0 set, DEL, and
			// the C1 set U+0080..U+009F, which arrives as ordinary UTF-8 but
			// carries the terminal's string introducers (see
			// textwidth.IsControl).
			runeDisplay = substitutesColor + runeToHexOrCtrl(r) + baseColor
			runeVisualWidth = substituteWidth(runeToHexOrCtrl(r))
		} else if base := prevBase(logicalIdx); textwidth.DefectiveMark(base, r) {
			// An ill-formed combining mark — no base to anchor onto, or a
			// script-specific mark riding a base of another script. It is
			// corruption, not text, and it CANNOT be painted as zero-width:
			// a terminal whose shaper rejects the pairing falls back to a
			// SPACING glyph (.notdef, or dotted-circle + mark) that advances
			// a column mew never budgeted, sliding the rest of the row right
			// and bleeding it past the window edge. So it is classified like
			// a control code: a definite-width hex substitute, in the
			// substitutes color, that both mew and the terminal measure the
			// same. See textwidth.DefectiveMark.
			form, fw := defectiveMarkForm(base, r)
			runeDisplay = substitutesColor + form + baseColor
			runeVisualWidth = fw
		} else {
			// Regular UTF-8 rune: terminal cell width (0 for combining and
			// zero-width characters, 2 for wide CJK/emoji, 1 otherwise). A
			// bracket inside a right-to-left run paints as its mirrored
			// counterpart (UAX #9 L4). Arabic letters paint as their shaped
			// presentation form (same cell width as the base letter); the
			// absorbed half of a lam-alef ligature takes no cell.
			g := glyphFor(logicalIdx)
			if g == bidi.LigatureAbsorbed {
				runeVisualWidth = 0
				runeDisplay = ""
			} else {
				runeVisualWidth = textwidth.Rune(r)
				if rtlCell(logicalIdx) {
					runeDisplay = string(bidi.Mirror(g))
				} else {
					runeDisplay = string(g)
				}
			}
		}

		// Zero-width rune (combining mark, ZWJ, ...): it occupies no cell of
		// its own — the terminal draws it into the preceding cell. Emit it
		// only when that cell was actually output, so a mark whose base is
		// scrolled off or truncated is dropped along with its base.
		if runeVisualWidth == 0 {
			// rtlCombining=off suppresses combining marks (Mn/Mc/Me — niqqud,
			// harakat) that ride an RTL letter, so pointed RTL renders like
			// mew's pre-shaped Arabic (one codepoint per cell). The mark is
			// only dropped from DISPLAY; it stays in the buffer (the caret
			// still walks it, the modebar still names it). ZWJ / bidi controls
			// (not mark categories) and LTR combining are never suppressed.
			suppress := w.ViewState.SuppressRTLCombining && rtlCell(logicalIdx) &&
				unicode.In(r, unicode.Mn, unicode.Mc, unicode.Me)
			if !suppress && currentVisualColumn > viewOffsetX && outputVisualColumn > 0 {
				displayLine.WriteString(runeDisplay)
			}
			documentRune++
			continue
		}

		// Handle scrolling - check if this rune (or part of it) should be visible
		if currentVisualColumn+runeVisualWidth > viewOffsetX {
			lastCellStart = displayLine.Len() // start of this (visible) cell's output
			lastCellColumn = outputVisualColumn
			// This rune is at least partially visible
			startOffset := 0
			if currentVisualColumn < viewOffsetX {
				startOffset = viewOffsetX - currentVisualColumn
			}
			availableWidth := runeVisualWidth - startOffset
			if availableWidth > width-outputVisualColumn {
				availableWidth = width - outputVisualColumn
			}

			if availableWidth > 0 && runeVisualWidth > 0 {
				if startOffset == 0 {
					// Rune is fully visible from the start
					if runeVisualWidth <= width-outputVisualColumn {
						// Rune fits completely
						displayLine.WriteString(baseColor)
						displayLine.WriteString(runeDisplay)
						outputVisualColumn += runeVisualWidth
					} else {
						// Rune is truncated on the right - show what we can
						if r == '\t' {
							visibleTabWidth := width - outputVisualColumn
							displayLine.WriteString(baseColor)
							if tabMarker != nil && visibleTabWidth <= len(tabMarker) {
								// Left part of the tab marker (end is off-edge).
								displayLine.WriteString(invisiblesColor + string(tabMarker[:visibleTabWidth]) + baseColor)
							} else {
								displayLine.WriteString(strings.Repeat(" ", visibleTabWidth))
							}
							outputVisualColumn = width
						} else if r < 0x20 || r == 0x7F {
							// Control character - show what we can
							hexStr := runeToHexOrCtrl(r)
							visibleWidth := width - outputVisualColumn
							displayLine.WriteString(substitutesColor)
							if visibleWidth >= 2 {
								displayLine.WriteString(hexStr)
							} else if visibleWidth == 1 {
								displayLine.WriteString(string(hexStr[0]))
							}
							displayLine.WriteString(baseColor)
							outputVisualColumn = width
						} else {
							// A wide rune with only one column left at the
							// right edge: a placeholder space keeps the row
							// from overflowing (the truncation indicator
							// replaces this cell anyway).
							displayLine.WriteString(baseColor)
							displayLine.WriteString(strings.Repeat(" ", width-outputVisualColumn))
							outputVisualColumn = width
						}
					}
				} else {
					// Rune is partially scrolled off the left
					if r == '\t' {
						// Partial tab (scrolled off the left by startOffset).
						remainingTabWidth := runeVisualWidth - startOffset
						actualWidth := remainingTabWidth
						if actualWidth > width-outputVisualColumn {
							actualWidth = width - outputVisualColumn
						}
						displayLine.WriteString(baseColor)
						if tabMarker != nil && startOffset+actualWidth <= len(tabMarker) {
							// Show the visible columns of the tab marker.
							displayLine.WriteString(invisiblesColor + string(tabMarker[startOffset:startOffset+actualWidth]) + baseColor)
						} else {
							displayLine.WriteString(strings.Repeat(" ", actualWidth))
						}
						outputVisualColumn += actualWidth
					} else if r < 0x20 || r == 0x7F {
						// Partial control character
						hexStr := runeToHexOrCtrl(r)
						if startOffset == 1 && availableWidth >= 1 {
							// Show second character of ^X
							displayLine.WriteString(substitutesColor)
							displayLine.WriteString(string(hexStr[1]))
							displayLine.WriteString(baseColor)
							outputVisualColumn++
						} else {
							// Show "?" for any other partial case
							displayLine.WriteString(substitutesColor)
							displayLine.WriteString("?")
							displayLine.WriteString(baseColor)
							outputVisualColumn++
						}
					} else {
						// A wide rune half scrolled off the left edge: its
						// remaining column shows a placeholder.
						displayLine.WriteString(baseColor)
						displayLine.WriteString("?")
						outputVisualColumn++
					}
				}
			}
		}

		currentVisualColumn += runeVisualWidth
		documentRune++
	}

	// Zero-width runes immediately after the loop's end belong to the last
	// emitted cell (a combining mark on the final visible character): append
	// them so the base keeps its mark, and so they do not read as truncated
	// content below.
	for documentRune < totalSlots && outputVisualColumn > 0 {
		li := logicalAt(documentRune)
		if li < 0 {
			break
		}
		r := runes[li]
		// Only a genuine zero-width RIDER may be appended here — this runs
		// past the row's width budget, so anything that would take cells of
		// its own stops the flush instead. That excludes controls (C1 included:
		// its terminal width is 0, so the width test alone would wave a string
		// introducer straight through onto the wire) and defective marks,
		// which are painted as sized substitutes, not as riders.
		if r == '\t' || textwidth.IsControl(r) || textwidth.Rune(r) != 0 ||
			textwidth.DefectiveMark(prevBase(li), r) ||
			(layout != nil && layout.Marked && bidi.IsDirectionControl(r)) {
			break
		}
		displayLine.WriteString(string(r))
		documentRune++
	}

	// showMarks: a mark at end of line has no rune to precede. With invisibles
	// on it rides the terminator marker (drawn in the loop above); with them off
	// there is no terminator slot, so on a plain line append its "*" as a
	// trailing cell — otherwise the final mark on the line would vanish. (Bidi
	// lines keep riding the terminator, since "after the content" is ambiguous
	// under reordering; lineMarkSet gates the caret math to match.)
	if showMarks && !showInvisibles && layout == nil && marksSet[len(runes)] &&
		documentRune >= totalSlots && outputVisualColumn < width {
		lastCellStart = displayLine.Len()
		lastCellColumn = outputVisualColumn
		displayLine.WriteString(marksColor + "*" + textColor)
		outputVisualColumn++
	}

	// Right-edge truncation indicator: when content continues past the right
	// edge, replace the last visible cell (dropped whole, escapes and all) with
	// the configured marker. Slicing at the recorded cell boundary avoids
	// breaking a trailing ANSI escape. (Left truncation is not per-line — a
	// scrolled view truncates every line, so that indicator lives once on the
	// ruler instead.)
	if width > 0 && outputVisualColumn >= width && documentRune < totalSlots && !rtl {
		full := displayLine.String()
		if lastCellStart >= 0 && lastCellStart <= len(full) {
			// The dropped cell may be wider than the 1-column marker (a wide
			// rune, a tab): pad the difference so the marker stays on the
			// right edge and the row keeps its exact width.
			pad := ""
			if extra := width - lastCellColumn - 1; extra > 0 {
				pad = strings.Repeat(" ", extra)
			}
			return textColor + full[:lastCellStart] + pad + truncatedColor + sr.indicators.TruncationRight + textColor + resetColor
		}
	}

	// Pad line to fill remaining width. Normally padding is plain text color,
	// but when this line's trailing newline is itself within the selection
	// (i.e. a start/middle line of a multi-line selection), extend the
	// highlight across the padding so the selected newline reads as selected.
	// A single-line selection or the selection's end line stops at its last
	// selected rune and must not bleed into the padding.
	if outputVisualColumn < width {
		padWidth := width - outputVisualColumn
		padColor := textColor
		if sel.exists && docLine >= sel.startLine && docLine < sel.endLine {
			padColor = selectionColor
		}
		// Under rtl the phantom column opens at the RIGHT edge, which is this
		// padding's last cell. It holds no data, so highlighting it would show
		// a selected space that does not exist in the line. (The ltr phantom is
		// written above, already in the plain text color.)
		if phantom && rtl && padColor != textColor && padWidth > 0 {
			if padWidth > 1 {
				displayLine.WriteString(padColor + strings.Repeat(" ", padWidth-1))
			}
			displayLine.WriteString(textColor + " ")
			padWidth = 0
		}
		if padWidth > 0 {
			displayLine.WriteString(padColor + strings.Repeat(" ", padWidth))
		}
	}

	return textColor + displayLine.String() + resetColor
}

// getTabWidth calculates the width of a tab at the given visual column.
func (sr *ScreenRenderer) getTabWidth(visualColumn int, w *viewport.Viewport) int {
	tabSize := sr.getTabSize(w)
	return tabSize - (visualColumn % tabSize)
}

// runeToHexOrCtrl converts a control character to ^X format or hex.
// Directly translated from TypeScript runeToHexOrCtrl
func runeToHexOrCtrl(r rune) string {
	value := int(r)
	if value <= 27 {
		switch value {
		case 0:
			return "^@"
		case 27:
			return "^["
		default:
			return "^" + string(rune(value+64))
		}
	} else if value <= 0xFF {
		// One byte of hex reads unambiguously on its own: FE.
		return fmt.Sprintf("%02X", value)
	}
	// Past one byte the digits need a boundary, or a run of substituted
	// codepoints reads as one long number: (0123). Wider planes keep whole
	// byte pairs.
	if value <= 0xFFFF {
		return fmt.Sprintf("(%04X)", value)
	}
	return fmt.Sprintf("(%06X)", value)
}

// substituteWidth is the column count of a substitute string — always plain
// ASCII, one column per rune. The renderer measures every substitute this way
// so the width model and the painted glyphs can never disagree.
func substituteWidth(s string) int { return len([]rune(s)) }

// defectiveMarkForm returns the text and column width mew paints for a
// combining mark it must not emit as zero-width (see textwidth.DefectiveMark),
// given the cluster base the mark would have attached to.
//
// A mark with no base that mew can draw is ANCHORED: a dotted circle supplies
// the missing base and the mark composes onto it — the Unicode convention for
// showing an isolated mark — so the reader sees the actual mark in one cell.
// Everything else falls back to the hex codepoint, which needs no glyph and no
// shaping at all.
//
// This is the single place the decision is made; both the paint and the width
// model call it, so they cannot drift.
func defectiveMarkForm(prev, r rune) (string, int) {
	if textwidth.AnchorMark(prev, r) {
		// The circle takes a cell and the mark rides it — at ZERO width for a
		// non-spacing mark (Mn/Me), but Mc marks are SPACING combining marks
		// and take a cell of their own, so the pair's width is the circle plus
		// whatever the mark itself advances.
		return string(textwidth.MarkAnchor) + string(r), 1 + textwidth.Rune(r)
	}
	s := runeToHexOrCtrl(r)
	return s, substituteWidth(s)
}

// renderPeekIndicators renders peek indicators for scrolled dock viewports.
// These hints live at the viewport-manager level, not inside any viewport, so
// their colors resolve at the global level only (no class/type cascade).
func (sr *ScreenRenderer) renderPeekIndicators(layout viewport.Layout) {
	hintColor := sr.colorScheme.Resolve("", "", "hint")
	resetColor := sr.colorScheme.Resolve("", "", "reset")
	// Right-align the (configurable, variable-width) label near the right edge.
	// Labels run through the modebar %CODE% engine first (e.g. "[%SPU%]" ->
	// the live stat_peek_up binding).
	draw := func(label string, y int) {
		if sr.peekLabelFn != nil {
			label = sr.peekLabelFn(label)
		}
		if label == "" {
			return
		}
		sr.MoveCursor(sr.Width-len([]rune(label)), y)
		sr.Write(hintColor + label + resetColor)
	}

	// Top pair: Esc U one line below the top edge (so it doesn't cover the
	// modebar), Esc V on the last row of the top dock. When both are active
	// and would land on the same row, Esc V takes priority.
	if len(layout.TopLayout) > 0 {
		upY := 2
		lastTop := layout.TopLayout[len(layout.TopLayout)-1]
		downY := lastTop.Y + lastTop.Height
		if sr.peekIndicators.StatPeekUp && !(sr.peekIndicators.StatPeekDown && downY <= upY) {
			draw(sr.indicators.StatPeekUp, upY)
		}
		if sr.peekIndicators.StatPeekDown {
			draw(sr.indicators.StatPeekDown, downY)
		}
	}

	// Bottom pair: Esc P on the first row of the bottom dock, Esc N on the
	// last screen row. When both are active and would land on the same row,
	// Esc P takes priority.
	if len(layout.BottomLayout) > 0 {
		firstBottom := layout.BottomLayout[0]
		upY := firstBottom.Y + 1
		downY := sr.Height
		if sr.peekIndicators.PromptPeekDown && !(sr.peekIndicators.PromptPeekUp && upY >= downY) {
			draw(sr.indicators.PromptPeekDown, downY)
		}
		if sr.peekIndicators.PromptPeekUp {
			draw(sr.indicators.PromptPeekUp, upY)
		}
	}
}

// positionCursor positions the terminal cursor within a viewport. It returns true
// if the hardware cursor should be hidden for this frame (used for the
// right-edge off-screen "@", so the block cursor doesn't obscure the marker).
// caretLineVisible reports whether the caret's document line is within the
// viewport's visible content rows. It is false when a free scroll (mouse wheel
// or scroll_* command) has parked the viewport away from a stationary caret,
// leaving the caret above or below the content area. ContentHeight already
// excludes the ruler and message bars, so this is the exact visible span.
func caretLineVisible(w *viewport.Viewport) bool {
	if w.ContentHeight <= 0 {
		return true // geometry not established yet: don't hide
	}
	rel := w.CursorPos().Line - w.ViewState.ViewOffsetY
	return rel >= 0 && rel < w.ContentHeight
}

func (sr *ScreenRenderer) positionCursor(w *viewport.Viewport, layout *viewport.ViewportLayout) bool {
	// A free scroll can leave the caret above or below the visible content
	// (its line scrolled out of view). Park the hardware cursor at home and
	// hide it rather than placing it on an unrelated row.
	if !caretLineVisible(w) {
		sr.MoveCursor(1, 1)
		return true
	}

	// Adjust for the column ruler and top message bar
	topOffset := 0
	if rulerActive(w, layout.Height) {
		topOffset++
	}
	if w.MessageTopInner != "" || w.MessageTopCenter != "" || w.MessageTopOuter != "" {
		topOffset++
	}

	// Convert cursor rune position to visual column — the caret-biased column
	// (an RTL caret parks at its rune's right edge), plus the right-alignment
	// pad of this line under direction=rtl. The geometry (base, content width,
	// scroll offset) comes from caretRowGeom in the caret line's cell space, so
	// a double-width caret line — one-cell gutter, half-width content, halved
	// scroll — is placed correctly with no special-casing here.
	rtl := sr.winRTL(w)
	line := ""
	visualColumn := w.CursorPos().Rune
	caretRune := w.CursorPos().Rune
	if w.Buffer != nil {
		raw := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
		// Browse-mode buttons: measure against the substituted display line,
		// with the caret mapped onto it (inside a button it parks on the
		// button's first cell). Identity when no buttons apply.
		var curRune int
		line, curRune = sr.displayCaretLine(w, raw, w.CursorPos().Rune)
		caretRune = curRune
		visualColumn = sr.caretVisualColumn(line, curRune, w)
	}
	base, pad, viewOff, contentWidth, _ := sr.caretRowGeom(w, line)

	screenY := layout.Y + 1 + topOffset + (w.CursorPos().Line - w.ViewState.ViewOffsetY)
	screenX := base + pad + (visualColumn - viewOff)

	// One column past the content's right edge has no content cell of its own.
	// Whether the caret may sit there depends on which side of its character
	// the insertion point is:
	//
	//   - On an LTR character the insertion point is to its RIGHT, so a caret
	//     after the last Latin letter of an RTL line belongs in that column.
	//     Under direction=rtl the mirrored gutter is exactly there, so the
	//     caret dips one column into it (caretGutterSlack) instead of falling
	//     back onto the letter it just passed — which read as being BEFORE it.
	//   - On an RTL character — the reading start, where the caret marks the
	//     cell the NEXT character will occupy rather than a position after one
	//     — there is nothing to its right, so it still clamps onto the edge
	//     cell, as it always has.
	//
	// With no gutter to dip into, the clamp stands either way rather than
	// addressing a column outside the viewport.
	caretRTL := sr.caretRunRTL(w, line, caretRune)
	gutterSlack := sr.caretGutterSlack(w)
	rightMost := base + contentWidth - 1
	if gutterSlack > 0 {
		rightMost++ // the caret may occupy at most the first gutter column
	}
	if rtl && contentWidth > 0 && screenX == base+contentWidth {
		if caretRTL || gutterSlack <= 0 {
			screenX = base + contentWidth - 1
		}
	}

	// When the cursor is horizontally scrolled off-screen (e.g. after a manual
	// scroll_left/right, or vertical navigation with the ghost column parked past
	// the edge), show an "@" indicator (in the ghost-cursor color) at the near
	// edge. On the LEFT edge, park the terminal cursor one column left of the "@"
	// (clamped to column 1) so the block cursor sits beside it rather than
	// obscuring it; on the RIGHT edge, park directly on the "@".
	if contentWidth > 0 && (screenX < base || screenX > rightMost) {
		indicatorColor := sr.col(w, "cursorOffScreen")
		isRight := screenX > base+contentWidth-1
		edgeX := base
		if isRight {
			edgeX = base + contentWidth - 1
		}
		sr.MoveCursor(edgeX, screenY)
		sr.Write(indicatorColor + sr.indicators.CursorOffScreen + sr.col(w, "reset"))
		parkX := edgeX
		if !isRight {
			parkX = edgeX - 1
			if parkX < 1 {
				parkX = 1
			}
		}
		sr.MoveCursor(parkX, screenY)
		// On the right edge, hide the hardware cursor so it doesn't obscure the
		// "@". On the left edge it sits beside the marker, so keep it visible.
		return isRight
	}

	// A BAR cursor draws on the LEFT edge of the cell it is addressed to; a
	// block or underline fills that cell. Every column computed above is the
	// block/underline one — the cell holding the character the caret just
	// passed — which for left-to-right text is also where a bar belongs (its
	// left edge is that character's right edge). In an RTL run the character's
	// right edge is the NEXT cell to the right, so a bar must be addressed one
	// cell further right or it draws on the far side of the character.
	if sr.barCursorOnRTL(w, line, caretRune) {
		screenX++
		// The nudge obeys the same right bound as everything above: one column
		// into the gutter when there is one, otherwise the content's edge.
		if contentWidth > 0 && screenX > rightMost {
			screenX = rightMost
		}
		if screenX > sr.Width {
			screenX = sr.Width
		}
	}

	sr.MoveCursor(screenX, screenY)
	return false
}

// caretRunRTL reports whether the caret sits on a right-to-left character.
// bidi.RTLAt is the same predicate that flips the modebar logo from M_ to _M,
// so every caret-side decision below agrees with what the logo is showing.
func (sr *ScreenRenderer) caretRunRTL(w *viewport.Viewport, line string, runePos int) bool {
	return bidi.RTLAt([]rune(line), runePos, sr.winRTL(w))
}

// barCursorOnRTL reports whether the caret's shape is a bar (DECSCUSR 5 or 6)
// AND it sits on a right-to-left character — the one combination whose cell
// address differs from the block/underline geometry the caret math produces.
func (sr *ScreenRenderer) barCursorOnRTL(w *viewport.Viewport, line string, runePos int) bool {
	if sr.cursorStyleFn == nil || w == nil {
		return false
	}
	if s := sr.cursorStyleFn(w); s != 5 && s != 6 {
		return false
	}
	return sr.caretRunRTL(w, line, runePos)
}

// caretGutterSlack is how many columns lie to the RIGHT of the caret line's
// content area: the line-number gutter, which caretRowGeom mirrors to that side
// under direction=rtl. Zero otherwise — an LTR gutter sits on the left, and
// there is nothing but margin to the right of the content.
//
// It is the room the caret has to sit ONE column past the content's right edge,
// which is where an insertion point after the rightmost character belongs.
func (sr *ScreenRenderer) caretGutterSlack(w *viewport.Viewport) int {
	if w == nil || !sr.winRTL(w) || !w.ViewState.ShowLineNumbers {
		return 0
	}
	gutter := w.LineNumWidth
	if sr.caretLineDoubleWide(w) {
		gutter /= 2 // matches caretRowGeom's halving on a doubled row
	}
	if gutter < 0 {
		return 0
	}
	return gutter
}

// visualColIsWideTrail reports whether a left-based visual column on the
// display line falls on the TRAILING cell of a double-width glyph — a cell
// that cannot be individually overstruck (the glyph lights up as a whole).
func (sr *ScreenRenderer) visualColIsWideTrail(w *viewport.Viewport, line string, col int) bool {
	runes := []rune(line)
	if len(runes) == 0 || col <= 0 {
		return false
	}
	marked := sr.lineMarkSet(w, runes)
	if layout := sr.layoutFor(w, runes); layout != nil {
		cols, _ := sr.bidiColumns(runes, layout, marked, w)
		for i := range runes {
			if sr.slotWidth(layout, runes, i, cols[i], w) == 2 && col == cols[i]+1 {
				return true
			}
		}
		return false
	}
	vw := 0
	for i, r := range runes {
		if marked[i] {
			vw++
		}
		wd := sr.getRuneVisualWidth(r, vw, w)
		if wd == 2 && col == vw+1 {
			return true
		}
		vw += wd
	}
	return false
}

// renderGhostCursor renders the ghost cursor indicator at the ideal visual column.
// The ghost cursor shows where the cursor "wants" to be on a shorter line.
func (sr *ScreenRenderer) renderGhostCursor(w *viewport.Viewport, layout *viewport.ViewportLayout) {
	if !w.HasGhostCursor || w.GhostCursorVisualColumn <= 0 {
		return
	}
	if !caretLineVisible(w) {
		return // caret line scrolled out of view (free scroll): no ghost
	}

	// Adjust for the column ruler and top message bar
	topOffset := 0
	if rulerActive(w, layout.Height) {
		topOffset++
	}
	if w.MessageTopInner != "" || w.MessageTopCenter != "" || w.MessageTopOuter != "" {
		topOffset++
	}

	// Ghost cursor is at the ideal visual column (shifted by the line's
	// right-alignment pad under direction=rtl, where the stored column is a
	// READING column — distance back from the reading start — that maps to the
	// left-based visual column vw-reading before positioning). Geometry (base,
	// content width, scroll) is in the caret line's cell space via caretRowGeom.
	rtl := sr.winRTL(w)
	ghostVisualColumn := w.GhostCursorVisualColumn
	line := ""
	if w.Buffer != nil {
		raw := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
		line, _ = sr.displayCaretLine(w, raw, 0) // browse-mode buttons: display geometry
		if rtl {
			ghostVisualColumn = sr.lineVisualWidth(w, line) - w.GhostCursorVisualColumn
		}
	}
	ghostBase, pad, viewOff, contentWidth, _ := sr.caretRowGeom(w, line)

	screenY := layout.Y + 1 + topOffset + (w.CursorPos().Line - w.ViewState.ViewOffsetY)
	screenX := ghostBase + pad + (ghostVisualColumn - viewOff)

	// Check if ghost cursor is visible on screen. The last visible content cell
	// is at column base+contentWidth-1, so a ghost at base+contentWidth would
	// land in the right margin and must be hidden.
	if screenX < ghostBase || screenX >= ghostBase+contentWidth {
		return // Ghost cursor is outside visible area
	}

	// A ghost landing on the TRAILING half of a double-width glyph draws
	// nothing: the glyph occupies both cells, and half of it cannot be
	// overstruck — writing there just corrupts the character.
	if sr.visualColIsWideTrail(w, line, ghostVisualColumn) {
		return
	}

	// Render ghost cursor as '|' with ghost cursor color
	ghostCursorColor := sr.col(w, "cursorGhost")
	resetColor := sr.col(w, "reset")

	sr.MoveCursor(screenX, screenY)
	sr.Write(ghostCursorColor + sr.indicators.CursorGhost + resetColor)
}

// CursorColumns returns the 1-based screen columns of the caret and its
// companion cursors — the ghost cursor and the secondary bidi cursor — for w,
// limited to those currently within the visible content area. The ruler uses
// this to mark the cursor's column(s) when rulerShowsCursor is enabled. The
// screen-X math mirrors positionCursor / renderGhostCursor / the secondary
// cursor exactly, so the highlighted ruler cells line up with the caret.
func (sr *ScreenRenderer) CursorColumns(w *viewport.Viewport) []int {
	if w == nil || w.Buffer == nil {
		return nil
	}
	rtl := sr.winRTL(w)
	line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
	line, curRune := sr.displayCaretLine(w, line, w.CursorPos().Rune) // browse-mode buttons
	base, pad, viewOff, contentWidth, dw := sr.caretRowGeom(w, line)
	if contentWidth <= 0 {
		return nil
	}

	var cols []int
	// The caret columns are computed in the caret line's cell space; the ruler
	// is painted on a NORMAL-width row, so a cell column on a double-width caret
	// line is mapped to its physical position before being recorded.
	add := func(cellX int) {
		if cellX < base || cellX > base+contentWidth-1 {
			return
		}
		screenX := cellToPhysical(cellX, dw)
		for _, c := range cols {
			if c == screenX {
				return
			}
		}
		cols = append(cols, screenX)
	}

	// Regular caret (caret-biased column, with the RTL right-edge clamp).
	caretX := base + pad + (sr.caretVisualColumn(line, curRune, w) - viewOff)
	if rtl && caretX == base+contentWidth {
		caretX = base + contentWidth - 1
	}
	add(caretX)

	// Ghost caret (its stored column is a reading column under RTL).
	if w.HasGhostCursor && w.GhostCursorVisualColumn > 0 {
		gvc := w.GhostCursorVisualColumn
		if rtl {
			gvc = sr.lineVisualWidth(w, line) - w.GhostCursorVisualColumn
		}
		add(base + pad + (gvc - viewOff))
	}

	// Secondary bidi cursor at an automatic direction boundary.
	runes := []rune(line)
	if bl := sr.layoutFor(w, runes); bl != nil {
		if secCol, ok := sr.secondaryCursorColumn(runes, bl, curRune, w); ok {
			add(base + pad + (secCol - viewOff))
		}
	}
	return cols
}

// secondaryCursorColumn computes the content-relative visual column of the
// SECONDARY cursor: when the caret sits on the edge of an AUTOMATIC direction
// change (a fragment boundary not expressed by an explicit direction-control
// character, which is its own visible marker), the logical position has two
// visual interpretations. Shown regardless of showBidi — the ambiguity exists
// whenever the line is bidirectional. The primary cursor follows the caret rune's own
// direction; the secondary rests where the caret would sit as the END of the
// PREVIOUS rune's fragment — one cell past that rune in ITS direction (left
// of its cell for RTL, right for LTR), the same "one past, in its own
// direction" form as the end-of-line rule.
func (sr *ScreenRenderer) secondaryCursorColumn(runes []rune, layout *bidi.Layout, p int, w *viewport.Viewport) (int, bool) {
	// The dual cursor shows whenever the line is bidirectional — with or
	// without showBidi's markers — since the boundary ambiguity exists either
	// way; only the layout (and thus the exact cells) differs. p is the caret
	// position in the coordinate space of runes (display space when the line
	// carries button substitution — the caller maps it).
	if layout == nil {
		return 0, false
	}
	if p <= 0 || p >= len(runes) {
		return 0, false
	}
	if layout.RTL[p] == layout.RTL[p-1] {
		return 0, false // not a boundary
	}
	if bidi.IsDirectionControl(runes[p]) || bidi.IsDirectionControl(runes[p-1]) {
		return 0, false // explicit override/control: it marks itself
	}

	cols := make([]int, len(runes))
	col := 0
	for _, li := range layout.Perm {
		if li >= 0 {
			cols[li] = col
		}
		col += sr.slotWidth(layout, runes, li, col, w)
	}

	// A zero-width combining mark shares its base's cell: step back to the
	// cluster base of the previous rune before placing the secondary.
	q := p - 1
	for q > 0 && sr.slotWidth(layout, runes, q, cols[q], w) == 0 {
		q--
	}
	if layout.RTL[q] {
		return cols[q] - 1, true
	}
	return cols[q] + sr.slotWidth(layout, runes, q, cols[q], w), true
}

// renderSecondaryCursor paints the secondary bidi cursor by re-drawing the
// cell just beyond the other end of the caret's fragment in reverse video.
func (sr *ScreenRenderer) renderSecondaryCursor(w *viewport.Viewport, layout *viewport.ViewportLayout) {
	if w.Buffer == nil {
		return
	}
	if !caretLineVisible(w) {
		return // caret line scrolled out of view (free scroll): no secondary
	}
	line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
	line, curRune := sr.displayCaretLine(w, line, w.CursorPos().Rune) // browse-mode buttons
	runes := []rune(line)
	bl := sr.layoutFor(w, runes)
	secCol, ok := sr.secondaryCursorColumn(runes, bl, curRune, w)
	if !ok {
		return
	}

	topOffset := 0
	if rulerActive(w, layout.Height) {
		topOffset++
	}
	if w.MessageTopInner != "" || w.MessageTopCenter != "" || w.MessageTopOuter != "" {
		topOffset++
	}
	base, pad, viewOff, contentWidth, _ := sr.caretRowGeom(w, line)

	screenY := layout.Y + 1 + topOffset + (w.CursorPos().Line - w.ViewState.ViewOffsetY)
	screenX := base + pad + (secCol - viewOff)
	if contentWidth <= 0 || screenX < base || screenX > base+contentWidth-1 {
		return // off-screen: no secondary indicator
	}

	// Resolve the glyph under the secondary cell so it can be re-painted
	// inverted: padding and beyond-content cells are spaces; content cells
	// show their marker / mirrored / plain glyph (tabs as space).
	glyph := " "
	col := 0
	if bl != nil {
		for _, li := range bl.Perm {
			wd := sr.slotWidth(bl, runes, li, col, w)
			if wd > 0 && secCol >= col && secCol < col+wd {
				switch {
				case li == bidi.MarkerRTL:
					glyph = "<"
				case li == bidi.MarkerLTR:
					glyph = ">"
				case li == bidi.MarkerEnd:
					glyph = "|"
				case bl.Marked && bidi.IsDirectionControl(runes[li]):
					if bl.RTL[li] {
						glyph = "<"
					} else {
						glyph = ">"
					}
				case runes[li] == '	':
					glyph = " "
				case runes[li] < 0x20 || runes[li] == 0x7F:
					glyph = " "
				case li >= 0 && bl.RTL[li]:
					glyph = string(bidi.Mirror(runes[li]))
				default:
					glyph = string(runes[li])
				}
				break
			}
			col += wd
		}
	}

	textColor := sr.col(w, "text")
	resetColor := sr.col(w, "reset")
	sr.MoveCursor(screenX, screenY)
	sr.Write(textColor + "[7m" + glyph + "[27m" + resetColor)
}

// runeToVisualColumn converts a rune position to a visual column position.
// This accounts for tabs (variable width) and control characters (2 chars wide).
func (sr *ScreenRenderer) runeToVisualColumn(line string, runePos int, w *viewport.Viewport) int {
	runes := []rune(line)

	// Bidirectional line: the cursor's cell is wherever its logical rune is
	// painted in visual order.
	if layout := sr.layoutFor(w, runes); layout != nil {
		col := 0
		target := -1
		for _, li := range layout.Perm {
			if li == runePos {
				target = col
			}
			col += sr.slotWidth(layout, runes, li, col, w)
		}
		if runePos >= len(runes) || target < 0 {
			return col // end of line
		}
		return target
	}

	if runePos <= 0 {
		return 0
	}
	maxRune := runePos
	if maxRune > len(runes) {
		maxRune = len(runes)
	}

	column := 0
	for i := 0; i < maxRune; i++ {
		runeWidth := sr.runeWidthAt(runes, i, column, w)
		column += runeWidth
	}

	return column
}

// lineMarkSet is the set of positions on the viewport's caret line that get a
// showMarks "*" cell, mirroring what prepareLineForDisplay draws: one before the
// cell of each marked rune, plus — only when invisibles are shown, so the
// terminator slot exists to host it — a mark at end of line. nil when showMarks
// is off or the line has no drawable marks. Every caret walk (plain and bidi)
// consumes this one set. Mirrors the editor's lineMarkSet.
func (sr *ScreenRenderer) lineMarkSet(w *viewport.Viewport, runes []rune) map[int]bool {
	if w == nil || !w.ViewState.MarksVisible() || w.Buffer == nil {
		return nil
	}
	// Mark cells are suppressed on a button-substituted caret line: the doc
	// positions the marks name have no cells of their own there (the paint
	// side suppresses identically in prepareLineForDisplay).
	if sr.lineIsSubstituted(w, w.CursorPos().Line) {
		return nil
	}
	raw := w.Buffer.MarksOnLine(w.CursorPos().Line, w.ViewState.MarksShowInternal())
	if len(raw) == 0 {
		return nil
	}
	// A plain line appends a trailing "*" for an end-of-line mark, so it always
	// shows; a bidi line's "end" is ambiguous under reordering, so there the EOL
	// mark still rides the terminator slot (invisibles only). Mirrors the editor.
	eolDrawn := w.ViewState.ShowInvisibles || sr.layoutFor(w, runes) == nil
	m := make(map[int]bool, len(raw))
	for _, p := range raw {
		if p < 0 || p > len(runes) {
			continue
		}
		if p == len(runes) && !eolDrawn {
			continue
		}
		m[p] = true
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// markedLine reports the plain (non-bidi) showMarks case: showMarks on, the
// caret line non-bidi, and it has drawable marks. Returns the line's runes and
// its mark-cell set; ok is false (base path) otherwise. Mirrors the editor.
func (sr *ScreenRenderer) markedLine(w *viewport.Viewport, line string) (runes []rune, marked map[int]bool, ok bool) {
	if w == nil || !w.ViewState.MarksVisible() || w.Buffer == nil {
		return nil, nil, false
	}
	runes = []rune(line)
	if sr.layoutFor(w, runes) != nil {
		return nil, nil, false
	}
	marked = sr.lineMarkSet(w, runes)
	if marked == nil {
		return nil, nil, false
	}
	return runes, marked, true
}

// runeToVisualColumnMarked is the plain forward walk with showMarks cells: a "*"
// cell before each marked rune, then the rune, with tab widths resolved at the
// SHIFTED column exactly as prepareLineForDisplay paints them. Returns the column
// of rune runePos's own cell. Mirrors the editor's runeToVisualColumnMarked.
func (sr *ScreenRenderer) runeToVisualColumnMarked(runes []rune, marked map[int]bool, runePos int, w *viewport.Viewport) int {
	if runePos < 0 {
		runePos = 0
	}
	col := 0
	for i := 0; i < len(runes); i++ {
		if marked[i] { // "*" cell before rune i
			col++
		}
		if i == runePos {
			return col
		}
		col += sr.runeWidthAt(runes, i, col, w)
	}
	if marked[len(runes)] {
		col++
	}
	return col
}

// bidiColumns walks a line's visual order accumulating cell columns: cols maps
// each LOGICAL rune index to the visual column its cell starts at, total is the
// full visual width. When marked is non-nil a showMarks "*" cell is inserted in
// visual order just before each marked rune's cell (and a trailing one for an
// end-of-line mark), so cols/total are exact for bidi lines with marks. Mirrors
// the editor's bidiColumns and the "*" insertion in prepareLineForDisplay.
func (sr *ScreenRenderer) bidiColumns(runes []rune, layout *bidi.Layout, marked map[int]bool, w *viewport.Viewport) (cols []int, total int) {
	cols = make([]int, len(runes))
	col := 0
	for _, li := range layout.Perm {
		if li >= 0 && marked[li] {
			col++
		}
		if li >= 0 {
			cols[li] = col
		}
		col += sr.slotWidth(layout, runes, li, col, w)
	}
	if marked[len(runes)] {
		col++
	}
	return cols, col
}

// caretVisualColumn is the caret's display column for a logical position,
// biased by direction: in RTL context "before rune i" is at the rune's RIGHT
// edge (one cell right of its cell); at end of line the boundary follows the
// last rune's direction. Mirrors the editor's caretVisualColumn. On a plain
// line with marks it walks the "*" cells inline; bidi lines are exact through
// caretVisualColumnBase, whose cols/total include the "*" cells.
func (sr *ScreenRenderer) caretVisualColumn(line string, runePos int, w *viewport.Viewport) int {
	if runes, marked, ok := sr.markedLine(w, line); ok {
		return sr.runeToVisualColumnMarked(runes, marked, runePos, w)
	}
	return sr.caretVisualColumnBase(line, runePos, w)
}

func (sr *ScreenRenderer) caretVisualColumnBase(line string, runePos int, w *viewport.Viewport) int {
	runes := []rune(line)
	layout := sr.layoutFor(w, runes)
	if layout == nil {
		return sr.runeToVisualColumn(line, runePos, w)
	}
	cols, col := sr.bidiColumns(runes, layout, sr.lineMarkSet(w, runes), w)
	rtlBase := sr.winRTL(w)

	// Zero-width combining marks share their base's cell: step back to the
	// cluster base so the caret rests on the side the base dictates.
	clusterBase := func(i int) int {
		for i > 0 && sr.slotWidth(layout, runes, i, cols[i], w) == 0 {
			i--
		}
		return i
	}

	// The caret sits where the next character of the caret's own direction
	// would land — independent of showBidi. Mirrors the editor's
	// caretVisualColumn exactly.
	if runePos >= len(runes) {
		last := clusterBase(len(runes) - 1)
		if layout.RTL[last] {
			return cols[last] - 1
		}
		if rtlBase {
			// One cell right of the last (LTR) character, not the line's
			// right edge (which is the gutter under RTL).
			return cols[last] + sr.slotWidth(layout, runes, last, cols[last], w)
		}
		return col
	}
	if runePos < 0 {
		runePos = 0
	}
	runePos = clusterBase(runePos)
	// The caret covers the cell of the rune it precedes — in either base
	// direction, for both LTR and RTL runes (an RTL rune's cell is at
	// cols[runePos], so a caret in an RTL fragment stays on the rune rather
	// than one cell to its right). Mirrors the editor's caretVisualColumn.
	return cols[runePos]
}

// rtlView maps a line of total visual width vw, scrolled by offset reading
// columns, into a view of the given width: the visible visual-column viewport
// is [eff, vw-offset), right-anchored, with pad columns of left padding when
// the viewport extends past the line's left end (right alignment).
//
// pad is returned UNCLAMPED: when the line is scrolled entirely off the right
// (offset so large that pad exceeds width), the extra pad is how far past the
// edge the content — and the caret on it — sits. Positioners (caret, ghost,
// the off-screen "@") need that distance to detect off-screen correctly; the
// painter clamps pad to width locally so it never writes past the content area.
func rtlView(vw, offset, width int) (pad, eff int) {
	eff = vw - offset - width
	if eff < 0 {
		pad = -eff
		eff = 0
	}
	return pad, eff
}

// lineVisualWidth is the total visual width of a line in this viewport (tab
// widths resolved in visual order, marker/ligature slot widths respected) —
// the renderer's counterpart to the editor's lineVisualWidth.
func (sr *ScreenRenderer) lineVisualWidth(w *viewport.Viewport, line string) int {
	runes := []rune(line)
	marked := sr.lineMarkSet(w, runes)
	layout := sr.layoutFor(w, runes)
	if layout != nil {
		_, total := sr.bidiColumns(runes, layout, marked, w)
		return total
	}
	vw := 0
	for i := range runes {
		if marked[i] {
			vw++
		}
		vw += sr.runeWidthAt(runes, i, vw, w)
	}
	if marked[len(runes)] {
		vw++
	}
	return vw
}

// rtlPadOffset returns the (pad, effective viewport start) prepareLineForDisplay
// uses for a line under direction=rtl — (0, ViewOffsetX) otherwise — so the
// cursor and ghost land on the same cells the line was painted with. The
// invisible-terminator markers prepareLineForDisplay may append occupy the
// visual END of the line; include their cells when they are shown.
func (sr *ScreenRenderer) rtlPadOffset(line string, w *viewport.Viewport, width int) (int, int) {
	// A double-width caret line lays its content into half the columns at half
	// the horizontal scroll; callers pass the halved (cell-space) width, so the
	// offset must be halved to match.
	off := w.ViewState.ViewOffsetX
	if sr.caretLineDoubleWide(w) {
		off /= 2
	}
	rtl := sr.winRTL(w)
	if !rtl || width <= 0 {
		return 0, off
	}
	runes := []rune(line)
	layout := sr.layoutFor(w, runes)
	vw := 0
	if layout != nil {
		for _, li := range layout.Perm {
			vw += sr.slotWidth(layout, runes, li, vw, w)
		}
	} else {
		for i := range runes {
			vw += sr.runeWidthAt(runes, i, vw, w)
		}
	}
	// Terminator markers are deliberately NOT counted: under rtl they paint
	// into the left padding rather than taking content columns (see
	// prepareLineForDisplay), so the content — and every caret, ghost and ruler
	// position measured against it — sits exactly where it would with
	// invisibles off.
	return rtlView(vw, off, width)
}

// caretRowGeom returns the horizontal geometry for placing a cursor on the
// viewport's caret line: base is the physical 1-based screen column where the
// content begins, contentCells its usable width, and pad/viewOff the
// right-alignment pad and horizontal scroll offset from rtlPadOffset. On a
// double-width caret line the gutter shrinks to half as many cells and the
// content lays into half the columns at half the scroll (matching
// renderContent), so every value is in that row's CELL space; dw reports it so
// a caller painting on a NORMAL row (the ruler) can map a cell column to its
// physical position with cellToPhysical. `line` is the display
// (button-substituted) caret line.
func (sr *ScreenRenderer) caretRowGeom(w *viewport.Viewport, line string) (base, pad, viewOff, contentCells int, dw bool) {
	lineNumWidth := 0
	if w.ViewState.ShowLineNumbers {
		lineNumWidth = w.LineNumWidth
	}
	sbw := 0
	if sr.showScrollbar(w) {
		sbw = 1 // the scrollbar column narrows the content like the gutter does
	}
	marginL, _ := sr.physMargins(w)
	contentCells = sr.Width - w.MarginInner - w.MarginOuter - lineNumWidth - sbw
	gutter := lineNumWidth
	dw = sr.caretLineDoubleWide(w)
	if dw {
		gutter /= 2 // the doubled row shows half as many gutter cells (rounded even)
		contentCells /= 2
		if contentCells < 1 {
			contentCells = 1
		}
	}
	base = 1 + marginL + gutter
	if sr.winRTL(w) {
		base = 1 + marginL + sbw // the gutter mirrors right; the bar sits left of the margin
	}
	pad, viewOff = sr.rtlPadOffset(line, w, contentCells)
	return
}

// cellToPhysical maps a caret-line cell column to the physical screen column of
// its left edge. Identity on ordinary rows; on a double-width (DECDWL) row each
// cell spans two physical columns, so cell c starts at physical 2c-1 — used to
// place ruler marks (drawn on a normal-width row) under a doubled caret.
func cellToPhysical(cellX int, doubleWide bool) int {
	if doubleWide {
		return 2*cellX - 1
	}
	return cellX
}

// getRuneVisualWidth returns the visual width of a rune at a given visual
// column: variable for tabs, 2 for control characters (^X display), and the
// terminal cell width otherwise — 0 for combining/zero-width characters,
// 2 for wide (CJK, emoji).
func (sr *ScreenRenderer) getRuneVisualWidth(r rune, currentColumn int, w *viewport.Viewport) int {
	if r == '\t' {
		tabSize := sr.getTabSize(w)
		return tabSize - (currentColumn % tabSize)
	} else if textwidth.IsControl(r) {
		// Control characters displayed as ^X / hex (C0, DEL, and C1)
		return substituteWidth(runeToHexOrCtrl(r))
	}
	return textwidth.Rune(r)
}

// runeWidthAt measures runes[i] with its cluster base in hand, so an
// ill-formed combining mark measures as the substitute prepareLineForDisplay
// actually paints for it (see textwidth.DefectiveMark) instead of the zero
// cells a well-formed mark would take. Every loop that walks a line for
// column math goes through here; a bare getRuneVisualWidth call has no base
// context and assumes a well-formed sequence.
func (sr *ScreenRenderer) runeWidthAt(runes []rune, i, currentColumn int, w *viewport.Viewport) int {
	r := runes[i]
	if textwidth.IsMark(r) {
		var base rune
		for j := i - 1; j >= 0; j-- {
			if !textwidth.IsMark(runes[j]) {
				base = runes[j]
				break
			}
		}
		if textwidth.DefectiveMark(base, r) {
			_, fw := defectiveMarkForm(base, r)
			return fw
		}
	}
	return sr.getRuneVisualWidth(r, currentColumn, w)
}

// lineHasZeroWidth reports whether s contains any zero-width rune (a combining
// mark) — the exact condition under which the emitted codepoint count exceeds
// the cell count, which is what makes a flip-mode host terminal misplace a
// background/reverse selection fill. Control characters (rendered as ^X, two
// cells) are excluded. s is plain display text (no ANSI).
func lineHasZeroWidth(s string) bool {
	for _, r := range s {
		if r >= 0x20 && r != 0x7F && textwidth.Rune(r) == 0 {
			return true
		}
	}
	return false
}

// getTabSize returns the tab size for a viewport.
func (sr *ScreenRenderer) getTabSize(w *viewport.Viewport) int {
	// TODO: Get from viewport or config preferences
	tabSize := 8 // Default
	if w != nil && w.ViewState.TabSize > 0 {
		tabSize = w.ViewState.TabSize
	}
	return tabSize
}

// calculateAnsiAwareLength calculates the visible column width of an
// ANSI-colored string (combining/zero-width runes count 0, wide runes 2).
func calculateAnsiAwareLength(s string) int {
	length := 0
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		length += textwidth.Rune(r)
	}
	return length
}
