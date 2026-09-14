package source

// A run of records the cache holds, and what it costs to hold.
//
// A cached scope is a stretch of one data set that is **complete between its
// ends**:
// every record of the sequence that falls between them is here, in order, with
// nothing missing. That is the same claim a watermark makes, which is why a
// scope's answer and a cache entry are the same object -- what came back from
// one question is exactly what can be handed to the next.
//
// Both ends are optional, and their absence is not ignorance but a stronger
// claim. No start means from the beginning of the sequence; no end means to the
// end of it. So a source that ignores every hint, sends all its records and says
// `exhausted` produces one cached scope with neither end -- the whole sequence,
// cached entire, and never asked for again.
//
// What the ends are is identities, because that is what a scope names its ends
// by and what a watermark is.
//
// Two things change a cached scope and neither re-queries anything. A scope
// answered immediately past its end EXTENDS it: the end moves and the records
// are appended. A record inserted inside it SLICES it: the guarantee holds on
// both sides of the insert and not across it, so it becomes two cached scopes
// and the records stay where they are.

import "github.com/phroun/kittytk/wire"

// The estimated cost of the parts a record is made of.
//
// These are for deciding when to evict, not for reporting memory, so what
// matters is that they are CONSISTENT rather than exact: a record's size is
// worked out once and kept, and eviction subtracts what insertion added. The
// counter then cannot drift however wrong the estimate is, and being wrong only
// makes a byte limit nominal.
//
// They are calibrated against the real heap rather than guessed -- see
// 0_cached scope_test.go, which fails if the structure changes enough that the
// estimate stops tracking reality.
// The numbers are the real struct sizes rather than guesses: a wire.Value is 80
// bytes, a wire.Arg 32, a cached record 64, each rounded up for the allocator's
// size class and the pointer that reaches it.
const (
	recordOverhead = 80 // a cached record in the run, plus its slot
	fieldOverhead  = 48 // one wire.Arg and the pointer to it
	valueOverhead  = 80 // a wire.Value beyond whatever it carries
)

// A cached record: what came back, and what the cache needs to know about it.
type cached struct {
	id     *wire.Value
	fields wire.Fields
	whole  bool // the record entire, rather than the fields one query asked for

	// size is what this record added to the cache's total, worked out once when
	// it went in. Eviction subtracts this rather than measuring again: a record
	// whose fields were patched by an invalidation in between would otherwise
	// give back a different number than it took.
	size int

	// gen is the generation of the data set this record was fetched at.
	//
	// Nothing reads it yet. It is what invalidation will compare against to
	// know whether a cached record predates a change, and it is here from the
	// start because adding it afterwards means every record already cached has a
	// generation nobody can work out.
	gen uint64

	// read is the tick this record was last handed to somebody.
	//
	// A tick rather than a clock: it only has to order, it is compared far more
	// often than it is set, and a monotonic counter cannot go backwards when
	// the machine's time does. It is per record rather than per cached scope, so
	// that a cold end can be trimmed off a warm one instead of dropping the lot.
	read uint64
}

// size is what one record costs to hold, worked out once.
func sizeOf(id *wire.Value, fields wire.Fields) int {
	n := recordOverhead + sizeOfValue(id)
	for _, f := range fields {
		n += fieldOverhead + len(f.Name) + sizeOfValue(f.Value)
	}
	return n
}

// sizeOfValue is what one value costs, following a block into its members.
func sizeOfValue(v *wire.Value) int {
	if v == nil {
		return 0
	}
	n := valueOverhead
	switch v.Kind {
	case wire.StringValue:
		n += len(v.Str)
	case wire.WordValue:
		n += len(v.Word)
	case wire.BlockValue:
		if v.Block != nil {
			for _, st := range v.Block.Statements {
				n += fieldOverhead + len(st.Verb)
				for _, a := range st.Args {
					n += sizeOfValue(a.Value)
				}
			}
		}
	}
	return n
}

// A cachedScope is a run of one data set's records, complete between its ends.
type cachedScope struct {
	// set is the data set these records belong to: this source, this sort,
	// this filter. Records of two different sequences are never in one cached
	// scope, because "complete between its ends" is a claim about an order, and
	// they have different ones.
	set string

	// start is the record this run begins AFTER, and end the record it is
	// complete UP TO. Nil start means the beginning of the sequence, nil end
	// the end of it -- both being claims rather than gaps.
	start *wire.Value
	end   *wire.Value

	recs []cached

	// size is what this whole run costs: its records, and its own overhead.
	size int
}

// newCachedScope is a run of records as a cached scope of one data set.
func newCachedScope(set string, start, end *wire.Value, recs []cached) *cachedScope {
	e := &cachedScope{set: set, start: start, end: end, recs: recs}
	for _, r := range recs {
		e.size += r.size
	}
	return e
}

