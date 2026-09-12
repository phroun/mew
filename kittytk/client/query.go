package client

// Hosting a query.
//
// Everything else in this package points one way: the application says `new`,
// `set`, `ask`, `do`, and the display answers with events. A query points the
// other way. The application holds records the display cannot see, so the
// display asks -- and this is where those questions arrive.
//
// What an author has to write is one function: given a window of the sequence,
// produce the records in it. The statement is taken apart before it gets here,
// so nothing in that function parses anything; and the answer is written into
// a sink that goes out in batches as it fills, so a million records need not be
// one message, or one uninterruptible stretch of work.
//
// docs/hosting-a-query.md is the wire spelling. wire/query.go is the structure.

import (
	"fmt"
	"strings"
	"sync"

	"github.com/phroun/kittytk/wire"
)

// flushBytes is how much answer accumulates before it goes out on its own. It
// trades round trips against how long a record waits: big enough that a fill
// of a screenful is one message, small enough that a fill of a million records
// is not held in memory.
const flushBytes = 16 << 10

// A Query is a sequence of an application's own records that a display is
// reading: one filter, one sort, and a window asked for at a time.
//
// The application makes it -- the display never says `new` to an application
// -- and holds it for as long as anything is looking.
type Query struct {
	c  *Conn
	id uint64

	mu      sync.Mutex
	spec    *wire.Spec
	fill    func(*Fill)
	respec  func(*wire.Spec)
	other   func(*wire.Statement)
	dropped func()
}

// ID is what the display addresses this query by.
func (q *Query) ID() uint64 { return q.id }

// Spec is the sequence as it currently stands. It changes when the display
// restates it, which is a new generation of the same query.
func (q *Query) Spec() *wire.Spec {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.spec
}

// OnRespec registers a handler for the display restating the sequence: a
// re-sort, a new filter, a different set of fields. Everything the application
// cached against the old spec that was keyed by position is stale; what was
// keyed by record identity is not.
//
// A query that ignores this is still correct -- the next fill carries the new
// spec -- so it is for applications with something to tear down.
func (q *Query) OnRespec(fn func(*wire.Spec)) {
	q.mu.Lock()
	q.respec = fn
	q.mu.Unlock()
}

// OnDropped registers a handler for the display letting the query go.
func (q *Query) OnDropped(fn func()) {
	q.mu.Lock()
	q.dropped = fn
	q.mu.Unlock()
}

// OnStatement registers a handler for anything else the display addresses to
// this query: a question this library does not know, an action, a property it
// does not read. The statement arrives as it parsed.
//
// This is the seam a fuller library is built on. Coverage and invalidation
// both travel this way and neither is implemented here, so a library that
// wants them adds them without this package having to grow.
func (q *Query) OnStatement(fn func(*wire.Statement)) {
	q.mu.Lock()
	q.other = fn
	q.mu.Unlock()
}

// Destroy tells the display the query is gone and stops answering for it.
func (q *Query) Destroy() error {
	q.c.mu.Lock()
	delete(q.c.hosted, q.id)
	q.c.mu.Unlock()
	_, err := q.c.Exec(fmt.Sprintf("destroy %d", q.id))
	return err
}

// HostQuery announces a query this application hosts and registers what
// answers its fills. The display addresses it from here on, and every question
// it puts reaches fill with the statement already taken apart.
func (c *Conn) HostQuery(spec *wire.Spec, fill func(*Fill)) (*Query, error) {
	if spec == nil {
		return nil, fmt.Errorf("HostQuery: a query needs a spec")
	}
	if fill == nil {
		return nil, fmt.Errorf("HostQuery: a query needs something to fill it")
	}
	src := "q=new " + wire.QueryType
	if args := spec.Encode(); args != "" {
		src += " " + args
	}
	reply, err := c.Exec(src)
	if err != nil {
		return nil, err
	}
	id := reply.IDs["q"]
	if id == 0 {
		return nil, fmt.Errorf("HostQuery: the display surfaced no id for the query")
	}
	q := &Query{c: c, id: id, spec: spec, fill: fill}
	c.mu.Lock()
	if c.hosted == nil {
		c.hosted = make(map[uint64]*Query)
	}
	c.hosted[id] = q
	c.mu.Unlock()
	return q, nil
}

// Hosted is the query this connection hosts under an id, and nil for an id it
// hosts nothing under.
func (c *Conn) Hosted(id uint64) *Query {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hosted[id]
}

// Inbound routes one statement the display addressed to something this
// connection hosts. A transport calls it for every such statement; it is
// exported for the same reason Deliver is, so a transport can be written
// outside this package.
//
// It must not run on the reader: answering a fill executes statements, and
// those need the reader free to route their replies.
func (c *Conn) Inbound(stmt *wire.Statement) {
	id, rest, ok := hostedTarget(stmt)
	if !ok {
		return
	}
	q := c.Hosted(id)
	if q == nil {
		return
	}
	switch stmt.Verb {
	case "ask":
		if len(rest) > 0 && rest[0].Value == nil && rest[0].Flag == wire.FlagTrue &&
			rest[0].Name == wire.AskFill {
			q.dispatchFill(rest[1:])
			return
		}
	case "set":
		spec, err := wire.ParseSpec(rest)
		if err == nil {
			q.dispatchRespec(spec)
			return
		}
	case "destroy":
		c.mu.Lock()
		delete(c.hosted, id)
		c.mu.Unlock()
		q.mu.Lock()
		fn := q.dropped
		q.mu.Unlock()
		if fn != nil {
			fn()
		}
		return
	}
	q.mu.Lock()
	other := q.other
	q.mu.Unlock()
	if other != nil {
		other(stmt)
	}
}

