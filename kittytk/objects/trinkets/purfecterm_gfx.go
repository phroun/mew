package trinkets

// The graphical paint path for PurfecTerm (D1): a faithful port of
// the reference gtk/qt widget renderers onto KittyTK's Painter and the
// shared text engine. It drives purfecterm.Buffer directly - screen
// splits, sprites, custom glyphs, screen scale/crop, selection,
// blink animation, cursor shapes with the unfocused hollow-box form,
// scrollbars, autoscroll, and the right-click context menu.
//
// Porting notes (deliberate deltas from the gtk reference):
//   - terminalLeftPadding is 0: KittyTK trinkets own their full bounds.
//   - Rect fills round to whole units edge-to-edge (no seams);
//     glyphs, sprites, and cursor overlays keep device-pixel
//     precision through Painter.DrawImageOffset.
//   - Scrollbars are overlay lanes inside the trinket (macOS style)
//     instead of composed native widgets.

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
	"github.com/phroun/kittytk/text"
	"github.com/phroun/purfecterm"
)

// arabicDiagOnce prints, the first time an Arabic cell is rendered, which face
// Arabic actually resolves to at RUNTIME and whether it cursively joins under
// the embedded shaper — the ground truth for diagnosing disconnected Arabic on
// a user machine (e.g. a locally-installed font silently overriding the
// embedded one).
var arabicDiagOnce sync.Once

// arabicGeomOnce prints the first both-sides-joining cell's exact slice
// geometry (box, ppu, window, keep range, slice bounds, edge-ink verdict).
var arabicGeomOnce sync.Once

const (
	// Overlay lane thickness: one layout column, matching every other
	// scrollbar in the toolkit.
	gfxScrollbarLane  = core.Unit(8)
	gfxMenuItemHeight = core.Unit(16) // context menu row height
	gfxMenuWidth      = core.Unit(150)
)

// purfecTermGfx is the graphical-path state carried by PurfecTerm.
type purfecTermGfx struct {
	// Blink animation (bobbing wave phase + cursor blink), driven by
	// a 50ms desktop timer like the gtk reference.
	blinkPhase    float64
	blinkTick     int
	cursorBlinkOn bool
	blinkTimer    *DesktopTimer

	// ppu is the renderer's pixels-per-unit, cached on each paint for the
	// input paths (scrollbar hit tests run in render pixels and have no
	// painter in hand). 0 = not painted yet; treat as 1.
	ppu float64
	// vpWpx/vpHpx are the widget's DEVICE extent as the outer system snapped
	// it, and hitKX/hitKY the scale from a unit coordinate onto it.
	vpWpx, vpHpx float64
	hitKX, hitKY float64

	// Local selection drag.
	mouseDown      bool
	mouseDownX     int
	mouseDownY     int
	selecting      bool
	selectionMoved bool
	lastMouseX     int
	lastMouseY     int

	// Auto-scroll while dragging beyond the edges.
	autoTimer *DesktopTimer
	autoVert  int
	autoHoriz int

	// Scrollbar drag, all in RENDER PIXELS. The thumb follows the pointer
	// at pixel granularity while the content offset snaps to whole lines
	// and columns — quantizing the THUMB was what made a drag lurch in
	// cell-sized steps while the wheel glided. Grab offset is where the
	// press landed within the thumb; thumb pos is the unsnapped thumb
	// origin along the track.
	vDragging bool
	hDragging bool
	vHover    bool // pointer over the vertical thumb
	hHover    bool // pointer over the horizontal thumb
	vGrabOff  float64
	hGrabOff  float64
	vThumbPos float64
	hThumbPos float64

	// Mouse reporting toggle (context menu).
	reportingDisabled bool

	// Context menu.
	menuHover int

	// Scheme mirror (cli.Terminal keeps its own copy privately).
	scheme    purfecterm.ColorScheme
	schemeSet bool

	// Caches + engine for scaled glyph imagery.
	engine     *text.Engine
	fontEpoch  uint64 // engine font-set epoch the text cache was built against
	textCur    map[coverMaskKey]*image.RGBA
	textPrev   map[coverMaskKey]*image.RGBA
	glyphCur   map[purfecterm.GlyphCacheKey]*image.RGBA
	glyphPrev  map[purfecterm.GlyphCacheKey]*image.RGBA
	overlayCur map[string]*image.RGBA
}

// coverMaskKey identifies a cached glyph COVERAGE mask - deliberately
// color-independent (no fg), so recoloring a glyph (an ls listing, a fire
// animation) is a cache hit that just re-tints the same grayscale mask.
// family distinguishes faces (font slots), so two families at the same box
// size never collide.
type coverMaskKey struct {
	str          string
	family       string
	bold         bool
	italic       bool
	wPx, hPx     int
	wide         bool
	kashL, kashR bool // Arabic kashida drawn on the left/right cell edge
}

const gfxCacheMax = 4096

// SetColorScheme sets the terminal color scheme for both the CLI
// renderer and the graphical path.
func (t *PurfecTerm) SetColorScheme(scheme purfecterm.ColorScheme) {
	if t.terminal != nil {
		t.terminal.SetColorScheme(scheme)
	}
	t.gfx.scheme = scheme
	t.gfx.schemeSet = true
	t.Update()
}

// SetMouseReportingEnabled controls whether mouse events are
// forwarded to the PTY when the application requests tracking.
func (t *PurfecTerm) SetMouseReportingEnabled(enabled bool) {
	t.gfx.reportingDisabled = !enabled
}

func (t *PurfecTerm) gfxScheme() purfecterm.ColorScheme {
	if t.gfx.schemeSet {
		return t.gfx.scheme
	}
	return termColorScheme()
}

func (t *PurfecTerm) gfxEngine() *text.Engine {
	// Prefer the process-wide engine (published by the raster backend) so the
	// terminal grid and the UI chrome share one font set — a live font change
	// via SetFontAlias / UseFont then re-fonts every surface at once.
	if shared := text.Shared(); shared != nil {
		t.gfx.engine = shared
		return shared
	}
	if t.gfx.engine == nil {
		t.gfx.engine = text.NewEngine()
		// Same tail-of-chain system fallbacks as the raster engine,
		// so terminal cells and UI text cover the same repertoire.
		t.gfx.engine.LoadSystemFallbacks()
	}
	return t.gfx.engine
}

// gfxFocused: the terminal shows its focused cursor form only when
// it has focus within the ACTIVE window chain - in any background
// window the inactive (hollow box) form paints instead. An embedded
// terminal has no focus of its own to hold (see SetEmbeddedFocus): its
// host declares it, and it is still subject to the window chain.
func (t *PurfecTerm) gfxFocused() bool {
	return t.paintFocused() && core.FocusChainActive(t.Self())
}

// gfxInputActive reports whether input events take the graphical
// handlers (the desktop paints graphical frames).
func (t *PurfecTerm) gfxInputActive() bool {
	return t.terminal != nil && core.FindGraphicalFrames(t)
}

// scrollLanesActive reports whether scrollbar lanes exist to interact
// with. Both surfaces have them — the graphical one overlays pixel lanes,
// the cell one reserves whole cells (see updateTerminalSize) — everywhere
// but editor mode, which reclaims the lane for text.
func (t *PurfecTerm) scrollLanesActive() bool {
	return t.terminal != nil && !t.editorMode
}

// termColorScheme builds the terminal's color scheme from the app's
// theme palettes: both the dark and light 16-color palettes (in ANSI
// order, which purfecterm indexes by) plus each theme's default
// background/foreground. The terminal's own DECSCNM reverse-video state
// then selects between them, staying in step with the app's colors.
func termColorScheme() purfecterm.ColorScheme {
	s := purfecterm.DefaultColorScheme() // inherit cursor/selection/blink defaults
	darkA := style.TermPaletteDark.ANSIColors()
	lightA := style.TermPaletteLight.ANSIColors()
	darkPal := make([]purfecterm.Color, 16)
	lightPal := make([]purfecterm.Color, 16)
	for i := 0; i < 16; i++ {
		darkPal[i] = pcColor(darkA[i])
		lightPal[i] = pcColor(lightA[i])
	}
	s.DarkPalette = darkPal
	s.LightPalette = lightPal
	s.DarkForeground = pcColor(style.TermPaletteDark.Foreground)
	s.DarkBackground = pcColor(style.TermPaletteDark.Background)
	s.LightForeground = pcColor(style.TermPaletteLight.Foreground)
	s.LightBackground = pcColor(style.TermPaletteLight.Background)
	return s
}

// pcColor converts a theme palette color to a purfecterm true color.
func pcColor(c style.TermRGB) purfecterm.Color {
	return purfecterm.TrueColor(c.R, c.G, c.B)
}

func pcRGBA(c purfecterm.Color) color.RGBA {
	return color.RGBA{c.R, c.G, c.B, 255}
}

func pcStyle(c purfecterm.Color) style.CellStyle {
	return style.DefaultStyle().WithBg(style.RGB(int(c.R), int(c.G), int(c.B)))
}

// fillUnitsF fills a float-unit rect, rounding edges (not sizes) so
// adjacent cells tile without seams.
// fillPixels fills a rectangle given in PurfecTerm's own coordinate units
// by converting to device pixels at ppu (the renderer's font_size-aware
// pixels-per-unit) and painting a CONTIGUOUS pixel rect anchored at the
// widget origin (unit 0,0). The terminal lays out its own grid in native
// pixels this way, so cells tile seamlessly and never re-snap to the
// host's cell grid. Adjacent cells share a boundary pixel (cell N's right
// edge rounds to the same pixel as cell N+1's left edge), so there are no
// seams.
func fillPixels(p *core.Painter, x0, y0, x1, y1, ppu float64, c purfecterm.Color) {
	rx0 := int(math.Round(x0 * ppu))
	ry0 := int(math.Round(y0 * ppu))
	rx1 := int(math.Round(x1 * ppu))
	ry1 := int(math.Round(y1 * ppu))
	if rx1 <= rx0 || ry1 <= ry0 {
		return
	}
	p.FillRectPixels(0, 0, rx0, ry0, rx1-rx0, ry1-ry0, pcStyle(c))
}

// ---------------------------------------------------------------
// Main paint
// ---------------------------------------------------------------

