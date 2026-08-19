package viewport

// Text an input method is still composing: typed, shown, but not part of the
// document.
//
// A Japanese input method builds "きょう" from three keystrokes and only hands
// it over when it is confirmed; macOS's accent palette holds a letter's
// alternatives the same way. Until then the characters belong to the input
// method, and a buffer that stored them would have to un-store them on every
// update — through the undo history, which is where the damage would be.
//
// So they are painted and not stored, the way a control character is painted
// as "^X" without the buffer holding two runes. The composition is SYNTHESIZED
// into the line at the caret when that line is prepared for display, and from
// there it is ordinary text: the existing width model measures it, a wide
// character counts two columns, and nothing needs a preedit-aware twin of the
// column arithmetic.
//
// One offset is the whole of the bookkeeping. The composition is one
// contiguous run at one known position, so a document rune's display position
// is its own plus the length of the composition when it sits at or after it —
// arithmetic, not a mapping table.
type Preedit struct {
	// Text is the whole composition. Empty means nothing is being composed.
	Text []rune

	// Caret is the input method's own cursor, as a rune index into Text. The
	// document caret paints there rather than at either end, which is what
	// lets an input method show progress through a long composition.
	Caret int

	// Line and Rune are where it sits: a document line, and a rune position on
	// that line. The composition follows the caret, so these are the caret's
	// position at the moment the composition was last updated.
	Line, Rune int

	// Covers is how many COMMITTED runes immediately before that position the
	// composition stands over, and HIDES while it stands.
	//
	// Normally 0: an ordinary composition builds text that was never in the
	// document. macOS's press-and-hold palette is why it is not always — it
	// commits the held letter the moment the key goes down and only then opens
	// over it, so the letter has to be hidden while its alternatives are shown
	// or the line reads "oò". Nothing is deleted to hide it: ending the
	// composition brings it straight back, which is what cancelling means.
	Covers int
}

// Active reports whether anything is being composed.
func (p Preedit) Active() bool { return len(p.Text) > 0 }

// Preedit returns what this viewport is composing, if anything.
func (w *Viewport) Preedit() Preedit { return w.preedit }

// SetPreedit installs the composition to paint at the caret, or an empty one to
// end it. The caller supplies the position, because the composition belongs
// where the caret was when the input method last spoke.
func (w *Viewport) SetPreedit(p Preedit) {
	if p.Caret < 0 {
		p.Caret = 0
	}
	if p.Caret > len(p.Text) {
		p.Caret = len(p.Text)
	}
	w.preedit = p
}

// ClearPreedit ends the composition without committing it.
func (w *Viewport) ClearPreedit() { w.preedit = Preedit{} }

// PreeditOnLine reports whether a composition is being painted into docLine.
func (w *Viewport) PreeditOnLine(docLine int) bool {
	return w.preedit.Active() && w.preedit.Line == docLine
}

// PreeditSplice returns the line as it should be DISPLAYED, with the
// composition synthesized in at its position, and the rune range [lo, hi) the
// composition occupies in that display line.
//
// lo == hi means nothing was spliced, and the line is returned unchanged — the
// answer for every line but the one being composed on.
//
// The position is clamped to the line, so a composition whose caret has been
// left behind by an edit lands at the end rather than out of range.
func (w *Viewport) PreeditSplice(docLine int, line string) (display string, lo, hi int) {
	if !w.PreeditOnLine(docLine) {
		return line, 0, 0
	}
	runes := []rune(line)
	at := w.preedit.Rune
	if at < 0 {
		at = 0
	}
	if at > len(runes) {
		at = len(runes)
	}
	// What the composition covers is hidden behind it rather than painted in
	// front of it. Nothing is removed from the line the buffer holds — this is
	// the display line, and ending the composition shows the text again.
	from := at - w.preedit.Covers
	if from < 0 {
		from = 0
	}
	out := make([]rune, 0, len(runes)+len(w.preedit.Text))
	out = append(out, runes[:from]...)
	out = append(out, w.preedit.Text...)
	out = append(out, runes[at:]...)
	return string(out), from, from + len(w.preedit.Text)
}

// PreeditShift is how many display runes a composition adds BEFORE a document
// position — the whole of the document-to-display mapping, since the
// composition is one run at one place.
//
// A document rune at the composition's own position sits after it: the
// composition is painted at the caret, and text the caret was in front of stays
// in front of it.
func (w *Viewport) PreeditShift(docLine, docRune int) int {
	if !w.PreeditOnLine(docLine) || docRune < w.preedit.Rune-w.preedit.Covers {
		return 0
	}
	// What it hides comes off the shift: the composition stands in those runes'
	// place rather than in front of them.
	return len(w.preedit.Text) - w.preedit.Covers
}

// PreeditCaretRune is where the document caret paints on the display line while
// a composition stands there: inside the composition, at the input method's own
// cursor. It answers the plain position when nothing is being composed.
func (w *Viewport) PreeditCaretRune(docLine, docRune int) int {
	if !w.PreeditOnLine(docLine) || docRune != w.preedit.Rune {
		return docRune + w.PreeditShift(docLine, docRune)
	}
	from := w.preedit.Rune - w.preedit.Covers
	if from < 0 {
		from = 0
	}
	return from + w.preedit.Caret
}
