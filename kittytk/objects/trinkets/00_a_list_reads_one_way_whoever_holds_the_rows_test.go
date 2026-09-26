package trinkets

// A list reads its rows one way, whether they are its own or somebody else's.
//
// The plain case has to go on meaning exactly what it meant -- that is the whole
// point of putting a source underneath it -- so most of what is checked here is
// that nothing changed.

import (
	"fmt"
	"testing"

	"github.com/phroun/serval"
)

func filled(n int) *ListView {
	l := NewListView()
	for i := 0; i < n; i++ {
		l.AddTextItem(fmt.Sprintf("item %d", i))
	}
	return l
}

// A list built the way lists have always been built counts, indexes and reads
// back exactly as it did.
func TestAPlainListIsUnchanged(t *testing.T) {
	l := filled(5)

	if l.Count() != 5 {
		t.Fatalf("it counts %d rows, want 5", l.Count())
	}
	for i := 0; i < 5; i++ {
		item := l.Item(i)
		if item == nil {
			t.Fatalf("row %d is nil", i)
		}
		if want := fmt.Sprintf("item %d", i); item.Text != want {
			t.Errorf("row %d says %q, want %q", i, item.Text, want)
		}
	}
	if l.Item(-1) != nil || l.Item(5) != nil {
		t.Error("it answered for a row off the end")
	}
	// And the items it was given are still the items it was given.
	if len(l.Items()) != 5 {
		t.Errorf("Items() has %d, want 5", len(l.Items()))
	}
}

// The plain case answers INSIDE the call. Nothing about it became asynchronous,
// which is what lets every existing example go on working: the rows are readable
// the instant they are added, with nothing pumped and nothing awaited.
func TestAPlainListAnswersAtOnce(t *testing.T) {
	l := NewListView()
	l.AddTextItem("only")
	if l.Count() != 1 || l.Item(0) == nil || l.Item(0).Text != "only" {
		t.Fatal("a row was not readable the moment it was added")
	}
	l.AddTextItem("second")
	if l.Count() != 2 || l.Item(1).Text != "second" {
		t.Error("the second row was not readable either")
	}
}

// Adding, inserting, removing and clearing all reach the sequence. A list whose
// source was built once and never rebuilt would answer out of a stale one.
func TestEveryChangeToTheItemsReachesTheSequence(t *testing.T) {
	l := filled(3)

	l.InsertItem(1, NewListItem("wedged"))
	if l.Count() != 4 {
		t.Fatalf("after an insert it counts %d, want 4", l.Count())
	}
	if got := l.Item(1); got == nil || got.Text != "wedged" {
		t.Errorf("row 1 is %v, want the inserted one", got)
	}
	if got := l.Item(2); got == nil || got.Text != "item 1" {
		t.Errorf("row 2 is %v, want what was pushed along", got)
	}

	l.RemoveItem(0)
	if l.Count() != 3 {
		t.Fatalf("after a remove it counts %d, want 3", l.Count())
	}
	if got := l.Item(0); got == nil || got.Text != "wedged" {
		t.Errorf("row 0 is %v, want what moved up", got)
	}

	l.Clear()
	if l.Count() != 0 {
		t.Errorf("after a clear it counts %d", l.Count())
	}
	if l.Item(0) != nil {
		t.Error("a cleared list still has a row 0")
	}
}

// In a made source a row's key IS its position, so the two accessors agree and
// a plain list's identities are the figures an application already knows.
func TestInAMadeSourceTheKeyIsThePosition(t *testing.T) {
	l := filled(4)
	l.extent(0, 4) // so the list has been told where all four rows are

	for i := 0; i < 4; i++ {
		id, ok := l.IDAt(i)
		if !ok {
			t.Fatalf("row %d has no identity", i)
		}
		if !id.IsInt || int(id.Int) != i {
			t.Errorf("row %d is keyed %v, want %d", i, id, i)
		}
		if at, ok := l.IndexOf(id); !ok || at != i {
			t.Errorf("identity %v stands at %d (%v), want %d", id, at, ok, i)
		}
	}
}

