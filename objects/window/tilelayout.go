package window

import (
	"sort"

	"github.com/phroun/kittytk/core"
)

// TileItem describes one window for TileLayout.
type TileItem struct {
	// Resizable is true when the window may be resized to fill its cell. A
	// non-resizable window keeps its own Size and is only positioned.
	Resizable bool
	// Size is the window's current size, used to choose a fitting cell for a
	// non-resizable window (ignored when Resizable).
	Size core.UnitSize
	// First pins this window to the upper-left cell (the app's main window in
	// a torn-off arrangement). At most one item should set it.
	First bool
}

// TileLayout arranges len(items) windows into a balanced grid inside area
// and returns, in the same order as items, the cell rectangle each window
// should occupy.
//
// Rows fill from the bottom: when an even split leaves a remainder, the
// extra windows go to the LOWER rows, so an incomplete row lands at the
// bottom (fuller) rather than the top - e.g. 7 windows tile as rows of
// 2, 2, 3 instead of 3, 3, 1. Every row divides the full width by its own
// window count, so a row of two splits in half and a row of three into
// thirds.
//
// Non-resizable windows are placed by fit: a fixed window too big for the
// narrowest cells claims a larger cell first, a fixed window small enough
// for the narrowest cell is placed last (leaving the roomier cells for
// resizable or larger windows), and resizable windows take what remains.
// The First window always gets the top-left cell regardless of fit.
func TileLayout(area core.UnitRect, items []TileItem) []core.UnitRect {
	n := len(items)
	out := make([]core.UnitRect, n)
	if n == 0 {
		return out
	}

	// Grid dimensions: cols = ceil(sqrt(n)), rows = ceil(n/cols).
	cols := 1
	for cols*cols < n {
		cols++
	}
	rows := (n + cols - 1) / cols

	// Per-row counts, remainder distributed to the bottom rows.
	base, rem := n/rows, n%rows
	counts := make([]int, rows)
	for r := 0; r < rows; r++ {
		counts[r] = base
		if r >= rows-rem {
			counts[r]++
		}
	}

	// Build the cell rectangles in row-major order (cells[0] is top-left).
	// Boundaries are proportional so rounding fills the whole area with no
	// seams or overhang.
	cells := make([]core.UnitRect, 0, n)
	for r := 0; r < rows; r++ {
		top := area.Y + core.Unit(r)*area.Height/core.Unit(rows)
		bot := area.Y + core.Unit(r+1)*area.Height/core.Unit(rows)
		c := counts[r]
		for j := 0; j < c; j++ {
			left := area.X + core.Unit(j)*area.Width/core.Unit(c)
			right := area.X + core.Unit(j+1)*area.Width/core.Unit(c)
			cells = append(cells, core.UnitRect{X: left, Y: top, Width: right - left, Height: bot - top})
		}
	}

	assigned := make([]int, n) // item index -> cell index
	for i := range assigned {
		assigned[i] = -1
	}
	used := make([]bool, len(cells))

	// The First window takes the top-left cell.
	firstItem := -1
	for i, it := range items {
		if it.First {
			firstItem = i
			break
		}
	}
	if firstItem >= 0 {
		assigned[firstItem] = 0
		used[0] = true
	}

	// Remaining cells, largest area first.
	remCells := make([]int, 0, len(cells))
	for ci := range cells {
		if !used[ci] {
			remCells = append(remCells, ci)
		}
	}
	sort.SliceStable(remCells, func(a, b int) bool {
		return cellArea(cells[remCells[a]]) > cellArea(cells[remCells[b]])
	})

	// Narrowest remaining cell, to decide which fixed windows fit anywhere.
	minW, minH := core.Unit(1)<<30, core.Unit(1)<<30
	for _, ci := range remCells {
		if cells[ci].Width < minW {
			minW = cells[ci].Width
		}
		if cells[ci].Height < minH {
			minH = cells[ci].Height
		}
	}

	// Priority order: fixed-and-large first (they need the roomy cells), then
	// resizable, then fixed-and-small (they fit the leftover narrow cells).
	var bigFixed, resizable, smallFixed []int
	for i, it := range items {
		if it.First {
			continue
		}
		switch {
		case it.Resizable:
			resizable = append(resizable, i)
		case it.Size.Width <= minW && it.Size.Height <= minH:
			smallFixed = append(smallFixed, i)
		default:
			bigFixed = append(bigFixed, i)
		}
	}
	sort.SliceStable(bigFixed, func(a, b int) bool {
		return sizeArea(items[bigFixed[a]].Size) > sizeArea(items[bigFixed[b]].Size)
	})

	order := make([]int, 0, n)
	order = append(order, bigFixed...)
	order = append(order, resizable...)
	order = append(order, smallFixed...)

	// Rank-match: the k-th most-constrained window gets the k-th largest cell.
	for k, item := range order {
		if k < len(remCells) {
			assigned[item] = remCells[k]
		} else {
			assigned[item] = remCells[len(remCells)-1]
		}
	}

	for i := range items {
		ci := assigned[i]
		if ci < 0 {
			ci = 0
		}
		out[i] = cells[ci]
	}
	return out
}

func cellArea(r core.UnitRect) int64 { return int64(r.Width) * int64(r.Height) }
func sizeArea(s core.UnitSize) int64 { return int64(s.Width) * int64(s.Height) }
