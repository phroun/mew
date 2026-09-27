package main

// The application's own ordering, asked directly.
//
// The live-session test drives `order` through a header click, which is what proves
// it is wired up -- but a display only ever asks for the two hints it currently uses.
// Two of the three things this function owes a query are therefore unreachable from
// there, and unreachable is not the same as unneeded: a reader that asked would get a
// wrong answer rather than a bigger one.
//
//	the levels    what the query named, in order
//	ties          the record's own identity, last and always
//	Reversed      every level turned over, that one included
//
// So they are asked for here.

import (
	"testing"

	"github.com/phroun/serval"
)

// keysOf is the order a level came out in, as its keys.
func keysOf(level []row) []int64 {
	out := make([]int64, 0, len(level))
	for _, r := range level {
		out = append(out, r.key)
	}
	return out
}

func sameKeys(got []int64, want ...int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// tied is four rows sharing two kinds, deliberately NOT in key order -- because a
// stable sort over a level that arrived in key order would hide whether anything
// broke the tie at all.
func tied() []row {
	return []row{
		{key: 3, name: "c", kind: "Text"},
		{key: 1, name: "a", kind: "Folder"},
		{key: 4, name: "d", kind: "Text"},
		{key: 2, name: "b", kind: "Folder"},
	}
}

// **A tie is broken by the record's identity**, which is not decoration: two rows
// equal on every named level would otherwise share a place, and "the record after
// this one" would name more than one row -- which is what a scope is asked from.
func TestATieIsBrokenByTheRecordsIdentity(t *testing.T) {
	level := tied()
	order(level, &serval.DataSetDescriptor{Sort: []serval.SortLevel{{Field: "kind"}}}, nil)
	// Folder before Text, and inside each the keys ascend.
	if got := keysOf(level); !sameKeys(got, 1, 2, 3, 4) {
		t.Errorf("sorted by kind the level reads %v, want the ties broken by key", got)
	}
}

// **Reversed turns every level over, the implicit one included.** It is the one hint
// an application cannot drop: every other omission only makes an answer bigger, and
// dropping this one makes it wrong -- and `Ordered` then claims an order that is not
// the sequence's.
func TestAReversedScopeTurnsEveryLevelOver(t *testing.T) {
	descriptor := &serval.DataSetDescriptor{Sort: []serval.SortLevel{{Field: "kind"}}}

	forward := tied()
	order(forward, descriptor, &serval.Scope{})
	if got := keysOf(forward); !sameKeys(got, 1, 2, 3, 4) {
		t.Fatalf("forward the level reads %v", got)
	}

	back := tied()
	order(back, descriptor, &serval.Scope{Reversed: true})
	if got := keysOf(back); !sameKeys(got, 4, 3, 2, 1) {
		t.Errorf("reversed the level reads %v, want every level turned over "+
			"-- the tiebreak too, or two rows tie in the other direction", got)
	}
}

// With nothing named, the order is the identity's alone -- so a level still has one
// place per row and a scope can still say where it starts.
func TestALevelWithNoSortIsInIdentityOrder(t *testing.T) {
	level := tied()
	order(level, &serval.DataSetDescriptor{}, nil)
	if got := keysOf(level); !sameKeys(got, 1, 2, 3, 4) {
		t.Errorf("with no sort the level reads %v, want identity order", got)
	}
}
