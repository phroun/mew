package trinkets

// Where a tree's rows come from.
//
// The same move a list made, and for the same reason: one mechanism, whether
// the rows are the tree's own or somebody else's. A tree given no source MAKES
// one out of the items it was handed, so there is no second code path for the
// plain case -- and every existing example goes on meaning exactly what it
// meant.
//
// # What a made source is
//
//	TreeSource               the rows that are VISIBLE, flat, in pre-order
//	  ListSource             every item this tree holds, as an adjacency list
//
// **An adjacency list, because that is what a tree of items already is.** Each
// row carries its parent's identity and nothing else about where it stands: no
// path, no delimiter, no address field. It is the shape serval's tree is
// simplest over, and it is the shape a `Parent` pointer already describes.
//
// # Keys are the items' own
//
// Unlike a list, a tree has a stable identity to offer: every TreeItem carries
// an ObjectID from the moment it is made. So a row's key is that, and the same
// item keeps the same key across an expand, a collapse, a sort and an insert --
// which is what lets the flattened sequence be turned back into the very
// POINTERS the tree was given, so that everything reading a row goes on
// comparing pointers as it always has.
//
// # The source sorts, through the mapping
//
// A tree states its sort in COLUMNS, and each kind of row translates that into
// fields of its own -- see treemap.go. So serval does the sorting, one mechanism
// whether the rows are the tree's own or somebody else's, and a made row carries
// a typed value per column for it to sort by: a size in bytes and not "1.2 MB",
// because the number is what the column MEANS.
//
// `seq` is still there and has a better job than it started with. It was a bridge
// while the source was not asked to sort; it is now the LAST sort level, which is
// what makes the sort stable -- rows equal on every level keep the order the
// application put them in, exactly as `sort.SliceStable` did. Unsorted, it is the
// whole order, so a tree whose items were inserted rather than appended still
// draws them where they were put.

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/serval"
)

// The fields a made row carries. Only two, because the rest of what a tree row
// holds -- its text, its icon, its cells -- is still on the TreeItem the key
// leads back to.
const (
	treeParent = "parent" // the parent's key, and undefined for a root
	treeSeq    = "seq"    // where it stands among its siblings
)

// treeFields is where a tree's OWN six fields go, moved out of the data's way.
//
// A flattening carries a depth, a path, a child count, a mark state, a kind and the
// chain of mark segments down to the row, and serval writes them into each row -- **dropping the record's own field of that
// name**, because a view cannot draw without them. That is deliberate and
// documented there, and the conclusion drawn from it is that the names are the
// caller's to move. This is the caller.
//
// It has to move them because of what sits on the other side: a view's columns are
// named by whoever wrote the window, and serval's defaults are `depth`, `path`,
// `expandable`, `state` and `kind`. A file listing with a Kind column is the most
// ordinary thing anybody builds, and with the default names its every cell came back
// empty -- the tree having eaten the field and put its own node-type kind, which for
// a tree of one shape is the empty string. Nothing failed; a column was simply
// blank.
//
// The `tree:` mark is the wire's own way of saying which namespace a name is in, so
// a data field colliding with one of these would have to be called `tree:kind` on
// purpose. These names never cross the wire -- a tree adds them after a level has
// answered, so they exist only in the rows a view reads.
var treeFields = serval.TreeFields{
	Depth:      "tree:depth",
	Path:       "tree:path",
	Expandable: "tree:expandable",
	State:      "tree:state",
	Kind:       "tree:kind",
	Chain:      "tree:chain",
}

// TreeFieldNames is where a tree's own fields go, for anybody building a
// `serval.TreeSource` a TreeView will read.
//
// Exported because a tree is not always built here -- the connections window builds
// its own, and so may an application -- and every one of them is putting these five
// names beside fields somebody else chose. A view does not need this to READ a tree
// (it asks the source, through `serval.TreeFieldsOf`); what it is for is not eating
// a column on the way in.
func TreeFieldNames() serval.TreeFields { return treeFields }

// treeReach is how many screenfuls a tree reads: the rows on show, and one
// either side, so a wheel notch or a page key is answered out of what is held
// rather than by another question.
const treeReach = 3

// everyTreeRow is the count a WHOLE flattening is read with -- a ceiling against
// a source that would answer forever, rather than an Extent.
const everyTreeRow = 1 << 30

// reach is how many rows this view asks for.
//
// **A tree of its OWN items reads all of them, and that is an Extent declined
// rather than an Extent forgotten.** A made source's rows are built out of the very
// items this view is holding -- makeSource walks them on every rebuild -- so
// reading an Extent of them saves no memory, no decoding and no round trip, and
// costs a question every time the reader scrolls. It also keeps two answers a
// whole sequence gives and an Extent cannot: an exact count without a walk to earn
// it, and a position for any item, which is what a resort following its selection
// needs.
//
// The cost an Extent exists to avoid is somewhere else entirely: a DECLARED source,
// whose records have to be fetched, and whose hundred thousand rows were being
// answered in full to fill forty lines -- and, past the cache's budget, answered
// again and again because the walk never settled.
//
// **The floor for a declared source is what the spine will KEEP.** Bounds arrive
// after construction and plenty of callers read rows before they do -- a bundle
// load, a window laying itself out, a test -- so a view whose height is not known
// yet has to ask for something, and answering "no rows" would be reporting the
// layout rather than the data. Asking for as much as it is willing to hold is the
// one figure that needs no second justification.
func (t *TreeView) reach() int {
	if t.source == nil {
		return everyTreeRow
	}
	if n := t.visibleCount() * treeReach; n > 0 {
		return n
	}
	return spineKept
}

// asking is the stretch this view wants: where to ask from, and how many.
//
// **A whole-sequence read starts at the TOP.** That is what reading the whole
// sequence means -- a view of its own items holds every one of them -- and asking
// for all of them FROM where the reader happens to be standing left everything
// above the viewport a placeholder, which is how a selection scrolled past came back
// unplaceable over a source that holds all its rows.
//
// **And an Extent starts a screenful ABOVE the reader**, not at it. `treeReach` says
// the rows on show and one either side, and asking from the scroll offset only ever
// bought the screenful below: scrolling back one line was a fresh question every
// time, for rows the view had been holding a moment earlier.
// The whole-read branch is an EQUIVALENT mutant today, and kept: a screenful of
// `everyTreeRow` is three hundred million rows, so the arithmetic below clamps to
// nought for any scroll offset anybody could reach. It says the intent directly
// rather than resting on that, and it is what stops a smaller ceiling than
// `everyTreeRow` quietly taking an Extent of a source that is meant to be read whole.
func (t *TreeView) asking() (int, int) {
	n := t.reach()
	if n >= everyTreeRow {
		return 0, n
	}
	at := t.scrollOffset - n/treeReach
	if at < 0 {
		at = 0
	}
	return at, n
}