func (t *PurfecTerm) paintGraphical(p *core.Painter, bounds core.UnitRect) {
	t.rotateGfxCaches()
	if t.gfx.blinkTimer == nil {
		// No animation timer (headless paint, or no desktop yet):
		// the cursor is steadily visible.
		t.gfx.cursorBlinkOn = true
	}
	buf := t.terminal.Buffer()
	scheme := t.gfxScheme()
	// PurfecTerm renders its interior in native device pixels. ppu is the
	// renderer's font_size-aware pixels-per-unit - the sole scaling knob -
	// used to convert the terminal's own unit cell geometry to pixels;
	// unlike the raw device zoom it tracks font_size, and unlike the unit
	// fill path it does not re-snap cells to the host grid.
	ppu := p.PxPerUnitF()
	baseCW, baseCH := t.cellDims()

	// The terminal's native-pixel viewport: the widget's FULL device-pixel
	// extent (snapped, as the outer system places it). Everything inside is
	// laid out in these pixels. Using bounds.Width*ppu instead would fall
	// short of the widget edge at fractional ppu, leaving a strip unpainted.
	vpFullWpx := p.UnitSpanPxX(0, bounds.Width)
	vpFullHpx := p.UnitSpanPxY(0, bounds.Height)

	// Two rates meet here and BOTH matter. ppu is the renderer's font-aware
	// pixels-per-unit, which the cell grid is laid out and painted with.
	// vpFull*px is the widget's true device extent, which the outer system
	// SNAPS — so it is not W*ppu, and the gap between them grows with the
	// distance from the origin. Anything anchored to the widget's EDGE (the
	// scrollbar lanes) must use the snapped extent, and any pointer must be
	// scaled onto it, or the hit box drifts from the paint. hitK is that
	// scale; it is 1 only when the two rates happen to coincide.
	t.gfx.ppu = ppu
	t.pushCellPixelSizeGfx() // assert the synthetic ?1016 cell size (CSI 16 t)
	t.gfx.vpWpx, t.gfx.vpHpx = float64(vpFullWpx), float64(vpFullHpx)
	t.gfx.hitKX, t.gfx.hitKY = 1, 1
	if bounds.Width > 0 && ppu > 0 {
		t.gfx.hitKX = float64(vpFullWpx) / (float64(bounds.Width) * ppu)
	}
	if bounds.Height > 0 && ppu > 0 {
		t.gfx.hitKY = float64(vpFullHpx) / (float64(bounds.Height) * ppu)
	}

	// Content pixel width (viewport minus the vertical scrollbar lane) drives
	// how many whole COLUMNS fit; size the terminal to that so the grid fills its
	// space (updateTerminalSize's unit division undercounts at fractional ppu).
	// The yellow scrollback line and text span this content width. The lane's
	// reservation is content-INDEPENDENT (gfxInputActive is the frame's state,
	// not the child's), so folding it into the grid the child is sized to is safe.
	contentWpx := vpFullWpx
	if t.gfxInputActive() && !t.editorMode {
		contentWpx = p.UnitSpanPxX(0, bounds.Width-gfxScrollbarLane)
	}
	// ROWS fit the FULL height. The child's grid must be a pure function of its
	// rectangle and font — never of its own content — or it oscillates. Reserving
	// a row for the horizontal bar (which shows only when a VISIBLE line overflows
	// the grid) would shrink the grid by a row, which changes which lines are
	// visible, which flips the bar's presence, which regrows the grid: a
	// self-sustaining Resize -> emitResize -> child SIGWINCH -> full redraw ->
	// repaint -> re-fit loop at a FIXED window size, spinning the emulator and
	// thrashing allocation. So the horizontal bar OVERLAYS the bottom row (it is a
	// thin lane, present only while a line overflows) instead of stealing a row.
	contentHpx := vpFullHpx
	// A MIRROR paint sizes nothing: it renders the grid the primary already set,
	// clipped to its own rectangle. Re-fitting here from the mirror's (often
	// smaller) rect would resize the child under a read-only copy.
	if !t.mirrorPaint.Load() && baseCW > 0 && baseCH > 0 {
		fitCols := clampGridDim(int(float64(contentWpx) / (float64(baseCW) * ppu)))
		fitRows := clampGridDim(int(float64(contentHpx) / (float64(baseCH) * ppu)))
		if fitCols > 0 && fitRows > 0 && (fitCols != t.cols || fitRows != t.rows) {
			t.cols, t.rows = fitCols, fitRows
			t.terminal.Resize(fitCols, fitRows)
			t.emitResize(fitCols, fitRows)
		}
	}

	isDark := buf.IsDarkTheme()
	cols, rows := buf.GetSize()
	cursorVisible := buf.IsCursorVisible()
	cursorShape, _ := buf.GetCursorStyle()
	scrollOffset := buf.GetEffectiveScrollOffset()
	cursorVisibleX, cursorVisibleY := buf.GetCursorVisiblePosition()
	if cursorVisibleX < 0 || cursorVisibleY < 0 {
		cursorVisible = false
	}
	cursorLineY := buf.GetCursorVisibleY()
	cursorLogicalX, _ := buf.GetCursor()
	buf.ClearHorizMemos()

	horizScale := buf.GetHorizontalScale()
	vertScale := buf.GetVerticalScale()
	cw := float64(baseCW) * horizScale // scaled cell width in units
	chh := float64(baseCH) * vertScale // scaled cell height in units

	// Whole-trinket background.
	// Backdrop covers the whole device-pixel viewport (the scrollbar paints
	// over its lane afterward), so no strip of the widget shows through at
	// fractional ppu.
	p.FillRectPixels(0, 0, 0, 0, vpFullWpx, vpFullHpx, pcStyle(scheme.Background(isDark)))

	// Screen crop clip (crop values in sprite units).
	widthCrop, heightCrop := buf.GetScreenCrop()
	unitX, unitY := buf.GetSpriteUnits()
	painter := p
	if widthCrop > 0 || heightCrop > 0 {
		cropW := float64(bounds.Width)
		cropH := float64(bounds.Height)
		if widthCrop > 0 {
			cropW = float64(widthCrop) * cw / float64(unitX)
		}
		if heightCrop > 0 {
			cropH = float64(heightCrop) * chh / float64(unitY)
		}
		painter = p.WithClip(core.UnitRect{
			Width:  core.Unit(math.Round(cropW)),
			Height: core.Unit(math.Round(cropH)),
		})
	}

	horizOffset := buf.GetHorizOffset()
	behind, front := buf.GetSpritesForRendering()
	t.renderSpritesGfx(painter, behind, buf, scheme, isDark, cw, chh, ppu, scrollOffset, horizOffset)

	focused := t.gfxFocused()
	cursorLineWasRendered := false

	for y := 0; y < rows; y++ {
		if y == cursorLineY {
			cursorLineWasRendered = true
		}
		lineAttr := buf.GetVisibleLineAttribute(y)
		effectiveCols := cols
		if lineAttr != purfecterm.LineAttrNormal {
			effectiveCols = cols / 2
		}
		lineMul := 1.0
		if lineAttr != purfecterm.LineAttrNormal {
			lineMul = 2.0
		}

		visibleAccumulatedWidth := 0.0
		for x := 0; x < effectiveCols; x++ {
			logicalX := x + horizOffset
			cell := buf.GetVisibleCell(x, y)

			cellVisualWidth := 1.0
			if cell.CellWidth > 0 { // CellWidth is authoritative (see patches/purfecterm/PROTOCOL.md)
				cellVisualWidth = cell.CellWidth
			}

			fg := scheme.ResolveColor(cell.Foreground, true, isDark)
			bg := scheme.ResolveColor(cell.Background, false, isDark)

			// Blink attribute per scheme mode.
			blinkVisible := true
			if cell.Blink {
				switch scheme.BlinkMode {
				case purfecterm.BlinkModeBright:
					palette := scheme.Palette(isDark)
					for i := 0; i < 8 && i+8 < len(palette); i++ {
						if bg == palette[i] {
							bg = palette[i+8]
							break
						}
					}
				case purfecterm.BlinkModeBlink:
					blinkVisible = t.gfx.blinkPhase < math.Pi
				}
			}

			if buf.IsInSelection(logicalX, y) {
				bg = scheme.Selection
			}

			isCursor := cursorVisible && x == cursorVisibleX && y == cursorVisibleY && t.gfx.cursorBlinkOn
			if isCursor && focused && cursorShape == 0 {
				fg, bg = bg, fg // solid block cursor when focused
			}

			cellX := visibleAccumulatedWidth * lineMul * cw
			cellY := float64(y) * chh
			cellW := cellVisualWidth * cw * lineMul
			cellH := chh
			visibleAccumulatedWidth += cellVisualWidth

			if bg != scheme.Background(isDark) {
				fillPixels(painter, cellX, cellY, cellX+cellW, cellY+cellH, ppu, bg)
			}

			if cell.Char != ' ' && cell.Char != 0 && blinkVisible {
				yOffPx := 0
				if cell.Blink && scheme.BlinkMode == purfecterm.BlinkModeBounce {
					wavePhase := t.gfx.blinkPhase + float64(x)*0.5
					yOffPx = int(math.Round(math.Sin(wavePhase) * 3.0 * ppu))
				}
				// Arabic contextual joining: a per-cell renderer must pick the
				// presentation form itself (a lone letter always shapes
				// isolated), from the neighbor cells' base characters.
				var leftCh, rightCh rune
				if x > 0 {
					leftCh = buf.GetVisibleCell(x-1, y).Char
				}
				if x+1 < effectiveCols {
					rightCh = buf.GetVisibleCell(x+1, y).Char
				}
				shaped, suppress := purfecterm.ShapeArabicCellVisual(leftCh, cell.Char, rightCh)
				if !suppress && !t.renderCustomGlyphCell(painter, buf, &cell, cellX, cellY, cellW, cellH, lineAttr, ppu, yOffPx) {
					// The app may emit PRESENTATION forms (mew pre-shapes
					// Arabic); joining and the shaping window are computed
					// from the BASE letters, or nothing would ever join.
					baseC := arabicBaseChar(cell.Char)
					baseL := arabicBaseChar(leftCh)
					baseR := arabicBaseChar(rightCh)
					kashL, kashR := arabicKashida(baseC, baseL, baseR)
					dc := cell
					var actx *arabicCellShape
					if purfecterm.ScriptClass(cell.Char) == "arabic" {
						// Shape a five-piece window (neighbours + tatweels +
						// letter) as one run so the font's GSUB joins for real;
						// the renderer cuts this cell's piece out of it.
						actx = arabicRenderContext(baseC, shaped, baseL, baseR, kashL, kashR, []rune(cell.Combining))
					} else {
						dc.Char = shaped
					}
					t.drawCellText(painter, &dc, t.cellFamily(buf, &cell), fg, cellX, cellY, cellW, cellH, lineAttr, ppu, yOffPx, cellVisualWidth, kashL, kashR, actx)
				}
			}

			t.drawCellDecorations(painter, buf, &cell, scheme, isDark, fg, cellX, cellY, cellW, cellH, lineAttr, ppu)

			if isCursor {
				t.drawCursorOverlay(painter, scheme, focused, cursorShape, cellX, cellY, cellW, cellH, ppu)

				// Report where the text is, so an input method can put its
				// candidate window under the cursor: the CJK candidate
				// list, macOS's press-and-hold accent picker, the emoji
				// picker. Without it they open at a corner of the window.
				//
				// Only the position — this path DRAWS its own cursor just
				// above, so asking for a platform caret as well would be
				// asking for a second one. (The cell path does ask, because
				// there the outer terminal draws it and nothing here can.)
				if focused {
					painter.RequestTextInputArea(core.Unit(math.Round(cellX)), core.Unit(math.Round(cellY)))
				}
			}
		}

		// Horizontal memo for the cursor's line (auto-scroll input).
		if y == cursorLineY && cursorLineY >= 0 {
			leftmostCell := horizOffset
			rightmostCell := horizOffset + effectiveCols - 1
			maxReachableCol := -1
			if widthCrop > 0 {
				maxReachableCol = widthCrop/unitX - 1
			}
			memo := purfecterm.HorizMemo{
				Valid: true, LogicalRow: -1,
				LeftmostCell: leftmostCell, RightmostCell: rightmostCell,
				DistanceToLeft: -1, DistanceToRight: -1,
			}
			if cursorLogicalX >= leftmostCell && cursorLogicalX <= rightmostCell {
				memo.CursorLocated = true
			} else if cursorLogicalX < leftmostCell && cursorLogicalX >= 0 {
				memo.DistanceToLeft = leftmostCell - cursorLogicalX
			} else if cursorLogicalX > rightmostCell {
				if maxReachableCol < 0 || cursorLogicalX <= maxReachableCol {
					memo.DistanceToRight = cursorLogicalX - rightmostCell
				}
			}
			buf.SetHorizMemo(y, memo)
		}
	}

	t.renderSpritesGfx(painter, front, buf, scheme, isDark, cw, chh, ppu, scrollOffset, horizOffset)

	// Screen splits overlay regions of the logical screen.
	splits := buf.GetScreenSplitsSorted()
	if len(splits) > 0 {
		w := t.renderSplitsGfx(painter, buf, splits, scheme, isDark, cols, rows, cw, chh, unitX, unitY, ppu, horizOffset)
		buf.SetSplitContentWidth(w)
	} else {
		buf.SetSplitContentWidth(0)
	}

	// Yellow dashed boundary between scrollback and the logical screen.
	// Filled in native pixels at the same ppu the cells use, so it lands on
	// the row boundary instead of drifting against the (ppu-rendered) rows.
	if boundaryRow := buf.GetScrollbackBoundaryVisibleRow(); boundaryRow > 0 {
		rowY := float64(boundaryRow) * chh
		yellow := purfecterm.TrueColor(255, 200, 0)
		// Dash across the full content width (in render-units), so the line
		// reaches the rightmost column instead of stopping at bounds.Width*ppu.
		endU := float64(contentWpx) / ppu
		for x := 0.0; x < endU; x += 8 {
			fillPixels(p, x, rowY, x+4, rowY+1, ppu, yellow)
		}
	}

	// These are the PRIMARY paint's alone. CheckCursorAutoScroll{,Horiz} scroll
	// the shared buffer to keep the cursor visible — measured against THIS paint's
	// clipped region. Run from a mirror (a different, often smaller rectangle),
	// they scroll the buffer to fit the mirror, and the primary then shows the
	// scrolled result: the terminal jumps a row before repainting, or a full line
	// chases horizontally to show only its tail. A mirror is read-only, so it must
	// touch none of this — it just draws the grid the primary settled.
	if !t.mirrorPaint.Load() {
		buf.SetCursorDrawn(cursorLineWasRendered)
		if buf.CheckCursorAutoScroll() {
			t.Update()
		}
		if buf.CheckCursorAutoScrollHoriz() {
			t.Update()
		}
		buf.ClearDirty()
	}

	if !t.editorMode {
		t.paintScrollbarsGfx(p, bounds, buf, chh)
	}
	t.ensureBlinkTimer()
}

// ---------------------------------------------------------------
// Text rendering with scaling (double lines, screen scale, flex)
// ---------------------------------------------------------------

// primaryTermFamily is the terminal grid's primary face — font slot 0 (SGR
// 10). It is the app-chosen terminal font (the "ui-term" engine alias by
// default, so the systematic ui-* tree and [window] ui_term config reach the
// grid), via effTermFont so the default is honored even before SetTerminalFont.
func (t *PurfecTerm) primaryTermFamily() string {
	if f := t.effTermFont(); f != nil && f.Name != "" {
		return f.Name
	}
	return "ui-term"
}

// cellFamily resolves the font family a cell paints in, most specific first:
//  1. an explicit per-cell font slot (SGR 10-20 / OSC 7004) the app selected;
//  2. an app-configured script-class font (OSC 7005) for the glyph's script,
//     overriding the engine's ui-term-<script> default so a program running in
//     the terminal gets the same script fonts on the SDL path as on gtk/qt;
//  3. the terminal's primary face.
//
// mew never sends OSC 7005, so step 2 is inert there (GetScriptFont == "") and
// scripts resolve through the engine's ui-term-<script> tree as before.
//
// The script is the CELL's, not the base rune's. A script-neutral base carrying
// a combining mark of some script — a dotted circle anchoring an isolated
// Hebrew point, most of all — belongs to the MARK's script, because the two have
// to shape as one cluster to compose at all. Resolve the base to the primary
// face and the mark to its script face and the run splits between them: the
// shaper then positions the mark as its own isolated run, at the pen position
// AFTER the base rather than on top of it, which for a one-cell mask means
// clipped away entirely. That is the whole difference between the terminal
// surface (where the host's own fallback composes them) and this one.
func (t *PurfecTerm) cellFamily(buf *purfecterm.Buffer, cell *purfecterm.Cell) string {
	if cell.Font != 0 {
		if fam := buf.GetFontSlot(int(cell.Font)); fam != "" {
			return fam
		}
	}
	cls := purfecterm.ScriptClass(cell.Char)
	if cls == "" {
		cls = combiningScriptClass(cell.Combining)
		if cls != "" {
			// The base has no script of its own, so nothing competes: name the
			// mark's script face outright rather than leaving the cluster to
			// per-rune fallback, which is what splits the run.
			if fam := buf.GetScriptFont(cls); fam != "" {
				return fam
			}
			return "ui-term-" + cls
		}
	}
	if cls != "" {
		if fam := buf.GetScriptFont(cls); fam != "" {
			return fam
		}
	}
	return t.primaryTermFamily()
}

// combiningScriptClass returns the script class of the first combining mark in
// a cell that has one, or "" when the marks are all script-neutral (or there are
// none). Marks of several scripts on one base is ill-formed text; the first one
// decides, since some face has to.
func combiningScriptClass(combining string) string {
	for _, r := range combining {
		if cls := purfecterm.ScriptClass(r); cls != "" {
			return cls
		}
	}
	return ""
}

