package editor

import (
	"fmt"

	"math"
	"strings"
	"time"

	"github.com/phroun/ifitfits"
	"github.com/phroun/pawscript"

	"github.com/phroun/mew/internal/bidi"
	"github.com/phroun/mew/internal/viewport"
)

// renderModebar is the custom renderer for the modebar.
func (e *Editor) renderModebar(w *viewport.Viewport, screenWidth int) string {
	e.Modebar.SetActiveSequence(e.ActiveSequence)
	e.Modebar.SetCompletions(e.activeCompletions)
	return e.Modebar.RenderContent(w, screenWidth)
}

// caretHidden reports whether the hardware caret should be withheld for a
// viewport — the predicate the renderer consults each frame.
//
// A viewport hosting a TERMINAL does not own a caret: the child's cursor is
// the one that means anything there. It sits at the shell's insertion point,
// in the shape and blink the program asked for, and it is drawn by the child
// itself. mew's own caret meanwhile sits wherever the document caret happens
// to be, which is nowhere in particular. Asking for both puts two blinking
// cursors on a graphical host, one of them pointing at the wrong place.
//
// This is true of EVERY pty viewport, focused or not. An unfocused one draws
// its child's cursor in the unfocused form (see SetEmbeddedFocus in the
// host's terminalPlace); mew adding a second caret over it would say the
// opposite.
func (e *Editor) caretHidden(w *viewport.Viewport) bool {
	if w != nil && e.visibleSessionFor(w) != nil {
		return true
	}
	return e.focusedLinkButton(w) != nil
}

// renderColumnRuler renders the column ruler line for a viewport with the
// ShowRuler view option enabled. With rulerShowsCursor on, the cursor's
// column(s) — caret, ghost, and secondary bidi cursor — are marked with the
// rulerCursor color.
func (e *Editor) renderColumnRuler(w *viewport.Viewport, screenWidth int) string {
	var cursorCols []int
	if e.optBool(w, "rulershowscursor", e.Config.RulerShowsCursor) {
		cursorCols = e.Renderer.CursorColumns(w)
	}
	return e.ColumnRuler.RenderContent(w, screenWidth, cursorCols)
}

// RequestRender requests a render with debouncing.
func (e *Editor) RequestRender() {
	e.renderRequested.Store(true)
}

