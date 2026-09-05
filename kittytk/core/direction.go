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
// the direction in force around it, so textbegin lands where layoutbegin does.
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
