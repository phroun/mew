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
	core.TrinketKeys
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

	// In-flight input method composition, painted at the caret but not
	// part of text until the platform commits it (which arrives as
	// ordinary typed characters). See core/preedit.go.
	preedit core.Preedit

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
	t.SetCommands(
		// Caret movement, and its with-selection twin for each direction.
		core.CmdTrinketItemLeft, core.CmdTrinketItemRight,
		core.CmdTrinketSelLeft, core.CmdTrinketSelRight,
		core.CmdTrinketBeg, core.CmdTrinketEnd,
		core.CmdTrinketBegOrSelectAll,
		core.CmdTrinketSelBeg, core.CmdTrinketSelEnd,
		// Editing.
		core.CmdTrinketDelPrior, core.CmdTrinketDelNext, core.CmdTrinketDelLine,
		core.CmdTrinketSelectAll,
		// Enter submits. A text field offers no edit command -- it IS the
		// editor -- so Enter falls through to activate here.
		core.CmdTrinketActivate,
	)
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

// AcceptsTextInput implements core.TextSink: this is the trinket that types.
//
// Not while it is read-only or disabled — a keystroke arriving there produces
// no text, so an input method has nothing to compose FOR and should not be
// left pointed at it.
func (t *TextInput) AcceptsTextInput() bool {
	return !t.readOnly && t.IsEnabled()
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

	// Committed characters end whatever was being composed - this IS the
	// commit, arriving as ordinary typed text. Clearing here rather than
	// waiting for the empty TEXT_EDITING that usually follows means the
	// preedit never briefly paints alongside the text it turned into,
	// whichever order the platform sends the two events in.
	t.preedit = core.Preedit{}

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

	// Scroll right if cursor is after visible area. Measured against the
	// COMPOSED run, so a growing input-method composition pushes the view
	// along instead of running off the right edge: the caret being chased
	// is the one inside the composition.
	displayText, _, _, caret := t.composedText()

	// Room the caret needs to stay visible past the text before it. At the
	// end of the text only the thin caret bar shows, so reserve a sliver,
	// not a whole cell - reserving a full cell made the field scroll a
	// character early, looking full while a cell of space still remained.
	// Mid-text, keep the character the caret sits on visible.
	var cursorWidth core.Unit
	if caret < len(displayText) {
		cursorWidth = font.MeasureText(string(displayText[caret]))
	} else {
		cursorWidth = metrics.CellWidth / 4
		if cursorWidth < 1 {
			cursorWidth = 1
		}
	}

	for caret > t.scrollOffset {
		// Calculate width from scrollOffset to the caret
		start := t.scrollOffset
		end := caret
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

	// Get display text. While an input method is composing, the run
	// painted here is the committed text with the composition spliced in
	// at the caret - one run, so the composition shapes with its
	// neighbours the way it will once it commits.
	var displayText []rune
	isPlaceholder := false
	preLo, preHi, caretIdx := 0, 0, 0
	if len(t.text) == 0 && !focused && t.placeholder != "" {
		displayText = []rune(t.placeholder)
		s = s.WithAttrs(style.StyleDim)
		isPlaceholder = true
	} else {
		displayText, preLo, preHi, caretIdx = t.composedText()
	}

	// Apply scroll offset
	if t.scrollOffset > 0 && t.scrollOffset < len(displayText) {
		displayText = displayText[t.scrollOffset:]
	} else if t.scrollOffset >= len(displayText) {
		displayText = nil
	}
	if t.scrollOffset > 0 {
		preLo -= t.scrollOffset
		preHi -= t.scrollOffset
		caretIdx -= t.scrollOffset
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

	clampIdx := func(d int) int {
		if d < 0 {
			return 0
		}
		if d > n {
			return n
		}
		return d
	}
	cursorDisp := clampIdx(caretIdx)
	preLo, preHi = clampIdx(preLo), clampIdx(preHi)
	composing := t.preedit.Active() && preHi > preLo && !isPlaceholder

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
	// While composing there is no selection to draw: selStart/selEnd index
	// the COMMITTED text, which the composition has displaced, and the
	// selection is about to be replaced by whatever commits anyway.
	selLo, selHi := -1, -1
	anchorDisp := cursorDisp
	if t.HasSelection() && !isPlaceholder && !composing {
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

	// 3. Mark the input method's composition. Two signals, because one is
	// not enough: the underline is the convention every platform uses for
	// "not committed yet", and the caret's own color says whose text this
	// is - the input method is still holding it, the way it is still
	// holding the caret. Drawn in the same overstrike style as the
	// selection, re-coloring the SAME run through a pixel clip so the
	// composed glyphs never shift as the composition grows.
	if composing {
		barStyle := scheme.GetFocusedEditBoxBarCursor()
		// The bar caret is a FILLED rectangle, so its color is the
		// style's background; as text that color has to become the
		// foreground.
		preStyle := s.WithFg(barStyle.Bg).WithBg(style.ColorTransparent)
		loX, loPx := prefixWidth(preLo)
		_, hiPx := prefixWidth(preHi)

		// The active clause - the segment the input method is converting
		// right now - is underscored twice as thick. Input methods that
		// report no clause get one even underline across the whole
		// composition, which is the common case.
		clauseLo, clauseHi := preLo, preLo
		if t.preedit.ClauseLen > 0 {
			clauseLo = clampIdx(preLo + t.preedit.ClauseStart)
			clauseHi = clampIdx(clauseLo + t.preedit.ClauseLen)
		}

		if usePx {
			p.DrawTextOffsetClipped(0, 0, 0, loPx, hiPx, string(displayText), preStyle, font)
			// Underline as an explicit rule rather than the font's own:
			// it has to sit at a known offset below the line so the thick
			// clause rule can share the same baseline, and a font
			// underline gives no say in either.
			thin := p.DeviceScale()
			if thin < 1 {
				thin = 1
			}
			lineH := p.UnitsToPx(font.LineHeight())
			ruleY := lineH - thin
			if ruleY < 0 {
				ruleY = 0
			}
			p.FillRectPixels(0, 0, loPx, ruleY, hiPx-loPx, thin, barStyle)
			if clauseHi > clauseLo {
				_, cLoPx := prefixWidth(clauseLo)
				_, cHiPx := prefixWidth(clauseHi)
				if y := ruleY - thin; y >= 0 {
					p.FillRectPixels(0, 0, cLoPx, y, cHiPx-cLoPx, thin, barStyle)
				}
			}
		} else {
			// Cell surfaces have no sub-cell rule to draw, so the
			// underline is the attribute and the clause is what stands
			// out - bold against the rest of the composition.
			cellStyle := preStyle.WithAttrs(style.StyleUnderline)
			p.DrawText(loX, 0, string(displayText[preLo:preHi]), cellStyle, font)
			if clauseHi > clauseLo {
				cLoX, _ := prefixWidth(clauseLo)
				p.DrawText(cLoX, 0, string(displayText[clauseLo:clauseHi]),
					cellStyle.WithAttrs(style.StyleUnderline|style.StyleBold), font)
			}
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
			// Tell the platform where the insertion point is, without
			// asking it to DRAW a caret — this trinket paints its own
			// just below, and a platform caret on top would be a second
			// one. What the OS does with it is place an input method's
			// candidate window: the CJK candidate list, macOS's
			// press-and-hold accent picker, the emoji picker. Reported
			// every frame while focused, so the blink never withdraws it.
			//
			// While composing, report the START of the composition rather
			// than the caret inside it: the candidate list belongs under
			// the text it is offering candidates FOR, and anchoring it to
			// the caret would walk it rightward with every keystroke.
			areaX := caretX
			if composing {
				areaX, _ = prefixWidth(preLo)
			}
			p.RequestTextInputArea(areaX, 0)
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
						// The character under the block comes from the run
						// actually on screen. Indexing the COMMITTED text
						// by cursorPos agreed with this for as long as the
						// two runs held the same characters - the scroll
						// offset cancels, since caretX is measured over the
						// scrolled run from that same character. A
						// composition breaks that: it is spliced into the
						// painted run and absent from the committed one, so
						// cursorPos lands on the wrong side of it.
						var cursorChar rune = ' '
						if cursorDisp < len(displayText) {
							cursorChar = displayText[cursorDisp]
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
	return t.echo(t.text)
}

// echo applies the echo mode to a run. Shared by the committed text and
// by an in-flight composition: a password field that masked only what it
// had already accepted would show the next word in the clear for as long
// as it took to compose.
func (t *TextInput) echo(src []rune) []rune {
	switch t.echoMode {
	case EchoPassword:
		result := make([]rune, len(src))
		for i := range result {
			result[i] = '•'
		}
		return result
	case EchoNoEcho:
		return nil
	default:
		return src
	}
}

// composedText returns the run the field actually paints: the committed
// display text with any in-flight composition spliced in at the caret,
// the composition's span within that run, and where the caret sits -
// inside the composition while composing, since that is where the input
// method's own cursor is.
//
// Indices below the caret mean the same thing in both spaces (the splice
// happens AT the caret), which is what lets scrollOffset - kept in
// committed indices - slice this run without translation.
func (t *TextInput) composedText() (runes []rune, preLo, preHi, caret int) {
	display := t.getDisplayText()
	at := t.cursorPos
	if at < 0 {
		at = 0
	}
	if at > len(display) {
		at = len(display)
	}
	if !t.preedit.Active() {
		return display, at, at, at
	}

	pre := t.echo(t.preedit.Text)
	out := make([]rune, 0, len(display)+len(pre))
	out = append(out, display[:at]...)
	out = append(out, pre...)
	out = append(out, display[at:]...)

	inner := t.preedit.Caret
	if inner > len(pre) {
		inner = len(pre)
	}
	return out, at, at + len(pre), at + inner
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

	// Every movement has a with-selection twin, which is a modifier axis
	// rather than a set of unrelated actions: the two forms share a case and
	// differ only in whether the caret drags the anchor with it. This used to
	// be a fold of the "S-" prefix into the bare name plus a read of the
	// modifier bitfield; the command says it directly now.
	cmd := t.KeyCommand(event.Key)
	extend := false
	switch cmd {
	case core.CmdTrinketSelLeft, core.CmdTrinketSelRight,
		core.CmdTrinketSelUp, core.CmdTrinketSelDown,
		core.CmdTrinketSelBeg, core.CmdTrinketSelEnd:
		extend = true
	}

	switch cmd {
	case core.CmdTrinketItemLeft, core.CmdTrinketSelLeft:
		if t.cursorPos > 0 {
			t.cursorPos--
			if !extend {
				t.selStart = t.cursorPos
				t.selEnd = t.cursorPos
			} else {
				t.selEnd = t.cursorPos
			}
			t.ensureCursorVisible()
			t.Update()
		} else if !extend && t.HasSelection() {
			// Caret already at the beginning: a plain Left can't move, so it
			// just collapses any selection (leaving the caret at the start).
			t.selStart = t.cursorPos
			t.selEnd = t.cursorPos
			t.Update()
		}
		return true

	case core.CmdTrinketItemRight, core.CmdTrinketSelRight:
		if t.cursorPos < len(t.text) {
			t.cursorPos++
			if !extend {
				t.selStart = t.cursorPos
				t.selEnd = t.cursorPos
			} else {
				t.selEnd = t.cursorPos
			}
			t.ensureCursorVisible()
			t.Update()
		} else if !extend && t.HasSelection() {
			// Caret already at the end: a plain Right can't move, so it just
			// collapses any selection (leaving the caret at the end).
			t.selStart = t.cursorPos
			t.selEnd = t.cursorPos
			t.Update()
		}
		return true

	case core.CmdTrinketBegOrSelectAll:
		// The Emacs home cycle: already at the beginning with nothing
		// selected selects all and puts the caret at the end. That is exactly
		// the case where going to the beginning would do nothing at all, so
		// the cycle costs the key nothing. Otherwise it is a plain move, which
		// is why this falls through to the same body below.
		if t.cursorPos == 0 && !t.HasSelection() {
			t.selStart = 0
			t.selEnd = len(t.text)
			t.cursorPos = t.selEnd
			t.ensureCursorVisible()
			t.Update()
			return true
		}
		t.cursorPos = 0
		t.selStart = 0
		t.selEnd = 0
		t.ensureCursorVisible()
		t.Update()
		return true

	case core.CmdTrinketBeg, core.CmdTrinketSelBeg:
		t.cursorPos = 0
		if !extend {
			t.selStart = 0
			t.selEnd = 0
		} else {
			t.selEnd = 0
		}
		t.ensureCursorVisible()
		t.Update()
		return true

	case core.CmdTrinketEnd, core.CmdTrinketSelEnd:
		t.cursorPos = len(t.text)
		if !extend {
			t.selStart = t.cursorPos
			t.selEnd = t.cursorPos
		} else {
			t.selEnd = t.cursorPos
		}
		t.ensureCursorVisible()
		t.Update()
		return true

	case core.CmdTrinketDelPrior:
		t.backspace()
		return true

	case core.CmdTrinketDelNext:
		t.delete()
		return true

	case core.CmdTrinketActivate:
		if t.onReturnPressed != nil {
			t.onReturnPressed()
		}
		return true

	case core.CmdTrinketDelLine:
		// Clear line
		t.text = nil
		t.cursorPos = 0
		t.selStart = 0
		t.selEnd = 0
		t.scrollOffset = 0
		t.textChanged()
		return true

	case core.CmdTrinketSelectAll:
		// Select all (Mega+A)
		t.SelectAll()
		return true

	}

	// Handle printable characters, in the order mew's own floor uses.
	//
	// A one-character KeyName IS the character: nothing needs looking up, and
	// this is the answer wherever no host has watched the keyboard at all — the
	// terminal backend, where a keystroke arrives already named and there is no
	// second event to observe.
	if utf8.RuneCountInString(event.Key) == 1 {
		t.insert(event.Key)
		return true
	}

	// Otherwise ask the host what this chord was watched typing. That is the
	// case where the name cannot carry the answer — macOS Option, where M-a
	// types "å" — and reading it off the event instead made this trinket depend
	// on text riding along with a press it does not identify.
	if text, ok := core.KeyChordTextFor(t, event.Key); ok &&
		utf8.RuneCountInString(text) == 1 {
		t.insert(text)
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
	// A composition belongs to the caret it was being typed at. Focus
	// moving elsewhere abandons it: the input method will start a fresh
	// one wherever typing resumes, and leaving these characters painted
	// in a field nobody is typing into would show provisional text as
	// though it were committed.
	t.preedit = core.Preedit{}
	// The selection survives - it shows in the resting selection
	// colors until the box is edited again.
	t.Update()
}

// HandleTextEditing implements core.TextEditingHandler: it takes one
// update to the input method's in-flight composition. The characters do
// NOT enter text - they are painted at the caret, underlined and in the
// caret's color, until the platform commits them as ordinary typed
// input (see insert) or ends the composition with an empty update.
//
// A read-only or disabled field declines, so the composition is dropped
// rather than shown somewhere it could never land.
func (t *TextInput) HandleTextEditing(event core.TextEditingEvent) bool {
	if t.readOnly || !t.IsEnabled() {
		return false
	}

	next := core.PreeditFrom(event)
	if !next.Active() && !t.preedit.Active() {
		// Input methods send an empty update to end a composition, and
		// some send one when nothing was composing at all. Repainting
		// for that would wake the whole surface for no visible change.
		return true
	}
	t.preedit = next

	// Composing is typing: the caret should be solid and in view, the
	// same as it is for a keystroke.
	t.resetCaretBlink()
	t.ensureCursorVisible()
	t.Update()
	return true
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

// HandlePaste inserts pasted text at the caret, the same way a clipboard paste
// does (newlines flattened for the single-line flow, selection replaced, one
// undo step). Satisfies core.PasteHandler so a bracketed paste the host
// received reaches a focused input directly, with no clipboard round-trip.
func (t *TextInput) HandlePaste(event core.PasteEvent) bool {
	if t.readOnly || !t.IsEnabled() {
		return false
	}
	t.pasteText(event.Text)
	return true
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
			// The 1-pixel outer frame every popup gets, in the padded
			// margin just outside the bounds (graphical only).
			if p.Graphical() {
				lineStyle := style.DefaultStyle().WithBg(t.GetScheme().GetMenuSeparator().Fg)
				paintPopupOuterStroke(p, menuBounds, p.DeviceScale(), lineStyle, 0, 0, false)
			}
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
