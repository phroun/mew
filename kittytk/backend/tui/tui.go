// Package backend provides rendering backends for KittyTK.
package tui

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/phroun/direct-key-handler/keyboard"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
	"golang.org/x/term"
)

// Cell represents a single character cell on the terminal.
type Cell struct {
	Char  rune
	Style style.CellStyle
}

// TUIBackend implements RenderBackend for terminal rendering.
type TUIBackend struct {
	mu sync.Mutex

	// Terminal state
	fd       int
	oldState *term.State
	cols     int
	rows     int

	// Cell metrics for unit conversion
	metrics core.CellMetrics

	// Screen buffers (double buffering)
	frontBuffer [][]Cell
	backBuffer  [][]Cell

	// Current state
	currentStyle  style.CellStyle
	clipRect      core.UnitRect
	cursorX       int
	cursorY       int
	cursorVisible bool

	// Input handling
	keyboard   *keyboard.Handler
	eventQueue chan core.Event
	stopChan   chan struct{}

	// Mouse state (for tracking position between Mouse@x,y and action events)
	pendingMouseX int
	pendingMouseY int

	// Output writer
	output io.Writer

	// Capabilities
	colorDepth int
	hasMouse   bool
	hasUnicode bool

	// Flag to clear lines on next render (after resize)
	needsLineClear bool

	// clipboard is the host's internal clipboard - the fallback Paste source
	// when OSC 52 read-back is off or the terminal doesn't answer. Copy/Cut
	// mirror it to the terminal's clipboard via OSC 52 when osc52 is set.
	clipboard string
	osc52     bool

	// osc52Paste enables OSC 52 read-back: a clipboard read queries the
	// terminal (RequestClipboardRead) and the reply arrives asynchronously via
	// the keyboard handler's OnClipboard callback. onClipboardRead is the
	// registered sink for that reply (see SetClipboardReadHandler); the desktop
	// wires it to drive a "waiting for clipboard" modal so the event loop is
	// never blocked while the terminal prompts the user.
	osc52Paste      bool
	onClipboardRead func(string)
}

// TUIOptions configures the TUI backend.
type TUIOptions struct {
	// Output is where to write terminal output (default: os.Stdout)
	Output io.Writer

	// Input is where to read input from (default: os.Stdin)
	Input io.Reader

	// CellMetrics defines unit-to-cell mapping (default: 8x16)
	CellMetrics core.CellMetrics

	// ColorDepth: 2, 16, 256, or 16777216 (default: auto-detect)
	ColorDepth int

	// EnableMouse enables mouse input (default: true)
	EnableMouse bool

	// AlternateScreen uses the alternate screen buffer (default: true)
	AlternateScreen bool

	// OSC52Clipboard mirrors Copy/Cut to the terminal's clipboard with the
	// OSC 52 escape sequence (supported by iTerm2, xterm, kitty, wezterm,
	// tmux with set-clipboard, ...). When false the host uses its own internal
	// clipboard only. Default: true.
	OSC52Clipboard bool

	// OSC52Paste enables OSC 52 clipboard read-back for Paste: query the
	// terminal for its clipboard and use the reply, falling back to the
	// internal clipboard when the terminal doesn't answer (many disable read
	// for security). Off by default; implies OSC52Clipboard for the query.
	OSC52Paste bool
}

// DefaultTUIOptions returns default options.
func DefaultTUIOptions() TUIOptions {
	return TUIOptions{
		Output:          os.Stdout,
		Input:           os.Stdin,
		CellMetrics:     core.DefaultCellMetrics(),
		ColorDepth:      0, // Auto-detect
		EnableMouse:     true,
		AlternateScreen: true,
		OSC52Clipboard:  true,
	}
}

