package trinkets

// What is chosen, and what happens to it when the rows move.

import (
	"fmt"
	"testing"

	"github.com/phroun/serval"
)

func multi(n int) *ListView {
	l := filled(n)
	l.SetSelectionMode(MultiSelection)
	// A list chooses its first row as it gets it -- AddItem sets the current
	// row, and in single selection that is a choice. These are about choosing,
	// so they start from nothing chosen.
	l.ClearSelection()
	l.extent(0, n)
	return l
}

// Single selection follows the current row, which is what it always did.
func TestSingleSelectionFollowsTheCurrentRow(t *testing.T) {
	l := filled(5)

	l.SetCurrentIndex(3)
	if !l.IsSelected(3) {
		t.Error("the current row is not chosen")
	}
	if got := l.SelectedIndexes(); len(got) != 1 || got[0] != 3 {
		t.Errorf("it chose %v, want just row 3", got)
	}

	l.SetCurrentIndex(1)
	if l.IsSelected(3) || !l.IsSelected(1) {
		t.Error("the old row stayed chosen, or the new one did not")
	}
}

// Choosing rows one at a time, and asking which are chosen.
func TestRowsAreChosenAndUnchosen(t *testing.T) {
	l := multi(6)

	l.SetSelected(1, true)
	l.SetSelected(4, true)
	if got := fmt.Sprint(l.SelectedIndexes()); got != "[1 4]" {
		t.Errorf("it chose %s, want [1 4]", got)
	}

	l.SetSelected(1, false)
	if got := fmt.Sprint(l.SelectedIndexes()); got != "[4]" {
		t.Errorf("after unchoosing one it chose %s", got)
	}
	if l.IsSelected(1) {
		t.Error("the unchosen row is still chosen")
	}
}

// **In order.** It used to be read off a map, so two runs of the same program
// disagreed about what the same selection was.
func TestSelectedIndexesComeBackInOrder(t *testing.T) {
	for try := 0; try < 8; try++ {
		l := multi(40)
		for _, at := range []int{31, 2, 17, 5, 28} {
			l.SetSelected(at, true)
		}
		if got := fmt.Sprint(l.SelectedIndexes()); got != "[2 5 17 28 31]" {
			t.Fatalf("it chose %s, want them in order", got)
		}
	}
}

// Choosing everything costs nothing and names nothing, which is what lets it
// work on a sequence the list has mostly never seen.
func TestChoosingEverythingNamesNothing(t *testing.T) {
	rows := make([]serval.Row, 100000)
	for i := range rows {
		rows[i] = serval.NewRow(serval.NewInt(int64(i)),
			serval.Record{serval.Named(rowDisplay, "x")})
	}
	l := NewListView()
	l.SetSelectionMode(MultiSelection)
	l.SetSource(serval.NewListSource(rows))
	l.extent(0, 30) // thirty rows of a hundred thousand

	l.SelectAll()
	if !l.SelectsEverything() {
		t.Fatal("it did not choose everything")
	}
	if got := l.SelectedIDs(); len(got) != 0 {
		t.Errorf("choosing everything named %d rows", len(got))
	}
	// Including rows it has never been told about, which is the point.
	if !l.IsSelected(99999) {
		t.Error("a row it has never seen is not chosen, and everything was")
	}
	if !l.IsSelected(5) {
		t.Error("a row it has seen is not chosen")
	}
}

// And one row can be spared out of everything, which is the only way to say it
// without writing down what everything was.
func TestOneRowCanBeSparedOutOfEverything(t *testing.T) {
	l := multi(20)
	l.SelectAll()

	l.SetSelected(7, false)
	if l.IsSelected(7) {
		t.Error("the spared row is still chosen")
	}
	if !l.IsSelected(6) || !l.IsSelected(8) {
		t.Error("sparing one row unchose its neighbours")
	}
	if got := fmt.Sprint(l.SelectedIDs()); got == "[]" {
		t.Error("nothing was named, and one row was spared")
	}
	got := l.SelectedIndexes()
	if len(got) != 19 {
		t.Errorf("it chose %d rows of twenty less one", len(got))
	}
	for _, at := range got {
		if at == 7 {
			t.Error("the spared row is in the list of chosen ones")
		}
	}
}

// A BLANK row is in no list of names, so it is chosen exactly when the names are
// what is left OUT -- which is right both times and needs no special case.
func TestABlankRowIsChosenOnlyWhenEverythingIs(t *testing.T) {
	rows := make([]serval.Row, 500)
	for i := range rows {
		rows[i] = serval.NewRow(serval.NewInt(int64(i)),
			serval.Record{serval.Named(rowDisplay, "x")})
	}
	l := NewListView()
	l.SetSelectionMode(MultiSelection)
	l.SetSource(serval.NewListSource(rows))
	l.extent(0, 10)

	if _, ok := l.IDAt(400); ok {
		t.Fatal("row 400 is not a placeholder, and this is about placeholders")
	}
	if l.IsSelected(400) {
		t.Error("a placeholder is chosen, and the selection names what is in")
	}
	l.SelectAll()
	if !l.IsSelected(400) {
		t.Error("a placeholder is not chosen, and the selection names what is out")
	}
}

