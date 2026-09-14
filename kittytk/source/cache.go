package source

// The cache of answered scopes, and which of them gives way when the room runs
// out.
//
// One cache for the whole process, holding runs of records belonging to many
// data sets. A scope's answer and a cached run are the same object -- complete
// between two ends -- so holding an answer is filing it, and answering from the
// cache is walking one that is already here. Nothing is re-derived: a run that
// meets another joins it, and a run asked for part of itself hands back a slice
// of its own links.
//
// # Keeping the hot records against a cold flood
//
// The thing an LRU cannot do is survive a scan. Recency is only a proxy for
// worth, and a walk that reads a million records and never looks back
// manufactures recency for free: every one of those records is the most
// recently used thing in the cache the moment it lands, and between them they
// push out everything that had actually proved itself. The records that were
// being used lose to records nothing ever wanted twice.
//
// So there are two segments, which is the standard answer (segmented LRU, or
// 2Q):
//
//   - Everything arrives on PROBATION. A record handed out once is no evidence
//     of anything -- a scan touches every record it passes exactly once.
//   - A record handed out a SECOND time is warm, and moves to the protected
//     segment.
//   - Eviction always takes from probation. Not preferentially: entirely. A
//     record that has proved itself is only ever touched once there is no
//     probationary record left anywhere in the cache to take, and it is that
//     rule a flood cannot get around, however many records it pours in.
//
// Two caps keep either segment from swallowing the cache:
//
//   - The protected segment is held under warmShare of the limit, so that what
//     has proved itself cannot fill the cache and leave no room to prove
//     anything else.
//   - One run's PROBATIONARY part is held under coldShare of the limit, and
//     that is the flood defence proper. A streaming answer is entirely cold, so
//     a run pouring in eats its own far end rather than the rest of the cache
//     once it passes the cap. It costs a large scan nothing it was going to use
//     anyway -- it never looks back -- and it costs everything else nothing at
//     all.
//
// A frequency sketch on top of this (TinyLFU: admit a record only if it scores
// better than the record it would evict, counting what has been ASKED for
// rather than what is held) is the stronger answer, and is what to reach for if
// measurement says these two are not enough. It is deliberately not here yet.
//
// # What is not here
//
// Invalidation. Entries carry a generation for it and runs can already be split
// and rejoined without re-querying, which is the machinery it will need, but
// nothing yet decides that a held record is stale.
//
// While that is true, the cache refuses to hold an answer overlapping records
// it already has: with no way to tell which copy is right, the one already
// filed is kept and the new answer is dropped rather than held twice. Runs
// carrying different fields are not an overlap -- those are different records
// as far as the cache is concerned, and coexist.

import (
	"sort"
	"strings"
	"sync"

	"github.com/phroun/kittytk/wire"
)

// defaultCacheLimit is what the cache holds until somebody says otherwise. A
// nominal figure in the units costOf counts, which are close enough to bytes to
// reason in.
const defaultCacheLimit = 64 << 20

// The shares the two segments are held to, as numerator and denominator so that
// they read as what they are.
const (
	warmShare, warmOf = 4, 5 // the protected segment: four fifths
	coldShare, coldOf = 1, 2 // one run's probationary part: one half
)

// hot is the process's cache. Global because a data set is global: two parts of
// a program reading the same source, sort and filter are reading the same
// sequence, and there is no reason for them to fetch it twice.
var hot = newCache(defaultCacheLimit)

type cache struct {
	mu sync.Mutex

	limit int // what it will hold
	cost  int // what it is holding
	warm  int // and how much of that is protected

	// tick orders hand-outs. See entry.hit.
	tick uint64

	// next names the run made next. Ids are opaque and never reused.
	next cachedScopeID

	runs map[cachedScopeID]*cachedScope
	sets map[string][]*cachedScope

	// at finds a record by data set and identity in one lookup. The slice holds
	// one entry per set of carried fields: a run fetched whole and a run fetched
	// for `fields={ name }` may each hold their own copy of record 7, and they
	// answer different questions.
	at map[string][]*entry
}

func newCache(limit int) *cache {
	return &cache{
		limit: limit,
		next:  1, // zero names no run, so a half-built one cannot be mistaken for one

		runs: map[cachedScopeID]*cachedScope{},
		sets: map[string][]*cachedScope{},
		at:   map[string][]*entry{},
	}
}

// Cost is what the cache is holding, and Limit what it will hold.
func (c *cache) Cost() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cost
}

func (c *cache) Limit() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.limit
}

// SetLimit fixes how much the cache holds, evicting down to it at once.
func (c *cache) SetLimit(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limit = n
	c.room(0)
}

