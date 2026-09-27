package trinkets

// Reaching a place a source will not jump to.
//
// `Scope.From` is a position and is best effort: a source honours it as well as it
// can and says where it actually began. An application walking its own body may
// honour it not at all -- there is no index into a sequence somebody else named -- and
// answering from the beginning has to be slower rather than wrong.
//
// **It was neither. It never finished.** The view held rows nowhere near the one it
// wanted, so it had nothing adjacent to carry on from, so the only question it could
// think to ask was the one it had just been unable to have answered. It asked for row
// three hundred for ever and never moved.
//
// A tree cannot show this: its descent materialises the walk and answers a position
// out of rows it already holds, so a tree always honours one. A list reads its source
// directly, which is where the question actually goes out.

import (
	"fmt"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/serval"
)

// plainRows is n records, keyed and named by position.
func plainRows(n int) *serval.ListSource {
	rows := make([]serval.Row, n)
	for i := range rows {
		rows[i] = serval.NewRow(serval.NewInt(int64(i)), serval.Record{
			serval.Named("display", fmt.Sprintf("row%05d", i)),
		})
	}
	return serval.NewListSource(rows)
}

// naiveRows honours the COUNT and ignores the position, which is what an application
// walking its own body does: a record it was handed it can find, a row number it
// cannot. Saying where it began is the whole of what it owes -- see client.Fill.First
// and the wire's `first=`.
type naiveRows struct {
	rows  *serval.ListSource
	n     int
	asked int // how many questions it has been put, so a test can watch a walk
}

func (s *naiveRows) Open(descriptor *serval.DataSetDescriptor) (serval.DataSet, error) {
	set, err := s.rows.Open(descriptor)
	if err != nil {
		return nil, err
	}
	return &naiveSet{src: s, child: set}, nil
}

type naiveSet struct {
	src   *naiveRows
	child serval.DataSet
}

func (v *naiveSet) Close()                          { v.child.Close() }
func (v *naiveSet) RecordCount() serval.RecordCount { return serval.Exactly(v.src.n) }

func (v *naiveSet) Read(sc *serval.Scope, out serval.Sink) error {
	v.src.asked++
	// The position is dropped and `after` is kept, which is the split an application
	// with no index has.
	walk := &serval.Scope{Count: sc.Count, After: sc.After, Until: sc.Until}
	return v.child.Read(walk, &naiveSink{out: out, began: sc.After == nil})
}

// naiveSink says where the answer began where the walk started at the beginning,
// which is the one thing this source owes a reader that asked for a position.
type naiveSink struct {
	out   serval.Sink
	began bool
}

func (k *naiveSink) Ordered() { k.out.Ordered() }
func (k *naiveSink) Record(id *serval.Value, f serval.Record) error {
	return k.out.Record(id, f)
}
func (k *naiveSink) Subset(id *serval.Value, f serval.Record, has serval.Totals) error {
	return k.out.Subset(id, f, has)
}
func (k *naiveSink) Done(c serval.Complete) {
	if k.began {
		c.First = serval.Exactly(0)
	}
	k.out.Done(c)
}

// listOver is a list with a height, reading a source.
func listOver(t *testing.T, src serval.Source, rows int) *ListView {
	t.Helper()
	l := NewListView()
	l.SetBounds(core.UnitRect{Width: 40 * cell, Height: core.Unit(rows) * 2 * cell})
	l.SetSource(src)
	return l
}

// **A list reaches a position its source will not jump to, and stops asking for
// it.**
func TestAListReachesAPlaceASourceWillNotJumpTo(t *testing.T) {
	naive := &naiveRows{rows: plainRows(600), n: 600}
	l := listOver(t, naive, 8)

	// Nothing is learned from the first row: it asked to begin at the beginning and
	// that is where the answer began, so the two agree and there is nothing to
	// notice.
	if l.Item(0) == nil {
		t.Fatal("the first row is a placeholder")
	}
	if l.walks {
		t.Fatal("it decided the source cannot jump before asking it to")
	}

	// The FIRST question for somewhere else asks for the position, hopefully -- a
	// source that honours one is answered then and there. This one answers from the
	// beginning, and that is what says it cannot.
	l.Item(300)
	if !l.walks {
		t.Fatal("the answer began at the beginning and the list did not notice")
	}

	// And it gets there, a stretch at a time, rather than asking the same
	// unanswerable question for ever.
	var got *ListItem
	for i := 0; i < 400 && got == nil; i++ {
		got = l.Item(300)
	}
	if got == nil {
		t.Fatalf("row 300 is still blank after %d questions", naive.asked)
	}
	if got.Text != "row00300" {
		t.Errorf("row 300 reads %q", got.Text)
	}
	// Every question moved it on. Three hundred rows at a screenful a question is
	// what a source that cannot skip costs, and the figure is loose because the
	// screenful is the layout's business.
	if naive.asked > 120 {
		t.Errorf("it took %d questions to walk three hundred rows", naive.asked)
	}
}

