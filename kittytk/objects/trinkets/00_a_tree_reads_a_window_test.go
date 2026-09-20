package trinkets

// A tree reads the rows it is looking at, and a node opening moves them rather
// than forgetting them.
//
// Two things that were one thing. A view used to read the WHOLE flattening on
// every rebuild, and a rebuild was what a click on a twisty cost -- so opening one
// folder of a hundred thousand rows re-asked for all hundred thousand, and past the
// cache's budget re-asked for ever.
//
// The second half is the interesting one, and it is not an optimisation. Every
// level of a tree is a data set of its own, opened with its own descriptor and
// cached on its own, so opening a node makes no record anywhere untrue and reorders
// no level: what changes is which levels the walk visits, and therefore what
// POSITION each row below the mark stands at. Above the mark nothing moves at all.
// So the view shifts what it holds by a figure it has in hand -- `tree:expandable`,
// which one census per node type already paid for -- and asks nothing.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/serval"
)

// manyRows is a flat source of n records, which read as a tree of one generation.
func manyRows(n int) *serval.ListSource {
	rows := make([]serval.Row, n)
	for i := range rows {
		rows[i] = serval.NewRow(serval.NewInt(int64(i)), serval.Record{
			serval.Named("name", fmt.Sprintf("row%05d", i)),
			serval.Named("seq", i),
		})
	}
	return serval.NewListSource(rows)
}

// windowed is a view with a height, so it asks for a window rather than for as much
// as it is willing to hold. A view nobody sized reads what the spine will keep, and
// a test about windowing should not rest on that fallback.
func windowed(t *testing.T, rows int) *TreeView {
	t.Helper()
	tv := NewTreeView()
	tv.SetKindMap("", InOrder("name"))
	tv.SetBounds(core.UnitRect{Width: 60 * cell, Height: core.Unit(rows) * 2 * cell})
	if got := tv.visibleCount(); got == 0 {
		t.Fatalf("a view %d cells tall reports no visible rows", rows*2)
	}
	return tv
}

// **A view reads a WINDOW.** The claim is not that it is faster -- it is that the
// rows it holds are the rows it is looking at, and that it knows there are more
// below without having walked to them.
func TestATreeReadsAWindowOfADeclaredSource(t *testing.T) {
	tv := windowed(t, 10)
	tv.SetSource(manyRows(5000))

	held := tv.bones.held()
	if held == 0 {
		t.Fatal("it read nothing at all")
	}
	if held > 200 {
		t.Errorf("it is holding %d rows of five thousand", held)
	}
	if got := tv.Length(); got.Exact {
		t.Errorf("it says the sequence holds %v; a walk that stopped at its budget"+
			" cannot count what it never reached", got)
	}

	// The rows it holds are the ones at the top, which is where the reader is.
	if got := tv.Item(0); got == nil || got.Text != "row00000" {
		t.Errorf("the first row reads %q", caption(got))
	}

	// And the floor GROWS as the reader scrolls, which is the only way a tree's
	// length is ever learned: the count is the walk.
	was := tv.Count()
	scrollTo(t, tv, 900)
	if got := tv.Item(900); got == nil || got.Text != "row00900" {
		t.Errorf("row 900 reads %q", caption(got))
	}
	if now := tv.Count(); now <= was {
		t.Errorf("after scrolling to row 900 it still says %d rows", now)
	}
	// Scrolling did not turn the window into a log.
	if n := tv.bones.held(); n > spineKept {
		t.Errorf("after scrolling it is holding %d identities, and the cap is %d",
			n, spineKept)
	}
	if _, held := tv.bones.idAt(0); held {
		t.Error("it is still holding the row it started at")
	}
}

