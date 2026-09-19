package trinkets

// A tree reading a DECLARED source.
//
// One mechanism, whether the rows are the tree's own or somebody else's -- which
// is the claim, and the way to check it is that everything the view asks of a row
// goes on being asked the same way. Fourteen places ask an item whether it is a
// leaf and seven ask how deep it stands; none of them learned about a field, so
// the interesting assertions here are the ones about `Level()`, `IsLeaf()` and
// `Expanded` over rows that were never items until the tree made them.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/serval"
)

// hosts is a two-source hierarchy: hosts, and the applications on them.
func hostsAndApps(t *testing.T) *serval.TreeSource {
	t.Helper()
	hosts := serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(1), serval.Record{
			serval.Named("hostname", "kestrel"), serval.Named("seq", 1),
		}),
		serval.NewRow(serval.NewInt(2), serval.Record{
			serval.Named("hostname", "merlin"), serval.Named("seq", 2),
		}),
	})
	apps := serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(10), serval.Record{
			serval.Named("title", "a browser"), serval.Named("host", 1),
			serval.Named("bytes", 2048),
		}),
		serval.NewRow(serval.NewInt(11), serval.Record{
			serval.Named("title", "an editor"), serval.Named("host", 1),
			serval.Named("bytes", 512),
		}),
	})
	src, err := serval.NewTreeSource(serval.TreeOptions{
		Source: hosts,
		Spec:   &serval.Spec{Sort: []serval.SortLevel{{Field: "seq"}}},
		Types: serval.NodeTypes{
			Default: &serval.NodeType{Then: serval.Always("applications")},
			Named: map[string]*serval.NodeType{
				"applications": {
					Source:   apps,
					Children: serval.ChildrenByKey("host"),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("stating the tree: %v", err)
	}
	return src
}

// onHosts is a view over that, with a Size column and a mapping per kind -- which
// is the whole point of the mapping being per kind: a host has no size and an
// application spells one `bytes`.
func onHosts(t *testing.T) (*TreeView, *serval.TreeSource) {
	t.Helper()
	tv := NewTreeView()
	tv.AddColumn(NewTreeColumn("size", "Size", 10*cell))
	tv.SetKindMap("", InOrder("hostname"))
	tv.SetKindMap("applications", InOrder("title", "bytes"))

	src := hostsAndApps(t)
	tv.SetSource(src)
	return tv, src
}

// Nothing expanded: the top level, out of somebody else's source.
func TestADeclaredSourceDrawsItsTopLevel(t *testing.T) {
	tv, _ := onHosts(t)
	if got, want := strings.Join(visualCaptions(tv), " "), "kestrel merlin"; got != want {
		t.Errorf("the tree reads\n  %s\nwant\n  %s", got, want)
	}
	if tv.Source() == nil {
		t.Error("it does not report the source it was given")
	}
}

// **`IsLeaf` goes on being the one question fourteen places ask.** A collapsed
// row out of a source has no Children slice, so counting the slice would draw
// every closed node as a leaf -- which is what `Kids` is for.
func TestACollapsedSourceRowIsNotALeaf(t *testing.T) {
	tv, _ := onHosts(t)
	kestrel, merlin := tv.flatList[0], tv.flatList[1]

	if len(kestrel.Children) != 0 {
		t.Errorf("a collapsed row holds %d children", len(kestrel.Children))
	}
	if kestrel.IsLeaf() {
		t.Error("the host with two applications says it is a leaf")
	}
	if kestrel.Kids != 2 {
		t.Errorf("it says it has %d children, want the two the source counted", kestrel.Kids)
	}
	// And a host with none really is a leaf, which is a statement and not an
	// ignorance: the source counted, and the answer was nought.
	if !merlin.IsLeaf() {
		t.Error("the host with no applications says it is not a leaf")
	}
}

// **`Level()` goes on being how seven places ask how deep a row stands.** The
// sequence arrives flat with a depth per row, and the parentage is rebuilt from
// it, so nothing that draws indentation had to learn about a field.
func TestDepthComesBackAsRealParentage(t *testing.T) {
	tv, src := onHosts(t)
	src.ExpandAll()
	tv.rebuildFlatList()

	if got, want := strings.Join(visualCaptions(tv), " "),
		"kestrel a browser an editor merlin"; got != want {
		t.Fatalf("expanded, the tree reads\n  %s\nwant\n  %s", got, want)
	}
	for i, want := range []int{0, 1, 1, 0} {
		if got := tv.flatList[i].Level(); got != want {
			t.Errorf("row %d (%s) stands at level %d, want %d",
				i, tv.flatList[i].Text, got, want)
		}
	}
	// The parentage is real, not just the number: a child's Parent is the row
	// above it that it belongs to, and that row holds it.
	if tv.flatList[1].Parent != tv.flatList[0] {
		t.Error("the application's parent is not the host it is on")
	}
	if len(tv.flatList[0].Children) != 2 {
		t.Errorf("the host holds %d visible children, want two",
			len(tv.flatList[0].Children))
	}
	// And a top-level row has no parent, whatever came before it.
	if tv.flatList[3].Parent != nil {
		t.Error("the second host has a parent")
	}
}

// Each KIND fills the columns from its own fields, which is why the mapping is
// per kind: a host spells its label `hostname` and an application `title`, and
// only the application has a size.
func TestEachKindFillsTheColumnsFromItsOwnFields(t *testing.T) {
	tv, src := onHosts(t)
	src.ExpandAll()
	tv.rebuildFlatList()

	if got := tv.flatList[0].Value("size"); got != "" {
		t.Errorf("the host's size cell holds %q, want nothing -- it has no size", got)
	}
	if got := tv.flatList[1].Value("size"); got != "2048" {
		t.Errorf("the application's size cell holds %q, want its bytes", got)
	}
}

// **The same row leads to the same pointer across a rebuild**, because everything
// reading flatList compares them -- selection restores itself by one and the row
// editor holds one.
func TestARowKeepsItsPointerAcrossARebuild(t *testing.T) {
	tv, src := onHosts(t)
	was := tv.flatList[0]

	src.ExpandAll()
	tv.rebuildFlatList()

	if tv.flatList[0] != was {
		t.Error("the host became a different object when the tree was redrawn")
	}
	// And the application data an app hung off it survives, which is what an
	// application uses the tree for at all.
	was.Data = "the application's own"
	tv.rebuildFlatList()
	if tv.flatList[0].Data != "the application's own" {
		t.Errorf("the row's Data is %v", tv.flatList[0].Data)
	}
}

// --- expansion -----------------------------------------------------------

// **The verbs reach the MARKS where a source was declared**, because there are no
// items to carry a field in from. One mechanism said from the other side.
func TestExpandingADeclaredRowMovesAMark(t *testing.T) {
	tv, src := onHosts(t)
	kestrel := tv.flatList[0]

	tv.ExpandItem(kestrel)
	if got, want := strings.Join(visualCaptions(tv), " "),
		"kestrel a browser an editor merlin"; got != want {
		t.Errorf("after expanding, the tree reads\n  %s\nwant\n  %s", got, want)
	}
	if held := src.Marks().Held(); held != 1 {
		t.Errorf("one node opened holds %d marks, want one", held)
	}
	// The state came back off the row, not out of a field the view set.
	if !tv.flatList[0].Expanded {
		t.Error("the expanded row does not say it is expanded")
	}

	tv.CollapseItem(tv.flatList[0])
	if got, want := strings.Join(visualCaptions(tv), " "), "kestrel merlin"; got != want {
		t.Errorf("after collapsing, the tree reads\n  %s\nwant\n  %s", got, want)
	}
	if held := src.Marks().Held(); held != 0 {
		t.Errorf("collapsed back it holds %d marks, want none -- closing a node "+
			"that inherits closed says nothing", held)
	}
}

// **Expanding everything is ONE mark**, which is the whole point of `openAll`
// being a state rather than an iteration.
func TestExpandingEverythingOnADeclaredSourceIsOneMark(t *testing.T) {
	tv, src := onHosts(t)
	tv.ExpandAll()

	if got, want := strings.Join(visualCaptions(tv), " "),
		"kestrel a browser an editor merlin"; got != want {
		t.Errorf("expanded, the tree reads\n  %s\nwant\n  %s", got, want)
	}
	if held := src.Marks().Held(); held != 1 {
		t.Errorf("the whole tree expanded holds %d marks, want one", held)
	}

	tv.CollapseAll()
	if got, want := strings.Join(visualCaptions(tv), " "), "kestrel merlin"; got != want {
		t.Errorf("collapsed, the tree reads\n  %s\nwant\n  %s", got, want)
	}
	if held := src.Marks().Held(); held != 0 {
		t.Errorf("collapsed it holds %d marks", held)
	}
}

// A tree's own items are untouched by any of this: the field is still the
// authoring surface where the tree makes its own source, which is what every
// existing example relies on.
func TestATreesOwnItemsStillUseTheField(t *testing.T) {
	tv, by := kinTree()
	tv.ExpandItem(by["alpha"])

	if !by["alpha"].Expanded {
		t.Error("expanding a made row did not set its field")
	}
	if got, want := rowsOf(tv), "alpha/0 beta/1 gamma/0"; got != want {
		t.Errorf("the made tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// --- a plain source ------------------------------------------------------

// A source that is not a tree at all is read as one flat level, which is a tree
// of one generation and is ordinary rather than an error.
func TestAPlainSourceIsOneFlatLevel(t *testing.T) {
	tv := NewTreeView()
	tv.SetKindMap("", InOrder(serval.ValueField))
	tv.SetSource(serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(1), serval.Record{serval.Named("value", "one")}),
		serval.NewRow(serval.NewInt(2), serval.Record{serval.Named("value", "two")}),
	}))

	if got, want := strings.Join(visualCaptions(tv), " "), "one two"; got != want {
		t.Errorf("a plain source reads\n  %s\nwant\n  %s", got, want)
	}
	// Every row is a leaf and stands at the top: nothing said otherwise, and a
	// flat source has nothing to say.
	for i, item := range tv.flatList {
		if !item.IsLeaf() || item.Level() != 0 {
			t.Errorf("row %d is a leaf=%v at level %d", i, item.IsLeaf(), item.Level())
		}
	}
}

// Setting a source replaces whatever the tree was reading, its own items
// included -- and setting nil goes back to them.
func TestSettingASourceReplacesTheItemsAndNilRestoresThem(t *testing.T) {
	tv, by := kinTree()
	if got := rowsOf(tv); got != "alpha/0 gamma/0" {
		t.Fatalf("before: %s", got)
	}

	tv.SetKindMap("", InOrder("hostname"))
	tv.SetSource(hostsAndApps(t))
	if got, want := strings.Join(visualCaptions(tv), " "), "kestrel merlin"; got != want {
		t.Errorf("with a source it reads\n  %s\nwant\n  %s", got, want)
	}

	tv.SetSource(nil)
	tv.SetKindMap("", NodeMap{})
	if got, want := rowsOf(tv), "alpha/0 gamma/0"; got != want {
		t.Errorf("back on its own items it reads\n  %s\nwant\n  %s", got, want)
	}
	// The items themselves were never touched.
	if by["alpha"].Text != "alpha" {
		t.Errorf("the item's own text is now %q", by["alpha"].Text)
	}
}

// --- three levels, which is where the shallow fixture above cannot reach ----

// A three-level source, because two levels hide three different mistakes: a mark
// chain of one segment reverses to itself, a chain that never walks up looks
// right, and a spine that never unwinds is never asked for a stale entry.
//
//	kestrel
//	  a browser
//	    a tab
//	  an editor
//	    a buffer
func deepTree(t *testing.T) *serval.TreeSource {
	t.Helper()
	hosts := serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(1), serval.Record{serval.Named("hostname", "kestrel")}),
	})
	apps := serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(10), serval.Record{
			serval.Named("title", "a browser"), serval.Named("host", 1),
			serval.Named("seq", 1),
		}),
		serval.NewRow(serval.NewInt(11), serval.Record{
			serval.Named("title", "an editor"), serval.Named("host", 1),
			serval.Named("seq", 2),
		}),
	})
	windows := serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(100), serval.Record{
			serval.Named("label", "a tab"), serval.Named("app", 10),
		}),
		serval.NewRow(serval.NewInt(101), serval.Record{
			serval.Named("label", "a buffer"), serval.Named("app", 11),
		}),
	})
	src, err := serval.NewTreeSource(serval.TreeOptions{
		Source: hosts,
		Spec:   &serval.Spec{},
		Types: serval.NodeTypes{
			Default: &serval.NodeType{Then: serval.Always("applications")},
			Named: map[string]*serval.NodeType{
				"applications": {
					Source: apps,
					Children: serval.Sorted(serval.ChildrenByKey("host"),
						serval.SortLevel{Field: "seq"}),
					Then: serval.Always("windows"),
				},
				"windows": {Source: windows, Children: serval.ChildrenByKey("app")},
			},
		},
	})
	if err != nil {
		t.Fatalf("stating the tree: %v", err)
	}
	return src
}

