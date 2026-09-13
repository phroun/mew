package source

// What a window costs.
//
// The claim the flat source makes is that a window is the log of the sequence's
// length and then the length of the window -- so reaching the last screenful of
// a hundred thousand records costs what reaching the first one costs. These
// measure it rather than asserting it, and the pair at the two ends of the
// sequence is the comparison that matters: if they diverge, the boundary is
// being walked to rather than found.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/phroun/kittytk/wire"
)

const benchRecords = 100000

// benchDoc is a PSL list of records with a name, a size and a kind.
func benchDoc(n int) string {
	var b strings.Builder
	b.Grow(n * 48)
	b.WriteString("(")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "(name: \"file%d.go\", size: %d, kind: %d),", i, (i*7919)%100000, i%5)
	}
	b.WriteString(")")
	return b.String()
}

func benchSource(tb testing.TB, n int) *PSL {
	tb.Helper()
	src, err := ParsePSL(benchDoc(n), Members)
	if err != nil {
		tb.Fatal(err)
	}
	return src
}

func benchSpec(tb testing.TB, args string) *wire.Spec {
	tb.Helper()
	script, err := wire.Parse("new query " + args)
	if err != nil {
		tb.Fatal(err)
	}
	spec, err := wire.ParseSpec(script.Statements[0].Args[1:])
	if err != nil {
		tb.Fatal(err)
	}
	return spec
}

type counter struct {
	n    int
	last wire.Fields
	key  *wire.Value
}

func (c *counter) Record(key *wire.Value, fields wire.Fields) error {
	c.n++
	c.last, c.key = fields, key
	return nil
}

// Reading the file: parsing the PSL and taking its records off it.
func BenchmarkReadThePSL(b *testing.B) {
	text := benchDoc(benchRecords)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ParsePSL(text, Members); err != nil {
			b.Fatal(err)
		}
	}
}

// Stating the sequence: the filter over every record, and the sort of what is
// left. Paid once, however many windows are drawn from it.
func BenchmarkStateTheSequence(b *testing.B) {
	src := benchSource(b, benchRecords)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// A spec of its own each time, so the cache never answers.
		spec := benchSpec(b, fmt.Sprintf("sort={ name natural } filter={ ge size %d }", i))
		if _, err := src.Open(spec); err != nil {
			b.Fatal(err)
		}
	}
}

// A window at the start of the sequence, and a window at the end of it. These
// are the two numbers to compare.
func BenchmarkWindowAtTheStart(b *testing.B) { benchWindow(b, 0) }
func BenchmarkWindowAtTheEnd(b *testing.B)   { benchWindow(b, benchRecords-40) }

func benchWindow(b *testing.B, after int) {
	src := benchSource(b, benchRecords)
	view, err := src.Open(benchSpec(b, "sort={ name natural }"))
	if err != nil {
		b.Fatal(err)
	}

	// Walk to the boundary once, outside the timer, so what is measured is one
	// window drawn from where somebody has scrolled to.
	from := wire.Fields{}
	if after > 0 {
		c := &counter{}
		if _, err := view.Fill(fillOf(b, 0, after), c); err != nil {
			b.Fatal(err)
		}
		from = wire.Fields{
			{Name: "name", Value: c.last.Get("name")},
			{Name: wire.KeyField, Value: c.key},
		}
	}

	f := fillOf(b, 0, 30)
	f.From = from
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := &counter{}
		if _, err := view.Fill(f, c); err != nil {
			b.Fatal(err)
		}
		if c.n != 30 {
			b.Fatalf("the window came back %d records long", c.n)
		}
	}
}

func fillOf(tb testing.TB, have, need int) *wire.Fill {
	tb.Helper()
	script, err := wire.Parse(fmt.Sprintf("query 1 have=%d need=%d", have, need))
	if err != nil {
		tb.Fatal(err)
	}
	f, err := wire.ParseFill(script.Statements[0].Args[1:])
	if err != nil {
		tb.Fatal(err)
	}
	return f
}
