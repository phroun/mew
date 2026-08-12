package trinkets

// acceleratorAssignment is the outcome for one item: which letter to underline,
// where, and whether that letter currently reaches anything.
type acceleratorAssignment struct {
	Char rune // the letter to underline, 0 for none at all
	Pos  int  // its index in the display text, -1 for none
	// Active is true when the letter reaches this item right now, and the
	// accelerator is drawn in the accelerator colour. False means the letter
	// is this item's, but something else in the current context has claimed
	// the chord it would form: drawn in the ordinary text colour, underline
	// kept, so the user can see whose it is and that it is not answering.
	Active bool
}

// assignAccelerators hands out the accelerator letters for one level — the
// menus on a bar, or the items in a single dropdown — reading each title's
// candidates in the order they were written.
//
// Assignment is greedy and left to right. Each item takes the first letter
// that is free on BOTH counts: not already taken by an earlier sibling, and
// not already claimed in the current context. That second test is what makes a
// backup letter worth writing — "&Hel&p" falls to p when something else owns
// the chord h would form, and Help stays reachable.
//
// When no candidate is free on both counts the item still takes the first one
// free of siblings, and shows it muted: the letter is genuinely this item's and
// will start answering again when whatever claimed the chord goes away. Only an
// item whose every candidate was consumed by an earlier sibling gets nothing —
// no underline at all, since the letter is not its to advertise and a sibling
// is displaying that same letter, lit, on the same level.
//
// A displayed letter is claimed either way, muted or not, so the display is
// stable: an item does not lose its letter to a later sibling just because the
// chord is temporarily spoken for.
//
// clashes reports whether the chord formed from a letter is already claimed in
// the current context. A nil clashes means nothing is claimed.
func assignAccelerators(siblings [][]acceleratorCandidate, clashes func(rune) bool) []acceleratorAssignment {
	if clashes == nil {
		clashes = func(rune) bool { return false }
	}
	taken := make(map[rune]bool, len(siblings))
	out := make([]acceleratorAssignment, len(siblings))

	for i, cands := range siblings {
		out[i] = acceleratorAssignment{Pos: -1}

		// First choice: free of siblings and free of the context.
		chosen := -1
		for j, c := range cands {
			if !taken[c.Char] && !clashes(c.Char) {
				chosen, out[i].Active = j, true
				break
			}
		}
		// Otherwise the first letter no sibling has taken, shown muted.
		if chosen < 0 {
			for j, c := range cands {
				if !taken[c.Char] {
					chosen = j
					break
				}
			}
		}
		if chosen < 0 {
			continue // every candidate belongs to an earlier sibling
		}
		out[i].Char, out[i].Pos = cands[chosen].Char, cands[chosen].Pos
		taken[out[i].Char] = true
	}
	return out
}
