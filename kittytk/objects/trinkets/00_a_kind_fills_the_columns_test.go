package trinkets

// What a kind of row puts in each of a view's columns.
//
// A column is the view's name for a thing that different sources spell
// differently, so the mapping is per kind of row -- and there are four cheap
// rungs before the full one, because the commonest cases should cost nothing to
// say. Every rung is checked here, and so is what happens where a column matches
// nothing.

import (
	"strings"
	"testing"

	"github.com/phroun/serval"
)

// A tree of two columns, so a mapping has somewhere to map to.
func mappedTree() *TreeView {
	tv := NewTreeView()
	tv.AddColumn(NewTreeColumn("size", "Size", 10*cell))
	tv.AddColumn(NewTreeColumn("kind", "Kind", 10*cell))
	return tv
}

// fieldsFor is which field each of a kind's cells names: the caption first, then
// a column at a time.
func fieldsFor(tv *TreeView, kind string) string {
	out := []string{tv.cellOf(kind, nil).sortField()}
	for _, col := range tv.columns {
		out = append(out, tv.cellOf(kind, col).sortField())
	}
	return strings.Join(out, " ")
}

// **A name means itself**, which is the rung to expect and the one a tree making
// its own source relies on.
func TestByDefaultAColumnTakesTheFieldOfItsOwnName(t *testing.T) {
	tv := mappedTree()
	if got, want := fieldsFor(tv, ""), "value size kind"; got != want {
		t.Errorf("the identity mapping names\n  %s\nwant\n  %s", got, want)
	}
}

// **A bundle's fields wear a dot**, so the identity with one is its own rung
// rather than something a caller writes out per column.
func TestTheDottedRungIsTheIdentityWithADot(t *testing.T) {
	tv := mappedTree()
	tv.SetKindMap("", NodeMap{Dotted: true})
	if got, want := fieldsFor(tv, ""), ".caption .size .kind"; got != want {
		t.Errorf("the dotted mapping names\n  %s\nwant\n  %s", got, want)
	}
}

// Positional is for a source whose records are tuples: `.0` is the caption, and
// each column after it in turn.
func TestThePositionalRungTakesTheFieldsByPosition(t *testing.T) {
	tv := mappedTree()
	tv.SetKindMap("", NodeMap{Positional: true})
	if got, want := fieldsFor(tv, ""), ".0 .1 .2"; got != want {
		t.Errorf("the positional mapping names\n  %s\nwant\n  %s", got, want)
	}

	// A column added afterwards is picked up, which is why it is a flag and not a
	// generated list of names.
	tv.AddColumn(NewTreeColumn("owner", "Owner", 10*cell))
	if got, want := fieldsFor(tv, ""), ".0 .1 .2 .3"; got != want {
		t.Errorf("after a column was added it names\n  %s\nwant\n  %s", got, want)
	}
}

// InOrder is a list lined up with the columns, caption first.
func TestInOrderLinesTheFieldsUpWithTheColumns(t *testing.T) {
	tv := mappedTree()
	tv.SetKindMap("", InOrder("label", "bytes", "what"))
	if got, want := fieldsFor(tv, ""), "label bytes what"; got != want {
		t.Errorf("a list names\n  %s\nwant\n  %s", got, want)
	}

	// A list shorter than the columns leaves the rest on the rung below, which
	// is the identity here.
	tv.SetKindMap("", InOrder("label", "bytes"))
	if got, want := fieldsFor(tv, ""), "label bytes kind"; got != want {
		t.Errorf("a short list names\n  %s\nwant\n  %s", got, want)
	}
}

// **The rungs are tried most-specific first**, so a caller may line everything up
// and then say more about one column without the general answer overriding the
// particular one.
func TestAnExplicitColumnWinsOverTheGeneralRung(t *testing.T) {
	tv := mappedTree()
	tv.SetKindMap("", NodeMap{
		Positional: true,
		Columns: map[string]CellMap{
			"kind": {Value: "theRealKindField", Collate: serval.CollateNatural},
		},
	})
	if got, want := fieldsFor(tv, ""), ".0 .1 theRealKindField"; got != want {
		t.Errorf("with one column said outright it names\n  %s\nwant\n  %s", got, want)
	}
	if got := tv.cellOf("", tv.columns[1]).Collate; got != serval.CollateNatural {
		t.Errorf("the collation is %q, want the natural one that was asked for", got)
	}
}