// extent makes sure the spine can name the rows from a position on, asking the
// sequence about the ones it cannot.
//
// It asks for the stretch WHOLE rather than for the gaps in it, for the same
// reason a list does: an Extent is a screenful, the answer is one question, and
// three questions to fill three holes cost three times as much as one for all of
// it.
//
// **This is what `everyTreeRow` used to be.** A view read the whole flattening
// on every rebuild, which for a hundred thousand rows across a connection is the
// whole body answered to fill forty lines -- and, past the cache's budget, a walk
// that never settled. serval's flattening takes the scope as a budget and asks
// each level for no more than the walk still needs, so an Extent here is an Extent
// all the way down.
func (t *TreeView) extent(at, n int) {
	if n <= 0 {
		return
	}
	if at < 0 {
		at = 0
	}
	if t.spineHolds(at, n) {
		return
	}
	set := t.sequence()
	if set == nil {
		return
	}

	// **After a RECORD where one names the same place, and at a position otherwise.**
	// A position is best effort -- a source honours it as well as it can -- and a
	// record is exact, so the row immediately above the stretch is the better
	// question wherever the view holds it.
	//
	// **And where this sequence will not jump to a position, after whatever is held
	// BELOW the stretch, however far below.** That is the case a source walking its
	// own body is in: it answered from the beginning, so the view holds rows nowhere
	// near the one it wants -- and asking for the same position again would be asking
	// the same unanswerable question, which is how a reader came to ask for row nine
	// hundred for ever and never move. Carrying on from what it holds converges,
	// which is the convergence `Scope.From` describes.
	//
	// It costs a question per stretch, which is what a source that cannot skip costs.
	// One that honours `from` is answered in a single question and never reaches here.
	scope := &serval.Scope{Count: n}
	before, ok := t.bones.idAt(at - 1)
	if !ok && t.walks {
		// **And the count covers the GAP, not the stretch.** A walk that asked for
		// only the rows wanted advanced by that many a question -- one row a question
		// for a caller asking one row at a time -- so reaching row three hundred took
		// three hundred questions. Asking for the gap reaches it in one wherever the
		// source will send that many.
		//
		// Capped at what the spine will KEEP, because a question for more than that is
		// a question whose answer is thrown away as it arrives. So a far place costs a
		// question per `spineKept` rows, which is the price of a source that cannot
		// skip and is the price an application removes by honouring `from`.
		if last, id, held := t.bones.lastBefore(at); held {
			before, ok = id, true
			if gap := at - last - 1 + n; gap > scope.Count {
				scope.Count = gap
				if scope.Count > spineKept {
					scope.Count = spineKept
				}
			}
		}
	}
	if ok && at > 0 {
		scope.After = before
	} else {
		scope.From = at
	}
	positional := scope.After == nil
	if err := set.Read(scope, &treeSink{
		tree: t, expected: at, asked: t.asks, positional: positional,
	}); err != nil {
		return
	}
}

// ask asks for a stretch, superseding whatever was asked before.
//
// The count does not move where the stretch is one the view already holds: there
// is nothing outstanding, so nothing to supersede, and bumping it would discard
// an answer still on its way to somewhere the reader has NOT left.
func (t *TreeView) ask(at, n int) {
	if t.spineHolds(at, n) {
		return
	}
	t.asks++
	t.extent(at, n)
}

// current reports whether an answer is still the one being waited for.
func (t *TreeView) current(a asking) bool { return a == t.asks }

// spineHolds reports whether every row of a stretch is already named.
func (t *TreeView) spineHolds(at, n int) bool {
	for i := 0; i < n; i++ {
		if _, ok := t.bones.idAt(at + i); !ok {
			return false
		}
	}
	return true
}

// rowCount is how many rows the tree draws.
//
// **The viewport is asked for first, and that is not laziness dressed up.** A
// list's source counts without being read -- a ListSource knows how many rows it
// holds -- but a tree's count IS the walk: a flattening that has not happened has
// counted nothing, so a view that read the count before asking for an Extent would
// be told nought rows for ever and never ask.
//
// What comes back may be a floor. A walk that stopped where its budget ran out
// knows there may be more and says so, which is what makes the thumb shrink as
// the reader scrolls rather than lie about where the end is.
func (t *TreeView) rowCount() int {
	set := t.sequence()
	if set == nil {
		return 0
	}
	t.ask(t.asking())
	t.bones.learn(serval.Complete{Total: serval.CountOf(set)})
	return t.bones.rows()
}

// rowAt is the item standing at a position, and nil for a row that is BLANK --
// one the view knows is there and knows nothing else about yet.
//
// Blank is ordinary. It is what every row is between the moment a thumb moves and
// the moment the answer arrives, and drawing one is drawing an empty row rather
// than drawing nothing.
func (t *TreeView) rowAt(at int) *TreeItem {
	id, ok := t.bones.idAt(at)
	if !ok {
		return nil
	}
	// A DECLARED source's rows lead to the items learnRow made for them; a made
	// source's lead back to the very items the caller handed in.
	if t.source != nil {
		return t.fromSource[serval.Key(id)]
	}
	if id.IsInt {
		return t.byID[objectIDOf(id)]
	}
	return nil
}

// Count is how many rows the tree draws.
//
// **It may be a FLOOR, and that is the honest answer rather than a shortcoming.**
// A tree's count is its walk, so a view that has read an Extent of a declared
// source has been told "at least this many" -- and a thumb drawn against a floor
// shrinks as the reader scrolls rather than lying about where the end is. Length
// says which of the two it is.
func (t *TreeView) Count() int { return t.rowCount() }

// Length is how long the sequence is, and how well that is known.
//
// Three answers, and they are three different things rather than three guesses.
// Exactly is a sequence walked to its end. AtLeast is a floor -- part of one seen
// is at least that many. Unknown is a sequence nobody has read at all.
func (t *TreeView) Length() serval.RecordCount {
	t.rowCount() // which is what learns it
	return t.bones.length()
}

