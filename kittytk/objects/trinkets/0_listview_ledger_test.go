package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/protocol"
)

// propsOf returns a registered type's property names, as a client reading the
// vocabulary would see them.
func propsOf(t *testing.T, typeName string) map[string]bool {
	t.Helper()
	for _, ti := range protocol.DescribeVocabulary().Types {
		if ti.Name != typeName {
			continue
		}
		out := make(map[string]bool, len(ti.Props))
		for _, p := range ti.Props {
			out[p.Name] = true
		}
		return out
	}
	t.Fatalf("%s is not a registered type", typeName)
	return nil
}

// Banding is spelled "ledger", and only "ledger".
//
// A list used to answer to two names for it. Both set the same field and the
// paint path had one branch, so the second name bought nothing and cost a
// reader the question of whether they differed -- and a tree only ever had
// "ledger", so a script moving a property between the two had to know which
// name went where.
func TestLedgerIsTheOnlyNameForBanding(t *testing.T) {
	list := propsOf(t, "listview")
	if !list["ledger"] {
		t.Error("listview does not offer ledger")
	}
	if list["alternate_rows"] {
		t.Error("listview still offers alternate_rows")
	}
	if tree := propsOf(t, "treeview"); !tree["ledger"] {
		t.Error("treeview does not offer ledger; the two should spell it the same")
	}
}

// The switch itself still works, under the name that survived.
func TestSetLedgerBandsAList(t *testing.T) {
	l := NewListView()
	if l.ledger {
		t.Fatal("a fresh list starts banded")
	}
	l.SetLedger(true)
	if !l.ledger {
		t.Error("SetLedger(true) did not turn banding on")
	}
	l.SetLedger(false)
	if l.ledger {
		t.Error("SetLedger(false) did not turn it off")
	}
}