// Value and display are two names for one thing until somebody says otherwise,
// and each falls back to the other.
func TestValueAndDisplayFallBackToEachOther(t *testing.T) {
	tv := mappedTree()
	tv.SetKindMap("", NodeMap{Columns: map[string]CellMap{
		"size": {Value: "bytes", Display: "humanSize"},
		"kind": {Display: "onlyShown"},
	}})
	if got := tv.cellOf("", tv.columns[0]); got.sortField() != "bytes" ||
		got.showField() != "humanSize" {
		t.Errorf("a column with both sorts by %q and shows %q",
			got.sortField(), got.showField())
	}
	// Display alone sorts by what is drawn, which is worse than sorting by what
	// is meant and better than not sorting.
	if got := tv.cellOf("", tv.columns[1]); got.sortField() != "onlyShown" ||
		got.showField() != "onlyShown" {
		t.Errorf("a column with only a display sorts by %q and shows %q",
			got.sortField(), got.showField())
	}
}

// Each KIND has its own mapping, which is the whole reason this is per kind: a
// host's rows say `bytes` and a window's say `size`, and the view wants one
// column called Size.
func TestEachKindMapsTheSameColumnToItsOwnField(t *testing.T) {
	tv := mappedTree()
	tv.SetKindMap("hosts", InOrder("hostname", "bytes", "hostKind"))
	tv.SetKindMap("windows", InOrder("title", "size", "windowKind"))

	if got, want := fieldsFor(tv, "hosts"), "hostname bytes hostKind"; got != want {
		t.Errorf("hosts name\n  %s\nwant\n  %s", got, want)
	}
	if got, want := fieldsFor(tv, "windows"), "title size windowKind"; got != want {
		t.Errorf("windows name\n  %s\nwant\n  %s", got, want)
	}
	// And a kind nobody mapped is the identity, not an error.
	if got, want := fieldsFor(tv, "volumes"), "value size kind"; got != want {
		t.Errorf("an unmapped kind names\n  %s\nwant\n  %s", got, want)
	}
}

// --- the sort ------------------------------------------------------------

// **The proxy resolves first, then the mapping.** The proxy is the view's own
// indirection, column to column; the mapping is the source's, column to field. A
// grafted kind never hears about the proxy and is only asked which of ITS fields
// fills the column the proxy landed on.
func TestTheProxyResolvesBeforeTheMapping(t *testing.T) {
	tv := NewTreeView()
	shown := NewTreeColumn("size", "Size", 10*cell)
	raw := NewTreeColumn("bytes", "", 0)
	raw.Hidden, raw.Numeric = true, true
	tv.AddColumn(shown)
	tv.AddColumn(raw)
	shown.SortProxy = 1 // sort the shown column by the hidden one

	// Two kinds spelling the RAW column differently. Sorting the shown column
	// must land on each kind's own raw field.
	tv.SetKindMap("hosts", NodeMap{Columns: map[string]CellMap{
		"bytes": {Value: "hostBytes"},
	}})
	tv.SetKindMap("windows", NodeMap{Columns: map[string]CellMap{
		"bytes": {Value: "windowBytes"},
	}})
	tv.SetSortLevels(SortLevel{By: 0}) // the SHOWN column

	for kind, want := range map[string]string{
		"hosts": "hostBytes", "windows": "windowBytes",
	} {
		got := tv.sortFields(kind)
		if len(got) != 2 || got[0].Field != want {
			t.Errorf("%s sorts by %v, want %q then the stability level", kind, got, want)
		}
	}
}

// A numeric column sorts by a NUMBER, so no collation is asked for -- numbers
// compare as numbers and a collation would say nothing about them.
func TestANumericColumnTakesNoCollation(t *testing.T) {
	tv := NewTreeView()
	num := NewTreeColumn("size", "Size", 10*cell)
	num.Numeric = true
	tv.AddColumn(num)
	tv.AddColumn(NewTreeColumn("name", "Name", 10*cell))
	tv.SetSortLevels(SortLevel{By: 0}, SortLevel{By: 1})

	got := tv.sortFields("")
	if len(got) != 3 {
		t.Fatalf("it produced %d levels, want two and the stability one", len(got))
	}
	if got[0].Collation != "" {
		t.Errorf("the numeric level asks for %q", got[0].Collation)
	}
	if got[1].Collation != serval.CollateFold {
		t.Errorf("the text level asks for %q, want the fold the view's own "+
			"comparison used", got[1].Collation)
	}
}

// **`seq` is the last level, and that is what makes the sort stable.** It was a
// bridge while the source was not asked to sort; now it is what `sort.SliceStable`
// was doing -- rows equal on every level keep the order the application put them
// in.
//
// This is what the mutation sweep found missing: dropping `seq` altogether passed
// every test, because the fixtures added their items in the order their keys were
// allocated, so key order stood in for it by accident.
func TestRowsEqualOnEveryLevelKeepTheApplicationsOrder(t *testing.T) {
	tv := NewTreeView()
	tv.AddColumn(NewTreeColumn("size", "Size", 10*cell))
	// Three rows with the SAME size, added in an order the sort cannot see.
	for _, name := range []string{"charlie", "alpha", "bravo"} {
		it := NewTreeItem(name)
		it.SetValue("size", "10")
		tv.AddRootItem(it)
	}
	tv.SetSortLevels(SortLevel{By: 0})

	if got, want := strings.Join(visualCaptions(tv), " "),
		"charlie alpha bravo"; got != want {
		t.Errorf("sorted on a column they all tie in, the rows read\n  %s\nwant\n  %s",
			got, want)
	}
}