// Item is the row at a position, and nil for one off the beginning.
//
// Nil is also what a BLANK row answers -- one the view knows is there and knows
// nothing else about yet, which is every row of a tree read by Extents until the answer
// arrives. Asking for one asks the source about it, so a caller drawing rows
// should ask for the stretch it wants rather than one row at a time.
//
// **It will ask past the floor, and that is the point.** A tree's length is a floor
// until the walk reaches the end, so refusing a position past it made an Extent-reading
// tree unjumpable: the view would not ask, though serval answers -- a scope's From
// IS the walk's budget, so asking for row nine hundred walks to row nine hundred and
// says so. The floor bounds what a THUMB can express, which is `clampScrollOffset`'s
// business; it is not a bound on what a caller may ask about.
//
// The cost is the walk down to the position, paid on a deliberate act. A position
// past the end of the sequence answers a placeholder, the walk having run out of tree.
func (t *TreeView) Item(at int) *TreeItem {
	if at < 0 {
		return nil
	}
	t.extent(at, 1)
	return t.rowAt(at)
}

// placeholderTreeRow is what a record the view cannot name yet is drawn as: a place with no
// words in it.
//
// Enabled, because a row nobody has described is not a row somebody has described
// as unavailable. A leaf standing at the top, because a twisty or an indent on a row
// nothing is known about would be a claim about a shape nobody has seen.
var placeholderTreeRow = &TreeItem{Enabled: true}

// drawRow is the item to PAINT at a position: the row where there is one, and a
// a placeholder where the view knows a record stands and knows nothing else about it.
//
// It is kept apart from rowAt, which tells the truth. Everything that DECIDES
// something -- a click, an edit, a selection -- has to be able to tell a row from a
// placeholder, and everything that DRAWS has to put something in the line either way.
func (t *TreeView) drawRow(at int) *TreeItem {
	if item := t.rowAt(at); item != nil {
		return item
	}
	return placeholderTreeRow
}

// held is every row the view can name, in position order, with where each one
// stands.
//
// What a walk over the whole flattening used to be. A caller that wants to look
// at every row -- measuring a column, searching for a caption -- can look only at
// the rows the view HOLDS, because the rest are placeholders and a placeholder has nothing to
// measure. A caller that needs them all has to ask for them all, and saying so
// here is what stops one quietly reading an Extent and calling it the sequence.
func (t *TreeView) held() []int {
	out := make([]int, 0, t.bones.held())
	for _, r := range t.bones.runs {
		for i := range r.rows {
			out = append(out, r.at+i)
		}
	}
	return out
}

// sequence is the stated sequence, made and opened if it is not already.
//
// Nil where there is nothing to read, which is an empty tree and is ordinary.
func (t *TreeView) sequence() serval.DataSet {
	read := t.reading()
	if read == nil {
		if t.restate || t.made == nil {
			t.closeSequence()
			t.made = t.makeSource()
			t.restate = false
		}
		read = t.made
	}
	if read == nil {
		return nil
	}
	if t.set == nil {
		set, err := read.Open(nil)
		if err != nil {
			// A sequence that cannot be stated is a tree with no rows, and now it says
			// so: it cannot ask a different question, having been told which one to
			// ask, and drawing nothing without explaining why was indistinguishable
			// from a source with nothing in it. See trouble.go.
			t.took(Trouble{Reason: err.Error(), At: -1})
			return nil
		}
		t.set = set
	}
	return t.set
}

// reading is the source a sequence is stated over: the tree GROWN from a hint
// where there is one, and otherwise what SetSource was given.
//
// They differ because a source may describe its own records without being a tree
// -- a bundle of files carrying a `parent` field is a flat list that says how to
// go down it -- and the view is what turns the saying into a tree. `Source`
// answers what the caller handed in either way, that being what the caller asked
// about.
func (t *TreeView) reading() serval.Source {
	if t.grown != nil {
		return t.grown
	}
	return t.source
}

// closeSequence lets the stated sequence go. The next read states it again.
func (t *TreeView) closeSequence() {
	if t.set != nil {
		t.set.Close()
		t.set = nil
	}
}

// touched says the items this tree was given have changed, so a source made out
// of them is out of date.
//
// **It is said on every rebuild, for now.** A tree's items are mutated through
// the ITEMS -- `item.AddChild(child)` never passes through the view -- so there
// is no moment a view can be sure it was told about. Until the source is the
// authority and the items are a projection of it, the safe answer is to remake,
// and the cost is the same order as the walk it replaces.
//
// A tree reading a DECLARED source does not care what its own items do, so
// nothing is restated there.
func (t *TreeView) touched() {
	if t.source == nil {
		t.restate = true
	}
}

// makeSource builds a tree out of the items this tree was given.
func (t *TreeView) makeSource() *serval.TreeSource {
	var rows []serval.Row
	t.byID = map[core.ObjectID]*TreeItem{}

	// `seq` is the item's own position among its siblings -- the order the
	// application put them in, untouched. The SORT is what reorders them, and it
	// is stated in columns and translated per kind.
	var walk func(items []*TreeItem, parent *TreeItem)
	walk = func(items []*TreeItem, parent *TreeItem) {
		for i, item := range items {
			t.byID[item.ID] = item
			fields := append(t.cells(item, ""), serval.Named(treeSeq, i))
			if parent != nil {
				fields = append(fields, serval.Named(treeParent, int64(parent.ID)))
			}
			rows = append(rows, serval.NewRow(treeKey(item.ID), fields))
			walk(item.Children, item)
		}
	}
	walk(t.rootItems, nil)

	// A made source has one kind of row, so one translation serves every level.
	by := t.sortFields("")
	// An EQUIVALENT mutant today, and kept: a made source's rows are read for their
	// IDENTITIES and nothing else -- flatten maps each key back to the item the
	// caller handed in, which is where the text and the cells already are -- so
	// there is no field for the tree's five to eat. The level's own sort names
	// column IDs and runs below the tree, before anything is written over.
	//
	// It is here because the answer must not depend on that. A made row that was
	// ever read for what it HOLDS would collide the moment somebody declared a
	// column called `kind`, and one file giving two answers about where a tree's
	// fields go is the trap rather than the saving.
	src, err := serval.NewTreeSource(serval.TreeOptions{
		Source: serval.NewListSource(rows),
		Fields: treeFields,
		Descriptor: &serval.DataSetDescriptor{
			Filter: &serval.Filter{
				Op: serval.OpEq, Field: treeParent, Values: []*serval.Value{nil},
			},
			Sort: by,
		},
		Types: serval.NodeTypes{Default: &serval.NodeType{
			Children: serval.Sorted(serval.ChildrenByKey(treeParent), by...),
		}},
	})
	if err != nil {
		return nil
	}
	t.seedMarks(src)
	return src
}

