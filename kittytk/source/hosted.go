package source

// A source whose records are an application's.
//
// The other kind. `PSL` reads records that are here; this one asks for them
// over a connection, in the query/result exchange docs/hosting-a-query.md
// spells out, and hands them on as they arrive. Both are one interface, so
// whatever asks the question does not know which kind answered it.
//
// Nothing is waited for. A stretch is asked for by writing a statement, and
// the results come back later on whatever thread reads the connection;
// `Inbound` is where they are handed in, and the sink is fed from there.

import (
	"fmt"
	"sync"

	"github.com/phroun/kittytk/wire"
)

// A Hosted source names a body of records an application serves, and the way
// to reach that application.
//
// Send writes one batch of statements. Whatever reads the connection hands
// every statement the application says back to Inbound; the ones addressed to
// a query this source opened are taken, and the rest are somebody else's.
type Hosted struct {
	name string
	send func(src string) error

	mu   sync.Mutex
	open []*hostedSet // opened, in the order their replies are owed
	byID map[uint64]*hostedSet
}

// NewHosted is a source backed by the application's records under this name.
func NewHosted(name string, send func(src string) error) *Hosted {
	return &Hosted{name: name, send: send, byID: map[uint64]*hostedSet{}}
}

// Name is what the application serves these records under.
func (h *Hosted) Name() string { return h.name }

// Open states a sequence. Nothing is said on the wire yet: a query is opened
// with the first stretch of it, because there is no reason to name a sequence
// nobody is reading.
func (h *Hosted) Open(spec *wire.Spec) (ResultSet, error) {
	if spec == nil {
		spec = &wire.Spec{}
	}
	if h.send == nil {
		return nil, fmt.Errorf("this source has no connection to ask")
	}
	stated := *spec
	stated.Source = h.name
	return &hostedSet{src: h, spec: &stated}, nil
}

// A hostedSet is one sequence the application is serving.
//
// Its id is the application's, and it does not exist until the application
// replies with it -- so a stretch asked for before that reply arrives is
// addressed by the key the query was opened under, which the same batch
// surfaces (docs/hosting-a-query.md).
type hostedSet struct {
	src  *Hosted
	spec *wire.Spec

	mu      sync.Mutex
	opened  bool
	closed  bool
	id      uint64
	pending []Sink   // sinks awaiting an answer, oldest first
	held    []string // stretches asked for before the id came back
}

// Fill asks the application for one stretch and returns. The records reach the
// sink when the application sends them.
func (s *hostedSet) Fill(f *wire.Fill, out Sink) error {
	if out == nil {
		return fmt.Errorf("a fill needs somewhere to put the answer")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("this result set has been closed")
	}
	var stmt string
	opening := !s.opened
	switch {
	case opening:
		// Opening carries the first stretch, which is one statement.
		s.opened = true
		stmt = "q=new query " + s.spec.Encode() + " " + f.Encode()
	case s.id != 0:
		stmt = fmt.Sprintf("%s %d %s", wire.QueryVerb, s.id, f.Encode())
	default:
		// The application names the sequence, and its reply has not come back
		// yet. The key it was opened under reaches it only inside the batch
		// that opened it -- a later batch naming `q` reaches an application
		// that has never heard of it -- so the request waits for the number.
		s.held = append(s.held, f.Encode())
	}
	s.pending = append(s.pending, out)
	s.mu.Unlock()

	if stmt == "" {
		return nil
	}

	// Outside the sequence's own lock, because the source takes its lock and
	// then each sequence's: holding one while reaching for the other in the
	// opposite order is how two threads stop dead.
	if opening {
		s.src.enqueue(s)
	}

	if err := s.src.send(stmt + "\nend\n"); err != nil {
		s.fail(err.Error())
		return err
	}
	return nil
}

// Close lets the sequence go, which is how the application learns this reader
// has finished.
func (s *hostedSet) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	id, opened := s.id, s.opened
	s.mu.Unlock()

	s.src.forget(s)
	if opened && id != 0 {
		_ = s.src.send(fmt.Sprintf("destroy %d\nend\n", id))
	}
}

// enqueue and forget keep the source's record of which sequences are open.
func (h *Hosted) enqueue(s *hostedSet) {
	h.mu.Lock()
	h.open = append(h.open, s)
	h.mu.Unlock()
}

func (h *Hosted) forget(s *hostedSet) {
	h.mu.Lock()
	for i, o := range h.open {
		if o == s {
			h.open = append(h.open[:i], h.open[i+1:]...)
			break
		}
	}
	if s.id != 0 {
		delete(h.byID, s.id)
	}
	h.mu.Unlock()
}