// cellTextImage rasterizes one cell's glyph into a color-independent COVERAGE
// mask (white ink, alpha = coverage) at an exact device-pixel box, applying the
// gtk stretch/center rules, and caches it keyed WITHOUT color. Callers tint the
// mask with the cell's foreground at draw time (DrawImageMaskTintOffset), so a
// glyph shown in many colors rasterizes once and re-tints per cell.
func (t *PurfecTerm) cellTextImage(str, family string, bold, italic bool, boxWPx, boxHPx int, ppu float64, wideCell bool, ch rune, kashL, kashR bool, actx *arabicCellShape) *image.RGBA {
	if boxWPx <= 0 || boxHPx <= 0 {
		return nil
	}
	if ppu <= 0 {
		ppu = 1
	}
	if family == "" {
		family = t.primaryTermFamily()
	}

	// A live font change (SetFontAlias / register) bumps the engine epoch: the
	// coverage masks were rasterized against the OLD font set, so flush them.
	if eng := t.gfxEngine(); eng != nil {
		if ep := eng.Epoch(); ep != t.gfx.fontEpoch {
			t.gfx.fontEpoch = ep
			t.gfx.textCur = map[coverMaskKey]*image.RGBA{}
			t.gfx.textPrev = map[coverMaskKey]*image.RGBA{}
		}
	}

	key := coverMaskKey{str: str, family: family, bold: bold, italic: italic, wPx: boxWPx, hPx: boxHPx, wide: wideCell, kashL: kashL, kashR: kashR}
	if img, ok := t.gfx.textCur[key]; ok {
		return img
	}
	if img, ok := t.gfx.textPrev[key]; ok {
		t.gfx.textCur[key] = img
		return img
	}
	// Choose the point size whose line budget fills the box height. The box
	// is device pixels at ppu, so dividing it back out gives the cell height
	// in units; the point size then follows the renderer's font_size.
	sizeUnits := int(math.Round(float64(boxHPx) / ppu))
	pt := sizeUnits * 3 / 4
	if pt < 1 {
		pt = 1
	}
	var fs core.FontStyle
	if bold {
		fs |= core.FontStyleBold
	}
	if italic {
		fs |= core.FontStyleItalic
	}
	f := &core.Font{Name: family, Size: pt, Style: fs}

	eng := t.gfxEngine()
	sp := eng.ShapeRun(f, str)
	naturalW := int(math.Round(float64(sp.Width()) * ppu))
	// The raster must hold the face's REAL ink, not just its nominal line
	// budget. emFor sizes a face so ascent+descent+gap fills the budget, but
	// the per-run bounds each round up independently and land a unit or two
	// either side of it — Noto Naskh reports 18 against a 16-unit budget, and
	// a budget-sized raster simply cut its descenders off.
	lineH := eng.LineHeight(f)
	if len(sp.Lines) > 0 {
		if ink := sp.Lines[0].Ascent + sp.Lines[0].Descent + sp.Lines[0].Gap; ink > lineH {
			lineH = ink
		}
	}
	naturalH := int(math.Round(float64(lineH) * ppu))
	if naturalW <= 0 {
		naturalW = 1
	}
	if naturalH <= 0 {
		naturalH = 1
	}
	// Baseline alignment: each face splits its line budget between ascent and
	// descent however its designer chose, so its baseline sits at its own
	// height in the box — Noto Kufi's shallow ascent (9 of 16) parks it high,
	// Noto Naskh's (11) lower, Latin's and Noto Serif Hebrew's (13) lower
	// still. Left alone every script rides at a different level in the row.
	// Shift the finished mask so this face's baseline lands where the PRIMARY
	// terminal face puts its own. A translation of whole pixels: the glyph
	// keeps its size and shape, it only moves.
	yShift := t.baselineShiftPx(family, ch, pt, fs, sp, ppu)
	raw := image.NewRGBA(image.Rect(0, 0, naturalW, naturalH))
	// Rasterize a WHITE glyph at the renderer's font_size-aware pixels-per-unit:
	// the result is a color-independent coverage mask (alpha = ink coverage);
	// the draw path tints it with the cell's foreground.
	text.Render(raw, sp, 0, 0, ppu, color.RGBA{255, 255, 255, 255})

	// Stretch/center per the gtk rules.
	out := image.NewRGBA(image.Rect(0, 0, boxWPx, boxHPx))
	if actx != nil {
		// Arabic: str is the shaping window (prev + tatweels + letter + tatweels
		// + next, joining sides only), shaped above as ONE run so the font's own
		// GSUB produced the true joined forms with real connecting strokes. The
		// cell keeps the slice between the neighbour letters — the letter WITH
		// its tatweel connectors INCLUDED — at NATURAL size (no horizontal
		// scaling, vertical exactly as every other cell): the letter is centred
		// in the cell and the tatweel runs simply continue to the cell edges,
		// where the crop cuts them mid-stroke to meet the neighbouring cells'
		// strokes. The window carries more tatweel than a cell can need, so the
		// stroke never runs out before the boundary; neighbour letters and
		// surplus tatweel fall outside the cell and are clipped.
		keep0, keep1 := actx.seg0, actx.seg1
		if actx.rt0 >= 0 {
			keep0 = actx.rt0 // include the right-side tatweels; drop prev
		}
		if actx.lt0 >= 0 {
			keep1 = actx.lt1 // include the left-side tatweels; drop next
		}
		// The keep range in window px: everything between the neighbour
		// letters (the letter with its tatweel runs).
		k0, k1 := 0, raw.Rect.Dx()
		if u0, u1, ok := sp.RuneSpanX(keep0, keep1); ok {
			k0 = int(math.Floor(u0 * ppu))
			k1 = int(math.Ceil(u1 * ppu))
		}
		// The letter's centre in window px.
		center := float64(k0+k1) / 2
		if b0, b1, ok := sp.RuneSpanX(actx.seg0, actx.seg1); ok {
			center = (b0 + b1) / 2 * ppu
		}
		// The cell's mask is a slice of the joined window EXACTLY one cell
		// wide, centred on the letter and bounded by the keep range. The
		// joined window's baseline is continuous through the letter and its
		// tatweels (the embedded archive faces substitute true contextual
		// forms), so wherever the cut lands — mid-tatweel when the cell is
		// wider than the letter, mid-letter when it is narrower — both cut
		// ends carry baseline ink, and adjacent cells meet at their shared
		// boundary. Each cell is fully self-contained (no overflow into
		// neighbours), so partial repaints and background fills can never
		// erase a join. A side with no join runs out of keep range and ends
		// naturally. No scaling in either axis beyond the standard
		// height-to-box treatment.
		lo := int(math.Round(center - float64(boxWPx)/2))
		s0, s1 := lo, lo+boxWPx
		// A JOINING side is never cropped short of the cell edge: the slice
		// keeps the full half-cell of the shaped window on that side — the
		// tatweel run, and past it the junction toward the neighbour letter —
		// whatever ink is there, so the cell's ink always reaches the edge.
		// Only a side with NO join is trimmed back to the keep range, so an
		// isolated or final edge keeps its natural gap.
		if actx.lt0 < 0 && s0 < k0 {
			s0 = k0
		}
		if actx.rt0 < 0 && s1 > k1 {
			s1 = k1
		}
		slice := cropCols(raw, s0, s1)
		if slice == nil {
			slice = raw
			s0, lo = 0, 0
		}
		compositeInto(out, slice, s0-lo, yShift)
		if actx.rt0 >= 0 || actx.lt0 >= 0 {
			// One-time geometry report for the first joining cell: every term
			// of the sizing equation at RUNTIME, plus whether the finished
			// mask's ink really reaches the joining cell edges.
			arabicGeomOnce.Do(func() {
				cwU, chU := t.cellDims()
				tf := t.effTermFont()
				tfSize := 0
				if tf != nil {
					tfSize = tf.Size
				}
				fmt.Fprintf(os.Stderr,
					"kittytk: arabic geom: box=%dx%d ppu=%.3f pt=%d cell=%vx%v units termFontSize=%d window=%q rawW=%d rawH=%d keep=[%d,%d) center=%.1f slice=[%d,%d) dstX=%d joinL=%v joinR=%v edgeInk L=%v R=%v\n",
					boxWPx, boxHPx, ppu, f.Size, cwU, chU, tfSize, str, raw.Rect.Dx(), raw.Rect.Dy(), k0, k1, center, s0, s1, s0-lo,
					actx.lt0 >= 0, actx.rt0 >= 0,
					colInked(out, 0), colInked(out, boxWPx-1))
			})
		}
	} else {
		var placed *image.RGBA
		xOff := 0
		// Width rules only: the height stays the raster's own so the baseline
		// shift below lands where it was computed. (Vertical scaling was a
		// no-op in any case — the budget-sized raster already matched the box.)
		switch {
		case naturalW > boxWPx:
			// Squeeze wide glyphs to fit the box.
			placed = scaleRGBA(raw, boxWPx, naturalH)
		case wideCell && purfecterm.IsAmbiguousWidth(ch) && !purfecterm.IsBlockOrLineDrawing(ch):
			// Ambiguous-width char in a wide cell: 1.5x, centered.
			w := naturalW * 3 / 2
			if w > boxWPx {
				w = boxWPx
			}
			placed = scaleRGBA(raw, w, naturalH)
			xOff = (boxWPx - w) / 2
		case wideCell && purfecterm.IsBlockOrLineDrawing(ch):
			// Block/line drawing stretches to connect.
			placed = scaleRGBA(raw, boxWPx, naturalH)
		default:
			placed = raw
			xOff = (boxWPx - naturalW) / 2
			if xOff < 0 {
				xOff = 0
			}
		}
		compositeInto(out, placed, xOff, yShift)
	}

	if len(t.gfx.textCur) >= gfxCacheMax {
		t.gfx.textPrev = t.gfx.textCur
		t.gfx.textCur = map[coverMaskKey]*image.RGBA{}
	}
	t.gfx.textCur[key] = out
	return out
}

// baselineShiftPx is how far to move a face's mask vertically so its baseline
// lands where the PRIMARY terminal face puts its own — the alignment that makes
// Hebrew and Arabic sit on the same line as Latin rather than each script
// riding at its own face-declared height.
//
// Zero for the primary face itself, and zero whenever the reference cannot be
// shaped (no engine, empty run), so an unknown case falls back to today's
// behaviour rather than a guess.
func (t *PurfecTerm) baselineShiftPx(family string, ch rune, pt int, fs core.FontStyle, sp *text.ShapedParagraph, ppu float64) int {
	if sp == nil || len(sp.Lines) == 0 {
		return 0
	}
	eng := t.gfxEngine()
	if eng == nil {
		return 0
	}
	ref := t.primaryTermFamily()
	if ref == "" {
		return 0
	}
	// Compare BASELINES, not family names. A terminal cell resolves its font
	// once — for mew that is always the primary family — while the engine
	// picks the script face per glyph inside ShapeRun, so sp already reflects
	// the face that will really paint. Gating on the requested name being
	// different from the primary made this a no-op for every cell mew draws.
	refSP := eng.ShapeRun(&core.Font{Name: ref, Size: pt, Style: fs}, "M")
	if refSP == nil || len(refSP.Lines) == 0 {
		return 0
	}
	shift := int(math.Round(float64(refSP.Lines[0].Baseline-sp.Lines[0].Baseline) * ppu))
	// A configured per-face correction rides on top, for a face whose own
	// metrics misplace it however faithfully they are followed. Cell units
	// (1/16 of a row), positive down — so it survives the live font zoom,
	// which the device-pixel conversion here applies.
	// The correction belongs to the face that actually painted, which for a
	// script glyph is the fallback target rather than the requested primary.
	if adj := eng.BaselineAdjust(eng.ScriptFaceFor(family, ch)); adj != 0 {
		shift += int(math.Round(float64(adj) * ppu))
	}
	// A correction larger than the cell is not an alignment, it is a bad
	// metric; leave such a face where it is rather than launching it out of
	// the row.
	limit := int(math.Round(float64(eng.LineHeight(&core.Font{Name: ref, Size: pt})) * ppu))
	if limit > 0 {
		if shift > limit {
			shift = limit
		}
		if shift < -limit {
			shift = -limit
		}
	}
	return shift
}

// canonicalFamily lowercases and strips spaces/hyphens so "Noto Sans Mono",
// "noto-sans-mono" and "notosansmono" compare equal — the same leniency the
// font database applies to alias names.
func canonicalFamily(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r != ' ' && r != '-' && r != '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// drawCellText renders one cell's character with all scaling rules,
// including double-width/height lines (top/bottom halves clipped).
func (t *PurfecTerm) drawCellText(p *core.Painter, cell *purfecterm.Cell, family string, fg purfecterm.Color,
	cellX, cellY, cellW, cellH float64, lineAttr purfecterm.LineAttribute, ppu float64, yOffPx int, cellVisualWidth float64, kashL, kashR bool, actx *arabicCellShape) {

	str := cell.String()
	if actx != nil {
		str = actx.s // Arabic: the five-piece shaping window (also the cache key)
		arabicDiagOnce.Do(func() {
			if eng := t.gfxEngine(); eng != nil {
				fmt.Fprintf(os.Stderr, "kittytk: %s\n", eng.ArabicJoinDiag(family))
			}
		})
	}
	// The mask box must be EXACTLY the painted purfecterm cell rect. Cell
	// rects are painted (fillPixels) as [round(x0*ppu), round(x1*ppu)) — at
	// fractional ppu (font_size not a multiple of 12) that differs from
	// round(width*ppu) by a pixel, so the mask must use the same edge math or
	// it is measurably narrower/shorter than the cell it fills and Arabic
	// joins fall short of the boundary.
	xPx := int(math.Round(cellX * ppu))
	yPx0 := int(math.Round(cellY * ppu))
	boxW := int(math.Round((cellX+cellW)*ppu)) - xPx
	contentH := int(math.Round((cellY+cellH)*ppu)) - yPx0
	yPx := yPx0 + yOffPx
	wide := cellVisualWidth > 1.0

	frgb := pcRGBA(fg)
	switch lineAttr {
	case purfecterm.LineAttrDoubleWidth:
		// Double width, single height. cellTextImage takes its point size from
		// the box HEIGHT alone, so widening the box on its own only centres an
		// ordinary glyph in it — which is what a DECDWL heading looked like.
		//
		// Ask for a 2x-TALL mask instead: that doubles the point size, and
		// since the box is already the doubled cell width the bigger glyph
		// fills it. Averaging the mask back down to one row keeps the full
		// horizontal detail of the 2x rasterization, which is what makes a
		// doubled heading read as drawn at that size rather than magnified.
		//
		// (The alternative — rasterize at the ORDINARY size and repeat each
		// column — reproduces the normal row's pixels exactly, so it cannot
		// lose density, but it widens by nearest-neighbour and gives up the
		// extra outline detail the 2x render carries. Kept under
		// KITTYTK_DWL=widen; judged by eye, the 2x render looks better.)
		mask := t.dwlDoubleWideMask(str, family, cell, boxW, contentH, ppu, wide, kashL, kashR, actx)
		if mask == nil {
			return
		}
		p.DrawImageMaskTintOffset(0, 0, xPx, yPx, mask, frgb.R, frgb.G, frgb.B)
	case purfecterm.LineAttrDoubleTop, purfecterm.LineAttrDoubleBottom:
		// Rendered at 2x height; only one half shows through the clip.
		mask := t.cellTextImage(str, family, cell.Bold, cell.Italic, boxW, contentH*2, ppu, wide, cell.Char, kashL, kashR, actx)
		if mask == nil {
			return
		}
		clip := p.WithClip(core.UnitRect{
			X: core.Unit(math.Round(cellX)), Y: core.Unit(math.Round(cellY)),
			Width: core.Unit(math.Round(cellW)), Height: core.Unit(math.Round(cellH)),
		})
		if lineAttr == purfecterm.LineAttrDoubleBottom {
			yPx -= contentH
		}
		clip.DrawImageMaskTintOffset(0, 0, xPx, yPx, mask, frgb.R, frgb.G, frgb.B)
	default:
		mask := t.cellTextImage(str, family, cell.Bold, cell.Italic, boxW, contentH, ppu, wide, cell.Char, kashL, kashR, actx)
		if mask == nil {
			return
		}
		p.DrawImageMaskTintOffset(0, 0, xPx, yPx, mask, frgb.R, frgb.G, frgb.B)
	}
}

// dwlWiden selects the alternative double-width strategy: rasterize at the
// ordinary size and repeat each column, reproducing the normal row's pixels
// exactly rather than resampling. Kept for side-by-side comparison
// (KITTYTK_DWL=widen); the default rasterizes at 2x for the finer outline.
var dwlWiden = strings.Contains(os.Getenv("KITTYTK_DWL"), "widen")

// dwlDoubleWideMask builds the glyph mask for one DECDWL cell, filling a box
// twice the ordinary cell width.
func (t *PurfecTerm) dwlDoubleWideMask(str, family string, cell *purfecterm.Cell,
	boxW, contentH int, ppu float64, wide, kashL, kashR bool, actx *arabicCellShape) *image.RGBA {

	if dwlWiden {
		// The ordinary-size raster, in the ordinary cell width — bit for bit
		// what this glyph looks like on a normal row — widened by repeating
		// columns, so no pixel is re-derived.
		natural := boxW / 2
		if natural < 1 {
			natural = 1
		}
		mask := t.cellTextImage(str, family, cell.Bold, cell.Italic, natural, contentH, ppu, wide, cell.Char, kashL, kashR, actx)
		if mask == nil {
			return nil
		}
		return widenCols(mask, boxW)
	}

	mask := t.cellTextImage(str, family, cell.Bold, cell.Italic, boxW, contentH*2, ppu, wide, cell.Char, kashL, kashR, actx)
	if mask == nil {
		return nil
	}
	return squashRowsRGBA(mask, contentH)
}

// widenCols stretches an image to dstW columns by repeating source columns —
// an exact column doubling at the 2:1 ratio double-width asks for. No filter:
// every output pixel is a source pixel at its original intensity, so the
// glyph's density and vertical crispness survive the widening untouched.
func widenCols(src *image.RGBA, dstW int) *image.RGBA {
	srcW, h := src.Rect.Dx(), src.Rect.Dy()
	if srcW <= 0 || h <= 0 || dstW <= srcW {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, h))
	for x := 0; x < dstW; x++ {
		sx := x * srcW / dstW
		if sx >= srcW {
			sx = srcW - 1
		}
		for y := 0; y < h; y++ {
			si := src.PixOffset(src.Rect.Min.X+sx, src.Rect.Min.Y+y)
			di := dst.PixOffset(x, y)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}

// squashRowsRGBA box-filters an image down to dstH rows, averaging each output
// row over the source rows it covers. Used to bring a 2x-tall glyph mask back
// to one row for a double-width line: nearest-neighbour would drop every other
// scanline and alias the stroke weights, which is exactly what a doubled
// heading must not do.
func squashRowsRGBA(src *image.RGBA, dstH int) *image.RGBA {
	w, srcH := src.Rect.Dx(), src.Rect.Dy()
	if w <= 0 || srcH <= 0 || dstH <= 0 || srcH == dstH {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, dstH))
	for y := 0; y < dstH; y++ {
		y0 := y * srcH / dstH
		y1 := (y + 1) * srcH / dstH
		if y1 <= y0 {
			y1 = y0 + 1
		}
		if y1 > srcH {
			y1 = srcH
		}
		n := y1 - y0
		for x := 0; x < w; x++ {
			var r, g, b, a int
			for sy := y0; sy < y1; sy++ {
				si := src.PixOffset(src.Rect.Min.X+x, src.Rect.Min.Y+sy)
				r += int(src.Pix[si])
				g += int(src.Pix[si+1])
				b += int(src.Pix[si+2])
				a += int(src.Pix[si+3])
			}
			o := dst.PixOffset(x, y)
			dst.Pix[o] = uint8(r / n)
			dst.Pix[o+1] = uint8(g / n)
			dst.Pix[o+2] = uint8(b / n)
			dst.Pix[o+3] = uint8(a / n)
		}
	}
	return dst
}

// scaleRGBA nearest-neighbor scales an image (glyph stretch for
// double-width lines, screen scale modes, flex cells).
func scaleRGBA(src *image.RGBA, w, h int) *image.RGBA {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	if w <= 0 || h <= 0 || sw <= 0 || sh <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	if sw == w && sh == h {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		sy := y * sh / h
		for x := 0; x < w; x++ {
			dst.SetRGBA(x, y, src.RGBAAt(x*sw/w, sy))
		}
	}
	return dst
}

// colInked reports whether column x of img has any inked pixel.
func colInked(img *image.RGBA, x int) bool {
	b := img.Bounds()
	if x < b.Min.X || x >= b.Max.X {
		return false
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		if img.RGBAAt(x, y).A != 0 {
			return true
		}
	}
	return false
}

func compositeInto(dst, src *image.RGBA, xOff, yOff int) {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			c := src.RGBAAt(x, y)
			if c.A != 0 && xOff+x < dst.Rect.Dx() && yOff+y < dst.Rect.Dy() {
				dst.SetRGBA(xOff+x, yOff+y, c)
			}
		}
	}
}