// What is chosen survives the rows going out of view: it is kept by identity,
// and an identity means the same thing whether the row is on screen or not.
func TestWhatIsChosenSurvivesScrollingAway(t *testing.T) {
	rows := make([]serval.Row, 2000)
	for i := range rows {
		rows[i] = serval.NewRow(serval.NewText(fmt.Sprintf("k%04d", i)),
			serval.Record{serval.Named(rowDisplay, "x")})
	}
	l := NewListView()
	l.SetSelectionMode(MultiSelection)
	l.SetSource(serval.NewListSource(rows))

	l.extent(0, 20)
	l.SetSelected(3, true)
	chosen := l.SelectedIDs()
	if len(chosen) != 1 || chosen[0].Str != "k0003" {
		t.Fatalf("it chose %v", chosen)
	}

	// Scroll far enough that the spine has let those rows go entirely.
	for at := 0; at < 2000; at += 40 {
		l.extent(at, 40)
	}
	if _, ok := l.IDAt(3); ok {
		t.Fatal("row 3 is still held, and this is about rows that are not")
	}
	// The selection is still what it was, by name.
	still := l.SelectedIDs()
	if len(still) != 1 || still[0].Str != "k0003" {
		t.Errorf("after scrolling away it chose %v, want k0003", still)
	}
	// And coming back to it finds it chosen again.
	l.extent(0, 20)
	if !l.IsSelected(3) {
		t.Error("the row came back and was not chosen")
	}
}

// A made source keys its rows BY POSITION, so inserting one rewrites the keys
// after it and what is chosen moves with them -- exactly as the indices shift.
func TestInsertingIntoAPlainListMovesWhatIsChosen(t *testing.T) {
	l := multi(6)
	l.SetSelected(4, true)

	l.InsertItem(1, NewListItem("wedged"))
	l.extent(0, l.Count())
	if got := fmt.Sprint(l.SelectedIndexes()); got != "[5]" {
		t.Errorf("after an insert above it, the chosen row is at %s, want [5]", got)
	}

	l.RemoveItem(0)
	l.extent(0, l.Count())
	if got := fmt.Sprint(l.SelectedIndexes()); got != "[4]" {
		t.Errorf("after a remove above it, the chosen row is at %s, want [4]", got)
	}
}

// Removing the chosen row unchooses it rather than leaving the choice to land on
// whatever moved up into its place.
func TestRemovingAChosenRowUnchoosesIt(t *testing.T) {
	l := multi(5)
	l.SetSelected(2, true)

	l.RemoveItem(2)
	l.extent(0, l.Count())
	if got := l.SelectedIndexes(); len(got) != 0 {
		t.Errorf("after removing the chosen row it chose %v", got)
	}
}

// NoSelection chooses nothing and refuses to be told otherwise.
func TestNoSelectionChoosesNothing(t *testing.T) {
	l := filled(5)
	l.extent(0, 5)
	l.SetSelectionMode(MultiSelection)
	l.SetSelected(2, true)

	l.SetSelectionMode(NoSelection)
	if got := l.SelectedIndexes(); len(got) != 0 {
		t.Errorf("switching to NoSelection left %v chosen", got)
	}
	l.SetSelected(1, true)
	l.SelectAll()
	if got := l.SelectedIndexes(); len(got) != 0 {
		t.Errorf("NoSelection took a choice: %v", got)
	}
}

// Single selection refuses SelectAll, which would be a contradiction.
func TestSingleSelectionRefusesEverything(t *testing.T) {
	l := filled(5)
	l.extent(0, 5)
	l.SetCurrentIndex(2)

	l.SelectAll()
	if l.SelectsEverything() {
		t.Error("a single-selection list chose everything")
	}
	if got := fmt.Sprint(l.SelectedIndexes()); got != "[2]" {
		t.Errorf("it chose %s, want just the current row", got)
	}
}

// The names come back in order too, and not in whatever order a map felt like.
func TestSelectedIDsComeBackInOrder(t *testing.T) {
	for try := 0; try < 8; try++ {
		rows := make([]serval.Row, 40)
		for i := range rows {
			rows[i] = serval.NewRow(serval.NewText(fmt.Sprintf("k%02d", i)),
				serval.Record{serval.Named(rowDisplay, "x")})
		}
		l := NewListView()
		l.SetSelectionMode(MultiSelection)
		l.SetSource(serval.NewListSource(rows))
		l.extent(0, 40)
		for _, at := range []int{31, 2, 17, 5, 28} {
			l.SetSelected(at, true)
		}
		var said []string
		for _, id := range l.SelectedIDs() {
			said = append(said, id.Str)
		}
		if got := fmt.Sprint(said); got != "[k02 k05 k17 k28 k31]" {
			t.Fatalf("it named %s, want them in order", got)
		}
	}
}

// An insert AT a chosen row's own position moves that row along with the rest:
// the row that was there is now one further down, and it is the row that was
// chosen. Inserting below it leaves it where it was.
func TestAnInsertAtAChosenRowMovesIt(t *testing.T) {
	l := multi(6)
	l.SetSelected(1, true)

	l.InsertItem(1, NewListItem("wedged"))
	l.extent(0, l.Count())
	if got := fmt.Sprint(l.SelectedIndexes()); got != "[2]" {
		t.Errorf("an insert at the chosen row left it at %s, want [2]", got)
	}

	// And one below it does not move it.
	l.InsertItem(4, NewListItem("later"))
	l.extent(0, l.Count())
	if got := fmt.Sprint(l.SelectedIndexes()); got != "[2]" {
		t.Errorf("an insert below the chosen row moved it to %s", got)
	}
}
