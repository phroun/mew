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
	Open(spec *wire.Spec) (ResultSet, error)
}

// A ResultSet is one stated sequence, which windows are drawn from until it is
// let go. It is what a query names, seen from the end that holds the records.
//
// It does not change. A different sort or a different filter is a different
// result set, opened alongside this one and taking its place -- which is also
// what keeps the source in use while the reader moves from one to the other.
type ResultSet interface {
	// Fill asks for one stretch of the sequence and says where to put it.
	//
	// It does not wait for the answer. Records reach the sink as they are
	// produced -- immediately, for a source whose records are here; as they
	// arrive, for one whose records are somewhere else -- and the sink is told
	// what ended it when it ends. The error is for a request that could not be
	// started at all, never for one that has not finished.
	Fill(f *wire.Fill, out Sink) error

	// Close lets the result set go, and with it whatever it was holding.
	Close()
}

// A Sink takes an answer as it is produced: the records one at a time, and
// then what ended them.
//
// One at a time because a stretch of a million records need not be assembled
// anywhere before the first of them moves, and because a source whose records
// are across a connection has them in that shape already.
type Sink interface {
	// Ordered says the records about to arrive are in the sequence's order.
	// It comes before the first of them or not at all, which is the only place
	// it is worth anything: a sink that learns it afterwards can no longer act
	// on the records it has already been given. Not being told means not
	// ordered, which is always safe.
	Ordered()

	// Record takes one record entire: its key, and every field it has.
	//
	// Whole is worth saying because it outlives the stretch that asked for it.
	// A record that arrived entire answers any question about that record, so
	// whoever holds it can answer the next query out of it instead of asking
	// again.
	Record(key *wire.Value, fields wire.Fields) error

	// Subset takes some of a record: its key, and the fields that were asked
	// for, which are fewer than the record has.
	//
	// It answers the question that asked for it and no other. A later question
	// naming a field this one left out is not answered by what came back here,
	// however many of the same records it names.
	Subset(key *wire.Value, fields wire.Fields) error

	Done(c Complete)
}

// Complete is what ends a stretch.
//
// Watermark says there is nothing between where the stretch was asked from and
// that point that the far end does not now have. Exhausted says there is
// nothing past the end at all, which is why it carries no watermark -- there is
// no point past the end to be complete up to.
//
// Order is not here. It is said before the records, on the sink, because a
// sink told afterwards cannot use it.
type Complete struct {
	Watermark wire.Fields
	Exhausted bool

	// Error is a refusal, which is an answer: this stretch cannot be produced,
	// the records are gone, the connection carrying the question broke. Whoever
	// asked carries on with what it has.
	Error string
}
