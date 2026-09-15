package client

// What an application has to write to serve a query, and what it never has to
// write.
//
// It never parses. The statement the display sent is taken apart before the
// handler sees it, so the handler reads fields off a struct. It never sees a
// request on the event line: `query` is answered by `result`, `ask` by
// `answer`, `sub` by `event`, and nothing carries two of them. And it never
// has to hold the whole answer: records go out in batches as they accumulate.

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/phroun/kittytk/wire"
	"github.com/phroun/serval"
)

// recorder is a transport that keeps everything written through it, both the
// batches an application sends and the answers it writes back.
type recorder struct {
	mu   sync.Mutex
	sent []string
}

func (r *recorder) Exec(src string) (*wire.Reply, error) {
	r.record(src)
	return &wire.Reply{}, nil
}

// Send is the reverse direction: written, not awaited.
func (r *recorder) Send(src string) error {
	r.record(src)
	return nil
}

func (r *recorder) Close() error { return nil }

func (r *recorder) record(src string) {
	r.mu.Lock()
	r.sent = append(r.sent, src)
	r.mu.Unlock()
}

func (r *recorder) since(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sent[n:]...)
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

// serveOne sets up a connection serving one source.
func serveOne(t *testing.T, fill func(*Fill)) (*Conn, *recorder, *Source) {
	t.Helper()
	r := &recorder{}
	c := NewWithTransport(r, nil)
	s, err := c.ProvideSource("files", fill)
	if err != nil {
		t.Fatal(err)
	}
	return c, r, s
}

// send delivers one batch the way the transport would.
func send(t *testing.T, c *Conn, src string) {
	t.Helper()
	script, err := wire.Parse(src)
	if err != nil {
		t.Fatalf("parsing %q: %v", src, err)
	}
	c.InboundBatch(script.Statements)
}

// Registering a source says nothing on the wire: it is a name, not an object.
func TestRegisteringASourceSaysNothing(t *testing.T) {
	_, r, s := serveOne(t, func(*Fill) {})
	if s.Name() != "files" {
		t.Errorf("the source is called %q", s.Name())
	}
	if n := r.count(); n != 0 {
		t.Errorf("registering a source wrote %d message(s): %v", n, r.since(0))
	}
}

// The display opens the query and the application names it, because every id
// in every statement that follows is the application's own.
func TestTheApplicationNamesTheQuery(t *testing.T) {
	var served *Fill
	c, r, _ := serveOne(t, func(f *Fill) {
		served = f
		_ = f.Exhausted()
	})
	send(t, c, `q=new query source="files" sort={ name natural } count=30`)

	if got := r.since(0)[0]; got != "reply q=1" {
		t.Errorf("the reply was %q, want %q", got, "reply q=1")
	}
	if served == nil {
		t.Fatal("the source was never asked for a scope")
	}
	if served.Query.ID() != 1 {
		t.Errorf("the query is id %d", served.Query.ID())
	}
	if q := c.Query(1); q == nil || q.Source().Name() != "files" {
		t.Errorf("the connection is not serving it: %#v", q)
	}
}

// Opening is one statement because the display never wants a sequence without
// wanting rows of it, and the reply goes out before any record, because the
// reply is what names a query the display has not heard of yet.
func TestTheReplyComesBeforeTheRecords(t *testing.T) {
	c, r, _ := serveOne(t, func(f *Fill) {
		f.Ordered()
		_ = f.Record(17, serval.Named("name", "src/parser.go"), serval.Named("size", 1024))
		_ = f.Record(42, serval.Named("name", "src/window.go"), serval.Named("size", 2048))
		_ = f.Filled(42)
	})
	send(t, c, `q=new query source="files" sort={ name natural } count=30`)

	got := strings.Join(r.since(0), "\n")
	want := "reply q=1\n" +
		// The order is declared before the records rather than after them,
		// which is the only place a far end can act on it -- so it rides on
		// the first of them rather than costing a line of its own.
		`result 1 ordered id=17 record={ name "src/parser.go"; size 1024 }` + "\n" +
		// And the terminator rides on the last, for the same reason.
		`result 1 id=42 record={ name "src/window.go"; size 2048 } complete watermark=42 filled`
	if got != want {
		t.Errorf("the answer was\n%s\nwant\n%s", got, want)
	}
}

