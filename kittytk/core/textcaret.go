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

// ResetTextCaretRequest clears the frame's request. The surface host calls it
// before painting so each frame decides afresh.
func (p *Painter) ResetTextCaretRequest() {
	if p.caret != nil {
		p.caret.caret = TextCaret{}
	}
}
