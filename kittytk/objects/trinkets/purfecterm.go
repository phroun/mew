// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
	"github.com/phroun/purfecterm"
	"github.com/phroun/purfecterm/cli"
)

// PurfecTerm is a terminal emulator trinket that embeds PurfecTerm's CLI adapter.
// It provides a fully functional terminal within the TUI application.
type PurfecTerm struct {
	core.TrinketBase
	core.AccessibleTrinket

	// The underlying terminal emulator
	terminal *cli.Terminal

	// Cached size in cells
	cols, rows int

	// termFont sets the terminal's own font (graphical mode): the
	// cell grid derives from ITS measured metrics (advance width and
	// line height at its point size), independent of the toolkit's
	// cell denomination. nil = the monospace default (defaultTermFont).
	// Text mode ignores the size (cells are cells).
	termFont *core.Font

	// Track which mouse button is currently held for drag events
	heldButton core.MouseButton

	// Debug callback for cell inspection
	onCellClicked func(info CellDebugInfo)

	// The child process lives on the CLIENT, never on the render server:
	// the terminal is a pure display+input surface. inputSink receives
	// every byte the user produces (keystrokes, mouse reports, paste) so
	// the client can write it to its own PTY; resizeSink receives the grid
	// dimensions whenever they change so the client can set the PTY
	// winsize. Display output flows the other way, in through Feed. When
	// no sink is installed the input is simply dropped - there is nothing
	// to type to.
	inputSink  func([]byte)
	resizeSink func(cols, rows int)

	// Graphical-path state (rendering caches, blink animation,
	// selection drag, scrollbars, context menu).
	gfx purfecTermGfx

	// editorMode configures the terminal as a full-screen editor
	// display surface rather than a scrolling terminal: the scrollback
	// buffer is disabled, no scrollbar lane is reserved or drawn, and
	// the local Shift+navigation scroll keys are not intercepted (they
	// pass through to the child). An embedded editor - mew - drives its
	// display through a PurfecTerm in this mode.
	editorMode bool
}

// CellDebugInfo contains debug information about a clicked cell.
type CellDebugInfo struct {
	Col, Row      int
	Char          rune
	FgType        string
	FgR, FgG, FgB uint8
	FgIndex       uint8
	BgType        string
	BgR, BgG, BgB uint8
	BgIndex       uint8
	Bold          bool
	Underline     bool
	Reverse       bool
}

// NewPurfecTerm creates a new terminal emulator trinket.
func NewPurfecTerm() *PurfecTerm {
	t := &PurfecTerm{
		cols: 80,
		rows: 24,
	}
	t.TrinketBase = *core.NewTrinketBase()
	t.Init(t)
	t.SetFocusPolicy(core.StrongFocus)
	t.SetAccessibleRole(core.RoleTerminal)

	// Create terminal in embedded mode
	term, err := cli.New(cli.Options{
		Cols:           t.cols,
		Rows:           t.rows,
		ScrollbackSize: 1000,
		Embedded:       true,
	})
	if err != nil {
		// Terminal creation failed - trinket will show error state
		return t
	}
	t.terminal = term

	// Apply the app's theme palette (dark + light) so the terminal
	// renders with the same colors as the rest of the UI.
	t.SetColorScheme(termColorScheme())

	// Set up callbacks
	t.terminal.SetOnBell(func() {
		// Could trigger a visual bell or notification
	})

	// The emulator produces PTY-bound bytes from keyboard input; there is
	// no local PTY, so intercept them and hand them to the input sink
	// (which relays to the client's child process). Returning true
	// consumes the byte so the emulator never tries to write it locally.
	t.terminal.SetInputCallback(func(b []byte) bool {
		t.toChild(b)
		return true
	})

	return t
}

// SetInputSink installs the callback that receives bytes destined for the
// client's child process (keystrokes, mouse reports, paste). Passing nil
// detaches it, after which input is dropped.
func (t *PurfecTerm) SetInputSink(fn func([]byte)) { t.inputSink = fn }

// SetResizeSink installs the callback that receives the terminal grid size
// (columns, rows) whenever it changes, so the client can match its PTY
// winsize. It fires once immediately with the current size so a freshly
// attached client sizes its PTY without waiting for the next relayout.
func (t *PurfecTerm) SetResizeSink(fn func(cols, rows int)) {
	t.resizeSink = fn
	if fn != nil && t.cols > 0 && t.rows > 0 {
		fn(t.cols, t.rows)
	}
}

