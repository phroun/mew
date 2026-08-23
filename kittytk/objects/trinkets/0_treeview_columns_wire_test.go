package trinkets

import (
	"fmt"
	"testing"

	"github.com/phroun/kittytk/protocol"
)

// Columns build over the wire alongside items; the treeview's column
// props apply; a second batch (once item IDs are known) fills the
// column-major cell values in via `set <column> children={new cell}`.
func TestTreeViewColumnsOverWire(t *testing.T) {
	ctx := &protocol.BindContext{}
	f := &captureFactory{inner: protocol.NewRegistryFactory(ctx)}
	s := protocol.NewSession()

	build := `
tree=new treeview caption="Name" showheader sorted sortedby=-1 children={
	sizec=new column id=size caption="Size" width=10 align=right sortable
	kindc=new column id=kind caption="Kind" width=12 optional
	a=new item caption="Report.txt"
	b=new item caption="Folder" expanded children={
		c=new item caption="inner.txt"
	}
}
`
	script, err := protocol.Parse(build)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := s.Execute(script, f)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	tv := f.targets[0].(*TreeView)
	if tv.KeyCaption() != "Name" || !tv.showHeader {
		t.Errorf("caption/showheader not applied: %q %v", tv.KeyCaption(), tv.showHeader)
	}
	if sorted, by, desc := tv.Sorted(); !sorted || by != -1 || desc {
		t.Errorf("sort state = %v/%d/%v", sorted, by, desc)
	}
	if len(tv.Columns()) != 2 {
		t.Fatalf("columns = %d, want 2", len(tv.Columns()))
	}
	size := tv.ColumnByID("size")
	if size == nil || size.Width != 10 || size.Align != "right" || !size.Sortable {
		t.Fatalf("size column misapplied: %+v", size)
	}
	// Wire-built columns get the documented defaults: resizable and
	// optional (divider dragging and the [=] chooser work without the
	// script spelling the flags out).
	for _, c := range tv.Columns() {
		if !c.Resizable || !c.Optional {
			t.Errorf("column %q: resizable=%v optional=%v, want true/true", c.ID, c.Resizable, c.Optional)
		}
	}

	// Surface the nested items' IDs (top-level reference statements),
	// then send the column-major cell values keyed by them.
	script, err = protocol.Parse("aid=tree.a\nbid=tree.b\ncid=tree.b.c")
	if err != nil {
		t.Fatal(err)
	}
	reply, err = s.Execute(script, f)
	if err != nil {
		t.Fatalf("surface: %v", err)
	}
	values := fmt.Sprintf(`
set tree.sizec children={
	new cell item=%d value="12 KB"
	new cell item=%d value="--"
	new cell item=%d value="1 KB"
}
`, reply.IDs["aid"], reply.IDs["bid"], reply.IDs["cid"])
	script, err = protocol.Parse(values)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(script, f); err != nil {
		t.Fatalf("values: %v", err)
	}

	if got := tv.rootItems[0].Value("size"); got != "12 KB" {
		t.Errorf("root value = %q", got)
	}
	if got := tv.rootItems[1].Children[0].Value("size"); got != "1 KB" {
		t.Errorf("nested value = %q", got)
	}

	// Live column mutation routes to the adopted column.
	script, _ = protocol.Parse(`set tree.sizec width=14 hidden`)
	if _, err := s.Execute(script, f); err != nil {
		t.Fatalf("set column: %v", err)
	}
	if size.Width != 14 || !size.Hidden {
		t.Errorf("live column set missed: %+v", size)
	}
}

// A sort request emits the wire event with the treeview's identity and
// the requested column/direction.
func TestTreeViewSortEventOverWire(t *testing.T) {
	f, events := buildWithEvents(t, nil, `
new treeview showheader children={
	new column id=size caption="Size" width=10 sortable
	new item caption="x"
}
`)
	tv := f.targets[0].(*TreeView)
	*events = nil

	tv.headerSortClick(tv.ColumnByID("size"))
	got := eventsOfType(*events, "sort")
	if len(got) != 1 {
		t.Fatalf("sort events = %d, want 1", len(got))
	}
	if id, ok := got[0].Trinket(); !ok || id != uint64(tv.ObjectID()) {
		t.Errorf("event trinket = %d", id)
	}
	if by, ok := got[0].Int("sortedby"); !ok || by != 0 {
		t.Errorf("sortedby = %d", by)
	}
	if got[0].Flag("descending") != protocol.FlagFalse {
		t.Errorf("descending = %v", got[0].Flag("descending"))
	}
}

// The generic collection packages members for an adopting parent:
// columns via a collection land exactly like direct children.
func TestTreeViewColumnsViaCollection(t *testing.T) {
	ctx := &protocol.BindContext{}
	f := &captureFactory{inner: protocol.NewRegistryFactory(ctx)}
	s := protocol.NewSession()
	script, err := protocol.Parse(`
new treeview children={
	new collection of=columns children={
		new column id=a caption="A" width=5
		new column id=b caption="B" width=6
	}
	new item caption="row"
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(script, f); err != nil {
		t.Fatalf("execute: %v", err)
	}
	tv := f.targets[0].(*TreeView)
	if len(tv.Columns()) != 2 || tv.ColumnByID("b") == nil {
		t.Fatalf("collection-packaged columns not adopted: %d", len(tv.Columns()))
	}
}
