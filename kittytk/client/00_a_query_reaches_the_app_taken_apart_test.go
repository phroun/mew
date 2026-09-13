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
	s, err := c.HostSource("files", fill)
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
	send(t, c, `q=new query source="files" sort={ name natural } have=0 need=30`)

	if got := r.since(0)[0]; got != "reply q=1" {
		t.Errorf("the reply was %q, want %q", got, "reply q=1")
	}
	if served == nil {
		t.Fatal("the source was never asked for a window")
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
		_ = f.Record(17, wire.Named("name", "src/parser.go"), wire.Named("size", 1024))
		_ = f.Record(42, wire.Named("name", "src/window.go"), wire.Named("size", 2048))
		_ = f.Done(wire.Fields{wire.Named("name", "src/window.go"), wire.Named(wire.KeyField, 42)})
	})
	send(t, c, `q=new query source="files" sort={ name natural } have=0 need=30`)

	got := strings.Join(r.since(0), "\n")
	want := "reply q=1\n" +
		`result 1 fields={ key 17; name "src/parser.go"; size 1024 }` + "\n" +
		`result 1 fields={ key 42; name "src/window.go"; size 2048 }` + "\n" +
		`result 1 complete ordered watermark={ name "src/window.go"; key 42 }`
	if got != want {
		t.Errorf("the answer was\n%s\nwant\n%s", got, want)
	}
}

// A window arrives as a struct. Nothing in the handler parses anything.
func TestAWindowArrivesTakenApart(t *testing.T) {
	var got *Fill
	c, _, _ := serveOne(t, func(f *Fill) {
		got = f
		_ = f.Exhausted()
	})
	send(t, c, `q=new query source="files" sort={ name natural } have=0 need=1`)
	send(t, c, `query 1 from={ name "README.md"; key 17 } to={ name "build.sh"; key 42 } have=30 need=50 fields={ name; size }`)

	if got.Have != 30 || got.Need != 50 {
		t.Errorf("have=%d need=%d, want 30 and 50", got.Have, got.Need)
	}
	if v := got.From.Get("name"); v == nil || v.Str != "README.md" {
		t.Errorf("from names %#v", v)
	}
	if v := got.From.Key(); v == nil || v.Int != 17 {
		t.Errorf("from's key is %#v", v)
	}
	if v := got.To.Key(); v == nil || v.Int != 42 {
		t.Errorf("to's key is %#v", v)
	}
	if names := strings.Join(got.Fields.Names(), ","); names != "name,size" {
		t.Errorf("this window wants %q", names)
	}
	// The sequence comes with it, so the handler need not have kept it.
	if got.Spec.Source != "files" || len(got.Spec.Sort) != 1 {
		t.Errorf("the spec came through as %#v", got.Spec)
	}
}

