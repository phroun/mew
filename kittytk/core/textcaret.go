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
}

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
	p.caret.caret = TextCaret{Visible: true, X: sx, Y: sy, Style: style}
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