// toChild routes user-produced input to the child process. With a sink
// installed (the normal case) it relays upstream to the client; with none
// it is dropped - the render server has no PTY of its own.
func (t *PurfecTerm) toChild(b []byte) {
	if len(b) == 0 {
		return
	}
	if t.inputSink != nil {
		t.inputSink(b)
	}
}

// --- Mouse-report relay (embedded mode) -----------------------------------
//
// When the hosted app requests mouse tracking (DECSET 1000/1002/1003, SGR
// via 1006 — mew does this for its link buttons), the trinket relays encoded
// reports STRAIGHT to the child. The embedded cli.Terminal's own pseudo-key
// mouse path encodes reports too, but writes them only to its PTY — which an
// embedded terminal does not have — so the bytes were silently dropped.
// Until purfecterm's sendToPTY falls back to the input callback, the trinket
// owns this relay. With tracking OFF, everything falls through to the local
// pseudo-key path (selection, scrollback) exactly as before.

// mouseTracking reports the hosted app's mouse tracking and encoding modes
// (0 = off).
func (t *PurfecTerm) mouseTracking() (mode, enc int) {
	if t.terminal == nil {
		return 0, 0
	}
	buf := t.terminal.Buffer()
	if buf == nil {
		return 0, 0
	}
	return buf.GetMouseTrackingMode(), buf.GetMouseEncodingMode()
}

// sendMouseReport encodes one mouse event and hands the bytes to the child.
func (t *PurfecTerm) sendMouseReport(btn, cellX, cellY int, press bool, enc int) {
	if data := purfecterm.EncodeMouseEvent(btn, cellX, cellY, press, enc); data != nil {
		t.toChild(data)
	}
}

// purfMouseButton maps a toolkit button to purfecterm's report encoding.
func purfMouseButton(b core.MouseButton) (int, bool) {
	switch b {
	case core.LeftButton:
		return purfecterm.MouseButtonLeft, true
	case core.MiddleButton:
		return purfecterm.MouseButtonMiddle, true
	case core.RightButton:
		return purfecterm.MouseButtonRight, true
	}
	return 0, false
}

// emitResize notifies the sink of a new grid size (deduplication is the
// caller's - it only fires when cols/rows actually change).
func (t *PurfecTerm) emitResize(cols, rows int) {
	if t.resizeSink != nil && cols > 0 && rows > 0 {
		t.resizeSink(cols, rows)
	}
}

// CursorShape implements core.CursorProvider: the terminal shows the
// text I-beam while hovered, like any text surface.
func (t *PurfecTerm) CursorShape() core.CursorShape {
	return core.CursorText
}

// CursorShapeAt implements core.CursorShaper: the terminal shows the text
// I-beam over its content, but a plain arrow over the scrollbar lanes (which
// are chrome, not text). The coordinates arrive in the same space as
// HandleMouseMove, so the very geometry the scrollbar press path uses locates
// the lanes.
func (t *PurfecTerm) CursorShapeAt(x, y core.Unit) core.CursorShape {
	if t.overScrollLane(x, y) {
		return core.CursorDefault
	}
	return core.CursorText
}

// SetEditorMode configures the terminal as a full-screen editor display
// surface (see the editorMode field). Turning it on disables the
// scrollback buffer, reclaims the scrollbar lane for text, and stops
// the trinket from intercepting the Shift+navigation scroll keys so an
// embedded editor receives them. Turning it off restores normal
// scrolling-terminal behavior.
func (t *PurfecTerm) SetEditorMode(on bool) {
	t.editorMode = on
	if t.terminal != nil {
		if buf := t.terminal.Buffer(); buf != nil {
			buf.SetScrollbackDisabled(on)
		}
	}
	t.updateTerminalSize()
	t.Update()
}

// EditorMode reports whether the terminal is configured as an editor
// display surface (see SetEditorMode).
func (t *PurfecTerm) EditorMode() bool { return t.editorMode }