// cropCols returns the column slice [x0,x1) of src at full height, or nil if
// the clamped range is empty. Used to cut one cluster's pixels out of a shaped
// Arabic window.
func cropCols(src *image.RGBA, x0, x1 int) *image.RGBA {
	if x0 < 0 {
		x0 = 0
	}
	if x1 > src.Rect.Dx() {
		x1 = src.Rect.Dx()
	}
	if x1 <= x0 {
		return nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, x1-x0, src.Rect.Dy()))
	compositeInto(dst, src, -x0, 0)
	return dst
}

// ---------------------------------------------------------------
// Underline styles, strikethrough
// ---------------------------------------------------------------

func (t *PurfecTerm) drawCellDecorations(p *core.Painter, buf *purfecterm.Buffer, cell *purfecterm.Cell,
	scheme purfecterm.ColorScheme, isDark bool, fg purfecterm.Color,
	cellX, cellY, cellW, cellH float64, lineAttr purfecterm.LineAttribute, ppu float64) {

	if cell.UnderlineStyle != purfecterm.UnderlineNone {
		ulColor := fg
		if cell.HasUnderlineColor {
			ulColor = scheme.ResolveColor(cell.UnderlineColor, true, isDark)
		}
		underlineY := cellY + cellH - 2
		lineH := 1.0
		if lineAttr == purfecterm.LineAttrDoubleTop || lineAttr == purfecterm.LineAttrDoubleBottom {
			lineH = 2.0
		}
		switch cell.UnderlineStyle {
		case purfecterm.UnderlineSingle:
			fillPixels(p, cellX, underlineY, cellX+cellW, underlineY+lineH, ppu, ulColor)
		case purfecterm.UnderlineDouble:
			fillPixels(p, cellX, underlineY-2, cellX+cellW, underlineY-2+lineH, ppu, ulColor)
			fillPixels(p, cellX, underlineY+1, cellX+cellW, underlineY+1+lineH, ppu, ulColor)
		case purfecterm.UnderlineCurly:
			numCycles := 2.0
			if cell.CellWidth >= 2.0 {
				numCycles = 4.0
			}
			amplitude := 1.5 * lineH
			steps := int(cellW / 2)
			if steps < 4 {
				steps = 4
			}
			for s := 0; s <= steps; s++ {
				tt := float64(s) / float64(steps)
				x := cellX + tt*cellW
				y := underlineY + amplitude*math.Sin(tt*numCycles*2*math.Pi)
				fillPixels(p, x, y, x+cellW/float64(steps)+0.5, y+lineH, ppu, ulColor)
			}
		case purfecterm.UnderlineDotted:
			dotSpacing := 3.0 * lineH
			for x := cellX; x < cellX+cellW; x += dotSpacing {
				fillPixels(p, x, underlineY, x+lineH, underlineY+lineH, ppu, ulColor)
			}
		case purfecterm.UnderlineDashed:
			dashLen := 4.0 * lineH
			gapLen := 2.0 * lineH
			for x := cellX; x < cellX+cellW; x += dashLen + gapLen {
				endX := math.Min(x+dashLen, cellX+cellW)
				fillPixels(p, x, underlineY, endX, underlineY+lineH, ppu, ulColor)
			}
		}
	}

	if cell.Strikethrough {
		strikeY := cellY + cellH*0.4
		strikeH := 1.0
		if lineAttr == purfecterm.LineAttrDoubleTop || lineAttr == purfecterm.LineAttrDoubleBottom {
			strikeH = 2.0
		}
		fillPixels(p, cellX, strikeY, cellX+cellW, strikeY+strikeH, ppu, fg)
	}
}

// ---------------------------------------------------------------
// Cursor overlays (shape + focus states)
// ---------------------------------------------------------------

// drawCursorOverlay paints the cursor's non-swap forms: the hollow
// box outline when the terminal is unfocused or its window inactive
// (the pre-existing inactive caret, kept working), and the underline
// and bar shapes with their thinner unfocused variants. All device-
// pixel exact via cached overlay images.
func (t *PurfecTerm) drawCursorOverlay(p *core.Painter, scheme purfecterm.ColorScheme, focused bool,
	shape int, cellX, cellY, cellW, cellH float64, ppu float64) {

	wPx := int(math.Round(cellW * ppu))
	hPx := int(math.Round(cellH * ppu))
	xPx := int(math.Round(cellX * ppu))
	yPx := int(math.Round(cellY * ppu))
	c := pcRGBA(scheme.Cursor)

	var img *image.RGBA
	switch shape {
	case 0: // block
		if focused {
			return // handled by the fg/bg swap in the cell loop
		}
		img = t.overlayImage(fmt.Sprintf("box|%d|%d|%02x%02x%02x", wPx, hPx, c.R, c.G, c.B), wPx, hPx, func(img *image.RGBA) {
			for x := 0; x < wPx; x++ {
				img.SetRGBA(x, 0, c)
				img.SetRGBA(x, hPx-1, c)
			}
			for y := 0; y < hPx; y++ {
				img.SetRGBA(0, y, c)
				img.SetRGBA(wPx-1, y, c)
			}
		})
	case 1: // underline: 1/4 height focused, 1/6 unfocused
		th := hPx / 4
		if !focused {
			th = hPx / 6
		}
		if th < 1 {
			th = 1
		}
		yPx += hPx - th
		img = t.overlayImage(fmt.Sprintf("ul|%d|%d|%02x%02x%02x", wPx, th, c.R, c.G, c.B), wPx, th, func(img *image.RGBA) {
			for y := 0; y < th; y++ {
				for x := 0; x < wPx; x++ {
					img.SetRGBA(x, y, c)
				}
			}
		})
	case 2: // bar: 2px focused, 1px unfocused
		th := 2
		if !focused {
			th = 1
		}
		img = t.overlayImage(fmt.Sprintf("bar|%d|%d|%02x%02x%02x", th, hPx, c.R, c.G, c.B), th, hPx, func(img *image.RGBA) {
			for y := 0; y < hPx; y++ {
				for x := 0; x < th; x++ {
					img.SetRGBA(x, y, c)
				}
			}
		})
	}
	if img != nil {
		p.DrawImageOffset(0, 0, xPx, yPx, img)
	}
}

func (t *PurfecTerm) overlayImage(key string, w, h int, draw func(*image.RGBA)) *image.RGBA {
	if w <= 0 || h <= 0 {
		return nil
	}
	if img, ok := t.gfx.overlayCur[key]; ok {
		return img
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw(img)
	if len(t.gfx.overlayCur) >= 256 {
		t.gfx.overlayCur = map[string]*image.RGBA{}
	}
	t.gfx.overlayCur[key] = img
	return img
}

// ---------------------------------------------------------------
// Custom glyphs
// ---------------------------------------------------------------

// renderCustomGlyphCell draws a custom glyph for a cell if one is
// defined for its rune. Mirrors the gtk path: cached pre-rendered
// images keyed by purfecterm.GlyphCacheKey, seam extension, flips,
// double-height clipping.
func (t *PurfecTerm) renderCustomGlyphCell(p *core.Painter, buf *purfecterm.Buffer, cell *purfecterm.Cell,
	cellX, cellY, cellW, cellH float64, lineAttr purfecterm.LineAttribute, ppu float64, yOffPx int) bool {

	glyph := buf.GetGlyph(cell.Char)
	if glyph == nil || glyph.Width == 0 || glyph.Height == 0 {
		return false
	}

	renderY := cellY
	scaleY := 1.0
	clipNeeded := false
	switch lineAttr {
	case purfecterm.LineAttrDoubleTop:
		scaleY, clipNeeded = 2.0, true
	case purfecterm.LineAttrDoubleBottom:
		scaleY, clipNeeded = 2.0, true
		renderY = cellY - cellH
	}

	wPx := int(math.Round(cellW * ppu))
	hPx := int(math.Round(cellH * scaleY * ppu))

	// Palette identity for the cache key.
	paletteNum := cell.BGP
	if paletteNum < 0 {
		paletteNum = buf.ColorToANSICode(cell.Foreground)
	}
	palette := buf.GetPalette(paletteNum)
	var paletteHash uint64
	if palette != nil {
		paletteHash = palette.ComputeHash()
	}

	key := purfecterm.GlyphCacheKey{
		Rune: cell.Char, Width: int16(wPx), Height: int16(hPx),
		IsCustomGlyph: true, XFlip: cell.XFlip, YFlip: cell.YFlip,
		PaletteHash: paletteHash, GlyphHash: glyph.ComputeHash(),
	}
	// Resolved colors participate (default-FG palettes, transparent
	// entries falling back to cell bg).
	if fgc, ok := buf.ResolveGlyphColor(cell, 1); ok {
		key.FgR, key.FgG, key.FgB = fgc.R, fgc.G, fgc.B
	}
	if bgc, ok := buf.ResolveGlyphColor(cell, 0); ok {
		key.BgR, key.BgG, key.BgB = bgc.R, bgc.G, bgc.B
	}

	img, ok := t.gfx.glyphCur[key]
	if !ok {
		if img, ok = t.gfx.glyphPrev[key]; ok {
			t.gfx.glyphCur[key] = img
		}
	}
	if !ok {
		img = t.buildCustomGlyphImage(buf, cell, glyph, wPx, hPx)
		if len(t.gfx.glyphCur) >= gfxCacheMax {
			t.gfx.glyphPrev = t.gfx.glyphCur
			t.gfx.glyphCur = map[purfecterm.GlyphCacheKey]*image.RGBA{}
		}
		t.gfx.glyphCur[key] = img
	}

	xPx := int(math.Round(cellX * ppu))
	yPx := int(math.Round(renderY*ppu)) + yOffPx
	target := p
	if clipNeeded {
		target = p.WithClip(core.UnitRect{
			X: core.Unit(math.Round(cellX)), Y: core.Unit(math.Round(cellY)),
			Width: core.Unit(math.Round(cellW)), Height: core.Unit(math.Round(cellH)),
		})
	}
	target.DrawImageOffset(0, 0, xPx, yPx, img)
	return true
}

// buildCustomGlyphImage rasterizes a custom glyph into wPx x hPx,
// with flips and seam extension (adjacent non-transparent pixels
// overlap by one device pixel to hide scaling seams).
func (t *PurfecTerm) buildCustomGlyphImage(buf *purfecterm.Buffer, cell *purfecterm.Cell, glyph *purfecterm.CustomGlyph, wPx, hPx int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, wPx, hPx))
	gw, gh := glyph.Width, glyph.Height
	pixelW := float64(wPx) / float64(gw)
	pixelH := float64(hPx) / float64(gh)

	for gy := 0; gy < gh; gy++ {
		for gx := 0; gx < gw; gx++ {
			paletteIdx := glyph.GetPixel(gx, gy)
			drawX, drawY := gx, gy
			if cell.XFlip {
				drawX = gw - 1 - gx
			}
			if cell.YFlip {
				drawY = gh - 1 - gy
			}
			px := float64(drawX) * pixelW
			py := float64(drawY) * pixelH
			drawW, drawH := pixelW, pixelH
			if glyph.GetPixel(gx+1, gy) != 0 {
				drawW++
			}
			if glyph.GetPixel(gx, gy+1) != 0 {
				drawH++
			}
			c, _ := buf.ResolveGlyphColor(cell, paletteIdx)
			rc := pcRGBA(c)
			x0, y0 := int(px), int(py)
			x1, y1 := int(px+drawW), int(py+drawH)
			for y := y0; y < y1 && y < hPx; y++ {
				for x := x0; x < x1 && x < wPx; x++ {
					img.SetRGBA(x, y, rc)
				}
			}
		}
	}
	return img
}

// ---------------------------------------------------------------
// Sprites
// ---------------------------------------------------------------

// spriteCoordToPx converts a sprite coordinate (subdivision units)
// to device pixels without accumulating rounding error.
func spriteCoordToPx(coordinate float64, unitsPerCell int, cellPx float64) float64 {
	wholeCells := int(coordinate) / unitsPerCell
	remainder := coordinate - float64(wholeCells*unitsPerCell)
	return float64(wholeCells)*cellPx + remainder*cellPx/float64(unitsPerCell)
}