// scrollTo moves the reader to a position, a window at a time.
//
// **A tree cannot be jumped into, and that is the model rather than a gap.** How
// long a flattening is IS the walk, so a view that has read sixty rows knows there
// are at least sixty and cannot know there are five thousand -- and a thumb drawn
// against a floor shrinks as the reader scrolls rather than lying about where the
// end is. Reaching row nine hundred means reading down to it, which is what
// scrolling does.
func scrollTo(t *testing.T, tv *TreeView, at int) {
	t.Helper()
	for step := 0; step < 2000; step++ {
		if at < tv.Count() {
			tv.scrollOffset = at
			tv.Count() // which is what asks for the window there
			return
		}
		was := tv.Count()
		tv.scrollOffset = was - 1
		if now := tv.Count(); now <= was {
			t.Fatalf("scrolling stopped growing at %d rows, short of %d", was, at)
		}
	}
	t.Fatalf("scrolling never reached %d; it stopped at %d", at, tv.Count())
}

// A row the view is not holding is a BLANK, which is a state a row is drawn in and
// not a failure to draw one.
func TestARowOutsideTheWindowIsBlank(t *testing.T) {
	tv := windowed(t, 10)
	tv.SetSource(manyRows(5000))

	far := tv.bones.rows() + 40 // past everything placed, inside nothing
	if _, held := tv.bones.idAt(far); held {
		t.Fatalf("row %d came back named and nothing placed it", far)
	}
	if got := tv.rowAt(far); got != nil {
		t.Errorf("rowAt says row %d is %q", far, got.Text)
	}
	if got := tv.drawRow(far); got != blankTreeRow {
		t.Errorf("drawRow would paint %q, want a blank", caption(got))
	}
	if blankTreeRow.Text != "" || !blankTreeRow.Enabled || !blankTreeRow.IsLeaf() {
		t.Error("a blank is not an empty enabled leaf")
	}
}

// --- what a node opening costs -------------------------------------------

// kinWindow is a two-level adjacency tree: `folders` roots, each with `kids`
// children, over a source that COUNTS -- so `tree:expandable` is exact and the
// delta for a click is in hand.
func kinWindow(t *testing.T, folders, kids int) *serval.TreeSource {
	t.Helper()
	var rows []serval.Row
	for f := 0; f < folders; f++ {
		key := int64(1000 + f)
		rows = append(rows, serval.NewRow(serval.NewInt(key), serval.Record{
			serval.Named("name", fmt.Sprintf("folder%02d", f)),
			serval.Named("seq", f),
		}))
		for k := 0; k < kids; k++ {
			rows = append(rows, serval.NewRow(serval.NewInt(key*100+int64(k)), serval.Record{
				serval.Named("name", fmt.Sprintf("folder%02d/kid%02d", f, k)),
				serval.Named("parent", key),
				serval.Named("seq", k),
			}))
		}
	}
	by := []serval.SortLevel{{Field: "seq"}}
	src, err := serval.NewTreeSource(serval.TreeOptions{
		Source: serval.NewListSource(rows),
		Fields: treeFields,
		Descriptor: &serval.DataSetDescriptor{
			Filter: &serval.Filter{
				Op: serval.OpEq, Field: "parent", Values: []*serval.Value{nil},
			},
			Sort: by,
		},
		Types: serval.NodeTypes{Default: &serval.NodeType{
			Children: serval.Sorted(serval.ChildrenByKey("parent"), by...),
		}},
	})
	if err != nil {
		t.Fatalf("stating the tree: %v", err)
	}
	return src
}

// captionsHeld is what the view can name, as `position:caption`, so a test can say
// where a row stands as well as what it says.
func captionsHeld(tv *TreeView) string {
	var out []string
	for _, at := range tv.held() {
		if item := tv.rowAt(at); item != nil {
			out = append(out, fmt.Sprintf("%d:%s", at, item.Text))
		}
	}
	return strings.Join(out, " ")
}

