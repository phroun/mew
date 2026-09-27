package main

// The records this application serves, and how it answers for them.
//
// Two bodies, both a hundred thousand rows, because they cost a tree completely
// different things:
//
//	flat    one level of a hundred thousand siblings. The windowed level read is
//	        the whole story: forty rows drawn, forty-one asked for.
//	deep    a hundred thousand rows over three levels. Many small levels instead
//	        of one enormous one, so what is exercised is the pre-order walk and
//	        the child counts rather than the window.
//
// # The fields are named after the COLUMNS
//
// A treeview named from the wire language gets the IDENTITY mapping -- column
// `size` is field `size` -- because the language cannot yet say what each kind of
// row puts where. So these records are named to suit the columns the window
// declares, which is what any application would do until `kindmap` is sayable.
//
// # The deep body descends by LOCATION and not by parent key
//
// Worth saying, because the obvious choice is wrong here. An adjacency list
// compares a child's `up` against its parent's KEY -- and a bundle's include puts
// its own name in front of every key it carries, so `up: 7` would be looking for a
// record whose key is now `rows/7`. The app would have to know what the bundle
// called it.
//
// A location does not have that problem: it is a path, and a path is a value of the
// record rather than an identity the composition rewrites. So the deep body carries
// `dir` and `name` and the bundle says so -- which also means this demo exercises
// both of serval's descents rather than one of them twice.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/serval"
)

// How many rows each body holds, and the shape of the deep one.
const (
	flatRows = 100000

	deepDirs  = 100 // top level
	deepSubs  = 10  // under each
	deepFiles = 100 // under each of those
)

// A row is one record, already in the words the columns want.
type row struct {
	key      int64
	name     string
	dir      string // the container it lives in; empty at the top
	kind     string
	size     string // what the Size column shows
	rawsize  int64  // what it SORTS by, which is the hidden column
	modified string
	tags     string
	kids     int64 // how many children, which is what draws a twisty
}

// fields is this row as a record, under the names the columns and the hint use.
func (r row) fields() serval.Record {
	return serval.Record{
		serval.Named("name", r.name),
		serval.Named("dir", r.dir),
		serval.Named("kind", r.kind),
		serval.Named("size", r.size),
		serval.Named("rawsize", r.rawsize),
		serval.Named("modified", r.modified),
		serval.Named("tags", r.tags),
		serval.Named("kids", r.kids),
	}
}