// performRender performs the actual render.
func (e *Editor) performRender() {
	// Serialize renders: this runs on both the main loop and the renderer's
	// resize goroutine, and its editor-level work below is not otherwise guarded.
	e.renderMu.Lock()
	defer e.renderMu.Unlock()

	// Resolve each main buffer's per-viewport options against its current grammar
	// (base [options] overlaid by [options.<grammar>]) before any layout or
	// paint reads ViewState, so direction/gutter/etc. are current this frame.
	for _, w := range e.ViewportManager.AllViewports() {
		e.reconcileGrammarOptions(w)
	}
	// Focused-scoped options (modebar, macOptionKeys, key mappings) follow the
	// focused viewport's grammar/class/type.
	e.reconcileFocusedOptions()

	// Push the focused viewport's read-only state to the host on transitions
	// (a host greys out its Edit-menu Cut for a read-only buffer).
	e.notifyEditState()
	// Push whether the built-in help viewport is open (a host syncs a "Quick
	// Help" menu checkmark to it).
	e.notifyHelpState()
	// Push whether any open buffer is modified (a host asks before closing
	// the window this session lives in).
	e.notifyUnsavedState()

	// Follow the cursor VERTICALLY only. Horizontal following is a "lock-in"
	// action performed by cursor/edit commands, not by rendering, so a manual
	// horizontal scroll (scroll_left/right) and the ghost column during vertical
	// navigation are not snapped back on every render.
	focusedViewport := e.ViewportManager.GetFocusedViewport()
	if focusedViewport != nil {
		e.renderFollowCaret(focusedViewport)
	}

	// Flip the modebar logo (M_ vs _M) to the text direction at the focused
	// caret, so the user can see which way the next keypress will move.
	if focusedViewport != nil && focusedViewport.Buffer != nil {
		lineText := strings.TrimRight(focusedViewport.Buffer.GetLine(focusedViewport.CursorPos().Line), "\n\r")
		e.Modebar.SetLogoRTL(bidi.RTLAt([]rune(lineText), focusedViewport.CursorPos().Rune, e.winRTL(focusedViewport)))
	}

	// Fill the modebar's context slot — computed for the same viewport the
	// modebar reads context from. Priority: the zero-width character backspace
	// would delete at the caret (a combining diacritic or invisible control —
	// the one thing on screen the user cannot see), then the outline breadcrumb
	// (the enclosing function/section chain), then the spawn placeholder.
	if cw := focusedViewport; cw != nil {
		if cw.Type == viewport.PromptViewport {
			cw = e.ViewportManager.GetLastMainViewport()
		}
		if cw != nil {
			if btn := e.focusedLinkButton(cw); btn != nil {
				// A focused link button shows its destination — the one thing
				// the button's resolved title hides.
				cw.Context = btn.Target
			} else if mark := e.caretMarkContext(cw); mark != "" {
				cw.Context = mark
			} else if crumb := e.outlineContext(cw); crumb != "" {
				cw.Context = crumb
			} else {
				cw.Context = cw.SpawnContext
			}
		}
	}

	// Calculate layout
	layout := e.LayoutManager.CalculateLayout(e.Renderer.Width, e.Renderer.Height)

	// Drive the main viewport's geometry from the ifitfits tiler (a minimal
	// geometry-only integration: the main document paints in its resolved tile).
	e.applyTilerGeometry(&layout)

	// Render
	e.Renderer.Render(layout)

	// Retain the main-area tiles for mouse hit-testing. Render filled each
	// entry's per-tile content bounds, so a click can be resolved to the exact
	// tile — necessary when one viewport is shown in several tiles.
	e.mainTiles = layout.MainLayout

	// The frame's viewport geometry is now set: publish the focused viewport's
	// editable rectangle to the host so a graphical pointer shows the I-beam
	// over text and the arrow over chrome, resolved locally from the pointer
	// cell (no per-motion round trip). Pushed only when the rectangle changes.
	e.notifyPointerRegion()
	e.notifyScrollbarRegions()
	e.notifyTerminalSurfaces()

	e.lastRenderTime = time.Now()
	e.renderRequested.Store(false)

	// debug_screen: snapshot the exact frame just painted (same layout) to its
	// armed box:/// target. CaptureFrame takes the renderer's own mutex (free
	// now that Render has returned); we hold renderMu, which is fine. Done after
	// clearing renderRequested so the "Wrote" toast's own RequestRender sticks
	// and schedules the frame that shows it.
	if e.pendingScreenCapture != "" {
		target := e.pendingScreenCapture
		e.pendingScreenCapture = ""
		if err := e.mew.WriteFile(target, []byte(e.Renderer.CaptureFrame(layout))); err != nil {
			e.ShowError("debug_screen: " + err.Error())
		} else {
			e.ShowNotification("Wrote " + target)
		}
	}

	// flipBidiForHost=auto: probe the terminal the first time RTL content
	// reaches the screen; resolve a probe whose reply never came.
	e.maybeSendBidiProbe()
	e.checkBidiProbeTimeout()

	// Show/hide the Kitty force_ltr nudge based on what this frame painted.
	e.updateNiqqudNudge()
}