// **Opening a node SHIFTS what is below it and keeps what is above.**
//
// Which is the whole claim: no record went stale, so nothing above the mark is
// re-asked and nothing above the mark moves. The row that stood at position 1
// stands at kids+1, because that is how many rows appeared -- and the figure came
// off the row that was clicked.
func TestOpeningANodeShiftsTheRowsBelowIt(t *testing.T) {
	tv := windowed(t, 40)
	tv.SetSource(kinWindow(t, 6, 4))

	first, second := tv.Item(0), tv.Item(1)
	if first == nil || second == nil {
		t.Fatalf("the top level reads %q", captionsHeld(tv))
	}
	if first.Kids != 4 {
		t.Fatalf("the first folder says it has %d children, want the four it has",
			first.Kids)
	}

	tv.ExpandItem(first)

	if got := tv.Item(0); got != first {
		t.Errorf("the row at the mark became %q", caption(got))
	}
	// The second folder moved down by exactly its sibling's children.
	if at, ok := tv.positionOf(second); !ok || at != 5 {
		t.Errorf("the second folder stands at %d (%v), want 5", at, ok)
	}
	if got := tv.Item(5); got != second {
		t.Errorf("row 5 holds %q, want the second folder", caption(got))
	}
	// And the children are in between, in order.
	for k := 0; k < 4; k++ {
		want := fmt.Sprintf("folder00/kid%02d", k)
		if got := tv.Item(1 + k); got == nil || got.Text != want {
			t.Errorf("row %d reads %q, want %q", 1+k, caption(got), want)
		}
	}
}

// caption is a row's text, and a word for a blank, so a failure reads as a row
// rather than as a struct.
func caption(item *TreeItem) string {
	if item == nil {
		return "<blank>"
	}
	return item.Text
}

// **A gap in what is held is a gap in what can be reconstructed.**
//
// Parentage is rebuilt by the rule a whole flattening used -- the parent of a row
// at depth d is the last row seen at d-1 -- and applying that ACROSS a gap would
// hang a row off an ancestor from somewhere else entirely. A thumb dragged leaves
// the view in two places for a moment, and the rows at the top of the far one have
// no ancestry here: what they DO have is the depth the source said and the chain it
// wrote down, which is why the indent and the twisty still work.
func TestAGapInWhatIsHeldIsAGapInTheParentage(t *testing.T) {
	tv := windowed(t, 8)
	src := kinWindow(t, 200, 4)
	tv.SetSource(src)
	src.ExpandAll()
	tv.moved()

	// A stretch a long way off, beginning on a CHILD row, so nothing above it is
	// held and the run before it is somewhere else entirely.
	tv.window(601, 20)
	if len(tv.bones.runs) < 2 {
		t.Fatalf("the spine holds %d run(s); this needs two", len(tv.bones.runs))
	}
	far := tv.rowAt(601)
	if far == nil {
		t.Fatalf("row 601 is blank; the view holds %q", captionsHeld(tv))
	}
	if far.Level() != 1 {
		t.Fatalf("row 601 reads %q at level %d, want a child", far.Text, far.Level())
	}
	if far.Parent != nil {
		t.Errorf("row 601 (%q) hangs off %q, which is in the other run entirely",
			far.Text, far.Parent.Text)
	}
	// And the chain is still whole, which is what a click needs.
	if got := tv.chainOf(far); len(got) != 2 {
		t.Errorf("its chain is %v, want the two segments down to it", got)
	}
}

