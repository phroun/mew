package client

// Serving a query.
//
// Everything else in this package points one way: the application says `new`,
// `set`, `ask`, `do`, and the display raises events at it. A query points the
// other way. Only the display knows a query is wanted and what it is -- the
// sort comes from the column header somebody clicked, the filter from the
// filter box, the scope from the scroll position -- so the display opens it,
// and the application, which is the end that holds the records, serves it.
//
// What an author writes is one function: given a scope of the sequence,
// produce the records in it. The statement is taken apart before it gets here,
// so nothing in that function parses anything; and the answer is written into
// a sink that goes out in batches as it fills, so a million records need not be
// one message, or one uninterruptible piece of work.
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
	"github.com/phroun/serval"
)

// flushBytes is how much answer accumulates before it goes out on its own. It
// trades write syscalls against how long a record waits: big enough that a
// scope of a screenful is one message, small enough that a scope of a
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
	dropped func(*Query)
	other   func(*Query, *wire.Statement)
}

// Name is what a display asks for this source by.
func (s *Source) Name() string { return s.name }

// OnDropped registers a handler for the display letting a query go.
//
// Records are held against the SOURCE rather than against any one query, so
// what this says is that one reader has finished. An application that
// materialised something may let it go when the last query against that source
// has gone -- which is why a display opens the query it is replacing something
// with before it destroys the old one.
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

