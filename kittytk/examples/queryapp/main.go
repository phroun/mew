// Command queryapp serves records to a display that asks for them.
//
// It is the whole of what an application writes to back a list it holds the
// records for. Two sources, at the two ends of how much work an author wants
// to do:
//
//	colours  the least possible: send everything, say exhausted, ignore the
//	         rest. Correct, and below a size the author picks, fastest.
//	files    the capable one: honour the boundary, the sort and the window,
//	         and answer with a watermark.
//
// Neither parses anything. The display's statement is taken apart before the
// handler sees it, so a window arrives as a struct and the answer is written
// into a sink that goes out in batches as it fills.
//
// No display opens queries yet, so run it against the stand-in:
//
//	go run ./cmd/kittytk-queryprobe &
//	KITTYTK_DISPLAY=/tmp/kittytk-queryprobe.sock go run ./examples/queryapp
//
// See docs/hosting-a-query.md.
package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/wire"
	"github.com/phroun/serval"
)

func main() {
	conn, err := client.Dial(client.DefaultEndpoint(), "queryapp", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Registering a source says nothing on the wire: a source is a name, not
	// an object. A real application would also tell whatever trinket is to
	// show these rows `data="files"`, and the display would open its queries
	// against that name when somebody scrolled.
	if _, err := conn.ProvideSource("colours", serveEverything); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	files, err := conn.ProvideSource("files", serveWindow)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Not needed to be correct: it is where an application holding something
	// for a reader finds out that reader has finished. What is held belongs to
	// the source rather than to any one query, so the moment to let it go is
	// when the last query against the source has gone.
	files.OnDropped(func(q *client.Query) {
		fmt.Fprintf(os.Stderr, "query %d: let go\n", q.ID())
	})

	fmt.Fprintln(os.Stderr, "serving; ^C to stop")
	<-conn.Closed()
}

// --- the least an implementation can do ---------------------------------

var colours = []string{"amber", "cerulean", "chartreuse", "ochre", "vermilion"}

// serveEverything ignores every hint, sends all its records, and says so.
//
// The display then holds the whole layer and asks nothing again until
// something invalidates it: no boundary arithmetic, no watermark, no round
// trip per scroll. Choosing this is a promise about SIZE -- my records fit in
// a message stream and I accept that all of them cross -- not about effort.
func serveEverything(f *client.Fill) {
	for i, name := range colours {
		if err := f.Record(i, serval.Named("name", name)); err != nil {
			return
		}
	}
	// Nothing is said about order, which leaves the display to sort. Saying
	// nothing is always safe; saying `Ordered` when it is not would not be.
	_ = f.Exhausted()
}

// --- the capable one ----------------------------------------------------

// A record this application holds. Anything at all: these come from a slice,
// a real one would read a directory or a database.
type entry struct {
	key  int64
	name string
	size int64
}

var entries = []entry{
	{1, "README.md", 2048}, {2, "build.sh", 310}, {3, "go.mod", 96},
	{4, "src/parser.go", 14022}, {5, "src/window.go", 9310},
	{6, "src/file2.go", 1200}, {7, "src/file10.go", 880},
	{8, "testdata/query.wire", 6100}, {9, ".hidden", 12},
	{10, "tools/gen.go", 430},
}

// serveWindow honours what it was asked: the filter, the sort, and the scope.
//
// The whole of what it owes the display: start past After, walk the way
// Reversed says, and send Count records -- stopping early at Until, which is a
// record the display already holds.
func serveWindow(f *client.Fill) {
	rows := make([]entry, 0, len(entries))
	for _, e := range entries {
		if serval.Match(serval.NewInt(e.key), e, f.Spec.Filter) {
			rows = append(rows, e)
		}
	}
	order(rows, f.Spec, f.Scope)

	// Where to start: the scope names the record it starts past, so find it
	// and step over it. An identity means one record, which is why it is what
	// a scope is named by -- two records that tie in every sorted field still
	// have one each.
	start := 0
	if f.After != nil {
		for start < len(rows) && !sameKey(rows[start], f.After) {
			start++
		}
		if start == len(rows) {
			// Not one of mine, and nothing here can place it. Guessing would
			// answer a question the display did not ask.
			_ = f.Fail("after %s: no record of mine", wire.EncodeValue(wire.AsWire(f.After)))
			return
		}
		start++
	}

	f.Ordered()
	sent, last := 0, -1
	for i := start; i < len(rows); i++ {
		e := rows[i]
		if f.Until != nil && sameKey(e, f.Until) {
			// The display holds this one and everything past it, so the two
			// runs it holds are now one.
			_ = f.Joined(mark(rows, last, start))
			return
		}
		if sent >= f.Count {
			_ = f.Filled(mark(rows, last, start))
			return
		}
		if err := f.Record(e.key, serval.Named("name", e.name), serval.Named("size", e.size)); err != nil {
			return
		}
		sent, last = sent+1, i
	}

	// Walked off the end: nothing past here, so there is no point past which
	// to be complete.
	_ = f.Exhausted()
}

// mark is the record a claim is made up to: the last one that went out, or --
// where none did -- the one the scope was asked from, which is complete up to
// itself. Nothing at all is complete up to nothing, which is no claim.
func mark(rows []entry, last, start int) any {
	switch {
	case last >= 0:
		return rows[last].key
	case start > 0:
		return rows[start-1].key
	}
	return nil
}

// sameKey reports whether a record is the one an identity names.
func sameKey(e entry, id *serval.Value) bool {
	return serval.Compare(serval.NewInt(e.key), id, "") == 0
}

// Field is one of a record's values by name, and undefined for a name this
// record does not have -- which is a value with a rank of its own, not an
// error. Answering it is the whole of what a record owes the filter.
func (e entry) Field(name string) *serval.Value {
	switch name {
	case "name":
		return serval.NewText(e.name)
	case "size":
		return serval.NewInt(e.size)
	}
	return nil // undefined: a field this record has not got
}

// order sorts by the query's levels, with the record's identity as the
// implicit final one -- without it two records could tie, and "the record
// after this one" would name more than one place.
func order(rows []entry, spec *serval.Spec, sc *serval.Scope) {
	cmp := levels(spec, sc)
	sort.SliceStable(rows, func(i, j int) bool {
		return serval.CompareLevels(
			tuple(rows[i], spec.Sort), tuple(rows[j], spec.Sort), cmp) < 0
	})
}

// levels is what this sequence compares positions by.
//
// Reversed turns every one of them over, the implicit final one included --
// which is why it has to be honoured rather than ignored. Every other hint an
// application drops can only make the answer bigger; dropping this one makes
// it wrong, and `Ordered` would then be a lie.
func levels(spec *serval.Spec, sc *serval.Scope) []serval.Level {
	out := append(serval.Levels(spec.Sort), serval.Level{})
	if sc != nil && sc.Reversed {
		return serval.Reverse(out)
	}
	return out
}

func tuple(e entry, levels []serval.SortLevel) []*serval.Value {
	out := make([]*serval.Value, 0, len(levels)+1)
	for _, l := range levels {
		out = append(out, e.Field(l.Field))
	}
	return append(out, serval.NewInt(e.key))
}