func (t *PurfecTerm) renderSpritesGfx(p *core.Painter, sprites []*purfecterm.Sprite, buf *purfecterm.Buffer,
	scheme purfecterm.ColorScheme, isDark bool, cw, chh, ppu float64, scrollOffsetY, horizOffsetX int) {

	if len(sprites) == 0 {
		return
	}
	unitX, unitY := buf.GetSpriteUnits()
	cwPx := cw * ppu
	chPx := chh * ppu
	scrollPixelY := float64(scrollOffsetY) * chPx
	scrollPixelX := float64(horizOffsetX) * cwPx
	defaultFg := scheme.Foreground(isDark)
	defaultBg := scheme.Background(isDark)

	for _, sprite := range sprites {
		if sprite == nil || len(sprite.Runes) == 0 {
			continue
		}
		var cropRect *purfecterm.CropRectangle
		if sprite.CropRect >= 0 {
			cropRect = buf.GetCropRect(sprite.CropRect)
		}

		basePixelX := spriteCoordToPx(sprite.X, unitX, cwPx) - scrollPixelX
		basePixelY := spriteCoordToPx(sprite.Y, unitY, chPx) + scrollPixelY

		spriteRows := len(sprite.Runes)
		spriteCols := 0
		for _, row := range sprite.Runes {
			if len(row) > spriteCols {
				spriteCols = len(row)
			}
		}
		tileW := cwPx * sprite.XScale
		tileH := chPx * sprite.YScale
		xFlip, yFlip := sprite.GetXFlip(), sprite.GetYFlip()

		// Compose the whole sprite into one image so fractional
		// positioning is exact; anchor at the floor pixel.
		originX := int(math.Floor(basePixelX))
		originY := int(math.Floor(basePixelY))
		fracX := basePixelX - float64(originX)
		fracY := basePixelY - float64(originY)
		imgW := int(math.Ceil(float64(spriteCols)*tileW+fracX)) + 1
		imgH := int(math.Ceil(float64(spriteRows)*tileH+fracY)) + 1
		if imgW <= 0 || imgH <= 0 || imgW > 8192 || imgH > 8192 {
			continue
		}
		img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))

		for rowIdx, row := range sprite.Runes {
			for colIdx, r := range row {
				if r == 0 || r == ' ' {
					continue
				}
				tileX, tileY := colIdx, rowIdx
				if xFlip {
					tileX = spriteCols - 1 - colIdx
				}
				if yFlip {
					tileY = spriteRows - 1 - rowIdx
				}
				pixelX := fracX + float64(tileX)*tileW
				pixelY := fracY + float64(tileY)*tileH

				glyph := buf.GetGlyph(r)
				if glyph == nil || glyph.Width == 0 || glyph.Height == 0 {
					continue
				}
				gw, gh := glyph.Width, glyph.Height
				pixW := tileW / float64(gw)
				pixH := tileH / float64(gh)
				for gy := 0; gy < gh; gy++ {
					for gx := 0; gx < gw; gx++ {
						paletteIdx := glyph.GetPixel(gx, gy)
						c, visible := buf.ResolveSpriteGlyphColor(sprite.FGP, paletteIdx, defaultFg, defaultBg)
						if !visible {
							continue
						}
						px := pixelX + float64(gx)*pixW
						py := pixelY + float64(gy)*pixH
						drawW, drawH := pixW, pixH
						if glyph.GetPixel(gx+1, gy) != 0 {
							drawW++
						}
						if glyph.GetPixel(gx, gy+1) != 0 {
							drawH++
						}
						rc := pcRGBA(c)
						for y := int(py); y < int(py+drawH) && y < imgH; y++ {
							for x := int(px); x < int(px+drawW) && x < imgW; x++ {
								if x >= 0 && y >= 0 {
									img.SetRGBA(x, y, rc)
								}
							}
						}
					}
				}
			}
		}

		// Crop rect confines the sprite (unit-space clip).
		target := p
		if cropRect != nil {
			cx0 := (spriteCoordToPx(cropRect.MinX, unitX, cwPx) - scrollPixelX) / ppu
			cy0 := (spriteCoordToPx(cropRect.MinY, unitY, chPx) + scrollPixelY) / ppu
			cx1 := (spriteCoordToPx(cropRect.MaxX, unitX, cwPx) - scrollPixelX) / ppu
			cy1 := (spriteCoordToPx(cropRect.MaxY, unitY, chPx) + scrollPixelY) / ppu
			target = p.WithClip(core.UnitRect{
				X: core.Unit(math.Round(cx0)), Y: core.Unit(math.Round(cy0)),
				Width: core.Unit(math.Round(cx1 - cx0)), Height: core.Unit(math.Round(cy1 - cy0)),
			})
		}
		target.DrawImageOffset(0, 0, originX, originY, img)
	}
}

// ---------------------------------------------------------------
// Screen splits
// ---------------------------------------------------------------

// renderSplitsGfx ports the gtk scanline split renderer: splits
// overlay regions of the logical screen with independent buffer
// positions and fine scroll. Returns the max content width found
// (for the horizontal scrollbar).
func (t *PurfecTerm) renderSplitsGfx(p *core.Painter, buf *purfecterm.Buffer, splits []*purfecterm.ScreenSplit,
	scheme purfecterm.ColorScheme, isDark bool, cols, rows int, cw, chh float64, unitX, unitY int, ppu float64, horizOffset int) int {

	maxSplitContentWidth := 0
	widthCrop, _ := buf.GetScreenCrop()
	cropCols := -1
	if widthCrop > 0 {
		cropCols = widthCrop / unitX
	}

	boundaryRow := buf.GetScrollbackBoundaryVisibleRow()
	scrollOffset := buf.GetScrollOffset()
	if scrollOffset > 0 && boundaryRow < 0 {
		return 0
	}
	logicalScreenStartRow := 0
	if boundaryRow > 0 {
		logicalScreenStartRow = boundaryRow
	}
	logicalStartY := float64(logicalScreenStartRow) * chh
	logicalScreenRows := rows - logicalScreenStartRow
	screenHeightUnits := logicalScreenRows * unitY

	cleared := map[int]bool{}
	currentSplitIdx := -1
	var currentSplit *purfecterm.ScreenSplit
	nextSplitBoundary := 0
	splitEndY := screenHeightUnits
	if len(splits) > 0 && splits[0].ScreenY == 0 {
		currentSplitIdx = 0
		currentSplit = splits[0]
		if len(splits) > 1 {
			nextSplitBoundary, splitEndY = splits[1].ScreenY, splits[1].ScreenY
		} else {
			nextSplitBoundary, splitEndY = screenHeightUnits, screenHeightUnits
		}
	} else if len(splits) > 0 {
		nextSplitBoundary = splits[0].ScreenY
	} else {
		nextSplitBoundary = screenHeightUnits
	}

	for y := 0; y < screenHeightUnits; y++ {
		if y >= nextSplitBoundary {
			for i := currentSplitIdx + 1; i < len(splits); i++ {
				if splits[i].ScreenY <= y {
					currentSplitIdx = i
					currentSplit = splits[i]
				} else {
					break
				}
			}
			if currentSplitIdx+1 < len(splits) {
				nextSplitBoundary, splitEndY = splits[currentSplitIdx+1].ScreenY, splits[currentSplitIdx+1].ScreenY
			} else {
				nextSplitBoundary, splitEndY = screenHeightUnits, screenHeightUnits
			}
		}
		if currentSplit == nil || (currentSplit.ScreenY == 0 && currentSplit.BufferRow == 0 && currentSplit.BufferCol == 0 &&
			currentSplit.TopFineScroll == 0 && currentSplit.LeftFineScroll == 0) {
			continue
		}

		startY := logicalStartY + float64(currentSplit.ScreenY)*chh/float64(unitY)
		endY := logicalStartY + float64(splitEndY)*chh/float64(unitY)

		if !cleared[currentSplitIdx] {
			cleared[currentSplitIdx] = true
			fillPixels(p, 0, startY, float64(cols)*cw, endY, ppu, scheme.Background(isDark))
		}

		relativeY := y - currentSplit.ScreenY + currentSplit.TopFineScroll
		if relativeY < 0 || relativeY%unitY != 0 {
			continue
		}
		rowInSplit := relativeY / unitY
		fineOffsetY := float64(currentSplit.TopFineScroll) * chh / float64(unitY)
		fineOffsetX := float64(currentSplit.LeftFineScroll) * cw / float64(unitX)
		rowY := logicalStartY + float64(y)*chh/float64(unitY) - fineOffsetY

		clip := p.WithClip(core.UnitRect{
			X: 0, Y: core.Unit(math.Round(startY)),
			Width:  core.Unit(math.Round(float64(cols) * cw)),
			Height: core.Unit(math.Round(endY - startY)),
		})

		lineAttr := buf.GetLineAttributeForSplit(rowInSplit, currentSplit.BufferRow)
		effectiveCols := cols
		lineMul := 1.0
		if lineAttr != purfecterm.LineAttrNormal {
			effectiveCols = cols / 2
			lineMul = 2.0
		}
		contentLen := buf.GetLineLengthForSplit(rowInSplit, currentSplit.BufferRow, currentSplit.BufferCol)
		maxRenderCol := effectiveCols
		if contentLen < maxRenderCol {
			maxRenderCol = contentLen
		}
		if cropCols > 0 && cropCols < maxRenderCol {
			maxRenderCol = cropCols
		}
		rowContentWidth := contentLen
		if cropCols > 0 && cropCols < rowContentWidth {
			rowContentWidth = cropCols
		}
		if rowContentWidth > maxSplitContentWidth {
			maxSplitContentWidth = rowContentWidth
		}

		for screenCol := 0; screenCol < maxRenderCol; screenCol++ {
			cell := buf.GetCellForSplit(screenCol+horizOffset, rowInSplit, currentSplit.BufferRow, currentSplit.BufferCol)
			cellX := float64(screenCol)*cw*lineMul - fineOffsetX
			cellW := cw * lineMul
			if cellX >= float64(cols)*cw {
				break
			}
			if cellX+cellW <= 0 {
				continue
			}
			fg := scheme.ResolveColor(cell.Foreground, true, isDark)
			bg := scheme.ResolveColor(cell.Background, false, isDark)
			if bg != scheme.Background(isDark) {
				fillPixels(clip, cellX, rowY, cellX+cellW, rowY+chh, ppu, bg)
			}
			if cell.Char != ' ' && cell.Char != 0 {
				// Arabic contextual joining from the split-view neighbors.
				var leftCh, rightCh rune
				if screenCol+horizOffset > 0 {
					leftCh = buf.GetCellForSplit(screenCol+horizOffset-1, rowInSplit, currentSplit.BufferRow, currentSplit.BufferCol).Char
				}
				rightCh = buf.GetCellForSplit(screenCol+horizOffset+1, rowInSplit, currentSplit.BufferRow, currentSplit.BufferCol).Char
				shaped, suppress := purfecterm.ShapeArabicCellVisual(leftCh, cell.Char, rightCh)
				if !suppress {
					baseC := arabicBaseChar(cell.Char)
					baseL := arabicBaseChar(leftCh)
					baseR := arabicBaseChar(rightCh)
					kashL, kashR := arabicKashida(baseC, baseL, baseR)
					dc := cell
					var actx *arabicCellShape
					if purfecterm.ScriptClass(cell.Char) == "arabic" {
						actx = arabicRenderContext(baseC, shaped, baseL, baseR, kashL, kashR, []rune(cell.Combining))
					} else {
						dc.Char = shaped
					}
					t.drawCellText(clip, &dc, t.cellFamily(buf, &cell), fg, cellX, rowY, cellW, chh, lineAttr, ppu, 0, 1.0, kashL, kashR, actx)
				}
			}
		}

		nextRowY := y + unitY - (relativeY % unitY)
		if nextRowY > y+1 && nextRowY < splitEndY {
			y = nextRowY - 1
		}
	}
	return maxSplitContentWidth
}

// ---------------------------------------------------------------
// Blink timer
// ---------------------------------------------------------------

func (t *PurfecTerm) findDesktop() *Desktop {
	return findDesktopFor(t)
}

// findDesktopFor walks a trinket's ancestry to the hosting Desktop
// (nil when detached or on a plain surface).
func findDesktopFor(w core.Trinket) *Desktop {
	current := w.Parent()
	for current != nil {
		if d, ok := current.(*Desktop); ok {
			return d
		}
		ww, ok := current.(core.Trinket)
		if !ok {
			break
		}
		current = ww.Parent()
	}
	return nil
}

// ensureBlinkTimer starts the 50ms animation timer (bobbing wave
// phase + cursor blink), mirroring the gtk reference.
func (t *PurfecTerm) ensureBlinkTimer() {
	if t.gfx.blinkTimer != nil || t.terminal == nil {
		return
	}
	d := t.findDesktop()
	if d == nil {
		return
	}
	t.gfx.cursorBlinkOn = true
	t.gfx.blinkTimer = d.StartRepeatingTimer(50*time.Millisecond, func() {
		t.gfx.blinkPhase += 0.21 // ~1.5s wave cycle
		if t.gfx.blinkPhase > 2*math.Pi {
			t.gfx.blinkPhase -= 2 * math.Pi
		}
		t.gfx.blinkTick++
		_, cursorBlink := t.terminal.Buffer().GetCursorStyle()
		if cursorBlink > 0 && t.gfxFocused() {
			ticksNeeded := 10 // slow blink ~500ms
			if cursorBlink >= 2 {
				ticksNeeded = 5 // fast blink ~250ms
			}
			if t.gfx.blinkTick >= ticksNeeded {
				t.gfx.blinkTick = 0
				t.gfx.cursorBlinkOn = !t.gfx.cursorBlinkOn
			}
		} else if !t.gfx.cursorBlinkOn {
			t.gfx.cursorBlinkOn = true
		}
		t.Update()
	})
}

// resetCursorBlink makes the cursor immediately visible and restarts
// its blink phase (called on key input).
func (t *PurfecTerm) resetCursorBlink() {
	t.gfx.blinkTick = 0
	if !t.gfx.cursorBlinkOn {
		t.gfx.cursorBlinkOn = true
		t.Update()
	}
}

func (t *PurfecTerm) stopGfxTimers() {
	if t.gfx.blinkTimer != nil {
		t.gfx.blinkTimer.Stop()
		t.gfx.blinkTimer = nil
	}
	if t.gfx.autoTimer != nil {
		t.gfx.autoTimer.Stop()
		t.gfx.autoTimer = nil
	}
}

func (t *PurfecTerm) rotateGfxCaches() {
	if t.gfx.textCur == nil {
		t.gfx.textCur = map[coverMaskKey]*image.RGBA{}
		t.gfx.textPrev = map[coverMaskKey]*image.RGBA{}
		t.gfx.glyphCur = map[purfecterm.GlyphCacheKey]*image.RGBA{}
		t.gfx.glyphPrev = map[purfecterm.GlyphCacheKey]*image.RGBA{}
		t.gfx.overlayCur = map[string]*image.RGBA{}
	}
}

// ---------------------------------------------------------------
// Scrollbars (overlay lanes)
// ---------------------------------------------------------------

// pxRect is a rectangle in RENDER PIXELS — the terminal's own device-pixel
// space, ppu times its units, the same space its cells are laid out in. The
// scrollbars live here so they can sit flush against the trinket's far
// edges and move at pixel granularity: unit rectangles quantize to the
// HOST's cell grid on the way to the screen, which is what marched a
// dragged thumb in cell-sized lurches and, in a hosted terminal at some
// zooms, snapped the vertical lane clean past the clip so no bar showed
// at all.
type pxRect struct{ X, Y, W, H float64 }

func (r pxRect) contains(x, y float64) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// gfxPixelFrame is the trinket's available space in render pixels — the
// extent the terminal's own pitch reaches, which for a hosted surface is
// exactly what the covering clip guarantees visible. The scrollbars anchor
// to its far right and bottom, un-quantized, overlaying content.
func (t *PurfecTerm) gfxPixelFrame() (wPx, hPx, ppu float64) {
	ppu = t.gfx.ppu
	if ppu <= 0 {
		ppu = 1
	}
	if t.gfx.vpWpx > 0 && t.gfx.vpHpx > 0 {
		// The widget's real device extent. A bar pinned to the right or bottom
		// edge must be pinned to THIS, not to bounds*ppu — those differ by the
		// outer system's snapping, and the pointer arrives in the snapped
		// space.
		return t.gfx.vpWpx, t.gfx.vpHpx, ppu
	}
	b := t.Bounds()
	return math.Round(float64(b.Width) * ppu), math.Round(float64(b.Height) * ppu), ppu
}

