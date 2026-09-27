package layout

import (
	"sort"

	"github.com/phroun/kittytk/core"
)

// A child that covers several tracks has a claim on how wide they are, and it
// is a claim on the RUN rather than on any one of them: what it needs is the
// tracks it covers plus the boundaries between them, taken together. A span
// that already fits asks for nothing.
//
// Which is why a span cannot simply raise each track it covers -- that would
// make two columns under a 120-wide span 120 each. It raises them together,
// and only by what is missing.

// span is one child's claim across the tracks it covers on one axis.
type span struct {
	start, count int
	size         core.Unit
}

// raiseForSpans raises tracks until every span fits across the ones it covers.
//
// Shortest first: a narrow span settles tracks that a wider one also covers,
// so the wider one then asks only for what is still missing -- and often for
// nothing. Going the other way inflates the narrow span's tracks twice over.
// Spans of equal reach keep the order they were given, so a grid measures the
// same on every pass.
func raiseForSpans(sizes []core.Unit, bands []Band, gaps []core.Unit, spans []span) {
	if len(spans) == 0 {
		return
	}
	ordered := make([]span, len(spans))
	copy(ordered, spans)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].count < ordered[j].count })

	for _, s := range ordered {
		if s.start < 0 || s.count < 1 || s.start+s.count > len(sizes) {
			continue
		}
		have := core.Unit(0)
		for i := s.start; i < s.start+s.count; i++ {
			have += sizes[i]
		}
		for i := s.start; i < s.start+s.count-1 && i < len(gaps); i++ {
			have += gaps[i]
		}
		if short := s.size - have; short > 0 {
			spreadShortfall(sizes, bands, s.start, s.count, short)
		}
	}
}

// spreadShortfall hands out what a span is missing across the tracks it
// covers: by stretch where any of them stretches, evenly where none does.
//
// Stretch first because a band that asked for none was pinned on purpose, and
// widening it behind the author's back is the thing they pinned it to prevent.
// Where nothing stretches there is no such statement to respect and the tracks
// share it.
func spreadShortfall(sizes []core.Unit, bands []Band, start, count int, short core.Unit) {
	totalStretch := 0
	for i := start; i < start+count; i++ {
		totalStretch += bandAt(bands, i).Stretch
	}

	given := core.Unit(0)
	if totalStretch > 0 {
		for i := start; i < start+count; i++ {
			st := bandAt(bands, i).Stretch
			if st == 0 {
				continue
			}
			portion := short * core.Unit(st) / core.Unit(totalStretch)
			sizes[i] += portion
			given += portion
		}
		// What integer division left over goes a unit at a time to the tracks
		// that took a share, so the span fits exactly rather than a unit short.
		for i := start; i < start+count && given < short; i++ {
			if bandAt(bands, i).Stretch > 0 {
				sizes[i]++
				given++
			}
		}
		return
	}

	each := short / core.Unit(count)
	for i := start; i < start+count; i++ {
		sizes[i] += each
		given += each
	}
	for i := start; i < start+count && given < short; i++ {
		sizes[i]++
		given++
	}
}

// columnSpans and rowSpans are the claims the grid's spanning children make on
// one axis. size reads the extent each pass is measuring -- a hint, a minimum,
// or the laid-out size -- so all three raise the same tracks by the same rule.
func (l *GridLayout) columnSpans(size func(core.Trinket) core.Unit) []span {
	var out []span
	for _, item := range l.items {
		if item.ColumnSpan > 1 {
			out = append(out, span{item.Column, item.ColumnSpan, size(item.Trinket)})
		}
	}
	return out
}

// rowSpans is columnSpans down the other axis.
func (l *GridLayout) rowSpans(size func(core.Trinket) core.Unit) []span {
	var out []span
	for _, item := range l.items {
		if item.RowSpan > 1 {
			out = append(out, span{item.Row, item.RowSpan, size(item.Trinket)})
		}
	}
	return out
}

// rowGaps is what each boundary between rows costs. Side-bearings are
// horizontal, so nothing collapses down the page and every boundary is the
// configured spacing -- which is what Layout puts between rows.
func (l *GridLayout) rowGaps(rows int, q core.Unit) []core.Unit {
	if rows < 2 {
		return nil
	}
	gaps := make([]core.Unit, rows-1)
	for i := range gaps {
		gaps[i] = l.cellSpacing(q)
	}
	return gaps
}