// overScrollLane reports whether a local point falls in either scrollbar
// track, mirroring scrollbarPress's hit tests.
func (t *PurfecTerm) overScrollLane(x, y core.Unit) bool {
	if t.editorMode {
		return false // no scrollbar lanes in editor mode
	}
	bounds := t.Bounds()
	if track, _, _, _, _, ok := t.vScrollGeometry(bounds); ok &&
		x >= track.X && y >= track.Y && y < track.Y+track.Height {
		return true
	}
	if track, _, _, _, _, ok := t.hScrollGeometry(bounds); ok &&
		y >= track.Y && x >= track.X && x < track.X+track.Width {
		return true
	}
	return false
}

// SetDarkTheme selects the terminal's dark (true) or light (false)
// palette, keeping it in step with the app theme. It sets both the
// current and preferred theme so a terminal reset stays consistent.
func (t *PurfecTerm) SetDarkTheme(dark bool) {
	if t.terminal == nil {
		return
	}
	if buf := t.terminal.Buffer(); buf != nil {
		buf.SetPreferredDarkTheme(dark)
		buf.SetDarkTheme(dark)
	}
	t.Update()
}

// SetOnCellClicked sets a callback for cell debug inspection.
// The callback receives detailed info about the clicked cell.
func (t *PurfecTerm) SetOnCellClicked(callback func(info CellDebugInfo)) {
	t.onCellClicked = callback
}

// Terminal returns the underlying cli.Terminal for advanced usage.
func (t *PurfecTerm) Terminal() *cli.Terminal {
	return t.terminal
}

// Close stops the terminal and cleans up resources.
func (t *PurfecTerm) Close() {
	t.stopGfxTimers()
	if t.terminal != nil {
		t.terminal.Close()
	}
}

// SetTerminalFont sets the terminal's own monospace font (family and
// size). On graphical targets the terminal's cell grid derives from
// this font's metrics; the text-based system keeps cell geometry
// regardless. nil restores the default (Monday 12).
func (t *PurfecTerm) SetTerminalFont(f *core.Font) {
	t.termFont = f
	t.updateTerminalSize()
	t.Update()
}

// TerminalFont returns the terminal's effective font (the app-chosen one
// or the monospace default).
func (t *PurfecTerm) TerminalFont() *core.Font { return t.effTermFont() }

// SetTerminalFontSize sets the terminal font's point size, keeping the
// current family (or the monospace default). On graphical targets the
// cell grid re-derives from the font at the new size. Values <= 0 are
// ignored.
func (t *PurfecTerm) SetTerminalFontSize(pt int) {
	if pt <= 0 {
		return
	}
	f := *t.effTermFont()
	f.Size = pt
	t.SetTerminalFont(&f)
}

// SetTerminalFontFamily sets the terminal font family, keeping the
// current point size (or the default). An empty name is ignored.
func (t *PurfecTerm) SetTerminalFontFamily(name string) {
	if name == "" {
		return
	}
	f := *t.effTermFont()
	f.Name = name
	t.SetTerminalFont(&f)
}

// defaultTermFont is the font used when no terminal font has been set: the
// "ui-term" alias, so the systematic ui-* tree and [window] ui_term config
// reach the grid (it resolves to the monospace default like the old "Monday",
// but now tracks reconfiguration). Its family MUST match the default render
// family in cellTextImage so the measured cell grid equals the rasterized
// glyphs. In the text (TUI) backend, "ui-term" is not the Tuesday design-aid,
// so it renders as the normal fixed-width Monday cell.
var defaultTermFont = core.Font{Name: "ui-term", Size: 12}

// effTermFont is the terminal's effective font: the app-chosen one, or
// the monospace default. Its Size is the app's REQUESTED point size
// (config getters/setters read it); rendering uses renderTermFont, which
// interprets that size relative to the interface font.
func (t *PurfecTerm) effTermFont() *core.Font {
	if t.termFont != nil {
		return t.termFont
	}
	f := defaultTermFont
	return &f
}

// renderTermFont resolves the point size actually rendered on graphical
// targets: the requested terminal size is interpreted RELATIVE to the
// interface (UI) font, with 12pt as the neutral anchor. So a terminal
// asking for 12pt renders at the interface font size, 14pt a touch
// larger, 10pt a touch smaller - keeping the terminal in proportion with
// the rest of the UI at any font_size. At the historical 12pt interface
// this is identity, so nothing changes for the default.
func (t *PurfecTerm) renderTermFont() *core.Font {
	f := *t.effTermFont()
	if ui := t.EffectiveFont(); ui != nil && ui.Size > 0 {
		f.Size = (f.Size*ui.Size + 6) / 12 // round(requested * interface / 12)
		if f.Size < 1 {
			f.Size = 1
		}
	}
	return &f
}