// NewTUIBackend creates a new terminal backend.
func NewTUIBackend(opts TUIOptions) *TUIBackend {
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
	if opts.Input == nil {
		opts.Input = os.Stdin
	}
	if opts.CellMetrics.CellWidth == 0 {
		opts.CellMetrics = core.DefaultCellMetrics()
	}

	t := &TUIBackend{
		metrics:    opts.CellMetrics,
		output:     opts.Output,
		eventQueue: make(chan core.Event, 256),
		stopChan:   make(chan struct{}),
		colorDepth: opts.ColorDepth,
		hasMouse:   opts.EnableMouse,
		hasUnicode: true, // Assume Unicode support
		osc52:      opts.OSC52Clipboard,
		osc52Paste: opts.OSC52Paste,
	}
	return t
}

// Init initializes the terminal backend.
func (t *TUIBackend) Init() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Get terminal file descriptor
	if f, ok := t.output.(*os.File); ok {
		t.fd = int(f.Fd())
	} else {
		t.fd = -1
	}

	// Get terminal size
	if t.fd >= 0 && term.IsTerminal(t.fd) {
		cols, rows, err := term.GetSize(t.fd)
		if err != nil {
			return fmt.Errorf("failed to get terminal size: %w", err)
		}
		t.cols = cols
		t.rows = rows
	} else {
		// Default size for non-terminal output
		t.cols = 80
		t.rows = 24
	}

	// Auto-detect color depth
	if t.colorDepth == 0 {
		t.colorDepth = detectColorDepth()
	}

	// Allocate buffers
	t.allocateBuffers()

	// Open /dev/tty directly to ensure escape sequences reach the terminal
	// This bypasses any stdout redirection
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		tty = os.Stdout
	}

	// Enable Kitty keyboard protocol for better key detection
	fmt.Fprint(tty, "\033[>1u")

	// Enable mouse if requested
	if t.hasMouse {
		fmt.Fprint(tty, "\033[?1000h\033[?1002h\033[?1006h")
	}

	// Enter alternate screen
	fmt.Fprint(tty, "\033[?1049h")

	// Hide cursor initially
	fmt.Fprint(tty, "\033[?25l")

	// Close tty if we opened it separately
	if tty != os.Stdout {
		tty.Close()
	}

	// Set up keyboard handler AFTER terminal modes are configured
	kbOpts := keyboard.Options{
		InputReader: os.Stdin,
	}
	t.keyboard = keyboard.New(kbOpts)
	t.keyboard.OnKey = t.handleKey
	if t.osc52Paste {
		// OSC 52 clipboard responses (replies to our read query) are delivered
		// here, not as keystrokes: keep the internal copy in sync and notify the
		// registered reader (the desktop resolves the pending paste).
		t.keyboard.OnClipboard = func(_ byte, data []byte) {
			t.deliverClipboard(string(data))
		}
	}

	// Now start the keyboard handler
	if err := t.keyboard.Start(); err != nil {
		return fmt.Errorf("failed to start keyboard handler: %w", err)
	}

	// Handle terminal resize
	go t.handleResize()

	return nil
}

// Shutdown cleans up the terminal backend.
func (t *TUIBackend) Shutdown() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Signal stop
	close(t.stopChan)

	// Stop keyboard handler
	if t.keyboard != nil {
		t.keyboard.Stop()
	}

	// Disable Kitty keyboard protocol
	t.write("\033[<u")

	// Disable mouse
	if t.hasMouse {
		t.write("\033[?1006l\033[?1002l\033[?1000l")
	}

	// Show cursor
	t.write("\033[?25h")

	// Leave alternate screen
	t.write("\033[?1049l")

	// Reset colors
	t.write("\033[0m")
}

// allocateBuffers creates the screen buffers.
func (t *TUIBackend) allocateBuffers() {
	t.frontBuffer = make([][]Cell, t.rows)
	t.backBuffer = make([][]Cell, t.rows)

	defaultCell := Cell{Char: ' ', Style: style.DefaultStyle()}

	for y := 0; y < t.rows; y++ {
		t.frontBuffer[y] = make([]Cell, t.cols)
		t.backBuffer[y] = make([]Cell, t.cols)
		for x := 0; x < t.cols; x++ {
			t.frontBuffer[y][x] = defaultCell
			t.backBuffer[y][x] = defaultCell
		}
	}
}