func onDeep(t *testing.T) (*TreeView, *serval.TreeSource) {
	t.Helper()
	tv := NewTreeView()
	tv.SetKindMap("", InOrder("hostname"))
	tv.SetKindMap("applications", InOrder("title"))
	tv.SetKindMap("windows", InOrder("label"))
	src := deepTree(t)
	tv.SetSource(src)
	return tv, src
}

// **A grandchild's parent is the row it belongs to, not the last row seen.** The
// spine has to unwind as the depth falls: after a browser's tab at depth 2, the
// editor stands at depth 1 again, and ITS child must hang off the editor rather
// than off whatever was at index 1 before.
func TestParentageIsRightWhereTheDepthFallsAndRises(t *testing.T) {
	tv, src := onDeep(t)
	src.ExpandAll()
	tv.rebuildFlatList()

	if got, want := strings.Join(visualCaptions(tv), " "),
		"kestrel a browser a tab an editor a buffer"; got != want {
		t.Fatalf("expanded, the tree reads\n  %s\nwant\n  %s", got, want)
	}
	for i, want := range []int{0, 1, 2, 1, 2} {
		if got := tv.flatList[i].Level(); got != want {
			t.Errorf("row %d (%s) stands at level %d, want %d",
				i, tv.flatList[i].Text, got, want)
		}
	}
	// The buffer belongs to the EDITOR, which is the row at depth 1 that came
	// after a deeper one. A spine that did not unwind would hang it off the
	// browser -- the stale entry still standing at that depth.
	buffer, editor, browser := tv.flatList[4], tv.flatList[3], tv.flatList[1]
	if buffer.Parent != editor {
		t.Errorf("the buffer hangs off %v, want the editor", buffer.Parent)
	}
	if buffer.Parent == browser {
		t.Error("the buffer hangs off the browser: the spine did not unwind")
	}
	if len(browser.Children) != 1 || browser.Children[0] != tv.flatList[2] {
		t.Errorf("the browser holds %d children, want just its tab", len(browser.Children))
	}
}