// Inbound hands over one statement the application said, and reports whether
// it was this source's.
//
// Two kinds arrive: the reply that names a sequence, and the results that fill
// one. Anything else belongs to somebody else on the same connection.
func (h *Hosted) Inbound(stmt *wire.Statement) bool {
	switch stmt.Verb {
	case "reply":
		return h.name1(stmt)
	case wire.ResultVerb:
		return h.result(stmt)
	}
	return false
}

// name1 takes the reply that names a sequence. A reply carrying no id belongs
// to a stretch of one already named, and says nothing this source needs.
func (h *Hosted) name1(stmt *wire.Statement) bool {
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
	var named *hostedSet
	for _, s := range h.open {
		s.mu.Lock()
		unnamed := s.id == 0
		if unnamed {
			s.id = id
		}
		s.mu.Unlock()
		if unnamed {
			h.byID[id] = s
			named = s
			break
		}
	}
	h.mu.Unlock()
	if named == nil {
		return false
	}
	named.release()
	return true
}

// release asks for the stretches that were waiting for the sequence to be
// named, now that it has a number to address.
func (s *hostedSet) release() {
	s.mu.Lock()
	held, id := s.held, s.id
	s.held = nil
	s.mu.Unlock()
	for _, args := range held {
		if err := s.src.send(fmt.Sprintf("%s %d %s\nend\n", wire.QueryVerb, id, args)); err != nil {
			s.fail(err.Error())
			return
		}
	}
}

// result takes one record, or the statement that ends a stretch.
func (h *Hosted) result(stmt *wire.Statement) bool {
	if len(stmt.Args) == 0 {
		return false
	}
	a := stmt.Args[0]
	if a.Value == nil || a.Name != "" || a.Value.Kind != wire.NumberValue || !a.Value.IsInt {
		return false
	}
	h.mu.Lock()
	s := h.byID[uint64(a.Value.Int)]
	h.mu.Unlock()
	if s == nil {
		return false
	}
	s.take(stmt.Args[1:])
	return true
}

// take reads one result statement into the sink waiting for it.
func (s *hostedSet) take(args []*wire.Arg) {
	var (
		bag      wire.Fields
		done     Complete
		complete bool
		ordered  bool
	)
	for _, a := range args {
		switch {
		case a.Name == wire.ResultComplete && a.Value == nil:
			complete = true
		case a.Name == "ordered" && a.Value == nil:
			ordered = true
		case a.Name == "exhausted" && a.Value == nil:
			done.Exhausted = true
		case a.Name == "fields" && a.Value != nil:
			bag, _ = wire.ParseFields(a.Value)
		case a.Name == "watermark" && a.Value != nil:
			done.Watermark, _ = wire.ParseFields(a.Value)
		case a.Name == "error" && a.Value != nil:
			done.Error = a.Value.Str
		}
	}

	sink := s.waiting()
	if sink == nil {
		return // nothing is waiting for this, so nobody wants it
	}
	if !complete {
		switch {
		case ordered:
			// The declaration that leads an answer, which is what lets whoever
			// is reading act on the records as they arrive.
			sink.Ordered()
		case len(bag) > 0:
			_ = sink.Record(bag.Key(), withoutKey(bag))
		}
		return
	}
	s.finish(sink, done)
}

// waiting is the sink the next result belongs to: answers come back in the
// order the stretches were asked for, one ordered stream.
func (s *hostedSet) waiting() Sink {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) == 0 {
		return nil
	}
	return s.pending[0]
}

// finish hands the sink what ended its stretch and takes it off the queue.
func (s *hostedSet) finish(sink Sink, done Complete) {
	s.mu.Lock()
	if len(s.pending) > 0 && s.pending[0] == sink {
		s.pending = s.pending[1:]
	}
	s.mu.Unlock()
	sink.Done(done)
}

// fail ends every stretch still waiting, which is what a connection that will
// not carry the question leaves them needing.
func (s *hostedSet) fail(why string) {
	s.mu.Lock()
	waiting := s.pending
	s.pending = nil
	s.mu.Unlock()
	for _, sink := range waiting {
		sink.Done(Complete{Error: why})
	}
}

// withoutKey is a record's fields with the key left out, the key travelling
// beside them rather than among them.
func withoutKey(bag wire.Fields) wire.Fields {
	out := make(wire.Fields, 0, len(bag))
	for _, a := range bag {
		if a.Name != wire.KeyField {
			out = append(out, a)
		}
	}
	return out
}

// Statements reads a run of wire text and hands each statement to Inbound,
// which is what a reader of the connection does with what it gets.
func (h *Hosted) Statements(src string) {
	script, err := wire.Parse(src)
	if err != nil {
		return
	}
	for _, stmt := range script.Statements {
		h.Inbound(stmt)
	}
}
