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
	for _, item := range l.shown() {
		if item.ColumnSpan > 1 {
			out = append(out, span{item.Column, item.ColumnSpan, size(item.Trinket)})
		}
	}
	return out
}

// rowSpans is columnSpans down the other axis.
func (l *GridLayout) rowSpans(size func(core.Trinket) core.Unit) []span {
	var out []span
	for _, item := range l.shown() {
		if item.RowSpan > 1 {
			out = append(out, span{item.Row, item.RowSpan, size(item.Trinket)})
		}
	}
	return out
}

// rowGaps is what each boundary between rows costs. Side-bearings are
// horizontal, so nothing collapses down the page and every boundary is the
// configured spacing -- which is what Layout puts between rows -- except the
// one an empty row would have had, which it gives up (see liveTracks).
func (l *GridLayout) rowGaps(rows int, q core.Unit) []core.Unit {
	if rows < 2 {
		return nil
	}
	gaps := make([]core.Unit, rows-1)
	seen := false
	for r, live := range liveTracks(rows, l.rows, l.rowsStoodIn()) {
		if !live {
			continue
		}
		if seen {
			gaps[r-1] = l.cellSpacing(q)
		}
		seen = true
	}
	return gaps
}

// columnsStoodIn and rowsStoodIn are where every shown child stands on one
// axis, one cell or many -- the tracks liveTracks keeps for what is in them.
func (l *GridLayout) columnsStoodIn() []span {
	var out []span
	for _, item := range l.shown() {
		out = append(out, span{start: item.Column, count: item.ColumnSpan})
	}
	return out
}

// rowsStoodIn is columnsStoodIn down the other axis.
func (l *GridLayout) rowsStoodIn() []span {
	var out []span
	for _, item := range l.shown() {
		out = append(out, span{start: item.Row, count: item.RowSpan})
	}
	return out
}

// liveTracks says which of n tracks on one axis take part in the arrangement.
//
// A track nothing shown stands in -- a row whose only child is hidden, or one
// no child was ever put in -- is taken out altogether: it is no size already,
// having nothing to measure, and it gives up the boundary beside it too, so
// hiding a row of a form closes the form up rather than leaving a doubled gap
// where the row was. A child spanning across a track stands in it.
//
// A band that declares a Minimum or a Stretch keeps its track whatever is in
// it. Those are statements about the track itself rather than about what it
// holds, and they are how a grid asks for an empty row on purpose; the other
// way is to put a spacer in it.
func liveTracks(n int, bands []Band, claims []span) []bool {
	live := make([]bool, n)
	for t := range live {
		b := bandAt(bands, t)
		live[t] = b.Minimum > 0 || b.Stretch > 0
	}
	for _, c := range claims {
		for t := c.start; t < c.start+c.count && t < n; t++ {
			if t >= 0 {
				live[t] = true
			}
		}
	}
	return live
}