// Size returns the current size in abstract units.
func (t *TUIBackend) Size() core.UnitSize {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.metrics.CellsToUnits(t.cols, t.rows)
}

// Metrics returns the cell metrics.
func (t *TUIBackend) Metrics() core.CellMetrics {
	return t.metrics
}

// BeginFrame starts a new frame for rendering.
func (t *TUIBackend) BeginFrame() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Clear back buffer
	defaultCell := Cell{Char: ' ', Style: style.DefaultStyle()}
	for y := 0; y < t.rows; y++ {
		for x := 0; x < t.cols; x++ {
			t.backBuffer[y][x] = defaultCell
		}
	}

	// Reset clip
	t.clipRect = core.UnitRect{
		Width:  t.metrics.CellToUnitsX(t.cols),
		Height: t.metrics.CellToUnitsY(t.rows),
	}
}

// EndFrame completes the frame and presents it.
func (t *TUIBackend) EndFrame() {
	t.mu.Lock()
	defer t.mu.Unlock()

	var sb strings.Builder
	clearLines := t.needsLineClear
	t.needsLineClear = false

	// Perform differential update
	for y := 0; y < t.rows; y++ {
		lineCleared := false

		// After resize, clear each line before updating
		if clearLines {
			// Move to line start, reset attributes, clear line
			sb.WriteString(fmt.Sprintf("\033[%d;1H\033[0m\033[2K", y+1))
			lineCleared = true
		}

		for x := 0; x < t.cols; x++ {
			// Check if cell below has overline attribute - if so, we need to add underline
			cellBelowHasOverline := false
			if y+1 < t.rows {
				belowStyle := t.backBuffer[y+1][x].Style
				if belowStyle.Attrs&style.StyleOverline != 0 {
					cellBelowHasOverline = true
				}
			}

			// Determine the effective cell for comparison (with underline from overline below)
			effectiveCell := t.backBuffer[y][x]
			if cellBelowHasOverline {
				effectiveCell.Style.Attrs |= style.StyleUnderline
			}

			if lineCleared || effectiveCell != t.frontBuffer[y][x] {
				// Move cursor to position
				sb.WriteString(fmt.Sprintf("\033[%d;%dH", y+1, x+1))

				// Set style (use effective style with underline from overline below)
				sb.WriteString(effectiveCell.Style.Code())

				// Write character
				if effectiveCell.Char == 0 {
					sb.WriteRune(' ')
				} else {
					sb.WriteRune(effectiveCell.Char)
				}

				// Update front buffer with effective cell
				t.frontBuffer[y][x] = effectiveCell
			}
		}
	}

	// Restore cursor position if visible
	if t.cursorVisible {
		sb.WriteString(fmt.Sprintf("\033[%d;%dH", t.cursorY+1, t.cursorX+1))
		sb.WriteString("\033[?25h")
	}

	t.write(sb.String())
}

// Clear fills the entire surface with a style.
func (t *TUIBackend) Clear(s style.CellStyle) {
	t.mu.Lock()
	defer t.mu.Unlock()

	cell := Cell{Char: ' ', Style: s}
	for y := 0; y < t.rows; y++ {
		for x := 0; x < t.cols; x++ {
			t.backBuffer[y][x] = cell
		}
	}
}

// SetClip sets the clipping rectangle.
func (t *TUIBackend) SetClip(clip core.UnitRect) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clipRect = clip
}

// isInClip checks if a cell coordinate is within the clip region.
// A cell is considered in clip if its starting position is within bounds.
func (t *TUIBackend) isInClip(col, row int) bool {
	x := t.metrics.CellToUnitsX(col)
	y := t.metrics.CellToUnitsY(row)
	return t.clipRect.Contains(core.UnitPoint{X: x, Y: y})
}

