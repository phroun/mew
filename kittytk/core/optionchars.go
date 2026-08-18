package core

// What the keyboard COMPOSED for a chord, as watched rather than as tabulated.
//
// On macOS the Option cap composes: Option+a is both the chord M-a and the
// character "å", and the window system hands over both — the key with its
// modifier, and the text it produced. The chord is named from the key, which is
// the authoritative half; the character is remembered against it here.
//
// A table of these characters can be written down, and both the key layer and
// this toolkit carry one for the hosts that never see the composition. But a
// table is one keyboard from memory, and this is the machine that is actually
// being typed on — so where an observation exists it is the better answer, and
// an application asks for it before falling back.

// OptionCharSource is an optional host capability: the characters this host has
// watched its own keyboard compose, by the chord that composed them.
//
// The map is written afresh on every keystroke that composes, so a chord whose
// character changes mid-session reports the new one from then on.
type OptionCharSource interface {
	// OptionChar returns the character observed for a chord ("M-a"), and
	// whether one has been observed at all.
	OptionChar(chord string) (string, bool)

	// OptionChars returns every observation, for a host that wants to show or
	// record what this keyboard does.
	OptionChars() map[string]string
}
