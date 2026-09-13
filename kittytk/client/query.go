package client

// Serving a query.
//
// Everything else in this package points one way: the application says `new`,
// `set`, `ask`, `do`, and the display raises events at it. A query points the
// other way. Only the display knows a query is wanted and what it is -- the
// sort comes from the column header somebody clicked, the filter from the
// filter box, the window from the scroll position -- so the display opens it,
// and the application, which is the end that holds the records, serves it.
//
// What an author writes is one function: given a window of the sequence,
// produce the records in it. The statement is taken apart before it gets here,
// so nothing in that function parses anything; and the answer is written into
// a sink that goes out in batches as it fills, so a million records need not be
// one message, or one uninterruptible stretch of work.
//
// It arrives nowhere near the event line. `query` is answered by `result`,
// `ask` by `answer`, and `sub` -- or an object's mere existence -- by `event`;
// nothing carries two of them, so a request for records can never be mistaken
// for something a subscription raised.
//
// docs/hosting-a-query.md is the wire spelling. wire/query.go is the structure.

import (
	"fmt"
	"strings"
	"sync"

	"github.com/phroun/kittytk/wire"
)

// flushBytes is how much answer accumulates before it goes out on its own. It
// trades write syscalls against how long a record waits: big enough that a
// window of a screenful is one message, small enough that a window of a
// million records is not held in memory.
const flushBytes = 16 << 10

// Sender is a transport that writes without waiting for a reply.
//
// The reverse direction needs it and the forward one does not: when the
// display sends a batch, the application answers it, and an answer that waited
// for an answer of its own would never be written at all.
type Sender interface {
	Send(src string) error
}

// A Source is a body of records this application can serve, under the name a
// display asks for it by.
//
// It is not an object and has no id. The application says `data="files"` on
// whatever trinket is to show it, and the display opens queries against that
// name when somebody scrolls.
type Source struct {
	c    *Conn
	name string

	mu      sync.Mutex
	fill    func(*Fill)
	respec  func(*Query, *wire.Spec)
	dropped func(*Query)
	other   func(*Query, *wire.Statement)
}

// Name is what a display asks for this source by.
func (s *Source) Name() string { return s.name }

// OnRespec registers a handler for the display restating a query's sequence: a
// re-sort, a new filter, a different set of fields. Everything the application
// cached against the old spec that was keyed by position is stale; what was
// keyed by record identity is not.
//
// A source that ignores this is still correct -- the next window carries the
// new spec -- so it is for applications with something to tear down.
func (s *Source) OnRespec(fn func(*Query, *wire.Spec)) {
	s.mu.Lock()
	s.respec = fn
	s.mu.Unlock()
}

// OnDropped registers a handler for the display letting a query go.
func (s *Source) OnDropped(fn func(*Query)) {
	s.mu.Lock()
	s.dropped = fn
	s.mu.Unlock()
}

// OnStatement registers a handler for anything else the display addresses to
// one of this source's queries: a question this library does not know, an
// action, a property it does not read. The statement arrives as it parsed.
//
// This is the seam a fuller library is built on. Coverage and invalidation
// both travel this way and neither is implemented here, so a library that
// wants them adds them without this package having to grow.
func (s *Source) OnStatement(fn func(*Query, *wire.Statement)) {
	s.mu.Lock()
	s.other = fn
	s.mu.Unlock()
}

// HostSource registers a body of records this application can serve, and what
// answers a window of it.
//
// Nothing crosses the wire here: a source is a name, not an object, and the
// display learns of it when a trinket is told `data="<name>"`.
func (c *Conn) HostSource(name string, fill func(*Fill)) (*Source, error) {
	if name == "" {
		return nil, fmt.Errorf("HostSource: a source needs a name")
	}
	if fill == nil {
		return nil, fmt.Errorf("HostSource: a source needs something to fill it")
	}
	if _, ok := c.transport.(Sender); !ok {
		return nil, fmt.Errorf("HostSource: this transport cannot carry the reverse direction")
	}
	s := &Source{c: c, name: name, fill: fill}
	c.mu.Lock()
	if c.sources == nil {
		c.sources = make(map[string]*Source)
	}
	c.sources[name] = s
	c.mu.Unlock()
	return s, nil
}