// cellFitsInClip checks if a cell fully fits within the clip region.
// Used for optional trailing elements like Tuesday font spacing.
func (t *TUIBackend) cellFitsInClip(col, row int) bool {
	x := t.metrics.CellToUnitsX(col)
	y := t.metrics.CellToUnitsY(row)
	// Check if cell end position is within clip (cell end = start + cell width)
	cellEndX := x + t.metrics.CellWidth
	cellEndY := y + t.metrics.CellHeight
	return x >= t.clipRect.X && cellEndX <= t.clipRect.X+t.clipRect.Width &&
		y >= t.clipRect.Y && cellEndY <= t.clipRect.Y+t.clipRect.Height
}

// setCell sets a cell in the back buffer with clipping.
func (t *TUIBackend) setCell(col, row int, ch rune, s style.CellStyle) {
	if col < 0 || col >= t.cols || row < 0 || row >= t.rows {
		return
	}
	if !t.isInClip(col, row) {
		return
	}
	t.backBuffer[row][col] = Cell{Char: ch, Style: s}
}

// DrawCell draws a single character at the given position.
func (t *TUIBackend) DrawCell(x, y core.Unit, ch rune, s style.CellStyle) {
	t.mu.Lock()
	defer t.mu.Unlock()

	col := t.metrics.UnitsToCellX(x)
	row := t.metrics.UnitsToCellY(y)
	t.setCell(col, row, ch, s)
}

// DrawText draws a string starting at the given position using the given font.
func (t *TUIBackend) DrawText(x, y core.Unit, text string, s style.CellStyle, font *core.Font) core.Unit {
	t.mu.Lock()
	defer t.mu.Unlock()

	if font == nil {
		font = core.DefaultFont()
	}

	// Apply font's foreground color if set (for debugging/visualization)
	effectiveStyle := s
	if !font.Foreground.IsDefault {
		effectiveStyle = s.WithFg(font.Foreground.Color)
	}

	col := t.metrics.UnitsToCellX(x)
	row := t.metrics.UnitsToCellY(y)

	startCol := col
	isTuesday := font.Name == "Tuesday"

	for _, ch := range text {
		if col >= t.cols {
			break
		}
		t.setCell(col, row, ch, effectiveStyle)
		col++

		// Handle wide characters (CJK, emoji)
		if runeWidth(ch) > 1 {
			if col < t.cols {
				t.setCell(col, row, 0, effectiveStyle) // Placeholder for wide char
				col++
			}
		} else if isTuesday && isAlphanumeric(ch) {
			// Tuesday font: add space after alphabetic/numeric chars
			// Only add the space if the cell fully fits in the clip region,
			// allowing "half" of a wide Tuesday character to be shown when truncated
			if col < t.cols && t.cellFitsInClip(col, row) {
				t.setCell(col, row, ' ', effectiveStyle)
				col++
			}
		}
	}

	return t.metrics.TextWidth(col - startCol)
}

