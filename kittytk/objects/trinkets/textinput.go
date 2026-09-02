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
	maskChar    rune // what EchoPassword paints; zero means the default bullet
	readOnly    bool

	// Cursor and selection
	cursorPos    int
	selStart     int
	selEnd       int
	scrollOffset int

	// Callbacks
	onTextChanged func(text string)
	onComplete    func()

	// Graphical caret blink: the bar toggles while focused and
	// restarts visible on every keystroke. Without a running timer
	// (cell surfaces, no desktop) the caret is steady.
	caretTimer *DesktopTimer
	caretOn    bool

	// In-flight input method composition, painted at the caret but not
	// part of text until the platform commits it (which arrives as
	// ordinary typed characters). See core/preedit.go.
	preedit core.Preedit

	// preeditAt is where the standing composition's region STARTS, and
	// preeditStanding whether there is one. A plain index is enough here: this
	// field owns every edit to its own runes, and the only one made while a
	// composition stands lands at the caret, which is at or past the region's
	// end. A document taking edits from elsewhere needs a cursor that tracks
	// them instead.
	//
	// The region outlives the PAINTING. An input method closes its composition
	// before it delivers the finished text, and a keystroke that dismisses a
	// palette lands before the commit catches up — so a region measured back
	// from the caret at commit time points at whatever was typed since, and one
	// thrown away entirely leaves the commit appending. Both showed as a
	// doubled letter.
	preeditAt       int
	preeditStanding bool

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

// defaultMaskChar is what a masked field paints when nothing says otherwise.
const defaultMaskChar = '\u2022' // BULLET

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
		// ...and offering activate is exactly why the space bar needs saying
		// out loud: it shares a key with activate, and without this a space
		// would submit instead of typing.
		core.CmdTrinketTypeSpace,
	)
	t.Init(t) // Enable polymorphic focus handling
	t.SetFocusPolicy(core.StrongFocus)
	// One line of text tall, and it cannot be more: given a row three deep it
	// sits in it rather than stretching to it. Across is another matter -- a
	// field is meant to take the width it is given.
	t.SetSizePolicy(core.NewSizePolicy(core.SizePreferred, core.SizeFixed))
	return t
}

