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
	"strings"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/wire"
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
	if _, err := conn.HostSource("colours", serveEverything); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	files, err := conn.HostSource("files", serveWindow)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Neither of these is needed to be correct. The first is where an
	// application with something to tear down tears it down; the second is
	// where one holding a large result set lets it go.
	files.OnRespec(func(q *client.Query, spec *wire.Spec) {
		fmt.Fprintf(os.Stderr, "query %d: re-sorted to %s\n", q.ID(), wire.EncodeSort(spec.Sort))
	})
	files.OnDropped(func(q *client.Query) {
		fmt.Fprintf(os.Stderr, "query %d: let go, dropping what it held\n", q.ID())
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
		if err := f.Record(i, wire.Named("name", name)); err != nil {
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

// serveWindow honours what it was asked: the filter, the sort, the boundary
// and the size of the window.
//
// The whole of what it owes the display: every record of its own in
// (From..To], and if that does not make up the shortfall, keep going past To
// until it does.
func serveWindow(f *client.Fill) {
	rows := make([]entry, 0, len(entries))
	for _, e := range entries {
		if keep(e, f.Spec.Filter) {
			rows = append(rows, e)
		}
	}
	order(rows, f.Spec.Sort)

	// Where to start: the boundary is exclusive, so skip everything at or
	// before it. A boundary carries the sort fields and the key, which is what
	// makes it name exactly one position even when two records tie.
	start := 0
	if len(f.From) > 0 {
		for start < len(rows) && compare(rows[start], f.From, f.Spec.Sort) <= 0 {
			start++
		}
	}

	f.Ordered()
	sent, last := 0, -1
	for i := start; i < len(rows) && f.Have+sent < f.Need; i++ {
		e := rows[i]
		if err := f.Record(e.key, wire.Named("name", e.name), wire.Named("size", e.size)); err != nil {
			return
		}
		sent, last = sent+1, i
	}

	if last < 0 || last == len(rows)-1 {
		// Nothing past here, so there is no point past which to be complete.
		_ = f.Exhausted()
		return
	}
	// The watermark: there is nothing of mine between where you asked from and
	// this point that you do not now have. It is what lets the display shrink
	// the window, grow it back and scroll inside it without asking again.
	_ = f.Done(boundary(rows[last], f.Spec.Sort))
}

// boundary is a record's position: its sort fields, and its key.
func boundary(e entry, levels []wire.SortLevel) wire.Fields {
	out := make(wire.Fields, 0, len(levels)+1)
	for _, l := range levels {
		out = append(out, &wire.Arg{Name: l.Field, Value: field(e, l.Field)})
	}
	return append(out, &wire.Arg{Name: wire.KeyField, Value: wire.NewInt(e.key)})
}

// field is one of a record's values by name, and undefined for a name this
// record does not have -- which is a value with a rank of its own, not an
// error.
func field(e entry, name string) *wire.Value {
	switch name {
	case "name":
		return wire.NewString(e.name)
	case "size":
		return wire.NewInt(e.size)
	case wire.KeyField:
		return wire.NewInt(e.key)
	}
	return wire.NewWord(wire.WordUndefined)
}

// order sorts by the query's levels, with the record key as the implicit final
// one -- without it two records could tie, and "the record after this point"
// would name more than one place.
func order(rows []entry, levels []wire.SortLevel) {
	cmp := append(wire.Levels(levels), wire.Level{})
	sort.SliceStable(rows, func(i, j int) bool {
		return wire.CompareLevels(tuple(rows[i], levels), tuple(rows[j], levels), cmp) < 0
	})
}

// compare places one record against a boundary, under the same levels.
func compare(e entry, at wire.Fields, levels []wire.SortLevel) int {
	var mine, theirs []*wire.Value
	for _, l := range levels {
		mine = append(mine, field(e, l.Field))
		theirs = append(theirs, at.Get(l.Field))
	}
	mine = append(mine, wire.NewInt(e.key))
	theirs = append(theirs, at.Key())
	return wire.CompareLevels(mine, theirs, append(wire.Levels(levels), wire.Level{}))
}

func tuple(e entry, levels []wire.SortLevel) []*wire.Value {
	out := make([]*wire.Value, 0, len(levels)+1)
	for _, l := range levels {
		out = append(out, field(e, l.Field))
	}
	return append(out, wire.NewInt(e.key))
}

// keep is the filter, walked as the tree it arrived as. A block is an AND, so
// the top of one always is.
func keep(e entry, f *wire.Filter) bool {
	if f == nil {
		return true
	}
	switch f.Op {
	case wire.OpAnd:
		for _, c := range f.Children {
			if !keep(e, c) {
				return false
			}
		}
		return true
	case wire.OpOr:
		for _, c := range f.Children {
			if keep(e, c) {
				return true
			}
		}
		return len(f.Children) == 0
	case wire.OpNot:
		for _, c := range f.Children {
			if keep(e, c) {
				return false
			}
		}
		return true
	case wire.OpStarts:
		v := f.Value()
		return v != nil && strings.HasPrefix(e.name, v.Str)
	case wire.OpContains:
		v := f.Value()
		return v != nil && strings.Contains(e.name, v.Str)
	}
	// The comparisons are the comparison core, which both ends compute the
	// same way from the same spec (docs/sort-and-filter.md).
	c := wire.Compare(field(e, f.Field), f.Value(), f.Collate)
	switch f.Op {
	case wire.OpEq:
		return c == 0
	case wire.OpNe:
		return c != 0
	case wire.OpLt:
		return c < 0
	case wire.OpLe:
		return c <= 0
	case wire.OpGt:
		return c > 0
	case wire.OpGe:
		return c >= 0
	case wire.OpIn:
		for _, v := range f.Values {
			if wire.Compare(field(e, f.Field), v, f.Collate) == 0 {
				return true
			}
		}
	}
	return false
}