// cellDims returns the terminal's cell size in units.
//
// On graphical targets the grid must follow the real font: the cell is
// the effective terminal font's measured advance width and line height
// at its point size (answered by the render target - G1), so glyphs and
// the grid share one pitch. On the text-based system a cell is a
// character cell, so the inherited denomination (which a container may
// override) governs - and there MeasureText answers in cell units
// anyway, keeping the two paths identical for the default font.
func (t *PurfecTerm) cellDims() (cw, ch core.Unit) {
	if core.HasTextMeasurer() {
		f := t.renderTermFont()
		cw = f.MeasureText("M")
		ch = f.LineHeight()
		if cw > 0 && ch > 0 {
			return cw, ch
		}
	}
	m := t.EffectiveCellMetrics()
	return m.CellWidth, m.CellHeight
}

// SizeHint returns the preferred size based on terminal dimensions.
func (t *PurfecTerm) SizeHint() core.UnitSize {
	metrics := t.EffectiveCellMetrics()
	return core.UnitSize{
		Width:  metrics.TextWidth(t.cols),
		Height: metrics.TextHeight(t.rows),
	}
}

// SetBounds updates the trinket bounds and resizes the terminal.
func (t *PurfecTerm) SetBounds(bounds core.UnitRect) {
	t.TrinketBase.SetBounds(bounds)
	t.updateTerminalSize()
}

// updateTerminalSize recalculates and applies the terminal size.
//
// This is the text-mode path: it divides the bounds by the cell size in
// units, which is exact when cells are whole units. On graphical surfaces
// the cell size is a fractional pixel rate, so this undercounts by a
// row/column - and paintGraphical already sizes the grid authoritatively
// from the native-pixel viewport. Running both would flip the emulator
// between the two counts on every relayout-then-paint, and each shrink
// scrolls the bottom line into scrollback (a runaway growth loop with the
// cursor stranded a row above freshly-painted text). So defer to paint
// when graphical frames are active.
func (t *PurfecTerm) updateTerminalSize() {
	if t.terminal == nil {
		return
	}
	if core.FindGraphicalFrames(t) {
		return
	}
	bounds := t.Bounds()
	cw, ch := t.cellDims()

	width := bounds.Width
	if t.gfxInputActive() && !t.editorMode {
		// The vertical scrollbar lane is present on pixel surfaces
		// (except in editor mode, where text owns the full width):
		// reserve its width so it never covers text.
		width -= gfxScrollbarLane
	}
	newCols := int(width / cw)
	newRows := int(bounds.Height / ch)

	if newCols > 0 && newRows > 0 && (newCols != t.cols || newRows != t.rows) {
		t.cols = newCols
		t.rows = newRows
		t.terminal.Resize(t.cols, t.rows)
		t.emitResize(t.cols, t.rows)
	}
}