// amendedOver is a record with what an amendment says about it written over the top.
//
// An ALTERATION names some members and the rest stand, which is what a cell edit is.
// Anything else the amendment holds is a record entire -- a replacement or an
// addition -- and stands alone.
//
// A removal is not here: a row taken out of a sequence is a row the next read will
// not send, and hiding it from a flattening that still holds it would leave a gap in
// the pre-order with a parent still counting it. Staleness is staleness, and this
// corrects a record's CONTENTS without pretending to correct the shape.
func amendedOver(fields serval.Record, held serval.Amendment) serval.Record {
	if held.How != serval.Altered {
		return held.Fields
	}
	out := make(serval.Record, len(fields), len(fields)+len(held.Fields))
	copy(out, fields)
	for _, m := range held.Fields {
		replaced := false
		for i, had := range out {
			if had.Name == m.Name {
				out[i], replaced = m, true
				break
			}
		}
		if !replaced {
			out = append(out, m)
		}
	}
	return out
}

// treeKey is an item's identity as the made source states it.
func treeKey(id core.ObjectID) *serval.Value { return serval.NewInt(int64(id)) }

// seedMarks carries `item.Expanded` into the source's marks.
//
// **The field is the authoring surface and the marks are the mechanism.** Eight
// test files and the wire's `expanded=` property write the field directly, so it
// must go on meaning what it meant; what reads it is now this, once, when the
// source is made.
//
// It writes through Marks rather than through the tree's own verbs on purpose.
// The verbs tell every live sequence to rebuild, which is right for a click and
// ruinous for a thousand items -- and there is nothing live to tell, this source
// not having been opened yet.
func (t *TreeView) seedMarks(src *serval.TreeSource) {
	m := src.Marks()
	var walk func(items []*TreeItem, chain []string)
	walk = func(items []*TreeItem, chain []string) {
		for _, item := range items {
			mine := append(append([]string{}, chain...), serval.Key(treeKey(item.ID)))
			if item.Expanded && !item.IsLeaf() {
				m.Open(mine...)
			}
			walk(item.Children, mine)
		}
	}
	walk(t.rootItems, nil)
}

// A treeSink is one Extent of the flattening arriving.
//
// It takes PLACES as well as records, which is what lets a drag stay smooth: what
// a view needs first is where the rows are and not what they hold, so an identity
// and a depth are enough to lay a row out and the values fill it in behind.
type treeSink struct {
	tree     *TreeView
	rows     []named
	begin    serval.RecordCount
	done     serval.Complete
	expected int    // where the view asked from, and so where it expects the answer
	asked    asking // which ask this answers, so a stale one can be dropped

	// positional says this read asked to begin at a POSITION rather than past a
	// record. See rowSink.positional.
	positional bool
}

func (s *treeSink) Ordered() {}

func (s *treeSink) Record(id *serval.Value, fields serval.Record) error {
	s.take(id, fields)
	return nil
}

func (s *treeSink) Subset(id *serval.Value, fields serval.Record, _ serval.Totals) error {
	s.take(id, fields)
	return nil
}

// Place is a row named and not yet filled in. One row arrives as exactly one of
// Place, Record or Subset, so this counts towards the run like the others and
// never doubles it.
func (s *treeSink) Place(id *serval.Value, fields serval.Record) error {
	s.take(id, fields)
	return nil
}

// Placed says the order is settled, which is when a view can lay out rows it has
// no values for and be sure nothing will turn up between two it already holds.
func (s *treeSink) Placed(c serval.Complete) { s.begin = c.First }

// Done ends the answer, which is when what arrived is written down.
//
// For a tree over records in hand that is inside the Read that asked; for one
// whose levels have to be fetched it is whenever they arrive, and the view
// repaints because rows that were placeholders are not any more.
func (s *treeSink) Done(c serval.Complete) {
	s.done = c
	s.settle()
	s.tree.Update()
}

// take learns one row: the item it leads to, and where it stands.
//
// **A MADE source's rows lead back to the items the caller handed in** -- what
// they hold is already on those items, so the record's fields are read for the
// depth and nothing else. A DECLARED source's rows have no items until learnRow
// makes them.
func (s *treeSink) take(id *serval.Value, fields serval.Record) {
	t := s.tree
	names, isTree := serval.TreeFieldsOf(t.reading())
	if t.source != nil {
		t.learnRow(id, fields, names, isTree)
	}
	s.rows = append(s.rows, named{id: id, deep: wholeOf(fields.Get(names.Depth))})
}

// settle writes what arrived into the spine.
//
// Where the answer says it began, that is where the run goes -- it is the source
// speaking about its own sequence, and it outranks anything the view worked out.
// Where it says NOTHING, the view uses what it expected, which is not a guess: it
// chose the place it asked from. What is never done is writing a run at a position
// nobody vouched for.
func (s *treeSink) settle() {
	t := s.tree
	t.bones.learn(s.done)
	if !t.current(s.asked) {
		// An answer for somewhere the reader has since left. Writing it down would
		// leave the spine holding where the reader was passing through rather than
		// where it is.
		return
	}
	// Whether this sequence will jump to a position at all. See rowSink.settle.
	if s.positional && s.done.First.Exact && s.done.First.N != s.expected {
		t.walks = true
	}

	first := s.done.First
	if !first.Exact {
		first = s.begin
	}
	if !first.Exact {
		first = serval.Exactly(s.expected)
	}
	if first.Exact && len(s.rows) > 0 {
		t.bones.place(first.N, s.rows)
	}
	// What the answer said was wrong, kept where a reader can see it. See trouble.go.
	if s.done.Error != "" {
		if t.took(Trouble{Reason: s.done.Error, At: s.expected}) {
			t.Update()
		}
	} else if len(s.rows) > 0 {
		if t.untroubled() {
			t.Update()
		}
	}

	t.hang()
	// A row the view could not place may be placeable now, this being the moment the
	// answer to "where is it" can have changed.
	t.resolve()
}

