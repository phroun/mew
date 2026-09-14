package source

// A data source, from this side of the wire.
//
// An application serves a query over records it holds; a display sometimes
// holds the records itself, and then there is nobody to ask. The two cases want
// the same answers to the same questions -- this filter, this sort, this scope
// -- so they are one interface, and what is behind it can be a body of static
// data sitting here or an application across a connection.
//
// The shapes are the wire's own. A spec and a fill arrive exactly as
// wire/query.go parses them off a statement, and what ends a scope is the
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
	Open(spec *wire.Spec) (DataSet, error)
}

// A DataSet is one stated sequence -- this source, this sort, this filter --
// prepared once and drawn from until it is let go. It is what a query names,
// seen from the end that holds the records.
//
// Those three name it, and nothing else does: the fields a query asks for
// change what a scope carries rather than which records are in the sequence,
// and the direction belongs to the scope. So two queries naming the same three
// are reading one data set, and whatever was worked out for either of them --
// the ordering, where each record stands -- holds for both.
//
// It does not change. A different sort or a different filter is a different
// data set, opened alongside this one and taking its place -- which is also
// what keeps the source in use while the reader moves from one to the other.
type DataSet interface {
	// Read asks for one scope of the sequence and says where to put it.
	//
	// It does not wait for the answer. Records reach the sink as they are
	// produced -- immediately, for a source whose records are here; as they
	// arrive, for one whose records are somewhere else -- and the sink is told
	// what ended it when it ends. The error is for a request that could not be
	// started at all, never for one that has not finished.
	//
	// A scope names its ends by identity, and an identity means something only
	// to the source that issued it. So a scope handed here was issued here: a
	// source made of several others translates rather than relays.
	Read(s *wire.Scope, out Sink) error

	// Close lets the data set go, and with it whatever it was holding.
	Close()
}

// A Sink takes an answer as it is produced: the records one at a time, and
// then what ended them.
//
// One at a time because a scope of a million records need not be assembled
// anywhere before the first of them moves, and because a source whose records
// are across a connection has them in that shape already.
type Sink interface {
	// Ordered says the records about to arrive are in the sequence's order.
	// It comes before the first of them or not at all, which is the only place
	// it is worth anything: a sink that learns it afterwards can no longer act
	// on the records it has already been given. Not being told means not
	// ordered, which is always safe.
	Ordered()

	// Record takes one record entire: its identity, and every field it has.
	//
	// Whole is worth saying because it outlives the scope that asked for it.
	// A record that arrived entire answers any question about that record, so
	// whoever holds it can answer the next query out of it instead of asking
	// again.
	//
	// The identity is beside the fields, not among them. A record may well
	// carry a field called `key`, and that field is data: it sorts, it
	// filters, it fills a column. What names the record is this.
	Record(id *wire.Value, fields wire.Fields) error

	// Subset takes some of a record: its identity, and the fields that were
	// asked for, which are fewer than the record has.
	//
	// It answers the question that asked for it and no other. A later question
	// naming a field this one left out is not answered by what came back here,
	// however many of the same records it names.
	Subset(id *wire.Value, fields wire.Fields) error

	Done(c Complete)
}

// Complete is what ends a scope: the wire's own, because what a source says
// here is what crosses.
//
// Order is not in it. It is said before the records, on the sink, because a
// sink told afterwards cannot use it.
type Complete = wire.Complete
