package client

// What an application has to write to host a query, and what it never has to
// write.
//
// It never parses. The statement the display sent is taken apart before the
// handler sees it, so the handler reads fields off a struct. And it never has
// to hold the whole answer: records go out in batches as they accumulate, so
// the answer may be produced over as long as it takes.

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/phroun/kittytk/wire"
)

// recorder is a transport that answers `new` with an id and keeps everything
// else that was sent.
type recorder struct {
	mu   sync.Mutex
	sent []string
}

func (r *recorder) Exec(src string) (*wire.Reply, error) {
	r.mu.Lock()
	r.sent = append(r.sent, src)
	r.mu.Unlock()
	if strings.HasPrefix(src, "q=new ") {
		return &wire.Reply{IDs: map[string]uint64{"q": 7}}, nil
	}
	return &wire.Reply{}, nil
}

func (r *recorder) Close() error { return nil }

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

// hostOne sets up a connection hosting one query.
func hostOne(t *testing.T, fill func(*Fill)) (*Conn, *recorder, *Query) {
	t.Helper()
	r := &recorder{}
	c := NewWithTransport(r, nil)
	spec := &wire.Spec{
		Source: "files",
		Sort:   []wire.SortLevel{{Field: "name", Level: wire.Level{Collation: wire.CollateNatural}}},
	}
	q, err := c.HostQuery(spec, fill)
	if err != nil {
		t.Fatal(err)
	}
	return c, r, q
}

// send delivers one statement the way the transport would.
func send(t *testing.T, c *Conn, src string) {
	t.Helper()
	script, err := wire.Parse(src)
	if err != nil {
		t.Fatalf("parsing %q: %v", src, err)
	}
	for _, stmt := range script.Statements {
		c.Inbound(stmt)
	}
}

// The application announces the query, and what it announced is the spec it
// was given.
func TestAQueryIsAnnouncedWithItsSpec(t *testing.T) {
	_, r, q := hostOne(t, func(*Fill) {})
	if q.ID() != 7 {
		t.Fatalf("the query is id %d, want the one the display surfaced", q.ID())
	}
	got := r.since(0)[0]
	want := `q=new query source="files" sort={ name natural }`
	if got != want {
		t.Errorf("announced\n  %s\nwant\n  %s", got, want)
	}
}

// A fill arrives as a struct. Nothing in the handler parses anything.
func TestAFillArrivesTakenApart(t *testing.T) {
	var got *Fill
	done := make(chan struct{})
	c, _, _ := hostOne(t, func(f *Fill) {
		got = f
		_ = f.Exhausted()
		close(done)
	})
	send(t, c, `ask 7 fill tag=4 from={ name "README.md"; key 17 } to={ name "build.sh"; key 42 } have=30 need=50 fields={ name; size }`)
	<-done

	if got.Tag != 4 {
		t.Errorf("tag is %d", got.Tag)
	}
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

// The answer is records and then one terminator, all stamped with the tag of
// the fill they answer.
func TestAnAnswerIsRecordsThenATerminator(t *testing.T) {
	c, r, _ := hostOne(t, func(f *Fill) {
		f.Ordered()
		_ = f.Record(17, wire.Named("name", "src/parser.go"), wire.Named("size", 1024))
		_ = f.Record(42, wire.Named("name", "src/window.go"), wire.Named("size", 2048))
		_ = f.Done(wire.Fields{wire.Named("name", "src/window.go"), wire.Named(wire.KeyField, 42)})
	})
	n := r.count()
	send(t, c, `ask 7 fill tag=9 have=0 need=2`)

	lines := strings.Split(strings.Join(r.since(n), "\n"), "\n")
	want := []string{
		`event query_record query=7 tag=9 fields={ key 17; name "src/parser.go"; size 1024 }`,
		`event query_record query=7 tag=9 fields={ key 42; name "src/window.go"; size 2048 }`,
		`event query_filled query=7 tag=9 ordered watermark={ name "src/window.go"; key 42 }`,
	}
	if len(lines) != len(want) {
		t.Fatalf("the answer was\n  %s", strings.Join(lines, "\n  "))
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d was\n  %s\nwant\n  %s", i, lines[i], want[i])
		}
	}
}

// The least an implementation can do: ignore every hint, send everything, say
// so. It says nothing about order, which is what leaves the display to sort.
func TestTheSimplestAnswerIsEverythingAndExhausted(t *testing.T) {
	c, r, _ := hostOne(t, func(f *Fill) {
		_ = f.Record("a")
		_ = f.Exhausted()
	})
	n := r.count()
	send(t, c, `ask 7 fill tag=1 have=0 need=10`)

	got := strings.Join(r.since(n), "\n")
	want := "event query_record query=7 tag=1 fields={ key \"a\" }\n" +
		"event query_filled query=7 tag=1 exhausted"
	if got != want {
		t.Errorf("the answer was\n  %s\nwant\n  %s", got, want)
	}
}

