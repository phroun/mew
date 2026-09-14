package source

// How much of a record came back.

import (
	"testing"

	"github.com/phroun/kittytk/client"
)

// Nothing narrowed it, so what went out is the record, and that is what is
// said of it.
func TestAWholeRecordIsAnsweredAsOne(t *testing.T) {
	out, _ := read(t, mustPSL(t, twoWays), "sort={ .size }", "count=4")
	for i, whole := range out.whole {
		if !whole {
			t.Errorf("record %s went out as part of one", out.keys[i])
		}
	}
}

// A query that named the fields it wanted gets those fields, and they are
// answered as what they are: some of the record, not the record.
func TestAQueryThatNamesFieldsIsAnsweredWithSubsets(t *testing.T) {
	out, _ := read(t, mustPSL(t, twoWays), "sort={ .size } fields={ .name }",
		"count=4")
	for i, whole := range out.whole {
		if whole {
			t.Errorf("record %s claimed to be whole", out.keys[i])
		}
	}
	if got := out.fields[0].Encode(); got != `{ .name "go.mod" }` {
		t.Errorf("the first record carries %s", got)
	}
}

// An exclusion that took something out leaves a subset behind.
func TestAnExclusionThatDroppedAFieldLeavesASubset(t *testing.T) {
	out, _ := read(t, mustPSL(t, twoWays), "sort={ .size } exclude={ .size }",
		"count=1")
	if out.whole[0] {
		t.Error("a record with a field taken out claimed to be whole")
	}
	if got := out.fields[0].Encode(); got != `{ key 2; .name "go.mod" }` {
		t.Errorf("the record carries %s", got)
	}
}

// An exclusion that took nothing out is not a narrowing. What went out is every
// field the record has, and the claim is about what went out rather than about
// what the query said.
func TestAnExclusionThatDroppedNothingStillLeavesAWholeRecord(t *testing.T) {
	out, _ := read(t, mustPSL(t, twoWays), "sort={ .size } exclude={ .nothing }",
		"count=1")
	if !out.whole[0] {
		t.Error("a record nothing was taken out of did not go out as a whole one")
	}
}

// Records an application's are the application's to describe. This source
// relays the claim and makes none of its own: what it says of a record is what
// the far end said of it.
func TestAnApplicationsClaimIsPassedThrough(t *testing.T) {
	for _, kind := range []struct {
		what  string
		serve func(*client.Fill)
		whole bool
	}{
		{"whole records", serveRecords, true},
		{"subsets", serveSubsets, false},
	} {
		t.Run(kind.what, func(t *testing.T) {
			out, _ := read(t, hosting(t, kind.serve), "sort={ .size }", "count=2")
			if out.joined() != "2,1" {
				t.Fatalf("the scope is %s", out.joined())
			}
			for i, whole := range out.whole {
				if whole != kind.whole {
					t.Errorf("record %s came through as whole=%v", out.keys[i], whole)
				}
			}
		})
	}
}

// A replacement is a whole record -- that is what Replace states -- so it goes
// out as one whatever the child said of the record it stands in for. And what
// an amended source says of a record it merely passed on is what the child
// said of it: it is relaying that one, not restating it.
func TestAnAmendedSourceStatesItsOwnAndRelaysTheChildsClaim(t *testing.T) {
	for _, kind := range []struct {
		what  string
		serve func(*client.Fill)
		whole bool
	}{
		{"a child sending whole records", serveRecords, true},
		{"a child sending subsets", serveSubsets, false},
	} {
		t.Run(kind.what, func(t *testing.T) {
			a := NewAmendedSource(hosting(t, kind.serve))
			a.Replace(key(2), fields("go.mod", 96))

			out, _ := read(t, a, "sort={ .size }", "count=2")
			if out.joined() != "2,1" {
				t.Fatalf("the scope is %s", out.joined())
			}
			if !out.whole[0] {
				t.Error("the replacement did not go out as a whole record")
			}
			if out.whole[1] != kind.whole {
				t.Errorf("the child's record came through as whole=%v", out.whole[1])
			}
		})
	}
}