// isAlphanumeric returns true if the character is a letter or digit.
func isAlphanumeric(ch rune) bool {
	return (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
}

// DrawTextAligned draws text aligned within a box using the given font.
func (t *TUIBackend) DrawTextAligned(bounds core.UnitRect, text string, hAlign, vAlign core.Alignment, s style.CellStyle, font *core.Font) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if font == nil {
		font = core.DefaultFont()
	}

	// Apply font's foreground color if set (for debugging/visualization)
	effectiveStyle := s
	if !font.Foreground.IsDefault {
		effectiveStyle = s.WithFg(font.Foreground.Color)
	}

	// Convert bounds to cells
	col1 := t.metrics.UnitsToCellX(bounds.X)
	row1 := t.metrics.UnitsToCellY(bounds.Y)
	col2 := t.metrics.UnitsToCellX(bounds.X + bounds.Width)
	row2 := t.metrics.UnitsToCellY(bounds.Y + bounds.Height)

	boxWidth := col2 - col1
	boxHeight := row2 - row1

	isTuesday := font.Name == "Tuesday"

	// Calculate text width in cells accounting for font
	textCells := 0
	for _, ch := range text {
		textCells++
		if runeWidth(ch) > 1 {
			textCells++
		} else if isTuesday && isAlphanumeric(ch) {
			textCells++ // Extra cell for spacing
		}
	}

	// Calculate horizontal position
	var col int
	switch hAlign {
	case core.AlignLeft:
		col = col1
	case core.AlignCenter:
		col = col1 + (boxWidth-textCells)/2
	case core.AlignRight:
		col = col2 - textCells
	default:
		col = col1
	}

	// Calculate vertical position
	var row int
	switch vAlign {
	case core.AlignTop:
		row = row1
	case core.AlignMiddle:
		row = row1 + boxHeight/2
	case core.AlignBottom:
		row = row2 - 1
	default:
		row = row1
	}

	// Draw text
	for _, ch := range text {
		if col >= col2 {
			break
		}
		if col >= col1 {
			t.setCell(col, row, ch, effectiveStyle)
		}
		col++

		// Handle wide characters
		if runeWidth(ch) > 1 {
			if col < col2 && col >= col1 {
				t.setCell(col, row, 0, effectiveStyle)
			}
			col++
		} else if isTuesday && isAlphanumeric(ch) {
			// Tuesday font: add space after alphabetic/numeric chars
			// Only add the space if the cell fully fits within bounds,
			// allowing "half" of a wide Tuesday character to be shown when truncated
			cellEndX := t.metrics.CellToUnitsX(col) + t.metrics.CellWidth
			if col < col2 && col >= col1 && cellEndX <= bounds.X+bounds.Width {
				t.setCell(col, row, ' ', effectiveStyle)
			}
			col++
		}
	}
}

// FillRect fills a rectangle with a character and style.
func (t *TUIBackend) FillRect(r core.UnitRect, ch rune, s style.CellStyle) {
	t.mu.Lock()
	defer t.mu.Unlock()

	col1 := t.metrics.UnitsToCellX(r.X)
	row1 := t.metrics.UnitsToCellY(r.Y)
	col2 := t.metrics.UnitsToCellX(r.X + r.Width)
	row2 := t.metrics.UnitsToCellY(r.Y + r.Height)

	for row := row1; row < row2; row++ {
		for col := col1; col < col2; col++ {
			t.setCell(col, row, ch, s)
		}
	}
}

// DrawRect draws just the border of a rectangle.
func (t *TUIBackend) DrawRect(r core.UnitRect, border style.BorderStyle, s style.CellStyle) {
	t.mu.Lock()
	defer t.mu.Unlock()

	col1 := t.metrics.UnitsToCellX(r.X)
	row1 := t.metrics.UnitsToCellY(r.Y)
	col2 := t.metrics.UnitsToCellX(r.X+r.Width) - 1
	row2 := t.metrics.UnitsToCellY(r.Y+r.Height) - 1

	if col2 < col1 || row2 < row1 {
		return
	}

	// Corners
	t.setCell(col1, row1, border.TopLeft, s)
	t.setCell(col2, row1, border.TopRight, s)
	t.setCell(col1, row2, border.BottomLeft, s)
	t.setCell(col2, row2, border.BottomRight, s)

	// Top and bottom edges
	for col := col1 + 1; col < col2; col++ {
		t.setCell(col, row1, border.Horizontal, s)
		t.setCell(col, row2, border.Horizontal, s)
	}

	// Left and right edges
	for row := row1 + 1; row < row2; row++ {
		t.setCell(col1, row, border.Vertical, s)
		t.setCell(col2, row, border.Vertical, s)
	}
}

// DrawHLine draws a horizontal line.
func (t *TUIBackend) DrawHLine(x, y, width core.Unit, ch rune, s style.CellStyle) {
	t.mu.Lock()
	defer t.mu.Unlock()

	col := t.metrics.UnitsToCellX(x)
	row := t.metrics.UnitsToCellY(y)
	endCol := t.metrics.UnitsToCellX(x + width)

	for c := col; c < endCol; c++ {
		t.setCell(c, row, ch, s)
	}
}

