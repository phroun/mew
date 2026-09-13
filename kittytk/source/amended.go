package source

// A source that amends another.
//
// The third kind, and the first that wraps. It holds replacements and
// deletions against the records of a child source, and it answers the query
// itself: it asks the child the same question, and merges what comes back with
// what it holds of its own.
//
//	a record the child sent that we replace   ours goes out, so the values are right
//	a record the child sent that we deleted   dropped
//	a record of ours the child never sent     ours goes out anyway, if it matches
//	anything else the child sent              passed on
//
// **Our amendments are authoritative for their keys.** Whatever the child says
// about a key we hold is suppressed, and what goes out is ours if it matches
// the query and nothing if it does not. That is what makes the child's extras
// harmless in both directions -- one more record is ignored, one missing is
// supplied.
//
// Amendments change at any time. This is a data source, not a query: it
// answers against what it holds when it is asked, and two scopes of one
// result set need not agree.

import (
	"fmt"
	"sort"
	"sync"

	"github.com/phroun/kittytk/wire"
)

// rounds is how many times one scope will go back to the child for the
// records its deletions took out. A prediction that was wrong is corrected by
// what the round taught it, so a second is nearly always enough; the cap is
// there because a child that keeps answering short should not be asked forever.
const rounds = 4

// An Amended source holds replacements and deletions against a child's
// records.
type Amended struct {
	child Source

	mu    sync.Mutex
	amend map[string]*amendment
}

// NewAmended amends the records of a child source. The child is any kind --
// records here, records an application's, or another amended source.
func NewAmended(child Source) *Amended {
	return &Amended{child: child, amend: map[string]*amendment{}}
}

// An amendment is what is held against one of the child's records.
//
// A deletion carries what was last known of the record it removes. That is not
// the record -- it is gone -- but what it takes to work out whether it would
// have fallen inside a scope, which is how the shortfall it causes is
// predicted rather than discovered.
type amendment struct {
	key     *wire.Value
	fields  wire.Fields // the replacement's content; nil for a deletion
	deleted bool
	seen    wire.Fields // last known fields of a deleted record
}

// place is what the amendment is positioned and filtered by.
func (a *amendment) place() wire.Fields {
	if a.deleted {
		return a.seen
	}
	return a.fields
}

// Replace says a record now carries these fields, whatever the child holds.
//
// These are the record entire, not a correction to some of it: what goes out
// for this key is exactly what is stated here, and it goes out as a whole
// record.
func (a *Amended) Replace(key *wire.Value, fields wire.Fields) {
	if key == nil {
		return
	}
	a.mu.Lock()
	a.amend[wire.EncodeValue(key)] = &amendment{key: key, fields: fields}
	a.mu.Unlock()
}

// Delete says a record is gone.
//
// Known is what was last seen of it, and may be nil. With it, the scope that
// record would have fallen in is known before the child is asked; without it,
// the shortfall is discovered afterwards and costs a second question -- which
// is also where the fields to remember are learned.
func (a *Amended) Delete(key *wire.Value, known wire.Fields) {
	if key == nil {
		return
	}
	a.mu.Lock()
	a.amend[wire.EncodeValue(key)] = &amendment{key: key, deleted: true, seen: known}
	a.mu.Unlock()
}

// Forget drops an amendment, leaving the child's own record to stand.
func (a *Amended) Forget(key *wire.Value) {
	if key == nil {
		return
	}
	a.mu.Lock()
	delete(a.amend, wire.EncodeValue(key))
	a.mu.Unlock()
}

// learn writes down where a deleted record actually sat, from a copy the child
// sent. The next scope over that scope of the sequence predicts its
// shortfall instead of discovering it.
func (a *Amended) learn(key *wire.Value, fields wire.Fields) {
	a.mu.Lock()
	if am := a.amend[wire.EncodeValue(key)]; am != nil && am.deleted {
		am.seen = fields
	}
	a.mu.Unlock()
}

// held is what the source holds, taken at the moment a scope is asked for.
func (a *Amended) held() []*amendment {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*amendment, 0, len(a.amend))
	for _, am := range a.amend {
		out = append(out, am)
	}
	return out
}

// lookup is the amendment against one key, and nil where there is none.
func (a *Amended) lookup(key *wire.Value) *amendment {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.amend[wire.EncodeValue(key)]
}

// Open states a sequence, and opens the same one on the child.
func (a *Amended) Open(spec *wire.Spec) (ResultSet, error) {
	if spec == nil {
		spec = &wire.Spec{}
	}
	child, err := a.child.Open(spec)
	if err != nil {
		return nil, err
	}
	return &amendedSet{
		src:    a,
		spec:   spec,
		child:  child,
		levels: append(wire.Levels(spec.Sort), wire.Level{}),
	}, nil
}

type amendedSet struct {
	src    *Amended
	spec   *wire.Spec
	child  ResultSet
	levels []wire.Level
}

// Close lets this sequence go, and the child's with it.
func (s *amendedSet) Close() { s.child.Close() }

// Fill answers one scope out of the child's records and this source's own.
func (s *amendedSet) Fill(f *wire.Fill, out Sink) error {
	if out == nil {
		return fmt.Errorf("a fill needs somewhere to put the answer")
	}

	m := &merge{set: s, want: f, out: out}
	m.prepare()
	return m.ask(f.From, f.Need+m.slack)
}

// A merge is one scope being answered: this source's own records for it, and
// the child's, going out as one run.
type merge struct {
	set  *amendedSet
	want *wire.Fill
	out  Sink

	mine  []*amendment // ours, in the sequence's order, still to go out
	slack int          // records of the child's this scope will take out

	sent        int
	round       int
	done        bool
	saidOrdered bool
}