// sizeWord is a byte count as the Size column shows it, so the shown value and the
// value it sorts by are the same number said two ways.
func sizeWord(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// flatBody is one level of a hundred thousand siblings.
func flatBody() []row {
	out := make([]row, 0, flatRows)
	for i := 0; i < flatRows; i++ {
		size := int64(97+i*13) % (1 << 22)
		out = append(out, row{
			key:      int64(i),
			name:     fmt.Sprintf("item %06d", i),
			kind:     []string{"Text", "PNG image", "Folder", "ARJ Archive"}[i%4],
			size:     sizeWord(size),
			rawsize:  size,
			modified: fmt.Sprintf("2026-%02d-%02d 0%d:%02d", 1+i%12, 1+i%28, i%10, i%60),
			tags:     []string{"", "red", "blue", "green"}[i%4],
		})
	}
	return out
}

// deepBody is a hundred thousand rows over three levels, addressed by where they
// live.
func deepBody() []row {
	out := make([]row, 0, deepDirs*(1+deepSubs*(1+deepFiles)))
	key := int64(0)
	next := func() int64 { key++; return key - 1 }

	for d := 0; d < deepDirs; d++ {
		top := fmt.Sprintf("volume %03d", d)
		out = append(out, row{
			key: next(), name: top, kind: "Folder",
			size: "--", modified: "2026-01-01 00:00", kids: deepSubs,
		})
		for s := 0; s < deepSubs; s++ {
			sub := fmt.Sprintf("group %02d", s)
			out = append(out, row{
				key: next(), name: sub, dir: top, kind: "Folder",
				size: "--", modified: "2026-02-02 00:00", kids: deepFiles,
			})
			for f := 0; f < deepFiles; f++ {
				size := int64(101+f*31+s*7+d*3) % (1 << 20)
				out = append(out, row{
					key:      next(),
					name:     fmt.Sprintf("file %03d.txt", f),
					dir:      top + "/" + sub,
					kind:     "Text",
					size:     sizeWord(size),
					rawsize:  size,
					modified: fmt.Sprintf("2026-03-%02d 1%d:%02d", 1+f%28, f%10, f%60),
					tags:     []string{"", "red", "blue"}[f%3],
				})
			}
		}
	}
	return out
}

// serve answers one scope of a body.
//
// **It honours the filter, the sort and the count, which is the application's
// job.** Each of the three was a bug here before it was a line:
//
//   - The FILTER makes a level. A tree asks for the rows under one node -- the top
//     level being "those with no container" -- and an application that sent
//     everything regardless puts every record at every level.
//   - The SORT is the level's order. Clicking a column header sends `sort=` down to
//     here, and an application that ignores it draws the arrow and changes nothing.
//     Worse than merely ignoring it, because `Ordered` below CLAIMS these records
//     are in the sequence's order -- so the display believes the answer and does
//     not sort it either.
//   - The COUNT is the window. Sending a hundred thousand rows for a window of
//     forty is correct and slow, which for a demo about how much a tree asks for
//     would hide the whole point.
//   - The TOTAL is what turns a floor into a count. A display cannot work out how
//     long a sequence is without reading it, which is the one thing a window exists
//     not to do -- so a windowed view over an application that says nothing draws a
//     thumb that shrinks as the reader scrolls. This one has the level in hand
//     already, so saying how long it is costs nothing, and the thumb is true from
//     the first frame.
//
// The scan and the sort are over the whole matching level, which is this
// application's own cost and is visible as such: a real one would index by
// container and keep its orders. It is left plain here because what this demo is
// for is watching the TREE behave, and a level answering out of an index would hide
// how much it was asked for.
func serve(body []row) func(*client.Fill) {
	return func(f *client.Fill) {
		// The level first, because the sort is a sort of the rows that are IN it and
		// ordering the whole body would be ordering rows nobody asked about.
		level := make([]row, 0, 64)
		for _, r := range body {
			if serval.Match(serval.NewInt(r.key), r.fields(), f.Descriptor.Filter) {
				level = append(level, r)
			}
		}
		order(level, f.Descriptor, f.Scope)

		// How long the LEVEL is, which is a different fact from how many are being
		// sent: the window is forty and the sequence is a hundred thousand, and a
		// reader wants both. It is free here, the level having just been gathered.
		f.Total(serval.Exactly(len(level)))

		f.Ordered()
		sent := 0
		var last any
		for _, r := range level {
			if sent >= f.Count {
				// **Complete up to the last one that went OUT, and say which one that
				// was.** Saying nothing is what lets a reader ask for the next window
				// without asking for this one again -- and it is also a run with no end
				// on it, which a cache reads as the whole sequence in one piece. This
				// application said a hundred thousand rows and was believed to have
				// sent all of them, so the display drew a thumb for eighty-eight.
				_ = f.Filled(last)
				return
			}
			if err := f.Record(serval.NewInt(r.key), r.fields()...); err != nil {
				return
			}
			last = serval.NewInt(r.key)
			sent++
		}
		_ = f.Exhausted()
	}
}

// order puts a level in the order the query asked for.
//
// The record's own identity is the last level always, and it is not decoration: two
// rows equal on every named level would otherwise tie, and "the record after this
// one" would name more than one place. `Reversed` turns every level over, that one
// included -- dropping it is the one hint an application cannot drop, because every
// other omission only makes an answer bigger and this one makes it wrong.
func order(level []row, descriptor *serval.DataSetDescriptor, sc *serval.Scope) {
	levels := append(serval.Levels(descriptor.Sort), serval.Level{})
	if sc != nil && sc.Reversed {
		levels = serval.Reverse(levels)
	}
	tuple := func(r row) []*serval.Value {
		fields := r.fields()
		out := make([]*serval.Value, 0, len(descriptor.Sort)+1)
		for _, l := range descriptor.Sort {
			out = append(out, fields.Get(l.Field))
		}
		return append(out, serval.NewInt(r.key))
	}
	sort.SliceStable(level, func(i, j int) bool {
		return serval.CompareLevels(tuple(level[i]), tuple(level[j]), levels) < 0
	})
}

// deepBundle is the document that says what shape the deep body is.
//
// The records are the APPLICATION's, reached by an include; the shape is the
// document's. Nothing about a view is in here -- no column, no width, no
// expansion -- which is the line a bundle does not cross.
func deepBundle() string {
	return strings.Join([]string{
		`(`,
		`  _bundle: (`,
		`    key: "deeptree", version: "1.0.0",`,
		`    includes: ( rows: ( source: "deep" ) ),`,
		`    tree: (`,
		`      location:  "dir",`,
		`      name:      "name",`,
		`      delimiter: "/",`,
		`      label:     "name",`,
		`      children:  "kids"`,
		`    )`,
		`  )`,
		`)`,
	}, "\n")
}