// DrawVLine draws a vertical line.
func (t *TUIBackend) DrawVLine(x, y, height core.Unit, ch rune, s style.CellStyle) {
	t.mu.Lock()
	defer t.mu.Unlock()

	col := t.metrics.UnitsToCellX(x)
	row := t.metrics.UnitsToCellY(y)
	endRow := t.metrics.UnitsToCellY(y + height)

	for r := row; r < endRow; r++ {
		t.setCell(col, r, ch, s)
	}
}

// DrawBox draws a box with optional title.
func (t *TUIBackend) DrawBox(r core.UnitRect, border style.BorderStyle, title string, s style.CellStyle) {
	// Draw the rectangle border
	t.DrawRect(r, border, s)

	if title == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Draw title on top edge
	col1 := t.metrics.UnitsToCellX(r.X)
	col2 := t.metrics.UnitsToCellX(r.X + r.Width)
	row := t.metrics.UnitsToCellY(r.Y)

	titleLen := utf8.RuneCountInString(title)
	maxLen := col2 - col1 - 4 // Leave space for " Title "
	if maxLen < 1 {
		return
	}

	displayTitle := title
	if titleLen > maxLen {
		displayTitle = string([]rune(title)[:maxLen-1]) + "…"
		titleLen = maxLen
	}

	// Center title
	startCol := col1 + 2
	t.setCell(startCol-1, row, ' ', s)
	col := startCol
	for _, ch := range displayTitle {
		t.setCell(col, row, ch, s)
		col++
	}
	t.setCell(col, row, ' ', s)
}

// PollEvent returns the next input event, or nil if none available.
func (t *TUIBackend) PollEvent() core.Event {
	select {
	case event := <-t.eventQueue:
		return event
	default:
		return nil
	}
}

// WaitEvent blocks until an event is available.
func (t *TUIBackend) WaitEvent() core.Event {
	select {
	case event := <-t.eventQueue:
		return event
	case <-t.stopChan:
		return core.QuitEvent{}
	}
}

// SetCursorVisible shows or hides the cursor.
func (t *TUIBackend) SetCursorVisible(visible bool) {
	t.mu.Lock()
	t.cursorVisible = visible
	t.mu.Unlock()

	if visible {
		t.write("\033[?25h")
	} else {
		t.write("\033[?25l")
	}
}

// SetCursorPosition positions the cursor.
func (t *TUIBackend) SetCursorPosition(x, y core.Unit) {
	t.mu.Lock()
	t.cursorX = t.metrics.UnitsToCellX(x)
	t.cursorY = t.metrics.UnitsToCellY(y)
	t.mu.Unlock()

	t.write(fmt.Sprintf("\033[%d;%dH", t.cursorY+1, t.cursorX+1))
}

// SupportsColor returns whether the backend supports color.
func (t *TUIBackend) SupportsColor() bool {
	return t.colorDepth > 2
}

// SupportsMouse returns whether the backend supports mouse input.
func (t *TUIBackend) SupportsMouse() bool {
	return t.hasMouse
}

// SupportsUnicode returns whether the backend supports Unicode.
func (t *TUIBackend) SupportsUnicode() bool {
	return t.hasUnicode
}

// ColorDepth returns the number of colors supported.
func (t *TUIBackend) ColorDepth() int {
	return t.colorDepth
}

// GetClipboard returns the host's internal clipboard - what Copy/Cut last
// stored (and the latest OSC 52 read-back reply, which is mirrored into it).
// This never blocks; the actual terminal query is the async RequestClipboardRead
// path (the AsyncClipboardReader capability), which the desktop drives so it can
// show a "waiting for clipboard" modal while the terminal prompts the user.
func (t *TUIBackend) GetClipboard() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.clipboard
}