// hang gives the rows the view HOLDS the parentage their depths imply.
//
// The rule is the one a whole flattening used -- the parent of a row at depth d is
// the last row seen at d-1 -- applied to the runs the spine holds rather than to a
// sequence read entire. Two things follow, and both are honest rather than
// regrettable.
//
// **A row whose parent is above the Extent has no parent here.** The view is not
// holding it, so there is nothing to point at. `Level()` answers from the depth
// the source SAID rather than from a walk up, which is why the indent is still
// right where the parentage stops -- and it is why serval puts a depth on every
// row.
//
// **And the top of a run is a place the ancestry is unknown**, not a place the
// rows are roots. A gap in the spine is a gap in what can be reconstructed, so the
// stack starts empty at each run and the first rows of a scrolled Extent hang off
// nothing until the rows above them arrive.
//
// `fromTop` is the depth-nought rows held, which is what RootItems answers for a
// declared source: a caller asking a tree what stands at the top is asking about
// the sequence, and a view holding an Extent of the middle of one truthfully has
// none of it.
func (t *TreeView) hang() {
	if t.source == nil {
		return // a made source's parentage is the caller's own and is not rebuilt
	}
	t.fromTop = nil
	for _, r := range t.bones.runs {
		var above []*TreeItem
		for i := range r.rows {
			item := t.rowAt(r.at + i)
			if item == nil {
				above = nil
				continue
			}
			d := r.rows[i].deep
			item.Children = nil
			if d > 0 && d <= len(above) {
				item.Parent = above[d-1]
				item.Parent.Children = append(item.Parent.Children, item)
			} else {
				item.Parent = nil
				if d == 0 {
					t.fromTop = append(t.fromTop, item)
				}
			}
			if d < len(above) {
				above = above[:d]
			}
			above = append(above, item)
		}
	}
}

// objectIDOf turns a row's key back into the identity it was made from.
func objectIDOf(id *serval.Value) core.ObjectID { return core.ObjectID(id.Int) }

// --- a declared source --------------------------------------------------

// SetSource declares where this tree's rows come from.
//
// It replaces whatever the tree was reading, its own items included, and drops
// what it knew about where the rows were -- a different sequence puts them
// somewhere else, and nothing it held about the old one says anything about the
// new. Nil goes back to reading the items the tree was given.
//
// The source is a `TreeSource` wherever the rows are a hierarchy, and the rows
// then carry their depth, their kind and how many children they have. A plain
// source is read as one flat level, which is a tree of one generation and is
// ordinary rather than an error.
func (t *TreeView) SetSource(src serval.Source) {
	t.closeSequence()
	t.source = src
	t.made = nil
	t.grown = nil
	t.hintLabel = ""
	t.fromSource = nil
	t.fromTop = nil
	t.restate = true
	// **And what the old sequence taught it.** A length only ever replaces one
	// that says less, which is right within one sequence and wrong across a change
	// of them: a count earned by walking a small source to its end would then
	// outrank the floor an Extent of a hundred thousand honestly reports, and the
	// view would draw a thumb for fifteen rows over a body it had barely started.
	t.bones = spine{}
	t.asks++
	t.currentIndex = -1
	t.scrollOffset = 0
	t.growFromHint()
	// Before the first read, so the rows arrive in the order the columns state
	// rather than in the configuration's and then again in this one.
	t.tellOrder()
	t.arrivals++
	// **The source that is READ**, which for a grown tree is the tree and not what
	// it was grown from. A tree turns a level answering into a walk and a walk
	// completing into news for its readers, so it is the tree's arrival a view
	// wants: that is the moment the rows exist.
	if read := t.reading(); read != nil {
		hearArrivals(t, read, t.arrivals, func() int { return t.arrivals }, t.Reread)
	}
	t.moved()
	t.Update()
}

// Source is what this tree reads, and nil for one reading its own items.
//
// What the CALLER handed in, which is not always what a sequence is stated over:
// a source that says its records are a hierarchy without being a tree is grown
// into one here. See reading.
func (t *TreeView) Source() serval.Source { return t.source }

// SetTreeHint says what shape the source's records are, for a source that cannot
// say for itself.
//
// **Two places a hint can come from, and this is the second.** A bundle says it in
// its own document and the source carries it; a source across a connection has
// nowhere to carry one, so whoever pointed the view at it is who knows. That is
// the VIEW being configured -- which is what a statement in the wire language is
// for -- rather than a document reaching into somebody else's window.
//
// Said here it WINS over anything the source says, because it is the more specific
// of the two: a caller who names a shape has looked at the records, and a source's
// own hint is a default for readers who have not.
//
// A zero hint takes it back off, and the source's own is heard again.
func (t *TreeView) SetTreeHint(hint serval.TreeHint) {
	t.saidHint = hint
	if t.source == nil {
		return
	}
	// The tree is grown from the source again, because which hint is in force has
	// changed and the grown tree is what that hint built.
	t.closeSequence()
	t.grown = nil
	t.hintLabel = ""
	t.bones = spine{} // a different shape over the same records is a different sequence
	t.growFromHint()
	t.tellOrder()
	t.moved()
	t.Update()
}

// TreeHint is the shape this view was told its source's records are, and the zero
// hint where nobody told it.
func (t *TreeView) TreeHint() serval.TreeHint { return t.saidHint }

