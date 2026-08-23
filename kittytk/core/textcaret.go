package core

// The text caret is the platform's OWN cursor — the one a terminal draws and
// blinks itself, in whatever shape DECSCUSR selected. It is distinct from the
// mouse cursor (see cursor.go) and from any caret a trinket paints into its own
// cells.
//
// A trinket that wants it calls Painter.RequestTextCaret during Paint, in its
// local coordinates; the painter transforms them to surface coordinates. The
// surface host applies the frame's request after painting (see SurfaceHost).
//
// Two rules give the caret to the right trinket with no policy code:
//
//   - A trinket requests it only while it holds keyboard focus, so an unfocused
//     terminal keeps painting its own caret instead.
//   - The LAST request of a frame wins, and painting runs back to front, so an
//     overlay, a menu, or a torn-off window painted above the content takes the
//     caret from whatever is underneath.
//
// A frame with no request leaves the platform caret hidden.

// TextCaret is one frame's platform-caret request, in surface coordinates.
type TextCaret struct {
	// Visible reports that some trinket asked for the caret this frame.
	Visible bool
	// X, Y are the caret's cell position in surface coordinates.
	X, Y Unit
	// Style is the DECSCUSR shape: 0 the terminal's own default, 1/2
	// blinking/steady block, 3/4 underline, 5/6 bar.
	Style int

	// InputArea marks (X, Y) as the INSERTION POINT for an input method,
	// whether or not the platform draws a caret there.
	//
	// The two are not the same question. A terminal wants the platform's
	// caret AND is the insertion point, so RequestTextCaret sets both. A
	// text field draws its own caret — a blinking bar, or a reverse-video
	// block on a cell surface — and asking for the platform's would paint
	// two; it wants only this, so an input method can put its candidate
	// window under the text being typed instead of at a default corner.
	InputArea bool
}

// Requested reports whether this frame asked for anything at all. A
// surface with neither a caret to draw nor an insertion point to report
// forgets both.
func (c TextCaret) Requested() bool { return c.Visible || c.InputArea }

// caretSink is the per-frame request slot. Painters derived with
// WithTransform/WithClip copy the POINTER, so a request made deep in the tree
// reaches the surface host that owns the frame.
type caretSink struct {
	caret TextCaret
}

// TextSink is a trinket that TYPES: one where a keystroke becomes text rather
// than a command. A text input, a terminal, an editor hosted in one.
//
// It exists because the caret request above cannot answer this. That request is
// made during PAINT, so its absence means "nothing drew an insertion point this
// frame", which is not the same as "text is not going anywhere" — a frame
// clipped to a damaged region, or one painted while a cursor was in the dark
// half of its blink, reports nothing and means nothing by it. Whether text has
// a destination is a question about focus, and focus is state.
type TextSink interface {
	// AcceptsTextInput reports whether keystrokes reaching this trinket
	// become text. A trinket that is disabled, or read-only, says false.
	AcceptsTextInput() bool
}

// TextSinkState is what the owner of a frame could determine about whether
// text is going anywhere at all.
//
// Unknown is not "no". It is the same absence the mode tokens use: a caller
// with no focus manager to ask has learned nothing, and acting on nothing —
// telling the OS there is no insertion point — would be inventing an answer.
type TextSinkState int

const (
	// TextSinkUnknown: there was nothing to ask.
	TextSinkUnknown TextSinkState = iota
	// TextSinkAbsent: focus is somewhere, and that somewhere does not type.
	TextSinkAbsent
	// TextSinkPresent: the trinket holding focus types.
	TextSinkPresent
)

// FocusedTextSink asks a focus manager whether the trinket holding focus types.
//
// Nothing focused answers Absent rather than Unknown: a focus manager that has
// been asked and holds nobody has given a real answer.
func FocusedTextSink(fm *FocusManager) TextSinkState {
	if fm == nil {
		return TextSinkUnknown
	}
	focused := fm.FocusedTrinket()
	if focused == nil {
		return TextSinkAbsent
	}
	if sink, ok := focused.(TextSink); ok && sink.AcceptsTextInput() {
		return TextSinkPresent
	}
	return TextSinkAbsent
}

// RequestTextCaret asks the platform to place its real text caret at local
// (x, y) with the given DECSCUSR shape. Later requests replace earlier ones —
// paint order decides, so whatever paints on top owns the caret. Out-of-range
// shapes are clamped to the terminal default.
func (p *Painter) RequestTextCaret(x, y Unit, style int) {
	if p.caret == nil {
		return
	}
	if style < 0 || style > 6 {
		style = 0
	}
	sx, sy := p.toScreen(x, y)
	// A drawn caret is also where typing goes, so this is an insertion
	// point too and an input method can anchor on it.
	p.caret.caret = TextCaret{Visible: true, InputArea: true, X: sx, Y: sy, Style: style}
}

// RequestTextInputArea marks local (x, y) as the insertion point for an
// input method WITHOUT asking the platform to draw a caret there. A
// trinket that paints its own caret uses this: it still needs the OS to
// know where the text is, or the CJK candidate list, the macOS
// press-and-hold accent picker and the emoji picker all appear at
// whatever corner the OS last used.
//
// Same last-request-wins rule as RequestTextCaret, and they share the
// slot: whichever paints on top owns both answers.
func (p *Painter) RequestTextInputArea(x, y Unit) {
	if p.caret == nil {
		return
	}
	sx, sy := p.toScreen(x, y)
	p.caret.caret = TextCaret{InputArea: true, X: sx, Y: sy}
}

// TextCaretRequest returns the caret requested during this frame (Visible false
// when none was).
func (p *Painter) TextCaretRequest() TextCaret {
	if p.caret == nil {
		return TextCaret{}
	}
	return p.caret.caret
}

// MarkPartial records that this frame paints only a damaged region rather than
// the whole surface. Called by the platform before it hands the painter to a
// surface handler; derived painters carry it.
func (p *Painter) MarkPartial() { p.partial = true }

// Complete reports whether this frame painted the whole surface, and so
// whether what it did NOT report is worth anything. A partial frame that
// mentioned no insertion point has not said there is none.
func (p *Painter) Complete() bool { return !p.partial }

// ResetTextCaretRequest clears the frame's request. The surface host calls it
// before painting so each frame decides afresh.
func (p *Painter) ResetTextCaretRequest() {
	if p.caret != nil {
		p.caret.caret = TextCaret{}
	}
}