// Len is how many records the cached scope holds.
func (e *cachedScope) Len() int { return len(e.recs) }

// at is where in the run a record stands, and false for one this cached scope
// does not hold.
func (e *cachedScope) at(id *wire.Value) (int, bool) {
	if id == nil {
		return 0, false
	}
	want := wire.EncodeValue(id)
	for i := range e.recs {
		if wire.EncodeValue(e.recs[i].id) == want {
			return i, true
		}
	}
	return 0, false
}

// from is where a scope starting after an identity would be served from, and
// whether this cached scope can serve it at all.
//
// Two identities serve: the record it starts after, which is its own start, and
// any record it holds. Anything else is outside what this cached scope claims,
// and answering from it would be answering about a stretch nothing here covers.
func (e *cachedScope) from(after *wire.Value) (int, bool) {
	if after == nil {
		return 0, e.start == nil
	}
	if e.start != nil && wire.EncodeValue(e.start) == wire.EncodeValue(after) {
		return 0, true
	}
	if i, ok := e.at(after); ok {
		return i + 1, true
	}
	return 0, false
}

// extend takes a run answered immediately past this one's end into it.
//
// The end moves and the records are appended. Nothing is re-queried and nothing
// is copied twice: what the next scope asked for beginning where this one
// stopped is the same stretch, so it is the same cached scope.
//
// It is refused where the run does not begin exactly at this end, because two
// stretches with anything unknown between them are two cached scopes: joining
// them would claim completeness across a gap nobody has looked at.
func (e *cachedScope) extend(after *wire.Value, recs []cached, end *wire.Value) bool {
	if e.end == nil {
		return false // it already runs to the end of the sequence
	}
	if after == nil || wire.EncodeValue(after) != wire.EncodeValue(e.end) {
		return false
	}
	e.recs = append(e.recs, recs...)
	for _, r := range recs {
		e.size += r.size
	}
	e.end = end
	return true
}

// slice cuts this in two before the record at i, which is what an insert at that
// point does to it.
//
// The guarantee holds on both sides and not across, so what comes back is two
// cached scopes holding the same records between them: the first complete up to the
// record before the cut, the second beginning after it. Nothing is re-queried,
// because nothing either side of an insert has changed.
//
// The cut is refused at the very start, where there is no first half to make.
func (e *cachedScope) slice(i int) (*cachedScope, *cachedScope) {
	if i <= 0 || i >= len(e.recs) {
		return e, nil
	}
	left := newCachedScope(e.set, e.start, e.recs[i-1].id, append([]cached(nil), e.recs[:i]...))
	right := newCachedScope(e.set, e.recs[i-1].id, e.end, append([]cached(nil), e.recs[i:]...))
	return left, right
}

// merge takes another cached scope into this one where they meet.
//
// They meet when this one is complete up to exactly where the other begins, so
// that between them nothing is unaccounted for. It is what a slice undoes, and
// what two scopes answered back to back amount to.
func (e *cachedScope) merge(other *cachedScope) bool {
	if e.set != other.set || e.end == nil || other.start == nil {
		return false
	}
	if wire.EncodeValue(e.end) != wire.EncodeValue(other.start) {
		return false
	}
	e.recs = append(e.recs, other.recs...)
	e.size += other.size
	e.end = other.end
	return true
}

// trimFront and trimBack take the coldest end off, giving back what that freed.
//
// Trimming keeps a cached scope true: dropping records from an end and moving
// that end in with them leaves the claim between the ends exactly as good as it
// was. Taking something out of the MIDDLE would not -- that is a slice, and it
// costs a cached scope rather than freeing one.
func (e *cachedScope) trimFront(n int) int {
	if n <= 0 {
		return 0
	}
	if n >= len(e.recs) {
		n = len(e.recs)
	}
	freed := 0
	for _, r := range e.recs[:n] {
		freed += r.size
	}
	e.start = e.recs[n-1].id
	e.recs = append([]cached(nil), e.recs[n:]...)
	e.size -= freed
	return freed
}

func (e *cachedScope) trimBack(n int) int {
	if n <= 0 {
		return 0
	}
	if n >= len(e.recs) {
		n = len(e.recs)
	}
	cut := len(e.recs) - n
	freed := 0
	for _, r := range e.recs[cut:] {
		freed += r.size
	}
	if cut == 0 {
		e.end = e.start
	} else {
		e.end = e.recs[cut-1].id
	}
	e.recs = e.recs[:cut]
	e.size -= freed
	return freed
}

// coldest is the tick of the least recently read record at each end, which is
// what says which end is worth trimming.
func (e *cachedScope) coldest() (front, back uint64) {
	if len(e.recs) == 0 {
		return 0, 0
	}
	return e.recs[0].read, e.recs[len(e.recs)-1].read
}
