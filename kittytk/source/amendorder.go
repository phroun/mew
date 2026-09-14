package source

// An amended source's own records, put in order once per data set.
//
// What this source holds does not depend on who is reading it or on where they
// have scrolled to: the sort and the filter decide which of its amendments are
// in the sequence and where each one stands, and those two are what name a data
// set. So the work is done once for that sequence and every scope of it is a
// binary search into the result, rather than a copy of the whole lot, a filter
// pass and a sort, per scope.
//
// **The direction is not in it.** One order is walked either way, the same way
// one prepared ordering serves a reversed scope -- which is why `reversed`
// belongs to the scope and not to the sequence, and why this is keyed by the
// same thing the orderings are.
//
// Amendments change at any time, though, which orderings of records do not. So
// each order carries the generation it was built at, and a source that has been
// amended since builds a new one. Ordering can be a hint; membership cannot --
// an amendment added since would simply not be in a stale list, and would never
// go out.

import (
	"sort"
	"sync"

	"github.com/phroun/kittytk/wire"
)

// An amendOrder is everything this source has to say about one sequence,
// arranged so that a scope of it costs a search rather than a walk.
type amendOrder struct {
	gen uint64

	// out is what goes out, in the sequence's own order, with where each one
	// stands beside it: the replacements and additions that match the filter
	// and have a placement.
	out   []*amendment
	outAt [][]*wire.Value
	// gone is what this source takes OUT of the child's answer, placed the same
	// way: a deletion whose position is known, and a replacement whose new
	// values no longer match and so loses the child's record too.
	gone   []*amendment
	goneAt [][]*wire.Value

	// unplaced is how many deletions have no known position.
	//
	// Not knowing where a deleted record sat is not knowing which scope it
	// falls in, so it is counted for all of them. Asking the child for one
	// more than was needed is the safe direction; asking for one fewer leaves
	// the scope short.
	unplaced int
}

// orders is one source's amendOrders, kept per data set and bounded the way the
// orderings are.
type orders struct {
	mu     sync.Mutex
	by     map[string]*amendOrder
	recent []string
}

func newOrders() *orders { return &orders{by: map[string]*amendOrder{}} }

// of is the order for one sequence at one generation, built if what is held is
// older than that.
func (o *orders) of(key string, gen uint64, build func() *amendOrder) *amendOrder {
	o.mu.Lock()
	defer o.mu.Unlock()
	if had := o.by[key]; had != nil && had.gen == gen {
		return had
	}
	made := build()
	made.gen = gen
	if _, had := o.by[key]; !had {
		o.recent = append(o.recent, key)
	}
	o.by[key] = made
	for len(o.recent) > orderingsKept {
		delete(o.by, o.recent[0])
		o.recent = o.recent[1:]
	}
	return made
}

// order is what this source has to say about a sequence, built if it has been
// amended since the last time it was asked.
func (a *AmendedSource) order(spec *wire.Spec) *amendOrder {
	a.mu.Lock()
	gen := a.gen
	held := make([]*amendment, 0, len(a.amend))
	for _, am := range a.amend {
		held = append(held, am)
	}
	a.mu.Unlock()

	return a.orders.of(dataSetKey(spec), gen, func() *amendOrder {
		return buildAmendOrder(held, spec)
	})
}

// buildAmendOrder does the once-per-sequence work: place each amendment, decide
// which side of the ledger it is on, and sort the two sides.
func buildAmendOrder(held []*amendment, spec *wire.Spec) *amendOrder {
	o := &amendOrder{}
	levels := ordering1(spec)

	for _, am := range held {
		if am.added && am.clashed {
			// Its key turned out to be the child's. The child's record stands,
			// and this one is neither sent nor subtracted.
			continue
		}
		place := am.place()
		if place == nil {
			// A deletion nobody has seen the record of. It cannot be placed, so
			// it cannot be ruled out of any scope.
			if am.deleted {
				o.unplaced++
			}
			continue
		}
		at := amendTuple(am, place, spec.Sort)
		switch {
		case am.deleted || !wire.Match(am.key, place, spec.Filter):
			// Nothing of ours goes out for it, and one of the child's will not
			// either -- except for an addition, which was never one of the
			// child's and so takes nothing away.
			if !am.added {
				o.gone = append(o.gone, am)
				o.goneAt = append(o.goneAt, at)
			}
		default:
			o.out = append(o.out, am)
			o.outAt = append(o.outAt, at)
		}
	}

	sortAmendments(o.out, o.outAt, levels)
	sortAmendments(o.gone, o.goneAt, levels)
	return o
}

// sortAmendments puts one side of the ledger in the sequence's order, keeping
// each amendment beside its position.
//
// The record's identity is the last level and no two share one, so no two
// positions are equal and there is nothing for stability to settle.
func sortAmendments(ams []*amendment, at [][]*wire.Value, levels []wire.Level) {
	sort.Sort(&amendSort{ams: ams, at: at, levels: levels})
}

type amendSort struct {
	ams    []*amendment
	at     [][]*wire.Value
	levels []wire.Level
}

func (s *amendSort) Len() int { return len(s.ams) }
func (s *amendSort) Swap(i, j int) {
	s.ams[i], s.ams[j] = s.ams[j], s.ams[i]
	s.at[i], s.at[j] = s.at[j], s.at[i]
}
func (s *amendSort) Less(i, j int) bool {
	return wire.CompareLevels(s.at[i], s.at[j], s.levels) < 0
}

// span is where each direction's walk of a sorted run begins, given the
// boundary it starts past.
//
// `up` is the first position a forward walk emits, and `down` is one past the
// last a backward walk emits -- so a backward walk starts at `down - 1`. The
// two differ by exactly the boundary record itself, where the run holds it: a
// forward walk steps over it and a backward walk stops before it.
//
// With no boundary at all the two ends of the run are where the walks begin,
// which is what makes `down` a count rather than a position: no boundary means
// every record is past it, whichever way that is.
func span(at [][]*wire.Value, bound []*wire.Value, levels []wire.Level) (up, down int) {
	if bound == nil {
		return 0, len(at)
	}
	up = sort.Search(len(at), func(i int) bool {
		return wire.CompareLevels(at[i], bound, levels) > 0
	})
	down = sort.Search(len(at), func(i int) bool {
		return wire.CompareLevels(at[i], bound, levels) >= 0
	})
	return up, down
}
