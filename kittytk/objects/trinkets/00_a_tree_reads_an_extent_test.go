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

// tall is a view with a height, so it asks for an Extent rather than for as much
// as it is willing to hold. A view nobody sized reads what the spine will keep, and
// a test about Extents should not rest on that fallback.
func tall(t *testing.T, rows int) *TreeView {
	t.Helper()
	tv := NewTreeView()
	tv.SetKindMap("", InOrder("name"))
	tv.SetBounds(core.UnitRect{Width: 60 * cell, Height: core.Unit(rows) * 2 * cell})
	if got := tv.visibleCount(); got == 0 {
		t.Fatalf("a view %d cells tall reports no visible rows", rows*2)
	}
	return tv
}

// **A view reads a WINDOW, and is told how long the sequence is anyway.** The two
// used to be a trade: the rows it holds are the rows it is looking at, and what it
// knew about the rest was a floor that grew as it scrolled.
//
// It is not a trade. A flattening's length is the top level counted plus the children
// of every open node, and neither of those is a row read -- so a view holding thirty
// rows of five thousand draws a TRUE thumb and can be dragged anywhere in the
// sequence. See serval's reckon.go.
func TestATreeReadsAWindowOfADeclaredSource(t *testing.T) {
	tv := tall(t, 10)
	tv.SetSource(manyRows(5000))

	held := tv.bones.held()
	if held == 0 {
		t.Fatal("it read nothing at all")
	}
	if held > 200 {
		t.Errorf("it is holding %d rows of five thousand", held)
	}
	if got := tv.Length(); got != serval.Exactly(5000) {
		t.Errorf("it says the sequence holds %v, want exactly five thousand -- one"+
			" count of the top level, and nothing walked", got)
	}

	// The rows it holds are the ones at the top, which is where the reader is.
	if got := tv.Item(0); got == nil || got.Text != "row00000" {
		t.Errorf("the first row reads %q", caption(got))
	}

	// And a row nine hundred down is reachable straight away, the length being known:
	// a thumb can be dragged to a place the view can say is there.
	if got := tv.Item(900); got == nil || got.Text != "row00900" {
		t.Errorf("row 900 reads %q", caption(got))
	}
	if got := tv.Length(); got != serval.Exactly(5000) {
		t.Errorf("after reading row 900 it says %v", got)
	}
	// Reading it did not turn the Extent into a log.
	if n := tv.bones.held(); n > spineKept {
		t.Errorf("after scrolling it is holding %d identities, and the cap is %d",
			n, spineKept)
	}
}

