package source

// A data source, from this side of the wire.
//
// An application serves a query over records it holds; a display sometimes
// holds the records itself, and then there is nobody to ask. The two cases want
// the same answers to the same questions -- this filter, this sort, this window
// -- so they are one interface, and what is behind it can be a body of static
// data sitting here or an application across a connection.
//
// The shapes are the wire's own. A spec and a fill arrive exactly as
// wire/query.go parses them off a statement, and what ends a window is the
// three things `result <id> complete` can carry. So an implementation backed by
// an application is a relay rather than a translation, and one backed by data
// here is answering the same question the application would have been asked.
//
// docs/hosting-a-query.md is the wire spelling. docs/sort-and-filter.md is the
// comparison the sort and the filter stand on.

import "github.com/phroun/kittytk/wire"

// A Source is a body of records that sequences can be read out of.
type Source interface {
	// Open states a sequence: one filter, one sort. It is refused where the
	// sequence cannot be produced exactly -- an op that is not implemented, a
	// collation that is not carried -- because an ordering that is quietly a
	// little different corrupts every answer after it and looks like data.
	Open(spec *wire.Spec) (View, error)
}

// A View is one stated sequence, which windows are drawn from until it is let
// go.
//
// It does not change. A different sort or a different filter is a different
// view, opened alongside this one and taking its place -- which is also what
// keeps the source in use while the reader moves from one to the other.
type View interface {
	// Fill produces one window into the sink, and reports what ends it.
	Fill(f *wire.Fill, out Sink) (Complete, error)

	// Close lets the view go, and with it whatever it was holding.
	Close()
}

// A Sink takes the records a window is made of, one at a time, so a window of a
// million records need not be assembled anywhere before the first of them
// moves.
type Sink interface {
	Record(key *wire.Value, fields wire.Fields) error
}

// Complete is what ends a window, and it is the three things the terminator can
// carry.
//
// Ordered says the records went in in the sequence's own order, which changes
// what the far end does with them: ordered, it merges; unordered, it sorts
// first. Watermark says there is nothing between where the window was asked
// from and that point that the far end does not now have. Exhausted says there
// is nothing past the end at all, which is why it carries no watermark -- there
// is no point past the end to be complete up to.
type Complete struct {
	Ordered   bool
	Watermark wire.Fields
	Exhausted bool
}