// Paint renders the terminal content.
func (t *PurfecTerm) Paint(p *core.Painter) {
	bounds := t.Bounds()
	metrics := t.EffectiveCellMetrics()
	theme := t.Theme()

	if t.terminal == nil {
		// Draw error state
		p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', theme.Normal)
		return
	}

	// Graphical targets get the pixel path (D1): terminal-font cell
	// grid, real bold/italic faces, cursor shapes.
	if p.Graphical() {
		t.paintGraphical(p, bounds)
		return
	}

	// Get terminal cells
	cells := t.terminal.GetCells()
	buf := t.terminal.Buffer()

	// Render each cell. The purfecterm grid is LOGICAL — one cell per
	// character, a wide character occupying ONE cell with its visual width
	// stored as an attribute (FlexWidth/CellWidth, the ?2027 model) — so map
	// logical cells to VISUAL columns by accumulating each cell's width,
	// exactly as the graphical path does. Without this, everything after a
	// wide glyph paints one column early and overlaps it. DEC double-width
	// lines route through the painter's DWL path (2x per cell): rows the
	// terminal fully owns become real ESC#6 lines; rows shared with other
	// windows degrade to double-spacing (see the TUI backend's EndFrame).
	for row, rowCells := range cells {
		y := metrics.CellToUnitsY(row)
		if y >= bounds.Height {
			break
		}
		var mode byte
		switch buf.GetVisibleLineAttribute(row) {
		case purfecterm.LineAttrDoubleWidth:
			mode = '6'
		case purfecterm.LineAttrDoubleTop:
			mode = '3'
		case purfecterm.LineAttrDoubleBottom:
			mode = '4'
		}

		acc := 0.0
		for col, cell := range rowCells {
			x := metrics.CellToUnitsX(int(acc))
			if x >= bounds.Width {
				break
			}

			// Per-cell visual width, same resolution as the graphical path.
			w := 1.0
			raw := buf.GetVisibleCell(col, row)
			if raw.CellWidth > 0 { // CellWidth is authoritative (see patches/purfecterm/PROTOCOL.md)
				w = raw.CellWidth
			}

			// Convert purfecterm cell to KittyTK style
			cellStyle := t.cellToStyle(cell)

			// Get the character (use space if empty)
			ch := cell.Char
			if ch == 0 {
				ch = ' '
			}

			if mode != 0 {
				p.DrawCellDWL(x, y, ch, cell.Combining, cellStyle, mode)
				acc += 2 * w
				continue
			}
			if cell.Combining != "" || w >= 1.5 {
				// DrawText attaches combining marks to the base cell and
				// claims the continuation column for a wide glyph.
				p.DrawText(x, y, string(ch)+cell.Combining, cellStyle, nil)
			} else {
				p.DrawCell(x, y, ch, cellStyle)
			}
			acc += w
		}
	}

	// Draw cursor if focused AND the terminal hasn't hidden its cursor
	// (some apps like vim/emacs manage their own cursor display)
	if t.HasFocus() && buf.IsCursorVisible() {
		cursorCol, cursorRow := buf.GetCursor()
		if cursorRow >= 0 && cursorRow < len(cells) && cursorCol >= 0 && cursorCol < t.cols {
			// The logical cursor column maps to a visual column through the
			// accumulated widths of the cells before it (doubled on a DEC
			// double-width line).
			mul := 1.0
			if buf.GetVisibleLineAttribute(cursorRow) != purfecterm.LineAttrNormal {
				mul = 2.0
			}
			acc := 0.0
			for c := 0; c < cursorCol; c++ {
				w := 1.0
				raw := buf.GetVisibleCell(c, cursorRow)
				if raw.CellWidth > 0 { // CellWidth is authoritative (see patches/purfecterm/PROTOCOL.md)
					w = raw.CellWidth
				}
				acc += w * mul
			}
			cursorX := metrics.CellToUnitsX(int(acc))
			cursorY := metrics.CellToUnitsY(cursorRow)
			if cursorX < bounds.Width && cursorY < bounds.Height {
				// Draw cursor as reverse video
				var ch rune = ' '
				if cursorCol < len(cells[cursorRow]) {
					ch = cells[cursorRow][cursorCol].Char
					if ch == 0 {
						ch = ' '
					}
				}
				cursorStyle := style.DefaultStyle().
					WithFg(style.ColorBlack).
					WithBg(style.ColorWhite)
				p.DrawCell(cursorX, cursorY, ch, cursorStyle)
			}
		}
	}
}

// cellToStyle converts a purfecterm RenderedCell to a KittyTK CellStyle.
func (t *PurfecTerm) cellToStyle(cell cli.RenderedCell) style.CellStyle {
	s := style.DefaultStyle()

	// Convert colors
	s = s.WithFg(t.convertColor(cell.Fg))
	s = s.WithBg(t.convertColor(cell.Bg))

	// Apply attributes
	if cell.Bold {
		s = s.Bold()
	}
	if cell.Underline {
		s = s.Underline()
	}
	if cell.Reverse {
		s = s.Reverse()
	}

	return s
}

// convertColor converts a purfecterm color to a KittyTK color.
func (t *PurfecTerm) convertColor(c purfecterm.Color) style.Color {
	// Check color type
	switch c.Type {
	case purfecterm.ColorTypeTrueColor:
		return style.RGB(int(c.R), int(c.G), int(c.B))
	case purfecterm.ColorTypePalette:
		return style.Color256(int(c.Index))
	case purfecterm.ColorTypeStandard:
		// Basic 16 colors
		switch c.Index {
		case 0:
			return style.ColorBlack
		case 1:
			return style.ColorRed
		case 2:
			return style.ColorGreen
		case 3:
			return style.ColorYellow
		case 4:
			return style.ColorBlue
		case 5:
			return style.ColorMagenta
		case 6:
			return style.ColorCyan
		case 7:
			return style.ColorWhite
		case 8:
			return style.ColorBrightBlack
		case 9:
			return style.ColorBrightRed
		case 10:
			return style.ColorBrightGreen
		case 11:
			return style.ColorBrightYellow
		case 12:
			return style.ColorBrightBlue
		case 13:
			return style.ColorBrightMagenta
		case 14:
			return style.ColorBrightCyan
		case 15:
			return style.ColorBrightWhite
		}
	}
	return style.ColorDefault
}