// A source that DOES honour a position is answered in one question and is never made
// to walk -- which is what the learning is for: the naive source pays, and only it.
func TestASourceThatJumpsIsNeverMadeToWalk(t *testing.T) {
	plain := plainRows(600)
	l := listOver(t, plain, 8)

	if l.Item(0) == nil {
		t.Fatal("the first row is a placeholder")
	}
	if got := l.Item(300); got == nil || got.Text != "row00300" {
		text := "<placeholder>"
		if got != nil {
			text = got.Text
		}
		t.Errorf("row 300 reads %q in one question, this source jumping", text)
	}
	if l.walks {
		t.Error("jumping straight to row 300 was read as a failure to jump")
	}
}

// The nearest row held BELOW a position is what there is to carry on from, and it is
// not the same question as the row immediately above the stretch.
//
// Those two being conflated is the whole bug: a list holding rows nought to
// twenty-three and wanting row three hundred has nothing at two hundred and
// ninety-nine, and used to conclude it had nothing at all.
func TestTheSpineFindsTheNearestRowBelowAPosition(t *testing.T) {
	s := &spine{}
	s.learn(serval.Complete{Total: serval.Exactly(600)})
	s.place(0, ids(0, 24))

	at, id, ok := s.lastBefore(300)
	if !ok {
		t.Fatal("it found nothing below row 300, holding the first two dozen")
	}
	if at != 23 || id == nil || id.Int != 23 {
		t.Errorf("the nearest row below 300 is %d (%v), want the twenty-fourth", at, id)
	}

	// A second run nearer the position wins, being nearer.
	s.place(200, ids(200, 10))
	if at, _, _ := s.lastBefore(300); at != 209 {
		t.Errorf("with a run at 200 the nearest below 300 is %d, want 209", at)
	}
	// A run STRADDLING the position is cut at it: what is below is below.
	s.place(295, ids(295, 20))
	if at, _, _ := s.lastBefore(300); at != 299 {
		t.Errorf("with a run across 300 the nearest below it is %d, want 299", at)
	}
	// And nothing below the beginning, there being nothing below it.
	if _, _, ok := s.lastBefore(0); ok {
		t.Error("it found a row below the first one")
	}
}

// A FAR place costs a question per `spineKept` rows, which is the price of a source
// that cannot skip -- and is bounded, rather than being one enormous answer that is
// thrown away as it arrives.
func TestAFarWalkIsBoundedByWhatTheSpineKeeps(t *testing.T) {
	const rows = 4000
	naive := &naiveRows{rows: plainRows(rows), n: rows}
	l := listOver(t, naive, 8)
	l.Item(0)
	l.Item(rows - 1) // the question that finds out it cannot jump

	var got *ListItem
	for i := 0; i < 200 && got == nil; i++ {
		got = l.Item(rows - 1)
	}
	if got == nil {
		t.Fatalf("the last row is a placeholder after %d questions", naive.asked)
	}
	if got.Text != "row03999" {
		t.Errorf("the last row reads %q", got.Text)
	}
	// Four thousand rows at a capped question each: a dozen or so, not four thousand
	// and not one.
	least, most := rows/spineKept, rows/spineKept+8
	if naive.asked < least || naive.asked > most {
		t.Errorf("it took %d questions to walk %d rows, want between %d and %d --"+
			" a question per %d rows",
			naive.asked, rows, least, most, spineKept)
	}
	// And it is still a window: nothing accumulated on the way.
	if held := l.bones.held(); held > spineKept {
		t.Errorf("after the walk it holds %d identities, and the cap is %d",
			held, spineKept)
	}
}
