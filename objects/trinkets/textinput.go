// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"fmt"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// TextInput is a single-line text entry trinket.
type TextInput struct {
	core.TrinketBase
	core.AccessibleTrinket

	text        []rune
	placeholder string
	maxLength   int
	echoMode    EchoMode
	readOnly    bool

	// Cursor and selection
	cursorPos    int
	selStart     int
	selEnd       int
	scrollOffset int

	// Callbacks
	onTextChanged   func(text string)
	onReturnPressed func()

	// Graphical caret blink: the bar toggles while focused and
	// restarts visible on every keystroke. Without a running timer
	// (cell surfaces, no desktop) the caret is steady.
	caretTimer *DesktopTimer
	caretOn    bool

	// Drag selection in progress (armed by a left press, extended by
	// motion while the button is held).
	selecting bool

	// Drag-select autoscroll: while the pointer is held past the left or
	// right edge, a repeating timer walks the caret (and the scroll) that
	// way, extending the selection - the horizontal analogue of a list
	// view's edge autoscroll. scrollDir is -1/+1/0; scrollOverX is how far
	// (in units) the pointer is past the edge, which sets the per-tick speed.
	scrollTimer *DesktopTimer
	scrollDir   int
	scrollOverX core.Unit

	// Context menu hover row (-1 = none).
	menuHover int

	// Multi-click selection: a quick second click on the same spot selects
	// the word under the pointer, a third selects all. clickStreak counts
	// consecutive fast clicks; lastClickTime gates the streak.
	lastClickTime time.Time
	clickStreak   int

	// Embedded-host bridge (SetEmbedHost): an unparented input hosted
	// inside another trinket (the TreeView's row editor) borrows the
	// host's ancestry for everything a parent chain normally provides -
	// the desktop/clipboard lookup, the popup controller walk, and the
	// screen mapping that places the context menu. embedOrigin reports
	// this input's current origin in the host's local space.
	embedHost   core.Trinket
	embedOrigin func() core.UnitPoint
}

// EchoMode controls how text is displayed.
type EchoMode int

const (
	EchoNormal         EchoMode = iota // Show text normally
	EchoPassword                       // Show bullets/asterisks
	EchoPasswordOnEdit                 // Show char briefly, then bullet
	EchoNoEcho                         // Show nothing
)

// NewTextInput creates a new text input.
func NewTextInput() *TextInput {
	t := &TextInput{
		echoMode:  EchoNormal,
		maxLength: -1, // No limit
	}
	t.TrinketBase = *core.NewTrinketBase()
	t.Init(t) // Enable polymorphic focus handling
	t.SetFocusPolicy(core.StrongFocus)
	t.SetAccessibleRole(core.RoleTextInput)
	return t
}

// CursorShape implements core.CursorProvider: an editable text field
// shows the text I-beam while hovered.
func (t *TextInput) CursorShape() core.CursorShape {
	return core.CursorText
}

// Text returns the current text.
func (t *TextInput) Text() string {
	return string(t.text)
}

// SetText sets the text content.
func (t *TextInput) SetText(text string) {
	t.text = []rune(text)
	t.cursorPos = len(t.text)
	t.selStart = 0
	t.selEnd = 0
	t.scrollOffset = 0
	t.Update()

	if t.onTextChanged != nil {
		t.onTextChanged(text)
	}
}

// Placeholder returns the placeholder text.
func (t *TextInput) Placeholder() string {
	return t.placeholder
}

// SetPlaceholder sets the placeholder text.
func (t *TextInput) SetPlaceholder(text string) {
	t.placeholder = text
	t.Update()
}

// MaxLength returns the maximum text length.
func (t *TextInput) MaxLength() int {
	return t.maxLength
}

// SetMaxLength sets the maximum text length (-1 for no limit).
func (t *TextInput) SetMaxLength(length int) {
	t.maxLength = length
}

// EchoMode returns the echo mode.
func (t *TextInput) EchoMode() EchoMode {
	return t.echoMode
}

// SetEchoMode sets the echo mode.
func (t *TextInput) SetEchoMode(mode EchoMode) {
	t.echoMode = mode
	if mode == EchoPassword {
		t.SetAccessibleRole(core.RolePasswordInput)
	} else {
		t.SetAccessibleRole(core.RoleTextInput)
	}
	t.Update()
}

// IsReadOnly returns whether the input is read-only.
func (t *TextInput) IsReadOnly() bool {
	return t.readOnly
}

// SetReadOnly sets the read-only state.
func (t *TextInput) SetReadOnly(readOnly bool) {
	t.readOnly = readOnly
	t.Update()
}

// CursorPosition returns the cursor position.
func (t *TextInput) CursorPosition() int {
	return t.cursorPos
}

// SetCursorPosition sets the cursor position.
func (t *TextInput) SetCursorPosition(pos int) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(t.text) {
		pos = len(t.text)
	}
	t.cursorPos = pos
	t.selStart = pos
	t.selEnd = pos
	t.ensureCursorVisible()
	// Moving the caret restarts the blink visible, so its new position
	// shows immediately.
	t.resetCaretBlink()
	t.Update()
}

// HasSelection returns whether there is a text selection.
func (t *TextInput) HasSelection() bool {
	return t.selStart != t.selEnd
}

// SelectedText returns the selected text.
func (t *TextInput) SelectedText() string {
	if t.selStart == t.selEnd {
		return ""
	}
	start, end := t.selStart, t.selEnd
	if start > end {
		start, end = end, start
	}
	return string(t.text[start:end])
}

