package trinkets

// The size a trinket asks for when nothing set one.
//
// The size is a count of denomination units. These are written as counts of
// CELLS -- so many * CellWidth across, so many * CellHeight down -- only
// because a default cannot be derived any other way: the units are the
// surface's, and nothing here can know what a designer would have specified
// in them.
//
// They are deliberately too small to use. A trinket laid out at one of these
// is a designer being told they forgot to give it a size.
const (
	// defaultSizeCells is the base: a trinket with nothing to derive a size
	// from asks for this many cells on each axis it cannot answer for.
	defaultSizeCells = 3

	// defaultTreeWidthCells is twice the base. A tree draws its expand and
	// collapse hardware beside the text, so it needs the extra room to be
	// even this legible.
	defaultTreeWidthCells = defaultSizeCells * 2

	// defaultTabsWidthCells is what a tab strip asks for.
	defaultTabsWidthCells = 12

	// defaultContainerHeightCells is what a tab strip, a scroll area and a
	// panel ask for down the page.
	defaultContainerHeightCells = 5
)
