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
// WHERE it sits is a garland cursor of the viewport's own, parked when the
// composition opens. Not the caret, and not a remembered line and rune: a
// composition outlives edits made while it stands. A palette dismissed by
// typing puts the keystroke in before the input method's commit catches up, and
// a position measured from the caret then points at the character that
// keystroke typed — the accent replaced it and the letter stayed, "oò" with
// the "." eaten. A cursor slides with the edit and still points at the letter.
type Preedit struct {
	// Text is the whole composition. Empty means nothing is being composed.
	Text []rune

	// Caret is the input method's own cursor, as a rune index into Text. The
	// document caret paints there rather than at either end, which is what
	// lets an input method show progress through a long composition.
	Caret int

	// Covers is how many COMMITTED runes from the composition's own cursor it
	// stands over, and HIDES while it stands.
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

// PreeditAt reports where the standing composition's region STARTS, and whether
// there is one.
//
// It reads a garland cursor, so it is still pointing at the same text after
// anything else in the buffer has been edited.
func (w *Viewport) PreeditAt() (line, runePos int, ok bool) {
	if !w.preedit.Active() || w.preeditAt == nil {
		return 0, 0, false
	}
	line, runePos = w.preeditAt.LineRune()
	return line, runePos, true
}

// SetPreedit installs the composition to paint, or ends it when the text is
// empty.
//
// covers is how many committed runes BEFORE THE CARET it stands over, which is
// how an input method describes it — and the anchor is minted from that once,
// when the composition opens. Later updates leave it where it is: the caret may
// have moved on, and the region has not.
func (w *Viewport) SetPreedit(text []rune, caret, covers int) {
	if len(text) == 0 {
		w.ClearPreedit()
		return
	}
	if caret < 0 {
		caret = 0
	}
	if caret > len(text) {
		caret = len(text)
	}
	if covers < 0 {
		covers = 0
	}

	if !w.preedit.Active() {
		// Opening. The cursor is minted ONCE per viewport and parked again on
		// each composition — garland adjusts every live cursor on every edit,
		// so one per viewport is a cost worth paying and one per palette is
		// not.
		pos := w.CursorPos()
		from := pos.Rune - covers
		if from < 0 {
			from, covers = 0, pos.Rune
		}
		if w.preeditAt == nil && w.Buffer != nil {
			w.preeditAt = w.Buffer.NewAnchor()
		}
		if w.preeditAt != nil {
			w.preeditAt.SeekLineRune(pos.Line, from)
		}
		w.preedit.Covers = covers
	}
	w.preedit.Text = text
	w.preedit.Caret = caret
}

// ClearPreedit ends the composition without committing it.
//
// The cursor stays: it is the viewport's, not this composition's, and parking
// it again costs nothing where minting a fresh one on every palette would. It
// is released with the viewport's other cursors (see releasePreedit).
func (w *Viewport) ClearPreedit() { w.preedit = Preedit{} }

// releasePreedit gives up the composition's cursor, called where the viewport's
// other tracking cursors are given up.
func (w *Viewport) releasePreedit() {
	if w.preeditAt != nil {
		w.preeditAt.Release()
		w.preeditAt = nil
	}
	w.preedit = Preedit{}
}

// PreeditOnLine reports whether a composition is being painted into docLine.
func (w *Viewport) PreeditOnLine(docLine int) bool {
	if !w.preedit.Active() {
		return false
	}
	line, _, ok := w.PreeditAt()
	return ok && line == docLine
}

// PreeditSplice returns the line as it should be DISPLAYED, with the
// composition synthesized in over the region it stands on, and the rune range
// [lo, hi) the composition occupies in that display line.
//
// lo == hi means nothing was spliced, and the line is returned unchanged — the
// answer for every line but the one being composed on.
//
// Positions are clamped to the line, so a composition whose region has been
// shortened out from under it paints at the end rather than out of range.
func (w *Viewport) PreeditSplice(docLine int, line string) (display string, lo, hi int) {
	if !w.PreeditOnLine(docLine) {
		return line, 0, 0
	}
	runes := []rune(line)
	_, from, _ := w.PreeditAt()
	if from < 0 {
		from = 0
	}
	if from > len(runes) {
		from = len(runes)
	}
	// What the composition covers is hidden behind it rather than painted in
	// front of it. Nothing is removed from the line the buffer holds — this is
	// the display line, and ending the composition shows the text again.
	to := from + w.preedit.Covers
	if to > len(runes) {
		to = len(runes)
	}
	out := make([]rune, 0, len(runes)+len(w.preedit.Text))
	out = append(out, runes[:from]...)
	out = append(out, w.preedit.Text...)
	out = append(out, runes[to:]...)
	return string(out), from, from + len(w.preedit.Text)
}

// PreeditShift is how many display runes a composition adds BEFORE a document
// position — the whole of the document-to-display mapping, since the
// composition is one run at one place.
//
// What it hides comes off the shift: the composition stands in those runes'
// place rather than in front of them.
func (w *Viewport) PreeditShift(docLine, docRune int) int {
	if !w.PreeditOnLine(docLine) {
		return 0
	}
	_, from, _ := w.PreeditAt()
	if docRune < from {
		return 0
	}
	return len(w.preedit.Text) - w.preedit.Covers
}

// PreeditCaretRune is where the document caret paints on the display line while
// a composition stands there: inside the composition, at the input method's own
// cursor, when the caret is still in the region it covers. A caret that has
// moved past the composition is merely shifted by it.
func (w *Viewport) PreeditCaretRune(docLine, docRune int) int {
	if !w.PreeditOnLine(docLine) {
		return docRune
	}
	_, from, _ := w.PreeditAt()
	if docRune >= from && docRune <= from+w.preedit.Covers {
		return from + w.preedit.Caret
	}
	return docRune + w.PreeditShift(docLine, docRune)
}