// SelectAll selects all text.
func (t *TextInput) SelectAll() {
	t.selStart = 0
	t.selEnd = len(t.text)
	t.cursorPos = t.selEnd
	t.Update()
}

// selectWordAt selects the run of same-class characters around pos: a word
// (letters, digits, underscore), a run of whitespace, or a single
// punctuation character. The caret lands at the end of the selection.
func (t *TextInput) selectWordAt(pos int) {
	if len(t.text) == 0 {
		t.cursorPos, t.selStart, t.selEnd = 0, 0, 0
		return
	}
	if pos >= len(t.text) {
		pos = len(t.text) - 1
	}
	if pos < 0 {
		pos = 0
	}

	isWord := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
	}

	start, end := pos, pos
	switch {
	case isWord(t.text[pos]):
		for start > 0 && isWord(t.text[start-1]) {
			start--
		}
		for end < len(t.text) && isWord(t.text[end]) {
			end++
		}
	case unicode.IsSpace(t.text[pos]):
		for start > 0 && unicode.IsSpace(t.text[start-1]) {
			start--
		}
		for end < len(t.text) && unicode.IsSpace(t.text[end]) {
			end++
		}
	default:
		// A lone punctuation/symbol character selects just itself.
		end = pos + 1
	}

	t.selStart = start
	t.selEnd = end
	t.cursorPos = end
	t.Update()
}

// ClearSelection clears the selection.
func (t *TextInput) ClearSelection() {
	t.selStart = t.cursorPos
	t.selEnd = t.cursorPos
	t.Update()
}

// SetOnTextChanged sets the text changed callback.
func (t *TextInput) SetOnTextChanged(handler func(text string)) {
	t.onTextChanged = handler
}

// SetOnReturnPressed sets the return pressed callback.
func (t *TextInput) SetOnReturnPressed(handler func()) {
	t.onReturnPressed = handler
}

// insert inserts text at the cursor position.
func (t *TextInput) insert(text string) {
	if t.readOnly {
		return
	}

	// Delete selection first
	t.deleteSelection()

	// Check max length
	runes := []rune(text)
	if t.maxLength >= 0 && len(t.text)+len(runes) > t.maxLength {
		remaining := t.maxLength - len(t.text)
		if remaining <= 0 {
			return
		}
		runes = runes[:remaining]
	}

	// Insert
	newText := make([]rune, len(t.text)+len(runes))
	copy(newText[:t.cursorPos], t.text[:t.cursorPos])
	copy(newText[t.cursorPos:], runes)
	copy(newText[t.cursorPos+len(runes):], t.text[t.cursorPos:])
	t.text = newText
	t.cursorPos += len(runes)
	t.selStart = t.cursorPos
	t.selEnd = t.cursorPos

	t.textChanged()
}

// deleteSelection deletes the selected text.
func (t *TextInput) deleteSelection() {
	if t.selStart == t.selEnd {
		return
	}

	start, end := t.selStart, t.selEnd
	if start > end {
		start, end = end, start
	}

	newText := make([]rune, len(t.text)-(end-start))
	copy(newText[:start], t.text[:start])
	copy(newText[start:], t.text[end:])
	t.text = newText
	t.cursorPos = start
	t.selStart = start
	t.selEnd = start
}

// backspace deletes the character before the cursor.
func (t *TextInput) backspace() {
	if t.readOnly {
		return
	}

	if t.HasSelection() {
		t.deleteSelection()
		t.textChanged()
		return
	}

	if t.cursorPos > 0 {
		newText := make([]rune, len(t.text)-1)
		copy(newText[:t.cursorPos-1], t.text[:t.cursorPos-1])
		copy(newText[t.cursorPos-1:], t.text[t.cursorPos:])
		t.text = newText
		t.cursorPos--
		t.selStart = t.cursorPos
		t.selEnd = t.cursorPos
		t.textChanged()
	}
}

// delete deletes the character after the cursor.
func (t *TextInput) delete() {
	if t.readOnly {
		return
	}

	if t.HasSelection() {
		t.deleteSelection()
		t.textChanged()
		return
	}

	if t.cursorPos < len(t.text) {
		newText := make([]rune, len(t.text)-1)
		copy(newText[:t.cursorPos], t.text[:t.cursorPos])
		copy(newText[t.cursorPos:], t.text[t.cursorPos+1:])
		t.text = newText
		t.textChanged()
	}
}

// textChanged triggers the text changed callback.
func (t *TextInput) textChanged() {
	t.ensureCursorVisible()
	t.Update()
	if t.onTextChanged != nil {
		t.onTextChanged(string(t.text))
	}
}