// A Query is one sequence of a source's records that a display is reading: one
// filter, one sort, and a window asked for at a time.
//
// The display opens it; the application names it, because the ids in every
// statement that follows are the application's own.
type Query struct {
	c      *Conn
	source *Source
	id     uint64

	mu   sync.Mutex
	spec *wire.Spec
}

// ID is what this application calls the query, and what the display addresses
// it by from the moment the reply carries it.
func (q *Query) ID() uint64 { return q.id }

// Source is where its records come from.
func (q *Query) Source() *Source { return q.source }

// Spec is the sequence as it currently stands. It changes when the display
// restates it, which is a new generation of the same query.
func (q *Query) Spec() *wire.Spec {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.spec
}

// Queries lists what this connection is currently serving.
func (c *Conn) Queries() []*Query {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*Query, 0, len(c.queries))
	for _, q := range c.queries {
		out = append(out, q)
	}
	return out
}

// mintID allocates an id in this application's own space.
//
// Each end names its own objects and the direction of travel says whose space
// a statement is in, so these never have to be told apart from the display's:
// a statement arriving here is about what this application holds.
func (c *Conn) mintID() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastHostedID++
	return c.lastHostedID
}

// InboundBatch runs one batch the display sent, answers it, and then produces
// whatever records it asked for.
//
// The reply goes out before any result does, because the reply is what names a
// query the display has not heard of yet. That ordering is not policy: the
// application mints the id, so it writes it before anything that carries it.
//
// A transport calls this for every inbound batch. It must not run on the
// reader: serving a window writes, and the reader has to stay free.
func (c *Conn) InboundBatch(stmts []*wire.Statement) {
	reply := &wire.Reply{IDs: map[string]uint64{}}
	keys := map[string]uint64{}
	var pending []func()
	var failed error

	for _, stmt := range stmts {
		if err := c.inbound(stmt, keys, reply, &pending); err != nil && failed == nil {
			failed = err
		}
	}
	if failed != nil {
		c.send(wire.EncodeError(failed.Error()))
	} else {
		c.send(wire.EncodeReply(reply))
	}
	for _, fn := range pending {
		fn()
	}
}

// send writes without waiting for anything back.
//
// What goes out this way is never a batch: a request is terminated by `end`
// and answered, and this end is answering. A reply, an error and a result are
// bare statements, which is exactly how the display writes its own replies and
// its events.
func (c *Conn) send(src string) {
	if s, ok := c.transport.(Sender); ok {
		_ = s.Send(src)
	}
}

// inbound routes one statement. Anything it means to answer with records is
// appended to pending rather than run, so the batch is replied to first.
func (c *Conn) inbound(stmt *wire.Statement, keys map[string]uint64,
	reply *wire.Reply, pending *[]func()) error {

	// `new query ...` makes one; `query <id> ...` asks it for another window.
	// Two verbs because they are two things: the object has a lifetime the
	// display ends with `destroy`, which is how the application learns it may
	// let the records go.
	if stmt.Verb == "new" {
		return c.open(stmt, keys, reply, pending)
	}

	id, rest, ok := hostedTarget(stmt, keys)
	if stmt.Verb == wire.QueryVerb {
		if !ok {
			return fmt.Errorf("query: expected the query to ask")
		}
		q := c.Query(id)
		if q == nil {
			return fmt.Errorf("query %d: no query of mine", id)
		}
		return q.window(rest, pending)
	}
	if !ok {
		return nil // not addressed to anything this application holds
	}
	q := c.Query(id)
	if q == nil {
		return fmt.Errorf("%s %d: no query of mine", stmt.Verb, id)
	}
	switch stmt.Verb {
	case "set":
		spec, err := wire.ParseSpec(rest)
		if err != nil {
			return fmt.Errorf("set %d: %w", id, err)
		}
		q.mu.Lock()
		q.spec = spec
		q.mu.Unlock()
		q.source.mu.Lock()
		fn := q.source.respec
		q.source.mu.Unlock()
		if fn != nil {
			*pending = append(*pending, func() { fn(q, spec) })
		}
		return nil
	case "destroy":
		c.mu.Lock()
		delete(c.queries, id)
		c.mu.Unlock()
		q.source.mu.Lock()
		fn := q.source.dropped
		q.source.mu.Unlock()
		if fn != nil {
			*pending = append(*pending, func() { fn(q) })
		}
		return nil
	}
	q.source.mu.Lock()
	fn := q.source.other
	q.source.mu.Unlock()
	if fn != nil {
		*pending = append(*pending, func() { fn(q, stmt) })
	}
	return nil
}

