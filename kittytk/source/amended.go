package source

// A source that amends another.
//
// The third kind, and the first that wraps. It holds two different things over
// a child source -- amendments against the child's records, and records of its
// own -- and it answers the query itself: it asks the child the same question,
// and merges what comes back with what it holds.
//
//	a record the child sent that we replace   ours goes out, so the values are right
//	a record the child sent that we deleted   dropped
//	a record of ours the child never sent     ours goes out, if it matches
//	one of ours the child sent after all      THEIRS goes out; ours is a clash
//	anything else the child sent              passed on
//
// **An amendment is a statement about a record of the child's; an addition is
// not.** An amendment names one of the child's and says "mine instead" or
// "gone", and is authoritative for that key: whatever the child says about it
// is suppressed. An addition names nothing of the child's -- it is a record
// this source owns, keyed however its author likes, with no prefix imposed --
// and where the two meet, the child wins.
//
// That is the opposite way round from a replacement, and deliberately. An
// addition's key is the author's to choose and the author's to keep clear; the
// child's records are not this source's to displace by accident. Where a child
// is a ComposedSource every key it hands out carries a slash, so an addition
// without one cannot collide at all -- and adding *into* an include's
// namespace is a legitimate thing to do, for records meant to be moved there
// later.
//
// **Nothing is asked to find a clash out.** It surfaces when the child's copy
// arrives, which is a lookup this source does on every record anyway. On the
// scope where it surfaces, ours has usually already gone and the child's is
// dropped rather than send one identity twice; the clash is written down, and
// every scope after that has ours out and the child's in.
//
// Amendments change at any time. This is a data source, not a query: it
// answers against what it holds when it is asked, and two scopes of one
// data set need not agree.

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

// An AmendedSource holds replacements and deletions against a child's records.
type AmendedSource struct {
	child Source
	notes *placebook

	mu    sync.Mutex
	amend map[string]*amendment
}

// NewAmendedSource amends the records of a child source. The child is any kind
// -- records here, records an application's, or another amended source.
func NewAmendedSource(child Source) *AmendedSource {
	return &AmendedSource{
		child: child,
		notes: newPlacebook(),
		amend: map[string]*amendment{},
	}
}

