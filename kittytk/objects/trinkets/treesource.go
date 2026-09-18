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
// # Sorting stays where it is
//
// A tree sorts each level by its own columns, with cached numeric values and
// case-folded text, and none of that belongs in a data layer. So the made source
// is not asked to sort: each row carries `seq`, its position among its siblings
// AFTER visualSiblings has run, and every level is ordered by that. serval
// preserves the order it was handed rather than being taught to reproduce it.

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

// everyTreeRow is the count a whole flattened tree is read with. The made
// source holds its records, so this is a ceiling against a source that would
// answer forever rather than a window.
const everyTreeRow = 1 << 30

// sequence is the stated sequence, made and opened if it is not already.
//
// Nil where there is nothing to read, which is an empty tree and is ordinary.
func (t *TreeView) sequence() serval.DataSet {
	if t.restate || t.made == nil {
		t.closeSequence()
		t.made = t.makeSource()
		t.restate = false
	}
	if t.made == nil {
		return nil
	}
	if t.set == nil {
		set, err := t.made.Open(nil)
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
func (t *TreeView) touched() {
	t.restate = true
}

// makeSource builds a tree out of the items this tree was given.
func (t *TreeView) makeSource() *serval.TreeSource {
	var rows []serval.Row
	t.byID = map[core.ObjectID]*TreeItem{}

	var walk func(items []*TreeItem, parent *TreeItem)
	walk = func(items []*TreeItem, parent *TreeItem) {
		for i, item := range t.visualSiblings(items) {
			t.byID[item.ID] = item
			fields := serval.Record{serval.Named(treeSeq, i)}
			if parent != nil {
				fields = append(fields, serval.Named(treeParent, int64(parent.ID)))
			}
			rows = append(rows, serval.NewRow(treeKey(item.ID), fields))
			walk(item.Children, item)
		}
	}
	walk(t.rootItems, nil)

	bySeq := []serval.SortLevel{{Field: treeSeq}}
	src, err := serval.NewTreeSource(serval.TreeOptions{
		Source: serval.NewListSource(rows),
		// The top level is the rows with no parent, in the order the tree's own
		// sort put them.
		Spec: &serval.Spec{
			Filter: &serval.Filter{
				Op: serval.OpEq, Field: treeParent, Values: []*serval.Value{nil},
			},
			Sort: bySeq,
		},
		Types: serval.ChildTypes{Default: &serval.ChildType{
			Children: serval.Sorted(serval.ChildrenByKey(treeParent), bySeq...),
		}},
	})
	if err != nil {
		return nil
	}
	t.seedMarks(src)
	return src
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
	items := make([]*TreeItem, 0, len(out.ids))
	for _, id := range out.ids {
		if item := t.byID[core.ObjectID(id.Int)]; item != nil {
			items = append(items, item)
		}
	}
	return items
}

// treeRows takes one flattened answer. Only the identities are wanted: what each
// row HOLDS is on the item the key leads to, for as long as the tree is making
// its own source.
type treeRows struct {
	ids []*serval.Value
}

func (r *treeRows) Ordered() {}
func (r *treeRows) Record(id *serval.Value, _ serval.Record) error {
	r.ids = append(r.ids, id)
	return nil
}
func (r *treeRows) Subset(id *serval.Value, f serval.Record, _ serval.Totals) error {
	return r.Record(id, f)
}
func (r *treeRows) Done(serval.Complete) {}

// objectIDOf turns a row's key back into the identity it was made from.
func objectIDOf(id *serval.Value) core.ObjectID { return core.ObjectID(id.Int) }