// RequestClipboardRead implements core.AsyncClipboardReader: it emits the OSC 52
// read query (ESC ] 52 ; c ; ? BEL) and returns whether a reply may arrive.
// When read-back is disabled it returns false so the caller uses the internal
// clipboard. The reply (if any) is delivered to the handler registered with
// SetClipboardReadHandler.
func (t *TUIBackend) RequestClipboardRead() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.osc52Paste || t.output == nil {
		return false
	}
	fmt.Fprint(t.output, "\033]52;c;?\a")
	return true
}

// SetClipboardReadHandler implements core.AsyncClipboardReader.
func (t *TUIBackend) SetClipboardReadHandler(fn func(text string)) {
	t.mu.Lock()
	t.onClipboardRead = fn
	t.mu.Unlock()
}

// deliverClipboard records a clipboard read-back reply (from the keyboard
// handler's OSC 52 callback) into the internal clipboard and notifies the
// registered read handler.
func (t *TUIBackend) deliverClipboard(s string) {
	t.mu.Lock()
	t.clipboard = s
	h := t.onClipboardRead
	t.mu.Unlock()
	if h != nil {
		h(s)
	}
}

// SetClipboard stores the text in the internal clipboard and, when OSC 52 is
// enabled, mirrors it to the terminal's clipboard so Copy/Cut reach other apps.
// OSC 52 set: ESC ] 52 ; c ; <base64> BEL.
func (t *TUIBackend) SetClipboard(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clipboard = text
	if !t.osc52 || t.output == nil {
		return
	}
	enc := base64.StdEncoding.EncodeToString([]byte(text))
	fmt.Fprintf(t.output, "\033]52;c;%s\a", enc)
}

// Beep produces an audible alert.
func (t *TUIBackend) Beep() {
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprint(t.output, "\a")
}

// handleKey processes key events from the keyboard handler.
func (t *TUIBackend) handleKey(key string) {
	// Check for mouse events from direct-key-handler
	// Mouse events come as two keys: "Mouse@x,y" (position) followed by action
	if strings.HasPrefix(key, "Mouse@") {
		// Parse position: Mouse@x,y
		// Terminal mouse coordinates are 1-indexed, convert to 0-indexed
		var x, y int
		if _, err := fmt.Sscanf(key, "Mouse@%d,%d", &x, &y); err == nil {
			t.mu.Lock()
			t.pendingMouseX = x - 1
			t.pendingMouseY = y - 1
			t.mu.Unlock()
		}
		return // Position events don't generate UI events
	}

	// Check for mouse action events
	if strings.HasPrefix(key, "Mouse") {
		t.handleMouseAction(key)
		return
	}

	// Parse modifiers while keeping the full key string
	// Key names follow direct-key-handler convention:
	// - Control+letter: "^A", "^X" etc.
	// - Special keys: "Left", "Right", "Up", "Down", "Enter", "Tab", "Escape", etc.
	// - Function keys: "F1", "F2", ... "F12"
	// - Alt combinations: "M-" prefix
	// - Shift combinations: "S-" prefix
	mods, keyName := core.ParseKeyModifiers(key)

	// Determine text content for printable characters
	var text string
	if len(keyName) == 1 && keyName[0] >= 32 && keyName[0] < 127 {
		text = keyName
	}

	event := core.KeyPressEvent{
		Key:       key,  // Full key string including modifier prefixes
		Modifiers: mods, // Also provide parsed modifiers for trinket convenience
		Text:      text,
	}

	select {
	case t.eventQueue <- event:
	default:
		// Queue full, drop event
	}
}

