package layout

import "github.com/phroun/kittytk/core"

// A BAND is one track of a grid: a column, or a row. It is the same idea on
// either axis -- a line the children of a grid start and end on -- so it is
// one type, and which axis a band belongs to is decided by the list it is put
// in rather than by anything it says about itself.
//
// Bands are given in order and take their index from their position, so a
// form never writes a track number down. A band that names itself can be
// placed into by that name, which survives an insertion ahead of it that a
// number would not.
type Band struct {
	// ID is the name children place themselves by (column="fields"). Blank
	// leaves the band reachable only by its position.
	ID string

	// Stretch is this band's share of what is left once every band has its
	// minimum. Zero takes none of it.
	Stretch int

	// Minimum is the floor, in units, that the band holds whatever its
	// children ask for.
	Minimum core.Unit
}

// bandAt returns the band at index, or a zero band where the grid has fewer
// bands than it has tracks -- a grid is as wide as its children make it, and
// bands describe only as far as they were given.
func bandAt(bands []Band, index int) Band {
	if index < 0 || index >= len(bands) {
		return Band{}
	}
	return bands[index]
}

// growBands extends bands so index is addressable, and returns the new list.
func growBands(bands []Band, index int) []Band {
	for len(bands) <= index {
		bands = append(bands, Band{})
	}
	return bands
}

// bandIndex finds the band called id, or -1. A blank id names nothing: a band
// that did not introduce itself cannot be asked for.
func bandIndex(bands []Band, id string) int {
	if id == "" {
		return -1
	}
	for i := range bands {
		if bands[i].ID == id {
			return i
		}
	}
	return -1
}
