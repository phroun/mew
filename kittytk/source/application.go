package source

// A source whose records are an application's.
//
// `PSLSource` reads records that are here; this one asks for them
// over a connection, in the query/result exchange docs/hosting-a-query.md
// spells out, and hands them on as they arrive. Both are one interface, so
// whatever asks the question does not know which kind answered it.
//
// Nothing is waited for. A scope is asked for by writing a statement, and
// the results come back later on whatever thread reads the connection;
// `Inbound` is where they are handed in, and the sink is fed from there.

import (
	"fmt"
	"sync"

	"github.com/phroun/kittytk/wire"
	"github.com/phroun/serval"
)

// An ApplicationSource names a body of records an application serves, and the
// way to reach that application.
//
// Send writes one batch of statements. Whatever reads the connection hands
// every statement the application says back to Inbound; the ones addressed to
// a query this source opened are taken, and the rest are somebody else's.
type ApplicationSource struct {
	name string
	send func(src string) error

	mu      sync.Mutex
	opening []*appScope // asked, in the order their replies are owed
	byID    map[uint64]*appScope
	tells   []func()            // told when a scope's answer has landed
	onStale []func(*wire.Stale) // told when the application says something is no longer true

	// firing and again coalesce the telling. A listener re-reads, a re-read asks
	// a question, and a question that finishes at once would be told about while
	// the first telling was still on the stack. See arrived.
	firing bool
	again  bool
}

// NewApplicationSource is a source backed by the application's records under
// this name.
func NewApplicationSource(name string, send func(src string) error) *ApplicationSource {
	return &ApplicationSource{name: name, send: send, byID: map[uint64]*appScope{}}
}

// Name is what the application serves these records under.
func (h *ApplicationSource) Name() string { return h.name }

// Open states a sequence. Nothing is said on the wire yet: a query is opened
// with the first scope of it, because there is no reason to name a sequence
// nobody is reading.
func (h *ApplicationSource) Open(spec *serval.Spec) (serval.DataSet, error) {
	if spec == nil {
		spec = &serval.Spec{}
	}
	if h.send == nil {
		return nil, fmt.Errorf("this source has no connection to ask")
	}
	stated := *spec
	stated.Source = h.name
	return &appSet{src: h, spec: &stated}, nil
}

// An appSet is one sequence, from this side.
//
// It says nothing on the wire of its own. A query states a sequence and asks
// for one scope of it, and nothing restates it afterwards, so every scope read
// here is a query of its own -- and this holds only what they are all queries
// *of*.
type appSet struct {
	src  *ApplicationSource
	spec *serval.Spec

	mu     sync.Mutex
	closed bool
	live   []*appScope // scopes still being answered
}

// An appScope is one scope asked for: one query on the wire, from the statement
// that opens it to the result that completes it.
//
// Its id is the application's, and it does not exist until the application
// replies with it. Nothing needs it before then: the whole question went out in
// the statement that opened it, so there is nothing waiting on the number
// except the results that will quote it back.
type appScope struct {
	set *appSet
	out serval.Sink

	// extend says this scope asked for results that lean on their places, which
	// it does only where the sink has somewhere to put a place. `held` is then
	// what each place carried, kept until its result arrives to be merged with.
	//
	// The asking and the holding are the same fact, which is why nothing here is
	// configured: a reader that drops places must not have results leaning on
	// them, so the one that can hold them is the one that asks.
	extend bool
	held   map[string]serval.Record

	mu   sync.Mutex
	id   uint64
	done bool
}

// Read asks the application for one scope and returns. The records reach the
// sink when the application sends them.
func (s *appSet) Read(sc *serval.Scope, out serval.Sink) error {
	if out == nil {
		return fmt.Errorf("a scope needs somewhere to put the answer")
	}
	if sc == nil {
		sc = &serval.Scope{}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("this data set has been closed")
	}
	_, places := out.(serval.Placing)
	q := &appScope{set: s, out: out, extend: places}
	if places {
		q.held = map[string]serval.Record{}
	}
	s.live = append(s.live, q)
	s.mu.Unlock()

	// Outside the sequence's own lock, because the source takes its lock and
	// then each scope's: holding one while reaching for the other in the
	// opposite order is how two threads stop dead.
	s.src.asked(q)

	stmt := "q=new query " + wire.EncodeSpec(s.spec) + " " + wire.EncodeScope(sc)
	if q.extend {
		stmt += " " + wire.ExtendArg
	}
	if err := s.src.send(stmt + "\nend\n"); err != nil {
		q.finish(serval.Complete{Error: err.Error()})
		return err
	}
	return nil
}

