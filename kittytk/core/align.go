package core

// HAlign is where something sits horizontally in the space a layout gives it.
//
// Four of the seven are stated against a direction and swap sides with it; the
// two optical ones never move, and are spelled at length so that pinning a
// side reads as a decision rather than an oversight.
type HAlign int

const (
	// AlignTextBegin is the side this trinket's OWN text begins on -- left for
	// an English caption, right for a Hebrew one. A trinket whose text names
	// no direction, and one that carries no text at all, falls back to the
	// direction in force around it, so this lands where AlignLayoutBegin does.
	AlignTextBegin HAlign = iota
	// AlignTextEnd is the opposite side from where the trinket's text begins.
	AlignTextEnd
	// AlignLayoutBegin is the side the surrounding direction begins on,
	// whatever the trinket's own text says.
	AlignLayoutBegin
	// AlignLayoutEnd is the opposite side from that.
	AlignLayoutEnd
	// AlignCenter is the middle, which no direction moves.
	AlignCenter
	// AlignOpticalLeft is the left, whatever any direction says.
	AlignOpticalLeft
	// AlignOpticalRight is the right, whatever any direction says.
	AlignOpticalRight
)

// VAlign is where something sits vertically in the space a layout gives it.
// Nothing here turns over: our rows run top to bottom in every direction.
type VAlign int

const (
	AlignTop VAlign = iota
	AlignMiddle
	AlignBottom
)

// HSide is a horizontal placement with the direction already spent: what is
// left once a logical alignment has been resolved.
//
// Backends take this and never an HAlign. A backend has no trinket tree to
// walk, so it cannot resolve a direction, and the type says so.
type HSide int

const (
	SideLeft HSide = iota
	SideCenter
	SideRight
)

// Alignment places a layout item in the space it is given: where it sits on
// each axis, and whether it grows to the allocation on that axis.
//
// The two are separate because an item that fills still needs somewhere to sit
// when there turns out to be nothing to fill -- a fixed-height field in a
// three-row row, or one held to a ceiling. Filling is what it does with the
// room; the alignment is where it goes without it.
//
// A box consults only the pair for its CROSS axis; what an item gets along the
// axis comes from the sizing pass and its stretch. A grid consults both.
type Alignment struct {
	H     HAlign
	V     VAlign
	FillH bool
	FillV bool
}

// DefaultAlignment fills both axes and centres on either one that turns out to
// have nothing to fill.
func DefaultAlignment() Alignment {
	return Alignment{H: AlignCenter, V: AlignMiddle, FillH: true, FillV: true}
}

// ResolveHAlign spends the directions and returns the side named.
//
// textDir is what the trinket's own text runs as (FindTextDirection), and
// layoutDir is the direction in force where it is being placed -- its
// CONTAINER's, not its own: a right-to-left panel sitting in a left-to-right
// row is placed against the row.
//
// DirInherit in either position means the question was handed on rather than
// answered: an unresolved text direction falls back to the layout's, and an
// unresolved layout direction to left-to-right.
func ResolveHAlign(a HAlign, textDir, layoutDir Direction) HSide {
	if layoutDir == DirInherit {
		layoutDir = DirLTR
	}
	if textDir == DirInherit {
		textDir = layoutDir
	}

	begin := func(d Direction) HSide {
		if d == DirRTL {
			return SideRight
		}
		return SideLeft
	}
	end := func(d Direction) HSide {
		if d == DirRTL {
			return SideLeft
		}
		return SideRight
	}

	switch a {
	case AlignTextBegin:
		return begin(textDir)
	case AlignTextEnd:
		return end(textDir)
	case AlignLayoutBegin:
		return begin(layoutDir)
	case AlignLayoutEnd:
		return end(layoutDir)
	case AlignOpticalLeft:
		return SideLeft
	case AlignOpticalRight:
		return SideRight
	}
	return SideCenter
}

// ResolveHAlignFor is ResolveHAlign with the directions read off the tree: the
// trinket's own text, and the direction in force around its container.
//
// A nil container means the trinket is placing something within ITSELF -- a
// label aligning its caption in its own box -- and the direction in force is
// then the trinket's own.
func ResolveHAlignFor(a HAlign, w Trinket, container Trinket) HSide {
	var layoutDir Direction
	if container != nil {
		layoutDir = FindEffectiveDirection(container)
	} else if w != nil {
		layoutDir = FindEffectiveDirection(w)
	}
	return ResolveHAlign(a, FindTextDirection(w), layoutDir)
}