// ProvideSource registers a body of records this application can serve, and what
// answers a scope of it.
//
// Nothing crosses the wire here: a source is a name, not an object, and the
// display learns of it when a trinket is told `data="<name>"`.
func (c *Conn) ProvideSource(name string, fill func(*Fill)) (*Source, error) {
	if name == "" {
		return nil, fmt.Errorf("ProvideSource: a source needs a name")
	}
	if fill == nil {
		return nil, fmt.Errorf("ProvideSource: a source needs something to fill it")
	}
	if _, ok := c.transport.(Sender); !ok {
		return nil, fmt.Errorf("ProvideSource: this transport cannot carry the reverse direction")
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
// filter, one sort, and a scope asked for at a time.
//
// The display opens it; the application names it, because the ids in every
// statement that follows are the application's own.
//
// **A query does not change.** It is stated when it is made and it is that
// sequence until it is destroyed; a different sort or a different filter is a
// different query. So the id IS the generation, and results that are still in
// flight when the display changes its mind are separated from the new ones by
// the number they are addressed to rather than by where they fall in a stream.
type Query struct {
	c      *Conn
	source *Source
	id     uint64
	spec   *serval.Spec
}

// ID is what this application calls the query, and what the display addresses
// it by from the moment the reply carries it.
func (q *Query) ID() uint64 { return q.id }

// Source is where its records come from.
func (q *Query) Source() *Source { return q.source }

// Spec is the sequence this query names, which is what it was opened with.
func (q *Query) Spec() *serval.Spec { return q.spec }

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
// reader: serving a scope writes, and the reader has to stay free.
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

	// `new query ...` states a sequence and asks for one scope of it, which is
	// the whole of what a display ever asks. Nothing addresses a query that
	// already exists except `destroy`: a query is the sequence it was opened
	// with, it answers once, and what keeps it alive afterwards is the
	// application knowing these records are still held.
	if stmt.Verb == "new" {
		return c.open(stmt, keys, reply, pending)
	}

	id, _, ok := hostedTarget(stmt, keys)
	if !ok {
		return nil // not addressed to anything this application holds
	}
	q := c.Query(id)
	if q == nil {
		return fmt.Errorf("%s %d: no query of mine", stmt.Verb, id)
	}
	switch stmt.Verb {
	case wire.QueryVerb:
		return fmt.Errorf("query %d: a query is asked when it is made and "+
			"answered once; ask a new one for the next scope", id)
	case "set":
		return fmt.Errorf("set %d: a query is the sequence it was opened with "+
			"and does not change; a different sequence is a different query", id)
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

// open makes a query and asks it for its first scope, which is one statement
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
	scope, err := wire.ParseScope(args)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	q.source.mu.Lock()
	fn := q.source.fill
	q.source.mu.Unlock()

	f := &Fill{Scope: scope, Query: q, Spec: spec}
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

// A Fill is one scope of the sequence, asked for -- and where the records
// that answer it are written.
//
// The reading side is what was asked: start past After, walk the way Reversed
// says, and send Count records -- stopping early if you reach Until, which is
// a record the display already holds.
//
// The writing side is Record, as many times as there are records, and then one
// of Filled, Joined, Exhausted or Fail. Records go out in batches as they
// accumulate, so the answer may be produced over as long as it takes and
// interleaved with other work; nothing has to be held until the end.
type Fill struct {
	*serval.Scope
	Query *Query
	Spec  *serval.Spec

	mu      sync.Mutex
	buf     strings.Builder
	sent    int // statements written, which is what decides a flush
	records int // records among them, which is what Sent reports
	ordered bool
	waiting *wire.Result // the last record, held so the end can ride on it
	closed  bool
}

// Record adds one whole record to the answer: its key, and every field it has.
//
//	f.Record(17, wire.Named("name", "src/parser.go"), wire.Named("size", 1024))
//
// Whole matters beyond this answer. A record that arrived entire answers any
// question about that record, so whoever asked can keep it and use it for the
// next query as well; part of one answers only the question that asked for it.
// So say Record when these are all the fields there are, and Subset when they
// are the ones somebody asked for.
//
// The identity is what names the record, and it travels beside the fields
// rather than among them: a record is free to carry a field called `key` of
// its own, and that field is data like any other.
func (f *Fill) Record(id any, fields ...*serval.Field) error {
	return f.record(wire.RecordArg, id, serval.Totals{}, fields)
}

// Subset adds some of a record: its key, HOW MANY MEMBERS THE RECORD HAS, and
// the fields this scope asked for. It crosses as `fields={ ... } map=5 len=3`.
//
//	f.Subset(17, serval.Totals{Named: 5}, serval.Named(".name", "parser.go"))
//
// The totals are what keep a subset worth more than the one question it
// answered. Say a record has five named members and send two, and whoever asked
// knows three are missing; send the other three later and they know they now
// hold the lot. Say a record has three members standing by POSITION and they
// know `0`, `1` and `2` are all there is, so `3` is answered without anyone
// being asked -- an ordered member being named by where it stands.
//
// Count them the way serval.Tally does: members only, and an absence not among
// them. A count that said a record had no positional members while carrying one
// called `.0` would have the far end answer "not there" about a member that is,
// which is worse than saying nothing at all.
//
// **A field the record has not got is worth sending as undefined** rather than
// leaving out. Left out it reads as a field nobody asked about and gets asked
// for again; sent, it is a guarantee, and it is not counted.
func (f *Fill) Subset(id any, has serval.Totals, fields ...*serval.Field) error {
	return f.record(wire.FieldsArg, id, has, fields)
}

// record queues one record, holding it back until the next one or the end.
//
// Held back because the statement that carries the last record can carry the
// terminator too, and an answer of one record is then one line rather than
// three. Nothing waits long: the next record releases it, so does Flush, and so
// does the end. Which form went out is not something the far end reads
// differently.
func (f *Fill) record(what string, id any, has serval.Totals, fields []*serval.Field) error {
	rec := &wire.Result{
		ID:     wire.AsWire(serval.Val(id)),
		Fields: append(serval.Record(nil), fields...),
		Whole:  what == wire.RecordArg,
		Has:    has,
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return fmt.Errorf("this scope has already been answered")
	}
	held := f.waiting
	rec.Ordered = f.ordered && f.records == 0
	f.waiting = rec
	f.records++
	f.mu.Unlock()

	if held == nil {
		return nil
	}
	return f.emit(f.result(held.Args()...))
}

// Ordered declares that the records are being sent in the query's own order,
// and goes out at once, before any of them.
//
// Up front because that is the only place it is worth anything. It changes
// what the far end does with what arrives -- ordered, it merges the records as
// they stand; unordered, it sorts them first -- and a far end that does not
// learn which until the records have all gone by cannot act on either. Saying
// nothing means unordered, which is always safe.
//
// So it is said before the first record or not at all: a declaration made
// after one has gone out is too late to be true of what has already crossed,
// and is dropped rather than sent.
func (f *Fill) Ordered() {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Late is measured in records produced, not statements sent. A record held
	// back for the terminator to ride on has still been produced, and a
	// declaration made after it is still a declaration made too late.
	if f.records > 0 || f.closed || f.ordered {
		return
	}
	f.ordered = true
}

// Filled finishes the answer with the count reached, and a watermark: there is
// nothing of mine between where you asked from and this record that you do not
// now have.
//
// The watermark is a completeness guarantee rather than a position, and it is
// what the next scope is asked from. So it names a record this source sent, or
// one it is otherwise prepared to place: the display quotes it straight back as
// `after`.
func (f *Fill) Filled(watermark any) error {
	return f.finish(&serval.Complete{Stop: serval.StopFilled, Watermark: serval.Val(watermark)})
}

// Joined finishes the answer at the record the display said it already held.
// What it holds on this side and what it holds on that are now one run, and it
// need not ask over this ground again.
func (f *Fill) Joined(watermark any) error {
	return f.finish(&serval.Complete{Stop: serval.StopJoined, Watermark: serval.Val(watermark)})
}

// Exhausted finishes the answer with everything there is: no watermark,
// because there is nothing past the end to be complete up to.
//
// It is what the simplest possible implementation says -- ignore every hint,
// send all your records, say this -- and it is not a toy: the display then
// holds the whole layer and asks nothing again until something invalidates it.
func (f *Fill) Exhausted() error {
	return f.finish(&serval.Complete{Stop: serval.StopExhausted})
}

// Fail finishes the answer with a refusal: this query cannot be honoured, this
// scope cannot be produced, the records are gone. A refusal is an answer --
// the display carries on with what it has.
func (f *Fill) Fail(format string, args ...any) error {
	return f.finish(&serval.Complete{Error: fmt.Sprintf(format, args...)})
}

// Sent is how many records have gone into the answer so far.
func (f *Fill) Sent() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.records
}

// Flush sends what has accumulated without finishing the answer, the record
// being held for the terminator included -- so a source that produces a record
// every few seconds does not leave one sitting here waiting for the next.
func (f *Fill) Flush() error {
	f.mu.Lock()
	held := f.waiting
	f.waiting = nil
	f.mu.Unlock()
	if held != nil {
		_ = f.emit(f.result(held.Args()...))
	}
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
		return fmt.Errorf("this scope has already been answered")
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

// finish ends the answer, on the last record's own statement where there is
// one, closes it and sends the rest.
func (f *Fill) finish(done *serval.Complete) error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return fmt.Errorf("this scope has already been answered")
	}
	end := f.waiting
	f.waiting = nil
	if end == nil {
		end = &wire.Result{Ordered: f.ordered && f.records == 0}
	}
	end.Complete = done
	stmt := f.result(end.Args()...)
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
