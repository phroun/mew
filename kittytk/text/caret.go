package text

import (
	"golang.org/x/image/math/fixed"

	"github.com/phroun/kittytk/core"
)

// The cluster-map operations (D6): caret placement and hit-testing
// expressed against shaped clusters, never per-rune arithmetic, so
// they stay correct inside ligatures and in RTL runs.

// cluster is a group of glyphs sharing one ClusterIndex: the atomic
// unit of caret movement. Runes covers its logical text range; width
// is its total advance.
type cluster struct {
	runes RuneRange
	width fixed.Int26_6
}

// clusters groups the run's glyphs in visual (left-to-right) order.
func (r *Run) clusters() []cluster {
	var out []cluster
	glyphs := r.raw.Glyphs
	for i := 0; i < len(glyphs); {
		ci := glyphs[i].ClusterIndex
		w := fixed.Int26_6(0)
		j := i
		for ; j < len(glyphs) && glyphs[j].ClusterIndex == ci; j++ {
			w += glyphs[j].Advance
		}
		out = append(out, cluster{runes: RuneRange{Start: ci}, width: w})
		i = j
	}
	// Fill in each cluster's logical end from its neighbors: visual
	// order is logical order for LTR, reversed for RTL.
	for i := range out {
		if r.RTL {
			if i == 0 {
				out[i].runes.End = r.Runes.End
			} else {
				out[i].runes.End = out[i-1].runes.Start
			}
		} else {
			if i == len(out)-1 {
				out[i].runes.End = r.Runes.End
			} else {
				out[i].runes.End = out[i+1].runes.Start
			}
		}
	}
	return out
}

// CaretX returns the x position (from the line's left edge) of the
// caret sitting before logical rune index idx. idx may equal the
// line's end index (caret after the last rune). Positions inside a
// cluster (e.g. inside a ligature) snap to the cluster start.
func (l *Line) CaretX(idx int) core.Unit {
	if len(l.Runs) == 0 {
		return 0
	}
	run := l.runFor(idx)
	if run == nil {
		// idx outside the line: clamp to the nearest edge.
		if idx <= l.Runes.Start {
			return l.edgeX(l.Runes.Start)
		}
		return l.edgeX(l.Runes.End)
	}
	x := fixed.I(int(run.X))
	if run.RTL {
		// Logical text flows right-to-left: the caret before idx sits
		// left of every cluster that starts at or after idx.
		for _, c := range run.clusters() {
			if c.runes.Start >= idx {
				x += c.width
			}
		}
	} else {
		for _, c := range run.clusters() {
			if c.runes.End <= idx {
				x += c.width
			}
		}
	}
	return core.Unit(x.Round())
}

// runFor picks the run owning caret index idx: the run containing it,
// or the one ending exactly at it.
func (l *Line) runFor(idx int) *Run {
	var atEnd *Run
	for i := range l.Runs {
		r := &l.Runs[i]
		if idx >= r.Runes.Start && idx < r.Runes.End {
			return r
		}
		if idx == r.Runes.End {
			atEnd = r
		}
	}
	return atEnd
}

// edgeX returns the caret x for one of the line's logical edges.
func (l *Line) edgeX(idx int) core.Unit {
	for i := range l.Runs {
		r := &l.Runs[i]
		if r.Runes.Start == idx && !r.RTL {
			return r.X
		}
		if r.Runes.End == idx && r.RTL {
			return r.X
		}
		if r.Runes.End == idx && !r.RTL {
			return r.X + r.Width
		}
		if r.Runes.Start == idx && r.RTL {
			return r.X + r.Width
		}
	}
	return 0
}

// RuneForX returns the logical rune index whose caret position is
// nearest to x (from the line's left edge). Hits inside a cluster
// resolve to the nearer cluster boundary.
func (l *Line) RuneForX(x core.Unit) int {
	if len(l.Runs) == 0 {
		return l.Runes.Start
	}
	fx := fixed.I(int(x))
	first := &l.Runs[0]
	if fx <= fixed.I(int(first.X)) {
		// Left of everything: the leftmost caret boundary.
		if first.RTL {
			return first.Runes.End
		}
		return first.Runes.Start
	}
	for i := range l.Runs {
		r := &l.Runs[i]
		left := fixed.I(int(r.X))
		pen := left
		if fx < left {
			continue
		}
		for _, c := range r.clusters() {
			if fx < pen+c.width {
				// Inside this cluster: nearer boundary wins.
				leftIdx, rightIdx := c.runes.Start, c.runes.End
				if r.RTL {
					leftIdx, rightIdx = c.runes.End, c.runes.Start
				}
				if fx-pen < pen+c.width-fx {
					return leftIdx
				}
				return rightIdx
			}
			pen += c.width
		}
	}
	// Right of everything: the rightmost caret boundary.
	last := &l.Runs[len(l.Runs)-1]
	if last.RTL {
		return last.Runes.Start
	}
	return last.Runes.End
}