// applyTilerGeometry drives the painted main viewport's geometry from the
// ifitfits tiler. This is a deliberately minimal, geometry-only integration:
// the tiler holds a "main" tile (split against a "blank" tile) whose resolved
// cell rect becomes the main viewport's on-screen box, proving the tiler's
// geometry flows into mew's window painting. Nothing else (focus, input, the
// blank tile's contents) is wired yet.
//
// The tiler works in the same cell units the main editor uses. Its workspace is
// the full main-area rectangle (screen width × the negotiated main height). The
// "main" tile's rect maps to the viewport two ways, because mew models the two
// axes differently:
//   - Horizontal: the ViewportLayout has no X/width, so the tile's X/width go on
//     the viewport itself (FrameX/FrameWidth) and the renderer honors them.
//   - Vertical: the ViewportLayout already carries per-viewport Y/height, and the
//     layout's MainHeight is exactly the tiler's workspace height, so the tile's
//     Y/height replace this entry's Y/height. That flows through the normal
//     render pass into ContentHeight, so paging/scroll/mouse all see the real
//     window height — and because it is per-viewport, a prompt buffer (which
//     sets its own ContentHeight) is unaffected.
//
// applyTilerGeometry replaces the layout's main-area entries with ones derived
// from the ifitfits tiler: the tiler owns the arrangement of the non-docked area,
// so its workspace is that area (screen width × the negotiated main height), and
// each visible tile becomes a ViewportLayout drawing the mew viewport its ref
// names. A tile whose ref names no live viewport (an empty tile) is skipped — the
// freshly cleared back buffer shows through as blank. Chrome (docked viewports)
// is untouched; the renderer paints tiles and docked viewports through the same
// agnostic path.
//
// Horizontal and vertical extent both ride on the layout ENTRY (the tile):
// FrameX/FrameWidth and Y/Height are per-tile, so a viewport referenced by
// several tiles (tiles↔viewports is many-to-many, e.g. after viewport_split
// clones a ref) gets a distinct frame in each. The renderer applies each tile's
// frame to the viewport just before painting it.
func (e *Editor) applyTilerGeometry(layout *viewport.Layout) {
	if layout.MainHeight <= 0 || e.Renderer.Width <= 0 {
		layout.MainLayout = nil
		return
	}
	vp := e.ensureTiler()
	vp.SetWorkspace(float64(e.Renderer.Width), float64(layout.MainHeight))

	tiles := vp.Tiles()
	stackSel := e.stackSelectedTabs(vp, tiles)

	// Clear last frame's stack-tab counters that no longer apply, leaving any
	// message some other feature owns untouched (only clear our own text).
	for id, msg := range e.stackTabCounters {
		if w := e.ViewportManager.GetViewport(id); w != nil && w.MessageTopInner == msg {
			w.MessageTopInner = ""
		}
	}
	e.stackTabCounters = nil

	focusedTile := vp.GetFocus()
	mainTop := layout.TopHeight
	mains := make([]viewport.ViewportLayout, 0, 4)
	for _, b := range tiles {
		w := e.ViewportManager.GetViewport(b.Ref)
		if w == nil {
			continue // empty/blank tile
		}
		// mew draws no tab strip yet, so a stacked group reserves nothing
		// (SetStackReserve defaults to 0) and the shown tab already fills its box.
		// Until there's a real strip, put a "[i/n]" counter in the viewport's
		// top-left message so a flat stack is at least legible.
		rect := b.Rect
		if st, ok := stackSel[b.Tile]; ok {
			msg := fmt.Sprintf("[%d/%d]", st.index+1, st.count)
			w.MessageTopInner = msg
			if e.stackTabCounters == nil {
				e.stackTabCounters = make(map[string]string)
			}
			e.stackTabCounters[w.ID] = msg
		}
		// Snap each tile to integer cell edges by rounding its LEFT and RIGHT
		// (and TOP/BOTTOM) edges, then taking the span. Rounding edges rather
		// than truncating width keeps adjacent tiles flush and the rightmost/
		// bottommost tile reaching the workspace edge, so a fractional split
		// (e.g. an 81-column area halved) never leaves a one-cell gap.
		x0 := int(math.Round(rect.X))
		x1 := int(math.Round(rect.X + rect.W))
		y0 := int(math.Round(rect.Y))
		y1 := int(math.Round(rect.Y + rect.H))
		// Geometry rides on the tile (this layout entry): a viewport shown in
		// several tiles gets a distinct frame per tile, and the renderer applies
		// each just before painting it. The viewport's own FrameX/FrameWidth are
		// also set (last tile wins) for the single-tile mouse hit-test path.
		w.FrameX = x0
		w.FrameWidth = x1 - x0
		mains = append(mains, viewport.ViewportLayout{
			Viewport:   w,
			Y:          mainTop + y0,
			Height:     y1 - y0,
			FrameX:     x0,
			FrameWidth: x1 - x0,
			Focused:    b.Tile == focusedTile,
			TileHandle: uint64(b.Tile),
		})
	}
	layout.MainLayout = mains
}

// stackTabInfo is a shown stack tab's position within its stack (0-based index
// and total tab count), used to stamp the "[i/n]" counter.
type stackTabInfo struct {
	index, count int
}

// stackSelectedTabs maps the leaf handle of each FLAT stack's shown tab to its
// tab position. Only flat stacks (every tab a single leaf) are annotated: a tab
// that is itself a split shows more than one leaf in the box and its selected
// handle isn't a leaf tile, so it wouldn't match anyway — those wait for a real
// tab strip. A stack's shown tab is flat when exactly one visible tile falls
// inside the stack's box (its buried tabs are hidden, so Tiles omits them).
func (e *Editor) stackSelectedTabs(vp *ifitfits.Viewport, tiles []ifitfits.Box) map[ifitfits.Handle]stackTabInfo {
	stacks := vp.Stacks()
	if len(stacks) == 0 {
		return nil
	}
	inside := func(r ifitfits.Rect, b ifitfits.Box) bool {
		cx := b.Rect.X + b.Rect.W/2
		cy := b.Rect.Y + b.Rect.H/2
		return cx >= r.X && cx <= r.X+r.W && cy >= r.Y && cy <= r.Y+r.H
	}
	out := map[ifitfits.Handle]stackTabInfo{}
	for _, s := range stacks {
		sel := -1
		for i, tb := range s.Tabs {
			if tb.Selected {
				sel = i
			}
		}
		if sel < 0 {
			continue
		}
		n := 0
		for _, b := range tiles {
			if inside(s.Rect, b) {
				n++
			}
		}
		if n != 1 {
			continue // the shown tab is a split, not a single leaf
		}
		out[s.Tabs[sel].Tile] = stackTabInfo{index: sel, count: len(s.Tabs)}
	}
	return out
}