// ensureCursorVisible scrolls to make the cursor visible.
func (t *TextInput) ensureCursorVisible() {
	bounds := t.Bounds()
	font := t.EffectiveFont()
	metrics := t.EffectiveCellMetrics()

	if bounds.Width <= 0 {
		return
	}

	// Scroll left if cursor is before visible area
	if t.cursorPos < t.scrollOffset {
		t.scrollOffset = t.cursorPos
	}

	// Scroll right if cursor is after visible area
	// Calculate width of text from scrollOffset to cursor using font metrics
	displayText := t.getDisplayText()

	// Room the caret needs to stay visible past the text before it. At the
	// end of the text only the thin caret bar shows, so reserve a sliver,
	// not a whole cell - reserving a full cell made the field scroll a
	// character early, looking full while a cell of space still remained.
	// Mid-text, keep the character the caret sits on visible.
	var cursorWidth core.Unit
	if t.cursorPos < len(displayText) {
		cursorWidth = font.MeasureText(string(displayText[t.cursorPos]))
	} else {
		cursorWidth = metrics.CellWidth / 4
		if cursorWidth < 1 {
			cursorWidth = 1
		}
	}

	for t.cursorPos > t.scrollOffset {
		// Calculate width from scrollOffset to cursorPos
		start := t.scrollOffset
		end := t.cursorPos
		if end > len(displayText) {
			end = len(displayText)
		}
		if start >= len(displayText) {
			break
		}
		visibleText := string(displayText[start:end])
		textWidth := font.MeasureText(visibleText)

		// Need room for text before cursor PLUS the cursor character itself
		if textWidth+cursorWidth <= bounds.Width {
			break
		}
		// Scroll right by one character
		t.scrollOffset++
	}
}

// SizeHint returns the preferred size.
func (t *TextInput) SizeHint() core.UnitSize {
	metrics := t.EffectiveCellMetrics()
	// TextInput has a fixed size in units (160 wide x 16 tall) - does not scale with font
	return core.UnitSize{
		Width:  metrics.TextWidth(20),
		Height: metrics.TextHeight(1),
	}
}

// IsInlineTrinket returns true to indicate this is a text-style trinket
// that should receive horizontal margins when in a vertical box layout.
func (t *TextInput) IsInlineTrinket() bool {
	return true
}