// And unsorted, `seq` is the WHOLE order -- the application's own, even where it
// differs from the order the keys were allocated in. A tree whose items were
// inserted rather than appended still draws them where they were put.
func TestUnsortedTheOrderIsTheApplicationsOwn(t *testing.T) {
	tv := NewTreeView()
	first := NewTreeItem("added first")
	second := NewTreeItem("added second")
	tv.AddRootItem(first)
	tv.AddRootItem(second)

	// Put a LATER-made item at the front, so position order and key order
	// disagree. Nothing but `seq` can tell them apart.
	third := NewTreeItem("put at the front")
	tv.rootItems = append([]*TreeItem{third}, tv.rootItems...)
	tv.rebuildFlatList()

	if got, want := strings.Join(visualCaptions(tv), " "),
		"put at the front added first added second"; got != want {
		t.Errorf("unsorted the rows read\n  %s\nwant\n  %s", got, want)
	}
}

// A column whose field the record has not got is an EMPTY CELL: undefined, which
// is what serval answers for a field a record lacks everywhere else. Nothing is
// guessed and nothing falls back to another rung.
func TestAColumnThatMatchesNothingIsEmpty(t *testing.T) {
	tv := mappedTree()
	tv.SetKindMap("", InOrder(serval.ValueField, "nothingHasThis"))

	it := NewTreeItem("a row")
	it.SetValue("size", "10")
	tv.AddRootItem(it)

	// The mapping points the Size column at a field the made rows do not carry,
	// so the cell is empty -- and the tree still draws.
	if len(tv.flatList) != 1 || tv.flatList[0] != it {
		t.Fatalf("the tree drew %d rows", len(tv.flatList))
	}
	var out treeRows
	if err := tv.sequence().Read(&serval.Scope{Count: 10}, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.ids) != 1 {
		t.Errorf("the sequence holds %d rows", len(out.ids))
	}
}

// **The three cheap rungs line up one-to-one with the three shapes a `Whole`
// reading produces**, which is why each picks a different name for the same cell
// and why none of them is an invented convention.
//
//	a plain value         key, value                 the caption is `value`
//	named members         key, .caption, .size       the caption is `.caption`
//	positional members    key, .0, .1                the caption is `.0`
//
// `value` never wears a dot because it is the record's own value and not a member
// of it: `psl.go` answers nil for it on a list, having no value beside its members
// to give. So a simple list of strings needs no mapping at all -- the same reason
// the identity rung exists, reached from the record's side instead of the
// column's. serval's own tests are what pin the shapes; this pins that the rungs
// agree with them.
func TestTheRungsMatchTheShapesARecordComesIn(t *testing.T) {
	tv := mappedTree()
	for _, c := range []struct {
		shape   string
		m       NodeMap
		caption string
	}{
		{"a plain value", NodeMap{}, serval.ValueField},
		{"named members", NodeMap{Dotted: true}, ".caption"},
		{"positional members", NodeMap{Positional: true}, ".0"},
	} {
		tv.SetKindMap("", c.m)
		if got := tv.cellOf("", nil).sortField(); got != c.caption {
			t.Errorf("%s: the caption is %q, want %q", c.shape, got, c.caption)
		}
	}
}

// And a MADE row's caption goes out under the same name, so a tree's own items
// and a simple list of strings read identically -- which is the whole of why one
// mechanism serves both.
func TestAMadeRowsCaptionIsUnderTheSameName(t *testing.T) {
	tv := NewTreeView()
	it := NewTreeItem("the label")
	tv.AddRootItem(it)

	var out captionRows
	if err := tv.sequence().Read(&serval.Scope{Count: 10}, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.fields) != 1 {
		t.Fatalf("the tree stated %d rows", len(out.fields))
	}
	if got := out.fields[0].Get(serval.ValueField); !serval.Equal(got, serval.NewText("the label")) {
		t.Errorf("a made row carries %v under %q, want its caption",
			got, serval.ValueField)
	}
}

// captionRows keeps the fields as well as the identities, which treeRows does not.
type captionRows struct {
	fields []serval.Record
}

func (r *captionRows) Ordered() {}
func (r *captionRows) Record(_ *serval.Value, f serval.Record) error {
	r.fields = append(r.fields, f)
	return nil
}
func (r *captionRows) Subset(id *serval.Value, f serval.Record, _ serval.Totals) error {
	return r.Record(id, f)
}
func (r *captionRows) Done(serval.Complete) {}
