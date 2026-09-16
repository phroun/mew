package source

// The same question, asked of records that are here and of records that are an
// application's.
//
// This is what the interface is for. The asking code below is written once and
// run twice, and it names neither kind: it opens a sequence, asks for scopes
// of it, and reads what arrives. A third kind is a third entry in the table.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/wire"
	"github.com/phroun/serval"
)

// The same records, said two ways: a PSL document, and the slice an
// application serves.
const twoWays = `(
  (name: "README.md", size: 2048),
  (name: "build.sh", size: 310),
  (name: "go.mod", size: 96),
  (name: "parser.go", size: 14022)
)`

var appRecords = []struct {
	key  int64
	name string
	size int64
}{
	{0, "README.md", 2048}, {1, "build.sh", 310},
	{2, "go.mod", 96}, {3, "parser.go", 14022},
}

// --- the application, and the loop that reaches it ----------------------

// loop is a connection with an application at one end and a ApplicationSource source at
// the other, wired to each other with nothing in between.
type loop struct{ src *ApplicationSource }

func (l *loop) Exec(src string) (*wire.Reply, error) {
	l.src.Statements(src)
	return &wire.Reply{}, nil
}
func (l *loop) Send(src string) error { l.src.Statements(src); return nil }
func (l *loop) Close() error          { return nil }

// serving stands up an application serving `files` and a ApplicationSource source that
// asks it for them. What the source sends reaches the application's inbound
// path, and what the application answers reaches the source.
func serving(t *testing.T) *ApplicationSource {
	t.Helper()
	return hosting(t, serveRecords)
}

// hosting is the same, for an application that answers some other way.
func hosting(t *testing.T, fill func(*client.Fill)) *ApplicationSource {
	t.Helper()
	l := &loop{}
	conn := client.NewWithTransport(l, nil)
	if _, err := conn.ProvideSource("files", fill); err != nil {
		t.Fatal(err)
	}
	l.src = NewApplicationSource("files", func(src string) error {
		script, err := wire.Parse(src)
		if err != nil {
			return err
		}
		conn.InboundBatch(script.Statements)
		return nil
	})
	return l.src
}

// serveRecords is an application honouring the sort and the scope: the whole
// of what an author writes.
func serveRecords(f *client.Fill) { serve(f, f.Record) }

// serveSubsets is the same application saying of every record that it is only
// the fields somebody asked for.
func serveSubsets(f *client.Fill) {
	// One member more than is sent, so that a subset stays a subset.
	serve(f, func(key any, fields ...*serval.Field) error {
		return f.Subset(key, serval.Totals{Named: len(fields) + 1}, fields...)
	})
}

func serve(f *client.Fill, send func(key any, fields ...*serval.Field) error) {
	rows := append([]struct {
		key  int64
		name string
		size int64
	}(nil), appRecords...)
	if len(f.Spec.Sort) > 0 && f.Spec.Sort[0].Field == ".size" {
		for i := 0; i < len(rows); i++ {
			for j := i + 1; j < len(rows); j++ {
				if rows[j].size < rows[i].size {
					rows[i], rows[j] = rows[j], rows[i]
				}
			}
		}
	}

	start := 0
	if f.After != nil {
		for start < len(rows) && rows[start].key != f.After.Int {
			start++
		}
		start++
	}

	f.Ordered()
	sent := 0
	for i := start; i < len(rows) && sent < f.Count; i++ {
		// `key` and `value` are fields under the serval.Whole reading -- the
		// document's own key for the record, handy to sort or show -- so an
		// application serving the same records carries them too. What
		// identifies the record is the first argument, beside the bag.
		_ = send(rows[i].key, serval.Named("key", rows[i].key),
			serval.Named(".name", rows[i].name), serval.Named(".size", rows[i].size))
		sent++
	}
	if start+sent >= len(rows) {
		_ = f.Exhausted()
		return
	}
	_ = f.Filled(rows[start+sent-1].key)
}

// --- the asking, written once -------------------------------------------

// A reader is one stated sequence, drawn from more than once -- which is what
// a display holds.
//
// The sequence is stated once and read from until it is let go, however many
// scopes that takes. Opening a fresh data set per scope would be a different
// sequence each time, and one that had never handed out the record the next
// scope means to resume from.
type reader struct {
	t   *testing.T
	set serval.DataSet
}

func opened(t *testing.T, src serval.Source, spec string) *reader {
	t.Helper()
	set, err := src.Open(parseSpec(t, spec))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(set.Close)
	return &reader{t: t, set: set}
}