// Paint renders the text input.
func (t *TextInput) Paint(p *core.Painter) {
	bounds := t.Bounds()
	scheme := t.GetScheme()
	focused := t.HasFocus()
	font := t.EffectiveFont()

	// Get inherited background color to determine pane type
	inheritedBg := t.EffectiveBackgroundColor()
	paneType := style.GetPaneType(inheritedBg)

	// Determine style
	var s style.CellStyle
	var fillChar rune = ' '
	if !t.IsEnabled() {
		s = style.DefaultStyle().WithFg(scheme.GetDisabledTextFG()).WithBg(scheme.GetEditBox(paneType).Bg)
	} else if focused {
		s = scheme.GetFocusedEditBoxText()
		// Use speckled fill character for focused state
		fillChar = '░'
	} else {
		// Unfocused editbox style depends on pane type
		s = scheme.GetEditBox(paneType)
	}

	// Draw background - use fill style with speckled pattern for focused state
	fillStyle := s
	if focused && t.IsEnabled() {
		// Focused fill uses the fill style from scheme
		fillStyle = scheme.GetFocusedEditBoxFill()
		// Text uses the text style from scheme
		s = scheme.GetFocusedEditBoxText()
	}
	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, fillChar, fillStyle)

	// Get display text
	var displayText []rune
	isPlaceholder := false
	if len(t.text) == 0 && !focused && t.placeholder != "" {
		displayText = []rune(t.placeholder)
		s = s.WithAttrs(style.StyleDim)
		isPlaceholder = true
	} else {
		displayText = t.getDisplayText()
	}

	// Apply scroll offset
	if t.scrollOffset > 0 && t.scrollOffset < len(displayText) {
		displayText = displayText[t.scrollOffset:]
	} else if t.scrollOffset >= len(displayText) {
		displayText = nil
	}

	// Truncate to visible width using font metrics
	visibleText := t.truncateToWidth(displayText, bounds.Width, font)
	displayText = []rune(visibleText)

	// Everything is measured from - and drawn as - the WHOLE text in one
	// shaped run, never split at the caret or the selection edges. Splitting
	// re-shapes the material at the split each time it moves (a substring
	// shapes differently than the same characters mid-run), which jittered
	// the text as the caret or selection swept through it. The caret and the
	// selection are measured against this stable run and painted on top.
	n := len(displayText)
	showCaret := focused && !t.readOnly && core.FocusChainActive(t.Self())

	cursorDisp := t.cursorPos - t.scrollOffset
	if cursorDisp < 0 {
		cursorDisp = 0
	}
	if cursorDisp > n {
		cursorDisp = n
	}

	selStyle := scheme.GetEditBoxSelection(focused && t.IsEnabled(), paneType)

	// On a pixel surface the glyphs rasterize at the unsnapped
	// pixels-per-unit, so measure the caret/selection and place the text by
	// device-pixel advance from the anchor at unit 0 (DrawTextOffset), never
	// re-snapping an intermediate unit position through the cell rate. On a
	// cell surface (no TextPixelDrawer) fall back to the whole-unit DrawText.
	_, usePx := p.DrawTextOffset(0, 0, 0, 0, "", s, font)

	// prefixWidth is the width, in units and device pixels, of the visible
	// text before display index d - measured against the whole stable run.
	prefixWidth := func(d int) (core.Unit, int) {
		if d < 0 {
			d = 0
		}
		if d > n {
			d = n
		}
		w := font.MeasureText(string(displayText[:d]))
		return w, p.UnitsToPx(w)
	}

	// Selection span (display indices) and the fixed anchor - the selection
	// end opposite the caret (selStart is the anchor; the caret is selEnd).
	selLo, selHi := -1, -1
	anchorDisp := cursorDisp
	if t.HasSelection() && !isPlaceholder {
		anchorDisp = t.selStart - t.scrollOffset
		if anchorDisp < 0 {
			anchorDisp = 0
		}
		if anchorDisp > n {
			anchorDisp = n
		}
		selLo, selHi = anchorDisp, cursorDisp
		if selLo > selHi {
			selLo, selHi = selHi, selLo
		}
	}

	// 1. Draw the whole text once - stable regardless of caret/selection.
	if usePx {
		p.DrawTextOffset(0, 0, 0, 0, string(displayText), s, font)
	} else {
		p.DrawText(0, 0, string(displayText), s, font)
	}

	caretX, caretXPx := prefixWidth(cursorDisp)

	// 2. Overstrike the selection: a highlight over the whole span, then the
	// selected text re-colored. On a pixel surface the re-color draws the SAME
	// whole run (identical glyph rasters as the base text) and reveals only
	// the selected columns with a pixel-precise clip, so the selected glyphs
	// never move - only the clip edge does - and neither the fixed anchor end
	// nor the interior jitters as the caret end grows the span. (Re-drawing a
	// re-shaped substring, right-aligned to the anchor, still jittered: the
	// substring re-shapes and its rounded left edge shifts every glyph.) On a
	// cell surface the substring path is exact (cell-aligned) and used as-is.
	if selLo >= 0 && selHi > selLo {
		loX, loPx := prefixWidth(selLo)
		hiX, hiPx := prefixWidth(selHi)
		// When the selection's far end is scrolled off the right of the box,
		// its highlight must reach the box's own right edge, not stop at the
		// last visible glyph - otherwise the trailing sliver draws in the
		// normal color even though the selection continues off-screen.
		selHiAbs := t.selStart
		if t.selEnd > selHiAbs {
			selHiAbs = t.selEnd
		}
		if selHiAbs > t.scrollOffset+n {
			if usePx {
				if edge := p.UnitSpanPxX(0, bounds.Width); edge > hiPx {
					hiPx = edge
				}
			} else if bounds.Width > hiX {
				hiX = bounds.Width
			}
		}
		if usePx {
			p.FillRectPixels(0, 0, loPx, 0, hiPx-loPx, p.UnitsToPx(font.LineHeight()), selStyle)
			selFg := selStyle.WithBg(style.ColorTransparent) // glyphs over the highlight
			p.DrawTextOffsetClipped(0, 0, 0, loPx, hiPx, string(displayText), selFg, font)
		} else {
			p.FillRect(core.UnitRect{X: loX, Width: hiX - loX, Height: bounds.Height}, ' ', selStyle)
			p.DrawText(loX, 0, string(displayText[selLo:selHi]), selStyle, font)
		}
	}

	// Draw cursor - only in the active window chain: a trinket keeps local
	// focus while its window is in the background, but showing the caret
	// there would put two carets on screen.
	if showCaret {
		if caretX >= 0 && caretX < bounds.Width {
			// The graphical bar caret uses a brighter white than the cell
			// block cursor, for contrast; the block fallback keeps the
			// regular (silver) white.
			cursorStyle := scheme.GetFocusedEditBoxCursor()
			barStyle := scheme.GetFocusedEditBoxBarCursor()
			// The graphical bar caret blinks (keystrokes restart the
			// phase); the cell-surface block stays steady.
			if p.Graphical() {
				t.ensureCaretTimer()
			}
			if !p.Graphical() || t.caretVisible() {
				drawn := false
				if usePx {
					// Site the bar at the same accumulated pixel advance the
					// glyphs painted at, so it sits exactly on the boundary
					// before the cursor's character.
					drawn = p.FillRectPixels(0, 0, caretXPx, 0,
						p.DeviceScale(), p.UnitsToPx(font.LineHeight()), barStyle)
				}
				if !drawn {
					// Cell surfaces fall back to the reverse-video block.
					if !p.DrawCaret(caretX, 0, font.LineHeight(), barStyle) {
						var cursorChar rune = ' '
						if t.cursorPos < len(t.getDisplayText()) {
							cursorChar = t.getDisplayText()[t.cursorPos]
						}
						p.DrawText(caretX, 0, string(cursorChar), cursorStyle, font)
					}
				}
			}
		}
	}
}

// caretVisible reports the blink state: visible whenever no blink
// timer is running (cell surfaces, detached trinkets).
func (t *TextInput) caretVisible() bool {
	return t.caretTimer == nil || t.caretOn
}

// ensureCaretTimer starts the ~2Hz blink cycle when the trinket can
// reach a desktop timer source.
func (t *TextInput) ensureCaretTimer() {
	if t.caretTimer != nil {
		return
	}
	d := findDesktopFor(t)
	if d == nil {
		return
	}
	t.caretOn = true
	t.caretTimer = d.StartRepeatingTimer(500*time.Millisecond, func() {
		t.caretOn = !t.caretOn
		t.invalidateCaretRegion()
	})
}

// invalidateCaretRegion requests a repaint for the blink. On the main desktop
// surface it damages only this input's rectangle (a partial repaint); anywhere
// else (a torn-off window, or no desktop) it falls back to a full repaint.
func (t *TextInput) invalidateCaretRegion() {
	if d := findDesktopFor(t); d != nil {
		if r, ok := t.mainSurfaceRect(d); ok {
			d.InvalidateRect(r)
			return
		}
	}
	t.Update()
}

