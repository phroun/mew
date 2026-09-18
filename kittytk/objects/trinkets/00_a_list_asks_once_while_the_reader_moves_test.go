package trinkets

// Dragging, against a source that does not answer at once.
//
// With the rows here a question is free, so nothing about a plain list needs any
// of this. With the rows somewhere that has to be asked, a drag is a new place
// every frame, and the two things that must not happen are a hundred questions
// queued and an answer for somewhere the reader has left being written down as
// where the reader is.

import (
	"fmt"
	"testing"

	"github.com/phroun/serval"
)

// A slow source answers when it is told to, not when it is asked. Every Read is
// noted and held, so a test can let them arrive in whatever order a real one
// would have managed.
type slowSource struct {
	rows  []serval.Row
	asked []*held
}

type held struct {
	scope *serval.Scope
	out   serval.Sink
	src   *slowSource
}

func (s *slowSource) Open(spec *serval.Spec) (serval.DataSet, error) {
	inner, err := serval.NewListSource(s.rows).Open(spec)
	if err != nil {
		return nil, err
	}
	return &slowSet{src: s, inner: inner}, nil
}

type slowSet struct {
	src   *slowSource
	inner serval.DataSet
}

func (v *slowSet) Close()                          { v.inner.Close() }
func (v *slowSet) RecordCount() serval.RecordCount { return serval.CountOf(v.inner) }

// Read takes the question and returns, which is what "nothing waits" means.
func (v *slowSet) Read(s *serval.Scope, out serval.Sink) error {
	copied := *s
	v.src.asked = append(v.src.asked, &held{scope: &copied, out: out, src: v.src})
	return nil
}

// answer lets one held question through, which is the moment its records arrive.
func (h *held) answer(t *testing.T, spec *serval.Spec, rows []serval.Row) {
	t.Helper()
	set, err := serval.NewListSource(rows).Open(spec)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if err := set.Read(h.scope, h.out); err != nil {
		t.Fatal(err)
	}
}

func slowRows(n int) []serval.Row {
	out := make([]serval.Row, n)
	for i := range out {
		out[i] = serval.NewRow(serval.NewInt(int64(i)),
			serval.Record{serval.Named(rowDisplay, fmt.Sprintf("row %d", i))})
	}
	return out
}

func slowList(t *testing.T, n int) (*ListView, *slowSource) {
	t.Helper()
	src := &slowSource{rows: slowRows(n)}
	l := NewListView()
	l.SetSource(src)
	if l.Count() != n {
		t.Fatalf("it counts %d rows, want %d", l.Count(), n)
	}
	return l, src
}

// Dragging asks once per move, not once per move plus every move before it.
func TestADragAsksOncePerMove(t *testing.T) {
	l, src := slowList(t, 10000)

	for _, at := range []int{100, 200, 300, 400, 500} {
		l.scrollOffset = at
		l.ask(at, 20)
	}
	if len(src.asked) != 5 {
		t.Fatalf("five moves asked %d questions", len(src.asked))
	}
	// And the last one asked for where the reader ended up.
	if last := src.asked[4].scope; last.From != 500 {
		t.Errorf("the last question asked from %d, want 500", last.From)
	}
}

// An answer for somewhere the reader has LEFT is dropped. Writing it down would
// leave the list holding where the drag passed through as though it were where
// the drag stopped.
func TestAnAnswerForSomewhereTheReaderHasLeftIsDropped(t *testing.T) {
	l, src := slowList(t, 10000)

	l.ask(100, 20) // asked, and on its way
	l.ask(500, 20) // the reader moved on; this supersedes it

	// The stale answer arrives first, as a slow one would.
	src.asked[0].answer(t, &l.spec, src.rows)
	if _, ok := l.IDAt(100); ok {
		t.Error("it wrote down an answer for a place the reader had left")
	}

	// And the one being waited for lands.
	src.asked[1].answer(t, &l.spec, src.rows)
	id, ok := l.IDAt(500)
	if !ok {
		t.Fatal("the answer it was waiting for did not land")
	}
	if !id.IsInt || id.Int != 500 {
		t.Errorf("row 500 is %v", id)
	}
}