// growFromHint makes a tree out of a declared source.
//
// **What a view reads is a flattening**: rows in pre-order with a depth on each
// one, answered when the whole walk has been taken in. A `TreeSource` is what
// answers that, and so a declared source is grown into one whether it says it is a
// hierarchy or not -- a hint says HOW MANY generations there are, not whether
// there is a flattening to read.
//
// So there are two ways down and both end in a TreeSource:
//
//	the source SAYS      serval's `Hinting`, or a hint the view was told, and
//	                     `Options` already knows what tree it describes
//	the source is FLAT   one generation: every record stands at the top and
//	                     nothing is under anything
//
// **The source is still asked, and nothing is guessed.** The flat case reads no
// field name and decides no column's meaning -- it is the absence of a descent
// said plainly, which is what a `NodeType` with no criterion means.
//
// It matters most for a source that answers LATER. A view reads into a sink and
// uses what the sink got, so a view reading an application's records straight
// would read before they existed and read nothing -- and read nothing again on
// every notice, asking once more each time. A tree is what holds a walk across the
// waiting and tells when it has finished, which is the whole reason the layer is
// there. A flat source that answers at once flattens on the thread that asked and
// is as it always was.
//
// A source that is ALREADY a tree is left alone: it has descended for itself, and
// a hint on it would describe the records its levels are drawn from rather than
// the flattening it answers.
//
// A hint that cannot mean what it says leaves the source ONE generation. It is a
// description that turned out contradictory, and one flat level of real records
// beats no rows at all -- the same call a bundle load makes about a bad include.
func (t *TreeView) growFromHint() {
	if t.source == nil {
		return
	}
	// An EQUIVALENT mutant today, and kept: a TreeSource does not embed
	// serval's HintSaid, so it never says a hint and TreeHintOf would turn this
	// away anyway. It is here because the REASON matters -- a tree has descended
	// for itself, and a hint on it would describe the records its levels are drawn
	// from rather than the flattening it answers -- and because the day something
	// gives a TreeSource a hint, this is what stops it being grown twice.
	if _, already := t.source.(*serval.TreeSource); already {
		return
	}
	// What the view was TOLD beats what the source says: see SetTreeHint.
	hint, said := t.saidHint, !t.saidHint.Nothing()
	if !said {
		hint, said = serval.TreeHintOf(t.source)
	}

	// A hint that fails Check yields the zero TreeOptions and NewTreeSource refuses
	// one for having no source, so the error could be ignored and the same nil
	// reached by a longer road. It is read because the two refusals are about
	// different things, and because a contradictory hint has to fall back to one
	// generation rather than to no tree at all.
	opt, used := oneGeneration(t.source), false
	if said {
		if fromHint, err := hint.Options(t.source); err == nil {
			opt, used = fromHint, true
		}
	}
	// **Whichever branch built it**, the tree's own fields go where a column will
	// not be. A hint's Options does not name them and neither does oneGeneration,
	// because where they go is not a fact about the records -- it is a fact about
	// who is reading them. See treeFields.
	opt.Fields = treeFields
	grown, err := serval.NewTreeSource(opt)
	if err != nil {
		return
	}
	t.grown = grown
	if used {
		// The label is the hint's one word about what a record is CALLED, and the
		// caption is where that goes. It is the weakest rung, so anything the caller
		// declared still wins -- see cellOf.
		t.hintLabel = hint.Label
	}
}

// oneGeneration is the tree a FLAT source is: every record at the top, and a node
// type with no criterion, which is how "nothing is under anything" is said.
//
// No filter on the top level either, because there is no field to filter on -- a
// flat source's every record stands there, and saying which ones do would be
// inventing a shape the source never described.
func oneGeneration(src serval.Source) serval.TreeOptions {
	return serval.TreeOptions{
		Source: src,
		Types:  serval.NodeTypes{Default: &serval.NodeType{}},
	}
}

// amendable is where an edit to a declared source's row is HELD: the nearest
// `serval.AmendedSource` from the top, and nil where there is none.
//
// **The top, because that is the layer a reader is looking at.** A bundle is
// assembled as an amendment over a composition of its includes, and the document's
// own amendments are already in it -- so an edit made here lands in the same place
// they did, and one thing has to be saved out rather than two.
//
// Nearest from the top is a type assertion and not a walk, because the two shapes
// that reach a view put it there: a bundle hands one in, and a live source across a
// connection is wrapped in one. A source that keeps its amendment further down
// cannot be reached from here and is not amended -- which is reported rather than
// hidden, see commitCellEdit.
//
// It is `source` and not `reading`: the grown tree over it is a FLATTENING, and a
// flattening is not where a record lives. Amending a row means amending the record
// the row was drawn from, which is the tree's child.
func (t *TreeView) amendable() *serval.AmendedSource {
	if a, ok := t.source.(*serval.AmendedSource); ok {
		return a
	}
	return nil
}

// Reread says the declared source's answer has changed, so the view reads it
// again.
//
// **The source is told, and then the view is told.** Two sayings at two doors,
// which is the same shape a cache has: whoever changed the data says so to the
// source it belongs to -- `ListSource.Restate` for a level's rows, then
// `TreeSource.Stale` -- and this is the view hearing that the sequence it holds
// answers differently now. Nothing here polls the source and nothing compares
// anything: a view nobody tells draws what it drew.
//
// The sequence is not restated, so the items the rows lead to keep their
// pointers -- selection and the row editor hold them. A caller who wants the
// rows forgotten says SetSource again.
//
// It does nothing for a tree reading its own items, whose source is remade from
// them on every rebuild anyway.
func (t *TreeView) Reread() {
	if t.source == nil {
		return
	}
	t.moved()
	t.Update()
}

// tellOrder hands a declared tree the order the columns state, translated into
// each kind's own field names.
//
// **A tree sorts level by level**, so this is a sort per kind rather than one
// sort -- which is what serval's `SortBy` takes, and what lets a host spell its
// size `bytes` where a window spells it `size`. The mapping is what translates,
// exactly as it does for a source made of the tree's own items; the difference is
// only which end states the sequence.
//
// Every kind the view has a mapping for is named, and the default kind always --
// it being the top level's, and a tree of one shape's only one. A kind the view
// has never heard of keeps the order its configuration chose, there being nothing
// to translate its columns with.
func (t *TreeView) tellOrder() {
	src := t.marks()
	if src == nil {
		return
	}
	// **With no sort in force the view says NOTHING**, and a tree keeps the order
	// its configuration chose. Saying "no levels" is a different answer -- it means
	// the order the source answers in -- and it would turn off an order the
	// application stated, which is how a window reading in the order its store put
	// the rows in came to read in identity order instead.
	if !t.sorted {
		src.SortBy(nil)
		return
	}
	by := make(map[string][]serval.SortLevel, len(t.kinds)+1)
	by[""] = t.columnLevels("")
	for kind := range t.kinds {
		by[kind] = t.columnLevels(kind)
	}
	src.SortBy(by)
}

