// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"fmt"
	"math"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/phroun/khatool"

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
	cursorPos int
	selStart  int
	selEnd    int

	// scroll is how far into the field's RUN its own left edge falls -- a
	// place on the line as drawn, not a count of characters. A count cannot
	// say what a field shows once any of its content reads right to left: the
	// characters after the tenth are not the part of the line past the tenth
	// cell, and the two answers are different halves of the word.
	scroll core.Unit

	// moreLeft and moreRight say whether the run continues past the field's
	// edges, which is what the arrows at those edges are drawn for. They are
	// settled with the scroll -- the arrows take room, so whether one shows
	// changes how much run fits, and both answers have to come out of the
	// same reckoning.
	moreLeft, moreRight bool

	// showAhead is how much of the run stays visible past the caret, counted
	// in blanks (see showAheadUnits).
	showAhead int

	// showBidiControls turns the direction markers on: where a run turns, and
	// where an author's own direction control sits. They show while the field
	// is FOCUSED -- they are for working on the text, and a field being read
	// wants the plain picture.
	//
	// On by default. Someone editing a line that turns over needs to see where
	// it turns; a field that hid that until it was asked would hide it from
	// everyone who did not already know to ask.
	showBidiControls bool

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

	// dragTurned says the drag began inside a RIGHT-TO-LEFT run, where the
	// text further left is the text that comes LATER -- so the walk past the
	// edge runs the other way through the content.
	//
	// Settled once, at the press, and not looked at again. A drag that changed
	// its mind on crossing into a run of the other direction would start giving
	// back what it had already taken while the reader is still dragging the
	// same way; fixed, the chosen range only ever grows.
	dragTurned bool

	// Held on an end-of-run arrow: which one (-1 left, +1 right), and the
	// timer walking the caret that way while the button stays down. Separate
	// from the drag autoscroll above, which is a SELECTION being dragged; this
	// one carries the caret alone.
	arrowDir    int
	arrowExtend bool
	arrowTimer  *DesktopTimer

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
		echoMode:         EchoNormal,
		maxLength:        -1, // No limit
		showAhead:        defaultShowAhead,
		showBidiControls: true,
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