// Close lets the sequence go, and with it any scope of it still unanswered.
func (s *appSet) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	live := s.live
	s.live = nil
	s.mu.Unlock()

	for _, q := range live {
		q.hungUp(serval.Complete{Error: "this data set has been closed"})
	}
}

// asked and forget keep the source's record of which scopes are outstanding.
func (h *ApplicationSource) asked(q *appScope) {
	h.mu.Lock()
	h.opening = append(h.opening, q)
	h.mu.Unlock()
}

func (h *ApplicationSource) forget(q *appScope) {
	h.mu.Lock()
	for i, o := range h.opening {
		if o == q {
			h.opening = append(h.opening[:i], h.opening[i+1:]...)
			break
		}
	}
	if q.id != 0 {
		delete(h.byID, q.id)
	}
	h.mu.Unlock()

	set := q.set
	set.mu.Lock()
	for i, o := range set.live {
		if o == q {
			set.live = append(set.live[:i], set.live[i+1:]...)
			break
		}
	}
	set.mu.Unlock()
}

// Inbound hands over one statement the application said, and reports whether
// it was this source's.
//
// Three kinds arrive. Two of them answer something this end asked: the reply
// that names a sequence, and the results that fill one. The third was not asked
// for at all -- a `stale` notice, the application speaking first, because only it
// knows its records moved.
//
// Anything else belongs to somebody else on the same connection.
func (h *ApplicationSource) Inbound(stmt *wire.Statement) bool {
	switch stmt.Verb {
	case "reply":
		return h.name1(stmt)
	case wire.ResultVerb:
		return h.result(stmt)
	case wire.PlaceVerb:
		return h.place(stmt)
	case wire.StaleVerb:
		return h.stale(stmt)
	}
	return false
}

// stale takes a notice about the records this source names.
//
// **Told, never decided.** Nothing here polls the application and nothing works
// out for itself that a record has moved; the application says so, and what it
// says is let go of. A notice for another source's name is not this one's, which
// is what lets one connection serve several.
//
// What it costs here is a re-read, and that is the whole of it at this layer: an
// ApplicationSource holds no records -- it asks for them and hands them on -- so
// there is nothing of its own to forget. Whoever is reading is told an answer has
// landed, reads again, and the screen corrects. A layer above that DOES hold
// something hears the notice through OnStale and forgets what it should.
//
// Invalidation causes forgetting rather than traffic everywhere else here; this
// is the one place it causes a read, because a view on screen is a standing
// question and the records behind it just changed.
func (h *ApplicationSource) stale(stmt *wire.Statement) bool {
	n, err := wire.ParseStale(stmt.Args)
	if err != nil || n.Source != h.name {
		return false
	}
	h.mu.Lock()
	told := h.onStale
	h.mu.Unlock()
	for _, tell := range told {
		tell(n)
	}
	h.arrived()
	return true
}

// OnStale adds something to be told when the application says records under this
// name have stopped being true.
//
// It ADDS, for the reason WhenArrived does: several layers may be holding
// something of one source, and each forgets what is its own to forget. The
// notice is handed over whole rather than acted on here, because what it COSTS
// depends on what the holder keeps -- a cache loses runs and values, an
// amendment layer loses one shadow, and this source loses nothing at all.
//
// Called on whatever thread the notice arrived on, which is the connection's
// reader. The same rule as WhenArrived: whoever cannot be touched from there is
// the one that knows so.
func (h *ApplicationSource) OnStale(tell func(*wire.Stale)) {
	if tell == nil {
		return
	}
	h.mu.Lock()
	h.onStale = append(h.onStale, tell)
	h.mu.Unlock()
}

