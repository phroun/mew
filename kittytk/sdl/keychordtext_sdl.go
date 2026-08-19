//go:build sdl

package sdl

// What this keyboard was watched TYPING, by the chord that typed it.
//
// The window system hands over both halves of a keystroke that produces text:
// the key with its modifiers, which names the chord, and the text itself. The
// name is what a binding is written against; the text is what the key does on
// this machine, under this layout, with this composition behaviour — and no
// table can know that as well as watching can.
//
// macOS Option is the first place it pays: Option+a is the chord M-a and the
// character "å", and an application that wants to type the character for an
// unbound M-a would otherwise be guessing from a US-layout table. See
// core.KeyChordTextSource.

// noteKeyChordText records what a chord produced.
//
// Written afresh every time: if the same chord starts producing something else
// mid-session, the new text is what this keyboard does now, and the old was
// only ever a record of what it did before.
func (p *Platform) noteKeyChordText(chord, text string) {
	if chord == "" || text == "" {
		return
	}
	p.chordTextMu.Lock()
	defer p.chordTextMu.Unlock()
	if p.chordText == nil {
		p.chordText = make(map[string]string)
	}
	p.chordText[chord] = text
}

// KeyChordText implements core.KeyChordTextSource.
func (p *Platform) KeyChordText(chord string) (string, bool) {
	p.chordTextMu.Lock()
	defer p.chordTextMu.Unlock()
	text, ok := p.chordText[chord]
	return text, ok
}

// AllKeyChordText implements core.KeyChordTextSource.
func (p *Platform) AllKeyChordText() map[string]string {
	p.chordTextMu.Lock()
	defer p.chordTextMu.Unlock()
	out := make(map[string]string, len(p.chordText))
	for k, v := range p.chordText {
		out[k] = v
	}
	return out
}