// lanePx is the lane thickness per axis. On a graphical surface both axes
// share one pixel width, so the corner where the bars meet is a square. On a
// cell surface a lane cannot be thinner than a character, so it is one CELL
// column wide and one CELL row tall — the ScrollArea idiom.
func (t *PurfecTerm) lanePx(ppu float64) (laneX, laneY float64) {
	if t.gfxInputActive() {
		lane := float64(gfxScrollbarLane) * ppu
		return lane, lane
	}
	m := t.EffectiveCellMetrics()
	return float64(m.CellWidth), float64(m.CellHeight)
}

// gfxPointerPx converts an incoming pointer position (trinket units) into the
// pixels the terminal PAINTS in. Everything inside this widget — cells, the
// scrollbar lanes, the frame gfxPixelFrame reports — is placed by multiplying
// units by ppu and rounding, so a pointer must make exactly that trip and no
// other. Scaling it by anything else puts the hit test in a different
// coordinate space than the paint, and the error grows with distance from the
// origin: the vertical lane, furthest right, misses worst.
func (t *PurfecTerm) gfxPointerPx(x, y core.Unit) (float64, float64) {
	_, _, ppu := t.gfxPixelFrame()
	kx, ky := t.gfx.hitKX, t.gfx.hitKY
	if kx <= 0 {
		kx = 1
	}
	if ky <= 0 {
		ky = 1
	}
	return float64(x) * kx * ppu, float64(y) * ky * ppu
}

// cellBoundaryPx is where a cell edge lands, in painted pixels: fillPixels
// rounds EVERY edge independently, so the boundary before cell N sits at
// round(units * ppu) — not at N times some rounded advance, and not at N times
// a fractional one.
func cellBoundaryPx(units, ppu float64) float64 {
	return math.Round(units * ppu)
}

// hScrollActive reports whether the horizontal bar has anything to show —
// the presence test hScrollGeometry applies, shared so the vertical lane can
// stop where the horizontal one begins.
func (t *PurfecTerm) hScrollActive() bool {
	buf := t.terminal.Buffer()
	cols, _ := buf.GetSize()
	maxContentWidth := 0
	if buf.GetScrollOffset() > 0 {
		maxContentWidth = buf.GetLongestLineVisible()
	}
	if w := buf.GetSplitContentWidth(); w > maxContentWidth {
		maxContentWidth = w
	}
	// A bar whose reason has gone but whose OFFSET remains stays: without
	// it the view is stuck scrolled right with no way back — scrolling down
	// past the wide line removed the bar while the columns stayed shifted.
	// It lives on until the offset is worked back to zero, and from zero it
	// cannot be dragged right again (span collapses with the offset).
	return maxContentWidth > cols || buf.GetHorizOffset() > 0
}

// vScrollGeometry mirrors gtk updateScrollbar: upper = maxOffset+rows,
// page = rows, value = maxOffset-offset (top of track = oldest). All
// rectangles in render pixels; both lanes share one pixel width, and when
// both bars are present the vertical lane STOPS at the horizontal lane's
// top edge — each bar ends where the other begins, and the corner square
// between them is left to the backdrop rather than fought over.
func (t *PurfecTerm) vScrollGeometry() (track, thumb pxRect, upper, page, value int, ok bool) {
	buf := t.terminal.Buffer()
	maxOffset := buf.GetMaxScrollOffset()
	if maxOffset <= 0 {
		return
	}
	wPx, hPx, ppu := t.gfxPixelFrame()
	laneX, laneY := t.lanePx(ppu)
	_, rows := buf.GetSize()
	upper = maxOffset + rows
	page = rows
	value = maxOffset - buf.GetScrollOffset()
	trackH := hPx
	if t.hScrollActive() {
		trackH = hPx - laneY
	} else if !t.gfxInputActive() {
		// CELL surface: give the bottom lane cell up as the corner even with
		// no horizontal bar. The widget's bottom-right cell may be the host
		// terminal's unwritable corner, and stopping one cell short matches
		// the spacing the corner square leaves when both bars are present —
		// the thumb bottoms out on the second-to-last row, never clipped.
		trackH = hPx - laneY
	}
	track = pxRect{X: wPx - laneX, Y: 0, W: laneX, H: trackH}
	thumbLen := track.H * float64(page) / float64(upper)
	// Minimum grab target: 8 device px on a graphical surface, one whole
	// cell on a cell surface (where 8*ppu would mean 8 rows).
	min := 8 * ppu
	if !t.gfxInputActive() {
		min = laneY
	}
	if thumbLen < min {
		thumbLen = min
	}
	if thumbLen > track.H {
		thumbLen = track.H
	}
	span := upper - page
	thumbY := 0.0
	if span > 0 {
		thumbY = (track.H - thumbLen) * float64(value) / float64(span)
	}
	if t.gfx.vDragging {
		// Mid-drag the thumb tracks the pointer at pixel granularity; only
		// the content offset snaps to whole lines.
		pos := t.gfx.vThumbPos
		if limit := track.H - thumbLen; pos > limit {
			pos = limit
		}
		if pos < 0 {
			pos = 0
		}
		thumbY = pos
	}
	thumb = pxRect{X: track.X, Y: thumbY, W: laneX, H: thumbLen}
	ok = true
	return
}

// hScrollGeometry mirrors gtk updateHorizScrollbar. Render pixels; its lane
// is the same pixel width as the vertical one and stops where that one
// begins, leaving a square between them.
func (t *PurfecTerm) hScrollGeometry() (track, thumb pxRect, contentW, cols, value int, ok bool) {
	buf := t.terminal.Buffer()
	cols, _ = buf.GetSize()
	if !t.hScrollActive() {
		return
	}
	maxContentWidth := 0
	if buf.GetScrollOffset() > 0 {
		maxContentWidth = buf.GetLongestLineVisible()
	}
	if w := buf.GetSplitContentWidth(); w > maxContentWidth {
		maxContentWidth = w
	}
	wPx, hPx, ppu := t.gfxPixelFrame()
	laneX, laneY := t.lanePx(ppu)
	value = buf.GetHorizOffset()
	contentW = maxContentWidth
	if min := cols + value; contentW < min {
		// The offset outlived the wide content (see hScrollActive): the
		// track's world is exactly the columns still reachable, so dragging
		// left drains the offset and the right edge is already the wall.
		contentW = min
	}
	track = pxRect{X: 0, Y: hPx - laneY, W: wPx - laneX, H: laneY}
	thumbLen := track.W * float64(cols) / float64(contentW)
	// Same minimum rule as the vertical bar: 8 px graphical, one cell on
	// a cell surface.
	min := 8 * ppu
	if !t.gfxInputActive() {
		min = laneX
	}
	if thumbLen < min {
		thumbLen = min
	}
	if thumbLen > track.W {
		thumbLen = track.W
	}
	span := contentW - cols
	thumbX := 0.0
	if span > 0 {
		thumbX = (track.W - thumbLen) * float64(value) / float64(span)
	}
	if t.gfx.hDragging {
		// Mid-drag the thumb tracks the pointer at pixel granularity; only
		// the content offset snaps to whole columns.
		pos := t.gfx.hThumbPos
		if limit := track.W - thumbLen; pos > limit {
			pos = limit
		}
		if pos < 0 {
			pos = 0
		}
		thumbX = pos
	}
	thumb = pxRect{X: thumbX, Y: track.Y, W: thumbLen, H: laneY}
	ok = true
	return
}

func (t *PurfecTerm) paintScrollbarsGfx(p *core.Painter, bounds core.UnitRect, buf *purfecterm.Buffer, chh float64) {
	thumbStyle := style.DefaultStyle().WithBg(style.RGB(168, 168, 168))
	// Hovered thumb uses the scheme hover colour (fill = its FG), matching
	// the rest of the toolkit's scrollbars.
	hs := t.GetScheme().GetHoveredScrollbarThumb()
	hoverThumb := hs.WithBg(hs.Fg)
	// Pixel fills, not unit fills: the geometry is in render pixels so the
	// lanes sit flush at the far edges and the thumb moves at pointer
	// granularity — and FillRectPixels rides the painter's pixel residual,
	// so a hosted terminal's bars shift with its content. The old rune-
	// shaded track ('░' at cell granularity) has no pixel-space equivalent;
	// a translucent grey wash reads the same.
	fillPx := func(r pxRect, s style.CellStyle) {
		x0, y0 := int(math.Round(r.X)), int(math.Round(r.Y))
		x1, y1 := int(math.Round(r.X+r.W)), int(math.Round(r.Y+r.H))
		if x1 > x0 && y1 > y0 {
			p.FillRectPixels(0, 0, x0, y0, x1-x0, y1-y0, s)
		}
	}
	trackWash := func(r pxRect) {
		x0, y0 := int(math.Round(r.X)), int(math.Round(r.Y))
		x1, y1 := int(math.Round(r.X+r.W)), int(math.Round(r.Y+r.H))
		if x1 > x0 && y1 > y0 {
			if !p.FillRectPixelsAlpha(0, 0, x0, y0, x1-x0, y1-y0, 128, 128, 128, 0.30) {
				p.FillRectPixels(0, 0, x0, y0, x1-x0, y1-y0, style.DefaultStyle().WithBg(style.RGB(128, 128, 128)))
			}
		}
	}
	if track, thumb, _, _, _, ok := t.vScrollGeometry(); ok {
		trackWash(track)
		ts := thumbStyle
		if t.gfx.vHover {
			ts = hoverThumb
		}
		fillPx(thumb, ts)
	}
	if track, thumb, _, _, _, ok := t.hScrollGeometry(); ok {
		trackWash(track)
		ts := thumbStyle
		if t.gfx.hHover {
			ts = hoverThumb
		}
		fillPx(thumb, ts)
	}
}

// updateScrollbarHoverGfx tracks whether the pointer is over either
// scrollbar thumb, repainting only on change.
func (t *PurfecTerm) updateScrollbarHoverGfx(x, y core.Unit) {
	if !t.scrollLanesActive() {
		return
	}
	px, py := t.gfxPointerPx(x, y)
	vh, hh := false, false
	if _, thumb, _, _, _, ok := t.vScrollGeometry(); ok {
		vh = thumb.contains(px, py)
	}
	if _, thumb, _, _, _, ok := t.hScrollGeometry(); ok {
		hh = thumb.contains(px, py)
	}
	if vh != t.gfx.vHover || hh != t.gfx.hHover {
		t.gfx.vHover = vh
		t.gfx.hHover = hh
		t.Update()
	}
}

// scrollbarPress starts a scrollbar drag if the press lands in a
// lane. Returns true when consumed.
func (t *PurfecTerm) scrollbarPress(event core.MousePressEvent) bool {
	if !t.scrollLanesActive() {
		return false // no lanes to press (editor mode reclaims them)
	}
	px, py := t.gfxPointerPx(event.X, event.Y)
	if track, thumb, _, _, _, ok := t.vScrollGeometry(); ok &&
		px >= track.X && py >= track.Y && py < track.Y+track.H {
		// Anchor the drag to the grab point within the thumb; a
		// press on the track jumps the thumb center to the pointer.
		if py >= thumb.Y && py < thumb.Y+thumb.H {
			t.gfx.vGrabOff = py - thumb.Y
		} else {
			t.gfx.vGrabOff = thumb.H / 2
		}
		t.gfx.vDragging = true
		t.scrollbarDragTo(event.X, event.Y)
		return true
	}
	if track, thumb, _, _, _, ok := t.hScrollGeometry(); ok &&
		py >= track.Y && px >= track.X && px < track.X+track.W {
		if px >= thumb.X && px < thumb.X+thumb.W {
			t.gfx.hGrabOff = px - thumb.X
		} else {
			t.gfx.hGrabOff = thumb.W / 2
		}
		t.gfx.hDragging = true
		t.scrollbarDragTo(event.X, event.Y)
		return true
	}
	return false
}

func (t *PurfecTerm) scrollbarDragTo(x, y core.Unit) {
	px, py := t.gfxPointerPx(x, y)
	buf := t.terminal.Buffer()
	if t.gfx.vDragging {
		if track, thumb, upper, page, _, ok := t.vScrollGeometry(); ok {
			span := track.H - thumb.H
			if span > 0 {
				pos := py - track.Y - t.gfx.vGrabOff
				if pos < 0 {
					pos = 0
				}
				if pos > span {
					pos = span
				}
				t.gfx.vThumbPos = pos
				// The THUMB is pixel-smooth; only the content offset
				// quantizes, to the whole line nearest the thumb.
				value := int(pos*float64(upper-page)/span + 0.5)
				maxOffset := buf.GetMaxScrollOffset()
				buf.SetScrollOffset(maxOffset - value)
				buf.NotifyManualVertScroll()
			}
		}
	}
	if t.gfx.hDragging {
		if track, thumb, contentW, cols, _, ok := t.hScrollGeometry(); ok {
			span := track.W - thumb.W
			if span > 0 {
				pos := px - track.X - t.gfx.hGrabOff
				if pos < 0 {
					pos = 0
				}
				if pos > span {
					pos = span
				}
				t.gfx.hThumbPos = pos
				buf.SetHorizOffset(int(pos*float64(contentW-cols)/span + 0.5))
			}
		}
	}
	t.Update()
}

// ---------------------------------------------------------------
// Graphical input: selection, mouse reporting, autoscroll, wheel
// ---------------------------------------------------------------

// screenToCellGfx maps trinket-unit coordinates to buffer cells,
// honoring screen scale, double-width lines, and flex-width cells
// (ported from the gtk reference).
func (t *PurfecTerm) screenToCellGfx(x, y core.Unit) (cellX, cellY int) {
	buf := t.terminal.Buffer()
	baseCW, baseCH := t.cellDims()
	cw := float64(baseCW) * buf.GetHorizontalScale()
	chh := float64(baseCH) * buf.GetVerticalScale()
	if chh <= 0 || cw <= 0 {
		return 0, 0
	}

	// The pointer scales onto the snapped frame (hitK), then the walk compares
	// against boundaries rounded exactly as fillPixels rounds them. Dividing
	// by a fractional advance instead accumulates error across the row and
	// lands on the wrong cell the further along the pointer goes — visible as
	// a selection that grabs the wrong column.
	fx, fy := t.gfxPointerPx(x, y)
	ppu := t.gfx.ppu
	if ppu <= 0 {
		ppu = 1
	}

	cellY = int(fy / (chh * ppu))
	cols, rows := buf.GetSize()
	if cellY < 0 {
		cellY = 0
	}
	if cellY >= rows {
		cellY = rows - 1
	}
	lineScale := 1.0
	if buf.GetVisibleLineAttribute(cellY) != purfecterm.LineAttrNormal {
		lineScale = 2.0
	}
	relativeX := fx
	if relativeX < 0 {
		return 0, cellY
	}
	horizOffset := buf.GetHorizOffset()
	accumulated := 0.0
	for col := horizOffset; col < cols+horizOffset; col++ {
		cell := buf.GetVisibleCell(col-horizOffset, cellY)
		w := 1.0
		if cell.CellWidth > 0 { // CellWidth is authoritative (see patches/purfecterm/PROTOCOL.md)
			w = cell.CellWidth
		}
		accumulated += w * cw * lineScale
		if relativeX < cellBoundaryPx(accumulated, ppu) {
			return col, cellY
		}
	}
	cellX = cols + horizOffset - 1
	if cellX < 0 {
		cellX = 0
	}
	return cellX, cellY
}