// CursorShapeAt implements core.CursorShaper: the I-beam over the text, and a
// plain arrow over the end-of-run arrows, which are chrome rather than text --
// a press there walks the caret toward that end instead of putting it under the
// pointer, and the shape says so before the press. The coordinates arrive in
// the same space as HandleMouseMove, so the geometry the press path uses
// locates them.
func (t *TextInput) CursorShapeAt(x, y core.Unit) core.CursorShape {
	if !t.IsEnabled() {
		return core.CursorDefault
	}
	if t.arrowAt(x) != 0 {
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
	t.scroll = 0
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

// ShowBidiControls reports whether the direction markers are asked for.
func (t *TextInput) ShowBidiControls() bool {
	return t.showBidiControls
}

// SetShowBidiControls turns the direction markers on or off: a mark where each
// fragment of the line begins and which way it reads, and the author's own
// direction controls shown as themselves rather than spent invisibly.
//
// They appear only while the field is FOCUSED. Marks take room and change what
// the line looks like, which is worth it while the text is being worked on and
// noise while it is being read, so a field that loses focus falls back to the
// plain picture of the same text.
//
// This is here to turn them OFF: they are on unless a caller says otherwise,
// because someone editing a line that turns over needs to see where it turns.
func (t *TextInput) SetShowBidiControls(show bool) {
	if t.showBidiControls == show {
		return
	}
	t.showBidiControls = show
	t.ensureCursorVisible()
	t.Update()
}

// ShowAhead is how much of the run stays visible past the caret, in characters.
func (t *TextInput) ShowAhead() int {
	return t.showAhead
}

// SetShowAhead sets how much of the run stays visible past the caret, counted
// in characters. Zero pins the caret to the edge it is scrolling toward.
func (t *TextInput) SetShowAhead(chars int) {
	if chars < 0 {
		chars = 0
	}
	t.showAhead = chars
	t.ensureCursorVisible()
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
	if bounds.Width <= 0 {
		return
	}
	// Measured against the COMPOSED run, so a growing input-method
	// composition pushes the view along instead of running off the edge: the
	// caret being chased is the one inside the composition.
	displayText, _, _, caret := t.composedText()
	g := t.runGeometry(displayText, t.EffectiveFont(), t.shapesText(), t.markersShown(), 0)
	t.scroll, _, t.moreLeft, t.moreRight = t.window(g, caret, bounds.Width,
		t.blankWidth(), t.scroll, t.scrollQuantum())
}

// shapesText reports whether the run is laid out by a shaper -- proportional
// glyphs, ordered and joined by the engine -- rather than by the cell rule.
func (t *TextInput) shapesText() bool { return core.HasTextMeasurer() }

// scrollQuantum is what the field's own left edge is allowed to land on
// multiples of: one cell where the target draws cells, and nothing at all
// where the run can slide smoothly under it.
func (t *TextInput) scrollQuantum() core.Unit {
	if t.shapesText() {
		return 0
	}
	return t.EffectiveCellMetrics().UnitsPerCellWidth
}

// blankWidth is one blank's worth of room: what the caret takes where there is
// no character under it, and the unit the show-ahead margin is counted in.
func (t *TextInput) blankWidth() core.Unit {
	if w := t.MeasureText(" "); w > 0 {
		return w
	}
	return t.EffectiveCellMetrics().UnitsPerCellWidth
}

// room is the span the RUN has inside the field: what is left of the field's
// width after the end-of-run arrows take their cells.
func (t *TextInput) room() (lo, hi core.Unit) {
	hi = t.Bounds().Width
	if t.moreLeft {
		lo = t.markWidth()
	}
	if t.moreRight {
		hi -= t.markWidth()
	}
	if hi < lo {
		hi = lo
	}
	return lo, hi
}

// arrowAt is which end-of-run arrow x lands on: -1 the left one, +1 the right,
// 0 neither. Only where one is actually drawn -- with nothing hidden that way
// the cell belongs to the run.
func (t *TextInput) arrowAt(x core.Unit) int {
	lo, hi := t.room()
	switch {
	case t.moreLeft && x >= 0 && x < lo:
		return -1
	case t.moreRight && x >= hi && x < t.Bounds().Width:
		return 1
	}
	return 0
}

// arrowStep moves the caret toward one end of the run: -1 the left, +1 the
// right. reach says whether it may go past what is already shown.
//
// A press takes the caret as far that way as the field ALREADY shows -- the
// outermost position in view, with nothing scrolling. When it is already there,
// a press reaches half the room's width further on and the view comes with it,
// which is what pressing an arrow that has stopped doing anything should do.
// Held down, it walks one character at a time instead, so the run creeps past
// rather than jumping.
func (t *TextInput) arrowStep(dir int, reach, extend bool) {
	if dir == 0 || t.Bounds().Width <= 0 {
		return
	}
	displayText, _, _, _ := t.composedText()
	g := t.runGeometry(displayText, t.EffectiveFont(), t.shapesText(), t.markersShown(), 0)
	blank := t.blankWidth()
	lo, hi := t.room()
	usable := hi - lo
	left := dir < 0

	// Whether the caret can stand at p without the view moving. It is the
	// WINDOW's own answer, asked directly, rather than a margin subtracted from
	// the room: the caret is normally kept clear of the edge it is walking
	// toward, but at the END of the run it legitimately stands inside that
	// margin, because there is nothing beyond it to look ahead at. Subtracting
	// the margin blindly refuses the last position and walks the caret back off
	// the end -- and the next step walks it forward again, which is a caret
	// twinkling between two places for as long as the arrow is held.
	stays := func(p int) bool {
		at, _, _, _ := t.window(g, p, t.Bounds().Width, blank, t.scroll, t.scrollQuantum())
		return at == t.scroll
	}

	// As far that way as the field already shows: the outermost position in the
	// room, walked back in until the view would sit still for it. The caret's
	// own position always would, so this ends.
	p, ok := g.outermostIn(t.scroll, t.scroll+usable, blank, left)
	for ok && !stays(p) {
		p, ok = g.nextVisual(p, blank, !left)
	}
	if ok && p != t.cursorPos {
		t.carryCaretTo(p, extend)
		return
	}
	if reach {
		// Half a room, ceiled so it is never a step of nothing. The view is
		// meant to move here, so the whole room is in play.
		step := (usable + 1) / 2
		at := t.scroll + step
		if left {
			at = t.scroll - step
		}
		if p, ok := g.outermostIn(at, at+usable, blank, left); ok && p != t.cursorPos {
			t.carryCaretTo(p, extend)
			return
		}
	}
	// Nothing fits the window asked for -- a field narrower than a character,
	// or the run already at its end. One position further along the line is
	// still an answer, and it is the one a held arrow wants anyway.
	if p, ok := g.nextVisual(t.cursorPos, blank, left); ok {
		t.carryCaretTo(p, extend)
	}
}

// carryCaretTo puts the caret at p and lets the window follow it, which is what
// scrolls the field: the view is worked out from where the caret is.
//
// extend drags the selection along instead of collapsing it -- the anchor is
// wherever it already was, and only the moving end follows, which is what
// shift does to every other way of moving the caret here.
func (t *TextInput) carryCaretTo(p int, extend bool) {
	if p < 0 {
		p = 0
	}
	if p > len(t.text) {
		p = len(t.text)
	}
	t.cursorPos = p
	if extend {
		t.selEnd = p
	} else {
		t.selStart, t.selEnd = p, p
	}
	t.ensureCursorVisible()
	t.resetCaretBlink()
	t.Update()
}

// startArrowRepeat walks the caret one character at a time while an arrow is
// held. The first step has already happened, so this waits out a hold's worth
// of stillness before repeating -- a press is a press, not the start of a run.
func (t *TextInput) startArrowRepeat(dir int, extend bool) {
	t.stopArrowRepeat()
	t.arrowDir, t.arrowExtend = dir, extend
	d := findDesktopFor(t)
	if d == nil {
		return
	}
	t.arrowTimer = d.StartTimer(400*time.Millisecond, func() {
		if t.arrowDir == 0 {
			return
		}
		t.arrowStep(t.arrowDir, false, t.arrowExtend)
		if d := findDesktopFor(t); d != nil {
			t.arrowTimer = d.StartRepeatingTimer(60*time.Millisecond, func() {
				t.arrowStep(t.arrowDir, false, t.arrowExtend)
			})
		}
	})
}

// stopArrowRepeat ends the walk when the button comes up.
func (t *TextInput) stopArrowRepeat() {
	if t.arrowTimer != nil {
		t.arrowTimer.Stop()
		t.arrowTimer = nil
	}
	t.arrowDir, t.arrowExtend = 0, false
}

// markersShown reports whether the direction markers are on the run: asked
// for, and the field focused. A field being read wants the plain picture.
func (t *TextInput) markersShown() bool {
	return t.showBidiControls && t.HasFocus()
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

	// A field is one line of text tall, and a line is one cell down. The
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

	// A background fill cannot be trusted on every RUN. A terminal that
	// reorders what it is sent counts codepoints where the grid counts cells,
	// so a fill over a run still carrying combining marks slides off the cells
	// it was meant for -- which on a pointed word is most of that word's
	// selection gone. Foreground colour and weight ride each glyph through that
	// reordering intact, so such a run wears those instead.
	//
	// Per RUN, because that is what the terminal reorders as a unit and so what
	// it counts wrongly. A field of chrome and English with one pointed word in
	// it gives up that word and keeps its bar everywhere else; giving up the
	// whole line for one vowel loses the fill everywhere it would have been
	// right. Folding settles which runs are in question: a point that folds
	// into its base no longer inflates the count, so pointed consonants come
	// out even. And only on a cell target, since a pixel one paints its own
	// fill and has no terminal to disagree with.
	// Filled in once the geometry says what order the cells go out in: "after"
	// means after on the SCREEN, and on a field that reads right to left that
	// is the other end of the text.
	var giveUpFill []bool
	ridingStyle := scheme.GetEditBoxSelectionRiding(focused && t.IsEnabled(), paneType)

	// runPx is a run's width in PIXELS, not its width in units scaled.
	// MeasureText rounds to whole units, which is the denomination the field is
	// laid out in and the wrong one for a position inside the run: the glyphs
	// rasterize at the unsnapped pixels-per-unit, so rounding at the unit and
	// again at the pixel drifts by up to half a unit against the very glyphs the
	// caret sits between. It shows wherever a rune's advance is a fraction of a
	// unit - a space beside CJK text is about two and a half.
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

	// Where the run went: the box every logical rune was drawn in, worked out
	// the way the target that will draw it works. Everything below is asked of
	// this - the caret, the selection, the composition's clause - because a
	// prefix measurement answers a different question on a line that turns over
	// (see fieldGeometry).
	ppu := 0.0
	if usePx {
		ppu = p.PxPerUnitF()
	}
	marked := t.markersShown() && !isPlaceholder
	g := t.runGeometry(displayText, font, t.shapesText(), marked, ppu)

	// Now the run's order is known, the fill question can be answered.
	if !usePx && core.HostMiscountsFill() {
		giveUpFill = khatool.UnplaceableFillAfterFold(
			displayText, core.RtlMarkFolds(), core.ZeroWidth, g.visualOrder())
	}

	// And where the field sits on it. The scroll is a place on the RUN, so a
	// field showing the middle of a line shows the middle of it as drawn.
	blank := t.blankWidth()
	var usable core.Unit
	t.scroll, usable, t.moreLeft, t.moreRight = t.window(g, cursorDisp,
		bounds.Width, blank, t.scroll, t.scrollQuantum())

	// The arrows are drawn INSIDE the field and the run gives up that much
	// room, so the text and the chrome describing it never overlap. roomLo and
	// roomHi are what is left for the run.
	mark := t.markWidth()
	fieldPx := p.UnitSpanPxX(0, bounds.Width)
	markPx := p.UnitSpanPxX(0, mark)
	roomLo := core.Unit(0)
	roomLoPx := 0
	if t.moreLeft {
		roomLo, roomLoPx = mark, markPx
	}
	// The right edge of the room is measured back from the field's own edge,
	// which is where the arrow is drawn: the two have to be the same pixel, or
	// the text runs under the arrow or stops short of it.
	roomHi, roomHiPx := bounds.Width, fieldPx
	if t.moreRight {
		roomHi, roomHiPx = bounds.Width-mark, fieldPx-markPx
	}
	if roomLo+usable < roomHi {
		roomHi, roomHiPx = roomLo+usable, roomLoPx+p.UnitSpanPxX(0, usable)
	}

	// originX is where the run's own zero falls in the field, which is what
	// every box below is shifted by.
	originX := roomLo - t.scroll
	originPx := roomLoPx - runPx("", 0)
	if usePx {
		originPx = roomLoPx - p.UnitsToPx(t.scroll)
	} else {
		originPx = roomLoPx - p.UnitSpanPxX(0, t.scroll)
	}

	// A span of the run, clipped to the room the run has. Off-field pieces of a
	// selection or a composition are not drawn short - they are not drawn.
	clipUnits := func(lo, hi core.Unit) (core.Unit, core.Unit, bool) {
		lo, hi = lo+originX, hi+originX
		if lo < roomLo {
			lo = roomLo
		}
		if hi > roomHi {
			hi = roomHi
		}
		return lo, hi, hi > lo
	}
	clipPx := func(lo, hi int) (int, int, bool) {
		lo, hi = lo+originPx, hi+originPx
		if lo < roomLoPx {
			lo = roomLoPx
		}
		if hi > roomHiPx {
			hi = roomHiPx
		}
		return lo, hi, hi > lo
	}
	unitPx := func(u core.Unit) int { return p.UnitSpanPxX(0, u) }

	// The run to hand the painter. A pixel target orders and shapes whole
	// paragraphs itself, so it gets the text (with substitutes spliced in where
	// there are any); a cell target gets the run already turned over.
	runStr := g.draw
	if runStr == "" {
		runStr = string(displayText)
	}
	// drawRun stamps the whole run and reveals only [lo, hi), both device-pixel
	// positions in the FIELD. A marked run is laid out piece by piece, each
	// piece moved right of where the shaper put it to leave room for the marks,
	// so the offset the run is stamped at is the offset of the piece the span
	// falls in.
	drawRun := func(lo, hi int, st style.CellStyle) {
		p.DrawTextOffsetClipped(0, 0, originPx+g.shiftAtPx(lo-originPx), lo, hi,
			runStr, st, font)
	}

	// Selection span (display indices) and the fixed anchor - the selection
	// end opposite the caret (selStart is the anchor; the caret is selEnd).
	// While composing there is no selection to draw: selStart/selEnd index
	// the COMMITTED text, which the composition has displaced, and the
	// selection is about to be replaced by whatever commits anyway.
	selLo, selHi := -1, -1
	if t.HasSelection() && !isPlaceholder && !composing {
		selLo, selHi = clampIdx(t.selStart), cursorDisp
		if selLo > selHi {
			selLo, selHi = selHi, selLo
		}
	}

	// 1. Draw the run once - stable regardless of caret/selection. Once per
	// PIECE where the direction marks have spread it apart, each piece being
	// the shaper's own glyphs moved along rather than glyphs re-shaped.
	if usePx {
		if len(g.pieces) == 0 {
			p.DrawTextOffsetClipped(0, 0, originPx+g.headPx, roomLoPx, roomHiPx,
				runStr, s, font)
		} else {
			for _, pc := range g.pieces {
				lo, hi, ok := clipPx(pc.loPx, pc.hiPx)
				if !ok {
					continue
				}
				p.DrawTextOffsetClipped(0, 0, originPx+pc.shiftPx, lo, hi, runStr, s, font)
			}
		}
	} else {
		cw := t.EffectiveCellMetrics().UnitsPerCellWidth
		vis, at := g.cellSlice(t.scroll, t.scroll+usable, cw)
		p.DrawText(originX+at, 0, vis, s, font)
	}

	// 2. Overstrike the selection: a highlight over each span, then the
	// selected text re-colored. On a pixel surface the re-color draws the SAME
	// whole run (identical glyph rasters as the base text) and reveals only
	// the selected columns with a pixel-precise clip, so the selected glyphs
	// never move - only the clip edge does - and neither the fixed anchor end
	// nor the interior jitters as the caret end grows the span. (Re-drawing a
	// re-shaped substring, right-aligned to the anchor, still jittered: the
	// substring re-shapes and its rounded left edge shifts every glyph.)
	//
	// SPANS, plural: a logical range that crosses a direction change sits in
	// two places on the line with unselected text between them, and one
	// rectangle over the pair would highlight what was not chosen.
	// The selection is painted in stretches, so a run that has to give up its
	// fill gives up only its own: the parts on either side keep the bar.
	for _, part := range fillStretches(giveUpFill, selLo, selHi) {
		st := selStyle
		if part.giveUp {
			st = ridingStyle
		}
		selFg := st.WithBg(style.ColorTransparent) // glyphs over the highlight
		if usePx {
			for _, sp := range g.spansPx(part.from, part.to, unitPx) {
				lo, hi, ok := clipPx(sp[0], sp[1])
				if !ok {
					continue
				}
				p.FillRectPixels(0, 0, lo, 0, hi-lo, rowHPx, st)
				drawRun(lo, hi, selFg)
			}
		} else {
			cw := t.EffectiveCellMetrics().UnitsPerCellWidth
			for _, sp := range g.spans(part.from, part.to) {
				lo, hi, ok := clipUnits(sp[0], sp[1])
				if !ok {
					continue
				}
				p.FillRect(core.UnitRect{X: lo, Width: hi - lo,
					Height: bounds.Height}, ' ', st)
				vis, at := g.cellSlice(lo-originX, hi-originX, cw)
				p.DrawText(originX+at, 0, vis, st, font)
			}
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
				for _, sp := range g.spansPx(preLo, preHi, unitPx) {
					lo, hi, ok := clipPx(sp[0], sp[1])
					if !ok {
						continue
					}
					drawRun(lo, hi, preStyle)
					p.FillRectPixels(0, 0, lo, ruleY, hi-lo, thin, rule(inactiveStyle.Fg))
				}
			}
			// Clipped from the WHOLE composition rather than drawn as its own
			// run, so the active span changes color without being re-shaped -
			// a substring shapes differently than the same characters mid-run,
			// and it would jitter as the candidate list is walked.
			activeFg := s.WithFg(clauseStyle.Fg).WithBg(style.ColorTransparent)
			for _, sp := range g.spansPx(clauseLo, clauseHi, unitPx) {
				lo, hi, ok := clipPx(sp[0], sp[1])
				if !ok {
					continue
				}
				drawRun(lo, hi, activeFg)
				p.FillRectPixels(0, 0, lo, ruleY, hi-lo, thin, rule(clauseStyle.Fg))
				if y := ruleY - thin; y >= 0 {
					p.FillRectPixels(0, 0, lo, y, hi-lo, thin, rule(clauseStyle.Fg))
				}
			}
		} else {
			// Cell surfaces have no sub-cell rule to draw, so the
			// underline is the attribute and the active span carries its
			// color and bold weight instead of a thicker rule.
			cw := t.EffectiveCellMetrics().UnitsPerCellWidth
			cellStyle := preStyle.WithAttrs(style.StyleUnderline)
			paint := func(lo, hi int, st style.CellStyle) {
				for _, sp := range g.spans(lo, hi) {
					a, b, ok := clipUnits(sp[0], sp[1])
					if !ok {
						continue
					}
					vis, at := g.cellSlice(a-originX, b-originX, cw)
					p.DrawText(originX+at, 0, vis, st, font)
				}
			}
			if hasInactive {
				paint(preLo, preHi, cellStyle)
			}
			paint(clauseLo, clauseHi, cellStyle.WithFg(clauseStyle.Fg).
				WithAttrs(style.StyleUnderline|style.StyleBold))
		}
	}

	// 4. The field's own notation: the direction markers, and the substitutes
	// standing in for characters that must not be drawn as themselves. Both in
	// the input method's active color, because both are the field speaking
	// about the text rather than showing it.
	if len(g.marks) > 0 {
		noteStyle := scheme.GetFocusedEditBoxIMEActiveClause()
		cw := t.EffectiveCellMetrics().UnitsPerCellWidth
		for _, m := range g.marks {
			if usePx {
				mLoPx, mHiPx := m.xPx, m.xPx+m.wPx
				if !g.havePx {
					mLoPx, mHiPx = unitPx(m.x), unitPx(m.x+m.w)
				}
				lo, hi, ok := clipPx(mLoPx, mHiPx)
				if !ok {
					continue
				}
				p.FillRectPixels(0, 0, lo, 0, hi-lo, rowHPx, noteStyle)
				fg := noteStyle.WithBg(style.ColorTransparent)
				if m.inRun {
					drawRun(lo, hi, fg)
				} else {
					// A direction mark is not part of the line the shaper laid
					// out -- it is what the field has to say about that line --
					// so it is stamped on its own, in the room the pieces were
					// spread apart to leave.
					p.DrawTextOffsetClipped(0, 0, mLoPx+originPx, lo, hi, m.text, fg, font)
				}
				continue
			}
			lo, hi, ok := clipUnits(m.x, m.x+m.w)
			if !ok {
				continue
			}
			p.FillRect(core.UnitRect{X: lo, Width: hi - lo,
				Height: bounds.Height}, ' ', noteStyle)
			vis, at := g.cellSlice(lo-originX, hi-originX, cw)
			p.DrawText(originX+at, 0, vis, noteStyle, font)
		}
	}

	// 5. The more-arrows, pinned to the field's own extreme edges: the run
	// continues that way past what is shown. They are drawn whether or not the
	// field is focused - the fact that there is text out of sight belongs to
	// the text, not to who is editing it - in the inverse of the active input
	// method color, which is the one pair in the scheme nothing else in a field
	// wears.
	if t.moreLeft || t.moreRight {
		active := scheme.GetFocusedEditBoxIMEActiveClause()
		arrow := active.WithFg(active.Bg).WithBg(active.Fg)
		if t.moreLeft {
			t.paintMoreArrow(p, moreLeftGlyph, 0, 0, mark, markPx, rowHPx, arrow, font, usePx)
		}
		if t.moreRight {
			t.paintMoreArrow(p, moreRightGlyph, bounds.Width-mark, fieldPx-markPx,
				mark, markPx, rowHPx, arrow, font, usePx)
		}
	}

	// Draw cursor - only in the active window chain: a trinket keeps local
	// focus while its window is in the background, but showing the caret
	// there would put two carets on screen.
	if !showCaret {
		return
	}
	caretLo, caretHi := g.caretBox(cursorDisp, blank)
	caretLoPx, caretHiPx := g.caretBoxPx(cursorDisp, runPx(" ", blank), unitPx)
	// The caret LEAVES its box by the leading edge of the direction the
	// character it sits on reads in, which is where a bar belongs and where an
	// input method's candidate window is anchored.
	caretX, caretXPx := caretLo+originX, caretLoPx+originPx
	if cursorDisp < n && g.rtl[cursorDisp] {
		caretX, caretXPx = caretHi+originX, caretHiPx+originPx
	} else if cursorDisp >= n && n > 0 && g.rtl[n-1] {
		caretX, caretXPx = caretHi+originX, caretHiPx+originPx
	}

	// The block cursor is a PAIR -- it covers a character and inverts it -- and
	// the insertion caret is one colour, since it covers nothing. The caret is
	// the brighter of the two by default: a few pixels wide, it has to be found
	// against whatever the field is painted in.
	cursorStyle := scheme.GetFocusedEditBoxCursor()
	caretInk := scheme.GetFocusedEditBoxCaret()
	barStyle := style.DefaultStyle().WithBg(caretInk)

	// On a cell surface the caret is the TERMINAL's own -- placed and shaped
	// through DECSCUSR, blinking on the reader's own settings, landing on the
	// cell grid a terminal cursor belongs on. A field that painted one there
	// would be putting a second caret beside the real one.
	//
	// A BAR sits at the left edge of the cell it is given, so it is asked for at
	// the caret's leading edge -- which on a right-to-left character is the cell
	// after that character. A BLOCK covers a cell, so it is asked for at the
	// cell the character itself occupies.
	if !usePx {
		// The field paints its own ground under the caret, so it says what
		// shows up on it: a terminal's caret colour is one global preference,
		// chosen against the terminal's own background, and a thin bar in it
		// disappears into a field that painted something else.
		if blockCaret {
			p.RequestTextCaret(caretLo, 0, decscusrBlock, cursorStyle.Bg)
		} else {
			p.RequestTextCaret(caretX, 0, decscusrBar, caretInk)
		}
		t.paintSecondaryCaret(p, g, displayText, cursorDisp, blank, bounds,
			cursorStyle, originX, clipUnits, font)
		return
	}

	// The graphical bar caret blinks (keystrokes restart the
	// phase); a block stays steady, on a read-only field. A blink says
	// "type here" and paces itself to a keystroke that is not coming.
	if !blockCaret {
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
		if lo, _, ok := g.boxOf(preLo); ok {
			areaX = lo + originX
		}
	}
	// Only where text can actually arrive: this is what an input
	// method anchors its candidate window to, and a field that
	// accepts nothing has nothing to compose for.
	if t.AcceptsTextInput() {
		p.RequestTextInputArea(areaX, 0)
	}

	if blockCaret {
		// The block covers the character the caret sits BEFORE -- the one it
		// is "at" -- painted in that text's own colours reversed: the field's
		// background becomes the ink and the ink becomes the ground. At the
		// end of the text there is no character to cover, so it takes one
		// blank's worth of the interior instead and comes out the same size
		// either way.
		//
		// Same two steps the selection uses: fill the span, then redraw the
		// glyphs clipped into it, so the block sits on the same pixel advance
		// the text was laid out at.
		//
		// The caret is always at one EDGE of a selection (the span runs anchor
		// to cursor), so the block covers a SELECTED character exactly when
		// the caret is at the left edge, which is what selecting backwards
		// leaves.
		overSel := selLo >= 0 && cursorDisp >= selLo && cursorDisp < selHi
		block := blockCaretStyle(s, selStyle, fillStyle.Bg, overSel)
		if lo, hi, ok := clipPx(caretLoPx, caretHiPx); ok {
			p.FillRectPixels(0, 0, lo, 0, hi-lo, rowHPx, block)
			drawRun(lo, hi, block.WithBg(style.ColorTransparent))
		}
		t.paintSecondaryCaretPx(p, g, displayText, cursorDisp, runPx(" ", blank),
			rowHPx, barStyle, clipPx)
		return
	}
	if !t.caretVisible() {
		return
	}
	if caretX < roomLo || caretX > roomHi {
		return
	}
	// Site the bar at the same accumulated pixel advance the glyphs painted at,
	// so it sits exactly on the boundary before the cursor's character.
	bar := p.DeviceScale()
	x := caretXPx
	if x+bar > roomHiPx {
		x = roomHiPx - bar
	}
	p.FillRectPixels(0, 0, x, 0, bar, rowHPx, barStyle)
	t.paintSecondaryCaretPx(p, g, displayText, cursorDisp, runPx(" ", blank),
		rowHPx, barStyle, clipPx)
}

// The DECSCUSR shapes a field asks the platform for. A bar sits between two
// characters, where text goes IN; a block sits on the character you are at,
// where it does not. Blinking says the field is waiting for a key; a field
// being read is not, so its block is steady.
const (
	decscusrBlock = 2
	decscusrBar   = 5
)

// paintSecondaryCaret marks the caret's other reading on a cell surface: the
// cell one past the character before the caret, in that character's own
// direction, drawn in reverse video the way the primary cell caret is.
//
// The terminal owns the one real caret, so this is painted. Which is right for
// what it is: the primary is where the caret IS, and this is where the text on
// the other side of the turn carries on.
func (t *TextInput) paintSecondaryCaret(p *core.Painter, g *fieldGeometry,
	runes []rune, caret int, blank core.Unit, bounds core.UnitRect,
	st style.CellStyle, originX core.Unit,
	clip func(lo, hi core.Unit) (core.Unit, core.Unit, bool), font *core.Font) {
	q, leftOf, ok := g.secondaryCaretAt(runes, caret)
	if !ok {
		return
	}
	lo, hi := g.hi[q], g.hi[q]+blank
	if leftOf {
		lo, hi = g.lo[q]-blank, g.lo[q]
		if lo < 0 {
			return
		}
	}
	a, b, on := clip(lo, hi)
	if !on {
		return
	}
	cw := t.EffectiveCellMetrics().UnitsPerCellWidth
	p.FillRect(core.UnitRect{X: a, Width: b - a, Height: bounds.Height}, ' ', st)
	vis, at := g.cellSlice(a-originX, b-originX, cw)
	p.DrawText(originX+at, 0, vis, st, font)
}

// paintSecondaryCaretPx is paintSecondaryCaret on a pixel surface, where the
// primary is a bar: the same mark in the same place, half the height, so the
// two are told apart at a glance.
func (t *TextInput) paintSecondaryCaretPx(p *core.Painter, g *fieldGeometry,
	runes []rune, caret, blankPx, rowHPx int, st style.CellStyle,
	clip func(lo, hi int) (int, int, bool)) {
	q, leftOf, ok := g.secondaryCaretAt(runes, caret)
	if !ok || !g.havePx {
		return
	}
	x := g.hiPx[q]
	if leftOf {
		x = g.loPx[q]
	}
	bar := p.DeviceScale()
	if bar < 1 {
		bar = 1
	}
	lo, _, on := clip(x, x+bar)
	if !on {
		return
	}
	p.FillRectPixels(0, 0, lo, rowHPx/2, bar, rowHPx-rowHPx/2, st)
}

// paintMoreArrow draws one end-of-run arrow at a fixed place in the field:
// unit x on a cell surface, device pixel xPx on a pixel one, so it sits
// exactly on the field's own edge rather than a rounding of it.
func (t *TextInput) paintMoreArrow(p *core.Painter, glyph rune, x core.Unit, xPx int,
	w core.Unit, wPx, rowHPx int, s style.CellStyle, font *core.Font, usePx bool) {
	if usePx {
		p.FillRectPixels(0, 0, xPx, 0, wPx, rowHPx, s)
		// Drawn rather than set from the font. A glyph's width belongs to the
		// face and this box is one cell wide whatever face the field carries,
		// so the glyph would have to be trimmed to fit -- and a triangle
		// trimmed at the point is an arrow with no point.
		fillTrianglePx(p, xPx, wPx, rowHPx, glyph == moreLeftGlyph, s.Fg)
		return
	}
	p.FillRect(core.UnitRect{X: x, Width: w,
		Height: t.EffectiveCellMetrics().UnitsPerCellHeight}, ' ', s)
	p.DrawText(x, 0, string(glyph), s, font)
}

// fillTrianglePx paints a solid triangle inside a device-pixel box, pointing
// left or right: rows of the box, each as long as the triangle is wide there,
// aligned against the vertical side the point is opposite.
func fillTrianglePx(p *core.Painter, xPx, wPx, hPx int, pointLeft bool, c style.Color) {
	// Inset, so the arrow reads as a mark inside the field's edge rather than
	// a block filling it.
	pad := hPx / 5
	top, h := pad, hPx-2*pad
	w := wPx - wPx/4
	if h < 1 || w < 1 {
		top, h, w = 0, hPx, wPx
	}
	left := xPx + (wPx-w)/2
	fill := style.DefaultStyle().WithBg(c)
	mid := float64(h-1) / 2
	for i := 0; i < h; i++ {
		d := 1.0
		if mid > 0 {
			d = math.Abs(float64(i)-mid) / mid
		}
		run := int(math.Round(float64(w) * (1 - d)))
		if run < 1 {
			run = 1
		}
		x := left
		if pointLeft {
			x = left + w - run
		}
		p.FillRectPixels(0, 0, x, top+i, run, 1, fill)
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

// findCharAtX is the insertion point a click at x asks for.
//
// It is resolved through the run's own geometry, not by measuring prefixes:
// on a line that turns over, the character drawn under the pointer is not the
// one a prefix of that width ends at, and the two answers are at opposite ends
// of a word. The box the click landed in names the character; which HALF of
// that box it landed in says whether the caret goes before or after it, read in
// that character's own direction, so clicking the right half of a Hebrew letter
// puts the caret on the letter's left the way it does everywhere else - after
// it, in the direction the word is read.
func (t *TextInput) findCharAtX(x core.Unit, font *core.Font) int {
	displayText := t.getDisplayText()
	if len(displayText) == 0 {
		return 0
	}
	g := t.runGeometry(displayText, font, t.shapesText(), t.markersShown(), 0)

	// Into the run's own coordinates: x arrives in the field's space, where
	// the run sits shifted by the scroll and by whatever room the left arrow
	// took.
	at := x + t.scroll
	if t.moreLeft {
		at -= t.markWidth()
	}

	best, bestGap := -1, core.Unit(0)
	for i := range displayText {
		lo, hi, ok := g.boxOf(i)
		if !ok || hi <= lo {
			continue
		}
		// Inside the box the half decides; outside it the nearer edge does,
		// which is what answers a click past an end of the run or in the gap a
		// turn leaves.
		var gap core.Unit
		var after bool
		switch {
		case at < lo:
			gap, after = lo-at, g.rtl[i]
		case at >= hi:
			gap, after = at-hi, !g.rtl[i]
		default:
			mid := lo + (hi-lo)/2
			after = at >= mid
			if g.rtl[i] {
				after = at < mid
			}
		}
		if best < 0 || gap < bestGap {
			best, bestGap = i, gap
			if after {
				best = i + 1
			}
		}
		if gap == 0 {
			break
		}
	}
	if best < 0 {
		return len(displayText)
	}
	return best
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
		t.scroll = 0
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
		// The end-of-run arrows are chrome, not text: a press on one walks the
		// caret toward that end rather than placing it where the pointer is.
		// It is not part of a multi-click run either -- a fast second press on
		// an arrow is a second press, not a word to select.
		if dir := t.arrowAt(event.X); dir != 0 {
			extend := event.Modifiers&core.ShiftModifier != 0
			t.SetFocus()
			t.clickStreak = 0
			t.selecting = false
			t.arrowStep(dir, true, extend)
			t.startArrowRepeat(dir, extend)
			return true
		}
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
			t.dragTurned = t.readsRightToLeftAt(pos)
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
				t.dragTurned = t.readsRightToLeftAt(pos)
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
// either side it hands off to the autoscroll timer (which keeps walking the
// selection while the pointer is held still out there); above or below it takes
// everything to that end of the content; inside the box it tracks the pointer
// directly.
func (t *TextInput) HandleMouseMove(event core.MouseMoveEvent) bool {
	if !t.selecting || event.Buttons&core.LeftButton == 0 {
		return false
	}
	// Dragged clear of the field ABOVE or BELOW, the selection runs to the end
	// of the content that way: back to the beginning, or on to the end. A
	// field is one line, so leaving it upward or downward is leaving the text
	// altogether -- there is no next line to reach for, and the only thing
	// further that way is the rest of what is here.
	//
	// It answers before the sideways reach and stops it, because it is an
	// answer rather than a walk: a pointer dragged out past a corner is asking
	// for everything to that end, and there is nothing for a timer to keep
	// stepping toward afterwards.
	bounds := t.Bounds()
	if event.Y < 0 || event.Y >= bounds.Height {
		t.stopAutoScroll()
		to := len(t.text)
		if event.Y < 0 {
			to = 0
		}
		if to != t.cursorPos {
			t.cursorPos = to
			t.selEnd = to
			t.ensureCursorVisible()
			t.resetCaretBlink()
			t.Update()
		}
		return true
	}

	// Past the room's edge, not the field's. The arrows sit INSIDE the field
	// and the run gives up their cells, so the pointer reaching one is already
	// past everything there is to select -- which is exactly when a drag wants
	// the text to keep coming.
	roomLo, roomHi := t.room()
	if event.X < roomLo {
		t.scrollOverX = roomLo - event.X
		t.startAutoScroll(-1)
		return true
	}
	if event.X >= roomHi {
		t.scrollOverX = event.X - roomHi
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

// readsRightToLeftAt reports whether the run holding position p reads right to
// left. Past the end of the text the last character answers, which is the same
// rule the caret's own last position follows.
func (t *TextInput) readsRightToLeftAt(p int) bool {
	displayText, _, _, _ := t.composedText()
	g := t.runGeometry(displayText, t.EffectiveFont(), t.shapesText(), t.markersShown(), 0)
	if len(g.rtl) == 0 {
		return false
	}
	if p >= len(g.rtl) {
		p = len(g.rtl) - 1
	}
	if p < 0 {
		p = 0
	}
	return g.rtl[p]
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

// autoScrollStep walks the caret the way the pointer is reaching, extending the
// selection and scrolling to keep it visible. The step size grows with how
// far the pointer is past the edge - a nudge crawls, a big overshoot races -
// and it stops itself at either end of the text.
//
// It walks the TEXT, one character at a time, so the chosen range only ever
// grows. Which way through the text the pointer's direction reaches was settled
// at the press (see dragTurned).
func (t *TextInput) autoScrollStep() {
	if t.scrollDir == 0 {
		return
	}
	// Which way through the TEXT the pointer's direction reaches. In a
	// right-to-left run the text further left is the text further on, so the
	// two are opposites -- and which it is was settled at the press.
	through := t.scrollDir
	if t.dragTurned {
		through = -through
	}
	moved := false
	for i := 0; i < t.autoScrollSpeed(); i++ {
		if through < 0 {
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

// HandleMouseRelease ends a drag selection, or a held end-of-run arrow.
func (t *TextInput) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	if t.arrowDir != 0 {
		t.stopArrowRepeat()
		return true
	}
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
	t.stopArrowRepeat()
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
	menuBounds := gridPopupRect(t.Self(), termMenuScreenMetrics(pc),
		core.UnitRect{X: at.X, Y: at.Y, Width: lay.width, Height: height})
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
					paintTermMenuSeparator(p, menuBounds, pos, lay)
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