// A refusal is an answer.
func TestARefusalIsAnAnswer(t *testing.T) {
	c, r, _ := hostOne(t, func(f *Fill) {
		_ = f.Fail("no records past %q", "build.sh")
	})
	n := r.count()
	send(t, c, `ask 7 fill tag=2 have=0 need=10`)

	got := strings.Join(r.since(n), "\n")
	want := `event query_filled query=7 tag=2 error="no records past \"build.sh\""`
	if got != want {
		t.Errorf("the refusal was\n  %s\nwant\n  %s", got, want)
	}
}

// Nothing is answered twice.
func TestAnAnsweredFillRefusesMore(t *testing.T) {
	var second, third error
	c, _, _ := hostOne(t, func(f *Fill) {
		_ = f.Exhausted()
		second = f.Record(1)
		third = f.Done(nil)
	})
	send(t, c, `ask 7 fill tag=3 have=0 need=1`)
	if second == nil || third == nil {
		t.Errorf("a finished fill took more: record=%v done=%v", second, third)
	}
}

// The answer goes out as it accumulates rather than all at the end, so a fill
// larger than one message is neither held in memory nor one uninterruptible
// stretch of work.
func TestALongAnswerGoesOutInBatches(t *testing.T) {
	const records = 400
	c, r, _ := hostOne(t, func(f *Fill) {
		f.Ordered()
		for i := 0; i < records; i++ {
			_ = f.Record(i, wire.Named("name", fmt.Sprintf("file-%03d-%s", i, strings.Repeat("x", 80))))
		}
		_ = f.Exhausted()
	})
	n := r.count()
	send(t, c, `ask 7 fill tag=5 have=0 need=400`)

	batches := r.since(n)
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
	if !strings.HasPrefix(all[records], "event query_filled ") {
		t.Errorf("the last statement is %.60s...", all[records])
	}
}

// The display restating the sequence is a new generation of the same query:
// the spec changes underneath, and the next fill carries the new one.
func TestTheDisplayCanRestateTheSequence(t *testing.T) {
	var told *wire.Spec
	var atFill *wire.Spec
	c, _, q := hostOne(t, func(f *Fill) {
		atFill = f.Spec
		_ = f.Exhausted()
	})
	q.OnRespec(func(s *wire.Spec) { told = s })

	send(t, c, `set 7 sort={ size desc; name fold } filter={ ge size 1024 }`)
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

	send(t, c, `ask 7 fill tag=6 have=0 need=1`)
	if atFill == nil || len(atFill.Sort) != 2 {
		t.Errorf("the fill carried the old spec: %#v", atFill)
	}
}

// The display letting the query go stops the application answering for it.
func TestTheDisplayCanDropTheQuery(t *testing.T) {
	dropped, filled := false, false
	c, _, q := hostOne(t, func(f *Fill) {
		filled = true
		_ = f.Exhausted()
	})
	q.OnDropped(func() { dropped = true })

	send(t, c, "destroy 7")
	if !dropped {
		t.Error("the application was not told the query was let go")
	}
	send(t, c, "ask 7 fill tag=1 have=0 need=1")
	if filled {
		t.Error("a query that was let go answered anyway")
	}
}

// Anything this library does not understand reaches the application whole, so
// what it does not implement does not have to be added here to be reachable.
func TestWhatTheLibraryDoesNotKnowReachesTheApp(t *testing.T) {
	var got *wire.Statement
	c, _, q := hostOne(t, func(*Fill) {})
	q.OnStatement(func(s *wire.Statement) { got = s })

	send(t, c, `do 7 cover handle=3 from={ key 1 } to={ key 200 }`)
	if got == nil {
		t.Fatal("the statement reached nobody")
	}
	// Whole means whole: the target, the action word, and its three arguments.
	if got.Verb != "do" || len(got.Args) != 5 {
		t.Errorf("it arrived as %#v", got)
	}
}

// A statement for an id this connection hosts nothing under is not an error to
// answer; there is nothing to answer it with.
func TestAStatementForNothingHostedIsDropped(t *testing.T) {
	called := false
	c, _, _ := hostOne(t, func(*Fill) { called = true })
	send(t, c, `ask 99 fill tag=1 have=0 need=1`)
	if called {
		t.Error("a query answered for an id that is not its own")
	}
}
