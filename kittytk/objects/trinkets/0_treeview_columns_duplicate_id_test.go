package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/protocol"
)

// A column's id is the key its cell values live under, so two columns
// sharing one share a single value per item: whichever was written last is
// what BOTH cells show, and neither column can hold anything of its own.
// The second declaration is refused rather than merged.
func TestASecondColumnCannotTakeATakenID(t *testing.T) {
	tv := NewTreeView()
	if err := tv.AddColumn(NewTreeColumn("size", "Size", 10)); err != nil {
		t.Fatalf("first column: %v", err)
	}

	err := tv.AddColumn(NewTreeColumn("size", "Kind", 12))
	if err == nil {
		t.Fatal("a second column took the id 'size' without complaint")
	}
	if !strings.Contains(err.Error(), "size") || !strings.Contains(err.Error(), "Size") {
		t.Errorf("error %q names neither the id nor the column already holding it", err)
	}
	if got := len(tv.Columns()); got != 1 {
		t.Errorf("%d columns after the refusal, want the first one only", got)
	}
}

// Blank is an id like any other. It is what a column declared with no id=
// carries, and two of those collapse onto Values[""] exactly as two named
// duplicates collapse -- so the second is refused the same way. A column
// that wants no data of its own still needs an id nothing else uses.
func TestABlankColumnIDIsStillAnID(t *testing.T) {
	tv := NewTreeView()
	if err := tv.AddColumn(&TreeColumn{Caption: "Spacer", Width: 2}); err != nil {
		t.Fatalf("first blank-id column: %v", err)
	}
	if err := tv.AddColumn(&TreeColumn{Caption: "Another", Width: 2}); err == nil {
		t.Error("a second column with no id was accepted; both write to Values[\"\"]")
	}
}

// The refusal reaches a wire client as the error on its statement, rather
// than leaving it with a column whose cells belong to another.
func TestTheWireRefusesADuplicateColumnID(t *testing.T) {
	script, err := protocol.Parse(`tv=new treeview caption="Name" showheader children={
	new column id=size caption="Size" width=10
	new column id=size caption="Kind" width=12
}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = protocol.NewSession().Execute(script, protocol.NewRegistryFactory(&protocol.BindContext{}))
	if err == nil {
		t.Fatal("the wire built two columns under one id")
	}
	if !strings.Contains(err.Error(), "size") {
		t.Errorf("error %q does not name the id that collided", err)
	}
}

// A collection is packaging -- the treeview adopts its members as if they
// were appended directly -- so it has to refuse a duplicate too, including
// one that collides with a column declared outside it.
func TestACollectionRefusesADuplicateColumnID(t *testing.T) {
	script, err := protocol.Parse(`tv=new treeview caption="Name" children={
	new column id=size caption="Size" width=10
	new collection children={
		new column id=kind caption="Kind" width=12
		new column id=size caption="Also Size" width=8
	}
}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = protocol.NewSession().Execute(script, protocol.NewRegistryFactory(&protocol.BindContext{}))
	if err == nil {
		t.Fatal("a collection member took an id the treeview already carried")
	}
	if !strings.Contains(err.Error(), "size") {
		t.Errorf("error %q does not name the id that collided", err)
	}
}

// Distinct ids are the ordinary case and stay ordinary, including the
// hidden-sort-column pattern: a displayed column and the hidden numeric one
// that sorts for it are two ids, not one.
func TestDistinctColumnIDsAreAccepted(t *testing.T) {
	tv := NewTreeView()
	raw := NewTreeColumn("rawsize", "", 0)
	raw.Hidden, raw.Numeric = true, true
	for _, c := range []*TreeColumn{
		NewTreeColumn("size", "Size", 10),
		NewTreeColumn("kind", "Kind", 12),
		raw,
	} {
		if err := tv.AddColumn(c); err != nil {
			t.Fatalf("AddColumn(%q): %v", c.ID, err)
		}
	}
	if got := len(tv.Columns()); got != 3 {
		t.Errorf("%d columns, want 3", got)
	}

	// And each one keeps its own cell value.
	it := NewTreeItem("Report.txt")
	tv.AddRootItem(it)
	it.SetValue("size", "311 KB")
	it.SetValue("kind", "Text Document")
	it.SetValue("rawsize", "318464")
	if it.Value("size") != "311 KB" || it.Value("kind") != "Text Document" || it.Value("rawsize") != "318464" {
		t.Errorf("cells ran together: size=%q kind=%q rawsize=%q",
			it.Value("size"), it.Value("kind"), it.Value("rawsize"))
	}
}