// handleMouseAction processes mouse action events from direct-key-handler.
func (t *TUIBackend) handleMouseAction(key string) {
	t.mu.Lock()
	x := t.pendingMouseX
	y := t.pendingMouseY
	t.mu.Unlock()

	// Convert cell coordinates to units
	unitX := t.metrics.CellToUnitsX(x)
	unitY := t.metrics.CellToUnitsY(y)

	// For drag events, position may be embedded: MouseLeftDrag@x,y
	// Terminal coordinates are 1-indexed, convert to 0-indexed
	if strings.Contains(key, "@") {
		var dragX, dragY int
		parts := strings.SplitN(key, "@", 2)
		if len(parts) == 2 {
			if _, err := fmt.Sscanf(parts[1], "%d,%d", &dragX, &dragY); err == nil {
				unitX = t.metrics.CellToUnitsX(dragX - 1)
				unitY = t.metrics.CellToUnitsY(dragY - 1)
			}
		}
		key = parts[0] // Strip position from key for matching
	}

	var event core.Event

	switch key {
	case "MouseLeftPress":
		event = core.MousePressEvent{X: unitX, Y: unitY, Button: core.LeftButton}
	case "MouseMiddlePress":
		event = core.MousePressEvent{X: unitX, Y: unitY, Button: core.MiddleButton}
	case "MouseRightPress":
		event = core.MousePressEvent{X: unitX, Y: unitY, Button: core.RightButton}
	case "MousePress":
		event = core.MousePressEvent{X: unitX, Y: unitY, Button: core.LeftButton}

	case "MouseLeftRelease":
		event = core.MouseReleaseEvent{X: unitX, Y: unitY, Button: core.LeftButton}
	case "MouseMiddleRelease":
		event = core.MouseReleaseEvent{X: unitX, Y: unitY, Button: core.MiddleButton}
	case "MouseRightRelease":
		event = core.MouseReleaseEvent{X: unitX, Y: unitY, Button: core.RightButton}
	case "MouseRelease":
		event = core.MouseReleaseEvent{X: unitX, Y: unitY, Button: core.LeftButton}

	case "MouseLeftDrag", "MouseMiddleDrag", "MouseRightDrag", "MouseDrag":
		event = core.MouseMoveEvent{X: unitX, Y: unitY}

	case "MouseScrollUp":
		event = core.MouseWheelEvent{X: unitX, Y: unitY, DeltaY: -1}
	case "MouseScrollDown":
		event = core.MouseWheelEvent{X: unitX, Y: unitY, DeltaY: 1}

	default:
		return // Unknown mouse event
	}

	select {
	case t.eventQueue <- event:
	default:
		// Queue full, drop event
	}
}

// write outputs a string to the terminal.
func (t *TUIBackend) write(s string) {
	io.WriteString(t.output, s)
}

// detectColorDepth attempts to detect the terminal's color capability.
func detectColorDepth() int {
	// Check COLORTERM for true color
	colorterm := os.Getenv("COLORTERM")
	if colorterm == "truecolor" || colorterm == "24bit" {
		return 16777216
	}

	// Check TERM for 256 colors
	termEnv := os.Getenv("TERM")
	if strings.Contains(termEnv, "256color") {
		return 256
	}

	// Check for basic color support
	if strings.Contains(termEnv, "color") || strings.Contains(termEnv, "xterm") {
		return 16
	}

	// Default to 16 colors
	return 16
}

// runeWidth returns the display width of a rune.
func runeWidth(r rune) int {
	// CJK characters, emoji, etc. are typically double-width
	if r >= 0x1100 &&
		(r <= 0x115F || // Hangul Jamo
			r == 0x2329 || r == 0x232A || // Angle brackets
			(r >= 0x2E80 && r <= 0xA4CF && r != 0x303F) || // CJK
			(r >= 0xAC00 && r <= 0xD7A3) || // Hangul Syllables
			(r >= 0xF900 && r <= 0xFAFF) || // CJK Compatibility
			(r >= 0xFE10 && r <= 0xFE1F) || // Vertical forms
			(r >= 0xFE30 && r <= 0xFE6F) || // CJK Compatibility Forms
			(r >= 0xFF00 && r <= 0xFF60) || // Fullwidth forms
			(r >= 0xFFE0 && r <= 0xFFE6) || // Fullwidth forms
			(r >= 0x1F000 && r <= 0x1FFFF) || // Emoji and symbols
			(r >= 0x20000 && r <= 0x2FFFF)) { // CJK Extension
		return 2
	}
	return 1
}