// HandleKeyPress handles keyboard input and forwards to the terminal.
func (t *PurfecTerm) HandleKeyPress(event core.KeyPressEvent) bool {
	if t.terminal == nil {
		return false
	}

	// Ensure terminal knows it's focused before handling input
	t.terminal.SetFocused(true)

	// Scrollback navigation is handled locally and never reaches the
	// child: since input is consumed by the sink callback the emulator's
	// own local-key path no longer runs, so honour the Shift+nav keys here.
	// Editor mode has no scrollback, so those keys pass through to the
	// child (the editor) like any other key.
	if !t.editorMode && t.handleScrollbackKey(event.Key) {
		t.Update()
		return true
	}

	// Typing must never happen behind an invisible cursor: restart
	// the blink phase so the cursor shows immediately.
	t.resetCursorBlink()

	// Forward the key to the terminal
	t.terminal.HandleKeyString(event.Key)
	t.Update()
	return true
}

// handleScrollbackKey processes the Shift-modified scrollback navigation
// keys locally (they scroll the view, they are not sent to the child).
// Returns true if the key was one of them.
func (t *PurfecTerm) handleScrollbackKey(key string) bool {
	page := t.rows - 1
	if page < 1 {
		page = 1
	}
	switch key {
	case "S-PageUp":
		t.ScrollUp(page)
	case "S-PageDown":
		t.ScrollDown(page)
	case "S-Up":
		t.ScrollUp(1)
	case "S-Down":
		t.ScrollDown(1)
	case "S-Home":
		t.ScrollToTop()
	case "S-End":
		t.ScrollToBottom()
	default:
		return false
	}
	return true
}

// HandleMousePress handles mouse clicks to focus the terminal and forward to CLI.
func (t *PurfecTerm) HandleMousePress(event core.MousePressEvent) bool {
	t.SetFocus()
	if t.terminal == nil {
		return true
	}
	if t.gfxInputActive() {
		// Graphical path: local selection, mouse reporting with the
		// Shift bypass, scrollbars, and the right-click context menu.
		return t.gfxMousePress(event)
	}

	// Track held button for drag events
	t.heldButton = event.Button

	// Convert unit coordinates to cell coordinates (terminal-font
	// cells, which equal toolkit cells for the default font)
	cw, chh := t.cellDims()
	cellCol := int(event.X / cw)  // 0-based for internal use
	cellRow := int(event.Y / chh) // 0-based for internal use

	// Debug callback - extract cell info
	if t.onCellClicked != nil {
		cells := t.terminal.GetCells()
		if cellRow < len(cells) && cellCol < len(cells[cellRow]) {
			cell := cells[cellRow][cellCol]
			info := CellDebugInfo{
				Col:       cellCol,
				Row:       cellRow,
				Char:      cell.Char,
				Bold:      cell.Bold,
				Underline: cell.Underline,
				Reverse:   cell.Reverse,
			}
			// Extract foreground color info
			switch cell.Fg.Type {
			case purfecterm.ColorTypeTrueColor:
				info.FgType = "RGB"
				info.FgR, info.FgG, info.FgB = cell.Fg.R, cell.Fg.G, cell.Fg.B
			case purfecterm.ColorTypePalette:
				info.FgType = "256"
				info.FgIndex = cell.Fg.Index
			case purfecterm.ColorTypeStandard:
				info.FgType = "Std"
				info.FgIndex = cell.Fg.Index
			default:
				info.FgType = "Def"
			}
			// Extract background color info
			switch cell.Bg.Type {
			case purfecterm.ColorTypeTrueColor:
				info.BgType = "RGB"
				info.BgR, info.BgG, info.BgB = cell.Bg.R, cell.Bg.G, cell.Bg.B
			case purfecterm.ColorTypePalette:
				info.BgType = "256"
				info.BgIndex = cell.Bg.Index
			case purfecterm.ColorTypeStandard:
				info.BgType = "Std"
				info.BgIndex = cell.Bg.Index
			default:
				info.BgType = "Def"
			}
			t.onCellClicked(info)
		}
	}

	// Convert to 1-based coordinates for CLI adapter
	cellX := cellCol + 1
	cellY := cellRow + 1

	// App-owned mouse: relay the encoded press straight to the child.
	if mode, enc := t.mouseTracking(); mode != 0 {
		if btn, ok := purfMouseButton(event.Button); ok {
			t.sendMouseReport(btn, cellX, cellY, true, enc)
		}
		t.Update()
		return true
	}

	// Send position update first
	t.terminal.HandleKeyString(fmt.Sprintf("Mouse@%d,%d", cellX, cellY))

	// Send button press
	var buttonStr string
	switch event.Button {
	case core.LeftButton:
		buttonStr = "MouseLeftPress"
	case core.MiddleButton:
		buttonStr = "MouseMiddlePress"
	case core.RightButton:
		buttonStr = "MouseRightPress"
	default:
		return true
	}
	t.terminal.HandleKeyString(buttonStr)
	t.Update()
	return true
}