// **And the rows appear the INSTANT the twisty flips**, before anything answers.
//
// That is #60, and it is the same move: laying out `Kids` blanks is the shift. The
// check is made against the spine directly, because a source holding its records
// answers inside the call that asked -- so the only way to see the blanks is to
// look before the reading happens.
func TestOpeningLaysTheRowsOutBeforeTheyArrive(t *testing.T) {
	tv := windowed(t, 40)
	tv.SetSource(kinWindow(t, 6, 4))

	first := tv.Item(0)
	if first == nil {
		t.Fatal("no first row")
	}
	was := tv.bones.rows()

	// The mark moves and the view is told, and nothing is read.
	if !tv.tellMarks(first, true) {
		t.Fatal("the marks would not take the chain")
	}
	tv.opened(0, first)

	if got, want := tv.bones.rows(), was+4; got != want {
		t.Errorf("it would draw %d rows, want %d -- the four that appeared", got, want)
	}
	for k := 1; k <= 4; k++ {
		if _, held := tv.bones.idAt(k); held {
			t.Errorf("row %d came back named before anything answered", k)
		}
	}
	// A blank draws as a blank rather than as nothing.
	if got := tv.drawRow(2); got != blankTreeRow {
		t.Errorf("row 2 would paint %q, want a blank", caption(got))
	}
}

// Closing is the same move the other way about, and it is EXACT: the rows going
// are the ones in hand.
func TestClosingANodeTakesOutExactlyTheRowsUnderIt(t *testing.T) {
	tv := windowed(t, 40)
	tv.SetSource(kinWindow(t, 6, 4))

	first, second := tv.Item(0), tv.Item(1)
	tv.ExpandItem(first)
	if at, _ := tv.positionOf(second); at != 5 {
		t.Fatalf("the second folder is at %d before closing", at)
	}
	if got := tv.closing(0); got != 4 {
		t.Errorf("it says %d rows are showing under the first folder, want 4", got)
	}

	tv.CollapseItem(first)

	if at, ok := tv.positionOf(second); !ok || at != 1 {
		t.Errorf("after closing, the second folder stands at %d (%v), want 1", at, ok)
	}
}

// **A delta nobody can work out forgets from the mark DOWN**, and keeps what is
// above -- which is very often the whole of what is on screen.
//
// An openAll region is the case: opening a node inside one reveals a whole subtree,
// because the children inherit the openAll rather than defaulting closed, and a
// subtree's size is not free. `Kids` is then a floor, and a shift by too little
// would put every row below in the wrong place.
func TestADeltaNobodyCanWorkOutForgetsOnlyBelowTheMark(t *testing.T) {
	tv := windowed(t, 40)
	src := kinWindow(t, 6, 4)
	tv.SetSource(src)

	first := tv.Item(0)
	if first == nil {
		t.Fatal("no first row")
	}
	// Everything open, then the first folder closed inside it: reopening the folder
	// resumes the openAll beneath, which is a subtree and not a level.
	src.ExpandAll()
	tv.moved()
	first = tv.Item(0)
	tv.CollapseItem(first)
	if got := tv.opening(first); got != -1 {
		t.Errorf("inside an expand-all it claims a delta of %d, want no claim", got)
	}

	// It still knows what stands above the mark. Put the reader further in, so
	// there IS something above to keep.
	third := tv.Item(2)
	if third == nil {
		t.Fatalf("the tree reads %q", captionsHeld(tv))
	}
	tv.bones.forgetFrom(2)
	if _, held := tv.bones.idAt(0); !held {
		t.Error("forgetting below the mark took the row above it")
	}
	if _, held := tv.bones.idAt(2); held {
		t.Error("forgetting below the mark left the row at it")
	}
	if got := tv.bones.length(); got.Exact {
		t.Errorf("it still claims an exact %v below a mark it forgot past", got)
	}
}

// A row NOBODY could count draws its twisty and finds out on opening, which is an
// unknown delta and not a delta of nought.
func TestARowNobodyCouldCountClaimsNoDelta(t *testing.T) {
	tv := windowed(t, 40)
	tv.SetSource(kinWindow(t, 2, 2))
	item := tv.Item(0)
	if item == nil {
		t.Fatal("no first row")
	}
	item.Kids = -1
	if got := tv.opening(item); got != -1 {
		t.Errorf("a row nobody could count claims a delta of %d", got)
	}
}

// --- the chain a windowed row carries ------------------------------------

