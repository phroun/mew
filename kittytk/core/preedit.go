package core

// Preedit is text an input method is still composing: typed, shown, but
// not yet part of the document. A Japanese IME builds "きょう" from three
// keystrokes and only hands it over on Enter; until then the characters
// belong to the IME, and a text field that stored them in its own buffer
// would have to un-store them when the composition changed.
//
// So a trinket keeps its committed text and its preedit apart, and
// paints the preedit at the caret with the convention every platform
// uses: underlined, marked as provisional. Nothing about it is editable
// by the field's own keys — arrows, backspace and the rest go to the
// input method, which sends back a whole new composition.

// TextEditingEvent carries one update to the in-flight composition. Text
// replaces the previous preedit entirely (input methods send the whole
// string every time, not a delta), and an empty Text ends the
// composition — which is how a cancelled one arrives too.
//
// Start and Length describe the input method's own selection WITHIN
// Text: the clause being converted, when the IME distinguishes one. Both
// are -1 when unset. Their units are the one thing the SDL3
// documentation leaves open, so PreeditFrom treats them as rune offsets
// (what every backend that sets them reports) and clamps rather than
// trusting them.
type TextEditingEvent struct {
	Text   string
	Start  int
	Length int

	// Covers is how many COMMITTED runes immediately before the composition
	// this composition stands over — text already in the document that the
	// input method has taken back to re-work, and that the composition hides
	// while it stands.
	//
	// Normally 0: an ordinary composition builds text that was never there.
	// macOS's press-and-hold palette is why it is not always: it commits the
	// held letter the moment the key goes down and only then opens over it, so
	// the letter is in the document and must be hidden while its alternatives
	// are shown, then replaced by whichever is chosen. Reconversion — selecting
	// a committed word and re-opening it — is the same fact with a bigger
	// number.
	//
	// The extent is the composition's, not the commit's. A commit replaces
	// whatever its composition covered, which the sink already knows, so
	// nothing about the extent has to travel twice.
	Covers int
}

func (TextEditingEvent) isEvent() {}

// TextEditingHandler is implemented by anything that can hold a
// composition — a text field, a terminal — and by the containers that
// route to them. The two roles share one interface for the same reason
// HandleKeyPress does: a container forwards without knowing whether the
// thing it forwards to is a leaf or another forwarder.
//
// Returning false means "not mine", and the caller may try elsewhere. A
// read-only or disabled field returns false rather than swallowing the
// composition silently.
type TextEditingHandler interface {
	HandleTextEditing(TextEditingEvent) bool
}

// Preedit is a composition in the form a trinket paints: runes, the
// input method's caret within them, and the active clause if there is
// one.
type Preedit struct {
	// Text is the whole composition. Empty means no composition.
	Text []rune

	// Covers is how many committed runes before the composition it stands
	// over and hides. See TextEditingEvent.Covers.
	Covers int

	// Caret is where the input method's own cursor sits, as a rune index
	// into Text. Always in [0, len(Text)].
	Caret int

	// ClauseStart and ClauseLen mark the segment being converted, as
	// rune indices into Text. ClauseLen is 0 when the input method
	// draws no distinction — a plain romaji run before conversion
	// starts, or a backend that never reports one.
	ClauseStart, ClauseLen int
}

// Active reports whether anything is being composed.
func (p Preedit) Active() bool { return len(p.Text) > 0 }

// PreeditFrom normalizes one TextEditingEvent. This is the ONLY place
// that interprets Start/Length, so the ambiguity in what an input method
// means by them is reasoned about once:
//
//   - Length > 0 means a clause is selected. The caret goes at its end,
//     which is where the next keystroke extends.
//   - Length <= 0 with a usable Start means Start IS the caret and there
//     is no clause.
//   - Anything else puts the caret at the end of the composition, which
//     is where it sits for the majority of input methods that never
//     report a position at all.
//
// Every index is clamped into the composition, so a backend that counts
// in bytes (or in nothing at all) produces a wrong-looking clause rather
// than a panic.
func PreeditFrom(ev TextEditingEvent) Preedit {
	runes := []rune(ev.Text)
	n := len(runes)
	p := Preedit{Text: runes, Caret: n, Covers: ev.Covers}
	if p.Covers < 0 {
		p.Covers = 0
	}
	if n == 0 {
		return Preedit{}
	}

	clamp := func(i int) int {
		if i < 0 {
			return 0
		}
		if i > n {
			return n
		}
		return i
	}

	switch {
	case ev.Length > 0 && ev.Start >= 0:
		p.ClauseStart = clamp(ev.Start)
		end := clamp(ev.Start + ev.Length)
		p.ClauseLen = end - p.ClauseStart
		p.Caret = end
	case ev.Start >= 0:
		p.Caret = clamp(ev.Start)
	}
	return p
}