// updateModebar updates the modebar display.
func (e *Editor) updateModebar() {
	// The modebar plugin handles its own rendering via the custom renderer
	// Just request a render to update the display
	e.RequestRender()
}

// cursorStyleFor reports the DECSCUSR shape a viewport's CURRENT editing mode
// calls for: navigationCursor while link browsing is armed (the mode sits over
// the top of the other two), overwriteCursor while typing replaces, and
// insertCursor otherwise. The renderer consults it only on frames that show the
// caret, so a shape is never selected for one being hidden — including the
// inert caret inside a focused link button.
func (e *Editor) cursorStyleFor(w *viewport.Viewport) int {
	switch {
	case w != nil && w.BrowseActive:
		return e.Config.NavigationCursor
	case w != nil && w.ViewState.OverwriteMode:
		return e.Config.OverwriteCursor
	default:
		return e.Config.InsertCursor
	}
}

// registerScreenCommands registers the commands that act on the frame itself.
func (e *Editor) registerScreenCommands(ps *pawscript.PawScript) {
	// screen_refresh discards the renderer's knowledge of what the terminal is
	// showing and forces a full clear-and-repaint on the next frame — recovery
	// from external corruption of the display (a stray program writing over it,
	// a garbled resize) that the incremental diff would otherwise preserve.
	ps.RegisterCommand("screen_refresh", func(ctx *pawscript.Context) pawscript.Result {
		e.Renderer.ForceRedraw()
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	// set_font re-points a host font alias at one or more font names on a
	// graphical host (e.g. set_font "ui-term", "JetBrainsMono"): the first name
	// that resolves is used, later names are fallbacks. The host loads unknown
	// names (system scan / configured search paths) and repaints live. On a
	// plain terminal (no FontSink) it warns — fonts are the terminal's there.
	ps.RegisterCommand("set_font", func(ctx *pawscript.Context) pawscript.Result {
		alias, ok := argString(ctx, 0)
		if !ok || strings.TrimSpace(alias) == "" {
			e.ShowWarning("set_font: usage: set_font \"<alias>\", \"<font>\" [, \"<fallback>\"...]")
			return pawscript.BoolStatus(false)
		}
		var names []string
		for i := 1; ; i++ {
			n, ok := argString(ctx, i)
			if !ok {
				break
			}
			if n = strings.TrimSpace(n); n != "" {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			e.ShowWarning("set_font: no font name given")
			return pawscript.BoolStatus(false)
		}
		if e.Config.FontSink == nil {
			e.ShowWarning("set_font: not supported on this terminal")
			return pawscript.BoolStatus(false)
		}
		got := e.Config.FontSink(strings.TrimSpace(alias), names)
		e.RequestRender()
		if got {
			e.ShowNotification(fmt.Sprintf("Font %q set to %s", alias, names[0]))
		} else {
			e.ShowWarning(fmt.Sprintf("Font %q: %q not found (using fallback)", alias, names[0]))
		}
		return pawscript.BoolStatus(got)
	})

	// debug_screen arms a full-frame ANSI snapshot of the screen, written to a
	// timestamped ".ans" file in the box:/// support tree (~/.mew locally) — a
	// capture that reproduces the screen when cat'd to a terminal. The write
	// rides the NEXT render (see performRender): the command only arms the target
	// and forces a full repaint, so it never re-renders (or re-locks renderMu)
	// itself — snapshotting the exact frame that gets painted.
	ps.RegisterCommand("debug_screen", func(ctx *pawscript.Context) pawscript.Result {
		e.pendingScreenCapture = "box:///" + time.Now().Format("2006-01-02 15.04.05") + ".ans"
		e.Renderer.ForceRedraw()
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	// Render command
	ps.RegisterCommand("render", func(ctx *pawscript.Context) pawscript.Result {
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})
}