// mainSurfaceRect returns this input's rectangle in main-surface (desktop)
// coordinates, padded to cover the antialiased caret edges, or ok=false when
// the input isn't on the main surface (so the caller repaints in full).
func (t *TextInput) mainSurfaceRect(d *Desktop) (core.UnitRect, bool) {
	pc := t.findPopupController()
	if pc == nil || !d.IsMainSurfaceController(pc) {
		return core.UnitRect{}, false
	}
	b := t.Bounds()
	if b.Width <= 0 || b.Height <= 0 {
		return core.UnitRect{}, false
	}
	tl := pc.MapToScreen(t.Self(), core.UnitPoint{X: 0, Y: 0})
	br := pc.MapToScreen(t.Self(), core.UnitPoint{X: b.Width, Y: b.Height})
	const pad = 2
	x0, y0 := tl.X-pad, tl.Y-pad
	x1, y1 := br.X+pad, br.Y+pad
	if x1 <= x0 || y1 <= y0 {
		return core.UnitRect{}, false
	}
	return core.UnitRect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}, true
}

func (t *TextInput) stopCaretTimer() {
	if t.caretTimer != nil {
		t.caretTimer.Stop()
		t.caretTimer = nil
	}
	t.caretOn = true
}

// resetCaretBlink restarts the blink phase with the caret visible -
// typing never happens behind an invisible caret.
func (t *TextInput) resetCaretBlink() {
	if t.caretTimer == nil {
		return
	}
	t.stopCaretTimer()
	t.ensureCaretTimer()
}

// getDisplayText returns the text with echo mode applied.
func (t *TextInput) getDisplayText() []rune {
	switch t.echoMode {
	case EchoPassword:
		result := make([]rune, len(t.text))
		for i := range result {
			result[i] = '•'
		}
		return result
	case EchoNoEcho:
		return nil
	default:
		return t.text
	}
}

// truncateToWidth truncates text to fit within the given width using font metrics.
func (t *TextInput) truncateToWidth(text []rune, maxWidth core.Unit, font *core.Font) string {
	if len(text) == 0 {
		return ""
	}

	// Find how many characters fit within maxWidth
	result := make([]rune, 0, len(text))
	var totalWidth core.Unit
	for _, r := range text {
		charWidth := font.MeasureText(string(r))
		if totalWidth+charWidth > maxWidth {
			break
		}
		result = append(result, r)
		totalWidth += charWidth
	}
	return string(result)
}

// findCharAtX finds the character index at the given X position using font metrics.
func (t *TextInput) findCharAtX(x core.Unit, font *core.Font) int {
	displayText := t.getDisplayText()
	if t.scrollOffset > 0 && t.scrollOffset < len(displayText) {
		displayText = displayText[t.scrollOffset:]
	} else if t.scrollOffset >= len(displayText) {
		return t.scrollOffset
	}

	var accumulatedWidth core.Unit
	for i, r := range displayText {
		charWidth := font.MeasureText(string(r))
		// Check if x is within this character's bounds
		if x < accumulatedWidth+charWidth/2 {
			return t.scrollOffset + i
		}
		accumulatedWidth += charWidth
	}
	// x is past all characters
	return t.scrollOffset + len(displayText)
}

