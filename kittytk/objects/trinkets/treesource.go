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
// POINTERS the tree was given, so that everything reading flatList goes on
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

// treeFields is where a tree's OWN five fields go, moved out of the data's way.
//
// A flattening carries a depth, a path, a child count, a mark state and a kind, and
// serval writes them into each row -- **dropping the record's own field of that
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

// everyTreeRow is the count a whole flattened tree is read with. The made
// source holds its records, so this is a ceiling against a source that would
// answer forever rather than a window.
const everyTreeRow = 1 << 30

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
			// A sequence that cannot be stated is a tree with no rows. There is
			// nothing a tree can usefully do with the refusal, having been told
			// which question to ask.
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

// flatten is the visible rows, read out of the sequence and turned back into the
// tree's own item pointers.
//
// The pointers matter: everything that reads flatList compares them, selection
// included, so a row that came back as a record has to lead to the very item the
// caller handed in. The key is what does that, which is why it is the item's own
// ObjectID rather than a position.
func (t *TreeView) flatten() []*TreeItem {
	set := t.sequence()
	if set == nil {
		return nil
	}
	var out treeRows
	if err := set.Read(&serval.Scope{Count: everyTreeRow}, &out); err != nil {
		return nil
	}

	// A MADE source's rows lead back to the items the caller handed in. A
	// DECLARED source's rows have no items until this, so one is made per
	// identity and kept, and the depth the tree reported is what its parentage is
	// rebuilt from.
	names, isTree := serval.TreeFieldsOf(t.reading())
	items := make([]*TreeItem, 0, len(out.ids))
	depth := make([]int, 0, len(out.ids))
	for i, id := range out.ids {
		if t.source == nil {
			if item := t.byID[core.ObjectID(id.Int)]; item != nil {
				items = append(items, item)
			}
			continue
		}
		items = append(items, t.learnRow(id, out.fields[i], names, isTree))
		depth = append(depth, wholeOf(out.fields[i].Get(names.Depth)))
	}
	if t.source != nil {
		t.fromTop = hangFrom(items, depth)
	}
	return items
}

// treeRows takes one flattened answer: the identities, and what each row holds.
//
// A made source needs only the identities -- what its rows HOLD is on the items
// the keys lead back to -- but a declared source's rows have nowhere else to be
// read from, so both are kept and the made path simply ignores the fields.
type treeRows struct {
	ids    []*serval.Value
	fields []serval.Record
}

func (r *treeRows) Ordered() {}
func (r *treeRows) Record(id *serval.Value, f serval.Record) error {
	r.ids = append(r.ids, id)
	r.fields = append(r.fields, f)
	return nil
}
func (r *treeRows) Subset(id *serval.Value, f serval.Record, _ serval.Totals) error {
	return r.Record(id, f)
}
func (r *treeRows) Done(serval.Complete) {}

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
	t.rebuildFlatList()
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
	t.growFromHint()
	t.tellOrder()
	t.rebuildFlatList()
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
	t.rebuildFlatList()
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

// hangFrom gives a source's rows the parentage their depth implies.
//
// The sequence arrives flat and in pre-order with a depth on every row, so the
// parent of a row at depth d is the last row seen at d-1. Reconstructing it is
// what lets `Level()` and the drawing that leans on it go on working unchanged --
// seven places ask an item how deep it stands, and none of them had to learn
// about a field.
//
// Only what is VISIBLE is hung: a collapsed node's children are not in the
// sequence, so its Children slice is empty and `Kids` is what says it has any.
//
// The top level comes back, because that is what a declared tree's RootItems
// answers -- one more projection of the source alongside `Parent` and `Children`,
// and the same argument: a caller asking a tree for its root items is asking what
// stands at the top, and a declared tree knows. Every one of them is in the
// sequence, the top level being what a tree with nothing open still shows.
//
// **It is kept apart from `rootItems`, which is the caller's own list.** Writing
// it there would destroy the items a tree was given, and `SetSource(nil)` promises
// them back.
func hangFrom(rows []*TreeItem, depth []int) []*TreeItem {
	var spine, top []*TreeItem
	for i, item := range rows {
		d := depth[i]
		item.Children = nil
		if d > 0 && d <= len(spine) {
			item.Parent = spine[d-1]
			item.Parent.Children = append(item.Parent.Children, item)
		} else {
			item.Parent = nil
			top = append(top, item)
		}
		if d < len(spine) {
			spine = spine[:d]
		}
		spine = append(spine, item)
	}
	return top
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
// A descent has this in hand, having walked it; a view reaching in from the side
// has to walk back UP for it, which is what the parentage rebuilt from the depth
// is for.
//
// **A segment is what the WALK spells, and that is not always the identity.** A
// level with a standing is marked by its path, so a chain of identities names a
// node that is not there -- which opens nothing and says nothing, the marks having
// no opinion about a chain nobody walked. `rowMark` is what the row was learned
// under, and asking the tree at that moment is what keeps the two spellings
// together.
func (t *TreeView) chainOf(item *TreeItem) []string {
	var up []string
	for at := item; at != nil; at = at.Parent {
		up = append(up, at.rowMark)
	}
	for i, j := 0, len(up)-1; i < j; i, j = i+1, j-1 {
		up[i], up[j] = up[j], up[i]
	}
	return up
}

// tellMarks moves a declared source's mark for one item, and reports whether it
// did -- so the caller falls back to the field where there is no source to tell.
func (t *TreeView) tellMarks(item *TreeItem, open bool) bool {
	src := t.marks()
	if src == nil || item == nil || item.rowKey == nil {
		return false
	}
	chain := t.chainOf(item)
	if open {
		src.Expand(chain...)
	} else {
		src.Collapse(chain...)
	}
	return true
}