// A DECLARED source is read the same way, and the list holds no items of its own.
func TestADeclaredSourceIsReadTheSameWay(t *testing.T) {
	rows := make([]serval.Row, 6)
	for i := range rows {
		rows[i] = serval.NewRow(serval.NewText(fmt.Sprintf("k%d", i)),
			serval.Record{serval.Named(rowDisplay, fmt.Sprintf("row %d", i))})
	}
	l := NewListView()
	l.SetSource(serval.NewListSource(rows))

	if l.Count() != 6 {
		t.Fatalf("it counts %d rows, want 6", l.Count())
	}
	if len(l.Items()) != 0 {
		t.Errorf("it is holding %d items of its own, and it was given none", len(l.Items()))
	}
	for i := 0; i < 6; i++ {
		item := l.Item(i)
		if item == nil {
			t.Fatalf("row %d is nil", i)
		}
		if want := fmt.Sprintf("row %d", i); item.Text != want {
			t.Errorf("row %d says %q, want %q", i, item.Text, want)
		}
	}
	// Its identities are the source's, not positions.
	id, ok := l.IDAt(2)
	if !ok || id.Str != "k2" {
		t.Errorf("row 2 is keyed %v, want k2", id)
	}
	if at, ok := l.IndexOf(serval.NewText("k4")); !ok || at != 4 {
		t.Errorf("k4 stands at %d (%v), want 4", at, ok)
	}
}

// Declaring a source replaces whatever the list was reading, its own items
// included -- and drops what it knew about where the rows were, a different
// sequence putting them somewhere else.
func TestDeclaringASourceReplacesTheItems(t *testing.T) {
	l := filled(5)
	if l.Count() != 5 {
		t.Fatal("the plain list did not fill")
	}

	l.SetSource(serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewText("a"), serval.Record{serval.Named(rowDisplay, "one")}),
	}))
	if l.Count() != 1 {
		t.Errorf("after declaring a source it counts %d, want 1", l.Count())
	}
	if got := l.Item(0); got == nil || got.Text != "one" {
		t.Errorf("row 0 is %v, want the source's", got)
	}

	// And nil goes back to the items it was given.
	l.SetSource(nil)
	if l.Count() != 5 {
		t.Errorf("back on its own items it counts %d, want 5", l.Count())
	}
	if got := l.Item(3); got == nil || got.Text != "item 3" {
		t.Errorf("row 3 is %v", got)
	}
}

// The selection is a PAIR, and setting either half sets the other.
func TestTheSelectionMovesByEitherHalf(t *testing.T) {
	l := filled(6)
	l.extent(0, 6) // the list has been told where its rows are

	l.SetCurrentIndex(3)
	if l.CurrentIndex() != 3 {
		t.Fatalf("the index is %d", l.CurrentIndex())
	}

	// And by identity, which lands on the same row.
	l.SetSelectedID(serval.NewInt(4))
	if l.CurrentIndex() != 4 {
		t.Errorf("after selecting identity 4 the index is %d", l.CurrentIndex())
	}
	if id := l.SelectedID(); id == nil || id.Int != 4 {
		t.Errorf("the selected identity is %v", id)
	}
}

// Nothing selected is index -1 and no identity, and setting the identity to
// nothing is how you get there.
func TestNothingSelectedIsBothHalvesEmpty(t *testing.T) {
	l := filled(4)
	l.extent(0, 4)
	l.SetCurrentIndex(2)

	l.SetSelectedID(nil)
	if l.CurrentIndex() != -1 {
		t.Errorf("the index is %d, want -1", l.CurrentIndex())
	}
	if l.SelectedID() != nil {
		t.Errorf("the identity is %v, want nothing", l.SelectedID())
	}
}

// An identity that is not in a sequence the list holds ENTIRELY is absent, not
// unmentioned, so there is nothing to wait for and the answer is nothing
// selected straight away.
func TestAnIdentityMissingFromAWholeSequenceIsNothing(t *testing.T) {
	l := filled(4)
	l.extent(0, 4) // all four rows, so the sequence is entirely in hand

	l.SetSelectedID(serval.NewText("nobody"))
	if l.CurrentIndex() != -1 {
		t.Errorf("it reported index %d, want -1", l.CurrentIndex())
	}
	if l.SelectedID() != nil {
		t.Errorf("it is holding %v, and the whole sequence says it is not there",
			l.SelectedID())
	}
}