// HandleKeyPress handles keyboard input.
func (t *TextInput) HandleKeyPress(event core.KeyPressEvent) bool {
	// Any keystroke makes the caret immediately visible.
	t.resetCaretBlink()

	// Both backends deliver navigation keys with their "S-" prefix
	// intact (e.g. "S-Left") alongside the parsed modifier. Fold the
	// prefix into the bare name so shift-extends the selection; the
	// caret-anchor logic in each case reads Modifiers. Control/Meta
	// spellings ("^A", "C-S-a") stay literal - they are matched whole.
	key := event.Key
	switch key {
	case "S-Left", "S-Right", "S-Home", "S-End", "S-Up", "S-Down":
		event.Modifiers |= core.ShiftModifier
		key = key[2:]
	}

	// Handle special keys
	switch key {
	case "Left":
		if t.cursorPos > 0 {
			t.cursorPos--
			if event.Modifiers&core.ShiftModifier == 0 {
				t.selStart = t.cursorPos
				t.selEnd = t.cursorPos
			} else {
				t.selEnd = t.cursorPos
			}
			t.ensureCursorVisible()
			t.Update()
		} else if event.Modifiers&core.ShiftModifier == 0 && t.HasSelection() {
			// Caret already at the beginning: a plain Left can't move, so it
			// just collapses any selection (leaving the caret at the start).
			t.selStart = t.cursorPos
			t.selEnd = t.cursorPos
			t.Update()
		}
		return true

	case "Right":
		if t.cursorPos < len(t.text) {
			t.cursorPos++
			if event.Modifiers&core.ShiftModifier == 0 {
				t.selStart = t.cursorPos
				t.selEnd = t.cursorPos
			} else {
				t.selEnd = t.cursorPos
			}
			t.ensureCursorVisible()
			t.Update()
		} else if event.Modifiers&core.ShiftModifier == 0 && t.HasSelection() {
			// Caret already at the end: a plain Right can't move, so it just
			// collapses any selection (leaving the caret at the end).
			t.selStart = t.cursorPos
			t.selEnd = t.cursorPos
			t.Update()
		}
		return true

	case "Home":
		t.cursorPos = 0
		if event.Modifiers&core.ShiftModifier == 0 {
			t.selStart = 0
			t.selEnd = 0
		} else {
			t.selEnd = 0
		}
		t.ensureCursorVisible()
		t.Update()
		return true

	case "End":
		t.cursorPos = len(t.text)
		if event.Modifiers&core.ShiftModifier == 0 {
			t.selStart = t.cursorPos
			t.selEnd = t.cursorPos
		} else {
			t.selEnd = t.cursorPos
		}
		t.ensureCursorVisible()
		t.Update()
		return true

	case "Backspace":
		t.backspace()
		return true

	case "Delete":
		t.delete()
		return true

	case "Enter":
		if t.onReturnPressed != nil {
			t.onReturnPressed()
		}
		return true

	case "^U":
		// Clear line
		t.text = nil
		t.cursorPos = 0
		t.selStart = 0
		t.selEnd = 0
		t.scrollOffset = 0
		t.textChanged()
		return true

	case "M-a":
		// Select all (Meta+A)
		t.SelectAll()
		return true

	case "^A":
		if event.Modifiers&core.ShiftModifier != 0 {
			// Shift+Ctrl+A: extend the selection to the beginning.
			t.cursorPos = 0
			t.selEnd = 0
			t.ensureCursorVisible()
			t.Update()
			return true
		}
		// Home cycle (Emacs C-a, with a convenience twist): a two-state toggle.
		// Already at the beginning with nothing selected -> select all, caret to
		// the end. Anywhere else (including with all selected) -> caret to the
		// beginning, clearing any selection.
		if t.cursorPos == 0 && !t.HasSelection() {
			t.selStart = 0
			t.selEnd = len(t.text)
			t.cursorPos = t.selEnd
		} else {
			t.cursorPos = 0
			t.selStart = 0
			t.selEnd = 0
		}
		t.ensureCursorVisible()
		t.Update()
		return true

	case "^E":
		// Go to end (Emacs binding)
		t.cursorPos = len(t.text)
		if event.Modifiers&core.ShiftModifier == 0 {
			t.selStart = t.cursorPos
			t.selEnd = t.cursorPos
		} else {
			t.selEnd = t.cursorPos
		}
		t.ensureCursorVisible()
		t.Update()
		return true

	case "C-S-a", "C-S-A":
		// Shift+Ctrl+A: extend the selection to the beginning (the
		// anchor is wherever the caret was when the selection began).
		t.cursorPos = 0
		t.selEnd = 0
		t.ensureCursorVisible()
		t.Update()
		return true

	case "C-S-e", "C-S-E":
		// Shift+Ctrl+E: extend the selection to the end.
		t.cursorPos = len(t.text)
		t.selEnd = t.cursorPos
		t.ensureCursorVisible()
		t.Update()
		return true
	}

	// Handle printable characters
	if event.Text != "" && utf8.RuneCountInString(event.Text) == 1 {
		t.insert(event.Text)
		return true
	}

	return false
}

// HandleMousePress handles mouse clicks.
func (t *TextInput) HandleMousePress(event core.MousePressEvent) bool {
	if event.Button == core.LeftButton {
		font := t.EffectiveFont()
		pos := t.findCharAtX(event.X, font)
		if pos > len(t.text) {
			pos = len(t.text)
		}
		if event.Modifiers&core.ShiftModifier != 0 {
			// Shift+click extends: the previous caret position is
			// (already) the anchor; only the moving end follows.
			t.cursorPos = pos
			t.selEnd = pos
			t.selecting = true
			t.clickStreak = 0 // shift-click isn't part of a multi-click run
		} else {
			// Count consecutive fast clicks: 2 selects the word under the
			// pointer, 3 (or more) selects all. A slow click restarts the run.
			now := time.Now()
			if !t.lastClickTime.IsZero() && now.Sub(t.lastClickTime) < 400*time.Millisecond {
				t.clickStreak++
			} else {
				t.clickStreak = 1
			}
			t.lastClickTime = now

			switch {
			case t.clickStreak >= 3:
				t.SelectAll()
				t.selecting = false
			case t.clickStreak == 2:
				t.selectWordAt(pos)
				t.selecting = false
			default:
				t.cursorPos = pos
				t.selStart = pos
				t.selEnd = pos
				t.selecting = true
			}
		}
		t.SetFocus()
		// A click that repositions the caret shows it immediately.
		t.resetCaretBlink()
		t.Update()
		return true
	}
	if event.Button == core.RightButton {
		t.SetFocus()
		t.showContextMenu(event)
		return true
	}
	return false
}

// HandleMouseMove extends the selection while the button is held. Past
// either edge it hands off to the autoscroll timer (which keeps walking the
// selection while the pointer is held still out there); inside the box it
// tracks the pointer directly.
func (t *TextInput) HandleMouseMove(event core.MouseMoveEvent) bool {
	if !t.selecting || event.Buttons&core.LeftButton == 0 {
		return false
	}
	bounds := t.Bounds()
	if event.X < 0 {
		t.scrollOverX = -event.X
		t.startAutoScroll(-1)
		return true
	}
	if event.X >= bounds.Width {
		t.scrollOverX = event.X - bounds.Width
		t.startAutoScroll(1)
		return true
	}
	t.stopAutoScroll()

	font := t.EffectiveFont()
	pos := t.findCharAtX(event.X, font)
	if pos > len(t.text) {
		pos = len(t.text)
	}
	if pos != t.cursorPos {
		t.cursorPos = pos
		t.selEnd = pos
		t.ensureCursorVisible()
		// Keep the caret visible as it tracks the drag.
		t.resetCaretBlink()
		t.Update()
	}
	return true
}