// hostedTarget reads the object a statement is addressed to: the leading
// operand, which is a bare id.
func hostedTarget(stmt *wire.Statement) (uint64, []*wire.Arg, bool) {
	if len(stmt.Args) == 0 {
		return 0, nil, false
	}
	a := stmt.Args[0]
	if a.Name != "" || a.Value == nil || a.Value.Kind != wire.NumberValue ||
		!a.Value.IsInt || a.Value.Int < 0 {
		return 0, nil, false
	}
	return uint64(a.Value.Int), stmt.Args[1:], true
}

// dispatchRespec records a restated sequence and tells the application.
func (q *Query) dispatchRespec(spec *wire.Spec) {
	q.mu.Lock()
	q.spec = spec
	fn := q.respec
	q.mu.Unlock()
	if fn != nil {
		fn(spec)
	}
}

// dispatchFill takes the request apart and hands it over with its sink.
func (q *Query) dispatchFill(args []*wire.Arg) {
	req, err := wire.ParseFill(args)
	q.mu.Lock()
	spec, fn := q.spec, q.fill
	q.mu.Unlock()
	f := &Fill{Fill: req, Query: q, Spec: spec}
	if err != nil {
		// The tag is in the request that would not parse, so there is nothing
		// to stamp a refusal with. Say so where it can be seen rather than
		// dropping the question on the floor.
		f.Fill = &wire.Fill{}
		_ = f.Fail("%v", err)
		return
	}
	fn(f)
}

// A Fill is one window of the sequence, asked for -- and where the answer to
// it is written.
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
// fields are whatever this fill asked for, which may be fewer than the query's
// own list when the display wants the skeleton of a wide stretch.
func (f *Fill) Record(key any, fields ...*wire.Arg) error {
	bag := make(wire.Fields, 0, len(fields)+1)
	bag = append(bag, wire.Named(wire.KeyField, key))
	bag = append(bag, fields...)
	ev := wire.NewEvent(wire.EventQueryRecord).
		WithUint("query", f.Query.id).
		WithInt("tag", int(f.Tag))
	ev.Fields = append(ev.Fields, &wire.Arg{Name: "fields", Value: bag.Block()})
	return f.emit(ev.Encode())
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
	ev := f.terminator()
	if len(watermark) > 0 {
		ev.Fields = append(ev.Fields, &wire.Arg{Name: "watermark", Value: watermark.Block()})
	}
	return f.finish(ev)
}

// Exhausted finishes the answer with everything there is: no watermark,
// because there is nothing past the end to be complete up to.
//
// It is what the simplest possible implementation says -- ignore every hint,
// send all your records, say this -- and it is not a toy: the display then
// holds the whole layer and asks nothing again until something invalidates it.
func (f *Fill) Exhausted() error {
	ev := f.terminator()
	ev.Fields = append(ev.Fields, &wire.Arg{Name: "exhausted", Flag: wire.FlagTrue})
	return f.finish(ev)
}

// Fail finishes the answer with a refusal: this query cannot be honoured, this
// window cannot be produced, the records are gone. A refusal is an answer --
// the display carries on with what it has.
func (f *Fill) Fail(format string, args ...any) error {
	ev := f.terminator()
	ev.WithString("error", fmt.Sprintf(format, args...))
	return f.finish(ev)
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
	if src == "" {
		return nil
	}
	_, err := f.Query.c.Exec(src)
	return err
}

// terminator builds the statement that ends an answer.
func (f *Fill) terminator() *wire.Event {
	f.mu.Lock()
	ordered := f.ordered
	f.mu.Unlock()
	ev := wire.NewEvent(wire.EventQueryFilled).
		WithUint("query", f.Query.id).
		WithInt("tag", int(f.Tag))
	if ordered {
		ev.Fields = append(ev.Fields, &wire.Arg{Name: "ordered", Flag: wire.FlagTrue})
	}
	return ev
}

// emit adds one statement to the answer, sending what has accumulated once it
// is worth a message of its own.
func (f *Fill) emit(stmt string) error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return fmt.Errorf("this fill has already been answered")
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
	_, err := f.Query.c.Exec(src)
	return err
}

// finish appends the terminator, closes the answer and sends the rest.
func (f *Fill) finish(ev *wire.Event) error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return fmt.Errorf("this fill has already been answered")
	}
	if f.buf.Len() > 0 {
		f.buf.WriteByte('\n')
	}
	f.buf.WriteString(ev.Encode())
	f.closed = true
	src := f.take()
	f.mu.Unlock()
	_, err := f.Query.c.Exec(src)
	return err
}

// take empties the buffer and returns what was in it. Called under the lock.
func (f *Fill) take() string {
	src := f.buf.String()
	f.buf.Reset()
	return src
}