// --- what is carried -----------------------------------------------------

// covers reports whether a run holding `carried` can answer a query wanting
// `wanted`. Covering rather than equal: a run that holds more than was asked
// for answers the question, and one that holds the records entire answers any
// question at all.
func covers(carried, wanted wire.Fields) bool {
	if carried == nil {
		return true
	}
	if wanted == nil {
		return false // the whole record was asked for; this run has part of one
	}
	for _, w := range wanted {
		if !carried.Has(w.Name) {
			return false
		}
	}
	return true
}

// carriedKey spells a set of carried fields so two of them can be compared. The
// names are a set and not a list -- `fields={ name size }` and
// `fields={ size name }` ask for the same thing -- so they are sorted.
func carriedKey(f wire.Fields) string {
	if f == nil {
		return "*"
	}
	names := append([]string(nil), f.Names()...)
	sort.Strings(names)
	return "=" + strings.Join(names, "\x00")
}

func sameCarried(a, b wire.Fields) bool { return carriedKey(a) == carriedKey(b) }

// recordKey finds a record of one data set by identity.
//
// wire.Key and not the wire spelling: a table keyed by how a value is WRITTEN
// files a nil identity and a record actually called `undefined` in the same
// place, and moves every key it holds the day a float is spelled differently.
func recordKey(set string, id *wire.Value) string {
	b := make([]byte, 0, len(set)+24)
	b = append(append(b, set...), 0)
	return string(wire.AppendKey(b, id))
}

// --- answering -----------------------------------------------------------

// serve answers a scope from what is held, or says it cannot.
//
// It answers only in FULL. A run that holds the first half of what was asked
// for is no use: the query is asked once and answered once, so half an answer
// would have to be stitched to a fetch for the rest, and the fetch has to
// happen either way. So a hit is a walk that reached the count, reached
// `until`, or reached the end of the sequence -- and falling off the end of the
// RUN, which is where its guarantee stops and not where the records do, is a
// miss.
func (c *cache) serve(set string, wanted wire.Fields, sc *wire.Scope) ([]*entry, wire.Complete, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	run, from := c.find(set, wanted, sc)
	if run == nil {
		return nil, wire.Complete{}, false
	}

	at := from
	switch {
	case at != nil:
		at = at.along(sc.Reversed)
	case sc.Reversed:
		at = run.tail
	default:
		at = run.head
	}

	var done wire.Complete
	out := make([]*entry, 0, sc.Count)
	for at != nil && (sc.Count <= 0 || len(out) < sc.Count) {
		if sc.Until != nil && wire.Equal(at.id, sc.Until) {
			done.Stop = wire.StopJoined
			break
		}
		out = append(out, at)
		at = at.along(sc.Reversed)
	}

	switch {
	case done.Stop == wire.StopJoined:
	case sc.Count > 0 && len(out) == sc.Count:
		done.Stop = wire.StopFilled
	case run.beyond(sc.Reversed) == nil:
		// The walk ran out inside a run whose far end is the sequence's own, so
		// there is nothing past it. No watermark: nothing to be complete up to.
		done.Stop = wire.StopExhausted
		c.handed(run, out)
		return out, done, true
	default:
		return nil, wire.Complete{}, false // past the guarantee, not past the records
	}
	if len(out) > 0 {
		done.Watermark = out[len(out)-1].id
	}
	c.handed(run, out)
	return out, done, true
}

// find is the run a scope reads and the entry it starts from, or nil for a
// scope no held run can answer.
func (c *cache) find(set string, wanted wire.Fields, sc *wire.Scope) (*cachedScope, *entry) {
	// No start named: the scope begins at the sequence's own beginning, or read
	// backwards, at its own end -- which is the run claiming that end.
	if sc.After == nil {
		for _, s := range c.sets[set] {
			if s.beyond(!sc.Reversed) == nil && covers(s.carried, wanted) {
				return s, nil
			}
		}
		return nil, nil
	}
	// The record is held, and its run carries on past it.
	for _, e := range c.at[recordKey(set, sc.After)] {
		s := c.runs[e.scope]
		if s == nil || !covers(s.carried, wanted) {
			continue
		}
		if e.along(sc.Reversed) != nil || s.beyond(sc.Reversed) == nil {
			return s, e
		}
	}
	// Or it is not held, or held right at the edge of its own run's guarantee --
	// and a run guaranteed FROM this record starts in exactly the place a scope
	// past it does. Reading backwards, one guaranteed TO it ends there.
	for _, s := range c.sets[set] {
		edge := s.beyond(!sc.Reversed)
		if edge != nil && wire.Equal(edge, sc.After) && covers(s.carried, wanted) {
			return s, nil
		}
	}
	return nil, nil
}