// startAutoScroll begins (or redirects) the edge autoscroll in direction
// dir (-1 left, +1 right). It steps once immediately so a drag past the edge
// reacts at once, then a repeating timer continues while the pointer stays
// out (no further move events arrive while it is held still).
func (t *TextInput) startAutoScroll(dir int) {
	if t.scrollDir == dir && t.scrollTimer != nil {
		return // already walking this way
	}
	t.stopAutoScroll()
	t.scrollDir = dir
	t.autoScrollStep()
	if d := findDesktopFor(t); d != nil {
		t.scrollTimer = d.StartRepeatingTimer(50*time.Millisecond, func() {
			t.autoScrollStep()
		})
	}
}

// stopAutoScroll halts the edge autoscroll.
func (t *TextInput) stopAutoScroll() {
	if t.scrollTimer != nil {
		t.scrollTimer.Stop()
		t.scrollTimer = nil
	}
	t.scrollDir = 0
}

// autoScrollStep walks the caret in the autoscroll direction, extending the
// selection and scrolling to keep it visible. The step size grows with how
// far the pointer is past the edge - a nudge crawls, a big overshoot races -
// and it stops itself at either end of the text.
func (t *TextInput) autoScrollStep() {
	if t.scrollDir == 0 {
		return
	}
	moved := false
	for i := 0; i < t.autoScrollSpeed(); i++ {
		if t.scrollDir < 0 {
			if t.cursorPos <= 0 {
				break
			}
			t.cursorPos--
		} else {
			if t.cursorPos >= len(t.text) {
				break
			}
			t.cursorPos++
		}
		moved = true
	}
	if !moved {
		t.stopAutoScroll()
		return
	}
	t.selEnd = t.cursorPos
	t.ensureCursorVisible()
	t.resetCaretBlink()
	t.Update()
}

// autoScrollSpeed is the number of characters to advance per tick: one at
// the edge, plus one for every cell the pointer is dragged past it, capped
// so a far overshoot stays controllable.
func (t *TextInput) autoScrollSpeed() int {
	speed := 1
	if cw := t.EffectiveCellMetrics().CellWidth; cw > 0 {
		speed += int(t.scrollOverX / cw)
	}
	if speed > 12 {
		speed = 12
	}
	return speed
}

// HandleMouseRelease ends a drag selection.
func (t *TextInput) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	if t.selecting {
		t.selecting = false
		t.stopAutoScroll()
		return true
	}
	return false
}

// HandleFocusIn is called when focus is gained.
func (t *TextInput) HandleFocusIn() {
	t.Update()
}

// HandleFocusOut is called when focus is lost.
func (t *TextInput) HandleFocusOut() {
	t.stopCaretTimer()
	t.stopAutoScroll()
	t.selecting = false
	// The selection survives - it shows in the resting selection
	// colors until the box is edited again.
	t.Update()
}

// AccessibleInfo returns accessibility information.
func (t *TextInput) AccessibleInfo() core.AccessibleInfo {
	info := t.AccessibleTrinket.AccessibleInfo()
	if t.echoMode == EchoPassword {
		info.Role = core.RolePasswordInput
	} else {
		info.Role = core.RoleTextInput
	}
	info.Value = string(t.text)
	if t.readOnly {
		info.State |= core.StateReadOnly
	}
	if !t.IsEnabled() {
		info.State |= core.StateDisabled
	}
	return info
}

// ---------------------------------------------------------------
// Clipboard actions + context menu
// ---------------------------------------------------------------

// SetEmbedHost lends this (unparented) input a host trinket's ancestry:
// desktop/clipboard lookup, popup-controller walk, and context-menu
// screen mapping all resolve as if the input sat at origin() within
// the host. The TreeView's in-place row editor is the model user.
func (t *TextInput) SetEmbedHost(host core.Trinket, origin func() core.UnitPoint) {
	t.embedHost = host
	t.embedOrigin = origin
}

// envAnchor is the trinket whose ancestry resolves this input's
// environment: the embed host when set, else the input itself.
func (t *TextInput) envAnchor() core.Trinket {
	if t.embedHost != nil {
		return t.embedHost
	}
	return t.Self()
}

// clipboardAccess finds the clipboard for this trinket: the desktop
// when the trinket lives in one, otherwise the popup controller (a
// torn-off window's host bridges the platform clipboard).
func (t *TextInput) clipboardAccess() (get func() string, set func(string)) {
	if d := findDesktopFor(t.envAnchor()); d != nil {
		return d.Clipboard, d.SetClipboard
	}
	type clipper interface {
		Clipboard() string
		SetClipboard(string)
	}
	if c, ok := t.findPopupController().(clipper); ok {
		return c.Clipboard, c.SetClipboard
	}
	return nil, nil
}

// Copy puts the selected text on the clipboard.
func (t *TextInput) Copy() {
	sel := t.SelectedText()
	if sel == "" {
		return
	}
	if _, set := t.clipboardAccess(); set != nil {
		set(sel)
	}
}

// Cut copies the selected text to the clipboard and removes it.
func (t *TextInput) Cut() {
	if t.readOnly || !t.HasSelection() {
		return
	}
	t.Copy()
	t.deleteSelection()
	t.textChanged()
}