// screenToVisualCellGfx maps trinket-unit coordinates to the PHYSICAL
// viewport cell — the coordinates a mouse report carries. Under the standard
// contract, mouse reports are visual screen columns (what a hardware terminal
// sends), which diverge from screenToCellGfx's LOGICAL buffer cells whenever
// wide characters precede the pointer: reporting the logical cell parks the
// hosted app's caret left of the click. Local selection keeps the logical
// mapping; reports must use this one.
func (t *PurfecTerm) screenToVisualCellGfx(x, y core.Unit) (col, row int) {
	buf := t.terminal.Buffer()
	baseCW, baseCH := t.cellDims()
	cw := float64(baseCW) * buf.GetHorizontalScale()
	chh := float64(baseCH) * buf.GetVerticalScale()
	if cw <= 0 || chh <= 0 {
		return 0, 0
	}
	ppu := t.gfx.ppu
	if ppu <= 0 {
		ppu = 1
	}
	// Snapped frame for the pointer, rounded boundaries for the cells.
	px, py := t.gfxPointerPx(x, y)
	cols, rows := buf.GetSize()
	col = 0
	for col+1 < cols && px >= cellBoundaryPx(float64(col+1)*cw, ppu) {
		col++
	}
	row = 0
	for row+1 < rows && py >= cellBoundaryPx(float64(row+1)*chh, ppu) {
		row++
	}
	if col < 0 {
		col = 0
	}
	if cols > 0 && col >= cols {
		col = cols - 1
	}
	if row < 0 {
		row = 0
	}
	if rows > 0 && row >= rows {
		row = rows - 1
	}
	return col, row
}

// gfxCellSubUnits is the width AND height, in synthetic "pixels", of one cell
// on the grid the hosted app reads under SGR-Pixels (?1016) — reported to it as
// the cell size via CSI 16 t. Device pixels can't be used directly: a cell's
// painted advance (baseCell*scale*ppu) is fractional, and its boundaries land
// at round(col*advance) — a PER-cell rounding. Uniform division of a device
// pixel by one integer cell size (col*round(advance)) diverges from that,
// drifting up to a full cell by the far edge of a wide/tall screen. Instead the
// report puts the exact cell index (from the same rounding walk the paint uses)
// in the high digits and the sub-cell fraction in the low gfxCellSubUnits — so
// the app's own uniform division recovers the cell with no drift and the
// remainder is the sub-cell position.
const gfxCellSubUnits = 1000

// pushCellPixelSizeGfx tells the hosted app that a cell is gfxCellSubUnits
// "pixels" square (CSI 16 t). It is a constant — the synthetic grid, unlike
// device pixels, does not move with font zoom — but is (re)asserted each paint
// so a buffer rebuild never leaves it unset.
func (t *PurfecTerm) pushCellPixelSizeGfx() {
	t.terminal.Buffer().SetCellPixelSize(gfxCellSubUnits, gfxCellSubUnits)
}

// screenToPixelReportGfx maps trinket-unit coordinates onto the synthetic pixel
// grid the hosted app reads under ?1016. Each axis is located by the SAME
// per-cell rounding walk the paint uses (cellBoundaryPx), so the integer cell
// never drifts from the glyph; the pointer's fraction across that one cell rides
// in the low gfxCellSubUnits digits. 0-based, matching screenToVisualCellGfx.
func (t *PurfecTerm) screenToPixelReportGfx(x, y core.Unit) (px, py int) {
	buf := t.terminal.Buffer()
	baseCW, baseCH := t.cellDims()
	cw := float64(baseCW) * buf.GetHorizontalScale()
	chh := float64(baseCH) * buf.GetVerticalScale()
	if cw <= 0 || chh <= 0 {
		return 0, 0
	}
	ppu := t.gfx.ppu
	if ppu <= 0 {
		ppu = 1
	}
	ptx, pty := t.gfxPointerPx(x, y)
	cols, rows := buf.GetSize()
	return pixelReportAxis(ptx, cw, ppu, cols), pixelReportAxis(pty, chh, ppu, rows)
}

// pixelReportAxis places a device-pixel coordinate pt on one axis of the
// synthetic ?1016 grid. It walks to the cell containing pt exactly as
// screenToVisualCellGfx does (boundaries rounded PER cell, cellBoundaryPx), then
// measures pt's fraction across that cell's painted span. The result is
// cell*gfxCellSubUnits + fraction, so dividing by gfxCellSubUnits recovers the
// SAME cell the paint drew — no cumulative drift from a fractional advance —
// while the remainder carries the sub-cell position. adv is the cell's unit
// advance, ppu the pixels-per-unit, n the cell count on this axis.
func pixelReportAxis(pt, adv, ppu float64, n int) int {
	col := 0
	for col+1 < n && pt >= cellBoundaryPx(float64(col+1)*adv, ppu) {
		col++
	}
	left := cellBoundaryPx(float64(col)*adv, ppu)
	right := cellBoundaryPx(float64(col+1)*adv, ppu)
	frac := 0.0
	if right > left {
		frac = (pt - left) / (right - left)
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 0.999999 {
		frac = 0.999999
	}
	return col*gfxCellSubUnits + int(frac*gfxCellSubUnits)
}

// reportMouseGfx forwards a mouse event to the hosted app, choosing the
// coordinate space by the app's encoding mode. SGR-Pixels (?1016) reports a
// position on the synthetic pixel grid (screenToPixelReportGfx): the cell is
// located drift-free by the paint's own rounding, and the sub-cell fraction
// lets a hosted editor land its caret on the nearest cell edge. Every other
// mode reports the visual cell. x,y are the event's trinket-unit coordinates;
// both paths are 1-based (the report space CSI 16 t declares).
func (t *PurfecTerm) reportMouseGfx(button int, x, y core.Unit, press bool) bool {
	if t.gfx.reportingDisabled {
		return false
	}
	buf := t.terminal.Buffer()
	if buf.GetMouseTrackingMode() == 0 {
		return false
	}
	var repX, repY int
	if buf.GetMouseEncodingMode() == 1016 {
		px, py := t.screenToPixelReportGfx(x, y)
		repX, repY = px+1, py+1
	} else {
		col, row := t.screenToVisualCellGfx(x, y)
		repX, repY = col+1, row+1
	}
	data := purfecterm.EncodeMouseEvent(button, repX, repY, press, buf.GetMouseEncodingMode())
	if data == nil {
		return false
	}
	t.toChild(data)
	return true
}

func gfxMouseModifiers(mods core.KeyModifiers) int {
	m := 0
	if mods&core.ShiftModifier != 0 {
		m |= purfecterm.MouseModShift
	}
	if mods&core.AltModifier != 0 {
		m |= purfecterm.MouseModAlt
	}
	if mods&core.ControlModifier != 0 {
		m |= purfecterm.MouseModControl
	}
	return m
}

func (t *PurfecTerm) gfxMousePress(event core.MousePressEvent) bool {
	t.SetFocus()
	if t.scrollbarPress(event) {
		return true
	}
	buf := t.terminal.Buffer()
	cellX, cellY := t.screenToCellGfx(event.X, event.Y)
	// Shift classically bypasses app mouse tracking for LOCAL grid selection —
	// except in editor mode, where the grid has no useful local selection and
	// the guest editor owns shift+click (extend-selection) itself.
	hasShift := event.Modifiers&core.ShiftModifier != 0 && !t.editorMode
	forwardToPTY := !t.gfx.reportingDisabled && buf.GetMouseTrackingMode() != 0 && !hasShift

	if event.Button == core.RightButton {
		if forwardToPTY {
			t.gfx.mouseDown = true
			t.reportMouseGfx(purfecterm.MouseButtonRight|gfxMouseModifiers(event.Modifiers), event.X, event.Y, true)
			return true
		}
		t.showContextMenu(event)
		return true
	}

	if forwardToPTY {
		btn := purfecterm.MouseButtonLeft
		if event.Button == core.MiddleButton {
			btn = purfecterm.MouseButtonMiddle
		}
		t.gfx.mouseDown = true
		t.reportMouseGfx(btn|gfxMouseModifiers(event.Modifiers), event.X, event.Y, true)
		return true
	}

	if event.Button == core.LeftButton {
		t.gfx.mouseDown = true
		t.gfx.mouseDownX = cellX
		t.gfx.mouseDownY = cellY
		t.gfx.selectionMoved = false
		buf.ClearSelection()
		t.Update()
	}
	return true
}

func (t *PurfecTerm) gfxMouseMove(event core.MouseMoveEvent) bool {
	if t.gfx.vDragging || t.gfx.hDragging {
		t.scrollbarDragTo(event.X, event.Y)
		return true
	}
	// Track scrollbar-thumb hover on plain moves (also clears on the
	// out-of-bounds move the pane sends when the pointer leaves). Hover is a
	// no-button affordance: while a button is held (a drag begun elsewhere
	// passing over) clear rather than light the thumb (off-point clears).
	if event.Buttons == 0 {
		t.updateScrollbarHoverGfx(event.X, event.Y)
	} else {
		t.updateScrollbarHoverGfx(-1, -1)
	}
	buf := t.terminal.Buffer()
	cellX, cellY := t.screenToCellGfx(event.X, event.Y)
	// Editor mode forwards shift (see gfxMousePress).
	hasShift := event.Modifiers&core.ShiftModifier != 0 && !t.editorMode
	trackingMode := buf.GetMouseTrackingMode()
	forwardToPTY := !t.gfx.reportingDisabled && trackingMode != 0 && !hasShift

	if forwardToPTY {
		if trackingMode == 1003 || (trackingMode == 1002 && t.gfx.mouseDown) {
			btn := purfecterm.MouseButtonNone | purfecterm.MouseMotionFlag
			if t.gfx.mouseDown {
				btn = purfecterm.MouseButtonLeft | purfecterm.MouseMotionFlag
			}
			t.reportMouseGfx(btn|gfxMouseModifiers(event.Modifiers), event.X, event.Y, true)
		}
		return true
	}

	if !t.gfx.mouseDown {
		return false
	}

	// Start selection only after leaving the press cell.
	if !t.gfx.selectionMoved {
		if cellX == t.gfx.mouseDownX && cellY == t.gfx.mouseDownY {
			return true
		}
		t.gfx.selectionMoved = true
		t.gfx.selecting = true
		buf.StartSelection(t.gfx.mouseDownX, t.gfx.mouseDownY)
	}
	t.gfx.lastMouseX = cellX
	t.gfx.lastMouseY = cellY

	// Edge auto-scroll (vertical + horizontal), speed capped at 5.
	baseCW, baseCH := t.cellDims()
	cw := float64(baseCW) * buf.GetHorizontalScale()
	chh := float64(baseCH) * buf.GetVerticalScale()
	cols, rows := buf.GetSize()
	vertDelta, horizDelta := 0, 0
	if my := float64(event.Y); my < 0 {
		vertDelta = -clampScrollSpeed(int(-my/chh) + 1)
	} else if my >= float64(rows)*chh {
		vertDelta = clampScrollSpeed(int((my-float64(rows)*chh)/chh) + 1)
	}
	if mx := float64(event.X); mx < 0 {
		horizDelta = -clampScrollSpeed(int(-mx/cw) + 1)
	} else if mx >= float64(cols)*cw {
		horizDelta = clampScrollSpeed(int((mx-float64(cols)*cw)/cw) + 1)
	}
	if vertDelta != 0 || horizDelta != 0 {
		t.startAutoScroll(vertDelta, horizDelta)
	} else {
		t.stopAutoScroll()
	}

	buf.UpdateSelection(cellX, cellY)
	t.Update()
	return true
}

func clampScrollSpeed(n int) int {
	if n > 5 {
		return 5
	}
	return n
}

func (t *PurfecTerm) gfxMouseRelease(event core.MouseReleaseEvent) bool {
	if t.gfx.vDragging || t.gfx.hDragging {
		t.gfx.vDragging = false
		t.gfx.hDragging = false
		return true
	}
	// Containers broadcast releases to every child; only act on a
	// release whose press we actually saw (gtk's implicit grab), so
	// sibling trinkets are not starved and the PTY never receives a
	// release for a press that landed elsewhere.
	if !t.gfx.mouseDown && !t.gfx.selecting {
		return false
	}
	buf := t.terminal.Buffer()
	// Editor mode forwards shift (see gfxMousePress).
	hasShift := event.Modifiers&core.ShiftModifier != 0 && !t.editorMode
	forwardToPTY := !t.gfx.reportingDisabled && buf.GetMouseTrackingMode() != 0 && !hasShift

	if forwardToPTY {
		btn := purfecterm.MouseButtonLeft
		switch event.Button {
		case core.MiddleButton:
			btn = purfecterm.MouseButtonMiddle
		case core.RightButton:
			btn = purfecterm.MouseButtonRight
		}
		t.gfx.mouseDown = false
		t.reportMouseGfx(btn|gfxMouseModifiers(event.Modifiers), event.X, event.Y, false)
		return true
	}

	if event.Button == core.LeftButton {
		t.gfx.mouseDown = false
		t.stopAutoScroll()
		if t.gfx.selecting {
			t.gfx.selecting = false
			buf.EndSelection()
		}
		t.Update()
	}
	return true
}

// Horizontal wheel buttons continue purfecterm's SGR scroll series
// (MouseScrollUp=64, MouseScrollDown=65): left=66, right=67, matching xterm.
const (
	mouseScrollLeftBtn  = 66
	mouseScrollRightBtn = 67
)

func (t *PurfecTerm) gfxMouseWheel(event core.MouseWheelEvent) bool {
	buf := t.terminal.Buffer()
	// Editor mode forwards shift+wheel too: the grid never scrolls under an
	// editor (alt screen), so the local shift-horizontal-scroll is meaningless
	// there — the guest editor interprets the modifier itself.
	hasShift := event.Modifiers&core.ShiftModifier != 0 && !t.editorMode
	forwardToPTY := !t.gfx.reportingDisabled && buf.GetMouseTrackingMode() != 0 && !hasShift

	if forwardToPTY {
		mods := gfxMouseModifiers(event.Modifiers)
		if event.DeltaY < 0 {
			t.reportMouseGfx(purfecterm.MouseScrollUp|mods, event.X, event.Y, true)
		} else if event.DeltaY > 0 {
			t.reportMouseGfx(purfecterm.MouseScrollDown|mods, event.X, event.Y, true)
		}
		// Horizontal wheel: SGR buttons 66/67 continue purfecterm's scroll
		// series (Up=64, Down=65). DeltaX>0 = scroll right. (An app whose input
		// layer decodes 66/67 as left/right acts on it; older decoders that only
		// know 64/65 ignore the extra buttons rather than mis-scroll vertically.)
		if event.DeltaX < 0 {
			t.reportMouseGfx(mouseScrollLeftBtn|mods, event.X, event.Y, true)
		} else if event.DeltaX > 0 {
			t.reportMouseGfx(mouseScrollRightBtn|mods, event.X, event.Y, true)
		}
		return true
	}

	if event.DeltaY < 0 { // up
		if hasShift {
			off := buf.GetHorizOffset() - 3
			if off < 0 {
				off = 0
			}
			buf.SetHorizOffset(off)
		} else {
			off := buf.GetScrollOffset() + 3
			if max := buf.GetMaxScrollOffset(); off > max {
				off = max
			}
			buf.SetScrollOffset(off)
			buf.NotifyManualVertScroll()
		}
	} else if event.DeltaY > 0 { // down
		if hasShift {
			off := buf.GetHorizOffset() + 3
			if max := buf.GetMaxHorizOffset(); off > max {
				off = max
			}
			buf.SetHorizOffset(off)
		} else {
			off := buf.GetScrollOffset() - 3
			if off < 0 {
				off = 0
			}
			buf.SetScrollOffset(off)
		}
	}
	t.Update()
	return true
}

// startAutoScroll runs a 50ms repeating timer scrolling and extending
// the selection toward the dragged-past edge (gtk parity).
func (t *PurfecTerm) startAutoScroll(vertDelta, horizDelta int) {
	t.gfx.autoVert = vertDelta
	t.gfx.autoHoriz = horizDelta
	if t.gfx.autoTimer != nil {
		return
	}
	d := t.findDesktop()
	if d == nil {
		return
	}
	t.gfx.autoTimer = d.StartRepeatingTimer(50*time.Millisecond, func() {
		if !t.gfx.selecting || (t.gfx.autoVert == 0 && t.gfx.autoHoriz == 0) {
			t.stopAutoScroll()
			return
		}
		buf := t.terminal.Buffer()
		cols, rows := buf.GetSize()
		selX, selY := t.gfx.lastMouseX, t.gfx.lastMouseY

		if t.gfx.autoVert != 0 {
			offset := buf.GetScrollOffset()
			amount := t.gfx.autoVert
			if amount < 0 {
				amount = -amount
			}
			if t.gfx.autoVert < 0 {
				offset += amount
				if max := buf.GetMaxScrollOffset(); offset > max {
					offset = max
				}
				selY = 0
			} else {
				offset -= amount
				if offset < 0 {
					offset = 0
				}
				selY = rows - 1
			}
			buf.SetScrollOffset(offset)
		}
		if t.gfx.autoHoriz != 0 {
			offset := buf.GetHorizOffset()
			amount := t.gfx.autoHoriz
			if amount < 0 {
				amount = -amount
			}
			if t.gfx.autoHoriz < 0 {
				offset -= amount
				if offset < 0 {
					offset = 0
				}
				selX = 0
			} else {
				offset += amount
				if max := buf.GetMaxHorizOffset(); offset > max {
					offset = max
				}
				selX = cols - 1
			}
			buf.SetHorizOffset(offset)
		}
		buf.UpdateSelection(selX, selY)
		t.Update()
	})
}

func (t *PurfecTerm) stopAutoScroll() {
	if t.gfx.autoTimer != nil {
		t.gfx.autoTimer.Stop()
		t.gfx.autoTimer = nil
	}
	t.gfx.autoVert = 0
	t.gfx.autoHoriz = 0
}

// ---------------------------------------------------------------
// Clipboard + context menu
// ---------------------------------------------------------------

// CopySelection copies the buffer's selected text to the clipboard.
func (t *PurfecTerm) CopySelection() {
	if t.terminal == nil {
		return
	}
	textSel := t.terminal.Buffer().GetSelectedText()
	if textSel == "" {
		return
	}
	if d := t.findDesktop(); d != nil {
		d.SetClipboard(textSel)
	}
}

// PasteClipboard sends the clipboard to the PTY (bracketed when the
// application enabled bracketed paste mode). The clipboard read may be
// asynchronous (a terminal OSC 52 query can prompt the user), so the desktop
// resolves it and calls back on the UI thread; SDL/internal reads are immediate.
func (t *PurfecTerm) PasteClipboard() {
	if t.terminal == nil {
		return
	}
	d := t.findDesktop()
	if d == nil {
		return
	}
	d.ReadClipboardAsync(func(s string) { t.sendPaste(s) })
}

// normalizePasteNewlines converts clipboard line endings to carriage return
// for the child PTY: a terminal's Enter key sends CR, and the child's line
// discipline (raw mode, no ICRNL on the paste) acts on CR, not LF. Clipboard
// text uses LF (or CRLF), so pasting it verbatim would swallow the line breaks.
// CRLF is collapsed first, then any lone LF.
func normalizePasteNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\r")
	s = strings.ReplaceAll(s, "\n", "\r")
	return s
}