// CursorShape implements core.CursorProvider: a text field shows the I-beam
// while hovered, unless it is DISABLED, where it shows the ordinary pointer.
//
// The I-beam is a promise that the pointer will do something here, and on a
// disabled field it will not. A read-only field keeps it: its text can still
// be selected with the mouse, which is exactly what the I-beam offers.
func (t *TextInput) CursorShape() core.CursorShape {
	if !t.IsEnabled() {
		return core.CursorDefault
	}
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

// SetMaskChar sets the character a masked field paints in place of each rune
// it holds. Zero restores the default bullet.
//
// It is one character, not a string: the mask stands in for a rune, and a
// multi-rune stand-in would make a masked field a different width from the
// text behind it -- which leaks the length in a caret position and breaks the
// column arithmetic besides.
func (t *TextInput) SetMaskChar(r rune) {
	t.maskChar = r
	t.Update()
}

// MaskChar returns the character a masked field paints, the default bullet
// included, so a caller never has to know about the zero value.
func (t *TextInput) MaskChar() rune {
	if t.maskChar == 0 {
		return defaultMaskChar
	}
	return t.maskChar
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

// SetOnComplete sets the callback for the field being COMPLETED: the person
// typing has said they are done with it.
//
// One callback, because there is one gesture. Return reaches it, and so does
// whatever else the keymap has made mean trinket_activate here -- the field
// does not care which key it was, only that the content is finished. It is
// not "submit": nothing is being sent anywhere, and a field completed is
// still a field, still editable, still holding its text.
func (t *TextInput) SetOnComplete(handler func()) {
	t.onComplete = handler
}

// insert inserts text at the cursor position.
func (t *TextInput) insert(text string) {
	// The innermost guard, where the content is actually changed. Enabled as
	// well as writable: every typing path leads here, and a disabled field
	// must not be editable down any of them.
	if !t.AcceptsTextInput() {
		return
	}

	// Committed characters end whatever was being PAINTED - this IS the
	// commit, arriving as ordinary typed text. Cleared here rather than
	// waiting for the empty TEXT_EDITING that usually follows means the
	// preedit never briefly paints alongside the text it turned into,
	// whichever order the platform sends the two events in.
	//
	// Its REGION stands, though. A palette dismissed by typing lands the
	// keystroke before the input method's commit catches up, and the accent
	// still belongs where the composition was — throwing the region away here
	// left the commit appending, "o.ò" for a letter that should have become
	// "ò.". The platform gives the region up by cancelling, which it does
	// whenever a keystroke ends a takeover; this is not the place to guess.
	t.preedit.Text = nil
	t.preedit.Caret = 0

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
		cursorWidth = t.MeasureText(string(displayText[caret]))
	} else {
		cursorWidth = metrics.UnitsPerCellWidth / 4
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
		textWidth := t.MeasureText(visibleText)

		// Need room for text before cursor PLUS the cursor character itself
		if textWidth+cursorWidth <= bounds.Width {
			break
		}
		// Scroll right by one character
		t.scrollOffset++
	}
}

// SizeHint returns the preferred size.
// textInputWidthUnits is the width a field asks for when nothing sets one,
// in units.
// SizeHint returns the preferred size: the fallback width for when nothing
// sets one (see defaultSizeCells), and one row.
func (t *TextInput) SizeHint() core.UnitSize {
	metrics := t.EffectiveCellMetrics()
	return core.UnitSize{
		Width:  metrics.UnitsPerCellWidth * defaultSizeCells,
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

	// A field is one line of text tall, and a line is one grid row. The
	// device-pixel fills below (highlight, block caret, bar caret) span that
	// row, measured end to end so they land on the same device grid the
	// glyphs beside them paint on.
	rowHPx := p.UnitSpanPxY(0, t.EffectiveCellMetrics().UnitsPerCellHeight)

	// Get inherited background color to determine pane type
	inheritedBg := t.EffectiveBackgroundColor()
	paneType := style.GetPaneType(inheritedBg)

	// Determine style
	var s style.CellStyle
	var fillChar rune = ' '
	if !t.IsEnabled() {
		// A disabled field sits on its CONTAINER's background. DisabledTextFG
		// is the disabled counterpart to normal window text -- inherited through
		// the window, beside FocusTextFG and HoverTextFG, and what
		// DisabledLabelFG and DisabledButtonFG fall back to -- so the ground it
		// is chosen against is the container's. The edit-box background is what
		// says "you can work in here", which a disabled field does not.
		s = style.DefaultStyle().WithFg(scheme.GetDisabledTextFG()).WithBg(inheritedBg)
		// The speckle carries the rest: a flat rectangle says "empty", the
		// texture says "a field, unavailable". Same ink as the text, so the
		// whole thing reads as one hatched-out surface.
		fillChar = '░'
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
	// A read-only field still shows a caret. It has no insertion point, but
	// it does have a position -- the text is selectable with the mouse and
	// walkable with the arrows, and a reader with no caret cannot see where
	// either of those left them.
	//
	// It is drawn as a BLOCK over the character rather than a bar between
	// two, which is the shape mew uses for navigating rather than editing,
	// and it says the difference without a word: a bar sits where text would
	// go in, a block sits on the character you are at.
	showCaret := focused && core.FocusChainActive(t.Self())
	blockCaret := t.readOnly

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
	//
	// The pixel answer is measured in PIXELS, not measured in units and
	// scaled. MeasureText rounds to whole units, which is the denomination
	// the field is laid out in and the wrong one for a position inside the
	// run: the glyphs rasterize at the unsnapped pixels-per-unit, so rounding
	// at the unit and again at the pixel drifts by up to half a unit against
	// the very glyphs the caret sits between. It shows wherever a rune's
	// advance is a fraction of a unit - a space beside CJK text is about two
	// and a half - where the caret after a second space landed short of the
	// space it was meant to follow.
	//
	// Where the painter cannot measure in pixels, the fallback maps the LOCAL
	// width onto the device grid. UnitsToPx cannot: it converts from the
	// default denomination, so inside a re-denominated interior it answers for
	// a different unit than the one MeasureText counted -- the same run came
	// out 15px at 4x8 and 58px at 16x32 where the truth was 29px throughout.
	runPx := func(run string, w core.Unit) int {
		if px, ok := p.MeasureTextPx(run, font); ok {
			return px
		}
		return p.UnitSpanPxX(0, w)
	}
	prefixWidth := func(d int) (core.Unit, int) {
		if d < 0 {
			d = 0
		}
		if d > n {
			d = n
		}
		run := string(displayText[:d])
		w := t.MeasureText(run)
		return w, runPx(run, w)
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
			p.FillRectPixels(0, 0, loPx, 0, hiPx-loPx, rowHPx, selStyle)
			selFg := selStyle.WithBg(style.ColorTransparent) // glyphs over the highlight
			p.DrawTextOffsetClipped(0, 0, 0, loPx, hiPx, string(displayText), selFg, font)
		} else {
			p.FillRect(core.UnitRect{X: loX, Width: hiX - loX, Height: bounds.Height}, ' ', selStyle)
			p.DrawText(loX, 0, string(displayText[selLo:selHi]), selStyle, font)
		}
	}

	// 3. Mark the input method's composition. Two signals, because one is
	// not enough: the underline is the convention every platform uses for
	// "not committed yet", and its own color says whose text this is - the
	// input method is still holding it, the way it is still holding the
	// caret. Drawn in the same overstrike style as the selection,
	// re-coloring the SAME run through a pixel clip so the composed glyphs
	// never shift as the composition grows.
	if composing {
		inactiveStyle := scheme.GetFocusedEditBoxIMEInactive()
		clauseStyle := scheme.GetFocusedEditBoxIMEActiveClause()
		// Only the foreground is read: the composition is overstruck on
		// whatever the field is already showing. A rule, being a filled
		// rectangle, wants that same color as its background instead.
		preStyle := s.WithFg(inactiveStyle.Fg).WithBg(style.ColorTransparent)
		rule := func(c style.Color) style.CellStyle {
			return style.DefaultStyle().WithBg(c)
		}
		loX, loPx := prefixWidth(preLo)
		_, hiPx := prefixWidth(preHi)

		// The ACTIVE span: the clause the input method is converting right
		// now, in the active color and underscored twice as thick, against
		// the rest of the composition dimmed to the inactive one.
		//
		// A Japanese composition is several clauses and a candidate list
		// converts one at a time, leaving the others as they were typed, so
		// without the distinction those read as characters the composition
		// failed to replace. An input method that reports NO clause is
		// working on all of it, and the whole composition is the active
		// span - same color, same thick rule. Only a composition that has a
		// clause has anything to dim, which is why the dimmed pass below is
		// skipped outright when there is none: nothing of it would show,
		// and drawing the same glyphs twice composites their edges twice.
		clauseLo, clauseHi := preLo, preHi
		if t.preedit.ClauseLen > 0 {
			clauseLo = clampIdx(preLo + t.preedit.ClauseStart)
			clauseHi = clampIdx(clauseLo + t.preedit.ClauseLen)
		}
		hasInactive := clauseLo > preLo || clauseHi < preHi

		if usePx {
			// Underline as an explicit rule rather than the font's own:
			// it has to sit at a known offset below the line so the thick
			// active rule can share the same baseline, and a font underline
			// gives no say in either.
			thin := p.DeviceScale()
			if thin < 1 {
				thin = 1
			}
			ruleY := rowHPx - thin
			if ruleY < 0 {
				ruleY = 0
			}
			if hasInactive {
				p.DrawTextOffsetClipped(0, 0, 0, loPx, hiPx, string(displayText),
					preStyle, font)
				p.FillRectPixels(0, 0, loPx, ruleY, hiPx-loPx, thin,
					rule(inactiveStyle.Fg))
			}
			if clauseHi > clauseLo {
				_, cLoPx := prefixWidth(clauseLo)
				_, cHiPx := prefixWidth(clauseHi)
				// Clipped from the WHOLE composition rather than drawn as
				// its own run, so the active span changes color without
				// being re-shaped - a substring shapes differently than the
				// same characters mid-run, and it would jitter as the
				// candidate list is walked.
				p.DrawTextOffsetClipped(0, 0, 0, cLoPx, cHiPx, string(displayText),
					s.WithFg(clauseStyle.Fg).WithBg(style.ColorTransparent), font)
				p.FillRectPixels(0, 0, cLoPx, ruleY, cHiPx-cLoPx, thin, rule(clauseStyle.Fg))
				if y := ruleY - thin; y >= 0 {
					p.FillRectPixels(0, 0, cLoPx, y, cHiPx-cLoPx, thin, rule(clauseStyle.Fg))
				}
			}
		} else {
			// Cell surfaces have no sub-cell rule to draw, so the
			// underline is the attribute and the active span carries its
			// color and bold weight instead of a thicker rule.
			cellStyle := preStyle.WithAttrs(style.StyleUnderline)
			if hasInactive {
				p.DrawText(loX, 0, string(displayText[preLo:preHi]), cellStyle, font)
			}
			if clauseHi > clauseLo {
				cLoX, _ := prefixWidth(clauseLo)
				p.DrawText(cLoX, 0, string(displayText[clauseLo:clauseHi]),
					cellStyle.WithFg(clauseStyle.Fg).
						WithAttrs(style.StyleUnderline|style.StyleBold), font)
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
			// phase); a block stays steady, on a cell surface and on a
			// read-only field alike. A blink says "type here" and paces
			// itself to a keystroke that is not coming.
			if p.Graphical() && !blockCaret {
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
			// Only where text can actually arrive: this is what an input
			// method anchors its candidate window to, and a field that
			// accepts nothing has nothing to compose for.
			if t.AcceptsTextInput() {
				p.RequestTextInputArea(areaX, 0)
			}
			if blockCaret {
				// The block covers the character the caret sits BEFORE --
				// the one it is "at" -- painted in that text's own colours
				// reversed: the field's background becomes the ink and the
				// ink becomes the ground. At the end of the text there is no
				// character to cover, so it takes one space's worth of the
				// interior instead and comes out the same size either way.
				//
				// Same two steps the selection uses: fill the span, then
				// redraw the glyphs clipped into it, so the block sits on the
				// same pixel advance the text was laid out at.
				endX, endPx := prefixWidth(cursorDisp + 1)
				if cursorDisp >= n {
					blank := t.MeasureText(" ")
					endX = caretX + blank
					endPx = caretXPx + runPx(" ", blank)
				}
				// The caret is always at one EDGE of a selection (the span
				// runs anchor to cursor), so the block covers a SELECTED
				// character exactly when the caret is at the left edge, which
				// is what selecting backwards leaves.
				overSel := selLo >= 0 && cursorDisp >= selLo && cursorDisp < selHi
				block := blockCaretStyle(s, selStyle, fillStyle.Bg, overSel)
				if usePx {
					p.FillRectPixels(0, 0, caretXPx, 0, endPx-caretXPx,
						rowHPx, block)
					p.DrawTextOffsetClipped(0, 0, 0, caretXPx, endPx,
						string(displayText), block.WithBg(style.ColorTransparent), font)
				} else {
					p.FillRect(core.UnitRect{X: caretX, Width: endX - caretX,
						Height: bounds.Height}, ' ', block)
					if cursorDisp < n {
						p.DrawText(caretX, 0, string(displayText[cursorDisp]), block, font)
					}
				}
			} else if !p.Graphical() || t.caretVisible() {
				drawn := false
				if usePx {
					// Site the bar at the same accumulated pixel advance the
					// glyphs painted at, so it sits exactly on the boundary
					// before the cursor's character.
					drawn = p.FillRectPixels(0, 0, caretXPx, 0,
						p.DeviceScale(), rowHPx, barStyle)
				}
				if !drawn {
					// Cell surfaces fall back to the reverse-video block.
					if !p.DrawCaret(caretX, 0, t.EffectiveCellMetrics().UnitsPerCellHeight, barStyle) {
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

// blockCaretStyle is the pair the read-only block inverts.
//
// It inverts whatever it is SITTING ON. Over selected text that is the
// selection's own colours, not the field's: inverting the ordinary pair inside
// a highlight either vanishes into it or clashes with it, and neither reads as
// "you are here". The glyph takes the ground it is being lifted off -- the
// selection's background there, the field's fill elsewhere -- so the character
// stays legible in the hole the block makes.
func blockCaretStyle(text, sel style.CellStyle, ground style.Color, overSelection bool) style.CellStyle {
	if overSelection {
		return sel.WithBg(sel.Fg).WithFg(sel.Bg)
	}
	return text.WithBg(text.Fg).WithFg(ground)
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
		mask := t.MaskChar()
		result := make([]rune, len(src))
		for i := range result {
			result[i] = mask
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

	// What the composition was opened OVER is hidden while it stands. macOS's
	// palette commits the held letter and only then opens over it, so without
	// this the field shows both at once — the letter and the accent that was
	// chosen to take its place, side by side — for as long as the palette is
	// up. Cancelling ends the composition and the letter is simply there
	// again; nothing was deleted to hide it.
	//
	// Placed at the region rather than back from the caret: the caret may have
	// moved on since the composition opened.
	from := t.preeditAt
	if from < 0 {
		from = 0
	}
	if from > len(display) {
		from = len(display)
	}
	to := from + t.preedit.Covers
	if to > len(display) {
		to = len(display)
	}

	pre := t.echo(t.preedit.Text)
	out := make([]rune, 0, len(display)+len(pre))
	out = append(out, display[:from]...)
	out = append(out, pre...)
	out = append(out, display[to:]...)

	inner := t.preedit.Caret
	if inner > len(pre) {
		inner = len(pre)
	}
	caret = from + inner
	if at > to {
		// The caret has moved past the composition — something was typed
		// beside it. It keeps its distance from the region's end, over the
		// composition's length rather than the covered text's.
		caret = from + len(pre) + (at - to)
	}
	return out, from, from + len(pre), caret
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
		charWidth := t.MeasureText(string(r))
		if totalWidth+charWidth > maxWidth {
			break
		}
		result = append(result, r)
		totalWidth += charWidth
	}
	return string(result)
}

// findCharAtX finds the character index at the given X position using font
// metrics.
//
// x arrives in this field's own denomination, so the prefixes it is compared
// against have to be measured in that same denomination. Font.MeasureText
// answers at the DEFAULT one, so inside a re-denominated window a click
// resolved against prefixes of the wrong size and the caret landed several
// characters from the pointer.
func (t *TextInput) findCharAtX(x core.Unit, font *core.Font) int {
	displayText := t.getDisplayText()
	if t.scrollOffset > 0 && t.scrollOffset < len(displayText) {
		displayText = displayText[t.scrollOffset:]
	} else if t.scrollOffset >= len(displayText) {
		return t.scrollOffset
	}

	// Measured as PREFIXES of the run, which is how the caret is placed
	// (prefixWidth), so a click puts the caret where the click was.
	//
	// Summing each rune's width on its own rounds every one of them to a whole
	// unit and the error compounds along the line - a space is about two and a
	// half units beside CJK text, so every one of them was over-counted by
	// half - and a rune measured alone is not the width it has in the run
	// anyway.
	var before core.Unit
	for i := range displayText {
		after := t.MeasureText(string(displayText[:i+1]))
		// The nearer edge wins: past the middle of a character is the position
		// after it.
		if x < (before+after)/2 {
			return t.scrollOffset + i
		}
		before = after
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
		// Already at the beginning with nothing selected selects all and puts
		// the caret at the end. That is exactly the case where going to the
		// beginning would do nothing at all, so the second meaning costs the
		// key nothing. Otherwise it is a plain move, which is why this falls
		// through to the same body below.
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

	case core.CmdTrinketTypeSpace:
		// The space bar, typed. It arrives as a command rather than as text
		// because the key layer names it "Space" -- five runes, so the typing
		// path below (which inserts a one-rune key name as itself) never sees
		// a character to insert. Spell it here and hand it to the same insert
		// as every other typed character, so selection replacement, the
		// preedit reset and the change notification are the ordinary ones.
		t.insert(" ")
		return true

	case core.CmdTrinketActivate:
		// The field is COMPLETE: the person typing has said they are done.
		//
		// Reaching here at all is what the table's ordering decides. A field
		// offers trinket_type_space and trinket_activate both, and a context
		// takes the FIRST of a key's meanings the trinket offers -- so on
		// Space, where the default writes type_space ahead of activate, the
		// space bar types and never arrives here. That precedence belongs to
		// the keymap and is stated there; nothing is re-decided at this end.
		if t.onComplete != nil {
			t.onComplete()
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
	// What the host watched this keyboard type comes first, and for every
	// chord: it saw both halves of the keystroke and this trinket sees only the
	// name. An observation of nothing is an answer too — a dead key arms an
	// accent and produces no character — so a chord that was watched is settled
	// here either way.
	if text, observed := core.KeyChordTextFor(t, event.Key); observed {
		if utf8.RuneCountInString(text) == 1 {
			t.insert(text)
		}
		return true
	}

	// Nothing watched it. A one-character KeyName IS the character, which is
	// the answer wherever there is no host to ask — the terminal backend, where
	// a keystroke arrives already named and there is no second event to
	// observe.
	if utf8.RuneCountInString(event.Key) == 1 {
		t.insert(event.Key)
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
	if cw := t.EffectiveCellMetrics().UnitsPerCellWidth; cw > 0 {
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
	if !next.Active() && !t.preedit.Active() && !t.preeditStanding {
		// Input methods send an empty update to end a composition, and
		// some send one when nothing was composing at all. Repainting
		// for that would wake the whole surface for no visible change.
		return true
	}
	switch {
	case next.Active() && !t.preeditStanding:
		// Opening: the region is fixed here, from where the caret is now.
		from := t.cursorPos - next.Covers
		if from < 0 {
			from, next.Covers = 0, t.cursorPos
		}
		t.preeditAt, t.preeditStanding = from, true
	case !next.Active() && next.Covers == 0:
		// A cancel says it covers NOTHING, which is what tells it apart from a
		// composition merely ending on its way to a commit.
		t.preeditStanding = false
	case !next.Active():
		// Ended, still standing over its region: keep Covers, stop painting.
		next.Covers = t.preedit.Covers
	}
	t.preedit = next

	// Composing is typing: the caret should be solid and in view, the
	// same as it is for a keystroke.
	t.resetCaretBlink()
	t.ensureCursorVisible()
	t.Update()
	return true
}

// HandleTextCommit implements core.TextCommitHandler: it takes a finished
// composition into the text.
//
// The composition's own extent is what makes this more than insert. macOS's
// press-and-hold palette commits the held letter the moment the key goes down,
// so choosing an accent has to remove a character that is already in the field.
// The composition has been standing over that character and hiding it, so the
// region is already known here — nothing about it has to arrive on the event.
//
// The removal is a plain edit rather than a synthesized Backspace on purpose.
// A Backspace would run whatever the user has bound to that key, which need
// not be an erase at all.
//
// A read-only or disabled field declines, so the commit is dropped rather than
// landing somewhere it could never be typed.
func (t *TextInput) HandleTextCommit(event core.TextCommitEvent) bool {
	if t.readOnly || !t.IsEnabled() {
		return false
	}

	// What the composition covered is what this replaces, at the REGION it
	// stood on rather than back from the caret: the caret may have moved on
	// since, and the accent still belongs where the letter was.
	from, covers, standing := t.preeditAt, t.preedit.Covers, t.preeditStanding
	t.preedit = core.Preedit{}
	t.preeditStanding = false

	trailing := 0
	if standing && covers > 0 && t.selStart == t.selEnd {
		// Only what is actually there. A selection is left to insert, which
		// deletes it — taking these runes as well would erase text beside the
		// composition that it was never standing over.
		if from < 0 {
			from = 0
		}
		to := from + covers
		if to > len(t.text) {
			to = len(t.text)
		}
		if to > from {
			// How much was typed BESIDE the composition, so the caret can be put
			// back after it once the region's length changes.
			if trailing = t.cursorPos - to; trailing < 0 {
				trailing = 0
			}
			t.text = append(t.text[:from], t.text[to:]...)
			t.cursorPos = from
			t.selStart, t.selEnd = t.cursorPos, t.cursorPos
		}
	}

	t.insert(event.Text)
	if trailing > 0 {
		// Back to where the person typing left it, on the far side of what they
		// typed while the palette was up.
		t.cursorPos += trailing
		if t.cursorPos > len(t.text) {
			t.cursorPos = len(t.text)
		}
		t.selStart, t.selEnd = t.cursorPos, t.cursorPos
	}
	t.resetCaretBlink()
	t.ensureCursorVisible()
	t.Update()
	return true
}

// HandleTextErase implements core.TextEraseHandler: it takes text back out on
// an input method's behalf.
//
// A plain edit, not a synthesized Backspace, for the same reason the platform
// sent this instead of the key it arrived on: a Backspace would run whatever
// the user has bound to that key.
//
// A selection is deleted whole and the count ignored, the same rule a commit
// follows: the selection is a region the user can see, and the count is about
// text beside it.
func (t *TextInput) HandleTextErase(event core.TextEraseEvent) bool {
	if t.readOnly || !t.IsEnabled() {
		return false
	}
	if t.selStart != t.selEnd {
		t.deleteSelection()
		t.resetCaretBlink()
		t.ensureCursorVisible()
		t.Update()
		return true
	}
	n := event.Count
	if n < 1 {
		n = 1
	}
	if n > t.cursorPos {
		n = t.cursorPos
	}
	if n == 0 {
		return true
	}
	t.text = append(t.text[:t.cursorPos-n], t.text[t.cursorPos:]...)
	t.cursorPos -= n
	t.selStart, t.selEnd = t.cursorPos, t.cursorPos
	t.textChanged()
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
	// Enabled as well as writable: this is public API and the context menu is
	// not the only way in. Copy is deliberately not guarded -- it only reads.
	if !t.AcceptsTextInput() || !t.HasSelection() {
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
	if !t.AcceptsTextInput() {
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
	if !t.AcceptsTextInput() || s == "" {
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
	// Cut and Paste CHANGE the content, so they are offered only where the
	// content can change; elsewhere they are greyed and inert.
	//
	// Copy and Select All only read, and reading a disabled field is the same
	// as selecting its text with the mouse, which one can.
	edits := t.AcceptsTextInput()
	return []termMenuItem{
		{label: "Cut", action: t.Cut, disabled: !edits},
		{label: "Copy", action: t.Copy},
		{label: "Paste", action: t.Paste, disabled: !edits},
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
	// The same menu PurfecTerm opens, measured by the same function.
	lay := termMenuLayoutFrom(core.FindGraphicalFrames(t), t.EffectiveFont(),
		termMenuScreenMetrics(pc), items)
	height := core.Unit(0)
	for _, it := range items {
		if it.separator {
			height += lay.sepH
		} else {
			height += lay.rowH
		}
	}
	height += 2 * lay.padTop
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
	if at.X+lay.width > screen.X+screen.Width {
		at.X = screen.X + screen.Width - lay.width
	}
	if at.Y+height > screen.Y+screen.Height {
		at.Y = screen.Y + screen.Height - height
	}
	menuBounds := core.UnitRect{X: at.X, Y: at.Y, Width: lay.width, Height: height}
	t.menuHover = -1

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
					inset := lay.indent / 2
					p.FillRect(core.UnitRect{
						X: menuBounds.X + inset, Y: pos + lay.sepH/2,
						Width: menuBounds.Width - inset*2, Height: p.HairlineHeight(),
					}, ' ', style.DefaultStyle().WithBg(style.RGB(200, 200, 200)))
					pos += lay.sepH
					continue
				}
				st := bg
				if it.disabled {
					st = bg.WithFg(style.RGB(150, 150, 150))
				} else if i == t.menuHover {
					st = hover
					p.FillRect(core.UnitRect{X: menuBounds.X, Y: pos, Width: menuBounds.Width, Height: lay.rowH}, ' ', st)
				}
				// Explicit bg: transparent resolves to the terminal's dark
				// default on the text backend (dark boxes behind the labels);
				// the explicit bg equals the fill/hover color, so the
				// graphical look is unchanged.
				p.DrawText(menuBounds.X+lay.indent, pos+lay.yOff, termMenuLabel(it), st, lay.font)
				pos += lay.rowH
			}
		},
		HandleMouseMove: func(event core.MouseMoveEvent) bool {
			if !menuBounds.Contains(core.UnitPoint{X: event.X, Y: event.Y}) {
				return false
			}
			idx := itemAt(event.Y - menuBounds.Y)
			if idx >= 0 && items[idx].disabled {
				idx = -1
			}
			if idx != t.menuHover {
				t.menuHover = idx
				t.Update()
			}
			return true
		},
		HandleMousePress: func(event core.MousePressEvent) bool {
			idx := itemAt(event.Y - menuBounds.Y)
			pc.UnregisterPopup(t.contextMenuID())
			if idx >= 0 && !items[idx].disabled && items[idx].action != nil {
				items[idx].action()
			}
			t.Update()
			return true
		},
	})
	t.Update()
}
