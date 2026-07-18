package editor

import (
	"strconv"
	"strings"
)

// pageSizeSpec decides how far go_page_prior/go_page_next move, built from the
// three [general] options: pageSizeOptimal (the desired distance), overlap
// (context lines to keep between pages), and step (round the distance down to a
// multiple of this). Optimal and overlap are each a fixed count ("24") or a
// percentage of the view height ("50%").
type pageSizeSpec struct {
	hasPct bool // optimal is a percentage
	pct    int
	fixed  int // optimal as a fixed count (when !hasPct)

	overlapPct    int
	overlapHasPct bool
	overlapFixed  int // overlap as a fixed count (when !overlapHasPct)

	step int // 0/1: none; else round the distance down to a multiple
}

// defaultPageSizeSpec is a full page (100%) with one overlap line and no step.
func defaultPageSizeSpec() pageSizeSpec {
	return pageSizeSpec{hasPct: true, pct: 100, overlapFixed: 1}
}

// parseCountOrPercent parses a fixed count ("24") or a percentage of the view
// height ("50%"). Used for both pageSizeOptimal and pageOverlapMinimum.
func parseCountOrPercent(s string) (hasPct bool, pct, fixed int, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return false, 0, 0, false
	}
	if k := strings.IndexByte(s, '%'); k >= 0 {
		if strings.TrimSpace(s[k+1:]) != "" { // nothing may follow the '%'
			return false, 0, 0, false
		}
		p, err := strconv.Atoi(strings.TrimSpace(s[:k]))
		if err != nil || p < 0 {
			return false, 0, 0, false
		}
		return true, p, 0, true
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return false, 0, 0, false
	}
	return false, 0, v, true
}

// buildPageSizeSpec assembles the spec from the three option values, falling
// back to the default for any that is malformed.
func buildPageSizeSpec(optimal, overlap string, step int) pageSizeSpec {
	spec := defaultPageSizeSpec()
	if hasPct, pct, fixed, ok := parseCountOrPercent(optimal); ok {
		spec.hasPct, spec.pct, spec.fixed = hasPct, pct, fixed
	}
	if hasPct, pct, fixed, ok := parseCountOrPercent(overlap); ok {
		spec.overlapHasPct, spec.overlapPct, spec.overlapFixed = hasPct, pct, fixed
	}
	if step >= 0 {
		spec.step = step
	}
	return spec
}

// eval resolves the spec against a view height into a concrete page distance.
// Order: the optimal distance (percentage floored); then cap so at least
// `overlap` lines of context remain — a percentage overlap is CEILed so a
// non-zero percentage never rounds away to zero on a short screen (only a
// literal 0 gives no overlap), and the cap floors at 1 so a window too short
// to honor the overlap still moves one line; then round down to the step
// multiple, unless that would zero it out; finally floor at 1.
func (s pageSizeSpec) eval(height int) int {
	n := s.fixed
	if s.hasPct {
		n = s.pct * height / 100
	}

	overlap := s.overlapFixed
	if s.overlapHasPct {
		// Ceil: (pct*height)/100 rounded up, so e.g. 10% of a 15-row view is 2,
		// and any non-zero percent of a tiny view is at least 1.
		overlap = (s.overlapPct*height + 99) / 100
	}

	cap := height - overlap
	if cap < 1 {
		cap = 1
	}
	if n > cap {
		n = cap
	}

	if s.step > 1 {
		if rounded := (n / s.step) * s.step; rounded >= 1 {
			n = rounded
		}
	}

	if n < 1 {
		n = 1
	}
	return n
}
