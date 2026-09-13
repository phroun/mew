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
func serveSubsets(f *client.Fill) { serve(f, f.Subset) }

func serve(f *client.Fill, send func(key any, fields ...*wire.Arg) error) {
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
	if key := f.From.Key(); key != nil {
		for start < len(rows) && rows[start].key != key.Int {
			start++
		}
		start++
	}

	f.Ordered()
	sent := 0
	for i := start; i < len(rows) && f.Have+sent < f.Need; i++ {
		_ = send(rows[i].key,
			wire.Named(".name", rows[i].name), wire.Named(".size", rows[i].size))
		sent++
	}
	if start+sent >= len(rows) {
		_ = f.Exhausted()
		return
	}
	last := rows[start+sent-1]
	_ = f.Done(wire.Fields{
		wire.Named(".size", last.size),
		wire.Named(wire.KeyField, last.key),
	})
}

// --- the asking, written once -------------------------------------------

// read opens a sequence and draws one scope of it, naming no kind.
func read(t *testing.T, src Source, spec, fill string) (*collector, Complete) {
	t.Helper()
	set, err := src.Open(parseSpec(t, spec))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	out := &collector{}
	if err := set.Fill(parseFill(t, fill), out); err != nil {
		t.Fatal(err)
	}
	if !out.ended {
		t.Fatal("the scope was never ended")
	}
	return out, out.done
}

func TestOneQuestionTwoKinds(t *testing.T) {
	for _, kind := range []struct {
		what string
		src  Source
	}{
		{"records that are here", mustPSL(t, twoWays)},
		{"records an application has", serving(t)},
	} {
		t.Run(kind.what, func(t *testing.T) {
			out, done := read(t, kind.src, "sort={ .size }", "have=0 need=2")
			if out.joined() != "2,1" {
				t.Errorf("by size the first two are %s", out.joined())
			}
			if got := out.fields[0].Encode(); got != `{ .name "go.mod"; .size 96 }` {
				t.Errorf("the first record carries %s", got)
			}
			if !out.ordered {
				t.Error("the answer did not say it was in order")
			}
			if done.Exhausted {
				t.Error("a scope with records past it claimed to be exhausted")
			}
			if got := done.Watermark.Encode(); got != "{ .size 310; key 1 }" {
				t.Errorf("the watermark is %s", got)
			}

			// And the scope after it, from where that one stopped.
			next, done := read(t, kind.src, "sort={ .size }",
				"from="+done.Watermark.Encode()+" have=0 need=9")
			if next.joined() != "0,3" {
				t.Errorf("the rest is %s", next.joined())
			}
			if !done.Exhausted {
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
	if err := set.Fill(parseFill(t, "have=0 need=2"), out); err != nil {
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
	if err := set.Fill(parseFill(t, "have=0 need=2"), out); err == nil {
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
	if _, err := NewApplicationSource("files", nil).Open(&wire.Spec{}); err == nil {
		t.Error("a source with no connection opened a sequence")
	}
}

func mustPSL(t *testing.T, text string) *PSLSource {
	t.Helper()
	src, err := ParsePSLSource(text, Whole)
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
		src  Source
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
			if err := set.Fill(parseFill(t, "have=0 need=2"), first); err != nil {
				t.Fatal(err)
			}
			if first.joined() != "2,1" {
				t.Fatalf("the first scope is %s", first.joined())
			}

			second := &collector{}
			if err := set.Fill(parseFill(t,
				"from="+first.done.Watermark.Encode()+" have=0 need=9"), second); err != nil {
				t.Fatal(err)
			}
			if second.joined() != "0,3" {
				t.Errorf("the second scope is %s", second.joined())
			}
			if !second.done.Exhausted {
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
	if err := set.Fill(parseFill(t, "have=0 need=2"), first); err != nil {
		t.Fatal(err)
	}
	// The sequence has no number yet, so this one waits for it rather than
	// naming a key a later batch cannot resolve.
	if err := set.Fill(parseFill(t, "from={ .size 310; key 1 } have=0 need=9"), second); err != nil {
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