// HandlePaste delivers pasted text (e.g. a bracketed paste the host received
// from its outer terminal) to the child, re-bracketing it per the CHILD's own
// paste mode — see sendPaste. This is what lets a paste into the outer terminal
// reach the program inside a focused PurfecTerm — or a mew editor, which embeds
// one — as a real paste rather than the flood of per-character keystrokes the
// host would otherwise forward. The outer terminal's framing never travels this
// far: the child's mode, not the host's, decides whether the body is bracketed.
// Satisfies core.PasteHandler.
func (t *PurfecTerm) HandlePaste(event core.PasteEvent) bool {
	if t.terminal == nil {
		return false
	}
	t.sendPaste(event.Text)
	return true
}

// sendPaste writes resolved clipboard text to the child PTY, bracketing it when
// the application enabled bracketed paste mode.
func (t *PurfecTerm) sendPaste(s string) {
	if t.terminal == nil || s == "" {
		return
	}
	s = normalizePasteNewlines(s)
	if t.terminal.Buffer().IsBracketedPasteModeEnabled() {
		s = "\x1b[200~" + s + "\x1b[201~"
	}
	t.resetCursorBlink()
	t.toChild([]byte(s))
}

// HasSelection reports whether the terminal buffer currently has selected
// text, so the Edit menu can surface a passive notice when Copy would be a
// no-op.
func (t *PurfecTerm) HasSelection() bool {
	return t.terminal != nil && t.terminal.Buffer().GetSelectedText() != ""
}

// SelectAll selects the whole buffer.
func (t *PurfecTerm) SelectAll() {
	if t.terminal != nil {
		t.terminal.Buffer().SelectAll()
		t.Update()
	}
}

// Copy, Cut, Paste satisfy the desktop's edit-action interface so
// the Edit menu operates on a focused terminal. A terminal's output
// can't be cut, so Cut is a no-op and CutEnabled reports false (the
// menu greys it out).

// Copy copies the selection to the clipboard.
func (t *PurfecTerm) Copy() { t.CopySelection() }

// Cut is a no-op: terminal text cannot be removed.
func (t *PurfecTerm) Cut() {}

// Paste sends the clipboard to the PTY.
func (t *PurfecTerm) Paste() { t.PasteClipboard() }

// CutEnabled reports whether Cut applies here - never, for a terminal.
func (t *PurfecTerm) CutEnabled() bool { return false }

type termMenuItem struct {
	label     string
	separator bool
	action    func()
	checked   func() bool
}

func (t *PurfecTerm) contextMenuItems() []termMenuItem {
	return []termMenuItem{
		{label: "Copy", action: t.CopySelection},
		{label: "Paste", action: t.PasteClipboard},
		{separator: true},
		{label: "Select All", action: t.SelectAll},
		{separator: true},
		{label: "Mouse Reporting", action: func() {
			t.gfx.reportingDisabled = !t.gfx.reportingDisabled
		}, checked: func() bool { return !t.gfx.reportingDisabled }},
	}
}

func (t *PurfecTerm) contextMenuID() string {
	return fmt.Sprintf("purfecterm-menu-%d", t.ObjectID())
}

// showContextMenu opens the right-click menu (Copy / Paste / Select
// All / Mouse Reporting toggle) as a popup overlay.
func (t *PurfecTerm) showContextMenu(event core.MousePressEvent) {
	t.showTermItemsMenu(core.UnitPoint{X: event.X, Y: event.Y}, t.contextMenuItems())
}

// termMenuLayout is the popup menu's measurement: fixed pixel-ish rows on a
// graphical desktop (the established TextInput/PurfecTerm look), CELL
// metrics on the text desktop — one full character row per item, cell-
// aligned width — following the same measurement approach as the Menu
// dropdowns and the ComboBox popup.
type termMenuLayout struct {
	rowH, sepH, width, padTop, indent core.Unit
	graphical                         bool
}

func (t *PurfecTerm) termMenuLayoutFor(pc core.PopupController, items []termMenuItem) termMenuLayout {
	if core.FindGraphicalFrames(t) {
		return termMenuLayout{rowH: gfxMenuItemHeight, sepH: 4, width: gfxMenuWidth, padTop: 2, indent: 8, graphical: true}
	}
	// Popups are desktop-surface overlays: like the ComboBox popup, measure
	// in the SCREEN's denomination (the popup controller's cell metrics),
	// not this trinket's possibly re-denominated interior.
	m := core.DefaultCellMetrics()
	if sm, ok := pc.(interface{ ScreenCellMetrics() core.CellMetrics }); ok {
		if s := sm.ScreenCellMetrics(); s.CellWidth > 0 && s.CellHeight > 0 {
			m = s
		}
	}
	cols := 12
	for _, it := range items {
		if n := len([]rune(it.label)) + 6; n > cols { // gutter + checkmark room
			cols = n
		}
	}
	return termMenuLayout{
		rowH:   m.CellHeight,
		sepH:   m.CellHeight, // a separator needs a full character row
		width:  core.Unit(cols) * m.CellWidth,
		indent: m.CellWidth, // one cell in
		// no sub-cell padding: rows land exactly on character rows
	}
}

// showTermItemsMenu opens a context menu of the given items as a popup
// overlay anchored at a LOCAL point of this trinket — the shared
// presentation behind the terminal's right-click menu, reused by the
// mew-backed Editor for its own (mew-driven) context menu. It works on
// both desktops: the popup overlay machinery is backend-neutral, and the
// layout is measured per backend (termMenuLayoutFor).
func (t *PurfecTerm) showTermItemsMenu(local core.UnitPoint, items []termMenuItem) {
	pc := t.PopupController()
	if pc == nil {
		pc = t.findPopupControllerTerm()
	}
	if pc == nil {
		return
	}
	lay := t.termMenuLayoutFor(pc, items)
	height := core.Unit(0)
	for _, it := range items {
		if it.separator {
			height += lay.sepH
		} else {
			height += lay.rowH
		}
	}
	height += 2 * lay.padTop
	at := pc.MapToScreen(t.Self(), local)
	screen := pc.ScreenBounds()
	if at.X+lay.width > screen.X+screen.Width {
		at.X = screen.X + screen.Width - lay.width
	}
	if at.Y+height > screen.Y+screen.Height {
		at.Y = screen.Y + screen.Height - height
	}
	menuBounds := core.UnitRect{X: at.X, Y: at.Y, Width: lay.width, Height: height}
	t.gfx.menuHover = -1

	itemAt := func(y core.Unit) int {
		pos := lay.padTop
		for i, it := range items {
			h := lay.rowH
			if it.separator {
				h = lay.sepH
			}
			if y >= pos && y < pos+h {
				if it.separator {
					return -1
				}
				return i
			}
			pos += h
		}
		return -1
	}

	pc.RegisterPopup(&core.PopupRequest{
		ID:     t.contextMenuID(),
		Bounds: menuBounds,
		Paint: func(p *core.Painter) {
			bg := style.DefaultStyle().WithFg(style.RGB(32, 32, 32)).WithBg(style.RGB(238, 238, 238))
			hover := style.DefaultStyle().WithFg(style.RGB(255, 255, 255)).WithBg(style.RGB(56, 120, 220))
			p.FillRect(core.UnitRect{X: menuBounds.X, Y: menuBounds.Y, Width: menuBounds.Width, Height: menuBounds.Height}, ' ', bg)
			// The 1-pixel outer frame every popup gets, in the padded
			// margin just outside the bounds (graphical only).
			if p.Graphical() {
				lineStyle := style.DefaultStyle().WithBg(t.GetScheme().GetMenuSeparator().Fg)
				paintPopupOuterStroke(p, menuBounds, p.DeviceScale(), lineStyle, 0, 0, false)
			}
			pos := menuBounds.Y + lay.padTop
			for i, it := range items {
				if it.separator {
					if lay.graphical {
						p.FillRect(core.UnitRect{X: menuBounds.X + 4, Y: pos + 2, Width: menuBounds.Width - 8, Height: 1}, ' ',
							style.DefaultStyle().WithBg(style.RGB(200, 200, 200)))
					} else {
						// Text cells: a full dim rule row.
						p.FillRect(core.UnitRect{X: menuBounds.X, Y: pos, Width: menuBounds.Width, Height: lay.sepH}, '─',
							bg.WithFg(style.RGB(160, 160, 160)))
					}
					pos += lay.sepH
					continue
				}
				st := bg
				if i == t.gfx.menuHover {
					st = hover
					p.FillRect(core.UnitRect{X: menuBounds.X, Y: pos, Width: menuBounds.Width, Height: lay.rowH}, ' ', st)
				}
				label := it.label
				if it.checked != nil {
					if it.checked() {
						label = "✓ " + label
					} else {
						label = "  " + label
					}
				}
				// Draw with the style's EXPLICIT background: a transparent bg
				// composites correctly on the graphical backend but resolves to
				// the terminal's default (dark) background on the text backend,
				// leaving dark boxes behind the labels. The explicit bg equals
				// the fill (or hover) color, so the graphical look is unchanged.
				p.DrawText(menuBounds.X+lay.indent, pos, label, st, nil)
				pos += lay.rowH
			}
		},
		HandleMouseMove: func(event core.MouseMoveEvent) bool {
			if !menuBounds.Contains(core.UnitPoint{X: event.X, Y: event.Y}) {
				return false
			}
			idx := itemAt(event.Y - menuBounds.Y)
			if idx != t.gfx.menuHover {
				t.gfx.menuHover = idx
				t.Update()
			}
			return true
		},
		HandleMousePress: func(event core.MousePressEvent) bool {
			idx := itemAt(event.Y - menuBounds.Y)
			pc.UnregisterPopup(t.contextMenuID())
			if idx >= 0 && items[idx].action != nil {
				items[idx].action()
			}
			t.Update()
			return true
		},
	})
	t.Update()
}

// cellToLocal maps a 1-based terminal cell to this trinket's local units —
// the anchor for a popup at that cell. The inverse of screenToCellGfx's
// uniform-grid part: cell pitch from the effective font (graphical) or the
// cell metrics (text UI), the terminal's scale factors, and the hit-space
// snap ratio applied in reverse. An approximation over per-cell width walks
// (double-width lines), which is fine for a menu anchor.
func (t *PurfecTerm) cellToLocal(col, row int) core.UnitPoint {
	baseCW, baseCH := t.cellDims()
	cw, chh := float64(baseCW), float64(baseCH)
	if t.terminal != nil {
		buf := t.terminal.Buffer()
		cw *= buf.GetHorizontalScale()
		chh *= buf.GetVerticalScale()
	}
	// The inverse of the hit path, and it must share its frame: cells are
	// placed at rounded pixel boundaries, so a cell's origin in units is that
	// boundary divided back by ppu.
	ppu := t.gfx.ppu
	if ppu <= 0 {
		ppu = 1
	}
	kx, ky := t.gfx.hitKX, t.gfx.hitKY
	if kx <= 0 {
		kx = 1
	}
	if ky <= 0 {
		ky = 1
	}
	return core.UnitPoint{
		X: core.Unit(cellBoundaryPx(float64(col-1)*cw, ppu) / (kx * ppu)),
		Y: core.Unit(cellBoundaryPx(float64(row-1)*chh, ppu) / (ky * ppu)),
	}
}

// findPopupControllerTerm walks the parent chain for a popup
// controller (same pattern as ComboBox).
func (t *PurfecTerm) findPopupControllerTerm() core.PopupController {
	current := t.Parent()
	for current != nil {
		if w, ok := current.(core.Trinket); ok {
			if getter, ok := w.(interface{ PopupController() core.PopupController }); ok {
				if pc := getter.PopupController(); pc != nil {
					return pc
				}
			}
			current = w.Parent()
		} else {
			break
		}
	}
	return nil
}
