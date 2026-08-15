package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/protocol"
)

// Wire registration for ListView. Rows are shared virtual items
// (items_protocol.go); nesting is a treeview affair:
//
//	new listview children={
//	    new item caption="Alpha"
//	    new item caption="Beta"
//	} selected=0
func init() {
	protocol.RegisterType("listview", &protocol.TypeSpec{
		New: func() any { return NewListView() },
		ID: func(t any) uint64 {
			return uint64(t.(*ListView).ObjectID())
		},
		Bind: func(ctx *protocol.BindContext, target any) {
			l := target.(*ListView)
			id := uint64(l.ObjectID())
			l.SetOnCurrentChanged(func(index int) {
				ctx.EmitEvent(protocol.NewEvent("change").
					WithUint("trinket", id).WithInt("selected", index))
			})
			l.SetOnItemActivated(func(index int) {
				ctx.EmitEvent(protocol.NewEvent("activate").
					WithUint("trinket", id).WithInt("selected", index))
			})
		},
		Props: map[string]protocol.Property{
			"selected":       intProp("selected", (*ListView).SetCurrentIndex).Tip("Selected row index (-1 = none).").Def("-1"),
			"alternate_rows": boolProp("alternate_rows", (*ListView).SetAlternateRowColors).Tip("Shade alternate rows.").Def("false"),
			"ledger":         boolProp("ledger", (*ListView).SetLedger).Tip("Alternate non-selected rows in the ledger colors.").Def("false"),
		},
		Append: func(parent, child any) error {
			l, ok := parent.(*ListView)
			if !ok {
				return fmt.Errorf("listview: wrong parent type %T", parent)
			}
			it, ok := child.(*wireItem)
			if !ok {
				return fmt.Errorf("listview: children must be items, got %T", child)
			}
			if len(it.children) != 0 {
				return fmt.Errorf("listview: items cannot nest (use a treeview)")
			}
			l.AddTextItem(it.caption)
			return nil
		},
		Destroy: func(t any) error {
			return destroyTrinket(t.(*ListView))
		},
	})
}