// An amendment is what is held against one of the child's records.
//
// A deletion carries what was last known of the record it removes. That is not
// the record -- it is gone -- but what it takes to work out whether it would
// have fallen inside a scope, which is how the shortfall it causes is
// predicted rather than discovered.
type amendment struct {
	key     *wire.Value
	fields  wire.Fields // the replacement's or addition's content; nil for a deletion
	deleted bool
	seen    wire.Fields // last known fields of a deleted record

	// added marks a record of this source's own rather than a statement about
	// one of the child's, and clashed marks one whose key turned out to be the
	// child's after all. A clashed addition never goes out again.
	added   bool
	clashed bool
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
func (a *AmendedSource) Replace(key *wire.Value, fields wire.Fields) {
	if key == nil {
		return
	}
	a.mu.Lock()
	a.amend[wire.EncodeValue(key)] = &amendment{key: key, fields: fields}
	a.mu.Unlock()
}

// Add says this source holds a record of its own under this key.
//
// It is not a statement about anything the child holds: the key is the
// author's to choose, and keeping it clear of the child's is the author's to
// do. Where it turns out not to be clear, the child's record is the one that
// stands and this one is dropped for good -- the opposite of Replace, which
// displaces whatever the child has.
func (a *AmendedSource) Add(key *wire.Value, fields wire.Fields) {
	if key == nil {
		return
	}
	a.mu.Lock()
	a.amend[wire.EncodeValue(key)] = &amendment{key: key, fields: fields, added: true}
	a.mu.Unlock()
}

// Delete says a record is gone.
//
// Known is what was last seen of it, and may be nil. With it, the scope that
// record would have fallen in is known before the child is asked; without it,
// the shortfall is discovered afterwards and costs a second question -- which
// is also where the fields to remember are learned.
func (a *AmendedSource) Delete(key *wire.Value, known wire.Fields) {
	if key == nil {
		return
	}
	a.mu.Lock()
	a.amend[wire.EncodeValue(key)] = &amendment{key: key, deleted: true, seen: known}
	a.mu.Unlock()
}

// Forget drops an amendment, leaving the child's own record to stand.
func (a *AmendedSource) Forget(key *wire.Value) {
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
func (a *AmendedSource) learn(key *wire.Value, fields wire.Fields) {
	a.mu.Lock()
	if am := a.amend[wire.EncodeValue(key)]; am != nil && am.deleted {
		am.seen = fields
	}
	a.mu.Unlock()
}

// clash writes down that an addition's key is the child's after all, which is
// the one thing about an addition that cannot be known until the child answers.
// From here on the child's record stands and this one does not go out.
func (a *AmendedSource) clash(key *wire.Value) {
	a.mu.Lock()
	if am := a.amend[wire.EncodeValue(key)]; am != nil && am.added {
		am.clashed = true
	}
	a.mu.Unlock()
}

// held is what the source holds, taken at the moment a scope is asked for.
func (a *AmendedSource) held() []*amendment {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*amendment, 0, len(a.amend))
	for _, am := range a.amend {
		out = append(out, am)
	}
	return out
}

// lookup is the amendment against one key, and nil where there is none.
func (a *AmendedSource) lookup(key *wire.Value) *amendment {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.amend[wire.EncodeValue(key)]
}

// Open states a sequence, and opens the same one on the child.
func (a *AmendedSource) Open(spec *wire.Spec) (DataSet, error) {
	if spec == nil {
		spec = &wire.Spec{}
	}
	child, err := a.child.Open(spec)
	if err != nil {
		return nil, err
	}
	notes := a.notes.of(spec, 2)
	return &amendedSet{
		src:    a,
		spec:   spec,
		child:  child,
		levels: ordering1(spec),
		placed: notes[0],
		resume: notes[1],
	}, nil
}

type amendedSet struct {
	src    *AmendedSource
	spec   *wire.Spec
	child  DataSet
	levels []wire.Level

	// placed is where each record this sequence has handed out stood. The
	// child places `after` for its own records, but this source has to place it
	// too -- it decides which of its own amendments come after it -- and an
	// identity is not a position.
	placed *places

	// resume is where the CHILD stood as each of those records went out.
	//
	// This source has records of its own, so a scope can end on one the child
	// has never heard of -- and handing that identity down would be asking the
	// child to place something that is not its. What goes down instead is the
	// last identity the child itself gave before that record crossed, which is
	// the same place in the merged sequence.
	resume *places
}

// Close lets this sequence go, and the child's with it.
func (s *amendedSet) Close() { s.child.Close() }

// Read answers one scope out of the child's records and this source's own.
func (s *amendedSet) Read(sc *wire.Scope, out Sink) error {
	if out == nil {
		return fmt.Errorf("a scope needs somewhere to put the answer")
	}
	if sc == nil {
		sc = &wire.Scope{}
	}

	var at []*wire.Value
	if sc.After != nil {
		t, ok := s.placed.get(sc.After)
		if !ok {
			out.Done(Complete{Error: fmt.Sprintf(
				"after %s: this sequence has not placed that record",
				wire.EncodeValue(sc.After))})
			return nil
		}
		at = t
	}

	// Where the child carries on from. For a record of the child's this is the
	// record itself; for one of ours it is whatever the child last gave before
	// ours went out.
	var from *wire.Value
	if sc.After != nil {
		if r, ok := s.resume.get(sc.After); ok && len(r) > 0 {
			from = r[0]
		}
	}

	m := &merge{set: s, want: sc, out: out, at: at, levels: s.levels, childAt: from}
	if sc.Reversed {
		// Walking the other way turns every comparison over, this source's
		// own records included: what "before" means is the only thing that
		// changes, and it changes for everyone at once.
		m.levels = wire.Reverse(s.levels)
	}
	m.prepare()

	// The child is asked for the shortfall: the scope, plus what our deletions
	// will take out of its answer, less what we will put in ourselves. Asking
	// for more than that is work nobody reads.
	want := sc.Count + m.slack - len(m.mine)
	if want < 0 {
		want = 0
	}
	return m.ask(from, want)
}

// A merge is one scope being answered: this source's own records for it, and
// the child's, going out as one run.
type merge struct {
	set    *amendedSet
	want   *wire.Scope
	out    Sink
	at     []*wire.Value // where the scope starts, nil at the sequence's end
	levels []wire.Level  // the walk's own direction

	mine  []*amendment // ours, in the sequence's order, still to go out
	slack int          // records of the child's this scope will take out

	childAt *wire.Value     // the last identity the child gave
	gone    map[string]bool // additions of ours that have already crossed

	sent        int
	round       int
	last        *wire.Value // the identity of the last record that went out
	joined      bool        // the walk reached the record the asker already held
	done        bool
	saidOrdered bool
}

// full reports whether the scope has as many records as it was asked for.
//
// Ours count towards it like anything else. A scope of thirty is thirty
// records whoever they came from, and what is left over is not lost -- it is
// the start of the next one.
func (m *merge) full() bool { return m.sent >= m.want.Count }

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
	at := m.at

	for _, am := range s.src.held() {
		place := am.place()
		matches := place != nil && wire.Match(am.key, place, s.spec.Filter)
		after := at == nil || place == nil ||
			wire.CompareLevels(amendTuple(am, place, s.spec.Sort), at, m.levels) > 0

		if !after {
			continue
		}
		if am.added && am.clashed {
			// Its key turned out to be the child's. The child's record stands
			// and this one never goes out again.
			continue
		}
		if am.deleted || !matches {
			// Nothing of ours goes out for it, and one of the child's will not
			// either -- as far as we can tell from where we last saw it.
			if (place != nil || am.deleted) && !am.added {
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
			amendTuple(b, b.place(), s.spec.Sort), m.levels) < 0
	})
}