// HandleMouseRelease handles mouse button releases.
func (t *PurfecTerm) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	if t.terminal == nil {
		return false
	}
	if t.gfxInputActive() {
		return t.gfxMouseRelease(event)
	}

	// Containers broadcast releases to every child; only act on a
	// release whose press we actually saw, so sibling trinkets are not
	// starved and the terminal never receives a release for a press
	// that landed elsewhere.
	if t.heldButton != event.Button {
		return false
	}
	t.heldButton = core.NoButton

	// Convert unit coordinates to 1-based cell coordinates
	cw, chh := t.cellDims()
	cellX := int(event.X/cw) + 1
	cellY := int(event.Y/chh) + 1

	// App-owned mouse: relay the encoded release straight to the child.
	if mode, enc := t.mouseTracking(); mode != 0 {
		if btn, ok := purfMouseButton(event.Button); ok {
			t.sendMouseReport(btn, cellX, cellY, false, enc)
		}
		t.Update()
		return true
	}

	// Send position update first
	t.terminal.HandleKeyString(fmt.Sprintf("Mouse@%d,%d", cellX, cellY))

	// Send button release
	var buttonStr string
	switch event.Button {
	case core.LeftButton:
		buttonStr = "MouseLeftRelease"
	case core.MiddleButton:
		buttonStr = "MouseMiddleRelease"
	case core.RightButton:
		buttonStr = "MouseRightRelease"
	default:
		return false
	}
	t.terminal.HandleKeyString(buttonStr)
	t.Update()
	return true
}

// HandleMouseMove handles mouse movement/drag events.
func (t *PurfecTerm) HandleMouseMove(event core.MouseMoveEvent) bool {
	if t.terminal == nil {
		return false
	}
	if t.gfxInputActive() {
		return t.gfxMouseMove(event)
	}

	// Convert unit coordinates to 1-based cell coordinates
	cw, chh := t.cellDims()
	cellX := int(event.X/cw) + 1
	cellY := int(event.Y/chh) + 1

	// App-owned mouse: relay motion per the tracking mode — drags from 1002
	// up, plain motion only under all-motion (1003). Motion the mode does
	// not report is swallowed (the app owns the mouse either way).
	if mode, enc := t.mouseTracking(); mode != 0 {
		if btn, ok := purfMouseButton(t.heldButton); ok {
			if mode >= 1002 {
				t.sendMouseReport(btn|purfecterm.MouseMotionFlag, cellX, cellY, true, enc)
			}
		} else if mode >= 1003 {
			t.sendMouseReport(purfecterm.MouseButtonNone|purfecterm.MouseMotionFlag, cellX, cellY, true, enc)
		}
		t.Update()
		return true
	}

	// Use tracked button state for drag events (since event.Buttons may not be set)
	switch t.heldButton {
	case core.LeftButton:
		t.terminal.HandleKeyString(fmt.Sprintf("MouseLeftDrag@%d,%d", cellX, cellY))
	case core.MiddleButton:
		t.terminal.HandleKeyString(fmt.Sprintf("MouseMiddleDrag@%d,%d", cellX, cellY))
	case core.RightButton:
		t.terminal.HandleKeyString(fmt.Sprintf("MouseRightDrag@%d,%d", cellX, cellY))
	default:
		// Plain movement (for mouse tracking modes)
		t.terminal.HandleKeyString(fmt.Sprintf("Mouse@%d,%d", cellX, cellY))
	}
	t.Update()
	return true
}