// A scope arrives as a struct. Nothing in the handler parses anything.
func TestAScopeArrivesTakenApart(t *testing.T) {
	var got *Fill
	c, _, _ := serveOne(t, func(f *Fill) {
		got = f
		_ = f.Exhausted()
	})
	send(t, c, `q=new query source="files" sort={ name natural } `+
		`fields={ name; size } after=17 until=42 count=50 reversed`)

	if got.Count != 50 {
		t.Errorf("count=%d, want 50", got.Count)
	}
	if v := got.After; v == nil || v.Int != 17 {
		t.Errorf("it starts after %#v", v)
	}
	if v := got.Until; v == nil || v.Int != 42 {
		t.Errorf("it stops before %#v", v)
	}
	if !got.Reversed {
		t.Error("it asked for the sequence backwards and that did not arrive")
	}
	// The sequence comes with it, so the handler need not have kept it.
	if got.Spec.Source != "files" || len(got.Spec.Sort) != 1 {
		t.Errorf("the spec came through as %#v", got.Spec)
	}
	if names := strings.Join(got.Spec.Fields.Names(), ","); names != "name,size" {
		t.Errorf("this sequence wants %q", names)
	}
}

// Nothing addresses a query that already exists but `destroy`. A query states
// its sequence and asks for its scope in one statement and is answered once;
// the next scope is a query of its own.
func TestAQueryIsAskedOnceAndAnsweredOnce(t *testing.T) {
	c, r, _ := serveOne(t, func(f *Fill) { _ = f.Exhausted() })
	send(t, c, `q=new query source="files" count=5`)
	n := r.count()
	send(t, c, `query 1 count=10`)

	got := strings.Join(r.since(n), "\n")
	if !strings.Contains(got, "answered once") {
		t.Errorf("asking an open query for more was taken as\n  %s", got)
	}
}

// Two scopes of one sequence are two queries, each naming the sequence again.
// That is what "a query does not change" costs and what it buys: no statement
// anywhere can alter one, so the id is the generation.
func TestEachScopeIsAQueryOfItsOwn(t *testing.T) {
	var asked []int
	c, r, _ := serveOne(t, func(f *Fill) {
		asked = append(asked, f.Count)
		_ = f.Exhausted()
	})
	send(t, c, "q=new query source=\"files\" count=5\n"+
		"r=new query source=\"files\" after=4 count=10")

	if len(asked) != 2 || asked[0] != 5 || asked[1] != 10 {
		t.Errorf("the source was asked for %v", asked)
	}
	if got := strings.Join(r.since(0)[:1], ""); got != "reply q=1 r=2" {
		t.Errorf("the reply was %q", got)
	}
}

// The least an implementation can do: ignore every hint, send everything, say
// so. It says nothing about order, which is what leaves the display to sort.
func TestTheSimplestAnswerIsEverythingAndExhausted(t *testing.T) {
	c, r, _ := serveOne(t, func(f *Fill) {
		_ = f.Record("a")
		_ = f.Exhausted()
	})
	n := r.count()
	send(t, c, `q=new query source="files" count=10`)

	got := strings.Join(r.since(n+1), "\n")
	want := `result 1 id="a" record={} complete exhausted`
	if got != want {
		t.Errorf("the answer was\n  %s\nwant\n  %s", got, want)
	}
}

// A record and a subset of one are two different claims, and they cross under
// two different words.
//
// The claim is what makes them worth telling apart. A whole record answers any
// question about that record, so whoever asked can keep it and answer the next
// query out of it; a subset answers the one question that asked for it. Neither
// end can work that out from the fields alone -- a record of two fields and two
// fields of a record of nine look the same -- so the answer says which it is.
func TestAWholeRecordAndASubsetCrossUnderDifferentWords(t *testing.T) {
	c, r, _ := serveOne(t, func(f *Fill) {
		_ = f.Record(17, serval.Named("name", "src/parser.go"), serval.Named("size", 1024))
		_ = f.Subset(42, serval.Totals{Named: 3}, serval.Named("name", "src/window.go"))
		_ = f.Exhausted()
	})
	n := r.count()
	send(t, c, `q=new query source="files" count=10 fields={ name }`)

	got := strings.Join(r.since(n+1), "\n")
	want := `result 1 id=17 record={ name "src/parser.go"; size 1024 }` + "\n" +
		`result 1 id=42 fields={ name "src/window.go" } map=3 complete exhausted`
	if got != want {
		t.Errorf("the answer was\n%s\nwant\n%s", got, want)
	}
}