// scope draws one scope of the sequence.
func (r *reader) scope(args string) (*collector, serval.Complete) {
	r.t.Helper()
	out := &collector{}
	if err := r.set.Read(parseScope(r.t, args), out); err != nil {
		r.t.Fatal(err)
	}
	if !out.ended {
		r.t.Fatal("the scope was never ended")
	}
	return out, out.done
}

// read is one scope of a sequence nobody reads twice, naming no kind.
func read(t *testing.T, src serval.Source, spec, scope string) (*collector, serval.Complete) {
	t.Helper()
	return opened(t, src, spec).scope(scope)
}

func TestOneQuestionTwoKinds(t *testing.T) {
	for _, kind := range []struct {
		what string
		src  serval.Source
	}{
		{"records that are here", mustPSL(t, twoWays)},
		{"records an application has", serving(t)},
	} {
		t.Run(kind.what, func(t *testing.T) {
			seq := opened(t, kind.src, "sort={ .size }")
			out, done := seq.scope("count=2")
			if out.joined() != "2,1" {
				t.Errorf("by size the first two are %s", out.joined())
			}
			if got := wire.EncodeRecord(out.fields[0]); got != `{ key 2; .name "go.mod"; .size 96 }` {
				t.Errorf("the first record carries %s", got)
			}
			if !out.ordered {
				t.Error("the answer did not say it was in order")
			}
			if done.Stop == serval.StopExhausted {
				t.Error("a scope with records past it claimed to be exhausted")
			}
			if got := done.Watermark.String(); got != "1" {
				t.Errorf("the watermark is %s", got)
			}

			// And the scope after it, from where that one stopped.
			next, done := seq.scope(
				"after=" + done.Watermark.String() + " count=9")
			if next.joined() != "0,3" {
				t.Errorf("the rest is %s", next.joined())
			}
			if done.Stop != serval.StopExhausted {
				t.Error("the end of the sequence did not say so")
			}
		})
	}
}