// scrollTo moves the reader to a position, asking for an Extent there.
//
// **A tree with an expand-all in force still cannot be jumped into**, and that is
// the model rather than a gap: under OpenAll an open node's contribution is its whole
// subtree, and nobody has counted one. So the length is a floor, the thumb shrinks as
// the reader scrolls, and reaching row nine hundred means reading down to it -- which
// is what scrolling does.
func scrollTo(t *testing.T, tv *TreeView, at int) {
	t.Helper()
	for step := 0; step < 2000; step++ {
		if at < tv.Count() {
			tv.scrollOffset = at
			tv.Count() // which is what asks for the Extent there
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
	tv := tall(t, 10)
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
	tv := tall(t, 40)
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
	tv := tall(t, 8)
	src := kinWindow(t, 200, 4)
	tv.SetSource(src)
	src.ExpandAll()
	tv.moved()

	// A stretch a long way off, beginning on a CHILD row, so nothing above it is
	// held and the run before it is somewhere else entirely.
	tv.extent(601, 20)
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
	tv := tall(t, 40)
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
	tv := tall(t, 40)
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
	tv := tall(t, 40)
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
	tv := tall(t, 40)
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

// --- the chain a read by Extents row carries ------------------------------------

// **A row deep in a scrolled tree opens the right node.**
//
// The chain used to be walked back up `Parent`, which works for a view holding the
// whole pre-order and cannot work for one holding an Extent: everything above the
// Extent is exactly what the view declined to hold, so a row's chain came back one
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
	tv := tall(t, 8)
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

// The depth comes off the row too, for the same reason: a view holding an Extent
// cannot count its way up to the top, and an indent worked out from a walk would
// draw a row forty deep flush against the margin.
func TestALevelComesFromWhatTheSourceSaid(t *testing.T) {
	_, deep := scrolledDeep(t)
	if deep.Level() == 0 {
		t.Error("a row whose ancestors are outside the Extent reads as a root")
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

// --- what a selection is ------------------------------------------------
//
// An IDENTITY, and the index only stands for it. Three things move a row out from
// under an index -- a resort, a node opening above it, and scrolling far enough that
// the view stops holding it -- and an index kept as the authority quietly named a
// different row after any of them.

// **A resort does not lose the selection**, even when it moves the row outside the
// Extent. The index goes; the selection does not.
func TestAResortKeepsTheSelectionOutsideTheWindow(t *testing.T) {
	tv := tall(t, 8)
	tv.SetSource(manyRows(400))
	tv.SetKindMap("", InOrder("name"))

	chosen := tv.Item(0)
	if chosen == nil {
		t.Fatal("no first row")
	}
	tv.SetCurrentIndex(0)
	if tv.CurrentItem() != chosen {
		t.Fatalf("choosing row 0 chose %q", caption(tv.CurrentItem()))
	}

	// Reversed: the first row becomes the last, which is far outside the Extent.
	tv.SetSorted(true, -1, true)

	if at, held := tv.positionOf(chosen); held {
		t.Fatalf("the row is still at %d, so this proves nothing", at)
	}
	if tv.CurrentIndex() != -1 {
		t.Errorf("it claims the selection stands at %d, and it cannot place it",
			tv.CurrentIndex())
	}
	if got := tv.CurrentItem(); got != chosen {
		t.Errorf("the selection reads %q, want the row that was chosen",
			caption(got))
	}
}

// And it comes back the moment the row does, which is `resolve` running when a
// Extent lands rather than anything asking.
func TestAnUnplacedSelectionResolvesWhenItsRowReturns(t *testing.T) {
	tv := tall(t, 8)
	tv.SetSource(manyRows(400))

	chosen := tv.Item(0)
	tv.SetCurrentIndex(0)

	// Away, so the view stops holding it.
	scrollTo(t, tv, 380)
	tv.bones.forget()
	tv.scrollOffset = 380
	tv.Count()
	if tv.CurrentIndex() != -1 {
		t.Fatalf("it still places the selection at %d", tv.CurrentIndex())
	}
	if tv.CurrentItem() != chosen {
		t.Fatal("it lost the selection on the way")
	}

	// And back.
	tv.scrollOffset = 0
	tv.Count()
	if got := tv.CurrentIndex(); got != 0 {
		t.Errorf("with the row back in the Extent it stands at %d, want 0", got)
	}
}

// **Nothing chosen and chosen-but-unplaced are different states**, and a movement
// key has to tell them apart: Down from nothing chooses the FIRST row, and Down from
// an unplaced selection moves from where the reader is looking.
func TestAMovementKeyTellsUnchosenFromUnplaced(t *testing.T) {
	tv := tall(t, 8)
	tv.SetSource(manyRows(400))

	// Nothing chosen: before the first row, so Down lands on it.
	if got := tv.movingFrom(); got != -1 {
		t.Errorf("with nothing chosen a movement starts from %d, want before the"+
			" first row", got)
	}

	// Chosen, then scrolled away from.
	tv.SetCurrentIndex(0)
	tv.bones.forget()
	tv.scrollOffset = 200
	tv.Count()
	if tv.CurrentIndex() != -1 {
		t.Fatalf("it still places the selection at %d", tv.CurrentIndex())
	}
	if got := tv.movingFrom(); got != 200 {
		t.Errorf("with the selection unplaced a movement starts from %d, want where"+
			" the reader is looking", got)
	}
}

// **A Extent holds a screenful either side of the reader**, which is what treeReach
// says and what asking from the scroll offset alone did not do: scrolling back one
// line was a fresh question for rows the view had been holding a moment earlier.
func TestAWindowHoldsRowsAboveTheReaderToo(t *testing.T) {
	tv := tall(t, 8)
	tv.SetSource(manyRows(400))
	scrollTo(t, tv, 200)

	at, n := tv.asking()
	if at >= tv.scrollOffset {
		t.Errorf("it asks from %d with the reader at %d, so nothing above is held",
			at, tv.scrollOffset)
	}
	if _, held := tv.bones.idAt(tv.scrollOffset - 1); !held {
		t.Errorf("the row just above the reader is blank; the Extent is %d from %d",
			n, at)
	}
}

// A view reading its OWN items holds all of them, wherever the reader stands -- so
// asking for the whole sequence starts at the top rather than at the viewport.
func TestAWholeReadStartsAtTheTop(t *testing.T) {
	tv := NewTreeView()
	for i := 0; i < 40; i++ {
		tv.AddRootItem(NewTreeItem(fmt.Sprintf("item%02d", i)))
	}
	tv.SetBounds(core.UnitRect{Width: 60 * cell, Height: 8 * cell})

	tv.scrollOffset = 30
	tv.Count()
	if at, _ := tv.asking(); at != 0 {
		t.Errorf("a whole read asks from %d, want the top", at)
	}
	if got := tv.rowAt(0); got == nil {
		t.Error("the first row is blank over a source that holds every row")
	}
	if n := tv.bones.held(); n != 40 {
		t.Errorf("it holds %d of its own forty items", n)
	}
}

// **Item will ask past the floor.** A tree's length is a floor until the walk
// reaches the end, and refusing a position past it made a read by Extents tree unjumpable
// -- though serval answers, a scope's From being the walk's own budget.
func TestItemAsksPastTheFloor(t *testing.T) {
	// An expand-all, which is the state a length really is a floor in: under OpenAll
	// an open node's contribution is a whole subtree and nobody has counted one.
	tv := tall(t, 10)
	src := kinWindow(t, 200, 4)
	tv.SetSource(src)
	src.ExpandAll()
	tv.moved()

	floor := tv.Length()
	if floor.Exact {
		t.Fatalf("the length reads %v; this needs a floor", floor)
	}
	if floor.N > 500 {
		t.Fatalf("the floor is already %v, so 500 is not past it", floor)
	}

	if got := tv.Item(500); got == nil {
		t.Error("row 500 is blank, and it is past the floor")
	}
	if now := tv.Length(); now.N <= floor.N {
		t.Errorf("the floor stayed at %v after walking to row 500", now)
	}

	// Past the END of the sequence is a blank, the walk having run out of tree.
	if got := tv.Item(999999); got != nil {
		t.Errorf("row 999999 reads %q over a body of a thousand", got.Text)
	}
	// And before the beginning is still nothing.
	if got := tv.Item(-1); got != nil {
		t.Errorf("row -1 reads %q", got.Text)
	}
}

// Collapsing a node the selection was INSIDE moves the selection to the node, and
// the identity follows the index there.
//
// It is the one place a row is chosen that the reader did not choose: the row they
// did choose has just stopped being in the sequence, and the node they closed is the
// nearest thing to where they were. The identity has to follow, or the next Extent
// landing would resolve against the row that went and unplace a selection that is
// perfectly well placed.
func TestCollapsingOntoTheNodeMovesTheIdentityToo(t *testing.T) {
	tv := tall(t, 40)
	tv.SetSource(kinWindow(t, 6, 4))

	folder := tv.Item(0)
	tv.ExpandItem(folder)
	kid := tv.Item(2)
	if kid == nil || kid.Level() != 1 {
		t.Fatalf("row 2 reads %q at level %d, want a child", caption(kid), kid.Level())
	}
	tv.SetCurrentIndex(2)

	tv.CollapseItem(folder)

	if got := tv.CurrentItem(); got != folder {
		t.Errorf("after closing the folder the selection reads %q, want the folder",
			caption(got))
	}
	if got := tv.CurrentIndex(); got != 0 {
		t.Errorf("it stands at %d, want the folder's own position", got)
	}
	// And an Extent landing does not undo it, which is what a stale identity would do.
	tv.extent(tv.asking())
	if got := tv.CurrentIndex(); got != 0 {
		t.Errorf("an Extent landing moved it to %d", got)
	}
}

// **A count earned before a change does not outlive it.** A length only ever
// replaces one that says less, so that a notice cannot leave the thumb flickering
// between a scale and none -- and that is right for a sequence that is still the same
// sequence. Across a change the view cannot describe it is wrong: an exact figure
// would outrank every honest floor that followed it, for ever.
func TestACountDoesNotOutliveTheSequenceItCounted(t *testing.T) {
	tv := tall(t, 10)
	src := kinWindow(t, 200, 4)
	tv.SetSource(src)

	// Nothing open: two hundred folders, counted.
	if got := tv.Length(); got != serval.Exactly(200) {
		t.Fatalf("with nothing open it says %v, want exactly the two hundred", got)
	}

	// Everything open, which is a length nobody can count -- and the old figure must
	// not stand in for it.
	src.ExpandAll()
	tv.moved()
	if got := tv.Length(); got.Exact {
		t.Errorf("under an expand-all it still says %v", got)
	}
	if got := tv.Length(); got.N < 200 {
		t.Errorf("it floors the sequence at %v, below the top level it had counted",
			got)
	}
}

// **A true thumb over an Extent, which used to be a contradiction.**
//
// A view that reads a screenful can say how long the whole sequence is, because a
// flattening's length is its top level counted plus the children of every open node
// and neither of those is a row read. So the thumb is true from the first frame and
// the end of the sequence is reachable -- and opening a folder keeps it true, the
// children being counted rather than walked.
func TestAWindowedViewDrawsATrueThumb(t *testing.T) {
	tv := tall(t, 8)
	tv.SetSource(kinWindow(t, 100, 4))

	if got := tv.Length(); got != serval.Exactly(100) {
		t.Fatalf("with nothing open it says %v, want exactly the hundred folders", got)
	}
	if n := tv.bones.held(); n >= 100 {
		t.Errorf("it is holding %d of the hundred rows; this is meant to be an Extent", n)
	}

	// Opening one keeps it exact: four more rows, counted and not walked.
	tv.ExpandItem(tv.Item(0))
	if got := tv.Length(); got != serval.Exactly(104) {
		t.Errorf("with one folder open it says %v, want exactly a hundred and four", got)
	}
	// And a second, somewhere else entirely.
	tv.ExpandItem(tv.Item(5))
	if got := tv.Length(); got != serval.Exactly(108) {
		t.Errorf("with two folders open it says %v, want exactly a hundred and eight",
			got)
	}

	// Closing takes it back, which is the same sum the other way about.
	tv.CollapseItem(tv.Item(0))
	if got := tv.Length(); got != serval.Exactly(104) {
		t.Errorf("after closing one it says %v, want exactly a hundred and four", got)
	}
}

// And an expand-all is the one state that still floors -- the thumb shrinks there,
// which is honest: nobody has counted a subtree.
func TestAnExpandAllStillFloorsTheView(t *testing.T) {
	tv := tall(t, 8)
	src := kinWindow(t, 100, 4)
	tv.SetSource(src)
	if got := tv.Length(); !got.Exact {
		t.Fatalf("with nothing open it says %v", got)
	}

	src.ExpandAll()
	tv.moved()
	if got := tv.Length(); got.Exact {
		t.Errorf("under an expand-all it says %v, want a floor", got)
	}
	// **But a floor of at least the top level**, every one of whose rows is in the
	// flattening -- not a floor of the rows on screen, which would be a thumb that
	// lurched to a twelfth of its size the moment somebody expanded everything.
	if got := tv.Length(); got.N < 100 {
		t.Errorf("it floors the sequence at %v, below the top level it had counted",
			got)
	}
}