// HandleMouseWheel handles scroll wheel events.
func (t *PurfecTerm) HandleMouseWheel(event core.MouseWheelEvent) bool {
	if t.terminal == nil {
		return false
	}
	// Terminals consume every wheel over them; claim the gesture so
	// pointer drift mid-scroll cannot re-target (core wheel latch).
	core.ClaimWheelGesture(event, t.HandleMouseWheel)
	if t.gfxInputActive() {
		return t.gfxMouseWheel(event)
	}

	// Convert unit coordinates to 1-based cell coordinates
	cw, chh := t.cellDims()
	cellX := int(event.X/cw) + 1
	cellY := int(event.Y/chh) + 1

	// App-owned mouse: relay the wheel as scroll-button presses.
	if mode, enc := t.mouseTracking(); mode != 0 {
		if event.DeltaY < 0 {
			t.sendMouseReport(purfecterm.MouseScrollUp, cellX, cellY, true, enc)
		} else if event.DeltaY > 0 {
			t.sendMouseReport(purfecterm.MouseScrollDown, cellX, cellY, true, enc)
		}
		t.Update()
		return true
	}

	// Send position update first
	t.terminal.HandleKeyString(fmt.Sprintf("Mouse@%d,%d", cellX, cellY))

	// Send scroll event based on direction
	if event.DeltaY < 0 {
		t.terminal.HandleKeyString("MouseScrollUp")
	} else if event.DeltaY > 0 {
		t.terminal.HandleKeyString("MouseScrollDown")
	}
	t.Update()
	return true
}

// HandleFocusIn is called when the trinket gains focus.
func (t *PurfecTerm) HandleFocusIn() {
	t.TrinketBase.HandleFocusIn()
	if t.terminal != nil {
		t.terminal.SetFocused(true)
	}
	t.Update()
}

// HandleFocusOut is called when the trinket loses focus.
func (t *PurfecTerm) HandleFocusOut() {
	t.TrinketBase.HandleFocusOut()
	if t.terminal != nil {
		t.terminal.SetFocused(false)
	}
	t.Update()
}

// Write sends data to the child process as if typed. It routes through
// the input sink to the client that owns the PTY (there is no server-side
// PTY); with no sink installed the bytes are dropped.
func (t *PurfecTerm) Write(data []byte) (int, error) {
	t.toChild(data)
	return len(data), nil
}

// Feed writes bytes directly to the terminal DISPLAY (parsed into the
// screen buffer as if they were program output), bypassing the PTY.
// This is the display-direction sink behind the wire's feed=
// pseudo-property; Write, by contrast, is keyboard input to the child
// process.
func (t *PurfecTerm) Feed(data []byte) {
	if t.terminal == nil {
		return
	}
	t.terminal.Feed(data)
	t.Update()
}

// ScrollUp scrolls the terminal view up by n lines.
func (t *PurfecTerm) ScrollUp(n int) {
	if t.terminal != nil {
		t.terminal.ScrollUp(n)
		t.Update()
	}
}

// ScrollDown scrolls the terminal view down by n lines.
func (t *PurfecTerm) ScrollDown(n int) {
	if t.terminal != nil {
		t.terminal.ScrollDown(n)
		t.Update()
	}
}

// ScrollToTop scrolls to the top of the scrollback buffer.
func (t *PurfecTerm) ScrollToTop() {
	if t.terminal != nil {
		t.terminal.ScrollToTop()
		t.Update()
	}
}

// ScrollToBottom scrolls to the bottom (current output).
func (t *PurfecTerm) ScrollToBottom() {
	if t.terminal != nil {
		t.terminal.ScrollToBottom()
		t.Update()
	}
}

// AccessibleInfo returns accessibility information.
func (t *PurfecTerm) AccessibleInfo() core.AccessibleInfo {
	info := t.AccessibleTrinket.AccessibleInfo()
	info.Role = core.RoleTerminal
	info.Name = "Terminal"
	return info
}