// An identity the list cannot place in a sequence it holds only PART of is
// pending: a row that has not been mentioned is not a row that is absent. It is
// reported back as asked for, with no position, and dropped once the order
// settles without it.
func TestAnIdentityMissingFromPartOfASequenceIsPending(t *testing.T) {
	rows := make([]serval.Row, 50)
	for i := range rows {
		rows[i] = serval.NewRow(serval.NewText(fmt.Sprintf("k%02d", i)),
			serval.Record{serval.Named(rowDisplay, fmt.Sprintf("row %d", i))})
	}
	l := NewListView()
	l.SetSource(serval.NewListSource(rows))
	l.extent(0, 5) // five rows of fifty

	// One the list has not been told about, which may yet turn up.
	l.SetSelectedID(serval.NewText("k40"))
	if l.CurrentIndex() != -1 {
		t.Errorf("an unplaced identity reported index %d, want -1", l.CurrentIndex())
	}
	if id := l.SelectedID(); id == nil || id.Str != "k40" {
		t.Fatalf("it is holding %v, want k40 still in play", id)
	}

	// And reading where it lives resolves it, no announcement needed: the same
	// row was current before and after.
	l.extent(38, 5)
	if l.CurrentIndex() != 40 {
		t.Errorf("after the record arrived the index is %d, want 40", l.CurrentIndex())
	}
	if id := l.SelectedID(); id == nil || id.Str != "k40" {
		t.Errorf("the identity is %v", id)
	}
}

// One that never turns up is dropped once the order is settled.
func TestAPendingIdentityThatNeverTurnsUpIsDropped(t *testing.T) {
	rows := make([]serval.Row, 50)
	for i := range rows {
		rows[i] = serval.NewRow(serval.NewText(fmt.Sprintf("k%02d", i)),
			serval.Record{serval.Named(rowDisplay, "x")})
	}
	l := NewListView()
	l.SetSource(serval.NewListSource(rows))
	l.extent(0, 5)

	l.SetSelectedID(serval.NewText("nobody"))
	if l.SelectedID() == nil {
		t.Fatal("it dropped the identity before anything settled the order")
	}

	// Reading to the end settles the order, and it is still not there.
	l.extent(0, 50)
	if l.SelectedID() != nil {
		t.Errorf("after the order settled it is still holding %v", l.SelectedID())
	}
	if l.CurrentIndex() != -1 {
		t.Errorf("and the index is %d", l.CurrentIndex())
	}
}

// A row selected by POSITION learns its identity when the record arrives, which
// is what keyboard scrolling into unloaded rows depends on: the selection moves
// first and the record catches up.
func TestARowSelectedByPositionLearnsItsIdentity(t *testing.T) {
	// Zero-padded, because a text key sorts as TEXT: k0, k1, k10, k11 ... so
	// unpadded keys would put k36 at position 30 and the test would be asserting
	// an order the data has not got.
	rows := make([]serval.Row, 50)
	for i := range rows {
		rows[i] = serval.NewRow(serval.NewText(fmt.Sprintf("k%02d", i)),
			serval.Record{serval.Named(rowDisplay, fmt.Sprintf("row %d", i))})
	}
	l := NewListView()
	l.SetSource(serval.NewListSource(rows))
	l.Count() // stated, but nothing read

	// Nothing is known about where the rows are, so this row is a placeholder.
	if _, ok := l.IDAt(30); ok {
		t.Fatal("it named a row before reading anything")
	}
	l.setCurrent(30, nil)
	if l.CurrentIndex() != 30 {
		t.Fatalf("the index is %d", l.CurrentIndex())
	}
	if l.SelectedID() != nil {
		t.Error("it invented an identity for a placeholder")
	}

	// The record arrives, and the same row is now named.
	l.extent(28, 5)
	if id := l.SelectedID(); id == nil || id.Str != "k30" {
		t.Errorf("after the record arrived the identity is %v, want k30", id)
	}
	if l.CurrentIndex() != 30 {
		t.Errorf("and the index moved to %d", l.CurrentIndex())
	}
}

// A row the list cannot name is drawn, not skipped. It is a place with no words
// in it, which is what keeps a thumb drag smooth.
func TestABlankRowIsStillARow(t *testing.T) {
	rows := make([]serval.Row, 20)
	for i := range rows {
		rows[i] = serval.NewRow(serval.NewInt(int64(i)),
			serval.Record{serval.Named(rowDisplay, "x")})
	}
	l := NewListView()
	l.SetSource(serval.NewListSource(rows))

	if l.Count() != 20 {
		t.Fatalf("it counts %d rows", l.Count())
	}
	// Nothing read yet, so every row is a placeholder -- and the count still says there
	// are twenty of them to draw.
	if got := l.rowAt(10); got != nil {
		t.Errorf("row 10 is %v, and nothing has been read", got)
	}
	if placeholderRow.Text != "" || !placeholderRow.Enabled {
		t.Error("a placeholder should be empty and not disabled")
	}
}