// open makes a query and asks it for its first window, which is one statement
// because the display never wants a sequence without wanting rows of it.
//
// The application names it. The display has no id to offer -- ids here are the
// application's own -- so the reply is what carries it back, and everything
// addressed to the query afterwards uses that number.
func (c *Conn) open(stmt *wire.Statement, keys map[string]uint64,
	reply *wire.Reply, pending *[]func()) error {

	if len(stmt.Args) == 0 || stmt.Args[0].Value != nil ||
		stmt.Args[0].Flag != wire.FlagTrue {
		return fmt.Errorf("new: expected a type")
	}
	if stmt.Args[0].Name != wire.QueryVerb {
		return fmt.Errorf("new: I host nothing called %q", stmt.Args[0].Name)
	}
	args := stmt.Args[1:]

	spec, err := wire.ParseSpec(args)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	c.mu.Lock()
	source := c.sources[spec.Source]
	c.mu.Unlock()
	if source == nil {
		return fmt.Errorf("query: I serve nothing called %q", spec.Source)
	}

	q := &Query{c: c, source: source, id: c.mintID(), spec: spec}
	c.mu.Lock()
	if c.queries == nil {
		c.queries = make(map[uint64]*Query)
	}
	c.queries[q.id] = q
	c.mu.Unlock()

	if stmt.Key != "" {
		reply.IDs[stmt.Key] = q.id
		keys[stmt.Key] = q.id
	}
	return q.window(args, pending)
}

// window takes a request for one window apart and queues serving it.
func (q *Query) window(args []*wire.Arg, pending *[]func()) error {
	req, err := wire.ParseFill(args)
	if err != nil {
		return fmt.Errorf("query %d: %w", q.id, err)
	}
	q.mu.Lock()
	spec := q.spec
	q.mu.Unlock()
	q.source.mu.Lock()
	fn := q.source.fill
	q.source.mu.Unlock()

	f := &Fill{Fill: req, Query: q, Spec: spec}
	*pending = append(*pending, func() { fn(f) })
	return nil
}

// Query is the query this connection serves under an id, and nil for an id it
// serves nothing under.
func (c *Conn) Query(id uint64) *Query {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.queries[id]
}

// hostedTarget reads the object a statement is addressed to: a bare id, or a
// key this same batch surfaced -- which is what lets a display open a query and
// address it again without waiting for the reply.
func hostedTarget(stmt *wire.Statement, keys map[string]uint64) (uint64, []*wire.Arg, bool) {
	if len(stmt.Args) == 0 {
		return 0, nil, false
	}
	a := stmt.Args[0]
	if a.Value != nil && a.Name == "" && a.Value.Kind == wire.NumberValue &&
		a.Value.IsInt && a.Value.Int >= 0 {
		return uint64(a.Value.Int), stmt.Args[1:], true
	}
	if a.Value == nil && a.Flag == wire.FlagTrue {
		if id, ok := keys[a.Name]; ok {
			return id, stmt.Args[1:], true
		}
	}
	return 0, nil, false
}

// A Fill is one window of the sequence, asked for -- and where the records
// that answer it are written.
//
// The reading side is what was asked: From and To are where the display's own
// knowledge starts and how far it runs, Have is how much of the window it can
// fill from that, and Need is how many rows the window is. Emit every record
// of your own in (From..To], and if that does not make up the shortfall, keep
// going past To until it does.
//
// The writing side is Record, as many times as there are records, and then one
// of Done, Exhausted or Fail. Records go out in batches as they accumulate, so
// the answer may be produced over as long as it takes and interleaved with
// other work; nothing has to be held until the end.
type Fill struct {
	*wire.Fill
	Query *Query
	Spec  *wire.Spec

	mu      sync.Mutex
	buf     strings.Builder
	sent    int
	ordered bool
	closed  bool
}

