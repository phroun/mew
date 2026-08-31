package trinkets

// defaultSizeCells is what a trinket asks for when nothing set a size: three
// cells of the surface it is painted on, so the fallback is stated in that
// surface's own denomination -- CellWidth and CellHeight are the units per
// cell, so three cells is 3*CellWidth units across and 3*CellHeight down.
//
// Nothing derives these sizes. They are the answer to "how big do you want to
// be" when nobody said, and the only thing that answer has to be is big
// enough to see and to grab. A larger number would be an arbitrary one
// pretending to be a measurement, which is what "thirty characters wide" was.
const defaultSizeCells = 3