// beyond is the end a walk in this direction runs out at: nil says the
// sequence's own end is there, so there is nothing past it.
func (s *cachedScope) beyond(back bool) *wire.Value {
	if back {
		return s.begin
	}
	return s.end
}

// handed marks records as given out, which is what decides what is worth
// keeping. A record given out a second time has proved something a record given
// out once has not.
func (c *cache) handed(run *cachedScope, es []*entry) {
	for _, e := range es {
		c.tick++
		if e.hit != 0 && !e.warm {
			run.warmUp(e)
			c.warm += e.cost
		}
		e.hit = c.tick
	}
	c.capWarm()
}

// --- filing --------------------------------------------------------------

// hold files an answered scope.
//
// The scope says what the run is guaranteed between: it starts past `after`,
// and it reaches the watermark -- or, where the answer was `exhausted`, the end
// of the sequence itself, which is the stronger claim and is spelled as no end
// at all. Read backwards the two ends swap, the records arriving furthest-first.
//
// Nothing is held for a refusal, for an empty answer, or for an answer
// overlapping records already held.
func (c *cache) hold(set string, carried wire.Fields, sc *wire.Scope, recs []*entry, done wire.Complete) {
	if done.Error != "" || len(recs) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, e := range recs {
		for _, held := range c.at[recordKey(set, e.id)] {
			if sameCarried(c.runs[held.scope].carried, carried) {
				return
			}
		}
	}

	run := &cachedScope{set: set, carried: carried}
	if sc.Reversed {
		run.begin, run.end = done.Watermark, sc.After
		for _, e := range recs {
			run.pushFront(e)
		}
	} else {
		run.begin, run.end = sc.After, done.Watermark
		for _, e := range recs {
			run.pushBack(e)
		}
	}

	// A flood is cut down to its share BEFORE anything is asked to give way for
	// it, or one enormous answer would evict the whole cache on its way in and
	// only then discover it was never entitled to the room. What is dropped is
	// the end furthest from where the reader is: records arrive in walk order,
	// so a reader scrolling down is at the back of what just came and will not
	// be asking for the front of it again.
	for max := c.limit * coldShare / coldOf; run.cold > max && run.n > 0; {
		if sc.Reversed {
			run.trimBack(1)
		} else {
			run.trimFront(1)
		}
	}
	if run.n == 0 {
		return // a cache too small to hold one record of it holds none
	}

	c.room(run.cost)
	c.cost += run.cost
	for _, e := range run.all() {
		k := recordKey(set, e.id)
		c.at[k] = append(c.at[k], e)
	}

	// Joined to whatever it meets, in either direction, so that scrolling leaves
	// one run rather than a run per screenful.
	if before := c.endingAt(set, run.begin, carried, run); before != nil && before.merge(run) {
		run = before
	} else {
		run.id = c.next
		c.next++
		for _, e := range run.all() {
			e.scope = run.id
		}
		c.runs[run.id] = run
		c.sets[set] = append(c.sets[set], run)
	}
	if after := c.beginningAt(set, run.end, carried, run); after != nil && run.merge(after) {
		c.forget(after)
	}
	c.capCold(run, sc.Reversed)
}

// endingAt and beginningAt are the runs `not` would meet, carrying the same
// fields. Runs of one data set are few -- each is a stretch somebody is reading
// -- so they are looked through rather than indexed.
//
// `not` is the run doing the asking, and is never the answer. A run that met
// itself would relabel its own entries, take its own count and cost into itself
// twice, and then be forgotten as the half that was absorbed -- so it is worth
// a pointer compare to make impossible, even though nothing reaches it today: a
// run in the table always spans at least one record, and one that does has a
// beginning and an end naming different records. Insurance, not a live path,
// which is why no test kills it.
func (c *cache) endingAt(set string, at *wire.Value, carried wire.Fields, not *cachedScope) *cachedScope {
	if at == nil {
		return nil
	}
	for _, s := range c.sets[set] {
		if s != not && wire.Equal(s.end, at) && sameCarried(s.carried, carried) {
			return s
		}
	}
	return nil
}

func (c *cache) beginningAt(set string, at *wire.Value, carried wire.Fields, not *cachedScope) *cachedScope {
	if at == nil {
		return nil
	}
	for _, s := range c.sets[set] {
		if s != not && wire.Equal(s.begin, at) && sameCarried(s.carried, carried) {
			return s
		}
	}
	return nil
}