// learnRow is the item standing for one of a source's rows, made if it is not
// made already and brought up to date either way.
//
// **The same row leads to the same pointer across a rebuild**, which is why this
// keeps them by identity rather than making one per read. Everything that reads
// flatList compares pointers -- selection restores itself by one, the row editor
// holds one -- so a row that has not changed must not become a different object
// because the tree was redrawn.
//
// What the row carries comes through the KIND's mapping, which is what lets one
// tree draw a host, an application and a window in one set of columns.
func (t *TreeView) learnRow(id *serval.Value, fields serval.Record,
	names serval.TreeFields, isTree bool) *TreeItem {
	key := serval.Key(id)
	if t.fromSource == nil {
		t.fromSource = map[string]*TreeItem{}
	}
	item := t.fromSource[key]
	if item == nil {
		item = &TreeItem{ID: core.NextObjectID(), Enabled: true, rowKey: id}
		t.fromSource[key] = item
	}

	kind := serval.Segment(fields.Get(names.Kind))
	item.rowKind = kind
	m := t.kinds[kind]

	// **What the amendment holds wins over what the flattening said.**
	//
	// A tree flattens once and holds the rows; an edit made afterwards is held
	// against the source UNDER that flattening, so the rows this is reading are a
	// snapshot that predates it. Nothing is wrong with the snapshot -- it was true
	// when it was taken -- and nothing has told the tree to walk again, deliberately:
	// re-asking every level on every committed cell is a query per level, and for one
	// flat level of a hundred thousand rows it is the whole body.
	//
	// So the snapshot is corrected here instead, which is the same move the amendment
	// itself makes on its child: the later saying wins. It costs one map lookup per
	// row and nothing at all for a source nobody has written in.
	//
	// The next real read needs none of this -- the amendment is below the tree and
	// the records come out already corrected. This is only for the gap between an
	// edit and whatever asks the question again.
	if over := t.amendable(); over != nil {
		if held, ok := over.Amendment(id); ok && held.Fields != nil {
			fields = amendedOver(fields, held)
		}
	}

	// **The segment this row's mark is filed under**, asked of the tree rather than
	// guessed: a level with a standing is marked by its PATH and an adjacency list by
	// its identity, and a row carries both without saying which. See chainOf.
	item.rowMark = key
	if src := t.marks(); src != nil && src.MarkedByPath(kind) {
		item.rowMark = serval.Segment(fields.Get(names.Path))
	}

	// **How deep it stands and what its chain is, as the source said them.** Both
	// used to be worked out here: the depth turned into parentage and the chain
	// walked back up it. Neither works for a WINDOW -- a view holding rows forty to
	// eighty holds no ancestor of any of them -- so both are read from the row,
	// which is where the walk that knew them wrote them down. See serval's
	// TreeFields.Chain.
	item.rowDepth = wholeOf(fields.Get(names.Depth))
	item.rowChain = chainSegments(fields.Get(names.Chain))

	item.Text = serval.Segment(fields.Get(t.cellOf(kind, nil).showField()))
	if m.Icon != "" {
		item.Icon = serval.Segment(fields.Get(m.Icon))
	}
	for _, col := range t.columns {
		item.SetValue(col.ID, serval.Segment(fields.Get(t.cellOf(kind, col).showField())))
	}
	if m.ReadOnly != "" {
		item.ReadOnly = trueOf(fields.Get(m.ReadOnly))
	}

	// The three things a tree knows and a record need not, as the source said
	// them. Expanded and Kids are what the fourteen callers of IsLeaf and the
	// readers of Expanded go on asking, so nothing else had to change.
	//
	// **A source that is not a tree says none of it**, and every row is then a
	// leaf standing at the top -- which is a flat source read as a tree of one
	// generation, and is ordinary. Asking the SOURCE rather than sniffing for a
	// field is what keeps that apart from a tree whose row nobody could count.
	if !isTree {
		item.Expanded, item.Kids = false, 0
		return item
	}
	switch serval.Segment(fields.Get(names.State)) {
	case "open", "openAll":
		item.Expanded = true
	default:
		item.Expanded = false
	}
	if n := fields.Get(names.Expandable); n == nil {
		// Nobody could say, so draw the twisty and find out on opening.
		item.Kids = -1
	} else if n.IsInt {
		item.Kids = int(n.Int)
	} else {
		item.Kids = int(n.Num)
	}
	return item
}

// trueOf is a value read as a yes or a no.
//
// A boolean is the answer where there is one. Everything else is read the way a
// record that carries a flag as a number or a word carries it -- nought and the
// empty string being no -- because a source is under no obligation to have a
// boolean type, and `psl.go` reads `1` out of a file as a number.
//
// **A field the record has not got is NO.** `undefined` is not a truth, and a
// row saying nothing about being held out of the editor is not held out.
//
// The BoolValue case is an equivalent mutant and is kept anyway: dropping it
// changes no answer, `serval.Segment` rendering a boolean as `true` or `false` and
// the word fallback reading both. It is here to say the direct answer directly,
// and to spare a format and a comparison where the value already is the answer.
func trueOf(v *serval.Value) bool {
	switch {
	case v == nil:
		return false
	case v.Kind == serval.BoolValue:
		return v.Bool
	case v.IsInt:
		return v.Int != 0
	case v.Kind == serval.NumberValue:
		return v.Num != 0
	}
	s := serval.Segment(v)
	return s != "" && s != "false" && s != "0"
}

// wholeOf is a value as a whole number, and nought for anything that is not one.
// A depth is a count and a count is an integer; a source that sent something else
// has said nothing a depth can be read out of.
func wholeOf(v *serval.Value) int {
	switch {
	case v == nil:
		return 0
	case v.IsInt:
		return int(v.Int)
	case v.Kind == serval.NumberValue:
		return int(v.Num)
	}
	return 0
}

// chainSegments is a chain value as the marks take it: the positional members of
// the list serval wrote, in order.
//
// Nil for a row that carries none, which is a source that is not a tree -- and a
// nil chain tells the marks nothing, which is right, there being no node for it
// to name.
func chainSegments(v *serval.Value) []string {
	if v == nil || v.Kind != serval.ListValue {
		return nil
	}
	out := make([]string, len(v.List))
	for i, m := range v.List {
		out[i] = serval.Segment(m.Value)
	}
	return out
}

// marks is the expansion of a DECLARED tree source, and nil for anything else.
//
// A tree reading its own items has no need of it: the field is the authoring
// surface there and makeSource carries it in. A tree reading a declared source
// has no items to carry anything from, so the marks are the only authority --
// which is the same mechanism said from the other side, not a second one.
func (t *TreeView) marks() *serval.TreeSource {
	if src, ok := t.reading().(*serval.TreeSource); ok {
		return src
	}
	return nil
}