// Both count as records, because both are one.
func TestASubsetCountsTowardsWhatWasSent(t *testing.T) {
	var sent int
	c, _, _ := serveOne(t, func(f *Fill) {
		_ = f.Record(1, serval.Named("name", "a"))
		_ = f.Subset(2, serval.Totals{Named: 3}, serval.Named("name", "b"))
		sent = f.Sent()
		_ = f.Exhausted()
	})
	send(t, c, `q=new query source="files" count=10`)

	if sent != 2 {
		t.Errorf("two records went out and it counted %d", sent)
	}
}

// A refusal is an answer.
func TestARefusalIsAnAnswer(t *testing.T) {
	c, r, _ := serveOne(t, func(f *Fill) {
		_ = f.Fail("no records past %q", "build.sh")
	})
	n := r.count()
	send(t, c, `q=new query source="files" count=10`)

	got := strings.Join(r.since(n+1), "\n")
	want := `result 1 complete error="no records past \"build.sh\""`
	if got != want {
		t.Errorf("the refusal was\n  %s\nwant\n  %s", got, want)
	}
}

// Nothing is answered twice.
func TestAnAnsweredScopeRefusesMore(t *testing.T) {
	var second, third error
	c, _, _ := serveOne(t, func(f *Fill) {
		_ = f.Exhausted()
		second = f.Record(1)
		third = f.Filled(1)
	})
	send(t, c, `q=new query source="files" count=1`)
	if second == nil || third == nil {
		t.Errorf("a finished scope took more: record=%v done=%v", second, third)
	}
}

// The answer goes out as it accumulates rather than all at the end, so a
// scope larger than one message is neither held in memory nor one
// uninterruptible piece of work.
func TestALongAnswerGoesOutInBatches(t *testing.T) {
	const records = 400
	c, r, _ := serveOne(t, func(f *Fill) {
		f.Ordered()
		for i := 0; i < records; i++ {
			_ = f.Record(i, serval.Named("name", fmt.Sprintf("file-%03d-%s", i, strings.Repeat("x", 80))))
		}
		_ = f.Exhausted()
	})
	n := r.count()
	send(t, c, `q=new query source="files" count=400`)

	batches := r.since(n + 1) // past the reply
	if len(batches) < 2 {
		t.Fatalf("the whole answer went in %d message(s); it was meant to stream", len(batches))
	}
	// The declaration of order leads on the first record, every record
	// arrives once and in order, and the terminator rides on the last.
	all := strings.Split(strings.Join(batches, "\n"), "\n")
	if len(all) != records {
		t.Fatalf("%d statements for %d records, which should carry the "+
			"declaration and the terminator between them", len(all), records)
	}
	if !strings.HasPrefix(all[0], "result 1 ordered id=0 ") {
		t.Fatalf("the answer leads with %.60s...", all[0])
	}
	for i := 0; i < records; i++ {
		if !strings.Contains(all[i], fmt.Sprintf("id=%d ", i)) {
			t.Fatalf("statement %d is %.60s...", i, all[i])
		}
	}
	if !strings.Contains(all[records-1], "complete exhausted") {
		t.Errorf("the last statement is %.60s...", all[records-1])
	}
}

// A different sequence is a different query.
//
// A query is stated when it is made and does not change, so the id IS the
// generation: results still in flight for the sort somebody just abandoned
// cannot be taken for results of the one they chose, because they are
// addressed to a different number. The display opens the replacement before it
// destroys what it is replacing, which is what keeps the source in use while
// the reader moves across.
func TestADifferentSequenceIsADifferentQuery(t *testing.T) {
	var specs []*serval.Spec
	c, r, _ := serveOne(t, func(f *Fill) {
		specs = append(specs, f.Spec)
		_ = f.Exhausted()
	})

	send(t, c, `q=new query source="files" sort={ name natural } count=1`)
	n := r.count()
	send(t, c, `r=new query source="files" sort={ size desc } count=1`)
	if answered := strings.Join(r.since(n), "\n"); !strings.Contains(answered, "reply r=2") {
		t.Errorf("the second query was not named in its own right: %q", answered)
	}
	if len(specs) != 2 {
		t.Fatalf("%d scopes were served", len(specs))
	}
	if specs[0].Sort[0].Field != "name" || specs[1].Sort[0].Field != "size" {
		t.Errorf("the two queries did not carry their own specs: %v", specs)
	}
	if len(c.Queries()) != 2 {
		t.Errorf("the application is serving %d queries", len(c.Queries()))
	}

	// And only then does the old one go.
	send(t, c, `destroy 1`)
	if c.Query(1) != nil || c.Query(2) == nil {
		t.Error("destroying the old query took the new one with it")
	}
}