// name1 takes the reply that names a query. Replies come back in the order the
// queries went out, so the number belongs to the oldest one still unnamed.
func (h *ApplicationSource) name1(stmt *wire.Statement) bool {
	var id uint64
	for _, a := range stmt.Args {
		if a.Name == "q" && a.Value != nil && a.Value.Kind == wire.NumberValue && a.Value.IsInt {
			id = uint64(a.Value.Int)
		}
	}
	if id == 0 {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, q := range h.opening {
		q.mu.Lock()
		unnamed := q.id == 0
		if unnamed {
			q.id = id
		}
		q.mu.Unlock()
		if unnamed {
			h.byID[id] = q
			return true
		}
	}
	return false
}

// result takes one record, or the statement that ends a scope.
func (h *ApplicationSource) result(stmt *wire.Statement) bool {
	q := h.addressed(stmt)
	if q == nil {
		return false
	}
	q.take(stmt.Args[1:])
	return true
}

// place takes a place statement to the sequence it belongs to, the same way a
// result is taken.
func (h *ApplicationSource) place(stmt *wire.Statement) bool {
	q := h.addressed(stmt)
	if q == nil {
		return false
	}
	q.takePlace(stmt.Args[1:])
	return true
}

// addressed is the sequence a result or a place is for, and nil for a statement
// that names none of ours.
func (h *ApplicationSource) addressed(stmt *wire.Statement) *appScope {
	if len(stmt.Args) == 0 {
		return nil
	}
	a := stmt.Args[0]
	if a.Value == nil || a.Name != "" || a.Value.Kind != wire.NumberValue || !a.Value.IsInt {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.byID[uint64(a.Value.Int)]
}

// takePlace reads one place statement into the sink waiting for it, where that
// sink has somewhere to put one.
//
// **A sink that has not is not given them**, and loses nothing by it: every
// record still arrives as a result, and the scope's own terminator still ends
// the answer. Which is why nothing here negotiates -- a place is dropped on the
// floor exactly as a reader that did not know the verb would drop it.
//
// The completion riding a place ends the ORDER and not the scope, so it never
// finishes anything: the records are still coming.
func (q *appScope) takePlace(args []*wire.Arg) {
	r, err := wire.ParsePlace(args)
	if err != nil {
		q.finish(serval.Complete{Error: err.Error()})
		return
	}
	if r.Ordered {
		q.out.Ordered()
	}
	p, ok := q.out.(serval.Placing)
	if !ok {
		return
	}
	if r.ID != nil {
		if q.extend {
			q.held[serval.Key(wire.AsData(r.ID))] = r.Fields
		}
		_ = p.Place(wire.AsData(r.ID), r.Fields)
	}
	if r.Complete != nil {
		p.Placed(*r.Complete)
	}
}

// take reads one result statement into the sink waiting for it.
//
// Order, record and end are taken in that order, whichever statement they
// arrived on: an answer of one record carries all three on one line, and an
// answer of many spreads them out, and neither end has to know which.
func (q *appScope) take(args []*wire.Arg) {
	r, err := wire.ParseResult(args)
	if err != nil {
		q.finish(serval.Complete{Error: err.Error()})
		return
	}
	if r.Ordered {
		// The declaration that leads an answer, which is what lets whoever is
		// reading act on the records as they arrive.
		q.out.Ordered()
	}
	if r.ID != nil {
		// The application said which it sent, and that is passed on as it
		// stands: this source claims nothing about the records it relays
		// beyond what the far end claimed about them.
		//
		// Under extend it leans on its place, so what the place carried goes
		// back on here -- which is the holding this end promised by asking.
		if q.extend {
			key := serval.Key(wire.AsData(r.ID))
			if was, ok := q.held[key]; ok {
				r.Fields = append(append(serval.Record(nil), was...), r.Fields...)
				delete(q.held, key)
			}
		}
		if r.Whole {
			_ = q.out.Record(wire.AsData(r.ID), r.Fields)
		} else {
			// How many members the record has is part of what the application
			// claimed, and travels with the subset that needs it.
			_ = q.out.Subset(wire.AsData(r.ID), r.Fields, r.Has)
		}
	}
	if r.Complete != nil {
		q.finish(*r.Complete)
	}
}

// finish hands the sink what ended its scope, once, and lets the query go.
//
// The query is destroyed with it. What keeps one alive past its answer is the
// application knowing that these records are still being held -- which is what
// invalidation will need and nothing yet does, so for now the honest thing is
// to say at once that they may be let go.
// finish ends a scope: with the answer where one came, and with a reason where the
// reader hung up instead.
//
// `answered` is what separates the two, and it decides whether anybody is told.
// **Hanging up is not news.** The tree's descent opens a level, reads it and
// closes it in one breath -- a synchronous source having filled the sink by then --
// so over a source that answers LATER the close arrives first and the scope ends
// unanswered. Calling that an arrival made the reader re-read, which opened the
// level again, which closed it again: eight hundred thousand queries in twelve
// seconds, and not one of them answered.
func (q *appScope) finishWith(done serval.Complete, answered bool) {
	q.mu.Lock()
	if q.done {
		q.mu.Unlock()
		return
	}
	q.done = true
	id := q.id
	q.mu.Unlock()

	q.set.src.forget(q)
	q.out.Done(done)
	if id != 0 {
		_ = q.set.src.send(fmt.Sprintf("destroy %d\nend\n", id))
	}
	// The sink is full, so whoever asked can read again. Said HERE and not per
	// record: a hundred thousand records would be a hundred thousand notices and
	// a hundred thousand re-reads, and a scope is what was asked for anyway --
	// asked once and answered once.
	if answered {
		q.set.src.arrived()
	}
}

// finish is a scope ending because its answer came.
func (q *appScope) finish(done serval.Complete) { q.finishWith(done, true) }

// hungUp is a scope ending because the reader let it go, which nobody is told
// about: there is nothing new to read, and saying so would send a reader back for
// an answer that is not coming.
func (q *appScope) hungUp(done serval.Complete) { q.finishWith(done, false) }

// WhenArrived adds something to be told once an answer has landed
// (serval.Arriving).
//
// It ADDS rather than replaces, because two trinkets naming one source read one
// object and both want telling.
func (h *ApplicationSource) WhenArrived(tell func()) {
	if tell == nil {
		return
	}
	h.mu.Lock()
	h.tells = append(h.tells, tell)
	h.mu.Unlock()
}

// arrived says a scope's answer has landed.
//
// **On whatever thread the records came in on**, which is the connection's
// reader. Nothing here knows what that means for whoever is listening: a view
// posts the re-read to the thread that owns its state, and a test calls it where
// it stands. See serval's arriving.go for why that is the reader's business.
//
// Told outside the lock, because what a listener does is its own -- a view that
// re-reads will open a sequence on this very source, and holding the lock while
// it did would be holding one lock and reaching for it again.
//
// **A notice arriving while listeners are still being told is one more ROUND, not
// a nested one.** A listener re-reads; a re-read asks a question; a question that
// finishes at once -- one that failed, or one a cache answered -- lands before the
// first telling has returned. Nested, that is unbounded recursion: it overflowed
// the stack the first time a tree read a bundle over one of these, because the
// re-read opened another sequence while the first Open was still on the way in.
//
// So the second arrival is remembered and told after the first round finishes,
// which also collapses a burst of them into one. What a reader wants is to know
// there is something new, and being told twice about two things is no better than
// being told once.
func (h *ApplicationSource) arrived() {
	h.mu.Lock()
	if h.firing {
		h.again = true
		h.mu.Unlock()
		return
	}
	h.firing = true
	for {
		tells := make([]func(), len(h.tells))
		copy(tells, h.tells)
		h.mu.Unlock()
		for _, tell := range tells {
			tell()
		}
		h.mu.Lock()
		if !h.again {
			break
		}
		h.again = false
	}
	h.firing = false
	h.mu.Unlock()
}

// Statements reads a run of wire text and hands each statement to Inbound,
// which is what a reader of the connection does with what it gets.
func (h *ApplicationSource) Statements(src string) {
	script, err := wire.Parse(src)
	if err != nil {
		return
	}
	for _, stmt := range script.Statements {
		h.Inbound(stmt)
	}
}