// prepare works out what this source has to say about the scope before the
// child is asked anything.
//
// Two lists come out of it. What goes out: the replacements that match the
// query and sit after the boundary, in order. And what is subtracted: the
// amendments that will take one of the child's records away -- a deletion, and
// a replacement whose new values no longer match, which loses a record just as
// surely if the child still holds the old ones.
func (m *merge) prepare() {
	s := m.set
	var at []*wire.Value
	if len(m.want.From) > 0 {
		at = boundaryTuple(m.want.From, s.spec.Sort)
	}

	for _, am := range s.src.held() {
		place := am.place()
		matches := place != nil && wire.Match(place, s.spec.Filter)
		after := at == nil || place == nil ||
			wire.CompareLevels(amendTuple(am, place, s.spec.Sort), at, s.levels) > 0

		if !after {
			continue
		}
		if am.deleted || !matches {
			// Nothing of ours goes out for it, and one of the child's will not
			// either -- as far as we can tell from where we last saw it.
			if place != nil || am.deleted {
				m.slack++
			}
			continue
		}
		m.mine = append(m.mine, am)
	}

	sort.SliceStable(m.mine, func(i, j int) bool {
		a, b := m.mine[i], m.mine[j]
		return wire.CompareLevels(
			amendTuple(a, a.place(), s.spec.Sort),
			amendTuple(b, b.place(), s.spec.Sort), s.levels) < 0
	})
}

// ask puts the scope to the child, with room for what this source will take
// out of the answer.
func (m *merge) ask(from wire.Fields, need int) error {
	m.round++
	next := *m.want
	next.From = from
	next.Need = need
	return m.set.child.Fill(&next, m)
}

// Ordered is the child saying its records are in the sequence's order, before
// any of them arrive.
//
// Ours go out in that order too, so what comes out of the merge is ordered
// exactly when what goes into it was -- and whoever is reading learns it in
// time to act on it, which is the whole reason it is said up front.
func (m *merge) Ordered() {
	if !m.saidOrdered {
		m.saidOrdered = true
		m.out.Ordered()
	}
}

// Record and Subset take one of the child's records, entire or in part. Which
// it was goes out unchanged: this source says of a record it passed on exactly
// what the child said of it.
func (m *merge) Record(key *wire.Value, fields wire.Fields) error {
	return m.theirs(key, fields, true)
}

func (m *merge) Subset(key *wire.Value, fields wire.Fields) error {
	return m.theirs(key, fields, false)
}

// theirs is one of the child's records reaching the merge.
//
// A key this source amends is the source's to answer: the child's copy is
// dropped, and ours goes out in its own place -- which is wherever the run of
// ours reaches, not wherever the child's copy turned up.
func (m *merge) theirs(key *wire.Value, fields wire.Fields, whole bool) error {
	if am := m.set.src.lookup(key); am != nil {
		if am.deleted {
			// The child still holds it, so this is where we find out where it
			// sat. Next time the shortfall is predicted rather than met.
			m.set.src.learn(key, fields)
		}
		return nil
	}
	m.flushBefore(recordTuple(key, fields, m.set.spec.Sort))
	return m.emit(key, fields, whole)
}

// Done is the end of one round of the child's answer.
//
// If the scope came up short of what was asked for -- a deletion landed in
// it that this source did not know about -- the child is asked again from
// where it got to. What the round taught about that deletion means the next
// scope over the same ground does not come up short again.
func (m *merge) Done(c Complete) {
	if m.done {
		return
	}
	short := m.sent < m.want.Have+m.want.Need
	if short && !c.Exhausted && c.Error == "" && len(c.Watermark) > 0 && m.round < rounds {
		if err := m.ask(c.Watermark, m.want.Have+m.want.Need-m.sent+1); err == nil {
			return
		}
	}

	// Whatever is left of ours goes out: it is the end of the scope, and the
	// records this source holds do not depend on the child having sent
	// anything.
	m.flushBefore(nil)
	m.done = true

	out := Complete{
		// Everything of ours from the boundary on has just gone out, so where
		// the child had nothing more, neither has anyone.
		Exhausted: c.Exhausted,
		Error:     c.Error,
	}
	if !out.Exhausted {
		out.Watermark = c.Watermark
	}
	m.out.Done(out)
}

// flushBefore sends the records of this source's own that belong before a
// position, and everything left when there is none.
func (m *merge) flushBefore(at []*wire.Value) {
	s := m.set
	for len(m.mine) > 0 {
		am := m.mine[0]
		if at != nil {
			mine := amendTuple(am, am.place(), s.spec.Sort)
			if wire.CompareLevels(mine, at, s.levels) > 0 {
				return
			}
		}
		m.mine = m.mine[1:]
		// A replacement is the record entire -- that is what Replace states --
		// so it goes out as one.
		if m.emit(am.key, am.fields, true) != nil {
			return
		}
	}
}

func (m *merge) emit(key *wire.Value, fields wire.Fields, whole bool) error {
	m.sent++
	if whole {
		return m.out.Record(key, fields)
	}
	return m.out.Subset(key, fields)
}

// amendTuple and recordTuple place a record: the value at each sort level, and
// then the key, which is the level that makes a position mean exactly one
// record.
func amendTuple(am *amendment, fields wire.Fields, levels []wire.SortLevel) []*wire.Value {
	return recordTuple(am.key, fields, levels)
}

func recordTuple(key *wire.Value, fields wire.Fields, levels []wire.SortLevel) []*wire.Value {
	out := make([]*wire.Value, 0, len(levels)+1)
	for _, l := range levels {
		out = append(out, fields.Get(l.Field))
	}
	return append(out, key)
}
