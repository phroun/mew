package core

// Direction is the side text begins on: the left for Latin, Cyrillic, CJK and
// most of the world's scripts, the right for Hebrew, Arabic, Syriac, Thaana,
// N'Ko and Adlam.
//
// It is what the logical alignments are stated against. "Begin" and "end" are
// sides of a box that swap with the direction in force; left and right never
// move.
type Direction int

const (
	// DirInherit takes the direction from the nearest ancestor that names one.
	// It is the zero value, so a trinket that says nothing inherits, and a
	// string with no strongly directional character in it reports this rather
	// than guessing.
	DirInherit Direction = iota
	// DirLTR begins on the left.
	DirLTR
	// DirRTL begins on the right.
	DirRTL
)

// String names the direction for diagnostics and the wire vocabulary.
func (d Direction) String() string {
	switch d {
	case DirLTR:
		return "ltr"
	case DirRTL:
		return "rtl"
	}
	return "inherit"
}

// DirectionProvider is implemented by trinkets that can name a direction for
// themselves and everything below them. Every trinket embedding TrinketBase
// does; the walk reads the value, not the interface.
type DirectionProvider interface {
	// Direction returns the direction set on this provider, or DirInherit to
	// take it from the parent chain.
	Direction() Direction
}

// DirectionObserver is implemented by a trinket that DERIVES something from
// the direction in force and holds on to the answer, rather than asking again
// each time it needs it. Everything a trinket resolves on demand -- where its
// chrome goes, which way a run travels -- needs none of this; what needs it is
// state settled once and kept, and the standing example is the set of commands
// a trinket declares it can carry out, which is read at the moment a key is
// resolved and cannot be re-derived from the keystroke.
//
// DirectionChanged says the direction this trinket inherits may now be a
// different one. Anything derived from it is stale.
type DirectionObserver interface {
	DirectionChanged()
}

// NotifyDirectionChanged tells w and the subtree under it that the direction
// they inherit has changed.
//
// The walk stops at any DESCENDANT that names a direction of its own: it and
// everything below it read exactly what they read before, so there is nothing
// there to be stale. w itself is always told, because it is where the change
// happened.
func NotifyDirectionChanged(w Trinket) {
	if w == nil {
		return
	}
	if o, ok := w.(DirectionObserver); ok {
		o.DirectionChanged()
	}
	c, ok := w.(Container)
	if !ok {
		return
	}
	for _, kid := range c.Children() {
		if dp, ok := kid.(DirectionProvider); ok && dp.Direction() != DirInherit {
			continue
		}
		NotifyDirectionChanged(kid)
	}
}

// FindEffectiveDirection walks up the trinket tree to the first ancestor that
// names a direction, mirroring FindEffectiveFont and FindEffectiveCellMetrics.
// It checks the trinket, then its ancestors (window, MDI pane, desktop).
// Returns DirLTR when nothing in the chain names one.
func FindEffectiveDirection(w Trinket) Direction {
	if w == nil {
		return DirLTR
	}

	if dp, ok := w.(DirectionProvider); ok {
		if d := dp.Direction(); d != DirInherit {
			return d
		}
	}

	current := w.Parent()
	for current != nil {
		if dp, ok := current.(DirectionProvider); ok {
			if d := dp.Direction(); d != DirInherit {
				return d
			}
		}
		if trinket, ok := current.(Trinket); ok {
			current = trinket.Parent()
		} else {
			break
		}
	}

	return DirLTR
}

// TextDirectioner is an optional capability: a trinket carrying text says
// which way that text runs.
//
// The second result is whether it has an opinion. A caption of digits, of
// punctuation, or of nothing at all has no strongly directional character in
// it and so names no direction -- which is the common case, not a rare one --
// and a trinket may also decline outright. Either way the layout falls back to
// the direction in force around it, so textnatural lands where layoutnatural does.
type TextDirectioner interface {
	TextDirection() (Direction, bool)
}

// FindTextDirection is the direction to state a trinket's own text against:
// what the trinket reports about its text, and the direction in force around
// it when the trinket reports nothing.
func FindTextDirection(w Trinket) Direction {
	if w == nil {
		return DirLTR
	}
	if td, ok := w.(TextDirectioner); ok {
		if d, has := td.TextDirection(); has && d != DirInherit {
			return d
		}
	}
	return FindEffectiveDirection(w)
}

// ChromeMirrored reports whether a trinket lays out its OWN chrome right to
// left: a checkbox's indicator, a combobox's arrow, a scroll area's bars.
//
// The direction in force around the trinket, not the direction of any text it
// carries. A Hebrew caption in an English form is still a control in an English
// form and keeps its box on the left; what turns chrome over is the room.
func ChromeMirrored(w Trinket) bool {
	return FindEffectiveDirection(w) == DirRTL
}

// LeadingX is where a piece of a trinket's chrome goes.
//
// A trinket measures what it paints from its LEADING edge -- the side the
// direction reads from -- and this turns that measurement into the x a painter
// wants: itself where the direction reads left to right, and reflected in the
// box where it reads right to left. `box` is the width being placed in, `at`
// how far in from the leading edge the piece begins, and `width` its own.
//
// It is the same reflection the layout managers apply to a run, spelled for one
// trinket placing something inside itself. Applying it to each piece in turn
// reverses their order without any of them being written twice.
func LeadingX(w Trinket, box, at, width Unit) Unit {
	if !ChromeMirrored(w) {
		return at
	}
	return box - at - width
}