// A stale answer still teaches the list HOW LONG the sequence is. That is a fact
// about the sequence rather than about the place the question asked for, so it
// holds good however old the answer is -- and a thumb that lost its scale every
// time a drag outran an answer would be unusable.
func TestAStaleAnswerStillTeachesTheLength(t *testing.T) {
	src := &slowSource{rows: slowRows(700)}
	l := NewListView()
	l.SetSource(src)

	l.ask(0, 20)
	l.ask(300, 20)
	// Nothing has told the spine a length except the count it took when stated.
	src.asked[0].answer(t, &l.spec, src.rows) // the stale one
	if got := l.bones.length(); !got.Exact || got.N != 700 {
		t.Errorf("a stale answer left the length at %v, want exactly 700", got)
	}
}

// Rows nobody has answered for yet are BLANK, and the list still knows how many
// there are -- which is what a drag looks like while the data is behind.
func TestWhileADragOutrunsTheDataTheRowsAreBlank(t *testing.T) {
	l, _ := slowList(t, 10000)

	l.scrollOffset = 4000
	l.ask(4000, 20)

	if l.Count() != 10000 {
		t.Errorf("it counts %d rows", l.Count())
	}
	for _, at := range []int{4000, 4010, 4019} {
		if l.rowAt(at) != nil {
			t.Errorf("row %d is not blank, and nothing has answered", at)
		}
	}
}

// Asking for a stretch already held is not an ask at all, so it supersedes
// nothing -- an answer still on its way to where the reader IS must not be
// thrown away by a question that was never sent.
func TestAskingForWhatIsHeldSupersedesNothing(t *testing.T) {
	l, src := slowList(t, 500)

	l.ask(100, 20)
	src.asked[0].answer(t, &l.spec, src.rows)
	if _, ok := l.IDAt(105); !ok {
		t.Fatal("the answer did not land")
	}

	l.ask(300, 20) // outstanding
	l.ask(100, 20) // already held, so no question and no superseding
	if len(src.asked) != 2 {
		t.Fatalf("it asked %d questions, want 2", len(src.asked))
	}
	src.asked[1].answer(t, &l.spec, src.rows)
	if _, ok := l.IDAt(305); !ok {
		t.Error("the outstanding answer was discarded by a question nobody asked")
	}
}

// A length that is only a FLOOR cannot say where the end is, so the thumb is
// kept off the bottom of its track. A thumb resting at the foot says "this is
// the end of the sequence", which a floor has no standing to claim.
func TestAFlooredLengthKeepsTheThumbOffTheBottom(t *testing.T) {
	l := NewListView()
	l.SetSource(&uncountedSource{rows: slowRows(400)})

	// Nothing counted it, so what is known is what has been placed.
	l.window(0, 40)
	if got := l.Length(); got.Exact {
		t.Fatalf("the length came back exact: %v", got)
	}
	if !l.thumbFloor() {
		t.Error("a length that is not exact should hold the thumb off the bottom")
	}

	// And an exact one does not hold it off.
	plain := filled(100)
	plain.Count()
	if got := plain.Length(); !got.Exact || got.N != 100 {
		t.Fatalf("a plain list's length is %v", got)
	}
	if plain.thumbFloor() {
		t.Error("an exact length should let the thumb reach the bottom")
	}
}

// uncountedSource answers scopes and will not say how many records it has, which
// is every source that has to ask somebody else.
//
// It has to refuse BOTH ways round. Total rides a completion so that a source
// able to count says so on an answer it was sending anyway, so muting
// RecordCount alone would leave the figure crossing by the other road and this
// would stand for a source that counts after all.
type uncountedSource struct{ rows []serval.Row }

func (u *uncountedSource) Open(spec *serval.Spec) (serval.DataSet, error) {
	set, err := serval.NewListSource(u.rows).Open(spec)
	if err != nil {
		return nil, err
	}
	return uncountedSet{set}, nil
}

type uncountedSet struct{ serval.DataSet }

func (uncountedSet) RecordCount() serval.RecordCount { return serval.Unknown() }

func (u uncountedSet) Read(s *serval.Scope, out serval.Sink) error {
	return u.DataSet.Read(s, uncountedSink{out})
}

type uncountedSink struct{ serval.Sink }

func (u uncountedSink) Done(c serval.Complete) {
	c.Total = serval.Unknown()
	u.Sink.Done(c)
}