// all is the run's entries, taken before anything relinks them.
func (s *cachedScope) all() []*entry {
	out := make([]*entry, 0, s.n)
	for e := s.head; e != nil; e = e.next {
		out = append(out, e)
	}
	return out
}

// --- making room ---------------------------------------------------------

// room evicts until n more will fit.
//
// Probation first and entirely: every unwarm record in the cache goes before a
// warm one is touched, which is the rule a flood cannot get around. Only when
// there is nothing left on probation anywhere does the coldest protected end
// give way, and a cache of nothing but proven records would otherwise wedge --
// full, unable to evict, and unable to hold anything new.
//
// Each pass drops one record, so this terminates whatever is held; it gives up
// only when the cache is empty and n still does not fit, which is a limit
// smaller than one record.
func (c *cache) room(n int) {
	for c.cost+n > c.limit {
		s, _, front := c.coldestEnd(false)
		if s == nil {
			s, _, front = c.coldestEnd(true)
		}
		if s == nil {
			return
		}
		c.drop(s, front)
	}
}

// capWarm holds the protected segment under its share, so that records which
// have proved themselves cannot fill the cache and leave nothing on probation
// with a chance to prove anything.
func (c *cache) capWarm() {
	for c.warm > c.limit*warmShare/warmOf {
		s, e, _ := c.coldestEnd(true)
		if s == nil {
			return // warm records, but none of them at an end to cool
		}
		s.coolDown(e)
		c.warm -= e.cost
	}
}

// capCold holds ONE run's probationary part under its share, which is the flood
// defence: an answer streaming in is entirely cold, so past the cap it eats its
// own far end instead of the rest of the cache. Records that have been read
// more than once are not charged against the cap and are never taken for it.
//
// Which end is the far one is the direction of the answer that just arrived,
// and not which end was read longest ago -- records that have just come in have
// been read NEVER, which sorts them coldest of all, and they are exactly the
// ones the reader is sitting on. A reader scrolling down is at the back of the
// run and will not be asking for the front of it again.
func (c *cache) capCold(s *cachedScope, back bool) {
	for s.cold > c.limit*coldShare/coldOf {
		e := s.head
		if back {
			e = s.tail
		}
		if e == nil || e.warm {
			return // nothing left, or what is there has proved itself
		}
		c.drop(s, !back)
	}
}

// coldestEnd is the least recently handed record at the end of any run, in the
// segment asked for. Only ends: taking a record out of the middle of a run
// would cost a run rather than freeing one.
//
// Every run is looked at, which is a walk of the runs per record evicted. Runs
// are few and records are many, so that is cheap; an eviction heap would be
// worth it only once it is not.
func (c *cache) coldestEnd(warm bool) (best *cachedScope, at *entry, front bool) {
	consider := func(s *cachedScope, e *entry, f bool) {
		if e == nil || e.warm != warm {
			return
		}
		if at == nil || e.hit < at.hit {
			best, at, front = s, e, f
		}
	}
	for _, s := range c.runs {
		consider(s, s.head, true)
		consider(s, s.tail, false)
	}
	return best, at, front
}

// drop takes one record off an end of a run, moving the run's end in with it so
// that what it still claims stays true.
func (c *cache) drop(s *cachedScope, front bool) {
	e := s.tail
	if front {
		e = s.head
	}
	if e == nil {
		return
	}
	c.unfile(s.set, e)
	if e.warm {
		s.coolDown(e) // it costs the protected segment nothing once it is gone
		c.warm -= e.cost
	}
	if front {
		c.cost -= s.trimFront(1)
	} else {
		c.cost -= s.trimBack(1)
	}
	if s.n == 0 {
		c.forget(s)
	}
}

// unfile takes a record out of the lookup, leaving any copy of it carrying
// different fields where it is.
func (c *cache) unfile(set string, e *entry) {
	k := recordKey(set, e.id)
	kept := c.at[k][:0]
	for _, held := range c.at[k] {
		if held != e {
			kept = append(kept, held)
		}
	}
	if len(kept) == 0 {
		delete(c.at, k)
	} else {
		c.at[k] = kept
	}
}

// forget drops an emptied or merged-away run.
func (c *cache) forget(s *cachedScope) {
	delete(c.runs, s.id)
	kept := c.sets[s.set][:0]
	for _, held := range c.sets[s.set] {
		if held != s {
			kept = append(kept, held)
		}
	}
	if len(kept) == 0 {
		delete(c.sets, s.set)
	} else {
		c.sets[s.set] = kept
	}
}