// A display may address a query it opened in the same batch, without waiting
// for the reply that names it.
func TestAQueryIsAddressableInTheBatchThatMadeIt(t *testing.T) {
	var asked []int
	c, _, _ := serveOne(t, func(f *Fill) {
		asked = append(asked, f.Need)
		_ = f.Exhausted()
	})
	send(t, c, "q=new query source=\"files\" have=0 need=5\nquery q have=5 need=10")

	if len(asked) != 2 || asked[0] != 5 || asked[1] != 10 {
		t.Errorf("the source was asked for %v", asked)
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
	send(t, c, `q=new query source="files" have=0 need=10`)

	got := strings.Join(r.since(n+1), "\n")
	want := "result 1 fields={ key \"a\" }\n" +
		"result 1 complete exhausted"
	if got != want {
		t.Errorf("the answer was\n  %s\nwant\n  %s", got, want)
	}
}

// A refusal is an answer.
func TestARefusalIsAnAnswer(t *testing.T) {
	c, r, _ := serveOne(t, func(f *Fill) {
		_ = f.Fail("no records past %q", "build.sh")
	})
	n := r.count()
	send(t, c, `q=new query source="files" have=0 need=10`)

	got := strings.Join(r.since(n+1), "\n")
	want := `result 1 complete error="no records past \"build.sh\""`
	if got != want {
		t.Errorf("the refusal was\n  %s\nwant\n  %s", got, want)
	}
}

// Nothing is answered twice.
func TestAnAnsweredWindowRefusesMore(t *testing.T) {
	var second, third error
	c, _, _ := serveOne(t, func(f *Fill) {
		_ = f.Exhausted()
		second = f.Record(1)
		third = f.Done(nil)
	})
	send(t, c, `q=new query source="files" have=0 need=1`)
	if second == nil || third == nil {
		t.Errorf("a finished window took more: record=%v done=%v", second, third)
	}
}

// The answer goes out as it accumulates rather than all at the end, so a
// window larger than one message is neither held in memory nor one
// uninterruptible stretch of work.
func TestALongAnswerGoesOutInBatches(t *testing.T) {
	const records = 400
	c, r, _ := serveOne(t, func(f *Fill) {
		f.Ordered()
		for i := 0; i < records; i++ {
			_ = f.Record(i, wire.Named("name", fmt.Sprintf("file-%03d-%s", i, strings.Repeat("x", 80))))
		}
		_ = f.Exhausted()
	})
	n := r.count()
	send(t, c, `q=new query source="files" have=0 need=400`)

	batches := r.since(n + 1) // past the reply
	if len(batches) < 2 {
		t.Fatalf("the whole answer went in %d message(s); it was meant to stream", len(batches))
	}
	// Every record still arrives, once, in order, and the terminator is last.
	all := strings.Split(strings.Join(batches, "\n"), "\n")
	if len(all) != records+1 {
		t.Fatalf("%d statements for %d records and a terminator", len(all), records)
	}
	for i := 0; i < records; i++ {
		if !strings.Contains(all[i], fmt.Sprintf("key %d;", i)) {
			t.Fatalf("statement %d is %.60s...", i, all[i])
		}
	}
	if !strings.HasPrefix(all[records], "result 1 complete") {
		t.Errorf("the last statement is %.60s...", all[records])
	}
}

// The display restating the sequence is a new generation of the same query:
// the spec changes underneath, and the next window carries the new one.
func TestTheDisplayCanRestateTheSequence(t *testing.T) {
	var told *wire.Spec
	var atWindow *wire.Spec
	c, _, s := serveOne(t, func(f *Fill) {
		atWindow = f.Spec
		_ = f.Exhausted()
	})
	s.OnRespec(func(_ *Query, spec *wire.Spec) { told = spec })

	send(t, c, `q=new query source="files" sort={ name natural } have=0 need=1`)
	send(t, c, `set 1 sort={ size desc; name fold } filter={ ge size 1024 }`)
	if told == nil {
		t.Fatal("the application was not told the sequence changed")
	}
	if len(told.Sort) != 2 || told.Sort[0].Field != "size" || !told.Sort[0].Descending {
		t.Errorf("the new sort came through as %#v", told.Sort)
	}
	if told.Filter == nil || len(told.Filter.Children) != 1 ||
		told.Filter.Children[0].Op != wire.OpGe {
		t.Errorf("the new filter came through as %#v", told.Filter)
	}

	send(t, c, `query 1 have=0 need=1`)
	if atWindow == nil || len(atWindow.Sort) != 2 {
		t.Errorf("the window carried the old spec: %#v", atWindow)
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

	send(t, c, `q=new query source="files" have=0 need=1`)
	send(t, c, "destroy 1")
	if !dropped {
		t.Error("the application was not told the query was let go")
	}
	if c.Query(1) != nil {
		t.Error("the connection is still serving it")
	}
	send(t, c, `query 1 have=0 need=1`)
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

	send(t, c, `q=new query source="files" have=0 need=1`)
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
	send(t, c, `q=new query source="ledgers" have=0 need=1`)

	got := r.since(0)[0]
	if !strings.HasPrefix(got, "error text=") || !strings.Contains(got, "ledgers") {
		t.Errorf("the batch was answered with\n  %s", got)
	}
	if len(c.Queries()) != 0 {
		t.Error("a query was made for a source that is not served")
	}
}
