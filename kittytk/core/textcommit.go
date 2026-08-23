package core

// TextCommitEvent is text an input method has FINISHED composing: the final
// form, to be taken into the document.
//
// It is not a keystroke and carries no key name. A press that types is still a
// press — the key is what happened and the character is what it made — so
// ordinary typing does not come this way. Here there is no key: the character
// was chosen from a palette or a candidate list, and the keystroke that chose
// it (a digit, an arrow, Return) belongs to the input method rather than to the
// document.
//
// It pairs with TextEditingEvent, which carries the same composition while it
// is still provisional. A sink that paints a preedit ends it on this event; a
// sink that paints none sees only this one.
//
// It carries no extent either. A commit replaces whatever its composition
// COVERED (see TextEditingEvent.Covers), which the sink has been painting all
// along — so the region is already known where it is needed, and does not have
// to be worked out again and sent a second time.
//
// Nothing about a commit belongs in the key-chord text memo. There is no chord
// to record it against — holding "o" and picking "ò" from the palette must
// never teach anything that the "o" chord types "ò".
type TextCommitEvent struct {
	// Text is the finished composition.
	Text string
}

func (TextCommitEvent) isEvent() {}

// TextCommitHandler is implemented by anything that can take a finished
// composition — a text field, an editor, and the containers that route to
// them.
//
// Kept apart from TextEditingHandler so a sink can accept commits, and so get
// a replacement right, before it can paint a preedit. The two are different
// amounts of work and a sink may reasonably do only the first.
//
// Returning false means "not mine", as everywhere else, and a commit nobody
// claims is delivered as ordinary typed text instead — which is what every
// sink saw before this event existed, minus the replacement.
type TextCommitHandler interface {
	HandleTextCommit(TextCommitEvent) bool
}