// ask puts the scope to the child, with room for what this source will take
// out of the answer.
//
// The identity goes down untranslated: this source amends the child's records
// rather than renaming them, so the two share one identity space and an `after`
// that means something here means the same thing there.
func (m *merge) ask(after *wire.Value, count int) error {
	m.round++
	next := *m.want
	next.After = after
	next.Count = count
	return m.set.child.Read(&next, m)
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
	// The child gave it, so this is where the child now stands -- whether or
	// not it is passed on, and that is what the next scope resumes it from.
	m.childAt = key

	if am := m.set.src.lookup(key); am != nil {
		if am.deleted {
			// The child still holds it, so this is where we find out where it
			// sat. Next time the shortfall is predicted rather than met.
			m.set.src.learn(key, fields)
			return nil
		}
		if !am.added {
			return nil // a replacement: ours stands in its place
		}
		// An addition whose key is the child's after all. The child's record
		// is the one that stands, and this is the only moment that can be
		// found out -- so it is written down, and every scope after this one
		// has ours out and the child's in.
		m.set.src.clash(key)
		m.drop(am)
		if m.gone[wire.EncodeValue(key)] {
			// Ours has already crossed in this scope. Sending the child's now
			// would put one identity on the wire twice, which is worse than
			// either record winning, so this scope keeps ours and the next one
			// -- and every one after it -- has the child's.
			return nil
		}
	}
	m.flushBefore(recordTuple(key, fields, m.set.spec.Sort))
	if m.full() {
		return nil
	}
	return m.emit(key, fields, whole)
}

// drop takes an amendment out of what is still to go out.
func (m *merge) drop(am *amendment) {
	for i, held := range m.mine {
		if held == am {
			m.mine = append(m.mine[:i], m.mine[i+1:]...)
			return
		}
	}
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
	if c.Stop == wire.StopJoined {
		// The asker holds the record the walk stopped at and everything past
		// it -- ours included, since ours crossed the same way. So nothing
		// left of ours goes out, and nothing is claimed past that point.
		m.joined = true
	}
	short := !m.full() && !m.joined
	more := c.Stop == wire.StopFilled || c.Stop == wire.StopJoined
	if short && more && c.Error == "" && c.Watermark != nil && m.round < rounds {
		if err := m.ask(c.Watermark, m.want.Count-m.sent+1); err == nil {
			return
		}
	}

	// Whatever is left of ours goes out, up to the count: it is the end of the
	// scope, and the records this source holds do not depend on the child
	// having sent anything.
	if !m.joined {
		m.flushBefore(nil)
	}
	m.done = true

	out := Complete{Error: c.Error}
	switch {
	case c.Error != "":
	case m.joined:
		out.Stop = wire.StopJoined
	case c.Stop == wire.StopExhausted && len(m.mine) == 0:
		// Everything of ours from the boundary on has gone out, so where the
		// child had nothing more, neither has anyone. This outranks a scope
		// that also happened to fill: there being nothing past the end is the
		// stronger fact, and the one that saves the next question.
		out.Stop = wire.StopExhausted
	case m.full():
		out.Stop = wire.StopFilled
	default:
		out.Stop = c.Stop
	}
	if out.Stop != wire.StopExhausted && out.Error == "" {
		// Ours, not the child's. The next scope quotes this back as `after`,
		// and it has to be a record this sequence can place -- which the
		// child's last one need not be, since a record we amend away never
		// reaches anybody and is never placed.
		out.Watermark = m.last
		if out.Watermark == nil {
			out.Watermark = m.want.After
		}
	}
	m.out.Done(out)
}

// flushBefore sends the records of this source's own that belong before a
// position, and everything left when there is none.
func (m *merge) flushBefore(at []*wire.Value) {
	s := m.set
	for len(m.mine) > 0 && !m.full() {
		am := m.mine[0]
		if at != nil {
			mine := amendTuple(am, am.place(), s.spec.Sort)
			if wire.CompareLevels(mine, at, m.levels) > 0 {
				return
			}
		}
		m.mine = m.mine[1:]
		if am.added {
			// Noted because the child may yet send a record under this key. If
			// it does, ours has already crossed and the child's is held back
			// rather than sending one identity twice.
			if m.gone == nil {
				m.gone = map[string]bool{}
			}
			m.gone[wire.EncodeValue(am.key)] = true
		}
		// A replacement is the record entire -- that is what Replace states --
		// and so is an addition. Both go out as one.
		if m.emit(am.key, am.fields, true) != nil {
			return
		}
	}
}

func (m *merge) emit(key *wire.Value, fields wire.Fields, whole bool) error {
	m.sent++
	m.last = key
	// Noted as it goes, because the next scope will name it as `after`: where
	// it stood, so this source can place its own records against it, and where
	// the child stood, so the child can be resumed without being shown an
	// identity that is not its.
	m.set.placed.put(key, recordTuple(key, fields, m.set.spec.Sort))
	m.set.resume.put(key, []*wire.Value{m.childAt})
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