// chainOf is an item's mark segments, from the root down to it.
//
// **The row carries it, and it had to start carrying it.** This walked back up
// `Parent` before, which works for a view holding the whole pre-order and cannot
// work for one holding an Extent: everything above the Extent is exactly what the
// view declined to hold, so a row forty deep in a scrolled tree had a chain of one
// segment and clicking its twisty opened a node that is not there. Nothing
// reported that, the marks having no opinion about a chain nobody walked.
//
// **A segment is what the WALK spells, and that is not always the identity.** A
// level with a standing is marked by its path, so a chain of identities names a
// node that is not there. serval spells it and the row carries it, which is the
// only way the two spellings cannot drift apart.
//
// Empty for a row out of a source that is not a tree, and for one the tree made
// itself -- neither has a mark to move, the field being the authoring surface
// there. See tellMarks.
func (t *TreeView) chainOf(item *TreeItem) []string { return item.rowChain }

// tellMarks moves a declared source's mark for one item, and reports whether it
// did -- so the caller falls back to the field where there is no source to tell.
func (t *TreeView) tellMarks(item *TreeItem, open bool) bool {
	src := t.marks()
	if src == nil || item == nil || item.rowKey == nil {
		return false
	}
	chain := t.chainOf(item)
	if len(chain) == 0 {
		return false
	}
	if open {
		src.Expand(chain...)
	} else {
		src.Collapse(chain...)
	}
	return true
}

// --- what a node opening and closing actually costs ----------------------
//
// **Nothing goes stale.** Every level of a tree is a data set of its own, opened
// with its own descriptor -- `eq parent <this row>` -- and cached on its own. So a
// node opening makes no record anywhere untrue and reorders no level: the whole
// sequence of each data set is exactly what it was. What changes is which levels
// the walk visits, and therefore what POSITION each row below the mark stands at.
// Above the mark, nothing moves at all.
//
// So the question is never whether to invalidate. It is whether the view can say
// how far the rows below the mark moved. Where it can, it shifts and keeps
// everything -- the rows on screen stay where they are, the twisty flips at once,
// and the placeholders fill in behind. Where it cannot, it forgets from the mark DOWN,
// which costs a re-ask and not a re-fetch: the levels' caches still hold every
// record either way.

// opening is how many rows appear when this row opens, and -1 where nobody can
// say.
//
// **`Kids` is exact for the ordinary click.** serval counts a row's children out
// of one census per node type -- not one question per twisty -- and puts the figure
// on the row, so the delta for "open this node, its children arrive closed" is in
// hand before the question is asked.
//
// Two cases cannot be counted, and both are unknown rather than wrong:
//
//	Kids < 0        nobody could count, which serval says as an undefined
//	                `tree:expandable` and the view draws as a live twisty
//	openAll above   the children inherit it, so opening this row reveals a whole
//	                SUBTREE -- and a subtree's size is not free: a census answers
//	                one level, and summing deeper levels needs the walk that has
//	                not happened. Kids is then a floor and a floor will not do,
//	                a shift by too little putting every row below in the wrong
//	                place.
//
// It is asked AFTER the mark has moved, because that is when the marks can say
// which of the two states this row ended up in: `Open` governs exactly one level,
// and `OpenAll` is the one that resumes beneath.
func (t *TreeView) opening(item *TreeItem) int {
	if item == nil || item.Kids < 0 {
		return -1
	}
	src := t.marks()
	if src == nil {
		return -1
	}
	chain := t.chainOf(item)
	if len(chain) == 0 {
		return -1
	}
	if src.Marks().Mark(chain...) != serval.Open {
		return -1
	}
	return item.Kids
}

// closing is how many rows go when the row at a position closes, and -1 where the
// view does not hold enough to say.
//
// The rows going are the ones under it in the flattening, which run from the next
// position to the first row standing no deeper than it does. That is a scan over
// what the spine holds, and it is exact whenever the end of the subtree is in hand
// -- which for a node the reader just clicked on usually means the rows on screen.
//
// **A subtree running past what is held is unknown, and the end of the SEQUENCE is
// only an answer where the count is exact.** A floor says there may be more below,
// and rows nobody has counted are rows that may be part of this subtree.
func (t *TreeView) closing(at int) int {
	deep, ok := t.bones.deepAt(at)
	if !ok {
		return -1
	}
	for i := at + 1; ; i++ {
		if d, held := t.bones.deepAt(i); held {
			if d <= deep {
				return i - at - 1
			}
			continue
		}
		// Off the end of a sequence somebody counted: everything below is the
		// subtree. Off the end of what is HELD, or of a floor: unknown.
		if length := t.bones.length(); length.Exact && i >= length.N {
			return i - at - 1
		}
		return -1
	}
}

// opened says the row at a position has been opened, and moves what the view
// holds rather than forgetting it.
//
// This is #60 and #64 in one move, because they were one move: laying the placeholder
// rows out the instant the twisty flips IS the shift, done before the records
// arrive. A reader clicking a folder sees it open at once, with as many empty rows
// under it as it has children, and the captions land when the answer does.
func (t *TreeView) opened(at int, item *TreeItem) {
	if n := t.opening(item); n > 0 {
		t.bones.grew(at+1, n)
		return
	}
	// Nobody could say how many, so what is below the mark is what is no longer
	// known -- and what is above it never moved.
	t.bones.forgetFrom(at + 1)
}

// closed says the row at a position has been closed, and takes the rows under it
// out.
func (t *TreeView) closed(at int) {
	switch n := t.closing(at); {
	case n > 0:
		t.bones.shrank(at+1, n)
	case n == 0:
		// Nothing was showing under it, so nothing moved.
	default:
		t.bones.forgetFrom(at + 1)
	}
}

// moved says the sequence has changed in a way the view cannot describe -- an item
// added, a row taken out, the sort restated -- so every position it holds is
// suspect and the window is asked for again.
//
// **It is the answer of last resort and not the ordinary one.** A node opening and
// closing is described, and goes through `opened` and `closed` instead; going
// through here would throw away the rows on screen to learn what the view already
// knew.
//
// **And the LENGTH goes with the positions, which `forget` on its own does not do.**
// A figure only ever replaces one that says less, so that a notice does not leave the
// thumb flickering between a scale and none -- and that is right for a notice about a
// sequence that is still the same sequence. It is wrong here: an item added, a sort
// restated, a whole tree expanded, and the count somebody earned before is a claim
// about a sequence that no longer exists. An exact figure kept across one of those
// outranks every honest floor that follows it, for ever.
//
// Nothing flickers for it, either. The re-ask below learns the length in the same
// breath, so there is no frame drawn against nothing.
func (t *TreeView) moved() {
	t.touched()
	t.asks++
	t.bones = spine{}
	t.clampScrollOffset()
	t.extent(t.asking())
}