// **A mark chain walks all the way up, in root-to-leaf order.** Expanding a row
// at depth 1 is what shows it: its chain is two segments, and a chain that
// stopped at the row itself, or came back reversed, would name a different node.
func TestExpandingADeeperRowNamesTheWholeChain(t *testing.T) {
	tv, _ := onDeep(t)
	tv.ExpandItem(tv.flatList[0]) // kestrel, so the applications show

	browser := tv.flatList[1]
	if browser.Text != "a browser" {
		t.Fatalf("row 1 is %q", browser.Text)
	}
	chain := tv.chainOf(browser)
	if len(chain) != 2 {
		t.Fatalf("the browser's chain is %v, want the host's segment then its own", chain)
	}
	if chain[0] != serval.Key(serval.NewInt(1)) {
		t.Errorf("the chain starts at %q, want the host it stands under", chain[0])
	}
	if chain[1] != serval.Key(serval.NewInt(10)) {
		t.Errorf("the chain ends at %q, want the browser itself", chain[1])
	}

	// And expanding it really reaches that node rather than some other.
	tv.ExpandItem(browser)
	if got, want := strings.Join(visualCaptions(tv), " "),
		"kestrel a browser a tab an editor"; got != want {
		t.Errorf("with the browser open the tree reads\n  %s\nwant\n  %s", got, want)
	}
	// The EDITOR is still closed, which is what a chain naming the wrong node
	// would have got wrong.
	if tv.flatList[3].Expanded {
		t.Error("the editor opened too: the chain named the wrong node")
	}
}

