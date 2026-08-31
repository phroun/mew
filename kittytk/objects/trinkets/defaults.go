package trinkets

// defaultSizeCells is the size a trinket asks for when nothing set one.
//
// The size is a count of denomination units. It is written here as a count of
// CELLS -- 3*CellWidth units across, 3*CellHeight down -- only because a
// default cannot be derived any other way: the units are the surface's, and
// nothing here can know what a designer would have specified in them.
//
// Three is deliberately too small to use. A trinket laid out this size is a
// designer being told they forgot to give it one.
const defaultSizeCells = 3