// A query cannot be restated. It is the sequence it was opened with, and a
// display asking for a different one asks for a different query.
func TestAQueryCannotBeRestated(t *testing.T) {
	c, r, _ := serveOne(t, func(f *Fill) { _ = f.Exhausted() })
	send(t, c, `q=new query source="files" sort={ name natural } count=1`)

	n := r.count()
	send(t, c, `set 1 sort={ size desc }`)
	answered := strings.Join(r.since(n), "\n")
	if !strings.Contains(answered, "error") {
		t.Errorf("a restatement was accepted: %q", answered)
	}
	if q := c.Query(1); q == nil || q.Spec().Sort[0].Field != "name" {
		t.Error("the refused restatement changed the query anyway")
	}
}

// The display letting the query go is how the application learns it may drop
// the records it was holding for it.
func TestTheDisplayCanDropTheQuery(t *testing.T) {
	dropped, served := false, 0
	c, _, s := serveOne(t, func(f *Fill) {
		served++
		_ = f.Exhausted()
	})
	s.OnDropped(func(q *Query) { dropped = true })

	send(t, c, `q=new query source="files" count=1`)
	send(t, c, "destroy 1")
	if !dropped {
		t.Error("the application was not told the query was let go")
	}
	if c.Query(1) != nil {
		t.Error("the connection is still serving it")
	}
	send(t, c, `query 1 count=1`)
	if served != 1 {
		t.Errorf("a query that was let go was served %d times", served)
	}
}

// Anything this library does not understand reaches the application whole, so
// what it does not implement does not have to be added here to be reachable.
func TestWhatTheLibraryDoesNotKnowReachesTheApp(t *testing.T) {
	var got *wire.Statement
	c, _, s := serveOne(t, func(f *Fill) { _ = f.Exhausted() })
	s.OnStatement(func(_ *Query, stmt *wire.Statement) { got = stmt })

	send(t, c, `q=new query source="files" count=1`)
	send(t, c, `do 1 cover handle=3 from={ key 1 } to={ key 200 }`)
	if got == nil {
		t.Fatal("the statement reached nobody")
	}
	// Whole means whole: the target, the action word, and its three arguments.
	if got.Verb != "do" || len(got.Args) != 5 {
		t.Errorf("it arrived as %#v", got)
	}
}

// A source this application does not serve is refused, and the refusal is what
// the batch is answered with.
func TestAnUnknownSourceIsRefused(t *testing.T) {
	c, r, _ := serveOne(t, func(*Fill) {})
	send(t, c, `q=new query source="ledgers" count=1`)

	got := r.since(0)[0]
	if !strings.HasPrefix(got, "error text=") || !strings.Contains(got, "ledgers") {
		t.Errorf("the batch was answered with\n  %s", got)
	}
	if len(c.Queries()) != 0 {
		t.Error("a query was made for a source that is not served")
	}
}

// The order is declared before the records or not at all. One declared after a
// record has gone out is too late to be true of what has already crossed, so
// it is dropped rather than sent.
func TestOrderDeclaredLateIsNotSent(t *testing.T) {
	c, r, _ := serveOne(t, func(f *Fill) {
		_ = f.Record(1, serval.Named("name", "a"))
		f.Ordered() // too late
		_ = f.Exhausted()
	})
	send(t, c, `q=new query source="files" count=2`)

	answered := strings.Join(r.since(0), "\n")
	if strings.Contains(answered, "ordered") {
		t.Errorf("a late declaration went out:\n%s", answered)
	}
}

// And declaring it twice says it once.
func TestOrderIsDeclaredOnce(t *testing.T) {
	c, r, _ := serveOne(t, func(f *Fill) {
		f.Ordered()
		f.Ordered()
		_ = f.Exhausted()
	})
	send(t, c, `q=new query source="files" count=2`)

	if n := strings.Count(strings.Join(r.since(0), "\n"), "ordered"); n != 1 {
		t.Errorf("the order was declared %d times", n)
	}
}