// --- and from the wire --------------------------------------------------

// plainTree is a hierarchy whose records name their label serval's own way, which
// is what a tree declared in the WIRE LANGUAGE gets: the language can say where
// the rows come from and not yet what each kind puts in which column, so the
// identity mapping is the one in force -- and its caption is `value`.
func plainTree(t *testing.T) *serval.TreeSource {
	t.Helper()
	hosts := serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(1), serval.Record{serval.Named("value", "kestrel")}),
	})
	apps := serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(10), serval.Record{
			serval.Named("value", "a browser"), serval.Named("host", 1),
		}),
	})
	src, err := serval.NewTreeSource(serval.TreeOptions{
		Source: hosts,
		Spec:   &serval.Spec{},
		Types: serval.NodeTypes{
			Default: &serval.NodeType{Then: serval.Always("applications")},
			Named: map[string]*serval.NodeType{
				"applications": {Source: apps, Children: serval.ChildrenByKey("host")},
			},
		},
	})
	if err != nil {
		t.Fatalf("stating the tree: %v", err)
	}
	return src
}

// **One word, and it means on a tree what it means on a list.** A statement names
// a source and the tree reads it, nesting and all -- the hierarchy being the
// SOURCE's, so nothing in the statement had to describe it.
func TestATreeReadsANamedSource(t *testing.T) {
	src := plainTree(t)
	RegisterSource("tree.wire", src)
	defer UnregisterSource("tree.wire")

	f, _ := buildWithEvents(t, nil, `
tv=new treeview source="source:tree.wire"
`)
	tv := f.targets[0].(*TreeView)
	if tv.Source() == nil {
		t.Fatal("the statement was taken and the tree reads its own items")
	}
	if got, want := strings.Join(visualCaptions(tv), " "), "kestrel"; got != want {
		t.Fatalf("the tree reads\n  %s\nwant\n  %s", got, want)
	}
	// The source counted, so the twisty is live before anything opens.
	if tv.flatList[0].IsLeaf() {
		t.Error("the host with an application says it is a leaf")
	}
	src.ExpandAll()
	tv.rebuildFlatList()
	if got, want := strings.Join(visualCaptions(tv), " "),
		"kestrel a browser"; got != want {
		t.Errorf("expanded, the tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// A name nothing stands for is refused by the statement rather than leaving a
// tree quietly empty -- the same answer the list gives.
func TestATreeRefusesANameNothingStandsFor(t *testing.T) {
	script, err := protocol.Parse(`tv=new treeview source="source:not.registered"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	f := &captureFactory{inner: protocol.NewRegistryFactory(&protocol.BindContext{})}
	if _, err := protocol.NewSession().Execute(script, f); err == nil {
		t.Fatal("the statement was taken, and nothing stands for that name")
	}
}

// --- the columns order a declared tree's levels --------------------------

// sized is a flat declared source with a `size` field, which is what a column
// called `size` reads under the identity mapping -- so nothing is declared here and
// the point is that nothing has to be.
func sized(t *testing.T) *serval.TreeSource {
	t.Helper()
	rows := serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(1), serval.Record{
			serval.Named("value", "middling"), serval.Named("size", 20),
			serval.Named("seq", 1),
		}),
		serval.NewRow(serval.NewInt(2), serval.Record{
			serval.Named("value", "large"), serval.Named("size", 300),
			serval.Named("seq", 2),
		}),
		serval.NewRow(serval.NewInt(3), serval.Record{
			serval.Named("value", "small"), serval.Named("size", 4),
			serval.Named("seq", 3),
		}),
	})
	src, err := serval.NewTreeSource(serval.TreeOptions{
		Source: rows,
		Spec:   &serval.Spec{Sort: []serval.SortLevel{{Field: "seq"}}},
		Types:  serval.NodeTypes{Default: &serval.NodeType{}},
	})
	if err != nil {
		t.Fatalf("stating the tree: %v", err)
	}
	return src
}

// **A tree with NO mapping at all still has its columns translated**, the identity
// rung being what a column and a field wanting the same name costs. So the default
// kind is told whether or not anybody has declared one.
func TestADeclaredTreeWithNoMappingStillSortsByAColumn(t *testing.T) {
	tv := NewTreeView()
	tv.AddColumn(NewTreeColumn("size", "Size", 10*cell))
	tv.SetSource(sized(t))

	if got, want := strings.Join(visualCaptions(tv), " "),
		"middling large small"; got != want {
		t.Fatalf("unsorted the tree reads\n  %s\nwant\n  %s", got, want)
	}
	tv.SetSorted(true, 0, false)
	if got, want := strings.Join(visualCaptions(tv), " "),
		"small middling large"; got != want {
		t.Errorf("by size the tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// **A sort already in force reaches the source the moment it is given one.** The
// rows arrive in the order the columns state rather than in the configuration's and
// then again in this one, which a first draw in the wrong order would show.
func TestASortAlreadySetReachesASourceGivenAfterwards(t *testing.T) {
	tv := NewTreeView()
	tv.AddColumn(NewTreeColumn("size", "Size", 10*cell))
	tv.SetSorted(true, 0, true)
	tv.SetSource(sized(t))

	if got, want := strings.Join(visualCaptions(tv), " "),
		"large middling small"; got != want {
		t.Errorf("the first draw reads\n  %s\nwant\n  %s", got, want)
	}
}

// --- a row says whether it may be written in -----------------------------

// **`ReadOnly` is per ROW and not per kind**, which is why the mapping names a
// field rather than carrying a flag: two rows of one kind can differ about it.
//
// A row that says nothing is not held out. `undefined` is not a truth, and a source
// that never heard of the field has held nothing out -- which is what every tree
// before this one did.
func TestARowSaysWhetherItMayBeWrittenIn(t *testing.T) {
	rows := serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(1), serval.Record{
			serval.Named("value", "the machine"), serval.Named("fixed", true),
		}),
		serval.NewRow(serval.NewInt(2), serval.Record{
			serval.Named("value", "a client"), serval.Named("fixed", false),
		}),
		serval.NewRow(serval.NewInt(3), serval.Record{
			serval.Named("value", "says nothing"),
		}),
		serval.NewRow(serval.NewInt(4), serval.Record{
			serval.Named("value", "a word"), serval.Named("fixed", "yes"),
		}),
		serval.NewRow(serval.NewInt(5), serval.Record{
			serval.Named("value", "a number"), serval.Named("fixed", 0),
		}),
	})
	src, err := serval.NewTreeSource(serval.TreeOptions{
		Source: rows,
		Spec:   &serval.Spec{},
		Types:  serval.NodeTypes{Default: &serval.NodeType{}},
	})
	if err != nil {
		t.Fatalf("stating the tree: %v", err)
	}
	tv := NewTreeView()
	tv.SetKindMap("", NodeMap{ReadOnly: "fixed"})
	tv.SetSource(src)

	for i, want := range []bool{true, false, false, true, false} {
		if got := tv.flatList[i].ReadOnly; got != want {
			t.Errorf("row %d (%s) says read-only=%v, want %v",
				i, tv.flatList[i].Text, got, want)
		}
	}
}

// **Nothing sorts a declared source's rows twice.** The sibling run the tree
// holds was rebuilt from the sequence serval produced, so it is already in visual
// order -- and the view's own comparison is a DIFFERENT one, folding with
// strings.ToLower where serval applies the level's collation. Two comparisons over
// one run that disagree put the elbow of a tree line on the wrong row.
//
// A collation the view has no equivalent for is what shows it: `natural` compares
// digit runs as numbers, so item2 stands before item10 where folding puts item10
// first.
func TestADeclaredSourcesSiblingsAreNotSortedTwice(t *testing.T) {
	rows := serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(1), serval.Record{serval.Named("value", "item10")}),
		serval.NewRow(serval.NewInt(2), serval.Record{serval.Named("value", "item2")}),
	})
	src, err := serval.NewTreeSource(serval.TreeOptions{
		Source: rows,
		Spec: &serval.Spec{Sort: []serval.SortLevel{
			{Field: "value", Level: serval.Level{Collation: serval.CollateNatural}},
		}},
		Types: serval.NodeTypes{Default: &serval.NodeType{}},
	})
	if err != nil {
		t.Fatalf("stating the tree: %v", err)
	}
	tv := NewTreeView()
	tv.SetTreeLines(true)
	tv.SetSource(src)

	// serval's natural order, which is what the rows arrive in.
	if got, want := strings.Join(visualCaptions(tv), " "), "item2 item10"; got != want {
		t.Fatalf("the tree reads\n  %s\nwant\n  %s", got, want)
	}
	// And the tree lines agree with the rows, rather than with a second comparison
	// that would have made item10 the first of the two.
	first, second := tv.flatList[0], tv.flatList[1]
	if !tv.hasNextVisualSibling(first) {
		t.Errorf("%q draws as the last of its run; it is the first", first.Text)
	}
	if tv.hasNextVisualSibling(second) {
		t.Errorf("%q draws as having a sibling after it; it is the last", second.Text)
	}
}

// And with a sort IN FORCE, which is the case that needs saying carefully.
//
// The view tells the source the order its columns state, so ordinarily the two
// agree by construction -- there is nothing for a second comparison to disagree
// with. They part where the mapping names a collation the view's own comparison
// has no equivalent for: serval compares `natural` by digit runs, and the view
// folds with strings.ToLower, which puts item10 first.
func TestASortedDeclaredSourceIsNotComparedTheViewsOwnWay(t *testing.T) {
	rows := serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(1), serval.Record{serval.Named("value", "item10")}),
		serval.NewRow(serval.NewInt(2), serval.Record{serval.Named("value", "item2")}),
	})
	src, err := serval.NewTreeSource(serval.TreeOptions{
		Source: rows,
		Spec:   &serval.Spec{},
		Types:  serval.NodeTypes{Default: &serval.NodeType{}},
	})
	if err != nil {
		t.Fatalf("stating the tree: %v", err)
	}
	tv := NewTreeView()
	tv.SetTreeLines(true)
	tv.SetKindMap("", NodeMap{Caption: CellMap{
		Value: "value", Collate: serval.CollateNatural,
	}})
	tv.SetSource(src)
	tv.SetSorted(true, -1, false) // the key column, ascending

	if got, want := strings.Join(visualCaptions(tv), " "), "item2 item10"; got != want {
		t.Fatalf("sorted naturally the tree reads\n  %s\nwant\n  %s", got, want)
	}
	// The elbow has to follow the rows. Folding would make item10 the first of
	// the two and put the last-of-run mark on item2.
	if !tv.hasNextVisualSibling(tv.flatList[0]) {
		t.Errorf("%q draws as the last of its run; it is the first",
			tv.flatList[0].Text)
	}
	if tv.hasNextVisualSibling(tv.flatList[1]) {
		t.Errorf("%q draws as having a sibling after it; it is the last",
			tv.flatList[1].Text)
	}
}