// Record adds one record to the answer: its key, and the fields asked for.
//
//	f.Record(17, wire.Named("name", "src/parser.go"), wire.Named("size", 1024))
//
// The key is what identifies the record, view-independent and permanent; the
// fields are whatever this window asked for, which may be fewer than the
// query's own list when the display wants the skeleton of a wide stretch.
func (f *Fill) Record(key any, fields ...*wire.Arg) error {
	bag := make(wire.Fields, 0, len(fields)+1)
	bag = append(bag, wire.Named(wire.KeyField, key))
	bag = append(bag, fields...)
	return f.emit(f.result(&wire.Arg{Name: "fields", Value: bag.Block()}))
}

// Ordered declares that the records are being sent in the query's own order.
//
// It is the one hint that cannot be left unsaid and assumed, because it
// changes what the display does with what arrives: ordered, it merges the
// records as they stand; unordered, it sorts them first. Saying nothing means
// unordered, which is always safe.
func (f *Fill) Ordered() {
	f.mu.Lock()
	f.ordered = true
	f.mu.Unlock()
}

// Done finishes the answer with a watermark: there is nothing of mine between
// where you asked from and this point that you do not now have.
//
// It is a completeness guarantee rather than a position, and it is what lets
// the display shrink the window, grow it back and scroll inside it without
// asking anything.
func (f *Fill) Done(watermark wire.Fields) error {
	var extra []*wire.Arg
	if len(watermark) > 0 {
		extra = append(extra, &wire.Arg{Name: "watermark", Value: watermark.Block()})
	}
	return f.finish(extra)
}

// Exhausted finishes the answer with everything there is: no watermark,
// because there is nothing past the end to be complete up to.
//
// It is what the simplest possible implementation says -- ignore every hint,
// send all your records, say this -- and it is not a toy: the display then
// holds the whole layer and asks nothing again until something invalidates it.
func (f *Fill) Exhausted() error {
	return f.finish([]*wire.Arg{{Name: "exhausted", Flag: wire.FlagTrue}})
}

// Fail finishes the answer with a refusal: this query cannot be honoured, this
// window cannot be produced, the records are gone. A refusal is an answer --
// the display carries on with what it has.
func (f *Fill) Fail(format string, args ...any) error {
	return f.finish([]*wire.Arg{wire.Named("error", fmt.Sprintf(format, args...))})
}

// Sent is how many records have gone into the answer so far.
func (f *Fill) Sent() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sent
}

// Flush sends what has accumulated without finishing the answer.
func (f *Fill) Flush() error {
	f.mu.Lock()
	src := f.take()
	f.mu.Unlock()
	if src != "" {
		f.Query.c.send(src)
	}
	return nil
}

// result builds one result statement, addressed to the query it belongs to the
// way every other statement addresses an object.
func (f *Fill) result(extra ...*wire.Arg) string {
	args := []*wire.Arg{{Value: wire.NewInt(int64(f.Query.id))}}
	return wire.EncodeStatement(&wire.Statement{
		Verb: wire.ResultVerb,
		Args: append(args, extra...),
	})
}

// emit adds one statement to the answer, sending what has accumulated once it
// is worth a message of its own.
func (f *Fill) emit(stmt string) error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return fmt.Errorf("this window has already been answered")
	}
	if f.buf.Len() > 0 {
		f.buf.WriteByte('\n')
	}
	f.buf.WriteString(stmt)
	f.sent++
	if f.buf.Len() < flushBytes {
		f.mu.Unlock()
		return nil
	}
	src := f.take()
	f.mu.Unlock()
	f.Query.c.send(src)
	return nil
}

// finish appends the terminator, closes the answer and sends the rest.
func (f *Fill) finish(extra []*wire.Arg) error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return fmt.Errorf("this window has already been answered")
	}
	// `ordered` rides on the terminator, so it can be decided after the records
	// have been produced rather than promised before.
	args := []*wire.Arg{{Name: wire.ResultComplete, Flag: wire.FlagTrue}}
	if f.ordered {
		args = append(args, &wire.Arg{Name: "ordered", Flag: wire.FlagTrue})
	}
	stmt := f.result(append(args, extra...)...)
	if f.buf.Len() > 0 {
		f.buf.WriteByte('\n')
	}
	f.buf.WriteString(stmt)
	f.closed = true
	src := f.take()
	f.mu.Unlock()
	f.Query.c.send(src)
	return nil
}

// take empties the buffer and returns what was in it. Called under the lock.
func (f *Fill) take() string {
	src := f.buf.String()
	f.buf.Reset()
	return src
}