// A scope is asked for and answered; nothing waits on anything. The records
// reach the sink as the application sends them, which here is during the send.
func TestAskingDoesNotWaitForTheAnswer(t *testing.T) {
	src := serving(t)
	set, err := src.Open(parseSpec(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	out := &collector{}
	if err := set.Read(parseScope(t, "count=2"), out); err != nil {
		t.Fatal(err)
	}
	if len(out.keys) != 2 || !out.ended {
		t.Errorf("the answer arrived as %v, ended=%v", out.keys, out.ended)
	}
}

// A connection that will not carry the question ends the scope rather than
// leaving whoever asked waiting for records that are never coming.
func TestAConnectionThatWillNotCarryTheQuestionSaysSo(t *testing.T) {
	src := NewApplicationSource("files", func(string) error { return errBroken{} })
	set, err := src.Open(parseSpec(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	out := &collector{}
	if err := set.Read(parseScope(t, "count=2"), out); err == nil {
		t.Fatal("a broken connection was not reported")
	}
	if !out.ended || out.done.Error == "" {
		t.Errorf("the scope ended as %#v", out.done)
	}
	if !strings.Contains(out.done.Error, "broken") {
		t.Errorf("what ended it reads %q", out.done.Error)
	}
}

type errBroken struct{}

func (errBroken) Error() string { return "the connection is broken" }

// A source with nothing to ask cannot open a sequence at all.
func TestASourceWithNoConnectionRefusesToOpen(t *testing.T) {
	if _, err := NewApplicationSource("files", nil).Open(&serval.Spec{}); err == nil {
		t.Error("a source with no connection opened a sequence")
	}
}

func mustPSL(t *testing.T, text string) *serval.ListSource {
	t.Helper()
	src, err := serval.ParsePSLSource(text, serval.Whole)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// --- two scopes of one sequence --------------------------------------

// One sequence, asked twice. The second scope starts where the first ended,
// and neither kind is told which it is.
func TestTwoScopesOfOneSequence(t *testing.T) {
	for _, kind := range []struct {
		what string
		src  serval.Source
	}{
		{"records that are here", mustPSL(t, twoWays)},
		{"records an application has", serving(t)},
	} {
		t.Run(kind.what, func(t *testing.T) {
			set, err := kind.src.Open(parseSpec(t, "sort={ .size }"))
			if err != nil {
				t.Fatal(err)
			}
			defer set.Close()

			first := &collector{}
			if err := set.Read(parseScope(t, "count=2"), first); err != nil {
				t.Fatal(err)
			}
			if first.joined() != "2,1" {
				t.Fatalf("the first scope is %s", first.joined())
			}

			second := &collector{}
			if err := set.Read(parseScope(t,
				"after="+first.done.Watermark.String()+" count=9"), second); err != nil {
				t.Fatal(err)
			}
			if second.joined() != "0,3" {
				t.Errorf("the second scope is %s", second.joined())
			}
			if second.done.Stop != serval.StopExhausted {
				t.Error("the end of the sequence did not say so")
			}
		})
	}
}

// held is a connection that keeps what the source says until it is let go, so
// two scopes can be asked for before either is answered.
type held struct {
	src     *ApplicationSource
	conn    *client.Conn
	batches []string
}

func (h *held) Exec(src string) (*wire.Reply, error) {
	h.src.Statements(src)
	return &wire.Reply{}, nil
}
func (h *held) Send(src string) error { h.src.Statements(src); return nil }
func (h *held) Close() error          { return nil }

func (h *held) let(t *testing.T) {
	t.Helper()
	for len(h.batches) > 0 {
		batch := h.batches[0]
		h.batches = h.batches[1:]
		script, err := wire.Parse(batch)
		if err != nil {
			t.Fatal(err)
		}
		h.conn.InboundBatch(script.Statements)
	}
}

// Two scopes asked for before either is answered go to their own sinks, in
// the order they were asked for. It is one ordered stream either way, so what
// separates them is nothing but their place in it.
func TestTwoScopesInFlightKeepTheirOwnAnswers(t *testing.T) {
	h := &held{}
	h.conn = client.NewWithTransport(h, nil)
	if _, err := h.conn.ProvideSource("files", serveRecords); err != nil {
		t.Fatal(err)
	}
	h.src = NewApplicationSource("files", func(src string) error {
		h.batches = append(h.batches, src)
		return nil
	})

	set, err := h.src.Open(parseSpec(t, "sort={ .size }"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	first, second := &collector{}, &collector{}
	if err := set.Read(parseScope(t, "count=2"), first); err != nil {
		t.Fatal(err)
	}
	// The sequence has no number yet, so this one waits for it rather than
	// naming a key a later batch cannot resolve.
	if err := set.Read(parseScope(t, "after=1 count=9"), second); err != nil {
		t.Fatal(err)
	}
	if first.ended || second.ended {
		t.Fatal("an answer arrived before the application was asked")
	}

	h.let(t)
	if first.joined() != "2,1" {
		t.Errorf("the first scope got %s", first.joined())
	}
	if second.joined() != "0,3" {
		t.Errorf("the second scope got %s", second.joined())
	}
	if !first.ended || !second.ended {
		t.Error("a scope was never ended")
	}
}

// --- what an answer arrived as ----------------------------------------

// collector is a Sink that keeps what it was given, and how much of each
// record it was told had come back.
type collector struct {
	keys    []string
	fields  []serval.Record
	whole   []bool
	has     []serval.Totals
	done    serval.Complete
	ended   bool
	ordered bool
}

// Ordered arrives before the records, so a sink that is told late is a sink
// that was told wrong.
func (c *collector) Ordered() {
	if len(c.keys) > 0 || c.ended {
		panic("the order was declared after the records it describes")
	}
	c.ordered = true
}

func (c *collector) Record(key *serval.Value, fields serval.Record) error {
	return c.took(key, fields, true)
}

func (c *collector) Subset(key *serval.Value, fields serval.Record, has serval.Totals) error {
	c.has = append(c.has, has)
	return c.subset(key, fields)
}

func (c *collector) subset(key *serval.Value, fields serval.Record) error {
	return c.took(key, fields, false)
}

func (c *collector) took(key *serval.Value, fields serval.Record, whole bool) error {
	c.keys = append(c.keys, key.String())
	c.fields = append(c.fields, fields)
	c.whole = append(c.whole, whole)
	return nil
}

func (c *collector) Done(done serval.Complete) { c.done, c.ended = done, true }

func (c *collector) joined() string { return strings.Join(c.keys, ",") }

// --- stating a sequence, the way one arrives --------------------------
//
// These read the wire's own grammar, which is what this side of the boundary
// is for: a spec reaches an application source as text and is taken apart
// before anything sees it. serval's own tests build their specs instead,
// having no grammar to lean on.

func parseSpec(t *testing.T, args string) *serval.Spec {
	t.Helper()
	spec, err := wire.ParseSpec(statement(t, "new query "+args).Args[1:])
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func parseScope(t *testing.T, args string) *serval.Scope {
	t.Helper()
	sc, err := wire.ParseScope(statement(t, "new query "+args).Args[1:])
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func statement(t *testing.T, text string) *wire.Statement {
	t.Helper()
	script, err := wire.Parse(text)
	if err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	return script.Statements[0]
}

func key(i int64) *serval.Value { return serval.NewInt(i) }

func fields(name string, size int64) serval.Record {
	return serval.Record{serval.Named(".name", name), serval.Named(".size", size)}
}
