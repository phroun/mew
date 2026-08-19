package core

// What a chord TYPED, as watched rather than as tabulated.
//
// A key that produces text produces it through the host's own layout and
// composition rules, and the host is handed both halves: the key with its
// modifiers, which names the chord, and the text that came out. The name is
// the authoritative half — it is what a binding is written against — and the
// text is remembered against it here.
//
// macOS Option is where this matters first, because there the two halves are
// most obviously different things: Option+a is the chord M-a and the character
// "å". A table of those characters can be written down, and both the key layer
// and this toolkit carry one for the hosts that are handed only one half. But a
// table is one keyboard from memory, and this is the machine being typed on, so
// where an observation exists it is the better answer.

// KeyChordTextSource is an optional host capability: the text this host has
// watched its own keyboard produce, by the chord that produced it.
//
// The entry is written afresh every time, so a chord whose text changes
// mid-session reports the new text from then on.
type KeyChordTextSource interface {
	// KeyChordText returns the text observed for a chord ("M-a"), and whether
	// any has been observed.
	KeyChordText(chord string) (string, bool)

	// AllKeyChordText returns every observation, for a host that wants to show
	// or record what this keyboard does.
	AllKeyChordText() map[string]string
}