// **A row deep in a scrolled tree opens the right node.**
//
// The chain used to be walked back up `Parent`, which works for a view holding the
// whole pre-order and cannot work for one holding a window: everything above the
// window is exactly what the view declined to hold, so a row's chain came back one
// segment long and clicking its twisty marked a node that is not there. Nothing
// reported it -- the marks have no opinion about a chain nobody walked -- so the
// twisty simply did not move.
func TestARowKeepsItsChainOutsideTheWindow(t *testing.T) {
	tv, deep := scrolledDeep(t)

	chain := tv.chainOf(deep)
	if len(chain) != deep.Level()+1 {
		t.Errorf("a row at level %d carries a chain of %d: %v",
			deep.Level(), len(chain), chain)
	}
	// And it is the chain the MARKS answer to: closing its parent hides it.
	tv.marks().Collapse(chain[:len(chain)-1]...)
	tv.moved()
	if at, held := tv.positionOf(deep); held {
		t.Errorf("after closing its parent the row still stands at %d", at)
	}
}

// scrolledDeep is a view a long way down a thousand-row tree, and a child row it
// holds whose every ancestor it does not.
//
// More than `spineKept` rows above the reader, deliberately: the point is that the
// rows the chain names have been DROPPED, not merely that they are off screen.
func scrolledDeep(t *testing.T) (*TreeView, *TreeItem) {
	t.Helper()
	tv := windowed(t, 8)
	src := kinWindow(t, 200, 4)
	tv.SetSource(src)
	src.ExpandAll()
	tv.moved()

	scrollTo(t, tv, 900)
	var deep *TreeItem
	for at := 900; at < 910; at++ {
		if item := tv.rowAt(at); item != nil && item.Level() > 0 {
			deep = item
			break
		}
	}
	if deep == nil {
		t.Fatalf("no child row around 900; the view holds %q", captionsHeld(tv))
	}
	if _, held := tv.bones.idAt(0); held {
		t.Fatal("the view is still holding the top of the tree, so this proves nothing")
	}
	return tv, deep
}

// The depth comes off the row too, for the same reason: a view holding a window
// cannot count its way up to the top, and an indent worked out from a walk would
// draw a row forty deep flush against the margin.
func TestALevelComesFromWhatTheSourceSaid(t *testing.T) {
	_, deep := scrolledDeep(t)
	if deep.Level() == 0 {
		t.Error("a row whose ancestors are outside the window reads as a root")
	}
	if deep.rowDepth != deep.Level() {
		t.Errorf("Level() answers %d and the source said %d", deep.Level(), deep.rowDepth)
	}
}

// **A subtree running past what anybody has COUNTED cannot be closed exactly.**
//
// The end of what is held is not the end of the sequence. A floor says there may be
// more below, and rows nobody has counted are rows that may be part of this
// subtree -- so taking the floor for the end would take out too few and leave every
// row after the mark one place too low.
func TestASubtreeRunningPastWhatIsCountedCannotBeClosedExactly(t *testing.T) {
	tv := NewTreeView()
	// A folder and four children, and the sequence FLOORED there rather than
	// counted: the walk stopped where its budget ran out.
	tv.bones.place(0, deep(0, 0, 1, 1, 1, 1))
	tv.bones.learn(serval.Complete{Total: serval.AtLeast(5)})
	if got := tv.closing(0); got != -1 {
		t.Errorf("it says %d rows stand under the folder, and the subtree runs past"+
			" everything anybody has counted", got)
	}

	// Counted to its end, the same rows give an exact answer.
	tv.bones.learn(serval.Complete{Total: serval.Exactly(5)})
	if got := tv.closing(0); got != 4 {
		t.Errorf("with the sequence counted it says %d, want the four children", got)
	}

	// And a mark the view cannot even name says nothing.
	if got := tv.closing(40); got != -1 {
		t.Errorf("it says %d rows stand under a row it has never heard of", got)
	}
}