// Paste inserts the clipboard at the caret, replacing any selection.
// A single-line input flattens newlines to spaces. Reading the clipboard can be
// asynchronous (a terminal's OSC 52 query may prompt the user), so the desktop
// resolves it and calls back - on the UI thread - when it is ready; SDL and
// internal reads resolve immediately.
func (t *TextInput) Paste() {
	if t.readOnly {
		return
	}
	if d := findDesktopFor(t.envAnchor()); d != nil {
		d.ReadClipboardAsync(func(s string) { t.pasteText(s) })
		return
	}
	get, _ := t.clipboardAccess()
	if get != nil {
		t.pasteText(get())
	}
}

// pasteText inserts resolved clipboard text at the caret (newlines flattened to
// spaces for the single-line flow).
func (t *TextInput) pasteText(s string) {
	if t.readOnly || s == "" {
		return
	}
	flat := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\n' || r == '\r' {
			r = ' '
		}
		flat = append(flat, r)
	}
	t.insert(string(flat))
}

// contextMenuID names this input's popup uniquely.
func (t *TextInput) contextMenuID() string {
	return fmt.Sprintf("textinput-menu-%d", t.ObjectID())
}

// contextMenuItems builds the right-click menu, each item equivalent
// to the matching Edit-menu action.
func (t *TextInput) contextMenuItems() []termMenuItem {
	return []termMenuItem{
		{label: "Cut", action: t.Cut},
		{label: "Copy", action: t.Copy},
		{label: "Paste", action: t.Paste},
		{separator: true},
		{label: "Select All", action: t.SelectAll},
	}
}

// findPopupController resolves the popup controller by checking this input's
// own field first, then walking up the parent chain. A directly-stamped
// controller isn't always present - e.g. an MDI child window's content is
// never stamped, but an ancestor (the MDI pane) is - so the walk is what makes
// the right-click menu and clipboard bridge work inside an MDI child.
func (t *TextInput) findPopupController() core.PopupController {
	if pc := t.PopupController(); pc != nil {
		return pc
	}
	// An embedded input has no parent of its own: the walk starts AT
	// its host (which may carry a controller or inherit one above).
	var current any = t.Parent()
	if t.embedHost != nil {
		current = t.embedHost
	}
	for current != nil {
		trinket, ok := current.(core.Trinket)
		if !ok {
			break
		}
		if getter, ok := trinket.(interface {
			PopupController() core.PopupController
		}); ok {
			if pc := getter.PopupController(); pc != nil {
				return pc
			}
		}
		current = trinket.Parent()
	}
	return nil
}

// showContextMenu opens the right-click menu as a popup overlay,
// using the same presentation as PurfecTerm's terminal menu.
func (t *TextInput) showContextMenu(event core.MousePressEvent) {
	pc := t.findPopupController()
	if pc == nil {
		return
	}
	items := t.contextMenuItems()
	height := core.Unit(0)
	for _, it := range items {
		if it.separator {
			height += 4
		} else {
			height += gfxMenuItemHeight
		}
	}
	height += 4 // padding
	// Screen placement: an embedded input maps through its HOST (its
	// own parentless bounds mean nothing to the controller).
	local := core.UnitPoint{X: event.X, Y: event.Y}
	target := t.Self()
	if t.embedHost != nil && t.embedOrigin != nil {
		o := t.embedOrigin()
		local.X += o.X
		local.Y += o.Y
		target = t.embedHost
	}
	at := pc.MapToScreen(target, local)
	screen := pc.ScreenBounds()
	if at.X+gfxMenuWidth > screen.X+screen.Width {
		at.X = screen.X + screen.Width - gfxMenuWidth
	}
	if at.Y+height > screen.Y+screen.Height {
		at.Y = screen.Y + screen.Height - height
	}
	menuBounds := core.UnitRect{X: at.X, Y: at.Y, Width: gfxMenuWidth, Height: height}
	t.menuHover = -1

	itemAt := func(y core.Unit) int {
		pos := core.Unit(2)
		for i, it := range items {
			h := gfxMenuItemHeight
			if it.separator {
				h = 4
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
			pos := menuBounds.Y + 2
			for i, it := range items {
				if it.separator {
					p.FillRect(core.UnitRect{X: menuBounds.X + 4, Y: pos + 2, Width: menuBounds.Width - 8, Height: 1}, ' ',
						style.DefaultStyle().WithBg(style.RGB(200, 200, 200)))
					pos += 4
					continue
				}
				st := bg
				if i == t.menuHover {
					st = hover
					p.FillRect(core.UnitRect{X: menuBounds.X, Y: pos, Width: menuBounds.Width, Height: gfxMenuItemHeight}, ' ', st)
				}
				// Explicit bg: transparent resolves to the terminal's dark
				// default on the text backend (dark boxes behind the labels);
				// the explicit bg equals the fill/hover color, so the
				// graphical look is unchanged.
				p.DrawText(menuBounds.X+8, pos, it.label, st, nil)
				pos += gfxMenuItemHeight
			}
		},
		HandleMouseMove: func(event core.MouseMoveEvent) bool {
			if !menuBounds.Contains(core.UnitPoint{X: event.X, Y: event.Y}) {
				return false
			}
			idx := itemAt(event.Y - menuBounds.Y)
			if idx != t.menuHover {
				t.menuHover = idx
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
