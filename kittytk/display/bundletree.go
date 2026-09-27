package display

// What a bundle says about the SHAPE of its own records.
//
// A bundle's records often are a hierarchy, and until now its author had no way
// to say so. Every reader had to be told again, in Go, by somebody who had gone
// and looked -- which is the thing a document is for.
//
//	_bundle: (
//	  key: "files", version: "1.0.0",
//	  tree: ( parent: ".up", order: ".rank", label: ".name", children: ".kids" )
//	)
//
// It is read straight into a `serval.TreeHint`, which is where what a hint MEANS
// lives, and hung on the assembled source so that a reader given only the source
// can ask. Nothing here interprets it.
//
// # The field names wear a dot, and this is where it bites
//
// A bundle is read under serval's `Whole`, so a record that is a LIST names its
// members `.name`, `.up`, `.0`. A hint names FIELDS as serval will see them, dot
// and all:
//
//	tree: ( parent: ".up", label: ".name" )
//
// It is the same rule `display=` and `value=` follow on a list, and taking the dot
// off would be a second rule about one thing. A bundle whose records are not lists
// -- a document of plain strings -- has `key` and `value` and no members, and then
// there is no dot to write.
//
// # It says what the records ARE, never what a view does
//
// Which field holds a parent, which holds a container, which says how many
// children there are, which is the row's own name. **And nothing else.** No
// column, no width, no alignment, no what-starts-expanded: a format that let a
// document reach into those would be a format that lets a document lay out
// somebody else's window, and a display that honoured it would be handing its
// layout to whatever it happened to load.
//
// The same line `_amendments` sits on: a bundle changes what it INCLUDED, and
// says nothing about who reads the result.
//
// # One hint, about this bundle's own records
//
// An include's hint does NOT travel up. `objectLibrary` including `figaro` gets
// figaro's records under `figaro/…`, and a hint on figaro describes figaro's --
// merging the two would mean one tree over records of two shapes, which is several
// node types, which is the configuration a document cannot state without naming
// sources outside itself.
//
// So a hint read here is the hint of the bundle that was ASKED for, and a
// composition of differently-shaped layers has no single answer to give. That is a
// limit worth stating rather than a bug: what the hint buys is the common case, a
// document that is a hierarchy read as one.

import (
	"github.com/phroun/pawscript"
	"github.com/phroun/serval"
)

// treeMark is the member of `_bundle` holding what the document says about the
// shape of its records.
const treeMark = "tree"

// The members of that block, each naming one FIELD of the bundle's records. They
// are serval's own words for the readings, so a hint reads the same in a document
// as it does in Go.
const (
	hintParent    = "parent"
	hintLocation  = "location"
	hintRoot      = "root"
	hintName      = "name"
	hintWhole     = "whole"
	hintDelimiter = "delimiter"
	hintChildren  = "children"
	hintOrder     = "order"
	hintLabel     = "label"
)

// treeHintOf is what a bundle's document says about the shape of its records, and
// false where it says nothing.
//
// Every member is read as TEXT by `pslText`, which takes a bare word and a quoted
// string alike -- `parent: up` and `parent: "up"` name the same field, the
// distinction PSL draws between a symbol and a string not being one a field NAME
// makes.
func treeHintOf(n *pawscript.PSLNode) (serval.TreeHint, bool) {
	meta, ok := n.Get(bundleMark)
	if !ok {
		return serval.TreeHint{}, false
	}
	block, ok := meta.(*pawscript.PSLNode)
	if !ok {
		return serval.TreeHint{}, false
	}
	held, ok := block.Get(treeMark)
	if !ok {
		return serval.TreeHint{}, false
	}
	said, ok := held.(*pawscript.PSLNode)
	if !ok {
		return serval.TreeHint{}, false
	}

	hint := serval.TreeHint{
		Parent:    pslText(said, hintParent),
		Location:  pslText(said, hintLocation),
		Root:      pslText(said, hintRoot),
		Name:      pslText(said, hintName),
		Whole:     pslText(said, hintWhole),
		Delimiter: pslText(said, hintDelimiter),
		Children:  pslText(said, hintChildren),
		Order:     pslText(said, hintOrder),
		Label:     pslText(said, hintLabel),
	}
	if hint.Nothing() {
		return serval.TreeHint{}, false
	}
	return hint, true
}

// A saying source is one that can be told what its records are. Both of the
// shapes a bundle becomes embed serval's HintSaid, which is what gives them this.
type saying interface {
	SetTreeHint(serval.TreeHint)
}

// sayShape hands a bundle's hint to the source it became, and reports the trouble
// where a hint cannot mean what it says.
//
// **A bad hint is a REPORT and not a refusal**, which is the rule the rest of a
// load follows: an optional include that resolves to nothing is absent and said
// so, a cycle has its back edge dropped and said so. A contradictory hint is the
// author's mistake about the SHAPE of records that are all still there, so the
// bundle loads, reads as a flat list, and says why it is not a tree. Refusing
// would lose the records over a sentence about them.
func (l *loader) sayShape(e bundleEntry, n *pawscript.PSLNode, src serval.Source) {
	hint, said := treeHintOf(n)
	if !said {
		return
	}
	if err := hint.Check(); err != nil {
		l.note(e.key, "", err.Error())
		return
	}
	if tell, ok := src.(saying); ok {
		tell.SetTreeHint(hint)
		return
	}
	// Nothing a bundle assembles to is outside this today, so a source that
	// cannot be told is a change here rather than a document's problem -- and
	// saying so beats a hint that silently went nowhere.
	l.note(e.key, "", "this says what its records are, and what it became cannot carry it")
}
